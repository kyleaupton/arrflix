package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

type CacheEntry struct {
	Key         string
	Category    *string
	Response    []byte
	Status      int32
	ContentType *string
	Headers     []byte
	StoredAt    time.Time
	ExpiresAt   time.Time
}

func (r *Repository) GetApiCache(ctx context.Context, key string) (CacheEntry, bool, error) {
	row, err := r.Q.GetApiCache(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// cache miss: no rows
			return CacheEntry{}, false, nil
		}

		// real error
		return CacheEntry{}, false, apperrors.FromPg(err, "api cache lookup %q", key)
	}

	// cache miss: expired
	if !row.ExpiresAt.After(time.Now()) {
		return CacheEntry{}, false, nil
	}

	return CacheEntry{
		Key:         row.Key,
		Category:    row.Category,
		Response:    row.Response,
		Status:      row.Status,
		ContentType: row.ContentType,
		Headers:     row.Headers,
		StoredAt:    row.StoredAt,
		ExpiresAt:   row.ExpiresAt,
	}, true, nil
}

func (r *Repository) UpsertApiCache(ctx context.Context, key string, category *string, response []byte, status int, contentType *string, headers []byte, ttl time.Duration) error {
	expires := time.Now().Add(ttl)

	return apperrors.FromPg(r.Q.UpsertApiCache(ctx, dbgen.UpsertApiCacheParams{
		Key:         key,
		Category:    category,
		Response:    response,
		Status:      int32(status),
		ContentType: contentType,
		Headers:     headers,
		ExpiresAt:   expires,
	}), "upsert api cache %q", key)
}

func (r *Repository) DeleteExpiredApiCache(ctx context.Context) error {
	return apperrors.FromPg(r.Q.DeleteExpiredApiCache(ctx), "delete expired api cache")
}
