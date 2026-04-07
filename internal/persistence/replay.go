package persistence

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// RunState represents the reconstructed state of a run from its event log.
type RunState struct {
	RunID            string
	StartedAt        time.Time
	Agents           map[string]AgentRunState // agentID → state
	CurrentPhase     string
	CurrentIteration int
	Completed        bool
	Failed           bool
	LastEvent        Event
}

// AgentRunState tracks per-agent state from events.
type AgentRunState struct {
	AgentID     string
	AgentName   string
	SessionID   string
	Spawned     bool
	Registered  bool
	Exited      bool
	ExitCode    int
	PromptsSent int
	LastPrompt  string
}

// Replay reads an event log and reconstructs RunState.
func Replay(runID string) (*RunState, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, configDir, "runs", runID)
	path := filepath.Join(dir, "events.jsonl")

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	state := &RunState{
		RunID:  runID,
		Agents: make(map[string]AgentRunState),
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue // lenient: skip malformed lines
		}
		state.LastEvent = event
		applyEvent(state, event)
	}

	return state, scanner.Err()
}

func applyEvent(state *RunState, event Event) {
	switch event.Type {
	case EventRunStarted:
		state.StartedAt = event.Time
	case EventAgentSpawned:
		as := state.Agents[event.AgentID]
		as.AgentID = event.AgentID
		as.AgentName = event.AgentName
		as.Spawned = true
		state.Agents[event.AgentID] = as
	case EventAgentRegistered:
		as := state.Agents[event.AgentID]
		as.Registered = true
		if event.Data != nil {
			var reg map[string]string
			if json.Unmarshal(event.Data, &reg) == nil {
				if sid, ok := reg["session_id"]; ok {
					as.SessionID = sid
				}
			}
		}
		state.Agents[event.AgentID] = as
	case EventPromptSent:
		as := state.Agents[event.AgentID]
		as.PromptsSent++
		if event.Data != nil {
			var prompt string
			_ = json.Unmarshal(event.Data, &prompt)
			as.LastPrompt = prompt
		}
		state.Agents[event.AgentID] = as
	case EventAgentExited:
		as := state.Agents[event.AgentID]
		as.Exited = true
		if event.Data != nil {
			var code int
			_ = json.Unmarshal(event.Data, &code)
			as.ExitCode = code
		}
		state.Agents[event.AgentID] = as
	case EventPhaseTransition:
		if event.Data != nil {
			var phase string
			_ = json.Unmarshal(event.Data, &phase)
			state.CurrentPhase = phase
		}
	case EventIteration:
		if event.Data != nil {
			var iter int
			_ = json.Unmarshal(event.Data, &iter)
			state.CurrentIteration = iter
		}
	case EventRunCompleted:
		state.Completed = true
	case EventRunFailed:
		state.Failed = true
	}
}

// ListRuns returns all run IDs with their last event status.
func ListRuns() ([]RunState, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, configDir, "runs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var runs []RunState
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := Replay(entry.Name())
		if err != nil {
			continue
		}
		runs = append(runs, *state)
	}
	return runs, nil
}
