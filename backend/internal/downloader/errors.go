package downloader

import "errors"

// IsUnreachable reports whether err means the downloader itself could not be
// reached or could not act — it is down, restarting, or refusing Arrflix's
// credentials — as opposed to having judged the request (a 4xx rejection of a
// torrent). A job that hits an unreachable downloader is retried indefinitely
// and never blocklists its release; the release was never looked at.
//
// Only errors marked by the downloader layer count: an error in the chain whose
// Unreachable method reports true. Client implementations mark failures of their
// own API (transport errors via MarkUnreachable, 5xx, auth). A network error from
// anything else — fetching a release's torrent file from an indexer, say — is
// deliberately not an outage: it says nothing about the downloader.
func IsUnreachable(err error) bool {
	var marked interface{ Unreachable() bool }
	return errors.As(err, &marked) && marked.Unreachable()
}

// MarkUnreachable wraps err so IsUnreachable reports true for it. Clients use it
// on transport failures talking to the downloader's own API.
func MarkUnreachable(err error) error {
	if err == nil {
		return nil
	}
	return &unreachableError{err}
}

type unreachableError struct{ err error }

func (e *unreachableError) Error() string     { return e.err.Error() }
func (e *unreachableError) Unwrap() error     { return e.err }
func (e *unreachableError) Unreachable() bool { return true }
