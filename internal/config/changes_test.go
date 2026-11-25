package config_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/examples"
	"github.com/rawmind0/arbor/test/testutil"
)

// --changed-only marks images from every source, measured like the version
// check (see git ChangedSince). Scenarios run on a copy of an example in a temp
// repository (examples.Repo): committed on master, a feature branch on top.

// changed runs change detection like `plan --changed-only` and returns the
// sorted names of the images it marked.
func changed(t *testing.T, useCase, baseRef string) []string {
	t.Helper()
	cfg := examples.LoadRepo(t, useCase)
	d := config.NewChangeDetector(cfg, ".")
	if err := d.DetectChanges(baseRef); err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}
	names := d.GetChangedImageNames()
	slices.Sort(names)
	return names
}

func want(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("changed = %v, want %v", got, want)
	}
}

func TestChangedOnly_DockerfileInSourcesFile(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Append(examples.Dockerfile(examples.MultiSource, "api"), "RUN echo changed\n")
	// Only the image itself is marked; its dependents are added by the plan.
	want(t, changed(t, examples.MultiSource, ""), "api")
}

// The reported case: a Dockerfile change plus its `--fix` bumps rebuilds the
// changed image and its children, not every image of the config file.
func TestChangedOnly_DockerfileChangeWithBumps(t *testing.T) {
	r := examples.Repo(t, examples.Hierarchy)
	r.Append(examples.Dockerfile(examples.Hierarchy, "nodejs"), "RUN echo changed\n")
	if err := config.UpdateVersions("examples/hierarchy/config.yaml",
		map[string]string{"nodejs": "20.0.2", "nodejs22": "22.0.1", "frontend": "1.0.2", "backend": "1.0.2"}); err != nil {
		t.Fatal(err)
	}
	r.Commit("change nodejs and bump")
	// nodejs22 is built from the same Dockerfile.
	want(t, changed(t, examples.Hierarchy, ""), "nodejs", "nodejs22", "frontend", "backend")
}

// A change belongs to its closest Dockerfile folder (see ChangedImages):
//   - a folder with one Dockerfile (api/, frontend/): any file in it, or below
//     it, changes that image, and nothing in the folders above
//   - a folder with several Dockerfiles (src/): only the images whose
//     Dockerfile mentions the file (Dockerfile.ubuntu: conf/apt.conf)
//   - a file outside every Dockerfile folder changes nothing
func TestChangedOnly_DockerfileFolder(t *testing.T) {
	r := examples.Repo(t, examples.Hierarchy)
	reset := func() {
		r.Git("checkout", "-q", "--", ".")
		r.Git("clean", "-qfd")
	}
	for _, tt := range []struct {
		file string
		want []string
	}{
		{"examples/hierarchy/src/api/main.go", []string{"api"}},
		{"examples/hierarchy/src/api/README", []string{"api"}},                   // not copied, still its folder
		{"examples/hierarchy/src/frontend/static/new.css", []string{"frontend"}}, // untracked, below its folder
		{"examples/hierarchy/src/conf/apt.conf", []string{"ubuntu"}},             // shared folder, mentioned
		{"examples/hierarchy/src/conf/other.conf", nil},                          // shared folder, not mentioned
		{"examples/hierarchy/src/NOTES", nil},                                    // shared folder, not mentioned
		{"examples/hierarchy/NOTES", nil},                                        // no Dockerfile folder
	} {
		t.Run(tt.file, func(t *testing.T) {
			reset()
			r.Write(tt.file, "changed\n")
			want(t, changed(t, examples.Hierarchy, ""), tt.want...)
		})
	}
}

// In a folder with several Dockerfiles, a Dockerfile mentions a file by its
// path or by one of its parent folders (COPY conf/ ...), as a whole path.
func TestChangedImages_SharedFolderMentions(t *testing.T) {
	cfg := examples.Files(t, map[string]string{
		"config.yaml": "images:\n" +
			"  a:\n    version: 1.0.0\n    base_image: alpine:3.20\n" +
			"  b:\n    version: 1.0.0\n    base_image: alpine:3.20\n" +
			"  c:\n    version: 1.0.0\n    base_image: alpine:3.20\n",
		"src/Dockerfile.a": "ARG BASE\nFROM ${BASE}\nCOPY conf/ /etc/app/\n",
		"src/Dockerfile.b": "ARG BASE\nFROM ${BASE}\nCOPY myconf/ /etc/app/\n",
		"src/Dockerfile.c": "ARG BASE\nFROM ${BASE}\nCOPY [\"./conf/app.yaml\", \"/app.yaml\"]\n",
	})
	for file, want := range map[string][]string{
		"src/conf/app.yaml":   {"a", "c"}, // a: its folder; c: the file itself
		"src/conf/other.yaml": {"a"},
		"src/myconf/x.yaml":   {"b"},
		"src/Dockerfile.b":    {"b"},
		"src/README":          nil,
	} {
		var got []string
		for img := range cfg.ChangedImages([]string{file}) {
			got = append(got, img.Name)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("ChangedImages(%s) = %v, want %v", file, got, want)
		}
	}
}

