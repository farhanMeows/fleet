// Package event defines fleet's versioned event schema — the contract
// between hook ingestion, local storage, and (later) cloud sync.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const SchemaVersion = 1

// Hook event names emitted by Claude Code that fleet consumes.
const (
	SessionStart      = "SessionStart"
	PreToolUse        = "PreToolUse"
	PostToolUse       = "PostToolUse"
	PermissionRequest = "PermissionRequest"
	Stop              = "Stop"
	SessionEnd        = "SessionEnd"
)

// Session states derived from events.
const (
	StateIdle       = "idle"
	StateWorking    = "working"
	StateNeedsInput = "needs_input"
	StateEnded      = "ended"
)

// HookPayload is the subset of Claude Code's hook stdin JSON that fleet reads.
type HookPayload struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	PermissionMode string          `json:"permission_mode"`
}

// Event is fleet's normalized, versioned event record.
type Event struct {
	SchemaVersion  int    `json:"schema_version"`
	Event          string `json:"event"`
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
	Summary        string `json:"summary,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	// InputHash fingerprints the exact tool_input of a PermissionRequest so
	// a remote approval can verify the pending prompt hasn't changed.
	InputHash  string `json:"input_hash,omitempty"`
	ReceivedAt int64  `json:"received_at"`
}

func FromPayload(eventName string, p *HookPayload, now int64) *Event {
	ev := &Event{
		SchemaVersion:  SchemaVersion,
		Event:          eventName,
		SessionID:      p.SessionID,
		Cwd:            p.Cwd,
		TranscriptPath: p.TranscriptPath,
		ToolName:       p.ToolName,
		Summary:        Summarize(p.ToolName, p.ToolInput),
		PermissionMode: p.PermissionMode,
		ReceivedAt:     now,
	}
	if eventName == PermissionRequest && len(p.ToolInput) > 0 {
		sum := sha256.Sum256(p.ToolInput)
		ev.InputHash = hex.EncodeToString(sum[:6])
	}
	return ev
}

// StateFor maps a hook event to the session state it implies.
// Empty string means the event does not change state.
func StateFor(eventName string) string {
	switch eventName {
	case SessionStart, Stop:
		return StateIdle
	case PreToolUse, PostToolUse:
		return StateWorking
	case PermissionRequest:
		return StateNeedsInput
	case SessionEnd:
		return StateEnded
	}
	return ""
}

// Summarize extracts a short human-readable description from tool_input.
func Summarize(toolName string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var in map[string]any
	if err := json.Unmarshal(raw, &in); err != nil {
		return ""
	}
	for _, key := range []string{"command", "description", "file_path", "prompt", "pattern", "url", "query"} {
		if v, ok := in[key].(string); ok && v != "" {
			return truncate(v, 80)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
