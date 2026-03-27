// Package models contains the pure data types shared across all Skwad packages.
// It has no dependencies on UI, persistence, or I/O — only standard library types.
package models

import "github.com/google/uuid"

// AgentType identifies which AI CLI tool an agent runs.
type AgentType string

const (
	AgentTypeClaude    AgentType = "claude"
	AgentTypeCodex     AgentType = "codex"
	AgentTypeOpenCode  AgentType = "opencode"
	AgentTypeGemini    AgentType = "gemini"
	AgentTypeCopilot   AgentType = "copilot"
	AgentTypeCustom1   AgentType = "custom1"
	AgentTypeCustom2   AgentType = "custom2"
	AgentTypeShell     AgentType = "shell"
)

// AgentStatus is the observable lifecycle state of an agent terminal.
type AgentStatus string

const (
	AgentStatusIdle    AgentStatus = "idle"
	AgentStatusRunning AgentStatus = "running"
	AgentStatusInput   AgentStatus = "input"
	AgentStatusError   AgentStatus = "error"
)

// ActivityTracking controls which sources drive running/idle status transitions.
type ActivityTracking int

const (
	ActivityTrackingAll       ActivityTracking = 0 // terminal output + user input
	ActivityTrackingUserInput ActivityTracking = 1 // hook-managed: only user input tracked locally
	ActivityTrackingNone      ActivityTracking = 2 // shell agents: no status tracking
)

// GitStats holds the diff summary for an agent's working folder.
type GitStats struct {
	Insertions int
	Deletions  int
	Files      int
}

// Agent is the persisted data model for a single agent.
type Agent struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Avatar       string     `json:"avatar"` // emoji or "data:image/png;base64,..."
	Folder       string     `json:"folder"`
	AgentType    AgentType  `json:"agentType"`
	ShellCommand string     `json:"shellCommand,omitempty"`
	PersonaID    *uuid.UUID `json:"personaId,omitempty"`
	CreatedBy    *uuid.UUID `json:"createdBy,omitempty"`
	IsCompanion  bool       `json:"isCompanion"`

	// Runtime state — not persisted.
	Status          AgentStatus      `json:"-"`
	StatusText      string           `json:"-"`
	StatusCategory  string           `json:"-"`
	IsRegistered    bool             `json:"-"`
	SessionID       string           `json:"-"`
	ResumeSessionID string           `json:"-"`
	IsFork          bool             `json:"-"` // when true, use fork flags instead of resume flags
	RestartToken    int              `json:"-"`
	TerminalTitle   string           `json:"-"`
	GitStats        GitStats         `json:"-"`
	WorkingFolder   string           `json:"-"` // hook-reported cwd when different from Folder
	Metadata        map[string]string `json:"-"`

	// Markdown/Mermaid preview state — not persisted.
	MarkdownFilePath   string   `json:"-"`
	MarkdownMaximized  bool     `json:"-"`
	MarkdownFileHistory []string `json:"-"`
	MermaidSource      string   `json:"-"`
	MermaidTitle       string   `json:"-"`
}

// ActivityMode returns the appropriate ActivityTracking for this agent type.
func (a *Agent) ActivityMode() ActivityTracking {
	switch a.AgentType {
	case AgentTypeShell:
		return ActivityTrackingNone
	case AgentTypeClaude, AgentTypeCodex:
		if a.SessionID != "" {
			return ActivityTrackingUserInput
		}
		return ActivityTrackingAll
	default:
		return ActivityTrackingAll
	}
}

// SupportsHooks reports whether this agent type emits lifecycle hook events.
func (a *Agent) SupportsHooks() bool {
	return a.AgentType == AgentTypeClaude || a.AgentType == AgentTypeCodex
}

// SupportsResume reports whether this agent type supports session resume.
func (a *Agent) SupportsResume() bool {
	switch a.AgentType {
	case AgentTypeClaude, AgentTypeCodex, AgentTypeGemini, AgentTypeCopilot:
		return true
	}
	return false
}

// IsNewSession reports whether this agent is starting a fresh session (not resume/fork).
func (a *Agent) IsNewSession() bool {
	return a.ResumeSessionID == "" && !a.IsFork
}

// SupportsSystemPrompt reports whether this agent type accepts a system prompt flag.
func (a *Agent) SupportsSystemPrompt() bool {
	return a.AgentType == AgentTypeClaude || a.AgentType == AgentTypeCodex
}
