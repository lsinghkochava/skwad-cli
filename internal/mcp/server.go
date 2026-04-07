// Package mcp implements the in-process MCP (Model Context Protocol) HTTP server.
// It exposes a JSON-RPC 2.0 endpoint at /mcp for AI agent tool calls, and a
// /hook endpoint for lifecycle events posted by claude/codex plugin scripts.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/agent"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
)

// Server is the in-process MCP HTTP server (JSON-RPC 2.0).
type Server struct {
	coordinator *agent.Coordinator
	store       *persistence.Store
	port        int

	httpServer  *http.Server
	tools       *toolHandler
	sessions    *sessionManager
	hookHandler *hookHandler

	mu      sync.Mutex
	started bool

	// Callbacks — set by UI layer.
	OnDisplayMarkdown func(agentID, filePath string)
	OnViewMermaid     func(agentID, source, title string)
	OnCreateAgent     func(req CreateAgentRequest) error
	OnCloseAgent      func(callerID, targetID string) error

	// OnToolCall is called after a tool is dispatched, for logging.
	OnToolCall func(agentID, agentName, toolName string, args map[string]interface{}, result interface{})
	// OnToolCallLog is called after a tool is dispatched, for TUI activity log display.
	OnToolCallLog func(agentName, toolName, argsPreview string)

	// StatusUpdater — set by the agent manager to receive hook events.
	StatusUpdater AgentStatusUpdater

	// EntryAgent is the default message recipient when "to" is omitted.
	EntryAgent string
}

// NewServer creates a new MCP server.
func NewServer(coord *agent.Coordinator, store *persistence.Store, port int) *Server {
	s := &Server{
		coordinator: coord,
		store:       store,
		port:        port,
		sessions:    newSessionManager(),
	}
	s.tools = newToolHandler(s)
	return s
}

// Start begins listening on the configured port.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}

	s.hookHandler = newHookHandler(s)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.Handle("/hook", s.hookHandler)
	mux.HandleFunc("/api/v1/agent/register", s.handleRegister)
	mux.HandleFunc("/api/v1/agent/status", s.handleStatus)
	mux.HandleFunc("/api/v1/agent/send", s.handleSend)
	mux.HandleFunc("/api/v1/agent/broadcast", s.handleBroadcast)
	mux.HandleFunc("/api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("/", s.handleDebug)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler: mux,
	}

	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("mcp server listen: %w", err)
	}

	s.started = true
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("mcp server error: %v", err)
		}
	}()
	log.Printf("MCP server listening on %s", s.httpServer.Addr)
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(context.Background())
	}
	s.started = false
}

