package version

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/git"
)

// ValidationResult contains the results of version validation
type ValidationResult struct {
	Valid           bool
	UpdatesRequired map[string]VersionUpdate // "<source>/<image>" -> update info
	Warnings        []string
	Errors          []string
}

// VersionUpdate contains information about a version update
type VersionUpdate struct {
	Source     string // ID of the source (file) the image is defined in
	ImageName  string
	OldVersion string
	NewVersion string
	Reason     string
	UpdateType UpdateType
}

// UpdateType represents the type of version update
type UpdateType int

const (
	UpdateTypePatch UpdateType = iota
	UpdateTypeMinor
	UpdateTypeMajor
	UpdateTypeForced // When parent changed
)

// Validator checks image versions against the changes, source by source: the
// config file's images and every --sources file.
type Validator struct {
	config    *config.Config
	gitClient *git.Client
}

// NewValidator creates a new version validator
func NewValidator(cfg *config.Config) *Validator {
	return &Validator{
		config:    cfg,
		gitClient: git.NewClient("."),
	}
}

// updateKey identifies an image across sources (names may repeat between them).
func updateKey(source, image string) string {
	return source + "/" + image
}

// Validate checks every source's images and returns the required updates:
//
//   - every version must be valid semver
//   - an image with a change in its Dockerfile folder since the base (see git
//     ChangedSince and config.ChangedImages) needs a bump while its version still equals its version at the base,
//     and the bump cascades to its children (with the same condition)
//
// Comparing against the base version makes the check idempotent: once an image
// is bumped, in any commit of the branch, it is no longer flagged. Images that
// are new since the base need no bump.
func (v *Validator) Validate() (*ValidationResult, error) {
	result := &ValidationResult{
		UpdatesRequired: make(map[string]VersionUpdate),
		Warnings:        []string{},
		Errors:          []string{},
	}

	base, changed, err := v.changes(result)
	if err != nil {
		return nil, fmt.Errorf("failed to check dockerfile changes: %w", err)
	}

	for _, id := range v.config.SourceIDs() {
		source := v.config.Sources[id]
		validateSemver(source.Images, result)
		if changed == nil {
			continue // no git: change detection skipped (warned)
		}
		baseVersions, err := v.baseVersions(base, source.Path)
		if err != nil {
			return nil, err
		}
		s := &sourceCheck{v: v, source: id, changed: changed, baseVersions: baseVersions, result: result}
		s.walk(source.Images)
	}

	result.Valid = len(result.UpdatesRequired) == 0 && len(result.Errors) == 0
	return result, nil
}

// changes returns the base commit and the images changed since it (with the
// file that changed each), or nil (with a warning) when change detection is not
// possible.
func (v *Validator) changes(result *ValidationResult) (string, map[*config.ImageConfig]string, error) {
	if !v.gitClient.IsGitRepo() {
		result.Warnings = append(result.Warnings, "Not a git repository, skipping dockerfile change detection")
		return "", nil, nil
	}
	if !v.gitClient.HasCommits() {
		result.Warnings = append(result.Warnings, "No commits yet, skipping dockerfile change detection")
		return "", nil, nil
	}
	base, files, err := v.gitClient.ChangedSince(v.config.Config.BaseRef)
	if err != nil {
		return "", nil, err
	}
	return base, v.config.ChangedImages(files), nil
}

// sourceCheck proposes the bumps for one source's image tree.
type sourceCheck struct {
	v            *Validator
	source       string
	changed      map[*config.ImageConfig]string // image -> the file that changed it
	baseVersions map[string]string
	result       *ValidationResult
}

// walk visits the tree parents first, proposing a bump for every image with a
// change in its Dockerfile folder (Config.ChangedImages).
func (s *sourceCheck) walk(images map[string]*config.ImageConfig) {
	for _, img := range sortedImages(images) {
		if file, ok := s.changed[img]; ok {
			reason := fmt.Sprintf("%s (in its Dockerfile folder) has been modified", file)
			if file == filepath.ToSlash(filepath.Clean(img.GetDockerfilePath(s.v.config.Config.Dockerfiles))) {
				reason = fmt.Sprintf("Dockerfile %s has been modified", file)
			}
			s.propose(img, reason)
		}
		s.walk(img.Images)
	}
}

// propose records a bump for img unless it is already proposed, was already
// bumped since the base, or is new since the base; then cascades to children.
func (s *sourceCheck) propose(img *config.ImageConfig, reason string) {
	key := updateKey(s.source, img.Name)
	if _, done := s.result.UpdatesRequired[key]; done {
		return
	}
	if old, existed := s.baseVersions[img.Name]; !existed || old != img.Version {
		return
	}

	updateType := s.v.configuredUpdateType()
	next, err := Bump(img.Version, updateType == UpdateTypeMinor)
	if err != nil {
		s.result.Errors = append(s.result.Errors,
			fmt.Sprintf("Cannot update '%s': invalid current version '%s'", img.Name, img.Version))
		return
	}

	s.result.UpdatesRequired[key] = VersionUpdate{
		Source:     s.source,
		ImageName:  img.Name,
		OldVersion: img.Version,
		NewVersion: next,
		Reason:     reason,
		UpdateType: updateType,
	}

	for _, child := range sortedImages(img.Images) {
		s.propose(child, fmt.Sprintf("Parent image '%s' version was updated", img.Name))
	}
}

