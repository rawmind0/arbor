package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/rawmind0/arbor/test/examples"
)

// The commands are tested the way a user runs them: the real command tree with
// real arguments (arbor), on copies of the examples in a temp git repository
// (examples.Repo). How plans are built is asserted in detail in
// test/integration; these tests cover what the CLI adds: flags, output, exit.

// arbor runs the CLI with args and returns everything it printed (stdout,
// where the commands print, and cobra's stderr) and its error.
func arbor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags(rootCmd)

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	captured := make(chan string)
	go func() {
		data, _ := io.ReadAll(r)
		captured <- string(data)
	}()

	cobraOut := new(bytes.Buffer)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(cobraOut)
	rootCmd.SetErr(cobraOut)
	runErr := rootCmd.Execute()

	_ = w.Close()
	os.Stdout = stdout
	out := <-captured + cobraOut.String()
	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	return out, runErr
}

// resetFlags restores every flag of the command tree to its default (and
// "not set"), as in a fresh process: the commands and their flag variables are
// package state shared by every run.
func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		if slice, ok := f.Value.(pflag.SliceValue); ok {
			_ = slice.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	c.SilenceUsage = false
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := arbor(t, args...)
	if err != nil {
		t.Fatalf("arbor %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func mustFail(t *testing.T, wantErr string, args ...string) string {
	t.Helper()
	out, err := arbor(t, args...)
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("arbor %s: error = %v, want one containing %q\n%s", strings.Join(args, " "), err, wantErr, out)
	}
	return out
}

func contains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output does not contain %q:\n%s", w, out)
		}
	}
}

func excludes(t *testing.T, out string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(out, u) {
			t.Errorf("output contains %q:\n%s", u, out)
		}
	}
}

var (
	minimal     = filepath.Join("examples", examples.Minimal, "config.yaml")
	multiArch   = filepath.Join("examples", examples.MultiArch, "config.yaml")
	multiSource = filepath.Join("examples", examples.MultiSource, "config.yaml")
	services    = filepath.Join("examples", examples.MultiSource, "services.yaml")
)

func TestRoot(t *testing.T) {
	contains(t, mustRun(t), "Available Commands:", "validate", "plan", "build")
	contains(t, mustRun(t, "--help"), "Usage:")
	mustFail(t, `unknown command "invalid"`, "invalid")
}

// validate reports the required bumps and fails; --fix writes them; afterwards all is
// consistent.
func TestValidate_VersionFlow(t *testing.T) {
	r := examples.Repo(t, examples.Minimal)
	contains(t, mustRun(t, "validate", "--config", minimal),
		"✓ Configuration is valid", "✓ All versions are consistent")

	r.Append(examples.Dockerfile(examples.Minimal, "app"), "RUN echo changed\n")
	before := r.Read(minimal)
	contains(t, mustFail(t, "1 version update(s) required; run `arbor validate --fix`", "validate", "--config", minimal),
		"app: 1.0.1 → 1.0.2")
	if r.Read(minimal) != before {
		t.Fatal("validate without --fix changed the config")
	}

	mustRun(t, "validate", "--config", minimal, "--fix")
	contains(t, r.Read(minimal), "version: 1.0.2")
	contains(t, mustRun(t, "validate", "--config", minimal), "✓ All versions are consistent")
}

// --sources files are loaded and their images checked too.
func TestValidate_Sources(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Append(examples.Dockerfile(examples.MultiSource, "api"), "RUN echo changed\n")
	contains(t, mustFail(t, "version update(s) required", "validate", "--config", multiSource, "--sources", services), "api: 1.0.1 → 1.0.2")
	excludes(t, mustRun(t, "validate", "--config", multiSource), "api:")
}

func TestValidate_Errors(t *testing.T) {
	examples.Repo(t, examples.Minimal)
	mustFail(t, `base ref "no-such-ref" not found`, "validate", "--config", minimal, "--base-ref", "no-such-ref")
	mustFail(t, "failed to load config", "validate", "--config", "missing.yaml")

	examples.Write(t, map[string]string{
		"malformed.yaml": "config:\n  invalid: [\n",
		"config.yaml": "images:\n  base:\n    version: 1.0.0\n    base_image: alpine:3.18\n" +
			"    images:\n      app:\n        version: 1.0.0\n",
		"src/Dockerfile.base": "ARG BASE\nFROM ${BASE}\n",
		"src/Dockerfile.app":  "FROM alpine:3.18\n",
	})
	mustFail(t, "failed to load config", "validate", "--config", "malformed.yaml")
	mustFail(t, `image "app" (src/Dockerfile.app): first instruction must be `+"`ARG BASE`", "validate", "--config", "config.yaml")
}

