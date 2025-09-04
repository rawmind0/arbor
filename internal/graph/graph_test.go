package graph

import (
	"testing"

	"github.com/rawmind0/container-builder/internal/config"
)

func TestNewGraph(t *testing.T) {
	cfg := &config.Config{}
	graph := NewGraph(cfg)

	if graph == nil {
		t.Error("Expected graph to be created")
	}
	if graph.config != cfg {
		t.Error("Expected graph to have reference to config")
	}
	if len(graph.Nodes) != 0 {
		t.Error("Expected new graph to have no nodes")
	}
}

func TestGraph_Build(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"parent": {
				Name:          "parent",
				Version:       "1.0.0",
				Architectures: []string{"linux/amd64"},
				BaseImage:     "ubuntu:22.04",
				Images: map[string]*config.ImageConfig{
					"child": {
						Name:          "child",
						Parent:        "parent",
						Version:       "2.0.0",
						Architectures: []string{"linux/amd64"},
					},
				},
			},
		},
	}

	graph := NewGraph(cfg)
	err := graph.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Should have 2 nodes (parent + child, each with 1 architecture)
	if len(graph.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(graph.Nodes))
	}

	// Should have 2 levels (parent at level 0, child at level 1)
	if len(graph.Levels) != 2 {
		t.Errorf("Expected 2 levels, got %d", len(graph.Levels))
	}

	// Check parent node
	parentNode := graph.findNode("parent", "linux/amd64")
	if parentNode == nil {
		t.Fatal("Expected to find parent node")
	}
	if len(parentNode.Dependencies) != 0 {
		t.Errorf("Expected parent to have no dependencies, got %d", len(parentNode.Dependencies))
	}
	if len(parentNode.Dependents) != 1 {
		t.Errorf("Expected parent to have 1 dependent, got %d", len(parentNode.Dependents))
	}
	if parentNode.Level != 0 {
		t.Errorf("Expected parent at level 0, got level %d", parentNode.Level)
	}

	// Check child node
	childNode := graph.findNode("child", "linux/amd64")
	if childNode == nil {
		t.Fatal("Expected to find child node")
	}
	if len(childNode.Dependencies) != 1 {
		t.Errorf("Expected child to have 1 dependency, got %d", len(childNode.Dependencies))
	}
	if len(childNode.Dependents) != 0 {
		t.Errorf("Expected child to have no dependents, got %d", len(childNode.Dependents))
	}
	if childNode.Level != 1 {
		t.Errorf("Expected child at level 1, got level %d", childNode.Level)
	}
}

func TestGraph_BuildMultiArch(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"parent": {
				Name:          "parent",
				Version:       "1.0.0",
				Architectures: []string{"linux/amd64", "linux/arm64"},
				BaseImage:     "ubuntu:22.04",
				Images: map[string]*config.ImageConfig{
					"child": {
						Name:          "child",
						Parent:        "parent",
						Version:       "2.0.0",
						Architectures: []string{"linux/amd64"}, // Only AMD64
					},
				},
			},
		},
	}

	graph := NewGraph(cfg)
	err := graph.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Should have 3 nodes (parent-amd64, parent-arm64, child-amd64)
	if len(graph.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(graph.Nodes))
	}

	// Check parent nodes exist for both architectures
	parentAmd64 := graph.findNode("parent", "linux/amd64")
	if parentAmd64 == nil {
		t.Error("Expected to find parent AMD64 node")
	}

	parentArm64 := graph.findNode("parent", "linux/arm64")
	if parentArm64 == nil {
		t.Error("Expected to find parent ARM64 node")
	}

	// Check child node exists only for AMD64
	childAmd64 := graph.findNode("child", "linux/amd64")
	if childAmd64 == nil {
		t.Error("Expected to find child AMD64 node")
	}

	childArm64 := graph.findNode("child", "linux/arm64")
	if childArm64 != nil {
		t.Error("Did not expect to find child ARM64 node")
	}

	// Check dependency is correct
	if len(childAmd64.Dependencies) != 1 || childAmd64.Dependencies[0] != parentAmd64 {
		t.Error("Expected child AMD64 to depend on parent AMD64")
	}
}

