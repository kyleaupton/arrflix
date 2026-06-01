package realtime

// Scan event names. Snake_case on the wire; frontend consumers
// (LibrarySettings.vue) listen for these literals.
const (
	NameScanStarted   = "scan_started"
	NameScanProgress  = "scan_progress"
	NameScanCompleted = "scan_completed"
	NameScanFailed    = "scan_failed"
)

// ScanStartedPayload is emitted when a library scan kicks off.
type ScanStartedPayload struct {
	ScanID    string `json:"scanId"`
	LibraryID string `json:"libraryId"`
}

// ScanProgressPayload is emitted periodically while a scan runs and once more
// with final totals before completion.
type ScanProgressPayload struct {
	ScanID            string `json:"scanId"`
	LibraryID         string `json:"libraryId"`
	FilesSeen         int    `json:"filesSeen"`
	MediaItemsCreated int    `json:"mediaItemsCreated"`
}

// ScanCompletedPayload carries the final per-band totals plus duration.
type ScanCompletedPayload struct {
	ScanID              string `json:"scanId"`
	LibraryID           string `json:"libraryId"`
	FilesSeen           int    `json:"filesSeen"`
	Confident           int    `json:"confident"`
	ConfidentReview     int    `json:"confidentReview"`
	LowConfidence       int    `json:"lowConfidence"`
	Ambiguous           int    `json:"ambiguous"`
	NoMatch             int    `json:"noMatch"`
	PartialSeries       int    `json:"partialSeries"`
	MediaItemsCreated   int    `json:"mediaItemsCreated"`
	EpisodeLookupFailed int    `json:"episodeLookupFailed"`
	Duration            int    `json:"duration"`
}

// ScanFailedPayload is emitted when a scan errors out.
type ScanFailedPayload struct {
	ScanID    string `json:"scanId"`
	LibraryID string `json:"libraryId"`
	Error     string `json:"error"`
}

// ScanStarted builds the scan_started event.
func ScanStarted(p ScanStartedPayload) Event {
	return Event{Name: NameScanStarted, Recipient: Broadcast, Data: mustMarshal(p)}
}

// ScanProgress builds the scan_progress event.
func ScanProgress(p ScanProgressPayload) Event {
	return Event{Name: NameScanProgress, Recipient: Broadcast, Data: mustMarshal(p)}
}

// ScanCompleted builds the scan_completed event.
func ScanCompleted(p ScanCompletedPayload) Event {
	return Event{Name: NameScanCompleted, Recipient: Broadcast, Data: mustMarshal(p)}
}

// ScanFailed builds the scan_failed event.
func ScanFailed(p ScanFailedPayload) Event {
	return Event{Name: NameScanFailed, Recipient: Broadcast, Data: mustMarshal(p)}
}
