package graph

import (
	"fmt"
	"sort"

	"github.com/rawmind0/arbor/internal/config"
)

// Node is one image (all of its architectures). Architecture is no longer a
// graph dimension — img.Architectures lists the platforms the image builds for.
type Node struct {
	Image        *config.ImageConfig
	ImageTag     string // name:version (the manifest tag for multi-arch)
	Dependencies []*Node
	Dependents   []*Node
	Level        int
	Changed      bool
}

type Graph struct {
	Nodes  []*Node
	Levels [][]*Node
	config *config.Config
}

func NewGraph(cfg *config.Config) *Graph {
	return &Graph{
		config: cfg,
	}
}

func (g *Graph) Build() error {
	// Create one node per image (architectures live on the image)
	g.createNodes(g.config.Images, "")

	// Build dependencies
	g.buildDependencies()

	// Calculate levels for parallel execution
	if err := g.calculateLevels(); err != nil {
		return err
	}

	return nil
}

func (g *Graph) createNodes(images map[string]*config.ImageConfig, parent string) {
	for name, img := range images {
		node := &Node{
			Image:        img,
			ImageTag:     g.buildImageTag(img),
			Dependencies: []*Node{},
			Dependents:   []*Node{},
			Changed:      img.Changed,
		}
		g.Nodes = append(g.Nodes, node)

		// Process child images recursively
		if img.Images != nil {
			g.createNodes(img.Images, name)
		}
	}
}

func (g *Graph) buildDependencies() {
	for _, node := range g.Nodes {
		if node.Image.Parent != "" {
			parentNode := g.findNode(node.Image.Parent)
			if parentNode != nil {
				node.Dependencies = append(node.Dependencies, parentNode)
				parentNode.Dependents = append(parentNode.Dependents, node)
			}
		}
	}
}

func (g *Graph) calculateLevels() error {
	// Reset levels
	for _, node := range g.Nodes {
		node.Level = -1
	}

	// Calculate levels using topological sorting
	g.Levels = [][]*Node{}
	visited := make(map[*Node]bool)

	for {
		currentLevel := []*Node{}

		for _, node := range g.Nodes {
			if visited[node] || node.Level != -1 {
				continue
			}

			// Check if all dependencies are satisfied
			allDepsSatisfied := true
			for _, dep := range node.Dependencies {
				if !visited[dep] {
					allDepsSatisfied = false
					break
				}
			}

			if allDepsSatisfied {
				node.Level = len(g.Levels)
				currentLevel = append(currentLevel, node)
			}
		}

		if len(currentLevel) == 0 {
			break
		}

		// Mark nodes as visited
		for _, node := range currentLevel {
			visited[node] = true
		}

		g.Levels = append(g.Levels, currentLevel)
	}

	// Check for circular dependencies
	for _, node := range g.Nodes {
		if node.Level == -1 {
			return fmt.Errorf("circular dependency detected involving image %s", node.Image.Name)
		}
	}

	return nil
}

func (g *Graph) findNode(imageName string) *Node {
	for _, node := range g.Nodes {
		if node.Image.Name == imageName {
			return node
		}
	}
	return nil
}

// buildImageTag returns the canonical, namespace-qualified tag
// ("[url/]owner/name:version") used everywhere as an image's identity: the
// build -t target, the FROM reference for child images, batch ids, and
// reporting. Centralizing on RegistryConfig.Tag keeps local and registry
// references consistent.
func (g *Graph) buildImageTag(img *config.ImageConfig) string {
	return g.config.Config.Registry.Tag(img.Name, img.Version)
}

func (g *Graph) GetBuildOrder() []*Node {
	var order []*Node
	for _, level := range g.Levels {
		order = append(order, level...)
	}
	return order
}

func (g *Graph) GetChangedNodes() []*Node {
	var changed []*Node
	for _, node := range g.Nodes {
		if node.Changed {
			changed = append(changed, node)
		}
	}
	return changed
}

func (g *Graph) GetAffectedNodes(changedNodes []*Node) []*Node {
	affected := make(map[*Node]bool)

	// Add all changed nodes
	for _, node := range changedNodes {
		affected[node] = true
	}

	// Add all dependents recursively
	var addDependents func(*Node)
	addDependents = func(node *Node) {
		for _, dependent := range node.Dependents {
			if !affected[dependent] {
				affected[dependent] = true
				addDependents(dependent)
			}
		}
	}

	for _, node := range changedNodes {
		addDependents(node)
	}

	// Convert map to slice and sort by level
	result := make([]*Node, 0, len(affected))
	for node := range affected {
		result = append(result, node)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Level < result[j].Level
	})

	return result
}

func (g *Graph) GetParentImageTag(node *Node) string {
	if len(node.Dependencies) == 0 {
		// Root image, use base_image if specified
		if node.Image.BaseImage != "" {
			return node.Image.BaseImage
		}
		return ""
	}

	// Use parent node's image tag
	parent := node.Dependencies[0]
	return parent.ImageTag
}
