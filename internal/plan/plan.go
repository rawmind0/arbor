package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// BuildPlan represents a complete build execution plan
// It contains multiple queues that can run in parallel
type BuildPlan struct {
	Metadata PlanMetadata `json:"metadata" yaml:"metadata"`
	Queues   []Queue      `json:"queues" yaml:"queues"` // Multiple queues that can run in parallel
	// Login is a one-time registry login step run before any queue (push only).
	Login [][]string `json:"login,omitempty" yaml:"login,omitempty"`
}

// Queue represents an independent build pipeline from a single source
// Queues can run in parallel, batches within a queue run sequentially
type Queue struct {
	ID       string         `json:"id" yaml:"id"`                                 // Source identifier (e.g., "main", "databases")
	Batches  []Batch        `json:"batches" yaml:"batches"`                       // Sequential batches for this queue
	Commands []BuildCommand `json:"commands,omitempty" yaml:"commands,omitempty"` // All commands in this queue
}

// PlanMetadata contains metadata about the plan
type PlanMetadata struct {
	Version     string        `json:"version" yaml:"version"`
	Generated   time.Time     `json:"generated" yaml:"generated"`
	ConfigFile  string        `json:"config_file" yaml:"config_file"`
	Builder     string        `json:"builder" yaml:"builder"`
	GitInfo     *GitInfo      `json:"git_info,omitempty" yaml:"git_info,omitempty"`
	ChangedOnly bool          `json:"changed_only" yaml:"changed_only"`
	TotalImages int           `json:"total_images" yaml:"total_images"`
	Workers     int           `json:"workers" yaml:"workers"`
	TaskTimeout time.Duration `json:"task_timeout" yaml:"task_timeout"` // Timeout for individual tasks
}

// GitInfo contains git repository information
type GitInfo struct {
	Branch  string `json:"branch,omitempty" yaml:"branch,omitempty"`
	Commit  string `json:"commit,omitempty" yaml:"commit,omitempty"`
	IsClean bool   `json:"is_clean" yaml:"is_clean"`
	BaseRef string `json:"base_ref,omitempty" yaml:"base_ref,omitempty"`
}

// BuildCommand represents a single build command to execute
type BuildCommand struct {
	// Unique identifier for this command
	ID string `json:"id" yaml:"id"`

	// Identification
	ImageName    string `json:"image_name" yaml:"image_name"`
	ImageTag     string `json:"image_tag" yaml:"image_tag"`
	Architecture string `json:"architecture" yaml:"architecture"`

	// Paths
	DockerfilePath string `json:"dockerfile_path" yaml:"dockerfile_path"`
	BuildContext   string `json:"build_context" yaml:"build_context"`

	// The actual command to execute (the build step). When Steps is non-empty
	// the executor runs Steps instead (build, push per arch, then manifest).
	Command []string   `json:"command" yaml:"command"`
	Steps   [][]string `json:"steps,omitempty" yaml:"steps,omitempty"`

	// Build configuration
	BuildArgs map[string]string `json:"build_args,omitempty" yaml:"build_args,omitempty"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// LoadFromFile loads a build plan from a file
func LoadFromFile(path string) (*BuildPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan file: %w", err)
	}

	plan := &BuildPlan{}

	// Try JSON first
	if err := json.Unmarshal(data, plan); err == nil {
		return plan, nil
	}

	// Try YAML
	if err := yaml.Unmarshal(data, plan); err == nil {
		return plan, nil
	}

	return nil, fmt.Errorf("failed to parse plan file as JSON or YAML")
}

// SaveToFile saves the build plan to a file
func (p *BuildPlan) SaveToFile(path string, format string) error {
	var data []byte
	var err error

	switch format {
	case "json":
		data, err = json.MarshalIndent(p, "", "  ")
	case "yaml", "yml":
		data, err = yaml.Marshal(p)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write plan file: %w", err)
	}

	return nil
}

// Validate checks if the plan is valid
func (p *BuildPlan) Validate() error {
	if p.Metadata.Version == "" {
		return fmt.Errorf("plan version is required")
	}

	if len(p.Queues) == 0 {
		return fmt.Errorf("plan must have at least one queue")
	}

	totalCommands := 0
	for qIdx, queue := range p.Queues {
		if queue.ID == "" {
			return fmt.Errorf("queue %d: ID is required", qIdx)
		}

		for i, cmd := range queue.Commands {
			if err := cmd.Validate(); err != nil {
				return fmt.Errorf("queue %s command %d: %w", queue.ID, i, err)
			}
		}
		totalCommands += len(queue.Commands)
	}

	if totalCommands == 0 {
		return fmt.Errorf("plan must have at least one command")
	}

	return nil
}

// Validate checks if a build command is valid
func (c *BuildCommand) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("command ID is required")
	}

	if c.ImageName == "" {
		return fmt.Errorf("image name is required")
	}

	if c.ImageTag == "" {
		return fmt.Errorf("image tag is required")
	}

	if len(c.Command) == 0 {
		return fmt.Errorf("command is required")
	}

	if c.DockerfilePath == "" {
		return fmt.Errorf("dockerfile path is required")
	}

	return nil
}

// GetCommandByImageTag finds a command by image tag across all queues
func (p *BuildPlan) GetCommandByImageTag(imageTag string) *BuildCommand {
	for _, queue := range p.Queues {
		for i := range queue.Commands {
			if queue.Commands[i].ImageTag == imageTag {
				return &queue.Commands[i]
			}
		}
	}
	return nil
}

// Batch represents a group of independent tasks that can run in parallel
// Each batch corresponds to a graph level and must complete before the next batch starts
type Batch struct {
	Tasks []string `json:"tasks" yaml:"tasks"` // Image tags that can be built in parallel
}

// GetTotalCommands returns the total number of commands across all queues
func (p *BuildPlan) GetTotalCommands() int {
	total := 0
	for _, queue := range p.Queues {
		total += len(queue.Commands)
	}
	return total
}

// GetTotalBatches returns the total number of batches across all queues
func (p *BuildPlan) GetTotalBatches() int {
	total := 0
	for _, queue := range p.Queues {
		total += len(queue.Batches)
	}
	return total
}

// PlatformCount returns the number of target platforms this command builds,
// derived from its comma-joined Architecture field (one image, many platforms).
func (c *BuildCommand) PlatformCount() int {
	if strings.TrimSpace(c.Architecture) == "" {
		return 1
	}
	n := 0
	for _, p := range strings.Split(c.Architecture, ",") {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
}

// GetTotalPlatforms returns the total number of platform builds across all
// commands. Each image is built once for all its platforms, so this is the sum
// of every command's platform count (always >= GetTotalCommands).
func (p *BuildPlan) GetTotalPlatforms() int {
	total := 0
	for _, queue := range p.Queues {
		for i := range queue.Commands {
			total += queue.Commands[i].PlatformCount()
		}
	}
	return total
}
