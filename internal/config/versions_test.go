package config_test

import (
	"os"
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/examples"
	"github.com/rawmind0/arbor/test/testutil"
)

// UpdateVersions changes only the named images' version values: every other
// byte of the file (comments, alignment, other keys) stays the same.
func TestUpdateVersions_OnlyVersionValues(t *testing.T) {
	dir := t.TempDir()
	testutil.CopyDir(t, examples.Dir("hierarchy"), dir)
	path := dir + "/config.yaml"
	before := read(t, path)

	if err := config.UpdateVersions(path, map[string]string{"nodejs": "20.0.2", "worker": "1.0.2"}); err != nil {
		t.Fatal(err)
	}

	want := strings.Replace(before, "      nodejs:\n        version: 20.0.1", "      nodejs:\n        version: 20.0.2", 1)
	want = strings.Replace(want,
		"          worker:              # FROM golang\n            version: 1.0.1",
		"          worker:              # FROM golang\n            version: 1.0.2", 1)
	if got := read(t, path); got != want {
		t.Errorf("file after update:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateVersions_KeepsQuoting(t *testing.T) {
	path := t.TempDir() + "/services.yaml"
	testutil.WriteFile(t, path, "images:\n  a:\n    version: \"1.0.0\" # pinned\n  b:\n    version: '2.0.0'\n")
	if err := config.UpdateVersions(path, map[string]string{"a": "1.0.1", "b": "2.0.1"}); err != nil {
		t.Fatal(err)
	}
	want := "images:\n  a:\n    version: \"1.0.1\" # pinned\n  b:\n    version: '2.0.1'\n"
	if got := read(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateVersions_ImageNotInFile(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	testutil.WriteFile(t, path, "images:\n  a:\n    version: 1.0.0\n")
	err := config.UpdateVersions(path, map[string]string{"missing": "1.0.1"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("UpdateVersions() = %v, want an error naming the missing image", err)
	}
	if got := read(t, path); got != "images:\n  a:\n    version: 1.0.0\n" {
		t.Errorf("file changed on error:\n%s", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
