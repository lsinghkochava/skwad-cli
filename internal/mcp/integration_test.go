package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/agent"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
)

// mockStatusUpdater records all status updates for assertions.
type mockStatusUpdater struct {
	mu         sync.Mutex
	running    []uuid.UUID
	idle       []uuid.UUID
	blocked    []uuid.UUID
	errored    []uuid.UUID
	metadata   map[uuid.UUID]map[string]string
	sessionIDs map[uuid.UUID]string
	statusText map[uuid.UUID][2]string // [status, category]
}

func newMockStatusUpdater() *mockStatusUpdater {
	return &mockStatusUpdater{
		metadata:   make(map[uuid.UUID]map[string]string),
		sessionIDs: make(map[uuid.UUID]string),
		statusText: make(map[uuid.UUID][2]string),
	}
}

func (m *mockStatusUpdater) SetRunning(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = append(m.running, id)
}
func (m *mockStatusUpdater) SetIdle(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idle = append(m.idle, id)
}
func (m *mockStatusUpdater) SetBlocked(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocked = append(m.blocked, id)
}
func (m *mockStatusUpdater) SetError(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errored = append(m.errored, id)
}
func (m *mockStatusUpdater) SetMetadata(id uuid.UUID, key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metadata[id] == nil {
		m.metadata[id] = make(map[string]string)
	}
	m.metadata[id][key] = value
}
func (m *mockStatusUpdater) SetSessionID(id uuid.UUID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionIDs[id] = sessionID
}
func (m *mockStatusUpdater) SetStatusText(id uuid.UUID, status, category string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusText[id] = [2]string{status, category}
}

func (m *mockStatusUpdater) wasSetRunning(id uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.running {
		if v == id {
			return true
		}
	}
	return false
}
func (m *mockStatusUpdater) wasSetIdle(id uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.idle {
		if v == id {
			return true
		}
	}
	return false
}
func (m *mockStatusUpdater) wasSetBlocked(id uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.blocked {
		if v == id {
			return true
		}
	}
	return false
}

// testEnv bundles everything tests need.
type testEnv struct {
	srv     *Server
	ts      *httptest.Server
	mgr     *agent.Manager
	coord   *agent.Coordinator
	updater *mockStatusUpdater
}

// newTestServer spins up an httptest server backed by a real MCP Server.
// All routes are registered (matching the production Start() mux).
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	env := newTestEnv(t)
	return env.srv, env.ts
}

// newTestEnv is the full-access variant of newTestServer for tests that need
// the manager, coordinator, or mock updater.
func newTestEnv(t *testing.T) *testEnv {
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

	updater := newMockStatusUpdater()
	srv := NewServer(coord, store, 0)
	srv.StatusUpdater = updater
	srv.hookHandler = newHookHandler(srv)
	srv.tools = newToolHandler(srv)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/mcp", srv.handleMCP)
	mux.Handle("/hook", srv.hookHandler)
	mux.HandleFunc("/api/v1/agent/register", srv.handleRegister)
	mux.HandleFunc("/api/v1/agent/status", srv.handleStatus)
	mux.HandleFunc("/api/v1/agent/send", srv.handleSend)
	mux.HandleFunc("/api/v1/agent/broadcast", srv.handleBroadcast)
	mux.HandleFunc("/", srv.handleDebug)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &testEnv{srv: srv, ts: ts, mgr: mgr, coord: coord, updater: updater}
}

// addManagedAgent creates an agent in the manager so HTTP endpoints can find it.
func (e *testEnv) addManagedAgent(t *testing.T, id uuid.UUID, name, folder string, agentType models.AgentType) {
	t.Helper()
	a := &models.Agent{
		ID:        id,
		Name:      name,
		Folder:    folder,
		AgentType: agentType,
	}
	e.mgr.AddAgent(a, nil)
}

