package template

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/rawmind0/container-builder/internal/config"
)

//go:embed *.tmpl
var baseTemplates embed.FS

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

type Processor struct {
	config *config.Config
}

func NewProcessor(cfg *config.Config) *Processor {
	return &Processor{
		config: cfg,
	}
}

func (p *Processor) GenerateDockerfile(img *config.ImageConfig, arch string, parentImage string) (string, error) {
	// Create template data
	data := &TemplateData{
		FROM:         parentImage,
		Architecture: arch,
		Args:         img.Args,
		Envs:         img.Envs,
		Labels:       p.mergeLabels(img),
		Config:       p.config,
		Image:        img,
		BuildDate:    time.Now().Format(time.RFC3339),
	}

	// If no args/envs, initialize empty maps
	if data.Args == nil {
		data.Args = make(map[string]string)
	}
	if data.Envs == nil {
		data.Envs = make(map[string]string)
	}

	// Load base template
	baseContent, err := baseTemplates.ReadFile("base.dockerfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to load base template: %w", err)
	}

	// Load image-specific template if it exists
	imagePath := img.GetDockerfilePath(p.config.Config.Dockerfiles.Source)
	var imageContent []byte
	if _, err := os.Stat(imagePath); err == nil {
		imageContent, err = os.ReadFile(imagePath)
		if err != nil {
			return "", fmt.Errorf("failed to read image template %s: %w", imagePath, err)
		}
	} else {
		// Create a simple default template that just extends base with empty content
		imageContent = []byte(`{{ template "base" . }}

{{ define "content" }}
# No custom content defined
{{ end }}`)
	}

	// Parse base template first
	tmpl, err := template.New("base").Parse(string(baseContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse base template: %w", err)
	}

	// Add image template
	_, err = tmpl.New("image").Parse(string(imageContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.ExecuteTemplate(&result, "image", data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return result.String(), nil
}

func (p *Processor) mergeLabels(img *config.ImageConfig) map[string]string {
	merged := make(map[string]string)

	// Start with global labels
	for k, v := range p.config.Config.Labels {
		merged[k] = v
	}

	// Add image-specific labels
	for k, v := range img.Labels {
		merged[k] = v
	}

	// Add automatic labels
	merged["image.name"] = img.Name
	merged["image.version"] = img.Version
	if img.Parent != "" {
		merged["image.parent"] = img.Parent
	}

	return merged
}

func (p *Processor) WriteDockerfile(content, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if err := os.WriteFile(destPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write dockerfile: %w", err)
	}

	return nil
}
