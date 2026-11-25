package version

import (
	"slices"
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/examples"
	"github.com/rawmind0/arbor/test/testutil"
)

// The version check needs git history, so every scenario runs on a copy of a
// real example in a temp repository (examples.Repo): committed on "master",
// with a "feature" branch on top, the way a pull request looks.

// bumps runs the check and returns the required updates as sorted
// "image old→new" strings.
func bumps(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	result := mustValidate(t, cfg)
	if len(result.Errors) > 0 {
		t.Fatalf("Validate errors: %v", result.Errors)
	}
	var out []string
	for _, u := range result.UpdatesRequired {
		out = append(out, u.ImageName+" "+u.OldVersion+"→"+u.NewVersion)
	}
	slices.Sort(out)
	return out
}

func wantBumps(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("bumps = %v, want %v", got, want)
	}
}

func mustValidate(t *testing.T, cfg *config.Config) *ValidationResult {
	t.Helper()
	result, err := NewValidator(cfg).Validate()
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return result
}

// fix runs `validate --fix`: validate, then apply.
func fix(t *testing.T, useCase string) {
	t.Helper()
	cfg := examples.LoadRepo(t, useCase)
	if err := NewValidator(cfg).Apply(mustValidate(t, cfg)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

// A changed Dockerfile needs a bump of every image built from it (nodejs22
// shares Dockerfile.nodejs), cascading to their children only.
func TestCheck_CascadesToChildren(t *testing.T) {
	r := examples.Repo(t, "hierarchy")
	r.Append(examples.Dockerfile("hierarchy", "nodejs"), "RUN echo changed\n")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "hierarchy")),
		"nodejs 20.0.1→20.0.2", "nodejs22 22.0.0→22.0.1", "frontend 1.0.1→1.0.2", "backend 1.0.1→1.0.2")
}

// A change in an image's Dockerfile folder needs a bump, like a change to its
// Dockerfile (see config.ChangedImages; the folder cases are covered with
// --changed-only, which uses the same rule).
func TestCheck_DockerfileFolderChanged(t *testing.T) {
	r := examples.Repo(t, "hierarchy", "podman")
	r.Append("examples/hierarchy/src/frontend/static/index.html", "<p>changed</p>\n")
	r.Write("examples/hierarchy/src/api/README", "notes\n")
	result := mustValidate(t, examples.LoadRepo(t, "hierarchy"))
	wantBumps(t, bumps(t, examples.LoadRepo(t, "hierarchy")), "frontend 1.0.1→1.0.2", "api 1.0.0→1.0.1")
	if got := result.UpdatesRequired["main/frontend"].Reason; got !=
		"examples/hierarchy/src/frontend/static/index.html (in its Dockerfile folder) has been modified" {
		t.Errorf("frontend reason = %q", got)
	}

	// podman bumps minor (version_increment: minor) and keeps the "v" prefix.
	r.Write("examples/podman/src/app/util.py", "VALUE = 1\n")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "podman")), "app v1.0.0→v1.1.0")

	// A shared file mentioned by Dockerfile.ubuntu: ubuntu and all its children.
	r.Git("checkout", "-q", "--", ".")
	r.Git("clean", "-qfd")
	r.Append("examples/hierarchy/src/conf/apt.conf", "# changed\n")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "hierarchy")),
		"ubuntu 22.04.1→22.04.2", "nodejs 20.0.1→20.0.2", "nodejs22 22.0.0→22.0.1",
		"frontend 1.0.1→1.0.2", "backend 1.0.1→1.0.2", "golang 1.23.6→1.23.7",
		"worker 1.0.1→1.0.2", "api 1.0.0→1.0.1")
}

func TestCheck_RootChangeCascadesToAll(t *testing.T) {
	r := examples.Repo(t, "hierarchy")
	r.Append(examples.Dockerfile("hierarchy", "ubuntu"), "RUN echo changed\n")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "hierarchy")),
		"ubuntu 22.04.1→22.04.2", "nodejs 20.0.1→20.0.2", "nodejs22 22.0.0→22.0.1",
		"frontend 1.0.1→1.0.2", "backend 1.0.1→1.0.2", "golang 1.23.6→1.23.7",
		"worker 1.0.1→1.0.2", "api 1.0.0→1.0.1")
}

