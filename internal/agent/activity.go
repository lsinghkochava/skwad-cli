package agent

import (
	"sync"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// ActivityController manages the status state machine for a single agent.
// Status is derived from stream-json messages (OnStreamMessage / OnProcessExit)
// or hook events (OnHookRunning / OnHookIdle / etc. for non-Claude agents).
// It is safe for concurrent use.
type ActivityController struct {
	mu      sync.Mutex
	agentID uuid.UUID
	mode    models.ActivityTracking
	status  models.AgentStatus
	manager *Manager

	// Callbacks
	OnStatusChanged func(id uuid.UUID, status models.AgentStatus)
	OnTurnComplete  func() // called when a stream result message is received
}

// NewActivityController creates a controller for the given agent.
func NewActivityController(agentID uuid.UUID, mode models.ActivityTracking, mgr *Manager) *ActivityController {
	return &ActivityController{
		agentID: agentID,
		mode:    mode,
		status:  models.AgentStatusIdle,
		manager: mgr,
	}
}

// ---------------------------------------------------------------------------
// Stream-based status detection (headless mode)
// ---------------------------------------------------------------------------

// OnStreamMessage derives agent status from stream-json message types.
// msgType is the top-level "type" field; subtype is the optional "subtype" field.
func (c *ActivityController) OnStreamMessage(msgType string, subtype string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch msgType {
	case "system":
		c.setStatus(models.AgentStatusRunning)
	case "assistant":
		c.setStatus(models.AgentStatusRunning)
	case "result":
		c.setStatus(models.AgentStatusIdle)
		if c.OnTurnComplete != nil {
			c.OnTurnComplete()
		}
	}
}

// OnProcessExit is called when the headless agent process exits.
func (c *ActivityController) OnProcessExit(exitCode int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if exitCode != 0 {
		c.setStatus(models.AgentStatusError)
	} else {
		c.setStatus(models.AgentStatusIdle)
	}
}

// ---------------------------------------------------------------------------
// Hook-based status detection (for non-Claude agents)
// ---------------------------------------------------------------------------

// OnHookRunning is called when a hook event signals the agent is working.
func (c *ActivityController) OnHookRunning() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusRunning)
}

// OnHookIdle is called when a hook event signals the agent has gone idle.
func (c *ActivityController) OnHookIdle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusIdle)
}

// OnHookBlocked is called when a hook event signals the agent needs user input.
func (c *ActivityController) OnHookBlocked() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusInput)
}

// OnHookError is called when a hook event signals an error.
func (c *ActivityController) OnHookError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusError)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (c *ActivityController) setStatus(s models.AgentStatus) {
	if c.status == s {
		return
	}
	c.status = s
	if c.manager != nil {
		c.manager.UpdateAgent(c.agentID, func(a *models.Agent) {
			a.Status = s
		})
	}
	if c.OnStatusChanged != nil {
		c.OnStatusChanged(c.agentID, s)
	}
}
