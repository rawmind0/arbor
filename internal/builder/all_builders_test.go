package builder

import (
	"strings"
	"testing"
)

// TestAllBuildersWithMockMode tests all builders using mock mode for command generation
func TestAllBuildersWithMockMode(t *testing.T) {
	builders := []struct {
		name        string
		builderName string
	}{
		{"Docker", BuilderDocker},
		{"Podman", BuilderPodman},
		{"Buildah", BuilderBuildah},
		{"Kaniko", BuilderKaniko},
	}

	opts := BuildOptions{
		ImageTag:       "myapp:v1.2.3",
		DockerfilePath: "Dockerfile.myapp",
		BuildContext:   ".",
		Platforms:      []string{"linux/amd64"},
		BuildArgs: map[string]string{
			"VERSION":    "1.2.3",
			"BASE_IMAGE": "alpine:3.18",
		},
		Labels: map[string]string{
			"version":    "1.2.3",
			"maintainer": "test@example.com",
			"app":        "myapp",
		},
		ExtraArgs: []string{"--no-cache"},
	}

	for _, tc := range builders {
		t.Run(tc.name+" with mock mode", func(t *testing.T) {
			// Get builder with mock mode
			builder, err := GetBuilder(tc.builderName, ModeMock)
			if err != nil {
				t.Fatalf("Failed to get %s builder with mock mode: %v", tc.name, err)
			}

			// Generate build command (first step)
			cmd := builder.BuildSteps(opts)[0]

			// Verify it's a mock command
			if cmd[0] != "sh" || cmd[1] != "-c" {
				t.Errorf("Expected mock command to start with 'sh -c', got %v", cmd[:2])
			}

			// Extract real command
			realCmd := ExtractRealCommand(cmd[2])
			if realCmd == "" {
				t.Errorf("Failed to extract real command from mock output")
			}

			// Verify builder-specific command structure
			switch tc.builderName {
			case BuilderDocker:
				// Single-arch uses a plain `docker build` (loads locally), not buildx.
				if !strings.HasPrefix(realCmd, "docker build") || strings.Contains(realCmd, "buildx") {
					t.Errorf("Expected single-arch docker build (no buildx), got: %s", realCmd)
				}
				if !strings.Contains(realCmd, "--platform linux/amd64") || !strings.Contains(realCmd, "-f Dockerfile.myapp") || !strings.Contains(realCmd, "-t myapp:v1.2.3") {
					t.Errorf("Missing platform/-f/-t: %s", realCmd)
				}

			case BuilderPodman:
				// Single-arch uses a plain `podman build -t`, not a manifest.
				if !strings.HasPrefix(realCmd, "podman build") {
					t.Errorf("Expected podman build, got: %s", realCmd)
				}
				if !strings.Contains(realCmd, "--platform linux/amd64") || !strings.Contains(realCmd, "-t myapp:v1.2.3") {
					t.Errorf("Missing platform/-t: %s", realCmd)
				}

			case BuilderBuildah:
				if !strings.HasPrefix(realCmd, "buildah build") {
					t.Errorf("Expected buildah build, got: %s", realCmd)
				}
				if !strings.Contains(realCmd, "--platform linux/amd64") {
					t.Error("Missing --platform flag")
				}

			case BuilderKaniko:
				if !strings.HasPrefix(realCmd, "/kaniko/executor") {
					t.Errorf("Expected kaniko command to start with '/kaniko/executor', got: %s", realCmd)
				}
				// Kaniko uses different flag names
				if !strings.Contains(realCmd, "--dockerfile") {
					t.Error("Missing --dockerfile flag")
				}
				if !strings.Contains(realCmd, "--destination") {
					t.Error("Missing --destination flag")
				}
				if !strings.Contains(realCmd, "--context") {
					t.Error("Missing --context flag")
				}
			}

			// Common checks for all builders
			if !strings.Contains(realCmd, "VERSION=1.2.3") {
				t.Error("Missing VERSION build arg")
			}
			if !strings.Contains(realCmd, "BASE_IMAGE=alpine:3.18") {
				t.Error("Missing BASE_IMAGE build arg")
			}

			// Check mock output format
			if !IsMockCommand(cmd[2]) {
				t.Error("Output should be recognized as mock command")
			}

			// Check for success marker
			if !strings.Contains(cmd[2], MockSuccessMarker) {
				t.Error("Mock command should contain success marker")
			}
		})
	}
}

