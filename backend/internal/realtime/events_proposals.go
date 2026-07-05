package realtime

import "github.com/google/uuid"

// Proposal event names. Snake_case on the wire.
const (
	NameProposalUpdated = "proposal_updated"
)

// ProposalUpdatedPayload identifies which tracking's proposals changed. The
// tracking id travels in the payload so the series page can refetch that
// tracking's proposal list — the event is kick-only (no proposal body rides
// along), mirroring ImportTaskUpdated.
type ProposalUpdatedPayload struct {
	TrackingID uuid.UUID `json:"trackingId"`
}

// ProposalUpdated builds the proposal_updated event for a tracking. It targets
// Admins — proposals are an operator-scoped surface, and Admins is the delivery
// primitive for that scoping.
func ProposalUpdated(trackingID uuid.UUID) Event {
	return Event{
		Name:      NameProposalUpdated,
		Recipient: Admins,
		Data:      mustMarshal(ProposalUpdatedPayload{TrackingID: trackingID}),
	}
}
