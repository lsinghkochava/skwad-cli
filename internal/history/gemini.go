package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GeminiProvider reads conversation history from Gemini CLI session files.
//
// The Google Gemini CLI stores sessions in ~/.gemini/tmp/ and ~/.gemini/sessions/
// as JSON files. Each file contains a conversation session.
type GeminiProvider struct{}

type geminiSession struct {
	ID         string      `json:"id"`
	CreatedAt  string      `json:"createdAt"`
	WorkingDir string      `json:"workingDir"`
	Messages   []geminiMsg `json:"messages"`
	Title      string      `json:"title"`
}

type geminiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Parts   []struct {
		Text string `json:"text"`
	} `json:"parts"`
}

// ListSessions returns summaries for Gemini sessions matching the given folder.
func (p *GeminiProvider) ListSessions(folder string) ([]SessionSummary, error) {
	base, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Gemini CLI may use ~/.gemini/sessions/ or ~/.gemini/tmp/.
	searchDirs := []string{
		filepath.Join(base, ".gemini", "sessions"),
		filepath.Join(base, ".gemini", "tmp"),
		filepath.Join(base, ".gemini"),
	}

	var sessions []SessionSummary
	seen := map[string]bool{}

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if seen[path] {
				continue
			}
			seen[path] = true

			sum, err := parseGeminiSession(path, folder)
			if err != nil || sum.SessionID == "" {
				continue
			}
			sessions = append(sessions, sum)
		}
	}
	return sessions, nil
}

// DeleteSession removes the session file for the given ID.
func (p *GeminiProvider) DeleteSession(sessionID string) error {
	base, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, dir := range []string{
		filepath.Join(base, ".gemini", "sessions"),
		filepath.Join(base, ".gemini", "tmp"),
		filepath.Join(base, ".gemini"),
	} {
		target := filepath.Join(dir, sessionID+".json")
		if _, err := os.Stat(target); err == nil {
			return os.Remove(target)
		}
	}
	return nil
}

func parseGeminiSession(path, folder string) (SessionSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionSummary{}, err
	}

	var s geminiSession
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
	if ts.IsZero() {
		// Fallback to file mod time.
		if info, err := os.Stat(path); err == nil {
			ts = info.ModTime()
		}
	}

	title := s.Title
	if title == "" {
		for _, m := range s.Messages {
			if m.Role == "user" {
				if m.Content != "" {
					title = truncate(m.Content, 60)
					break
				}
				for _, part := range m.Parts {
					if part.Text != "" {
						title = truncate(part.Text, 60)
						break
					}
				}
				if title != "" {
					break
				}
			}
		}
	}
	if title == "" {
		title = "(empty session)"
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
