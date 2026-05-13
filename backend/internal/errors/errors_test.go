package errors_test

import (
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

func TestKindOfWalksWrapChain(t *testing.T) {
	t.Parallel()
	base := apperrors.NotFoundf("library %s", "abc")
	wrapped := fmt.Errorf("get library: %w", base)
	doubleWrapped := fmt.Errorf("handler: %w", wrapped)

	if got := apperrors.KindOf(doubleWrapped); got != apperrors.KindNotFound {
		t.Fatalf("KindOf through fmt.Errorf chain = %q, want %q", got, apperrors.KindNotFound)
	}
	if !apperrors.IsNotFound(doubleWrapped) {
		t.Fatal("IsNotFound should match through wrap chain")
	}
}

func TestHTTPStatusByKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want int
	}{
		{apperrors.NotFoundf("x"), 404},
		{apperrors.Conflictf("x"), 409},
		{apperrors.Validation("x"), 422},
		{apperrors.Forbiddenf("x"), 403},
		{apperrors.Unauthenticatedf("x"), 401},
		{apperrors.BadGatewayf("x"), 502},
		{apperrors.Internalf("x"), 500},
		{stderrors.New("naked"), 500},
		{nil, 500},
	}
	for _, c := range cases {
		if got := apperrors.HTTPStatus(c.err); got != c.want {
			t.Errorf("HTTPStatus(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestValidationFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	err := apperrors.Validation("input invalid",
		apperrors.Field("body.name", "required"),
		apperrors.Field("body.type", "must be 'movie' or 'series'"))

	fields := apperrors.FieldsOf(err)
	if len(fields) != 2 {
		t.Fatalf("FieldsOf len = %d, want 2", len(fields))
	}
	if fields[0].Location != "body.name" || fields[0].Message != "required" {
		t.Errorf("field[0] = %+v", fields[0])
	}

	pd := apperrors.ToProblem(err)
	if pd.Status != 422 {
		t.Errorf("problem.Status = %d, want 422", pd.Status)
	}
	if pd.Type != "/errors/validation" {
		t.Errorf("problem.Type = %q, want /errors/validation", pd.Type)
	}
	if len(pd.Errors) != 2 {
		t.Errorf("problem.Errors len = %d, want 2", len(pd.Errors))
	}
}

func TestToProblemHidesInternalDetail(t *testing.T) {
	t.Parallel()
	err := apperrors.Internalf("sql: column \"secret_token\" does not exist")
	pd := apperrors.ToProblem(err)
	if pd.Detail == err.Error() {
		t.Error("internal error detail leaked to wire format")
	}
	if pd.Detail != "an internal error occurred" {
		t.Errorf("internal Detail = %q", pd.Detail)
	}

	naked := stderrors.New("internal sql error")
	pd2 := apperrors.ToProblem(naked)
	if pd2.Detail == naked.Error() {
		t.Error("naked error detail leaked to wire format")
	}
}

func TestRetryableDerivedFromKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{apperrors.NotFoundf("x"), false},
		{apperrors.Conflictf("x"), false},
		{apperrors.Validation("x"), false},
		{apperrors.Forbiddenf("x"), false},
		{apperrors.Unauthenticatedf("x"), false},
		{apperrors.BadGatewayf("x"), true},
		{apperrors.Internalf("x"), true},
		{stderrors.New("naked"), true},
	}
	for _, c := range cases {
		if got := apperrors.IsRetryable(c.err); got != c.want {
			t.Errorf("IsRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestNotRetryableOverridesKindRetry(t *testing.T) {
	t.Parallel()
	// BadGateway is retryable by default; .NotRetryable() should flip it.
	err := apperrors.BadGatewayf("upstream down").NotRetryable()
	if apperrors.IsRetryable(err) {
		t.Error("NotRetryable did not override BadGateway's retryable default")
	}
	// Kind survives the override.
	if !apperrors.IsBadGateway(err) {
		t.Error("NotRetryable dropped the kind")
	}
}

func TestFromPgMapsNoRowsToNotFound(t *testing.T) {
	t.Parallel()
	err := apperrors.FromPg(pgx.ErrNoRows, "library %s", "abc")
	if !apperrors.IsNotFound(err) {
		t.Fatal("pgx.ErrNoRows did not map to NotFound")
	}
	if !stderrors.Is(err, pgx.ErrNoRows) {
		t.Error("FromPg should preserve pgx.ErrNoRows in wrap chain")
	}

	other := fmt.Errorf("connection refused")
	err2 := apperrors.FromPg(other, "context")
	if apperrors.IsNotFound(err2) {
		t.Error("non-NoRows error should not become NotFound")
	}
}

func TestFromPgMapsConstraintCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code     string
		wantKind apperrors.Kind
	}{
		{"23505", apperrors.KindConflict},    // unique_violation
		{"23503", apperrors.KindConflict},    // foreign_key_violation
		{"23502", apperrors.KindValidation},  // not_null_violation
		{"23514", apperrors.KindValidation},  // check_violation
		{"42P01", apperrors.KindUnspecified}, // undefined_table → falls through (500)
	}
	for _, c := range cases {
		pgErr := &pgconn.PgError{Code: c.code, Message: "violation"}
		got := apperrors.FromPg(pgErr, "library")
		if k := apperrors.KindOf(got); k != c.wantKind {
			t.Errorf("code %s: KindOf = %q, want %q", c.code, k, c.wantKind)
		}
	}
}

