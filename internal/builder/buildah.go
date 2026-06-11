package builder

import (
	"fmt"
	"os/exec"
	"strings"
)

// Buildah implements Builder for buildah
type Buildah struct{}

// NewBuildah creates a new buildah builder
func NewBuildah() *Buildah {
	return &Buildah{}
}

// addCommon appends the parameters shared by every buildah build variant: the
// Dockerfile, build args, labels, extra args, and the build context (added
// last, as a positional argument). The caller adds platform/tag/manifest flags.
func (b *Buildah) addCommon(build *CommandBuilder, opts BuildOptions) []string {
	build.AddParamValue("-f", opts.DockerfilePath)
	build.AddMap("--build-arg", opts.BuildArgs)
	build.AddMap("--label", opts.Labels)
	build.AddExtra(opts.ExtraArgs)
	build.AddParam(opts.BuildContext)
	return build.Build()
}

// BuildSteps builds a single platform into a local image and, when pushing,
// pushes it as a separate step.
func (b *Buildah) BuildSteps(opts BuildOptions) [][]string {
	build := NewCommandBuilder("buildah", "build")
	if len(opts.Platforms) > 0 {
		build.AddParamValue("--platform", opts.Platforms[0])
	}
	build.AddParamValue("-t", opts.ImageTag)
	steps := [][]string{b.addCommon(build, opts)}
	if opts.Push {
		steps = append(steps,
			NewCommandBuilder("buildah", "push").AddParam(opts.ImageTag).Build())
	}
	return steps
}

// BuildMultiSteps builds all platforms into a local manifest list and, when
// pushing, pushes it as a separate step.
func (b *Buildah) BuildMultiSteps(opts BuildOptions) [][]string {
	build := NewCommandBuilder("buildah", "build")
	build.AddParamValue("--platform", strings.Join(opts.Platforms, ","))
	build.AddParamValue("--manifest", opts.ImageTag)
	steps := [][]string{b.addCommon(build, opts)}
	if opts.Push {
		steps = append(steps,
			NewCommandBuilder("buildah", "manifest").AddParam("push").AddParam("--all").AddParam(opts.ImageTag).Build())
	}
	return steps
}

// LoginCommand authenticates to the registry using REGISTRY_USERNAME /
// REGISTRY_PASSWORD (password via stdin).
func (b *Buildah) LoginCommand(url string) []string {
	return []string{
		"sh", "-c",
		fmt.Sprintf(`echo "$REGISTRY_PASSWORD" | buildah login -u "$REGISTRY_USERNAME" --password-stdin %s`, url),
	}
}

// IsAvailable checks if buildah is available
func (b *Buildah) IsAvailable() bool {
	cmd := exec.Command("buildah", "--version")
	return cmd.Run() == nil
}

// Validate checks if buildah is properly configured
func (b *Buildah) Validate() error {
	if !b.IsAvailable() {
		return fmt.Errorf("buildah is not available in PATH")
	}
	return nil
}

// Name returns the builder name
func (b *Buildah) Name() string {
	return "buildah"
}

// Version returns the buildah version
func (b *Buildah) Version() (string, error) {
	cmd := exec.Command("buildah", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
