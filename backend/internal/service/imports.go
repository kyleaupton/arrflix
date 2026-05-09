package service

import (
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/mediainfo"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// ImportService is the (now-vestigial) container for service-layer import
// helpers. Its previous methods (ImportMovieFile, ImportSeriesJob) were
// removed once the import worker (internal/jobs/import) became the sole
// driver of imports. The struct itself stays so service.New() still compiles
// against the existing field; future work may collapse it entirely.
type ImportService struct {
	repo      *repo.Repository
	log       *logger.Logger
	mediaInfo *mediainfo.Analyzer
}

func NewImportService(r *repo.Repository, l *logger.Logger) *ImportService {
	return &ImportService{
		repo:      r,
		log:       l,
		mediaInfo: mediainfo.NewAnalyzer(*l),
	}
}
