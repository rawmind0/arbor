package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/rawmind0/arbor/internal/git"
)

// ChangeDetector handles detection of changes in configuration and templates
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

	// Get changed files
	var changedFiles []string
	var err error

	if baseRef == "" {
		// Check for uncommitted changes first
		changedFiles, err = cd.gitClient.GetUncommittedFiles()
		if err != nil {
			return fmt.Errorf("failed to get uncommitted files: %w", err)
		}

		// If no uncommitted changes, check against last commit
		if len(changedFiles) == 0 {
			changedFiles, err = cd.gitClient.GetChangedFiles("HEAD~1")
			if err != nil {
				log.Warn().Err(err).Msg("failed to detect changes - building all images")
				cd.markAllImagesChanged()
				return nil
			}
		}
	} else {
		changedFiles, err = cd.gitClient.GetChangedFiles(baseRef)
		if err != nil {
			return fmt.Errorf("failed to get changed files: %w", err)
		}
	}

	if len(changedFiles) == 0 {
		log.Info().Msg("no changes detected")
		return nil
	}

	log.Info().Int("count", len(changedFiles)).Msg("detected changed files")

	// Analyze changed files and mark affected images
	cd.analyzeChangedFiles(changedFiles)

	return nil
}

// markAllImagesChanged marks all images as changed
func (cd *ChangeDetector) markAllImagesChanged() {
	cd.markImagesChangedRecursive(cd.config.Images)
}

func (cd *ChangeDetector) markImagesChangedRecursive(images map[string]*ImageConfig) {
	for _, img := range images {
		img.Changed = true
		if img.Images != nil {
			cd.markImagesChangedRecursive(img.Images)
		}
	}
}

// analyzeChangedFiles examines the list of changed files and marks affected images
func (cd *ChangeDetector) analyzeChangedFiles(changedFiles []string) {
	configChanged := false
	changedTemplates := make(map[string]bool)

	for _, file := range changedFiles {
		// Check if config file changed
		if strings.HasSuffix(file, "config.yaml") || strings.HasSuffix(file, "config.yml") {
			configChanged = true
			log.Debug().Str("file", file).Msg("config file changed")
			continue
		}

		// Check if it's a dockerfile template
		if cd.isDockerfileTemplate(file) {
			changedTemplates[file] = true
			log.Debug().Str("file", file).Msg("template changed")
		}
	}

	// If config changed, mark all images as changed
	if configChanged {
		log.Info().Msg("configuration changed - marking all images for rebuild")
		cd.markAllImagesChanged()
		return
	}

	// Mark specific images based on template changes
	cd.markImagesForChangedTemplates(changedTemplates)
}

// isDockerfileTemplate checks if a file is a dockerfile template
func (cd *ChangeDetector) isDockerfileTemplate(filePath string) bool {
	sourceDir := cd.config.Config.Dockerfiles.Source

	// Check if file is in the source directory
	if !strings.HasPrefix(filePath, sourceDir+"/") && filePath != sourceDir {
		return false
	}

	// Check common dockerfile patterns
	basename := filepath.Base(filePath)
	return strings.HasPrefix(basename, "Dockerfile") ||
		strings.Contains(basename, "dockerfile") ||
		strings.HasSuffix(basename, ".dockerfile")
}

// markImagesForChangedTemplates marks images whose templates have changed
func (cd *ChangeDetector) markImagesForChangedTemplates(changedTemplates map[string]bool) {
	cd.markImagesForChangedTemplatesRecursive(cd.config.Images, "", changedTemplates)
}

func (cd *ChangeDetector) markImagesForChangedTemplatesRecursive(images map[string]*ImageConfig, parentPath string, changedTemplates map[string]bool) {
	for name, img := range images {
		// Get the expected template path for this image
		templatePath := img.GetDockerfilePath(cd.config.Config.Dockerfiles.Source)

		// Check if this image's template was changed
		for changedFile := range changedTemplates {
			if changedFile == templatePath || strings.HasSuffix(changedFile, filepath.Base(templatePath)) {
				img.Changed = true
				log.Debug().Str("image", name).Str("template", changedFile).Msg("marking image as changed")
				break
			}
		}

		// Process child images
		if img.Images != nil {
			childPath := name
			if parentPath != "" {
				childPath = parentPath + "/" + name
			}
			cd.markImagesForChangedTemplatesRecursive(img.Images, childPath, changedTemplates)
		}
	}
}

// GetChangedImageNames returns a list of image names that have been marked as changed
func (cd *ChangeDetector) GetChangedImageNames() []string {
	var changed []string
	cd.getChangedImageNamesRecursive(cd.config.Images, "", &changed)
	return changed
}

func (cd *ChangeDetector) getChangedImageNamesRecursive(images map[string]*ImageConfig, parentPath string, result *[]string) {
	for name, img := range images {
		fullName := name
		if parentPath != "" {
			fullName = parentPath + "/" + name
		}

		if img.Changed {
			*result = append(*result, fullName)
		}

		if img.Images != nil {
			cd.getChangedImageNamesRecursive(img.Images, fullName, result)
		}
	}
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
