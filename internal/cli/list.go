package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents in the running daemon",
	Long:  "Queries the running Skwad daemon and displays agent names, IDs, and types.",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tTYPE")
	for _, a := range agents {
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.Name, a.AgentID, a.AgentType)
	}
	return w.Flush()
}
