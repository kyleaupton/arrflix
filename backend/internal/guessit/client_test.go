package guessit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthy_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if !c.Healthy(context.Background()) {
		t.Fatal("expected healthy")
	}
}

func TestHealthy_Down(t *testing.T) {
	c := NewClient("http://127.0.0.1:0") // nothing listening
	if c.Healthy(context.Background()) {
		t.Fatal("expected unhealthy")
	}
}

func TestParseBatch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var filenames []string
		if err := json.NewDecoder(r.Body).Decode(&filenames); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(filenames) != 2 {
			t.Fatalf("expected 2 filenames, got %d", len(filenames))
		}

		title1 := "South Park"
		season1, episode1 := 1, 1
		title2 := "The Matrix"
		year2 := 1999
		results := []ParseResult{
			{Title: &title1, Season: &season1, Episode: &episode1},
			{Title: &title2, Year: &year2},
		}
		json.NewEncoder(w).Encode(results)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	results, err := c.ParseBatch(context.Background(), []string{
		"South Park S01E01.mkv",
		"The Matrix (1999)/The Matrix.mkv",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title == nil || *results[0].Title != "South Park" {
		t.Errorf("expected title 'South Park', got %v", results[0].Title)
	}
	if results[0].Season == nil || *results[0].Season != 1 {
		t.Errorf("expected season 1, got %v", results[0].Season)
	}
	if results[1].Year == nil || *results[1].Year != 1999 {
		t.Errorf("expected year 1999, got %v", results[1].Year)
	}
}

func TestParseBatch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.ParseBatch(context.Background(), []string{"test.mkv"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseBatch_CountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 1 result for 2 inputs
		results := []ParseResult{{}}
		json.NewEncoder(w).Encode(results)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.ParseBatch(context.Background(), []string{"a.mkv", "b.mkv"})
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}
}
