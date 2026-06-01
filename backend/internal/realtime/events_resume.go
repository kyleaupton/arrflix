package realtime

// NameResumeGap is the wire name of the gap signal.
const NameResumeGap = "resume_gap"

// ResumeGapPayload is the kick sent when a reconnecting client's resume point
// has aged out of the replay ring. It carries no state — the client reacts by
// refetching the snapshots for its subscribed topics rather than replaying.
type ResumeGapPayload struct {
	Reason string `json:"reason"`
}

// ResumeGap builds the gap event. Broadcast because it's a transport-level
// signal to whichever session reconnected, not a user-scoped payload.
func ResumeGap() Event {
	return Event{Name: NameResumeGap, Recipient: Broadcast, Data: mustMarshal(ResumeGapPayload{Reason: "buffer_exhausted"})}
}
