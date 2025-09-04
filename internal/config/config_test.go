package config

import (
	"os"
	"path/filepath"
	"testing"
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

			cfg, err := LoadConfig(configPath)
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
	tests := []struct {
		name    string
		img     *ImageConfig
		destDir string
		arch    string
		want    string
	}{
		{
			name: "with architecture",
			img: &ImageConfig{
				Name: "test",
			},
			destDir: "build",
			arch:    "linux/amd64",
			want:    "build/Dockerfile.test-amd64",
		},
		{
			name: "without architecture",
			img: &ImageConfig{
				Name: "test",
			},
			destDir: "build",
			arch:    "",
			want:    "build/Dockerfile.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.img.GetBuildPath(tt.destDir, tt.arch)
			if got != tt.want {
				t.Errorf("GetBuildPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeArch(t *testing.T) {
	tests := []struct {
		name string
		arch string
		want string
	}{
		{
			name: "linux/amd64",
			arch: "linux/amd64",
			want: "amd64",
		},
		{
			name: "linux/arm64",
			arch: "linux/arm64",
			want: "arm64",
		},
		{
			name: "simple arch",
			arch: "amd64",
			want: "amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeArch(tt.arch)
			if got != tt.want {
				t.Errorf("sanitizeArch() = %v, want %v", got, tt.want)
			}
		})
	}
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

	configPath := filepath.Join(tmpDir, "test-save.yaml")
	if err := cfg.SaveConfig(configPath); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created")
	}

	// Try to load it back
	loadedCfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	if loadedCfg.Config.Builder != cfg.Config.Builder {
		t.Errorf("Builder mismatch after save/load: got %s, want %s", loadedCfg.Config.Builder, cfg.Config.Builder)
	}
}
