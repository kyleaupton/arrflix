package errors

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Postgres SQLSTATE codes we recognize. Full list at
// https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgNotNullViolation    = "23502"
	pgCheckViolation      = "23514"
)

// FromPg converts a pgx/postgres error into the appropriate kind, using the
// supplied format string for the user-facing detail. It is the standard
// translation point at the repo layer; services call repo methods and pass
// results through unchanged.
//
// Mappings:
//   - pgx.ErrNoRows                → KindNotFound
//   - SQLSTATE 23505 (unique)      → KindConflict
//   - SQLSTATE 23503 (foreign key) → KindConflict
//   - SQLSTATE 23502 (not null)    → KindValidation
//   - SQLSTATE 23514 (check)       → KindValidation
//   - anything else                → wrapped without a kind (surfaces as 500)
//
// Returns nil if err is nil. The original error is preserved in the wrap
// chain for logging.
func FromPg(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	kind := KindUnspecified
	if errors.Is(err, pgx.ErrNoRows) {
		kind = KindNotFound
	} else {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case pgUniqueViolation, pgForeignKeyViolation:
				kind = KindConflict
			case pgNotNullViolation, pgCheckViolation:
				kind = KindValidation
			}
		}
	}
	return &Error{
		kind:    kind,
		msg:     fmt.Sprintf(format, args...),
		wrapped: err,
		stack:   captureStack(1),
	}
}
