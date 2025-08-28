# Software Definition: Dockform

> Repository: [github.com/gcstr/dockform](https://github.com/gcstr/dockform)

## 1. Purpose

A CLI tool to **manage Docker Compose projects declaratively**.\
It ensures that the **current Docker state** matches the **desired state defined in a YAML file**, supporting planning, applying, and pruning of resources.

---

## 2. High-Level Features

- **Input**: A YAML file describing resources (volumes, networks, applications).
- **Validation**: Schema-based validation using [go-playground/validator](https://github.com/go-playground/validator).
- **Diff & Plan**: Compare current Docker state (via Docker CLI) with desired state and display differences.
- **Apply**: Apply the desired state in an idempotent way.
- **Prune**: Optionally delete unmanaged resources after confirmation.
- **Human-friendly output**: Color-coded diff with `[noop]`, `[add]`, `[remove]`, `[change]`.
- **CLI Framework**: Built with [Cobra](https://github.com/spf13/cobra).
- **Fancy Output Styling**: Uses [Lip Gloss](https://github.com/charmbracelet/lipgloss) for rich terminal visuals.

---

## 3. Command-Line Interface

### Commands

- `dockform plan [--file config.yml]`

  - Parse YAML, validate schema. If `--file` is omitted, defaults to `config.yml` or `config.yaml` in the current directory.
  - Resolve resources.
  - Compare current Docker state vs desired.
  - Display diff.

- `dockform apply [--file config.yml] [--prune]`

  - Runs `plan` first and shows diff.
  - Applies changes via Docker CLI.
  - Idempotent: running twice with no changes results in `[noop]` only.
  - If `--prune` is set, prompts for confirmation before deleting unmanaged resources.

---

## 4. Desired State Configuration (YAML)

### Root structure

```yaml
docker:
  context: "default" # used for docker CLI context
  identifier: "my-project" # optional; if provided, will be applied as a label to every managed resource

applications:
  <app-name>:
    root: ./website              # required, relative path
    files:                       # optional, list of compose files
      - docker-compose.yml
    profiles:                    # optional, list of profiles
      - staging
    env-file:                    # optional, list of env files
      - .env.staging
    project:                     # optional, overrides compose project name
      name: website

volumes:
  demo-volume:
    external: false              # optional; default false. If true, treat as external
    labels:                      # optional; custom labels for this volume
      owner: dockform

networks:
  demo-network:
    external: false              # optional; default false. If true, treat as external
    labels:                      # optional; custom labels for this network
      owner: dockform
```

---

## 5. Validation Rules

- `applications` keys: must match regex `^[a-z0-9_.-]+$`.
- `root`: required, must be a valid relative path.
- `files`: optional, defaults to `<root>/docker-compose.yml`.
- `volumes` and `networks`: must be expressed in **map form** only (e.g. `demo-volume: { external: false }`).
- For both resources, `external` is optional boolean (default `false`), `labels` is optional map.
- `docker.context`: optional, defaults to `default`.
- `docker.identifier`: optional, if provided Dockform adds it as a label to every managed resource and uses it to scope state comparison (ignores resources without this label).
- Config file: if `--file` is not provided, defaults to `config.yml` or `config.yaml` in the current directory.

---

## 6. State Comparison

Dockform uses **Compose’s native config-hash** to compare desired and running state for services:

- For each service, run `docker compose config --hash <service>` with the same inputs Dockform will use on apply (user files + identifier overlay).
- Read the running container label `com.docker.compose.config-hash` via `docker inspect`.
- If hashes match → `[noop]` for that service.
- If hashes differ or label missing → `[change]`.

For **top-level resources** (volumes and networks):
- **Volumes**: presence, `external` flag semantics, labels, driver/options (if discoverable), and references by services.
- **Networks**: presence, `external` flag semantics, labels, driver/options (if discoverable), and references by services.

Resources without the matching `docker.identifier` label are ignored during state comparison.

---

## 7. Diff Output Format

Each line is prefixed with a color-coded marker (styled with Lip Gloss):

- `[noop]` (blue): no change required.
- `[add]` (green): resource will be created.
- `[remove]` (red): resource will be deleted (only with `--prune`).
- `[change]` (yellow): resource will be updated.

Example (output of `dockform plan`):

```
[noop] network demo-network exists
[add]  volume demo-volume will be created
[change] service website/nginx image: nginx:1.24 -> nginx:1.25
[remove] container old-app will be removed
```

---

## 8. Resource Lifecycle

- **Volumes & Networks** (top-level):

  - **Default (managed)**: `external: false` (or omitted). The tool **owns** lifecycle: create/update as needed; delete only with `--prune` and confirmation.
  - **External**: `external: true`. Treated as pre-existing shared resources. The tool **verifies** existence and **creates them before applying** applications if missing; never deletes them (ignored by `--prune`).

- **Applications**:

  - Managed via `docker compose` using provided context, files, env-files, profiles, and project name.
  - Apply is **idempotent**: repeat runs produce no changes.
  - Changes are always displayed before execution.

### 8.1 Identifier Overlay Strategy (recommended)

To ensure every managed resource carries `io.dockform/<identifier>: "1"` **and** to keep Compose hashes consistent, Dockform uses a small ephemeral **overlay compose file**:

- **What gets generated**

  - For each service: adds label key `io.dockform/<identifier>` with value `"1"` (and nothing else).
  - For managed volumes/networks at creation time: adds the same label key and value to the resource.

- **How it is used**

  - Dockform writes the overlay to a **temp file** (e.g., `/tmp/dockform.overlay.yml`).
  - All Compose calls include the overlay via additional `-f` flag(s):
    - `docker compose -f <user files...> -f /tmp/dockform.overlay.yml --project-directory <root> [--env-file ...] [--profile ...] config --hash <service>`
    - `docker compose -f <user files...> -f /tmp/dockform.overlay.yml --project-directory <root> [--env-file ...] [--profile ...] up -d`
  - The temp file is removed after the command finishes (configurable: keep on `--debug`).

- **Why this works**

  - The **desired** hash (from `compose config --hash`) and the **running** containers are computed from the **same inputs**, so `com.docker.compose.config-hash` **matches**.
  - The identifier becomes a stable part of the Compose inputs without modifying user files.

- **Alternatives**

  - **STDIN/**``** piping** is possible but brittle with multiple files and merge semantics; the temp-file overlay is more compatible and explicit.

---

## 9. Dependencies

- Go (1.22+)
- [go-playground/validator](https://github.com/go-playground/validator) for YAML validation
- [Cobra](https://github.com/spf13/cobra) for CLI structure
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) for terminal styling
- Docker CLI installed and accessible (Compose v2.23.1+ required for `docker compose config --hash`)
- Source: [github.com/gcstr/dockform](https://github.com/gcstr/dockform)

---

## 10. Future Enhancements (Optional Roadmap)

- JSON/YAML diff export for automation.
- Interactive approval step before apply.
- Dry-run mode for `apply` (skip execution).
- Support for secrets & configs.
- State caching to speed up large projects.
- Multi-identifier coexistence: allow multiple Dockform configs to manage distinct sets of resources in the same Docker context, each scoped by its own `docker.identifier`.

---

## 11. Testing Strategy

### 11.1 Test Types

- **Unit Tests**

  - `config`: YAML parsing, normalization (defaults), and validation (regex, map-only resources).
  - `planner`: fine-grained diffs for services (image, ports, env, labels, volumes, networks, resources, healthcheck) and top-level volumes/networks (`external` semantics). Use **golden tests**.
  - `ui`: Lip Gloss renderers. Snapshot expected output. Provide helpers to **strip ANSI** when needed or snapshot with ANSI consistently.
  - `dockercli`: command building (args, env like `DOCKER_CONTEXT`) and stdout parsing.

- **Integration Tests** (opt-in)

  - Run against a local Docker daemon. Tag with build tag, e.g., `//go:build integration`.
  - Scenarios: create managed volume/network, ensure idempotent `compose up -d`, detect drift, `--prune` guarded deletion.

- **End-to-End (E2E) Smoke Tests**

  - Black-box: invoke the compiled CLI with fixture configs and assert on stdout (plan/apply) and Docker side-effects.

### 11.2 Test Fixtures

- `test/fixtures/configs/` with minimal, complex, and edge-case YAMLs.
- `test/fixtures/docker/` with canned JSON outputs for `docker ps`, `docker inspect`, `docker compose ps`, etc.
- `test/golden/` snapshots for planner diffs and UI output.

### 11.3 Fakes & Mocks

- **Fake Docker Adapter**: implement `dockercli.Exec` interface returning deterministic outputs and errors for scenarios.
- **Clock/Time Abstraction**: to make healthcheck intervals/timeouts deterministic.

### 11.4 CI Guidance

- Lint & vet (golangci-lint), unit tests, and golden verification on every PR.
- Integration/E2E jobs optional (nightly or on-demand) to keep the fast feedback loop.
- Cache Go build/test modules for speed.

### 11.5 Determinism & Snapshots

- Normalize unordered collections (env maps, labels, sets) before diffing.
- Stable sort for ports, mounts, networks.
- Provide `TEST_UPDATE_GOLDEN=1` convention to update snapshots when intentional changes occur.

### 11.6 Coverage & Goals

- Unit test coverage target: **≥80%** for `planner`, `config`, and `ui` packages.
- Critical paths (apply + prune confirmations) must have at least one E2E test.

### 11.7 Error Injection

- Simulate common failures: invalid YAML, missing context, docker not found, compose returns non‑zero, permission denied.
- Assert that user-facing errors are concise and actionable (no internal file\:line leaks).

### 11.8 Performance

- Benchmark discovery and planning on large fixture sets.
- Ensure discovery batches `docker inspect` where possible; verify no quadratic behavior.

