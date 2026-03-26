package terminal

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSession_SpawnAndExit(t *testing.T) {
	s, err := NewSession("exit 0", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.Start()
	defer s.Kill()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("process did not exit within 3s")
		case <-time.After(50 * time.Millisecond):
			if !s.IsRunning() {
				return
			}
		}
	}
}

func TestSession_OnExitCallback(t *testing.T) {
	var (
		mu      sync.Mutex
		gotCode int
		called  bool
	)

	s, err := NewSession("exit 42", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.OnExit = func(code int) {
		mu.Lock()
		gotCode = code
		called = true
		mu.Unlock()
	}
	s.Start()
	defer s.Kill()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("OnExit not called within 3s")
		case <-time.After(50 * time.Millisecond):
			mu.Lock()
			done := called
			mu.Unlock()
			if done {
				if gotCode != 42 {
					t.Errorf("exit code: got %d, want 42", gotCode)
				}
				return
			}
		}
	}
}

func TestSession_OutputCallback(t *testing.T) {
	var (
		mu  sync.Mutex
		out strings.Builder
	)

	s, err := NewSession("echo hello_skwad", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.OnOutput = func(data []byte) {
		mu.Lock()
		out.Write(data)
		mu.Unlock()
	}
	s.Start()
	defer s.Kill()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("did not receive expected output within 3s, got: %q", out.String())
		case <-time.After(50 * time.Millisecond):
			mu.Lock()
			got := out.String()
			mu.Unlock()
			if strings.Contains(got, "hello_skwad") {
				return
			}
		}
	}
}

func TestSession_InjectText(t *testing.T) {
	var (
		mu  sync.Mutex
		out strings.Builder
	)

	s, err := NewSession("cat", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.OnOutput = func(data []byte) {
		mu.Lock()
		out.Write(data)
		mu.Unlock()
	}
	s.Start()
	defer s.Kill()

	time.Sleep(100 * time.Millisecond)
	s.InjectText("injected_marker")

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("injected text not echoed within 3s, got: %q", out.String())
		case <-time.After(50 * time.Millisecond):
			mu.Lock()
			got := out.String()
			mu.Unlock()
			if strings.Contains(got, "injected_marker") {
				return
			}
		}
	}
}

func TestSession_Kill(t *testing.T) {
	s, err := NewSession("sleep 60", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.Start()

	if !s.IsRunning() {
		t.Fatal("process should be running")
	}
	s.Kill()

	time.Sleep(200 * time.Millisecond)
	if s.IsRunning() {
		t.Error("process should not be running after Kill")
	}
}

func TestSession_Resize(t *testing.T) {
	s, err := NewSession("sleep 1", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.Start()
	defer s.Kill()

	// Should not panic.
	s.Resize(120, 40)
}

func TestSession_ExitCode(t *testing.T) {
	s, err := NewSession("exit 7", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s.Start()
	defer s.Kill()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("process did not exit within 3s")
		case <-time.After(50 * time.Millisecond):
			if !s.IsRunning() {
				code := s.ExitCode()
				if code != 7 {
					t.Errorf("exit code: got %d, want 7", code)
				}
				return
			}
		}
	}
}
