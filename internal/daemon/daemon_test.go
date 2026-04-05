package daemon

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// freePort finds a random available TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestDaemonNewAndStart(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	d, err := New(Config{
		MCPPort: port,
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	// Give server a moment to bind.
	time.Sleep(50 * time.Millisecond)

	// Verify MCP server responds on the port.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		t.Errorf("expected 'OK', got %q", string(body))
	}
}

func TestDaemonNewAndStart_PortFreedAfterStop(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	d, err := New(Config{MCPPort: port, DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	d.Stop()
	time.Sleep(50 * time.Millisecond)

	// Port should be free now — verify we can bind to it.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("expected port %d to be free after Stop, got: %v", port, err)
	}
	ln.Close()
}

func TestDaemonStopIdempotent(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	d, err := New(Config{MCPPort: port, DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Stop twice — should not panic.
	d.Stop()
	d.Stop()
}

func TestDaemonNew_UsesDataDir(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Config{MCPPort: 0, DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.Store.Dir() != dir {
		t.Errorf("expected store dir %q, got %q", dir, d.Store.Dir())
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		name string
		input string
		want string
	}{
		{"simple", "Coder", "coder"},
		{"spaces", "Lead Coder", "lead-coder"},
		{"slashes", "feature/test", "feature-test"},
		{"mixed", "My Agent/v2", "my-agent-v2"},
		{"already clean", "tester", "tester"},
		{"multiple spaces", "a  b", "a--b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeName(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestDaemonStart_SetsSessionID(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	d, err := New(Config{MCPPort: port, DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	if d.SessionID == "" {
		t.Error("expected SessionID to be set after Start")
	}
	if len(d.SessionID) != 8 {
		t.Errorf("expected SessionID length 8, got %d (%q)", len(d.SessionID), d.SessionID)
	}
}

func TestDaemonStart_InitializesWorktreeMap(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	d, err := New(Config{MCPPort: port, DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	if d.worktrees == nil {
		t.Error("expected worktrees map to be initialized after Start")
	}
}

// newTestDaemon creates a minimal daemon suitable for testing ApplyTeamConfig.
func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	dir := t.TempDir()
	port := freePort(t)
	d, err := New(Config{MCPPort: port, DataDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { d.Stop() })
	return d
}

func TestApplyTeamConfig_AutonomousMode_AutoClaim(t *testing.T) {
	d := newTestDaemon(t)

	repo := t.TempDir()
	d.ApplyTeamConfig(&config.TeamConfig{
		Name:         "Test",
		Repo:         repo,
		Coordination: "autonomous",
		Agents:       []config.AgentConfig{{Name: "Bot", AgentType: "claude"}},
	})

	// Register an agent.
	agentID := uuid.New()
	d.Manager.AddAgent(&models.Agent{ID: agentID, Name: "Bot", AgentType: models.AgentTypeClaude, Folder: "/tmp"}, nil)
	d.Coordinator.RegisterAgent(agentID, "Bot", "/tmp", "")

	// Create a task.
	task, err := d.Coordinator.CreateTask(agentID, "Do stuff", "Description", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Fire OnAgentIdle — should auto-claim.
	d.Coordinator.OnAgentIdle(agentID)

	// Verify task was claimed.
	claimed, err := d.Coordinator.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if claimed.Status != models.TaskStatusInProgress {
		t.Errorf("expected status in_progress, got %s", claimed.Status)
	}
	if claimed.AssigneeID == nil || *claimed.AssigneeID != agentID {
		t.Errorf("expected assignee %s, got %v", agentID, claimed.AssigneeID)
	}
}

func TestApplyTeamConfig_ManagedMode_NoAutoClaim(t *testing.T) {
	d := newTestDaemon(t)

	repo := t.TempDir()
	d.ApplyTeamConfig(&config.TeamConfig{
		Name:         "Test",
		Repo:         repo,
		Coordination: "managed",
		Agents:       []config.AgentConfig{{Name: "Bot", AgentType: "claude"}},
	})

	agentID := uuid.New()
	d.Manager.AddAgent(&models.Agent{ID: agentID, Name: "Bot", AgentType: models.AgentTypeClaude, Folder: "/tmp"}, nil)
	d.Coordinator.RegisterAgent(agentID, "Bot", "/tmp", "")

	task, err := d.Coordinator.CreateTask(agentID, "Do stuff", "Description", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Fire OnAgentIdle — should NOT auto-claim in managed mode.
	d.Coordinator.OnAgentIdle(agentID)

	unclaimed, err := d.Coordinator.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if unclaimed.Status != models.TaskStatusPending {
		t.Errorf("expected status pending, got %s", unclaimed.Status)
	}
	if unclaimed.AssigneeID != nil {
		t.Errorf("expected no assignee in managed mode, got %v", unclaimed.AssigneeID)
	}
}

func TestApplyTeamConfig_AutonomousMode_NoPendingTasks(t *testing.T) {
	d := newTestDaemon(t)

	repo := t.TempDir()
	d.ApplyTeamConfig(&config.TeamConfig{
		Name:         "Test",
		Repo:         repo,
		Coordination: "autonomous",
		Agents:       []config.AgentConfig{{Name: "Bot", AgentType: "claude"}},
	})

	agentID := uuid.New()
	d.Manager.AddAgent(&models.Agent{ID: agentID, Name: "Bot", AgentType: models.AgentTypeClaude, Folder: "/tmp"}, nil)
	d.Coordinator.RegisterAgent(agentID, "Bot", "/tmp", "")

	// No tasks — OnAgentIdle should not panic.
	d.Coordinator.OnAgentIdle(agentID)
}

func TestApplyTeamConfig_AutonomousMode_SkipsBlockedTasks(t *testing.T) {
	d := newTestDaemon(t)

	repo := t.TempDir()
	d.ApplyTeamConfig(&config.TeamConfig{
		Name:         "Test",
		Repo:         repo,
		Coordination: "autonomous",
		Agents:       []config.AgentConfig{{Name: "Bot", AgentType: "claude"}},
	})

	agentID := uuid.New()
	d.Manager.AddAgent(&models.Agent{ID: agentID, Name: "Bot", AgentType: models.AgentTypeClaude, Folder: "/tmp"}, nil)
	d.Coordinator.RegisterAgent(agentID, "Bot", "/tmp", "")

	// Create a pending dep task, then a blocked task depending on it.
	dep, _ := d.Coordinator.CreateTask(agentID, "Dep", "Dep task", nil)
	blocked, _ := d.Coordinator.CreateTask(agentID, "Blocked", "Blocked task", []uuid.UUID{dep.ID})

	// Verify blocked status.
	bt, _ := d.Coordinator.GetTask(blocked.ID)
	if bt.Status != models.TaskStatusBlocked {
		t.Fatalf("expected blocked, got %s", bt.Status)
	}

	// Fire OnAgentIdle — should claim the dep (pending), not the blocked task.
	d.Coordinator.OnAgentIdle(agentID)

	depTask, _ := d.Coordinator.GetTask(dep.ID)
	blockedTask, _ := d.Coordinator.GetTask(blocked.ID)

	if depTask.Status != models.TaskStatusInProgress {
		t.Errorf("dep task should be in_progress, got %s", depTask.Status)
	}
	if blockedTask.Status != models.TaskStatusBlocked {
		t.Errorf("blocked task should still be blocked, got %s", blockedTask.Status)
	}
}
