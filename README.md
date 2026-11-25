# Arbor

A Go-based tool to manage Docker image hierarchies with dependency-aware builds, multi-architecture support, and parallel execution capabilities.

## Features

- **Hierarchical Image Management**: Define image dependencies in YAML configuration
- **Multi-Architecture Support**: Build for multiple platforms with smart defaults
- **Multi-Source Configs**: Compose image stacks from multiple source files (`--sources`)
- **Plain Dockerfiles**: Regular Dockerfiles that also build with a plain `docker build`; arbor injects the parent image through `ARG BASE`
- **Parallel Batch Execution**: Build images in optimized batches respecting dependencies
- **Change Detection**: Build only changed images and their dependents (Git-based)
- **Version Management**: Semantic versioning with cascade updates; plan/build refuse inconsistent versions
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
shows the point — a base image and a child built on top of it:

1. `examples/minimal/config.yaml` — the image hierarchy:

```yaml
config:
  builder: docker
  architectures:
    - linux/amd64
  dockerfiles: examples/minimal/src
  labels:
    project: minimal-example

images:
  base:                  # root: the only image that sets base_image
    version: 1.0.1
    base_image: alpine:3.18
    images:
      app:               # child: built with BASE=<base's tag>, so it builds after it
        version: 1.0.1
```

2. Plain Dockerfiles in `examples/minimal/src/`. Each one starts with `ARG BASE`
   and builds `FROM ${BASE}`; arbor passes the parent image as
   `--build-arg BASE=<parent tag>` (and `base_image` for the root):

```dockerfile
# Dockerfile.base — the default lets a plain `docker build` work too
ARG BASE=alpine:3.18
FROM ${BASE}
RUN apk add --no-cache ca-certificates tini
ENTRYPOINT ["/sbin/tini", "--"]
```
```dockerfile
# Dockerfile.app — arbor builds it with --build-arg BASE=library/base:1.0.1
ARG BASE
FROM ${BASE}
ENV APP_ENV=production
CMD ["sh", "-c", "echo app running in ${APP_ENV}"]
```

3. Run it end to end:

```bash
CONFIG=examples/minimal/config.yaml

arbor validate --config $CONFIG          # Validate config, Dockerfiles and versions
arbor plan     --config $CONFIG          # Show the build plan
arbor build    --config $CONFIG          # Build the images

# …or drive the examples through Make:
make example-validate EXAMPLE=minimal
```

For a layered hierarchy (base OS → runtime layers → apps) see `examples/hierarchy/`;
for multi-arch + push see `examples/multiarch/` (docker) and `examples/podman/`;
for multiple source files see `examples/multisource/`.

## Commands

