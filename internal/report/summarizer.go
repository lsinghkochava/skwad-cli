package report

import (
	"fmt"
	"strings"
)

// SummaryConfig controls output truncation behavior.
type SummaryConfig struct {
	MaxLines  int // truncate above this (default 500)
	HeadLines int // keep first N lines (default 50)
	TailLines int // keep last N lines (default 50)
}

// DefaultSummaryConfig returns the default truncation settings.
func DefaultSummaryConfig() SummaryConfig {
	return SummaryConfig{
		MaxLines:  500,
		HeadLines: 50,
		TailLines: 50,
	}
}

// Truncate checks line count and returns truncated output if over threshold.
// Returns (output, wasTruncated).
func Truncate(output string, cfg SummaryConfig) (string, bool) {
	if output == "" {
		return output, false
	}
	lines := strings.Split(output, "\n")
	if len(lines) <= cfg.MaxLines {
		return output, false
	}
	if cfg.HeadLines+cfg.TailLines >= len(lines) {
		return output, false
	}
	head := strings.Join(lines[:cfg.HeadLines], "\n")
	tail := strings.Join(lines[len(lines)-cfg.TailLines:], "\n")
	truncated := len(lines) - cfg.HeadLines - cfg.TailLines
	return head + "\n\n[... " + fmt.Sprintf("%d", truncated) + " lines truncated ...]\n\n" + tail, true
}
