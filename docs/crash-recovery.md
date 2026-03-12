---
title: Crash recovery guide
sidebar_label: Crash recovery
description: Startup, orphan reconciliation, catchup behavior, and recovery expectations after crashes or restarts.
sidebar_position: 7
---

# Husky crash recovery and startup sequence

This guide explains what the daemon does at startup, how it handles stale runtime artifacts, and how catchup and orphan reconciliation work.

## Startup sequence overview

When `husky daemon run` starts, the daemon performs a predictable sequence:

1. load and validate `husky.yaml`
2. load and validate `huskyd.yaml`
3. build and validate the DAG
4. ensure the data directory exists
5. acquire or overwrite the PID file if stale
6. open the SQLite store
7. configure auth and logging
8. construct the executor and scheduler
9. reconcile orphaned runs
10. reconcile missed schedules for catchup-enabled jobs
11. start the IPC server
12. start the HTTP API and embedded dashboard
13. begin periodic scheduler ticks and retention vacuuming

This ordering matters:

- invalid config prevents the daemon from starting
- stale PID detection happens before normal serving begins
- orphan reconciliation and catchup happen before regular steady-state scheduling

## PID file handling

The daemon stores its PID in:

```text
<data>/husky.pid
```

Behavior:

- if the PID file exists and the PID is alive, startup fails with `daemon already running`
- if the PID file exists but the PID is dead, Husky logs a stale PID warning and overwrites it
- the PID file is removed on normal shutdown

## IPC socket and API address files

The daemon also manages:

- `husky.sock` — local IPC socket for CLI control
- `api.addr` — actual HTTP bind address written after the API listener starts

These runtime files are regenerated as part of normal startup.

## Orphan reconciliation

### What counts as an orphan

An orphan is a run left in `RUNNING` state because the daemon exited or crashed before it could finalize that run.

At startup, Husky queries all `RUNNING` runs and treats them as orphaned work from the previous daemon invocation.

### What Husky does

For each orphaned run:

1. mark the run `FAILED`
2. log the orphan reconciliation event
3. inspect the job's retry policy
4. if retries remain, schedule a retry after the configured backoff delay

Important behavior:

- the daemon does not assume the old process is still healthy
- the orphaned run is finalized conservatively as failed work
- retries continue using the job's configured retry policy

## Catchup behavior

Catchup is controlled per job with `catchup: true` in `husky.yaml`.

At startup, for each catchup-enabled job, Husky checks `job_state.next_run`.

If `next_run` is in the past:

- and within `scheduler.catchup_window` if one is configured, Husky triggers the missed run immediately
- if outside the catchup window, Husky skips the stale execution and logs why

### Why `catchup_window` exists

Without a window, a long outage could cause very old missed schedules to execute after restart. `catchup_window` prevents that backlog flood.

Example:

```yaml
scheduler:
  catchup_window: "24h"
```

## Hot reload and crash recovery

`husky reload` and SIGHUP do not interrupt running jobs.

Reload sequence:

1. load new `husky.yaml`
2. validate schema and semantics
3. rebuild the DAG
4. atomically swap config and graph if successful
5. keep the old config if validation fails

Bearer tokens from `auth.bearer.token_file` are also reloaded on SIGHUP.

## Data durability

Husky uses SQLite in WAL mode and serializes writes through a single writer goroutine. This improves resilience under normal operation and reduces lock contention.

Recovery-relevant persisted data includes:

- run status and attempts
- next scheduled run timestamps
- log lines
- output variables by `cycle_id`
- alert delivery history

## Failure scenarios and expected outcomes

### Daemon crashes while a job is running

Expected outcome on next startup:

- run is marked failed
- retry may be scheduled if configured
- downstream jobs do not treat the orphaned run as success

### Machine is down during a scheduled time

Expected outcome on next startup:

- jobs with `catchup: true` may run immediately
- jobs with `catchup: false` simply advance to the next future schedule
- jobs outside the configured catchup window are skipped

### Config reload introduces a cycle

Expected outcome:

- reload is rejected
- current running config remains active
- running jobs continue uninterrupted

### Stale PID file remains after an unclean exit

Expected outcome:

- Husky probes the PID
- dead PID is treated as stale
- file is overwritten and startup continues

## Operational recommendations

- use a stable dedicated data directory in service environments
- pair `catchup: true` with a sensible `scheduler.catchup_window`
- monitor daemon logs after restarts to understand catchup and orphan events
- use integration tests under `cmd/huskyd/` and `tests/` when changing recovery behavior

## Related testing coverage

Relevant integration coverage includes:

- orphan reconciliation on restart
- catchup true / false behavior
- reload rejection when a cycle is introduced
- reload while jobs are still running

See [testing.md](testing.md) for commands.
