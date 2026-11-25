package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/rawmind0/arbor/internal/git"
)

// ChangeDetector handles detection of changes in configuration and Dockerfiles
type ChangeDetector struct {
	gitClient *git.Client
	config    *Config
}

// NewChangeDetector creates a new change detector
func NewChangeDetector(cfg *Config, workDir string) *ChangeDetector {
	return &ChangeDetector{
		gitClient: git.NewClient(workDir),
		config:    cfg,
	}
}

// DetectChanges analyzes git changes and marks affected images
func (cd *ChangeDetector) DetectChanges(baseRef string) error {
	// If not in a git repo, mark all images as changed
	if !cd.gitClient.IsGitRepo() {
		log.Info().Msg("not in a git repository - building all images")
		cd.markAllImagesChanged()
		return nil
	}

	// If no commits exist, mark all images as changed
	if !cd.gitClient.HasCommits() {
		log.Info().Msg("no commits found - building all images")
		cd.markAllImagesChanged()
		return nil
	}

	// Changed files since the branch split from the base ref (default
	// master, then main), including uncommitted changes. The version check
	// measures from the same point, so both always agree.
	base, changedFiles, err := cd.gitClient.ChangedSince(baseRef)
	if err != nil {
		return fmt.Errorf("failed to detect changes: %w", err)
	}

	if len(changedFiles) == 0 {
		log.Info().Msg("no changes detected")
		return nil
	}

	log.Info().Int("count", len(changedFiles)).Msg("detected changed files")

	// Analyze changed files and mark affected images
	return cd.analyzeChangedFiles(base, changedFiles)
}

// markAllImagesChanged marks every image, in every source, as changed
func (cd *ChangeDetector) markAllImagesChanged() {
	for _, img := range cd.config.allImages() {
		img.Changed = true
	}
}

// analyzeChangedFiles examines the files changed since base and marks the
// affected images; children are rebuilt through the graph's dependents.
//
//   - a change in an image's Dockerfile folder marks it (see
//     Config.ChangedImages)
//   - a changed source file (the config file or a --sources file) is compared
//     with its content at base: an image whose entry changed (version, args,
//     labels, base_image, ...), moved to another parent or is new is marked.
//     Comments and formatting don't count, so a version bump marks only the
//     bumped images.
//   - a change in the config file's global settings (see buildSettingsChanged)
//     marks every image, in every source; a change outside `images` in a
//     --sources file marks that source's images. A source file that didn't
//     exist at base marks all its images.
func (cd *ChangeDetector) analyzeChangedFiles(base string, changedFiles []string) error {
	changed := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		changed[cleanPath(f)] = true
	}

	for _, id := range cd.config.SourceIDs() {
		source := cd.config.Sources[id]
		if source.Path == "" || !changed[cleanPath(source.Path)] {
			continue
		}
		images, all, err := cd.changedEntries(base, source.Path, id == MainSource)
		if err != nil {
			return err
		}
		if all && id == MainSource {
			log.Info().Str("file", source.Path).Msg("global configuration changed - marking all images for rebuild")
			cd.markAllImagesChanged()
			return nil
		}
		walkImages(source.Images, func(img *ImageConfig) {
			if all || images[img.Name] {
				img.Changed = true
				log.Debug().Str("image", img.Name).Str("file", source.Path).Msg("image definition changed")
			}
		})
	}

	for img, file := range cd.config.ChangedImages(changedFiles) {
		img.Changed = true
		log.Debug().Str("image", img.Name).Str("file", file).Msg("marking image as changed")
	}
	return nil
}

// commandDefaults are the `config:` keys that only set command defaults: they
// don't change what is built, so changing them rebuilds nothing.
var commandDefaults = []string{"workers", "skip_version_check", "changed_only", "base_ref", "version_increment"}

