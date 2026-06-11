package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/config"
	"github.com/rawmind0/arbor/internal/generate"
	"github.com/rawmind0/arbor/internal/graph"
	"github.com/rawmind0/arbor/internal/logging"
	"github.com/rawmind0/arbor/internal/plan"
	"github.com/rawmind0/arbor/internal/version"
)

var (
	cfgFile          string
	workers          int
	changedOnly      bool
	showLevels       bool
	showArch         bool
	arch             string
	fix              bool
	baseRef          string
	gitStatus        bool
	outputPath       string
	planFormat       string
	planFile         string
	skipVersionCheck bool
	sourcesGlob      []string
	taskTimeout      string
	dryRun           bool
	withErrors       bool
	push             bool
	genFirst         bool
	logLevel         = logging.NewLevelFlag(logging.DefaultLevel())
)

// SetPlatform sets the default platform for config
func SetPlatform(platform string) {
	config.DefaultPlatform = platform
}

// loadConfigured maps this command's flags into a partial-config override map and
// asks the config package for a fully-assembled config — the config owns load,
// defaults, merging, and git constraints. A flag is included only when the user
// set it (so the YAML value stands otherwise). --sources is the one exception: it
// has no single config field, so it loads its extra image files afterwards.
func loadConfigured(cmd *cobra.Command) (*config.Config, error) {
	c := map[string]interface{}{}
	if cmd.Flags().Changed("skip-version-check") {
		c["skip_version_check"] = skipVersionCheck
	}
	if cmd.Flags().Changed("changed-only") {
		c["changed_only"] = changedOnly
	}
	if cmd.Flags().Changed("base-ref") {
		c["base_ref"] = baseRef
	}
	if cmd.Flags().Changed("push") {
		c["push"] = push
	}
	if taskTimeout != "" {
		c["task_timeout"] = taskTimeout
	}
	if dryRun {
		c["dry_run"] = true
		if withErrors {
			c["mode"] = string(builder.ModeMockError)
		} else {
			c["mode"] = string(builder.ModeMock)
		}
	}

	var overrides map[string]interface{}
	if len(c) > 0 {
		overrides = map[string]interface{}{"config": c}
	}

	cfg, err := config.LoadConfig(cfgFile, overrides)
	if err != nil {
		return nil, err
	}

	if len(sourcesGlob) > 0 {
		if err := cfg.LoadAdditionalImages(cfgFile, sourcesGlob); err != nil {
			return nil, fmt.Errorf("failed to load additional images: %w", err)
		}
	}
	return cfg, nil
}

// resolvePlan validates the config, applies git/changed-only handling, builds
// the dependency graph, and generates the build plan. The returned bool is true
// when there is nothing to build (changed-only with no changes). Shared by the
// plan and build commands.
func resolvePlan(cfg *config.Config) (*plan.BuildPlan, bool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, false, err
	}

	// With --generate, render the Dockerfiles first so the plan/build uses
	// up-to-date ones. Reuses the generate module — no duplicated logic.
	if genFirst {
		if _, err := generate.New(cfg).Run(); err != nil {
			return nil, false, fmt.Errorf("failed to generate Dockerfiles: %w", err)
		}
	}

	// Detect changes when changed-only is enabled (config is the source of truth).
	if cfg.Config.ChangedOnly {
		detector := config.NewChangeDetector(cfg, ".")
		if err := detector.DetectChanges(cfg.Config.BaseRef); err != nil {
			return nil, false, fmt.Errorf("failed to detect changes: %w", err)
		}
	}

	// Build dependency graph
	g := graph.NewGraph(cfg)
	if err := g.Build(); err != nil {
		return nil, false, fmt.Errorf("failed to build dependency graph: %w", err)
	}

	// Generate execution plan
	planGenerator := plan.NewGenerator(cfg, g)
	if cfg.Config.ChangedOnly {
		buildPlan, err := planGenerator.GenerateChanged()
		if err != nil {
			return nil, false, fmt.Errorf("failed to generate plan for changed images: %w", err)
		}
		return buildPlan, buildPlan.GetTotalCommands() == 0, nil
	}

	buildPlan, err := planGenerator.Generate()
	if err != nil {
		return nil, false, fmt.Errorf("failed to generate plan: %w", err)
	}
	return buildPlan, false, nil
}

