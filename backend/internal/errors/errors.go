// Package errors is arrflix's typed error model. Errors carry a Kind that
// drives HTTP status (via HTTPStatus / ToProblem) and worker retry decisions
// (via IsRetryable). Authors construct errors with the kind-specific helpers
// (NotFoundf, Conflictf, Validation, ...) and chain modifiers like Op and
// NotRetryable when needed; handlers and workers branch on KindOf or use IsXxx
// predicates.
//
// Wire format: ToProblem produces an RFC 9457 application/problem+json shape
// matching huma's default ErrorModel, so service-emitted errors and
// framework-emitted errors look identical to clients.
//
// API shape:
//
//   - Constructors (NotFoundf, Internalf, Validation, ...) return *Error so
//     they can be chained: errors.Internalf("...").Op("Foo").NotRetryable().
//   - Builder methods (Op, NotRetryable) return a copy of the receiver with
//     the field set, so chaining never mutates a shared instance.
//   - Wrap, WithOp, and FromPg take an existing error (which may be nil) and
//     return error, preserving nil-in / nil-out semantics. They are not
//     chainable.
//
// Be aware of the Go nil-interface trap: a *Error returned as `error` will
// satisfy `err != nil` even when the *Error pointer is nil. Constructors
// never return nil *Error; only the wrap-style helpers can produce nil and
// they return error (interface) for that reason.
package errors

import (
	"errors"
	"fmt"
	"log/slog"
)

// Error is arrflix's structured error type. Construct via the kind-specific
// helpers (NotFoundf, Conflictf, ...) rather than literal struct values.
type Error struct {
	kind    Kind
	msg     string       // user-safe detail (becomes RFC 9457 detail)
	op      string       // logging context, never sent on the wire
	fields  []FieldError // populated for KindValidation
	wrapped error
	stack   []uintptr

	// retryOverride lets NotRetryable force a retry decision regardless of
	// kind. nil means "derive from kind".
	retryOverride *bool
}

// FieldError describes a single field-level validation problem. The format of
// Location matches huma: body.field_name, path.id, query.param, etc.
type FieldError struct {
	Location string `json:"location"`
	Message  string `json:"message"`
	Value    any    `json:"value,omitempty"`
}

// Field constructs a FieldError without a value. Use FieldVal to include the
// rejected value.
func Field(location, message string) FieldError {
	return FieldError{Location: location, Message: message}
}

// FieldVal constructs a FieldError including the rejected value. Pass values
// that are safe to expose in API responses; do not include secrets.
func FieldVal(location, message string, value any) FieldError {
	return FieldError{Location: location, Message: message, Value: value}
}

// Error implements the error interface. Returns the user-facing detail. For
// constructors, this already includes any %w-wrapped error's message (because
// the constructor uses fmt.Errorf to build msg). For Wrap and FromPg, msg is
// the user-supplied detail only — they intentionally replace the wrapped
// error's message rather than appending to it. Op and stack are excluded
// from this output; access them via slog.LogValue.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.msg != "" {
		return e.msg
	}
	if e.wrapped != nil {
		return e.wrapped.Error()
	}
	return ""
}

// Unwrap exposes the wrapped error chain to errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.wrapped }

// LogValue makes *Error log as a structured group when passed to slog.
// Example: slog.Error("op failed", "err", err) emits kind=, op=, fields=, etc.
func (e *Error) LogValue() slog.Value {
	if e == nil {
		return slog.AnyValue(nil)
	}
	attrs := []slog.Attr{
		slog.String("kind", string(e.kind)),
		slog.String("msg", e.Error()),
	}
	if e.op != "" {
		attrs = append(attrs, slog.String("op", e.op))
	}
	if len(e.fields) > 0 {
		attrs = append(attrs, slog.Any("fields", e.fields))
	}
	if len(e.stack) > 0 {
		attrs = append(attrs, slog.String("stack", formatStack(e.stack)))
	}
	return slog.GroupValue(attrs...)
}

// Op returns a copy of e with the operation name set. Op flows to slog via
// LogValue and is never exposed on the wire. Use it to record the calling
// stack in a structured way (e.g., "LibrariesService.Get").
func (e *Error) Op(op string) *Error {
	cp := *e
	cp.op = op
	return &cp
}

// NotRetryable returns a copy of e marked as not retryable, regardless of
// kind. Workers consult this via IsRetryable. Use it for kinds whose default
// retry behavior is wrong for a specific callsite — e.g., an Internal error
// that represents an invariant violation rather than a transient failure.
func (e *Error) NotRetryable() *Error {
	cp := *e
	f := false
	cp.retryOverride = &f
	return &cp
}

// newErr is the single internal constructor; all kind helpers funnel through
// it. Format semantics match fmt.Errorf: %w wraps the corresponding error
// argument into the chain (errors.Is / errors.As traverse to it); %v / %s /
// other verbs format the value into the message but do NOT wrap. To wrap an
// error without including its text in the user-facing message, use Wrap.
func newErr(kind Kind, format string, args []any, skip int) *Error {
	formatted := fmt.Errorf(format, args...)
	return &Error{
		kind:    kind,
		msg:     formatted.Error(),
		wrapped: errors.Unwrap(formatted),
		stack:   captureStack(skip + 1),
	}
}

// NotFoundf reports that a resource does not exist. Maps to HTTP 404.
func NotFoundf(format string, args ...any) *Error {
	return newErr(KindNotFound, format, args, 1)
}

// Conflictf reports a state conflict: the request cannot be applied because of
// the resource's current state (e.g., uniqueness violation, scan already
// running). Maps to HTTP 409.
func Conflictf(format string, args ...any) *Error {
	return newErr(KindConflict, format, args, 1)
}

