package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golift.io/starr"
	"golift.io/starr/prowlarr"

	"github.com/kyleaupton/arrflix/internal/config"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type IndexerService struct {
	repo           *repo.Repository
	logger         *logger.Logger
	prowlarrURL    string
	prowlarrAPIKey string
	prowlarr       *prowlarr.Prowlarr
}

// Client returns the underlying Prowlarr client for use by the indexer adapter.
func (s *IndexerService) Client() *prowlarr.Prowlarr {
	return s.prowlarr
}

func NewIndexerService(r *repo.Repository, l *logger.Logger, c *config.Config) *IndexerService {
	url := fmt.Sprintf("http://localhost:%s", c.ProwlarrPort)
	cfg := starr.New(c.ProwlarrAPIKey, url, 60*time.Second)
	prowlarrClient := prowlarr.New(cfg)

	return &IndexerService{repo: r, logger: l, prowlarrURL: url, prowlarrAPIKey: c.ProwlarrAPIKey, prowlarr: prowlarrClient}
}

// IndexersConfigured returns all configured indexers from Jackett
func (s *IndexerService) ListConfiguredIndexers(ctx context.Context) ([]*prowlarr.IndexerOutput, error) {
	configuredIndexers, err := s.prowlarr.GetIndexersContext(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to list configured indexers")
		return nil, apperrors.BadGatewayf("prowlarr list indexers: %v", err).
			Op("IndexerService.ListConfiguredIndexers")
	}

	return configuredIndexers, nil
}

// IndexersUnconfigured returns all unconfigured indexers from Jackett
func (s *IndexerService) GetSchema(ctx context.Context) ([]any, error) {
	// hand-roll schema call
	url := fmt.Sprintf("%s/api/v1/indexer/schema", s.prowlarrURL)
	schema, err := get(url, map[string]string{
		"x-api-key":    s.prowlarrAPIKey,
		"Accept":       "application/json",
		"Content-Type": "application/json",
		"User-Agent":   "Arrflix/1.0",
	})
	if err != nil {
		return nil, apperrors.BadGatewayf("prowlarr indexer schema: %v", err).
			Op("IndexerService.GetSchema")
	}

	var schemaData []any
	err = json.Unmarshal(schema, &schemaData)
	if err != nil {
		return nil, apperrors.Internalf("unmarshal prowlarr schema: %v", err).
			Op("IndexerService.GetSchema").
			NotRetryable()
	}

	return schemaData, nil
}

// GetIndexer returns a specific indexer by ID
func (s *IndexerService) GetIndexer(ctx context.Context, indexerID int64) (*prowlarr.IndexerOutput, error) {
	res, err := s.prowlarr.GetIndexerContext(ctx, indexerID)
	if err != nil {
		return nil, apperrors.BadGatewayf("prowlarr get indexer %d: %v", indexerID, err).
			Op("IndexerService.GetIndexer")
	}

	return res, nil
}

// SaveIndexerConfig saves or updates the configuration for a specific indexer
func (s *IndexerService) SaveIndexerConfig(ctx context.Context, input *prowlarr.IndexerInput) (*prowlarr.IndexerOutput, error) {
	var res *prowlarr.IndexerOutput
	var err error

	// If ID is set, update the existing indexer, otherwise add a new one
	if input.ID != 0 {
		s.logger.Info().Int64("indexerID", input.ID).Msg("Updating indexer")
		res, err = s.prowlarr.UpdateIndexerContext(ctx, input, false)
		if err != nil {
			s.logger.Error().Int64("indexerID", input.ID).Err(err).Msg("Failed to update indexer")
			return nil, apperrors.BadGatewayf("prowlarr update indexer %d: %v", input.ID, err).
				Op("IndexerService.SaveIndexerConfig")
		}
		s.logger.Info().Int64("indexerID", input.ID).Msg("Successfully updated indexer")
	} else {
		s.logger.Info().Str("name", input.Name).Msg("Adding new indexer")
		res, err = s.prowlarr.AddIndexerContext(ctx, input)
		if err != nil {
			s.logger.Error().Str("name", input.Name).Err(err).Msg("Failed to add indexer")
			return nil, apperrors.BadGatewayf("prowlarr add indexer %q: %v", input.Name, err).
				Op("IndexerService.SaveIndexerConfig")
		}
		s.logger.Info().Int64("indexerID", res.ID).Str("name", input.Name).Msg("Successfully added indexer")
	}

	return res, nil
}