func TestFromPgNilPassthrough(t *testing.T) {
	t.Parallel()
	if got := apperrors.FromPg(nil, "x"); got != nil {
		t.Errorf("FromPg(nil) = %v, want nil", got)
	}
}

func TestWithOpRecordsOp(t *testing.T) {
	t.Parallel()
	err := apperrors.WithOp(apperrors.NotFoundf("x"), "LibrariesService.Get")
	if got := apperrors.OpOf(err); got != "LibrariesService.Get" {
		t.Errorf("OpOf = %q", got)
	}
	if !apperrors.IsNotFound(err) {
		t.Error("WithOp should preserve kind")
	}
}

func TestWrapPreservesKind(t *testing.T) {
	t.Parallel()
	base := apperrors.NotFoundf("library %s", "abc")
	wrapped := apperrors.Wrap(base, "during fetch")
	if !apperrors.IsNotFound(wrapped) {
		t.Error("Wrap should preserve kind through chain")
	}
}

func TestBuilderChainSetsKindOpAndRetry(t *testing.T) {
	t.Parallel()
	err := apperrors.Internalf("unknown protocol: %s", "magnet").
		Op("Worker.poll").
		NotRetryable()

	if !apperrors.IsInternal(err) {
		t.Error("kind lost through chain")
	}
	if got := apperrors.OpOf(err); got != "Worker.poll" {
		t.Errorf("OpOf = %q", got)
	}
	if apperrors.IsRetryable(err) {
		t.Error("NotRetryable did not stick")
	}
}

func TestBuilderMethodsAreCopyOnModify(t *testing.T) {
	t.Parallel()
	base := apperrors.Internalf("base")

	// Modify; original should be unchanged.
	withOp := base.Op("ServiceA")
	if apperrors.OpOf(base) != "" {
		t.Error("Op mutated the receiver")
	}
	if apperrors.OpOf(withOp) != "ServiceA" {
		t.Error("Op did not set on the copy")
	}

	notRetry := base.NotRetryable()
	if !apperrors.IsRetryable(base) {
		t.Error("NotRetryable mutated the receiver")
	}
	if apperrors.IsRetryable(notRetry) {
		t.Error("NotRetryable did not set on the copy")
	}
}

func TestWrapPreservesNotRetryableThroughChain(t *testing.T) {
	t.Parallel()
	// Regression: previously Wrap stripped the retry override because
	// IsRetryable only consulted the outermost *Error.
	inner := apperrors.Internalf("config bad").NotRetryable()
	outer := apperrors.Wrap(inner, "during startup")

	if apperrors.IsRetryable(outer) {
		t.Error("Wrap should not strip NotRetryable from the chain")
	}
	if !apperrors.IsInternal(outer) {
		t.Error("Wrap should preserve kind through the chain")
	}
}

func TestIsRetryableFindsOverrideThroughFmtErrorf(t *testing.T) {
	t.Parallel()
	// Override should survive even fmt.Errorf %w wrapping.
	inner := apperrors.Internalf("invariant").NotRetryable()
	wrapped := fmt.Errorf("layer: %w", inner)
	doubleWrapped := fmt.Errorf("handler: %w", wrapped)

	if apperrors.IsRetryable(doubleWrapped) {
		t.Error("override lost through fmt.Errorf %w chain")
	}
}

// TestConstructorWrapsOnW pins down: %w wraps and Error() does not duplicate.
// Previously newErr used fmt.Sprintf and a side-channel auto-wrap, producing
// strings like "downloader: network down: network down".
func TestConstructorWrapsOnW(t *testing.T) {
	t.Parallel()
	cause := stderrors.New("network down")
	err := apperrors.Internalf("downloader: %w", cause)

	if got := err.Error(); got != "downloader: network down" {
		t.Errorf("Error() = %q, want %q (no duplication)", got, "downloader: network down")
	}
	if !stderrors.Is(err, cause) {
		t.Error("%w should put cause in the wrap chain so errors.Is matches")
	}
	if !apperrors.IsInternal(err) {
		t.Error("kind should still be Internal")
	}
}

// TestConstructorDoesNotWrapOnV pins the new contract: %v formats the error
// into the message but does NOT put it in the wrap chain. This is the
// semantic change from the previous auto-wrap behavior — a future contributor
// who switches a %w to %v should see errors.Is stop matching.
func TestConstructorDoesNotWrapOnV(t *testing.T) {
	t.Parallel()
	cause := stderrors.New("network down")
	err := apperrors.Internalf("downloader: %v", cause)

	if got := err.Error(); got != "downloader: network down" {
		t.Errorf("Error() = %q, want formatted message including cause text", got)
	}
	if stderrors.Is(err, cause) {
		t.Error("verb-v formats but does not wrap; errors.Is must not match cause")
	}
	if !apperrors.IsInternal(err) {
		t.Error("kind should still be Internal")
	}
}

func TestNilSafe(t *testing.T) {
	t.Parallel()
	if apperrors.Wrap(nil, "x") != nil {
		t.Error("Wrap(nil) must return nil")
	}
	if apperrors.WithOp(nil, "x") != nil {
		t.Error("WithOp(nil) must return nil")
	}
	if apperrors.FromPg(nil, "x") != nil {
		t.Error("FromPg(nil) must return nil")
	}
}
