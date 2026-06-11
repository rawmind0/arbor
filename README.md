# Arbor

A Go-based tool to manage Docker image hierarchies with dependency-aware builds, multi-architecture support, and parallel execution capabilities.

## Features

- **Hierarchical Image Management**: Define image dependencies in YAML configuration
- **Multi-Architecture Support**: Build for multiple platforms with smart defaults
- **Multi-Source Configs**: Compose image stacks from multiple source files (`--sources`)
- **Template-Based Dockerfiles**: Use Go templates with variable substitution and inheritance
- **Parallel Batch Execution**: Build images in optimized batches respecting dependencies
- **Change Detection**: Build only changed images and their dependents (Git-based)
- **Automatic Version Management**: Semantic versioning with cascade updates and validation
- **Builder Support**: Compatible with Docker and Podman
- **Registry Push**: Tag and push images and multi-architecture manifests
- **Execution Plans**: Generate, save, and reuse optimized build plans
- **Git-Aware**: Automatically adapts features based on git repository availability
- **Configuration Defaults**: Set command defaults in config file

## Installation

### From Source
```bash
go install github.com/rawmind0/arbor@latest
```

### Build Locally
```bash
git clone https://github.com/rawmind0/arbor.git
cd arbor
make build        # Build for current platform
make build-all    # Build for all platforms
./dist/arbor_*/arbor --help
```

## Quick Start

The quickest way to see it work is to run one of the bundled use cases in
[`examples/`](examples/). The `minimal` example is the smallest hierarchy that
shows the point — a base image and a child that inherits it:

1. `examples/minimal/config.yaml` — the image hierarchy:

```yaml
config:
  builder: docker
  architectures:
    - linux/amd64
  dockerfiles:
    source: examples/minimal/src
    destination: examples/minimal/build
  labels:
    project: minimal-example

images:
  base:                  # root: the only image that sets base_image
    version: 1.0.0
    base_image: alpine:3.18
    images:
      app:               # child: FROM resolves to base, so it builds after it
        version: 1.0.0
        envs:
          APP_ENV: production
```

2. Templates in `examples/minimal/src/` extend the base template; the child just
   adds its own layer on top of its parent:

```dockerfile
# Dockerfile.base
{{ template "base" . }}
{{ define "content" }}
RUN apk add --no-cache ca-certificates tini
ENTRYPOINT ["/sbin/tini", "--"]
{{ end }}
```
```dockerfile
# Dockerfile.app  — its FROM resolves to the base above
{{ template "base" . }}
{{ define "content" }}
CMD ["sh", "-c", "echo app running in ${APP_ENV}"]
{{ end }}
```

3. Run it end to end:

```bash
CONFIG=examples/minimal/config.yaml

arbor validate --config $CONFIG          # Validate config + version cascade
arbor generate --config $CONFIG          # Generate Dockerfiles (idempotent)
arbor plan     --config $CONFIG          # Show the build plan
arbor build    --config $CONFIG          # Build the images

# …or drive the examples through Make:
make example-generate EXAMPLE=minimal
```

For a layered hierarchy (base OS → runtime layers → apps) see `examples/hierarchy/`;
for multi-arch + push see `examples/multiarch/`; for multiple source files see
`examples/multisource/`.

## Commands

### `validate`
Validate configuration and check version cascading:
```bash
arbor validate        # Check for version issues
arbor validate --fix  # Auto-fix version cascading issues
```

### `generate`
Generate Dockerfiles from templates with automatic version validation. This is an
idempotent operation: it always regenerates the full set (cleaning the destination
first), so incremental selection lives in `plan`/`build`, not here.
```bash
arbor generate                            # Generate all (with version check)
arbor generate --skip-version-check       # Skip version validation
arbor generate --sources "stacks/*.yaml"  # Also load extra image source files
```

### `plan`
Create and display build execution plan:
```bash
arbor plan                      # Show execution plan
arbor plan --format json        # Output as JSON (also: yaml, graph, commands)
arbor plan --output plan.yaml   # Save to file
arbor plan --changed-only       # Only changed images + dependents (requires git)
arbor plan --push               # Include registry login + tag/push + manifest steps
arbor plan --generate           # (Re)generate Dockerfiles first
arbor plan --dry-run            # Plan with mock commands (no Docker)
arbor plan --git-status         # Show git repository info
```

### `build`
Build container images using execution plan:
```bash
arbor build                    # Build all images
arbor build --plan plan.yaml   # Use saved plan
arbor build --workers 5        # Custom worker count
arbor build --changed-only     # Only changed images + dependents (requires git)
arbor build --push             # Tag and push images + multi-arch manifests
arbor build --generate         # (Re)generate Dockerfiles before building
arbor build --dry-run          # Simulate builds with mock tasks
```

