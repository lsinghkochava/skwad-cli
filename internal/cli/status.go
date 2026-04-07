package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// agentDebugInfo matches the JSON shape from GET / debug endpoint.
type agentDebugInfo struct {
	AgentID    string            `json:"agent_id"`
	Name       string            `json:"name"`
	Folder     string            `json:"folder"`
	State      string            `json:"state"`
	Status     string            `json:"status"`
	Registered bool              `json:"registered"`
	AgentType  string            `json:"agent_type"`
	SessionID  string            `json:"session_id"`
	Metadata   map[string]string `json:"metadata"`
}

// ANSI color codes.
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorBlue   = "\033[34m"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of all agents in the running daemon",
	Long:  "Queries the running Skwad daemon and displays a formatted table of agent states.",
	RunE:  runStatus,
}

func init() {
	statusCmd.Flags().Bool("tasks", false, "Also display the task board")
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	port := resolvePort()
	data, err := apiGet(port, "/")
	if err != nil {
		fmt.Fprintf(os.Stderr, "No daemon running on port %d\n", port)
		os.Exit(1)
	}

	var agents []agentDebugInfo
	if err := json.Unmarshal(data, &agents); err != nil {
		return fmt.Errorf("parse agent list: %w", err)
	}

	if len(agents) == 0 {
		fmt.Println("No agents running.")
		return nil
	}

	useColor := isTerminal(os.Stdout)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tSTATE\tSTATUS\tFOLDER")

	for _, a := range agents {
		state := a.State
		if useColor {
			state = colorizeState(a.State)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", a.Name, a.AgentType, state, a.Status, a.Folder)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	showTasks, _ := cmd.Flags().GetBool("tasks")
	if showTasks {
		if err := printTaskBoard(port, useColor); err != nil {
			return err
		}
	}

	return nil
}

type taskInfo struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	AssigneeName  string `json:"assigneeName"`
	CreatedByName string `json:"createdByName"`
}

func printTaskBoard(port int, useColor bool) error {
	data, err := apiGet(port, "/api/v1/tasks")
	if err != nil {
		return fmt.Errorf("fetch tasks: %w", err)
	}

	var tasks []taskInfo
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("parse tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("\nNo tasks")
		return nil
	}

	fmt.Println("\nTasks:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  ID\tSTATUS\tTITLE\tASSIGNEE\tCREATED BY")

	for _, t := range tasks {
		shortID := t.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		status := t.Status
		if useColor {
			status = colorizeTaskStatus(t.Status)
		}

		assignee := t.AssigneeName
		if assignee == "" {
			assignee = "—"
		}

		createdBy := t.CreatedByName
		if createdBy == "" {
			createdBy = "—"
		}

		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", shortID, status, t.Title, assignee, createdBy)
	}
	return w.Flush()
}

func colorizeTaskStatus(status string) string {
	switch status {
	case "completed":
		return colorGreen + status + colorReset
	case "in_progress":
		return colorYellow + status + colorReset
	case "pending":
		return colorBlue + status + colorReset
	case "blocked":
		return colorRed + status + colorReset
	default:
		return status
	}
}

func colorizeState(state string) string {
	switch state {
	case "running":
		return colorGreen + state + colorReset
	case "idle":
		return colorYellow + state + colorReset
	case "error":
		return colorRed + state + colorReset
	case "input":
		return colorYellow + state + colorReset
	default:
		return state
	}
}
