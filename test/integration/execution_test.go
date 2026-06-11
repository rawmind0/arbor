package integration

import (
	"context"
	"testing"
	"time"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/graph"
	"github.com/rawmind0/arbor/internal/plan"
	"github.com/rawmind0/arbor/test/examples"
)

// planFor builds a plan from an example use case in the given builder mode.
func planFor(t *testing.T, useCase string, mode builder.BuilderMode) *plan.BuildPlan {
	t.Helper()
	cfg, err := config.LoadConfig(examples.Config(useCase), nil)
	if err != nil {
		t.Fatalf("load %s: %v", useCase, err)
	}
	cfg.Config.Mode = string(mode)
	p, err := plan.NewGenerator(cfg, graph.NewGraph(cfg)).Generate()
	if err != nil {
		t.Fatalf("generate plan for %s: %v", useCase, err)
	}
	return p
}

// TestHierarchyExecutionWithFailures injects random failures (mock-error) and
// verifies the executor's failure semantics. (The happy-path execution for every
// example is covered by TestExamplesPipeline.)
func TestHierarchyExecutionWithFailures(t *testing.T) {
	p := planFor(t, examples.Hierarchy, builder.ModeMockError)

	executor := plan.NewExecutor(8, 30*time.Second)
	// mock-error surfaces failures as a returned error (and stops the affected
	// queue early); that error is expected precisely when some task fails.
	results, err := executor.Execute(context.Background(), p)

	failures := 0
	for _, r := range results {
		if r.Error != nil {
			failures++
		}
	}
	if failures > 0 && err == nil {
		t.Error("expected Execute to surface an error when tasks failed")
	}
	if failures == 0 && err != nil {
		t.Errorf("unexpected error from Execute with no failures: %v", err)
	}
	t.Logf("hierarchy mock-error run: %d/%d commands failed", failures, len(results))
}
