package realtime

import "time"

// System event names.
const (
	NameReady = "ready"
	NamePing  = "ping"
)

// ReadyPayload is sent once on connect. Wire: {"ok":true,"sessionId":"<uuid>"}.
// SessionId is the broker session allocated for this stream; later phases use
// it for reconnect (?session=) and the subscription control plane.
type ReadyPayload struct {
	OK        bool   `json:"ok"`
	SessionID string `json:"sessionId"`
}

// PingPayload is the heartbeat. Wire: {"ts":<unix-seconds>}.
type PingPayload struct {
	TS int64 `json:"ts"`
}

// Ready builds the connect-time ready event carrying the session id.
func Ready(sessionID string) Event {
	return Event{Name: NameReady, Recipient: Broadcast, Data: mustMarshal(ReadyPayload{OK: true, SessionID: sessionID})}
}

// Ping builds a heartbeat stamped with the current unix time.
func Ping() Event {
	return Event{Name: NamePing, Recipient: Broadcast, Data: mustMarshal(PingPayload{TS: time.Now().Unix()})}
}
