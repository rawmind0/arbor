package graph

import (
	"slices"
	"sort"
	"testing"

	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/test/examples"
)

// The graph is built per source, as the plan does: from one source's images.

// sourceGraph builds the graph of one source of a loaded config.
func sourceGraph(t *testing.T, cfg *config.Config, source string) *Graph {
	t.Helper()
	g := NewGraph(cfg, source)
	if err := g.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	return g
}

func names(nodes []*Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Image.Name)
	}
	sort.Strings(out)
	return out
}

func levels(g *Graph) [][]string {
	var out [][]string
	for _, l := range g.Levels {
		out = append(out, names(l))
	}
	return out
}

// The hierarchy example: three levels, each image depending on its parent; its
// two roots (ubuntu, tools) start the first level.
func TestBuild_Hierarchy(t *testing.T) {
	g := sourceGraph(t, examples.Load(t, "hierarchy"), "main")

	want := [][]string{{"tools", "ubuntu"}, {"api", "golang", "nodejs", "nodejs22"}, {"backend", "frontend", "worker"}}
	if got := levels(g); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("levels = %v, want %v", got, want)
	}
	deps := map[string][]string{
		"ubuntu": nil, "tools": nil,
		"nodejs": {"ubuntu"}, "nodejs22": {"ubuntu"}, "golang": {"ubuntu"}, "api": {"ubuntu"},
		"frontend": {"nodejs"}, "backend": {"nodejs"}, "worker": {"golang"},
	}
	for _, n := range g.Nodes {
		if got := names(n.Dependencies); !slices.Equal(got, deps[n.Image.Name]) {
			t.Errorf("%s depends on %v, want %v", n.Image.Name, got, deps[n.Image.Name])
		}
	}
	if got := names(g.findNode("nodejs").Dependents); !slices.Equal(got, []string{"backend", "frontend"}) {
		t.Errorf("nodejs dependents = %v", got)
	}

	// Build order is level by level.
	order := g.GetBuildOrder()
	for i := 1; i < len(order); i++ {
		if order[i].Level < order[i-1].Level {
			t.Errorf("build order not by level: %v", names(order))
		}
	}
}

// Tags are the canonical [url/]owner/name:version; a root's parent is its
// base_image, a child's its parent's tag.
func TestTagsAndParents(t *testing.T) {
	g := sourceGraph(t, examples.Load(t, "multiarch"), "main")
	want := map[string][2]string{ // name -> {tag, parent tag}
		"ubuntu":    {"registry.example.com/myorg/ubuntu:22.04.1", "ubuntu:22.04"},
		"golang":    {"registry.example.com/myorg/golang:1.23.6", "registry.example.com/myorg/ubuntu:22.04.1"},
		"app":       {"registry.example.com/myorg/app:1.0.1", "registry.example.com/myorg/golang:1.23.6"},
		"agent":     {"registry.example.com/myorg/agent:1.0.0", "registry.example.com/myorg/ubuntu:22.04.1"},
		"agent-app": {"registry.example.com/myorg/agent-app:1.0.0", "registry.example.com/myorg/agent:1.0.0"},
	}
	for _, n := range g.Nodes {
		w := want[n.Image.Name]
		if n.ImageTag != w[0] {
			t.Errorf("%s tag = %q, want %q", n.Image.Name, n.ImageTag, w[0])
		}
		if got := g.GetParentImageTag(n); got != w[1] {
			t.Errorf("%s parent = %q, want %q", n.Image.Name, got, w[1])
		}
	}
}

// A root without base_image (the hierarchy's tools) has no parent to inject.
func TestParentOfRootWithoutBaseImage(t *testing.T) {
	g := sourceGraph(t, examples.Load(t, "hierarchy"), "main")
	if got := g.GetParentImageTag(g.findNode("tools")); got != "" {
		t.Errorf("parent = %q, want none", got)
	}
}

// Changed images plus everything below them, sorted by level.
func TestAffectedNodes(t *testing.T) {
	cfg := examples.Load(t, "hierarchy")
	cfg.Sources["main"].Images["ubuntu"].Images["nodejs"].Changed = true
	g := sourceGraph(t, cfg, "main")

	changed := g.GetChangedNodes()
	if got := names(changed); !slices.Equal(got, []string{"nodejs"}) {
		t.Fatalf("changed = %v, want [nodejs]", got)
	}
	affected := g.GetAffectedNodes(changed)
	if got := names(affected); !slices.Equal(got, []string{"backend", "frontend", "nodejs"}) {
		t.Errorf("affected = %v, want nodejs and its children", got)
	}
	if affected[0].Image.Name != "nodejs" {
		t.Errorf("affected not sorted by level: first is %s", affected[0].Image.Name)
	}
}

// Several roots and leaves in one source.
func TestMultipleRoots(t *testing.T) {
	cfg := examples.Files(t, map[string]string{
		"config.yaml": "images:\n" +
			"  a:\n    version: 1.0.0\n    base_image: alpine:3.18\n" +
			"    images:\n      a1:\n        version: 1.0.0\n      a2:\n        version: 1.0.0\n" +
			"  b:\n    version: 1.0.0\n    base_image: alpine:3.18\n",
	})
	g := sourceGraph(t, cfg, "main")
	want := [][]string{{"a", "b"}, {"a1", "a2"}}
	if got := levels(g); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("levels = %v, want %v", got, want)
	}
}

// A config without images (no sources) has nothing to build.
func TestBuild_Empty(t *testing.T) {
	cfg := examples.Files(t, map[string]string{"config.yaml": "config:\n  builder: docker\n"})
	g := NewGraph(cfg, config.MainSource)
	if err := g.Build(); err != nil || len(g.Nodes) != 0 || len(g.Levels) != 0 {
		t.Errorf("empty graph: err=%v nodes=%d levels=%d", err, len(g.Nodes), len(g.Levels))
	}
}

// The level calculation rejects cycles. (A nested image tree cannot express
// one, so the nodes are wired by hand here.)
func TestCalculateLevels_Cycle(t *testing.T) {
	g := &Graph{}
	a := &Node{Image: &config.ImageConfig{Name: "a"}}
	b := &Node{Image: &config.ImageConfig{Name: "b"}}
	a.Dependencies, b.Dependencies = []*Node{b}, []*Node{a}
	g.Nodes = []*Node{a, b}
	if err := g.calculateLevels(); err == nil {
		t.Error("expected a circular dependency error")
	}
}
