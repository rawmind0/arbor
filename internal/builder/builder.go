package builder

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	"github.com/rawmind0/container-builder/internal/config"
	"github.com/rawmind0/container-builder/internal/graph"
	"github.com/rawmind0/container-builder/internal/template"
)

type Builder interface {
	Build(ctx context.Context, node *graph.Node, dockerfile string) error
	GetName() string
}

type DockerBuilder struct{}

func NewDockerBuilder() *DockerBuilder {
	return &DockerBuilder{}
}

func (d *DockerBuilder) Build(ctx context.Context, node *graph.Node, dockerfile string) error {
	args := []string{
		"build",
		"--platform", node.Architecture,
		"-f", dockerfile,
		"-t", node.ImageTag,
	}

	// Add build args
	for key, value := range node.Image.Args {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	// Add current directory as build context
	args = append(args, ".")

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = nil // Will be handled by caller for logging
	cmd.Stderr = nil

	return cmd.Run()
}

func (d *DockerBuilder) GetName() string {
	return "docker"
}

type PodmanBuilder struct{}

func NewPodmanBuilder() *PodmanBuilder {
	return &PodmanBuilder{}
}

func (p *PodmanBuilder) Build(ctx context.Context, node *graph.Node, dockerfile string) error {
	args := []string{
		"build",
		"--platform", node.Architecture,
		"-f", dockerfile,
		"-t", node.ImageTag,
	}

	// Add build args
	for key, value := range node.Image.Args {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	// Add current directory as build context
	args = append(args, ".")

	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Stdout = nil // Will be handled by caller for logging
	cmd.Stderr = nil

	return cmd.Run()
}

func (p *PodmanBuilder) GetName() string {
	return "podman"
}

type BuildManager struct {
	builder Builder
	config  *config.Config
	graph   *graph.Graph
	workers int
}

func NewBuildManager(cfg *config.Config, graph *graph.Graph) (*BuildManager, error) {
	var builder Builder

	switch cfg.Config.Builder {
	case "docker":
		builder = NewDockerBuilder()
	case "podman":
		builder = NewPodmanBuilder()
	default:
		return nil, fmt.Errorf("unsupported builder: %s", cfg.Config.Builder)
	}

	return &BuildManager{
		builder: builder,
		config:  cfg,
		graph:   graph,
		workers: cfg.Config.Workers,
	}, nil
}

type BuildResult struct {
	Node  *graph.Node
	Error error
}

func (bm *BuildManager) BuildAll(ctx context.Context) ([]BuildResult, error) {
	return bm.buildNodes(ctx, bm.graph.GetBuildOrder())
}

func (bm *BuildManager) BuildChanged(ctx context.Context) ([]BuildResult, error) {
	changedNodes := bm.graph.GetChangedNodes()
	affectedNodes := bm.graph.GetAffectedNodes(changedNodes)
	return bm.buildNodes(ctx, affectedNodes)
}

func (bm *BuildManager) buildNodes(ctx context.Context, nodes []*graph.Node) ([]BuildResult, error) {
	results := make([]BuildResult, 0, len(nodes))
	resultsCh := make(chan BuildResult, len(nodes))

	// Process nodes level by level for proper dependency ordering
	for _, level := range bm.graph.Levels {
		var levelNodes []*graph.Node
		for _, node := range level {
			// Only include nodes that are in our build list
			for _, buildNode := range nodes {
				if node == buildNode {
					levelNodes = append(levelNodes, node)
					break
				}
			}
		}

		if len(levelNodes) == 0 {
			continue
		}

		// Build level nodes in parallel with worker pool
		if err := bm.buildLevel(ctx, levelNodes, resultsCh); err != nil {
			return results, err
		}

		// Collect results for this level
		for i := 0; i < len(levelNodes); i++ {
			result := <-resultsCh
			results = append(results, result)
			if result.Error != nil {
				return results, fmt.Errorf("build failed for %s: %w", result.Node.ImageTag, result.Error)
			}
		}
	}

	return results, nil
}

func (bm *BuildManager) buildLevel(ctx context.Context, nodes []*graph.Node, resultsCh chan<- BuildResult) error {
	// Create worker pool
	jobs := make(chan *graph.Node, len(nodes))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < min(bm.workers, len(nodes)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range jobs {
				err := bm.buildNode(ctx, node)
				resultsCh <- BuildResult{Node: node, Error: err}
			}
		}()
	}

	// Send jobs
	for _, node := range nodes {
		jobs <- node
	}
	close(jobs)

	// Wait for all workers to complete
	wg.Wait()

	return nil
}

func (bm *BuildManager) buildNode(ctx context.Context, node *graph.Node) error {
	// Generate dockerfile content
	processor := template.NewProcessor(bm.config)
	parentImage := bm.graph.GetParentImageTag(node)

	dockerfile, err := processor.GenerateDockerfile(node.Image, node.Architecture, parentImage)
	if err != nil {
		return fmt.Errorf("failed to generate dockerfile: %w", err)
	}

	// Write dockerfile to build directory
	dockerfilePath := node.Image.GetBuildPath(bm.config.Config.Dockerfiles.Destination, node.Architecture)
	if err := processor.WriteDockerfile(dockerfile, dockerfilePath); err != nil {
		return fmt.Errorf("failed to write dockerfile: %w", err)
	}

	// Build image
	return bm.builder.Build(ctx, node, dockerfilePath)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
