## Dockform

Manage Docker Compose projects declaratively.

Overview

Dockform ensures the current Docker state matches the desired state defined in a YAML file, supporting planning, applying, and pruning of resources. It compares the desired compose configuration to the running state using Docker Compose’s native config-hash.

### Quick start

1) Install Go 1.22+
2) Build

```sh
go build ./cmd/dockform
```

3) Try the example

```sh
./dockform plan -f ./example/config.yml
```

### Configuration (config.yml)

```yaml
docker:
  context: "default"         # docker CLI context
  identifier: "my-project"   # optional; applied as label to every managed resource

applications:
  website:
    root: ./example/website
    files:
      - docker-compose.yaml
    project:
      name: website

volumes:
  demo-volume-1: {}

networks:
  demo-network: {}
```

The config file expects a docker context name. If you plan to work with a remote machine, please configure the context before running the app:

```sh
docker context create \
  --docker host=ssh://user@host \
  --description="Remote daemon" \
  my-remote-docker

my-remote-docker
Successfully created context "my-remote-docker"
```

### How it works

- Identifier labeling: all managed resources are labeled with key `io.dockform/<identifier>` and value `"1"`.
- Hash-based service comparison:
  - Desired: `docker compose config --hash <service>` (with a temporary overlay that injects the identifier label).
  - Running: reads `com.docker.compose.config-hash` from the container labels.
  - If hashes match → `[noop]`; otherwise → `[change]`.
- Top-level resources (volumes/networks): presence and identifier labeling are verified.
- Orphan management: resources labeled with the identifier but no longer in the config are listed as ↓ in plan.

### Commands

- `dockform plan [-f config.yml] [--prune]`
  - Parses/validates the config.
  - Compares current Docker state vs desired; prints a colorized plan.
  - If removals are planned and `--prune` is not set, prints:
    `No resources will be removed. Include --prune to delete them`.

- `dockform apply [-f config.yml] [--prune]`
  - Runs `plan` and prints the plan.
  - Applies changes (idempotent).
  - With `--prune`, removes unmanaged resources labeled with the identifier (containers, volumes, networks). Without `--prune`, prints the guidance above and performs no deletions.

