package plan

import (
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/graph"
	"github.com/rawmind0/arbor/test/examples"
)

func TestNewGenerator(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Builder: "docker",
			Workers: 2,
		},
	}
	g := graph.NewGraph(cfg)

	gen := NewGenerator(cfg, g)
	if gen == nil {
		t.Fatal("NewGenerator returned nil")
	}
	if gen.config != cfg {
		t.Error("Generator config mismatch")
	}
	if gen.graph != g {
		t.Error("Generator graph mismatch")
	}
}

func TestGenerator_GenerateMultiSource(t *testing.T) {
	// Create a config with multiple sources manually
	// Since Sources is not loaded from YAML (has yaml:"-" tag)

	// Create configs for each source
	baseImage := &config.ImageConfig{
		Name:          "base",
		Version:       "1.0.0",
		BaseImage:     "ubuntu:22.04",
		FullName:      "base",
		Architectures: []string{"linux/amd64"},
	}

	postgresImage := &config.ImageConfig{
		Name:          "postgres",
		Version:       "15.0",
		BaseImage:     "postgres:15",
		FullName:      "postgres",
		Architectures: []string{"linux/amd64"},
	}

	mysqlImage := &config.ImageConfig{
		Name:          "mysql",
		Version:       "8.0",
		BaseImage:     "mysql:8",
		FullName:      "mysql",
		Architectures: []string{"linux/amd64"},
	}

	mainConfig := &config.Config{
		Config: config.ConfigSection{
			Builder:       "docker",
			Workers:       4,
			Architectures: []string{"linux/amd64"},
			Dockerfiles: config.DockerfilesConfig{
				Source:      "src",
				Destination: "build",
			},
		},
		Sources: map[string]*config.ImageSource{
			"main": {
				ID: "main",
				Images: map[string]*config.ImageConfig{
					"base": baseImage,
				},
			},
			"databases": {
				ID: "databases",
				Images: map[string]*config.ImageConfig{
					"postgres": postgresImage,
					"mysql":    mysqlImage,
				},
			},
		},
	}

	// Generate plan with multiple sources
	gen := NewGenerator(mainConfig, nil)

	// Debug: Check sources are set
	if len(mainConfig.Sources) == 0 {
		t.Error("mainConfig.Sources is empty")
	}
	for id, source := range mainConfig.Sources {
		t.Logf("Source %s has %d images", id, len(source.Images))
	}

	plan, err := gen.GenerateMultiSource()
	if err != nil {
		t.Fatalf("GenerateMultiSource() failed: %v", err)
	}

	// Verify we have multiple queues
	if len(plan.Queues) != 2 {
		t.Errorf("Expected 2 queues, got %d", len(plan.Queues))
	}

	// Verify queue IDs exist
	queueIDs := make(map[string]bool)
	for _, q := range plan.Queues {
		queueIDs[q.ID] = true
	}

	// Check that we have expected source queues (one queue per source)
	expectedQueues := []string{"main", "databases"}
	for _, expected := range expectedQueues {
		if !queueIDs[expected] {
			t.Errorf("Missing expected queue: %s", expected)
		}
	}

	// Verify total commands is reasonable
	totalCommands := plan.GetTotalCommands()
	if totalCommands != 3 { // base + postgres + mysql
		t.Errorf("Expected 3 commands, got %d", totalCommands)
	}
}

func TestGenerator_createBuildCommand(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Builder: "docker",
			Dockerfiles: config.DockerfilesConfig{
				Source:      "src",
				Destination: "build",
			},
			Labels: map[string]string{
				"global": "label",
			},
		},
	}

	gen := &Generator{config: cfg}

	node := &graph.Node{
		Image: &config.ImageConfig{
			Name:          "test",
			Version:       "1.0.0",
			Architectures: []string{"linux/amd64"},
			Args: map[string]string{
				"ARG1": "value1",
			},
			Envs: map[string]string{
				"ENV1": "value1",
			},
			Labels: map[string]string{
				"local": "label",
			},
		},
		ImageTag: "test:1.0.0",
	}

	cmd := gen.createBuildCommand(node)

	// Verify command fields
	if cmd.ImageName != "test" {
		t.Errorf("ImageName = %s, want test", cmd.ImageName)
	}
	if cmd.ImageTag != "test:1.0.0" {
		t.Errorf("ImageTag = %s, want test:1.0.0", cmd.ImageTag)
	}
	if cmd.Architecture != "linux/amd64" {
		t.Errorf("Architecture = %s, want linux/amd64", cmd.Architecture)
	}
	if cmd.DockerfilePath != "build/Dockerfile.test" {
		t.Errorf("DockerfilePath = %s, want build/Dockerfile.test", cmd.DockerfilePath)
	}

	// Verify labels merge - should have global + local + automatic labels
	if len(cmd.Labels) < 2 {
		t.Errorf("Expected at least 2 labels, got %d", len(cmd.Labels))
	}
	if cmd.Labels["global"] != "label" {
		t.Error("Missing global label")
	}
	if cmd.Labels["local"] != "label" {
		t.Error("Missing local label")
	}
	// Check automatic labels are present
	if cmd.Labels["image.version"] == "" {
		t.Error("Missing automatic image.version label")
	}
	if cmd.Labels["image.name"] == "" {
		t.Error("Missing automatic image.name label")
	}

	// Verify args and envs
	if len(cmd.BuildArgs) != 1 || cmd.BuildArgs["ARG1"] != "value1" {
		t.Error("BuildArgs not set correctly")
	}
	if len(cmd.Environment) != 1 || cmd.Environment["ENV1"] != "value1" {
		t.Error("Environment not set correctly")
	}
}

