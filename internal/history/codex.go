package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CodexProvider reads conversation history from Codex session JSON files.
//
// Sessions are stored in ~/.codex/history/ as JSON files named by session UUID.
type CodexProvider struct{}

type codexSession struct {
	ID        string       `json:"id"`
	CreatedAt string       `json:"createdAt"`
	Messages  []codexMsg   `json:"messages"`
	WorkingDir string      `json:"workingDir"`
}

type codexMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ListSessions returns summaries for Codex sessions that match the given folder.
func (p *CodexProvider) ListSessions(folder string) ([]SessionSummary, error) {
	base, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	historyDir := filepath.Join(base, ".codex", "history")

	entries, err := os.ReadDir(historyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []SessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		sum, err := parseCodexSession(filepath.Join(historyDir, e.Name()), folder)
		if err != nil {
			continue
		}
		if sum.SessionID == "" {
			continue
		}
		sessions = append(sessions, sum)
	}
	return sessions, nil
}

// DeleteSession removes the session file for the given ID.
func (p *CodexProvider) DeleteSession(sessionID string) error {
	base, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	target := filepath.Join(base, ".codex", "history", sessionID+".json")
	return os.Remove(target)
}

func parseCodexSession(path, folder string) (SessionSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionSummary{}, err
	}
	var s codexSession
	if err := json.Unmarshal(data, &s); err != nil {
		return SessionSummary{}, err
	}

	// Skip sessions that don't belong to this folder.
	if s.WorkingDir != "" && !strings.HasPrefix(s.WorkingDir, folder) {
		return SessionSummary{}, nil
	}

	var ts time.Time
	if s.CreatedAt != "" {
		ts, _ = time.Parse(time.RFC3339, s.CreatedAt)
	}

	title := "(empty session)"
	for _, m := range s.Messages {
		if m.Role == "user" && m.Content != "" {
			title = truncate(m.Content, 60)
			break
		}
	}

	id := s.ID
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), ".json")
	}

	return SessionSummary{
		SessionID:    id,
		Title:        title,
		Timestamp:    ts,
		MessageCount: len(s.Messages),
	}, nil
}