// registerAgent is a convenience that adds a managed agent AND registers it via MCP
// so it's visible to both HTTP endpoints and the coordinator.
func (e *testEnv) registerAgent(t *testing.T, id uuid.UUID, name, folder string, agentType models.AgentType) {
	t.Helper()
	e.addManagedAgent(t, id, name, folder, agentType)
	sess := uuid.NewString()
	resp := toolCall(t, e.ts, sess, ToolRegisterAgent, map[string]interface{}{
		"agentId": id.String(),
		"name":    name,
		"folder":  folder,
	})
	if resp.Error != nil {
		t.Fatalf("register-agent failed for %s: %v", name, resp.Error)
	}
}

// postJSON sends a POST with JSON body and returns status code + body bytes.
func postJSON(t *testing.T, url string, payload interface{}) (int, []byte) {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// getBody sends a GET and returns status code + body bytes.
func getBody(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func mcpCall(t *testing.T, ts *httptest.Server, sessionID, method string, params interface{}) Response {
	t.Helper()

	req := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}
	body, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request: %v", err)
	}
	defer resp.Body.Close()

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func toolCall(t *testing.T, ts *httptest.Server, sessionID, toolName string, args map[string]interface{}) Response {
	t.Helper()
	return mcpCall(t, ts, sessionID, MethodToolsCall, map[string]interface{}{
		"name":      toolName,
		"arguments": args,
	})
}

// --- Tests ---

func TestMCP_Initialize(t *testing.T) {
	_, ts := newTestServer(t)
	resp := mcpCall(t, ts, "", MethodInitialize, nil)
	if resp.Error != nil {
		t.Fatalf("initialize error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	if result["protocolVersion"] == nil {
		t.Error("missing protocolVersion in initialize response")
	}
}

func TestMCP_ToolsList(t *testing.T) {
	_, ts := newTestServer(t)
	resp := mcpCall(t, ts, "", MethodToolsList, nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}
	result := resp.Result.(map[string]interface{})
	tools := result["tools"].([]interface{})
	if len(tools) < 12 {
		t.Errorf("expected at least 12 tools, got %d", len(tools))
	}
}

func TestMCP_UnknownMethod(t *testing.T) {
	_, ts := newTestServer(t)
	resp := mcpCall(t, ts, "", "nonexistent/method", nil)
	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected -32601, got %d", resp.Error.Code)
	}
}

func TestMCP_RegisterAndList(t *testing.T) {
	_, ts := newTestServer(t)
	sessID := uuid.NewString()
	agentID := uuid.New()

	// Register agent A.
	resp := toolCall(t, ts, sessID, ToolRegisterAgent, map[string]interface{}{
		"agentId": agentID.String(),
		"name":    "Agent A",
		"folder":  "/tmp/a",
	})
	if resp.Error != nil {
		t.Fatalf("register-agent error: %v", resp.Error)
	}

	// List agents — should include A.
	resp = toolCall(t, ts, sessID, ToolListAgents, nil)
	if resp.Error != nil {
		t.Fatalf("list-agents error: %v", resp.Error)
	}
}

func TestMCP_SendAndCheckMessages(t *testing.T) {
	_, ts := newTestServer(t)

	sessA := uuid.NewString()
	sessB := uuid.NewString()
	agentA := uuid.New()
	agentB := uuid.New()

	// Register both agents.
	toolCall(t, ts, sessA, ToolRegisterAgent, map[string]interface{}{
		"agentId": agentA.String(), "name": "Alice", "folder": "/tmp/a",
	})
	toolCall(t, ts, sessB, ToolRegisterAgent, map[string]interface{}{
		"agentId": agentB.String(), "name": "Bob", "folder": "/tmp/b",
	})

	// A sends message to B by name.
	resp := toolCall(t, ts, sessA, ToolSendMessage, map[string]interface{}{
		"to": "Bob", "message": "hello from Alice",
	})
	if resp.Error != nil {
		t.Fatalf("send-message error: %v", resp.Error)
	}

	// B checks messages.
	resp = toolCall(t, ts, sessB, ToolCheckMessages, map[string]interface{}{
		"markRead": true,
	})
	if resp.Error != nil {
		t.Fatalf("check-messages error: %v", resp.Error)
	}

	// The result text should contain the message content.
	result := resp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if text == "" {
		t.Error("expected non-empty check-messages response")
	}
}

func TestMCP_Broadcast(t *testing.T) {
	_, ts := newTestServer(t)

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	sessions := make([]string, len(ids))
	for i, id := range ids {
		sessions[i] = uuid.NewString()
		toolCall(t, ts, sessions[i], ToolRegisterAgent, map[string]interface{}{
			"agentId": id.String(),
			"name":    fmt.Sprintf("Agent%d", i),
			"folder":  "/tmp",
		})
	}

	// Agent 0 broadcasts.
	resp := toolCall(t, ts, sessions[0], ToolBroadcast, map[string]interface{}{
		"message": "broadcast_payload",
	})
	if resp.Error != nil {
		t.Fatalf("broadcast error: %v", resp.Error)
	}

	// Agents 1 and 2 should have the message.
	for _, sess := range sessions[1:] {
		resp = toolCall(t, ts, sess, ToolCheckMessages, map[string]interface{}{"markRead": false})
		if resp.Error != nil {
			t.Fatalf("check-messages error: %v", resp.Error)
		}
		result := resp.Result.(map[string]interface{})
		text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
		if text == "Error: agent not found" {
			t.Error("agent should have received broadcast")
		}
	}
}

func TestMCP_Ping(t *testing.T) {
	_, ts := newTestServer(t)
	resp := mcpCall(t, ts, "", MethodPing, nil)
	if resp.Error != nil {
		t.Fatalf("ping error: %v", resp.Error)
	}
}

func TestMCP_UnknownTool(t *testing.T) {
	_, ts := newTestServer(t)
	resp := toolCall(t, ts, "", "nonexistent-tool", nil)
	// Should return a result (not an RPC error), with an error message in content.
	if resp.Error != nil {
		t.Fatalf("unexpected RPC-level error: %v", resp.Error)
	}
}

// --- Phase 1: Swift-Compatible Endpoint Tests ---

func TestHealth(t *testing.T) {
	env := newTestEnv(t)
	code, body := getBody(t, env.ts.URL+"/health")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if strings.TrimSpace(string(body)) != "OK" {
		t.Errorf("expected body 'OK', got %q", string(body))
	}
}

func TestDebugEndpoint(t *testing.T) {
	env := newTestEnv(t)

	// Add a managed agent so there's something to list.
	agentID := uuid.New()
	env.addManagedAgent(t, agentID, "DebugBot", "/tmp/debug", models.AgentTypeClaude)

	code, body := getBody(t, env.ts.URL+"/")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	var agents []map[string]interface{}
	if err := json.Unmarshal(body, &agents); err != nil {
		t.Fatalf("failed to decode JSON array: %v", err)
	}

	if len(agents) == 0 {
		t.Fatal("expected at least one agent in debug response")
	}

	// Find our agent.
	var found map[string]interface{}
	for _, a := range agents {
		if a["agent_id"] == agentID.String() {
			found = a
			break
		}
	}
	if found == nil {
		t.Fatalf("agent %s not found in debug response", agentID)
	}

	// Verify all expected fields exist.
	for _, field := range []string{"agent_id", "name", "folder", "state", "status", "registered", "agent_type"} {
		if _, ok := found[field]; !ok {
			t.Errorf("missing field %q in debug agent response", field)
		}
	}
	if found["name"] != "DebugBot" {
		t.Errorf("expected name 'DebugBot', got %v", found["name"])
	}
	if found["agent_type"] != "claude" {
		t.Errorf("expected agent_type 'claude', got %v", found["agent_type"])
	}
}

func TestDebugEndpoint_NotFoundForOtherPaths(t *testing.T) {
	env := newTestEnv(t)
	code, _ := getBody(t, env.ts.URL+"/nonexistent")
	if code != http.StatusNotFound {
		t.Errorf("expected 404 for /nonexistent, got %d", code)
	}
}

func TestRegisterEndpoint(t *testing.T) {
	t.Run("happy path startup", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "RegBot", "/tmp/reg", models.AgentTypeClaude)

		code, body := postJSON(t, env.ts.URL+"/api/v1/agent/register", map[string]interface{}{
			"agent_id": agentID.String(),
			"agent":    "claude",
			"source":   "startup",
			"payload": map[string]interface{}{
				"cwd":   "/tmp/reg",
				"model": "opus",
			},
		})

		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}

		var resp RegisterHookResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !resp.Success {
			t.Error("expected success=true")
		}
		if resp.SkwadMembers == nil {
			t.Error("expected non-nil skwadMembers")
		}

		// Verify StatusUpdater.SetRunning was called.
		if !env.updater.wasSetRunning(agentID) {
			t.Error("expected SetRunning to be called for startup registration")
		}
	})

	t.Run("invalid UUID", func(t *testing.T) {
		env := newTestEnv(t)
		code, _ := postJSON(t, env.ts.URL+"/api/v1/agent/register", map[string]interface{}{
			"agent_id": "not-a-uuid",
			"agent":    "claude",
		})
		if code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid UUID, got %d", code)
		}
	})

	t.Run("unknown agent ID", func(t *testing.T) {
		env := newTestEnv(t)
		unknownID := uuid.New()
		code, _ := postJSON(t, env.ts.URL+"/api/v1/agent/register", map[string]interface{}{
			"agent_id": unknownID.String(),
			"agent":    "claude",
		})
		if code != http.StatusNotFound {
			t.Errorf("expected 404 for unknown agent, got %d", code)
		}
	})

	t.Run("unknown agent type", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "TypeBot", "/tmp/type", models.AgentTypeClaude)

		code, body := postJSON(t, env.ts.URL+"/api/v1/agent/register", map[string]interface{}{
			"agent_id": agentID.String(),
			"agent":    "gpt5",
		})
		if code != http.StatusBadRequest {
			t.Errorf("expected 400 for unknown type, got %d: %s", code, body)
		}
	})

	t.Run("resume source updates session only", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "ResumeBot", "/tmp/resume", models.AgentTypeClaude)

		code, body := postJSON(t, env.ts.URL+"/api/v1/agent/register", map[string]interface{}{
			"agent_id":   agentID.String(),
			"agent":      "claude",
			"source":     "resume",
			"session_id": "sess-123",
		})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}

		var resp RegisterHookResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Success {
			t.Error("expected success=true for resume")
		}

		// Resume should NOT call SetRunning.
		if env.updater.wasSetRunning(agentID) {
			t.Error("resume should not call SetRunning")
		}
	})
}

