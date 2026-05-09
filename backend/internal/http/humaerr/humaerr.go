// Package humaerr wires apperrors.ToProblem into huma's error customization
// hook. Importing this package (for its side effect) is enough to install
// the custom NewError; both internal/http (the live server) and
// cmd/genspec/main.go (the spec generator) depend on it being in place
// before any huma.Register call.
//
// Behavior:
//   - If any error in the chain is a *apperrors.Error (errors.As match),
//     render via apperrors.ToProblem (preserves Kind → status, type URI,
//     field details, and the no-leak rule for 500s).
//   - Otherwise, synthesize a *apperrors.Error from the status huma
//     supplies (mapped to a Kind), the supplied message (used as the
//     detail), and any *huma.ErrorDetail entries in the error chain
//     (turned into FieldErrors). The result is rendered through the
//     same apperrors.ToProblem path so framework-emitted binding /
//     schema-validation failures share the exact RFC 9457 wire shape
//     used by service-emitted errors.
package humaerr

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
)

// problemDetailsType is captured once so Schema() doesn't reflect on every
// call.
var problemDetailsType = reflect.TypeOf(apperrors.ProblemDetails{})

func init() {
	huma.NewError = func(status int, message string, errs ...error) huma.StatusError {
		// Look for a typed apperrors.Error anywhere in the supplied chain.
		// errs are the framework-provided detail errors (binding, schema
		// validation, or handler-returned errors).
		for _, e := range errs {
			var ae *apperrors.Error
			if errors.As(e, &ae) {
				return &apperrorsStatusError{err: ae}
			}
		}
		// Fallback: synthesize a *apperrors.Error so the wire shape matches
		// service-emitted errors. Pull *huma.ErrorDetail entries (huma's
		// binding / schema-validation details) into FieldErrors; everything
		// else becomes a message-only field.
		fields := make([]apperrors.FieldError, 0, len(errs))
		for _, e := range errs {
			if e == nil {
				continue
			}
			var det *huma.ErrorDetail
			if errors.As(e, &det) {
				fields = append(fields, apperrors.FieldError{
					Location: det.Location,
					Message:  det.Message,
					Value:    det.Value,
				})
				continue
			}
			fields = append(fields, apperrors.FieldError{Message: e.Error()})
		}
		ae := apperrors.Problem(kindForStatus(status), message, fields)
		return &apperrorsStatusError{err: ae}
	}
}

// kindForStatus maps the HTTP status huma supplies to a Kind. Statuses huma
// commonly emits during binding / schema validation (400, 422) become
// KindValidation; anything unrecognized becomes KindInternal. KindInternal's
// ToProblem path masks the detail with "an internal error occurred" — that's
// intentional, since huma's 5xx message is generic and not worth surfacing.
func kindForStatus(status int) apperrors.Kind {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return apperrors.KindValidation
	case http.StatusUnauthorized:
		return apperrors.KindUnauthenticated
	case http.StatusForbidden:
		return apperrors.KindForbidden
	case http.StatusNotFound:
		return apperrors.KindNotFound
	case http.StatusConflict:
		return apperrors.KindConflict
	case http.StatusBadGateway:
		return apperrors.KindBadGateway
	default:
		return apperrors.KindInternal
	}
}

// apperrorsStatusError adapts *apperrors.Error to huma.StatusError. The
// JSON body is produced by apperrors.ToProblem so kind, type URI, status,
// detail (with the 500-hiding rule), and field errors all appear with
// arrflix's RFC 9457 problem-details wire shape.
type apperrorsStatusError struct {
	err *apperrors.Error
}

func (s *apperrorsStatusError) Error() string  { return s.err.Error() }
func (s *apperrorsStatusError) GetStatus() int { return apperrors.HTTPStatus(s.err) }

// MarshalJSON is what huma calls to encode the response body.
func (s *apperrorsStatusError) MarshalJSON() ([]byte, error) {
	return json.Marshal(apperrors.ToProblem(s.err))
}

// Schema implements huma.SchemaProvider so the OpenAPI spec advertises the
// real wire shape (apperrors.ProblemDetails) instead of an empty object. The
// wrapper has no exported fields — its body is produced by MarshalJSON — so
// without this, huma's reflective generation produces an empty schema for
// every error response across every operation.
//
// Returning a registry ref keeps the spec DRY: ProblemDetails (and its nested
// FieldError element) get registered once under components.schemas and every
// operation's error response is a $ref to it.
func (s *apperrorsStatusError) Schema(r huma.Registry) *huma.Schema {
	return r.Schema(problemDetailsType, true, "ProblemDetails")
}
