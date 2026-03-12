---
title: What is Husky?
sidebar_label: What is Husky?
description: Product overview, positioning, and the main capabilities of Husky.
---

# What is Husky?

Husky is a local-first job scheduler and workflow runner for repositories that need more than cron, but do not want a separate orchestration platform.

It is built around a simple model:

- define jobs in `husky.yaml`
- keep that file in Git next to the code it automates
- run one local daemon
- persist runtime state in SQLite
- operate everything through the CLI, HTTP API, and embedded dashboard

## Why it exists

Husky is designed for teams that want scheduling and orchestration to behave like application code:

- readable schedules instead of raw cron for common cases
- dependency-aware execution with DAG semantics
- retry and failure policy built into each job
- local state that is disposable and easy to recover
- observability without extra infrastructure

## What Husky includes

Husky ships as a single binary and covers the full runtime loop:

- scheduler
- DAG resolver
- subprocess executor
- retry engine
- healthchecks
- output passing
- notifications
- audit trail
- web dashboard
- REST API
- WebSocket log streaming

## Core capabilities

### Readable scheduling

Husky supports human-oriented schedule forms such as:

- `hourly`
- `daily`
- `weekly`
- `monthly`
- `weekdays`
- `weekends`
- `every:15s`
- `on:[monday,wednesday,friday]`
- `after:upstream_job`
- `manual`

### Dependency graphs

Jobs can depend on other jobs through `depends_on` or `after:<job>`. Husky builds a DAG, rejects cycles, and only dispatches downstream work when upstream requirements are satisfied.

### Reliability controls

Each job can define:

- `timeout`
- `retries`
- `retry_delay`
- `concurrency`
- `on_failure`
- `catchup`

This lets a project choose whether failures should alert, skip, stop a pipeline branch, or be ignored.

### Observability

Husky records runs, logs, alerts, outputs, and scheduler state in SQLite. Operators can inspect:

- recent history
- live logs
- audit metadata
- output variables
- SLA breaches
- healthcheck results
- dashboard status views

### Notification routing

Notifications can be sent on:

- success
- failure
- retry
- SLA breach

Supported providers include Slack, Discord, PagerDuty, SMTP email, and generic webhooks.

## Where Husky fits best

Husky is a strong fit for:

- build and release automation inside a repo
- backups and maintenance tasks
- ETL-style local pipelines
- scheduled reporting
- developer tooling and workstation automation
- small-team operations that want a single-binary scheduler

## Current runtime model

Today, Husky runs as one embedded daemon process started by the `husky` CLI.

Typical lifecycle:

1. `husky validate`
2. `husky start`
3. `husky status`
4. `husky run <job>` or let schedules fire naturally
5. inspect with `husky logs`, `husky history`, `husky audit`, or the dashboard

## Documentation map

- [Architecture](../overview/architecture.md)
- [Core concepts](../overview/core-concepts.md)
- [Installation](../getting-started/installation.md)
- [Quickstart](../getting-started/quickstart.md)
- [Workflow authoring](../writing-workflows/overview.md)
- [Operations](../operations/overview.md)
