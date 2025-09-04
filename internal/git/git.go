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

// IsGitRepo checks if the current directory is inside a git repository
func (c *Client) IsGitRepo() bool {
	// Check for .git directory
	gitDir := filepath.Join(c.workDir, ".git")
	if info, err := os.Stat(gitDir); err == nil {
		return info.IsDir()
	}

	// Check if we're in a subdirectory of a git repo
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = c.workDir
	err := cmd.Run()
	return err == nil
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

// GetChangedFiles returns files that have changed since the given reference
func (c *Client) GetChangedFiles(baseRef string) ([]string, error) {
	if !c.IsGitRepo() {
		return nil, fmt.Errorf("not in a git repository")
	}

	if !c.HasCommits() {
		return nil, fmt.Errorf("no commits found in repository")
	}

	// Default to HEAD~1 if no base ref provided
	if baseRef == "" {
		baseRef = "HEAD~1"
	}

	// Get changed files using git diff
	cmd := exec.Command("git", "diff", "--name-only", baseRef)
	cmd.Dir = c.workDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get changed files: %w", err)
	}

	// Parse output into file list
	files := []string{}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
}

// GetUncommittedFiles returns files that have been modified but not committed
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

	// Parse output into file list
	files := []string{}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
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
