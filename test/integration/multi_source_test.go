package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/plan"
)

func TestMultiSourceLoading(t *testing.T) {
	// Loads examples/multisource/ (main config.yaml + services.yaml). Each file
	// is an independent hierarchy; only top-level images are counted per source.
	cfg := loadMultiSource(t)

	// main: web-base (root of the web stack); services: api-base (root of the api stack).
	wantImages := map[string]int{"main": 1, "services": 1}
	if len(cfg.Sources) != len(wantImages) {
		t.Errorf("Expected %d sources, got %d", len(wantImages), len(cfg.Sources))
	}
	for id, want := range wantImages {
		src, ok := cfg.Sources[id]
		if !ok {
			t.Errorf("source %q not found", id)
			continue
		}
		if len(src.Images) != want {
			t.Errorf("source %q: expected %d images, got %d", id, want, len(src.Images))
		}
	}
}

func TestConfigFileExclusion(t *testing.T) {
	tmpDir := t.TempDir()

	// Create config files
	configs := map[string]string{
		"config.yaml":     "images:\n  main:\n    version: 1.0.0\n    base_image: ubuntu:22.04",
		"additional.yaml": "images:\n  extra:\n    version: 1.0.0\n    base_image: alpine:3.18",
	}

	for name, content := range configs {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Load config
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfg, err := config.LoadConfig(configPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Load additional with glob that would match config.yaml
	pattern := filepath.Join(tmpDir, "*.yaml")
	if err := cfg.LoadAdditionalImages(configPath, []string{pattern}); err != nil {
		t.Fatal(err)
	}

	// Should have 2 sources: main and additional
	if len(cfg.Sources) != 2 {
		t.Errorf("Expected 2 sources, got %d", len(cfg.Sources))
	}

	// Verify config.yaml was excluded
	if _, ok := cfg.Sources["config"]; ok {
		t.Error("Config file should have been excluded from sources")
	}
}

func TestQueueGeneration(t *testing.T) {
	// Generate a plan from the multisource example (one queue per source).
	p := multiSourcePlan(t)

	// Verify queue structure
	if len(p.Queues) < 2 {
		t.Error("Expected at least 2 queues")
	}

	// Each queue should have its own ID
	seenIDs := make(map[string]bool)
	for _, queue := range p.Queues {
		if queue.ID == "" {
			t.Error("Queue missing ID")
		}
		if seenIDs[queue.ID] {
			t.Errorf("Duplicate queue ID: %s", queue.ID)
		}
		seenIDs[queue.ID] = true

		// Each queue should have batches
		if len(queue.Batches) == 0 {
			t.Errorf("Queue %s has no batches", queue.ID)
		}

		// Each queue should have commands
		if len(queue.Commands) == 0 {
			t.Errorf("Queue %s has no commands", queue.ID)
		}
	}
}

func TestParallelQueueExecution(t *testing.T) {
	// A multi-queue plan from the multisource example, in mock mode so its
	// commands execute without Docker.
	p := multiSourcePlan(t)

	// Create real executor with short timeout for testing
	executor := plan.NewExecutor(4, 10*time.Second)

	// Execute should process queues in parallel
	ctx := context.Background()
	results, err := executor.Execute(ctx, p)

	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	// Verify all commands were executed
	totalCommands := 0
	for _, queue := range p.Queues {
		totalCommands += len(queue.Commands)
	}

	if len(results) != totalCommands {
		t.Errorf("Expected %d results, got %d", totalCommands, len(results))
	}

	// Verify all results are successful
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Command %s failed: %v", result.Command.ImageTag, result.Error)
		}
	}
}

