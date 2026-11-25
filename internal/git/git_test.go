package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rawmind0/arbor/test/testutil"
)

func TestNewClient_DefaultsToWorkingDirectory(t *testing.T) {
	if got := NewClient("").workDir; got != "." {
		t.Errorf("NewClient(\"\").workDir = %q, want \".\"", got)
	}
}

// Outside a repository nothing is reported as git and every query errors.
func TestNotAGitRepo(t *testing.T) {
	c := NewClient(t.TempDir())
	if c.IsGitRepo() || c.HasCommits() {
		t.Fatalf("plain directory: IsGitRepo=%v HasCommits=%v, want false", c.IsGitRepo(), c.HasCommits())
	}
	queries := map[string]func() error{
		"ChangedSince":        func() error { _, _, err := c.ChangedSince(""); return err },
		"GetUncommittedFiles": func() error { _, err := c.GetUncommittedFiles(); return err },
		"GetWorkingDirectory": func() error { _, err := c.GetWorkingDirectory(); return err },
		"GetCurrentBranch":    func() error { _, err := c.GetCurrentBranch(); return err },
		"IsCleanWorkingTree":  func() error { _, err := c.IsCleanWorkingTree(); return err },
	}
	for name, query := range queries {
		if query() == nil {
			t.Errorf("%s() = nil error outside a repository", name)
		}
	}
}

// A repository is detected from its root, a subdirectory and a worktree (whose
// .git is a file, not a directory).
func TestIsGitRepo(t *testing.T) {
	r := testutil.NewRepo(t, "master")
	if !NewClient(r.Dir).IsGitRepo() || NewClient(r.Dir).HasCommits() {
		t.Fatal("new repository: want IsGitRepo and no commits")
	}
	r.Write("sub/a.txt", "a\n")
	r.Commit("init")

	worktree := filepath.Join(t.TempDir(), "wt")
	r.Git("worktree", "add", "-q", "-b", "wt", worktree)
	if info, err := os.Stat(filepath.Join(worktree, ".git")); err != nil || info.IsDir() {
		t.Fatalf("worktree .git should be a file: %v", err)
	}
	for name, dir := range map[string]string{"root": r.Dir, "subdirectory": filepath.Join(r.Dir, "sub"), "worktree": worktree} {
		if c := NewClient(dir); !c.IsGitRepo() || !c.HasCommits() {
			t.Errorf("%s: IsGitRepo=%v HasCommits=%v, want both true", name, c.IsGitRepo(), c.HasCommits())
		}
	}
}

func TestStatus(t *testing.T) {
	r := testutil.NewRepo(t, "master")
	r.Write("a.txt", "a\n")
	r.Commit("init")
	c := NewClient(r.Dir)

	if branch, err := c.GetCurrentBranch(); err != nil || branch != "master" {
		t.Errorf("GetCurrentBranch() = %q, %v; want master", branch, err)
	}
	got, err := c.GetWorkingDirectory()
	want, _ := filepath.EvalSymlinks(r.Dir)
	if got, _ = filepath.EvalSymlinks(got); err != nil || got != want {
		t.Errorf("GetWorkingDirectory() = %q, %v; want %q", got, err, want)
	}
	if clean, err := c.IsCleanWorkingTree(); err != nil || !clean {
		t.Errorf("IsCleanWorkingTree() = %v, %v; want clean", clean, err)
	}
	r.Write("a.txt", "changed\n")
	if clean, err := c.IsCleanWorkingTree(); err != nil || clean {
		t.Errorf("after a change IsCleanWorkingTree() = %v, %v; want dirty", clean, err)
	}
}
