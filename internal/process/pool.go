// Package process manages headless agent processes using stdin/stdout JSON streams
// instead of PTY sessions. This is the replacement for internal/terminal for the
// headless architecture.
package process

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// managedAgent wraps a Runner with metadata for pool management.
type managedAgent struct {
	runner    *Runner
	agentID   uuid.UUID
	name      string
	ready     chan struct{} // closed when first system/assistant message received
	readyOnce sync.Once
}

// Pool manages the lifecycle of multiple headless agent processes.
// It is safe for concurrent use.
type Pool struct {
	mu     sync.RWMutex
	agents map[uuid.UUID]*managedAgent
	mcpURL string

	// OutputSubscriber is called when a stream message arrives from an agent.
	OutputSubscriber func(agentID uuid.UUID, agentName string, data []byte)
	// LogSubscriber receives human-readable log lines for each stream message.
	LogSubscriber func(agentID uuid.UUID, agentName string, data []byte)
	// OnStatusChanged is called when an agent's inferred status changes.
	OnStatusChanged func(agentID uuid.UUID, status models.AgentStatus)
	// OnExit is called when an agent process exits.
	OnExit func(agentID uuid.UUID, exitCode int)
}

// NewPool creates a Pool with the given MCP server URL.
func NewPool(mcpURL string) *Pool {
	return &Pool{
		agents: make(map[uuid.UUID]*managedAgent),
		mcpURL: mcpURL,
	}
}

// Spawn creates and starts a headless agent process.
func (p *Pool) Spawn(agentID uuid.UUID, name string, args []string, env []string, dir string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.agents[agentID]; exists {
		return fmt.Errorf("agent %s already spawned", name)
	}

	runner := NewRunner(args, env, dir)
	ma := &managedAgent{
		runner:  runner,
		agentID: agentID,
		name:    name,
		ready:   make(chan struct{}),
	}

	runner.OnMessage = func(msg StreamMessage) {
		// Mark ready on first system or assistant message.
		if msg.Type == "system" || msg.Type == "assistant" {
			ma.readyOnce.Do(func() {
				close(ma.ready)
			})
		}

		// Forward raw bytes to output subscriber.
		if p.OutputSubscriber != nil && len(msg.Raw) > 0 {
			p.OutputSubscriber(agentID, name, msg.Raw)
		}

		// Forward log line.
		if p.LogSubscriber != nil {
			logLine := formatLogLine(msg)
			if logLine != "" {
				p.LogSubscriber(agentID, name, []byte(logLine))
			}
		}

		// Infer status from message type.
		if p.OnStatusChanged != nil {
			switch msg.Type {
			case "system":
				p.OnStatusChanged(agentID, models.AgentStatusRunning)
			case "assistant":
				p.OnStatusChanged(agentID, models.AgentStatusRunning)
			case "result":
				p.OnStatusChanged(agentID, models.AgentStatusIdle)
			}
		}
	}

	runner.OnExit = func(exitCode int) {
		slog.Info("agent process exited", "agentID", agentID, "name", name, "exitCode", exitCode)

		// Mark ready so any blocked SendPrompt unblocks with an error.
		ma.readyOnce.Do(func() {
			close(ma.ready)
		})

		if p.OnExit != nil {
			p.OnExit(agentID, exitCode)
		}
	}

	if err := runner.Start(); err != nil {
		return fmt.Errorf("start agent %s: %w", name, err)
	}

	p.agents[agentID] = ma
	return nil
}

// SendPrompt blocks until the agent is ready, then sends the prompt via stdin.
// Returns an error if the process exits before becoming ready.
func (p *Pool) SendPrompt(agentID uuid.UUID, text string) error {
	p.mu.RLock()
	ma, ok := p.agents[agentID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	// Wait for readiness or early exit.
	select {
	case <-ma.ready:
		// Check if process is still alive after becoming ready.
		select {
		case <-ma.runner.Wait():
			return fmt.Errorf("agent %s exited before prompt could be sent (exit code %d)", ma.name, ma.runner.ExitCode())
		default:
		}
		return ma.runner.SendPrompt(text)
	case <-ma.runner.Wait():
		return fmt.Errorf("agent %s exited before becoming ready (exit code %d)", ma.name, ma.runner.ExitCode())
	}
}

// Stop gracefully stops the agent process (SIGTERM → 5s → SIGKILL).
func (p *Pool) Stop(agentID uuid.UUID) error {
	p.mu.RLock()
	ma, ok := p.agents[agentID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	return ma.runner.Stop()
}

// Kill immediately sends SIGKILL to the agent process group.
func (p *Pool) Kill(agentID uuid.UUID) {
	p.mu.RLock()
	ma, ok := p.agents[agentID]
	p.mu.RUnlock()

	if ok {
		ma.runner.Kill()
	}
}

// Restart stops and removes an agent from the pool.
// The caller (daemon.SpawnAgent) handles re-spawning with full config.
func (p *Pool) Restart(agentID uuid.UUID) {
	p.Kill(agentID)
}

// SendText sends raw text to the agent (compatibility shim — use SendPrompt for structured input).
func (p *Pool) SendText(agentID uuid.UUID, text string) {
	_ = p.SendPrompt(agentID, text)
}

// StopAll gracefully stops all agent processes.
func (p *Pool) StopAll() {
	p.mu.RLock()
	agents := make([]*managedAgent, 0, len(p.agents))
	for _, ma := range p.agents {
		agents = append(agents, ma)
	}
	p.mu.RUnlock()

	if len(agents) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, ma := range agents {
		wg.Add(1)
		go func(ma *managedAgent) {
			defer wg.Done()
			if err := ma.runner.Stop(); err != nil {
				slog.Warn("stop agent failed", "name", ma.name, "error", err)
			}
		}(ma)
	}
	wg.Wait()
}

// IsRunning reports whether the agent process is currently running.
func (p *Pool) IsRunning(agentID uuid.UUID) bool {
	p.mu.RLock()
	ma, ok := p.agents[agentID]
	p.mu.RUnlock()

	if !ok {
		return false
	}
	return ma.runner.IsRunning()
}

// ExitCode returns the exit code of the agent process, or -1 if not found
// or the process hasn't exited yet.
func (p *Pool) ExitCode(agentID uuid.UUID) int {
	p.mu.RLock()
	ma, ok := p.agents[agentID]
	p.mu.RUnlock()

	if !ok {
		return -1
	}
	return ma.runner.ExitCode()
}

// Resize is a no-op for headless processes (no PTY to resize).
func (p *Pool) Resize(agentID uuid.UUID, cols, rows uint16) {}

// MCPURL returns the MCP server URL the pool was configured with.
func (p *Pool) MCPURL() string {
	return p.mcpURL
}

// formatLogLine produces a human-readable summary for a stream message.
func formatLogLine(msg StreamMessage) string {
	switch msg.Type {
	case "system":
		if msg.Subtype == "init" {
			return "session initialized"
		}
		return "system: " + msg.Subtype
	case "assistant":
		return "assistant message received"
	case "result":
		return "result: " + msg.Subtype
	default:
		return ""
	}
}
