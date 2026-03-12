# Husky

Local-first job scheduler with dependency graphs.

Husky is a single-binary scheduler for developers and small teams that want more than cron without introducing external infrastructure. Jobs are declared in `husky.yaml`, executed as a DAG, persisted in local SQLite, and operated through a CLI, HTTP API, and embedded web dashboard.

> Alpha status: Husky is in public alpha. Breaking changes are still likely, APIs and config may change.

## Philosophy

**Workflows should evolve with code and be managed like code.**

That is the core idea behind Husky.

Unlike infrastructure-first DAG schedulers, Husky is intentionally project-scoped and Git-versioned. The workflow definition lives next to the application or repository it automates, so changes to jobs, retries, dependencies, notifications, and schedules can be reviewed, diffed, branched, reverted, and shipped with the rest of the codebase.

Husky is designed for developers first:

- the workflow definition belongs in the repo
- workflow changes belong in pull requests
- branches can evolve workflow behavior alongside code changes
- local runtime state is disposable and regenerated when needed
- the scheduler should feel like part of the project, not a separate platform
- job scheduling should stay as readable as possible, using forms like `every:15m`, `on:[monday,friday]`, and `after:build` instead of raw cron syntax whenever practical

## Why Husky

Husky combines scheduling, orchestration, and observability in one local runtime:

- project-scoped workflows that live in the repository, not in a separate control plane
- Git-friendly automation: `husky.yaml` can be reviewed, diffed, branched, and versioned like application code
- designed for developers first, with readable YAML instead of platform-specific orchestration UIs
- a workflow model that evolves with the codebase instead of drifting away from it in external scheduler state
- readable scheduling syntax instead of cron expressions for the workflows Husky is designed to run
- dependency-aware execution with `depends_on` and `after:<job>`
- retries, backoff, and configurable failure handling
- per-job healthchecks, SLA budgets, tags, and audit metadata
- output passing between jobs using `{{ outputs.job.var }}` templates
- built-in HTTP API, WebSocket log streaming, and dashboard
- single deployable `husky` binary with embedded dashboard assets
- SQLite persistence with no external database required

## How it works

Husky ships one executable: `husky`.

- `husky start` launches the embedded daemon in the background
- `husky daemon run` runs the daemon in the foreground for debugging or service managers
- the CLI talks to the daemon over a local Unix socket
- the daemon stores state in SQLite and serves the REST API and dashboard

The repository-owned files are the important part:

- `husky.yaml` is the project workflow definition
- optional `huskyd.yaml` is the project or environment runtime config
- `.husky/` is generated local runtime state and should not be committed

That split is deliberate: the workflow is versioned, while the runtime state is disposable.

Runtime files in the data directory typically include:

- `husky.sock` — CLI ↔ daemon IPC socket
- `husky.pid` — daemon PID file
- `husky.db` — SQLite state database
- `api.addr` — bound HTTP address for dashboard/API discovery
- `huskyd.log` — daemon log file when configured

The default data directory is `.husky/`. Husky recreates it as needed, so it belongs in `.gitignore` rather than source control.

## Project-scoped and Git-versioned by design

Husky is built around a simple idea: a workflow definition should live with the project it automates.

That means:

- a feature branch can change application code and `husky.yaml` together
- a pull request can show exactly how job schedules, DAG edges, and retry policy changed
- a revert can roll back workflow behavior the same way it rolls back code
- a fresh clone can recover local scheduler state by running Husky again, because `.husky/` is regenerated

In Husky, the durable artifact is the workflow definition, not the runtime state.

- `husky.yaml` is meant to be committed
- optional `huskyd.yaml` is meant to be committed when runtime config should be versioned with the project
- `.husky/` is meant to be ignored and regenerated

For day-to-day development, this is a major difference from heavier DAG platforms. Husky is meant to feel like a developer tool in the repo, not a separate platform that owns the workflow definition elsewhere.

## Installation

### From source

```bash
make build
export PATH="$PWD/bin:$PATH"
husky version
```

### From a release archive

Use the platform archive from GitHub Releases, extract it, and place `husky` on your `PATH`.

### Via install script

```bash
curl -fsSL https://raw.githubusercontent.com/husky-scheduler/husky/main/install.sh | sh
```

### Via Homebrew

```bash
brew tap husky-scheduler/husky
brew install husky
```

### Homebrew / service packaging

Packaging assets live under `packaging/` and include:

- Homebrew formula template
- systemd unit
- launchd plist
- nfpm hooks for package builds

See [docs/operations.md](docs/operations.md) for service-manager guidance.

## Quick start

Create `husky.yaml`:

```yaml
version: "1"
defaults:
  timeout: "30m"
  retries: 2
  retry_delay: exponential
  timezone: "America/New_York"

jobs:
  ingest:
    description: "Download daily data"
    frequency: on:[monday,tuesday,wednesday,thursday,friday]
    time: "0200"
    command: "./scripts/ingest.sh"
    output:
      file_path: last_line

  transform:
    description: "Transform the ingested file"
    frequency: after:ingest
    command: "python transform.py --input {{ outputs.ingest.file_path }}"
    timeout: "20m"
    on_failure: stop
```

Optionally create `huskyd.yaml` for daemon runtime settings:

```yaml
api:
  addr: 127.0.0.1:8420
log:
  level: info
storage:
  engine: sqlite
  sqlite:
    path: .husky/husky.db
```

