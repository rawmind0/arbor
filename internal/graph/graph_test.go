package graph

import (
	"testing"

	"github.com/rawmind0/arbor/internal/config"
)

func TestNewGraph(t *testing.T) {
	cfg := &config.Config{}
	graph := NewGraph(cfg)

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
	parentNode := graph.findNode("parent")
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
	childNode := graph.findNode("child")
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

	// One node per image: parent and child (architectures live on the image)
	if len(graph.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(graph.Nodes))
	}

	parent := graph.findNode("parent")
	if parent == nil {
		t.Fatal("Expected to find parent node")
	}
	if len(parent.Image.Architectures) != 2 {
		t.Errorf("Expected parent to target 2 architectures, got %d", len(parent.Image.Architectures))
	}

	child := graph.findNode("child")
	if child == nil {
		t.Fatal("Expected to find child node")
	}
	if len(child.Image.Architectures) != 1 {
		t.Errorf("Expected child to target 1 architecture, got %d", len(child.Image.Architectures))
	}

	// Check dependency is correct
	if len(child.Dependencies) != 1 || child.Dependencies[0] != parent {
		t.Error("Expected child to depend on parent")
	}
}

func TestGraph_buildImageTag(t *testing.T) {
	// Tags are the canonical [url/]owner/name:version (the multi-arch manifest
	// tag); arch is not part of the tag.
	graph := NewGraph(&config.Config{
		Config: config.ConfigSection{Registry: config.RegistryConfig{Owner: "myorg"}},
	})

	img := &config.ImageConfig{Name: "test", Version: "1.0.0"}
	if got := graph.buildImageTag(img); got != "myorg/test:1.0.0" {
		t.Errorf("buildImageTag() = %v, want myorg/test:1.0.0", got)
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

	parentNode := graph.findNode("parent")
	childNode := graph.findNode("child")

	// Parent node should return base image
	parentTag := graph.GetParentImageTag(parentNode)
	if parentTag != "ubuntu:22.04" {
		t.Errorf("Expected parent tag to be 'ubuntu:22.04', got '%s'", parentTag)
	}

	// Child node should return parent's image tag
	childTag := graph.GetParentImageTag(childNode)
	expected := "parent:1.0.0"
	if childTag != expected {
		t.Errorf("Expected child tag to be '%s', got '%s'", expected, childTag)
	}
}

func TestGraph_CalculateLevels_Errors(t *testing.T) {
	// Test circular dependency detection
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{},
	}

	graph := NewGraph(cfg)

	// Manually create circular dependency
	node1 := &Node{Image: &config.ImageConfig{Name: "node1"}}
	node2 := &Node{Image: &config.ImageConfig{Name: "node2"}}

	// Create circular reference
	node1.Dependencies = []*Node{node2}
	node2.Dependencies = []*Node{node1}
	node1.Dependents = []*Node{node2}
	node2.Dependents = []*Node{node1}

	graph.Nodes = []*Node{node1, node2}

	// Should detect circular dependency
	err := graph.calculateLevels()
	if err == nil {
		t.Error("Expected error for circular dependency")
	}
}

func TestGraph_Build_EmptyConfig(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{},
	}

	graph := NewGraph(cfg)
	err := graph.Build()
	if err != nil {
		t.Errorf("Build() with empty config should not error: %v", err)
	}

	if len(graph.Nodes) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(graph.Nodes))
	}

	if len(graph.Levels) != 0 {
		t.Errorf("Expected 0 levels, got %d", len(graph.Levels))
	}
}

