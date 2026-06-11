package repo

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// uuidFromPgtype converts a pgtype.UUID (the persistence-layer shape) into a
// google/uuid.UUID (the domain-layer shape). Invalid pgtype values map to
// uuid.Nil — the domain doesn't carry a "valid" bit, so we collapse the
// nullable wrapper at the translation boundary.
func uuidFromPgtype(p pgtype.UUID) uuid.UUID {
	if !p.Valid {
		return uuid.Nil
	}
	return uuid.UUID(p.Bytes)
}

// pgtypeFromUUID converts a domain uuid.UUID into the pgtype.UUID shape that
// SQLC-generated query parameters expect. The result is always Valid; callers
// that want a NULL-shaped pgtype should pass pgtype.UUID{} directly rather
// than going through this helper.
func pgtypeFromUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// pgtypeFromUUIDOrNull converts a domain uuid.UUID into a pgtype.UUID,
// mapping uuid.Nil to a NULL-shaped pgtype.UUID. Use this for nullable
// foreign-key columns where the domain represents absence as uuid.Nil
// rather than carrying a separate "valid" bit.
func pgtypeFromUUIDOrNull(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{Valid: false}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidPtrFromPgtype converts a nullable pgtype.UUID into a *uuid.UUID — nil
// for NULL. Use for nullable columns whose domain shape is a pointer rather
// than the uuid.Nil sentinel.
func uuidPtrFromPgtype(p pgtype.UUID) *uuid.UUID {
	if !p.Valid {
		return nil
	}
	id := uuid.UUID(p.Bytes)
	return &id
}

// pgtypeFromUUIDPtr converts a *uuid.UUID into a pgtype.UUID — NULL-shaped
// for nil. The write-side counterpart of uuidPtrFromPgtype.
func pgtypeFromUUIDPtr(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{Valid: false}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}
