---
title: Operations guide
sidebar_label: Operations
description: How to run, reload, package, and operate Husky locally or under service managers.
sidebar_position: 3
---

# Husky operations guide

This guide covers how to run Husky in development, as a local background service, and under OS service managers.

## Runtime model

Husky now ships as one executable: `husky`.

Important commands:

- `husky start` — start the embedded daemon in the background
- `husky daemon run` — run the daemon in the foreground
- `husky stop` — graceful shutdown
- `husky stop --force` — force stop
- `husky reload` — hot-reload `husky.yaml`
- `husky dash` — open the dashboard in a browser

## Data directory

By default, Husky uses `.husky` under the current working directory. Override it with `--data`.

Typical contents:

- `husky.sock` — Unix socket for CLI IPC
- `husky.pid` — daemon PID file
- `husky.db` — SQLite database
- `api.addr` — dynamically bound API address
- `huskyd.log` — daemon log output when configured

## Starting Husky

### Background mode

```bash
husky start --config husky.yaml --data .husky
```

This re-execs the current `husky` binary into background mode and returns control to the shell.

### Foreground mode

```bash
husky daemon run --config husky.yaml --data .husky
```

Use this when:

- debugging startup or scheduling behavior
- running under `systemd`, `launchd`, or another process manager
- wanting daemon logs in the current terminal

## Stopping and reloading

Graceful stop:

```bash
husky stop
```

Force stop:

```bash
husky stop --force
```

Reload config in place:

```bash
husky reload
```

Hot reload reparses `husky.yaml`, rebuilds the DAG, and swaps config atomically. Running jobs are not interrupted. If the new config is invalid, the old config remains active.

## Common operator commands

### Status and schedules

```bash
husky status
husky dag
husky dag --json
```

### Manual execution

```bash
husky run my_job --reason "manual replay"
husky retry my_job
husky cancel my_job
husky skip my_job
```

### Tag-based bulk operations

```bash
husky tags list
husky status --tag release
husky run --tag release
husky pause --tag release
husky resume --tag release
```

### Logs and history

```bash
husky logs my_job
husky logs my_job --tail
husky logs my_job --run 42 --include-healthcheck
husky history my_job --last 20
husky audit --job my_job --status failed --reason hotfix
```

## Dashboard and API

The daemon serves an HTTP API and embedded dashboard. When `huskyd.yaml` does not set `api.addr`, Husky binds to `127.0.0.1:0` and writes the actual address into `<data>/api.addr`.

The CLI uses `api.addr` to discover the dashboard and WebSocket endpoints.

Open the dashboard with:

```bash
husky dash
```

## Packaging and services

### systemd

Reference unit file:

- `packaging/systemd/huskyd.service`

It runs Husky in foreground mode:

```text
/usr/bin/husky daemon run --config /etc/husky/husky.yaml --daemon-config /etc/husky/huskyd.yaml --data /var/lib/husky
```

### launchd

Reference plist:

- `packaging/launchd/com.husky-scheduler.huskyd.plist`

### Homebrew service

Reference formula:

- `packaging/homebrew/husky.rb`

The service stanza also runs `husky daemon run` directly.

## Build and release commands

Common development targets from `Makefile`:

```bash
make build
make test
make lint
make run
make dist
make package
make formula
```

What they do:

- `make build` — build the single `husky` binary
- `make test` — run Go tests and frontend tests
- `make run` — build and launch `husky daemon run`
- `make dist` — cross-compile and archive release artifacts
- `make package` — build snapshot packages with GoReleaser
- `make formula` — render the Homebrew formula from checksums

## Retention and vacuum

The daemon periodically vacuums old run data according to `huskyd.yaml` storage retention settings.

Relevant knobs:

- `storage.retention.max_age`
- `storage.retention.max_runs_per_job`

Completed runs may be pruned, but active `PENDING` and `RUNNING` rows are never vacuumed.

## Operational tips

- keep `husky.yaml` and `huskyd.yaml` alongside your project or in a stable config directory
- use `husky validate --strict` before reloading changes
- use a dedicated `--data` directory when running multiple daemons on one machine
- when exposing the API off localhost, also enable auth and TLS
- prefer `auth.bearer.token_file` over inline tokens in `huskyd.yaml`

## Known limits

These config surfaces exist but are not yet fully implemented across the runtime:

- OIDC auth
- Postgres storage
- some metrics, tracing, and secrets backends beyond the currently active runtime behavior
