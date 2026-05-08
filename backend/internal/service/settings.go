package service

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/kyleaupton/arrflix/internal/config"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/repo"
)

type SettingsService struct {
	repo     *repo.Repository
	mu       sync.RWMutex
	mem      map[string]any
	onChange map[string]func(ctx context.Context, val any) error
}

func NewSettingsService(r *repo.Repository) *SettingsService {
	return &SettingsService{
		repo:     r,
		mem:      make(map[string]any),
		onChange: make(map[string]func(ctx context.Context, val any) error),
	}
}

// OnChange registers a callback that fires after a setting is persisted.
func (s *SettingsService) OnChange(key string, fn func(ctx context.Context, val any) error) {
	s.onChange[key] = fn
}

// GetAll returns a materialized map of settings with defaults applied and caches it.
func (s *SettingsService) GetAll(ctx context.Context) (map[string]any, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(rows))
	for _, it := range rows {
		switch it.Type {
		case string(SettingText):
			var v string
			_ = json.Unmarshal(it.ValueJson, &v)
			out[it.Key] = v
		case string(SettingBool):
			var v bool
			_ = json.Unmarshal(it.ValueJson, &v)
			out[it.Key] = v
		case string(SettingInt):
			var v int64
			_ = json.Unmarshal(it.ValueJson, &v)
			out[it.Key] = v
		case string(SettingJSON):
			var v any
			_ = json.Unmarshal(it.ValueJson, &v)
			out[it.Key] = v
		default:
			var v any
			_ = json.Unmarshal(it.ValueJson, &v)
			out[it.Key] = v
		}
	}
	for k, spec := range Registry {
		if _, ok := out[k]; !ok {
			out[k] = spec.Default
		}
	}
	s.mu.Lock()
	s.mem = out
	s.mu.Unlock()

	// Mask sensitive values in the returned map
	masked := make(map[string]any, len(out))
	for k, v := range out {
		if spec, ok := Registry[k]; ok && spec.Sensitive {
			if str, ok := v.(string); ok && str != "" {
				masked[k] = "********"
			} else {
				masked[k] = v
			}
		} else {
			masked[k] = v
		}
	}
	return masked, nil
}

// GetRaw returns the unmasked value for a single key (from DB or default).
// Used internally for setup status checks and TMDB client init.
func (s *SettingsService) GetRaw(ctx context.Context, key string) (any, error) {
	spec, ok := Registry[key]
	if !ok {
		return nil, apperrors.NotFoundf("unknown setting %q", key).
			Op("SettingsService.GetRaw")
	}

	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range rows {
		if it.Key == key {
			switch it.Type {
			case string(SettingText):
				var v string
				_ = json.Unmarshal(it.ValueJson, &v)
				return v, nil
			case string(SettingBool):
				var v bool
				_ = json.Unmarshal(it.ValueJson, &v)
				return v, nil
			case string(SettingInt):
				var v int64
				_ = json.Unmarshal(it.ValueJson, &v)
				return v, nil
			default:
				var v any
				_ = json.Unmarshal(it.ValueJson, &v)
				return v, nil
			}
		}
	}
	return spec.Default, nil
}

// SeedDefaults seeds settings from environment variables if not already in the DB.
func (s *SettingsService) SeedDefaults(ctx context.Context, cfg *config.Config) error {
	if cfg.TmdbAPIKey != "" {
		raw, err := s.GetRaw(ctx, "tmdb.api_key")
		if err != nil {
			return err
		}
		if str, ok := raw.(string); ok && str == "" {
			if err := s.Set(ctx, "tmdb.api_key", cfg.TmdbAPIKey); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetUserRegion returns the user's region code for watch provider lookups.
// TODO: Make this configurable via user settings.
func (s *SettingsService) GetUserRegion(ctx context.Context) string {
	return "US"
}

func (s *SettingsService) Set(ctx context.Context, key string, val any) error {
	spec, ok := Registry[key]
	if !ok {
		return apperrors.NotFoundf("unknown setting %q", key).
			Op("SettingsService.Set")
	}
	var (
		typ = string(spec.Type)
		b   []byte
		err error
	)
	switch spec.Type {
	case SettingText:
		if _, ok := val.(string); !ok {
			return apperrors.Validation("invalid setting",
				apperrors.Field("body.value", "must be text"),
			).Op("SettingsService.Set")
		}
	case SettingBool:
		if _, ok := val.(bool); !ok {
			return apperrors.Validation("invalid setting",
				apperrors.Field("body.value", "must be boolean"),
			).Op("SettingsService.Set")
		}
	case SettingInt:
		switch t := val.(type) {
		case float64:
			val = int64(t)
		case int:
			val = int64(t)
		case int64:
		default:
			return apperrors.Validation("invalid setting",
				apperrors.Field("body.value", "must be integer"),
			).Op("SettingsService.Set")
		}
	case SettingJSON:
		// any
	}
	// Value-level validation for specific settings
	if key == "auth.signup_strategy" {
		v, _ := val.(string)
		if v != "invite_only" && v != "open" {
			return apperrors.Validation("invalid setting",
				apperrors.Field("body.value", "auth.signup_strategy must be 'invite_only' or 'open'"),
			).Op("SettingsService.Set")
		}
	}
	if b, err = json.Marshal(val); err != nil {
		return apperrors.Internalf("marshal setting value: %v", err).Op("SettingsService.Set")
	}
	if err := s.repo.Upsert(ctx, key, typ, b); err != nil {
		return err
	}
	s.mu.Lock()
	s.mem[key] = val
	s.mu.Unlock()

	if fn, ok := s.onChange[key]; ok {
		if err := fn(ctx, val); err != nil {
			return err
		}
	}
	return nil
}
