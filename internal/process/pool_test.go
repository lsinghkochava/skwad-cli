package process

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPoolSpawnAndIsRunning(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	err := p.Spawn(id, "test-agent", []string{"sleep", "10"}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}
	defer p.Kill(id)

	if !p.IsRunning(id) {
		t.Error("IsRunning() should be true after Spawn()")
	}
}

func TestPoolSpawnDuplicate(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	err := p.Spawn(id, "test-agent", []string{"sleep", "10"}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}
	defer p.Kill(id)

	err = p.Spawn(id, "test-agent", []string{"sleep", "10"}, nil, "")
	if err == nil {
		t.Error("second Spawn() with same ID should return error")
	}
}

func TestPoolStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal-based stop not supported on Windows")
	}

	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	err := p.Spawn(id, "test-agent", []string{"sleep", "60"}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}

	if err := p.Stop(id); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if p.IsRunning(id) {
		t.Error("IsRunning() should be false after Stop()")
	}
}

func TestPoolKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal-based kill not supported on Windows")
	}

	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	err := p.Spawn(id, "test-agent", []string{"sleep", "60"}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}

	p.Kill(id)

	// Give process group time to be reaped.
	time.Sleep(200 * time.Millisecond)

	if p.IsRunning(id) {
		t.Error("IsRunning() should be false after Kill()")
	}
}

func TestPoolSendPromptBlocksUntilReady(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	// Script: wait 200ms, emit a system init message, then cat stdin to stdout.
	script := `sleep 0.2; echo '{"type":"system","subtype":"init"}'; cat`
	err := p.Spawn(id, "test-agent", []string{"sh", "-c", script}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}
	defer p.Kill(id)

	// SendPrompt should block until the system message arrives, then succeed.
	err = p.SendPrompt(id, "hello")
	if err != nil {
		t.Fatalf("SendPrompt() error: %v", err)
	}
}

func TestPoolSendPromptErrorOnEarlyExit(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	// Process exits immediately without emitting a system/assistant message.
	err := p.Spawn(id, "test-agent", []string{"sh", "-c", "exit 1"}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}

	err = p.SendPrompt(id, "hello")
	if err == nil {
		t.Error("SendPrompt() should return error when process exits before ready")
	}
}

func TestPoolSendPromptNotFound(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	err := p.SendPrompt(uuid.New(), "hello")
	if err == nil {
		t.Error("SendPrompt() should return error for unknown agent")
	}
}

func TestPoolStopAll(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal-based stop not supported on Windows")
	}

	p := NewPool("http://localhost:8080/mcp")
	ids := make([]uuid.UUID, 3)
	for i := range ids {
		ids[i] = uuid.New()
		err := p.Spawn(ids[i], "agent-"+string(rune('A'+i)), []string{"sleep", "60"}, nil, "")
		if err != nil {
			t.Fatalf("Spawn(%d) error: %v", i, err)
		}
	}

	p.StopAll()

	for i, id := range ids {
		if p.IsRunning(id) {
			t.Errorf("agent %d still running after StopAll()", i)
		}
	}
}

func TestPoolExitCode(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	err := p.Spawn(id, "test-agent", []string{"sh", "-c", "exit 42"}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}

	// Wait for process to exit.
	time.Sleep(500 * time.Millisecond)

	code := p.ExitCode(id)
	if code != 42 {
		t.Errorf("ExitCode() = %d, want 42", code)
	}
}

func TestPoolExitCodeNotFound(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	if p.ExitCode(uuid.New()) != -1 {
		t.Error("ExitCode() should return -1 for unknown agent")
	}
}

func TestPoolOutputSubscriber(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	var mu sync.Mutex
	var received [][]byte
	p.OutputSubscriber = func(agentID uuid.UUID, agentName string, data []byte) {
		mu.Lock()
		cp := make([]byte, len(data))
		copy(cp, data)
		received = append(received, cp)
		mu.Unlock()
	}

	script := `echo '{"type":"system","subtype":"init"}'
echo '{"type":"assistant","uuid":"u1"}'
echo '{"type":"result","subtype":"success"}'`

	err := p.Spawn(id, "test-agent", []string{"sh", "-c", script}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}

	// Wait for process to finish.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 3 {
		t.Fatalf("received %d messages, want 3", len(received))
	}
}

func TestPoolOnStreamMessage(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	var mu sync.Mutex
	var messages []StreamMessage
	p.OnStreamMessage = func(agentID uuid.UUID, msg StreamMessage) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
	}

	script := `echo '{"type":"assistant","uuid":"u1"}'
echo '{"type":"result","subtype":"success"}'`

	err := p.Spawn(id, "test-agent", []string{"sh", "-c", script}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(messages) < 2 {
		t.Fatalf("got %d stream messages, want at least 2", len(messages))
	}

	if messages[0].Type != "assistant" {
		t.Errorf("messages[0].Type = %q, want %q", messages[0].Type, "assistant")
	}
	if messages[1].Type != "result" {
		t.Errorf("messages[1].Type = %q, want %q", messages[1].Type, "result")
	}
}

func TestPoolOnExit(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	id := uuid.New()

	var gotID uuid.UUID
	var gotCode int
	var wg sync.WaitGroup
	wg.Add(1)
	p.OnExit = func(agentID uuid.UUID, exitCode int) {
		gotID = agentID
		gotCode = exitCode
		wg.Done()
	}

	err := p.Spawn(id, "test-agent", []string{"sh", "-c", "exit 3"}, nil, "")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}

	wg.Wait()

	if gotID != id {
		t.Errorf("OnExit agentID = %s, want %s", gotID, id)
	}
	if gotCode != 3 {
		t.Errorf("OnExit exitCode = %d, want 3", gotCode)
	}
}

func TestPoolIsRunningNotFound(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	if p.IsRunning(uuid.New()) {
		t.Error("IsRunning() should return false for unknown agent")
	}
}

func TestPoolStopNotFound(t *testing.T) {
	p := NewPool("http://localhost:8080/mcp")
	if err := p.Stop(uuid.New()); err == nil {
		t.Error("Stop() should return error for unknown agent")
	}
}

func TestPoolMCPURL(t *testing.T) {
	p := NewPool("http://localhost:9999/mcp")
	if p.MCPURL() != "http://localhost:9999/mcp" {
		t.Errorf("MCPURL() = %q, want %q", p.MCPURL(), "http://localhost:9999/mcp")
	}
}
