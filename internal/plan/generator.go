package plan

import (
	"fmt"
	"strings"
	"time"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/git"
	"github.com/rawmind0/arbor/internal/graph"
)

// Generator creates build plans from configuration and dependency graph
type Generator struct {
	config    *config.Config
	graph     *graph.Graph
	gitClient *git.Client
}

// NewGenerator creates a new plan generator
func NewGenerator(cfg *config.Config, g *graph.Graph) *Generator {
	return &Generator{
		config:    cfg,
		graph:     g,
		gitClient: git.NewClient("."),
	}
}

// Generate creates a complete build plan
func (g *Generator) Generate() (*BuildPlan, error) {
	// Always use multi-source approach for consistency
	return g.GenerateMultiSource()
}

// GenerateChanged creates a build plan for changed images only
func (g *Generator) GenerateChanged() (*BuildPlan, error) {
	changedNodes := g.graph.GetChangedNodes()
	if len(changedNodes) == 0 {
		return &BuildPlan{
			Metadata: g.createMetadata(true, 0),
			Queues:   []Queue{},
		}, nil
	}

	affectedNodes := g.graph.GetAffectedNodes(changedNodes)
	return g.GenerateForNodes(affectedNodes)
}

// GenerateForNodes creates a build plan for specific nodes
// This is kept for tests that might use it directly
func (g *Generator) GenerateForNodes(nodes []*graph.Node) (*BuildPlan, error) {
	if len(nodes) == 0 {
		return &BuildPlan{
			Metadata: g.createMetadata(false, 0),
			Queues:   []Queue{},
		}, nil
	}

	plan := &BuildPlan{
		Metadata: g.createMetadata(false, len(nodes)),
		Queues:   []Queue{g.buildSourceQueue("main", nodes)},
	}
	g.setLogin(plan)

	return plan, nil
}

// GenerateMultiSource creates a build plan with multiple queues
func (g *Generator) GenerateMultiSource() (*BuildPlan, error) {
	plan := &BuildPlan{
		Metadata: g.createMetadata(false, 0),
		Queues:   []Queue{},
	}

	totalImages := 0

	// If no sources defined, use main images as single source
	if len(g.config.Sources) == 0 {
		g.config.Sources = map[string]*config.ImageSource{
			"main": {
				Images: g.config.Images,
			},
		}
	}

	// Generate a queue for each source
	for sourceID, source := range g.config.Sources {
		// Create a temporary config for this source
		tempConfig := &config.Config{
			Config:    g.config.Config,
			Images:    source.Images,
			IsGitRepo: g.config.IsGitRepo,
		}

		// Build graph for this source
		sourceGraph := graph.NewGraph(tempConfig)
		if err := sourceGraph.Build(); err != nil {
			return nil, fmt.Errorf("failed to build graph for source %s: %w", sourceID, err)
		}

		// Get nodes in build order
		nodes := sourceGraph.GetBuildOrder()
		if len(nodes) == 0 {
			continue
		}

		// One queue per source (architectures are handled within each image task)
		plan.Queues = append(plan.Queues, g.buildSourceQueue(sourceID, nodes))

		totalImages += len(nodes)
	}

	// Update metadata with total images
	plan.Metadata.TotalImages = totalImages
	g.setLogin(plan)

	return plan, nil
}

// builderMode returns the configured builder mode (defaulting to normal).
func (g *Generator) builderMode() builder.BuilderMode {
	mode := builder.BuilderMode(g.config.Config.Mode)
	if mode == "" {
		mode = builder.ModeNormal
	}
	return mode
}

// setLogin attaches the one-time registry login step to the plan when pushing.
func (g *Generator) setLogin(plan *BuildPlan) {
	if !g.config.Config.Push {
		return
	}
	if b, err := builder.GetBuilder(g.config.Config.Builder, g.builderMode()); err == nil {
		plan.Login = [][]string{b.LoginCommand(g.config.Config.Registry.URL)}
	}
}

// buildSourceQueue builds one queue for a source: one command per image, batches
// by graph level. Each image command handles all of its architectures.
func (g *Generator) buildSourceQueue(id string, nodes []*graph.Node) Queue {
	commands := make([]BuildCommand, 0, len(nodes))
	for _, node := range nodes {
		commands = append(commands, g.createBuildCommand(node))
	}
	return Queue{ID: id, Batches: g.createBatches(nodes), Commands: commands}
}

