package plan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/test/examples"
)

func TestNewFormatter(t *testing.T) {
	plan := &BuildPlan{}
	var buf bytes.Buffer

	formatter := NewFormatter(plan, &buf)
	if formatter == nil {
		t.Fatal("NewFormatter returned nil")
	}
	if formatter.plan != plan {
		t.Error("Formatter plan mismatch")
	}
	if formatter.writer != &buf {
		t.Error("Formatter writer mismatch")
	}
}

func TestFormatter_Format(t *testing.T) {
	// Use hierarchical scenario for comprehensive testing
	plan := planFromExample(t, examples.Hierarchy, builder.ModeMock)

	tests := []struct {
		name        string
		format      string
		wantErr     bool
		checkOutput func(t *testing.T, output string)
	}{
		{
			name:    "human format",
			format:  "human",
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				// Check for key sections
				if !strings.Contains(output, "BUILD PLAN") {
					t.Error("Missing BUILD PLAN header")
				}
				if !strings.Contains(output, "METADATA") {
					t.Error("Missing METADATA section")
				}
				if !strings.Contains(output, "EXECUTION QUEUES") {
					t.Error("Missing EXECUTION QUEUES section")
				}
				if !strings.Contains(output, "BUILD COMMANDS") {
					t.Error("Missing BUILD COMMANDS section")
				}
			},
		},
		{
			name:    "text format (alias for human)",
			format:  "text",
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "BUILD PLAN") {
					t.Error("Missing BUILD PLAN header")
				}
			},
		},
		{
			name:    "json format",
			format:  "json",
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				var p BuildPlan
				if err := json.Unmarshal([]byte(output), &p); err != nil {
					t.Errorf("Invalid JSON output: %v", err)
				}
				if len(p.Queues) == 0 {
					t.Error("No queues in JSON output")
				}
			},
		},
		{
			name:    "yaml format",
			format:  "yaml",
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				var p BuildPlan
				if err := yaml.Unmarshal([]byte(output), &p); err != nil {
					t.Errorf("Invalid YAML output: %v", err)
				}
				if len(p.Queues) == 0 {
					t.Error("No queues in YAML output")
				}
			},
		},
		{
			name:    "yml format (alias for yaml)",
			format:  "yml",
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				var p BuildPlan
				if err := yaml.Unmarshal([]byte(output), &p); err != nil {
					t.Errorf("Invalid YAML output: %v", err)
				}
			},
		},
		{
			name:    "commands format",
			format:  "commands",
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "#!/bin/bash") {
					t.Error("Missing shebang")
				}
				if !strings.Contains(output, "docker build") && !strings.Contains(output, "podman build") {
					t.Error("Missing build commands")
				}
				if !strings.Contains(output, "# Batch") {
					t.Error("Missing batch markers")
				}
			},
		},
		{
			name:    "graph format",
			format:  "graph",
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "digraph BuildPlan") {
					t.Error("Missing DOT graph header")
				}
				if !strings.Contains(output, "subgraph") {
					t.Error("Missing subgraph clusters")
				}
			},
		},
		{
			name:    "invalid format",
			format:  "invalid",
			wantErr: true,
		},
		{
			name:    "empty format (defaults to human)",
			format:  "",
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "BUILD PLAN") {
					t.Error("Default format should be human")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			formatter := NewFormatter(plan, &buf)

			err := formatter.Format(tt.format)
			if (err != nil) != tt.wantErr {
				t.Errorf("Format() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkOutput != nil {
				tt.checkOutput(t, buf.String())
			}
		})
	}
}

func TestFormatter_formatHuman(t *testing.T) {
	// Use the hierarchy scenario for comprehensive human format testing
	plan := planFromExample(t, examples.Hierarchy, builder.ModeMock)

	// Enrich plan with additional metadata for testing
	plan.Metadata.Generated = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	plan.Metadata.ChangedOnly = true
	plan.Metadata.TaskTimeout = 5 * time.Minute
	plan.Metadata.GitInfo = &GitInfo{
		Branch:  "main",
		IsClean: false,
	}

	var buf bytes.Buffer
	formatter := NewFormatter(plan, &buf)

	if err := formatter.formatHuman(); err != nil {
		t.Fatalf("formatHuman() failed: %v", err)
	}

	output := buf.String()

	// Check all expected sections
	expectedStrings := []string{
		"BUILD PLAN",
		"METADATA",
		"Version:",
		"Generated:",
		"Config:",
		"Builder:",
		"Workers:",
		"Total Images:",
		"Git Info:",
		"Branch:",
		"Clean:",
		"EXECUTION QUEUES",
		"Queue: main",
		"Batch",
		"BUILD COMMANDS",
		"Image:",
		"Arch:",
		"Dockerfile:",
		// Labels might not be shown if there are more than 3
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Missing expected content: %s", expected)
		}
	}
}

func TestFormatter_formatCommands(t *testing.T) {
	// Use the multisource scenario which has multiple images
	plan := planFromExample(t, examples.MultiSource, builder.ModeMock)

	// Override builder to podman for testing variety
	plan.Metadata.Builder = "podman"

	var buf bytes.Buffer
	formatter := NewFormatter(plan, &buf)

	if err := formatter.formatCommands(); err != nil {
		t.Fatalf("formatCommands() failed: %v", err)
	}

	output := buf.String()
	lines := strings.Split(output, "\n")

	// Check structure
	if !strings.HasPrefix(lines[0], "#!/bin/bash") {
		t.Error("Missing shebang")
	}

	if !strings.Contains(output, "# Batch") {
		t.Error("Missing batch markers")
	}

	// Should have build commands
	if !strings.Contains(output, "build") {
		t.Error("Missing build commands")
	}
}