// changedEntries compares a source file on disk with its content at base. It
// returns the names of the images whose entry differs, or all=true when every
// image of the file must be rebuilt: the file is new, or its settings outside
// `images` changed (for the config file, ignoring commandDefaults).
func (cd *ChangeDetector) changedEntries(base, path string, isMain bool) (map[string]bool, bool, error) {
	current, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	old, err := cd.gitClient.ShowFile(base, path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	var before, after map[string]any
	if err := yaml.Unmarshal(old, &before); err != nil {
		return nil, false, fmt.Errorf("failed to parse %s at %s: %w", path, base, err)
	}
	if err := yaml.Unmarshal(current, &after); err != nil {
		return nil, false, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if !reflect.DeepEqual(settings(before, isMain), settings(after, isMain)) {
		return nil, true, nil
	}

	beforeEntries, afterEntries := map[string]imageEntry{}, map[string]imageEntry{}
	flattenEntries(before["images"], "", beforeEntries)
	flattenEntries(after["images"], "", afterEntries)
	names := map[string]bool{}
	for name, entry := range afterEntries {
		if prev, ok := beforeEntries[name]; !ok || !reflect.DeepEqual(prev, entry) {
			names[name] = true
		}
	}
	return names, false, nil
}

// settings returns a parsed source file without its images (and, for the
// config file, without the command defaults).
func settings(doc map[string]any, isMain bool) map[string]any {
	out := map[string]any{}
	for k, v := range doc {
		if k == "images" {
			continue
		}
		if k == "config" && isMain {
			if section, ok := v.(map[string]any); ok {
				trimmed := map[string]any{}
				for sk, sv := range section {
					trimmed[sk] = sv
				}
				for _, key := range commandDefaults {
					delete(trimmed, key)
				}
				v = trimmed
			}
		}
		out[k] = v
	}
	return out
}

// imageEntry is one image's definition in a source file: its parent and its
// own fields (without its children).
type imageEntry struct {
	parent string
	fields map[string]any
}

// flattenEntries collects every image of a parsed `images` tree by name.
func flattenEntries(images any, parent string, out map[string]imageEntry) {
	tree, _ := images.(map[string]any)
	for name, v := range tree {
		fields := map[string]any{}
		node, _ := v.(map[string]any)
		for k, fv := range node {
			if k != "images" {
				fields[k] = fv
			}
		}
		out[name] = imageEntry{parent: parent, fields: fields}
		flattenEntries(node["images"], name, out)
	}
}

// cleanPath normalizes a path for comparison with git's output: slash-separated,
// cleaned, and relative to the working directory when given as absolute.
func cleanPath(p string) string {
	if filepath.IsAbs(p) {
		if wd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(wd, p); err == nil {
				p = rel
			}
		}
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// GetChangedImageNames returns the names of the images marked as changed
func (cd *ChangeDetector) GetChangedImageNames() []string {
	var changed []string
	for _, img := range cd.config.allImages() {
		if img.Changed {
			changed = append(changed, img.Name)
		}
	}
	return changed
}

// ShouldBuildAll returns true if all images should be built (not in git repo, no commits, etc.)
func (cd *ChangeDetector) ShouldBuildAll() bool {
	if !cd.gitClient.IsGitRepo() || !cd.gitClient.HasCommits() {
		return true
	}

	// Check if any image is marked as changed
	return len(cd.GetChangedImageNames()) == 0
}

// GetGitStatus returns a summary of the git repository status
func (cd *ChangeDetector) GetGitStatus() map[string]interface{} {
	status := make(map[string]interface{})

	status["is_git_repo"] = cd.gitClient.IsGitRepo()
	status["has_commits"] = cd.gitClient.HasCommits()

	if cd.gitClient.IsGitRepo() {
		if branch, err := cd.gitClient.GetCurrentBranch(); err == nil {
			status["current_branch"] = branch
		}

		if clean, err := cd.gitClient.IsCleanWorkingTree(); err == nil {
			status["is_clean"] = clean
		}

		if workDir, err := cd.gitClient.GetWorkingDirectory(); err == nil {
			status["work_dir"] = workDir
		}
	}

	return status
}
