package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/builder"
)

func TestLoadConfig(t *testing.T) {
	// Create temporary directory for test config
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name       string
		configYAML string
		wantErr    bool
		validate   func(*Config) error
	}{
		{
			name: "valid basic config",
			configYAML: `
config:
  builder: docker
  dockerfiles:
    source: src
    destination: build
images:
  ubuntu:
    version: 22.04
    base_image: ubuntu:22.04
`,
			wantErr: false,
			validate: func(cfg *Config) error {
				if cfg.Config.Builder != "docker" {
					t.Errorf("Expected builder 'docker', got '%s'", cfg.Config.Builder)
				}
				if cfg.Config.Workers != 2 {
					t.Errorf("Expected default workers 2, got %d", cfg.Config.Workers)
				}
				if len(cfg.Config.Architectures) != 1 || cfg.Config.Architectures[0] != "linux/amd64" {
					t.Errorf("Expected default architecture [linux/amd64], got %v", cfg.Config.Architectures)
				}
				return nil
			},
		},
		{
			name: "config with hierarchical images",
			configYAML: `
config:
  builder: docker
  workers: 4
  architectures:
    - linux/amd64
    - linux/arm64
  dockerfiles:
    source: templates
    destination: generated
images:
  base:
    version: 1.0.0
    base_image: ubuntu:22.04
    args:
      BASE_ARG: value1
    envs:
      BASE_ENV: value2
    images:
      child:
        version: 2.0.0
        args:
          CHILD_ARG: value3
`,
			wantErr: false,
			validate: func(cfg *Config) error {
				if cfg.Config.Workers != 4 {
					t.Errorf("Expected workers 4, got %d", cfg.Config.Workers)
				}
				if len(cfg.Config.Architectures) != 2 {
					t.Errorf("Expected 2 architectures, got %d", len(cfg.Config.Architectures))
				}

				baseImg := cfg.Images["base"]
				if baseImg == nil {
					t.Fatal("Expected 'base' image to exist")
				}
				if baseImg.Name != "base" {
					t.Errorf("Expected image name 'base', got '%s'", baseImg.Name)
				}
				if baseImg.FullName != "base" {
					t.Errorf("Expected full name 'base', got '%s'", baseImg.FullName)
				}

				childImg := baseImg.Images["child"]
				if childImg == nil {
					t.Fatal("Expected 'child' image to exist")
				}
				if childImg.Parent != "base" {
					t.Errorf("Expected parent 'base', got '%s'", childImg.Parent)
				}
				if childImg.FullName != "base/child" {
					t.Errorf("Expected full name 'base/child', got '%s'", childImg.FullName)
				}

				return nil
			},
		},
		{
			name: "invalid yaml",
			configYAML: `
config:
  builder: docker
  invalid_yaml: [
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, tt.name+".yaml")
			if err := os.WriteFile(configPath, []byte(tt.configYAML), 0644); err != nil {
				t.Fatalf("Failed to write config file: %v", err)
			}

			cfg, err := LoadConfig(configPath, nil)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.validate != nil {
				if err := tt.validate(cfg); err != nil {
					t.Errorf("Validation failed: %v", err)
				}
			}
		})
	}
}

func TestImageConfig_GetDockerfilePath(t *testing.T) {
	tests := []struct {
		name      string
		img       *ImageConfig
		sourceDir string
		want      string
	}{
		{
			name: "default dockerfile name",
			img: &ImageConfig{
				Name:       "test",
				Dockerfile: "Dockerfile.test",
			},
			sourceDir: "src",
			want:      "src/Dockerfile.test",
		},
		{
			name: "custom dockerfile name",
			img: &ImageConfig{
				Name:       "test",
				Dockerfile: "custom.dockerfile",
			},
			sourceDir: "templates",
			want:      "templates/custom.dockerfile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.img.GetDockerfilePath(tt.sourceDir)
			if got != tt.want {
				t.Errorf("GetDockerfilePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestImageConfig_GetBuildPath(t *testing.T) {
	// One Dockerfile per image (architecture is a build-time flag, not a file).
	img := &ImageConfig{Name: "test"}
	if got := img.GetBuildPath("build"); got != "build/Dockerfile.test" {
		t.Errorf("GetBuildPath() = %v, want build/Dockerfile.test", got)
	}
}

// TestLoadConfig_OverridesMerge proves the override map is YAML-overlaid onto the
// file: present keys win, absent keys keep their file values, and nil is a no-op.
func TestLoadConfig_OverridesMerge(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	const fileYAML = `
config:
  builder: docker
  workers: 4
  push: false
  task_timeout: 300s
images:
  app:
    version: 1.0.0
    base_image: alpine:3.19
`
	if err := os.WriteFile(configPath, []byte(fileYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Run("nil overrides keeps file values", func(t *testing.T) {
		cfg, err := LoadConfig(configPath, nil)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.Config.Push {
			t.Error("push should be false (from file)")
		}
		if cfg.Config.Mode != "" {
			t.Errorf("mode should be empty (from file), got %q", cfg.Config.Mode)
		}
	})

	t.Run("present keys win, absent keys untouched", func(t *testing.T) {
		overrides := map[string]interface{}{
			"config": map[string]interface{}{
				"push": true,
				"mode": "mock",
			},
		}
		cfg, err := LoadConfig(configPath, overrides)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		// Overridden keys take the new value.
		if !cfg.Config.Push {
			t.Error("push override (true) was not applied")
		}
		if cfg.Config.Mode != "mock" {
			t.Errorf("mode override not applied, got %q", cfg.Config.Mode)
		}
		// Keys absent from the override keep their file values.
		if cfg.Config.Builder != "docker" {
			t.Errorf("builder should stay 'docker', got %q", cfg.Config.Builder)
		}
		if cfg.Config.Workers != 4 {
			t.Errorf("workers should stay 4, got %d", cfg.Config.Workers)
		}
		if cfg.Config.TaskTimeout != "300s" {
			t.Errorf("task_timeout should stay '300s', got %q", cfg.Config.TaskTimeout)
		}
	})
}

func TestSaveConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Config: ConfigSection{
			Builder:       "docker",
			Workers:       3,
			Architectures: []string{"linux/amd64"},
			Dockerfiles: DockerfilesConfig{
				Source:      "src",
				Destination: "build",
			},
		},
		Images: map[string]*ImageConfig{
			"test": {
				Version:   "1.0.0",
				BaseImage: "ubuntu:22.04",
			},
		},
	}

	tests := []struct {
		name       string
		configPath string
		cfg        *Config
		wantErr    bool
		setup      func()
		cleanup    func()
	}{
		{
			name:       "successful save",
			configPath: filepath.Join(tmpDir, "test-save.yaml"),
			cfg:        cfg,
			wantErr:    false,
		},
		{
			name:       "save to non-existent directory",
			configPath: filepath.Join(tmpDir, "subdir", "test-save.yaml"),
			cfg:        cfg,
			wantErr:    false, // Should create directory
		},
		{
			name:       "save to invalid path",
			configPath: "/invalid\x00path/config.yaml", // Null character in path
			cfg:        cfg,
			wantErr:    true,
		},
		{
			name:       "directory creation failure",
			configPath: filepath.Join("/", "root", "no-permission", "config.yaml"),
			cfg:        cfg,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			defer func() {
				if tt.cleanup != nil {
					tt.cleanup()
				}
			}()

			tt.cfg.Path = tt.configPath
			err := tt.cfg.SaveConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("SaveConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify file was created
				if _, err := os.Stat(tt.configPath); os.IsNotExist(err) {
					t.Error("Config file was not created")
				}

				// Try to load it back
				loadedCfg, err := LoadConfig(tt.configPath, nil)
				if err != nil {
					t.Fatalf("Failed to load saved config: %v", err)
				}

				if loadedCfg.Config.Builder != tt.cfg.Config.Builder {
					t.Errorf("Builder mismatch after save/load: got %s, want %s",
						loadedCfg.Config.Builder, tt.cfg.Config.Builder)
				}
			}
		})
	}
}

func TestLoadConfig_Errors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		setup       func() string
		wantErr     bool
		errContains string
	}{
		{
			name: "non-existent file",
			setup: func() string {
				return filepath.Join(tmpDir, "non-existent.yaml")
			},
			wantErr:     true,
			errContains: "failed to read config file",
		},
		{
			name: "file with invalid permissions",
			setup: func() string {
				path := filepath.Join(tmpDir, "no-read.yaml")
				if err := os.WriteFile(path, []byte("test"), 0000); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr:     true,
			errContains: "failed to read config file",
		},
		{
			name: "empty file",
			setup: func() string {
				path := filepath.Join(tmpDir, "empty.yaml")
				if err := os.WriteFile(path, []byte(""), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr: false, // Empty YAML is valid
		},
		{
			name: "malformed YAML",
			setup: func() string {
				path := filepath.Join(tmpDir, "malformed.yaml")
				content := `
config:
  builder: docker
  invalid: [
`
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr:     true,
			errContains: "failed to parse config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := tt.setup()

			cfg, err := LoadConfig(configPath, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing '%s', got '%v'", tt.errContains, err)
				}
			}

			if !tt.wantErr && cfg == nil {
				t.Error("Expected config to be loaded")
			}
		})
	}
}

func TestProcessImageTree_Errors(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		parent  string
		images  map[string]*ImageConfig
		wantErr bool
	}{
		{
			name: "valid tree",
			config: &Config{
				Config: ConfigSection{
					Architectures: []string{"linux/amd64"},
				},
			},
			parent: "",
			images: map[string]*ImageConfig{
				"test": {
					Version: "1.0.0",
				},
			},
			wantErr: false,
		},
		{
			name: "nested images with parent inheritance",
			config: &Config{
				Config: ConfigSection{
					Architectures: []string{"linux/amd64", "linux/arm64"},
				},
				Images: map[string]*ImageConfig{
					"parent": {
						Name:          "parent",
						Architectures: []string{"linux/amd64"},
					},
				},
			},
			parent: "parent",
			images: map[string]*ImageConfig{
				"child": {
					Version: "2.0.0",
					// Should inherit parent's architectures
				},
			},
			wantErr: false,
		},
		{
			name: "deeply nested images",
			config: &Config{
				Config: ConfigSection{
					Architectures: []string{"linux/amd64"},
				},
			},
			parent: "",
			images: map[string]*ImageConfig{
				"level1": {
					Version: "1.0.0",
					Images: map[string]*ImageConfig{
						"level2": {
							Version: "2.0.0",
							Images: map[string]*ImageConfig{
								"level3": {
									Version: "3.0.0",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := processImageTree(tt.config, tt.parent, tt.images)
			if (err != nil) != tt.wantErr {
				t.Errorf("processImageTree() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify processing was done correctly
				for name, img := range tt.images {
					if img.Name != name {
						t.Errorf("Expected image name '%s', got '%s'", name, img.Name)
					}
					if img.Parent != tt.parent {
						t.Errorf("Expected parent '%s', got '%s'", tt.parent, img.Parent)
					}
					if img.Dockerfile == "" {
						t.Error("Expected dockerfile to be set")
					}
					if len(img.Architectures) == 0 {
						t.Error("Expected architectures to be set")
					}
				}
			}
		})
	}
}

func TestFindImageByName(t *testing.T) {
	images := map[string]*ImageConfig{
		"root": {
			Name:    "root",
			Version: "1.0.0",
			Images: map[string]*ImageConfig{
				"child1": {
					Name:    "child1",
					Version: "2.0.0",
					Images: map[string]*ImageConfig{
						"grandchild": {
							Name:    "grandchild",
							Version: "3.0.0",
						},
					},
				},
				"child2": {
					Name:    "child2",
					Version: "2.1.0",
				},
			},
		},
		"another": {
			Name:    "another",
			Version: "1.0.0",
		},
	}

	tests := []struct {
		name      string
		imageName string
		wantFound bool
		wantVer   string
	}{
		{
			name:      "find root level",
			imageName: "root",
			wantFound: true,
			wantVer:   "1.0.0",
		},
		{
			name:      "find nested child",
			imageName: "child1",
			wantFound: true,
			wantVer:   "2.0.0",
		},
		{
			name:      "find deeply nested",
			imageName: "grandchild",
			wantFound: true,
			wantVer:   "3.0.0",
		},
		{
			name:      "not found",
			imageName: "nonexistent",
			wantFound: false,
		},
		{
			name:      "empty name",
			imageName: "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := findImageByName(images, tt.imageName)
			if tt.wantFound {
				if found == nil {
					t.Error("Expected to find image but got nil")
				} else if found.Version != tt.wantVer {
					t.Errorf("Expected version '%s', got '%s'", tt.wantVer, found.Version)
				}
			} else {
				if found != nil {
					t.Errorf("Expected nil but found image: %+v", found)
				}
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name      string
		config    string
		checkFunc func(*Config) error
	}{
		{
			name: "minimal config with defaults",
			config: `
config:
  builder: docker
images:
  test:
    version: 1.0.0
    base_image: ubuntu:22.04
`,
			checkFunc: func(cfg *Config) error {
				if cfg.Config.Workers != 2 {
					return fmt.Errorf("Expected default workers 2, got %d", cfg.Config.Workers)
				}
				if len(cfg.Config.Architectures) != 1 || cfg.Config.Architectures[0] != "linux/amd64" {
					return fmt.Errorf("Expected default arch [linux/amd64], got %v", cfg.Config.Architectures)
				}
				if cfg.Config.Dockerfiles.Source != "src" {
					return fmt.Errorf("Expected default source 'src', got '%s'", cfg.Config.Dockerfiles.Source)
				}
				if cfg.Config.Dockerfiles.Destination != "build" {
					return fmt.Errorf("Expected default destination 'build', got '%s'", cfg.Config.Dockerfiles.Destination)
				}
				return nil
			},
		},
		{
			name: "override all defaults",
			config: `
config:
  builder: podman
  workers: 5
  architectures:
    - linux/arm64
    - darwin/amd64
  dockerfiles:
    source: templates
    destination: output
images:
  test:
    version: 1.0.0
    base_image: ubuntu:22.04
`,
			checkFunc: func(cfg *Config) error {
				if cfg.Config.Builder != "podman" {
					return fmt.Errorf("Expected builder 'podman', got '%s'", cfg.Config.Builder)
				}
				if cfg.Config.Workers != 5 {
					return fmt.Errorf("Expected workers 5, got %d", cfg.Config.Workers)
				}
				if len(cfg.Config.Architectures) != 2 {
					return fmt.Errorf("Expected 2 architectures, got %d", len(cfg.Config.Architectures))
				}
				if cfg.Config.Dockerfiles.Source != "templates" {
					return fmt.Errorf("Expected source 'templates', got '%s'", cfg.Config.Dockerfiles.Source)
				}
				if cfg.Config.Dockerfiles.Destination != "output" {
					return fmt.Errorf("Expected destination 'output', got '%s'", cfg.Config.Dockerfiles.Destination)
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, tt.name+".yaml")
			if err := os.WriteFile(configPath, []byte(tt.config), 0644); err != nil {
				t.Fatalf("Failed to write config: %v", err)
			}

			cfg, err := LoadConfig(configPath, nil)
			if err != nil {
				t.Fatalf("LoadConfig() unexpected error: %v", err)
			}

			if err := tt.checkFunc(cfg); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestValidateBuilder verifies the builder validation honors the builder mode:
// mock modes never probe a real daemon, and an unsupported builder errors.
func TestValidateBuilder(t *testing.T) {
	tests := []struct {
		name      string
		builder   string
		mode      string
		wantError bool
	}{
		{"mock builder never probes", builder.BuilderDocker, string(builder.ModeMock), false},
		{"mock-error builder never probes", builder.BuilderDocker, string(builder.ModeMockError), false},
		{"unsupported builder errors", "nonexistent", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Config: ConfigSection{Builder: tt.builder, Mode: tt.mode}}
			err := cfg.ValidateBuilder()
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateBuilder() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
