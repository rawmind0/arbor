package integration

import (
	"path/filepath"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/plan"
	"github.com/rawmind0/arbor/test/examples"
	"github.com/rawmind0/arbor/test/testutil"
)

// TestMultiSourceLoading checks the multisource example loads as two sources,
// each with its own file and image tree.
func TestMultiSourceLoading(t *testing.T) {
	cfg := examples.Load(t, "multisource")

	want := map[string]struct {
		path string
		root string
	}{
		"main":     {"examples/multisource/config.yaml", "web-base"},
		"services": {"examples/multisource/services.yaml", "api-base"},
	}
	if got := cfg.SourceIDs(); len(got) != len(want) || got[0] != "main" {
		t.Fatalf("sources = %v, want main first then services", got)
	}
	for id, w := range want {
		src := cfg.Sources[id]
		if src.Path != w.path {
			t.Errorf("source %s path = %q, want %q", id, src.Path, w.path)
		}
		if len(src.Images) != 1 || src.Images[w.root] == nil {
			t.Errorf("source %s roots = %v, want only %s", id, keys(src.Images), w.root)
		}
	}
}

// writeAndLoad writes files into a temp dir and loads config.yaml with every
// *.yaml there as --sources, returning the loading error (unlike examples.Files).
func writeAndLoad(t *testing.T, files map[string]string) (*config.Config, error) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		testutil.WriteFile(t, filepath.Join(dir, name), content)
	}
	main := filepath.Join(dir, "config.yaml")
	cfg, err := config.LoadConfig(main, nil)
	if err != nil {
		return nil, err
	}
	return cfg, cfg.LoadAdditionalImages(main, []string{filepath.Join(dir, "*.yaml")})
}

// The --sources glob may match the config file itself; it is not loaded twice.
func TestSources_ConfigFileExcluded(t *testing.T) {
	cfg := examples.Files(t, map[string]string{
		"config.yaml":     "images:\n  main:\n    version: 1.0.0\n    base_image: ubuntu:22.04\n",
		"additional.yaml": "images:\n  extra:\n    version: 1.0.0\n    base_image: alpine:3.18\n",
	})
	if got := cfg.SourceIDs(); len(got) != 2 || got[0] != "main" || got[1] != "additional" {
		t.Errorf("sources = %v, want [main additional]", got)
	}
}

func TestSources_EmptyFileSkipped(t *testing.T) {
	cfg := examples.Files(t, map[string]string{
		"config.yaml": "images:\n  base:\n    version: 1.0.0\n    base_image: ubuntu:22.04\n",
		"empty.yaml":  "# This source has no images\nimages:\n",
	})
	if got := cfg.SourceIDs(); len(got) != 1 || got[0] != "main" {
		t.Errorf("sources = %v, want only main", got)
	}
}

func TestSources_InvalidFile(t *testing.T) {
	_, err := writeAndLoad(t, map[string]string{
		"config.yaml":  "images:\n  base:\n    version: 1.0.0\n",
		"invalid.yaml": "images:\n  broken:\n    version: [this is not valid\n",
	})
	if err == nil {
		t.Error("expected an error loading an invalid source file")
	}
}

// A source's images inherit architectures from their own parent: from the
// image's own list when set, else the parent's, else the global ones.
func TestSources_ArchitectureInheritance(t *testing.T) {
	cfg := examples.Files(t, map[string]string{
		"config.yaml": "config:\n  architectures: [linux/amd64]\n" +
			"images:\n  web:\n    version: 1.0.0\n    images:\n      site:\n        version: 1.0.0\n",
		"services.yaml": "images:\n" +
			"  api-base:\n    version: 1.0.0\n    architectures: [linux/arm64]\n" +
			"    images:\n      api:\n        version: 1.0.0\n" +
			"        images:\n          api-job:\n            version: 1.0.0\n" +
			"  tools:\n    version: 1.0.0\n",
	})
	want := map[string]string{
		"web": "linux/amd64", "site": "linux/amd64", // main: global
		"api-base": "linux/arm64", "api": "linux/arm64", "api-job": "linux/arm64", // own, inherited
		"tools": "linux/amd64", // source root without its own: global
	}
	for _, id := range cfg.SourceIDs() {
		walk(cfg.Sources[id].Images, func(img *config.ImageConfig) {
			if got := filepath.Join(img.Architectures...); got != want[img.Name] {
				t.Errorf("%s architectures = %v, want %s", img.Name, img.Architectures, want[img.Name])
			}
		})
	}
}

// The same image name may be used in two sources: each source plans its own.
func TestSources_SameImageNameInTwoSources(t *testing.T) {
	cfg := examples.Files(t, map[string]string{
		"config.yaml":        "images:\n  app:\n    version: 1.0.0\n    base_image: alpine:3.18\n",
		"other.yaml":         "images:\n  app:\n    version: 2.0.0\n    base_image: alpine:3.19\n",
		"src/Dockerfile.app": "ARG BASE\nFROM ${BASE}\n",
	})
	cfg.Config.SkipVersionCheck = true
	p, _, err := plan.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, q := range p.Queues {
		for _, c := range q.Commands {
			got[q.ID] = c.ImageTag + " BASE=" + c.BuildArgs["BASE"]
		}
	}
	want := map[string]string{
		"main":  "library/app:1.0.0 BASE=alpine:3.18",
		"other": "library/app:2.0.0 BASE=alpine:3.19",
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("queue %s = %q, want %q", id, got[id], w)
		}
	}
}

func walk(images map[string]*config.ImageConfig, fn func(*config.ImageConfig)) {
	for _, img := range images {
		fn(img)
		walk(img.Images, fn)
	}
}

func keys(images map[string]*config.ImageConfig) []string {
	var out []string
	for k := range images {
		out = append(out, k)
	}
	return out
}
