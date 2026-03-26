package report

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// PostPRComment posts the report as a PR comment, replacing any previous skwad comment.
// Requires the `gh` CLI to be installed and authenticated.
func PostPRComment(prRef string, r *RunReport) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI is required for github-pr-comment format. Install from https://cli.github.com")
	}

	// Parse prRef to extract owner/repo and PR number.
	owner, repo, prNum, err := parsePRRef(prRef)
	if err != nil {
		return err
	}

	// Find and minimize existing skwad comments.
	if err := minimizeOldComments(owner, repo, prNum); err != nil {
		// Non-fatal — continue to post new comment.
		_ = err
	}

	// Build comment body.
	body := BuildCommentBody(r)

	// Post new comment.
	cmd := exec.Command("gh", "pr", "comment", prRef, "--body", body)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr comment: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return nil
}

// parsePRRef extracts owner, repo, and PR number from a PR reference.
// Supports: "123", "owner/repo#123", "https://github.com/owner/repo/pull/123"
func parsePRRef(ref string) (owner, repo, prNum string, err error) {
	// Simple number: use current repo context.
	if !strings.Contains(ref, "/") && !strings.Contains(ref, "#") {
		return "", "", ref, nil
	}

	// owner/repo#123
	if strings.Contains(ref, "#") {
		parts := strings.SplitN(ref, "#", 2)
		repoParts := strings.SplitN(parts[0], "/", 2)
		if len(repoParts) != 2 {
			return "", "", "", fmt.Errorf("invalid PR reference: %s", ref)
		}
		return repoParts[0], repoParts[1], parts[1], nil
	}

	// URL: https://github.com/owner/repo/pull/123
	if strings.Contains(ref, "github.com") {
		ref = strings.TrimSuffix(ref, "/")
		parts := strings.Split(ref, "/")
		if len(parts) >= 5 {
			return parts[len(parts)-4], parts[len(parts)-3], parts[len(parts)-1], nil
		}
	}

	return "", "", "", fmt.Errorf("invalid PR reference: %s", ref)
}

// minimizeOldComments finds and deletes previous skwad review comments.
func minimizeOldComments(owner, repo, prNum string) error {
	// If owner/repo not provided, skip — gh will use repo context.
	if owner == "" || repo == "" {
		return nil
	}

	endpoint := fmt.Sprintf("repos/%s/%s/issues/%s/comments", owner, repo, prNum)
	out, err := exec.Command("gh", "api", endpoint).Output()
	if err != nil {
		return err
	}

	var comments []struct {
		ID   int    `json:"id"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &comments); err != nil {
		return err
	}

	for _, c := range comments {
		if strings.Contains(c.Body, CommentMarker) {
			deleteEndpoint := fmt.Sprintf("repos/%s/%s/issues/comments/%d", owner, repo, c.ID)
			_ = exec.Command("gh", "api", "-X", "DELETE", deleteEndpoint).Run()
		}
	}

	return nil
}
