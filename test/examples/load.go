package examples

import (
	"path/filepath"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/testutil"
)

// Load loads an example exactly the way the commands do — its config.yaml plus
// every other *.yaml in its folder as --sources (like `make example-*`) — from
// the repository root, which it makes the working directory for the rest of the
// test (the configs' dockerfiles paths are repo-relative).
func Load(t testing.TB, useCase string) *config.Config {
	t.Helper()
	t.Chdir(Root())
	return LoadRepo(t, useCase)
}

// LoadFrom loads configPath plus the --sources glob patterns, relative to the
// working directory, the way the commands do.
func LoadFrom(t testing.TB, configPath string, sources ...string) *config.Config {
	t.Helper()
	cfg, err := config.LoadConfig(configPath, nil)
	if err != nil {
		t.Fatalf("load %s: %v", configPath, err)
	}
	if len(sources) > 0 {
		if err := cfg.LoadAdditionalImages(configPath, sources); err != nil {
			t.Fatalf("load sources %v: %v", sources, err)
		}
	}
	return cfg
}

// Files writes files (see Write) and loads "config.yaml" plus every other
// *.yaml there as --sources. For small custom cases: the config goes through
// the same loading as a real one.
func Files(t testing.TB, files map[string]string) *config.Config {
	t.Helper()
	Write(t, files)
	return LoadFrom(t, "config.yaml", "*.yaml")
}

// Write writes files (path relative to a new temp dir → content) without
// loading them, for tests where loading itself is under test (errors, the
// CLI). The temp dir becomes the working directory for the rest of the test;
// Write returns it.
func Write(t testing.TB, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		testutil.WriteFile(t, filepath.Join(dir, path), content)
	}
	t.Chdir(dir)
	return dir
}

// Dir returns the absolute path to a use case's folder.
func Dir(useCase string) string {
	return filepath.Join(dir(), useCase)
}

// Repo creates a temp git repository holding the use cases (at
// examples/<useCase>, see CopyToRepo) committed on "master", with a "feature"
// branch checked out on top: the way a pull request looks. The repository is
// the working directory for the rest of the test; load the copies with
// LoadRepo.
func Repo(t testing.TB, useCases ...string) *testutil.Repo {
	t.Helper()
	r := testutil.NewRepo(t, "master")
	for _, uc := range useCases {
		CopyToRepo(r, uc)
	}
	r.Commit("add examples")
	r.Git("checkout", "-q", "-b", "feature")
	return r
}

// LoadRepo loads a use case copied into the working directory's repository
// (see Repo) like Load does: its config.yaml plus every other *.yaml in its
// folder as --sources.
func LoadRepo(t testing.TB, useCase string) *config.Config {
	t.Helper()
	return LoadFrom(t, filepath.Join("examples", useCase, "config.yaml"),
		filepath.Join("examples", useCase, "*.yaml"))
}

// Dockerfile returns the repo-relative path of a use case's image Dockerfile
// (Dockerfile.<image> in its src/), as copied by Repo.
func Dockerfile(useCase, image string) string {
	return filepath.Join("examples", useCase, "src", "Dockerfile."+image)
}

// CopyToRepo copies a use case into repo at the same relative path it has in
// this repository (examples/<useCase>), so its repo-relative dockerfiles paths
// keep working with the repo as working directory. It does not commit.
func CopyToRepo(repo *testutil.Repo, useCase string) {
	repo.CopyDir(Dir(useCase), filepath.Join("examples", useCase))
}
