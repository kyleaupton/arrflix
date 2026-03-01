// Command testlib materialises fake media library fixtures on disk.
//
// Usage:
//
//	go run ./cmd/testlib <dir> <type> <style>   — create one fixture
//	go run ./cmd/testlib <dir>                  — create all fixtures
//
// Examples:
//
//	go run ./cmd/testlib /tmp/test-movies movie scene
//	go run ./cmd/testlib /tmp/test-series series clean
//	go run ./cmd/testlib /tmp/test-all
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kyleaupton/arrflix/internal/testutil/fakelibrary"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	dir := os.Args[1]

	switch len(os.Args) {
	case 2:
		// Create all fixtures, each in a subdirectory named after the fixture.
		for _, lib := range fakelibrary.All {
			sub := filepath.Join(dir, lib.Name)
			if err := os.MkdirAll(sub, 0o755); err != nil {
				fatal(err)
			}
			if err := fakelibrary.Create(sub, lib); err != nil {
				fatal(err)
			}
			fmt.Printf("created %s (%d files)\n", sub, len(lib.Files))
		}
	case 4:
		typ := os.Args[2]
		style := os.Args[3]
		lib, err := fakelibrary.ByTypeAndStyle(typ, style)
		if err != nil {
			fatal(err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fatal(err)
		}
		if err := fakelibrary.Create(dir, lib); err != nil {
			fatal(err)
		}
		fmt.Printf("created %s (%d files)\n", dir, len(lib.Files))
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: testlib <dir> [<type> <style>]")
	fmt.Fprintln(os.Stderr, "  type:  movie | series")
	fmt.Fprintln(os.Stderr, "  style: scene | clean | embedded-id")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "examples:")
	fmt.Fprintln(os.Stderr, "  testlib /tmp/test-movies movie scene")
	fmt.Fprintln(os.Stderr, "  testlib /tmp/test-all")
	os.Exit(1)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
