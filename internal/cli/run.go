package cli

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/git"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
	"github.com/lsinghkochava/skwad-cli/internal/pipeline"
	"github.com/lsinghkochava/skwad-cli/internal/process"
	"github.com/lsinghkochava/skwad-cli/internal/report"
)

var (
	runFlagPrompt       string
	runFlagPromptFile   string
	runFlagTimeout      string
	runFlagOutputFormat  string
	runFlagExplore           bool
	runFlagMaxIterations     int
	runFlagAutoMerge         bool
	runFlagConsolidateBranch string
	runFlagCleanRuns         string
	runFlagListRuns          bool
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
	runCmd.Flags().BoolVar(&runFlagExplore, "explore", false, "Run all agents in read-only explore mode")
	runCmd.Flags().IntVar(&runFlagMaxIterations, "max-iterations", 1, "Max fix→verify cycles before stopping (0=unlimited)")
	runCmd.Flags().BoolVar(&runFlagAutoMerge, "auto-merge", false, "Automatically consolidate agent branches on run completion")
	runCmd.Flags().StringVar(&runFlagConsolidateBranch, "consolidate-branch", "", "Branch name for consolidation (default: skwad/<session-id>/consolidate)")
	runCmd.Flags().StringVar(&runFlagCleanRuns, "clean-runs", "", "Delete run state older than duration (e.g., '7d', '24h', 'all')")
	runCmd.Flags().BoolVar(&runFlagListRuns, "list-runs", false, "List previous run states and exit")
	rootCmd.AddCommand(runCmd)
}

