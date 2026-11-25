package integration

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rawmind0/arbor/internal/builder"
	"github.com/rawmind0/arbor/internal/plan"
)

// example is what plan/build must do for one example: the registry login (when
// pushing) and one queue per source.
type example struct {
	login  string // the login step, arguments joined by spaces ("" = no login)
	queues []queue
}

// queue is the expected queue of one source: its batches (tags per graph level,
// sorted) and its images by name.
type queue struct {
	id      string
	batches [][]string
	images  map[string]image
}

// image is what must be built for one image of an example.
type image struct {
	tag    string
	parent string            // the parent image ("" for a root)
	labels map[string]string // global + own labels; the automatic ones are checked too
	steps  []string          // each step's arguments joined by spaces, without the --label flags
}

// expectedPlans lists, for every example, exactly what plan/build must run.
var expectedPlans = map[string]example{
	"minimal": {
		queues: []queue{
			{id: "main",
				batches: [][]string{{"library/base:1.0.1"}, {"library/app:1.0.1"}},
				images: map[string]image{
					"base": {tag: "library/base:1.0.1", parent: "", labels: map[string]string{"layer": "base", "project": "minimal-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t library/base:1.0.1 -f examples/minimal/src/Dockerfile.base --build-arg BASE=alpine:3.18 examples/minimal/src",
						}},
					"app": {tag: "library/app:1.0.1", parent: "base", labels: map[string]string{"layer": "app", "project": "minimal-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t library/app:1.0.1 -f examples/minimal/src/Dockerfile.app --build-arg BASE=library/base:1.0.1 examples/minimal/src",
						}},
				},
			},
		},
	},
	"hierarchy": {
		queues: []queue{
			{id: "main",
				batches: [][]string{{"acme/tools:1.0.0", "acme/ubuntu:22.04.1"}, {"acme/api:1.0.0", "acme/golang:1.23.6", "acme/nodejs22:22.0.0", "acme/nodejs:20.0.1"}, {"acme/backend:1.0.1", "acme/frontend:1.0.1", "acme/worker:1.0.1"}},
				images: map[string]image{
					"ubuntu": {tag: "acme/ubuntu:22.04.1", parent: "", labels: map[string]string{"layer": "os", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/ubuntu:22.04.1 -f examples/hierarchy/src/Dockerfile.ubuntu --build-arg BASE=ubuntu:22.04 examples/hierarchy/src",
						}},
					"tools": {tag: "acme/tools:1.0.0", parent: "", labels: map[string]string{"layer": "tools", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/tools:1.0.0 -f examples/hierarchy/src/Dockerfile.tools examples/hierarchy/src",
						}},
					"nodejs": {tag: "acme/nodejs:20.0.1", parent: "ubuntu", labels: map[string]string{"layer": "runtime", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/nodejs:20.0.1 -f examples/hierarchy/src/Dockerfile.nodejs --build-arg BASE=acme/ubuntu:22.04.1 --build-arg NODE_VERSION=20 examples/hierarchy/src",
						}},
					"nodejs22": {tag: "acme/nodejs22:22.0.0", parent: "ubuntu", labels: map[string]string{"layer": "runtime", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/nodejs22:22.0.0 -f examples/hierarchy/src/Dockerfile.nodejs --build-arg BASE=acme/ubuntu:22.04.1 --build-arg NODE_VERSION=22 examples/hierarchy/src",
						}},
					"golang": {tag: "acme/golang:1.23.6", parent: "ubuntu", labels: map[string]string{"layer": "runtime", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/golang:1.23.6 -f examples/hierarchy/src/Dockerfile.golang --build-arg BASE=acme/ubuntu:22.04.1 --build-arg GO_VERSION=1.23.5 examples/hierarchy/src",
						}},
					"api": {tag: "acme/api:1.0.0", parent: "ubuntu", labels: map[string]string{"layer": "app", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/api:1.0.0 -f examples/hierarchy/src/api/Dockerfile --build-arg BASE=acme/ubuntu:22.04.1 examples/hierarchy/src/api",
						}},
					"frontend": {tag: "acme/frontend:1.0.1", parent: "nodejs", labels: map[string]string{"layer": "app", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/frontend:1.0.1 -f examples/hierarchy/src/frontend/Dockerfile --build-arg BASE=acme/nodejs:20.0.1 examples/hierarchy/src/frontend",
						}},
					"backend": {tag: "acme/backend:1.0.1", parent: "nodejs", labels: map[string]string{"layer": "app", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/backend:1.0.1 -f examples/hierarchy/src/Dockerfile.backend --build-arg BASE=acme/nodejs:20.0.1 examples/hierarchy/src",
						}},
					"worker": {tag: "acme/worker:1.0.1", parent: "golang", labels: map[string]string{"layer": "app", "project": "hierarchy-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t acme/worker:1.0.1 -f examples/hierarchy/src/Dockerfile.worker --build-arg BASE=acme/golang:1.23.6 examples/hierarchy/src",
						}},
				},
			},
		},
	},
	"multiarch": {
		login: "sh -c echo \"$REGISTRY_PASSWORD\" | docker login -u \"$REGISTRY_USERNAME\" --password-stdin registry.example.com",
		queues: []queue{
			{id: "main",
				batches: [][]string{{"registry.example.com/myorg/ubuntu:22.04.1"}, {"registry.example.com/myorg/agent:1.0.0", "registry.example.com/myorg/golang:1.23.6"}, {"registry.example.com/myorg/agent-app:1.0.0", "registry.example.com/myorg/app:1.0.1"}},
				images: map[string]image{
					"ubuntu": {tag: "registry.example.com/myorg/ubuntu:22.04.1", parent: "", labels: map[string]string{"project": "multiarch-example"},
						steps: []string{
							"docker buildx build --platform linux/amd64,linux/arm64 -t registry.example.com/myorg/ubuntu:22.04.1 --push -f examples/multiarch/src/Dockerfile.ubuntu --build-arg BASE=ubuntu:22.04 examples/multiarch/src",
						}},
					"golang": {tag: "registry.example.com/myorg/golang:1.23.6", parent: "ubuntu", labels: map[string]string{"project": "multiarch-example"},
						steps: []string{
							"docker buildx build --platform linux/amd64,linux/arm64 -t registry.example.com/myorg/golang:1.23.6 --push -f examples/multiarch/src/Dockerfile.golang --build-arg BASE=registry.example.com/myorg/ubuntu:22.04.1 --build-arg GO_VERSION=1.23.5 examples/multiarch/src",
						}},
					"agent": {tag: "registry.example.com/myorg/agent:1.0.0", parent: "ubuntu", labels: map[string]string{"project": "multiarch-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t registry.example.com/myorg/agent:1.0.0 -f examples/multiarch/src/Dockerfile.agent --build-arg BASE=registry.example.com/myorg/ubuntu:22.04.1 examples/multiarch/src",
							"docker push registry.example.com/myorg/agent:1.0.0",
						}},
					"app": {tag: "registry.example.com/myorg/app:1.0.1", parent: "golang", labels: map[string]string{"project": "multiarch-example"},
						steps: []string{
							"docker buildx build --platform linux/amd64,linux/arm64 -t registry.example.com/myorg/app:1.0.1 --push -f examples/multiarch/src/Dockerfile.app --build-arg BASE=registry.example.com/myorg/golang:1.23.6 examples/multiarch/src",
						}},
					"agent-app": {tag: "registry.example.com/myorg/agent-app:1.0.0", parent: "agent", labels: map[string]string{"project": "multiarch-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t registry.example.com/myorg/agent-app:1.0.0 -f examples/multiarch/src/Dockerfile.agent-app --build-arg BASE=registry.example.com/myorg/agent:1.0.0 examples/multiarch/src",
							"docker push registry.example.com/myorg/agent-app:1.0.0",
						}},
				},
			},
		},
	},
	"multisource": {
		queues: []queue{
			{id: "main",
				batches: [][]string{{"library/web-base:1.0.1"}, {"library/admin:1.0.1", "library/site:1.0.1"}},
				images: map[string]image{
					"web-base": {tag: "library/web-base:1.0.1", parent: "", labels: map[string]string{"project": "multisource-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t library/web-base:1.0.1 -f examples/multisource/src/Dockerfile.web-base --build-arg BASE=ubuntu:22.04 --build-arg NODE_VERSION=20 examples/multisource/src",
						}},
					"site": {tag: "library/site:1.0.1", parent: "web-base", labels: map[string]string{"project": "multisource-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t library/site:1.0.1 -f examples/multisource/src/Dockerfile.site --build-arg BASE=library/web-base:1.0.1 examples/multisource/src",
						}},
					"admin": {tag: "library/admin:1.0.1", parent: "web-base", labels: map[string]string{"project": "multisource-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t library/admin:1.0.1 -f examples/multisource/src/Dockerfile.admin --build-arg BASE=library/web-base:1.0.1 examples/multisource/src",
						}},
				},
			},
			{id: "services",
				batches: [][]string{{"library/api-base:1.0.1"}, {"library/api:1.0.1", "library/worker:1.0.1"}},
				images: map[string]image{
					"api-base": {tag: "library/api-base:1.0.1", parent: "", labels: map[string]string{"project": "multisource-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t library/api-base:1.0.1 -f examples/multisource/src/Dockerfile.api-base --build-arg BASE=ubuntu:22.04 --build-arg GO_VERSION=1.23.5 examples/multisource/src",
						}},
					"api": {tag: "library/api:1.0.1", parent: "api-base", labels: map[string]string{"project": "multisource-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t library/api:1.0.1 -f examples/multisource/src/Dockerfile.api --build-arg BASE=library/api-base:1.0.1 examples/multisource/src",
						}},
					"worker": {tag: "library/worker:1.0.1", parent: "api-base", labels: map[string]string{"project": "multisource-example"},
						steps: []string{
							"docker build --platform linux/amd64 -t library/worker:1.0.1 -f examples/multisource/src/Dockerfile.worker --build-arg BASE=library/api-base:1.0.1 examples/multisource/src",
						}},
				},
			},
		},
	},
	"podman": {
		login: "sh -c echo \"$REGISTRY_PASSWORD\" | podman login -u \"$REGISTRY_USERNAME\" --password-stdin registry.example.com",
		queues: []queue{
			{id: "main",
				batches: [][]string{{"registry.example.com/myorg/alpine:3.20.0"}, {"registry.example.com/myorg/python:3.12.0"}, {"registry.example.com/myorg/app:v1.0.0"}},
				images: map[string]image{
					"alpine": {tag: "registry.example.com/myorg/alpine:3.20.0", parent: "", labels: map[string]string{"project": "podman-example"},
						steps: []string{
							"podman build --platform linux/amd64,linux/arm64 --manifest registry.example.com/myorg/alpine:3.20.0 -f examples/podman/src/Dockerfile.alpine --build-arg BASE=alpine:3.20 examples/podman/src",
							"podman manifest push registry.example.com/myorg/alpine:3.20.0",
						}},
					"python": {tag: "registry.example.com/myorg/python:3.12.0", parent: "alpine", labels: map[string]string{"project": "podman-example"},
						steps: []string{
							"podman build --platform linux/amd64,linux/arm64 --manifest registry.example.com/myorg/python:3.12.0 -f examples/podman/src/Dockerfile.python --build-arg BASE=registry.example.com/myorg/alpine:3.20.0 examples/podman/src",
							"podman manifest push registry.example.com/myorg/python:3.12.0",
						}},
					"app": {tag: "registry.example.com/myorg/app:v1.0.0", parent: "python", labels: map[string]string{"project": "podman-example"},
						steps: []string{
							"podman build --platform linux/amd64,linux/arm64 --manifest registry.example.com/myorg/app:v1.0.0 -f examples/podman/src/app/Dockerfile --build-arg BASE=registry.example.com/myorg/python:3.12.0 examples/podman/src/app",
							"podman manifest push registry.example.com/myorg/app:v1.0.0",
						}},
				},
			},
		},
	},
}

