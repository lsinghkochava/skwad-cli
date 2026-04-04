// Package report handles formatting and outputting agent run reports.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// AgentResult holds the output and exit status for a single agent run.
type AgentResult struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

// RunReport is the top-level report structure output by `skwad run`.
type RunReport struct {
	Agents []AgentResult `json:"agents"`
}

// LoadReport reads a RunReport from a JSON file.
func LoadReport(path string) (*RunReport, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open report: %w", err)
	}
	defer f.Close()
	return LoadReportFromReader(f)
}

// LoadReportFromReader reads a RunReport from an io.Reader.
func LoadReportFromReader(r io.Reader) (*RunReport, error) {
	var rr RunReport
	if err := json.NewDecoder(r).Decode(&rr); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}
	if len(rr.Agents) == 0 {
		return nil, fmt.Errorf("report contains no agents")
	}
	return &rr, nil
}

// FormatMarkdown renders the report as a markdown document.
func FormatMarkdown(r *RunReport) string {
	var sb strings.Builder
	sb.WriteString("# Skwad Run Report\n\n")
	for _, a := range r.Agents {
		fmt.Fprintf(&sb, "## %s (%s)\n", a.Name, a.Type)
		sb.WriteString("```\n")
		cfg := DefaultSummaryConfig()
		output, _ := Truncate(a.Output, cfg)
		sb.WriteString(output)
		if !strings.HasSuffix(output, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n\n")
	}
	return sb.String()
}

// FormatJSON renders the report as pretty-printed JSON.
func FormatJSON(r *RunReport) (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}
	return string(data) + "\n", nil
}

// CommentMarker is prepended to PR comments so we can find and replace them.
const CommentMarker = "<!-- skwad-review -->"

// BuildCommentBody creates the full PR comment body with marker.
func BuildCommentBody(r *RunReport) string {
	md := FormatMarkdown(r)
	return CommentMarker + "\n" + md
}
