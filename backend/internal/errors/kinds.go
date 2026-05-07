package errors

import "net/http"

// Kind is the semantic category of an error. It drives HTTP status mapping and
// (by default) retry decisions. Pick the closest match; resist adding kinds
// without a real callsite.
type Kind string

const (
	// KindUnspecified is the zero value. Treated as Internal for HTTP mapping
	// and as retryable for workers (preserves naked-error behavior).
	KindUnspecified Kind = ""

	KindNotFound        Kind = "not_found"
	KindConflict        Kind = "conflict"
	KindValidation      Kind = "validation"
	KindForbidden       Kind = "forbidden"
	KindUnauthenticated Kind = "unauthenticated"
	KindBadGateway      Kind = "bad_gateway"
	KindInternal        Kind = "internal"
)

// HTTPStatus returns the HTTP status code for an error's kind. Unrecognized
// or unspecified kinds map to 500.
func HTTPStatus(err error) int {
	switch KindOf(err) {
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindValidation:
		return http.StatusUnprocessableEntity
	case KindForbidden:
		return http.StatusForbidden
	case KindUnauthenticated:
		return http.StatusUnauthorized
	case KindBadGateway:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// title is the human-readable RFC 9457 title for a kind.
func (k Kind) title() string {
	switch k {
	case KindNotFound:
		return "Not Found"
	case KindConflict:
		return "Conflict"
	case KindValidation:
		return "Unprocessable Entity"
	case KindForbidden:
		return "Forbidden"
	case KindUnauthenticated:
		return "Unauthorized"
	case KindBadGateway:
		return "Bad Gateway"
	default:
		return "Internal Server Error"
	}
}

// typeURI is the stable RFC 9457 type identifier for a kind. Frontends branch
// on this string; it must remain stable across versions. Path is relative;
// hosting docs at this path is optional.
func (k Kind) typeURI() string {
	switch k {
	case KindNotFound:
		return "/errors/not-found"
	case KindConflict:
		return "/errors/conflict"
	case KindValidation:
		return "/errors/validation"
	case KindForbidden:
		return "/errors/forbidden"
	case KindUnauthenticated:
		return "/errors/unauthenticated"
	case KindBadGateway:
		return "/errors/bad-gateway"
	default:
		return "/errors/internal"
	}
}

// retryableByKind returns the default retry decision for a kind when no
// explicit override has been set on the error.
func retryableByKind(k Kind) bool {
	switch k {
	case KindBadGateway, KindInternal, KindUnspecified:
		return true
	default:
		return false
	}
}
