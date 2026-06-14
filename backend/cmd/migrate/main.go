package main

import (
	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/db"
	"github.com/kyleaupton/arrflix/internal/logger"
)

// Applies all pending migrations and exits. The API also migrates on startup;
// this exists so DB-reset tooling can re-create the schema without a full boot.
func main() {
	logg := logger.New(true)
	cfg := config.Load(logg)

	if err := db.ApplyMigrations(cfg.DatabaseURL); err != nil {
		logg.Fatal().Err(err).Msg("migrate")
	}

	logg.Info().Msg("migrations applied")
}
