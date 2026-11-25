package plan

import (
	"testing"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/test/examples"
)

// planFromExample loads an example like the commands do (with its --sources)
// and generates its full plan in the given builder mode (builder.ModeMock for
// dry-run execution in tests).
func planFromExample(t *testing.T, useCase string, mode builder.BuilderMode) *BuildPlan {
	t.Helper()
	cfg := examples.Load(t, useCase)
	cfg.Config.Mode = string(mode)
	p, err := NewGenerator(cfg).Generate()
	if err != nil {
		t.Fatalf("generate plan for %q: %v", useCase, err)
	}
	return p
}