// TestExamplesPlan checks, for every example, that plan/build (plan.Resolve on
// the example loaded with its --sources) runs exactly the expected steps: the
// login, the queue per source, the batch order and, per image, its tag, labels
// and build/push steps (Dockerfile, platforms, BASE and the other build args).
func TestExamplesPlan(t *testing.T) {
	for _, uc := range allExamples {
		t.Run(uc, func(t *testing.T) {
			want, ok := expectedPlans[uc]
			if !ok {
				t.Fatalf("no expected plan for example %q: add it to expectedPlans", uc)
			}
			_, p := resolve(t, uc, builder.ModeNormal)

			var login []string
			for _, l := range p.Login {
				login = append(login, strings.Join(l, " "))
			}
			if strings.Join(login, "\n") != want.login {
				t.Errorf("login = %q, want %q", login, want.login)
			}

			if len(p.Queues) != len(want.queues) {
				t.Fatalf("queues = %d, want %d", len(p.Queues), len(want.queues))
			}
			for i, wq := range want.queues {
				q := p.Queues[i]
				if q.ID != wq.id {
					t.Errorf("queue %d = %q, want %q", i, q.ID, wq.id)
				}
				var batches [][]string
				for _, b := range q.Batches {
					tasks := slices.Clone(b.Tasks)
					sort.Strings(tasks)
					batches = append(batches, tasks)
				}
				if !slices.EqualFunc(batches, wq.batches, slices.Equal) {
					t.Errorf("queue %s batches = %v, want %v", q.ID, batches, wq.batches)
				}

				if len(q.Commands) != len(wq.images) {
					t.Errorf("queue %s: %d commands, want %d", q.ID, len(q.Commands), len(wq.images))
				}
				for _, c := range q.Commands {
					w, ok := wq.images[c.ImageName]
					if !ok {
						t.Errorf("queue %s: unexpected image %q", q.ID, c.ImageName)
						continue
					}
					checkImage(t, c, w)
				}
			}
		})
	}
}

