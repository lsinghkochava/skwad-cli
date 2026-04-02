package tui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/google/uuid"
)

// testColorAssigner returns a simple color assigner for testing.
func testColorAssigner() func(string) color.Color {
	colors := map[string]color.Color{}
	idx := 0
	return func(name string) color.Color {
		if c, ok := colors[name]; ok {
			return c
		}
		c := agentColors[idx%len(agentColors)]
		idx++
		colors[name] = c
		return c
	}
}

func TestNewActivityLog_EmptyState(t *testing.T) {
	al := NewActivityLog()

	if len(al.Lines()) != 0 {
		t.Errorf("Lines() = %d, want 0", len(al.Lines()))
	}
	if al.ScrollPos() != 0 {
		t.Errorf("ScrollPos() = %d, want 0", al.ScrollPos())
	}
}

func TestActivityLog_Append_SingleLine(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("hello world")}
	al.Append(msg, 10, assign)

	if len(al.Lines()) != 1 {
		t.Fatalf("Lines() = %d, want 1", len(al.Lines()))
	}
	if al.Lines()[0].agentName != "Coder" {
		t.Errorf("agentName = %q, want %q", al.Lines()[0].agentName, "Coder")
	}
	if al.Lines()[0].text != "hello world" {
		t.Errorf("text = %q, want %q", al.Lines()[0].text, "hello world")
	}
	if al.Lines()[0].timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestActivityLog_Append_MultiLine(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("line1\nline2\nline3")}
	al.Append(msg, 10, assign)

	if len(al.Lines()) != 3 {
		t.Fatalf("Lines() = %d, want 3", len(al.Lines()))
	}
	if al.Lines()[0].text != "line1" {
		t.Errorf("line 0 = %q, want %q", al.Lines()[0].text, "line1")
	}
	if al.Lines()[2].text != "line3" {
		t.Errorf("line 2 = %q, want %q", al.Lines()[2].text, "line3")
	}
}

func TestActivityLog_Append_EmptyData(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("")}
	al.Append(msg, 10, assign)

	if len(al.Lines()) != 0 {
		t.Errorf("Lines() = %d, want 0 for empty data", len(al.Lines()))
	}
}

func TestActivityLog_Append_EmptyLinesSkipped(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("a\n\n\nb")}
	al.Append(msg, 10, assign)

	if len(al.Lines()) != 2 {
		t.Errorf("Lines() = %d, want 2 (empty lines skipped)", len(al.Lines()))
	}
}

func TestActivityLog_ScrollDown_Increments(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	// Add 30 lines to allow scrolling with viewHeight=10.
	for i := 0; i < 30; i++ {
		al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}, 10, assign)
	}

	// Reset scroll to 0 for a clean test.
	al.scrollPos = 0

	al.ScrollDown(10)
	if al.ScrollPos() != 1 {
		t.Errorf("ScrollPos() = %d, want 1", al.ScrollPos())
	}
}

func TestActivityLog_ScrollDown_ClampedAtMax(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	// Add 15 lines, viewHeight=10 → maxScroll = 5.
	for i := 0; i < 15; i++ {
		al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}, 10, assign)
	}

	// Scroll far past max.
	for i := 0; i < 100; i++ {
		al.ScrollDown(10)
	}

	max := al.MaxScroll(10)
	if al.ScrollPos() != max {
		t.Errorf("ScrollPos() = %d, want max %d", al.ScrollPos(), max)
	}
}

func TestActivityLog_ScrollUp_Decrements(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	for i := 0; i < 20; i++ {
		al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}, 10, assign)
	}

	// Position at scroll 5.
	al.scrollPos = 5
	al.ScrollUp()
	if al.ScrollPos() != 4 {
		t.Errorf("ScrollPos() = %d, want 4", al.ScrollPos())
	}
}

func TestActivityLog_ScrollUp_ClampedAtZero(t *testing.T) {
	al := NewActivityLog()

	al.ScrollUp()
	if al.ScrollPos() != 0 {
		t.Errorf("ScrollPos() = %d, want 0", al.ScrollPos())
	}
}

func TestActivityLog_AutoScroll_NearBottom(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	// Add lines until scrollable, staying at auto-scroll position.
	for i := 0; i < 20; i++ {
		al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}, 10, assign)
	}

	// scrollPos should be at maxScroll (auto-scrolled).
	max := al.MaxScroll(10)
	if al.ScrollPos() != max {
		t.Errorf("auto-scroll: ScrollPos() = %d, want max %d", al.ScrollPos(), max)
	}

	// Append another line — should still auto-scroll.
	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("new")}, 10, assign)
	max = al.MaxScroll(10)
	if al.ScrollPos() != max {
		t.Errorf("auto-scroll after append: ScrollPos() = %d, want max %d", al.ScrollPos(), max)
	}
}

func TestActivityLog_NoAutoScroll_WhenScrolledUp(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	for i := 0; i < 20; i++ {
		al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}, 10, assign)
	}

	// Scroll up significantly (away from bottom).
	al.scrollPos = 0

	// Append new line — should NOT auto-scroll since we're far from bottom.
	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("new")}, 10, assign)
	if al.ScrollPos() != 0 {
		t.Errorf("should not auto-scroll when user scrolled up, got ScrollPos() = %d", al.ScrollPos())
	}
}

