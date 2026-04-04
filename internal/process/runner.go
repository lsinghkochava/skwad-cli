package process

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Runner manages a headless CLI process with JSON stream I/O.
// It is safe for concurrent use.
type Runner struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	stopped  atomic.Bool
	exitCode atomic.Int32
	done     chan struct{}
	args     []string
	env      []string
	dir      string

	// Callbacks — set before calling Start(), never modified after.
	OnMessage func(msg StreamMessage)
	OnExit    func(exitCode int)
}

// NewRunner creates a Runner for the given command arguments, environment, and
// working directory. Callbacks (OnMessage, OnExit) must be set before Start().
func NewRunner(args []string, env []string, dir string) *Runner {
	r := &Runner{
		args: args,
		env:  env,
		dir:  dir,
		done: make(chan struct{}),
	}
	r.exitCode.Store(-1)
	return r
}

// Start launches the process, wires up stdin/stdout/stderr pipes, and starts
// read goroutines. The first element of args is used as the command name.
func (r *Runner) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cmd != nil {
		return fmt.Errorf("process already started")
	}

	if len(r.args) == 0 {
		return fmt.Errorf("no command arguments provided")
	}

	slog.Debug("runner.Start", "executable", r.args[0], "args", r.args[1:], "dir", r.dir)
	cmd := exec.Command(r.args[0], r.args[1:]...)
	cmd.Dir = r.dir
	if len(r.env) > 0 {
		// TODO temporary short circuit till the time we get an API key
		cmd.Env = append(os.Environ(), r.env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}
	slog.Debug("runner process started", "pid", cmd.Process.Pid)

	r.cmd = cmd
	r.stdin = stdin
	r.stdout = stdout
	r.stderr = stderr

	go r.readStdout()
	go r.readStderr()
	go r.waitLoop()

	return nil
}

// SendPrompt writes a UserInputMessage as JSON followed by a newline to stdin.
func (r *Runner) SendPrompt(text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopped.Load() {
		return fmt.Errorf("process is stopped")
	}
	if r.stdin == nil {
		return fmt.Errorf("process not started")
	}

	msg := NewUserInput(text)
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal prompt: %w", err)
	}

	data = append(data, '\n')
	if _, err := r.stdin.Write(data); err != nil {
		return fmt.Errorf("write stdin: %w", err)
	}

	return nil
}

// Stop sends SIGTERM to the process group, waits up to 5 seconds, then SIGKILL.
func (r *Runner) Stop() error {
	if !r.stopped.CompareAndSwap(false, true) {
		return nil // already stopping
	}

	r.mu.Lock()
	cmd := r.cmd
	r.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid

	// Send SIGTERM to process group.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		slog.Warn("sigterm failed", "pid", pid, "error", err)
	}

	// Wait up to 5 seconds for exit.
	select {
	case <-r.done:
		return nil
	case <-time.After(5 * time.Second):
		slog.Warn("process did not exit after SIGTERM, sending SIGKILL", "pid", pid)
		r.killGroup(pid)
		<-r.done
		return nil
	}
}

// Kill immediately sends SIGKILL to the process group.
func (r *Runner) Kill() {
	r.stopped.Store(true)

	r.mu.Lock()
	cmd := r.cmd
	r.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	r.killGroup(cmd.Process.Pid)
}

// CloseStdin closes the stdin pipe, causing the process to exit naturally.
// Safe to call multiple times.
func (r *Runner) CloseStdin() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stdin == nil {
		return nil
	}
	err := r.stdin.Close()
	r.stdin = nil
	return err
}

// IsRunning returns true if the process has been started and has not yet exited.
func (r *Runner) IsRunning() bool {
	select {
	case <-r.done:
		return false
	default:
		r.mu.Lock()
		started := r.cmd != nil
		r.mu.Unlock()
		return started
	}
}

// ExitCode returns the process exit code, or -1 if the process hasn't exited.
func (r *Runner) ExitCode() int {
	return int(r.exitCode.Load())
}

// Wait returns a channel that is closed when the process exits.
func (r *Runner) Wait() <-chan struct{} {
	return r.done
}

// readStdout scans stdout line-by-line, parses JSON stream messages, and
// invokes OnMessage for each.
func (r *Runner) readStdout() {
	scanner := bufio.NewScanner(r.stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg StreamMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			slog.Warn("failed to parse stdout JSON", "error", err, "line", string(line))
			continue
		}

		// Store the raw bytes for passthrough.
		raw := make([]byte, len(line))
		copy(raw, line)
		msg.Raw = raw

		if r.OnMessage != nil {
			r.OnMessage(msg)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Debug("stdout scanner error", "error", err)
	}
}

// readStderr reads stderr line-by-line and logs each line as a warning.
func (r *Runner) readStderr() {
	scanner := bufio.NewScanner(r.stderr)
	for scanner.Scan() {
		slog.Warn("process stderr", "line", scanner.Text())
	}
}

// waitLoop waits for the process to exit, records the exit code, and fires OnExit.
func (r *Runner) waitLoop() {
	defer close(r.done)

	err := r.cmd.Wait()

	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	r.exitCode.Store(int32(code))
	slog.Debug("runner process exited", "exitCode", code, "dir", r.dir)

	if r.OnExit != nil {
		r.OnExit(code)
	}
}

// killGroup sends SIGKILL to the process group.
func (r *Runner) killGroup(pid int) {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		slog.Warn("sigkill failed", "pid", pid, "error", err)
	}
}
