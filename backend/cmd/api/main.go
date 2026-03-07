package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/db"
	"github.com/kyleaupton/arrflix/internal/downloader"
	"github.com/kyleaupton/arrflix/internal/downloader/qbittorrent"
	"github.com/kyleaupton/arrflix/internal/http"
	downloadworker "github.com/kyleaupton/arrflix/internal/jobs/download"
	enrichmentworker "github.com/kyleaupton/arrflix/internal/jobs/enrichment"
	importworker "github.com/kyleaupton/arrflix/internal/jobs/import"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/sse"
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

	// In-process SSE broker
	broker := sse.NewBroker()

	// Services
	ctx := context.Background()
	services := service.New(ctx, repo, logg, &cfg, broker, service.WithJWTSecret(cfg.JWTSecret))

	// Seed settings from env vars (e.g. TMDB_API_KEY) for backwards compat
	if err := services.Settings.SeedDefaults(ctx, &cfg); err != nil {
		logg.Error().Err(err).Msg("failed to seed default settings")
	}

	// Downloader Manager
	downloaderRegistry := downloader.NewRegistry()
	qbittorrent.Register(downloaderRegistry)
	downloaderManager := downloader.NewManager(downloaderRegistry, repo, logg)
	if err := downloaderManager.Initialize(ctx); err != nil {
		logg.Error().Err(err).Msg("failed to initialize downloader manager")
		// Don't fatal - allow server to start even if downloaders fail
	}

	// HTTP
	e := http.NewServer(cfg, logg, pool, services, repo, downloaderManager, broker)
	go func() {
		logg.Info().Str("port", cfg.Port).Msg("http listen")
		if err := e.Start(":" + cfg.Port); err != nil {
			log.Println("server stopped:", err)
		}
	}()

	// Download and import workers
	workerCtx, workerCancel := context.WithCancel(context.Background())
	services.Scanner.SetContext(workerCtx)
	dlWorker := downloadworker.New(repo, downloaderManager, logg, broker)
	impWorker := importworker.New(repo, downloaderManager, logg, broker)
	enrichWorker := enrichmentworker.New(services.Enrichment, logg)
	go dlWorker.Run(workerCtx)
	go impWorker.Run(workerCtx)
	go enrichWorker.Run(workerCtx)

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	<-ctx.Done()
	stop()
	workerCancel()

	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = e.Shutdown(shCtx)
	logg.Info().Msg("bye")
}
