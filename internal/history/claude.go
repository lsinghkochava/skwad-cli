package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeProvider reads conversation history from Claude's JSONL session files.
//
// Sessions are stored in ~/.claude/projects/{encoded-path}/*.jsonl where
// {encoded-path} is the project's absolute path with '/' replaced by '-'.
type ClaudeProvider struct{}

// claudeEntry represents one line in a Claude JSONL session file.
type claudeEntry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
	Summary   string          `json:"summary"`
}

// claudeMessage holds the role and content fields for user/assistant messages.
type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []contentBlock
}

type claudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ListSessions returns summaries for all Claude sessions that belong to folder.
func (p *ClaudeProvider) ListSessions(folder string) ([]SessionSummary, error) {
	base, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	projectsDir := filepath.Join(base, ".claude", "projects")

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// Build candidate directory names for this folder.
	// Claude encodes the path by replacing '/' with '-'.
	encoded := strings.ReplaceAll(folder, "/", "-")

	var sessions []SessionSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !pathMatchesFolder(e.Name(), folder, encoded) {
			continue
		}
		dir := filepath.Join(projectsDir, e.Name())
		subs, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range subs {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(f.Name(), ".jsonl")
			sum, err := parseClaudeSession(filepath.Join(dir, f.Name()), sessionID)
			if err != nil {
				continue
			}
			sessions = append(sessions, sum)
		}
	}
	return sessions, nil
}

// DeleteSession removes the JSONL file for the given session ID.
func (p *ClaudeProvider) DeleteSession(sessionID string) error {
	base, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	projectsDir := filepath.Join(base, ".claude", "projects")

	// Walk all project dirs to find the file.
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		target := filepath.Join(projectsDir, e.Name(), sessionID+".jsonl")
		if _, err := os.Stat(target); err == nil {
			return os.Remove(target)
		}
	}
	return nil
}

// pathMatchesFolder checks whether a Claude project directory name corresponds
// to the given folder path. Claude encodes the path by replacing '/' with '-'.
func pathMatchesFolder(dirName, folder, encoded string) bool {
	// Direct encoded match (leading slash becomes leading dash).
	if dirName == encoded {
		return true
	}
	// Try trimming leading dash (some versions omit the leading slash).
	if strings.TrimPrefix(dirName, "-") == strings.TrimPrefix(encoded, "-") {
		return true
	}
	// Decode back by replacing '-' with '/' and compare.
	decoded := strings.ReplaceAll(dirName, "-", "/")
	if decoded == folder {
		return true
	}
	return false
}

// parseClaudeSession reads a JSONL file and extracts summary metadata.
func parseClaudeSession(path, sessionID string) (SessionSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return SessionSummary{}, err
	}
	defer f.Close()

	var (
		title        string
		latestTime   time.Time
		messageCount int
	)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry claudeEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		if t, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			if t.After(latestTime) {
				latestTime = t
			}
		}

		switch entry.Type {
		case "user":
			messageCount++
			if title == "" && len(entry.Message) > 0 {
				title = extractTitle(entry.Message)
			}
		case "assistant":
			messageCount++
		case "summary":
			// Use summary as title if no user message found yet.
			if title == "" && entry.Summary != "" {
				title = truncate(entry.Summary, 60)
			}
		}
	}

	if title == "" {
		title = "(empty session)"
	}
	return SessionSummary{
		SessionID:    sessionID,
		Title:        title,
		Timestamp:    latestTime,
		MessageCount: messageCount,
	}, nil
}

// extractTitle pulls the first human-readable text from a Claude message JSON.
func extractTitle(raw json.RawMessage) string {
	// Try plain string content.
	var msg claudeMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return ""
	}
	if msg.Role != "user" {
		return ""
	}
	// Content may be a plain string.
	var text string
	if err := json.Unmarshal(msg.Content, &text); err == nil {
		return truncate(text, 60)
	}
	// Or an array of content blocks.
	var blocks []claudeContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return truncate(b.Text, 60)
			}
		}
	}
	return ""
}

func truncate(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}
