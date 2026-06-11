package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/generate"
	"github.com/rawmind0/arbor/internal/graph"
	"github.com/rawmind0/arbor/internal/plan"
	"github.com/rawmind0/arbor/test/examples"
)

// TestExamplesGenerateMatchesCommitted regenerates every example's Dockerfiles in
// memory and checks them against the committed examples/<uc>/build/ files. It
// fails if generation drifts from what's checked in (run `make example-generate`
// to refresh), exercising the real template pipeline against the examples.
func TestExamplesGenerateMatchesCommitted(t *testing.T) {
	useRepoRoot(t) // template source paths in the configs are repo-relative
	for _, uc := range allExamples {
		t.Run(uc, func(t *testing.T) {
			cfg := loadExampleFull(t, uc)
			processor := generate.New(cfg)

			// Mirror `generate`: one graph per source, FROM resolved to the parent.
			for _, source := range cfg.Sources {
				src := &config.Config{Config: cfg.Config, Images: source.Images, IsGitRepo: cfg.IsGitRepo}
				g := graph.NewGraph(src)
				if err := g.Build(); err != nil {
					t.Fatalf("build graph: %v", err)
				}
				for _, node := range g.GetBuildOrder() {
					got, err := processor.GenerateDockerfile(node.Image, "", g.GetParentImageTag(node))
					if err != nil {
						t.Fatalf("generate %s: %v", node.Image.Name, err)
					}
					wantPath := examples.File(uc, filepath.Join("build", "Dockerfile."+node.Image.Name))
					want, err := os.ReadFile(wantPath)
					if err != nil {
						t.Fatalf("missing committed Dockerfile for %s (run `make example-generate`): %v", node.Image.Name, err)
					}
					if got != string(want) {
						t.Errorf("%s: generated Dockerfile differs from committed build/ — run `make example-generate`", node.Image.Name)
					}
				}
			}
		})
	}
}

// TestExamplesPipeline runs each example end to end — generate the plan, then
// execute it in mock mode — proving the full generate→plan→build pipeline works
// against the real example configs (including multi-source and the push path).
func TestExamplesPipeline(t *testing.T) {
	for _, uc := range allExamples {
		t.Run(uc, func(t *testing.T) {
			cfg := loadExampleFull(t, uc)
			cfg.Config.Mode = string(builder.ModeMock)

			p, err := plan.NewGenerator(cfg, graph.NewGraph(cfg)).Generate()
			if err != nil {
				t.Fatalf("generate plan: %v", err)
			}

			results, err := plan.NewExecutor(4, 30*time.Second).Execute(context.Background(), p)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if len(results) != p.GetTotalCommands() {
				t.Errorf("expected %d results, got %d", p.GetTotalCommands(), len(results))
			}
			for _, r := range results {
				if r.Error != nil {
					t.Errorf("unexpected failure for %s: %v", r.Command.ImageTag, r.Error)
				}
			}
		})
	}
}
