package integration

import (
	"context"
	"testing"
	"time"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/plan"
)

// TestHierarchyExecutionWithFailures injects random failures (mock-error) and
// verifies the executor's failure semantics. (The happy-path execution for every
// example is covered by TestExamplesPipeline.)
func TestHierarchyExecutionWithFailures(t *testing.T) {
	_, p := resolve(t, "hierarchy", builder.ModeMockError)

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

// TestMultiSourceParallelExecution runs the multisource plan (one queue per
// source) with the mock builder: every command of every queue runs and passes.
func TestMultiSourceParallelExecution(t *testing.T) {
	_, p := resolve(t, "multisource", builder.ModeMock)

	results, err := plan.NewExecutor(4, 10*time.Second).Execute(context.Background(), p)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}
	if len(results) != p.GetTotalCommands() {
		t.Errorf("Expected %d results, got %d", p.GetTotalCommands(), len(results))
	}
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Command %s failed: %v", result.Command.ImageTag, result.Error)
		}
	}
}