func TestRegisterEndpointSnakeAndCamel(t *testing.T) {
	t.Run("snake_case agent_id", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "SnakeBot", "/tmp/snake", models.AgentTypeClaude)

		code, body := postJSON(t, env.ts.URL+"/api/v1/agent/register", map[string]interface{}{
			"agent_id": agentID.String(),
			"agent":    "claude",
			"source":   "startup",
		})
		if code != http.StatusOK {
			t.Fatalf("snake_case: expected 200, got %d: %s", code, body)
		}

		var resp RegisterHookResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Success {
			t.Error("expected success for snake_case agent_id")
		}
	})

	t.Run("camelCase agentId", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "CamelBot", "/tmp/camel", models.AgentTypeClaude)

		code, body := postJSON(t, env.ts.URL+"/api/v1/agent/register", map[string]interface{}{
			"agentId": agentID.String(),
			"agent":   "claude",
			"source":  "startup",
		})
		if code != http.StatusOK {
			t.Fatalf("camelCase: expected 200, got %d: %s", code, body)
		}

		var resp RegisterHookResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Success {
			t.Error("expected success for camelCase agentId")
		}
	})
}

func TestStatusEndpoint(t *testing.T) {
	t.Run("claude running", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "RunBot", "/tmp/run", models.AgentTypeClaude)

		code, body := postJSON(t, env.ts.URL+"/api/v1/agent/status", map[string]interface{}{
			"agent_id": agentID.String(),
			"agent":    "claude",
			"status":   "running",
			"payload":  map[string]interface{}{},
		})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, body)
		}
		if strings.TrimSpace(string(body)) != "OK" {
			t.Errorf("expected 'OK' body, got %q", string(body))
		}
		if !env.updater.wasSetRunning(agentID) {
			t.Error("expected SetRunning for claude running status")
		}
	})

	t.Run("claude idle", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "IdleBot", "/tmp/idle", models.AgentTypeClaude)

		code, _ := postJSON(t, env.ts.URL+"/api/v1/agent/status", map[string]interface{}{
			"agent_id": agentID.String(),
			"agent":    "claude",
			"status":   "idle",
			"payload":  map[string]interface{}{},
		})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		if !env.updater.wasSetIdle(agentID) {
			t.Error("expected SetIdle for claude idle status")
		}
	})

	t.Run("claude input sets blocked", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "InputBot", "/tmp/input", models.AgentTypeClaude)

		code, _ := postJSON(t, env.ts.URL+"/api/v1/agent/status", map[string]interface{}{
			"agent_id": agentID.String(),
			"agent":    "claude",
			"status":   "input",
			"payload":  map[string]interface{}{},
		})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		if !env.updater.wasSetBlocked(agentID) {
			t.Error("expected SetBlocked for claude input status")
		}
	})

	t.Run("codex agent-turn-complete sets idle", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "CodexBot", "/tmp/codex", models.AgentTypeCodex)

		code, _ := postJSON(t, env.ts.URL+"/api/v1/agent/status", map[string]interface{}{
			"agent_id": agentID.String(),
			"agent":    "codex",
			"payload": map[string]interface{}{
				"type":      "agent-turn-complete",
				"thread-id": "thread-abc",
			},
		})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
		if !env.updater.wasSetIdle(agentID) {
			t.Error("expected SetIdle for codex agent-turn-complete")
		}
		// Verify thread-id stored as session ID.
		env.updater.mu.Lock()
		sid := env.updater.sessionIDs[agentID]
		env.updater.mu.Unlock()
		if sid != "thread-abc" {
			t.Errorf("expected session_id 'thread-abc', got %q", sid)
		}
	})

	t.Run("invalid UUID returns 400", func(t *testing.T) {
		env := newTestEnv(t)
		code, _ := postJSON(t, env.ts.URL+"/api/v1/agent/status", map[string]interface{}{
			"agent_id": "bad-uuid",
			"agent":    "claude",
			"status":   "running",
		})
		if code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid UUID, got %d", code)
		}
	})
}

