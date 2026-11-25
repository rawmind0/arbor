package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/examples"
)

// Loading an example keeps the file's settings and completes every image:
// name, parent, default Dockerfile and inherited architectures.
func TestLoad_Example(t *testing.T) {
	cfg := examples.Load(t, examples.Hierarchy)

	if cfg.Config.Workers != 4 || cfg.Config.Registry.Owner != "acme" ||
		cfg.Config.Dockerfiles != "examples/hierarchy/src" || cfg.Config.Labels["project"] != "hierarchy-example" {
		t.Errorf("file settings not kept: %+v", cfg.Config)
	}
	if ids := cfg.SourceIDs(); len(ids) != 1 || ids[0] != config.MainSource {
		t.Fatalf("sources = %v, want [main]", ids)
	}
	frontend := cfg.Sources[config.MainSource].Images["ubuntu"].Images["nodejs"].Images["frontend"]
	checks := []struct{ what, got, want string }{
		{"name", frontend.Name, "frontend"},
		{"parent", frontend.Parent, "nodejs"},
		{"dockerfile", frontend.GetDockerfilePath(cfg.Config.Dockerfiles), "examples/hierarchy/src/frontend/Dockerfile"},
		{"architectures", strings.Join(frontend.Architectures, ","), "linux/amd64"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("frontend %s = %q, want %q", c.what, c.got, c.want)
		}
	}
}

// The Dockerfile is Dockerfile.<name> in the dockerfiles directory unless the
// image's dockerfile setting names another (a shared file, or one in a
// subfolder); the build context is the Dockerfile's folder.
func TestLoad_DockerfileAndContext(t *testing.T) {
	cfg := examples.Load(t, examples.Hierarchy)
	ubuntu := cfg.Sources[config.MainSource].Images["ubuntu"]
	for _, tt := range []struct {
		img                 *config.ImageConfig
		dockerfile, context string
	}{
		{ubuntu, "examples/hierarchy/src/Dockerfile.ubuntu", "examples/hierarchy/src"},                    // default
		{ubuntu.Images["nodejs22"], "examples/hierarchy/src/Dockerfile.nodejs", "examples/hierarchy/src"}, // shared
		{ubuntu.Images["api"], "examples/hierarchy/src/api/Dockerfile", "examples/hierarchy/src/api"},     // subfolder
	} {
		if got := tt.img.GetDockerfilePath(cfg.Config.Dockerfiles); got != tt.dockerfile {
			t.Errorf("%s dockerfile = %q, want %q", tt.img.Name, got, tt.dockerfile)
		}
		if got := tt.img.GetBuildContext(cfg.Config.Dockerfiles); got != tt.context {
			t.Errorf("%s build context = %q, want %q", tt.img.Name, got, tt.context)
		}
	}
}

// Overrides (what the commands build from their flags) are overlaid on the
// file: present keys win, the others keep the file's values.
func TestLoadConfig_Overrides(t *testing.T) {
	path := examples.Config(examples.MultiArch) // push: true, builder: docker
	cfg, err := config.LoadConfig(path, map[string]any{
		"config": map[string]any{"push": false, "mode": string(builder.ModeMock)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Config.Push || cfg.Config.Mode != string(builder.ModeMock) {
		t.Errorf("overrides not applied: push=%v mode=%q", cfg.Config.Push, cfg.Config.Mode)
	}
	if cfg.Config.Builder != "docker" || len(cfg.Config.Architectures) != 2 {
		t.Errorf("file values lost: builder=%q architectures=%v", cfg.Config.Builder, cfg.Config.Architectures)
	}

	cfg, err = config.LoadConfig(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Config.Push {
		t.Error("without overrides push should keep the file's true")
	}
}

func TestLoadConfig_Errors(t *testing.T) {
	examples.Write(t, map[string]string{
		"malformed.yaml":  "config:\n  builder: docker\n  invalid: [\n",
		"increment.yaml":  "config:\n  version_increment: major\nimages:\n  app:\n    version: 1.0.0\n",
		"unreadable.yaml": "images: {}\n",
	})
	if err := os.Chmod("unreadable.yaml", 0); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ file, wantErr string }{
		{"missing.yaml", "failed to read config file"},
		{"unreadable.yaml", "failed to read config file"},
		{"malformed.yaml", "failed to parse config"},
		{"increment.yaml", "invalid version_increment: major (must be 'patch' or 'minor')"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			if _, err := config.LoadConfig(tt.file, nil); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("LoadConfig(%s) = %v, want an error containing %q", tt.file, err, tt.wantErr)
			}
		})
	}
}

// An empty file is a valid config with nothing to build.
func TestLoadConfig_Empty(t *testing.T) {
	cfg := examples.Files(t, map[string]string{"config.yaml": ""})
	if ids := cfg.SourceIDs(); len(ids) != 0 {
		t.Errorf("sources = %v, want none", ids)
	}
}

// The builder check run by build: mock modes (--dry-run) never probe a real
// daemon; an unsupported builder is an error.
func TestValidateBuilder(t *testing.T) {
	for _, mode := range []builder.BuilderMode{builder.ModeMock, builder.ModeMockError} {
		cfg, err := config.LoadConfig(examples.Config(examples.Minimal),
			map[string]any{"config": map[string]any{"mode": string(mode)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.ValidateBuilder(); err != nil {
			t.Errorf("mode %s: ValidateBuilder() = %v, want nil", mode, err)
		}
	}

	cfg := examples.Files(t, map[string]string{"config.yaml": "config:\n  builder: nonexistent\n"})
	if err := cfg.ValidateBuilder(); err == nil {
		t.Error("unsupported builder: ValidateBuilder() = nil, want an error")
	}
}
