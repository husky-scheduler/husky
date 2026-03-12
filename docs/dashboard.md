---
title: Dashboard guide
sidebar_label: Dashboard
description: Operator guide for the embedded Husky dashboard, run views, logs, and API-backed workflows.
sidebar_position: 4
---

# Husky dashboard guide

Husky includes an embedded dashboard built from the `web/` app and served by the daemon. No separate frontend service is required in production.

## Accessing the dashboard

Start the daemon, then run:

```bash
husky dash
```

The CLI opens the HTTP address recorded in `<data>/api.addr`.

## What the dashboard provides today

### Top bar / daemon controls

The dashboard shows daemon connectivity, uptime, and version information.

Current daemon actions include:

- reload config
- stop daemon gracefully or forcefully
- inspect daemon info such as PID, config path, DB path, and counts

### Jobs tab

The Jobs tab is the main operator surface.

It provides:

- job list with description, frequency, timezone, tags, and schedule state
- current running / paused state
- tag filter dropdown
- tag health summary strip
- per-job run, cancel, pause, and resume actions
- expandable recent-run history per job
- run pills that open detailed run views

### Run detail view

The dashboard has a dedicated run detail page that shows:

- status, attempt, trigger, triggered-by, and reason
- start / finish / duration
- exit code
- SLA state
- healthcheck status
- full log viewer
- captured output variables
- links to previous and next runs for the same job

### Log viewing

The log viewer supports:

- historical log retrieval for completed runs
- live WebSocket streaming for running jobs
- optional healthcheck stream visibility
- stream-aware rendering for stdout, stderr, and healthcheck lines

### Audit tab

The Audit view exposes searchable run history across jobs.

Filters include:

- job
- status
- trigger
- tag
- date / since filter
- reason text

Rows can open a run detail workflow so operators can move from audit metadata to logs and outputs quickly.

### Outputs / Data views

The dashboard includes database-backed views for:

- `run_outputs`
- `job_runs`
- `run_logs`
- `job_state`
- `alerts`

These are especially useful for pipeline debugging and notification troubleshooting.

### DAG tab

The DAG view renders job dependencies and supports:

- graph navigation
- opening job-related run views
- cycle/correlation-oriented workflows when following a pipeline execution

### Health and integrations

The dashboard exposes:

- SLA and health-oriented summaries
- integration status and test actions
- config viewing and save/reload workflows

## API relationship

The dashboard is a thin client over the Husky HTTP API.

Important endpoint groups include:

- `/api/jobs`
- `/api/runs/:id`
- `/api/audit`
- `/api/tags`
- `/api/dag`
- `/api/db/*`
- `/api/config*`
- `/api/daemon/*`
- `/ws/logs/:run_id`

See [configuration.md](configuration.md) and [operations.md](operations.md) for surrounding runtime details.

## Dashboard deployment model

- frontend source lives in `web/`
- built assets are generated into `internal/api/dashboard/`
- Go embeds those assets into the `husky` binary
- the daemon serves the embedded bundle directly

That means:

- production does not need Node.js
- there is no separate dashboard package to deploy
- dashboard version always matches the daemon binary version

## Dashboard-focused troubleshooting

If `husky dash` does not work:

1. verify the daemon is running with `husky status`
2. inspect `<data>/api.addr`
3. check whether `huskyd.yaml` bound the API to a different address
4. if auth is enabled, confirm the browser or client has valid credentials
5. review daemon logs for API startup failures

If live logs do not stream:

- confirm the run is still active
- confirm WebSocket access is allowed under your auth and RBAC config
- check whether a reverse proxy is stripping WebSocket upgrades

If the dashboard loads but data is empty:

- confirm the daemon was started with the expected `husky.yaml`
- confirm `huskyd.yaml` base path or auth settings are consistent with how you access the dashboard