// DeleteIndexer removes an indexer by its ID
func (s *IndexerService) DeleteIndexer(ctx context.Context, indexerID int64) error {
	s.logger.Info().Int64("indexerID", indexerID).Msg("Deleting indexer")

	err := s.prowlarr.DeleteIndexerContext(ctx, indexerID)
	if err != nil {
		s.logger.Error().Int64("indexerID", indexerID).Err(err).Msg("Failed to delete indexer")
		return apperrors.BadGatewayf("prowlarr delete indexer %d: %v", indexerID, err).
			Op("IndexerService.DeleteIndexer")
	}

	s.logger.Info().Int64("indexerID", indexerID).Msg("Successfully deleted indexer")
	return nil
}

// ToggleIndexer toggles the enable state of an indexer
func (s *IndexerService) ToggleIndexer(ctx context.Context, indexerID int64) (*prowlarr.IndexerOutput, error) {
	s.logger.Info().Int64("indexerID", indexerID).Msg("Toggling indexer enable state")

	// Get the indexer config from Prowlarr
	indexer, err := s.prowlarr.GetIndexerContext(ctx, indexerID)
	if err != nil {
		s.logger.Error().Int64("indexerID", indexerID).Err(err).Msg("Failed to get indexer")
		return nil, apperrors.BadGatewayf("prowlarr get indexer %d: %v", indexerID, err).
			Op("IndexerService.ToggleIndexer")
	}

	// Convert FieldOutput to FieldInput
	fields := make([]*starr.FieldInput, len(indexer.Fields))
	for i, field := range indexer.Fields {
		fields[i] = &starr.FieldInput{
			Name:  field.Name,
			Value: field.Value,
		}
	}

	// Create input with toggled enable state
	input := &prowlarr.IndexerInput{
		ID:             indexer.ID,
		Enable:         !indexer.Enable, // Toggle the boolean
		Redirect:       indexer.Redirect,
		Priority:       indexer.Priority,
		AppProfileID:   indexer.AppProfileID,
		ConfigContract: indexer.ConfigContract,
		Implementation: indexer.Implementation,
		Name:           indexer.Name,
		Protocol:       indexer.Protocol,
		Tags:           indexer.Tags,
		Fields:         fields,
	}

	// Save the updated configuration
	result, err := s.SaveIndexerConfig(ctx, input)
	if err != nil {
		s.logger.Error().Int64("indexerID", indexerID).Err(err).Msg("Failed to toggle indexer")
		// SaveIndexerConfig already returns a typed error.
		return nil, err
	}

	s.logger.Info().Int64("indexerID", indexerID).Bool("enabled", result.Enable).Msg("Successfully toggled indexer")
	return result, nil
}

func (s *IndexerService) Action(ctx context.Context, actionName string, input interface{}) (any, error) {
	// hand-roll action call
	url := fmt.Sprintf("%s/api/v1/indexer/action/%s", s.prowlarrURL, actionName)

	// Convert input to JSON bytes
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, apperrors.Internalf("marshal indexer action input: %v", err).
			Op("IndexerService.Action").
			NotRetryable()
	}

	// Add the missing headers that Prowlarr expects
	headers := map[string]string{
		"x-api-key":         s.prowlarrAPIKey,
		"Accept":            "application/json, text/javascript, */*; q=0.01",
		"Content-Type":      "application/json",
		"User-Agent":        "Arrflix/1.0",
		"X-Prowlarr-Client": "true",
		"X-Requested-With":  "XMLHttpRequest",
		"Origin":            s.prowlarrURL,
		"Referer":           fmt.Sprintf("%s/", s.prowlarrURL),
		"Sec-Fetch-Dest":    "empty",
		"Sec-Fetch-Mode":    "cors",
		"Sec-Fetch-Site":    "same-origin",
	}

	actionBytes, err := post(url, headers, bytes.NewReader(inputJSON))
	if err != nil {
		s.logger.Error().Err(err).Str("action", actionName).Msg("Failed to perform indexer action")
		return nil, apperrors.BadGatewayf("prowlarr indexer action %q: %v", actionName, err).
			Op("IndexerService.Action")
	}

	// Parse the JSON response to avoid base64 encoding when serializing
	var action any
	err = json.Unmarshal(actionBytes, &action)
	if err != nil {
		return nil, apperrors.Internalf("unmarshal indexer action response: %v", err).
			Op("IndexerService.Action").
			NotRetryable()
	}

	return action, nil
}

