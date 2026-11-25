package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/git"
)

// DefaultPlatform is set at build time via ldflags
var DefaultPlatform = "linux/amd64"

// DefaultOwner is the owner/namespace used when config.registry.owner is unset.
// Owner is always part of the image tag ("<owner>/<name>:<version>") so that
// local and registry references stay consistent and carry namespace context;
// "library" mirrors Docker Hub's implicit namespace for official images.
const DefaultOwner = "library"

// BaseArg is the build argument every Dockerfile declares (`ARG BASE` before its
// first FROM) and uses as its final stage's base image. arbor injects the parent
// image through it (`--build-arg BASE=<parent tag>`), which keeps the Dockerfiles
// plain and buildable with a regular `docker build`.
const BaseArg = "BASE"

type Config struct {
	Config ConfigSection           `yaml:"config"`
	Images map[string]*ImageConfig `yaml:"images"`

	// Sources holds every image the program works on, one entry per file:
	// MainSource for the images section above, plus one per --sources file.
	// Validate fills it; nothing else reads Images directly.
	Sources map[string]*ImageSource `yaml:"-"`

	// Runtime state
	IsGitRepo bool   `yaml:"-"` // Set during config load
	Path      string `yaml:"-"` // Path this config was loaded from
}

// MainSource is the ID of the source holding the config file's own images.
const MainSource = "main"

// ImageSource represents a collection of images from a single file
type ImageSource struct {
	ID     string                  // Source identifier (e.g., "main", "databases")
	Path   string                  // File the images were loaded from (where their versions live)
	Images map[string]*ImageConfig // Images from this source
}

type ConfigSection struct {
	Builder       string            `yaml:"builder"`
	Workers       int               `yaml:"workers,omitempty"`
	Architectures []string          `yaml:"architectures,omitempty"`
	Dockerfiles   string            `yaml:"dockerfiles,omitempty"` // Directory holding the Dockerfiles (default: src)
	Labels        map[string]string `yaml:"labels,omitempty"`
	TaskTimeout   string            `yaml:"task_timeout,omitempty"` // Task timeout duration (e.g., "300s")
	Registry      RegistryConfig    `yaml:"registry,omitempty"`     // Push target (url + owner)

	// Command defaults
	SkipVersionCheck bool   `yaml:"skip_version_check,omitempty"` // plan/build: skip the version check
	ChangedOnly      bool   `yaml:"changed_only,omitempty"`
	BaseRef          string `yaml:"base_ref,omitempty"` // Git ref changes are measured from (default: master, then main)
	Push             bool   `yaml:"push,omitempty"`     // Push images + manifests to the registry

	// Version validation settings
	VersionIncrement string `yaml:"version_increment,omitempty"` // "patch" (default) or "minor"

	// Runtime flags: omitempty keeps them out of a saved config, but the yaml
	// tags let the override-merge set them (the source of truth is the merge).
	DryRun bool   `yaml:"dry_run,omitempty"` // Simulate builds with mock tasks
	Mode   string `yaml:"mode,omitempty"`    // Builder mode: normal, mock, mock-error
}

type ImageConfig struct {
	Version       string                  `yaml:"version"`
	BaseImage     string                  `yaml:"base_image,omitempty"`
	Dockerfile    string                  `yaml:"dockerfile,omitempty"`
	Architectures []string                `yaml:"architectures,omitempty"`
	Args          map[string]string       `yaml:"args,omitempty"`
	Labels        map[string]string       `yaml:"labels,omitempty"`
	Images        map[string]*ImageConfig `yaml:"images,omitempty"`

	// Internal fields populated during processing
	Name    string `yaml:"-"`
	Parent  string `yaml:"-"`
	Changed bool   `yaml:"-"`
}

// isGitRepository reports whether dir is inside a git repository.
func isGitRepository(dir string) bool {
	return git.NewClient(dir).IsGitRepo()
}

// LoadConfig reads the config file at configPath, overlays the optional
// command-line overrides (a partial config map — flags win over the file),
// records where the config came from, and completes it with Validate. Pass nil
// for no overrides. Images from extra files are added with LoadAdditionalImages.
//
// Because the config is YAML-marshalable, override merging is just a YAML
// overlay: marshal the overrides map and unmarshal it onto the parsed config, so
// only the keys present are replaced. No bespoke override type or per-field code.
func LoadConfig(configPath string, overrides map[string]interface{}) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	if err := checkRemovedSettings(configPath, data); err != nil {
		return nil, err
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

	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

// checkRemovedSettings rejects settings that were removed with Dockerfile
// generation, saying what replaces them. Without it an old config would fail
// with a bare YAML type error (the dockerfiles mapping) or, worse, silently
// lose its environment variables (envs is otherwise ignored).
func checkRemovedSettings(file string, data []byte) error {
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) != nil {
		return nil // the real parse reports it
	}
	var errs []error
	if section, ok := doc["config"].(map[string]any); ok {
		if _, ok := section["dockerfiles"].(map[string]any); ok {
			errs = append(errs, fmt.Errorf("%s: config.dockerfiles is now the Dockerfiles directory "+
				"(e.g. `dockerfiles: src`); source/destination were removed: Dockerfiles are built in place", file))
		}
	}
	var walk func(images any)
	walk = func(images any) {
		tree, _ := images.(map[string]any)
		for _, name := range slices.Sorted(maps.Keys(tree)) {
			img, _ := tree[name].(map[string]any)
			if _, ok := img["envs"]; ok {
				errs = append(errs, fmt.Errorf("%s: image %q: envs was removed; set them with ENV in its Dockerfile", file, name))
			}
			walk(img["images"])
		}
	}
	walk(doc["images"])
	return errors.Join(errs...)
}

