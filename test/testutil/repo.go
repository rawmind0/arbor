// Package testutil holds test helpers shared by every package: temporary git
// repositories and file helpers. It imports no arbor package, so any test
// (including tests inside internal/config) can use it without import cycles.
package testutil

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a temporary git repository that is also the test's working directory
// (arbor resolves config and Dockerfile paths relative to it).
type Repo struct {
	t   testing.TB
	Dir string
}

// NewRepo creates an empty git repository on branch in a new temp dir, isolated
// from the machine's git config (signing, hooks, default branch), and chdirs
// into it for the rest of the test.
func NewRepo(t testing.TB, branch string) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	r := &Repo{t: t, Dir: t.TempDir()}
	t.Chdir(r.Dir)
	r.Git("init", "-q", "-b", branch)
	return r
}

// Git runs a git command in the repository and returns its trimmed output.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Write writes content to path (relative to the repository), creating dirs.
func (r *Repo) Write(path, content string) {
	r.t.Helper()
	WriteFile(r.t, filepath.Join(r.Dir, path), content)
}

// Append appends content to path (relative to the repository).
func (r *Repo) Append(path, content string) {
	r.t.Helper()
	f, err := os.OpenFile(filepath.Join(r.Dir, path), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		r.t.Fatal(err)
	}
}

// Read returns the content of path (relative to the repository).
func (r *Repo) Read(path string) string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.Dir, path))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

// Replace replaces the first occurrence of old in path (relative to the
// repository) with new; old must be present.
func (r *Repo) Replace(path, old, new string) {
	r.t.Helper()
	content := r.Read(path)
	if !strings.Contains(content, old) {
		r.t.Fatalf("%s does not contain %q", path, old)
	}
	r.Write(path, strings.Replace(content, old, new, 1))
}

// Commit stages everything and commits it.
func (r *Repo) Commit(msg string) {
	r.t.Helper()
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", msg)
}

// CopyDir copies the directory tree src (any path) to dst (relative to the
// repository).
func (r *Repo) CopyDir(src, dst string) {
	r.t.Helper()
	CopyDir(r.t, src, filepath.Join(r.Dir, dst))
}

// WriteFile writes content to path, creating parent directories.
func WriteFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// CopyDir copies the directory tree src to dst.
func CopyDir(t testing.TB, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
}
