package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// --- helpers ---

func writeTempFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- Claude ---

func TestParseClaudeSession_BasicUserMessage(t *testing.T) {
	dir := t.TempDir()
	jsonl := `{"type":"user","timestamp":"2024-01-15T10:00:00Z","message":{"role":"user","content":"Hello world"}}` + "\n" +
		`{"type":"assistant","timestamp":"2024-01-15T10:00:05Z","message":{"role":"assistant","content":"Hi!"}}` + "\n"
	path := writeTempFile(t, dir, "abc123.jsonl", []byte(jsonl))

	sum, err := parseClaudeSession(path, "abc123")
	if err != nil {
		t.Fatalf("parseClaudeSession: %v", err)
	}
	if sum.SessionID != "abc123" {
		t.Errorf("SessionID = %q, want %q", sum.SessionID, "abc123")
	}
	if sum.Title != "Hello world" {
		t.Errorf("Title = %q, want %q", sum.Title, "Hello world")
	}
	if sum.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", sum.MessageCount)
	}
	if sum.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestParseClaudeSession_ContentBlocks(t *testing.T) {
	dir := t.TempDir()
	// Use a raw JSONL line with content as a proper JSON array (not double-encoded).
	line := `{"type":"user","timestamp":"2024-06-01T12:00:00Z","message":{"role":"user","content":[{"type":"text","text":"Block message"}]}}` + "\n"
	path := writeTempFile(t, dir, "sess1.jsonl", []byte(line))

	sum, err := parseClaudeSession(path, "sess1")
	if err != nil {
		t.Fatalf("parseClaudeSession: %v", err)
	}
	if sum.Title != "Block message" {
		t.Errorf("Title = %q, want %q", sum.Title, "Block message")
	}
}

func TestParseClaudeSession_SummaryFallback(t *testing.T) {
	dir := t.TempDir()
	jsonl := `{"type":"summary","timestamp":"2024-01-01T00:00:00Z","summary":"Project summary title"}` + "\n"
	path := writeTempFile(t, dir, "sess2.jsonl", []byte(jsonl))

	sum, err := parseClaudeSession(path, "sess2")
	if err != nil {
		t.Fatalf("parseClaudeSession: %v", err)
	}
	if sum.Title != "Project summary title" {
		t.Errorf("Title = %q, want %q", sum.Title, "Project summary title")
	}
}

func TestParseClaudeSession_Empty(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "empty.jsonl", []byte{})

	sum, err := parseClaudeSession(path, "empty")
	if err != nil {
		t.Fatalf("parseClaudeSession: %v", err)
	}
	if sum.Title != "(empty session)" {
		t.Errorf("Title = %q, want %q", sum.Title, "(empty session)")
	}
	if sum.MessageCount != 0 {
		t.Errorf("MessageCount = %d, want 0", sum.MessageCount)
	}
}

func TestClaudeProvider_ListSessions(t *testing.T) {
	home := t.TempDir()
	folder := "/home/user/myproject"
	encoded := "-home-user-myproject"
	projectDir := filepath.Join(home, ".claude", "projects", encoded)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write two session files.
	jsonl1 := `{"type":"user","timestamp":"2024-03-01T08:00:00Z","message":{"role":"user","content":"First session"}}` + "\n"
	jsonl2 := `{"type":"user","timestamp":"2024-03-02T09:00:00Z","message":{"role":"user","content":"Second session"}}` + "\n"
	writeTempFile(t, projectDir, "sess-a.jsonl", []byte(jsonl1))
	writeTempFile(t, projectDir, "sess-b.jsonl", []byte(jsonl2))

	// Override home dir for the test by patching UserHomeDir via env.
	t.Setenv("HOME", home)

	p := &ClaudeProvider{}
	sessions, err := p.ListSessions(folder)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("got %d sessions, want 2", len(sessions))
	}
}

func TestClaudeProvider_ListSessions_NonExistentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := &ClaudeProvider{}
	sessions, err := p.ListSessions("/some/folder")
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got %v", err)
	}
	if sessions != nil {
		t.Errorf("expected nil sessions for missing dir")
	}
}

func TestClaudeProvider_DeleteSession(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, ".claude", "projects", "-home-user-proj")
	jsonl := `{"type":"user","timestamp":"2024-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}` + "\n"
	target := writeTempFile(t, projectDir, "todelete.jsonl", []byte(jsonl))
	t.Setenv("HOME", home)

	p := &ClaudeProvider{}
	if err := p.DeleteSession("todelete"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("file should have been deleted")
	}
}

// --- pathMatchesFolder ---

func TestPathMatchesFolder(t *testing.T) {
	cases := []struct {
		dirName string
		folder  string
		want    bool
	}{
		{"-home-user-project", "/home/user/project", true},
		{"home-user-project", "/home/user/project", true},  // no leading dash
		{"-other-folder", "/home/user/project", false},
		{"/home/user/project", "/home/user/project", true}, // decoded match
	}
	for _, tc := range cases {
		encoded := strings.ReplaceAll(tc.folder, "/", "-")
		got := pathMatchesFolder(tc.dirName, tc.folder, encoded)
		if got != tc.want {
			t.Errorf("pathMatchesFolder(%q, %q) = %v, want %v", tc.dirName, tc.folder, got, tc.want)
		}
	}
}

