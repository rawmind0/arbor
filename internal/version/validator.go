package version

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/git"
)

// ValidationResult contains the results of version validation
type ValidationResult struct {
	Valid           bool
	UpdatesRequired map[string]VersionUpdate // image name -> update info
	Warnings        []string
	Errors          []string
}

// VersionUpdate contains information about a version update
type VersionUpdate struct {
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

// Validator handles version validation and cascading
type Validator struct {
	config     *config.Config
	gitClient  *git.Client
	sourceDir  string
	updates    map[string]VersionUpdate
	imageGraph map[string][]string // parent -> children mapping
}

// NewValidator creates a new version validator
func NewValidator(cfg *config.Config) *Validator {
	return &Validator{
		config:     cfg,
		gitClient:  git.NewClient("."),
		sourceDir:  cfg.Config.Dockerfiles.Source,
		updates:    make(map[string]VersionUpdate),
		imageGraph: make(map[string][]string),
	}
}

// Validate performs version validation and returns required updates
func (v *Validator) Validate() (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:           true,
		UpdatesRequired: make(map[string]VersionUpdate),
		Warnings:        []string{},
		Errors:          []string{},
	}

	// Build parent-child relationship map
	v.buildImageGraph()

	// Check for Dockerfile changes without version updates
	if err := v.checkDockerfileChanges(result); err != nil {
		return nil, fmt.Errorf("failed to check dockerfile changes: %w", err)
	}

	// Check for parent version changes that require child updates
	if err := v.checkParentVersionChanges(result); err != nil {
		return nil, fmt.Errorf("failed to check parent version changes: %w", err)
	}

	// Validate all versions are valid semver
	if err := v.validateSemverCompliance(result); err != nil {
		return nil, fmt.Errorf("failed to validate semver compliance: %w", err)
	}

	// Set validity based on whether updates are required
	result.Valid = len(result.UpdatesRequired) == 0 && len(result.Errors) == 0
	result.UpdatesRequired = v.updates

	return result, nil
}

// Apply applies a validation result's proposed version bumps to the config and
// persists it (to Config.Path). A no-op when there are no updates. Shared by the
// validate command's --fix path and Reconcile, so the apply/save logic lives in
// one place.
func (v *Validator) Apply(result *ValidationResult) error {
	if len(result.UpdatesRequired) == 0 {
		return nil
	}
	if err := v.ApplyUpdates(result.UpdatesRequired); err != nil {
		return fmt.Errorf("failed to apply version updates: %w", err)
	}
	if err := v.config.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save updated config: %w", err)
	}
	log.Info().Str("config", v.config.Path).Int("updates", len(result.UpdatesRequired)).Msg("applied version updates")
	return nil
}

// Reconcile keeps versions consistent: it validates the cascade and, when bumps
// are required, applies and persists them. A real validation error (not a
// proposed update) is returned; warnings and applied updates are logged. This is
// the version step the generate flow runs before rendering — one call, no
// reimplementation needed by callers.
func (v *Validator) Reconcile() error {
	result, err := v.Validate()
	if err != nil {
		return fmt.Errorf("failed to validate versions: %w", err)
	}

	for _, w := range result.Warnings {
		log.Warn().Msg(w)
	}
	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			log.Error().Msg(e)
		}
		return fmt.Errorf("version validation failed")
	}

	if len(result.UpdatesRequired) == 0 {
		log.Info().Msg("all versions are consistent")
		return nil
	}

	for _, u := range result.UpdatesRequired {
		log.Info().Str("image", u.ImageName).Str("from", u.OldVersion).Str("to", u.NewVersion).
			Str("reason", u.Reason).Msg("version update required")
	}
	return v.Apply(result)
}

// buildImageGraph builds a map of parent -> children relationships
func (v *Validator) buildImageGraph() {
	var processImages func(parent string, images map[string]*config.ImageConfig)

	processImages = func(parent string, images map[string]*config.ImageConfig) {
		for _, img := range images {
			if parent != "" {
				v.imageGraph[parent] = append(v.imageGraph[parent], img.Name)
			}
			if img.Images != nil {
				processImages(img.Name, img.Images)
			}
		}
	}

	processImages("", v.config.Images)
}

// checkDockerfileChanges checks if Dockerfiles have changed without version updates
func (v *Validator) checkDockerfileChanges(result *ValidationResult) error {
	if !v.gitClient.IsGitRepo() {
		result.Warnings = append(result.Warnings, "Not a git repository, skipping dockerfile change detection")
		return nil
	}

	// Get changed files from git
	changedFiles, err := v.gitClient.GetChangedFiles("")
	if err != nil {
		// Try uncommitted files
		changedFiles, err = v.gitClient.GetUncommittedFiles()
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Could not detect changed files: %v", err))
			return nil
		}
	}

	// Check each changed file
	for _, file := range changedFiles {
		if v.isDockerfileTemplate(file) {
			imageName := v.getImageNameFromDockerfile(file)
			if imageName != "" {
				img := v.config.FindImageByName(imageName)
				if img != nil {
					// Check if this image's version needs updating
					v.proposeVersionUpdate(img, v.getConfiguredUpdateType(),
						fmt.Sprintf("Dockerfile %s has been modified", file), result)
				}
			}
		}
	}

	return nil
}

