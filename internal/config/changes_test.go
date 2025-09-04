package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewChangeDetector(t *testing.T) {
	cfg := &Config{
		Config: ConfigSection{
			Dockerfiles: DockerfilesConfig{
				Source: "src",
			},
		},
	}

	detector := NewChangeDetector(cfg, "/tmp")
	if detector == nil {
		t.Error("Expected detector to be created")
	}
	if detector.config != cfg {
		t.Error("Expected detector to have reference to config")
	}
	if detector.gitClient == nil {
		t.Error("Expected detector to have git client")
	}
}

func TestChangeDetector_markAllImagesChanged(t *testing.T) {
	cfg := &Config{
		Images: map[string]*ImageConfig{
			"parent": {
				Name: "parent",
				Images: map[string]*ImageConfig{
					"child": {
						Name: "child",
						Images: map[string]*ImageConfig{
							"grandchild": {
								Name: "grandchild",
							},
						},
					},
				},
			},
		},
	}

	detector := NewChangeDetector(cfg, ".")
	detector.markAllImagesChanged()

	// Verify all images are marked as changed
	if !cfg.Images["parent"].Changed {
		t.Error("Expected parent image to be marked as changed")
	}
	if !cfg.Images["parent"].Images["child"].Changed {
		t.Error("Expected child image to be marked as changed")
	}
	if !cfg.Images["parent"].Images["child"].Images["grandchild"].Changed {
		t.Error("Expected grandchild image to be marked as changed")
	}
}

func TestChangeDetector_isDockerfileTemplate(t *testing.T) {
	cfg := &Config{
		Config: ConfigSection{
			Dockerfiles: DockerfilesConfig{
				Source: "src",
			},
		},
	}

	detector := NewChangeDetector(cfg, ".")

	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{
			name:     "dockerfile in source dir",
			filePath: "src/Dockerfile.test",
			want:     true,
		},
		{
			name:     "dockerfile with different extension",
			filePath: "src/test.dockerfile",
			want:     true,
		},
		{
			name:     "dockerfile contains name",
			filePath: "src/custom-dockerfile",
			want:     true,
		},
		{
			name:     "not in source dir",
			filePath: "other/Dockerfile.test",
			want:     false,
		},
		{
			name:     "not a dockerfile",
			filePath: "src/config.yaml",
			want:     false,
		},
		{
			name:     "source dir itself",
			filePath: "src",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detector.isDockerfileTemplate(tt.filePath)
			if got != tt.want {
				t.Errorf("isDockerfileTemplate(%s) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestChangeDetector_analyzeChangedFiles(t *testing.T) {
	cfg := &Config{
		Config: ConfigSection{
			Dockerfiles: DockerfilesConfig{
				Source: "src",
			},
		},
		Images: map[string]*ImageConfig{
			"test1": {
				Name:       "test1",
				Dockerfile: "Dockerfile.test1",
			},
			"test2": {
				Name:       "test2",
				Dockerfile: "Dockerfile.test2",
			},
		},
	}

	detector := NewChangeDetector(cfg, ".")

	tests := []struct {
		name         string
		changedFiles []string
		expectAll    bool
		expectImages []string
	}{
		{
			name:         "config file changed",
			changedFiles: []string{"config.yaml", "src/Dockerfile.other"},
			expectAll:    true,
			expectImages: nil, // All images marked, specific checking not needed
		},
		{
			name:         "specific dockerfile changed",
			changedFiles: []string{"src/Dockerfile.test1"},
			expectAll:    false,
			expectImages: []string{"test1"},
		},
		{
			name:         "multiple dockerfiles changed",
			changedFiles: []string{"src/Dockerfile.test1", "src/Dockerfile.test2"},
			expectAll:    false,
			expectImages: []string{"test1", "test2"},
		},
		{
			name:         "non-docker files changed",
			changedFiles: []string{"README.md", "other/file.txt"},
			expectAll:    false,
			expectImages: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset changed flags
			for _, img := range cfg.Images {
				img.Changed = false
			}

			detector.analyzeChangedFiles(tt.changedFiles)

			if tt.expectAll {
				// Check that all images are marked as changed
				for name, img := range cfg.Images {
					if !img.Changed {
						t.Errorf("Expected image %s to be marked as changed", name)
					}
				}
			} else {
				// Check specific images
				changedImages := detector.GetChangedImageNames()
				if len(changedImages) != len(tt.expectImages) {
					t.Errorf("Expected %d changed images, got %d", len(tt.expectImages), len(changedImages))
				}

				for _, expected := range tt.expectImages {
					found := false
					for _, changed := range changedImages {
						if changed == expected {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected image %s to be changed, but it wasn't", expected)
					}
				}
			}
		})
	}
}

func TestChangeDetector_GetChangedImageNames(t *testing.T) {
	cfg := &Config{
		Images: map[string]*ImageConfig{
			"parent": {
				Name:    "parent",
				Changed: true,
				Images: map[string]*ImageConfig{
					"child1": {
						Name:    "child1",
						Changed: false,
					},
					"child2": {
						Name:    "child2",
						Changed: true,
					},
				},
			},
			"other": {
				Name:    "other",
				Changed: false,
			},
		},
	}

	detector := NewChangeDetector(cfg, ".")
	changedNames := detector.GetChangedImageNames()

	expected := []string{"parent", "parent/child2"}
	if len(changedNames) != len(expected) {
		t.Errorf("Expected %d changed images, got %d", len(expected), len(changedNames))
	}

	for _, expectedName := range expected {
		found := false
		for _, name := range changedNames {
			if name == expectedName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find changed image %s", expectedName)
		}
	}
}

func TestChangeDetector_ShouldBuildAll(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "change-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Images: map[string]*ImageConfig{
			"test": {
				Name:    "test",
				Changed: false,
			},
		},
	}

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
			want: true,
		},
		{
			name: "git repo but no commits",
			setupDir: func(dir string) error {
				return os.Mkdir(filepath.Join(dir, ".git"), 0755)
			},
			want: true,
		},
		{
			name: "no changed images",
			setupDir: func(dir string) error {
				return os.Mkdir(filepath.Join(dir, ".git"), 0755)
			},
			want: true,
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

			detector := NewChangeDetector(cfg, testDir)
			got := detector.ShouldBuildAll()
			if got != tt.want {
				t.Errorf("ShouldBuildAll() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChangeDetector_GetGitStatus(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "change-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{}
	detector := NewChangeDetector(cfg, tmpDir)
	status := detector.GetGitStatus()

	// Should have basic status fields
	if _, ok := status["is_git_repo"]; !ok {
		t.Error("Expected git status to include 'is_git_repo'")
	}
	if _, ok := status["has_commits"]; !ok {
		t.Error("Expected git status to include 'has_commits'")
	}

	// For non-git repo, should be false
	if status["is_git_repo"] != false {
		t.Error("Expected is_git_repo to be false for non-git directory")
	}
}
