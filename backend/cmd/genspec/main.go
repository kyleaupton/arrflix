// genspec generates the OpenAPI 3.1 spec for the humachi-served portion of
// the arrflix API and writes it to internal/http/docs/openapi.{yaml,json}.
// Operations are registered against a nil service — huma.Register only
// inspects type metadata for spec generation, so the build stays minimal
// (no DB, no service init).
//
// Usage:
//
//	go run ./cmd/genspec
//
// The output is committed and consumed by the frontend's openapi-ts step.
// Regenerate after any handler change.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/kyleaupton/arrflix/internal/http/handlers"

	// Side-effect import: installs apperrors.ToProblem as huma's NewError.
	_ "github.com/kyleaupton/arrflix/internal/http/humaerr"
)

func main() {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Arrflix API", "0.0.1"))

	handlers.RegisterHumachiHandlers(api, handlers.Deps{})

	bytes, err := api.OpenAPI().YAML()
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi yaml:", err)
		os.Exit(1)
	}
	yamlOut := filepath.Join("internal", "http", "docs", "openapi.yaml")
	if err := os.MkdirAll(filepath.Dir(yamlOut), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir docs:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(yamlOut, bytes, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write yaml:", err)
		os.Exit(1)
	}

	jsonBytes, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi json:", err)
		os.Exit(1)
	}
	jsonOut := filepath.Join("internal", "http", "docs", "openapi.json")
	if err := os.WriteFile(jsonOut, jsonBytes, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write json:", err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s\nwrote %s\n", yamlOut, jsonOut)
}
