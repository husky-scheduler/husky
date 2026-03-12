---
title: Crash recovery
sidebar_label: Crash recovery
description: PID files, stale lock handling, orphaned runs, catchup, and hot reload.
---

# Crash recovery

Husky is designed to recover cleanly from local crashes, abrupt termination, and configuration reloads.

## PID lock handling

On startup, Husky checks the daemon PID file.

Outcomes:

- live PID: startup is rejected as an already-running daemon
- dead PID: the stale lock is cleared and startup continues

## Orphaned runs

If the daemon crashes while a job is running, Husky reconciles that run on restart.

Typical sequence:

1. find persisted running state
2. detect dead process ownership
3. mark orphaned runs as failed
4. apply retry policy where appropriate

## Catchup

`catchup` controls missed schedules after downtime.

- `catchup: true` triggers missed work immediately within the configured recovery window
- `catchup: false` skips the missed fire time and advances the schedule

## Hot reload

`husky reload` triggers a config reload without interrupting in-flight jobs.

Reload behavior:

- parse new config fully first
- reject invalid config and keep the old one
- keep running jobs alive
- rebuild DAG and scheduling state

## Failure cases worth testing

- invalid config during reload
- cycle introduced in the new config
- stale PID file after forced kill
- daemon crash while a long-running job is active
- catchup behavior for scheduled jobs after restart

## Useful commands

```bash
husky reload
husky stop --force
husky history <job>
husky audit --status failed
```