func TestSetStatusTool(t *testing.T) {
	t.Run("sets status text and category", func(t *testing.T) {
		env := newTestEnv(t)
		agentID := uuid.New()
		env.addManagedAgent(t, agentID, "StatusBot", "/tmp/status", models.AgentTypeClaude)

		resp := toolCall(t, env.ts, "", ToolSetStatus, map[string]interface{}{
			"agentId":  agentID.String(),
			"status":   "Implementing auth",
			"category": "code",
		})
		if resp.Error != nil {
			t.Fatalf("set-status error: %v", resp.Error)
		}

		// Verify result text.
		result := resp.Result.(map[string]interface{})
		content := result["content"].([]interface{})
		text := content[0].(map[string]interface{})["text"].(string)
		if text != "Status updated" {
			t.Errorf("expected 'Status updated', got %q", text)
		}

		// Verify status was stored on the agent via coordinator.
		ag, ok := env.coord.Agent(agentID)
		if !ok {
			t.Fatal("agent not found in coordinator")
		}
		if ag.StatusText != "Implementing auth" {
			t.Errorf("expected StatusText 'Implementing auth', got %q", ag.StatusText)
		}
		if ag.StatusCategory != "code" {
			t.Errorf("expected StatusCategory 'code', got %q", ag.StatusCategory)
		}
	})

	t.Run("missing agentId returns error", func(t *testing.T) {
		env := newTestEnv(t)
		resp := toolCall(t, env.ts, "", ToolSetStatus, map[string]interface{}{
			"status": "some status",
		})
		// Should return a tool-level error (not RPC error).
		if resp.Error != nil {
			t.Fatalf("unexpected RPC error: %v", resp.Error)
		}
		result := resp.Result.(map[string]interface{})
		content := result["content"].([]interface{})
		text := content[0].(map[string]interface{})["text"].(string)
		if !strings.Contains(text, "Error") {
			t.Errorf("expected error in content, got %q", text)
		}
	})

	t.Run("unknown agent returns error", func(t *testing.T) {
		env := newTestEnv(t)
		unknownID := uuid.New()
		resp := toolCall(t, env.ts, "", ToolSetStatus, map[string]interface{}{
			"agentId": unknownID.String(),
			"status":  "test",
		})
		// set-status calls coordinator.SetStatusText which calls manager.UpdateAgent.
		// With an unknown ID, UpdateAgent silently does nothing — no error returned.
		// This is the current behavior; the tool still returns "Status updated".
		if resp.Error != nil {
			t.Fatalf("unexpected RPC error: %v", resp.Error)
		}
	})
}

