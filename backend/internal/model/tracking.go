package model

import (
	"time"

	"github.com/google/uuid"
)

// TrackingState is the lifecycle of a tracking. The DB stores it as TEXT;
// these typed consts give services and tests safe names.
type TrackingState string

const (
	TrackingActive   TrackingState = "active"
	TrackingPaused   TrackingState = "paused"
	TrackingArchived TrackingState = "archived"
	TrackingCanceled TrackingState = "canceled"
)

// Autonomy is the acquisition-autonomy dial — who picks the release for a want
// segment. Held per segment (backfill/ongoing) on a tracking. AutonomyPropose is
// a later phase: the DB CHECK admits it so the schema is stable, but the API and
// UI reject it for now.
type Autonomy string

const (
	AutonomyAuto    Autonomy = "auto"
	AutonomyPropose Autonomy = "propose"
	AutonomyManual  Autonomy = "manual"
)

// Tracking is the domain shape for a tracking row — the ongoing-intent
// primitive. For movies it is the single-atom case: Scope is inert and there
// is one want per tracking. Scope/UpgradeBehavior/ScheduleStrategy are
// placeholders where series tracking and the scheduler plug in. Mirrors
// dbgen.Tracking.
type Tracking struct {
	ID               uuid.UUID `json:"id"`
	MediaItemID      uuid.UUID `json:"mediaItemId"`
	QualityProfileID uuid.UUID `json:"qualityProfileId"`
	State            string    `json:"state"`
	Scope            string    `json:"scope"`
	UpgradeBehavior  string    `json:"upgradeBehavior"`
	ScheduleStrategy string    `json:"scheduleStrategy"`
	AutonomyBackfill string    `json:"autonomyBackfill"`
	AutonomyOngoing  string    `json:"autonomyOngoing"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// AutonomyFor returns the autonomy dial governing a want in the given segment.
func (t Tracking) AutonomyFor(seg WantSegment) Autonomy {
	if seg == WantSegmentBackfill {
		return Autonomy(t.AutonomyBackfill)
	}
	return Autonomy(t.AutonomyOngoing)
}
