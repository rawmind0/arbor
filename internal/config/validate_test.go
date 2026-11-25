package config_test

import (
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/examples"
)

// A config built in code gets, from Validate, exactly what loading a file
// gives: defaults, the main source, and every derived image field.
func TestValidate_HandBuiltConfig(t *testing.T) {
	cfg := &config.Config{
		Config: config.ConfigSection{Architectures: []string{"linux/arm64"}},
		Images: map[string]*config.ImageConfig{
			"base": {Version: "1.0.0", BaseImage: "alpine:3.18",
				Images: map[string]*config.ImageConfig{
					"app": {Version: "1.0.0", Dockerfile: "custom.Dockerfile"},
				}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	if cfg.Config.Builder != "docker" || cfg.Config.Workers != 2 || cfg.Config.Dockerfiles != "src" ||
		cfg.Config.Registry.Owner != "library" || cfg.Config.VersionIncrement != "patch" {
		t.Errorf("defaults not applied: %+v", cfg.Config)
	}
	if ids := cfg.SourceIDs(); len(ids) != 1 || ids[0] != config.MainSource {
		t.Fatalf("sources = %v, want [main]", ids)
	}
	base := cfg.Sources[config.MainSource].Images["base"]
	app := base.Images["app"]
	checks := []struct{ what, got, want string }{
		{"base name", base.Name, "base"},
		{"base parent", base.Parent, ""},
		{"base dockerfile", base.Dockerfile, "Dockerfile.base"},
		{"base architectures", strings.Join(base.Architectures, ","), "linux/arm64"},
		{"app name", app.Name, "app"},
		{"app parent", app.Parent, "base"},
		{"app dockerfile", app.Dockerfile, "custom.Dockerfile"},
		{"app architectures", strings.Join(app.Architectures, ","), "linux/arm64"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}

	// Validate is safe to run again.
	if err := cfg.Validate(); err != nil || len(cfg.SourceIDs()) != 1 {
		t.Errorf("second Validate: err=%v sources=%v", err, cfg.SourceIDs())
	}
}

// Every example passes the Dockerfile and push checks validate/plan/build run.
func TestValidateBuild_Examples(t *testing.T) {
	for _, uc := range examples.All() {
		t.Run(uc, func(t *testing.T) {
			if err := examples.Load(t, uc).ValidateBuild(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Every broken image, in any source, is reported with its name and path.
func TestValidateBuild_ReportsEveryBrokenDockerfile(t *testing.T) {
	cfg := examples.Files(t, map[string]string{
		"config.yaml": "images:\n  base:\n    version: 1.0.0\n    base_image: alpine:3.18\n" +
			"    images:\n      app:\n        version: 1.0.0\n",
		"services.yaml":       "images:\n  api:\n    version: 1.0.0\n    base_image: alpine:3.18\n",
		"src/Dockerfile.base": "ARG BASE=alpine:3.18\nFROM ${BASE}\n",
		"src/Dockerfile.app":  "FROM alpine:3.18\n", // hardcoded FROM in a child
		// Dockerfile.api is missing
	})
	err := cfg.ValidateBuild()
	if err == nil {
		t.Fatal("ValidateBuild() = nil, want errors")
	}
	for _, want := range []string{
		"invalid Dockerfiles",
		`image "app" (src/Dockerfile.app): first instruction must be ` + "`ARG BASE`",
		`image "api" (src/Dockerfile.api): `,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), `image "base"`) {
		t.Errorf("valid image reported:\n%v", err)
	}
}
