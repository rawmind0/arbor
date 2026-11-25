package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Client struct {
	workDir string
}

func NewClient(workDir string) *Client {
	if workDir == "" {
		workDir = "."
	}
	return &Client{workDir: workDir}
}

// IsGitRepo reports whether the working directory is inside a git repository
// (any subdirectory, a worktree or a submodule, where .git is a file); false
// when git is not installed.
func (c *Client) IsGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = c.workDir
	return cmd.Run() == nil
}

// HasCommits checks if the repository has any commits
func (c *Client) HasCommits() bool {
	if !c.IsGitRepo() {
		return false
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = c.workDir
	err := cmd.Run()
	return err == nil
}

// DefaultBaseRefs are the branches tried, in order, when no base ref is
// configured: the usual names of the branch work gets merged into.
var DefaultBaseRefs = []string{"master", "main"}

// ResolveBaseRef returns the ref changes are measured from. A configured ref
// must exist; with none configured, the first existing DefaultBaseRefs branch
// is used. origin/HEAD is deliberately not consulted: only `git clone` creates
// it, so it is missing in many checkouts.
func (c *Client) ResolveBaseRef(ref string) (string, error) {
	if ref != "" {
		if !c.refExists(ref) {
			return "", fmt.Errorf("base ref %q not found; set --base-ref or config.base_ref", ref)
		}
		return ref, nil
	}
	for _, candidate := range DefaultBaseRefs {
		if c.refExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("base ref not found (tried %s); set --base-ref or config.base_ref",
		strings.Join(DefaultBaseRefs, ", "))
}

// refExists reports whether ref resolves to a commit.
func (c *Client) refExists(ref string) bool {
	_, err := c.revParse(ref)
	return err == nil
}

// MergeBase returns the commit where HEAD's history split from ref
// (`git merge-base <ref> HEAD`). When ref is an ancestor of HEAD (e.g. HEAD~1,
// or the branch itself), that is ref's own commit.
func (c *Client) MergeBase(ref string) (string, error) {
	cmd := exec.Command("git", "merge-base", ref, "HEAD")
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find merge base of %q and HEAD: %w", ref, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// ChangedSince returns the point changes are measured from, plus every tracked
// file changed from it to the working tree.
//
// On a branch (HEAD has commits that baseRef does not) that point is the merge
// base of baseRef and HEAD, so every commit on the branch plus uncommitted
// changes count. On the base branch itself (merge base == HEAD, e.g. CI on
// master after a merge) it is HEAD when there are uncommitted changes, otherwise
// HEAD~1 — what the last commit brought in — or the empty tree for a repository
// with a single commit. baseRef is resolved with ResolveBaseRef.
func (c *Client) ChangedSince(baseRef string) (string, []string, error) {
	if !c.IsGitRepo() {
		return "", nil, fmt.Errorf("not in a git repository")
	}
	if !c.HasCommits() {
		return "", nil, fmt.Errorf("no commits found in repository")
	}

	ref, err := c.ResolveBaseRef(baseRef)
	if err != nil {
		return "", nil, err
	}
	base, err := c.MergeBase(ref)
	if err != nil {
		return "", nil, err
	}
	if head, err := c.revParse("HEAD"); err == nil && head == base {
		if base, err = c.baseOnTargetBranch(); err != nil {
			return "", nil, err
		}
	}

	cmd := exec.Command("git", "diff", "--name-only", base)
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get changed files: %w", err)
	}
	// git diff never lists untracked files: new files count as changed too.
	untracked, err := c.untrackedFiles()
	if err != nil {
		return "", nil, err
	}
	return base, append(splitLines(output), untracked...), nil
}

// untrackedFiles returns the files that are not tracked and not ignored, with
// repository-relative paths like git diff --name-only.
func (c *Client) untrackedFiles() ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard", "--full-name")
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list untracked files: %w", err)
	}
	return splitLines(output), nil
}

// baseOnTargetBranch is the base used when HEAD is on the base branch:
// HEAD with uncommitted changes, else HEAD~1, else the empty tree.
func (c *Client) baseOnTargetBranch() (string, error) {
	uncommitted, err := c.GetUncommittedFiles()
	if err != nil {
		return "", err
	}
	if len(uncommitted) > 0 {
		return c.revParse("HEAD")
	}
	if parent, err := c.revParse("HEAD~1"); err == nil {
		return parent, nil
	}
	// Single commit: everything in it is new.
	cmd := exec.Command("git", "hash-object", "-t", "tree", os.DevNull)
	cmd.Dir = c.workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get the empty tree: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// revParse resolves a revision to its commit hash.
func (c *Client) revParse(rev string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	cmd.Dir = c.workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetChangedFiles returns the files changed since the base (see ChangedSince).
func (c *Client) GetChangedFiles(baseRef string) ([]string, error) {
	_, files, err := c.ChangedSince(baseRef)
	return files, err
}

// ShowFile returns the content of path (relative to the client's working
// directory, or absolute) at ref. It returns an error wrapping os.ErrNotExist
// when the file does not exist at that ref.
func (c *Client) ShowFile(ref, path string) ([]byte, error) {
	if filepath.IsAbs(path) {
		dir, err := filepath.Abs(c.workDir)
		if err != nil {
			return nil, err
		}
		if path, err = filepath.Rel(dir, path); err != nil {
			return nil, err
		}
	}
	spec := ref + ":./" + filepath.ToSlash(filepath.Clean(path))

	exists := exec.Command("git", "cat-file", "-e", spec)
	exists.Dir = c.workDir
	if exists.Run() != nil {
		return nil, fmt.Errorf("%s at %s: %w", path, ref, os.ErrNotExist)
	}

	cmd := exec.Command("git", "show", spec)
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read %s at %s: %w", path, ref, err)
	}
	return output, nil
}

// splitLines turns command output into its non-empty, trimmed lines.
func splitLines(output []byte) []string {
	files := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files
}

// GetUncommittedFiles returns the files that have been modified but not
// committed, untracked (not ignored) files included
func (c *Client) GetUncommittedFiles() ([]string, error) {
	if !c.IsGitRepo() {
		return nil, fmt.Errorf("not in a git repository")
	}

	// Get staged and unstaged changes
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		// If no HEAD exists (no commits), get all files
		cmd = exec.Command("git", "ls-files")
		cmd.Dir = c.workDir
		output, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to get files: %w", err)
		}
	}

	untracked, err := c.untrackedFiles()
	if err != nil {
		return nil, err
	}
	return append(splitLines(output), untracked...), nil
}

// GetWorkingDirectory returns the git working directory
func (c *Client) GetWorkingDirectory() (string, error) {
	if !c.IsGitRepo() {
		return "", fmt.Errorf("not in a git repository")
	}

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetCurrentBranch returns the current branch name
func (c *Client) GetCurrentBranch() (string, error) {
	if !c.IsGitRepo() {
		return "", fmt.Errorf("not in a git repository")
	}

	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// IsCleanWorkingTree checks if the working tree is clean (no uncommitted changes)
func (c *Client) IsCleanWorkingTree() (bool, error) {
	if !c.IsGitRepo() {
		return false, fmt.Errorf("not in a git repository")
	}

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check working tree status: %w", err)
	}

	return strings.TrimSpace(string(output)) == "", nil
}
