package realtime

import "time"

// System event names.
const (
	NameReady = "ready"
	NamePing  = "ping"
)

// ReadyPayload is sent once on connect. Wire: {"ok":true}. A session_id field
// is added when per-user scoping lands.
type ReadyPayload struct {
	OK bool `json:"ok"`
}

// PingPayload is the heartbeat. Wire: {"ts":<unix-seconds>}.
type PingPayload struct {
	TS int64 `json:"ts"`
}

// Ready builds the connect-time ready event.
func Ready() Event {
	return Event{Name: NameReady, Recipient: Broadcast, Data: mustMarshal(ReadyPayload{OK: true})}
}

// Ping builds a heartbeat stamped with the current unix time.
func Ping() Event {
	return Event{Name: NamePing, Recipient: Broadcast, Data: mustMarshal(PingPayload{TS: time.Now().Unix()})}
}