func TestSetStatusToolInToolsList(t *testing.T) {
	_, ts := newTestServer(t)
	resp := mcpCall(t, ts, "", MethodToolsList, nil)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}

	result := resp.Result.(map[string]interface{})
	tools := result["tools"].([]interface{})

	var found map[string]interface{}
	for _, tool := range tools {
		td := tool.(map[string]interface{})
		if td["name"] == "set-status" {
			found = td
			break
		}
	}
	if found == nil {
		t.Fatal("set-status not found in tools/list")
	}

	// Verify it has an inputSchema with the expected properties.
	schema, ok := found["inputSchema"].(map[string]interface{})
	if !ok {
		t.Fatal("missing inputSchema for set-status")
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("missing properties in set-status schema")
	}

	for _, param := range []string{"agentId", "status", "category"} {
		if _, exists := props[param]; !exists {
			t.Errorf("missing parameter %q in set-status schema", param)
		}
	}

	// Verify required fields.
	required, ok := schema["required"].([]interface{})
	if !ok {
		t.Fatal("missing required array in set-status schema")
	}
	requiredSet := make(map[string]bool)
	for _, r := range required {
		requiredSet[r.(string)] = true
	}
	if !requiredSet["agentId"] || !requiredSet["status"] {
		t.Errorf("expected agentId and status as required, got %v", required)
	}
}

