// Package history provides conversation history listing and management
// for supported AI agent types (claude, codex).
// Each agent type has its own Provider that knows where to find session files.
package history

import (
	"sort"
	"time"

	"github.com/Jared-Boschmann/skwad-linux/internal/models"
)

// SessionSummary holds display metadata for a single past session.
type SessionSummary struct {
	SessionID    string
	Title        string // first meaningful user message (truncated)
	Timestamp    time.Time
	MessageCount int
}

// Provider lists and manages sessions for a specific agent type.
type Provider interface {
	// ListSessions returns sessions relevant to the given project folder,
	// sorted by timestamp descending. Returns at most 20 entries.
	ListSessions(folder string) ([]SessionSummary, error)
	// DeleteSession removes the session file(s) for the given session ID.
	DeleteSession(sessionID string) error
}

// Service aggregates Providers for all supported agent types.
type Service struct {
	providers map[models.AgentType]Provider
}

// New creates a Service with built-in providers for all supported agent types.
func New() *Service {
	return &Service{
		providers: map[models.AgentType]Provider{
			models.AgentTypeClaude:  &ClaudeProvider{},
			models.AgentTypeCodex:   &CodexProvider{},
			models.AgentTypeGemini:  &GeminiProvider{},
			models.AgentTypeCopilot: &CopilotProvider{},
		},
	}
}

// Supports reports whether history is available for the given agent type.
func (s *Service) Supports(agentType models.AgentType) bool {
	_, ok := s.providers[agentType]
	return ok
}

// ListSessions returns up to 20 sessions for the agent, newest first.
func (s *Service) ListSessions(agentType models.AgentType, folder string) ([]SessionSummary, error) {
	p, ok := s.providers[agentType]
	if !ok {
		return nil, nil
	}
	sessions, err := p.ListSessions(folder)
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp.After(sessions[j].Timestamp)
	})
	if len(sessions) > 20 {
		sessions = sessions[:20]
	}
	return sessions, nil
}

// DeleteSession removes session files for the given agent type and session ID.
func (s *Service) DeleteSession(agentType models.AgentType, sessionID string) error {
	p, ok := s.providers[agentType]
	if !ok {
		return nil
	}
	return p.DeleteSession(sessionID)
}