func TestActivityLog_Render_EmptyLines(t *testing.T) {
	al := NewActivityLog()

	out := al.Render(80, 5, "")
	// Should be 5 empty lines.
	if strings.Count(out, "\n") != 5 {
		t.Errorf("empty render should have 5 newlines, got %d", strings.Count(out, "\n"))
	}
}

func TestActivityLog_Render_VisibleWindow(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	// Add 10 lines.
	for i := 0; i < 10; i++ {
		msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "Agent", Data: []byte("line")}
		al.Append(msg, 5, assign)
	}

	// Render with height 5 at scroll position 0.
	al.scrollPos = 0
	out := al.Render(80, 5, "")

	// Should contain "line" text and agent name.
	if !strings.Contains(out, "line") {
		t.Error("rendered log should contain 'line'")
	}
	if !strings.Contains(out, "Agent") {
		t.Error("rendered log should contain agent name")
	}
}

func TestActivityLog_ColorConsistency(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	msg1 := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("first")}
	msg2 := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("second")}
	al.Append(msg1, 10, assign)
	al.Append(msg2, 10, assign)

	lines := al.Lines()
	if lines[0].color != lines[1].color {
		t.Error("same agent should get same color")
	}
}

func TestActivityLog_MaxScroll_NoLines(t *testing.T) {
	al := NewActivityLog()

	if al.MaxScroll(10) != 0 {
		t.Errorf("MaxScroll with no lines = %d, want 0", al.MaxScroll(10))
	}
}

func TestActivityLog_MaxScroll_LessLinesThanHeight(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("one")}, 10, assign)

	if al.MaxScroll(10) != 0 {
		t.Errorf("MaxScroll with 1 line and height 10 = %d, want 0", al.MaxScroll(10))
	}
}

func TestActivityLog_MaxScroll_ZeroViewHeight(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	for i := 0; i < 5; i++ {
		al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}, 10, assign)
	}

	// viewHeight 0 should be treated as 1.
	max := al.MaxScroll(0)
	if max != 4 {
		t.Errorf("MaxScroll(0) = %d, want 4 (5 lines - 1)", max)
	}
}

func TestActivityLog_Render_FilterAgent(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("code line")}, 10, assign)
	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "Tester", Data: []byte("test line")}, 10, assign)
	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("more code")}, 10, assign)

	// Filter to Coder only.
	out := al.Render(80, 10, "Coder")
	if !strings.Contains(out, "code line") {
		t.Error("filtered render should contain Coder's lines")
	}
	if strings.Contains(out, "test line") {
		t.Error("filtered render should NOT contain Tester's lines")
	}
}

func TestActivityLog_Render_FilterNoMatch(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("line")}, 10, assign)

	// Filter to nonexistent agent.
	out := al.Render(80, 5, "NonExistent")
	// Should be all empty lines.
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			t.Errorf("filtered render with no match should be empty, got %q", line)
		}
	}
}

func TestActivityLog_Render_EmptyFilterShowsAll(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("code")}, 10, assign)
	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "Tester", Data: []byte("test")}, 10, assign)

	out := al.Render(80, 10, "")
	if !strings.Contains(out, "code") {
		t.Error("unfiltered render should contain Coder lines")
	}
	if !strings.Contains(out, "test") {
		t.Error("unfiltered render should contain Tester lines")
	}
}

func TestActivityLog_ResetScroll(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	for i := 0; i < 30; i++ {
		al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}, 10, assign)
	}

	// Scroll should be non-zero (auto-scrolled to bottom).
	if al.ScrollPos() == 0 {
		t.Fatal("scrollPos should be non-zero after many appends")
	}

	al.ResetScroll()
	if al.ScrollPos() != 0 {
		t.Errorf("ResetScroll() should set scrollPos to 0, got %d", al.ScrollPos())
	}
}

func TestActivityLog_ResetScroll_AfterManualScroll(t *testing.T) {
	al := NewActivityLog()
	assign := testColorAssigner()

	for i := 0; i < 20; i++ {
		al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}, 10, assign)
	}

	// Manually scroll down.
	al.scrollPos = 5
	if al.ScrollPos() != 5 {
		t.Fatal("scrollPos should be 5")
	}

	al.ResetScroll()
	if al.ScrollPos() != 0 {
		t.Errorf("ResetScroll() after manual scroll should be 0, got %d", al.ScrollPos())
	}
}

func TestActivityLog_Render_TruncatesLongLines(t *testing.T) {
	al := NewActivityLog()

	longText := strings.Repeat("x", 200)
	assign := func(name string) color.Color {
		return lipgloss.Color("#00CCCC")
	}

	al.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte(longText)}, 10, assign)

	out := al.Render(80, 5, "")
	// Each rendered line should not exceed width.
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 80 {
			t.Errorf("line length %d exceeds width 80", len(line))
		}
	}
}
