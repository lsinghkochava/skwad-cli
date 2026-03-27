package terminal

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/agent"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
)

// newTestPool creates a Pool backed by a temp store with the given mcpURL.
func newTestPool(t *testing.T, mcpURL string) (*Pool, *agent.Manager) {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	mgr, err := agent.NewManager(store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	coord := agent.NewCoordinator(mgr)
	pool := NewPool(mgr, coord, mcpURL, "")
	return pool, mgr
}

func TestPool_Spawn_SetsSkwadURL(t *testing.T) {
	pool, mgr := newTestPool(t, "http://127.0.0.1:8766/mcp")

	agentID := uuid.New()
	a := &models.Agent{
		ID:        agentID,
		Name:      "test-agent",
		Folder:    t.TempDir(),
		AgentType: models.AgentTypeCustom1, // custom falls back to $SHELL
		Metadata:  make(map[string]string),
	}
	mgr.AddAgent(a, nil)

	var (
		mu  sync.Mutex
		out strings.Builder
	)

	// We can't intercept the session callbacks set by spawnNow directly.
	// Instead, use the OutputSubscriber hook on the Pool.
	pool.OutputSubscriber = func(id uuid.UUID, name string, data []byte) {
		mu.Lock()
		out.Write(data)
		mu.Unlock()
	}

	pool.Spawn(a)
	defer pool.StopAll()

	// Give it a moment to start, then inject a printenv command.
	time.Sleep(500 * time.Millisecond)
	pool.InjectText(agentID, "echo SKWAD_URL_VAL=$SKWAD_URL")

	wantMarker := "SKWAD_URL_VAL=http://127.0.0.1:8766"
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			mu.Lock()
			got := out.String()
			mu.Unlock()
			t.Fatalf("timed out waiting for SKWAD_URL in output, got: %q", got)
		case <-time.After(100 * time.Millisecond):
			mu.Lock()
			got := out.String()
			mu.Unlock()
			if strings.Contains(got, wantMarker) {
				// Verify /mcp suffix was stripped.
				if strings.Contains(got, "SKWAD_URL_VAL=http://127.0.0.1:8766/mcp") {
					t.Fatal("/mcp suffix was not stripped from SKWAD_URL")
				}
				return
			}
		}
	}
}

func TestPool_Spawn_SetsSkwadURL_NonDefaultPort(t *testing.T) {
	pool, mgr := newTestPool(t, "http://127.0.0.1:9999/mcp")

	agentID := uuid.New()
	a := &models.Agent{
		ID:        agentID,
		Name:      "test-port",
		Folder:    t.TempDir(),
		AgentType: models.AgentTypeCustom1,
		Metadata:  make(map[string]string),
	}
	mgr.AddAgent(a, nil)

	var (
		mu  sync.Mutex
		out strings.Builder
	)
	pool.OutputSubscriber = func(id uuid.UUID, name string, data []byte) {
		mu.Lock()
		out.Write(data)
		mu.Unlock()
	}

	pool.Spawn(a)
	defer pool.StopAll()

	time.Sleep(500 * time.Millisecond)
	pool.InjectText(agentID, "echo SKWAD_URL_VAL=$SKWAD_URL")

	wantMarker := "SKWAD_URL_VAL=http://127.0.0.1:9999"
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			mu.Lock()
			got := out.String()
			mu.Unlock()
			t.Fatalf("timed out waiting for SKWAD_URL in output, got: %q", got)
		case <-time.After(100 * time.Millisecond):
			mu.Lock()
			got := out.String()
			mu.Unlock()
			if strings.Contains(got, wantMarker) {
				return
			}
		}
	}
}

func TestPool_Spawn_EmptyMCPURL(t *testing.T) {
	pool, mgr := newTestPool(t, "")

	agentID := uuid.New()
	a := &models.Agent{
		ID:        agentID,
		Name:      "test-empty",
		Folder:    t.TempDir(),
		AgentType: models.AgentTypeCustom1,
		Metadata:  make(map[string]string),
	}
	mgr.AddAgent(a, nil)

	var (
		mu  sync.Mutex
		out strings.Builder
	)
	pool.OutputSubscriber = func(id uuid.UUID, name string, data []byte) {
		mu.Lock()
		out.Write(data)
		mu.Unlock()
	}

	pool.Spawn(a)
	defer pool.StopAll()

	// With empty mcpURL, SKWAD_URL should be set to empty string.
	// Use a unique marker so we can detect when the echo has completed.
	time.Sleep(500 * time.Millisecond)
	pool.InjectText(agentID, "echo SKWAD_URL_EMPTY_CHECK=$SKWAD_URL:END")

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			mu.Lock()
			got := out.String()
			mu.Unlock()
			t.Fatalf("timed out waiting for output, got: %q", got)
		case <-time.After(100 * time.Millisecond):
			mu.Lock()
			got := out.String()
			mu.Unlock()
			// With empty SKWAD_URL, the output should be "SKWAD_URL_EMPTY_CHECK=:END"
			if strings.Contains(got, "SKWAD_URL_EMPTY_CHECK=:END") {
				return
			}
		}
	}
}