> Pushing requires a registry owner (`config.registry.owner`); credentials come
> from `REGISTRY_USERNAME` / `REGISTRY_PASSWORD`. Multi-architecture images are
> only valid with `--push` (a manifest list can't be loaded locally).

## Configuration Reference

### Global Configuration
```yaml
config:
  builder: docker              # docker or podman (default: docker)
  workers: 3                   # Parallel build workers (default: 2)
  architectures:               # Default architectures (default: build platform)
    - linux/amd64
    - linux/arm64
  dockerfiles:
    source: src                # Template source directory (default: src)
    destination: build         # Generated Dockerfile directory (default: build)
  labels:                      # Common labels for all images
    maintainer: "team@company.com"
    project: "my-project"
  registry:                    # Image namespace + push target
    url: registry.example.com  # Registry host (optional; omit for Docker Hub)
    owner: myorg               # Owner/namespace, used in every image tag (default: library)
  push: false                  # Push images + multi-arch manifests (default: false)

  # Command defaults (all optional; a CLI flag overrides the matching value)
  skip_version_check: false    # generate: skip version validation (default: false)
  changed_only: false          # plan/build: only changed images (default: false)
  base_ref: main               # plan/build: git reference for change detection
  version_increment: patch     # Version bump type: "patch" or "minor" (default: patch)
```

### Image Configuration
```yaml
images:
  image-name:
    version: 1.0.0             # Image version
    base_image: ubuntu:22.04   # External base image (root only)
    dockerfile: Dockerfile.custom  # Custom template name
    architectures:             # Override global architectures
      - linux/amd64
    args:                      # Build arguments
      VERSION: 1.0.0
    envs:                      # Environment variables
      DEBUG: true
    labels:                    # Image-specific labels
      component: backend
    images:                    # Child images
      child-image:
        version: 1.1.0
```

## Build Execution

### Batch Processing
Arbor organizes builds into batches (graph levels) for optimal parallel execution:
- Images with no dependencies form the first batch
- Each subsequent batch contains images that depend only on previous batches
- All images within a batch can be built in parallel
- Batches are processed sequentially

### Execution Plans
Build plans can be generated, saved, and reused:
```bash
# Generate and save plan
arbor plan --output build-plan.yaml

# View plan in different formats
arbor plan --format graph    # Dependency graph
arbor plan --format commands # Raw docker commands

# Reuse saved plan for consistent builds
arbor build --plan build-plan.yaml
```

## Template System

Arbor uses Go templates with a base template system for maximum code reuse.

### Base Template
All images inherit from a base template that provides:
- FROM statement with dependency resolution
- Common labels (global + image-specific + automatic)
- Build arguments including platform info (TARGETARCH, TARGETOS, etc.)
- Environment variables
- Template extension point for image-specific content

### Image Templates
Image-specific templates extend the base template:

```dockerfile
{{ template "base" . }}

{{ define "content" }}
# Your image-specific content here
RUN apt-get install -y ${PACKAGE_NAME}

# Use TARGETARCH for architecture-specific logic
RUN case "${TARGETARCH}" in \
    amd64) echo "AMD64 specific setup" ;; \
    arm64) echo "ARM64 specific setup" ;; \
    esac
{{ end }}
```

### Template Variables
Templates have access to:
- `{{ .FROM }}` - Parent image reference
- `{{ .Architecture }}` - Target architecture (linux/amd64)
- `{{ .Args }}` - Build arguments map
- `{{ .Envs }}` - Environment variables map
- `{{ .Labels }}` - All labels (global + image + automatic)
- `{{ .Config }}` - Full configuration object
- `{{ .Image }}` - Current image configuration
- `{{ .BuildDate }}` - Build timestamp

## Version Management

Arbor includes automatic semantic versioning management:

### Automatic Version Validation
- Validates all image versions follow semantic versioning (major.minor.patch)
- Detects when Dockerfiles have changed and versions need updating
- Cascades version updates to child images when parents change
- Configurable increment strategy (patch or minor)

### Version Commands
```bash
# Check version consistency
arbor validate

# Auto-fix version issues
arbor validate --fix

# Generate with automatic version updates
arbor generate  # Updates versions automatically

# Skip version checks
arbor generate --skip-version-check
```

### Configuration
```yaml
config:
  version_increment: patch  # or "minor" - controls how versions increment
  skip_version_check: false # Set to true to disable version validation
```

## Git Integration

Arbor is git-aware and automatically adjusts features based on repository status:

### Git-Dependent Features
- **Change Detection**: Requires git to detect modified files
- **Version Validation**: Uses git to track Dockerfile changes
- **--changed-only**: Builds only modified images and dependents

### Non-Git Environments
When not in a git repository:
- Version validation is automatically disabled
- Changed-only mode is disabled
- Warnings are displayed for disabled features
- All other features continue to work normally

## Development

### Prerequisites
- Go 1.26+
- Docker or Podman
- Make

### Building
```bash
make build              # Build for current platform (using goreleaser)
make build-all          # Build for all platforms
make build-linux-amd64  # Cross-compile for Linux AMD64
make build-linux-arm64  # Cross-compile for Linux ARM64
make build-darwin-arm64 # Cross-compile for Darwin ARM64
```

### Testing
```bash
make test           # Run tests
make test-coverage  # Run tests with coverage
make fmt            # Format code
make vet            # Run go vet
```

### Releasing
```bash
make release         # Create release with goreleaser
make release-snapshot # Create snapshot release
```

## Examples

The `examples/` directory contains runnable, self-contained use cases — each with
its own `config.yaml`, `src/` templates, and committed `build/` output:

- **`minimal/`** — the smallest hierarchy: a base image and a child that inherits it
- **`hierarchy/`** — base OS → runtime layers (Node, Go) → apps, showing inheritance,
  build-arg-driven versioned installs, per-arch logic, and a registry owner
- **`multiarch/`** — a multi-architecture build with `push` to a registry
- **`multisource/`** — images composed from multiple source files via `--sources`

Run them with the Make targets (every example by default, or `EXAMPLE=<name>`):
```bash
make example-generate                      # generate every example's Dockerfiles
make example-plan EXAMPLE=hierarchy        # show one example's plan
make example-build EXAMPLE=multiarch DRY=1 # build one example (DRY=1 simulates)
```

## License

MIT License - see LICENSE file for details.