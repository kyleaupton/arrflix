// Command mklib materialises a fake media library on disk from `tree` output.
//
// It reads a directory listing — either the default Unicode box-drawing form of
// tree(1) or a flat one-path-per-line listing — and recreates every leaf as an
// empty file under a target root, building intermediate directories as it goes.
// The result is a faithful, zero-byte stand-in for someone else's library:
// enough for the scanner, parser, and matcher to chew on, since matching reads
// names and paths only, never file content.
//
// Usage:
//
//	mklib <outdir> [treefile]      — read treefile, or stdin if omitted
//	tree ... | mklib <outdir>
//
// Flags:
//
//	-n   dry run: print the paths that would be created, create nothing
//
// Example:
//
//	mklib /mnt/media-pipeline/testing/messy-movies movie_tree.txt
//	# then point an Arrflix library at /data/messy-movies and scan
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	dryRun := flag.Bool("n", false, "dry run: print paths, create nothing")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}
	outdir := flag.Arg(0)

	var input *os.File
	if flag.NArg() >= 2 {
		f, err := os.Open(flag.Arg(1))
		if err != nil {
			fatal(err)
		}
		defer func() { _ = f.Close() }()
		input = f
	} else {
		input = os.Stdin
	}

	lines, err := readLines(input)
	if err != nil {
		fatal(err)
	}

	paths := parse(lines)
	if len(paths) == 0 {
		fatal(fmt.Errorf("no files parsed from input — is this tree output?"))
	}

	if *dryRun {
		for _, p := range paths {
			fmt.Println(filepath.Join(outdir, p))
		}
		fmt.Fprintf(os.Stderr, "would create %d files under %s\n", len(paths), outdir)
		return
	}

	for _, p := range paths {
		abs := filepath.Join(outdir, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			fatal(fmt.Errorf("mkdir %s: %w", filepath.Dir(abs), err))
		}
		if err := os.WriteFile(abs, nil, 0o644); err != nil {
			fatal(fmt.Errorf("write %s: %w", abs, err))
		}
	}
	fmt.Fprintf(os.Stderr, "created %d files under %s\n", len(paths), outdir)
}

// footer matches tree(1)'s trailing summary line, e.g. "734 directories, 3549 files".
var footer = regexp.MustCompile(`^\d+ director(?:y|ies), \d+ files?$`)

// readLines slurps all input lines. tree paths are short, but the buffer is
// generous so a pathological line never silently truncates a path.
func readLines(f *os.File) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

// parse turns tree output into a sorted, deduplicated list of relative file
// paths. It auto-detects box-drawing vs. flat input.
func parse(lines []string) []string {
	box := false
	for _, l := range lines {
		if strings.ContainsAny(l, "├└│") {
			box = true
			break
		}
	}
	if box {
		return parseBox(lines)
	}
	return parseFlat(lines)
}

// node is one parsed tree entry: its indentation depth and its name.
type node struct {
	depth int
	name  string
}

// parseBox parses the default Unicode tree layout. Indentation is built from
// 4-column groups ("│   " or "    ") followed by a "├── " / "└── " connector;
// depth is the count of those groups. The root header line (no connector) is
// dropped, so depth-0 entries land directly under outdir.
//
// File-vs-directory is decided structurally: a node is a directory iff the next
// entry is nested deeper, so only leaves are emitted as files. (A genuinely
// empty directory therefore materialises as an extensionless empty file — the
// scanner ignores it, and these dumps effectively never contain one.)
func parseBox(lines []string) []string {
	var nodes []node
	for _, line := range lines {
		runes := []rune(line)
		conn := connectorIndex(runes)
		if conn < 0 {
			// Header or blank line — not an entry.
			continue
		}
		nodes = append(nodes, node{
			depth: conn / 4,
			name:  string(runes[conn+4:]),
		})
	}

	var paths []string
	stack := make([]string, 0, 16)
	for i, n := range nodes {
		stack = append(stack[:n.depth], n.name)
		isDir := i+1 < len(nodes) && nodes[i+1].depth > n.depth
		if !isDir {
			paths = append(paths, filepath.Join(stack...))
		}
	}
	return paths
}

// connectorIndex returns the rune index of the "├"/"└" connector, or -1 if the
// line has none (header, blank, or footer).
func connectorIndex(runes []rune) int {
	for i, r := range runes {
		if r == '├' || r == '└' {
			return i
		}
	}
	return -1
}

// parseFlat handles flat, one-path-per-line input (e.g. `tree -fi --noreport`
// or `find -type f`). Directory lines (trailing slash) and the summary footer
// are skipped; everything else is taken verbatim as a relative path.
func parseFlat(lines []string) []string {
	var paths []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasSuffix(line, "/") || footer.MatchString(line) {
			continue
		}
		paths = append(paths, filepath.Clean(line))
	}
	return paths
}

func usage() {
	fmt.Fprint(os.Stderr, `mklib — materialise a fake media library from tree output

usage:
  mklib <outdir> [treefile]      read treefile, or stdin if omitted
  tree ... | mklib <outdir>

flags:
  -n   dry run: print paths, create nothing

example:
  mklib /mnt/media-pipeline/testing/messy-movies movie_tree.txt
`)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