func checkImage(t *testing.T, c plan.BuildCommand, w image) {
	t.Helper()
	if c.ImageTag != w.tag {
		t.Errorf("%s: tag = %q, want %q", c.ImageName, c.ImageTag, w.tag)
	}

	labels := maps.Clone(w.labels)
	labels["image.name"] = c.ImageName
	labels["image.version"] = w.tag[strings.LastIndex(w.tag, ":")+1:]
	if w.parent != "" {
		labels["image.parent"] = w.parent
	}
	got := maps.Clone(c.Labels)
	if _, err := time.Parse(time.RFC3339, got["build.date"]); err != nil {
		t.Errorf("%s: label build.date = %q, want an RFC 3339 time", c.ImageName, got["build.date"])
	}
	delete(got, "build.date")
	if !maps.Equal(got, labels) {
		t.Errorf("%s: labels = %v, want %v", c.ImageName, got, labels)
	}

	var steps []string
	for _, step := range c.Steps {
		steps = append(steps, strings.Join(withoutLabels(step), " "))
	}
	if !slices.Equal(steps, w.steps) {
		t.Errorf("%s: steps =\n  %s\nwant\n  %s", c.ImageName, strings.Join(steps, "\n  "), strings.Join(w.steps, "\n  "))
	}
	// Every label is passed to the build step.
	for k, v := range c.Labels {
		if !slices.Contains(c.Steps[0], k+"="+v) {
			t.Errorf("%s: build step misses --label %s=%s", c.ImageName, k, v)
		}
	}
}

// withoutLabels drops the --label flags (checked through the labels) from a step.
func withoutLabels(step []string) []string {
	var out []string
	for i := 0; i < len(step); i++ {
		if step[i] == "--label" {
			i++
			continue
		}
		out = append(out, step[i])
	}
	return out
}

// TestExamplesPipeline runs each example end to end, like `build --dry-run`:
// plan, then execute it with the mock builder, which runs every step.
func TestExamplesPipeline(t *testing.T) {
	for _, uc := range allExamples {
		t.Run(uc, func(t *testing.T) {
			_, p := resolve(t, uc, builder.ModeMock)

			results, err := plan.NewExecutor(4, 30*time.Second).Execute(context.Background(), p)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if len(results) != p.GetTotalCommands() {
				t.Errorf("expected %d results, got %d", p.GetTotalCommands(), len(results))
			}
			for _, r := range results {
				if r.Error != nil {
					t.Errorf("unexpected failure for %s: %v", r.Command.ImageTag, r.Error)
				}
			}
		})
	}
}
