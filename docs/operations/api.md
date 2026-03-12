---
title: API and WebSockets
sidebar_label: API and WebSockets
description: The local Husky API surface for jobs, runs, logs, audit, and daemon control.
---

# API and WebSockets

The daemon exposes a local HTTP API plus WebSocket log streaming.

## Base URL

Use the address written to `.husky/api.addr`, or the configured API address.

## Core endpoints

### Status and jobs

- `GET /api/status`
- `GET /api/jobs`
- `GET /api/jobs?tag=<tag>`
- `GET /api/jobs/:name`
- `POST /api/jobs/:name/run`
- `POST /api/jobs/:name/cancel`
- `POST /api/jobs/pause?tag=<tag>`
- `POST /api/jobs/resume?tag=<tag>`

### Runs and logs

- `GET /api/runs/:id`
- `GET /api/runs/:id/logs`
- `GET /api/runs/:id/outputs`
- `GET /ws/logs/:run_id`

### Observability

- `GET /api/audit`
- `GET /api/tags`
- `GET /api/dag`

### Daemon control

- `POST /api/daemon/stop`
- `POST /api/daemon/reload`
- `GET /api/daemon/info`

### Config and database views

- `GET /api/config`
- `GET /api/config/daemon`
- `POST /api/config/validate`
- `POST /api/config/save`
- `GET /api/db/job_runs`
- `GET /api/db/run_logs`
- `GET /api/db/run_outputs`
- `GET /api/db/alerts`
- `GET /api/db/state`

## Example calls

```bash
curl -s http://127.0.0.1:8420/api/jobs | python3 -m json.tool
curl -s -X POST http://127.0.0.1:8420/api/jobs/ingest_users/run | python3 -m json.tool
curl -s http://127.0.0.1:8420/api/audit?trigger=manual | python3 -m json.tool
```

## WebSocket log streaming

Connect to:

```text
ws://127.0.0.1:8420/ws/logs/<run_id>
```

Behavior:

- historical lines are backfilled first
- new lines stream live
- the stream closes when the run reaches a terminal state

## Auth and RBAC

When daemon auth is enabled, the API and WebSocket endpoints are protected by the configured auth middleware and RBAC rules.
