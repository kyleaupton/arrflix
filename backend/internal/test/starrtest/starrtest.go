// Package starrtest spins up live Sonarr/Radarr containers via testcontainers
// and exposes their /api/v3/parse endpoint. It is the ground-truth reference
// for the parser parity suite (see internal/test/parity): we run the corpus
// through real Sonarr/Radarr and capture what they produce.
//
// It is the Sonarr/Radarr analogue of dbtest (postgres) and tmdbtest (TMDB):
// reusable test infrastructure, imported only by build-tagged tests. Like
// dbtest it spins sibling containers on the host daemon (the dev container
// mounts /var/run/docker.sock for exactly this).
//
// Typical usage:
//
//	sonarr, err := starrtest.StartSonarr(ctx)
//	if err != nil { ... }
//	defer sonarr.Terminate(ctx)
//	raw, err := sonarr.Parse(ctx, "The.Series.S01E01.1080p.WEB-DL.x264-GROUP")
package starrtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// APIKey is the fixed 32-char key seeded into config.xml. Auth is otherwise
// disabled; this key just satisfies the API's X-Api-Key requirement.
const APIKey = "a0b1c2d3e4f5061728394a5b6c7d8e9f"

// Pinned image tags — latest stable at time of writing (Sonarr v4, Radarr v6;
// the spec said "v5" but Radarr's current stable line is v6). Bump deliberately
// via a parity refresh PR so goldens never drift silently. (*Instance).Version
// reports the running build so a regenerated golden can stamp its provenance.
const (
	sonarrImage = "linuxserver/sonarr:4.0.17.2952-ls312"
	radarrImage = "linuxserver/radarr:6.1.1.10360-ls303"
)

// startupTimeout is generous because the first boot runs DB migrations before
// the API answers.
const startupTimeout = 3 * time.Minute

// appConfig captures the per-app differences between Sonarr and Radarr.
type appConfig struct {
	image        string
	internalPort string // container-internal API port
	instanceName string
}

// Instance is a running Sonarr or Radarr container ready to parse.
type Instance struct {
	ctr     testcontainers.Container
	baseURL string
	client  *http.Client
}

// StartSonarr boots a Sonarr container and waits for its API to answer.
func StartSonarr(ctx context.Context) (*Instance, error) {
	return start(ctx, appConfig{image: sonarrImage, internalPort: "8989", instanceName: "Sonarr"})
}

// StartRadarr boots a Radarr container and waits for its API to answer.
func StartRadarr(ctx context.Context) (*Instance, error) {
	return start(ctx, appConfig{image: radarrImage, internalPort: "7878", instanceName: "Radarr"})
}

func start(ctx context.Context, cfg appConfig) (*Instance, error) {
	port := cfg.internalPort + "/tcp"

	req := testcontainers.ContainerRequest{
		Image:        cfg.image,
		ExposedPorts: []string{port},
		// PUID/PGID 0 so the app runs as root and can read+rewrite the
		// root-owned config.xml we inject below.
		Env: map[string]string{"PUID": "0", "PGID": "0", "TZ": "Etc/UTC"},
		Files: []testcontainers.ContainerFile{{
			Reader:            strings.NewReader(configXML(cfg.internalPort, cfg.instanceName)),
			ContainerFilePath: "/config/config.xml",
			FileMode:          0o644,
		}},
		WaitingFor: wait.ForHTTP("/api/v3/system/status").
			WithPort(port).
			WithHeaders(map[string]string{"X-Api-Key": APIKey}).
			WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }).
			WithStartupTimeout(startupTimeout),
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("starrtest: start %s: %w", cfg.instanceName, err)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		_ = ctr.Terminate(ctx)
		return nil, fmt.Errorf("starrtest: host: %w", err)
	}
	mapped, err := ctr.MappedPort(ctx, port)
	if err != nil {
		_ = ctr.Terminate(ctx)
		return nil, fmt.Errorf("starrtest: mapped port: %w", err)
	}

	return &Instance{
		ctr:     ctr,
		baseURL: fmt.Sprintf("http://%s:%s", host, mapped.Port()),
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Terminate stops and removes the container.
func (i *Instance) Terminate(ctx context.Context) error {
	return i.ctr.Terminate(ctx)
}

// Parse calls GET /api/v3/parse?title=... and returns the raw JSON response
// body. We deliberately return raw bytes rather than a typed struct: the whole
// point of the reference is to capture everything the app emits, so the golden
// reflects the true output shape (which informs the ParsedRelease model).
func (i *Instance) Parse(ctx context.Context, title string) (json.RawMessage, error) {
	u := i.baseURL + "/api/v3/parse?" + url.Values{"title": {title}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("starrtest: build request: %w", err)
	}
	req.Header.Set("X-Api-Key", APIKey)

	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("starrtest: parse %q: %w", title, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("starrtest: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("starrtest: parse %q: status %d: %s", title, resp.StatusCode, body)
	}

	return json.RawMessage(body), nil
}

// Version returns the app's reported version (from /api/v3/system/status), so a
// regenerated golden can record which Sonarr/Radarr build produced it.
func (i *Instance) Version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.baseURL+"/api/v3/system/status", nil)
	if err != nil {
		return "", fmt.Errorf("starrtest: build status request: %w", err)
	}
	req.Header.Set("X-Api-Key", APIKey)

	resp, err := i.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("starrtest: status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var status struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return "", fmt.Errorf("starrtest: decode status: %w", err)
	}
	return status.Version, nil
}

// configXML renders a minimal config.xml that fixes the API key and disables
// interactive auth (External = an upstream proxy "handles" it, so the app never
// prompts; the API still honors X-Api-Key).
func configXML(port, instanceName string) string {
	return fmt.Sprintf(`<Config>
  <BindAddress>*</BindAddress>
  <Port>%s</Port>
  <UrlBase></UrlBase>
  <EnableSsl>False</EnableSsl>
  <ApiKey>%s</ApiKey>
  <AuthenticationMethod>External</AuthenticationMethod>
  <AuthenticationRequired>DisabledForLocalAddresses</AuthenticationRequired>
  <LaunchBrowser>False</LaunchBrowser>
  <AnalyticsEnabled>False</AnalyticsEnabled>
  <Branch>main</Branch>
  <LogLevel>info</LogLevel>
  <InstanceName>%s</InstanceName>
</Config>
`, port, APIKey, instanceName)
}
