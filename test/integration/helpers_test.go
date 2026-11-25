package integration

import (
	"testing"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/plan"
	"github.com/rawmind0/arbor/test/examples"
)

// allExamples is every example use case, discovered dynamically from the
// examples/ directory (same as the Makefile) — no hardcoded list to maintain.
var allExamples = examples.All()

// resolve loads an example like the commands do and runs the same flow plan and
// build run (plan.Resolve), with the builder in mode. The version check is
// skipped here because it depends on this repository's git history; it is
// covered on example copies in temp repos (internal/version).
func resolve(t *testing.T, useCase string, mode builder.BuilderMode) (*config.Config, *plan.BuildPlan) {
	t.Helper()
	cfg := examples.Load(t, useCase)
	cfg.Config.Mode = string(mode)
	cfg.Config.SkipVersionCheck = true
	p, _, err := plan.Resolve(cfg)
	if err != nil {
		t.Fatalf("resolve %s: %v", useCase, err)
	}
	return cfg, p
}
