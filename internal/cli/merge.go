package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/lsinghkochava/skwad-cli/internal/git"
	"github.com/spf13/cobra"
)

var (
	mergeFlagBranch  string
	mergeFlagBase    string
	mergeFlagCleanup bool
	mergeFlagDryRun  bool
	mergeFlagRepo    string
	mergeFlagSession string
)

var mergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Consolidate agent branches into a single branch",
	RunE:  runMerge,
}

func init() {
	mergeCmd.Flags().StringVarP(&mergeFlagBranch, "branch", "b", "", "Consolidation branch name (default: skwad/consolidate)")
	mergeCmd.Flags().StringVar(&mergeFlagBase, "base", "", "Base branch to merge onto (default: current branch)")
	mergeCmd.Flags().BoolVar(&mergeFlagCleanup, "cleanup", false, "Remove worktrees after successful consolidation")
	mergeCmd.Flags().BoolVar(&mergeFlagDryRun, "dry-run", false, "List branches without merging")
	mergeCmd.Flags().StringVar(&mergeFlagRepo, "repo", "", "Repository path (default: auto-detect from cwd)")
	mergeCmd.Flags().StringVar(&mergeFlagSession, "session", "", "Only merge branches from this session ID")
	rootCmd.AddCommand(mergeCmd)
}

func runMerge(cmd *cobra.Command, args []string) error {
	repoPath := mergeFlagRepo
	if repoPath == "" {
		cwd, _ := os.Getwd()
		root, err := git.RootOf(cwd)
		if err != nil {
			return fmt.Errorf("not in a git repository (use --repo to specify): %w", err)
		}
		repoPath = root
	}

	// Discover skwad branches.
	cli := &git.CLI{RepoPath: repoPath}
	out, err := cli.RunLines("branch", "--list", "skwad/*")
	if err != nil {
		return fmt.Errorf("list branches: %w", err)
	}

	var branches []string
	for _, line := range out {
		branch := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		if mergeFlagSession != "" && !strings.Contains(branch, mergeFlagSession) {
			continue
		}
		if strings.HasSuffix(branch, "/consolidate") {
			continue
		}
		branches = append(branches, branch)
	}

	if len(branches) == 0 {
		fmt.Println("No skwad agent branches found.")
		return nil
	}

	consolidateBranch := mergeFlagBranch
	if consolidateBranch == "" {
		consolidateBranch = "skwad/consolidate"
	}

	baseBranch := mergeFlagBase
	if baseBranch == "" {
		base, err := cli.Run("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return fmt.Errorf("detect current branch: %w", err)
		}
		baseBranch = strings.TrimSpace(base)
	}

	if mergeFlagDryRun {
		fmt.Printf("Would consolidate %d branches into %s (base: %s):\n", len(branches), consolidateBranch, baseBranch)
		for _, b := range branches {
			fmt.Printf("  - %s\n", b)
		}
		return nil
	}

	fmt.Printf("Consolidating %d agent branches into %s...\n\n", len(branches), consolidateBranch)

	result, err := git.Consolidate(repoPath, baseBranch, branches, consolidateBranch)
	if err != nil {
		return fmt.Errorf("consolidation failed: %w", err)
	}

	for _, b := range result.MergedFrom {
		fmt.Printf("  ✓ %s — merged\n", b)
	}
	for _, b := range result.Skipped {
		fmt.Printf("\n  ✗ SKIPPED: %s\n", b)
		if detail, ok := result.ConflictDetails[b]; ok {
			fmt.Printf("    Conflict in: %s\n", detail)
		}
		fmt.Printf("    To resolve manually:\n")
		fmt.Printf("      cd %s\n", result.WorktreePath)
		fmt.Printf("      git merge %s\n", b)
		fmt.Printf("      # resolve conflicts, then: git commit\n")
	}

	fmt.Printf("\nResult: %d/%d branches merged.", len(result.MergedFrom), len(branches))
	if len(result.Skipped) > 0 {
		fmt.Printf(" %d SKIPPED (conflicts).", len(result.Skipped))
	}
	fmt.Println()

	if mergeFlagCleanup && len(result.MergedFrom) > 0 {
		wm := git.NewWorktreeManager(repoPath)
		trees, _ := wm.List()
		for _, tree := range trees {
			for _, merged := range result.MergedFrom {
				if tree.Branch == merged {
					_ = wm.Remove(tree.Path)
					_ = wm.DeleteBranch(merged)
				}
			}
		}
		_ = wm.Prune()
		fmt.Printf("Cleaned up %d worktrees.\n", len(result.MergedFrom))
	}

	return nil
}
