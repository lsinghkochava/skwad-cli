package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
	"github.com/lsinghkochava/skwad-cli/internal/pipeline"
	"github.com/lsinghkochava/skwad-cli/internal/process"
	"github.com/lsinghkochava/skwad-cli/internal/report"
)

var (
	runFlagPrompt       string
	runFlagPromptFile   string
	runFlagTimeout      string
	runFlagFormat        string
	runFlagOutput        string
	runFlagExplore           bool
	runFlagMaxIterations     int
	runFlagAutoMerge         bool
	runFlagConsolidateBranch string
	runFlagOutputFile        string
	runFlagCleanRuns         string
	runFlagListRuns          bool
	runFlagResume            string
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
	runCmd.Flags().StringVar(&runFlagFormat, "format", "text", "output format: text, json, or markdown")
	runCmd.Flags().StringVar(&runFlagOutput, "output", "entry", "output mode: entry (entry agent result), all (all agent results), raw (full stream)")
	runCmd.Flags().BoolVar(&runFlagExplore, "explore", false, "Run all agents in read-only explore mode")
	runCmd.Flags().IntVar(&runFlagMaxIterations, "max-iterations", 1, "Max fix→verify cycles before stopping (0=unlimited)")
	runCmd.Flags().BoolVar(&runFlagAutoMerge, "auto-merge", false, "Automatically consolidate agent branches on run completion")
	runCmd.Flags().StringVar(&runFlagConsolidateBranch, "consolidate-branch", "", "Branch name for consolidation (default: skwad/<session-id>/consolidate)")
	runCmd.Flags().StringVar(&runFlagOutputFile, "output-file", "", "write output to file instead of stdout")
	runCmd.Flags().StringVar(&runFlagCleanRuns, "clean-runs", "", "Delete run state older than duration (e.g., '7d', '24h', 'all')")
	runCmd.Flags().BoolVar(&runFlagListRuns, "list-runs", false, "List previous run states and exit")
	runCmd.Flags().StringVar(&runFlagResume, "resume", "", "Resume a previous run by ID")
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
		fmt.Printf("%-30s %-12s %-8s %-10s %s\n", "RUN ID", "STATUS", "AGENTS", "COMPLETED", "STARTED")
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
			completedCount := 0
			for _, as := range r.Agents {
				if as.Exited && as.ExitCode == 0 {
					completedCount++
				}
			}
			fmt.Printf("%-30s %-12s %-8d %-10s %s\n", r.RunID, status, len(r.Agents),
				fmt.Sprintf("%d/%d", completedCount, len(r.Agents)), started)
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

	// Determine run ID and handle resume state restoration.
	var runID string
	var resumePrompts map[uuid.UUID]string // agentID → last prompt to re-send
	isResume := runFlagResume != ""

	if isResume {
		runID = runFlagResume
		state, err := persistence.Replay(runID)
		if err != nil {
			return fmt.Errorf("cannot resume run %q: %w (use --list-runs to see available runs)", runID, err)
		}
		if state.Failed {
			slog.Warn("resuming a previously failed run", "runID", runID)
		}

		rr, err := resolveResumeAgents(state, agents)
		if err != nil {
			return err
		}
		resumePrompts = rr.resumePrompts

		// Update manager: swap agent IDs to restored UUIDs.
		for oldID := range rr.idSwaps {
			d.Manager.RemoveAgent(oldID)
		}
		for _, a := range rr.agents {
			d.Manager.AddAgent(a, nil)
		}

		agents = rr.agents
		slog.Info("resuming run", "runID", runID, "resuming", len(agents), "skipped", rr.skippedCount)
	} else {
		runID = fmt.Sprintf("%s-%s", time.Now().Format("20060102-150405"), uuid.New().String()[:8])
	}

	// 6. Set up output collection.
	outputMu := &sync.Mutex{}
	outputBufs := make(map[uuid.UUID]*bytes.Buffer)
	resultTexts := make(map[uuid.UUID]string)
	for _, a := range agents {
		outputBufs[a.ID] = &bytes.Buffer{}
	}

	// 7. Exit codes are captured by Pool.ExitCode() from the agent process.

	// 8. Start daemon.
	if err := d.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	d.ApplyTeamConfig(tc)
	defer d.Stop()

	startTime := time.Now()
	slog.Info("starting run", "runID", runID, "agents", len(agents), "timeout", timeout, "resume", isResume)
	fmt.Fprintf(os.Stderr, "Run ID: %s\n", runID)

	// Event log for run state persistence (appends to existing log on resume).
	eventLog, _ := persistence.NewEventLog(runID)
	defer eventLog.Close()
	if !isResume {
		eventLog.Append(persistence.Event{Type: persistence.EventRunStarted})
	}

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

	// Emit EventAgentRegistered when session_id is captured from the stream.
	// Chain with the daemon's OnSessionID (set in daemon.Start).
	prevOnSessionID := d.Pool.OnSessionID
	agentNameMap := make(map[uuid.UUID]string)
	for _, a := range agents {
		agentNameMap[a.ID] = a.Name
	}
	d.Pool.OnSessionID = func(agentID uuid.UUID, sessionID string) {
		if prevOnSessionID != nil {
			prevOnSessionID(agentID, sessionID)
		}
		data, _ := json.Marshal(map[string]string{"session_id": sessionID})
		eventLog.Append(persistence.Event{
			Type:      persistence.EventAgentRegistered,
			AgentID:   agentID.String(),
			AgentName: agentNameMap[agentID],
			Data:      data,
		})
	}

	// Emit EventAgentExited when an agent process exits.
	prevOnExit := d.Pool.OnExit
	d.Pool.OnExit = func(agentID uuid.UUID, exitCode int) {
		if prevOnExit != nil {
			prevOnExit(agentID, exitCode)
		}
		exitData, _ := json.Marshal(exitCode)
		eventLog.Append(persistence.Event{
			Type:      persistence.EventAgentExited,
			AgentID:   agentID.String(),
			AgentName: agentNameMap[agentID],
			Data:      exitData,
		})
	}

	// Capture result text for --output filtering.
	prevOnStream := d.Pool.OnStreamMessage
	d.Pool.OnStreamMessage = func(agentID uuid.UUID, msg process.StreamMessage) {
		if prevOnStream != nil {
			prevOnStream(agentID, msg)
		}
		if msg.Type == "result" {
			if text := parseResultText(msg.Raw); text != "" {
				outputMu.Lock()
				resultTexts[agentID] = text
				outputMu.Unlock()
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
		bootstrapMsg := defaultBootstrapPrompt
		if isResume {
			bootstrapMsg += "\n\nNOTE: This is a resumed session. You were previously working on this task but the run was interrupted. Continue where you left off."
		}
		if err := d.Pool.SendBootstrapPrompt(a.ID, bootstrapMsg); err != nil {
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
	// On resume, re-send the last prompt from the prior run to agents that need it.
	promptsSent := 0
	if isResume {
		for _, a := range agents {
			if rp, ok := resumePrompts[a.ID]; ok {
				if err := d.Pool.SendPrompt(a.ID, rp); err != nil {
					slog.Error("failed to re-send prompt on resume", "agent", a.Name, "error", err)
				} else {
					promptData, _ := json.Marshal(rp)
					eventLog.Append(persistence.Event{
						Type: persistence.EventPromptSent, AgentID: a.ID.String(),
						AgentName: a.Name, Data: promptData,
					})
					promptsSent++
				}
			}
		}
	} else {
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
				} else {
					promptData, _ := json.Marshal(agentPrompt)
					eventLog.Append(persistence.Event{
						Type:      persistence.EventPromptSent,
						AgentID:   a.ID.String(),
						AgentName: a.Name,
						Data:      promptData,
					})
				}
				promptsSent++
			}
		}
	}
	if promptsSent > 0 {
		slog.Info("work prompts sent", "count", promptsSent)
	}

	// 13. Pipeline iteration loop.
	pl := pipeline.NewPipeline(runFlagMaxIterations, timeout)
	pl.SetPhase("execute")
	phaseData, _ := json.Marshal("execute")
	eventLog.Append(persistence.Event{
		Type: persistence.EventPhaseTransition,
		Data: phaseData,
	})

	timedOut := false
	for {
		iter, err := pl.NextIteration()
		if err != nil {
			slog.Warn("max iterations reached", "iterations", pl.Iteration)
			pl.RecordEvent("max_iterations", fmt.Sprintf("stopped after %d iterations", pl.Iteration))
			break
		}

		iterData, _ := json.Marshal(iter)
		eventLog.Append(persistence.Event{
			Type: persistence.EventIteration,
			Data: iterData,
		})

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

		// Wait loop: check every 2s for all-exited or team quiescence.
		deadline := time.Now().Add(timeout)
		quiescentCycles := 0
		for time.Now().Before(deadline) && !cancelled {
			time.Sleep(2 * time.Second)

			// Check if all processes have exited (fast path).
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

			// Check team quiescence: all agents idle/errored + no pending messages.
			teamQuiescent := true
			for _, a := range agents {
				ag, ok := d.Manager.Agent(a.ID)
				if !ok {
					continue
				}
				if ag.Status != models.AgentStatusIdle && ag.Status != models.AgentStatusError {
					teamQuiescent = false
					break
				}
			}
			if teamQuiescent && !d.Coordinator.HasUnreadMessages() {
				quiescentCycles++
			} else {
				quiescentCycles = 0
			}

			if quiescentCycles >= 2 {
				slog.Info("team quiescent, closing agent stdin")
				for _, a := range agents {
					if err := d.Pool.CloseStdin(a.ID); err != nil {
						slog.Debug("close stdin", "agent", a.Name, "error", err)
					}
				}
				// Brief wait for graceful process exit.
				time.Sleep(3 * time.Second)
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
	endPhaseData, _ := json.Marshal("")
	eventLog.Append(persistence.Event{
		Type: persistence.EventPhaseTransition,
		Data: endPhaseData,
	})
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
			Name:       a.Name,
			Type:       string(a.AgentType),
			ExitCode:   agentExitCodes[a.ID],
			Output:     output,
			ResultText: resultTexts[a.ID],
		})
	}

	// Filter and format output based on --output and --format flags.
	var output string
	filtered := filterRunOutput(runFlagOutput, rr.Agents, tc.EntryAgent)
	if filtered != nil {
		// entry or all mode — print only result text.
		switch runFlagFormat {
		case "json":
			output, _ = report.FormatResultTextJSON(filtered)
		case "markdown":
			output = report.FormatResultText(filtered)
		default: // "text"
			output = report.FormatResultTextPlain(filtered)
		}
	} else {
		// raw mode — full report.
		switch runFlagFormat {
		case "json":
			output, _ = report.FormatJSON(rr)
		case "markdown":
			output = report.FormatMarkdown(rr)
		default: // "text"
			output = report.FormatText(rr)
		}
	}

	// Write output to file or stdout.
	if runFlagOutputFile != "" {
		if err := os.WriteFile(runFlagOutputFile, []byte(output), 0o644); err != nil {
			return fmt.Errorf("write output file: %w", err)
		}
		slog.Info("output written to file", "path", runFlagOutputFile)
	} else {
		fmt.Print(output)
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

// parseResultText extracts the Result field from a raw result stream message.
func parseResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var rm process.ResultMessage
	if err := json.Unmarshal(raw, &rm); err != nil {
		return ""
	}
	return rm.Result
}

// filterRunOutput returns the subset of agents to display based on the --output mode.
// Returns nil for "raw" mode, signaling the caller to use the full report.
func filterRunOutput(mode string, agents []report.AgentResult, entryAgent string) []report.AgentResult {
	switch mode {
	case "all":
		return agents
	case "entry":
		// Find the entry agent by name.
		for _, a := range agents {
			if a.Name == entryAgent {
				return []report.AgentResult{a}
			}
		}
		// Fall back to first agent if no entry agent configured or not found.
		if len(agents) > 0 {
			return []report.AgentResult{agents[0]}
		}
		return nil
	default:
		// "raw" or unrecognized — full report.
		return nil
	}
}

// resumeResult holds the output of resolveResumeAgents.
type resumeResult struct {
	agents        []*models.Agent        // agents to spawn (excludes completed)
	resumePrompts map[uuid.UUID]string   // agentID → last prompt to re-send
	skippedCount  int                    // agents skipped because they completed successfully
	idSwaps       map[uuid.UUID]uuid.UUID // oldID → newID for agents whose UUID was restored
}

// resolveResumeAgents examines prior RunState and decides which agents to resume.
// It restores original UUIDs, sets ResumeSessionID, and collects last prompts.
// Returns an error if the run is already completed or all agents succeeded.
func resolveResumeAgents(state *persistence.RunState, agents []*models.Agent) (*resumeResult, error) {
	if state.Completed {
		return nil, fmt.Errorf("run %q already completed, nothing to resume", state.RunID)
	}

	// Build lookup by agent name from prior run.
	priorByName := make(map[string]persistence.AgentRunState)
	for _, as := range state.Agents {
		priorByName[as.AgentName] = as
	}

	result := &resumeResult{
		resumePrompts: make(map[uuid.UUID]string),
		idSwaps:       make(map[uuid.UUID]uuid.UUID),
	}

	for _, a := range agents {
		prior, found := priorByName[a.Name]
		if !found {
			// New agent not in prior run — spawn as-is.
			result.agents = append(result.agents, a)
			continue
		}

		// Restore original UUID from event log.
		restoredID, parseErr := uuid.Parse(prior.AgentID)
		if parseErr != nil {
			result.agents = append(result.agents, a)
			continue
		}
		oldID := a.ID
		a.ID = restoredID
		result.idSwaps[oldID] = restoredID

		// Skip agents that completed successfully.
		if prior.Exited && prior.ExitCode == 0 {
			result.skippedCount++
			continue
		}

		// Set resume session ID.
		if prior.SessionID != "" {
			a.ResumeSessionID = prior.SessionID
		}

		// Track last prompt for re-send.
		if prior.LastPrompt != "" {
			result.resumePrompts[a.ID] = prior.LastPrompt
		}

		result.agents = append(result.agents, a)
	}

	if len(result.agents) == 0 {
		return nil, fmt.Errorf("all agents completed successfully in run %q, nothing to resume", state.RunID)
	}

	return result, nil
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