func TestSourceIsolation(t *testing.T) {
	// Test that images from different sources stay separate.
	cfg := loadMultiSource(t)

	// Add a top-level image to the services source; it must not affect main.
	cfg.Sources["services"].Images["extra"] = &config.ImageConfig{
		Name:    "extra",
		Version: "1.0.0",
	}

	// Sources remain separate.
	if len(cfg.Sources) != 2 {
		t.Error("Sources should remain separate")
	}

	// Verify each source has its expected top-level images.
	expectations := map[string]int{
		"main":     1, // web-base
		"services": 2, // api-base + extra
	}

	for sourceID, expectedCount := range expectations {
		source := cfg.Sources[sourceID]
		if source == nil {
			t.Errorf("Source %s not found", sourceID)
			continue
		}
		if len(source.Images) != expectedCount {
			t.Errorf("Source %s: expected %d images, got %d",
				sourceID, expectedCount, len(source.Images))
		}
	}
}

func TestEmptySourceHandling(t *testing.T) {
	tmpDir := t.TempDir()

	// Create config with empty images section
	emptyConfig := `
# This source has no images
images:
`
	emptyPath := filepath.Join(tmpDir, "empty.yaml")
	if err := os.WriteFile(emptyPath, []byte(emptyConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main config
	mainConfig := `
config:
  builder: docker
images:
  base:
    version: 1.0.0
    base_image: ubuntu:22.04
`
	mainPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(mainPath, []byte(mainConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Load configs
	cfg, err := config.LoadConfig(mainPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Load additional sources including empty one
	pattern := filepath.Join(tmpDir, "*.yaml")
	if err := cfg.LoadAdditionalImages(mainPath, []string{pattern}); err != nil {
		t.Fatal(err)
	}

	// Empty source should be skipped
	if _, ok := cfg.Sources["empty"]; ok {
		t.Error("Empty source should not be added")
	}

	// Should only have main source
	if len(cfg.Sources) != 1 {
		t.Errorf("Expected 1 source, got %d", len(cfg.Sources))
	}
}

func TestInvalidImageFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid YAML file
	invalidYAML := `
images:
  broken:
    version: [this is not valid
`
	invalidPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Create main config
	mainConfig := `
config:
  builder: docker
images:
  base:
    version: 1.0.0
`
	mainPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(mainPath, []byte(mainConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Load main config
	cfg, err := config.LoadConfig(mainPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to load invalid file
	err = cfg.LoadAdditionalImages(mainPath, []string{invalidPath})
	if err == nil {
		t.Error("Expected error loading invalid YAML")
	}
}

func TestDuplicateImageNames(t *testing.T) {
	// Test handling of duplicate image names across sources. The source map
	// mechanics are what's under test, so start from an empty config.
	cfg := &config.Config{Sources: map[string]*config.ImageSource{}}

	// Add sources with duplicate image names
	cfg.Sources["source1"] = &config.ImageSource{
		ID: "source1",
		Images: map[string]*config.ImageConfig{
			"app": {
				Name:    "app",
				Version: "1.0.0",
			},
		},
	}

	cfg.Sources["source2"] = &config.ImageSource{
		ID: "source2",
		Images: map[string]*config.ImageConfig{
			"app": { // Same name as in source1
				Name:    "app",
				Version: "2.0.0",
			},
		},
	}

	// Both sources should exist independently
	if len(cfg.Sources) != 2 {
		t.Errorf("Expected 2 sources, got %d", len(cfg.Sources))
	}

	// Each should have its own "app" image
	for _, sourceID := range []string{"source1", "source2"} {
		source := cfg.Sources[sourceID]
		if source == nil {
			t.Errorf("Source %s not found", sourceID)
			continue
		}

		if app, ok := source.Images["app"]; !ok {
			t.Errorf("App not found in source %s", sourceID)
		} else {
			// Verify they are different versions
			expectedVersion := "1.0.0"
			if sourceID == "source2" {
				expectedVersion = "2.0.0"
			}
			if app.Version != expectedVersion {
				t.Errorf("Source %s: expected version %s, got %s",
					sourceID, expectedVersion, app.Version)
			}
		}
	}
}
