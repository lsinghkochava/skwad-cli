package git

import "testing"

func TestSuggestedPath_SimpleBranch(t *testing.T) {
	got := SuggestedPath("/home/user/myrepo", "feature-foo")
	want := "/home/user/myrepo-feature-foo"
	if got != want {
		t.Errorf("SuggestedPath = %q, want %q", got, want)
	}
}

func TestSuggestedPath_SlashInBranch(t *testing.T) {
	got := SuggestedPath("/home/user/myrepo", "feature/foo")
	want := "/home/user/myrepo-feature-foo"
	if got != want {
		t.Errorf("SuggestedPath = %q, want %q", got, want)
	}
}

func TestSuggestedPath_SpaceInBranch(t *testing.T) {
	got := SuggestedPath("/home/user/myrepo", "my branch")
	want := "/home/user/myrepo-my-branch"
	if got != want {
		t.Errorf("SuggestedPath = %q, want %q", got, want)
	}
}

func TestSuggestedPath_MultipleSlashes(t *testing.T) {
	got := SuggestedPath("/home/user/myrepo", "feature/sub/task")
	want := "/home/user/myrepo-feature-sub-task"
	if got != want {
		t.Errorf("SuggestedPath = %q, want %q", got, want)
	}
}

func TestSuggestedPath_MixedSlashesAndSpaces(t *testing.T) {
	got := SuggestedPath("/home/user/myrepo", "feature/my task")
	want := "/home/user/myrepo-feature-my-task"
	if got != want {
		t.Errorf("SuggestedPath = %q, want %q", got, want)
	}
}

func TestNewWorktreeManager(t *testing.T) {
	m := NewWorktreeManager("/tmp/repo")
	if m == nil {
		t.Fatal("expected non-nil WorktreeManager")
	}
	if m.cli.RepoPath != "/tmp/repo" {
		t.Errorf("expected RepoPath=/tmp/repo, got %q", m.cli.RepoPath)
	}
}
