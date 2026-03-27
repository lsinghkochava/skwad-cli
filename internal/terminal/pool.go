// Package terminal handles PTY session lifecycle for agent terminals.
// Pool is the central orchestrator that wires together Sessions,
// ActivityControllers, and the AgentCoordinator.
package terminal

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/agent"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

const (
	shellStartDelay   = 500 * time.Millisecond // initial delay before first shell starts
	shellStaggerDelay = 300 * time.Millisecond // delay between subsequent shell agents
)

const (
	maxOutputBuf    = 4096  // bytes of ANSI-stripped output retained per agent (autopilot use)
	maxRawOutputBuf = 65536 // bytes of raw output retained per agent (display use)
)

// Pool owns one Session + ActivityController per agent, wired together.
// It is the central orchestrator between AgentManager, PTY sessions,
// ActivityControllers, and the AgentCoordinator.
type Pool struct {
	mu      sync.RWMutex
	entries map[uuid.UUID]*entry

	manager     *agent.Manager
	coordinator *agent.Coordinator
	builder     *agent.CommandBuilder
	mcpURL      string
	pluginDir   string

	// lastOutput stores the most recent ANSI-stripped terminal output per agent (autopilot).
	lastOutputMu sync.RWMutex
	lastOutput   map[uuid.UUID][]byte

	// rawOutput stores the most recent raw (with ANSI) terminal output per agent (display).
	rawOutputMu sync.RWMutex
	rawOutput   map[uuid.UUID][]byte

	// OnRawOutput is called when new raw output arrives for an agent.
	OnRawOutput func(agentID uuid.UUID)

	// OnFocusRequest is called when a pane requests keyboard focus.
	OnFocusRequest func(agentID uuid.UUID)
	// OnTitleChanged is called when a terminal's OSC title changes.
	OnTitleChanged func(agentID uuid.UUID, title string)
	// OnStatusChanged is called when an agent's status changes.
	OnStatusChanged func(agentID uuid.UUID, status models.AgentStatus)
	// OutputSubscriber is called when new output arrives from an agent's terminal.
	// The callback receives the agent ID, display name, and raw output bytes.
	OutputSubscriber func(agentID uuid.UUID, agentName string, data []byte)
}

type entry struct {
	session    *Session
	activity   *agent.ActivityController
	agentID    uuid.UUID
}

// NewPool creates a Pool. mcpURL and pluginDir are used by the CommandBuilder.
func NewPool(mgr *agent.Manager, coord *agent.Coordinator, mcpURL, pluginDir string) *Pool {
	return &Pool{
		entries:    make(map[uuid.UUID]*entry),
		lastOutput: make(map[uuid.UUID][]byte),
		rawOutput:  make(map[uuid.UUID][]byte),
		manager:    mgr,
		coordinator: coord,
		builder:     &agent.CommandBuilder{MCPServerURL: mcpURL, PluginDir: pluginDir},
		mcpURL:      mcpURL,
		pluginDir:   pluginDir,
	}
}

// Spawn creates and starts a terminal session for the given agent.
// If a session already exists for that agent (same RestartToken), it is a no-op.
func (p *Pool) Spawn(a *models.Agent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.entries[a.ID]; exists {
		return
	}

	settings := p.manager.ActiveSettings()
	var persona *models.Persona
	if a.PersonaID != nil {
		persona = p.manager.Persona(*a.PersonaID)
	}

	cmd := p.builder.Build(a, persona, settings)
	env := []string{
		"SKWAD_AGENT_ID=" + a.ID.String(),
		"SKWAD_URL=" + strings.TrimSuffix(p.mcpURL, "/mcp"),
	}

	// Shell agents use deferred staggered startup on restore.
	if a.AgentType == models.AgentTypeShell {
		go p.spawnDeferred(a.ID, cmd, env)
		return
	}

	p.spawnNow(a.ID, a, cmd, env)
}

func (p *Pool) spawnDeferred(agentID uuid.UUID, cmd string, env []string) {
	time.Sleep(shellStartDelay)
	p.mu.Lock()
	a, ok := p.manager.Agent(agentID)
	if !ok {
		p.mu.Unlock()
		return
	}
	p.spawnNow(agentID, a, cmd, env)
	p.mu.Unlock()
}

