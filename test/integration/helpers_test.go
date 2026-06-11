package integration

import (
	"os"
	"testing"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/graph"
	"github.com/rawmind0/arbor/internal/plan"
	"github.com/rawmind0/arbor/test/examples"
)

// useRepoRoot runs the test from the repository root so the example configs'
// relative dockerfiles.source paths resolve (the template processor reads them
// relative to the working directory).
func useRepoRoot(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(examples.Root()); err != nil {
		t.Fatal(err)
	}
}

// allExamples is every example use case, discovered dynamically from the
// examples/ directory (same as the Makefile) — no hardcoded list to maintain.
var allExamples = examples.All()

// loadExampleFull loads an example's config plus any extra source files in its
// folder (auto-globbed, exactly like `make example-* --sources "examples/<uc>/*.yaml"`).
// For single-config examples the glob matches only config.yaml (excluded), so it
// is a no-op; for multisource it pulls in the second stack.
func loadExampleFull(t *testing.T, useCase string) *config.Config {
	t.Helper()
	main := examples.Config(useCase)
	cfg, err := config.LoadConfig(main, nil)
	if err != nil {
		t.Fatalf("load %s config: %v", useCase, err)
	}
	if err := cfg.LoadAdditionalImages(main, []string{examples.File(useCase, "*.yaml")}); err != nil {
		t.Fatalf("load %s extra images: %v", useCase, err)
	}
	return cfg
}

// loadMultiSource is the multisource example loaded with its second stack.
func loadMultiSource(t *testing.T) *config.Config {
	t.Helper()
	return loadExampleFull(t, examples.MultiSource)
}

// multiSourcePlan builds a multi-queue plan from the multisource example in mock
// mode (commands are mock-wrapped, so the plan executes without Docker).
func multiSourcePlan(t *testing.T) *plan.BuildPlan {
	t.Helper()
	cfg := loadMultiSource(t)
	cfg.Config.Mode = string(builder.ModeMock)
	p, err := plan.NewGenerator(cfg, graph.NewGraph(cfg)).Generate()
	if err != nil {
		t.Fatalf("generate multisource plan: %v", err)
	}
	return p
}
