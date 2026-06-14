package parsing

import "testing"

func TestNormalizeTitleForMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"The Matrix", "matrix"},
		{"Matrix", "matrix"},
		{"The Lord of the Rings", "lordrings"},
		{"WALL-E", "walle"},
		{"Wall E", "walle"},
		{"Michael", "michael"},
		{"Boxing 2026.01.16 Nikita Tszyu vs. Michael Zerafa (2026)", "boxing20260116nikitatszyuvsmichaelzerafa2026"},
		{"", ""},
		{"The", "the"}, // all-article falls back to the raw join
	}
	for _, tt := range tests {
		if got := NormalizeTitleForMatch(tt.in); got != tt.want {
			t.Errorf("NormalizeTitleForMatch(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTitlesMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "The Matrix", "The Matrix", true},
		{"article drift", "The Matrix", "Matrix", true},
		{"punctuation drift", "WALL-E", "Wall E", true},
		{"arabic vs roman sequel", "Highlander 2", "Highlander II", true},
		// The production incident: a wrong-title release must not match.
		{"wrong movie (shared word only)", "Michael", "Boxing 2026 Nikita Tszyu vs Michael Zerafa", false},
		{"different movies", "Heat", "Inception", false},
		{"empty never matches", "", "Michael", false},
	}
	for _, tt := range tests {
		if got := TitlesMatch(tt.a, tt.b); got != tt.want {
			t.Errorf("TitlesMatch(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
