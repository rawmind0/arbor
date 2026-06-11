package config

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/rawmind0/arbor/internal/builder"
)

// Validate runs the configuration checks shared by plan and build: when push is
// on, the registry must be usable. It deliberately does NOT enforce the
// multi-arch-requires-push rule (see ValidateMultiArchPush) — that is a
// build-only concern, so `plan` and `generate` remain previews that succeed
// even for an unbuildable multi-arch/no-push config. Builder availability is
// likewise a separate, build-only concern (see ValidateBuilder).
func (c *Config) Validate() error {
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
// enforced only by the build command (plan/generate are previews).
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

// allImages returns every image across all sources (or the main images when no
// sources are registered), flattened across the hierarchy. Effective
// architectures are already resolved onto each image during load.
func (c *Config) allImages() []*ImageConfig {
	var out []*ImageConfig
	collect := func(images map[string]*ImageConfig) {
		var walk func(map[string]*ImageConfig)
		walk = func(m map[string]*ImageConfig) {
			for _, img := range m {
				out = append(out, img)
				if img.Images != nil {
					walk(img.Images)
				}
			}
		}
		walk(images)
	}

	if len(c.Sources) > 0 {
		for _, source := range c.Sources {
			collect(source.Images)
		}
		return out
	}
	collect(c.Images)
	return out
}
