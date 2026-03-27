package tui

import (
	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// OutputMsg carries raw PTY output from an agent's session.
type OutputMsg struct {
	AgentID uuid.UUID
	Data    []byte
}

// StatusMsg carries agent status changes from the Pool.
type StatusMsg struct {
	AgentID uuid.UUID
	Status  models.AgentStatus
	Text    string
}

// AgentExitMsg signals that an agent's PTY session has exited.
type AgentExitMsg struct {
	AgentID  uuid.UUID
	ExitCode int
}
