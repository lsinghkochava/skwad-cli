package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var broadcastFlagFrom string

var broadcastCmd = &cobra.Command{
	Use:   "broadcast [message]",
	Short: "Broadcast a message to all agents",
	Long:  "Sends a message from one agent to all other agents via the running Skwad daemon.",
	Args:  cobra.ExactArgs(1),
	RunE:  runBroadcast,
}

func init() {
	broadcastCmd.Flags().StringVar(&broadcastFlagFrom, "from", "", "sender agent name or ID (required)")
	_ = broadcastCmd.MarkFlagRequired("from")
	rootCmd.AddCommand(broadcastCmd)
}

func runBroadcast(cmd *cobra.Command, args []string) error {
	port := resolvePort()

	payload := map[string]string{
		"from":    broadcastFlagFrom,
		"content": args[0],
	}

	data, err := apiPost(port, "/api/v1/agent/broadcast", payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "No daemon running on port %d\n", port)
		os.Exit(1)
	}

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !resp.Success {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Message)
		os.Exit(1)
	}

	fmt.Println(resp.Message)
	return nil
}
