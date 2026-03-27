package cli

import (
	"os"
	"testing"
)

func TestResolvePort_Default(t *testing.T) {
	// Ensure env var is unset and flag is at default.
	os.Unsetenv("SKWAD_MCP_PORT")
	flagPort = 8777
	if got := resolvePort(); got != 8777 {
		t.Errorf("expected 8777, got %d", got)
	}
}

func TestResolvePort_EnvVar(t *testing.T) {
	flagPort = 8777 // default
	t.Setenv("SKWAD_MCP_PORT", "9999")
	if got := resolvePort(); got != 9999 {
		t.Errorf("expected 9999, got %d", got)
	}
}

func TestResolvePort_FlagOverridesEnv(t *testing.T) {
	flagPort = 7777
	t.Setenv("SKWAD_MCP_PORT", "9999")
	defer func() { flagPort = 8777 }()

	if got := resolvePort(); got != 7777 {
		t.Errorf("expected 7777 (flag), got %d", got)
	}
}

func TestResolvePort_InvalidEnvFallsBack(t *testing.T) {
	flagPort = 8777
	t.Setenv("SKWAD_MCP_PORT", "not-a-number")
	if got := resolvePort(); got != 8777 {
		t.Errorf("expected 8777 (fallback), got %d", got)
	}
}

func TestApiURL(t *testing.T) {
	got := apiURL(8777, "/health")
	want := "http://127.0.0.1:8777/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestApiURL_CustomPort(t *testing.T) {
	got := apiURL(9000, "/api/v1/agent/send")
	want := "http://127.0.0.1:9000/api/v1/agent/send"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}
