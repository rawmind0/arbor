package builder

import (
	"strings"
	"testing"
)

// TestMockOutputParsing tests the mock output parsing functions
func TestMockOutputParsing(t *testing.T) {
	t.Run("ExtractRealCommand", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "basic command",
				input:    "[DRY-RUN] Would execute: {docker build .}",
				expected: "docker build .",
			},
			{
				name:     "command with multiple parts",
				input:    "[DRY-RUN] Would execute: {docker build -t test:v1 --no-cache .} && sleep 2",
				expected: "docker build -t test:v1 --no-cache .",
			},
			{
				name:     "command with error suffix",
				input:    "[DRY-RUN] Would execute: {podman build -f Dockerfile .} && echo '[DRY-RUN] ERROR: Failed' >&2",
				expected: "podman build -f Dockerfile .",
			},
			{
				name:     "no command found",
				input:    "Regular output without mock command",
				expected: "",
			},
			{
				name:     "empty braces",
				input:    "[DRY-RUN] Would execute: {}",
				expected: "",
			},
			{
				name:     "nested braces",
				input:    "[DRY-RUN] Would execute: {docker build --build-arg JSON={\"key\":\"value\"} .}",
				expected: "docker build --build-arg JSON={\"key\":\"value\"} .",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := ExtractRealCommand(tc.input)
				if result != tc.expected {
					t.Errorf("ExtractRealCommand(%q) = %q, want %q", tc.input, result, tc.expected)
				}
			})
		}
	})

	t.Run("ExtractRealCommandArgs", func(t *testing.T) {
		testCases := []struct {
			name         string
			input        string
			expectedLen  int
			expectedArgs []string
		}{
			{
				name:         "docker build command",
				input:        "[DRY-RUN] Would execute: {docker build -t test:v1 .}",
				expectedLen:  5,
				expectedArgs: []string{"docker", "build", "-t", "test:v1", "."},
			},
			{
				name:         "command with equals in args",
				input:        "[DRY-RUN] Would execute: {docker build --build-arg VERSION=1.0.0 .}",
				expectedLen:  5,
				expectedArgs: []string{"docker", "build", "--build-arg", "VERSION=1.0.0", "."},
			},
			{
				name:         "empty command",
				input:        "No command here",
				expectedLen:  0,
				expectedArgs: nil,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := ExtractRealCommandArgs(tc.input)
				if len(result) != tc.expectedLen {
					t.Errorf("ExtractRealCommandArgs(%q) returned %d args, want %d", tc.input, len(result), tc.expectedLen)
				}
				if tc.expectedArgs != nil {
					for i, arg := range tc.expectedArgs {
						if i >= len(result) || result[i] != arg {
							t.Errorf("Arg[%d] = %q, want %q", i, result[i], arg)
						}
					}
				}
			})
		}
	})

	t.Run("IsMockCommand", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    string
			expected bool
		}{
			{
				name:     "valid mock command",
				input:    "[DRY-RUN] Would execute: {docker build .}",
				expected: true,
			},
			{
				name:     "mock command in larger output",
				input:    "Starting build\n[DRY-RUN] Would execute: {docker build .}\nCompleted",
				expected: true,
			},
			{
				name:     "dry-run without command",
				input:    "[DRY-RUN] Starting simulation",
				expected: false,
			},
			{
				name:     "regular output",
				input:    "Building docker image",
				expected: false,
			},
			{
				name:     "command without dry-run prefix",
				input:    "Would execute: {docker build .}",
				expected: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := IsMockCommand(tc.input)
				if result != tc.expected {
					t.Errorf("IsMockCommand(%q) = %v, want %v", tc.input, result, tc.expected)
				}
			})
		}
	})

	t.Run("GetMockCommandStatus", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "success status",
				input:    "[DRY-RUN] Would execute: {docker build .} && echo '[DRY-RUN] SUCCESS'",
				expected: "SUCCESS",
			},
			{
				name:     "error status",
				input:    "[DRY-RUN] Would execute: {docker build .} && echo '[DRY-RUN] ERROR: Build failed'",
				expected: "ERROR",
			},
			{
				name:     "no status",
				input:    "[DRY-RUN] Would execute: {docker build .}",
				expected: "",
			},
			{
				name:     "success in output",
				input:    "Build output\n[DRY-RUN] SUCCESS\nMore output",
				expected: "SUCCESS",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := GetMockCommandStatus(tc.input)
				if result != tc.expected {
					t.Errorf("GetMockCommandStatus(%q) = %q, want %q", tc.input, result, tc.expected)
				}
			})
		}
	})

	t.Run("GetMockErrorMessage", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name:     "error with message",
				input:    "[DRY-RUN] ERROR: Build failed due to missing dependency",
				expected: "Build failed due to missing dependency",
			},
			{
				name:     "error in longer output",
				input:    "Starting build\n[DRY-RUN] ERROR: Permission denied\nBuild aborted",
				expected: "Permission denied",
			},
			{
				name:     "no error",
				input:    "[DRY-RUN] SUCCESS",
				expected: "",
			},
			{
				name:     "error marker without message",
				input:    "[DRY-RUN] ERROR:",
				expected: "",
			},
			{
				name:     "error with whitespace",
				input:    "[DRY-RUN] ERROR:   Out of memory   ",
				expected: "Out of memory",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := GetMockErrorMessage(tc.input)
				if result != tc.expected {
					t.Errorf("GetMockErrorMessage(%q) = %q, want %q", tc.input, result, tc.expected)
				}
			})
		}
	})
}

