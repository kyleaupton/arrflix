// Command dumplib walks a media library directory and outputs its structure
// as JSON. The output can be used to create fakelibrary test fixtures from
// real-world libraries.
//
// Usage:
//
//	go run ./cmd/dumplib <dir> [type]
//
// Arguments:
//
//	dir  — path to the library root
//	type — "movie" or "series" (optional, defaults to "unknown")
//
// The output is a JSON object matching the fakelibrary.Library struct,
// ready to be saved and loaded with fakelibrary.LoadJSON.
//
// Example:
//
//	go run ./cmd/dumplib /mnt/media/movies movie > my-movies.json
//	go run ./cmd/dumplib /mnt/media/tv series > my-series.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type output struct {
	Name  string   `json:"name"`
	Type  string   `json:"type"`
	Files []string `json:"files"`
}

// Media file extensions the scanner recognises.
var mediaExts = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".avi":  true,
	".mov":  true,
	".wmv":  true,
	".flv":  true,
	".m4v":  true,
	".webm": true,
}

// Common sidecar extensions to include for realistic fixtures.
var sidecarExts = map[string]bool{
	".nfo": true,
	".srt": true,
	".sub": true,
	".idx": true,
	".ass": true,
	".ssa": true,
	".jpg": true,
	".png": true,
	".nzb": true,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: dumplib <dir> [movie|series]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Walks a media library and outputs its structure as JSON.")
		fmt.Fprintln(os.Stderr, "Send the output to the Arrflix devs so they can reproduce your layout in tests.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "examples:")
		fmt.Fprintln(os.Stderr, "  dumplib /mnt/media/movies movie > my-movies.json")
		fmt.Fprintln(os.Stderr, "  dumplib /mnt/media/tv series > my-series.json")
		os.Exit(1)
	}

	dir := os.Args[1]
	typ := "unknown"
	if len(os.Args) >= 3 {
		typ = os.Args[2]
	}

	// Resolve to absolute so filepath.Rel works reliably.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		fatal(err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		fatal(err)
	}
	if !info.IsDir() {
		fatal(fmt.Errorf("%s is not a directory", absDir))
	}

	var files []string

	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip permission errors, keep walking.
			fmt.Fprintf(os.Stderr, "warn: %v\n", err)
			return nil
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !mediaExts[ext] && !sidecarExts[ext] {
			return nil
		}

		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		fatal(err)
	}

	name := filepath.Base(absDir) + "-dump"

	out := output{
		Name:  name,
		Type:  typ,
		Files: files,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fatal(err)
	}

	fmt.Fprintf(os.Stderr, "dumped %d files from %s\n", len(files), absDir)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