### `validate`
Validate the configuration, every image's Dockerfile (see
[Dockerfile Contract](#dockerfile-contract)) and the version cascade:
```bash
arbor validate                            # Check config, Dockerfiles and versions (exit 1 if a bump is missing)
arbor validate --fix                      # Apply the required version bumps
arbor validate --base-ref develop         # Measure changes from another branch
arbor validate --sources "stacks/*.yaml"  # Also load extra image source files
```

`validate` exits non-zero when a version needs a bump, so it works as a CI gate;
with `--fix` it writes the bumps and succeeds.

Both `plan` and `build` first run the same checks (without `--fix`): they fail if
a Dockerfile breaks the contract or a version needs a bump, and they never
rewrite the config.

### `plan`
Create and display build execution plan:
```bash
arbor plan                      # Show execution plan
arbor plan --format json        # Output as JSON (also: yaml, graph, commands)
arbor plan --output plan.yaml   # Save to file
arbor plan --changed-only       # Only changed images + dependents (requires git)
arbor plan --push               # Include registry login + tag/push + manifest steps
arbor plan --skip-version-check # Skip the version consistency check
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
arbor build --skip-version-check # Skip the version consistency check
arbor build --dry-run          # Simulate builds with mock tasks
```

> Pushing requires a registry owner (`config.registry.owner`); credentials come
> from `REGISTRY_USERNAME` / `REGISTRY_PASSWORD`. Multi-architecture images are
> only valid with `--push` (a manifest list can't be loaded locally).

## Configuration Reference

### Global Configuration
```yaml
config:
  builder: docker              # docker, podman, buildah or kaniko (default: docker)
  workers: 3                   # Parallel build workers (default: 2)
  task_timeout: 10m            # Time limit for each build/push step (default: 300s)
  architectures:               # Default architectures (default: build platform)
    - linux/amd64
    - linux/arm64
  dockerfiles: src             # Directory holding the Dockerfiles (default: src)
  labels:                      # Common labels for all images
    maintainer: "team@company.com"
    project: "my-project"
  registry:                    # Image namespace + push target
    url: registry.example.com  # Registry host (optional; omit for Docker Hub)
    owner: myorg               # Owner/namespace, used in every image tag (default: library)
  push: false                  # Push images + multi-arch manifests (default: false)

  # Command defaults (all optional; a CLI flag overrides the matching value)
  skip_version_check: false    # plan/build: skip the version check (default: false)
  changed_only: false          # plan/build: only changed images (default: false)
  base_ref: master             # git ref changes are measured from (default: master, then main)
  version_increment: patch     # Version bump type: "patch" or "minor" (default: patch)
```

### Image Configuration
```yaml
images:
  image-name:
    version: 1.0.0             # Image version
    base_image: ubuntu:22.04   # External base image (root only), passed as BASE
    dockerfile: api/Dockerfile # Dockerfile path in the dockerfiles directory (default:
                               # Dockerfile.<image-name>); its folder is the build context
    architectures:             # Override global architectures
      - linux/amd64
    args:                      # Build arguments (--build-arg)
      VERSION: 1.0.0
    labels:                    # Image-specific labels (--label)
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

## Dockerfile Contract

Dockerfiles are regular Dockerfiles, built in place from the `dockerfiles`
directory. Each image's build context is its Dockerfile's folder, so `COPY`
and `ADD` paths are relative to the Dockerfile. An image whose Dockerfile is in
a subfolder (`dockerfile: api/Dockerfile`) keeps the files it copies next to it:

```
src/
  Dockerfile.base
  api/
    Dockerfile        # COPY go.mod main.go ./
    go.mod
    main.go
```

**Recommended layout:** keep images whose Dockerfile only uses instructions
(`RUN`, `ENV`, ...) as `Dockerfile.<name>` in the `dockerfiles` directory, and
give every image that copies files (`COPY`/`ADD`) its own subfolder with those
files. Change detection then works per folder: any change in an image's folder
changes that image only. Keep those subfolders side by side (`src/api/`,
`src/worker/`), not one inside another: a file in a nested image's folder
belongs to that image only, even though the outer image's build could copy it.
In a folder shared by several Dockerfiles, a file
change is matched by name, so a file copied through a glob or a whole folder
copied by `COPY . .` isn't detected there (see [Git Integration](#git-integration)).

arbor injects the hierarchy through one build argument, `BASE`:

- a child image is built with `--build-arg BASE=<parent's [url/]owner/name:version>`
- a root image with `base_image` is built with `--build-arg BASE=<base_image>`
- a root image without `base_image` gets no `BASE`; its own `FROM` (or
  `ARG BASE=<default>`) applies

So every Dockerfile that receives `BASE` must:

1. start with `ARG BASE` (comments and parser directives may come first; a
   default such as `ARG BASE=ubuntu:22.04` is allowed), and
2. build its final stage `FROM ${BASE}` (or `$BASE`), directly or through stage
   aliases, so multi-stage builds work:

```dockerfile
ARG BASE
FROM golang:1.23 AS build
RUN go build -o /app .

FROM ${BASE}
COPY --from=build /app /usr/local/bin/app
```

`validate`, `plan` and `build` reject Dockerfiles that break these rules, with
one error per image. Configured `args` and `labels` are passed as `--build-arg`
and `--label`; a Dockerfile still declares `ARG NAME` (after `FROM`) to use an
arg, and the same goes for the platform args (`TARGETOS`, `TARGETARCH`, ...).
Environment variables are plain `ENV` instructions in the Dockerfile.

Because nothing is generated, the same files build without arbor:

```bash
docker build -t library/base:1.0.1 -f examples/minimal/src/Dockerfile.base examples/minimal/src
docker build --build-arg BASE=library/base:1.0.1 -f examples/minimal/src/Dockerfile.app examples/minimal/src
```

### Migrating from Dockerfile templates

Older versions generated Dockerfiles from templates. To migrate:

- `dockerfiles: {source: src, destination: build}` → `dockerfiles: src`; delete
  the generated directory
- in each template, replace the generated `FROM` with `ARG BASE` + `FROM ${BASE}`
  (roots: `ARG BASE=<base_image>`), see the contract above
- move each image's `envs:` into `ENV` lines in its Dockerfile
- make `COPY`/`ADD` paths relative to the Dockerfile's folder (the build
  context), not to the directory arbor runs from, and keep those files next to
  the Dockerfile (e.g. `dockerfile: api/Dockerfile`)
- `arbor generate` and `--generate` are gone: `plan` and `build` use the
  Dockerfiles as they are

Configs still using `envs` or the old `dockerfiles` mapping fail to load with a
message saying what to change.

## Version Management

Arbor checks that image versions follow the changes, for the images of the
config file and of every `--sources` file:

- every version must be a numeric tag: `major[.minor[.patch]]`, optionally with
  a `v` prefix and a `-suffix` (`1.0.1`, `22.04`, `22.04.1`, `v1.2.3`,
  `1.2.3-alpine`); `latest` or `20.04.x` are rejected
- an image with a change in its Dockerfile folder needs a version bump (see
  [Git Integration](#git-integration) for which images a file change
  affects). The bump cascades
  to its children (`version_increment`: `patch` or `minor`). The bump keeps the
  tag's format: `patch` raises the third number (`22.04.1` → `22.04.2`, `22.04`
  → `22.04.1`), `minor` raises the second and resets the patch (`22.04.1` →
  `22.05.0`)

An image only needs a bump while its version still equals the one it had where
changes are measured from (see [base_ref](#base_ref)), so once it is bumped (in
any commit) it is no longer flagged, and images that are new need none.

```bash
arbor validate          # Report the required bumps (and exit 1)
arbor validate --fix    # Apply them
```

`--fix` writes each bump into the file the image is defined in (the config file
or its `--sources` file) and changes only that `version:` value: comments,
formatting and everything else in the file stay as they are.

`plan` and `build` run the same check and stop if a bump is missing; they never
rewrite the config. `--skip-version-check` (or `skip_version_check: true`)
bypasses it.

### base_ref

`base_ref` (`--base-ref`) is the branch work is merged into; both the version
check and `--changed-only` measure changes against it:

- default: the `master` branch, else `main`; if neither exists, arbor stops and
  asks you to set it (e.g. `base_ref: develop`)
- on a feature branch: everything since the branch split from `base_ref`
  (`git merge-base <base_ref> HEAD`) — every commit on the branch plus
  uncommitted changes
- on `base_ref` itself (e.g. CI on `master` after a merge): the uncommitted
  changes if there are any, otherwise the last commit (what the merge brought in)
- `--base-ref <ref>` with any other commit (e.g. `HEAD~3`) measures from it
- in CI, checkouts are usually shallow and have no local target branch: set
  `base_ref: origin/<target>` and fetch the history (e.g. `fetch-depth: 0`)

## Git Integration

Arbor is git-aware and automatically adjusts features based on repository status:

### Git-Dependent Features
- **Change Detection**: Requires git to detect modified files
- **Version Validation**: Uses git to track changes in Dockerfile folders since `base_ref`
- **--changed-only**: Builds only modified images and dependents
  - an image changes when its Dockerfile folder changes (below), or when its
    entry in the config file or `--sources` file changes (version, args,
    labels, base_image, parent, ...), compared with the file at the base;
    comments and formatting don't count
  - a changed file belongs to its closest Dockerfile folder: its own folder,
    or the nearest one above it that holds an image's Dockerfile. Folders
    above that one are not affected. In that folder:
    - a changed Dockerfile changes the images built from it
    - with a single Dockerfile (e.g. `src/api/Dockerfile`), any other file in
      the folder, or below it, changes that image
    - with several Dockerfiles (e.g. the `Dockerfile.<name>` files in `src/`),
      another file changes only the images whose Dockerfile mentions it: its
      path relative to the folder (`conf/app.yaml`) or one of its parent
      folders (`conf/`). A file matched only by a glob isn't detected there;
      give such an image its own folder
  - children follow their parent: they need a bump and are rebuilt with it
  - a change in the global `config:` section rebuilds every image, except the
    command defaults (`workers`, `skip_version_check`, `changed_only`,
    `base_ref`, `version_increment`); a new `--sources` file rebuilds its images
  - untracked files (not ignored by `.gitignore`) count as changes

### Non-Git Environments
When not in a git repository:
- Version validation is automatically disabled
- Changed-only mode is disabled
- Warnings are displayed for disabled features
- All other features continue to work normally

## Development

### Prerequisites
- Go 1.26+
- Docker or Podman (to build the examples for real)
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
its own `config.yaml` and `src/` Dockerfiles:

- **`minimal/`** — the smallest hierarchy: a base image and a child that inherits it
- **`hierarchy/`** — base OS → runtime layers (Node, Go) → apps, showing inheritance,
  build-arg-driven versioned installs, per-arch logic and a registry owner; also
  two roots (one without `base_image`), one Dockerfile shared by two images, a
  multi-stage build, images in their own folder (`dockerfile: api/Dockerfile`)
  and a file shared in a folder with several Dockerfiles
- **`multiarch/`** — a multi-architecture build with `push` to a registry (docker
  buildx), plus an amd64-only image whose child inherits the override
- **`podman/`** — multi-arch with podman (`--manifest` + `manifest push`),
  `task_timeout`, `version_increment: minor`, a `v`-prefixed version and an
  app in its own folder
- **`multisource/`** — images composed from multiple source files via `--sources`

Run them with the Make targets (every example by default, or `EXAMPLE=<name>`):
```bash
make example-validate                      # validate every example
make example-plan EXAMPLE=hierarchy        # show one example's plan
make example-build EXAMPLE=multiarch DRY=1 # build one example (DRY=1 simulates)
```

## License

MIT License - see LICENSE file for details.