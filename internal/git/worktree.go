package git

import (
	"path/filepath"
	"strings"
)

// WorktreeManager handles worktree discovery and creation for a repository.
type WorktreeManager struct {
	cli *CLI
}

// NewWorktreeManager returns a manager for the given repo path.
func NewWorktreeManager(repoPath string) *WorktreeManager {
	return &WorktreeManager{cli: NewCLI(repoPath)}
}

// List returns all worktrees for the repository.
func (m *WorktreeManager) List() ([]Worktree, error) {
	lines, err := m.cli.RunLines("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var result []Worktree
	var current Worktree
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if current.Path != "" {
				result = append(result, current)
			}
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "HEAD "):
			current.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			branch := strings.TrimPrefix(line, "branch ")
			current.Branch = strings.TrimPrefix(branch, "refs/heads/")
		case line == "bare":
			current.Bare = true
		}
	}
	if current.Path != "" {
		result = append(result, current)
	}
	return result, nil
}

// Create creates a new worktree at destPath on a new branch branchName.
func (m *WorktreeManager) Create(branchName, destPath string) error {
	_, err := m.cli.Run("worktree", "add", "-b", branchName, destPath)
	return err
}

// Remove removes a worktree by path.
func (m *WorktreeManager) Remove(path string) error {
	_, err := m.cli.Run("worktree", "remove", path)
	return err
}

// Prune cleans up stale worktree references.
func (m *WorktreeManager) Prune() error {
	_, err := m.cli.Run("worktree", "prune")
	return err
}

// CreateFromExisting creates a worktree for a branch that already exists (no -b flag).
func (m *WorktreeManager) CreateFromExisting(branchName, destPath string) error {
	_, err := m.cli.Run("worktree", "add", destPath, branchName)
	return err
}

// BranchExists checks if a branch exists.
func (m *WorktreeManager) BranchExists(name string) bool {
	_, err := m.cli.Run("rev-parse", "--verify", "refs/heads/"+name)
	return err == nil
}

// DeleteBranch deletes a local branch.
func (m *WorktreeManager) DeleteBranch(name string) error {
	_, err := m.cli.Run("branch", "-D", name)
	return err
}

// SuggestedPath derives a sibling worktree path from the repo path and branch name.
// e.g. /home/user/myrepo + "feature/foo" → /home/user/myrepo-feature-foo
func SuggestedPath(repoPath, branchName string) string {
	dir := filepath.Dir(repoPath)
	base := filepath.Base(repoPath)
	safe := strings.ReplaceAll(branchName, "/", "-")
	safe = strings.ReplaceAll(safe, " ", "-")
	return filepath.Join(dir, base+"-"+safe)
}
