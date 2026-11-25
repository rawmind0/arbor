package plan

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/git"
	"github.com/rawmind0/arbor/internal/graph"
)

// Generator creates build plans from a validated configuration: one queue per
// source (the config file's images and each --sources file), each built from
// that source's own dependency graph.
type Generator struct {
	config    *config.Config
	gitClient *git.Client
}

// NewGenerator creates a new plan generator
func NewGenerator(cfg *config.Config) *Generator {
	return &Generator{
		config:    cfg,
		gitClient: git.NewClient("."),
	}
}

// Generate creates a build plan for every image of every source.
func (g *Generator) Generate() (*BuildPlan, error) {
	return g.generate(false)
}

// GenerateChanged creates a build plan for the images marked as changed (see
// config.ChangeDetector) plus everything that depends on them, in every source.
func (g *Generator) GenerateChanged() (*BuildPlan, error) {
	return g.generate(true)
}

// generate builds one queue per source, in config.SourceIDs order. With
// changedOnly, each queue holds only the changed images and their dependents.
func (g *Generator) generate(changedOnly bool) (*BuildPlan, error) {
	plan := &BuildPlan{
		Metadata: g.createMetadata(changedOnly, 0),
		Queues:   []Queue{},
	}

	totalImages := 0
	for _, sourceID := range g.config.SourceIDs() {
		sourceGraph := graph.NewGraph(g.config, sourceID)
		if err := sourceGraph.Build(); err != nil {
			return nil, fmt.Errorf("failed to build graph for source %s: %w", sourceID, err)
		}

		nodes := sourceGraph.GetBuildOrder()
		if changedOnly {
			nodes = buildOrder(sourceGraph.GetAffectedNodes(sourceGraph.GetChangedNodes()))
		}
		if len(nodes) == 0 {
			continue
		}

		plan.Queues = append(plan.Queues, g.buildSourceQueue(sourceID, sourceGraph, nodes))
		totalImages += len(nodes)
	}

	plan.Metadata.TotalImages = totalImages
	g.setLogin(plan)
	return plan, nil
}

// buildOrder sorts nodes by graph level, then name, so a changed-only queue
// lists commands in the same order a full build would.
func buildOrder(nodes []*graph.Node) []*graph.Node {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Level != nodes[j].Level {
			return nodes[i].Level < nodes[j].Level
		}
		return nodes[i].Image.Name < nodes[j].Image.Name
	})
	return nodes
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
func (g *Generator) buildSourceQueue(id string, sourceGraph *graph.Graph, nodes []*graph.Node) Queue {
	commands := make([]BuildCommand, 0, len(nodes))
	for _, node := range nodes {
		commands = append(commands, g.createBuildCommand(sourceGraph, node))
	}
	return Queue{ID: id, Batches: g.createBatches(nodes), Commands: commands}
}

// createBuildCommand creates the build (and, when pushing, push) step sequence
// for one image: a single multi-platform build for all of its architectures.
func (g *Generator) createBuildCommand(sourceGraph *graph.Graph, node *graph.Node) BuildCommand {
	img := node.Image
	reg := g.config.Config.Registry
	cmd := BuildCommand{
		ID:             img.Name,
		ImageName:      img.Name,
		ImageTag:       node.ImageTag, // canonical [url/]owner/name:version
		Architecture:   strings.Join(img.Architectures, ","),
		DockerfilePath: img.GetDockerfilePath(g.config.Config.Dockerfiles),
		BuildContext:   img.GetBuildContext(g.config.Config.Dockerfiles),
		BuildArgs:      buildArgs(sourceGraph, node),
		Labels:         g.mergeLabels(img),
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

// buildArgs returns the image's configured build args plus BASE, the image its
// Dockerfile builds FROM: the parent's canonical tag for a child, base_image for
// a root. A root without base_image gets no BASE, so the Dockerfile's own
// `ARG BASE=<default>` (or hardcoded FROM) applies — an empty --build-arg would
// override that default. img.Args is copied, never mutated.
func buildArgs(sourceGraph *graph.Graph, node *graph.Node) map[string]string {
	args := make(map[string]string, len(node.Image.Args)+1)
	for k, v := range node.Image.Args {
		args[k] = v
	}
	if base := sourceGraph.GetParentImageTag(node); base != "" {
		args[config.BaseArg] = base
	}
	return args
}

// generateFallbackCommand creates a docker-style multi-platform build when the
// configured builder is not found.
func (g *Generator) generateFallbackCommand(opts builder.BuildOptions) []string {
	args := []string{g.config.Config.Builder, "build"}
	if len(opts.Platforms) > 0 {
		args = append(args, "--platform", strings.Join(opts.Platforms, ","))
	}
	args = append(args, "-f", opts.DockerfilePath, "-t", opts.ImageTag)
	for _, key := range slices.Sorted(maps.Keys(opts.BuildArgs)) {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, opts.BuildArgs[key]))
	}
	for _, key := range slices.Sorted(maps.Keys(opts.Labels)) {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, opts.Labels[key]))
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