// validateSemver records an error for every image without a valid version.
func validateSemver(images map[string]*config.ImageConfig, result *ValidationResult) {
	for _, img := range sortedImages(images) {
		if img.Version == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("Image '%s' is missing version", img.Name))
		} else if _, err := Parse(img.Version); err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Image '%s' has invalid version '%s': %v", img.Name, img.Version, err))
		}
		validateSemver(img.Images, result)
	}
}

// configuredUpdateType returns the update type based on configuration
func (v *Validator) configuredUpdateType() UpdateType {
	if v.config.Config.VersionIncrement == "minor" {
		return UpdateTypeMinor
	}
	return UpdateTypePatch
}

// baseVersions reads a source file (the config file or a --sources file) as it
// was at the base commit and returns each image's version there (name →
// version). A file that did not exist at the base yields an empty map: every
// image in it is new.
func (v *Validator) baseVersions(base, path string) (map[string]string, error) {
	versions := map[string]string{}
	if path == "" {
		return versions, nil
	}
	data, err := v.gitClient.ShowFile(base, path)
	if errors.Is(err, os.ErrNotExist) {
		return versions, nil
	}
	if err != nil {
		return nil, err
	}

	var old struct {
		Images map[string]*config.ImageConfig `yaml:"images"`
	}
	if err := yaml.Unmarshal(data, &old); err != nil {
		return nil, fmt.Errorf("failed to parse %s at %s: %w", path, base, err)
	}
	var walk func(images map[string]*config.ImageConfig)
	walk = func(images map[string]*config.ImageConfig) {
		for name, img := range images {
			if img == nil {
				continue
			}
			versions[name] = img.Version
			walk(img.Images)
		}
	}
	walk(old.Images)
	return versions, nil
}

// Apply applies a validation result's bumps: it sets each image's version in
// the config and writes it into the file the image is defined in (the config
// file or a --sources file), changing only the `version:` values. A no-op when
// there are no updates. Used by the validate command's --fix path.
func (v *Validator) Apply(result *ValidationResult) error {
	byFile := map[string]map[string]string{} // file -> image -> new version
	for _, u := range result.UpdatesRequired {
		source := v.config.Sources[u.Source]
		if source == nil || source.Path == "" {
			return fmt.Errorf("cannot save version of image %q: unknown source %q", u.ImageName, u.Source)
		}
		img := findImage(source.Images, u.ImageName)
		if img == nil {
			return fmt.Errorf("image %q not found in source %q", u.ImageName, u.Source)
		}
		img.Version = u.NewVersion
		if byFile[source.Path] == nil {
			byFile[source.Path] = map[string]string{}
		}
		byFile[source.Path][u.ImageName] = u.NewVersion
	}

	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		if err := config.UpdateVersions(f, byFile[f]); err != nil {
			return fmt.Errorf("failed to save version updates: %w", err)
		}
		log.Info().Str("file", f).Int("updates", len(byFile[f])).Msg("applied version updates")
	}
	return nil
}

// Check verifies versions are consistent without changing anything: it is the
// gate plan and build run before planning. Warnings are logged. It returns an
// error listing every problem when a version is invalid or a bump is required;
// bumps are applied only by `arbor validate --fix`, never silently.
func (v *Validator) Check() error {
	result, err := v.Validate()
	if err != nil {
		return fmt.Errorf("failed to validate versions: %w", err)
	}
	for _, w := range result.Warnings {
		log.Warn().Msg(w)
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("version validation failed:\n  %s", strings.Join(result.Errors, "\n  "))
	}
	if len(result.UpdatesRequired) == 0 {
		return nil
	}
	return fmt.Errorf("version updates required:\n  %s\nrun `arbor validate --fix` to apply them (or pass --skip-version-check)",
		strings.Join(result.Lines(), "\n  "))
}

// Lines returns the required updates as sorted "image: old → new (reason)" lines.
func (r *ValidationResult) Lines() []string {
	keys := make([]string, 0, len(r.UpdatesRequired))
	for k := range r.UpdatesRequired {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		u := r.UpdatesRequired[k]
		lines = append(lines, fmt.Sprintf("%s: %s → %s (%s)", u.ImageName, u.OldVersion, u.NewVersion, u.Reason))
	}
	return lines
}

// findImage finds an image by name within one source's tree.
func findImage(images map[string]*config.ImageConfig, name string) *config.ImageConfig {
	for _, img := range images {
		if img.Name == name {
			return img
		}
		if found := findImage(img.Images, name); found != nil {
			return found
		}
	}
	return nil
}

// sortedImages returns the images of a map in name order (deterministic output).
func sortedImages(images map[string]*config.ImageConfig) []*config.ImageConfig {
	names := make([]string, 0, len(images))
	for name := range images {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*config.ImageConfig, 0, len(names))
	for _, name := range names {
		out = append(out, images[name])
	}
	return out
}
