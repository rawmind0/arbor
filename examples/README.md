# Examples

Each folder is a self-contained use case: a `config.yaml` plus the
`src/Dockerfile.<name>` templates it needs. Running `generate` renders the final
Dockerfiles into each folder's `build/` (committed, so the generated output is
visible and tests can verify it). These examples are also the single source of
config data for the test suite.

Run through `make`. By default every example is processed; pass `EXAMPLE=<folder>`
to scope to one, and `DRY=1` to simulate builds instead of running real docker:

```bash
make example-generate                     # render Dockerfiles for every example
make example-plan     EXAMPLE=hierarchy   # show one example's build plan
make example-build    EXAMPLE=hierarchy   # build one example (real docker)
make example-build    EXAMPLE=hierarchy DRY=1   # simulate instead
```

Each folder's extra source files are auto-loaded via `--sources`, so `multisource`
picks up its second stack automatically.

| Use case      | Demonstrates |
|---------------|--------------|
| `minimal`     | The smallest hierarchy: a `base` image and an `app` child that inherits it (FROM resolves to the base; two build levels). |
| `hierarchy`   | A layered hierarchy: `ubuntu` → `{nodejs → frontend, backend; golang → worker}`. Each child inherits its parent's image; runtimes are installed via build args. |
| `multiarch`   | The same layered model built for `linux/amd64` + `linux/arm64`, pushed to a registry (`push: true`, `registry.url` + `owner`). Building it for real needs `REGISTRY_USERNAME` / `REGISTRY_PASSWORD`. |
| `multisource` | Two independent hierarchies in separate files (a web stack in `config.yaml` + an api stack in `services.yaml`), loaded together via `--sources`. |
