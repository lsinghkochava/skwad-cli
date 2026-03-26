package terminal

import (
	"regexp"
	"strings"
)

// ansiEscape matches ANSI/VT100 escape sequences.
// OSC (] ... BEL or ] ... ST) must come before the generic C1 catch-all
// because ']' (0x5D) falls inside the [\x5C-\x5F] range.
var ansiEscape = regexp.MustCompile(`\x1b(?:\][^\x07\x1b]*(?:\x07|\x1b\\)|\[[0-?]*[ -/]*[@-~]|[@-Z\\-_])`)

// StripANSI removes ANSI escape sequences from s.
func StripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// spinnerChars are leading characters commonly emitted by AI CLIs to indicate
// activity. They are stripped from terminal titles before display.
var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏", "◐", "◓", "◑", "◒", "⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷", "⠁", "⠂", "⠄", "⡀", "⢀", "⠠", "⠐", "⠈", "✓", "✗", "●", "○", "►", "▶", "…", "·"}

// CleanTitle strips ANSI sequences and leading spinner characters from a
// terminal title string, returning a clean display name.
func CleanTitle(title string) string {
	title = StripANSI(title)
	title = strings.TrimSpace(title)

	for _, ch := range spinnerChars {
		title = strings.TrimPrefix(title, ch)
		title = strings.TrimSpace(title)
	}

	// Strip common status prefixes like "[running] " or "(idle) "
	for _, prefix := range []string{"[running]", "[idle]", "[input]", "[error]", "(running)", "(idle)"} {
		if strings.HasPrefix(strings.ToLower(title), prefix) {
			title = strings.TrimSpace(title[len(prefix):])
		}
	}

	return title
}

// CleanOutput strips ANSI escape sequences, carriage-return overwrites, and
// non-printable control characters from raw terminal output. The result is
// clean, loggable text suitable for display in --watch mode.
func CleanOutput(data []byte) []byte {
	s := StripANSI(string(data))

	// Handle \r without \n (TUI line overwrites): keep only the last
	// segment after each \r that isn't followed by \n.
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if idx := strings.LastIndex(line, "\r"); idx >= 0 {
			line = line[idx+1:]
		}
		lines = append(lines, line)
	}
	s = strings.Join(lines, "\n")

	// Strip remaining non-printable control chars (except \n and \t).
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' || r >= 32 {
			b.WriteRune(r)
		}
	}

	return []byte(b.String())
}
