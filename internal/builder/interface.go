package builder

import (
	"fmt"
	"maps"
	"slices"
)

// Builder defines the interface for container image builders
type Builder interface {
	// BuildSteps returns the command sequence to build a SINGLE platform and
	// load the result into the local image store (also pushing when opts.Push).
	// Used for single-arch images so a child's `FROM name:version` resolves
	// locally without a registry round-trip.
	BuildSteps(opts BuildOptions) [][]string

	// BuildMultiSteps returns the command sequence to build ALL the requested
	// platforms and push the multi-arch manifest. It is only valid when
	// opts.Push is set: a multi-platform result cannot be loaded into the local
	// image store, so it must live in a registry for the next hierarchy level to
	// consume it. Each builder encodes its own mechanism (docker buildx --push;
	// buildah/podman --manifest + manifest push).
	BuildMultiSteps(opts BuildOptions) [][]string

	// LoginCommand authenticates to the registry url, reading credentials from
	// the REGISTRY_USERNAME / REGISTRY_PASSWORD environment variables.
	LoginCommand(url string) []string

	// Validation
	IsAvailable() bool
	Validate() error

	// Info
	Name() string
	Version() (string, error)
}

// BuildOptions contains all options for a multi-platform image build.
type BuildOptions struct {
	ImageTag       string // the (manifest) tag, e.g. registry/owner/name:version
	DockerfilePath string
	BuildContext   string
	Platforms      []string // target platforms, e.g. ["linux/amd64", "linux/arm64"]
	Push           bool     // build and push the multi-arch manifest
	BuildArgs      map[string]string
	Labels         map[string]string
	ExtraArgs      []string // For additional parameters
}

// CommandBuilder helps construct commands with flexibility
type CommandBuilder struct {
	args []string
}

// NewCommandBuilder creates a new command builder
func NewCommandBuilder(executable, command string) *CommandBuilder {
	return &CommandBuilder{
		args: []string{executable, command},
	}
}

// AddParam adds a single parameter
func (c *CommandBuilder) AddParam(param string) *CommandBuilder {
	c.args = append(c.args, param)
	return c
}

// AddParamValue adds a parameter with its value
func (c *CommandBuilder) AddParamValue(param, value string) *CommandBuilder {
	if value != "" {
		c.args = append(c.args, param, value)
	}
	return c
}

// AddParamFormat adds a parameter with formatted value
func (c *CommandBuilder) AddParamFormat(param, format string, value interface{}) *CommandBuilder {
	c.args = append(c.args, param, fmt.Sprintf(format, value))
	return c
}

// AddFlag adds a flag if enabled
func (c *CommandBuilder) AddFlag(flag string, enabled bool) *CommandBuilder {
	if enabled {
		c.args = append(c.args, flag)
	}
	return c
}

// AddMap adds multiple key-value pairs with the same parameter, sorted by key
// so the same plan always yields the same command.
func (c *CommandBuilder) AddMap(param string, values map[string]string) *CommandBuilder {
	for _, k := range slices.Sorted(maps.Keys(values)) {
		c.args = append(c.args, param, fmt.Sprintf("%s=%s", k, values[k]))
	}
	return c
}

// AddExtra adds extra arguments
func (c *CommandBuilder) AddExtra(extra []string) *CommandBuilder {
	c.args = append(c.args, extra...)
	return c
}

// Build returns the constructed command
func (c *CommandBuilder) Build() []string {
	return c.args
}
