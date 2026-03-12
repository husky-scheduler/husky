---
title: Dashboard
sidebar_label: Dashboard
description: What the Husky web dashboard exposes today and how to use it.
---

# Dashboard

The dashboard is served by the daemon and uses the local API and WebSocket log stream.

## Access

By default the dashboard is available on the bound API address, commonly:

```text
http://127.0.0.1:8420
```

## Main views

### Jobs view

Current dashboard capabilities include:

- job list with status, last run, next run, tags, and timezone
- tag filter dropdown
- tag health summary strip
- per-job run and cancel actions
- expandable recent run history

### Run detail areas

Inside expanded job history you can inspect:

- live logs for running jobs
- historic logs for completed jobs
- captured outputs
- healthcheck logs via toggle

### Audit view

The audit screen supports filtering by:

- job
- status
- trigger
- tag
- date

### DAG view

The DAG tab renders dependency information from the API so operators can inspect pipeline shape and execution order.

## Live logs

When a run is active, the dashboard streams log lines through WebSocket. Existing lines are backfilled first, then new lines arrive in real time.

## Redaction

Environment variable keys can appear in config views, but sensitive values are redacted.

## Practical operator workflow

1. open the Jobs view
2. filter by tag if needed
3. trigger or cancel work
4. expand a job row
5. follow logs and outputs
6. switch to Audit for cross-job investigation
