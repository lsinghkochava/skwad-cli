package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EventType identifies the kind of run lifecycle event.
type EventType string

const (
	EventRunStarted       EventType = "run_started"
	EventAgentSpawned     EventType = "agent_spawned"
	EventAgentRegistered  EventType = "agent_registered"
	EventPromptSent       EventType = "prompt_sent"
	EventResponseReceived EventType = "response_received"
	EventAgentExited      EventType = "agent_exited"
	EventPhaseTransition  EventType = "phase_transition"
	EventIteration        EventType = "iteration"
	EventRunCompleted     EventType = "run_completed"
	EventRunFailed        EventType = "run_failed"
)

// Event is a single run lifecycle event.
type Event struct {
	Time      time.Time       `json:"time"`
	Type      EventType       `json:"type"`
	RunID     string          `json:"run_id"`
	AgentID   string          `json:"agent_id,omitempty"`
	AgentName string          `json:"agent_name,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// criticalEvents are fsynced immediately for durability.
var criticalEvents = map[EventType]bool{
	EventRunStarted:   true,
	EventRunCompleted: true,
	EventRunFailed:    true,
	EventAgentExited:  true,
}

// EventLog is an append-only event log for run state persistence.
type EventLog struct {
	mu    sync.Mutex
	file  *os.File
	runID string
	enc   *json.Encoder
	count int
}

// NewEventLog creates or opens an event log at ~/.config/skwad/runs/<runID>/events.jsonl.
func NewEventLog(runID string) (*EventLog, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, configDir, "runs", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &EventLog{
		file:  f,
		runID: runID,
		enc:   json.NewEncoder(f),
	}, nil
}

// Append writes an event to the log.
func (l *EventLog) Append(event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	event.RunID = l.runID
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	if err := l.enc.Encode(event); err != nil {
		return err
	}
	l.count++
	if criticalEvents[event.Type] || l.count%10 == 0 {
		return l.file.Sync()
	}
	return nil
}

// Close flushes and closes the log file.
func (l *EventLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Sync()
		return l.file.Close()
	}
	return nil
}
