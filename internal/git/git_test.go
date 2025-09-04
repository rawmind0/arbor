package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		workDir string
		want    string
	}{
		{
			name:    "with working directory",
			workDir: "/tmp/test",
			want:    "/tmp/test",
		},
		{
			name:    "empty working directory",
			workDir: "",
			want:    ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.workDir)
			if client.workDir != tt.want {
				t.Errorf("NewClient() workDir = %v, want %v", client.workDir, tt.want)
			}
		})
	}
}

func TestIsGitRepo(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name     string
		setupDir func(string) error
		want     bool
	}{
		{
			name: "not a git repo",
			setupDir: func(dir string) error {
				return nil // No setup needed
			},
			want: false,
		},
		{
			name: "has .git directory",
			setupDir: func(dir string) error {
				return os.Mkdir(filepath.Join(dir, ".git"), 0755)
			},
			want: true,
		},
		{
			name: "has .git file (not directory)",
			setupDir: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /path/to/git"), 0644)
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := filepath.Join(tmpDir, tt.name)
			if err := os.Mkdir(testDir, 0755); err != nil {
				t.Fatalf("Failed to create test dir: %v", err)
			}

			if err := tt.setupDir(testDir); err != nil {
				t.Fatalf("Failed to setup test dir: %v", err)
			}

			client := NewClient(testDir)
			got := client.IsGitRepo()
			if got != tt.want {
				t.Errorf("IsGitRepo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasCommits(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewClient(tmpDir)

	// Test without git repo
	if client.HasCommits() {
		t.Error("HasCommits() should return false for non-git repo")
	}

	// Create .git directory
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}

	// Test with git repo but no commits (this will likely fail the git command)
	// In a real test, we'd need to initialize a proper git repo
	hasCommits := client.HasCommits()
	// We expect this to be false since we didn't initialize a proper git repo
	if hasCommits {
		t.Error("HasCommits() should return false for git repo without commits")
	}
}

func TestGetChangedFiles_NotGitRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewClient(tmpDir)
	_, err = client.GetChangedFiles("HEAD~1")
	if err == nil {
		t.Error("GetChangedFiles() should return error for non-git repo")
	}
}

func TestGetUncommittedFiles_NotGitRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewClient(tmpDir)
	_, err = client.GetUncommittedFiles()
	if err == nil {
		t.Error("GetUncommittedFiles() should return error for non-git repo")
	}
}

func TestGetWorkingDirectory_NotGitRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewClient(tmpDir)
	_, err = client.GetWorkingDirectory()
	if err == nil {
		t.Error("GetWorkingDirectory() should return error for non-git repo")
	}
}

func TestGetCurrentBranch_NotGitRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewClient(tmpDir)
	_, err = client.GetCurrentBranch()
	if err == nil {
		t.Error("GetCurrentBranch() should return error for non-git repo")
	}
}

func TestIsCleanWorkingTree_NotGitRepo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewClient(tmpDir)
	_, err = client.IsCleanWorkingTree()
	if err == nil {
		t.Error("IsCleanWorkingTree() should return error for non-git repo")
	}
}
