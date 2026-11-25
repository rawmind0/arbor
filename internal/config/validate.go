package config

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/rawmind0/arbor/internal/builder"
)

// ValidateBuild runs the buildability checks shared by validate, plan and build:
// every Dockerfile follows the BASE contract (see ValidateDockerfiles) and, when
// push is on, the registry must be usable. Unlike Validate it reads the
// Dockerfiles from disk. It deliberately does NOT enforce the
// multi-arch-requires-push rule (see ValidateMultiArchPush) — that is a
// build-only concern, so `plan` remains a preview that succeeds even for an
// unbuildable multi-arch/no-push config. Builder availability is likewise a
// separate, build-only concern (see ValidateBuilder).
func (c *Config) ValidateBuild() error {
	if err := c.ValidateDockerfiles(); err != nil {
		return fmt.Errorf("invalid Dockerfiles:\n%w", err)
	}
	if c.Config.Push {
		return c.ValidatePush()
	}
	return nil
}

// ValidateBuilder checks that the configured builder is supported and available.
// It honors the builder mode, so mock/dry-run modes use the no-op mock builder
// and never probe a real docker/podman daemon. Call this from commands that
// actually run the builder (e.g. build), after the mode has been set.
func (c *Config) ValidateBuilder() error {
	builderInstance, err := builder.GetBuilder(c.Config.Builder, builder.BuilderMode(c.Config.Mode))
	if err != nil {
		return fmt.Errorf("unsupported builder '%s': %w", c.Config.Builder, err)
	}
	return builderInstance.Validate()
}

// ValidatePush checks the config can push. owner is required (it defaults to
// "library" at load time, so this is a defensive guard). Multi-arch with kaniko
// is unsupported (single-arch only) — warn, don't fail.
func (c *Config) ValidatePush() error {
	if c.Config.Registry.Owner == "" {
		return fmt.Errorf("push requires config.registry.owner to be set")
	}
	if c.Config.Builder == builder.BuilderKaniko && len(c.Config.Architectures) > 1 {
		log.Warn().Msg("kaniko is single-arch: multi-arch manifests will not be created")
	}
	return nil
}

// ValidateMultiArchPush fails when an image targets more than one architecture
// but push is disabled. A multi-platform build produces a manifest list that
// cannot be loaded into the local image store, so the next hierarchy level
// cannot resolve its `FROM` locally — it must come from a registry. This is
// enforced only by the build command (plan is a preview).
func (c *Config) ValidateMultiArchPush() error {
	if c.Config.Push {
		return nil
	}
	for _, img := range c.allImages() {
		if len(img.Architectures) > 1 {
			return fmt.Errorf(
				"multi-arch image %q (%d architectures) requires push: enable config.push "+
					"(or pass --push) so children can resolve FROM from the registry, or "+
					"reduce it to a single architecture",
				img.Name, len(img.Architectures))
		}
	}
	return nil
}

// allImages returns every image across all sources, flattened across the
// hierarchy, in a stable order (sources as SourceIDs, parents before children).
func (c *Config) allImages() []*ImageConfig {
	var out []*ImageConfig
	for _, id := range c.SourceIDs() {
		walkImages(c.Sources[id].Images, func(img *ImageConfig) { out = append(out, img) })
	}
	return out
}
