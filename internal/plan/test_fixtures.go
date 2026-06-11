package plan

import (
	"time"

	"github.com/rawmind0/arbor/internal/builder"
)

// CreateTestPlanWithQueues creates a test plan using the new Queue structure
func CreateTestPlanWithQueues(commands []BuildCommand, batches []Batch) *BuildPlan {
	return &BuildPlan{
		Metadata: PlanMetadata{
			Version:     "1.0.0",
			Generated:   time.Now(),
			ConfigFile:  "test.yaml",
			Builder:     builder.BuilderMock,
			Workers:     2,
			TotalImages: len(commands),
		},
		Queues: []Queue{
			{
				ID:       "main",
				Batches:  batches,
				Commands: commands,
			},
		},
	}
}

// CreateSimpleTestPlan creates a simple test plan with basic commands
func CreateSimpleTestPlan() *BuildPlan {
	commands := []BuildCommand{
		{
			ID:             "base-amd64",
			ImageName:      "base",
			ImageTag:       "base:1.0.0",
			Architecture:   "linux/amd64",
			DockerfilePath: "build/Dockerfile.base",
			BuildContext:   ".",
			Command:        []string{"sh", "-c", "echo 'Building base:1.0.0'"},
		},
		{
			ID:             "app-amd64",
			ImageName:      "app",
			ImageTag:       "app:1.0.0",
			Architecture:   "linux/amd64",
			DockerfilePath: "build/Dockerfile.app",
			BuildContext:   ".",
			Command:        []string{"sh", "-c", "echo 'Building app:1.0.0'"},
		},
	}

	batches := []Batch{
		{Tasks: []string{"base:1.0.0"}},
		{Tasks: []string{"app:1.0.0"}},
	}

	return CreateTestPlanWithQueues(commands, batches)
}

// CreateMultiQueueTestPlan creates a test plan with multiple queues
func CreateMultiQueueTestPlan() *BuildPlan {
	return &BuildPlan{
		Metadata: PlanMetadata{
			Version:     "1.0.0",
			Generated:   time.Now(),
			ConfigFile:  "test.yaml",
			Builder:     builder.BuilderMock,
			Workers:     4,
			TotalImages: 4,
		},
		Queues: []Queue{
			{
				ID: "main",
				Batches: []Batch{
					{Tasks: []string{"base:1.0.0"}},
					{Tasks: []string{"app:1.0.0"}},
				},
				Commands: []BuildCommand{
					{
						ID:       "base-amd64",
						ImageTag: "base:1.0.0",
						Command:  []string{"sh", "-c", "echo 'base'"},
					},
					{
						ID:       "app-amd64",
						ImageTag: "app:1.0.0",
						Command:  []string{"sh", "-c", "echo 'app'"},
					},
				},
			},
			{
				ID: "databases",
				Batches: []Batch{
					{Tasks: []string{"postgres:15.0.0", "mysql:8.0.0"}},
				},
				Commands: []BuildCommand{
					{
						ID:       "postgres-amd64",
						ImageTag: "postgres:15.0.0",
						Command:  []string{"sh", "-c", "echo 'postgres'"},
					},
					{
						ID:       "mysql-amd64",
						ImageTag: "mysql:8.0.0",
						Command:  []string{"sh", "-c", "echo 'mysql'"},
					},
				},
			},
		},
	}
}