// Validate completes and checks a config, whether it was loaded from files or
// built in code: it applies defaults, checks values, applies the git
// constraints, registers the config's own images as the MainSource source, and
// fills in the derived fields of every source's images (name, parent, default
// Dockerfile, inherited architectures). Safe to call more than once
// (LoadAdditionalImages calls it again after adding sources).
func (c *Config) Validate() error {
	if c.Config.Builder == "" {
		c.Config.Builder = builder.BuilderDocker
	}
	if c.Config.Workers == 0 {
		c.Config.Workers = 2
	}
	if len(c.Config.Architectures) == 0 {
		c.Config.Architectures = []string{DefaultPlatform}
	}
	if c.Config.Dockerfiles == "" {
		c.Config.Dockerfiles = "src"
	}
	// Owner is always present so every tag is namespace-qualified (local and
	// registry alike); default to the shared "library" namespace when unset.
	if c.Config.Registry.Owner == "" {
		c.Config.Registry.Owner = DefaultOwner
	}
	if c.Config.VersionIncrement == "" {
		c.Config.VersionIncrement = "patch"
	}
	if c.Config.VersionIncrement != "patch" && c.Config.VersionIncrement != "minor" {
		return fmt.Errorf("invalid version_increment: %s (must be 'patch' or 'minor')", c.Config.VersionIncrement)
	}
	if c.Config.TaskTimeout == "" {
		c.Config.TaskTimeout = "300s"
	}

	c.ApplyGitConstraints()

	if c.Sources == nil {
		c.Sources = make(map[string]*ImageSource)
	}
	if len(c.Images) > 0 {
		c.Sources[MainSource] = &ImageSource{ID: MainSource, Path: c.Path, Images: c.Images}
	}

	for _, id := range c.SourceIDs() {
		processImages(c, "", nil, c.Sources[id].Images)
	}
	return nil
}

// SourceIDs returns the source IDs in a stable order: MainSource first, then
// the --sources files by name.
func (c *Config) SourceIDs() []string {
	ids := make([]string, 0, len(c.Sources))
	for id := range c.Sources {
		if id != MainSource {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if _, ok := c.Sources[MainSource]; ok {
		ids = append([]string{MainSource}, ids...)
	}
	return ids
}

// walkImages calls fn for every image in the tree, parents before children,
// siblings in name order.
func walkImages(images map[string]*ImageConfig, fn func(*ImageConfig)) {
	names := make([]string, 0, len(images))
	for name := range images {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		img := images[name]
		fn(img)
		walkImages(img.Images, fn)
	}
}

// processImages fills in the derived fields of an image tree, passing each
// image's architectures down to its children: an image uses its own
// architectures, else its parent's, else (for a root) the global ones.
func processImages(config *Config, parentName string, parentArchs []string, images map[string]*ImageConfig) {
	for name, img := range images {
		img.Name = name
		img.Parent = parentName

		// Set default dockerfile path if not specified
		if img.Dockerfile == "" {
			img.Dockerfile = fmt.Sprintf("Dockerfile.%s", name)
		}

		if len(img.Architectures) == 0 {
			if parentName != "" {
				img.Architectures = parentArchs
			} else {
				img.Architectures = config.Config.Architectures
			}
		}

		processImages(config, name, img.Architectures, img.Images)
	}
}

// IsInGitRepo returns whether the configuration is in a git repository
func (c *Config) IsInGitRepo() bool {
	return c.IsGitRepo
}

// ApplyGitConstraints disables features that require a git repository when none
// is available: version validation and changed-only. It is idempotent and warns
// only when it actually changes a setting, so it is safe to re-run.
func (c *Config) ApplyGitConstraints() {
	if c.IsGitRepo || (c.Config.SkipVersionCheck && !c.Config.ChangedOnly) {
		return
	}
	reason := "not in a git repository"
	if exec.Command("git", "--version").Run() != nil {
		reason = "git is not installed or not in PATH"
	}
	if !c.Config.SkipVersionCheck {
		log.Warn().Msgf("%s: enabling skip-version-check (version validation requires git)", reason)
		c.Config.SkipVersionCheck = true
	}
	if c.Config.ChangedOnly {
		log.Warn().Msgf("%s: disabling changed-only (requires git to detect changes)", reason)
		c.Config.ChangedOnly = false
	}
}

// LoadAdditionalImages loads additional image definitions from glob patterns,
// one source per file (named after the file), then re-runs Validate.
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
			if err := checkRemovedSettings(file, data); err != nil {
				return err
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

			if c.Sources == nil {
				c.Sources = make(map[string]*ImageSource)
			}
			c.Sources[sourceID] = &ImageSource{
				ID:     sourceID,
				Path:   file,
				Images: imageFile.Images,
			}
		}
	}

	// Complete the new sources' images.
	return c.Validate()
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

// GetDockerfilePath returns the image's Dockerfile path inside dir (the
// config's dockerfiles directory). One Dockerfile per image; architecture is a
// build-time flag, not a file variant.
func (img *ImageConfig) GetDockerfilePath(dir string) string {
	return filepath.Join(dir, img.Dockerfile)
}

// GetBuildContext returns the image's build context: the directory its
// Dockerfile is in (the dockerfiles directory, or a subfolder of it when
// `dockerfile:` names one, e.g. api/Dockerfile). COPY/ADD paths are relative to
// it, so the Dockerfile builds the same with or without arbor.
func (img *ImageConfig) GetBuildContext(dir string) string {
	return filepath.Dir(img.GetDockerfilePath(dir))
}
