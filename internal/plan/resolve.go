package plan

import (
	"fmt"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/version"
)

// Resolve runs the steps plan and build share, on a loaded config:
//
//  1. ValidateBuild: every Dockerfile follows the BASE contract, push is usable
//  2. the version check, unless skipped: no changed image is missing a bump
//     (this never rewrites the config; bumps are applied by `validate --fix`)
//  3. with changed-only, mark the changed images
//  4. generate the plan, one queue per source
//
// The returned bool is true when there is nothing to build (changed-only with
// no changes).
func Resolve(cfg *config.Config) (*BuildPlan, bool, error) {
	if err := cfg.ValidateBuild(); err != nil {
		return nil, false, err
	}

	if !cfg.Config.SkipVersionCheck {
		if err := version.NewValidator(cfg).Check(); err != nil {
			return nil, false, err
		}
	}

	generator := NewGenerator(cfg)
	if cfg.Config.ChangedOnly {
		if err := config.NewChangeDetector(cfg, ".").DetectChanges(cfg.Config.BaseRef); err != nil {
			return nil, false, fmt.Errorf("failed to detect changes: %w", err)
		}
		buildPlan, err := generator.GenerateChanged()
		if err != nil {
			return nil, false, fmt.Errorf("failed to generate plan for changed images: %w", err)
		}
		return buildPlan, buildPlan.GetTotalCommands() == 0, nil
	}

	buildPlan, err := generator.Generate()
	if err != nil {
		return nil, false, fmt.Errorf("failed to generate plan: %w", err)
	}
	return buildPlan, false, nil
}
