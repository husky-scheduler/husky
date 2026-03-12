---
title: CLI overview
sidebar_label: CLI overview
description: The main Husky commands and how they map to the daemon runtime.
---

# CLI overview

## Daemon lifecycle

### Start in background

```bash
husky start
```

Starts the daemon, writes runtime files into `.husky/`, and returns when the socket is ready.

### Run in foreground

```bash
husky daemon run
```

Useful for local debugging, service managers, and manual scenario validation.

### Stop

```bash
husky stop
husky stop --force
```

`--force` immediately terminates in-flight work.

### Reload config

```bash
husky reload
```

Triggers hot reload without interrupting running jobs.

## Validation and config

```bash
husky validate
husky validate --strict
husky config show
```

Use `config show` to inspect the effective config after defaults are applied.

## Job control

```bash
husky run <job>
husky run <job> --reason "why this was triggered"
husky retry <job>
husky cancel <job>
husky skip <job>
```

## Status and history

```bash
husky status
husky history <job>
husky audit
husky dag
husky dag --json
```

## Logs

```bash
husky logs <job>
husky logs <job> --run <id>
husky logs <job> --tail
husky logs <job> --include-healthcheck
husky logs --tag nightly --tail
```

## Tag-based operations

```bash
husky tags list
husky status --tag <tag>
husky run --tag <tag>
husky pause --tag <tag>
husky resume --tag <tag>
```

## Integrations

```bash
husky integrations list
husky integrations test <name>
```

## Export

```bash
husky export --format=json
```

## Global flags

Most commands support:

- `--config` to select a `husky.yaml`
- `--data` to select the runtime directory

Example:

```bash
husky --config ./envs/prod/husky.yaml --data ./.husky-prod status
```

## Failure behavior when the daemon is absent

Commands that require the daemon fail clearly when it is not running. This includes operations such as `status`, `run`, `cancel`, and `reload`.
