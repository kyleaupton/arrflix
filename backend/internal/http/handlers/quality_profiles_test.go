package handlers

import (
	"context"
	"testing"
)

// TestListBins verifies the bin-vocabulary endpoint returns a non-empty list
// with rendered names for both domains. ListBins is pure — it reads the parsing
// vocabulary directly and never touches the service — so a nil-service handler
// exercises the whole path.
func TestListBins(t *testing.T) {
	h := &QualityProfiles{}

	for _, domain := range []string{"movie", "series"} {
		out, err := h.ListBins(context.Background(), &QualityListBinsInput{Domain: domain})
		if err != nil {
			t.Fatalf("ListBins(%q) returned error: %v", domain, err)
		}
		if len(out.Body) == 0 {
			t.Fatalf("ListBins(%q) returned no bins", domain)
		}
		for i, opt := range out.Body {
			if opt.Name == "" {
				t.Errorf("ListBins(%q) bin %d has empty name", domain, i)
			}
		}
	}
}
