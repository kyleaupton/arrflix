package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// RoutingRule is the domain shape for one dispatch rule row: a condition tree
// over the Subject plus the action set applied on match. Rules form one
// global ordered list (Position); evaluation order, first-match/continue
// semantics, and the grab-time reduction live in internal/routing.
//
// Conditions is the rules substrate's canonical JSON tree carried as raw
// bytes — the JSONB column, the wire body, and the in-memory tree are one
// encoding. The bytes stay opaque here because model cannot name
// *rules.Condition (rules imports model for the Subject); the routing
// service decodes/encodes at its boundary.
//
// Action targets are nullable: a slot can be left unset by the author or
// nulled by the database when its target is deleted (ON DELETE SET NULL).
// Either way the rule still matches, contributes nothing for that slot, and
// the slot falls back to the configured default at dispatch.
type RoutingRule struct {
	ID             uuid.UUID       `json:"id"`
	Name           string          `json:"name"`
	Enabled        bool            `json:"enabled"`
	Position       int32           `json:"position"`
	Conditions     json.RawMessage `json:"conditions"`
	DownloaderID   *uuid.UUID      `json:"downloaderId,omitempty"`
	LibraryID      *uuid.UUID      `json:"libraryId,omitempty"`
	NameTemplateID *uuid.UUID      `json:"nameTemplateId,omitempty"`
	Continue       bool            `json:"continue"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}
