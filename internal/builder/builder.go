package builder

import (
	"fmt"
	"strings"
)

// BuilderMode represents the execution mode for builders
type BuilderMode string

const (
	// Builder modes
	ModeNormal    BuilderMode = "normal"     // Use real builder (default)
	ModeMock      BuilderMode = "mock"       // Use mock wrapper (no errors)
	ModeMockError BuilderMode = "mock-error" // Use mock wrapper with errors
)

// Builder name constants
const (
	BuilderDocker    = "docker"
	BuilderPodman    = "podman"
	BuilderBuildah   = "buildah"
	BuilderKaniko    = "kaniko"
	BuilderMock      = "mock"
	BuilderMockError = "mock-error"
)

// GetBuilder returns a builder instance based on the name and mode
func GetBuilder(name string, mode BuilderMode) (Builder, error) {
	// Normalize the builder name
	name = strings.ToLower(strings.TrimSpace(name))

	// Default mode to normal if empty
	if mode == "" {
		mode = ModeNormal
	}

	// Create the real builder
	var realBuilder Builder

	switch name {
	case BuilderDocker:
		realBuilder = NewDockerCompatible(BuilderDocker)
	case BuilderPodman:
		realBuilder = NewDockerCompatible(BuilderPodman)
	case BuilderBuildah:
		realBuilder = NewBuildah()
	case BuilderKaniko:
		realBuilder = NewKaniko()
	case BuilderMock:
		// Legacy: direct mock request gets mock wrapper around docker
		realBuilder = NewDockerCompatible(BuilderDocker)
		mode = ModeMock // Force mock mode
	case BuilderMockError:
		// Legacy: direct mock-error request gets mock wrapper around docker
		realBuilder = NewDockerCompatible(BuilderDocker)
		mode = ModeMockError // Force mock mode
	default:
		return nil, fmt.Errorf("unsupported builder: %s", name)
	}

	// Wrap in mock if requested
	switch mode {
	case ModeMock:
		return &MockBuilder{
			ErrorRate:   0,
			BuilderName: BuilderMock,
			RealBuilder: realBuilder,
		}, nil
	case ModeMockError:
		return &MockBuilder{
			ErrorRate:   0.5,
			BuilderName: BuilderMockError,
			RealBuilder: realBuilder,
		}, nil
	case ModeNormal:
		return realBuilder, nil
	default:
		// Unknown mode, default to normal
		return realBuilder, nil
	}
}

// IsBuilderSupported checks if a builder is supported
func IsBuilderSupported(name string) bool {
	_, err := GetBuilder(name, ModeNormal)
	return err == nil
}

// GetSupportedBuilders returns a list of supported builders
func GetSupportedBuilders() []string {
	return []string{BuilderDocker, BuilderPodman, BuilderBuildah, BuilderKaniko, BuilderMock, BuilderMockError}
}
