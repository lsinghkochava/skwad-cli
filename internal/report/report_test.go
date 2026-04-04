package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var sampleReport = &RunReport{
	Agents: []AgentResult{
		{Name: "Performance Analyst", Type: "claude", ExitCode: 0, Output: "Found 3 slow queries\n"},
		{Name: "Bug Hunter", Type: "claude", ExitCode: 1, Output: "Detected null pointer at line 42\n"},
	},
}

func TestFormatMarkdown(t *testing.T) {
	md := FormatMarkdown(sampleReport)

	if !strings.Contains(md, "# Skwad Run Report") {
		t.Error("missing report title")
	}
	if !strings.Contains(md, "## Performance Analyst (claude)") {
		t.Error("missing Performance Analyst section header")
	}
	if !strings.Contains(md, "## Bug Hunter (claude)") {
		t.Error("missing Bug Hunter section header")
	}
	if !strings.Contains(md, "Found 3 slow queries") {
		t.Error("missing Performance Analyst output")
	}
	if !strings.Contains(md, "Detected null pointer at line 42") {
		t.Error("missing Bug Hunter output")
	}
	// Verify output is wrapped in code fences.
	if strings.Count(md, "```") != 4 { // 2 agents * 2 fences each
		t.Errorf("expected 4 code fences, got %d", strings.Count(md, "```"))
	}
}

func TestFormatMarkdown_NoTrailingNewline(t *testing.T) {
	r := &RunReport{
		Agents: []AgentResult{
			{Name: "Bot", Type: "codex", Output: "no trailing newline"},
		},
	}
	md := FormatMarkdown(r)
	// Should still have proper code fence closure.
	if !strings.Contains(md, "no trailing newline\n```") {
		t.Error("expected newline added before closing fence")
	}
}

func TestFormatJSON(t *testing.T) {
	out, err := FormatJSON(sampleReport)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}

	// Verify it's valid JSON.
	var parsed RunReport
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(parsed.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(parsed.Agents))
	}
	if parsed.Agents[0].Name != "Performance Analyst" {
		t.Errorf("expected first agent 'Performance Analyst', got %q", parsed.Agents[0].Name)
	}

	// Verify it's pretty-printed (indented).
	if !strings.Contains(out, "  ") {
		t.Error("expected indented output")
	}
}

func TestLoadReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")

	data, _ := json.Marshal(sampleReport)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rr, err := LoadReport(path)
	if err != nil {
		t.Fatalf("LoadReport: %v", err)
	}
	if len(rr.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(rr.Agents))
	}
	if rr.Agents[1].ExitCode != 1 {
		t.Errorf("expected Bug Hunter exit_code=1, got %d", rr.Agents[1].ExitCode)
	}
}

func TestLoadReport_FileNotFound(t *testing.T) {
	_, err := LoadReport("/nonexistent/results.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadReportFromReader(t *testing.T) {
	data, _ := json.Marshal(sampleReport)
	rr, err := LoadReportFromReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("LoadReportFromReader: %v", err)
	}
	if len(rr.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(rr.Agents))
	}
}

func TestLoadReportFromReader_InvalidJSON(t *testing.T) {
	_, err := LoadReportFromReader(strings.NewReader("{bad json"))
	if err == nil || !strings.Contains(err.Error(), "parse report") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestLoadReportFromReader_EmptyAgents(t *testing.T) {
	_, err := LoadReportFromReader(strings.NewReader(`{"agents": []}`))
	if err == nil || !strings.Contains(err.Error(), "no agents") {
		t.Errorf("expected 'no agents' error, got %v", err)
	}
}

func TestBuildCommentBody(t *testing.T) {
	body := BuildCommentBody(sampleReport)

	// Must start with the marker.
	if !strings.HasPrefix(body, CommentMarker) {
		t.Errorf("comment body should start with marker %q", CommentMarker)
	}

	// Must contain the report content.
	if !strings.Contains(body, "# Skwad Run Report") {
		t.Error("missing report title in comment body")
	}
	if !strings.Contains(body, "Performance Analyst") {
		t.Error("missing agent section in comment body")
	}
}

func TestParsePRRef_SimpleNumber(t *testing.T) {
	owner, repo, num, err := parsePRRef("123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "" || repo != "" {
		t.Errorf("expected empty owner/repo for simple number, got %q/%q", owner, repo)
	}
	if num != "123" {
		t.Errorf("expected num '123', got %q", num)
	}
}

func TestParsePRRef_OwnerRepoHash(t *testing.T) {
	owner, repo, num, err := parsePRRef("anthropics/claude-code#456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "anthropics" || repo != "claude-code" || num != "456" {
		t.Errorf("expected anthropics/claude-code/456, got %s/%s/%s", owner, repo, num)
	}
}

func TestParsePRRef_URL(t *testing.T) {
	owner, repo, num, err := parsePRRef("https://github.com/org/myrepo/pull/789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "org" || repo != "myrepo" || num != "789" {
		t.Errorf("expected org/myrepo/789, got %s/%s/%s", owner, repo, num)
	}
}

func TestParsePRRef_Invalid(t *testing.T) {
	_, _, _, err := parsePRRef("invalid/ref/without/hash")
	if err == nil {
		t.Error("expected error for invalid PR ref")
	}
}

func TestFormatMarkdown_LongOutputTruncated(t *testing.T) {
	// Generate output with 600 lines (over default MaxLines=500)
	lines := make([]string, 600)
	for i := range lines {
		lines[i] = fmt.Sprintf("output line %d", i+1)
	}
	r := &RunReport{
		Agents: []AgentResult{
			{Name: "Verbose", Type: "claude", Output: strings.Join(lines, "\n")},
		},
	}

	md := FormatMarkdown(r)

	if !strings.Contains(md, "lines truncated") {
		t.Error("expected truncation marker in markdown for output over 500 lines")
	}
	// Should still contain head and tail
	if !strings.Contains(md, "output line 1") {
		t.Error("expected head lines in truncated output")
	}
	if !strings.Contains(md, "output line 600") {
		t.Error("expected tail lines in truncated output")
	}
}

func TestFormatMarkdown_ShortOutputUnchanged(t *testing.T) {
	r := &RunReport{
		Agents: []AgentResult{
			{Name: "Brief", Type: "claude", Output: "line1\nline2\nline3"},
		},
	}

	md := FormatMarkdown(r)

	if strings.Contains(md, "truncated") {
		t.Error("short output should NOT be truncated")
	}
	if !strings.Contains(md, "line1") || !strings.Contains(md, "line3") {
		t.Error("expected all lines present in output")
	}
}
