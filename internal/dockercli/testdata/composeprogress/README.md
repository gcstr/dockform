# compose --progress json fixtures

Captured from real `docker compose --progress json up -d` runs, compose **v5.1.2**,
on 2026-09-17. Do not hand-edit: `--progress json` is not a documented stable
compose API, so these files are the only record of what it actually emits.
Re-capture rather than adjust when compose changes.

| file | scenario |
|---|---|
| `create.jsonl` | two services created from nothing |
| `recreate.jsonl` | the common apply path — both services recreated |
| `container_name_override.jsonl` | a service using `container_name:` |
| `pull_with_layers.jsonl` | image pull, with per-layer progress |
| `error.jsonl` | pull failure, including the terminal error line |
| `depends_on_healthy.jsonl` | `depends_on: condition: service_healthy` from nothing |
| `partial_change_with_unchanged_deps.jsonl` | only one service changed; its unchanged dependency must turn healthy again |

## What the capture established

1. **Event shape is `{id, status, text}`** — plus `details`, `current`, `total`,
   `percent` on layer progress, and `parent_id` on layer events.
2. **`status` is one of `Working`, `Done`, `Error`.**
3. **`id` for a container is the CONTAINER NAME, not the service name**
   (`Container skqfixture-alpha-1`). With `container_name:` set there is no trace
   of the service at all (`Container totally-custom-name`). Mapping an event back
   to a service therefore cannot be done by parsing the name — dockform must use
   the service→container_name mapping from the labeled compose document it
   already builds.
4. **The terminal error line has a DIFFERENT SHAPE**: `{"error":true,"message":...}`,
   with no `id`, `status` or `text`. A parser assuming the base shape mishandles it.
6. **Healthcheck waits are visible, on the DEPENDENCY.** `Container db: Waiting`
   then `Healthy`. The blocked dependent (`app`) emits nothing during the wait.
7. **Unchanged services emit events.** When only `app` changed, `bystander` and
   `db` still emitted `Done Running`, and `db` emitted `Waiting`/`Healthy` again
   because `app` depends on it. dockform seeds only CHANGED services, so these
   events name containers the view has no line for.
5. **Pull is noisy**: 15 lines for a single service, most of them per-layer, all
   carrying `parent_id` so they can be filtered or aggregated.
