package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Stream agent output (use `skwad start --watch` instead)",
	Long:  "Standalone watch is not supported. Use `skwad start --watch` to stream agent output during a daemon session.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "Watch is only available with `skwad start --watch`")
		os.Exit(1)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(watchCmd)
}
