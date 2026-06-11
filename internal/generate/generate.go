// Package generate renders and writes the Dockerfiles for a configuration. It
// owns the whole generation step (cleaning the destination, walking each source's
// dependency graph, resolving each image's FROM, rendering its template, and
// writing the result); the cmd layer only wires flags and calls Run.
package generate

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/graph"
	"github.com/rawmind0/arbor/internal/version"
)

//go:embed *.tmpl
var baseTemplates embed.FS

// TemplateData is the data passed to a Dockerfile template.
type TemplateData struct {
	FROM         string
	Architecture string
	Args         map[string]string
	Envs         map[string]string
	Labels       map[string]string
	Config       *config.Config
	Image        *config.ImageConfig
	BuildDate    string
}

// Generator renders and writes Dockerfiles for a configuration.
type Generator struct {
	config *config.Config
}

// New creates a generator for the given config.
func New(cfg *config.Config) *Generator {
	return &Generator{config: cfg}
}

// Run performs a full, idempotent generation: it clears the destination, then
// for every image in every source renders the Dockerfile (FROM resolved to the
// parent image) and writes it. The destination ends up reflecting exactly the
// current config. Returns the number of Dockerfiles written.
func (g *Generator) Run() (int, error) {
	// Keep versions consistent (cascade) before rendering, unless disabled in
	// config. The version module owns the validate/apply/save logic.
	if !g.config.Config.SkipVersionCheck {
		if err := version.NewValidator(g.config).Reconcile(); err != nil {
			return 0, err
		}
	}

	if err := g.cleanDestination(); err != nil {
		return 0, fmt.Errorf("failed to clean destination: %w", err)
	}

	count := 0
	for sourceID, source := range g.config.Sources {
		log.Info().Str("source", sourceID).Msg("processing source")

		// One graph per source (sources are independent).
		src := &config.Config{Config: g.config.Config, Images: source.Images, IsGitRepo: g.config.IsGitRepo}
		dag := graph.NewGraph(src)
		if err := dag.Build(); err != nil {
			return count, fmt.Errorf("failed to build dependency graph for source %s: %w", sourceID, err)
		}

		nodes := dag.GetBuildOrder()
		log.Info().Str("source", sourceID).Int("images", len(nodes)).Msg("generating Dockerfiles")
		for _, node := range nodes {
			// FROM: an external base for roots; for an internal parent its
			// canonical [url/]owner/name:version tag (resolved by the graph).
			content, err := g.GenerateDockerfile(node.Image, "", dag.GetParentImageTag(node))
			if err != nil {
				return count, fmt.Errorf("failed to generate dockerfile for %s: %w", node.ImageTag, err)
			}
			destPath := node.Image.GetBuildPath(g.config.Config.Dockerfiles.Destination)
			if err := g.writeDockerfile(content, destPath); err != nil {
				return count, fmt.Errorf("failed to write dockerfile for %s: %w", node.ImageTag, err)
			}
			log.Info().Str("dockerfile", destPath).Msg("generated")
			count++
		}
	}
	return count, nil
}

// GenerateDockerfile renders a single image's Dockerfile in memory, with FROM set
// to parentImage. Exposed so callers can verify generation without writing.
func (g *Generator) GenerateDockerfile(img *config.ImageConfig, arch string, parentImage string) (string, error) {
	data := &TemplateData{
		FROM:         parentImage,
		Architecture: arch,
		Args:         img.Args,
		Envs:         img.Envs,
		Labels:       g.mergeLabels(img),
		Config:       g.config,
		Image:        img,
		BuildDate:    time.Now().Format(time.RFC3339),
	}
	if data.Args == nil {
		data.Args = make(map[string]string)
	}
	if data.Envs == nil {
		data.Envs = make(map[string]string)
	}

	baseContent, err := baseTemplates.ReadFile("base.dockerfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to load base template: %w", err)
	}

	// Load image-specific template if it exists, else extend base with no content.
	imagePath := img.GetDockerfilePath(g.config.Config.Dockerfiles.Source)
	var imageContent []byte
	if _, err := os.Stat(imagePath); err == nil {
		imageContent, err = os.ReadFile(imagePath)
		if err != nil {
			return "", fmt.Errorf("failed to read image template %s: %w", imagePath, err)
		}
	} else {
		imageContent = []byte(`{{ template "base" . }}

{{ define "content" }}
# No custom content defined
{{ end }}`)
	}

	tmpl, err := template.New("base").Parse(string(baseContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse base template: %w", err)
	}
	if _, err := tmpl.New("image").Parse(string(imageContent)); err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.ExecuteTemplate(&result, "image", data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	return result.String(), nil
}

// mergeLabels combines global and image labels with the automatic image.* labels.
func (g *Generator) mergeLabels(img *config.ImageConfig) map[string]string {
	merged := make(map[string]string)
	for k, v := range g.config.Config.Labels {
		merged[k] = v
	}
	for k, v := range img.Labels {
		merged[k] = v
	}
	merged["image.name"] = img.Name
	merged["image.version"] = img.Version
	if img.Parent != "" {
		merged["image.parent"] = img.Parent
	}
	return merged
}

// writeDockerfile writes content to destPath, creating parent directories.
func (g *Generator) writeDockerfile(content, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	if err := os.WriteFile(destPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write dockerfile: %w", err)
	}
	return nil
}

// cleanDestination removes previously generated Dockerfile.* files so a full
// generation never leaves stale Dockerfiles behind (from removed/renamed images).
func (g *Generator) cleanDestination() error {
	matches, err := filepath.Glob(filepath.Join(g.config.Config.Dockerfiles.Destination, "Dockerfile.*"))
	if err != nil {
		return err
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			return fmt.Errorf("failed to remove stale dockerfile %s: %w", m, err)
		}
	}
	return nil
}