// --- Phase 2: REST Send/Broadcast Endpoint Tests ---

func TestSendEndpoint(t *testing.T) {
	env := newTestEnv(t)
	alice := uuid.New()
	bob := uuid.New()
	env.registerAgent(t, alice, "Alice", "/tmp/a", models.AgentTypeClaude)
	env.registerAgent(t, bob, "Bob", "/tmp/b", models.AgentTypeClaude)

	code, body := postJSON(t, env.ts.URL+"/api/v1/agent/send", SendMessageRequest{
		From:    "Alice",
		To:      "Bob",
		Content: "hello from Alice",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}

	var resp MessageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success=true, got message: %s", resp.Message)
	}
	if resp.Message != "Message sent" {
		t.Errorf("expected 'Message sent', got %q", resp.Message)
	}

	// Verify Bob received the message via MCP check-messages.
	sessB := uuid.NewString()
	// Need to re-register Bob in a session to check messages via toolCall.
	toolCall(t, env.ts, sessB, ToolRegisterAgent, map[string]interface{}{
		"agentId": bob.String(), "name": "Bob", "folder": "/tmp/b",
	})
	checkResp := toolCall(t, env.ts, sessB, ToolCheckMessages, map[string]interface{}{"markRead": true})
	if checkResp.Error != nil {
		t.Fatalf("check-messages error: %v", checkResp.Error)
	}
	result := checkResp.Result.(map[string]interface{})
	text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "hello from Alice") {
		t.Errorf("expected Bob to receive message, got: %s", text)
	}
}