// Forbiddenf reports the caller is authenticated but not allowed. Maps to 403.
// Use Unauthenticatedf for missing/invalid credentials.
func Forbiddenf(format string, args ...any) *Error {
	return newErr(KindForbidden, format, args, 1)
}

// Unauthenticatedf reports missing or invalid credentials. Maps to 401.
func Unauthenticatedf(format string, args ...any) *Error {
	return newErr(KindUnauthenticated, format, args, 1)
}

// BadGatewayf reports an upstream dependency (TMDB, Prowlarr, qBittorrent)
// returned an error or was unreachable. Maps to 502 and is retryable by
// default.
func BadGatewayf(format string, args ...any) *Error {
	return newErr(KindBadGateway, format, args, 1)
}

// Internalf reports an unexpected internal failure. Maps to 500. Retryable by
// default; chain .NotRetryable() for invariant violations and programming
// errors that won't be fixed by retrying. Prefer a more specific kind when
// one applies.
func Internalf(format string, args ...any) *Error {
	return newErr(KindInternal, format, args, 1)
}

// Validation reports semantically invalid input. Pass field-level details via
// the variadic Field/FieldVal arguments. Maps to HTTP 422 and aligns with
// huma's binding-validation shape.
//
// Examples:
//
//	errors.Validation("name and type are required",
//	    errors.Field("body.name", "required"),
//	    errors.Field("body.type", "must be 'movie' or 'series'"))
//
//	errors.Validation("invalid combination of options")  // no field details
func Validation(detail string, fields ...FieldError) *Error {
	return &Error{
		kind:   KindValidation,
		msg:    detail,
		fields: fields,
		stack:  captureStack(1),
	}
}

// Wrap annotates an existing error with additional context, preserving the
// kind from the wrapped chain. Returns nil if err is nil. Returns error
// (not *Error) so nil-in / nil-out works correctly through the interface.
func Wrap(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return &Error{
		kind:    KindOf(err),
		msg:     fmt.Sprintf(format, args...),
		wrapped: err,
		stack:   captureStack(1),
	}
}

// WithOp records the operation name (e.g., "LibrariesService.Get") for logs.
// Returns nil when err is nil. Returns a copy of any *Error in the chain with
// op set, or wraps a non-typed error in a fresh *Error with op set. Op flows
// to slog and is never exposed on the wire.
//
// Inside service code with a freshly-constructed typed error, prefer the Op
// builder method on *Error: errors.Internalf("...").Op("...").
func WithOp(err error, op string) error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		cp := *e
		cp.op = op
		return &cp
	}
	return &Error{
		kind:    KindOf(err),
		op:      op,
		wrapped: err,
		stack:   captureStack(1),
	}
}

// KindOf returns the kind for an error by walking the wrap chain. The first
// *Error encountered with a non-Unspecified kind wins. Errors with no typed
// layer return KindUnspecified, which HTTPStatus treats as 500.
func KindOf(err error) Kind {
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		if e, ok := cur.(*Error); ok && e.kind != KindUnspecified {
			return e.kind
		}
	}
	return KindUnspecified
}

// IsKind reports whether err's kind (anywhere in the wrap chain) matches k.
func IsKind(err error, k Kind) bool { return KindOf(err) == k }

// IsNotFound is shorthand for IsKind(err, KindNotFound).
func IsNotFound(err error) bool { return IsKind(err, KindNotFound) }

// IsConflict is shorthand for IsKind(err, KindConflict).
func IsConflict(err error) bool { return IsKind(err, KindConflict) }

// IsValidation is shorthand for IsKind(err, KindValidation).
func IsValidation(err error) bool { return IsKind(err, KindValidation) }

// IsForbidden is shorthand for IsKind(err, KindForbidden).
func IsForbidden(err error) bool { return IsKind(err, KindForbidden) }

// IsUnauthenticated is shorthand for IsKind(err, KindUnauthenticated).
func IsUnauthenticated(err error) bool { return IsKind(err, KindUnauthenticated) }

// IsBadGateway is shorthand for IsKind(err, KindBadGateway).
func IsBadGateway(err error) bool { return IsKind(err, KindBadGateway) }

// IsInternal is shorthand for IsKind(err, KindInternal).
func IsInternal(err error) bool { return IsKind(err, KindInternal) }

// IsRetryable reports whether worker code should retry this error. The chain
// is walked: the first explicit NotRetryable override wins. Otherwise the
// first typed kind's default applies. Errors with no typed layer default to
// true (preserves naked-error behavior — workers retry unknown errors).
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var topKind = KindUnspecified
	sawTyped := false
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		e, ok := cur.(*Error)
		if !ok {
			continue
		}
		if e.retryOverride != nil {
			return *e.retryOverride
		}
		if !sawTyped && e.kind != KindUnspecified {
			topKind = e.kind
			sawTyped = true
		}
	}
	if sawTyped {
		return retryableByKind(topKind)
	}
	return true
}

// OpOf returns the recorded operation for an error, or "" if none is set.
func OpOf(err error) string {
	if err == nil {
		return ""
	}
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		if e, ok := cur.(*Error); ok && e.op != "" {
			return e.op
		}
	}
	return ""
}

// FieldsOf returns the validation field details for an error, or nil if none.
func FieldsOf(err error) []FieldError {
	if err == nil {
		return nil
	}
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		if e, ok := cur.(*Error); ok && len(e.fields) > 0 {
			return e.fields
		}
	}
	return nil
}