func (p *Pool) spawnNow(agentID uuid.UUID, a *models.Agent, cmd string, env []string) {
	sess, err := NewSession(cmd, env)
	if err != nil {
		slog.Error("failed to spawn session", "agentID", agentID, "err", err)
		p.manager.UpdateAgent(agentID, func(ag *models.Agent) {
			ag.Status = models.AgentStatusError
		})
		return
	}

	ac := agent.NewActivityController(agentID, a.ActivityMode(), p.manager)
	ac.OnStatusChanged = func(id uuid.UUID, status models.AgentStatus) {
		if status == models.AgentStatusIdle {
			p.coordinator.NotifyIdleAgent(id)
		}
		if p.OnStatusChanged != nil {
			p.OnStatusChanged(id, status)
		}
	}
	ac.OnDeliverPending = func(id uuid.UUID, texts []string) {
		p.mu.RLock()
		e, ok := p.entries[id]
		p.mu.RUnlock()
		if ok {
			for _, t := range texts {
				e.session.InjectText(t)
			}
		}
	}

	sess.OnOutput = func(data []byte) {
		ac.OnTerminalOutput()

		// Store ANSI-stripped output for autopilot analysis.
		cleaned := []byte(StripANSI(string(data)))
		p.lastOutputMu.Lock()
		existing := p.lastOutput[agentID]
		existing = append(existing, cleaned...)
		if len(existing) > maxOutputBuf {
			existing = existing[len(existing)-maxOutputBuf:]
		}
		p.lastOutput[agentID] = existing
		p.lastOutputMu.Unlock()

		// Store raw output for display in the terminal pane.
		p.rawOutputMu.Lock()
		raw := p.rawOutput[agentID]
		raw = append(raw, data...)
		if len(raw) > maxRawOutputBuf {
			raw = raw[len(raw)-maxRawOutputBuf:]
		}
		p.rawOutput[agentID] = raw
		p.rawOutputMu.Unlock()

		if p.OnRawOutput != nil {
			p.OnRawOutput(agentID)
		}

		// Notify output subscriber (used by --watch mode).
		if p.OutputSubscriber != nil {
			name := ""
			if ag, ok := p.manager.Agent(agentID); ok {
				name = ag.Name
			}
			p.OutputSubscriber(agentID, name, data)
		}
	}
	sess.OnTitleChange = func(title string) {
		clean := CleanTitle(title)
		p.manager.UpdateAgent(agentID, func(ag *models.Agent) {
			ag.TerminalTitle = clean
		})
		if p.OnTitleChanged != nil {
			p.OnTitleChanged(agentID, clean)
		}
	}
	sess.OnExit = func(code int) {
		slog.Info("session exited", "agentID", agentID, "code", code)
		p.manager.UpdateAgent(agentID, func(ag *models.Agent) {
			ag.Status = models.AgentStatusIdle
		})
	}

	// Start session goroutines AFTER all callbacks are set.
	sess.Start()

	e := &entry{session: sess, activity: ac, agentID: agentID}
	p.entries[agentID] = e

	// Schedule registration prompt injection (skip Claude — uses inline registration).
	if a.AgentType != models.AgentTypeClaude {
		go func() {
			time.Sleep(registrationDelay)
			if settings := p.manager.ActiveSettings(); settings.MCPServerEnabled {
				prompt := agent.RegistrationPrompt(agentID, p.mcpURL, a.AgentType)
				if prompt != "" {
					p.InjectText(agentID, prompt)
				}
			}
		}()
	}
}

// RawOutput returns the most recent raw terminal output for the agent (up to 64KB).
// This includes ANSI escape sequences; intended for display in a terminal renderer.
func (p *Pool) RawOutput(agentID uuid.UUID) []byte {
	p.rawOutputMu.RLock()
	defer p.rawOutputMu.RUnlock()
	data := p.rawOutput[agentID]
	result := make([]byte, len(data))
	copy(result, data)
	return result
}

// ForceRegistration re-injects the MCP registration prompt for the given agent.
// This is used by the "Register" context menu item to force re-registration.
func (p *Pool) ForceRegistration(agentID uuid.UUID) {
	a, ok := p.manager.Agent(agentID)
	if !ok {
		return
	}
	settings := p.manager.ActiveSettings()
	if settings == nil || !settings.MCPServerEnabled {
		return
	}
	prompt := agent.RegistrationPrompt(agentID, p.mcpURL, a.AgentType)
	if prompt != "" {
		p.InjectText(agentID, prompt)
	}
}