// Images from a --sources file are checked like the config file's own images.
func TestCheck_SourcesFilesAreChecked(t *testing.T) {
	r := examples.Repo(t, "multisource")
	r.Append(examples.Dockerfile("multisource", "api"), "RUN echo changed\n")      // services.yaml
	r.Append(examples.Dockerfile("multisource", "web-base"), "RUN echo changed\n") // config.yaml
	wantBumps(t, bumps(t, examples.LoadRepo(t, "multisource")),
		"api 1.0.1→1.0.2", "web-base 1.0.1→1.0.2", "site 1.0.1→1.0.2", "admin 1.0.1→1.0.2")
}

// --fix writes every bump into the file its image is defined in, changing only
// those version values; afterwards nothing more is requested.
func TestFix_OnlyVersionValuesChange(t *testing.T) {
	r := examples.Repo(t, "multisource")
	r.Append(examples.Dockerfile("multisource", "site"), "RUN echo changed\n")     // config.yaml
	r.Append(examples.Dockerfile("multisource", "api-base"), "RUN echo changed\n") // services.yaml
	mainBefore := r.Read("examples/multisource/config.yaml")
	servicesBefore := r.Read("examples/multisource/services.yaml")

	fix(t, "multisource")

	wantOnlyVersionsChanged(t, mainBefore, r.Read("examples/multisource/config.yaml"),
		"site:", "1.0.2")
	wantOnlyVersionsChanged(t, servicesBefore, r.Read("examples/multisource/services.yaml"),
		"api-base:", "1.0.2", "api:", "1.0.2", "worker:", "1.0.2")

	wantBumps(t, bumps(t, examples.LoadRepo(t, "multisource")))
}

// wantOnlyVersionsChanged checks after differs from before only in `version:`
// lines, exactly the expected ones: pairs of (the image key on the line above,
// the new version).
func wantOnlyVersionsChanged(t *testing.T, before, after string, pairs ...string) {
	t.Helper()
	b, a := strings.Split(before, "\n"), strings.Split(after, "\n")
	if len(a) != len(b) {
		t.Fatalf("line count changed: %d → %d", len(b), len(a))
	}
	var changed []string
	for i := range b {
		if a[i] == b[i] {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(b[i]), "version:") || i == 0 {
			t.Errorf("non-version line changed:\n- %s\n+ %s", b[i], a[i])
			continue
		}
		key := strings.Fields(b[i-1])[0]
		changed = append(changed, key+" "+strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a[i]), "version:")))
	}
	var want []string
	for i := 0; i+1 < len(pairs); i += 2 {
		want = append(want, pairs[i]+" "+pairs[i+1])
	}
	if !slices.Equal(changed, want) {
		t.Errorf("changed versions = %q, want %q", changed, want)
	}
}

// Dockerfile changed in the first commit of a two-commit branch, never bumped.
func TestCheck_TwoCommitBranch_ChangeInFirstCommit(t *testing.T) {
	r := examples.Repo(t, "minimal")
	r.Append(examples.Dockerfile("minimal", "app"), "RUN echo changed\n")
	r.Commit("change app")
	r.Write("NOTES", "unrelated\n")
	r.Commit("unrelated")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "minimal")), "app 1.0.1→1.0.2")
}

// Bump in the first commit, Dockerfile edit in the second: already bumped.
func TestCheck_TwoCommitBranch_BumpThenEdit(t *testing.T) {
	r := examples.Repo(t, "minimal")
	if err := config.UpdateVersions("examples/minimal/config.yaml", map[string]string{"app": "1.0.2"}); err != nil {
		t.Fatal(err)
	}
	r.Commit("bump app")
	r.Append(examples.Dockerfile("minimal", "app"), "RUN echo changed\n")
	r.Commit("change app")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "minimal")))
}

func TestCheck_NewImageNeedsNoBump(t *testing.T) {
	r := examples.Repo(t, "minimal")
	r.Append("examples/minimal/config.yaml", "      extra:\n        version: 1.0.0\n")
	r.Write(examples.Dockerfile("minimal", "extra"), "ARG BASE\nFROM ${BASE}\n")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "minimal")))
}

