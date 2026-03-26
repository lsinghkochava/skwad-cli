package agent

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

const (
	idleTimeoutDefault = 5 * time.Second
	idleTimeoutHook    = 30 * time.Second
	inputGuardDuration = 10 * time.Second
)

// ActivityController manages the status state machine for a single agent.
// It is safe for concurrent use.
type ActivityController struct {
	mu      sync.Mutex
	agentID uuid.UUID
	mode    models.ActivityTracking
	status  models.AgentStatus
	manager *Manager

	idleTimer    *time.Timer
	inputGuard   *time.Timer
	guardActive  bool
	pendingTexts []string

	OnStatusChanged  func(id uuid.UUID, status models.AgentStatus)
	OnDeliverPending func(id uuid.UUID, texts []string)
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

// OnTerminalOutput is called when the terminal produces output.
func (c *ActivityController) OnTerminalOutput() {
	if c.mode == models.ActivityTrackingNone {
		return
	}
	if c.mode == models.ActivityTrackingUserInput {
		// Hook-managed agents ignore output as a status source.
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusRunning)
	c.resetIdleTimer(idleTimeoutDefault)
}

// OnUserInput is called when the user presses a key in the terminal.
func (c *ActivityController) OnUserInput(keyCode uint16) {
	if c.mode == models.ActivityTrackingNone {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Activate input guard to block automatic text injection.
	c.activateInputGuard()

	// Return key unblocks a blocked agent.
	if c.status == models.AgentStatusInput && isReturnKey(keyCode) {
		c.setStatus(models.AgentStatusRunning)
		c.resetIdleTimer(idleTimeoutDefault)
		return
	}
	// Escape key cancels blocked state.
	if c.status == models.AgentStatusInput && isEscapeKey(keyCode) {
		c.setStatus(models.AgentStatusIdle)
		return
	}
}

// OnHookRunning is called when a hook event signals the agent is working.
func (c *ActivityController) OnHookRunning() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusRunning)
	c.stopIdleTimer()
}

// OnHookIdle is called when a hook event signals the agent has gone idle.
func (c *ActivityController) OnHookIdle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusIdle)
	c.stopIdleTimer()
	c.deliverPending()
}

// OnHookBlocked is called when a hook event signals the agent needs user input.
func (c *ActivityController) OnHookBlocked() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusInput)
	c.stopIdleTimer()
}

// OnHookError is called when a hook event signals an error.
func (c *ActivityController) OnHookError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setStatus(models.AgentStatusError)
	c.stopIdleTimer()
}

// QueueText queues text for injection, subject to the input guard.
func (c *ActivityController) QueueText(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.guardActive {
		c.pendingTexts = append(c.pendingTexts, text)
		return
	}
	if c.OnDeliverPending != nil {
		c.OnDeliverPending(c.agentID, []string{text})
	}
}

func (c *ActivityController) setStatus(s models.AgentStatus) {
	if c.status == s {
		return
	}
	c.status = s
	c.manager.UpdateAgent(c.agentID, func(a *models.Agent) {
		a.Status = s
	})
	if c.OnStatusChanged != nil {
		c.OnStatusChanged(c.agentID, s)
	}
}

func (c *ActivityController) resetIdleTimer(d time.Duration) {
	c.stopIdleTimer()
	c.idleTimer = time.AfterFunc(d, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.setStatus(models.AgentStatusIdle)
		c.deliverPending()
	})
}

func (c *ActivityController) stopIdleTimer() {
	if c.idleTimer != nil {
		c.idleTimer.Stop()
		c.idleTimer = nil
	}
}

func (c *ActivityController) activateInputGuard() {
	if c.inputGuard != nil {
		c.inputGuard.Reset(inputGuardDuration)
		return
	}
	c.guardActive = true
	c.inputGuard = time.AfterFunc(inputGuardDuration, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.guardActive = false
		c.inputGuard = nil
		c.deliverPending()
	})
}

func (c *ActivityController) deliverPending() {
	if len(c.pendingTexts) == 0 {
		return
	}
	texts := c.pendingTexts
	c.pendingTexts = nil
	if c.OnDeliverPending != nil {
		c.OnDeliverPending(c.agentID, texts)
	}
}

func isReturnKey(keyCode uint16) bool  { return keyCode == 36 || keyCode == 13 }
func isEscapeKey(keyCode uint16) bool  { return keyCode == 53 || keyCode == 27 }