func TestPool_Spawn_PreservesSkwadAgentID(t *testing.T) {
	pool, mgr := newTestPool(t, "http://127.0.0.1:8766/mcp")

	agentID := uuid.New()
	a := &models.Agent{
		ID:        agentID,
		Name:      "test-agentid",
		Folder:    t.TempDir(),
		AgentType: models.AgentTypeCustom1,
		Metadata:  make(map[string]string),
	}
	mgr.AddAgent(a, nil)

	var (
		mu  sync.Mutex
		out strings.Builder
	)
	pool.OutputSubscriber = func(id uuid.UUID, name string, data []byte) {
		mu.Lock()
		out.Write(data)
		mu.Unlock()
	}

	pool.Spawn(a)
	defer pool.StopAll()

	time.Sleep(500 * time.Millisecond)
	pool.InjectText(agentID, "echo AGENT_ID_VAL=$SKWAD_AGENT_ID")

	wantMarker := "AGENT_ID_VAL=" + agentID.String()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			mu.Lock()
			got := out.String()
			mu.Unlock()
			t.Fatalf("timed out waiting for SKWAD_AGENT_ID in output, got: %q", got)
		case <-time.After(100 * time.Millisecond):
			mu.Lock()
			got := out.String()
			mu.Unlock()
			if strings.Contains(got, wantMarker) {
				return
			}
		}
	}
}

func TestPool_Spawn_NonClaude_GetsDelayedRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow registration delay test in short mode")
	}

	pool, mgr := newTestPool(t, "http://127.0.0.1:8766/mcp")

	agentID := uuid.New()
	a := &models.Agent{
		ID:        agentID,
		Name:      "test-registration",
		Folder:    t.TempDir(),
		AgentType: models.AgentTypeCustom1, // non-Claude → gets delayed registration
		Metadata:  make(map[string]string),
	}
	mgr.AddAgent(a, nil)

	var (
		mu  sync.Mutex
		out strings.Builder
	)
	pool.OutputSubscriber = func(id uuid.UUID, name string, data []byte) {
		mu.Lock()
		out.Write(data)
		mu.Unlock()
	}

	pool.Spawn(a)
	defer pool.StopAll()

	// Registration happens after registrationDelay (3s). Wait up to 6s.
	deadline := time.After(6 * time.Second)
	for {
		select {
		case <-deadline:
			mu.Lock()
			got := out.String()
			mu.Unlock()
			t.Fatalf("non-Claude agent did not receive registration prompt within 6s, got: %q", got)
		case <-time.After(200 * time.Millisecond):
			mu.Lock()
			got := out.String()
			mu.Unlock()
			// RegistrationPrompt contains "register-agent tool"
			if strings.Contains(got, "register-agent") {
				return
			}
		}
	}
}

func TestPool_Spawn_Claude_SkipsDelayedRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow registration delay test in short mode")
	}

	pool, mgr := newTestPool(t, "http://127.0.0.1:8766/mcp")

	agentID := uuid.New()
	a := &models.Agent{
		ID:        agentID,
		Name:      "test-claude-skip",
		Folder:    t.TempDir(),
		AgentType: models.AgentTypeClaude, // Claude → skips delayed registration
		Metadata:  make(map[string]string),
	}
	mgr.AddAgent(a, nil)

	var (
		mu  sync.Mutex
		out strings.Builder
	)
	pool.OutputSubscriber = func(id uuid.UUID, name string, data []byte) {
		mu.Lock()
		out.Write(data)
		mu.Unlock()
	}

	pool.Spawn(a)
	defer pool.StopAll()

	// Wait beyond registrationDelay (3s) + buffer.
	time.Sleep(4 * time.Second)

	mu.Lock()
	got := out.String()
	mu.Unlock()

	// Claude should NOT have received the delayed registration prompt.
	if strings.Contains(got, "register-agent tool") {
		t.Errorf("Claude agent should NOT receive delayed registration prompt, but got: %q", got)
	}
}

func TestTrimSuffix_MCPURLVariants(t *testing.T) {
	tests := []struct {
		name    string
		mcpURL  string
		wantURL string
	}{
		{
			name:    "standard mcp suffix",
			mcpURL:  "http://127.0.0.1:8766/mcp",
			wantURL: "http://127.0.0.1:8766",
		},
		{
			name:    "non-default port",
			mcpURL:  "http://127.0.0.1:9999/mcp",
			wantURL: "http://127.0.0.1:9999",
		},
		{
			name:    "no mcp suffix",
			mcpURL:  "http://127.0.0.1:8766",
			wantURL: "http://127.0.0.1:8766",
		},
		{
			name:    "empty string",
			mcpURL:  "",
			wantURL: "",
		},
		{
			name:    "trailing slash mcp",
			mcpURL:  "http://127.0.0.1:8766/mcp/",
			wantURL: "http://127.0.0.1:8766/mcp/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.TrimSuffix(tt.mcpURL, "/mcp")
			if got != tt.wantURL {
				t.Errorf("TrimSuffix(%q, \"/mcp\") = %q, want %q", tt.mcpURL, got, tt.wantURL)
			}
		})
	}
}
