package mcp

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// HookEventType identifies the lifecycle event emitted by a plugin hook.
type HookEventType string

const (
	HookEventPreToolUse  HookEventType = "PreToolUse"
	HookEventPostToolUse HookEventType = "PostToolUse"
	HookEventStart       HookEventType = "Start"
	HookEventStop        HookEventType = "Stop"
	HookEventNotify      HookEventType = "Notify"
	// Codex-specific
	HookEventCodexStart HookEventType = "start"
	HookEventCodexStop  HookEventType = "stop"
	HookEventCodexAsk   HookEventType = "ask"
	HookEventCodexError HookEventType = "error"
)

// HookEvent is the JSON payload posted by a plugin hook script.
type HookEvent struct {
	AgentID   string        `json:"agentId"`
	SessionID string        `json:"sessionId,omitempty"`
	EventType HookEventType `json:"eventType"`
	// Claude-specific fields
	HookEventName string `json:"hook_event_name,omitempty"`
	// Metadata / context
	CWD            string `json:"cwd,omitempty"`
	Model          string `json:"model,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Message        string `json:"message,omitempty"`
}

// AgentStatusUpdater is implemented by whatever owns the agent status (the UI/Manager).
type AgentStatusUpdater interface {
	SetRunning(agentID uuid.UUID)
	SetIdle(agentID uuid.UUID)
	SetBlocked(agentID uuid.UUID)
	SetError(agentID uuid.UUID)
	SetMetadata(agentID uuid.UUID, key, value string)
	SetSessionID(agentID uuid.UUID, sessionID string)
	SetStatusText(agentID uuid.UUID, status, category string)
}

// hookHandler processes POST /hook requests from claude/codex plugin scripts.
type hookHandler struct {
	updater AgentStatusUpdater
}

func newHookHandler(updater AgentStatusUpdater) *hookHandler {
	return &hookHandler{updater: updater}
}

func (h *hookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var event HookEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	agentID, err := uuid.Parse(event.AgentID)
	if err != nil {
		// Unknown agent — silently ignore.
		w.WriteHeader(http.StatusOK)
		return
	}

	if h.updater != nil {
		h.dispatch(agentID, event)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *hookHandler) dispatch(agentID uuid.UUID, event HookEvent) {
	// Build metadata from HookEvent fields.
	metadata := make(map[string]string)
	if event.CWD != "" {
		metadata["cwd"] = event.CWD
	}
	if event.Model != "" {
		metadata["model"] = event.Model
	}
	if event.TranscriptPath != "" {
		metadata["transcript_path"] = event.TranscriptPath
	}

	// Store session ID.
	if event.SessionID != "" {
		h.updater.SetSessionID(agentID, event.SessionID)
	}

	// Normalise event type (Claude uses hook_event_name field).
	eventType := event.EventType
	if event.HookEventName != "" {
		eventType = HookEventType(event.HookEventName)
	}

	// Map event type to status string.
	var status string
	switch eventType {
	case HookEventPreToolUse, HookEventStart, HookEventCodexStart:
		status = "running"
	case HookEventPostToolUse, HookEventStop, HookEventCodexStop:
		status = "idle"
	case HookEventNotify, HookEventCodexAsk:
		status = "input"
	case HookEventCodexError:
		status = "error"
	}

	h.dispatchStatus(agentID, status, metadata)
}

// dispatchStatus is the shared dispatch function used by both /hook and /api/v1/agent/status.
// It applies metadata and maps a status string to the appropriate AgentStatusUpdater calls.
func (h *hookHandler) dispatchStatus(agentID uuid.UUID, status string, metadata map[string]string) {
	// Apply metadata.
	for k, v := range metadata {
		h.updater.SetMetadata(agentID, k, v)
	}

	// Apply status.
	switch status {
	case "running":
		h.updater.SetRunning(agentID)
	case "idle":
		h.updater.SetIdle(agentID)
	case "input":
		h.updater.SetBlocked(agentID)
	case "error":
		h.updater.SetError(agentID)
	}
}
