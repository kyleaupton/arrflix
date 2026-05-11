package repo

import (
	"context"

	"github.com/jackc/pgx/v5"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// InTx runs fn inside a database transaction.
//
// The *Repository passed to fn is a transactional view of the receiver: its Q
// is bound to the transaction, so every Q-backed repo method called on it
// participates in the same tx. Pool is carried through unchanged, so any code
// that reaches for a fresh connection via Pool bypasses the tx — use Q-backed
// methods (i.e., the regular repo methods) for everything that should be
// atomic.
//
// If fn returns nil, the tx is committed; any commit error is wrapped via
// apperrors.FromPg. If fn returns non-nil, the tx is rolled back and fn's
// error is returned unchanged (it's already typed by the callee). The
// BeginTx error is also wrapped via apperrors.FromPg.
//
// Default isolation is pgx's default (read committed), which is the right
// choice for the common case. If a callsite needs Serializable, add a
// dedicated helper rather than parameterizing this one.
func (r *Repository) InTx(ctx context.Context, fn func(*Repository) error) error {
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return apperrors.FromPg(err, "begin tx")
	}
	defer tx.Rollback(ctx)

	txRepo := &Repository{
		Pool: r.Pool,
		Q:    r.Q.WithTx(tx),
	}

	if err := fn(txRepo); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return apperrors.FromPg(err, "commit tx")
	}
	return nil
}
