// genspec generates the OpenAPI 3.1 spec for the humachi-served portion
// of the arrflix API and writes it to backend/internal/http/docs/openapi.json.
//
// As of phase 2 of the humachi migration, the libraries handler is registered
// on this API as the reference vertical; phase 3 adds the rest of the
// handlers as they migrate. This entrypoint registers operations against a
// nil service (the registration only inspects type information for spec
// generation — no service methods are actually invoked), so the build stays
// minimal: no DB, no service init, no logger.
//
// Usage:
//
//	go run ./cmd/genspec
//
// The output (openapi.yaml + openapi.json) is committed to the repo and is
// what the frontend's openapi-ts step consumes. Regenerate after any
// humachi handler change.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/kyleaupton/arrflix/internal/config"
	"github.com/kyleaupton/arrflix/internal/http/handlers"

	// Imported for the side effect of huma.NewError wiring (apperrors.ToProblem).
	_ "github.com/kyleaupton/arrflix/internal/http/humaerr"
)

func main() {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Arrflix API", "0.0.1"))

	// Phase 2: register the libraries handler so its operations appear in
	// the spec. The handler is constructed against a nil service — huma.Register
	// only inspects the input/output type metadata to build the spec, it
	// does not invoke the handler function bodies.
	handlers.NewLibraries(nil).RegisterHumachi(api)

	// Phase 3 wave 1.
	handlers.NewDownloaders(nil, nil).RegisterHumachi(api)
	handlers.NewNameTemplates(nil).RegisterHumachi(api)
	handlers.NewPolicies(nil).RegisterHumachi(api)
	handlers.NewSettings(nil).RegisterHumachi(api)
	handlers.NewInvites(nil).RegisterHumachi(api)
	handlers.NewUsers(nil).RegisterHumachi(api)
	handlers.NewRoles(nil).RegisterHumachi(api)

	// Phase 3 wave 2a: auth, setup, media. Same nil-service trick —
	// huma.Register reads only the operation metadata.
	handlers.NewAuth(config.Config{}, nil, nil, nil).RegisterHumachi(api)
	handlers.NewSetup(nil).RegisterHumachi(api)
	handlers.NewMedia(nil).RegisterHumachi(api)

	// Phase 3 wave 2b: events, download-jobs, import-tasks.
	handlers.NewEvents(nil, nil).RegisterHumachi(api)
	handlers.NewDownloadJobs(nil).RegisterHumachi(api)
	handlers.NewImportTasks(nil).RegisterHumachi(api)

	// Phase 3 wave 2c: bootstrap, health, version, download-candidates,
	// filesystem, feed, indexers, unmatched-files.
	handlers.NewBootstrap(config.Config{}, nil).RegisterHumachi(api)
	handlers.NewHealth().RegisterHumachi(api)
	handlers.NewVersion(nil).RegisterHumachi(api)
	handlers.NewDownloadCandidates(nil).RegisterHumachi(api)
	handlers.NewFilesystem(nil).RegisterHumachi(api)
	handlers.NewFeed(nil).RegisterHumachi(api)
	handlers.NewIndexers(nil).RegisterHumachi(api)
	handlers.NewUnmatchedFiles(nil).RegisterHumachi(api)

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