// createBuildCommand creates the build (and, when pushing, push) step sequence
// for one image: a single multi-platform build for all of its architectures.
func (g *Generator) createBuildCommand(node *graph.Node) BuildCommand {
	img := node.Image
	reg := g.config.Config.Registry
	cmd := BuildCommand{
		ID:             img.Name,
		ImageName:      img.Name,
		ImageTag:       node.ImageTag, // canonical [url/]owner/name:version
		Architecture:   strings.Join(img.Architectures, ","),
		DockerfilePath: img.GetBuildPath(g.config.Config.Dockerfiles.Destination),
		BuildContext:   ".",
		BuildArgs:      img.Args,
		Environment:    img.Envs,
		Labels:         g.mergeLabels(img),
		Timeout:        600,
	}

	opts := builder.BuildOptions{
		ImageTag:       reg.Tag(img.Name, img.Version),
		DockerfilePath: cmd.DockerfilePath,
		BuildContext:   cmd.BuildContext,
		Platforms:      img.Architectures,
		Push:           g.config.Config.Push,
		BuildArgs:      cmd.BuildArgs,
		Labels:         cmd.Labels,
	}

	bld, err := builder.GetBuilder(g.config.Config.Builder, g.builderMode())
	if err != nil {
		cmd.Command = g.generateFallbackCommand(opts)
		return cmd
	}
	// Multi-arch images must be pushed (a manifest cannot be loaded locally);
	// single-arch images build and load locally so children resolve FROM.
	if len(img.Architectures) > 1 {
		cmd.Steps = bld.BuildMultiSteps(opts)
	} else {
		cmd.Steps = bld.BuildSteps(opts)
	}
	cmd.Command = cmd.Steps[0] // first build step, for Validate/formatter display
	return cmd
}

// generateFallbackCommand creates a docker-style multi-platform build when the
// configured builder is not found.
func (g *Generator) generateFallbackCommand(opts builder.BuildOptions) []string {
	args := []string{g.config.Config.Builder, "build"}
	if len(opts.Platforms) > 0 {
		args = append(args, "--platform", strings.Join(opts.Platforms, ","))
	}
	args = append(args, "-f", opts.DockerfilePath, "-t", opts.ImageTag)
	for key, value := range opts.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}
	for key, value := range opts.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}
	return append(args, opts.BuildContext)
}

// createBatches groups nodes by execution level into batches
// Each batch contains tasks that can run in parallel (same graph level)
func (g *Generator) createBatches(nodes []*graph.Node) []Batch {
	// Find max level
	maxLevel := 0
	for _, node := range nodes {
		if node.Level > maxLevel {
			maxLevel = node.Level
		}
	}

	// Group by level into batches
	levels := make([][]string, maxLevel+1)
	for _, node := range nodes {
		levels[node.Level] = append(levels[node.Level], node.ImageTag)
	}

	// Convert to batches, removing empty ones
	result := []Batch{}
	for _, level := range levels {
		if len(level) > 0 {
			result = append(result, Batch{Tasks: level})
		}
	}

	return result
}

// createMetadata creates plan metadata
func (g *Generator) createMetadata(changedOnly bool, totalImages int) PlanMetadata {
	metadata := PlanMetadata{
		Version:     "1.0.0",
		Generated:   time.Now(),
		ConfigFile:  "config.yaml", // Default config file name
		Builder:     g.config.Config.Builder,
		ChangedOnly: changedOnly,
		TotalImages: totalImages,
		Workers:     g.config.Config.Workers,
		TaskTimeout: g.config.GetTaskTimeout(),
	}

	// Add git info if in a git repo
	if g.gitClient.IsGitRepo() {
		gitInfo := &GitInfo{}

		if branch, err := g.gitClient.GetCurrentBranch(); err == nil {
			gitInfo.Branch = branch
		}

		if clean, err := g.gitClient.IsCleanWorkingTree(); err == nil {
			gitInfo.IsClean = clean
		}

		metadata.GitInfo = gitInfo
	}

	return metadata
}

// mergeLabels merges global and image-specific labels
func (g *Generator) mergeLabels(img *config.ImageConfig) map[string]string {
	merged := make(map[string]string)

	// Start with global labels
	for k, v := range g.config.Config.Labels {
		merged[k] = v
	}

	// Add image-specific labels
	for k, v := range img.Labels {
		merged[k] = v
	}

	// Add automatic labels
	merged["image.name"] = img.Name
	merged["image.version"] = img.Version
	if img.Parent != "" {
		merged["image.parent"] = img.Parent
	}
	merged["build.date"] = time.Now().Format(time.RFC3339)

	return merged
}
