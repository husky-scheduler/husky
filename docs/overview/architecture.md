---
title: Architecture
sidebar_label: Architecture
description: How the Husky binary, daemon, store, API, and dashboard fit together.
---

# Architecture

Husky is intentionally compact. One binary owns the scheduler, executor, local API, dashboard, and state store.

## High-level design

```text
husky.yaml + huskyd.yaml
          │
          ▼
     husky daemon
          │
 ┌────────┼────────┬───────────────┬───────────────┐
 ▼        ▼        ▼               ▼               ▼
DAG    scheduler executor       SQLite        HTTP/WebSocket
build   loop      pool          state         API + dashboard
```

## Main runtime pieces

### CLI

The `husky` CLI starts and controls the daemon, validates config, and exposes operator commands such as:

- `start`
- `stop`
- `reload`
- `status`
- `run`
- `logs`
- `history`
- `audit`
- `dag`

Most commands talk to the daemon through a local socket.

### Daemon

The daemon is the long-running process that:

- loads config
- computes schedules
- dispatches jobs
- tracks retries
- runs healthchecks
- persists state
- serves the dashboard and API

It can run in the background with `husky start` or foreground with `husky daemon run`.

### Config loaders

Husky separates workflow definition from daemon runtime settings:

- `husky.yaml` defines jobs
- `huskyd.yaml` defines API, auth, TLS, logging, storage, limits, and runtime behavior

Both are validated before use.

### Scheduler

The scheduler resolves wall-clock schedules using per-job timezone settings. It handles:

- default run times
- interval schedules
- day-list schedules
- DST gaps and overlaps
- catchup after restart

### DAG engine

Dependency edges come from both `depends_on` and `after:<job>`. Husky topologically sorts the graph and rejects cycles before startup or reload completes.

### Executor

Jobs run as subprocesses. Husky captures stdout and stderr in real time, enforces timeouts, and kills process groups when required.

Execution logic also handles:

- retries with backoff and jitter
- concurrency policies
- output capture
- downstream template rendering
- post-run healthchecks

### State store

Husky persists local runtime state in SQLite using WAL mode. Important tables include:

- `job_runs`
- `job_state`
- `run_logs`
- `run_outputs`
- `alerts`

This gives the dashboard and CLI a durable source of truth without external services.

### API and dashboard

The daemon serves:

- REST endpoints under `/api/*`
- live log streaming under `/ws/logs/*`
- an embedded dashboard at `/`

The dashboard is compiled from the `web/` app and embedded into the binary.

## Request and execution flow

### Scheduled run

1. Scheduler computes a due job.
2. DAG rules and job state are checked.
3. A run row is created.
4. The executor launches the subprocess.
5. Logs stream into SQLite and WebSocket clients.
6. Output variables are captured.
7. Healthcheck runs if configured.
8. Alerts and downstream triggers are evaluated.
9. `job_state` is updated.

### Manual run

A manual trigger follows the same execution path, but the run is recorded with `trigger=manual` and may carry a reason string.

## Data directories

By default Husky stores runtime state under `.husky/`.

Common files:

- `husky.sock`
- `husky.pid`
- `husky.db`
- `api.addr`
- `huskyd.log`

These are generated artifacts and should not be committed.

## Platform model

Husky is built for Linux, macOS, and Windows. Process handling, sockets, signal behavior, and detach logic use platform-specific code where needed, but the workflow model stays the same.

## Design boundaries

Husky is local-first. It does not require:

- a separate control plane
- a message broker
- an external database
- a Kubernetes deployment

That constraint keeps the product simple and makes the repository the center of workflow ownership.
