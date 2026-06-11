package plan

import (
	"context"
	"testing"
	"time"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/test/examples"
)

func TestExecutor_Execute(t *testing.T) {
	t.Run("successful execution with mock builder", func(t *testing.T) {
		plan := planFromExample(t, examples.Hierarchy, builder.ModeMock)

		executor := NewExecutor(2, 10*time.Second)
		results, err := executor.Execute(context.Background(), plan)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if len(results) != plan.GetTotalCommands() {
			t.Errorf("Expected %d results, got %d", plan.GetTotalCommands(), len(results))
		}
		// All should succeed with the mock builder.
		for _, result := range results {
			if result.Error != nil {
				t.Errorf("Unexpected error for %s: %v", result.Command.ImageTag, result.Error)
			}
		}
	})

	t.Run("execution with mock-error builder", func(t *testing.T) {
		plan := planFromExample(t, examples.Hierarchy, builder.ModeMockError)

		executor := NewExecutor(2, 10*time.Second)
		// mock-error surfaces failures as a returned error and stops the queue;
		// that error is expected here — we only inspect the per-result outcomes.
		results, _ := executor.Execute(context.Background(), plan)

		errorCount := 0
		for _, result := range results {
			if result.Error != nil {
				errorCount++
			}
		}
		t.Logf("Got %d errors out of %d commands with mock-error builder", errorCount, len(results))
	})

	t.Run("parallel execution within batch", func(t *testing.T) {
		// The hierarchy has sibling images that build in parallel within a batch.
		plan := planFromExample(t, examples.Hierarchy, builder.ModeMock)

		executor := NewExecutor(4, 10*time.Second)
		results, err := executor.Execute(context.Background(), plan)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if len(results) != plan.GetTotalCommands() {
			t.Errorf("Expected %d results, got %d", plan.GetTotalCommands(), len(results))
		}
	})
}

func TestExecutor_EmptyPlan(t *testing.T) {
	// Generate a plan and then clear its queues to simulate an empty plan.
	plan := planFromExample(t, examples.Minimal, builder.ModeMock)
	plan.Queues = []Queue{}
	plan.Metadata.TotalImages = 0

	executor := NewExecutor(2, 10*time.Second)
	if _, err := executor.Execute(context.Background(), plan); err == nil {
		t.Fatal("Expected error for empty plan, but got none")
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	// A multi-batch plan with a single worker runs long enough (mock builds
	// sleep) to be cancelled mid-flight.
	plan := planFromExample(t, examples.Hierarchy, builder.ModeMock)

	executor := NewExecutor(1, 10*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	if _, err := executor.Execute(ctx, plan); err == nil {
		t.Error("Expected context cancellation error")
	}
}

func TestExecutor_MultipleQueues(t *testing.T) {
	// Combine two single-source plans into one multi-queue plan.
	plan1 := planFromExample(t, examples.Hierarchy, builder.ModeMock)
	plan2 := planFromExample(t, examples.Minimal, builder.ModeMock)

	plan := &BuildPlan{
		Metadata: PlanMetadata{
			Version: "1.0.0",
			Builder: builder.BuilderDocker,
			Workers: 4,
		},
		Queues: []Queue{},
	}
	if len(plan1.Queues) > 0 {
		q := plan1.Queues[0]
		q.ID = "main"
		plan.Queues = append(plan.Queues, q)
		plan.Metadata.TotalImages += len(q.Commands)
	}
	if len(plan2.Queues) > 0 {
		q := plan2.Queues[0]
		q.ID = "secondary"
		plan.Queues = append(plan.Queues, q)
		plan.Metadata.TotalImages += len(q.Commands)
	}

	executor := NewExecutor(4, 10*time.Second)
	results, err := executor.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != plan.Metadata.TotalImages {
		t.Errorf("Expected %d results, got %d", plan.Metadata.TotalImages, len(results))
	}
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Unexpected error for %s: %v", result.Command.ImageTag, result.Error)
		}
	}
}
