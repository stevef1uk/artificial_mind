package eventbus

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// CanonicalEvent represents the uniform event envelope used across the system.
type CanonicalEvent struct {
	EventID   string        `json:"event_id"`
	Source    string        `json:"source"`
	Type      string        `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	Context   EventContext  `json:"context"`
	Payload   EventPayload  `json:"payload"`
	Security  EventSecurity `json:"security"`
}

type EventContext struct {
	Channel   string `json:"channel,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

type EventPayload struct {
	Text        string                 `json:"text,omitempty"`
	Attachments []string               `json:"attachments,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type EventSecurity struct {
	Sensitivity string `json:"sensitivity,omitempty"` // low|medium|high
	OriginAuth  string `json:"origin_auth,omitempty"`
}

// NewEventID generates a compact unique event id with a date prefix.
func NewEventID(prefix string, t time.Time) string {
	// 8 random bytes -> 16 hex chars
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + t.UTC().Format("20060102") + "_" + hex.EncodeToString(b)
}

// MinimalValidate checks required fields.
func (e *CanonicalEvent) MinimalValidate() bool {
	return e.EventID != "" && e.Source != "" && e.Type != "" && !e.Timestamp.IsZero()
}
