package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// logLine is a single entry in the activity log.
type logLine struct {
	timestamp time.Time
	agentName string
	text      string
	color     color.Color
}

// ActivityLog manages the scrollable activity log panel.
type ActivityLog struct {
	lines     []logLine
	scrollPos int
}

// NewActivityLog creates an empty activity log.
func NewActivityLog() *ActivityLog {
	return &ActivityLog{}
}

// Append adds new log entries from a LogEntryMsg, splitting multi-line data.
// assignColor is called to get the consistent color for the agent.
// viewHeight is the visible log area height for auto-scroll calculations.
func (al *ActivityLog) Append(msg LogEntryMsg, viewHeight int, assignColor func(string) color.Color) {
	c := assignColor(msg.AgentName)
	for _, line := range strings.Split(string(msg.Data), "\n") {
		if line == "" {
			continue
		}
		al.lines = append(al.lines, logLine{
			timestamp: time.Now(),
			agentName: msg.AgentName,
			text:      line,
			color:     c,
		})
	}

	// Auto-scroll to bottom if user is near the bottom.
	maxScroll := al.MaxScroll(viewHeight)
	if al.scrollPos >= maxScroll-2 || maxScroll <= 0 {
		al.scrollPos = al.MaxScroll(viewHeight)
	}
}

// ScrollUp scrolls the log up by one line.
func (al *ActivityLog) ScrollUp() {
	if al.scrollPos > 0 {
		al.scrollPos--
	}
}

// ScrollDown scrolls the log down by one line.
func (al *ActivityLog) ScrollDown(viewHeight int) {
	if al.scrollPos < al.MaxScroll(viewHeight) {
		al.scrollPos++
	}
}

// MaxScroll returns the maximum scroll position for the given view height.
func (al *ActivityLog) MaxScroll(viewHeight int) int {
	if viewHeight < 1 {
		viewHeight = 1
	}
	max := len(al.lines) - viewHeight
	if max < 0 {
		return 0
	}
	return max
}

// Render renders the visible portion of the activity log.
// If filterAgent is non-empty, only lines from that agent are shown.
func (al *ActivityLog) Render(width, height int, filterAgent string) string {
	var b strings.Builder

	// Build the visible set of lines (filtered or all).
	visible := al.lines
	if filterAgent != "" {
		visible = make([]logLine, 0, len(al.lines))
		for _, ll := range al.lines {
			if ll.agentName == filterAgent {
				visible = append(visible, ll)
			}
		}
	}

	start := al.scrollPos
	if start > len(visible) {
		start = len(visible)
	}
	end := start + height
	if end > len(visible) {
		end = len(visible)
	}

	linesRendered := 0
	for i := start; i < end; i++ {
		ll := visible[i]
		ts := ll.timestamp.Format("15:04:05")
		prefix := lipgloss.NewStyle().Foreground(ll.color).Render(fmt.Sprintf("[%s]", ll.agentName))
		line := fmt.Sprintf(" %s %s %s", ts, prefix, ll.text)
		if len(line) > width {
			line = line[:width]
		}
		b.WriteString(line)
		b.WriteString("\n")
		linesRendered++
	}

	// Fill remaining lines.
	for i := linesRendered; i < height; i++ {
		b.WriteString("\n")
	}

	return b.String()
}

// Lines returns the log lines for testing/inspection.
func (al *ActivityLog) Lines() []logLine {
	return al.lines
}

// ScrollPos returns the current scroll position for testing/inspection.
func (al *ActivityLog) ScrollPos() int {
	return al.scrollPos
}

// ResetScroll resets the scroll position to the top.
func (al *ActivityLog) ResetScroll() {
	al.scrollPos = 0
}
