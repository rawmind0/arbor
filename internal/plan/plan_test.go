package plan

import (
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/test/examples"
	"github.com/rawmind0/arbor/test/testutil"
)

func TestBuildPlan_Validate(t *testing.T) {
	tests := []struct {
		name      string
		useCase   string
		setupPlan func(*BuildPlan) // optional mutation to force an edge case
		wantErr   bool
	}{
		{name: "valid plan with single image", useCase: examples.Minimal},
		{name: "valid plan with hierarchy", useCase: examples.Hierarchy},
		{
			name:      "empty plan forces error",
			useCase:   examples.Minimal,
			wantErr:   true,
			setupPlan: func(p *BuildPlan) { p.Queues = []Queue{} },
		},
		{
			name:    "plan with empty queue",
			useCase: examples.Minimal,
			wantErr: true,
			setupPlan: func(p *BuildPlan) {
				if len(p.Queues) > 0 {
					p.Queues[0].Commands = []BuildCommand{}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := planFromExample(t, tt.useCase, builder.ModeMock)
			if tt.setupPlan != nil {
				tt.setupPlan(plan)
			}
			if err := plan.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildPlan_HelperMethods(t *testing.T) {
	// hierarchy = ubuntu, tools, nodejs, nodejs22, golang, api, frontend, backend, worker
	plan := planFromExample(t, examples.Hierarchy, builder.ModeMock)

	t.Run("GetTotalCommands", func(t *testing.T) {
		if total := plan.GetTotalCommands(); total != 9 {
			t.Errorf("GetTotalCommands() = %d, want 9", total)
		}
	})

	t.Run("GetTotalBatches", func(t *testing.T) {
		if total := plan.GetTotalBatches(); total < 1 {
			t.Errorf("GetTotalBatches() = %d, want at least 1", total)
		}
	})

	t.Run("GetCommandByImageTag", func(t *testing.T) {
		for _, queue := range plan.Queues {
			for _, cmd := range queue.Commands {
				found := plan.GetCommandByImageTag(cmd.ImageTag)
				if found == nil {
					t.Errorf("GetCommandByImageTag(%s) returned nil", cmd.ImageTag)
				} else if found.ImageTag != cmd.ImageTag {
					t.Errorf("GetCommandByImageTag(%s) returned wrong command", cmd.ImageTag)
				}
			}
		}
		if cmd := plan.GetCommandByImageTag("nonexistent:1.0.0"); cmd != nil {
			t.Error("GetCommandByImageTag should return nil for non-existent tag")
		}
	})
}

func TestLoadFromFile(t *testing.T) {
	plan := planFromExample(t, examples.Minimal, builder.ModeMock)

	planFile := filepath.Join(t.TempDir(), "test.yaml")
	data, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatalf("Failed to marshal plan: %v", err)
	}
	testutil.WriteFile(t, planFile, string(data))

	loaded, err := LoadFromFile(planFile)
	if err != nil {
		t.Fatalf("Failed to load plan: %v", err)
	}
	if loaded.Metadata.Version != plan.Metadata.Version {
		t.Errorf("Version mismatch: got %s, want %s", loaded.Metadata.Version, plan.Metadata.Version)
	}
	if len(loaded.Queues) != len(plan.Queues) {
		t.Errorf("Queue count mismatch: got %d, want %d", len(loaded.Queues), len(plan.Queues))
	}
	if loaded.GetTotalCommands() != plan.GetTotalCommands() {
		t.Errorf("Command count mismatch: got %d, want %d", loaded.GetTotalCommands(), plan.GetTotalCommands())
	}
}

func TestPlanEdgeCases(t *testing.T) {
	t.Run("LoadFromFile with non-existent file", func(t *testing.T) {
		if _, err := LoadFromFile("/non/existent/file.yaml"); err == nil {
			t.Error("Expected error for non-existent file")
		}
	})

	t.Run("LoadFromFile with invalid YAML", func(t *testing.T) {
		badFile := filepath.Join(t.TempDir(), "bad.yaml")
		testutil.WriteFile(t, badFile, "not: valid: yaml: structure:")
		if _, err := LoadFromFile(badFile); err == nil {
			t.Error("Expected error for invalid YAML")
		}
	})

	t.Run("multi-arch does not multiply commands", func(t *testing.T) {
		// The multiarch use case is ubuntu -> {golang -> app, agent -> agent-app}
		// (5 images), mostly built for two architectures. Architecture is a
		// build flag, so each image is still exactly one command.
		plan := planFromExample(t, examples.MultiArch, builder.ModeMock)
		if plan.GetTotalCommands() != 5 {
			t.Errorf("Expected 5 commands (one per image), got %d", plan.GetTotalCommands())
		}
		if len(plan.Queues) != 1 {
			t.Errorf("Expected 1 queue (one per source), got %d", len(plan.Queues))
		}
	})
}

func TestBuildPlan_SaveToFileRoundTrip(t *testing.T) {
	// A plan produced by the real generator in mock mode (commands come from the
	// pipeline, not hand-written).
	plan := planFromExample(t, examples.Minimal, builder.ModeMock)

	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plan."+format)

			if err := plan.SaveToFile(path, format); err != nil {
				t.Fatalf("SaveToFile(%s) failed: %v", format, err)
			}

			loaded, err := LoadFromFile(path)
			if err != nil {
				t.Fatalf("LoadFromFile failed: %v", err)
			}
			if loaded.Metadata.Version != plan.Metadata.Version {
				t.Errorf("Version mismatch: got %s, want %s", loaded.Metadata.Version, plan.Metadata.Version)
			}
			if loaded.GetTotalCommands() != plan.GetTotalCommands() {
				t.Errorf("Command count mismatch: got %d, want %d", loaded.GetTotalCommands(), plan.GetTotalCommands())
			}
		})
	}
}
