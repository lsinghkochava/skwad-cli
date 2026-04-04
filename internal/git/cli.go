package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const commandTimeout = 30 * time.Second

// CLI is a low-level git command runner with a timeout.
type CLI struct {
	RepoPath string
}

// NewCLI returns a CLI rooted at repoPath.
func NewCLI(repoPath string) *CLI {
	return &CLI{RepoPath: repoPath}
}

// Run executes a git command and returns stdout.
func (c *CLI) Run(args ...string) (string, error) {
	return c.RunWithTimeout(commandTimeout, args...)
}

// RunWithTimeout executes a git command with a custom timeout.
func (c *CLI) RunWithTimeout(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = c.RepoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("git %s: timeout after %s", strings.Join(args, " "), timeout)
		}
		return "", fmt.Errorf("git %s: %w — %s", strings.Join(args, " "), err, stderr.String())
	}

	return strings.TrimRight(stdout.String(), "\n"), nil
}

// RunLines executes a git command and returns output split by newline.
func (c *CLI) RunLines(args ...string) ([]string, error) {
	return c.RunLinesWithTimeout(commandTimeout, args...)
}

// RunLinesWithTimeout executes a git command with a custom timeout and returns lines.
func (c *CLI) RunLinesWithTimeout(timeout time.Duration, args ...string) ([]string, error) {
	out, err := c.RunWithTimeout(timeout, args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// IsRepo returns true if the path is inside a git repository.
func IsRepo(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

// RootOf returns the root of the git repo containing path.
func RootOf(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--show-toplevel")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
