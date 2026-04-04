package report

import (
	"fmt"
	"strings"
	"testing"
)

func TestTruncate_UnderThreshold(t *testing.T) {
	cfg := SummaryConfig{MaxLines: 10, HeadLines: 3, TailLines: 3}
	input := "line1\nline2\nline3\nline4\nline5"
	out, truncated := Truncate(input, cfg)
	if truncated {
		t.Error("expected truncated=false for input under threshold")
	}
	if out != input {
		t.Errorf("expected output unchanged, got %q", out)
	}
}

func TestTruncate_OverThreshold(t *testing.T) {
	cfg := SummaryConfig{MaxLines: 5, HeadLines: 2, TailLines: 2}
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	input := strings.Join(lines, "\n")
	out, truncated := Truncate(input, cfg)
	if !truncated {
		t.Error("expected truncated=true for input over threshold")
	}
	if !strings.Contains(out, "line1") {
		t.Error("expected head to contain line1")
	}
	if !strings.Contains(out, "line2") {
		t.Error("expected head to contain line2")
	}
	if !strings.Contains(out, "line9") {
		t.Error("expected tail to contain line9")
	}
	if !strings.Contains(out, "line10") {
		t.Error("expected tail to contain line10")
	}
	if !strings.Contains(out, "[... 6 lines truncated ...]") {
		t.Errorf("expected truncation marker with 6 lines, got %q", out)
	}
}

func TestTruncate_ExactlyAtThreshold(t *testing.T) {
	cfg := SummaryConfig{MaxLines: 5, HeadLines: 2, TailLines: 2}
	lines := make([]string, 5)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	input := strings.Join(lines, "\n")
	out, truncated := Truncate(input, cfg)
	if truncated {
		t.Error("expected truncated=false for input exactly at threshold")
	}
	if out != input {
		t.Errorf("expected output unchanged, got %q", out)
	}
}

func TestTruncate_EmptyOutput(t *testing.T) {
	cfg := DefaultSummaryConfig()
	out, truncated := Truncate("", cfg)
	if truncated {
		t.Error("expected truncated=false for empty input")
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestTruncate_CorrectTruncatedCount(t *testing.T) {
	cfg := SummaryConfig{MaxLines: 10, HeadLines: 3, TailLines: 3}
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	input := strings.Join(lines, "\n")
	out, truncated := Truncate(input, cfg)
	if !truncated {
		t.Error("expected truncated=true")
	}
	// 20 total - 3 head - 3 tail = 14 truncated
	if !strings.Contains(out, "[... 14 lines truncated ...]") {
		t.Errorf("expected 14 truncated lines in marker, got %q", out)
	}
}

func TestTruncate_SingleLine(t *testing.T) {
	cfg := SummaryConfig{MaxLines: 5, HeadLines: 2, TailLines: 2}
	input := "only one line"
	out, truncated := Truncate(input, cfg)
	if truncated {
		t.Error("single line should not be truncated")
	}
	if out != input {
		t.Errorf("expected output unchanged, got %q", out)
	}
}

func TestTruncate_HeadPlusTailExceedsMaxLines(t *testing.T) {
	// HeadLines(4) + TailLines(4) = 8, but MaxLines = 5
	// With 10 lines input, it IS over threshold so truncation triggers.
	// The head/tail slicing still works because len(lines)=10 > HeadLines and TailLines.
	cfg := SummaryConfig{MaxLines: 5, HeadLines: 4, TailLines: 4}
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	input := strings.Join(lines, "\n")
	out, truncated := Truncate(input, cfg)
	if !truncated {
		t.Error("expected truncated=true")
	}
	// Should contain head lines
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line4") {
		t.Error("expected head lines 1-4")
	}
	// Should contain tail lines
	if !strings.Contains(out, "line7") || !strings.Contains(out, "line10") {
		t.Error("expected tail lines 7-10")
	}
	// Truncated count: 10 - 4 - 4 = 2
	if !strings.Contains(out, "[... 2 lines truncated ...]") {
		t.Errorf("expected 2 truncated lines, got %q", out)
	}
}

func TestTruncate_DefaultSummaryConfig(t *testing.T) {
	cfg := DefaultSummaryConfig()
	if cfg.MaxLines != 500 {
		t.Errorf("expected MaxLines=500, got %d", cfg.MaxLines)
	}
	if cfg.HeadLines != 50 {
		t.Errorf("expected HeadLines=50, got %d", cfg.HeadLines)
	}
	if cfg.TailLines != 50 {
		t.Errorf("expected TailLines=50, got %d", cfg.TailLines)
	}
}

func TestTruncate_JustOverThreshold(t *testing.T) {
	cfg := SummaryConfig{MaxLines: 5, HeadLines: 2, TailLines: 2}
	lines := make([]string, 6)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	input := strings.Join(lines, "\n")
	out, truncated := Truncate(input, cfg)
	if !truncated {
		t.Error("expected truncated=true for 6 lines with MaxLines=5")
	}
	// 6 - 2 - 2 = 2 truncated
	if !strings.Contains(out, "[... 2 lines truncated ...]") {
		t.Errorf("expected 2 truncated lines, got %q", out)
	}
	if !strings.Contains(out, "line1") || !strings.Contains(out, "line2") {
		t.Error("expected head lines")
	}
	if !strings.Contains(out, "line5") || !strings.Contains(out, "line6") {
		t.Error("expected tail lines")
	}
	// Middle lines should NOT be present
	if strings.Contains(out, "line3\n") && !strings.Contains(out, "truncated") {
		t.Error("middle lines should be truncated")
	}
}

func TestTruncate_OverlapGuard_HeadTailCoverAllLines(t *testing.T) {
	// HeadLines(5) + TailLines(5) = 10, which >= len(lines)=8
	// Overlap guard should return output unchanged
	cfg := SummaryConfig{MaxLines: 5, HeadLines: 5, TailLines: 5}
	lines := make([]string, 8)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	input := strings.Join(lines, "\n")
	out, truncated := Truncate(input, cfg)
	if truncated {
		t.Error("overlap guard should prevent truncation when HeadLines+TailLines >= len(lines)")
	}
	if out != input {
		t.Errorf("expected output unchanged with overlap guard, got %q", out)
	}
}

func TestTruncate_OverlapGuard_ExactlyEqual(t *testing.T) {
	// HeadLines(3) + TailLines(3) = 6, exactly equal to len(lines)=6
	cfg := SummaryConfig{MaxLines: 5, HeadLines: 3, TailLines: 3}
	lines := make([]string, 6)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	input := strings.Join(lines, "\n")
	out, truncated := Truncate(input, cfg)
	if truncated {
		t.Error("overlap guard should prevent truncation when HeadLines+TailLines == len(lines)")
	}
	if out != input {
		t.Errorf("expected output unchanged, got %q", out)
	}
}
