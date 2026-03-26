package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

var (
	runFlagPrompt       string
	runFlagPromptFile   string
	runFlagTimeout      string
	runFlagOutputFormat string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "One-shot mode: spawn agents, run prompts, collect output, exit",
	Long:  "CI/scripting mode. Spawns agents from config, sends prompts, waits for completion or timeout, then reports results and exits.",
	RunE:  executeRun,
}

func init() {
	runCmd.Flags().StringVar(&flagConfig, "config", "", "path to team config file (required)")
	runCmd.Flags().StringVar(&runFlagPrompt, "prompt", "", "initial prompt to send to all agents")
	runCmd.Flags().StringVar(&runFlagPromptFile, "prompt-file", "", "file containing initial prompt")
	runCmd.Flags().StringVar(&runFlagTimeout, "timeout", "10m", "maximum time to wait for agents")
	runCmd.Flags().StringVar(&runFlagOutputFormat, "output-format", "markdown", "output format: markdown or json")
	_ = runCmd.MarkFlagRequired("config")
	rootCmd.AddCommand(runCmd)
}

func executeRun(cmd *cobra.Command, args []string) error {
	// 1. Load team config.
	tc, err := config.LoadTeamConfig(flagConfig)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. Resolve prompt.
	prompt, err := resolvePrompt(tc)
	if err != nil {
		return err
	}

	// 3. Parse timeout.
	timeout, err := time.ParseDuration(runFlagTimeout)
	if err != nil {
		return fmt.Errorf("invalid timeout: %w", err)
	}

	// 4. Initialize daemon.
	cfg := daemon.Config{
		MCPPort:   flagPort,
		PluginDir: findPluginDir(),
	}
	d, err := daemon.New(cfg)
	if err != nil {
		return fmt.Errorf("initialize daemon: %w", err)
	}

	// 5. Create agents from team config.
	agents := createAgentsFromConfig(d, tc)

	// 6. Set up output collection.
	outputMu := &sync.Mutex{}
	outputBufs := make(map[uuid.UUID]*bytes.Buffer)
	for _, a := range agents {
		outputBufs[a.ID] = &bytes.Buffer{}
	}

	// 7. Track exit codes.
	// NOTE: Exit codes are not yet captured from agent processes.
	// Pool.OnExit does not expose the real exit code — all agents
	// report exit_code=0. Precise exit code capture is a Phase 3 TODO.
	exitMu := &sync.Mutex{}
	exitCodes := make(map[uuid.UUID]int)

	// 8. Start daemon.
	if err := d.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	defer d.Stop()

	// Wire output subscriber.
	d.Pool.OutputSubscriber = func(agentID uuid.UUID, agentName string, data []byte) {
		outputMu.Lock()
		if buf, ok := outputBufs[agentID]; ok {
			buf.Write(data)
		}
		outputMu.Unlock()
	}

	// Wire exit tracking via OnStatusChanged — when status goes idle after
	// the session exits, we can detect it. But better: use Pool.IsRunning.
	// We also need exit codes. The Pool's spawnNow sets sess.OnExit which
	// sets status to idle. We'll capture exit codes by hooking into the
	// Pool's OnStatusChanged and checking IsRunning.
	d.Pool.OnStatusChanged = func(agentID uuid.UUID, status models.AgentStatus) {
		if !d.Pool.IsRunning(agentID) {
			exitMu.Lock()
			if _, exists := exitCodes[agentID]; !exists {
				exitCodes[agentID] = 0 // default to 0 if we don't have the code
			}
			exitMu.Unlock()
		}
	}

	// 9. Spawn all agents.
	for _, a := range agents {
		d.Pool.Spawn(a)
	}

	// 10. Wait for agents to register (up to 30s).
	fmt.Fprintf(os.Stderr, "Waiting for %d agents to start...\n", len(agents))
	regDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(regDeadline) {
		allRunning := true
		for _, a := range agents {
			if !d.Pool.IsRunning(a.ID) {
				allRunning = false
				break
			}
		}
		if allRunning {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// 11. Send prompt to each agent.
	if prompt != "" {
		// Brief delay to let agents initialize.
		time.Sleep(2 * time.Second)
		for _, a := range agents {
			d.Pool.QueueText(a.ID, prompt+"\n")
		}
		fmt.Fprintf(os.Stderr, "Prompt sent to %d agents\n", len(agents))
	}

	// 12. Wait loop: check every 2s if all sessions have exited.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		allExited := true
		for _, a := range agents {
			if d.Pool.IsRunning(a.ID) {
				allExited = false
				break
			}
		}
		if allExited {
			break
		}
	}

	// Check if timeout was reached — kill remaining.
	timedOut := false
	for _, a := range agents {
		if d.Pool.IsRunning(a.ID) {
			timedOut = true
			break
		}
	}
	if timedOut {
		fmt.Fprintf(os.Stderr, "Timeout reached, stopping remaining agents...\n")
		d.Pool.StopAll()
		// Wait 5s for graceful exit.
		time.Sleep(5 * time.Second)
	}

	// Small delay to collect final output.
	time.Sleep(500 * time.Millisecond)

	// 13. Format and print report.
	outputMu.Lock()
	defer outputMu.Unlock()

	switch runFlagOutputFormat {
	case "json":
		printJSONReport(agents, outputBufs, exitCodes, exitMu)
	default:
		printMarkdownReport(agents, outputBufs)
	}

	// 14. Determine exit code.
	if timedOut {
		os.Exit(1)
	}
	exitMu.Lock()
	for _, code := range exitCodes {
		if code != 0 {
			exitMu.Unlock()
			os.Exit(2)
		}
	}
	exitMu.Unlock()

	return nil
}

func resolvePrompt(tc *config.TeamConfig) (string, error) {
	if runFlagPromptFile != "" {
		data, err := os.ReadFile(runFlagPromptFile)
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		return string(data), nil
	}
	if runFlagPrompt != "" {
		return runFlagPrompt, nil
	}
	// Per-agent prompts handled individually — return empty for global.
	return "", nil
}

func printMarkdownReport(agents []*models.Agent, outputs map[uuid.UUID]*bytes.Buffer) {
	fmt.Println("# Skwad Run Report")
	fmt.Println()
	for _, a := range agents {
		fmt.Printf("## %s (%s)\n", a.Name, a.AgentType)
		fmt.Println("```")
		if buf, ok := outputs[a.ID]; ok {
			fmt.Print(buf.String())
		}
		fmt.Println("```")
		fmt.Println()
	}
}

func printJSONReport(agents []*models.Agent, outputs map[uuid.UUID]*bytes.Buffer, exitCodes map[uuid.UUID]int, exitMu *sync.Mutex) {
	type agentResult struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		ExitCode int    `json:"exit_code"` // always 0 until Pool exposes real exit codes
		Output   string `json:"output"`
	}

	exitMu.Lock()
	defer exitMu.Unlock()

	results := make([]agentResult, len(agents))
	for i, a := range agents {
		output := ""
		if buf, ok := outputs[a.ID]; ok {
			output = buf.String()
		}
		results[i] = agentResult{
			Name:     a.Name,
			Type:     string(a.AgentType),
			ExitCode: exitCodes[a.ID],
			Output:   output,
		}
	}

	report := map[string]interface{}{
		"agents": results,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}
