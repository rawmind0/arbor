package git

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rawmind0/arbor/test/testutil"
)

// newRepo creates a repository on branch with one commit containing a.txt.
func newRepo(t *testing.T, branch string) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t, branch)
	r.Write("a.txt", "one\n")
	r.Commit("init")
	return r
}

func TestResolveBaseRef(t *testing.T) {
	t.Run("defaults to master", func(t *testing.T) {
		r := newRepo(t, "master")
		r.Git("branch", "main")
		if got, err := NewClient(r.Dir).ResolveBaseRef(""); err != nil || got != "master" {
			t.Fatalf("ResolveBaseRef(\"\") = %q, %v; want master", got, err)
		}
	})
	t.Run("falls back to main", func(t *testing.T) {
		r := newRepo(t, "main")
		if got, err := NewClient(r.Dir).ResolveBaseRef(""); err != nil || got != "main" {
			t.Fatalf("ResolveBaseRef(\"\") = %q, %v; want main", got, err)
		}
	})
	t.Run("no default branch", func(t *testing.T) {
		r := newRepo(t, "develop")
		_, err := NewClient(r.Dir).ResolveBaseRef("")
		if err == nil || !strings.Contains(err.Error(), "base ref not found") {
			t.Fatalf("ResolveBaseRef(\"\") error = %v, want 'base ref not found'", err)
		}
	})
	t.Run("explicit ref", func(t *testing.T) {
		r := newRepo(t, "develop")
		if got, err := NewClient(r.Dir).ResolveBaseRef("develop"); err != nil || got != "develop" {
			t.Fatalf("ResolveBaseRef(develop) = %q, %v", got, err)
		}
	})
	t.Run("unknown explicit ref", func(t *testing.T) {
		r := newRepo(t, "master")
		_, err := NewClient(r.Dir).ResolveBaseRef("nope")
		if err == nil || !strings.Contains(err.Error(), `base ref "nope" not found`) {
			t.Fatalf("ResolveBaseRef(nope) error = %v", err)
		}
	})
}

func TestMergeBase(t *testing.T) {
	r := newRepo(t, "master")
	fork := r.Git("rev-parse", "HEAD")
	r.Git("checkout", "-q", "-b", "feature")
	r.Write("b.txt", "b\n")
	r.Commit("feature work")
	r.Git("checkout", "-q", "master")
	r.Write("c.txt", "c\n")
	r.Commit("master moves on")
	r.Git("checkout", "-q", "feature")

	c := NewClient(r.Dir)
	if got, err := c.MergeBase("master"); err != nil || got != fork {
		t.Fatalf("MergeBase(master) = %q, %v; want fork point %q", got, err, fork)
	}
	// An ancestor of HEAD is its own merge base.
	if got, err := c.MergeBase("HEAD~1"); err != nil || got != fork {
		t.Fatalf("MergeBase(HEAD~1) = %q, %v; want %q", got, err, fork)
	}
}

func changedSince(t *testing.T, r *testutil.Repo, ref string) (string, []string) {
	t.Helper()
	base, files, err := NewClient(r.Dir).ChangedSince(ref)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return base, files
}

// On a branch: everything since the merge base — every commit plus uncommitted.
func TestChangedSince_Branch(t *testing.T) {
	r := newRepo(t, "master")
	r.Git("checkout", "-q", "-b", "feature")
	r.Write("b.txt", "b\n")
	r.Commit("first")
	r.Write("c.txt", "c\n")
	r.Commit("second")
	r.Write("a.txt", "uncommitted\n")

	base, files := changedSince(t, r, "")
	if want := r.Git("rev-parse", "master"); base != want {
		t.Errorf("base = %q, want master %q", base, want)
	}
	if want := []string{"a.txt", "b.txt", "c.txt"}; !slices.Equal(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
}

// On the base branch itself: uncommitted changes if any, else the last commit.
func TestChangedSince_BaseBranch(t *testing.T) {
	r := newRepo(t, "master")
	r.Write("b.txt", "b\n")
	r.Commit("second")

	if _, files := changedSince(t, r, ""); !slices.Equal(files, []string{"b.txt"}) {
		t.Errorf("clean tree: files = %v, want the last commit's [b.txt]", files)
	}
	r.Write("a.txt", "uncommitted\n")
	if _, files := changedSince(t, r, ""); !slices.Equal(files, []string{"a.txt"}) {
		t.Errorf("uncommitted: files = %v, want only [a.txt]", files)
	}
}

// Untracked (not ignored) files count as changes: on a branch, and on the base
// branch, where they are uncommitted changes like any other.
func TestChangedSince_UntrackedFiles(t *testing.T) {
	r := newRepo(t, "master")
	r.Write(".gitignore", "*.log\n")
	r.Write("b.txt", "b\n")
	r.Commit("second")
	r.Write("new.txt", "new\n")
	r.Write("ignored.log", "x\n")

	if _, files := changedSince(t, r, ""); !slices.Equal(files, []string{"new.txt"}) {
		t.Errorf("base branch: files = %v, want the uncommitted [new.txt]", files)
	}
	r.Git("checkout", "-q", "-b", "feature")
	if _, files := changedSince(t, r, ""); !slices.Equal(files, []string{"new.txt"}) {
		t.Errorf("branch: files = %v, want [new.txt]", files)
	}
}

// A single-commit repository on its base branch: everything is new.
func TestChangedSince_SingleCommit(t *testing.T) {
	r := newRepo(t, "master")
	if _, files := changedSince(t, r, ""); !slices.Equal(files, []string{"a.txt"}) {
		t.Errorf("files = %v, want [a.txt]", files)
	}
}

func TestShowFile(t *testing.T) {
	r := newRepo(t, "master")
	r.Write("a.txt", "two\n")
	r.Commit("second")

	c := NewClient(r.Dir)
	got, err := c.ShowFile("HEAD~1", "a.txt")
	if err != nil || string(got) != "one\n" {
		t.Fatalf("ShowFile(HEAD~1, a.txt) = %q, %v; want \"one\\n\"", got, err)
	}
	if got, err := c.ShowFile("HEAD~1", r.Dir+"/a.txt"); err != nil || string(got) != "one\n" {
		t.Fatalf("ShowFile with an absolute path = %q, %v", got, err)
	}
	if _, err := c.ShowFile("HEAD~1", "missing.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ShowFile(missing) error = %v, want os.ErrNotExist", err)
	}
}