// TestMockCommandRegex tests the regex pattern for extracting commands
func TestMockCommandRegex(t *testing.T) {
	// Test that the regex is compiled and available
	if mockCommandRegex == nil {
		t.Fatal("mockCommandRegex is not initialized")
	}

	testCases := []struct {
		name    string
		input   string
		matches bool
		extract string
	}{
		{
			name:    "simple command",
			input:   "{docker build .}",
			matches: true,
			extract: "docker build .",
		},
		{
			name:    "command with special chars",
			input:   "{docker build --build-arg KEY=\"value with spaces\" .}",
			matches: true,
			extract: "docker build --build-arg KEY=\"value with spaces\" .",
		},
		{
			name:    "multiple braces",
			input:   "{first} {second}",
			matches: true,
			extract: "first", // Regex should match the first one
		},
		{
			name:    "nested braces",
			input:   "{docker build --arg {nested} .}",
			matches: true,
			extract: "docker build --arg {nested} .",
		},
		{
			name:    "no braces",
			input:   "docker build .",
			matches: false,
			extract: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matches := mockCommandRegex.MatchString(tc.input)
			if matches != tc.matches {
				t.Errorf("Regex match = %v, want %v for input %q", matches, tc.matches, tc.input)
			}

			if tc.matches {
				result := mockCommandRegex.FindStringSubmatch(tc.input)
				if len(result) > 1 && result[1] != tc.extract {
					t.Errorf("Regex extracted %q, want %q", result[1], tc.extract)
				}
			}
		})
	}
}

// TestMockBuilderIntegration tests the full integration of mock builder with parsing
func TestMockBuilderIntegration(t *testing.T) {
	// Create a mock builder for docker
	mockBuilder, err := GetBuilder(BuilderDocker, ModeMock)
	if err != nil {
		t.Fatalf("Failed to get mock builder: %v", err)
	}

	opts := BuildOptions{
		ImageTag:       "test:v1.0.0",
		DockerfilePath: "Dockerfile",
		BuildContext:   ".",
		Platforms:      []string{"linux/amd64"},
	}

	// Generate command
	cmd := mockBuilder.BuildSteps(opts)[0]

	// Verify it's a shell command
	if cmd[0] != "sh" || cmd[1] != "-c" {
		t.Fatal("Expected shell command wrapper")
	}

	shellCmd := cmd[2]

	// Test all parsing functions work together
	if !IsMockCommand(shellCmd) {
		t.Error("Should be recognized as mock command")
	}

	realCmd := ExtractRealCommand(shellCmd)
	if !strings.HasPrefix(realCmd, "docker build") || strings.Contains(realCmd, "buildx") {
		t.Errorf("Expected single-arch docker build command (no buildx), got: %s", realCmd)
	}

	args := ExtractRealCommandArgs(shellCmd)
	if len(args) == 0 || args[0] != "docker" {
		t.Error("Failed to extract command args")
	}

	status := GetMockCommandStatus(shellCmd)
	if status != "SUCCESS" {
		t.Errorf("Expected SUCCESS status, got: %s", status)
	}

	// Test with mock-error builder
	mockErrorBuilder, err := GetBuilder(BuilderDocker, ModeMockError)
	if err != nil {
		t.Fatalf("Failed to get mock-error builder: %v", err)
	}

	// Run multiple times to get both success and error
	gotSuccess := false
	gotError := false
	for i := 0; i < 20 && (!gotSuccess || !gotError); i++ {
		cmd := mockErrorBuilder.BuildSteps(opts)[0]
		shellCmd := cmd[2]

		status := GetMockCommandStatus(shellCmd)
		switch status {
		case "SUCCESS":
			gotSuccess = true
		case "ERROR":
			gotError = true
			// Should have error message
			errMsg := GetMockErrorMessage(shellCmd)
			if errMsg == "" {
				t.Error("Error status should have error message")
			}
		}

		// Should always have the real command
		realCmd := ExtractRealCommand(shellCmd)
		if realCmd == "" {
			t.Error("Should always extract real command")
		}
	}

	if !gotSuccess || !gotError {
		t.Error("Mock-error builder should produce both successes and errors")
	}
}