// URL returns the full MCP endpoint URL.
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/mcp", s.port)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	// Only respond to exactly GET /
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	agents := s.coordinator.AllAgents()
	type debugAgent struct {
		AgentID    string            `json:"agent_id"`
		Name       string            `json:"name"`
		Folder     string            `json:"folder"`
		State      string            `json:"state"`
		Status     string            `json:"status"`
		Registered bool              `json:"registered"`
		AgentType  string            `json:"agent_type"`
		SessionID  string            `json:"session_id"`
		Metadata   map[string]string `json:"metadata"`
	}

	result := make([]debugAgent, len(agents))
	for i, a := range agents {
		result[i] = debugAgent{
			AgentID:    a.ID.String(),
			Name:       a.Name,
			Folder:     a.Folder,
			State:      string(a.Status),
			Status:     a.StatusText,
			Registered: a.IsRegistered,
			AgentType:  string(a.AgentType),
			SessionID:  a.SessionID,
			Metadata:   a.Metadata,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tasks := s.coordinator.ListTasks()
	agents := s.coordinator.AllAgents()

	// Build agent ID → name lookup.
	nameByID := make(map[string]string, len(agents))
	for _, a := range agents {
		nameByID[a.ID.String()] = a.Name
	}

	type taskResponse struct {
		ID            string  `json:"id"`
		Title         string  `json:"title"`
		Status        string  `json:"status"`
		AssigneeName  string  `json:"assigneeName,omitempty"`
		CreatedByName string  `json:"createdByName,omitempty"`
	}

	result := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		result[i] = taskResponse{
			ID:            t.ID.String(),
			Title:         t.Title,
			Status:        string(t.Status),
			AssigneeName:  t.AssigneeName,
			CreatedByName: nameByID[t.CreatedBy.String()],
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse into raw map to support both snake_case and camelCase agent_id/agentId.
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	agentIDStr := stringFromMap(raw, "agent_id")
	if agentIDStr == "" {
		agentIDStr = stringFromMap(raw, "agentId")
	}
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		http.Error(w, "Missing or invalid agent_id", http.StatusBadRequest)
		return
	}

	// Verify agent exists.
	ag, ok := s.coordinator.Agent(agentID)
	if !ok {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	agentType := stringFromMap(raw, "agent")
	if agentType == "" {
		agentType = "claude"
	}
	if agentType != "claude" {
		http.Error(w, fmt.Sprintf("Unknown agent type: %s", agentType), http.StatusBadRequest)
		return
	}

	source := stringFromMap(raw, "source")
	if source == "" {
		source = "startup"
	}

	// Extract metadata from payload subobject.
	metadata := extractMetadata(raw)
	sessionID := stringFromMap(raw, "session_id")
	if sessionID == "" {
		// Also check payload for session_id.
		if payload, ok := raw["payload"].(map[string]interface{}); ok {
			sessionID = stringFromMap(payload, "session_id")
		}
	}

	// Apply metadata via StatusUpdater.
	if s.StatusUpdater != nil {
		for k, v := range metadata {
			s.StatusUpdater.SetMetadata(agentID, k, v)
		}
	}

	if source == "resume" {
		// Resume: only update session ID if not forking.
		if !ag.IsFork && sessionID != "" && s.StatusUpdater != nil {
			s.StatusUpdater.SetSessionID(agentID, sessionID)
		}
	} else {
		// Startup: full registration.
		// Only pass session ID if agent is not resuming an existing session.
		regSessionID := sessionID
		if ag.ResumeSessionID != "" && !ag.IsFork {
			regSessionID = ""
		}
		members, unread, _ := s.coordinator.RegisterAgent(agentID, ag.Name, ag.Folder, regSessionID)
		if s.StatusUpdater != nil {
			s.StatusUpdater.SetRunning(agentID)
		}

		// Build response.
		memberResponses := make([]AgentInfoResponse, len(members))
		for i, m := range members {
			memberResponses[i] = AgentInfoResponse{
				ID:           m.ID.String(),
				Name:         m.Name,
				Folder:       m.Folder,
				Status:       string(m.Status),
				IsRegistered: m.IsRegistered,
			}
		}

		resp := RegisterHookResponse{
			Success:            true,
			Message:            "Registered",
			UnreadMessageCount: unread,
			SkwadMembers:       memberResponses,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Resume path — still return a response.
	members := s.coordinator.ListAgents()
	memberResponses := make([]AgentInfoResponse, len(members))
	for i, m := range members {
		memberResponses[i] = AgentInfoResponse{
			ID:           m.ID.String(),
			Name:         m.Name,
			Folder:       m.Folder,
			Status:       string(m.Status),
			IsRegistered: m.IsRegistered,
		}
	}
	resp := RegisterHookResponse{
		Success:            true,
		Message:            "Registered",
		UnreadMessageCount: 0,
		SkwadMembers:       memberResponses,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse into raw map to support both snake_case and camelCase.
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	agentIDStr := stringFromMap(raw, "agent_id")
	if agentIDStr == "" {
		agentIDStr = stringFromMap(raw, "agentId")
	}
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		http.Error(w, "Missing or invalid agent_id", http.StatusBadRequest)
		return
	}

	agentType := stringFromMap(raw, "agent")
	if s.StatusUpdater == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
		return
	}

	metadata := extractMetadata(raw)

	switch agentType {
	case "claude":
		// Claude: status field maps directly; extract session_id from payload.
		status := stringFromMap(raw, "status")
		if payload, ok := raw["payload"].(map[string]interface{}); ok {
			if sid := stringFromMap(payload, "session_id"); sid != "" {
				s.StatusUpdater.SetSessionID(agentID, sid)
			}
		}
		s.hookHandler.dispatchStatus(agentID, status, metadata)
	case "codex":
		// Codex: only process agent-turn-complete events.
		if payload, ok := raw["payload"].(map[string]interface{}); ok {
			if stringFromMap(payload, "type") != "agent-turn-complete" {
				break
			}
			// Store thread-id as session ID.
			if threadID := stringFromMap(payload, "thread-id"); threadID != "" {
				s.StatusUpdater.SetSessionID(agentID, threadID)
			}
		}
		s.hookHandler.dispatchStatus(agentID, "idle", metadata)
	default:
		// Unknown agent type — still return OK for forward compat.
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// stringFromMap safely extracts a string value from a map.
func stringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractMetadata pulls known metadata keys from the payload subobject.
func extractMetadata(raw map[string]interface{}) map[string]string {
	payload, ok := raw["payload"].(map[string]interface{})
	if !ok {
		return nil
	}
	meta := make(map[string]string)
	for _, key := range []string{"transcript_path", "cwd", "model", "session_id", "thread-id", "turn-id"} {
		if v := stringFromMap(payload, key); v != "" {
			meta[key] = v
		}
	}
	return meta
}

// resolveAgentID finds a registered agent by UUID string or name and returns its UUID.
func (s *Server) resolveAgentID(nameOrID string) (uuid.UUID, bool) {
	// Try parsing as UUID first.
	if id, err := uuid.Parse(nameOrID); err == nil {
		if _, ok := s.coordinator.Agent(id); ok {
			return id, true
		}
	}
	// Fall back to name lookup via registered agents.
	for _, a := range s.coordinator.ListAgents() {
		if strings.EqualFold(a.Name, nameOrID) {
			return a.ID, true
		}
	}
	// Fall back to all agents (including unregistered) from the manager.
	for _, a := range s.coordinator.AllAgents() {
		if strings.EqualFold(a.Name, nameOrID) {
			return a.ID, true
		}
	}
	return uuid.UUID{}, false
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.From == "" || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, MessageResponse{
			Success: false,
			Message: "from and content are required",
		})
		return
	}

	if req.To == "" {
		if s.EntryAgent == "" {
			writeJSON(w, http.StatusBadRequest, MessageResponse{
				Success: false,
				Message: "no --to specified and no entry_agent configured",
			})
			return
		}
		req.To = s.EntryAgent
	}

	// Resolve sender — "user" is a synthetic sender (not a real agent).
	var fromID uuid.UUID
	if !strings.EqualFold(req.From, "user") {
		var ok bool
		fromID, ok = s.resolveAgentID(req.From)
		if !ok {
			writeJSON(w, http.StatusNotFound, MessageResponse{
				Success: false,
				Message: fmt.Sprintf("sender not found: %s", req.From),
			})
			return
		}
	}

	if err := s.coordinator.SendMessage(fromID, req.To, req.Content); err != nil {
		writeJSON(w, http.StatusNotFound, MessageResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, MessageResponse{
		Success: true,
		Message: "Message sent",
	})
}

func (s *Server) handleBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req BroadcastMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.From == "" || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, MessageResponse{
			Success: false,
			Message: "from and content are required",
		})
		return
	}

	fromID, ok := s.resolveAgentID(req.From)
	if !ok {
		writeJSON(w, http.StatusNotFound, MessageResponse{
			Success: false,
			Message: fmt.Sprintf("sender not found: %s", req.From),
		})
		return
	}

	s.coordinator.BroadcastMessage(fromID, req.Content)

	writeJSON(w, http.StatusOK, MessageResponse{
		Success: true,
		Message: "Message broadcast",
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeResponse(w, Response{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: -32700, Message: "parse error"},
		})
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	session := s.sessions.getOrCreate(sessionID)

	// Auto-bind agent ID from URL query param.
	if session.agentID == uuid.Nil {
		if agentStr := r.URL.Query().Get("agent"); agentStr != "" {
			if parsed, err := uuid.Parse(agentStr); err == nil {
				session.agentID = parsed
			}
		}
	}

	resp := s.dispatch(req, session)
	writeResponse(w, resp)
}

func (s *Server) dispatch(req Request, session *session) Response {
	base := Response{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case MethodPing:
		base.Result = map[string]interface{}{}

	case MethodInitialize:
		base.Result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "skwad", "version": "1.0.0"},
		}

	case MethodToolsList:
		base.Result = map[string]interface{}{"tools": s.tools.list()}

	case MethodToolsCall:
		params, err := parseToolCallParams(req.Params)
		if err != nil {
			base.Error = &RPCError{Code: -32602, Message: "invalid params"}
			return base
		}
		result, toolErr := s.tools.call(params, session)
		if toolErr != nil {
			base.Error = &RPCError{Code: -32000, Message: toolErr.Error()}
		} else {
			base.Result = result
		}

	default:
		base.Error = &RPCError{Code: -32601, Message: "method not found"}
	}

	return base
}


func parseToolCallParams(raw interface{}) (ToolCallParams, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return ToolCallParams{}, err
	}
	var p ToolCallParams
	return p, json.Unmarshal(data, &p)
}

func writeResponse(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
