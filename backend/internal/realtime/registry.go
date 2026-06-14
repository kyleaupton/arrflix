package realtime

import "github.com/kyleaupton/arrflix/internal/model"

// EventSchema describes one event type for OpenAPI spec generation: its wire
// name and a zero-value sample of its payload Go type. The transport layer
// enumerates Registry to build the SSE `oneOf` array (one schema per event,
// `data` derived from the sample's type).
//
// This is the single event registry. Runtime emit does NOT consult it — the
// name is a field on each Event — so the two consumers (emit, spec-gen) can't
// drift: adding an event means adding a constructor and a Registry entry, and
// the compiler enforces the payload type on both.
type EventSchema struct {
	Name          string
	PayloadSample any
}

// Registry is the full set of realtime event types, in wire order grouped by
// domain.
var Registry = []EventSchema{
	{Name: NameReady, PayloadSample: ReadyPayload{}},
	{Name: NamePing, PayloadSample: PingPayload{}},
	{Name: NameResumeGap, PayloadSample: ResumeGapPayload{}},
	{Name: NameDownloadJobUpdated, PayloadSample: model.DownloadJobWithSummary{}},
	{Name: NameWantUpdated, PayloadSample: model.Want{}},
	{Name: NameImportTaskUpdated, PayloadSample: ImportTaskUpdatedPayload{}},
	{Name: NameScanStarted, PayloadSample: ScanStartedPayload{}},
	{Name: NameScanProgress, PayloadSample: ScanProgressPayload{}},
	{Name: NameScanCompleted, PayloadSample: ScanCompletedPayload{}},
	{Name: NameScanFailed, PayloadSample: ScanFailedPayload{}},
}