func TestFormatter_formatGraph(t *testing.T) {
	// Use hierarchical scenario for clear dependency visualization
	plan := planFromExample(t, examples.Hierarchy, builder.ModeMock)

	var buf bytes.Buffer
	formatter := NewFormatter(plan, &buf)

	if err := formatter.formatGraph(); err != nil {
		t.Fatalf("formatGraph() failed: %v", err)
	}

	output := buf.String()

	// Check DOT format structure
	if !strings.Contains(output, "digraph BuildPlan") {
		t.Error("Missing DOT graph header")
	}

	// Check for nodes - the hierarchy is ubuntu -> {nodejs, golang} -> {frontend, backend, worker}
	expectedImages := []string{"ubuntu", "nodejs", "frontend"}
	for _, img := range expectedImages {
		if !strings.Contains(output, img) {
			t.Errorf("Missing expected image in graph: %s", img)
		}
	}
}

func TestFormatter_WithAllScenarios(t *testing.T) {
	scenarios := []string{
		examples.Minimal,
		examples.Hierarchy,
		examples.MultiArch,
		examples.MultiSource,
	}

	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			// Generate plan from the example use case
			plan := planFromExample(t, scenario, builder.ModeMock)

			// Test all formats with each scenario
			formats := []string{"human", "json", "yaml", "commands", "graph"}
			for _, format := range formats {
				t.Run(format, func(t *testing.T) {
					var buf bytes.Buffer
					formatter := NewFormatter(plan, &buf)

					if err := formatter.Format(format); err != nil {
						t.Errorf("Format(%s) failed for scenario %s: %v", format, scenario, err)
					}

					if buf.Len() == 0 {
						t.Errorf("Format(%s) produced no output for scenario %s", format, scenario)
					}
				})
			}
		})
	}
}

func TestFormatter_EdgeCases(t *testing.T) {
	t.Run("empty plan", func(t *testing.T) {
		// Generate a plan and clear its queues to create empty plan
		plan := planFromExample(t, examples.Minimal, builder.ModeMock)
		plan.Queues = []Queue{}
		plan.Metadata.TotalImages = 0

		var buf bytes.Buffer
		formatter := NewFormatter(plan, &buf)

		// Should handle empty plan gracefully
		err := formatter.Format("human")
		if err != nil {
			t.Errorf("Format should handle empty plan: %v", err)
		}
	})

	t.Run("plan with minimal data", func(t *testing.T) {
		// Use the minimal scenario
		plan := planFromExample(t, examples.Minimal, builder.ModeMock)

		// Simplify to single command for minimal test
		if len(plan.Queues) > 0 && len(plan.Queues[0].Commands) > 0 {
			firstCmd := plan.Queues[0].Commands[0]
			plan.Queues = []Queue{
				{
					ID:       "main",
					Batches:  []Batch{{Tasks: []string{firstCmd.ImageTag}}},
					Commands: []BuildCommand{firstCmd},
				},
			}
			plan.Metadata.TotalImages = 1
		}

		formats := []string{"human", "json", "yaml", "commands", "graph"}
		for _, format := range formats {
			var buf bytes.Buffer
			formatter := NewFormatter(plan, &buf)

			if err := formatter.Format(format); err != nil {
				t.Errorf("Format(%s) failed for minimal plan: %v", format, err)
			}
		}
	})
}

// The commands script runs every step of every image (the push after a
// single-arch build too) and quotes what the shell would split: the login's
// `sh -c` script.
func TestFormatter_CommandsRunEveryStep(t *testing.T) {
	plan := planFromExample(t, examples.MultiArch, builder.ModeNormal)
	for _, format := range []string{"commands", "human"} {
		var buf bytes.Buffer
		if err := NewFormatter(plan, &buf).Format(format); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		for _, want := range []string{
			`sh -c 'echo "$REGISTRY_PASSWORD" | docker login -u "$REGISTRY_USERNAME" --password-stdin registry.example.com'`,
			"docker push registry.example.com/myorg/agent:1.0.0",
			"docker push registry.example.com/myorg/agent-app:1.0.0",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s output misses %q:\n%s", format, want, out)
			}
		}
	}
}

func TestShellJoin(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"docker", "build", "-t", "a/b:1.0", "--label", "build.date=2026-01-01T00:00:00+01:00", "."},
			"docker build -t a/b:1.0 --label build.date=2026-01-01T00:00:00+01:00 ."},
		{[]string{"sh", "-c", "echo $X | y"}, `sh -c 'echo $X | y'`},
		{[]string{"echo", "it's"}, `echo 'it'\''s'`},
		{[]string{"echo", ""}, `echo ''`},
	}
	for _, tt := range tests {
		if got := shellJoin(tt.args); got != tt.want {
			t.Errorf("shellJoin(%q) = %s, want %s", tt.args, got, tt.want)
		}
	}
}
