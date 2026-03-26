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
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of all agents in the running daemon",
	Long:  "Queries the running Skwad daemon and displays a formatted table of agent states.",
	RunE:  runStatus,
}

func init() {
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
	return w.Flush()
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