var rootCmd = &cobra.Command{
	Use:   "arbor",
	Short: "A tool to manage Docker hierarchy builds",
	Long: `Arbor manages Docker image hierarchies with dependency-aware builds,
multi-architecture support, and parallel execution capabilities.`,
	// Configure the logger from --log-level before any command runs
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logging.Configure(logLevel.Level())
	},
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and check version cascading",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(cfgFile, nil)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Println("✓ Configuration is valid")

		// Validate versions
		validator := version.NewValidator(cfg)
		result, err := validator.Validate()
		if err != nil {
			return fmt.Errorf("failed to validate versions: %w", err)
		}

		// Show validation results
		for _, warning := range result.Warnings {
			log.Warn().Msg(warning)
		}

		if len(result.Errors) > 0 {
			for _, e := range result.Errors {
				log.Error().Msg(e)
			}
			return fmt.Errorf("validation failed")
		}

		if len(result.UpdatesRequired) > 0 {
			fmt.Println("\n📋 Version updates required:")
			for _, update := range result.UpdatesRequired {
				fmt.Printf("  • %s: %s → %s (%s)\n",
					update.ImageName, update.OldVersion, update.NewVersion, update.Reason)
			}

			if fix {
				// The version module owns apply + save (and logs what it did).
				if err := validator.Apply(result); err != nil {
					return err
				}
			} else {
				fmt.Println("\n💡 Use --fix flag to apply these updates")
			}
		} else {
			fmt.Println("✓ All versions are consistent")
		}

		return nil
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate Dockerfiles from templates",
	Long: `Generate Dockerfiles from templates for all images.
This is a lightweight, idempotent operation: it always regenerates the full set
of Dockerfiles (the destination is cleaned first), so the output reflects exactly
the current config. Incremental selection lives in 'plan'/'build', not here.
By default, validates and automatically updates versions to maintain consistency.
Use --skip-version-check to bypass version validation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigured(cmd)
		if err != nil {
			return err
		}

		// All generation logic (version reconcile + idempotent render) lives in
		// the generate module; this command only loads the config and calls it.
		count, err := generate.New(cfg).Run()
		if err != nil {
			return err
		}

		fmt.Printf("\n✓ Generated %d Dockerfiles total\n", count)
		return nil
	},
}

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Create build execution plan",
	Long: `Create an optimized build execution plan based on the dependency graph.
