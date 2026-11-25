package plan

import (
	"slices"
	"strings"
	"testing"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/test/examples"
)

// What every example builds (tags, platforms, Dockerfiles, BASE and args per
// image) is checked end to end in test/integration (TestExamplesPlan). These
// tests cover the generator's own choices.

// commandNames returns, per queue ID, the image names in command order.
func commandNames(p *BuildPlan) map[string][]string {
	out := map[string][]string{}
	for _, q := range p.Queues {
		for _, c := range q.Commands {
			out[q.ID] = append(out[q.ID], c.ImageName)
		}
	}
	return out
}

// Changed-only plans cover every source: only the changed images and their
// dependents, in build order, each in its own source's queue.
func TestGenerateChanged_PerSource(t *testing.T) {
	cfg := examples.Load(t, "multisource")
	cfg.Config.Mode = string(builder.ModeMock)
	cfg.Sources["main"].Images["web-base"].Changed = true
	cfg.Sources["services"].Images["api-base"].Images["api"].Changed = true

	p, err := NewGenerator(cfg).GenerateChanged()
	if err != nil {
		t.Fatal(err)
	}
	got := commandNames(p)
	want := map[string][]string{
		"main":     {"web-base", "admin", "site"},
		"services": {"api"},
	}
	for id, names := range want {
		if !slices.Equal(got[id], names) {
			t.Errorf("queue %s = %v, want %v", id, got[id], names)
		}
	}
	if len(got) != len(want) || p.Metadata.TotalImages != 4 || !p.Metadata.ChangedOnly {
		t.Errorf("queues = %v, total = %d, changedOnly = %v", got, p.Metadata.TotalImages, p.Metadata.ChangedOnly)
	}
}

func TestGenerateChanged_NothingChanged(t *testing.T) {
	cfg := examples.Load(t, "multisource")
	p, err := NewGenerator(cfg).GenerateChanged()
	if err != nil {
		t.Fatal(err)
	}
	if p.GetTotalCommands() != 0 || len(p.Queues) != 0 {
		t.Errorf("expected an empty plan, got %v", commandNames(p))
	}
}

// Queues follow the sources: the config file's first, then --sources by name.
func TestGenerate_QueueOrder(t *testing.T) {
	p := planFromExample(t, "multisource", builder.ModeMock)
	var ids []string
	for _, q := range p.Queues {
		ids = append(ids, q.ID)
	}
	if !slices.Equal(ids, []string{"main", "services"}) {
		t.Errorf("queues = %v, want [main services]", ids)
	}
}

// BASE is added to the command's build args only; the image's configured args
// are left untouched.
func TestGenerate_DoesNotMutateImageArgs(t *testing.T) {
	cfg := examples.Load(t, "hierarchy")
	if _, err := NewGenerator(cfg).Generate(); err != nil {
		t.Fatal(err)
	}
	nodejs := cfg.Sources["main"].Images["ubuntu"].Images["nodejs"]
	if _, ok := nodejs.Args["BASE"]; ok || len(nodejs.Args) != 1 {
		t.Errorf("nodejs args mutated: %v", nodejs.Args)
	}
}

// Push (multiarch example), after login: a multi-arch image is one
// multi-platform build that pushes; a single-arch one (agent, amd64 only, and
// its child) builds locally, then pushes.
func TestGenerate_Push(t *testing.T) {
	cfg := examples.Load(t, "multiarch")
	p, err := NewGenerator(cfg).Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Login) == 0 {
		t.Error("expected plan.Login to be set when pushing")
	}
	for _, c := range p.Queues[0].Commands {
		build := strings.Join(c.Steps[0], " ")
		want := []string{"docker buildx build", "--platform linux/amd64,linux/arm64", "-t " + c.ImageTag, "--push"}
		steps := 1
		if c.Architecture == "linux/amd64" {
			want = []string{"docker build", "--platform linux/amd64", "-t " + c.ImageTag}
			steps = 2
		}
		if len(c.Steps) != steps {
			t.Fatalf("%s: want %d steps, got %v", c.ImageName, steps, c.Steps)
		}
		for _, w := range want {
			if !strings.Contains(build, w) {
				t.Errorf("%s: %q missing %q", c.ImageName, build, w)
			}
		}
		if steps == 2 && strings.Join(c.Steps[1], " ") != "docker push "+c.ImageTag {
			t.Errorf("%s: push step = %v", c.ImageName, c.Steps[1])
		}
	}
}

// Without push the same images still build their own platforms, with no
// login or push.
func TestGenerate_NoPushBuildsAllArches(t *testing.T) {
	cfg := examples.Load(t, "multiarch")
	cfg.Config.Push = false
	p, err := NewGenerator(cfg).Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Login) != 0 {
		t.Error("non-push plan should have no login")
	}
	for _, c := range p.Queues[0].Commands {
		s := strings.Join(c.Steps[0], " ")
		if len(c.Steps) != 1 || !strings.Contains(s, "--platform "+c.Architecture) || strings.Contains(s, "--push") {
			t.Errorf("%s: want one build for %s and no push: %v", c.ImageName, c.Architecture, c.Steps)
		}
	}
}
