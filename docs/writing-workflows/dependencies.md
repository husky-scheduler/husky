---
title: Dependencies and DAGs
sidebar_label: Dependencies and DAGs
description: How Husky builds dependency graphs and dispatches downstream jobs.
---

# Dependencies and DAGs

## Two ways to declare edges

Husky creates DAG edges from:

- `depends_on`
- `after:<job>` frequency rules

## `depends_on`

Use `depends_on` when a job has one or more upstream prerequisites.

```yaml
jobs:
  transform:
    description: "Transform raw events"
    frequency: manual
    depends_on: [ingest]
    command: "./scripts/transform.sh"
```

## `after:<job>`

Use `after:<job>` when a job should fire immediately after a successful upstream run.

```yaml
jobs:
  generate_report:
    description: "Generate summary"
    frequency: after:transform
    command: "./scripts/report.sh"
```

## Example pipeline

```yaml
jobs:
  ingest_events:
    description: "Download raw events"
    frequency: manual
    command: "./scripts/ingest.sh"

  transform_events:
    description: "Transform events"
    frequency: after:ingest_events
    command: "./scripts/transform.sh"

  generate_report:
    description: "Build report"
    frequency: after:transform_events
    command: "./scripts/report.sh"
```

## DAG validation

Husky rejects cycles before the daemon starts or reloads.

Example invalid chain:

- `job_a -> job_b -> job_c -> job_a`

## Runtime rules

A downstream job only runs when required upstream jobs have succeeded.

Important effects:

- failed upstream jobs block downstream dispatch
- `on_failure: stop` halts the affected pipeline branch
- output templates resolve at dispatch time using the current `cycle_id`

## Inspecting the graph

```bash
husky dag
husky dag --json
curl http://127.0.0.1:8420/api/dag
```

## Fan-out and fan-in

Husky supports:

- fan-out: one upstream triggers multiple downstream jobs
- fan-in: one job waits for multiple upstream jobs

The graph must remain acyclic.
