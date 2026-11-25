package builder

import (
	"fmt"
	"os/exec"
	"strings"
)

// Kaniko implements Builder for kaniko
type Kaniko struct{}

// NewKaniko creates a new kaniko builder
func NewKaniko() *Kaniko {
	return &Kaniko{}
}

// BuildSteps builds a single platform and pushes via --destination. Kaniko is
// single-arch (no buildx/manifest); only the first platform is used.
func (k *Kaniko) BuildSteps(opts BuildOptions) [][]string {
	cmd := NewCommandBuilder("/kaniko/executor", "")
	cmd.AddParamValue("--dockerfile", opts.DockerfilePath)
	cmd.AddParamValue("--context", opts.BuildContext)
	cmd.AddParamValue("--destination", opts.ImageTag)
	if len(opts.Platforms) > 0 {
		cmd.AddParamValue("--custom-platform", opts.Platforms[0])
	}
	cmd.AddMap("--build-arg", opts.BuildArgs)
	cmd.AddMap("--label", opts.Labels)
	cmd.AddExtra(opts.ExtraArgs)
	return [][]string{cmd.Build()}
}

// BuildMultiSteps falls back to the single-arch build: kaniko cannot create a
// multi-arch manifest (validation already warns on multi-arch + kaniko), so it
// builds and pushes only the first platform.
func (k *Kaniko) BuildMultiSteps(opts BuildOptions) [][]string {
	return k.BuildSteps(opts)
}

// LoginCommand is a stub: kaniko authenticates via /kaniko/.docker/config.json.
func (k *Kaniko) LoginCommand(url string) []string {
	return []string{"echo", "Kaniko uses /kaniko/.docker/config.json for registry auth"}
}

// IsAvailable checks if kaniko executor is available
func (k *Kaniko) IsAvailable() bool {
	// Kaniko is typically run in a container, check for executor
	cmd := exec.Command("/kaniko/executor", "--version")
	return cmd.Run() == nil
}

// Validate checks if kaniko is properly configured
func (k *Kaniko) Validate() error {
	if !k.IsAvailable() {
		return fmt.Errorf("kaniko executor is not available at /kaniko/executor")
	}
	return nil
}

// Name returns the builder name
func (k *Kaniko) Name() string {
	return "kaniko"
}

// Version returns the kaniko version
func (k *Kaniko) Version() (string, error) {
	cmd := exec.Command("/kaniko/executor", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
