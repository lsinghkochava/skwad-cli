package daemon

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
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
