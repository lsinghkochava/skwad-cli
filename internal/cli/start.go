package cli

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/terminal"
)

var (
	startFlagWatch   bool
	startFlagDataDir string
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Skwad daemon with agents from a team config",
	Long:  "Starts the MCP server, spawns agents defined in the team config, and blocks until SIGINT/SIGTERM.",
	RunE:  runStart,
}

func init() {
	startCmd.Flags().BoolVar(&startFlagWatch, "watch", false, "stream agent output to stdout")
	startCmd.Flags().StringVar(&startFlagDataDir, "data-dir", "", "data directory (default ~/.config/skwad/)")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	// 1. Load team config (from file or template).
	vars := config.ParseSetFlags(flagSet)
	tc, err := config.LoadConfigOrTemplate(flagConfig, flagTeam, vars)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. Initialize daemon.
	cfg := daemon.Config{
		MCPPort:   flagPort,
		DataDir:   startFlagDataDir,
		PluginDir: findPluginDir(),
	}
	d, err := daemon.New(cfg)
	if err != nil {
		return fmt.Errorf("initialize daemon: %w", err)
	}

	// 3. Create agents from team config.
	agents := createAgentsFromConfig(d, tc)

	// 4. Start daemon (MCP server + pool).
	if err := d.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// 5. Wire --watch output subscriber.
	if startFlagWatch {
		watcher := newWatchOutput(os.Stdout)
		d.Pool.OutputSubscriber = func(agentID uuid.UUID, agentName string, data []byte) {
			cleaned := terminal.CleanOutput(data)
			if len(cleaned) > 0 {
				watcher.write(agentName, cleaned)
			}
		}
	}

	// 6. Spawn all agents.
	for _, a := range agents {
		d.Pool.Spawn(a)
	}

	// 7. Write PID file.
	dataDir := startFlagDataDir
	if dataDir == "" {
		dataDir = d.Store.Dir()
	}
	pidFile, err := daemon.WritePIDFile(dataDir)
	if err != nil {
		slog.Warn("failed to write PID file", "error", err)
	}

	// 8. Print startup banner.
	slog.Info("daemon started", "port", flagPort, "agents", len(agents))
	fmt.Printf("skwad started on port %d\n", flagPort)
	fmt.Printf("Agents: %d\n", len(agents))
	for _, a := range agents {
		slog.Debug("spawning agent", "name", a.Name, "type", a.AgentType)
		fmt.Printf("  - %s (%s)\n", a.Name, a.AgentType)
	}

	// 9. Block on signals — double signal = force kill.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	// 10. Graceful shutdown — second signal forces exit.
	slog.Info("shutting down...")
	fmt.Println("\nShutting down...")
	go func() {
		<-sig
		slog.Warn("force killing...")
		os.Exit(1)
	}()
	d.Stop()
	if pidFile != nil {
		pidFile.Close()
	}
	daemon.RemovePIDFile(dataDir)
	return nil
}

// --- watch output ---

var watchColors = []string{
	"\033[36m", // cyan
	"\033[33m", // yellow
	"\033[32m", // green
	"\033[35m", // magenta
	"\033[34m", // blue
	"\033[91m", // bright red
}

const watchReset = "\033[0m"

// watchOutput manages per-agent line-buffered output for --watch mode.
type watchOutput struct {
	mu      sync.Mutex
	out     io.Writer
	writers map[string]*lineWriter
	nextIdx int
}

func newWatchOutput(out io.Writer) *watchOutput {
	return &watchOutput{
		out:     out,
		writers: make(map[string]*lineWriter),
	}
}

func (w *watchOutput) write(agentName string, data []byte) {
	w.mu.Lock()
	lw, ok := w.writers[agentName]
	if !ok {
		color := watchColors[w.nextIdx%len(watchColors)]
		w.nextIdx++
		lw = &lineWriter{
			prefix: agentName,
			color:  color,
			out:    w.out,
		}
		w.writers[agentName] = lw
	}
	w.mu.Unlock()
	lw.write(data)
}

// lineWriter buffers partial lines and emits complete lines with a colored prefix.
type lineWriter struct {
	mu     sync.Mutex
	prefix string
	color  string
	buf    []byte
	out    io.Writer
}

func (lw *lineWriter) write(data []byte) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	lw.buf = append(lw.buf, data...)
	for {
		idx := bytes.IndexByte(lw.buf, '\n')
		if idx < 0 {
			break
		}
		line := lw.buf[:idx]
		lw.buf = lw.buf[idx+1:]
		fmt.Fprintf(lw.out, "%s[%s]%s %s\n", lw.color, lw.prefix, watchReset, line)
	}
}

// findPluginDir locates the plugin/ directory.
func findPluginDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "plugin")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	wd, _ := os.Getwd()
	dir := filepath.Join(wd, "plugin")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}
