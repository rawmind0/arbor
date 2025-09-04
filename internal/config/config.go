package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Config ConfigSection           `yaml:"config"`
	Images map[string]*ImageConfig `yaml:"images"`
}

type ConfigSection struct {
	Builder       string            `yaml:"builder"`
	Workers       int               `yaml:"workers,omitempty"`
	Architectures []string          `yaml:"architectures,omitempty"`
	Dockerfiles   DockerfilesConfig `yaml:"dockerfiles"`
	Labels        map[string]string `yaml:"labels,omitempty"`
}

type DockerfilesConfig struct {
	Source      string `yaml:"source"`
	Destination string `yaml:"destination"`
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

func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set defaults
	if config.Config.Workers == 0 {
		config.Config.Workers = 2
	}
	if len(config.Config.Architectures) == 0 {
		config.Config.Architectures = []string{"linux/amd64"}
	}
	if config.Config.Dockerfiles.Source == "" {
		config.Config.Dockerfiles.Source = "src"
	}
	if config.Config.Dockerfiles.Destination == "" {
		config.Config.Dockerfiles.Destination = "build"
	}

	// Process image tree and populate internal fields
	if err := processImageTree(&config, "", config.Images); err != nil {
		return nil, fmt.Errorf("failed to process image tree: %w", err)
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

func (c *Config) SaveConfig(configPath string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (img *ImageConfig) GetDockerfilePath(sourceDir string) string {
	return filepath.Join(sourceDir, img.Dockerfile)
}

func (img *ImageConfig) GetBuildPath(destDir, arch string) string {
	archSuffix := ""
	if arch != "" {
		archSuffix = "-" + sanitizeArch(arch)
	}
	return filepath.Join(destDir, fmt.Sprintf("Dockerfile.%s%s", img.Name, archSuffix))
}

func sanitizeArch(arch string) string {
	return filepath.Base(arch) // converts "linux/amd64" to "amd64"
}
