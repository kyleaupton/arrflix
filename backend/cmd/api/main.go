package main

import (
	"context"
	"log"
	nethttp "net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/db"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/downloader/qbittorrent"
	"github.com/kyleaupton/arrflix/internal/email"
	emailsmtp "github.com/kyleaupton/arrflix/internal/email/smtp"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/http"
	acquisitionworker "github.com/kyleaupton/arrflix/internal/jobs/acquisition"
	downloadworker "github.com/kyleaupton/arrflix/internal/jobs/download"
	enrichmentworker "github.com/kyleaupton/arrflix/internal/jobs/enrichment"
	importworker "github.com/kyleaupton/arrflix/internal/jobs/import"
	notificationworker "github.com/kyleaupton/arrflix/internal/jobs/notification"
	sessionworker "github.com/kyleaupton/arrflix/internal/jobs/session"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/notifications"
	"github.com/kyleaupton/arrflix/internal/notifications/emailadapter"
	"github.com/kyleaupton/arrflix/internal/notifications/pushadapter"
	"github.com/kyleaupton/arrflix/internal/push"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
	"github.com/kyleaupton/arrflix/internal/titlenotify"
)

func main() {
	// Logger
	logg := logger.New(true)

	// Load config
	cfg := config.Load(logg)

	// DB
	pool, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		logg.Fatal().Err(err).Msg("open db")
	}
	defer pool.Close()

	// Migrations (run on startup; idempotent, using embedded files)
	if err := db.ApplyMigrations(cfg.DatabaseURL); err != nil {
		logg.Fatal().Err(err).Msg("migrate")
	}

	// Repo
	repo := repo.New(pool)

	// Root context (drives the SSE broker's session sweeper, services, etc.)
	ctx := context.Background()

	// In-process SSE broker
	broker := sse.NewBroker(ctx)

	// Title-status notifier. Everything that moves a title's acquisition state
	// announces it here; the notifier coalesces a burst into one event per title
	// and sweeps for anything that changed without announcing itself. The two
	// closures are the only place this wiring touches the repository — the
	// notifier itself holds none.
	titles := titlenotify.New(broker, func(ctx context.Context, mediaItemID uuid.UUID) (titlenotify.Ref, error) {
		item, err := repo.GetMediaItem(ctx, mediaItemID)
		if err != nil {
			return titlenotify.Ref{}, err
		}
		if item.TmdbID == nil {
			// Clients key on TMDB id, so an item without one has no address to
			// send a kick to. Nothing to emit.
			return titlenotify.Ref{}, apperrors.NotFoundf("media item %s has no tmdb id", mediaItemID)
		}
		return titlenotify.Ref{MediaType: item.Type, TmdbID: *item.TmdbID}, nil
	}, repo.ListMediaItemsTouchedSince, logg)

	// Services
	services := service.New(ctx, repo, logg, &cfg, broker,
		service.WithJWTSecret(cfg.JWTSecret),
		service.WithTitleNotifier(titles),
	)

	// Seed settings from env vars (e.g. TMDB_API_KEY) for backwards compat
	if err := services.Settings.SeedDefaults(ctx, &cfg); err != nil {
		logg.Error().Err(err).Msg("failed to seed default settings")
	}

	// Seed the HD/4K quality-profile presets + tier bindings (idempotent,
	// non-clobbering — user edits to a preset survive a restart).
	if err := services.QualityProfiles.SeedDefaults(ctx); err != nil {
		logg.Error().Err(err).Msg("failed to seed default quality profiles")
	}

	// Downloader Manager
	downloaderRegistry := downloader.NewRegistry()
	qbittorrent.Register(downloaderRegistry)
	downloaderManager := downloader.NewManager(downloaderRegistry, repo, logg)
	if err := downloaderManager.Initialize(ctx); err != nil {
		logg.Error().Err(err).Msg("failed to initialize downloader manager")
		// Don't fatal - allow server to start even if downloaders fail
	}

	// Email Manager. Phase 1 wires the SMTP transport behind the seam; the
	// manager builds transports on demand for the test-send flow only (no
	// delivery worker yet — that's Phase 2).
	emailRegistry := email.NewRegistry()
	emailsmtp.Register(emailRegistry)
	emailManager := email.NewManager(emailRegistry, repo)

	// Push (Web Push): the VAPID identity self-generates on first boot, so push
	// needs no operator setup. EnsureConfig makes the keypair a startup invariant
	// (generated exactly once — regenerating would invalidate every subscription).
	// A failure degrades push only; the rest of the app keeps running.
	pushManager := push.NewManager(repo)
	if _, err := pushManager.EnsureConfig(ctx); err != nil {
		logg.Error().Err(err).Msg("failed to initialize VAPID config; push disabled")
	}

	// HTTP. NewServer wires chi (top-level) + humachi + Echo (catch-all);
	// the chi router is what we bind to the listener.
	srv := http.NewServer(cfg, logg, pool, services, repo, downloaderManager, emailManager, pushManager, broker)
	httpServer := &nethttp.Server{Addr: ":" + cfg.Port, Handler: srv.Router}
	go func() {
		logg.Info().Str("port", cfg.Port).Msg("http listen")
		if err := httpServer.ListenAndServe(); err != nil && err != nethttp.ErrServerClosed {
			log.Println("server stopped:", err)
		}
	}()

	// Download and import workers
	workerCtx, workerCancel := context.WithCancel(context.Background())
	services.Scanner.SetContext(workerCtx)
	dlWorker := downloadworker.New(repo, downloaderManager, logg, broker, titles)
	impWorker := importworker.New(repo, downloaderManager, logg, broker, services.Notifications, titles)
	enrichWorker := enrichmentworker.New(services.Enrichment, logg)
	acqWorker := acquisitionworker.New(repo, services.Acquisition, services.Scheduler, logg, broker, titles)
	// The notification worker drains the outbox over the in_app, email, and push
	// channels. Wiring an adapter makes template Verify demand that channel's
	// templates at construction — a missing one is a startup fatal, not a
	// first-delivery surprise. The email adapter parks mail as awaiting_config
	// until SMTP is set up (see Manager.IsConfigured); push self-configures.
	emailAdapter := emailadapter.New(emailManager, notifications.MustNewRenderer(), repo)
	pushAdapter := pushadapter.New(repo, pushManager, notifications.MustNewRenderer())
	notifWorker, err := notificationworker.New(repo, logg, notifications.InAppAdapter{}, emailAdapter, pushAdapter)
	if err != nil {
		logg.Fatal().Err(err).Msg("failed to build notification worker")
	}
	sessWorker := sessionworker.New(services.Sessions, logg)
	go titles.Run(workerCtx)
	go dlWorker.Run(workerCtx)
	go impWorker.Run(workerCtx)
	go enrichWorker.Run(workerCtx)
	go acqWorker.Run(workerCtx)
	go notifWorker.Run(workerCtx)
	go sessWorker.Run(workerCtx)

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	<-ctx.Done()
	stop()
	workerCancel()

	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = httpServer.Shutdown(shCtx)
	logg.Info().Msg("bye")
}
