package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	sendFlagFrom string
	sendFlagTo   string
)

var sendCmd = &cobra.Command{
	Use:   "send [message]",
	Short: "Send a message to another agent",
	Long:  "Sends a message from one agent to another via the running Skwad daemon.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSend,
}

func init() {
	sendCmd.Flags().StringVar(&sendFlagFrom, "from", "", "sender agent name or ID (required)")
	sendCmd.Flags().StringVar(&sendFlagTo, "to", "", "target agent name or ID (uses entry_agent if omitted)")
	_ = sendCmd.MarkFlagRequired("from")
	rootCmd.AddCommand(sendCmd)
}

func runSend(cmd *cobra.Command, args []string) error {
	port := resolvePort()

	payload := map[string]string{
		"from":    sendFlagFrom,
		"to":      sendFlagTo,
		"content": args[0],
	}

	data, err := apiPost(port, "/api/v1/agent/send", payload)
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