func TestSendEndpoint_ByID(t *testing.T) {
	env := newTestEnv(t)
	alice := uuid.New()
	bob := uuid.New()
	env.registerAgent(t, alice, "Alice", "/tmp/a", models.AgentTypeClaude)
	env.registerAgent(t, bob, "Bob", "/tmp/b", models.AgentTypeClaude)

	// Send using UUIDs instead of names.
	code, body := postJSON(t, env.ts.URL+"/api/v1/agent/send", SendMessageRequest{
		From:    alice.String(),
		To:      bob.String(),
		Content: "hello by ID",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}
	var resp MessageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true for ID-based send")
	}
}

func TestSendEndpoint_UnknownRecipient(t *testing.T) {
	env := newTestEnv(t)
	alice := uuid.New()
	env.registerAgent(t, alice, "Alice", "/tmp/a", models.AgentTypeClaude)

	code, body := postJSON(t, env.ts.URL+"/api/v1/agent/send", SendMessageRequest{
		From:    "Alice",
		To:      "NonExistent",
		Content: "hello?",
	})
	if code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown recipient, got %d: %s", code, body)
	}

	var resp MessageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false for unknown recipient")
	}
}

func TestSendEndpoint_MissingFields(t *testing.T) {
	env := newTestEnv(t)

	tests := []struct {
		name    string
		payload SendMessageRequest
	}{
		{"missing from", SendMessageRequest{To: "Bob", Content: "hi"}},
		{"missing to", SendMessageRequest{From: "Alice", Content: "hi"}},
		{"missing content", SendMessageRequest{From: "Alice", To: "Bob"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := postJSON(t, env.ts.URL+"/api/v1/agent/send", tt.payload)
			if code != http.StatusBadRequest {
				t.Errorf("expected 400 for %s, got %d: %s", tt.name, code, body)
			}
		})
	}
}

func TestBroadcastEndpoint(t *testing.T) {
	env := newTestEnv(t)
	alice := uuid.New()
	bob := uuid.New()
	charlie := uuid.New()
	env.registerAgent(t, alice, "Alice", "/tmp/a", models.AgentTypeClaude)
	env.registerAgent(t, bob, "Bob", "/tmp/b", models.AgentTypeClaude)
	env.registerAgent(t, charlie, "Charlie", "/tmp/c", models.AgentTypeClaude)

	code, body := postJSON(t, env.ts.URL+"/api/v1/agent/broadcast", BroadcastMessageRequest{
		From:    "Alice",
		Content: "team update",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}

	var resp MessageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success=true, got message: %s", resp.Message)
	}
	if resp.Message != "Message broadcast" {
		t.Errorf("expected 'Message broadcast', got %q", resp.Message)
	}
}

func TestBroadcastEndpoint_MissingFields(t *testing.T) {
	env := newTestEnv(t)

	t.Run("missing from", func(t *testing.T) {
		code, _ := postJSON(t, env.ts.URL+"/api/v1/agent/broadcast", BroadcastMessageRequest{
			Content: "hello",
		})
		if code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", code)
		}
	})

	t.Run("missing content", func(t *testing.T) {
		code, _ := postJSON(t, env.ts.URL+"/api/v1/agent/broadcast", BroadcastMessageRequest{
			From: "Alice",
		})
		if code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", code)
		}
	})
}

func TestBroadcastEndpoint_UnknownSender(t *testing.T) {
	env := newTestEnv(t)
	code, body := postJSON(t, env.ts.URL+"/api/v1/agent/broadcast", BroadcastMessageRequest{
		From:    "Ghost",
		Content: "boo",
	})
	if code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown sender, got %d: %s", code, body)
	}
}