// checkParentVersionChanges checks if parent versions have changed requiring child updates
func (v *Validator) checkParentVersionChanges(result *ValidationResult) error {
	// Process in dependency order
	var checkImage func(img *config.ImageConfig, parentVersion string)

	checkImage = func(img *config.ImageConfig, parentVersion string) {
		needsUpdate := false
		reason := ""

		// Check if parent version has changed
		if parentVersion != "" && img.BaseImage != "" {
			// Compare versions if parent was updated
			if update, exists := v.updates[img.Parent]; exists {
				needsUpdate = true
				reason = fmt.Sprintf("Parent image '%s' version changed from %s to %s",
					img.Parent, update.OldVersion, update.NewVersion)
			}
		}

		// If this image needs update, propose it
		if needsUpdate {
			v.proposeVersionUpdate(img, v.getConfiguredUpdateType(), reason, result)
		}

		// Check children
		if img.Images != nil {
			for _, child := range img.Images {
				checkImage(child, img.Version)
			}
		}
	}

	// Start from root images
	for _, img := range v.config.Images {
		checkImage(img, "")
	}

	return nil
}

// validateSemverCompliance validates all versions follow semver
func (v *Validator) validateSemverCompliance(result *ValidationResult) error {
	var validateImage func(img *config.ImageConfig)

	validateImage = func(img *config.ImageConfig) {
		if img.Version == "" {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Image '%s' is missing version", img.Name))
			return
		}

		_, err := Parse(img.Version)
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Image '%s' has invalid version '%s': %v", img.Name, img.Version, err))
		}

		// Check children
		if img.Images != nil {
			for _, child := range img.Images {
				validateImage(child)
			}
		}
	}

	for _, img := range v.config.Images {
		validateImage(img)
	}

	return nil
}

// getConfiguredUpdateType returns the update type based on configuration
func (v *Validator) getConfiguredUpdateType() UpdateType {
	switch v.config.Config.VersionIncrement {
	case "minor":
		return UpdateTypeMinor
	case "patch":
		return UpdateTypePatch
	default:
		return UpdateTypePatch
	}
}

// proposeVersionUpdate proposes a version update for an image
func (v *Validator) proposeVersionUpdate(img *config.ImageConfig, updateType UpdateType, reason string, result *ValidationResult) {
	// Skip if already proposed
	if _, exists := v.updates[img.Name]; exists {
		return
	}

	currentVer, err := Parse(img.Version)
	if err != nil {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Cannot update '%s': invalid current version '%s'", img.Name, img.Version))
		return
	}

	var newVer *Semver
	switch updateType {
	case UpdateTypePatch:
		newVer = currentVer.IncrementPatch()
	case UpdateTypeMinor:
		newVer = currentVer.IncrementMinor()
	case UpdateTypeMajor:
		newVer = currentVer.IncrementMajor()
	default:
		newVer = currentVer.IncrementPatch()
	}

	v.updates[img.Name] = VersionUpdate{
		ImageName:  img.Name,
		OldVersion: img.Version,
		NewVersion: newVer.String(),
		Reason:     reason,
		UpdateType: updateType,
	}

	// Cascade to children
	v.cascadeVersionUpdate(img.Name, result)
}

// cascadeVersionUpdate cascades version updates to child images
func (v *Validator) cascadeVersionUpdate(parentName string, result *ValidationResult) {
	children, exists := v.imageGraph[parentName]
	if !exists {
		return
	}

	for _, childName := range children {
		// Skip if already updated
		if _, exists := v.updates[childName]; exists {
			continue
		}

		child := v.config.FindImageByName(childName)
		if child != nil {
			reason := fmt.Sprintf("Parent image '%s' version was updated", parentName)
			v.proposeVersionUpdate(child, v.getConfiguredUpdateType(), reason, result)
		}
	}
}

// isDockerfileTemplate checks if a file is a Dockerfile template
func (v *Validator) isDockerfileTemplate(path string) bool {
	// Check if in source directory
	rel, err := filepath.Rel(v.sourceDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}

	// Check if it's a Dockerfile
	base := filepath.Base(path)
	return strings.HasPrefix(base, "Dockerfile")
}

// getImageNameFromDockerfile extracts image name from Dockerfile path
func (v *Validator) getImageNameFromDockerfile(path string) string {
	base := filepath.Base(path)
	// Remove "Dockerfile." prefix
	name := strings.TrimPrefix(base, "Dockerfile.")
	// Remove architecture suffix if present
	parts := strings.Split(name, "-")
	if len(parts) > 1 {
		// Check if last part is an architecture
		lastPart := parts[len(parts)-1]
		if lastPart == "amd64" || lastPart == "arm64" {
			name = strings.Join(parts[:len(parts)-1], "-")
		}
	}
	return name
}

// ApplyUpdates applies the proposed version updates to the configuration
func (v *Validator) ApplyUpdates(updates map[string]VersionUpdate) error {
	for _, update := range updates {
		img := v.config.FindImageByName(update.ImageName)
		if img == nil {
			return fmt.Errorf("image '%s' not found", update.ImageName)
		}
		img.Version = update.NewVersion
	}
	return nil
}

// GetLastModifiedTime gets the last modified time of a Dockerfile template
func (v *Validator) GetLastModifiedTime(imageName string) (time.Time, error) {
	// Try different possible names
	possibleFiles := []string{
		fmt.Sprintf("Dockerfile.%s", imageName),
		fmt.Sprintf("Dockerfile.%s-amd64", imageName),
		fmt.Sprintf("Dockerfile.%s-arm64", imageName),
	}

	var latestTime time.Time
	foundFile := false

	for _, filename := range possibleFiles {
		path := filepath.Join(v.sourceDir, filename)
		info, err := os.Stat(path)
		if err == nil {
			foundFile = true
			if info.ModTime().After(latestTime) {
				latestTime = info.ModTime()
			}
		}
	}

	if !foundFile {
		return time.Time{}, fmt.Errorf("no dockerfile found for image '%s'", imageName)
	}

	return latestTime, nil
}
