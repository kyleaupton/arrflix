package config

import (
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/kyleaupton/arrflix/internal/logger"
)

type Config struct {
	Env            string // dev|prod
	Port           string // API internal port, default 8080
	DatabaseURL    string
	CORSOrigin     string        // used for dev SPA
	JWTSecret      string        // HMAC secret for JWT signing
	TmdbAPIKey     string        // TMDB API key
	ProwlarrPort   string        // Prowlarr port, default 9696
	ProwlarrAPIKey string        // Prowlarr API key
	EnableAPIDocs  bool          // serve /api/docs + /api/openapi.{json,yaml}; default on in dev, off in prod
	AccessTokenTTL time.Duration // access-token lifetime; Phase 1 keeps 24h parity with the legacy token
	SessionTTL     time.Duration // refresh-token / session absolute lifetime
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envBoolOr(k string, d bool) bool {
	v := os.Getenv(k)
	switch v {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true
	case "0", "false", "FALSE", "False", "no", "off":
		return false
	}
	return d
}

func envDurationOr(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			return parsed
		}
	}
	return d
}

func Load(log *logger.Logger) Config {
	// Best effort to load .env file
	_ = godotenv.Load()

	env := envOr("APP_ENV", "prod")
	config := Config{
		Env:            env,
		Port:           envOr("PORT", "8080"),
		DatabaseURL:    envOr("DATABASE_URL", "postgres://arrflix:arrflixpw@127.0.0.1:5432/arrflix?sslmode=disable"),
		CORSOrigin:     envOr("SSE_ALLOW_ORIGIN", "*"),
		JWTSecret:      envOr("JWT_SECRET", "dev-insecure-change-me"),
		TmdbAPIKey:     envOr("TMDB_API_KEY", ""),
		ProwlarrPort:   envOr("PROWLARR_PORT", "9696"),
		ProwlarrAPIKey: envOr("PROWLARR_API_KEY", "prowlarr-api-key"),
		EnableAPIDocs:  envBoolOr("ENABLE_API_DOCS", env == "dev"),
		AccessTokenTTL: envDurationOr("ACCESS_TOKEN_TTL", 24*time.Hour),
		SessionTTL:     envDurationOr("SESSION_TTL", 90*24*time.Hour),
	}

	log.Debug().Interface("config", config).Msg("config")

	return config
}
