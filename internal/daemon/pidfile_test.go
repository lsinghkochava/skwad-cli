package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndReadPIDFile(t *testing.T) {
	dir := t.TempDir()

	f, err := WritePIDFile(dir)
	if err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	defer f.Close()

	pid, err := ReadPIDFile(dir)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("expected pid %d, got %d", os.Getpid(), pid)
	}
}

func TestRemovePIDFile(t *testing.T) {
	dir := t.TempDir()

	f, err := WritePIDFile(dir)
	if err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	f.Close()

	if err := RemovePIDFile(dir); err != nil {
		t.Fatalf("RemovePIDFile: %v", err)
	}

	// Verify file is gone.
	path := filepath.Join(dir, pidFileName)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file removed, got: %v", err)
	}
}

func TestReadPIDFileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadPIDFile(dir)
	if err == nil {
		t.Error("expected error for missing PID file")
	}
}

func TestWritePIDFilePreventsDouble(t *testing.T) {
	dir := t.TempDir()

	// First write succeeds.
	f1, err := WritePIDFile(dir)
	if err != nil {
		t.Fatalf("first WritePIDFile: %v", err)
	}
	defer f1.Close()

	// Second write should fail due to flock.
	f2, err := WritePIDFile(dir)
	if err == nil {
		f2.Close()
		t.Fatal("expected error for double PID file write (flock)")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}