// --- Codex ---

func TestParseCodexSession_Basic(t *testing.T) {
	dir := t.TempDir()
	folder := "/home/user/proj"
	s := codexSession{
		ID:         "codex-session-1",
		CreatedAt:  "2024-05-10T14:30:00Z",
		WorkingDir: folder,
		Messages: []codexMsg{
			{Role: "user", Content: "Fix the bug"},
			{Role: "assistant", Content: "Done"},
		},
	}
	data, _ := json.Marshal(s)
	path := writeTempFile(t, dir, "codex-session-1.json", data)

	sum, err := parseCodexSession(path, folder)
	if err != nil {
		t.Fatalf("parseCodexSession: %v", err)
	}
	if sum.SessionID != "codex-session-1" {
		t.Errorf("SessionID = %q", sum.SessionID)
	}
	if sum.Title != "Fix the bug" {
		t.Errorf("Title = %q", sum.Title)
	}
	if sum.MessageCount != 2 {
		t.Errorf("MessageCount = %d", sum.MessageCount)
	}
}

func TestParseCodexSession_WrongFolder(t *testing.T) {
	dir := t.TempDir()
	s := codexSession{
		ID:         "sess",
		WorkingDir: "/other/folder",
		Messages:   []codexMsg{{Role: "user", Content: "hello"}},
	}
	data, _ := json.Marshal(s)
	path := writeTempFile(t, dir, "sess.json", data)

	sum, err := parseCodexSession(path, "/my/folder")
	if err != nil {
		t.Fatalf("parseCodexSession: %v", err)
	}
	// Should be filtered (empty SessionID).
	if sum.SessionID != "" {
		t.Errorf("expected empty SessionID for non-matching folder, got %q", sum.SessionID)
	}
}

func TestParseCodexSession_FallbackIDFromFilename(t *testing.T) {
	dir := t.TempDir()
	s := codexSession{
		// No ID field — should fall back to filename.
		WorkingDir: "/my/folder",
		Messages:   []codexMsg{{Role: "user", Content: "hi"}},
	}
	data, _ := json.Marshal(s)
	path := writeTempFile(t, dir, "file-uuid.json", data)

	sum, err := parseCodexSession(path, "/my/folder")
	if err != nil {
		t.Fatalf("parseCodexSession: %v", err)
	}
	if sum.SessionID != "file-uuid" {
		t.Errorf("SessionID = %q, want %q", sum.SessionID, "file-uuid")
	}
}

// --- Service ---

func TestService_Supports(t *testing.T) {
	svc := New()
	for _, at := range []models.AgentType{
		models.AgentTypeClaude, models.AgentTypeCodex,
		models.AgentTypeGemini, models.AgentTypeCopilot,
	} {
		if !svc.Supports(at) {
			t.Errorf("Supports(%s) = false, want true", at)
		}
	}
	if svc.Supports(models.AgentTypeShell) {
		t.Error("Supports(shell) = true, want false")
	}
}

func TestService_ListSessions_SortedNewestFirst(t *testing.T) {
	// Build a fake provider that returns sessions in arbitrary order.
	svc := &Service{
		providers: map[models.AgentType]Provider{
			models.AgentTypeClaude: &fixedProvider{sessions: []SessionSummary{
				{SessionID: "old", Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
				{SessionID: "new", Timestamp: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
				{SessionID: "mid", Timestamp: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
			}},
		},
	}
	sessions, err := svc.ListSessions(models.AgentTypeClaude, "/any")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions", len(sessions))
	}
	if sessions[0].SessionID != "new" || sessions[1].SessionID != "mid" || sessions[2].SessionID != "old" {
		t.Errorf("wrong order: %v", sessions)
	}
}

func TestService_ListSessions_Cap20(t *testing.T) {
	var ss []SessionSummary
	for i := 0; i < 25; i++ {
		ss = append(ss, SessionSummary{
			SessionID: "s",
			Timestamp: time.Date(2024, 1, i+1, 0, 0, 0, 0, time.UTC),
		})
	}
	svc := &Service{
		providers: map[models.AgentType]Provider{
			models.AgentTypeClaude: &fixedProvider{sessions: ss},
		},
	}
	sessions, _ := svc.ListSessions(models.AgentTypeClaude, "/any")
	if len(sessions) != 20 {
		t.Errorf("got %d sessions, want 20", len(sessions))
	}
}

func TestService_ListSessions_UnsupportedType(t *testing.T) {
	svc := New()
	sessions, err := svc.ListSessions(models.AgentTypeShell, "/any")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if sessions != nil {
		t.Error("expected nil sessions for unsupported type")
	}
}

// fixedProvider is a test double for Provider.
type fixedProvider struct {
	sessions []SessionSummary
	deleted  []string
}

func (f *fixedProvider) ListSessions(_ string) ([]SessionSummary, error) {
	return f.sessions, nil
}
func (f *fixedProvider) DeleteSession(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

// --- truncate ---

func TestTruncate(t *testing.T) {
	cases := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"", 5, ""},
		{"  spaces  ", 10, "spaces"},
	}
	for _, tc := range cases {
		got := truncate(tc.input, tc.max)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
		}
	}
}
