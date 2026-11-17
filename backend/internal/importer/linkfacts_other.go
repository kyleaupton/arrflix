//go:build !unix

package importer

import (
	"os"

	"github.com/kyleaupton/arrflix/internal/model"
)

// LinkFactsOf returns nil: link facts are only read on unix platforms.
func LinkFactsOf(os.FileInfo) *model.LinkFacts { return nil }
