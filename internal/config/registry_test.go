package config

import "testing"

func TestRegistryTags(t *testing.T) {
	withURL := RegistryConfig{URL: "docker.io", Owner: "myorg"}
	if got := withURL.Tag("base", "1.0.0"); got != "docker.io/myorg/base:1.0.0" {
		t.Errorf("Tag() = %q", got)
	}

	// Empty URL falls back to "<owner>/<name>"
	noURL := RegistryConfig{Owner: "myorg"}
	if got := noURL.Tag("base", "1.0.0"); got != "myorg/base:1.0.0" {
		t.Errorf("Tag() without url = %q", got)
	}
}

func TestValidatePush(t *testing.T) {
	// Owner required
	cfg := &Config{Config: ConfigSection{Builder: "docker"}}
	if err := cfg.ValidatePush(); err == nil {
		t.Error("ValidatePush() with no owner should error")
	}

	cfg.Config.Registry.Owner = "myorg"
	if err := cfg.ValidatePush(); err != nil {
		t.Errorf("ValidatePush() with owner should pass: %v", err)
	}

	// kaniko + multi-arch is allowed (warns) but does not error
	cfg.Config.Builder = "kaniko"
	cfg.Config.Architectures = []string{"linux/amd64", "linux/arm64"}
	if err := cfg.ValidatePush(); err != nil {
		t.Errorf("ValidatePush() kaniko multi-arch should warn, not error: %v", err)
	}
}
