package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/lsinghkochava/skwad-cli/internal/git"
	"github.com/spf13/cobra"
)

var (
	cleanFlagBranches bool
	cleanFlagSession  string
	cleanFlagForce    bool
	cleanFlagRepo     string
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove agent worktrees and optionally their branches",
	RunE:  runClean,
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanFlagBranches, "branches", false, "Also delete skwad/* branches")
	cleanCmd.Flags().StringVar(&cleanFlagSession, "session", "", "Clean only a specific session ID")
	cleanCmd.Flags().BoolVar(&cleanFlagForce, "force", false, "Force remove even with uncommitted changes")
	cleanCmd.Flags().StringVar(&cleanFlagRepo, "repo", "", "Repository path (default: auto-detect from cwd)")
	rootCmd.AddCommand(cleanCmd)
}

func runClean(cmd *cobra.Command, args []string) error {
	repoPath := cleanFlagRepo
	if repoPath == "" {
		cwd, _ := os.Getwd()
		root, err := git.RootOf(cwd)
		if err != nil {
			return fmt.Errorf("not in a git repository (use --repo to specify): %w", err)
		}
		repoPath = root
	}

	wm := git.NewWorktreeManager(repoPath)
	trees, err := wm.List()
	if err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}

	removed := 0
	for _, tree := range trees {
		if !strings.HasPrefix(tree.Branch, "skwad/") {
			continue
		}
		if cleanFlagSession != "" && !strings.Contains(tree.Branch, cleanFlagSession) {
			continue
		}

		if cleanFlagForce {
			cli := &git.CLI{RepoPath: repoPath}
			_, _ = cli.Run("worktree", "remove", "--force", tree.Path)
		} else {
			_ = wm.Remove(tree.Path)
		}
		removed++
		fmt.Printf("  Removed worktree: %s (%s)\n", tree.Path, tree.Branch)

		if cleanFlagBranches {
			_ = wm.DeleteBranch(tree.Branch)
			fmt.Printf("  Deleted branch: %s\n", tree.Branch)
		}
	}

	_ = wm.Prune()

	if removed == 0 {
		fmt.Println("No skwad worktrees found.")
	} else {
		fmt.Printf("\nCleaned %d worktrees.\n", removed)
	}

	return nil
}
