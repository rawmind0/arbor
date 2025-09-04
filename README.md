# Container Builder

A Go-based tool to manage Docker image hierarchies with dependency-aware builds, multi-architecture support, and parallel execution capabilities.

## Features

- **Hierarchical Image Management**: Define image dependencies in YAML configuration
- **Multi-Architecture Support**: Build for multiple platforms (linux/amd64, linux/arm64, darwin/arm64)
- **Template-Based Dockerfiles**: Use Go templates with variable substitution and inheritance
- **Parallel Builds**: Respect dependencies while building images concurrently
- **Change Detection**: Build only changed images and their dependents (Git-based)
- **Version Cascading**: Automatically update child image versions when parents change
- **Builder Support**: Compatible with Docker and Podman

## Installation

### From Source
```bash
go install github.com/rawmind0/container-builder@latest
```

### Build Locally
```bash
git clone https://github.com/rawmind0/container-builder.git
cd container-builder
make build
./bin/container-builder --help
```

## Quick Start

1. Create a configuration file (`config.yaml`):

```yaml
config:
  builder: docker
  workers: 3
  architectures:
    - linux/amd64
    - linux/arm64
  dockerfiles:
    source: src
    destination: build
  labels:
    maintainer: "your-team@company.com"

images:
  ubuntu22:
    version: 22.04.2
    base_image: ubuntu:22.04
    args:
      GH_VERSION: 2.76.2
    images:
      rust:
        version: 1.89.0-4
        args:
          RUST_VERSION: 1.89.0
        images:
          tauri:
            version: 2.0.0-4
            args:
              NODE_VERSION: 22
```

2. Create template Dockerfiles in `src/`:

```dockerfile
# src/Dockerfile.ubuntu22
{{ template "base" . }}

{{ define "content" }}
RUN apt-get update && apt-get install -y curl git
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
{{ end }}
```

3. Generate and build:

```bash
# Validate configuration
container-builder validate

# Show build plan
container-builder plan --show-levels

# Generate Dockerfiles
container-builder generate

# Build all images
container-builder build
```

## Commands

### `validate`
Validate configuration and check version cascading:
```bash
container-builder validate --fix  # Auto-fix version cascading issues
```

### `generate`
Generate Dockerfiles from templates:
```bash
container-builder generate --changed-only     # Only changed images
container-builder generate --arch linux/amd64 # Specific architecture
```

### `plan`
Show build execution plan:
```bash
container-builder plan --show-levels  # Show parallel execution levels
container-builder plan --show-arch    # Show architecture in names
container-builder plan --changed-only # Only changed images
```

### `build`
Build container images:
```bash
container-builder build --workers 5        # Custom worker count
container-builder build --changed-only     # Only changed images
container-builder build --arch linux/amd64 # Specific architecture
```

## Configuration Reference

### Global Configuration
```yaml
config:
  builder: docker              # docker or podman
  workers: 3                   # Parallel build workers
  architectures:               # Default architectures
    - linux/amd64
    - linux/arm64
  dockerfiles:
    source: src                # Template source directory
    destination: build         # Generated Dockerfile directory
  labels:                      # Common labels for all images
    maintainer: "team@company.com"
    project: "my-project"
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

## Template System

Container Builder uses Go templates with a base template system for maximum code reuse.

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

## Development

### Prerequisites
- Go 1.21+
- Docker or Podman
- Make

### Building
```bash
make build              # Build for current platform
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

See the `examples/` directory for a complete example configuration with:
- Multi-stage Ubuntu → Rust → Tauri → Windows cross-compilation setup
- Multi-architecture builds (AMD64 + ARM64)
- Template inheritance and variable usage
- Architecture-specific logic using TARGETARCH

## License

MIT License - see LICENSE file for details.