package plan

import (
	"testing"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/graph"
	"github.com/rawmind0/arbor/test/examples"
)

// loadExample loads an example use-case config (the single source of test config
// data). Use the examples.* constants for the use case name.
func loadExample(t *testing.T, useCase string) *config.Config {
	t.Helper()
	cfg, err := config.LoadConfig(examples.Config(useCase), nil)
	if err != nil {
		t.Fatalf("load example %q: %v", useCase, err)
	}
	return cfg
}

// planFromExample loads an example, builds its graph, and generates a plan in the
// given builder mode (use builder.ModeMock for dry-run execution in tests).
func planFromExample(t *testing.T, useCase string, mode builder.BuilderMode) *BuildPlan {
	t.Helper()
	cfg := loadExample(t, useCase)
	cfg.Config.Mode = string(mode)

	g := graph.NewGraph(cfg)
	if err := g.Build(); err != nil {
		t.Fatalf("build graph for %q: %v", useCase, err)
	}
	p, err := NewGenerator(cfg, g).Generate()
	if err != nil {
		t.Fatalf("generate plan for %q: %v", useCase, err)
	}
	return p
}