// Inside a git worktree (.git is a file) changes are detected as usual, not
// treated as "no git" (which rebuilds everything).
func TestChangedOnly_InWorktree(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	worktree := filepath.Join(t.TempDir(), "wt")
	r.Git("worktree", "add", "-q", "-b", "wt", worktree)
	t.Chdir(worktree)
	testutil.WriteFile(t, examples.Dockerfile(examples.MultiSource, "site"), "ARG BASE\nFROM ${BASE}\nRUN echo changed\n")
	want(t, changed(t, examples.MultiSource, ""), "site")
}

// Comments and formatting are not changes.
func TestChangedOnly_CommentsOnly(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Append("examples/multisource/config.yaml", "# touched\n")
	r.Append("examples/multisource/services.yaml", "# touched\n")
	want(t, changed(t, examples.MultiSource, ""))
}

// A version bump in a --sources file marks only the bumped image.
func TestChangedOnly_VersionBumpInSourcesFile(t *testing.T) {
	examples.Repo(t, examples.MultiSource)
	if err := config.UpdateVersions("examples/multisource/services.yaml", map[string]string{"api": "1.0.2"}); err != nil {
		t.Fatal(err)
	}
	want(t, changed(t, examples.MultiSource, ""), "api")
}

// Any other field of an image entry counts too (args here).
func TestChangedOnly_ImageArgsChanged(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Replace("examples/multisource/services.yaml", `GO_VERSION: "1.23.5"`, `GO_VERSION: "1.23.6"`)
	want(t, changed(t, examples.MultiSource, ""), "api-base")
}

// A global setting that affects the builds marks every image, in every source.
func TestChangedOnly_GlobalSettingChanged(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Replace("examples/multisource/config.yaml", "project: multisource-example", "project: renamed")
	want(t, changed(t, examples.MultiSource, ""), "web-base", "site", "admin", "api-base", "api", "worker")
}

// Command defaults (workers, base_ref, ...) don't change what is built.
func TestChangedOnly_CommandDefaultChanged(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Replace("examples/multisource/config.yaml", "workers: 4", "workers: 8")
	want(t, changed(t, examples.MultiSource, ""))
}

// A --sources file that didn't exist at the base: all its images are new.
func TestChangedOnly_NewSourcesFile(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Write("examples/multisource/extra.yaml",
		"images:\n  extra:\n    version: 1.0.0\n    base_image: ubuntu:22.04\n    dockerfile: Dockerfile.api-base\n")
	want(t, changed(t, examples.MultiSource, ""), "extra")
}

// Moving an image under another parent changes its BASE.
func TestChangedOnly_ImageMoved(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Replace("examples/multisource/config.yaml",
		"    images:\n      site:            # FROM web-base\n        version: 1.0.1\n      admin:           # FROM web-base\n        version: 1.0.1",
		"    images:\n      site:            # FROM web-base\n        version: 1.0.1\n        images:\n          admin:\n            version: 1.0.1")
	want(t, changed(t, examples.MultiSource, ""), "admin")
}

// Every commit on the branch counts; --base-ref HEAD~1 narrows to the last.
func TestChangedOnly_MultiCommitBranch(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Append(examples.Dockerfile(examples.MultiSource, "site"), "RUN echo one\n")
	r.Commit("change site")
	r.Append(examples.Dockerfile(examples.MultiSource, "worker"), "RUN echo two\n")
	r.Commit("change worker")
	want(t, changed(t, examples.MultiSource, ""), "site", "worker")
	want(t, changed(t, examples.MultiSource, "HEAD~1"), "worker")
}

// On master after a merge, the default covers what the merge brought in.
func TestChangedOnly_DefaultBranchAfterMerge(t *testing.T) {
	r := examples.Repo(t, examples.MultiSource)
	r.Append(examples.Dockerfile(examples.MultiSource, "admin"), "RUN echo changed\n")
	r.Commit("change admin")
	r.Git("checkout", "-q", "master")
	r.Git("merge", "-q", "--no-ff", "feature", "-m", "merge feature")
	want(t, changed(t, examples.MultiSource, ""), "admin")
}