// TestAllBuildersWithMockErrorMode tests error injection for all builders
func TestAllBuildersWithMockErrorMode(t *testing.T) {
	builders := []string{BuilderDocker, BuilderPodman, BuilderBuildah, BuilderKaniko}

	opts := BuildOptions{
		ImageTag:       "test:v1.0.0",
		DockerfilePath: "Dockerfile",
		BuildContext:   ".",
	}

	for _, builderName := range builders {
		t.Run(builderName+" with mock-error mode", func(t *testing.T) {
			builder, err := GetBuilder(builderName, ModeMockError)
			if err != nil {
				t.Fatalf("Failed to get %s builder with mock-error mode: %v", builderName, err)
			}

			// Run multiple times to test error injection
			successCount := 0
			errorCount := 0
			iterations := 20

			for i := 0; i < iterations; i++ {
				cmd := builder.BuildSteps(opts)[0]

				// Should still be a mock command
				if cmd[0] != "sh" || cmd[1] != "-c" {
					t.Errorf("Expected mock command structure")
				}

				// Check if it's success or error
				status := GetMockCommandStatus(cmd[2])
				switch status {
				case "SUCCESS":
					successCount++
				case "ERROR":
					errorCount++
					// Verify error message exists
					errMsg := GetMockErrorMessage(cmd[2])
					if errMsg == "" {
						t.Error("Error command should have error message")
					}
				default:
					t.Errorf("Unknown status: %s", status)
				}

				// Should always contain the real command
				realCmd := ExtractRealCommand(cmd[2])
				if realCmd == "" {
					t.Error("Failed to extract real command from mock output")
				}
			}

			// With 50% error rate, we expect some of each
			if successCount == 0 || errorCount == 0 {
				t.Logf("Warning: Got %d successes and %d errors (statistically unlikely but possible)", successCount, errorCount)
			}
		})
	}
}

// TestBuilderCommandValidation tests that generated commands are valid
func TestBuilderCommandValidation(t *testing.T) {
	testCases := []struct {
		name    string
		builder string
		opts    BuildOptions
		checks  []string // Expected strings in the command
	}{
		{
			name:    "Docker with all options",
			builder: BuilderDocker,
			opts: BuildOptions{
				ImageTag:       "app:v1.0.0",
				DockerfilePath: "docker/Dockerfile.app",
				BuildContext:   ".",
				Platforms:      []string{"linux/arm64"},
				BuildArgs: map[string]string{
					"ARG1": "value1",
					"ARG2": "value2",
				},
				Labels: map[string]string{
					"label1": "value1",
					"label2": "value2",
				},
				ExtraArgs: []string{"--squash", "--no-cache"},
			},
			checks: []string{
				"docker build",
				"--platform linux/arm64",
				"-f docker/Dockerfile.app",
				"-t app:v1.0.0",
				"--build-arg ARG1=value1",
				"--build-arg ARG2=value2",
				"--label label1=value1",
				"--label label2=value2",
				"--squash",
				"--no-cache",
			},
		},
		{
			name:    "Buildah with minimal options",
			builder: BuilderBuildah,
			opts: BuildOptions{
				ImageTag:       "minimal:latest",
				DockerfilePath: "Dockerfile",
				BuildContext:   ".",
			},
			checks: []string{
				"buildah build",
				"-f Dockerfile",
				"-t minimal:latest",
			},
		},
		{
			name:    "Kaniko with context",
			builder: BuilderKaniko,
			opts: BuildOptions{
				ImageTag:       "registry.example.com/app:v2.0.0",
				DockerfilePath: "Dockerfile.prod",
				BuildContext:   "dir://workspace",
				BuildArgs: map[string]string{
					"ENV": "production",
				},
			},
			checks: []string{
				"/kaniko/executor",
				"--dockerfile Dockerfile.prod",
				"--destination registry.example.com/app:v2.0.0",
				"--context dir://workspace",
				"--build-arg ENV=production",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder, err := GetBuilder(tc.builder, ModeMock)
			if err != nil {
				t.Fatalf("Failed to get builder: %v", err)
			}

			cmd := builder.BuildSteps(tc.opts)[0]
			realCmd := ExtractRealCommand(cmd[2])

			for _, check := range tc.checks {
				if !strings.Contains(realCmd, check) {
					t.Errorf("Command missing expected string '%s'. Full command: %s", check, realCmd)
				}
			}
		})
	}
}

// TestExtractRealCommandWithDifferentFormats tests command extraction with various formats
func TestExtractRealCommandWithDifferentFormats(t *testing.T) {
	testCases := []struct {
		name     string
		output   string
		expected string
	}{
		{
			name:     "Simple docker build",
			output:   "[DRY-RUN] Would execute: {docker build -t test:v1 .}",
			expected: "docker build -t test:v1 .",
		},
		{
			name:     "Complex command with multiple args",
			output:   "[DRY-RUN] Would execute: {docker build --platform linux/amd64 -f Dockerfile -t app:v1.0.0 --build-arg VERSION=1.0.0 .} && sleep 3",
			expected: "docker build --platform linux/amd64 -f Dockerfile -t app:v1.0.0 --build-arg VERSION=1.0.0 .",
		},
		{
			name:     "Buildah command",
			output:   "[DRY-RUN] Would execute: {buildah bud -t test:latest .}",
			expected: "buildah bud -t test:latest .",
		},
		{
			name:     "Kaniko command",
			output:   "[DRY-RUN] Would execute: {/kaniko/executor --dockerfile Dockerfile --destination test:v1 --context .}",
			expected: "/kaniko/executor --dockerfile Dockerfile --destination test:v1 --context .",
		},
		{
			name:     "Command with special characters",
			output:   "[DRY-RUN] Would execute: {docker build --label key=\"value with spaces\" .}",
			expected: "docker build --label key=\"value with spaces\" .",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractRealCommand(tc.output)
			if result != tc.expected {
				t.Errorf("ExtractRealCommand() = %q, want %q", result, tc.expected)
			}
		})
	}
}
