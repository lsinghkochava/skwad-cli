package runlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// rawEntry is used to decode JSON lines from the log file for verification.
type rawEntry struct {
	Timestamp string                 `json:"timestamp"`
	Event     string                 `json:"event"`
	AgentID   string                 `json:"agent_id"`
	AgentName string                 `json:"agent_name"`
	Data      map[string]interface{} `json:"data"`
}

// readEntries reads all JSON lines from the log file.
func readEntries(t *testing.T, dir string) []rawEntry {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no log files found")
	}

	f, err := os.Open(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	var entries []rawEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e rawEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshal line %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return entries
}

// --- Constructor tests ---

func TestNew_CreatesDirectoryAndFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs", "nested")

	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rl.Close()

	// Directory should exist.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("path should be a directory")
	}

	// File should exist with .jsonl extension.
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if !strings.HasSuffix(files[0].Name(), ".jsonl") {
		t.Errorf("filename should end with .jsonl, got %q", files[0].Name())
	}
}

func TestNew_FilenameFormat(t *testing.T) {
	dir := t.TempDir()

	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rl.Close()

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	// Filename should match YYYY-MM-DDThh-mm-ss.jsonl pattern.
	name := files[0].Name()
	// Strip .jsonl suffix and parse.
	ts := strings.TrimSuffix(name, ".jsonl")
	_, err = time.Parse("2006-01-02T15-04-05", ts)
	if err != nil {
		t.Errorf("filename %q does not match expected timestamp format: %v", name, err)
	}
}

// --- Nil safety tests (CRITICAL) ---

func TestNilLogger_AllMethodsSafe(t *testing.T) {
	var rl *RunLogger

	// None of these should panic.
	rl.LogToolCall("id", "name", "tool", map[string]interface{}{"k": "v"}, "result")
	rl.LogMessage("from", "fromName", "to", "content")
	rl.LogBroadcast("from", "fromName", "content")
	rl.LogStatus("id", "name", "running", "doing stuff", "code")
	rl.LogSpawn("id", "name", "claude", "/tmp", []string{"--flag"})
	rl.LogExit("id", "name", 0)
	rl.LogPrompt("id", "name", "initial", "do the thing")
	rl.LogHookEvent("id", "name", "status_change", "running")

	err := rl.Close()
	if err != nil {
		t.Errorf("Close on nil should return nil, got %v", err)
	}
}

// --- Happy path: each log method writes correct JSON ---

func TestLogToolCall_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogToolCall("agent-1", "Coder", "read-file", map[string]interface{}{"path": "/foo"}, "file contents")
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Event != "tool_call" {
		t.Errorf("event = %q, want %q", e.Event, "tool_call")
	}
	if e.AgentID != "agent-1" {
		t.Errorf("agent_id = %q, want %q", e.AgentID, "agent-1")
	}
	if e.AgentName != "Coder" {
		t.Errorf("agent_name = %q, want %q", e.AgentName, "Coder")
	}
	if e.Data["tool"] != "read-file" {
		t.Errorf("data.tool = %v, want %q", e.Data["tool"], "read-file")
	}

	// Verify timestamp is valid RFC3339Nano.
	if _, err := time.Parse(time.RFC3339Nano, e.Timestamp); err != nil {
		t.Errorf("timestamp %q is not valid RFC3339Nano: %v", e.Timestamp, err)
	}
}

func TestLogMessage_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogMessage("from-id", "Coder", "to-id", "hello there")
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Event != "message" {
		t.Errorf("event = %q, want %q", e.Event, "message")
	}
	if e.Data["to_id"] != "to-id" {
		t.Errorf("data.to_id = %v, want %q", e.Data["to_id"], "to-id")
	}
	if e.Data["content"] != "hello there" {
		t.Errorf("data.content = %v, want %q", e.Data["content"], "hello there")
	}
}

func TestLogBroadcast_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogBroadcast("from-id", "Manager", "all hands on deck")
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Event != "broadcast" {
		t.Errorf("event = %q, want %q", e.Event, "broadcast")
	}
	if e.AgentName != "Manager" {
		t.Errorf("agent_name = %q, want %q", e.AgentName, "Manager")
	}
	if e.Data["content"] != "all hands on deck" {
		t.Errorf("data.content = %v, want %q", e.Data["content"], "all hands on deck")
	}
}

func TestLogStatus_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogStatus("agent-1", "Coder", "running", "implementing feature", "code")
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Event != "status" {
		t.Errorf("event = %q, want %q", e.Event, "status")
	}
	if e.Data["status"] != "running" {
		t.Errorf("data.status = %v, want %q", e.Data["status"], "running")
	}
	if e.Data["status_text"] != "implementing feature" {
		t.Errorf("data.status_text = %v, want %q", e.Data["status_text"], "implementing feature")
	}
	if e.Data["category"] != "code" {
		t.Errorf("data.category = %v, want %q", e.Data["category"], "code")
	}
}

func TestLogSpawn_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogSpawn("agent-1", "Coder", "claude", "/workspace", []string{"--model", "opus"})
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Event != "spawn" {
		t.Errorf("event = %q, want %q", e.Event, "spawn")
	}
	if e.Data["type"] != "claude" {
		t.Errorf("data.type = %v, want %q", e.Data["type"], "claude")
	}
	if e.Data["folder"] != "/workspace" {
		t.Errorf("data.folder = %v, want %q", e.Data["folder"], "/workspace")
	}
}