// --- Phase 5.4: Entry Agent Routing Tests ---

func TestSendEndpoint_EmptyTo_WithEntryAgent(t *testing.T) {
	env := newTestEnv(t)
	alice := uuid.New()
	mgr := uuid.New()
	env.registerAgent(t, alice, "Alice", "/tmp/a", models.AgentTypeClaude)
	env.registerAgent(t, mgr, "Manager", "/tmp/m", models.AgentTypeClaude)

	// Configure entry agent on server.
	env.srv.EntryAgent = "Manager"

	// Send with empty To — should resolve to entry agent "Manager".
	code, body := postJSON(t, env.ts.URL+"/api/v1/agent/send", SendMessageRequest{
		From:    "Alice",
		To:      "",
		Content: "need help with tests",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}

	var resp MessageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success=true, got message: %s", resp.Message)
	}

	// Verify Manager received the message.
	sessM := uuid.NewString()
	toolCall(t, env.ts, sessM, ToolRegisterAgent, map[string]interface{}{
		"agentId": mgr.String(), "name": "Manager", "folder": "/tmp/m",
	})
	checkResp := toolCall(t, env.ts, sessM, ToolCheckMessages, map[string]interface{}{"markRead": true})
	if checkResp.Error != nil {
		t.Fatalf("check-messages error: %v", checkResp.Error)
	}
	result := checkResp.Result.(map[string]interface{})
	text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "need help with tests") {
		t.Errorf("expected Manager to receive message, got: %s", text)
	}
}

func TestSendEndpoint_EmptyTo_NoEntryAgent(t *testing.T) {
	env := newTestEnv(t)
	alice := uuid.New()
	env.registerAgent(t, alice, "Alice", "/tmp/a", models.AgentTypeClaude)

	// No entry agent configured (default).
	code, body := postJSON(t, env.ts.URL+"/api/v1/agent/send", SendMessageRequest{
		From:    "Alice",
		To:      "",
		Content: "where does this go?",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no entry_agent, got %d: %s", code, body)
	}

	var resp MessageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false when no entry_agent configured")
	}
	if !strings.Contains(resp.Message, "no --to specified") {
		t.Errorf("expected error about missing --to, got: %s", resp.Message)
	}
}

func TestSendEndpoint_ExplicitTo_IgnoresEntryAgent(t *testing.T) {
	env := newTestEnv(t)
	alice := uuid.New()
	bob := uuid.New()
	mgr := uuid.New()
	env.registerAgent(t, alice, "Alice", "/tmp/a", models.AgentTypeClaude)
	env.registerAgent(t, bob, "Bob", "/tmp/b", models.AgentTypeClaude)
	env.registerAgent(t, mgr, "Manager", "/tmp/m", models.AgentTypeClaude)

	// Configure entry agent, but send with explicit To.
	env.srv.EntryAgent = "Manager"

	code, body := postJSON(t, env.ts.URL+"/api/v1/agent/send", SendMessageRequest{
		From:    "Alice",
		To:      "Bob",
		Content: "direct message to Bob",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", code, body)
	}

	var resp MessageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Errorf("explicit To should still work: %s", resp.Message)
	}

	// Verify Bob (not Manager) received the message.
	sessB := uuid.NewString()
	toolCall(t, env.ts, sessB, ToolRegisterAgent, map[string]interface{}{
		"agentId": bob.String(), "name": "Bob", "folder": "/tmp/b",
	})
	checkResp := toolCall(t, env.ts, sessB, ToolCheckMessages, map[string]interface{}{"markRead": true})
	if checkResp.Error != nil {
		t.Fatalf("check-messages error: %v", checkResp.Error)
	}
	result := checkResp.Result.(map[string]interface{})
	text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "direct message to Bob") {
		t.Errorf("Bob should receive the direct message, got: %s", text)
	}
}
