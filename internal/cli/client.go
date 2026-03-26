package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// resolvePort returns MCP server port from: --port flag > SKWAD_MCP_PORT env > default 8766.
func resolvePort() int {
	// If the user explicitly set --port, that takes priority.
	// Cobra sets flagPort to default 8766, so check env var as fallback when flag is default.
	if flagPort != 8766 {
		return flagPort
	}
	if envPort := os.Getenv("SKWAD_MCP_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			return p
		}
	}
	return flagPort
}

// apiURL builds a full URL: http://127.0.0.1:{port}{path}
func apiURL(port int, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// apiGet does a GET request and returns body bytes and error.
func apiGet(port int, path string) ([]byte, error) {
	resp, err := httpClient.Get(apiURL(port, path))
	if err != nil {
		return nil, fmt.Errorf("connection refused — no daemon running on port %d", port)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// apiPost does a POST request with JSON body and returns body bytes and error.
func apiPost(port int, path string, body interface{}) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	resp, err := httpClient.Post(apiURL(port, path), "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("connection refused — no daemon running on port %d", port)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// isTerminal returns true if the given file is a terminal (for color output decisions).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
