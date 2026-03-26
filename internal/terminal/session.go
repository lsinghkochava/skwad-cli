package terminal

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"unicode/utf8"

	"github.com/creack/pty"
)

// Session manages a single PTY process (one per agent).
// It is safe for concurrent use.
type Session struct {
	mu       sync.Mutex
	ptmx     *os.File  // PTY master — guarded by mu for write/resize/close
	cmd      *exec.Cmd // set once in constructor, read-only after
	stopped  atomic.Bool
	exitCode atomic.Int32 // exit code from the process (-1 = unknown)

	// Callbacks — set once before goroutines start, never modified after.
	OnOutput      func(data []byte)
	OnTitleChange func(title string)
	OnExit        func(exitCode int)
}

// NewSession spawns a shell command in a PTY and returns the session.
// Callbacks (OnOutput, OnTitleChange, OnExit) must be set before calling Start().
// command is the full shell command string (passed to $SHELL -c).
// env is extra environment variables merged with the current environment.
func NewSession(command string, env []string) (*Session, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell, "-c", command)
	cmd.Env = append(os.Environ(), env...)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	s := &Session{
		ptmx: ptmx,
		cmd:  cmd,
	}
	s.exitCode.Store(-1)

	return s, nil
}

// Start begins the read and wait goroutines. Must be called after setting callbacks.
func (s *Session) Start() {
	go s.readLoop()
	go s.waitLoop()
}

// SendText writes text to the PTY input without a newline.
func (s *Session) SendText(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped.Load() {
		return
	}
	_, _ = io.WriteString(s.ptmx, text)
}

// SendReturn sends a carriage return.
func (s *Session) SendReturn() {
	s.SendText("\r")
}

// InjectText sends text followed by a carriage return.
func (s *Session) InjectText(text string) {
	s.SendText(text)
	s.SendReturn()
}

// Resize informs the PTY of the new terminal dimensions.
func (s *Session) Resize(cols, rows uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped.Load() || s.ptmx == nil {
		return
	}
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// Kill terminates the process and closes the PTY.
func (s *Session) Kill() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped.Swap(true) {
		return // already stopped
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(syscall.SIGTERM)
	}
	if s.ptmx != nil {
		_ = s.ptmx.Close()
		s.ptmx = nil
	}
}

// IsRunning reports whether the underlying process is still alive.
func (s *Session) IsRunning() bool {
	return !s.stopped.Load()
}

// ExitCode returns the exit code of the process.
// Returns -1 if the process has not exited yet.
func (s *Session) ExitCode() int {
	return int(s.exitCode.Load())
}

func (s *Session) readLoop() {
	buf := make([]byte, 4096)
	for {
		// Read from the PTY master fd. This is safe to call concurrently with
		// Kill() — when Kill() closes ptmx, this read returns an error.
		// We hold a local reference via the struct field (never nil during read
		// because readLoop starts before any Kill can occur, and the read
		// itself will error out when the fd is closed).
		s.mu.Lock()
		f := s.ptmx
		s.mu.Unlock()
		if f == nil {
			return
		}

		n, err := f.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			if title, ok := extractTitle(chunk); ok && s.OnTitleChange != nil {
				s.OnTitleChange(title)
			}
			if s.OnOutput != nil {
				s.OnOutput(chunk)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) waitLoop() {
	var code int
	if err := s.cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	s.exitCode.Store(int32(code))
	s.stopped.Store(true)

	if s.OnExit != nil {
		s.OnExit(code)
	}
}

// extractTitle scans data for an OSC 0 or OSC 2 title escape sequence.
// Format: ESC ] 0 ; <title> BEL  or  ESC ] 2 ; <title> BEL
func extractTitle(data []byte) (string, bool) {
	const (
		esc = 0x1b
		bel = 0x07
	)
	for i := 0; i < len(data)-3; i++ {
		if data[i] != esc || data[i+1] != ']' {
			continue
		}
		if data[i+2] != '0' && data[i+2] != '2' {
			continue
		}
		if i+3 >= len(data) || data[i+3] != ';' {
			continue
		}
		end := bytes.IndexByte(data[i+4:], bel)
		if end < 0 {
			end = bytes.Index(data[i+4:], []byte{esc, '\\'})
		}
		if end < 0 {
			continue
		}
		raw := data[i+4 : i+4+end]
		if !utf8.Valid(raw) {
			continue
		}
		return string(raw), true
	}
	return "", false
}
