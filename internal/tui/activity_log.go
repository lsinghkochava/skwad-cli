package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// maxLines is the maximum number of lines retained in the activity log ring buffer.
const maxLines = 10000

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

	// Evict oldest lines if we exceed the ring buffer limit.
	if len(al.lines) > maxLines {
		evicted := len(al.lines) - maxLines
		al.lines = al.lines[evicted:]
		al.scrollPos -= evicted
		if al.scrollPos < 0 {
			al.scrollPos = 0
		}
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

// PageUp scrolls the log up by one page (viewHeight lines).
func (al *ActivityLog) PageUp(viewHeight int) {
	al.scrollPos -= viewHeight
	if al.scrollPos < 0 {
		al.scrollPos = 0
	}
}

// PageDown scrolls the log down by one page (viewHeight lines).
func (al *ActivityLog) PageDown(viewHeight int) {
	al.scrollPos += viewHeight
	if max := al.MaxScroll(viewHeight); al.scrollPos > max {
		al.scrollPos = max
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

// Render renders the visible portion of the activity log inside a bordered frame.
// If filterAgent is non-empty, only lines from that agent are shown.
// height is the total height available including the border and header.
func (al *ActivityLog) Render(width, height int, filterAgent string) string {
	// Border (top+bottom) + header = 3 lines of overhead.
	innerHeight := height - 3
	if innerHeight < 1 {
		innerHeight = 1
	}
	// Border left+right = 2 columns of overhead.
	innerWidth := width - 2
	if innerWidth < 1 {
		innerWidth = 1
	}

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

	// When filtering, scrollPos indexes into al.lines (full set) but we display
	// from the filtered visible slice. Clamp to the filtered range.
	filteredMax := len(visible) - innerHeight
	if filteredMax < 0 {
		filteredMax = 0
	}
	scrollOffset := al.scrollPos
	if scrollOffset > filteredMax {
		scrollOffset = filteredMax
	}

	// Header line: title on left, scroll indicator on right.
	title := "Activity Log"
	if filterAgent != "" {
		title = fmt.Sprintf("Activity Log [%s]", filterAgent)
	}
	scrollInfo := fmt.Sprintf("[%d/%d]", scrollOffset, filteredMax)
	padding := innerWidth - len(title) - len(scrollInfo)
	if padding < 1 {
		padding = 1
	}
	header := title + strings.Repeat(" ", padding) + scrollInfo

	start := scrollOffset
	if start > len(visible) {
		start = len(visible)
	}
	end := start + innerHeight
	if end > len(visible) {
		end = len(visible)
	}

	var content strings.Builder
	content.WriteString(header)
	content.WriteString("\n")

	linesRendered := 0
	for i := start; i < end; i++ {
		ll := visible[i]
		ts := ll.timestamp.Format("15:04:05")
		// Build plain text first, truncate, then apply ANSI styling to avoid
		// cutting mid-escape-sequence.
		agentTag := fmt.Sprintf("[%s]", ll.agentName)
		plainLine := fmt.Sprintf(" %s %s %s", ts, agentTag, ll.text)
		if len(plainLine) > innerWidth {
			plainLine = plainLine[:innerWidth]
		}
		// Re-apply color to the agent tag portion within the truncated line.
		styledLine := strings.Replace(plainLine, agentTag, lipgloss.NewStyle().Foreground(ll.color).Render(agentTag), 1)
		content.WriteString(styledLine)
		content.WriteString("\n")
		linesRendered++
	}

	// Fill remaining lines.
	for i := linesRendered; i < innerHeight; i++ {
		content.WriteString("\n")
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(innerWidth)

	return borderStyle.Render(content.String())
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
