Dockform (early)

Manage Docker Compose projects declaratively. Define desired state in YAML, then plan/apply changes.

Quick start

1) Install Go 1.22+
2) Build:

```
go build ./cmd/dockform
```

3) Try the included example:

```
./dockform plan -f ./example/config.yml
```

Commands

- `dockform plan [-f config.yml]`: Parse and validate config, show a diff plan.
- `dockform apply [-f config.yml] [--prune]`: Show the plan, then apply (WIP).

Status

- MVP scaffolding: config parsing, minimal planner, styled output.
- Apply/prune and full service diffing are in progress.


