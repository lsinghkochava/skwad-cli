package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/lsinghkochava/skwad-cli/internal/report"
)

var (
	reportFlagFormat string
	reportFlagInput  string
	reportFlagPR     string
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Format and output an agent run report",
	Long:  "Reads a JSON run report (from `skwad run --output-format json`) and formats it as markdown, JSON, or posts it as a GitHub PR comment.",
	RunE:  runReport,
}

func init() {
	reportCmd.Flags().StringVar(&reportFlagFormat, "format", "markdown", "output format: markdown, json, or github-pr-comment")
	reportCmd.Flags().StringVar(&reportFlagInput, "input", "", "path to JSON report file (reads stdin if omitted)")
	reportCmd.Flags().StringVar(&reportFlagPR, "pr", "", "PR number or URL (required for github-pr-comment format)")
	rootCmd.AddCommand(reportCmd)
}

func runReport(cmd *cobra.Command, args []string) error {
	// Load report.
	var rr *report.RunReport
	var err error
	if reportFlagInput != "" {
		rr, err = report.LoadReport(reportFlagInput)
	} else {
		rr, err = report.LoadReportFromReader(os.Stdin)
	}
	if err != nil {
		return fmt.Errorf("load report: %w", err)
	}

	switch reportFlagFormat {
	case "markdown":
		fmt.Print(report.FormatMarkdown(rr))
	case "json":
		out, err := report.FormatJSON(rr)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "github-pr-comment":
		if reportFlagPR == "" {
			return fmt.Errorf("--pr is required for github-pr-comment format")
		}
		if err := report.PostPRComment(reportFlagPR, rr); err != nil {
			return err
		}
		fmt.Println("PR comment posted successfully")
	default:
		return fmt.Errorf("unknown format: %s (use markdown, json, or github-pr-comment)", reportFlagFormat)
	}

	return nil
}
