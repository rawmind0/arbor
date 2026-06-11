package builder

import (
	"strings"
	"testing"
)

func joinSteps(b Builder, opts BuildOptions) []string {
	return joinAll(b.BuildSteps(opts))
}

func joinMultiSteps(b Builder, opts BuildOptions) []string {
	return joinAll(b.BuildMultiSteps(opts))
}

func joinAll(s [][]string) []string {
	out := make([]string, len(s))
	for i, step := range s {
		out[i] = strings.Join(step, " ")
	}
	return out
}

// Single-arch BuildSteps must use a plain `docker build` (no buildx) so the
// result loads into the local image store; --push only adds a separate push.
func TestDockerSingleBuildStepsUsesPlainBuild(t *testing.T) {
	d := NewDockerCompatible(BuilderDocker)
	opts := BuildOptions{
		ImageTag:       "reg/owner/app:1.0.0",
		DockerfilePath: "build/Dockerfile.app",
		BuildContext:   ".",
		Platforms:      []string{"linux/amd64"},
	}

	s := joinSteps(d, opts)
	if len(s) != 1 || !strings.HasPrefix(s[0], "docker build ") {
		t.Fatalf("expected one plain docker build, got %v", s)
	}
	if strings.Contains(s[0], "buildx") {
		t.Errorf("single-arch build must not use buildx: %q", s[0])
	}
	if !strings.Contains(s[0], "--platform linux/amd64") || !strings.Contains(s[0], "-t reg/owner/app:1.0.0") {
		t.Errorf("missing platform/tag: %q", s[0])
	}
	if strings.Contains(s[0], "--push") {
		t.Errorf("non-push build must not have --push: %q", s[0])
	}

	opts.Push = true
	s = joinSteps(d, opts)
	if len(s) != 2 || s[1] != "docker push reg/owner/app:1.0.0" {
		t.Errorf("push build should add a separate docker push step: %v", s)
	}
}

// Multi-arch BuildMultiSteps must use buildx with --push (a manifest list
// cannot be loaded locally, so it is pushed directly).
func TestDockerMultiBuildStepsUsesBuildx(t *testing.T) {
	d := NewDockerCompatible(BuilderDocker)
	opts := BuildOptions{
		ImageTag:       "reg/owner/app:1.0.0",
		DockerfilePath: "build/Dockerfile.app",
		BuildContext:   ".",
		Platforms:      []string{"linux/amd64", "linux/arm64"},
		Push:           true,
	}

	s := joinMultiSteps(d, opts)
	if len(s) != 1 || !strings.HasPrefix(s[0], "docker buildx build ") {
		t.Fatalf("expected one buildx build, got %v", s)
	}
	if !strings.Contains(s[0], "--platform linux/amd64,linux/arm64") || !strings.Contains(s[0], "-t reg/owner/app:1.0.0") {
		t.Errorf("missing platform/tag: %q", s[0])
	}
	if !strings.Contains(s[0], "--push") {
		t.Errorf("multi-arch build should push: %q", s[0])
	}
}

func TestBuildahMultiBuildStepsUsesManifest(t *testing.T) {
	opts := BuildOptions{
		ImageTag:       "reg/owner/app:1.0.0",
		DockerfilePath: "build/Dockerfile.app",
		BuildContext:   ".",
		Platforms:      []string{"linux/amd64", "linux/arm64"},
		Push:           true,
	}
	s := joinMultiSteps(NewBuildah(), opts)
	if len(s) != 2 {
		t.Fatalf("expected build + manifest push, got %v", s)
	}
	if !strings.Contains(s[0], "buildah build") || !strings.Contains(s[0], "--manifest reg/owner/app:1.0.0") {
		t.Errorf("build --manifest: %q", s[0])
	}
	if s[1] != "buildah manifest push --all reg/owner/app:1.0.0" {
		t.Errorf("manifest push: %q", s[1])
	}
}

// Single-arch buildah builds a plain tagged image and pushes it directly.
func TestBuildahSingleBuildStepsPlain(t *testing.T) {
	opts := BuildOptions{ImageTag: "reg/owner/app:1.0.0", DockerfilePath: "x", BuildContext: ".", Platforms: []string{"linux/amd64"}, Push: true}
	s := joinSteps(NewBuildah(), opts)
	if len(s) != 2 || !strings.Contains(s[0], "buildah build") || strings.Contains(s[0], "--manifest") {
		t.Errorf("single-arch buildah should build -t without --manifest: %v", s)
	}
	if s[1] != "buildah push reg/owner/app:1.0.0" {
		t.Errorf("buildah push: %q", s[1])
	}
}

func TestPodmanMultiBuildStepsUsesManifest(t *testing.T) {
	opts := BuildOptions{ImageTag: "reg/owner/app:1.0.0", DockerfilePath: "x", BuildContext: ".", Platforms: []string{"linux/amd64", "linux/arm64"}, Push: true}
	s := joinMultiSteps(NewDockerCompatible(BuilderPodman), opts)
	if len(s) != 2 || !strings.Contains(s[0], "podman build") || !strings.Contains(s[0], "--manifest") {
		t.Errorf("podman build --manifest: %v", s)
	}
	if s[1] != "podman manifest push reg/owner/app:1.0.0" {
		t.Errorf("podman manifest push: %q", s[1])
	}
}

func TestLoginCommand(t *testing.T) {
	login := strings.Join(NewDockerCompatible(BuilderDocker).LoginCommand("docker.io"), " ")
	if !strings.Contains(login, "--password-stdin") || !strings.Contains(login, "$REGISTRY_PASSWORD") {
		t.Errorf("login should use env + password-stdin: %q", login)
	}
	if strings.Contains(login, "-p ") {
		t.Errorf("login must not pass the password on argv: %q", login)
	}
}

func TestMockWrapsBuildSteps(t *testing.T) {
	m, err := GetBuilder(BuilderDocker, ModeMock)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range m.BuildSteps(BuildOptions{ImageTag: "reg/owner/app:1.0.0", Platforms: []string{"linux/amd64"}, Push: true}) {
		if !IsMockCommand(strings.Join(step, " ")) {
			t.Errorf("build step not wrapped for dry-run: %v", step)
		}
	}
}