// TestIndexer tests an unsaved indexer configuration
func (s *IndexerService) TestIndexer(ctx context.Context, input *prowlarr.IndexerInput) (*model.IndexerTestResult, error) {
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := s.prowlarr.TestIndexerContext(testCtx, input)
	if err != nil {
		return &model.IndexerTestResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &model.IndexerTestResult{
		Success: true,
		Message: "Connection test passed",
	}, nil
}

// TestIndexerByID tests a saved indexer by ID
func (s *IndexerService) TestIndexerByID(ctx context.Context, indexerID int64) (*model.IndexerTestResult, error) {
	// Get the indexer config from Prowlarr
	indexer, err := s.prowlarr.GetIndexerContext(ctx, indexerID)
	if err != nil {
		return nil, apperrors.BadGatewayf("prowlarr get indexer %d: %v", indexerID, err).
			Op("IndexerService.TestIndexerByID")
	}

	// Convert to IndexerInput for testing
	// Convert FieldOutput to FieldInput
	fields := make([]*starr.FieldInput, len(indexer.Fields))
	for i, field := range indexer.Fields {
		fields[i] = &starr.FieldInput{
			Name:  field.Name,
			Value: field.Value,
		}
	}

	input := &prowlarr.IndexerInput{
		ID:             indexer.ID,
		Enable:         indexer.Enable,
		Redirect:       indexer.Redirect,
		Priority:       indexer.Priority,
		AppProfileID:   indexer.AppProfileID,
		ConfigContract: indexer.ConfigContract,
		Implementation: indexer.Implementation,
		Name:           indexer.Name,
		Protocol:       indexer.Protocol,
		Tags:           indexer.Tags,
		Fields:         fields,
	}

	return s.TestIndexer(ctx, input)
}

// TestAllIndexers tests all configured indexers using Prowlarr's testall endpoint
func (s *IndexerService) TestAllIndexers(ctx context.Context) ([]*model.IndexerBatchTestResult, error) {
	_, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// First, fetch all configured indexers to get their names
	indexers, err := s.prowlarr.GetIndexersContext(ctx)
	if err != nil {
		return nil, apperrors.BadGatewayf("prowlarr list indexers: %v", err).
			Op("IndexerService.TestAllIndexers")
	}

	// Create a map of indexer ID to name
	indexerNames := make(map[int64]string)
	for _, indexer := range indexers {
		indexerNames[indexer.ID] = indexer.Name
	}

	url := fmt.Sprintf("%s/api/v1/indexer/testall", s.prowlarrURL)

	headers := map[string]string{
		"x-api-key":    s.prowlarrAPIKey,
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}

	responseBytes, err := post(url, headers, nil)
	if err != nil {
		return nil, apperrors.BadGatewayf("prowlarr testall: %v", err).
			Op("IndexerService.TestAllIndexers")
	}

	// Parse Prowlarr response (returns array of test results)
	var prowlarrResults []map[string]interface{}
	if err := json.Unmarshal(responseBytes, &prowlarrResults); err != nil {
		return nil, apperrors.Internalf("parse prowlarr testall response: %v", err).
			Op("IndexerService.TestAllIndexers").
			NotRetryable()
	}

	// Convert to our result type
	results := make([]*model.IndexerBatchTestResult, 0, len(prowlarrResults))
	for _, pr := range prowlarrResults {
		result := &model.IndexerBatchTestResult{}

		// Safely extract ID
		if id, ok := pr["id"].(float64); ok {
			result.IndexerID = int64(id)
			// Look up the name from our map
			if name, found := indexerNames[result.IndexerID]; found {
				result.IndexerName = name
			}
		}

		// Safely extract isValid
		if isValid, ok := pr["isValid"].(bool); ok {
			result.Success = isValid
		}

		if !result.Success {
			if validationFailures, ok := pr["validationFailures"].([]interface{}); ok && len(validationFailures) > 0 {
				if failure, ok := validationFailures[0].(map[string]interface{}); ok {
					if msg, ok := failure["errorMessage"].(string); ok {
						result.Error = msg
					}
				}
			}
		} else {
			result.Message = "Test passed"
		}

		results = append(results, result)
	}

	return results, nil
}

// Helpers

// httpRequest makes an HTTP request to the given URL and returns the response body as a []byte.
// It returns an error if the request fails or the response status is not 2xx.
// The errors are intentionally untyped here; the public IndexerService methods
// that use this helper wrap the result into typed BadGateway errors at the
// service boundary.
func httpRequest(method, url string, headers map[string]string, body io.Reader) ([]byte, error) {
	client := &http.Client{
		Timeout: 60 * time.Second, // good default timeout
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, apperrors.Internalf("create request: %v", err).NotRetryable()
	}

	// Optional headers (can pass nil if none)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, apperrors.BadGatewayf("perform request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, apperrors.BadGatewayf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, apperrors.BadGatewayf("read response: %v", err)
	}

	return responseBody, nil
}

// get makes a GET request to the given URL and returns the response body as a []byte.
// It returns an error if the request fails or the response status is not 2xx.
func get(url string, headers map[string]string) ([]byte, error) {
	return httpRequest(http.MethodGet, url, headers, nil)
}

// post makes a POST request to the given URL and returns the response body as a []byte.
// It returns an error if the request fails or the response status is not 2xx.
func post(url string, headers map[string]string, body io.Reader) ([]byte, error) {
	return httpRequest(http.MethodPost, url, headers, body)
}
