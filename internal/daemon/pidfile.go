package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const pidFileName = "skwad.pid"

// WritePIDFile writes the current process ID to {dir}/skwad.pid and acquires
// an advisory flock to prevent two daemons from running simultaneously.
// The returned file must be kept open for the lock to remain held.
func WritePIDFile(dir string) (*os.File, error) {
	path := filepath.Join(dir, pidFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open pid file: %w", err)
	}

	// Advisory lock — fails fast if another daemon holds it.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another skwad daemon is already running (pid file locked)")
	}

	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		f.Close()
		return nil, fmt.Errorf("write pid: %w", err)
	}

	return f, nil
}

// RemovePIDFile removes the PID file from the given directory.
func RemovePIDFile(dir string) error {
	return os.Remove(filepath.Join(dir, pidFileName))
}

// ReadPIDFile reads the PID from the PID file in the given directory.
// Returns 0 if the file does not exist or cannot be parsed.
func ReadPIDFile(dir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(dir, pidFileName))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid: %w", err)
	}
	return pid, nil
}
