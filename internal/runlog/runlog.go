// Package runlog provides structured JSONL logging for agent activity.
// Each run produces a timestamped .jsonl file containing one JSON object per line,
// capturing tool calls, messages, status changes, spawns, exits, and hooks.
package runlog

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// logEntry is the JSON envelope written for every event.
type logEntry struct {
	Timestamp string      `json:"timestamp"`
	Event     string      `json:"event"`
	AgentID   string      `json:"agent_id,omitempty"`
	AgentName string      `json:"agent_name,omitempty"`
	Data      interface{} `json:"data"`
}

// RunLogger writes structured JSONL events to a file.
type RunLogger struct {
	file *os.File
	enc  *json.Encoder
	mu   sync.Mutex
}

// New creates a RunLogger that writes to a timestamped .jsonl file in dir.
// TODO: implement log rotation — currently files grow unboundedly
func New(dir string) (*RunLogger, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	filename := time.Now().Format("2006-01-02T15-04-05") + ".jsonl"
	path := filepath.Join(dir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	abs, _ := filepath.Abs(path)
	slog.Info("runlog started", "path", abs)

	return &RunLogger{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Close flushes and closes the log file.
func (rl *RunLogger) Close() error {
	if rl == nil {
		return nil
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.file.Close()
}

// write encodes a single JSON line to the log file.
func (rl *RunLogger) write(entry logEntry) {
	if rl == nil {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	_ = rl.enc.Encode(entry)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// LogToolCall records an MCP tool invocation.
func (rl *RunLogger) LogToolCall(agentID, agentName, toolName string, args map[string]interface{}, result interface{}) {
	if rl == nil {
		return
	}
	rl.write(logEntry{
		Timestamp: now(),
		Event:     "tool_call",
		AgentID:   agentID,
		AgentName: agentName,
		Data: map[string]interface{}{
			"tool":   toolName,
			"args":   args,
			"result": result,
		},
	})
}

// LogMessage records a message sent between agents.
func (rl *RunLogger) LogMessage(fromID, fromName, toID, content string) {
	if rl == nil {
		return
	}
	rl.write(logEntry{
		Timestamp: now(),
		Event:     "message",
		AgentID:   fromID,
		AgentName: fromName,
		Data: map[string]interface{}{
			"to_id":   toID,
			"content": content,
		},
	})
}

// LogBroadcast records a broadcast message sent to all agents.
func (rl *RunLogger) LogBroadcast(fromID, fromName, content string) {
	if rl == nil {
		return
	}
	rl.write(logEntry{
		Timestamp: now(),
		Event:     "broadcast",
		AgentID:   fromID,
		AgentName: fromName,
		Data: map[string]interface{}{
			"content": content,
		},
	})
}

// LogStatus records an agent status change.
func (rl *RunLogger) LogStatus(agentID, agentName, status, statusText, category string) {
	if rl == nil {
		return
	}
	rl.write(logEntry{
		Timestamp: now(),
		Event:     "status",
		AgentID:   agentID,
		AgentName: agentName,
		Data: map[string]interface{}{
			"status":      status,
			"status_text": statusText,
			"category":    category,
		},
	})
}

// LogSpawn records an agent being spawned.
func (rl *RunLogger) LogSpawn(agentID, agentName, agentType, folder string, args []string) {
	if rl == nil {
		return
	}
	rl.write(logEntry{
		Timestamp: now(),
		Event:     "spawn",
		AgentID:   agentID,
		AgentName: agentName,
		Data: map[string]interface{}{
			"type":   agentType,
			"folder": folder,
			"args":   args,
		},
	})
}

// LogExit records an agent exiting.
func (rl *RunLogger) LogExit(agentID, agentName string, exitCode int) {
	if rl == nil {
		return
	}
	rl.write(logEntry{
		Timestamp: now(),
		Event:     "exit",
		AgentID:   agentID,
		AgentName: agentName,
		Data: map[string]interface{}{
			"exit_code": exitCode,
		},
	})
}

// LogPrompt records a prompt sent to an agent.
func (rl *RunLogger) LogPrompt(agentID, agentName, promptType, prompt string) {
	if rl == nil {
		return
	}
	rl.write(logEntry{
		Timestamp: now(),
		Event:     "prompt",
		AgentID:   agentID,
		AgentName: agentName,
		Data: map[string]interface{}{
			"prompt_type": promptType,
			"prompt":      prompt,
		},
	})
}

// LogHookEvent records a hook event from an agent.
func (rl *RunLogger) LogHookEvent(agentID, agentName, eventType, status string) {
	if rl == nil {
		return
	}
	rl.write(logEntry{
		Timestamp: now(),
		Event:     "hook",
		AgentID:   agentID,
		AgentName: agentName,
		Data: map[string]interface{}{
			"event_type": eventType,
			"status":     status,
		},
	})
}
