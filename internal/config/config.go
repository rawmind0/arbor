package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/rawmind0/arbor/internal/builder"
)

// DefaultPlatform is set at build time via ldflags
var DefaultPlatform = "linux/amd64"

// DefaultOwner is the owner/namespace used when config.registry.owner is unset.
// Owner is always part of the image tag ("<owner>/<name>:<version>") so that
// local and registry references stay consistent and carry namespace context;
// "library" mirrors Docker Hub's implicit namespace for official images.
const DefaultOwner = "library"

type Config struct {
	Config ConfigSection           `yaml:"config"`
	Images map[string]*ImageConfig `yaml:"images"`

	// Multiple image sources (key: source ID like "main", "databases", etc.)
	Sources map[string]*ImageSource `yaml:"-"`

	// Runtime state
	IsGitRepo bool   `yaml:"-"` // Set during config load
	Path      string `yaml:"-"` // Path this config was loaded from (for SaveConfig)
}

// ImageSource represents a collection of images from a single source file
type ImageSource struct {
	ID     string                  // Source identifier (e.g., "main", "databases")
	Images map[string]*ImageConfig // Images from this source
}

type ConfigSection struct {
	Builder       string            `yaml:"builder"`
	Workers       int               `yaml:"workers,omitempty"`
	Architectures []string          `yaml:"architectures,omitempty"`
	Dockerfiles   DockerfilesConfig `yaml:"dockerfiles"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	TaskTimeout   string            `yaml:"task_timeout,omitempty"` // Task timeout duration (e.g., "300s")
	Registry      RegistryConfig    `yaml:"registry,omitempty"`     // Push target (url + owner)

	// Generate command defaults
	SkipVersionCheck bool   `yaml:"skip_version_check,omitempty"`
	ChangedOnly      bool   `yaml:"changed_only,omitempty"`
	BaseRef          string `yaml:"base_ref,omitempty"`
	Push             bool   `yaml:"push,omitempty"` // Push images + manifests to the registry

	// Version validation settings
	VersionIncrement string `yaml:"version_increment,omitempty"` // "patch" (default) or "minor"

	// Runtime flags: omitempty keeps them out of a saved config, but the yaml
	// tags let the override-merge set them (the source of truth is the merge).
	DryRun bool   `yaml:"dry_run,omitempty"` // Simulate builds with mock tasks
	Mode   string `yaml:"mode,omitempty"`    // Builder mode: normal, mock, mock-error
}

type DockerfilesConfig struct {
	Source      string `yaml:"source,omitempty"`
	Destination string `yaml:"destination,omitempty"`
}

type ImageConfig struct {
	Version       string                  `yaml:"version"`
	BaseImage     string                  `yaml:"base_image,omitempty"`
	Dockerfile    string                  `yaml:"dockerfile,omitempty"`
	Architectures []string                `yaml:"architectures,omitempty"`
	Args          map[string]string       `yaml:"args,omitempty"`
	Envs          map[string]string       `yaml:"envs,omitempty"`
	Labels        map[string]string       `yaml:"labels,omitempty"`
	Images        map[string]*ImageConfig `yaml:"images,omitempty"`

	// Internal fields populated during processing
	Name     string `yaml:"-"`
	Parent   string `yaml:"-"`
	FullName string `yaml:"-"`
	Changed  bool   `yaml:"-"`
}

// isGitRepository checks if git is available and we're in a git repository
func isGitRepository(dir string) bool {
	// First check if git command is available
	checkGit := exec.Command("git", "--version")
	if err := checkGit.Run(); err != nil {
		// Git is not installed or not in PATH
		return false
	}

	// Now check if we're in a git repository
	cmd := exec.Command("git", "status")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		// Not in a git repository
		return false
	}

	return true
}

// LoadConfig reads the config file and returns a fully-assembled config: it
// parses the YAML, merges in the optional command-line overrides (a partial
// config map — flags win over the file), applies defaults, enforces git
// constraints, processes the image tree, and loads any additional image sources
// (overrides["sources"]). Pass nil for no overrides. The config is the single
// source of truth — callers read cfg.Config.* afterwards.
//
// Because the config is YAML-marshalable, override merging is just a YAML
// overlay: marshal the overrides map and unmarshal it onto the parsed config, so
// only the keys present are replaced. No bespoke override type or per-field code.
func LoadConfig(configPath string, overrides map[string]interface{}) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Overlay the override map (e.g. {"config": {"push": true}}) onto the parsed
	// config — present keys win, the rest are untouched.
	if len(overrides) > 0 {
		overlay, err := yaml.Marshal(overrides)
		if err != nil {
			return nil, fmt.Errorf("failed to encode overrides: %w", err)
		}
		if err := yaml.Unmarshal(overlay, &config); err != nil {
			return nil, fmt.Errorf("failed to apply overrides: %w", err)
		}
	}

	// Record where this config came from and whether we're in a git repository.
	config.Path = configPath
	config.IsGitRepo = isGitRepository(filepath.Dir(configPath))

	// Structural defaults.
	if config.Config.Builder == "" {
		config.Config.Builder = builder.BuilderDocker
	}
	if config.Config.Workers == 0 {
		config.Config.Workers = 2
	}
	if len(config.Config.Architectures) == 0 {
		config.Config.Architectures = []string{DefaultPlatform}
	}
	if config.Config.Dockerfiles.Source == "" {
		config.Config.Dockerfiles.Source = "src"
	}
	if config.Config.Dockerfiles.Destination == "" {
		config.Config.Dockerfiles.Destination = "build"
	}
	// Owner is always present so every tag is namespace-qualified (local and
	// registry alike); default to the shared "library" namespace when unset.
	if config.Config.Registry.Owner == "" {
		config.Config.Registry.Owner = DefaultOwner
	}
	if config.Config.VersionIncrement == "" {
		config.Config.VersionIncrement = "patch"
	}
	if config.Config.VersionIncrement != "patch" && config.Config.VersionIncrement != "minor" {
		return nil, fmt.Errorf("invalid version_increment: %s (must be 'patch' or 'minor')", config.Config.VersionIncrement)
	}
	if config.Config.TaskTimeout == "" {
		config.Config.TaskTimeout = "300s"
	}

	// Enforce git-dependent constraints (overrides were already overlaid above).
	config.ApplyGitConstraints()

	// Process the main images into the primary source.
	config.Sources = make(map[string]*ImageSource)
	if len(config.Images) > 0 {
		if err := processImageTree(&config, "", config.Images); err != nil {
			return nil, fmt.Errorf("failed to process image tree: %w", err)
		}
		config.Sources["main"] = &ImageSource{ID: "main", Images: config.Images}
	}

	return &config, nil
}

func processImageTree(config *Config, parentName string, images map[string]*ImageConfig) error {
	for name, img := range images {
		img.Name = name
		img.Parent = parentName

		if parentName != "" {
			img.FullName = fmt.Sprintf("%s/%s", parentName, name)
		} else {
			img.FullName = name
		}

		// Set default dockerfile path if not specified
		if img.Dockerfile == "" {
			img.Dockerfile = fmt.Sprintf("Dockerfile.%s", name)
		}

		// Inherit architectures if not specified
		if len(img.Architectures) == 0 {
			if parentName != "" {
				// Inherit from parent
				if parent := findImageByName(config.Images, parentName); parent != nil {
					img.Architectures = parent.Architectures
				}
			} else {
				// Use global config
				img.Architectures = config.Config.Architectures
			}
		}

		// Process child images recursively
		if img.Images != nil {
			if err := processImageTree(config, name, img.Images); err != nil {
				return err
			}
		}
	}
	return nil
}

func findImageByName(images map[string]*ImageConfig, name string) *ImageConfig {
	for imgName, img := range images {
		if imgName == name {
			return img
		}
		if img.Images != nil {
			if found := findImageByName(img.Images, name); found != nil {
				return found
			}
		}
	}
	return nil
}

// FindImageByName finds an image by name in the configuration tree
func (c *Config) FindImageByName(name string) *ImageConfig {
	if name == "" {
		return nil
	}
	return findImageByName(c.Images, name)
}

// IsInGitRepo returns whether the configuration is in a git repository
func (c *Config) IsInGitRepo() bool {
	return c.IsGitRepo
}

// ApplyGitConstraints disables features that require a git repository when none
// is available: version validation and changed-only. It is idempotent and warns
// only when it actually changes a setting, so it is safe to re-run after folding
// command-line flags into the config.
func (c *Config) ApplyGitConstraints() {
	if c.IsGitRepo {
		return
	}
	if exec.Command("git", "--version").Run() != nil {
		log.Warn().Msg("git is not installed or not in PATH")
	} else {
		log.Warn().Msg("not in a git repository")
	}
	if !c.Config.SkipVersionCheck {
		log.Warn().Msg("enabling skip-version-check (version validation requires git)")
		c.Config.SkipVersionCheck = true
	}
	if c.Config.ChangedOnly {
		log.Warn().Msg("disabling changed-only (requires git to detect changes)")
		c.Config.ChangedOnly = false
	}
}

// SaveConfig writes the config back to the path it was loaded from (Config.Path).
func (c *Config) SaveConfig() error {
	if c.Path == "" {
		return fmt.Errorf("config has no path to save to")
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(c.Path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(c.Path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadAdditionalImages loads additional image definitions from glob patterns
func (c *Config) LoadAdditionalImages(configPath string, patterns []string) error {
	// Get absolute path of config file for comparison
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		absConfigPath = configPath // Fall back to relative path if abs fails
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("invalid glob pattern %s: %w", pattern, err)
		}

		for _, file := range matches {
			// Get absolute path of this file for comparison
			absFile, err := filepath.Abs(file)
			if err != nil {
				absFile = file // Fall back to relative path if abs fails
			}

			// Skip if this is the main config file
			if absFile == absConfigPath {
				continue
			}

			// Extract source ID from filename (e.g., "databases.yaml" -> "databases")
			sourceID := filepath.Base(file)
			sourceID = sourceID[:len(sourceID)-len(filepath.Ext(sourceID))]

			// Load the YAML file
			data, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("failed to read image file %s: %w", file, err)
			}

			// Parse only the images section
			var imageFile struct {
				Images map[string]*ImageConfig `yaml:"images"`
			}
			if err := yaml.Unmarshal(data, &imageFile); err != nil {
				return fmt.Errorf("failed to parse image file %s: %w", file, err)
			}

			if len(imageFile.Images) == 0 {
				continue // Skip files with no images
			}

			// Process the image tree for this source
			if err := processImageTree(c, "", imageFile.Images); err != nil {
				return fmt.Errorf("failed to process images from %s: %w", file, err)
			}

			// Add to sources
			c.Sources[sourceID] = &ImageSource{
				ID:     sourceID,
				Images: imageFile.Images,
			}
		}
	}

	return nil
}

// GetTaskTimeout returns the task timeout as a Duration
func (c *Config) GetTaskTimeout() time.Duration {
	if c.Config.TaskTimeout == "" {
		return 300 * time.Second // Default 5 minutes
	}

	duration, err := time.ParseDuration(c.Config.TaskTimeout)
	if err != nil {
		log.Warn().Str("task_timeout", c.Config.TaskTimeout).Msg("invalid task_timeout, using default 300s")
		return 300 * time.Second
	}

	return duration
}

func (img *ImageConfig) GetDockerfilePath(sourceDir string) string {
	return filepath.Join(sourceDir, img.Dockerfile)
}

// GetBuildPath returns the generated Dockerfile path for the image. One
// Dockerfile per image (architecture is a build-time flag, not a file variant).
func (img *ImageConfig) GetBuildPath(destDir string) string {
	return filepath.Join(destDir, fmt.Sprintf("Dockerfile.%s", img.Name))
}
