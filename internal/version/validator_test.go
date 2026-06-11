package version

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
)

func TestNewValidator(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Dockerfiles: config.DockerfilesConfig{
				Source: "src",
			},
		},
	}

	validator := NewValidator(cfg)
	if validator == nil {
		t.Fatal("NewValidator() returned nil")
	}

	if validator.config != cfg {
		t.Error("Validator config not set correctly")
	}

	if validator.sourceDir != "src" {
		t.Errorf("Source dir = %s, want 'src'", validator.sourceDir)
	}
}

func TestValidator_buildImageGraph(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Dockerfiles: config.DockerfilesConfig{
				Source: "src",
			},
		},
		Images: map[string]*config.ImageConfig{
			"parent": {
				Name:    "parent",
				Version: "1.0.0",
				Images: map[string]*config.ImageConfig{
					"child1": {
						Name:    "child1",
						Version: "1.0.0",
					},
					"child2": {
						Name:    "child2",
						Version: "1.0.0",
						Images: map[string]*config.ImageConfig{
							"grandchild": {
								Name:    "grandchild",
								Version: "1.0.0",
							},
						},
					},
				},
			},
		},
	}

	validator := NewValidator(cfg)
	validator.buildImageGraph()

	// Check parent -> children mapping
	if len(validator.imageGraph["parent"]) != 2 {
		t.Errorf("Parent should have 2 children, got %d", len(validator.imageGraph["parent"]))
	}

	if len(validator.imageGraph["child2"]) != 1 {
		t.Errorf("Child2 should have 1 child, got %d", len(validator.imageGraph["child2"]))
	}

	if len(validator.imageGraph["child1"]) != 0 {
		t.Errorf("Child1 should have no children, got %d", len(validator.imageGraph["child1"]))
	}
}

func TestValidator_validateSemverCompliance(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *config.Config
		wantError bool
		errCount  int
	}{
		{
			name: "valid semver versions",
			cfg: &config.Config{
				Images: map[string]*config.ImageConfig{
					"test": {
						Name:    "test",
						Version: "1.0.0",
					},
				},
			},
			wantError: false,
		},
		{
			name: "invalid semver version",
			cfg: &config.Config{
				Images: map[string]*config.ImageConfig{
					"test": {
						Name:    "test",
						Version: "invalid",
					},
				},
			},
			wantError: true,
			errCount:  1,
		},
		{
			name: "missing version",
			cfg: &config.Config{
				Images: map[string]*config.ImageConfig{
					"test": {
						Name:    "test",
						Version: "",
					},
				},
			},
			wantError: true,
			errCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewValidator(tt.cfg)
			result := &ValidationResult{
				Errors: []string{},
			}

			err := validator.validateSemverCompliance(result)
			if err != nil {
				t.Errorf("validateSemverCompliance() returned error: %v", err)
			}

			if tt.wantError && len(result.Errors) == 0 {
				t.Error("Expected errors but got none")
			}

			if !tt.wantError && len(result.Errors) > 0 {
				t.Errorf("Unexpected errors: %v", result.Errors)
			}

			if tt.wantError && len(result.Errors) != tt.errCount {
				t.Errorf("Expected %d errors, got %d", tt.errCount, len(result.Errors))
			}
		})
	}
}

func TestValidator_proposeVersionUpdate(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"test": {
				Name:    "test",
				Version: "1.2.3",
			},
		},
	}

	validator := NewValidator(cfg)
	result := &ValidationResult{
		Errors: []string{},
	}

	img := cfg.Images["test"]

	t.Run("patch update", func(t *testing.T) {
		validator.updates = make(map[string]VersionUpdate)
		validator.proposeVersionUpdate(img, UpdateTypePatch, "test reason", result)

		update, exists := validator.updates["test"]
		if !exists {
			t.Fatal("Update not proposed")
		}

		if update.NewVersion != "1.2.4" {
			t.Errorf("Expected version 1.2.4, got %s", update.NewVersion)
		}
	})

	t.Run("minor update", func(t *testing.T) {
		validator.updates = make(map[string]VersionUpdate)
		validator.proposeVersionUpdate(img, UpdateTypeMinor, "test reason", result)

		update, exists := validator.updates["test"]
		if !exists {
			t.Fatal("Update not proposed")
		}

		if update.NewVersion != "1.3.0" {
			t.Errorf("Expected version 1.3.0, got %s", update.NewVersion)
		}
	})

	t.Run("major update", func(t *testing.T) {
		validator.updates = make(map[string]VersionUpdate)
		validator.proposeVersionUpdate(img, UpdateTypeMajor, "test reason", result)

		update, exists := validator.updates["test"]
		if !exists {
			t.Fatal("Update not proposed")
		}

		if update.NewVersion != "2.0.0" {
			t.Errorf("Expected version 2.0.0, got %s", update.NewVersion)
		}
	})
}

