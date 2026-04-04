package git

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const mergeTimeout = 120 * time.Second

// ConsolidateResult holds the outcome of a branch consolidation.
type ConsolidateResult struct {
	Branch          string            `json:"branch"`
	WorktreePath    string            `json:"worktreePath"`
	MergedFrom      []string          `json:"mergedFrom"`
	Skipped         []string          `json:"skipped"`
	ConflictDetails map[string]string `json:"conflictDetails,omitempty"`
	CommitHashes    []string          `json:"commitHashes"`
}

// Consolidate creates a consolidation branch from baseBranch HEAD, then
// sequentially merges each agent branch in its own temporary worktree.
// The main repo checkout is never modified. On conflict per branch, that
// merge is aborted and the branch is skipped.
func Consolidate(repoPath, baseBranch string, agentBranches []string, consolidateBranch string) (*ConsolidateResult, error) {
	cli := &CLI{RepoPath: repoPath}
	wm := NewWorktreeManager(repoPath)

	result := &ConsolidateResult{
		Branch:          consolidateBranch,
		ConflictDetails: make(map[string]string),
	}

	// 1. Create consolidation branch from baseBranch HEAD.
	_, err := cli.Run("branch", consolidateBranch, baseBranch)
	if err != nil {
		return nil, fmt.Errorf("create consolidation branch: %w", err)
	}

	// 2. Create a temporary worktree for the consolidation.
	worktreeDir := filepath.Join(repoPath, ".skwad-worktrees", "consolidate")
	if err := wm.CreateFromExisting(consolidateBranch, worktreeDir); err != nil {
		return nil, fmt.Errorf("create consolidation worktree: %w", err)
	}
	result.WorktreePath = worktreeDir

	// 3. Create a CLI rooted in the consolidation worktree for merge operations.
	wtCLI := &CLI{RepoPath: worktreeDir}

	// 4. Sort branches for deterministic merge order.
	sort.Strings(agentBranches)

	// 5. Sequentially merge each agent branch.
	for _, branch := range agentBranches {
		mergeMsg := fmt.Sprintf("merge: %s into consolidation", branch)
		_, err := wtCLI.RunWithTimeout(mergeTimeout, "merge", "--no-ff", branch, "-m", mergeMsg)
		if err != nil {
			// Merge conflict — capture conflict files before aborting.
			conflictFiles, _ := wtCLI.Run("diff", "--name-only", "--diff-filter=U")
			_, _ = wtCLI.Run("merge", "--abort")
			if conflictFiles == "" {
				conflictFiles = "unknown conflict"
			}

			result.Skipped = append(result.Skipped, branch)
			result.ConflictDetails[branch] = conflictFiles
			continue
		}

		// Extract commit hash.
		hash, _ := wtCLI.Run("rev-parse", "HEAD")
		if hash != "" {
			result.CommitHashes = append(result.CommitHashes, strings.TrimSpace(hash))
		}
		result.MergedFrom = append(result.MergedFrom, branch)
	}

	return result, nil
}
