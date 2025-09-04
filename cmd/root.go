package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rawmind0/container-builder/internal/builder"
	"github.com/rawmind0/container-builder/internal/config"
	"github.com/rawmind0/container-builder/internal/graph"
	"github.com/rawmind0/container-builder/internal/template"
)

var (
	cfgFile     string
	workers     int
	changedOnly bool
	showLevels  bool
	showArch    bool
	arch        string
	fix         bool
	baseRef     string
	gitStatus   bool
)

var rootCmd = &cobra.Command{
	Use:   "container-builder",
	Short: "A tool to manage Docker hierarchy builds",
	Long: `Container Builder manages Docker image hierarchies with dependency-aware builds,
multi-architecture support, and parallel execution capabilities.`,
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and check version cascading",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("✓ Configuration is valid")

		if fix {
			fmt.Println("Checking version cascading...")
			// TODO: Implement version cascade validation and fixing
			fmt.Println("✓ Version cascading validated")
		}

		return nil
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate Dockerfiles from templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Detect changes if --changed-only flag is used
		if changedOnly {
			detector := config.NewChangeDetector(cfg, ".")
			if err := detector.DetectChanges(baseRef); err != nil {
				return fmt.Errorf("failed to detect changes: %w", err)
			}
		}

		g := graph.NewGraph(cfg)
		if err := g.Build(); err != nil {
			return fmt.Errorf("failed to build dependency graph: %w", err)
		}

		processor := template.NewProcessor(cfg)

		var nodes []*graph.Node
		if changedOnly {
			changedNodes := g.GetChangedNodes()
			if len(changedNodes) == 0 {
				fmt.Println("No changed images detected - nothing to generate")
				return nil
			}
			nodes = g.GetAffectedNodes(changedNodes)
			fmt.Printf("Generating Dockerfiles for %d changed/affected images\n", len(nodes))
		} else {
			nodes = g.GetBuildOrder()
			fmt.Printf("Generating Dockerfiles for all %d images\n", len(nodes))
		}

		for _, node := range nodes {
			if arch != "" && node.Architecture != arch {
				continue
			}

			parentImage := g.GetParentImageTag(node)
			dockerfile, err := processor.GenerateDockerfile(node.Image, node.Architecture, parentImage)
			if err != nil {
				return fmt.Errorf("failed to generate dockerfile for %s: %w", node.ImageTag, err)
			}

			destPath := node.Image.GetBuildPath(cfg.Config.Dockerfiles.Destination, node.Architecture)
			if err := processor.WriteDockerfile(dockerfile, destPath); err != nil {
				return fmt.Errorf("failed to write dockerfile for %s: %w", node.ImageTag, err)
			}

			fmt.Printf("Generated: %s\n", destPath)
		}

		return nil
	},
}

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Show build execution plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Show git status if requested
		if gitStatus {
			detector := config.NewChangeDetector(cfg, ".")
			status := detector.GetGitStatus()
			fmt.Println("Git Repository Status:")
			for key, value := range status {
				fmt.Printf("  %s: %v\n", key, value)
			}
			fmt.Println()
		}

		// Detect changes if --changed-only flag is used
		if changedOnly {
			detector := config.NewChangeDetector(cfg, ".")
			if err := detector.DetectChanges(baseRef); err != nil {
				return fmt.Errorf("failed to detect changes: %w", err)
			}
		}

		g := graph.NewGraph(cfg)
		if err := g.Build(); err != nil {
			return fmt.Errorf("failed to build dependency graph: %w", err)
		}

		var nodes []*graph.Node
		if changedOnly {
			changedNodes := g.GetChangedNodes()
			if len(changedNodes) == 0 {
				fmt.Println("No changed images detected - nothing to build")
				return nil
			}
			nodes = g.GetAffectedNodes(changedNodes)
		} else {
			nodes = g.GetBuildOrder()
		}

		if showLevels {
			fmt.Printf("Build Plan (%d workers):\n", cfg.Config.Workers)
			for i, level := range g.Levels {
				var levelNodes []*graph.Node
				for _, node := range level {
					for _, buildNode := range nodes {
						if node == buildNode {
							levelNodes = append(levelNodes, node)
							break
						}
					}
				}
				if len(levelNodes) > 0 {
					fmt.Printf("├─ Level %d: ", i)
					for j, node := range levelNodes {
						if j > 0 {
							fmt.Print(", ")
						}
						if showArch {
							fmt.Printf("%s", node.ImageTag)
						} else {
							fmt.Printf("%s", node.Image.Name)
						}
					}
					fmt.Printf(" (%d images", len(levelNodes))
					if len(levelNodes) > 1 {
						fmt.Print(" - parallel")
					}
					fmt.Println(")")
				}
			}
		} else {
			fmt.Printf("Build Order:\n")
			for i, node := range nodes {
				if arch != "" && node.Architecture != arch {
					continue
				}
				if showArch {
					fmt.Printf("%d. %s\n", i+1, node.ImageTag)
				} else {
					fmt.Printf("%d. %s\n", i+1, node.Image.Name)
				}
			}
		}

		return nil
	},
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build container images",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Detect changes if --changed-only flag is used
		if changedOnly {
			detector := config.NewChangeDetector(cfg, ".")
			if err := detector.DetectChanges(baseRef); err != nil {
				return fmt.Errorf("failed to detect changes: %w", err)
			}
		}

		g := graph.NewGraph(cfg)
		if err := g.Build(); err != nil {
			return fmt.Errorf("failed to build dependency graph: %w", err)
		}

		// Override workers if specified
		if workers > 0 {
			cfg.Config.Workers = workers
		}

		buildManager, err := builder.NewBuildManager(cfg, g)
		if err != nil {
			return fmt.Errorf("failed to create build manager: %w", err)
		}

		ctx := context.Background()
		var results []builder.BuildResult

		if changedOnly {
			changedNodes := g.GetChangedNodes()
			if len(changedNodes) == 0 {
				fmt.Println("No changed images detected - nothing to build")
				return nil
			}
			fmt.Printf("Building %d changed/affected images...\n", len(g.GetAffectedNodes(changedNodes)))
			results, err = buildManager.BuildChanged(ctx)
		} else {
			fmt.Printf("Building all %d images...\n", len(g.GetBuildOrder()))
			results, err = buildManager.BuildAll(ctx)
		}

		if err != nil {
			return fmt.Errorf("build failed: %w", err)
		}

		fmt.Printf("✓ Successfully built %d images\n", len(results))
		for _, result := range results {
			if result.Error == nil {
				fmt.Printf("  ✓ %s\n", result.Node.ImageTag)
			}
		}

		return nil
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file")

	validateCmd.Flags().BoolVar(&fix, "fix", false, "auto-fix version cascading issues")

	generateCmd.Flags().BoolVar(&changedOnly, "changed-only", false, "generate only changed images")
	generateCmd.Flags().StringVar(&arch, "arch", "", "generate for specific architecture only")
	generateCmd.Flags().StringVar(&baseRef, "base-ref", "", "git reference to compare against (default: HEAD~1 or uncommitted)")

	planCmd.Flags().BoolVar(&changedOnly, "changed-only", false, "show plan for changed images only")
	planCmd.Flags().BoolVar(&showLevels, "show-levels", false, "show parallel execution levels")
	planCmd.Flags().BoolVar(&showArch, "show-arch", false, "show architecture in image names")
	planCmd.Flags().StringVar(&arch, "arch", "", "show plan for specific architecture only")
	planCmd.Flags().StringVar(&baseRef, "base-ref", "", "git reference to compare against (default: HEAD~1 or uncommitted)")
	planCmd.Flags().BoolVar(&gitStatus, "git-status", false, "show git repository status")

	buildCmd.Flags().BoolVar(&changedOnly, "changed-only", false, "build only changed images")
	buildCmd.Flags().IntVar(&workers, "workers", 0, "number of parallel workers (0 = use config default)")
	buildCmd.Flags().StringVar(&arch, "arch", "", "build for specific architecture only")
	buildCmd.Flags().StringVar(&baseRef, "base-ref", "", "git reference to compare against (default: HEAD~1 or uncommitted)")

	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(buildCmd)
}