func TestGenerator_WithScenarios(t *testing.T) {
	// Test with pre-generated scenario configs
	scenarios := []struct {
		name        string
		minQueues   int
		minCommands int
		minBatches  int
	}{
		{examples.Minimal, 1, 1, 1},
		{examples.Hierarchy, 1, 6, 3},   // ubuntu -> {nodejs->{frontend,backend}, golang->worker}
		{examples.MultiArch, 1, 3, 3},   // ubuntu -> golang -> app (one command per image)
		{examples.MultiSource, 1, 3, 2}, // main source: web-base -> {site, admin}
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			cfg := loadExample(t, sc.name)

			g := graph.NewGraph(cfg)
			if err := g.Build(); err != nil {
				t.Fatalf("Failed to build graph: %v", err)
			}

			gen := NewGenerator(cfg, g)
			plan, err := gen.Generate()
			if err != nil {
				t.Fatalf("Generate() failed: %v", err)
			}

			// Validate minimum expectations
			if len(plan.Queues) < sc.minQueues {
				t.Errorf("Expected at least %d queues, got %d", sc.minQueues, len(plan.Queues))
			}

			totalCommands := plan.GetTotalCommands()
			if totalCommands < sc.minCommands {
				t.Errorf("Expected at least %d commands, got %d", sc.minCommands, totalCommands)
			}

			totalBatches := plan.GetTotalBatches()
			if totalBatches < sc.minBatches {
				t.Errorf("Expected at least %d batches, got %d", sc.minBatches, totalBatches)
			}

			if err := plan.Validate(); err != nil {
				t.Errorf("Generated plan is invalid: %v", err)
			}

			// Every generated command must carry its required fields.
			for _, queue := range plan.Queues {
				for _, cmd := range queue.Commands {
					if cmd.ImageTag == "" || cmd.ImageName == "" || cmd.DockerfilePath == "" {
						t.Errorf("command %q missing required fields", cmd.ImageName)
					}
				}
			}
		})
	}
}
func TestGenerator_GenerateChanged(t *testing.T) {
	cfg := createMinimalTestConfig()
	g := buildTestGraph(t, cfg)
	gen := NewGenerator(cfg, g)

	// With no changed nodes detected, the plan should be empty but flagged changed-only.
	plan, err := gen.GenerateChanged()
	if err != nil {
		t.Fatalf("GenerateChanged() failed: %v", err)
	}

	if len(plan.Queues) != 0 {
		t.Errorf("Expected no queues for no changes, got %d", len(plan.Queues))
	}

	if !plan.Metadata.ChangedOnly {
		t.Error("Expected ChangedOnly to be true")
	}
}