// On master after a merge, the default (no base_ref) checks the last commit:
// the merged branch bumped its versions, so nothing is requested; an unbumped
// change committed directly on master is.
func TestCheck_DefaultBranchAfterMerge(t *testing.T) {
	r := examples.Repo(t, "minimal")
	r.Append(examples.Dockerfile("minimal", "app"), "RUN echo changed\n")
	fix(t, "minimal")
	r.Commit("change and bump app")
	r.Git("checkout", "-q", "master")
	r.Git("merge", "-q", "--no-ff", "feature", "-m", "merge feature")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "minimal")))

	r.Append(examples.Dockerfile("minimal", "app"), "RUN echo hotfix\n")
	r.Commit("hotfix without bump")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "minimal")), "app 1.0.2→1.0.3")
}

// On master, uncommitted changes are what is checked.
func TestCheck_DefaultBranchUncommitted(t *testing.T) {
	r := examples.Repo(t, "minimal")
	r.Git("checkout", "-q", "master")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "minimal")))
	r.Append(examples.Dockerfile("minimal", "base"), "RUN echo changed\n")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "minimal")), "base 1.0.1→1.0.2", "app 1.0.1→1.0.2")
}

// Default base ref: master, else main; neither → error until base_ref is set.
func TestCheck_BaseRefResolution(t *testing.T) {
	t.Run("main", func(t *testing.T) {
		r := testutil.NewRepo(t, "main")
		examples.CopyToRepo(r, "minimal")
		r.Commit("init")
		r.Git("checkout", "-q", "-b", "feature")
		r.Append(examples.Dockerfile("minimal", "app"), "RUN echo changed\n")
		wantBumps(t, bumps(t, examples.LoadRepo(t, "minimal")), "app 1.0.1→1.0.2")
	})
	t.Run("neither", func(t *testing.T) {
		r := testutil.NewRepo(t, "develop")
		examples.CopyToRepo(r, "minimal")
		r.Commit("init")
		r.Git("checkout", "-q", "-b", "feature")
		cfg := examples.LoadRepo(t, "minimal")
		if _, err := NewValidator(cfg).Validate(); err == nil || !strings.Contains(err.Error(), "base ref not found") {
			t.Fatalf("Validate() = %v, want a 'base ref not found' error", err)
		}
		cfg.Config.BaseRef = "develop"
		wantBumps(t, bumps(t, cfg))
	})
	t.Run("unknown", func(t *testing.T) {
		examples.Repo(t, "minimal")
		cfg := examples.LoadRepo(t, "minimal")
		cfg.Config.BaseRef = "nope"
		if _, err := NewValidator(cfg).Validate(); err == nil || !strings.Contains(err.Error(), `base ref "nope" not found`) {
			t.Fatalf("Validate() = %v, want a `base ref \"nope\" not found` error", err)
		}
	})
}

// The podman example sets version_increment: minor; the bump cascades as minor.
func TestCheck_MinorIncrement(t *testing.T) {
	r := examples.Repo(t, "podman")
	r.Append(examples.Dockerfile("podman", "python"), "RUN echo changed\n")
	wantBumps(t, bumps(t, examples.LoadRepo(t, "podman")), "python 3.12.0→3.13.0", "app v1.0.0→v1.1.0")
}

// Check is the plan/build gate: an error listing the bumps, nil when consistent.
func TestCheck_Gate(t *testing.T) {
	r := examples.Repo(t, "minimal")
	if err := NewValidator(examples.LoadRepo(t, "minimal")).Check(); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
	r.Append(examples.Dockerfile("minimal", "app"), "RUN echo changed\n")
	err := NewValidator(examples.LoadRepo(t, "minimal")).Check()
	if err == nil || !strings.Contains(err.Error(), "app: 1.0.1 → 1.0.2") ||
		!strings.Contains(err.Error(), "arbor validate --fix") {
		t.Fatalf("Check() = %v, want the app bump and a pointer to `arbor validate --fix`", err)
	}
}

func TestCheck_InvalidVersion(t *testing.T) {
	cfg := examples.Files(t, map[string]string{
		"config.yaml": "images:\n  app:\n    version: not-a-version\n    base_image: alpine:3.18\n",
	})
	err := NewValidator(cfg).Check()
	if err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("Check() = %v, want an invalid version error", err)
	}
}
