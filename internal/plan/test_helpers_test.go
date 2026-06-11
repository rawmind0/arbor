package plan

import (
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/graph"
)

// createMinimalTestConfig creates a minimal valid config for testing.
func createMinimalTestConfig() *config.Config {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Builder:       "docker",
			Workers:       2,
			Architectures: []string{"linux/amd64"},
			Dockerfiles: config.DockerfilesConfig{
				Source:      "src",
				Destination: "build",
			},
		},
		Images: map[string]*config.ImageConfig{
			"base": {
				Name:          "base",
				Version:       "1.0.0",
				BaseImage:     "ubuntu:22.04",
				Architectures: []string{"linux/amd64"},
			},
			"app": {
				Name:          "app",
				Version:       "2.0.0",
				Parent:        "base",
				Architectures: []string{"linux/amd64"},
			},
		},
	}
	processTestImageTree(cfg)
	return cfg
}

// processTestImageTree ensures all internal fields are populated.
func processTestImageTree(cfg *config.Config) {
	for name, img := range cfg.Images {
		if img.Name == "" {
			img.Name = name
		}
		if len(img.Architectures) == 0 {
			img.Architectures = cfg.Config.Architectures
		}
	}
}

// buildTestGraph creates a graph from config and builds it.
func buildTestGraph(t *testing.T, cfg *config.Config) *graph.Graph {
	t.Helper()
	g := graph.NewGraph(cfg)
	if err := g.Build(); err != nil {
		t.Fatalf("Failed to build graph: %v", err)
	}
	return g
}