func TestValidator_cascadeVersionUpdate(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"parent": {
				Name:    "parent",
				Version: "1.0.0",
				Images: map[string]*config.ImageConfig{
					"child": {
						Name:    "child",
						Version: "1.0.0",
						Parent:  "parent",
					},
				},
			},
		},
	}

	validator := NewValidator(cfg)
	validator.buildImageGraph()

	result := &ValidationResult{
		Errors: []string{},
	}

	// Propose parent update
	parent := cfg.Images["parent"]
	validator.proposeVersionUpdate(parent, UpdateTypePatch, "parent changed", result)

	// Check if child was cascaded
	if len(validator.updates) != 2 {
		t.Errorf("Expected 2 updates (parent + child), got %d", len(validator.updates))
	}

	childUpdate, exists := validator.updates["child"]
	if !exists {
		t.Fatal("Child update not cascaded")
	}

	if childUpdate.NewVersion != "1.0.1" {
		t.Errorf("Child version should be 1.0.1, got %s", childUpdate.NewVersion)
	}
}

func TestValidator_isDockerfileTemplate(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		sourceDir string
		want      bool
	}{
		{
			name:      "dockerfile in source dir",
			path:      "src/Dockerfile.test",
			sourceDir: "src",
			want:      true,
		},
		{
			name:      "dockerfile with arch",
			path:      "src/Dockerfile.test-amd64",
			sourceDir: "src",
			want:      true,
		},
		{
			name:      "not a dockerfile",
			path:      "src/test.txt",
			sourceDir: "src",
			want:      false,
		},
		{
			name:      "dockerfile outside source",
			path:      "other/Dockerfile.test",
			sourceDir: "src",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &Validator{
				sourceDir: tt.sourceDir,
			}

			if got := validator.isDockerfileTemplate(tt.path); got != tt.want {
				t.Errorf("isDockerfileTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidator_getImageNameFromDockerfile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple name",
			path:     "src/Dockerfile.test",
			expected: "test",
		},
		{
			name:     "with amd64 arch",
			path:     "src/Dockerfile.test-amd64",
			expected: "test",
		},
		{
			name:     "with arm64 arch",
			path:     "src/Dockerfile.test-arm64",
			expected: "test",
		},
		{
			name:     "hyphenated name",
			path:     "src/Dockerfile.my-app",
			expected: "my-app",
		},
		{
			name:     "hyphenated with arch",
			path:     "src/Dockerfile.my-app-amd64",
			expected: "my-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &Validator{}

			if got := validator.getImageNameFromDockerfile(tt.path); got != tt.expected {
				t.Errorf("getImageNameFromDockerfile() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestValidator_ApplyUpdates(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"test1": {
				Name:    "test1",
				Version: "1.0.0",
			},
			"test2": {
				Name:    "test2",
				Version: "2.0.0",
			},
		},
	}

	updates := map[string]VersionUpdate{
		"test1": {
			ImageName:  "test1",
			OldVersion: "1.0.0",
			NewVersion: "1.0.1",
		},
		"test2": {
			ImageName:  "test2",
			OldVersion: "2.0.0",
			NewVersion: "2.1.0",
		},
	}

	validator := NewValidator(cfg)

	err := validator.ApplyUpdates(updates)
	if err != nil {
		t.Fatalf("ApplyUpdates() error = %v", err)
	}

	if cfg.Images["test1"].Version != "1.0.1" {
		t.Errorf("test1 version = %s, want 1.0.1", cfg.Images["test1"].Version)
	}

	if cfg.Images["test2"].Version != "2.1.0" {
		t.Errorf("test2 version = %s, want 2.1.0", cfg.Images["test2"].Version)
	}
}

func TestValidator_GetLastModifiedTime(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "validator-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	dockerfile1 := filepath.Join(tmpDir, "Dockerfile.test")
	dockerfile2 := filepath.Join(tmpDir, "Dockerfile.test-amd64")

	if err := os.WriteFile(dockerfile1, []byte("FROM test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dockerfile2, []byte("FROM test"), 0644); err != nil {
		t.Fatal(err)
	}

	validator := &Validator{
		sourceDir: tmpDir,
	}

	t.Run("existing image", func(t *testing.T) {
		modTime, err := validator.GetLastModifiedTime("test")
		if err != nil {
			t.Fatalf("GetLastModifiedTime() error = %v", err)
		}
		if modTime.IsZero() {
			t.Error("Expected non-zero modification time")
		}
	})

	t.Run("non-existing image", func(t *testing.T) {
		_, err := validator.GetLastModifiedTime("nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent image")
		}
	})
}