// LastOutput returns the most recent clean terminal output for the agent (up to 4KB).
func (p *Pool) LastOutput(agentID uuid.UUID) string {
	p.lastOutputMu.RLock()
	defer p.lastOutputMu.RUnlock()
	return string(p.lastOutput[agentID])
}

// Kill stops the session for the given agent and removes it from the pool.
func (p *Pool) Kill(agentID uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[agentID]; ok {
		e.session.Kill()
		delete(p.entries, agentID)
	}
	p.coordinator.UnregisterAgent(agentID)
	p.lastOutputMu.Lock()
	delete(p.lastOutput, agentID)
	p.lastOutputMu.Unlock()
	p.rawOutputMu.Lock()
	delete(p.rawOutput, agentID)
	p.rawOutputMu.Unlock()
}

// Restart kills the existing session and spawns a fresh one.
func (p *Pool) Restart(agentID uuid.UUID) {
	p.Kill(agentID)
	if a, ok := p.manager.Agent(agentID); ok {
		p.Spawn(a)
	}
}

// SendText sends text to the agent's terminal without a newline.
func (p *Pool) SendText(agentID uuid.UUID, text string) {
	p.mu.RLock()
	e, ok := p.entries[agentID]
	p.mu.RUnlock()
	if ok {
		e.session.SendText(text)
		e.activity.OnUserInput(0)
	}
}

// InjectText sends text + return, bypassing the input guard (used for automated injection).
func (p *Pool) InjectText(agentID uuid.UUID, text string) {
	p.mu.RLock()
	e, ok := p.entries[agentID]
	p.mu.RUnlock()
	if ok {
		e.session.InjectText(text)
	}
}

// QueueText queues text for injection, subject to the input protection guard.
func (p *Pool) QueueText(agentID uuid.UUID, text string) {
	p.mu.RLock()
	e, ok := p.entries[agentID]
	p.mu.RUnlock()
	if ok {
		e.activity.QueueText(text)
	}
}

// OnUserInput forwards a keypress event to the activity controller.
func (p *Pool) OnUserInput(agentID uuid.UUID, keyCode uint16) {
	p.mu.RLock()
	e, ok := p.entries[agentID]
	p.mu.RUnlock()
	if ok {
		e.activity.OnUserInput(keyCode)
	}
}

// OnHookRunning signals that a hook event put this agent into the running state.
func (p *Pool) OnHookRunning(agentID uuid.UUID) {
	p.mu.RLock()
	e, ok := p.entries[agentID]
	p.mu.RUnlock()
	if ok {
		e.activity.OnHookRunning()
	}
}

// OnHookIdle signals that a hook event put this agent into the idle state.
func (p *Pool) OnHookIdle(agentID uuid.UUID) {
	p.mu.RLock()
	e, ok := p.entries[agentID]
	p.mu.RUnlock()
	if ok {
		e.activity.OnHookIdle()
	}
}

// OnHookBlocked signals that a hook event put this agent into the blocked state.
func (p *Pool) OnHookBlocked(agentID uuid.UUID) {
	p.mu.RLock()
	e, ok := p.entries[agentID]
	p.mu.RUnlock()
	if ok {
		e.activity.OnHookBlocked()
	}
}

// Resize informs the terminal of new dimensions.
func (p *Pool) Resize(agentID uuid.UUID, cols, rows uint16) {
	p.mu.RLock()
	e, ok := p.entries[agentID]
	p.mu.RUnlock()
	if ok {
		e.session.Resize(cols, rows)
	}
}

// IsRunning reports whether the agent has a live session.
func (p *Pool) IsRunning(agentID uuid.UUID) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.entries[agentID]
	return ok && e.session.IsRunning()
}

// ExitCode returns the exit code for an agent's session.
// Returns -1 if the agent is not found or the session hasn't exited.
func (p *Pool) ExitCode(agentID uuid.UUID) int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if e, ok := p.entries[agentID]; ok {
		return e.session.ExitCode()
	}
	return -1
}

// StopAll kills every session in the pool.
func (p *Pool) StopAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, e := range p.entries {
		e.session.Kill()
		delete(p.entries, id)
	}
}
