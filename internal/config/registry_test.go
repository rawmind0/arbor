package config_test

import (
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/examples"
)

func TestRegistryTags(t *testing.T) {
	withURL := config.RegistryConfig{URL: "docker.io", Owner: "myorg"}
	if got := withURL.Tag("base", "1.0.0"); got != "docker.io/myorg/base:1.0.0" {
		t.Errorf("Tag() = %q", got)
	}
	// Empty URL falls back to "<owner>/<name>"
	noURL := config.RegistryConfig{Owner: "myorg"}
	if got := noURL.Tag("base", "1.0.0"); got != "myorg/base:1.0.0" {
		t.Errorf("Tag() without url = %q", got)
	}
}

// The push checks: the multiarch example (push: true) passes; kaniko with
// several architectures only warns.
func TestValidatePush(t *testing.T) {
	if err := examples.Load(t, examples.MultiArch).ValidatePush(); err != nil {
		t.Errorf("multiarch example: ValidatePush() = %v", err)
	}
	cfg := examples.Files(t, map[string]string{
		"config.yaml": "config:\n  builder: kaniko\n  push: true\n  architectures: [linux/amd64, linux/arm64]\n" +
			"  registry:\n    owner: myorg\nimages:\n  app:\n    version: 1.0.0\n",
	})
	if err := cfg.ValidatePush(); err != nil {
		t.Errorf("kaniko multi-arch: ValidatePush() = %v, want only a warning", err)
	}
}
