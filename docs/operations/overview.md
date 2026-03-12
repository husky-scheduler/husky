---
title: Operations
sidebar_label: Introduction
description: Running, packaging, and operating Husky in local and service-managed environments.
---

# Operations

This section covers the runtime side of Husky after workflow authoring is complete.

## Common operator tasks

- validate config before deployment
- start or stop the daemon
- inspect status, history, and logs
- reload config safely
- secure the API
- recover after crashes
- package Husky under system managers

## Normal lifecycle

```bash
husky validate --strict
husky start
husky status
husky reload
husky stop
```

## Foreground debugging

Use:

```bash
husky daemon run
```

This is the simplest way to observe scheduler behavior, retry timing, and log output directly in a terminal.

## Runtime state

The daemon writes generated state under `.husky/` by default. That directory typically contains:

- PID file
- socket
- SQLite database
- API address file
- daemon log file

## Production-minded concerns

Husky supports runtime configuration for:

- bind address and base path
- TLS
- bearer or basic auth
- RBAC
- logging output and format
- SQLite path and retention
- executor pool size and shell
- metrics and tracing settings in daemon config

## Related guides

- [Dashboard](../operations/dashboard.md)
- [API](../operations/api.md)
- [Crash recovery](../operations/crash-recovery.md)
- [Security](../operations/security.md)
- [Testing](../operations/testing.md)
- [Packaging](../operations/packaging.md)
