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
| `pull_multilayer.jsonl` | 14-layer image pull (postgres:16.4-bookworm), 176 events |
| `start_stopped.jsonl` | `up` against an existing stopped container |
| `pull_fully_qualified_ref.jsonl` | pull of `docker.io/library/alpine:3.18`, written fully qualified |
| `pull_shared_base_layer.jsonl` | pull of python:3.12-slim with python:3.11-slim's debian base layer already cached |
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

## Pull percentage: what the multi-layer capture proved

Four aggregation algorithms were run against `pull_multilayer.jsonl`:

| algorithm | went backwards | first reading |
|---|---|---|
| naive: latest current/total, both phases mixed | 9 times | 100% (a tiny layer finishing first) |
| download-only, denominator grows as layers appear | 4 times | 100% |
| download-only, wait until every layer total is known | 0 | event 86 (34.7%), then stuck at 100% for 29 events during extraction |
| **download + extract bytes over 2 x total, frozen denominator** | **0** | **event 86 (25.3%)**, reaches 100% only when extraction is actually done |

Two facts drive this:

- Every layer runs TWO byte-counted phases, `Downloading` then `Extracting`, each
  0 -> 100%. Anything that mixes them naively goes backwards.
- A layer's `total` is only reported when that layer STARTS downloading, and docker
  downloads a few layers at a time (the `Waiting` events). So the full size of an
  image is unknown until its last layer begins. A percentage that never goes
  backwards cannot start before then — show "pulling..." until it can.

Events carry no timestamps, so event position is not wall-clock time.

## Verified while writing the implementation plan

- **Stopped container:** `up` emits exactly `Starting` then `Started`. No `Creating`,
  no `Recreate`.
- **Image refs are not normalised.** `compose config` keeps each ref as written, and
  the pull event's `Image` id matches it byte for byte — including the fully-qualified
  `docker.io/library/alpine:3.18`. Exact string matching between an event and
  `ComposeConfigFull` is correct.
- **A cached layer emits exactly ONE event**: `Already exists`, with no `total`, and is
  never announced by `Pulling fs layer`. A percentage accumulator must therefore record
  a layer as announced ONLY on `Pulling fs layer`. Announcing on any layer event makes
  the cached layer a permanently total-less member of the set, and the percentage never
  appears — measured on `pull_shared_base_layer.jsonl`: announcing on any event gives
  0 readings; announcing on `Pulling fs layer` gives 10 readings, 60.4% to 100%, never
  backwards. Pulls that share a base layer with a cached image are the common case for
  image updates.