func TestLogExit_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogExit("agent-1", "Coder", 0)
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Event != "exit" {
		t.Errorf("event = %q, want %q", e.Event, "exit")
	}
	// JSON numbers decode as float64.
	if e.Data["exit_code"] != float64(0) {
		t.Errorf("data.exit_code = %v, want 0", e.Data["exit_code"])
	}
}

func TestLogPrompt_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogPrompt("agent-1", "Coder", "initial", "implement the login page")
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Event != "prompt" {
		t.Errorf("event = %q, want %q", e.Event, "prompt")
	}
	if e.Data["prompt_type"] != "initial" {
		t.Errorf("data.prompt_type = %v, want %q", e.Data["prompt_type"], "initial")
	}
	if e.Data["prompt"] != "implement the login page" {
		t.Errorf("data.prompt = %v, want %q", e.Data["prompt"], "implement the login page")
	}
}

func TestLogHookEvent_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogHookEvent("agent-1", "Coder", "status_change", "running")
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Event != "hook" {
		t.Errorf("event = %q, want %q", e.Event, "hook")
	}
	if e.Data["event_type"] != "status_change" {
		t.Errorf("data.event_type = %v, want %q", e.Data["event_type"], "status_change")
	}
	if e.Data["status"] != "running" {
		t.Errorf("data.status = %v, want %q", e.Data["status"], "running")
	}
}

// --- Multiple log calls produce multiple JSON lines ---

func TestMultipleLogCalls_ProduceMultipleLines(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogSpawn("a1", "Coder", "claude", "/tmp", nil)
	rl.LogStatus("a1", "Coder", "running", "coding", "code")
	rl.LogToolCall("a1", "Coder", "read", nil, nil)
	rl.LogMessage("a1", "Coder", "a2", "hello")
	rl.LogExit("a1", "Coder", 0)
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	expectedEvents := []string{"spawn", "status", "tool_call", "message", "exit"}
	for i, want := range expectedEvents {
		if entries[i].Event != want {
			t.Errorf("entry[%d].event = %q, want %q", i, entries[i].Event, want)
		}
	}
}

// --- Edge cases ---

func TestLogToolCall_NilArgsAndResult(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogToolCall("a1", "Coder", "noop", nil, nil)
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Data["tool"] != "noop" {
		t.Errorf("data.tool = %v, want %q", e.Data["tool"], "noop")
	}
	// args and result should be null in JSON (decoded as nil).
	if e.Data["args"] != nil {
		t.Errorf("data.args = %v, want nil", e.Data["args"])
	}
	if e.Data["result"] != nil {
		t.Errorf("data.result = %v, want nil", e.Data["result"])
	}
}

func TestLogSpawn_EmptyArgs(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogSpawn("a1", "Coder", "claude", "/tmp", []string{})
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// Empty slice should encode as [] not null.
	args, ok := entries[0].Data["args"].([]interface{})
	if !ok {
		t.Fatalf("data.args should be an array, got %T", entries[0].Data["args"])
	}
	if len(args) != 0 {
		t.Errorf("data.args should be empty, got %v", args)
	}
}

func TestLogPrompt_EmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogPrompt("a1", "Coder", "follow-up", "")
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Data["prompt"] != "" {
		t.Errorf("data.prompt = %v, want empty string", entries[0].Data["prompt"])
	}
}

func TestLogExit_NonZeroExitCode(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogExit("a1", "Coder", 137)
	rl.Close()

	entries := readEntries(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Data["exit_code"] != float64(137) {
		t.Errorf("data.exit_code = %v, want 137", entries[0].Data["exit_code"])
	}
}

// --- Concurrent writes ---

func TestConcurrentWrites_NoCorruption(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const goroutines = 10
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for j := range callsPerGoroutine {
				switch j % 8 {
				case 0:
					rl.LogToolCall("a1", "Coder", "tool", nil, nil)
				case 1:
					rl.LogMessage("a1", "Coder", "a2", "msg")
				case 2:
					rl.LogBroadcast("a1", "Coder", "broadcast")
				case 3:
					rl.LogStatus("a1", "Coder", "running", "text", "code")
				case 4:
					rl.LogSpawn("a1", "Coder", "claude", "/tmp", nil)
				case 5:
					rl.LogExit("a1", "Coder", id)
				case 6:
					rl.LogPrompt("a1", "Coder", "initial", "prompt")
				case 7:
					rl.LogHookEvent("a1", "Coder", "hook", "running")
				}
			}
		}(g)
	}

	wg.Wait()
	rl.Close()

	// Verify all lines are valid JSON.
	entries := readEntries(t, dir)
	expectedTotal := goroutines * callsPerGoroutine
	if len(entries) != expectedTotal {
		t.Errorf("expected %d entries, got %d", expectedTotal, len(entries))
	}

	// Verify every entry has a valid timestamp.
	for i, e := range entries {
		if _, err := time.Parse(time.RFC3339Nano, e.Timestamp); err != nil {
			t.Errorf("entry[%d] invalid timestamp %q: %v", i, e.Timestamp, err)
		}
	}
}

// --- Close ---

func TestClose_ClosesFile(t *testing.T) {
	dir := t.TempDir()
	rl, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rl.LogStatus("a1", "Coder", "running", "test", "test")

	err = rl.Close()
	if err != nil {
		t.Errorf("Close: %v", err)
	}

	// Writing after close should not panic (write silently fails).
	// The encoder will get a write error on the closed file, but write() ignores it.
	rl.LogStatus("a1", "Coder", "after-close", "test", "test")
}