func TestGenerator_GenerateForNodes(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Builder:       "podman",
			Workers:       4,
			Architectures: []string{"linux/amd64", "linux/arm64"},
			Dockerfiles: config.DockerfilesConfig{
				Destination: "dockerfiles",
			},
		},
		Images: map[string]*config.ImageConfig{
			"base": {
				Name:          "base",
				Version:       "1.0.0",
				BaseImage:     "alpine:latest",
				Architectures: []string{"linux/amd64", "linux/arm64"},
			},
			"service1": {
				Name:          "service1",
				Version:       "1.0.0",
				Parent:        "base",
				Architectures: []string{"linux/amd64", "linux/arm64"},
			},
			"service2": {
				Name:          "service2",
				Version:       "1.0.0",
				Parent:        "base",
				Architectures: []string{"linux/amd64", "linux/arm64"},
			},
		},
	}

	processTestImageTree(cfg)
	g := buildTestGraph(t, cfg)
	gen := NewGenerator(cfg, g)

	// Empty node set produces an empty plan.
	plan, err := gen.GenerateForNodes([]*graph.Node{})
	if err != nil {
		t.Fatalf("GenerateForNodes() failed: %v", err)
	}
	if len(plan.Queues) != 0 {
		t.Errorf("Expected no queues for no nodes, got %d", len(plan.Queues))
	}

	// A specific node set produces one command per node, in a single "main" queue.
	nodes := g.GetBuildOrder()
	if len(nodes) > 0 {
		testNodes := nodes
		if len(nodes) > 3 {
			testNodes = nodes[:3]
		}

		plan, err = gen.GenerateForNodes(testNodes)
		if err != nil {
			t.Fatalf("GenerateForNodes() failed: %v", err)
		}

		if plan.GetTotalCommands() != len(testNodes) {
			t.Errorf("Expected %d commands, got %d", len(testNodes), plan.GetTotalCommands())
		}

		if plan.Metadata.Builder != "podman" {
			t.Errorf("Expected podman builder, got %s", plan.Metadata.Builder)
		}
	}
}

func TestGenerator_Push(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Builder:       "docker",
			Architectures: []string{"linux/amd64", "linux/arm64"},
			Registry:      config.RegistryConfig{URL: "docker.io", Owner: "myorg"},
			Push:          true,
			Dockerfiles:   config.DockerfilesConfig{Source: "src", Destination: "build"},
		},
		Images: map[string]*config.ImageConfig{
			"app": {
				Name:          "app",
				Version:       "1.0.0",
				BaseImage:     "alpine:3.18",
				Architectures: []string{"linux/amd64", "linux/arm64"},
			},
		},
	}

	g := graph.NewGraph(cfg)
	if err := g.Build(); err != nil {
		t.Fatalf("graph build: %v", err)
	}
	plan, err := NewGenerator(cfg, g).Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// One queue per source, login attached once.
	if len(plan.Queues) != 1 {
		t.Fatalf("want 1 queue, got %d", len(plan.Queues))
	}
	if len(plan.Login) == 0 {
		t.Error("expected plan.Login to be set when pushing")
	}

	cmd := plan.Queues[0].Commands[0]
	// docker buildx: a single multi-platform build that pushes the manifest.
	if len(cmd.Steps) != 1 {
		t.Fatalf("want 1 buildx step, got %d: %v", len(cmd.Steps), cmd.Steps)
	}
	s := strings.Join(cmd.Steps[0], " ")
	if !strings.Contains(s, "docker buildx build") || !strings.Contains(s, "--platform linux/amd64,linux/arm64") {
		t.Errorf("expected multi-platform buildx build: %q", s)
	}
	if !strings.Contains(s, "-t docker.io/myorg/app:1.0.0") || !strings.Contains(s, "--push") {
		t.Errorf("expected registry tag + --push: %q", s)
	}
}

func TestGenerator_NoPushBuildsAllArches(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{
			Builder:       "docker",
			Architectures: []string{"linux/amd64", "linux/arm64"},
			Dockerfiles:   config.DockerfilesConfig{Source: "src", Destination: "build"},
		},
		Images: map[string]*config.ImageConfig{
			"app": {Name: "app", Version: "1.0.0", BaseImage: "alpine:3.18", Architectures: []string{"linux/amd64", "linux/arm64"}},
		},
	}
	g := graph.NewGraph(cfg)
	if err := g.Build(); err != nil {
		t.Fatal(err)
	}
	plan, err := NewGenerator(cfg, g).Generate()
	if err != nil {
		t.Fatal(err)
	}

	// Non-push still builds every architecture; just no login/push/manifest.
	if len(plan.Login) != 0 {
		t.Error("non-push plan should have no login")
	}
	cmd := plan.Queues[0].Commands[0]
	// Single multi-platform build for both arches; no --push.
	if len(cmd.Steps) != 1 {
		t.Fatalf("expected single multi-platform build, got %d: %v", len(cmd.Steps), cmd.Steps)
	}
	s := strings.Join(cmd.Steps[0], " ")
	if !strings.Contains(s, "--platform linux/amd64,linux/arm64") {
		t.Errorf("non-push build should target both arches: %q", s)
	}
	if strings.Contains(s, "--push") {
		t.Errorf("non-push must not have --push: %q", s)
	}
}
