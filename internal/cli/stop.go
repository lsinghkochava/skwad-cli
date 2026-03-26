package cli

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
)

var stopFlagDataDir string

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running Skwad daemon",
	Long:  "Sends SIGTERM to the running Skwad daemon and waits for graceful shutdown.",
	RunE:  runStop,
}

func init() {
	stopCmd.Flags().StringVar(&stopFlagDataDir, "data-dir", "", "data directory (default ~/.config/skwad/)")
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	// 1. Resolve data directory.
	dataDir := stopFlagDataDir
	if dataDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("resolve config dir: %w", err)
		}
		dataDir = base + "/skwad"
	}

	// 2. Read PID file.
	pid, err := daemon.ReadPIDFile(dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "No daemon running")
		os.Exit(1)
	}

	// 3. Check if process is alive.
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintln(os.Stderr, "No daemon running")
		daemon.RemovePIDFile(dataDir)
		os.Exit(1)
	}

	if err := proc.Signal(syscall.Signal(0)); err != nil {
		fmt.Fprintln(os.Stderr, "No daemon running (cleaned stale PID file)")
		daemon.RemovePIDFile(dataDir)
		os.Exit(1)
	}

	// 4. Send SIGTERM.
	slog.Debug("sending SIGTERM", "pid", pid)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}

	// 5. Wait up to 5 seconds for process to exit.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			slog.Info("daemon stopped", "pid", pid)
			fmt.Println("Daemon stopped")
			daemon.RemovePIDFile(dataDir)
			return nil
		}
	}

	// 6. Force kill if still alive.
	slog.Warn("force killing daemon", "pid", pid)
	_ = proc.Signal(syscall.SIGKILL)
	fmt.Println("Daemon force killed")
	daemon.RemovePIDFile(dataDir)
	return nil
}
