package downloader

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// Manager manages downloader client instances
type Manager struct {
	registry *Registry
	clients  map[InstanceID]Client
	mu       sync.RWMutex
	repo     *repo.Repository
	logger   *logger.Logger
}

// NewManager creates a new downloader manager
func NewManager(registry *Registry, repo *repo.Repository, logg *logger.Logger) *Manager {
	return &Manager{
		registry: registry,
		clients:  make(map[InstanceID]Client),
		repo:     repo,
		logger:   logg,
	}
}

// Initialize loads all enabled downloaders from the database and initializes them
func (m *Manager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	downloaders, err := m.repo.ListDownloaders(ctx)
	if err != nil {
		return fmt.Errorf("list downloaders: %w", err)
	}

	for _, dl := range downloaders {
		// Only initialize enabled downloaders
		if !dl.Enabled {
			continue
		}

		instanceID := InstanceID(dl.ID.String())

		// Convert DB model to ConfigRecord
		rec := ConfigRecord{
			ID:       instanceID,
			Type:     Type(dl.Type),
			URL:      dl.URL,
			Username: dl.Username,
			Password: dl.Password,
			Config:   []byte(dl.ConfigJSON),
		}

		// Build client
		client, err := m.registry.Build(rec)
		if err != nil {
			// Log error but continue with other downloaders
			m.logger.Error().
				Err(err).
				Str("downloader_id", dl.ID.String()).
				Str("downloader_name", dl.Name).
				Str("downloader_type", dl.Type).
				Msg("failed to build downloader client")
			continue
		}

		// Test connectivity before adding to active clients
		testResult, err := client.Test(ctx)
		if err != nil {
			m.logger.Error().
				Err(err).
				Str("downloader_id", dl.ID.String()).
				Str("downloader_name", dl.Name).
				Str("downloader_type", dl.Type).
				Msg("failed to test downloader connection")
			continue
		}

		if !testResult.Success {
			m.logger.Warn().
				Str("downloader_id", dl.ID.String()).
				Str("downloader_name", dl.Name).
				Str("downloader_type", dl.Type).
				Str("error", testResult.Error).
				Msg("downloader connection test failed - not adding to active clients")
			continue
		}

		m.logger.Info().
			Str("downloader_id", dl.ID.String()).
			Str("downloader_name", dl.Name).
			Str("downloader_type", dl.Type).
			Str("version", testResult.Version).
			Msg("initialized downloader")

		m.clients[instanceID] = client
	}

	return nil
}

// GetClient gets a client by instance ID
func (m *Manager) GetClient(ctx context.Context, instanceID InstanceID) (Client, error) {
	m.mu.RLock()
	client, ok := m.clients[instanceID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("downloader not found: %s", instanceID)
	}

	return client, nil
}

// GetClientByID gets a client by UUID string
func (m *Manager) GetClientByID(ctx context.Context, id string) (Client, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID: %w", err)
	}

	return m.GetClient(ctx, InstanceID(parsed.String()))
}

// GetDefaultClient gets the default client for a protocol
func (m *Manager) GetDefaultClient(ctx context.Context, protocol string) (Client, error) {
	dl, err := m.repo.GetDefaultDownloader(ctx, protocol)
	if err != nil {
		return nil, fmt.Errorf("get default downloader: %w", err)
	}

	return m.GetClientByID(ctx, dl.ID.String())
}

// ListClients returns all initialized clients
func (m *Manager) ListClients(ctx context.Context) []Client {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clients := make([]Client, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}

	return clients
}

// BuildTestClient builds a fresh client instance for testing (not cached)
func (m *Manager) BuildTestClient(ctx context.Context, id string) (Client, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid UUID: %w", err)
	}

	dl, err := m.repo.GetDownloader(ctx, parsed)
	if err != nil {
		return nil, fmt.Errorf("get downloader: %w", err)
	}

	instanceID := InstanceID(dl.ID.String())
	rec := ConfigRecord{
		ID:       instanceID,
		Type:     Type(dl.Type),
		URL:      dl.URL,
		Username: dl.Username,
		Password: dl.Password,
		Config:   []byte(dl.ConfigJSON),
	}

	return m.registry.Build(rec)
}

// BuildClientFromConfig builds a client instance from a ConfigRecord directly (no DB lookup)
// This is useful for testing configurations before they are saved to the database
func (m *Manager) BuildClientFromConfig(ctx context.Context, rec ConfigRecord) (Client, error) {
	return m.registry.Build(rec)
}

// InitializeDownloader initializes a single downloader by ID
// If the downloader is disabled, it will be removed from active clients
// If enabled, it will be tested and added to active clients if the test succeeds
func (m *Manager) InitializeDownloader(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid UUID: %w", err)
	}

	dl, err := m.repo.GetDownloader(ctx, parsed)
	if err != nil {
		return fmt.Errorf("get downloader: %w", err)
	}

	instanceID := InstanceID(dl.ID.String())

	// Remove existing client if it exists (for updates)
	delete(m.clients, instanceID)

	// Only initialize if enabled
	if !dl.Enabled {
		m.logger.Info().
			Str("downloader_id", dl.ID.String()).
			Str("downloader_name", dl.Name).
			Msg("downloader disabled - removed from active clients")
		return nil
	}

	// Convert DB model to ConfigRecord
	rec := ConfigRecord{
		ID:       instanceID,
		Type:     Type(dl.Type),
		URL:      dl.URL,
		Username: dl.Username,
		Password: dl.Password,
		Config:   []byte(dl.ConfigJSON),
	}

	// Build client
	client, err := m.registry.Build(rec)
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}

	// Test connectivity before adding to active clients
	testResult, err := client.Test(ctx)
	if err != nil {
		return fmt.Errorf("test connection: %w", err)
	}

	if !testResult.Success {
		return fmt.Errorf("connection test failed: %s", testResult.Error)
	}

	m.logger.Info().
		Str("downloader_id", dl.ID.String()).
		Str("downloader_name", dl.Name).
		Str("downloader_type", dl.Type).
		Str("version", testResult.Version).
		Msg("initialized downloader")

	m.clients[instanceID] = client
	return nil
}

// RemoveClient removes a client from active clients by ID
func (m *Manager) RemoveClient(ctx context.Context, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	parsed, err := uuid.Parse(id)
	if err != nil {
		return
	}

	instanceID := InstanceID(parsed.String())
	delete(m.clients, instanceID)

	m.logger.Info().
		Str("downloader_id", id).
		Msg("removed downloader from active clients")
}

// Close closes all clients and cleans up resources
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// TODO: If clients implement io.Closer, call Close() here
	m.clients = make(map[InstanceID]Client)
	return nil
}
