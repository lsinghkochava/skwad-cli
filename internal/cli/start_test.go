package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestLineWriter_CompleteLine(t *testing.T) {
	var buf bytes.Buffer
	lw := &lineWriter{prefix: "Bot", color: "\033[36m", out: &buf}
	lw.write([]byte("hello world\n"))

	got := buf.String()
	if !strings.Contains(got, "[Bot]") {
		t.Errorf("expected prefix [Bot] in output, got %q", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", got)
	}
}

func TestLineWriter_PartialLineBuffered(t *testing.T) {
	var buf bytes.Buffer
	lw := &lineWriter{prefix: "Bot", color: "", out: &buf}

	// Write partial line — should not emit anything.
	lw.write([]byte("partial"))
	if buf.Len() != 0 {
		t.Errorf("expected no output for partial line, got %q", buf.String())
	}

	// Complete the line.
	lw.write([]byte(" end\n"))
	if !strings.Contains(buf.String(), "partial end") {
		t.Errorf("expected 'partial end' after completing line, got %q", buf.String())
	}
}

func TestLineWriter_MultipleLines(t *testing.T) {
	var buf bytes.Buffer
	lw := &lineWriter{prefix: "Bot", color: "", out: &buf}

	lw.write([]byte("line1\nline2\nline3\n"))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	for i, expected := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(lines[i], expected) {
			t.Errorf("line %d: expected %q, got %q", i, expected, lines[i])
		}
	}
}

func TestLineWriter_MultipleLinesInOneChunk(t *testing.T) {
	var buf bytes.Buffer
	lw := &lineWriter{prefix: "X", color: "", out: &buf}

	// One write with two complete lines and one partial.
	lw.write([]byte("a\nb\nc"))
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 emitted lines (partial 'c' buffered), got %d: %v", len(lines), lines)
	}

	// Verify partial is still buffered.
	if len(lw.buf) == 0 || string(lw.buf) != "c" {
		t.Errorf("expected 'c' in buffer, got %q", string(lw.buf))
	}
}

func TestLineWriter_ConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	lw := &lineWriter{prefix: "Race", color: "", out: &buf}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lw.write([]byte("concurrent line\n"))
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 100 {
		t.Errorf("expected 100 lines, got %d", len(lines))
	}
}

func TestWatchOutput_AssignsColors(t *testing.T) {
	var buf bytes.Buffer
	wo := newWatchOutput(&buf)

	wo.write("Alice", []byte("hello\n"))
	wo.write("Bob", []byte("world\n"))

	output := buf.String()
	if !strings.Contains(output, "[Alice]") {
		t.Errorf("expected [Alice] prefix in output")
	}
	if !strings.Contains(output, "[Bob]") {
		t.Errorf("expected [Bob] prefix in output")
	}
}

func TestWatchOutput_SameAgentReusesWriter(t *testing.T) {
	var buf bytes.Buffer
	wo := newWatchOutput(&buf)

	wo.write("Alice", []byte("line1\n"))
	wo.write("Alice", []byte("line2\n"))

	if len(wo.writers) != 1 {
		t.Errorf("expected 1 writer for Alice, got %d", len(wo.writers))
	}
}
