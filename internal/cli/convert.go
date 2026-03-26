package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/lsinghkochava/skwad-cli/internal/config"
)

var convertFlagOutput string

var convertCmd = &cobra.Command{
	Use:   "convert",
	Short: "Convert a macOS Skwad workspace export to CLI team config",
	Long:  "Reads a macOS Skwad workspace export JSON file and converts it to the CLI team config format.",
	RunE:  executeConvert,
}

func init() {
	convertCmd.Flags().StringVar(&flagConfig, "input", "", "path to macOS export file (required)")
	convertCmd.Flags().StringVar(&convertFlagOutput, "output", "", "output file path (default: stdout)")
	_ = convertCmd.MarkFlagRequired("input")
	rootCmd.AddCommand(convertCmd)
}

func executeConvert(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(flagConfig)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	if !config.IsMacOSExport(data) {
		return fmt.Errorf("input does not appear to be a macOS Skwad export (missing formatVersion/appVersion)")
	}

	tc, err := config.ConvertMacOSExport(data)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(tc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if convertFlagOutput != "" {
		if err := os.WriteFile(convertFlagOutput, out, 0644); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		fmt.Printf("Converted config written to %s\n", convertFlagOutput)
	} else {
		fmt.Println(string(out))
	}

	return nil
}
