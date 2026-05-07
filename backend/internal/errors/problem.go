package errors

// ProblemDetails is the RFC 9457 application/problem+json wire shape arrflix
// returns for all error responses. It mirrors huma's default ErrorModel so
// service-emitted errors and huma-binding errors are indistinguishable to
// clients.
//
// The Type field is a stable identifier — frontends should branch on it and
// it must remain backward-compatible across versions.
type ProblemDetails struct {
	Type   string       `json:"type"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Errors []FieldError `json:"errors,omitempty"`
}

// ContentType is the media type for ProblemDetails responses per RFC 9457.
const ContentType = "application/problem+json"

// ToProblem maps any error to a ProblemDetails. Errors without a kind become
// generic 500s with an opaque title; the original message is intentionally
// dropped to avoid leaking internal detail. Use slog with the original error
// for debugging.
func ToProblem(err error) ProblemDetails {
	if err == nil {
		return ProblemDetails{}
	}
	kind := KindOf(err)
	pd := ProblemDetails{
		Type:   kind.typeURI(),
		Title:  kind.title(),
		Status: HTTPStatus(err),
		Errors: FieldsOf(err),
	}
	if kind == KindUnspecified || kind == KindInternal {
		// Don't leak internal detail. Logs have the full error.
		pd.Detail = "an internal error occurred"
	} else {
		pd.Detail = err.Error()
	}
	return pd
}
