# Examples

Each folder is a self-contained use case: a `config.yaml` plus the plain
`src/Dockerfile.<name>` files it builds (or `src/<name>/Dockerfile`, with the
files it copies next to it; each build's context is its Dockerfile's folder). Every Dockerfile starts with `ARG BASE`
and builds `FROM ${BASE}`; arbor passes each child its parent's tag (and each
root its `base_image`) as `--build-arg BASE=...`, so the same files also build
with a plain `docker build`. These examples are also the single source of config
data for the test suite: `test/integration` pins every image's exact build and
push steps, and runs each example with the mock builder (like `DRY=1`).

Run through `make`. By default every example is processed; pass `EXAMPLE=<folder>`
to scope to one, and `DRY=1` to simulate builds instead of running real docker:

```bash
make example-validate                     # validate every example (config, Dockerfiles, versions)
make example-plan     EXAMPLE=hierarchy   # show one example's build plan
make example-build    EXAMPLE=hierarchy   # build one example (real docker)
make example-build    EXAMPLE=hierarchy DRY=1   # simulate instead
```

Each folder's extra source files are auto-loaded via `--sources`, so `multisource`
picks up its second stack automatically.

| Use case      | Demonstrates |
|---------------|--------------|
| `minimal`     | The smallest hierarchy: a `base` image and an `app` child built on it (BASE = the base's tag; two build levels). |
| `hierarchy`   | A layered hierarchy: `ubuntu` → `{nodejs → frontend, backend; nodejs22; golang → worker; api}`, plus a second root, `tools`. Each child inherits its parent's image; runtimes are installed via build args. `tools` has no `base_image` (no BASE; its own `FROM`), `nodejs22` reuses `Dockerfile.nodejs` with another `NODE_VERSION`, `api` is a multi-stage build, and `frontend` / `api` have their Dockerfile in a subfolder (`dockerfile: frontend/Dockerfile`) with their files next to it: the build context is the Dockerfile's folder, and any change in it changes only that image. `src/conf/apt.conf` is a file shared in `src/`, which holds several Dockerfiles: a change to it changes only `ubuntu`, whose Dockerfile mentions it (and, through it, its children). |
| `multiarch`   | The same layered model built for `linux/amd64` + `linux/arm64`, pushed to a registry (`push: true`, `registry.url` + `owner`) with `docker buildx`. `agent` overrides the architectures (amd64 only), its child inherits it, and both build locally, then `docker push`. Building it for real needs `REGISTRY_USERNAME` / `REGISTRY_PASSWORD`. |
| `podman`      | A multi-arch hierarchy built with `builder: podman`: a local manifest list per image (`podman build --manifest`), then `podman manifest push`. Also `task_timeout`, `version_increment: minor`, a `v`-prefixed version (`v1.0.0` → `v1.1.0`) and an app in its own folder (`app/Dockerfile`: any change in `app/` changes it). Building it for real needs registry credentials. |
| `multisource` | Two independent hierarchies in separate files (a web stack in `config.yaml` + an api stack in `services.yaml`), loaded together via `--sources`. |