func TestGraph_ComplexDependencyChain(t *testing.T) {
	// Create a complex dependency chain: A -> B -> C -> D
	//                                       \-> E -> F
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"a": {
				Name:          "a",
				Version:       "1.0.0",
				BaseImage:     "ubuntu:22.04",
				Architectures: []string{"linux/amd64"},
				Images: map[string]*config.ImageConfig{
					"b": {
						Name:          "b",
						Parent:        "a",
						Version:       "1.0.0",
						Architectures: []string{"linux/amd64"},
						Images: map[string]*config.ImageConfig{
							"c": {
								Name:          "c",
								Parent:        "b",
								Version:       "1.0.0",
								Architectures: []string{"linux/amd64"},
								Images: map[string]*config.ImageConfig{
									"d": {
										Name:          "d",
										Parent:        "c",
										Version:       "1.0.0",
										Architectures: []string{"linux/amd64"},
									},
								},
							},
							"e": {
								Name:          "e",
								Parent:        "b",
								Version:       "1.0.0",
								Architectures: []string{"linux/amd64"},
								Images: map[string]*config.ImageConfig{
									"f": {
										Name:          "f",
										Parent:        "e",
										Version:       "1.0.0",
										Architectures: []string{"linux/amd64"},
									},
								},
							},
						},
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

	// Should have 6 nodes
	if len(graph.Nodes) != 6 {
		t.Errorf("Expected 6 nodes, got %d", len(graph.Nodes))
	}

	// Should have 4 levels: a -> b -> (c,e) -> (d,f)
	if len(graph.Levels) != 4 {
		t.Errorf("Expected 4 levels, got %d", len(graph.Levels))
	}

	// Verify level assignments
	nodeA := graph.findNode("a")
	if nodeA.Level != 0 {
		t.Errorf("Expected node 'a' at level 0, got %d", nodeA.Level)
	}

	nodeB := graph.findNode("b")
	if nodeB.Level != 1 {
		t.Errorf("Expected node 'b' at level 1, got %d", nodeB.Level)
	}

	nodeC := graph.findNode("c")
	nodeE := graph.findNode("e")
	if nodeC.Level != 2 || nodeE.Level != 2 {
		t.Errorf("Expected nodes 'c' and 'e' at level 2, got %d and %d", nodeC.Level, nodeE.Level)
	}

	nodeD := graph.findNode("d")
	nodeF := graph.findNode("f")
	if nodeD.Level != 3 || nodeF.Level != 3 {
		t.Errorf("Expected nodes 'd' and 'f' at level 3, got %d and %d", nodeD.Level, nodeF.Level)
	}
}

func TestGraph_GetAffectedNodes_ComplexTree(t *testing.T) {
	// Create a tree where changes propagate through multiple paths
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"base": {
				Name:          "base",
				Version:       "1.0.0",
				BaseImage:     "ubuntu:22.04",
				Changed:       true, // Base is changed
				Architectures: []string{"linux/amd64"},
				Images: map[string]*config.ImageConfig{
					"middle1": {
						Name:          "middle1",
						Parent:        "base",
						Version:       "1.0.0",
						Architectures: []string{"linux/amd64"},
						Images: map[string]*config.ImageConfig{
							"leaf1": {
								Name:          "leaf1",
								Parent:        "middle1",
								Version:       "1.0.0",
								Architectures: []string{"linux/amd64"},
							},
						},
					},
					"middle2": {
						Name:          "middle2",
						Parent:        "base",
						Version:       "1.0.0",
						Architectures: []string{"linux/amd64"},
						Images: map[string]*config.ImageConfig{
							"leaf2": {
								Name:          "leaf2",
								Parent:        "middle2",
								Version:       "1.0.0",
								Architectures: []string{"linux/amd64"},
							},
							"leaf3": {
								Name:          "leaf3",
								Parent:        "middle2",
								Version:       "1.0.0",
								Architectures: []string{"linux/amd64"},
							},
						},
					},
				},
			},
			"independent": {
				Name:          "independent",
				Version:       "1.0.0",
				BaseImage:     "alpine:latest",
				Architectures: []string{"linux/amd64"},
			},
		},
	}

	graph := NewGraph(cfg)
	err := graph.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	changedNodes := graph.GetChangedNodes()
	affectedNodes := graph.GetAffectedNodes(changedNodes)

	// Should include all except independent (base + 2 middle + 3 leaves = 6)
	if len(affectedNodes) != 6 {
		t.Errorf("Expected 6 affected nodes, got %d", len(affectedNodes))
	}

	// Verify independent is not affected
	for _, node := range affectedNodes {
		if node.Image.Name == "independent" {
			t.Error("Independent node should not be affected")
		}
	}
}

func TestGraph_GetParentImageTag_EdgeCases(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"test": {
				Name:          "test",
				Version:       "1.0.0",
				BaseImage:     "ubuntu:22.04",
				Architectures: []string{"linux/amd64"},
			},
		},
	}

	graph := NewGraph(cfg)
	graph.Build()

	// Test with node without dependencies (should not test nil as it would panic)
	testNode := graph.findNode("test")
	tag := graph.GetParentImageTag(testNode)
	if tag != "ubuntu:22.04" {
		t.Errorf("Expected 'ubuntu:22.04', got '%s'", tag)
	}
}

func TestGraph_FindNode_NotFound(t *testing.T) {
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"test": {
				Name:          "test",
				Version:       "1.0.0",
				Architectures: []string{"linux/amd64"},
			},
		},
	}

	graph := NewGraph(cfg)
	graph.Build()

	// Test finding non-existent node
	node := graph.findNode("nonexistent")
	if node != nil {
		t.Error("Expected nil for non-existent node")
	}
}

func TestGraph_GetBuildOrder_Empty(t *testing.T) {
	graph := &Graph{
		Nodes:  []*Node{},
		Levels: [][]*Node{},
	}

	order := graph.GetBuildOrder()
	if len(order) != 0 {
		t.Errorf("Expected empty build order, got %d nodes", len(order))
	}
}

func TestGraph_MultipleRootsAndLeaves(t *testing.T) {
	// Test graph with multiple root nodes (no dependencies) and leaf nodes (no dependents)
	cfg := &config.Config{
		Images: map[string]*config.ImageConfig{
			"root1": {
				Name:          "root1",
				Version:       "1.0.0",
				BaseImage:     "ubuntu:22.04",
				Architectures: []string{"linux/amd64"},
				Images: map[string]*config.ImageConfig{
					"child1": {
						Name:          "child1",
						Parent:        "root1",
						Version:       "1.0.0",
						Architectures: []string{"linux/amd64"},
					},
				},
			},
			"root2": {
				Name:          "root2",
				Version:       "1.0.0",
				BaseImage:     "alpine:latest",
				Architectures: []string{"linux/amd64"},
				Images: map[string]*config.ImageConfig{
					"child2": {
						Name:          "child2",
						Parent:        "root2",
						Version:       "1.0.0",
						Architectures: []string{"linux/amd64"},
					},
				},
			},
			"root3": {
				Name:          "root3",
				Version:       "1.0.0",
				BaseImage:     "debian:stable",
				Architectures: []string{"linux/amd64"},
			},
		},
	}

	graph := NewGraph(cfg)
	err := graph.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Should have 5 nodes
	if len(graph.Nodes) != 5 {
		t.Errorf("Expected 5 nodes, got %d", len(graph.Nodes))
	}

	// Should have 2 levels
	if len(graph.Levels) != 2 {
		t.Errorf("Expected 2 levels, got %d", len(graph.Levels))
	}

	// Level 0 should have 3 nodes (all roots)
	if len(graph.Levels[0]) != 3 {
		t.Errorf("Expected 3 nodes at level 0, got %d", len(graph.Levels[0]))
	}

	// Level 1 should have 2 nodes (child1 and child2)
	if len(graph.Levels[1]) != 2 {
		t.Errorf("Expected 2 nodes at level 1, got %d", len(graph.Levels[1]))
	}
}
