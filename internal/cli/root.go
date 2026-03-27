// Package cli implements the Cobra command tree for the skwad CLI binary.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

var (
	flagPort    int
	flagVerbose bool
	flagQuiet   bool
	flagConfig  string
	flagTeam    string
	flagSet     []string
)

var rootCmd = &cobra.Command{
	Use:   "skwad",
	Short: "Multi-agent CLI orchestrator",
	Long:  "Skwad runs multiple AI coding agents in parallel, coordinates them via MCP, and manages their lifecycle from the command line.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		level := slog.LevelInfo
		if flagVerbose {
			level = slog.LevelDebug
		} else if flagQuiet {
			level = slog.LevelError
		}
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(handler))
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().IntVar(&flagPort, "port", models.DefaultMCPPort, "MCP server port")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&flagQuiet, "quiet", false, "suppress non-error output")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "path to team config file")
	rootCmd.PersistentFlags().StringVar(&flagTeam, "team", "", "built-in team template name (e.g. review-team, dev-team)")
	rootCmd.PersistentFlags().StringSliceVar(&flagSet, "set", nil, "template variables (key=value, e.g. --set repo=/path --set prompt='Review this PR')")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
