---
title: Overview
sidebar_label: Overview
description: Documentation hub for Husky, including overview, quickstart, workflow authoring, and operations.
slug: /
---

# Husky documentation

Husky is a local-first scheduler and workflow runner for repository-owned automation.

> Alpha status: Husky is in public alpha. Expect breaking changes before beta, and avoid critical production use until the runtime and packaging story harden further.

This docs site is organized in the same broad style as modern workflow-engine documentation: start with the product model, move into quickstart, then authoring, then runtime operations.

## Start here

- [What is Husky?](overview/introduction.md)
- [Installation](getting-started/installation.md)
- [Quickstart](getting-started/quickstart.md)
- [CLI overview](getting-started/cli.md)

## Documentation sections

### Overview

- [What is Husky?](overview/introduction.md)
- [Architecture](overview/architecture.md)
- [Core concepts](overview/core-concepts.md)

### Getting started

- [Installation](getting-started/installation.md)
- [Quickstart](getting-started/quickstart.md)
- [CLI overview](getting-started/cli.md)

### Writing workflows

- [Introduction](writing-workflows/overview.md)
- [YAML reference](writing-workflows/yaml-reference.md)
- [Scheduling](writing-workflows/scheduling.md)
- [Dependencies and DAGs](writing-workflows/dependencies.md)
- [Retries and concurrency](writing-workflows/retries-and-concurrency.md)
- [Output passing](writing-workflows/output-passing.md)
- [Healthchecks and SLAs](writing-workflows/healthchecks-and-slas.md)
- [Notifications](writing-workflows/notifications.md)
- [Tags, audit, and timezones](writing-workflows/tags-audit-and-timezones.md)

### Operations

- [Introduction](operations/overview.md)
- [Dashboard](operations/dashboard.md)
- [API and WebSockets](operations/api.md)
- [Crash recovery](operations/crash-recovery.md)
- [Security](operations/security.md)
- [Testing](operations/testing.md)
- [Packaging and deployment](operations/packaging.md)

## What Husky consists of

- `cmd/husky/` — user-facing CLI entrypoint
- `cmd/huskyd/` — embedded daemon runtime package, invoked through `husky daemon run`
- `internal/config/` — `husky.yaml` schema, defaults, validation, env interpolation
- `internal/daemoncfg/` — `huskyd.yaml` schema, defaults, validation
- `internal/scheduler/` — schedule evaluation and timezone/DST logic
- `internal/dag/` — DAG build, cycle detection, topological order
- `internal/executor/` — subprocess execution, timeout handling, healthchecks, output capture
- `internal/store/` — SQLite persistence layer
- `internal/api/` — REST API, WebSocket log streaming, embedded dashboard serving
- `internal/ipc/` — local CLI ↔ daemon socket protocol
- `internal/notify/` — notification dispatch and alert persistence
- `web/` — dashboard source app
- `tests/` and `cmd/huskyd/*integration*_test.go` — integration coverage

## Recommended reading order

1. [What is Husky?](overview/introduction.md)
2. [Quickstart](getting-started/quickstart.md)
3. [YAML reference](writing-workflows/yaml-reference.md)
4. [Scheduling](writing-workflows/scheduling.md)
5. [Dependencies and DAGs](writing-workflows/dependencies.md)
6. [Operations](operations/overview.md)

## Scope note

Documentation in this folder describes the current single-binary Husky runtime and the features surfaced by the repository today. Older planning material in `dev-docs/` remains useful background, but this folder is the maintained reference.

Current alpha caveats:

- OIDC auth is not complete yet
- Postgres storage is not complete yet
- metrics, tracing, and secrets backends are not fully wired end to end yet
- service-manager guidance is strongest on macOS and Linux

- background daemon started with `husky start`
- foreground daemon path `husky daemon run`
- single shipped `husky` executable

## License

Husky is released under the [MIT License](https://github.com/husky-scheduler/husky/blob/main/LICENSE).