Start the daemon:

```bash
husky start
```

Check status and run jobs:

```bash
husky status
husky run ingest --reason "manual smoke test"
husky history ingest
husky logs ingest --tail
husky dash
```

Validate config before starting or reloading:

```bash
husky validate --strict
```

## CLI reference at a glance

### Daemon lifecycle

- `husky start`
- `husky daemon run`
- `husky stop [--force]`
- `husky reload`
- `husky status`
- `husky dash`

### Job operations

- `husky run <job> [--reason ...]`
- `husky retry <job>`
- `husky cancel <job>`
- `husky skip <job>`
- `husky pause --tag <tag>`
- `husky resume --tag <tag>`
- `husky run --tag <tag>`

### Observability

- `husky logs <job> [--run <id>] [--tail] [--include-healthcheck]`
- `husky history <job> --last <n>`
- `husky audit [--job ... --status ... --trigger ... --reason ... --tag ...]`
- `husky dag [--json]`
- `husky export --format=json`

### Configuration and integrations

- `husky validate [--strict]`
- `husky config show`
- `husky tags list`
- `husky integrations list`
- `husky integrations test <name>`

## Core concepts

## Community and project policies

- Contribution guide: [CONTRIBUTING.md](CONTRIBUTING.md)
- Code of conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- Security policy: [SECURITY.md](SECURITY.md)

## Releases

Releases are automated from Git tags.

Create and push a tag matching `v*` to trigger the release workflow, for example:

```bash
git tag v0.1.0-alpha.1
git push origin v0.1.0-alpha.1
```

The release workflow publishes GitHub release artifacts and updates the `husky-scheduler/homebrew-husky` tap automatically.

Set `HOMEBREW_TAP_GITHUB_TOKEN` to a GitHub personal access token with repository contents write access to the tap repository.

See [.github/workflows/release.yml](.github/workflows/release.yml) for the release workflow.

Release notes and versioning policy live in [CHANGELOG.md](CHANGELOG.md).

### `husky.yaml`

`husky.yaml` defines jobs, dependencies, schedules, retries, notifications, tags, healthchecks, outputs, and timezone behavior.

### `huskyd.yaml`

`huskyd.yaml` controls the daemon itself: API binding, auth, RBAC, TLS, logging, storage, scheduler limits, executor options, dashboard options, and process-level settings.

### DAG execution

Husky builds a dependency graph from explicit `depends_on` edges and implicit `after:<job>` frequencies. Cycles are rejected before the daemon starts or reloads config.

### Run lifecycle

A run typically moves through:

`PENDING → RUNNING → SUCCESS | FAILED | SKIPPED`

Retries, healthchecks, SLA flags, and failure policy can change how a run completes and whether downstream jobs continue.

## Documentation map

- [docs/index.md](docs/index.md) — documentation hub
- [docs/configuration.md](docs/configuration.md) — `husky.yaml` and `huskyd.yaml` reference
- [docs/security.md](docs/security.md) — auth, TLS, RBAC, and operational hardening
- [docs/crash-recovery.md](docs/crash-recovery.md) — startup, stale PID handling, orphan reconciliation, catchup
- [docs/output-passing.md](docs/output-passing.md) — output capture modes and pipeline templating
- [docs/notifications.md](docs/notifications.md) — integrations, channels, templates, log attachments
- [docs/operations.md](docs/operations.md) — running Husky locally and under service managers
- [docs/dashboard.md](docs/dashboard.md) — dashboard and API-backed workflows
- [docs/testing.md](docs/testing.md) — unit, integration, and manual test workflows
- [docs/task.md](docs/task.md) — implementation task tracker

## Documentation site

The markdown source of truth stays in [docs/index.md](docs/index.md), while the Docusaurus app that renders the site lives in [docs-site/README.md](docs-site/README.md).

Common docs-site commands:

- `make docs-install`
- `make docs-dev`
- `make docs-build`

## Alpha scope and known issues

Known issues for the current alpha:

- Windows release archives may exist, but service-manager packaging and operator documentation are focused on macOS and Linux
- OIDC auth is declared in config surface but not yet implemented end to end
- Postgres storage is not yet ready for production use
- metrics, tracing, and secrets backends are still partial runtime surfaces
- large-scale performance characterization is still limited during alpha hardening

## Current scope and known limits

Implemented today:

- single-binary CLI + embedded daemon model
- SQLite-backed state store
- dashboard, REST API, and WebSocket log streaming
- tags, audit, healthchecks, SLA flags, output passing, timezone-aware scheduling

Declared in config but not yet fully available end to end:

- OIDC auth
- Postgres storage
- broader metrics, tracing, and secrets backends beyond the currently wired runtime surface

## Alpha release artifacts

The current alpha release story includes:

- source code in this repository
- prebuilt GitHub release archives
- Homebrew tap publishing through `husky-scheduler/homebrew-husky`
- launchd and systemd packaging assets under `packaging/`
- documentation site publishing from `docs/` and `docs-site/`

## Development

Common development commands:

```bash
make build
make test
make lint
make run
make dist
```

The frontend dashboard is built from `web/` into embedded assets under `internal/api/dashboard/`.

## License

Husky is released under the [MIT License](LICENSE).

## Status

Husky is under active development. The codebase has already moved to the single-binary runtime model, and documentation is being consolidated under `docs/`.
