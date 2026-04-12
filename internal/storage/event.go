package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Event represents a raw memory event in the append-only log.
type Event struct {
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Actor         string `json:"actor"`
	Content       string `json:"content"`
	Source        string `json:"source"`
	SessionID     string `json:"session_id,omitempty"`
	ActiveProject string `json:"active_project,omitempty"`
	ActiveTask    string `json:"active_task,omitempty"`
	Durability    string `json:"durability"`
	Actionability string `json:"actionability"`
	SourceType    string `json:"source_type"`
	EvidenceKind  string `json:"evidence_kind"`
	TrustTier     int    `json:"trust_tier"`
}

// GenerateEventID creates a unique event ID like "evt_20260412_143022_a7f3".
func GenerateEventID() string {
	now := time.Now().UTC()
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("evt_%s_%s", now.Format("20060102_150405"), hex.EncodeToString(b))
}
