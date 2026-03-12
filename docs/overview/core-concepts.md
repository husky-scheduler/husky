---
title: Core concepts
sidebar_label: Core concepts
description: The key nouns and behaviors to understand before writing Husky workflows.
---

# Core concepts

## `husky.yaml`

`husky.yaml` is the source of truth for workflows. It defines jobs, schedules, retries, notifications, outputs, tags, and pipeline dependencies.

## `huskyd.yaml`

`huskyd.yaml` is optional daemon configuration. It controls runtime concerns such as:

- API bind address
- TLS
- auth and RBAC
- logging
- storage paths
- executor limits
- dashboard options

## Job

A job is the basic execution unit. Each job has:

- a name
- a description
- a frequency
- a command

Optional fields add reliability, observability, and routing behavior.

## Run

A run is one execution attempt of a job. A job can have many runs over time.

A run records:

- trigger type
- reason
- attempt number
- timestamps
- exit code
- SLA breach flag
- healthcheck status

## Trigger

A run can be started by:

- `schedule`
- `manual`
- `dependency`

These values appear in CLI history, audit output, notifications, and API responses.

## DAG

The directed acyclic graph describes dependency ordering between jobs. Husky builds it from:

- `depends_on`
- `after:<job>` frequency expressions

A cycle is always a configuration error.

## Cycle ID

When a root trigger starts a dependency chain, Husky assigns a `cycle_id`. Downstream jobs in the same chain share that `cycle_id`, which keeps output variables scoped correctly.

## Output variable

A job can capture a value from its output and publish it under a variable name. Downstream jobs read it with template syntax:

```yaml
{{ outputs.ingest.file_path }}
```

## Healthcheck

A healthcheck is a second command that runs only after the main command exits successfully. It verifies outcome quality instead of mere process success.

## SLA

An SLA is a soft time budget. When a running job exceeds it, Husky raises an SLA breach signal but does not kill the run.

## Timeout

A timeout is a hard runtime limit. If the main command exceeds it, Husky terminates the process group.

## Concurrency policy

Concurrency controls overlapping runs of the same job:

- `allow`
- `forbid`
- `replace`

## Failure policy

When retries are exhausted, `on_failure` defines the final behavior:

- `alert`
- `skip`
- `stop`
- `ignore`

## Tag

Tags are labels used for grouping and bulk operations. They power filtered status views, tag-based runs, pause/resume flows, and API filtering.

## Catchup

`catchup: true` tells Husky to reconcile missed schedule ticks after a crash or downtime window. `catchup: false` skips the missed execution and moves on.

## Local-first state

Husky treats runtime state as local and recoverable. The committed artifact is the workflow definition. The daemon's state directory is regenerated as needed.
