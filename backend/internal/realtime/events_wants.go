package realtime

import "github.com/kyleaupton/arrflix/internal/model"

// Want event names. Snake_case on the wire; the frontend realtime bindings
// listen for these literals.
const (
	NameWantUpdated = "want_updated"
)

// WantUpdated builds a per-change delta for one want as it moves through the
// acquisition lifecycle. The payload is the full want, so the frontend can
// render status/attempt_count/last_error without a refetch.
func WantUpdated(want model.Want) Event {
	return Event{Name: NameWantUpdated, Recipient: Broadcast, Data: mustMarshal(want)}
}