The plan determines the optimal execution order and parallelization strategy.
It assumes Dockerfiles have already been generated.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigured(cmd)
		if err != nil {
			return err
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

		buildPlan, done, err := resolvePlan(cfg)
		if err != nil {
			return err
		}
		if done {
			fmt.Println("No changed images detected - nothing to build")
			return nil
		}

		// Output plan
		if outputPath != "" {
			// Save to file
			if err := buildPlan.SaveToFile(outputPath, planFormat); err != nil {
				return fmt.Errorf("failed to save plan: %w", err)
			}
			fmt.Printf("✓ Plan saved to %s\n", outputPath)
			fmt.Printf("  Total commands: %d\n", buildPlan.GetTotalCommands())
			fmt.Printf("  Execution batches: %d\n", buildPlan.GetTotalBatches())
			fmt.Printf("  Max parallelism: %d workers\n", buildPlan.Metadata.Workers)
		} else {
			// Display to stdout
			formatter := plan.NewFormatter(buildPlan, os.Stdout)
			if err := formatter.Format(planFormat); err != nil {
				return fmt.Errorf("failed to format plan: %w", err)
			}
		}

		return nil
	},
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build container images",
	Long: `Build container images using an execution plan.
Can either load a plan from file or generate one on-the-fly.
Assumes Dockerfiles have been generated with the 'generate' command.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		var buildPlan *plan.BuildPlan

		// Load or generate plan
		if planFile != "" {
			// Load plan from file
			var err error
			buildPlan, err = plan.LoadFromFile(planFile)
			if err != nil {
				return fmt.Errorf("failed to load plan: %w", err)
			}
			log.Info().Str("plan", planFile).Int("commands", buildPlan.GetTotalCommands()).Int("workers", buildPlan.Metadata.Workers).Msg("loaded plan from file")
		} else {
			// Generate plan on-the-fly
			cfg, err := loadConfigured(cmd)
			if err != nil {
				return err
			}

			// Validate the builder now that the mode is known: real builds verify
			// docker/podman is available; mock/dry-run modes skip the daemon probe.
			if err := cfg.ValidateBuilder(); err != nil {
				log.Warn().Err(err).Msg("builder validation failed; you may encounter issues during the build process")
			}

			// A multi-arch image must be pushed (its manifest cannot be loaded
			// locally); enforce this only at build time so plan/generate stay
			// lenient previews.
			if err := cfg.ValidateMultiArchPush(); err != nil {
				return err
			}

			var done bool
			buildPlan, done, err = resolvePlan(cfg)
			if err != nil {
				return err
			}
			if done {
				fmt.Println("No changed images detected - nothing to build")
				return nil
			}

			// Override workers if specified
			if workers > 0 {
				buildPlan.Metadata.Workers = workers
			}
		}

		// Execute the plan
		images := buildPlan.GetTotalCommands()
		platforms := buildPlan.GetTotalPlatforms()
		log.Info().
			Bool("dry_run", dryRun).
			Int("queues", len(buildPlan.Queues)).
			Int("images", images).
			Int("platform_builds", platforms).
			Int("batches", buildPlan.GetTotalBatches()).
			Int("workers", buildPlan.Metadata.Workers).
			Str("task_timeout", buildPlan.Metadata.TaskTimeout.String()).
			Msg("executing build plan")

		executor := plan.NewExecutor(buildPlan.Metadata.Workers, buildPlan.Metadata.TaskTimeout)

		// Execute the plan
		results, execErr := executor.Execute(ctx, buildPlan)

		// Split results by outcome. A failed queue stops early, so some planned
		// images may have no result at all — those are reported as not built.
		var succeeded, failed []plan.ExecutionResult
		built := map[string]bool{}
		for _, result := range results {
			built[result.Command.ImageTag] = true
			if result.Error != nil {
				failed = append(failed, result)
			} else {
				succeeded = append(succeeded, result)
			}
		}

		if len(succeeded) > 0 {
			fmt.Printf("\n✓ Built %d/%d images\n", len(succeeded), buildPlan.GetTotalCommands())
			for _, result := range succeeded {
				duration := result.EndTime.Sub(result.StartTime)
				if plats := result.Command.Architecture; plats != "" {
					fmt.Printf("  ✓ %s [%s] (%.1fs)\n", result.Command.ImageTag, plats, duration.Seconds())
				} else {
					fmt.Printf("  ✓ %s (%.1fs)\n", result.Command.ImageTag, duration.Seconds())
				}
			}
		}

		if len(failed) > 0 {
			fmt.Printf("\n❌ Failed %d images\n", len(failed))
			for _, result := range failed {
				fmt.Printf("  ✗ %s: %v\n", result.Command.ImageTag, result.Error)
				if result.Output != "" {
					fmt.Printf("    Output: %s\n", strings.TrimSpace(result.Output))
				}
			}
		}

		// Report images that were planned but never executed (queue stopped early).
		var skipped []string
		for _, queue := range buildPlan.Queues {
			for i := range queue.Commands {
				if !built[queue.Commands[i].ImageTag] {
					skipped = append(skipped, queue.Commands[i].ImageTag)
				}
			}
		}
		if len(skipped) > 0 {
			fmt.Printf("\n⊘ Not built %d images (a dependency or earlier batch failed)\n", len(skipped))
			for _, tag := range skipped {
				fmt.Printf("  ⊘ %s\n", tag)
			}
		}

		if execErr != nil {
			fmt.Printf("\n❌ Build failed: %v\n", execErr)
			return execErr
		}
		if len(failed) > 0 {
			return fmt.Errorf("%d image(s) failed to build", len(failed))
		}

		fmt.Printf("\n✓ All %d images built successfully (%d platform builds)\n", len(succeeded), buildPlan.GetTotalPlatforms())
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
	rootCmd.PersistentFlags().Var(logLevel, "log-level",
		fmt.Sprintf("log level: %s (env: %s)", logging.LevelsHelp, logging.EnvLevel))

	// Validate command flags
	validateCmd.Flags().BoolVar(&fix, "fix", false, "auto-fix version cascading issues")

	// Generate command flags
	generateCmd.Flags().StringVar(&arch, "arch", "", "generate for specific architecture only")
	generateCmd.Flags().BoolVar(&skipVersionCheck, "skip-version-check", false, "skip version validation and automatic updates")
	generateCmd.Flags().StringSliceVar(&sourcesGlob, "sources", nil, "glob patterns for additional image source files")
	generateCmd.Flags().StringVar(&taskTimeout, "task-timeout", "", "timeout for individual tasks (e.g., '300s', '5m')")

	// Plan command flags
	planCmd.Flags().BoolVar(&changedOnly, "changed-only", false, "create plan for changed images only")
	planCmd.Flags().StringVar(&baseRef, "base-ref", "", "git reference to compare against (default: HEAD~1 or uncommitted)")
	planCmd.Flags().BoolVar(&gitStatus, "git-status", false, "show git repository status")
	planCmd.Flags().StringVar(&outputPath, "output", "", "save plan to file")
	planCmd.Flags().StringVar(&planFormat, "format", "human", "output format: human, json, yaml, commands, graph")
	planCmd.Flags().StringSliceVar(&sourcesGlob, "sources", nil, "glob patterns for additional image source files")
	planCmd.Flags().StringVar(&taskTimeout, "task-timeout", "", "timeout for individual tasks (e.g., '300s', '5m')")
	planCmd.Flags().BoolVar(&dryRun, "dry-run", false, "generate plan with mock tasks for testing")
	planCmd.Flags().BoolVar(&withErrors, "with-errors", false, "when used with --dry-run, simulate random build errors")
	planCmd.Flags().BoolVar(&push, "push", false, "include tag/push + manifest steps in the plan (requires config.registry.owner)")
	planCmd.Flags().BoolVar(&genFirst, "generate", false, "regenerate Dockerfiles before planning")

	// Build command flags
	buildCmd.Flags().StringVar(&planFile, "plan", "", "use plan from file instead of generating one")
	buildCmd.Flags().BoolVar(&changedOnly, "changed-only", false, "build only changed images (when generating plan)")
	buildCmd.Flags().IntVar(&workers, "workers", 0, "number of parallel workers (0 = use config/plan default)")
	buildCmd.Flags().StringVar(&baseRef, "base-ref", "", "git reference to compare against (when generating plan)")
	buildCmd.Flags().StringSliceVar(&sourcesGlob, "sources", nil, "glob patterns for additional image source files")
	buildCmd.Flags().StringVar(&taskTimeout, "task-timeout", "", "timeout for individual tasks (e.g., '300s', '5m')")
	buildCmd.Flags().BoolVar(&dryRun, "dry-run", false, "simulate builds with mock tasks for testing")
	buildCmd.Flags().BoolVar(&withErrors, "with-errors", false, "when used with --dry-run, simulate random build errors")
	buildCmd.Flags().BoolVar(&push, "push", false, "tag and push each arch, then create+push the manifest (requires config.registry.owner; creds from REGISTRY_USERNAME/REGISTRY_PASSWORD)")
	buildCmd.Flags().BoolVar(&genFirst, "generate", false, "regenerate Dockerfiles before building")

	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(buildCmd)
}
