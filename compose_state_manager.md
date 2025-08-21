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

networks:
  demo-network:
    external: false              # optional; default false. If true, treat as external
```

---

## 5. Validation Rules

- `applications` keys: must match regex `^[a-z0-9_.-]+$`.
- `root`: required, must be a valid relative path.
- `files`: optional, defaults to `<root>/docker-compose.yml`.
- `volumes` and `networks`: must be expressed in **map form** only (e.g. `demo-volume: { external: false }`).
- For both resources, `external` is optional boolean (default `false`).
- `docker.context`: optional, defaults to `default`.
- Config file: if `--file` is not provided, defaults to `config.yml` or `config.yaml` in the current directory.

---

## 6. State Comparison

When running `plan` or `apply`, compare the following attributes between **desired** and **current state** for each **application**:

- image
- name
- port
- command
- entrypoint
- env
- labels
- volumes (service mounts and top-level volume existence/`external`)
- networks (service attachments and top-level network existence/`external`)
- resources (CPU/mem)
- healthcheck

For **top-level resources**:

- **Volumes**: presence, `external` flag semantics, driver/options (if discoverable), and references by services.
- **Networks**: presence, `external` flag semantics, driver/options (if discoverable), and references by services.

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

---

## 9. Dependencies

- Go (1.22+)
- [go-playground/validator](https://github.com/go-playground/validator) for YAML validation
- [Cobra](https://github.com/spf13/cobra) for CLI structure
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) for terminal styling
- Docker CLI installed and accessible
- Source: [github.com/gcstr/dockform](https://github.com/gcstr/dockform)

---

## 10. Future Enhancements (Optional Roadmap)

- JSON/YAML diff export for automation.
- Interactive approval step before apply.
- Dry-run mode for `apply` (skip execution).
- Support for secrets & configs.
- State caching to speed up large projects.

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

