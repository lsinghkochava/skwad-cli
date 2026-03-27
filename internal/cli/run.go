package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
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
	runCmd.Flags().StringVar(&runFlagPrompt, "prompt", "", "initial prompt to send to all agents")
	runCmd.Flags().StringVar(&runFlagPromptFile, "prompt-file", "", "file containing initial prompt")
	runCmd.Flags().StringVar(&runFlagTimeout, "timeout", "10m", "maximum time to wait for agents")
	runCmd.Flags().StringVar(&runFlagOutputFormat, "output-format", "markdown", "output format: markdown or json")
	rootCmd.AddCommand(runCmd)
}

func executeRun(cmd *cobra.Command, args []string) error {
	// 1. Load team config (from file or template).
	vars := config.ParseSetFlags(flagSet)
	tc, err := config.LoadConfigOrTemplate(flagConfig, flagTeam, vars)
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
		MCPPort:    flagPort,
		PluginDir:  findPluginDir(),
		EntryAgent: tc.EntryAgent,
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

	// 7. Exit codes are captured by Pool.ExitCode() from the PTY session.

	// 8. Start daemon.
	if err := d.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	defer d.Stop()

	startTime := time.Now()
	slog.Info("starting run", "agents", len(agents), "timeout", timeout)

	// Signal handling — first SIGINT/SIGTERM graceful, second force kills.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	cancelled := false
	go func() {
		<-sigCh
		slog.Info("received signal, shutting down...")
		cancelled = true
		d.Pool.StopAll()
		// Second signal → force exit.
		<-sigCh
		slog.Warn("force killing...")
		os.Exit(1)
	}()

	// Wire output subscriber.
	d.Pool.OutputSubscriber = func(agentID uuid.UUID, agentName string, data []byte) {
		outputMu.Lock()
		if buf, ok := outputBufs[agentID]; ok {
			buf.Write(data)
		}
		outputMu.Unlock()
	}

	// 9. Spawn all agents.
	for _, a := range agents {
		d.Pool.Spawn(a)
	}

	// 10. Wait for agents to register (up to 30s).
	slog.Info("waiting for agents to start", "count", len(agents))
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

	// 11. Send prompt to each agent (per-agent > --prompt > team prompt).
	// When entry_agent is set, global/flag prompts go only to the entry agent.
	time.Sleep(2 * time.Second)
	promptsSent := 0
	for i, a := range agents {
		var agentPrompt string
		if tc.EntryAgent != "" {
			// Per-agent prompt always goes to that agent.
			if tc.Agents[i].Prompt != "" {
				agentPrompt = tc.Agents[i].Prompt
			} else if a.Name == tc.EntryAgent {
				// Global/flag prompt goes only to the entry agent.
				if prompt != "" {
					agentPrompt = prompt
				} else {
					agentPrompt = tc.Prompt
				}
			}
		} else {
			agentPrompt = resolveAgentPrompt(tc.Agents[i], prompt, tc.Prompt)
		}
		if agentPrompt != "" {
			d.Pool.QueueText(a.ID, agentPrompt+"\n")
			promptsSent++
		}
	}
	if promptsSent > 0 {
		slog.Info("prompts sent", "count", promptsSent)
	}

	// 12. Wait loop: check every 2s if all sessions have exited.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && !cancelled {
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
		slog.Warn("timeout reached, stopping remaining agents")
		d.Pool.StopAll()
		time.Sleep(5 * time.Second)
	}

	// Small delay to collect final output.
	time.Sleep(500 * time.Millisecond)

	// 13. Format and print report.
	outputMu.Lock()
	defer outputMu.Unlock()

	// Build exit code map from pool.
	agentExitCodes := make(map[uuid.UUID]int)
	for _, a := range agents {
		agentExitCodes[a.ID] = d.Pool.ExitCode(a.ID)
	}

	switch runFlagOutputFormat {
	case "json":
		printJSONReport(agents, outputBufs, agentExitCodes)
	default:
		printMarkdownReport(agents, outputBufs)
	}

	slog.Info("run complete", "duration", time.Since(startTime).Round(time.Second))

	// 14. Determine exit code.
	if timedOut || cancelled {
		os.Exit(1)
	}
	for _, code := range agentExitCodes {
		if code != 0 {
			os.Exit(2)
		}
	}

	return nil
}

// resolveAgentPrompt returns the prompt for a specific agent, following priority:
// per-agent config > --prompt/--prompt-file flag > team-level prompt.
func resolveAgentPrompt(ac config.AgentConfig, flagPrompt, teamPrompt string) string {
	if ac.Prompt != "" {
		return ac.Prompt
	}
	if flagPrompt != "" {
		return flagPrompt
	}
	return teamPrompt
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

func printJSONReport(agents []*models.Agent, outputs map[uuid.UUID]*bytes.Buffer, exitCodes map[uuid.UUID]int) {
	type agentResult struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		ExitCode int    `json:"exit_code"`
		Output   string `json:"output"`
	}

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
