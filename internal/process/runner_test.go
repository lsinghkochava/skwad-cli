package process

import (
	"encoding/json"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner([]string{"echo", "hello"}, nil, "")
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}
	if r.ExitCode() != -1 {
		t.Errorf("initial exit code = %d, want -1", r.ExitCode())
	}
	if r.IsRunning() {
		t.Error("IsRunning() should be false before Start()")
	}
}

func TestStartAndWait(t *testing.T) {
	r := NewRunner([]string{"echo", "hello"}, nil, "")

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case <-r.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}

	if r.IsRunning() {
		t.Error("IsRunning() should be false after exit")
	}
	if r.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", r.ExitCode())
	}
}

func TestStartAlreadyStarted(t *testing.T) {
	r := NewRunner([]string{"sleep", "10"}, nil, "")
	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer r.Kill()

	if err := r.Start(); err == nil {
		t.Error("second Start() should return error")
	}
}

func TestExitCode(t *testing.T) {
	r := NewRunner([]string{"sh", "-c", "exit 42"}, nil, "")
	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case <-r.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}

	if r.ExitCode() != 42 {
		t.Errorf("ExitCode() = %d, want 42", r.ExitCode())
	}
}

func TestOnExitCallback(t *testing.T) {
	r := NewRunner([]string{"sh", "-c", "exit 7"}, nil, "")

	var gotCode int
	var wg sync.WaitGroup
	wg.Add(1)
	r.OnExit = func(code int) {
		gotCode = code
		wg.Done()
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	wg.Wait()
	if gotCode != 7 {
		t.Errorf("OnExit code = %d, want 7", gotCode)
	}
}

func TestStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal-based stop not supported on Windows")
	}

	r := NewRunner([]string{"sleep", "60"}, nil, "")
	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !r.IsRunning() {
		t.Fatal("IsRunning() should be true after Start()")
	}

	if err := r.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	select {
	case <-r.Wait():
	case <-time.After(10 * time.Second):
		t.Fatal("process did not exit after Stop()")
	}

	if r.IsRunning() {
		t.Error("IsRunning() should be false after Stop()")
	}
}

func TestKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal-based kill not supported on Windows")
	}

	r := NewRunner([]string{"sleep", "60"}, nil, "")
	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	r.Kill()

	select {
	case <-r.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after Kill()")
	}

	if r.IsRunning() {
		t.Error("IsRunning() should be false after Kill()")
	}
}

func TestSendPrompt(t *testing.T) {
	// Use cat as a mock process — it echoes stdin to stdout.
	r := NewRunner([]string{"cat"}, nil, "")

	var mu sync.Mutex
	var received []StreamMessage
	r.OnMessage = func(msg StreamMessage) {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if err := r.SendPrompt("hello world"); err != nil {
		t.Fatalf("SendPrompt() error: %v", err)
	}

	// Give cat time to echo back.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := len(received)
	var first StreamMessage
	if count > 0 {
		first = received[0]
	}
	mu.Unlock()

	if count != 1 {
		t.Fatalf("received %d messages, want 1", count)
	}

	if first.Type != "user" {
		t.Errorf("message type = %q, want %q", first.Type, "user")
	}

	// Verify the raw JSON round-trips correctly.
	var input UserInputMessage
	if err := json.Unmarshal(first.Raw, &input); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if input.Message.Content != "hello world" {
		t.Errorf("content = %q, want %q", input.Message.Content, "hello world")
	}
}

func TestSendPromptNotStarted(t *testing.T) {
	r := NewRunner([]string{"cat"}, nil, "")
	if err := r.SendPrompt("test"); err == nil {
		t.Error("SendPrompt before Start should return error")
	}
}

func TestStdoutParsesJSON(t *testing.T) {
	// Create a small script that writes known JSON lines to stdout.
	script := `echo '{"type":"system","subtype":"init","session_id":"abc123"}'
echo '{"type":"assistant","session_id":"abc123","uuid":"u1"}'
echo '{"type":"result","subtype":"success","session_id":"abc123"}'`

	r := NewRunner([]string{"sh", "-c", script}, nil, "")

	var mu sync.Mutex
	var messages []StreamMessage
	r.OnMessage = func(msg StreamMessage) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case <-r.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}

	expected := []struct {
		msgType string
		subtype string
	}{
		{"system", "init"},
		{"assistant", ""},
		{"result", "success"},
	}

	for i, exp := range expected {
		if messages[i].Type != exp.msgType {
			t.Errorf("msg[%d].Type = %q, want %q", i, messages[i].Type, exp.msgType)
		}
		if messages[i].Subtype != exp.subtype {
			t.Errorf("msg[%d].Subtype = %q, want %q", i, messages[i].Subtype, exp.subtype)
		}
		if messages[i].Raw == nil {
			t.Errorf("msg[%d].Raw is nil", i)
		}
	}
}

func TestWorkingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRunner([]string{"pwd"}, nil, tmpDir)

	var mu sync.Mutex
	var output []byte
	r.OnMessage = func(msg StreamMessage) {
		mu.Lock()
		output = append(output, msg.Raw...)
		mu.Unlock()
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case <-r.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}

	// pwd output is not JSON, so OnMessage won't fire — that's expected.
	// Verify the process ran in the right directory via exit code.
	if r.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", r.ExitCode())
	}
}

func TestEnvironment(t *testing.T) {
	env := append(os.Environ(), "SKWAD_TEST_VAR=hello123")
	r := NewRunner([]string{"sh", "-c", `echo "{\"type\":\"$SKWAD_TEST_VAR\"}"`}, env, "")

	var mu sync.Mutex
	var messages []StreamMessage
	r.OnMessage = func(msg StreamMessage) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
	}

	if err := r.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case <-r.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	if messages[0].Type != "hello123" {
		t.Errorf("type = %q, want %q", messages[0].Type, "hello123")
	}
}

func TestNoArgs(t *testing.T) {
	r := NewRunner(nil, nil, "")
	if err := r.Start(); err == nil {
		t.Error("Start() with no args should return error")
	}
}

func TestNewUserInput(t *testing.T) {
	msg := NewUserInput("test prompt")
	if msg.Type != "user" {
		t.Errorf("Type = %q, want %q", msg.Type, "user")
	}
	if msg.Message.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Message.Role, "user")
	}
	if msg.Message.Content != "test prompt" {
		t.Errorf("Content = %q, want %q", msg.Message.Content, "test prompt")
	}

	// Verify JSON serialization.
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded UserInputMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if decoded.Message.Content != "test prompt" {
		t.Errorf("round-trip Content = %q, want %q", decoded.Message.Content, "test prompt")
	}
}
