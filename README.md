## Dockform

Manage Docker Compose projects declaratively: plan, apply, and prune to keep Docker in sync with a YAML-defined desired state.

### Features

- Declarative config: define applications, volumes, and networks in YAML.
- Safe apply: shows a colorized plan before making changes.
- Idempotent operations: only changes what’s needed.
- Automatic pruning: removes labeled orphans after apply.
- Identifier scoping: labels every managed resource to avoid collisions.
- Hash-based diffs: compares desired vs running compose config-hash.

### Install

Requires Go 1.22+

```sh
go build ./cmd/dockform
```

This produces a `dockform` binary in the repo root.

### Quick Start

1) Build (see above)
2) Preview changes with the example config:

```sh
./dockform plan -f ./example/config.yml
```

3) Apply changes (you’ll be asked to confirm):

```sh
./dockform apply -f ./example/config.yml
```

To run non-interactively (e.g., CI), bypass confirmation with:

```sh
./dockform apply --skip-confirmation -f ./example/config.yml
```

### Configuration

Dockform reads a YAML file describing your desired state. Minimal example:

```yaml
docker:
  context: "default"         # docker CLI context name
  identifier: "my-project"   # optional; used to label and scope managed resources

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

Remote Docker? Configure a context first:

```sh
docker context create \
  --docker host=ssh://user@host \
  --description="Remote daemon" \
  my-remote-docker

docker context use my-remote-docker
```

### How It Works

- Identifier labeling: all managed resources get label `io.dockform/<identifier>=1`.
- Service comparison: desired hash uses `docker compose config --hash <service>` (with a temp overlay to include the label). Running hash is read from `com.docker.compose.config-hash` on containers.
- Actions:
  - `[noop]` no change required
  - `↑` create/start
  - `→` change/update
  - `↓` prune orphaned/labeled resources

### CLI Reference

Global flags:

- `-c, --config`: Path to configuration file or directory (defaults to `dockform.yml` or `dockform.yaml` in current directory)
- `-v, --verbose`: Verbose error output

Commands:

- `dockform plan [flags]`
  - Parses/validates config and prints a plan of changes.

- `dockform apply [flags]`
  - Prints the plan, applies changes, then prunes labeled orphans.
  - Prompts for confirmation; use `--skip-confirmation` to bypass.

- `dockform filesets plan [flags]`
  - Prints only fileset-related plan lines.

- `dockform filesets apply [flags]`
  - Applies only fileset diffs (no confirmation prompt).

- `dockform validate [flags]`
  - Validates configuration and environment (e.g., Docker context).

- `dockform secret ...`
  - Secret management helpers (see `dockform secret --help`).

- `dockform manifest ...`
  - Manifest helpers (see `dockform manifest --help`).

### Non-interactive / CI

Use `--skip-confirmation` with `apply` to disable the interactive prompt.

```sh
dockform apply --skip-confirmation -c ./dockform.yml
```

### Troubleshooting

- Docker daemon/context errors: ensure the daemon is reachable from the selected context.
- Exit codes:
  - 2: invalid input
  - 69/70: unavailable/timeout/external errors (e.g., docker failures)
  - 1: unexpected error

### Project

Project home: https://github.com/gcstr/dockform