func executeRun(cmd *cobra.Command, args []string) error {
	// Handle --clean-runs before anything else.
	if runFlagCleanRuns != "" {
		return cleanOldRuns(runFlagCleanRuns)
	}

	// Handle --list-runs.
	if runFlagListRuns {
		runs, err := persistence.ListRuns()
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			fmt.Println("No run state found.")
			return nil
		}
		fmt.Printf("%-30s %-10s %-6s %s\n", "RUN ID", "STATUS", "AGENTS", "STARTED")
		for _, r := range runs {
			status := "running"
			if r.Completed {
				status = "completed"
			} else if r.Failed {
				status = "failed"
			}
			started := ""
			if !r.StartedAt.IsZero() {
				started = r.StartedAt.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%-30s %-10s %-6d %s\n", r.RunID, status, len(r.Agents), started)
		}
		return nil
	}

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
		RepoPath:   tc.Repo,
	}
	d, err := daemon.New(cfg)
	if err != nil {
		return fmt.Errorf("initialize daemon: %w", err)
	}

	// 5. Create agents from team config.
	agents := createAgentsFromConfig(d, tc)

	if runFlagExplore {
		for _, a := range agents {
			a.ExploreMode = true
		}
	}

	// 6. Set up output collection.
	outputMu := &sync.Mutex{}
	outputBufs := make(map[uuid.UUID]*bytes.Buffer)
	for _, a := range agents {
		outputBufs[a.ID] = &bytes.Buffer{}
	}

	// 7. Exit codes are captured by Pool.ExitCode() from the agent process.

	// 8. Start daemon.
	if err := d.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	defer d.Stop()

	startTime := time.Now()
	runID := fmt.Sprintf("%s-%s", time.Now().Format("20060102-150405"), uuid.New().String()[:8])
	slog.Info("starting run", "runID", runID, "agents", len(agents), "timeout", timeout)

	// Event log for run state persistence.
	eventLog, _ := persistence.NewEventLog(runID)
	defer eventLog.Close()
	eventLog.Append(persistence.Event{Type: persistence.EventRunStarted})

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

	// Close stdin on result message so agents exit naturally (run mode only).
	// Only close stdin after the team is ready (i.e., after init turns complete),
	// so that the init turn's result doesn't cause premature exit.
	workDispatched := make(chan struct{})
	prevOnStream := d.Pool.OnStreamMessage
	d.Pool.OnStreamMessage = func(agentID uuid.UUID, msg process.StreamMessage) {
		if prevOnStream != nil {
			prevOnStream(agentID, msg)
		}
		if msg.Type == "result" {
			select {
			case <-workDispatched:
				slog.Debug("work result received, closing all agent stdin", "agentID", agentID)
				for _, a := range agents {
					if err := d.Pool.CloseStdin(a.ID); err != nil {
						slog.Debug("close stdin", "agent", a.Name, "error", err)
					}
				}
			default:
				// Init turn result — don't close stdin yet.
			}
		}
	}

	// 9. Set team size and spawn all agents.
	d.SetTeamSize(len(agents))
	for _, a := range agents {
		d.SpawnAgent(a)
		eventLog.Append(persistence.Event{Type: persistence.EventAgentSpawned, AgentID: a.ID.String(), AgentName: a.Name})
	}

	// 10. Send bootstrap prompt to ALL agents (init turn).
	slog.Info("sending bootstrap prompts", "count", len(agents))
	for _, a := range agents {
		if err := d.Pool.SendBootstrapPrompt(a.ID, defaultBootstrapPrompt); err != nil {
			slog.Error("failed to send bootstrap prompt", "agent", a.Name, "error", err)
		}
	}

	// 11. Wait for team readiness (all agents complete init turn).
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer readyCancel()
	slog.Info("waiting for team readiness", "count", len(agents))
	if err := d.WaitForTeamReady(readyCtx); err != nil {
		slog.Warn("team readiness timeout, proceeding anyway", "error", err)
	} else {
		slog.Info("team ready")
	}

	// 12. Send work prompt to entry agent (or all agents if no entry_agent).
	promptsSent := 0
	for i, a := range agents {
		var agentPrompt string
		if tc.EntryAgent != "" {
			if tc.Agents[i].Prompt != "" {
				agentPrompt = tc.Agents[i].Prompt
			} else if a.Name == tc.EntryAgent {
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
			if err := d.Pool.SendPrompt(a.ID, agentPrompt); err != nil {
				slog.Error("failed to send work prompt", "agent", a.Name, "error", err)
			}
			promptsSent++
		}
	}
	if promptsSent > 0 {
		slog.Info("work prompts sent", "count", promptsSent)
	}
	close(workDispatched)

	// 13. Pipeline iteration loop.
	pl := pipeline.NewPipeline(runFlagMaxIterations, timeout)
	pl.SetPhase("execute")

	timedOut := false
	for {
		iter, err := pl.NextIteration()
		if err != nil {
			slog.Warn("max iterations reached", "iterations", pl.Iteration)
			pl.RecordEvent("max_iterations", fmt.Sprintf("stopped after %d iterations", pl.Iteration))
			break
		}

		if iter > 1 {
			// Re-prompt agents for retry.
			for _, a := range agents {
				if d.Pool.IsRunning(a.ID) {
					continue // still running from previous iteration
				}
				exitCode := d.Pool.ExitCode(a.ID)
				if isRetryableExit(exitCode) {
					slog.Info("retrying agent", "agent", a.Name, "iteration", iter, "prevExitCode", exitCode)
					d.SpawnAgent(a)
					d.Pool.SendBootstrapPrompt(a.ID, fmt.Sprintf("Previous iteration failed (exit code %d). Please review and fix the issues. Original task: %s", exitCode, prompt))
				}
			}
		}

		// Wait loop: check every 2s if all sessions have exited.
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
			break
		}

		// Check results.
		allSuccess := true
		anyRetryable := false
		for _, a := range agents {
			code := d.Pool.ExitCode(a.ID)
			if code != 0 {
				allSuccess = false
				if isRetryableExit(code) {
					anyRetryable = true
				}
			}
		}

		if allSuccess || !anyRetryable || pl.IsExpired() {
			break
		}
	}

	pl.SetPhase("")
	pl.RecordEvent("complete", fmt.Sprintf("finished after %d iterations", pl.Iteration))

	// Small delay to collect final output.
	time.Sleep(500 * time.Millisecond)

	// 14. Format and print report.
	outputMu.Lock()
	defer outputMu.Unlock()

	// Build exit code map from pool.
	agentExitCodes := make(map[uuid.UUID]int)
	for _, a := range agents {
		agentExitCodes[a.ID] = d.Pool.ExitCode(a.ID)
	}

	// Build RunReport from collected data.
	rr := &report.RunReport{}
	for _, a := range agents {
		output := ""
		if buf, ok := outputBufs[a.ID]; ok {
			output = buf.String()
		}
		rr.Agents = append(rr.Agents, report.AgentResult{
			Name:     a.Name,
			Type:     string(a.AgentType),
			ExitCode: agentExitCodes[a.ID],
			Output:   output,
		})
	}

	switch runFlagOutputFormat {
	case "json":
		out, _ := report.FormatJSON(rr)
		fmt.Print(out)
	default:
		fmt.Print(report.FormatMarkdown(rr))
	}

	slog.Info("run complete", "duration", time.Since(startTime).Round(time.Second))

	// Auto-merge if requested.
	if runFlagAutoMerge && d.Config.RepoPath != "" {
		var agentBranches []string
		for _, a := range agents {
			if a.WorktreeBranch != "" {
				agentBranches = append(agentBranches, a.WorktreeBranch)
			}
		}

		if len(agentBranches) > 0 {
			consolidateBranch := runFlagConsolidateBranch
			if consolidateBranch == "" {
				consolidateBranch = fmt.Sprintf("skwad/%s/consolidate", d.SessionID)
			}

			repoCLI := &git.CLI{RepoPath: d.Config.RepoPath}
			base, _ := repoCLI.Run("rev-parse", "--abbrev-ref", "HEAD")
			baseBranch := strings.TrimSpace(base)
			if baseBranch == "" {
				baseBranch = "main"
			}

			slog.Info("auto-merging agent branches", "branches", len(agentBranches), "into", consolidateBranch)
			mergeResult, mergeErr := git.Consolidate(d.Config.RepoPath, baseBranch, agentBranches, consolidateBranch)
			if mergeErr != nil {
				slog.Error("auto-merge failed", "error", mergeErr)
			} else {
				for _, b := range mergeResult.MergedFrom {
					slog.Info("merged", "branch", b)
				}
				for _, b := range mergeResult.Skipped {
					slog.Warn("skipped (conflict)", "branch", b, "conflict", mergeResult.ConflictDetails[b])
				}
				d.CleanupWorktrees()
			}
		}
	}

	// 15. Determine exit code.
	if timedOut || cancelled {
		eventLog.Append(persistence.Event{Type: persistence.EventRunFailed})
		os.Exit(1)
	}
	if pl.Iteration >= pl.MaxIterations && pl.MaxIterations > 0 {
		eventLog.Append(persistence.Event{Type: persistence.EventRunFailed})
		os.Exit(1)
	}
	for _, code := range agentExitCodes {
		if code != 0 {
			eventLog.Append(persistence.Event{Type: persistence.EventRunFailed})
			os.Exit(2)
		}
	}

	eventLog.Append(persistence.Event{Type: persistence.EventRunCompleted})
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

// isRetryableExit returns true if an exit code indicates the agent should be retried.
func isRetryableExit(code int) bool {
	switch code {
	case 0:   // success
		return false
	case 2:   // permission denied
		return false
	case 130: // SIGINT
		return false
	case 137: // SIGKILL
		return false
	default:
		return true
	}
}

// cleanOldRuns deletes run state older than the given spec.
func cleanOldRuns(spec string) error {
	home, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "skwad", "runs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No run state found.")
			return nil
		}
		return err
	}

	if spec == "all" {
		for _, e := range entries {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
		fmt.Printf("Cleaned %d run(s).\n", len(entries))
		return nil
	}

	duration, err := parseDurationSpec(spec)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", spec, err)
	}

	cutoff := time.Now().Add(-duration)
	cleaned := 0
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.RemoveAll(filepath.Join(dir, e.Name()))
			cleaned++
		}
	}
	fmt.Printf("Cleaned %d run(s) older than %s.\n", cleaned, spec)
	return nil
}

// parseDurationSpec parses a duration string, supporting "Nd" for days.
func parseDurationSpec(spec string) (time.Duration, error) {
	if strings.HasSuffix(spec, "d") {
		days := strings.TrimSuffix(spec, "d")
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(spec)
}

