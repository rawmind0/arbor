// Package examples resolves paths to the example use-case configs in examples/.
// Those examples are the single source of config data for the whole test suite;
// tests load them with config.LoadConfig(examples.Config(examples.Hierarchy)).
//
// Paths are resolved relative to this source file (via runtime.Caller) so they
// work regardless of the calling test's working directory.
package examples

import (
	"os"
	"path/filepath"
	"runtime"
)

// Use-case names — one per folder under examples/.
const (
	Minimal     = "minimal"
	Hierarchy   = "hierarchy"
	MultiArch   = "multiarch"
	MultiSource = "multisource"
)

// Root returns the absolute path to the repository root. Command tests that run
// generate/build (which resolve dockerfiles.source/destination relative to the
// working directory) chdir here so the example configs' relative paths resolve.
func Root() string {
	_, self, _, _ := runtime.Caller(0) // <repo>/test/examples/examples.go
	return filepath.Join(filepath.Dir(self), "..", "..")
}

// dir returns the absolute path to the repository's examples/ directory.
func dir() string {
	return filepath.Join(Root(), "examples")
}

// All returns every example use-case name — each subfolder of examples/ that has
// a config.yaml — discovered dynamically, mirroring the Makefile's wildcard. Use
// it to iterate all examples instead of hardcoding the list.
func All() []string {
	entries, err := os.ReadDir(dir())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir(), e.Name(), "config.yaml")); err == nil {
			names = append(names, e.Name())
		}
	}
	return names
}

// Config returns the path to a use case's main config.yaml.
func Config(useCase string) string {
	return filepath.Join(dir(), useCase, "config.yaml")
}

// File returns the path to a named file within a use case (e.g. an extra
// --sources file like "databases.yaml").
func File(useCase, name string) string {
	return filepath.Join(dir(), useCase, name)
}