// plan builds each Dockerfile in place with BASE set to the parent's tag.
func TestPlan_Commands(t *testing.T) {
	examples.Repo(t, examples.Minimal)
	contains(t, mustRun(t, "plan", "--config", minimal, "--format", "commands"),
		"-t library/base:1.0.1 -f examples/minimal/src/Dockerfile.base --build-arg BASE=alpine:3.18",
		"-t library/app:1.0.1 -f examples/minimal/src/Dockerfile.app --build-arg BASE=library/base:1.0.1")
}

// plan refuses a missing bump (without the usage text) unless the check is
// skipped.
func TestPlan_VersionGate(t *testing.T) {
	r := examples.Repo(t, examples.Minimal)
	r.Append(examples.Dockerfile(examples.Minimal, "app"), "RUN echo changed\n")
	out := mustFail(t, "version updates required", "plan", "--config", minimal)
	contains(t, out, "app: 1.0.1 → 1.0.2", "arbor validate --fix")
	excludes(t, out, "Usage:")
	mustRun(t, "plan", "--config", minimal, "--skip-version-check")
}

// --changed-only plans the changed image and its dependents only.
func TestPlan_ChangedOnly(t *testing.T) {
	r := examples.Repo(t, examples.Minimal)
	r.Append(examples.Dockerfile(examples.Minimal, "app"), "RUN echo changed\n")
	mustRun(t, "validate", "--config", minimal, "--fix")
	out := mustRun(t, "plan", "--config", minimal, "--changed-only", "--format", "commands")
	contains(t, out, "-t library/app:1.0.2")
	excludes(t, out, "-t library/base:")

	// A branch whose commits touch no image has nothing to build.
	r.Git("checkout", "-q", "--", ".")
	r.Write("NOTES", "unrelated\n")
	r.Commit("unrelated")
	contains(t, mustRun(t, "plan", "--config", minimal, "--changed-only"), "No changed images detected")
}

func TestPlan_Sources(t *testing.T) {
	examples.Repo(t, examples.MultiSource)
	contains(t, mustRun(t, "plan", "--config", multiSource, "--sources", services, "--format", "commands"),
		"-t library/site:", "-t library/api:", "--build-arg BASE=library/api-base:1.0.1")
	excludes(t, mustRun(t, "plan", "--config", multiSource, "--format", "commands"), "-t library/api:")
}

// --push=false overrides the file's push: true; plan adds the push steps only
// with push.
func TestPlan_Push(t *testing.T) {
	examples.Repo(t, examples.MultiArch)
	contains(t, mustRun(t, "plan", "--config", multiArch, "--format", "commands"), "--push")
	excludes(t, mustRun(t, "plan", "--config", multiArch, "--format", "commands", "--push=false"), "--push")
}

// A saved plan (YAML or JSON from the extension) is what build --plan runs.
func TestPlan_OutputAndBuildFromPlan(t *testing.T) {
	examples.Repo(t, examples.Minimal)
	for _, file := range []string{"plan.yaml", "plan.json"} {
		contains(t, mustRun(t, "plan", "--config", minimal, "--dry-run", "--output", file), "✓ Plan saved to "+file)
		contains(t, mustRun(t, "build", "--plan", file), "All 2 images built successfully")
	}
	contains(t, readFile(t, "plan.json"), `"metadata": {`)
	mustFail(t, "unsupported format: graph", "plan", "--config", minimal, "--output", "plan.txt", "--format", "graph")
}

func TestBuild_DryRun(t *testing.T) {
	examples.Repo(t, examples.Minimal, examples.MultiArch)
	contains(t, mustRun(t, "build", "--config", minimal, "--dry-run"), "All 2 images built successfully")
	// A multi-arch image can only be built to push.
	mustFail(t, `multi-arch image "ubuntu" (2 architectures) requires push`,
		"build", "--config", multiArch, "--dry-run", "--push=false")
}

// A wrong flag shows the usage; a failing command only its error.
func TestErrorsWithoutUsage(t *testing.T) {
	contains(t, mustFail(t, "unknown flag: --no-such-flag", "plan", "--no-such-flag"), "Usage:")
	examples.Write(t, map[string]string{
		"config.yaml": "images:\n  app:\n    version: 1.0.0\n    base_image: alpine:3.18\n",
	})
	excludes(t, mustFail(t, `image "app"`, "plan", "--config", "config.yaml"), "Usage:")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
