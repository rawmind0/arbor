package builder

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

const (
	// MockCommandPattern is the regex to extract command from mock output
	// Updated to handle nested braces properly
	MockCommandPattern = `\{([^{}]*(?:\{[^{}]*\}[^{}]*)*)\}`
	// MockDryRunPrefix identifies mock command output
	MockDryRunPrefix = "[DRY-RUN]"
	// MockSuccessMarker identifies successful mock execution
	MockSuccessMarker = "[DRY-RUN] SUCCESS"
	// MockErrorMarker identifies failed mock execution
	MockErrorMarker = "[DRY-RUN] ERROR:"
)

// Compile regex once at package level for performance
var mockCommandRegex = regexp.MustCompile(MockCommandPattern)

// MockBuilder is a mock builder for dry-run testing
type MockBuilder struct {
	ErrorRate   float32 // Probability of error (0.0 to 1.0)
	BuilderName string  // Builder name (mock or mock-error)
	RealBuilder Builder // The real builder instance
}

// NewMockBuilder creates a new mock builder instance (always succeeds)
func NewMockBuilder() *MockBuilder {
	// Default to docker if called directly (for legacy compatibility)
	realBuilder, _ := GetBuilder(BuilderDocker, ModeNormal)
	return &MockBuilder{
		ErrorRate:   0,
		BuilderName: BuilderMock,
		RealBuilder: realBuilder,
	}
}

// NewMockBuilderWithErrors creates a mock builder that fails ~50% of the time
func NewMockBuilderWithErrors() *MockBuilder {
	// Default to docker if called directly (for legacy compatibility)
	realBuilder, _ := GetBuilder(BuilderDocker, ModeNormal)
	return &MockBuilder{
		ErrorRate:   0.5,
		BuilderName: BuilderMockError,
		RealBuilder: realBuilder,
	}
}

// wrapCommand handles getting real command and adding timeout/error simulation
func (m *MockBuilder) wrapCommand(realCmd []string, operation string) []string {
	cmdString := strings.Join(realCmd, " ")

	// Simulate work with a short random delay between 0 and 1000 milliseconds
	// (fractional seconds; accepted by both BSD and GNU sleep)
	durationSec := float64(rand.Intn(1001)) / 1000.0

	// Determine if this task should fail based on error rate
	shouldFail := rand.Float32() < m.ErrorRate

	if shouldFail {
		errorType := []string{
			"Dockerfile not found",
			"Base image pull failed",
			"Build step failed",
			"Out of memory",
			"Permission denied",
		}
		errorMsg := errorType[rand.Intn(len(errorType))]

		return []string{
			"sh", "-c",
			fmt.Sprintf("echo '%s Would execute: {%s}' && sleep %.3f && echo '%s %s' >&2 && exit 1",
				MockDryRunPrefix, cmdString, durationSec, MockErrorMarker, errorMsg),
		}
	}

	// Success with curly brace delimited command
	return []string{
		"sh", "-c",
		fmt.Sprintf("echo '%s Would execute: {%s}' && sleep %.3f && echo '%s'",
			MockDryRunPrefix, cmdString, durationSec, MockSuccessMarker),
	}
}

// wrapSteps wraps each real build step for dry-run.
func (m *MockBuilder) wrapSteps(realSteps [][]string) [][]string {
	wrapped := make([][]string, 0, len(realSteps))
	for _, step := range realSteps {
		wrapped = append(wrapped, m.wrapCommand(step, "build"))
	}
	return wrapped
}

// BuildSteps wraps each real single-arch build step for dry-run.
func (m *MockBuilder) BuildSteps(opts BuildOptions) [][]string {
	return m.wrapSteps(m.RealBuilder.BuildSteps(opts))
}

// BuildMultiSteps wraps each real multi-arch build step for dry-run.
func (m *MockBuilder) BuildMultiSteps(opts BuildOptions) [][]string {
	return m.wrapSteps(m.RealBuilder.BuildMultiSteps(opts))
}

// LoginCommand wraps the real login command for dry-run.
func (m *MockBuilder) LoginCommand(url string) []string {
	return m.wrapCommand(m.RealBuilder.LoginCommand(url), "login")
}

// IsAvailable returns the real builder availability
func (m *MockBuilder) IsAvailable() bool {
	return m.RealBuilder.IsAvailable()
}

// Validate calls real builder validate
func (m *MockBuilder) Validate() error {
	return nil
}

// Name returns the mock builder name with real builder info
func (m *MockBuilder) Name() string {
	return fmt.Sprintf("mock[%s]", m.RealBuilder.Name())
}

// Version returns the real version with mock indicator
func (m *MockBuilder) Version() (string, error) {
	realVersion, err := m.RealBuilder.Version()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("mock[%s]", realVersion), nil
}

// Helper functions for parsing mock output

// ExtractRealCommand extracts the real command from mock output using regex
func ExtractRealCommand(output string) string {
	matches := mockCommandRegex.FindStringSubmatch(output)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// ExtractRealCommandArgs extracts the real command as []string (split arguments)
func ExtractRealCommandArgs(output string) []string {
	cmd := ExtractRealCommand(output)
	if cmd == "" {
		return nil
	}
	return strings.Fields(cmd)
}

// IsMockCommand checks if output contains a mock command
func IsMockCommand(output string) bool {
	return strings.Contains(output, MockDryRunPrefix) && mockCommandRegex.MatchString(output)
}

// GetMockCommandStatus returns "SUCCESS", "ERROR", or "" from mock output
func GetMockCommandStatus(output string) string {
	if strings.Contains(output, MockSuccessMarker) {
		return "SUCCESS"
	}
	if strings.Contains(output, MockErrorMarker) {
		return "ERROR"
	}
	return ""
}

// GetMockErrorMessage extracts error message from mock output
func GetMockErrorMessage(output string) string {
	if idx := strings.Index(output, MockErrorMarker); idx >= 0 {
		msg := output[idx+len(MockErrorMarker):]
		// Take only the first line of the error message
		if nlIdx := strings.Index(msg, "\n"); nlIdx >= 0 {
			msg = msg[:nlIdx]
		}
		return strings.TrimSpace(msg)
	}
	return ""
}
