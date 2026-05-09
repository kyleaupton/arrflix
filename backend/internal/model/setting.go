package model

import (
	"encoding/json"
	"time"
)

// Setting is the domain shape for an app_setting row. It mirrors the
// persistence-layer dbgen.AppSetting but uses idiomatic Go types
// (time.Time, json.RawMessage in place of []byte).
type Setting struct {
	Key       string          `json:"key"`
	Type      string          `json:"type"`
	ValueJSON json.RawMessage `json:"valueJson"`
	Version   int32           `json:"version"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