func TestGraph_buildImageTag(t *testing.T) {
	graph := &Graph{}

	tests := []struct {
		name string
		img  *config.ImageConfig
		arch string
		want string
	}{
		{
			name: "with architecture",
			img: &config.ImageConfig{
				Name:    "test",
				Version: "1.0.0",
			},
			arch: "linux/amd64",
			want: "test:1.0.0-linux-amd64",
		},
		{
			name: "without architecture",
			img: &config.ImageConfig{
				Name:    "test",
				Version: "1.0.0",
			},
			arch: "",
			want: "test:1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graph.buildImageTag(tt.img, tt.arch)
			if got != tt.want {
				t.Errorf("buildImageTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGraph_GetBuildOrder(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"a": {
				Name:          "a",
				Version:       "1.0.0",
				Architectures: []string{"linux/amd64"},
				BaseImage:     "ubuntu:22.04",
				Images: map[string]*config.ImageConfig{
					"b": {
						Name:          "b",
						Parent:        "a",
						Version:       "2.0.0",
						Architectures: []string{"linux/amd64"},
						Images: map[string]*config.ImageConfig{
							"c": {
								Name:          "c",
								Parent:        "b",
								Version:       "3.0.0",
								Architectures: []string{"linux/amd64"},
							},
						},
					},
				},
			},
		},
	}

	graph := NewGraph(cfg)
	if err := graph.Build(); err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	order := graph.GetBuildOrder()
	if len(order) != 3 {
		t.Errorf("Expected 3 nodes in build order, got %d", len(order))
	}

	// Check order: a, then b, then c
	if order[0].Image.Name != "a" {
		t.Errorf("Expected first node to be 'a', got '%s'", order[0].Image.Name)
	}
	if order[1].Image.Name != "b" {
		t.Errorf("Expected second node to be 'b', got '%s'", order[1].Image.Name)
	}
	if order[2].Image.Name != "c" {
		t.Errorf("Expected third node to be 'c', got '%s'", order[2].Image.Name)
	}
}

func TestGraph_GetChangedNodes(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"changed": {
				Name:          "changed",
				Version:       "1.0.0",
				Changed:       true,
				Architectures: []string{"linux/amd64"},
			},
			"unchanged": {
				Name:          "unchanged",
				Version:       "1.0.0",
				Changed:       false,
				Architectures: []string{"linux/amd64"},
			},
		},
	}

	graph := NewGraph(cfg)
	if err := graph.Build(); err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	changedNodes := graph.GetChangedNodes()
	if len(changedNodes) != 1 {
		t.Errorf("Expected 1 changed node, got %d", len(changedNodes))
	}

	if changedNodes[0].Image.Name != "changed" {
		t.Errorf("Expected changed node to be 'changed', got '%s'", changedNodes[0].Image.Name)
	}
}

func TestGraph_GetAffectedNodes(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"parent": {
				Name:          "parent",
				Version:       "1.0.0",
				Changed:       true,
				Architectures: []string{"linux/amd64"},
				BaseImage:     "ubuntu:22.04",
				Images: map[string]*config.ImageConfig{
					"child": {
						Name:          "child",
						Parent:        "parent",
						Version:       "2.0.0",
						Changed:       false,
						Architectures: []string{"linux/amd64"},
					},
				},
			},
		},
	}

	graph := NewGraph(cfg)
	if err := graph.Build(); err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	changedNodes := graph.GetChangedNodes()
	affectedNodes := graph.GetAffectedNodes(changedNodes)

	// Should include both parent (changed) and child (affected by parent change)
	if len(affectedNodes) != 2 {
		t.Errorf("Expected 2 affected nodes, got %d", len(affectedNodes))
	}

	// Check that both parent and child are included
	foundParent, foundChild := false, false
	for _, node := range affectedNodes {
		if node.Image.Name == "parent" {
			foundParent = true
		}
		if node.Image.Name == "child" {
			foundChild = true
		}
	}

	if !foundParent {
		t.Error("Expected to find parent in affected nodes")
	}
	if !foundChild {
		t.Error("Expected to find child in affected nodes")
	}
}

func TestGraph_GetParentImageTag(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"parent": {
				Name:          "parent",
				Version:       "1.0.0",
				BaseImage:     "ubuntu:22.04",
				Architectures: []string{"linux/amd64"},
				Images: map[string]*config.ImageConfig{
					"child": {
						Name:          "child",
						Parent:        "parent",
						Version:       "2.0.0",
						Architectures: []string{"linux/amd64"},
					},
				},
			},
		},
	}

	graph := NewGraph(cfg)
	if err := graph.Build(); err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	parentNode := graph.findNode("parent", "linux/amd64")
	childNode := graph.findNode("child", "linux/amd64")

	// Parent node should return base image
	parentTag := graph.GetParentImageTag(parentNode)
	if parentTag != "ubuntu:22.04" {
		t.Errorf("Expected parent tag to be 'ubuntu:22.04', got '%s'", parentTag)
	}

	// Child node should return parent's image tag
	childTag := graph.GetParentImageTag(childNode)
	expected := "parent:1.0.0-linux-amd64"
	if childTag != expected {
		t.Errorf("Expected child tag to be '%s', got '%s'", expected, childTag)
	}
}

func TestSanitizeArch(t *testing.T) {
	tests := []struct {
		name string
		arch string
		want string
	}{
		{
			name: "linux/amd64",
			arch: "linux/amd64",
			want: "linux-amd64",
		},
		{
			name: "linux/arm64",
			arch: "linux/arm64",
			want: "linux-arm64",
		},
		{
			name: "darwin/arm64",
			arch: "darwin/arm64",
			want: "darwin-arm64",
		},
		{
			name: "simple arch",
			arch: "amd64",
			want: "amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeArch(tt.arch)
			if got != tt.want {
				t.Errorf("sanitizeArch() = %v, want %v", got, tt.want)
			}
		})
	}
}
