package builder

import (
	"fmt"
	"os/exec"
	"strings"
)

// DockerCompatible implements Builder for docker and podman
type DockerCompatible struct {
	executable string // docker or podman
}

// NewDockerCompatible creates a new docker-compatible builder
func NewDockerCompatible(executable string) *DockerCompatible {
	return &DockerCompatible{
		executable: executable,
	}
}

// addCommon appends the parameters shared by every docker/podman build variant:
// the Dockerfile, build args, labels, extra args, and the build context (added
// last, as a positional argument). The caller adds platform/tag/push flags.
func (d *DockerCompatible) addCommon(build *CommandBuilder, opts BuildOptions) []string {
	build.AddParamValue("-f", opts.DockerfilePath)
	build.AddMap("--build-arg", opts.BuildArgs)
	build.AddMap("--label", opts.Labels)
	build.AddExtra(opts.ExtraArgs)
	build.AddParam(opts.BuildContext)
	return build.Build()
}

// BuildSteps builds a single platform and loads it into the local image store
// so a child image's `FROM name:version` resolves locally. A plain `docker`/
// `podman build` (no buildx) loads natively; when opts.Push is set the image is
// pushed as a separate step.
func (d *DockerCompatible) BuildSteps(opts BuildOptions) [][]string {
	build := NewCommandBuilder(d.executable, "build")
	if len(opts.Platforms) > 0 {
		build.AddParamValue("--platform", opts.Platforms[0])
	}
	build.AddParamValue("-t", opts.ImageTag)
	steps := [][]string{d.addCommon(build, opts)}
	if opts.Push {
		steps = append(steps,
			NewCommandBuilder(d.executable, "push").AddParam(opts.ImageTag).Build())
	}
	return steps
}

// BuildMultiSteps builds every requested platform and pushes the multi-arch
// manifest (a multi-platform result cannot be loaded locally). docker uses
// buildx (manifest is automatic with --push); podman builds into a local
// manifest list and pushes it as a separate step.
func (d *DockerCompatible) BuildMultiSteps(opts BuildOptions) [][]string {
	platforms := strings.Join(opts.Platforms, ",")

	if d.executable == BuilderPodman {
		build := NewCommandBuilder("podman", "build")
		build.AddParamValue("--platform", platforms)
		build.AddParamValue("--manifest", opts.ImageTag)
		steps := [][]string{d.addCommon(build, opts)}
		if opts.Push {
			steps = append(steps,
				NewCommandBuilder("podman", "manifest").AddParam("push").AddParam(opts.ImageTag).Build())
		}
		return steps
	}

	build := NewCommandBuilder("docker", "buildx").AddParam("build")
	build.AddParamValue("--platform", platforms)
	build.AddParamValue("-t", opts.ImageTag)
	build.AddFlag("--push", opts.Push)
	return [][]string{d.addCommon(build, opts)}
}

// LoginCommand authenticates to the registry using credentials from the
// REGISTRY_USERNAME / REGISTRY_PASSWORD environment variables (password via
// stdin so it never appears on argv).
func (d *DockerCompatible) LoginCommand(url string) []string {
	return []string{
		"sh", "-c",
		fmt.Sprintf(`echo "$REGISTRY_PASSWORD" | %s login -u "$REGISTRY_USERNAME" --password-stdin %s`,
			d.executable, url),
	}
}

// IsAvailable checks if the builder executable is available
func (d *DockerCompatible) IsAvailable() bool {
	cmd := exec.Command(d.executable, "--version")
	return cmd.Run() == nil
}

// Validate checks if the builder is properly configured
func (d *DockerCompatible) Validate() error {
	if !d.IsAvailable() {
		return fmt.Errorf("%s is not available in PATH", d.executable)
	}

	// Check if daemon is accessible (for docker)
	if d.executable == "docker" {
		cmd := exec.Command(d.executable, "info")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker daemon is not accessible")
		}
	}

	return nil
}

// Name returns the builder name
func (d *DockerCompatible) Name() string {
	return d.executable
}

// Version returns the builder version
func (d *DockerCompatible) Version() (string, error) {
	cmd := exec.Command(d.executable, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Simple version extraction
	return strings.TrimSpace(string(output)), nil
}
