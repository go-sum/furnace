package model

import "time"

type AuditEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	AppName   string            `json:"app"`
	Action    string            `json:"action"`
	Status    string            `json:"status"`
	Actor     string            `json:"actor"`
	Image     string            `json:"image,omitempty"`
	Error     string            `json:"error,omitempty"`
	DurationMs int64            `json:"duration_ms,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
