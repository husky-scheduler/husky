---
title: Healthchecks and SLAs
sidebar_label: Healthchecks and SLAs
description: Verify post-run correctness and alert on slow jobs.
---

# Healthchecks and SLAs

## Healthchecks

A healthcheck is a second command that runs only after the main command exits with code `0`.

```yaml
healthcheck:
  command: "./scripts/healthcheck.sh"
  timeout: "30s"
  on_fail: mark_failed
```

## `on_fail` behavior

| Value | Meaning |
| --- | --- |
| `mark_failed` | Fail the run and apply retry policy |
| `warn_only` | Keep the run successful but mark `hc_status=warn` |

## Important runtime rule

If the main command fails, Husky does not run the healthcheck.

## Healthcheck logs

Healthcheck output is stored separately in the log stream and is hidden from normal log output unless requested.

```bash
husky logs <job>
husky logs <job> --include-healthcheck
```

## SLA budgets

An SLA is a soft duration budget for observability.

```yaml
sla: "5s"
timeout: "30s"
```

If a job exceeds its SLA while still running:

- Husky marks the run as SLA-breached
- `on_sla_breach` notification can fire
- the job keeps running

## Constraint

When both are set:

- `sla` must be less than `timeout`

## Where SLA state appears

- `husky history`
- `/api/runs/<id>`
- dashboard run state and run history

## Example

```yaml
jobs:
  slow_job_with_sla:
    description: "Long-running job with early warning"
    frequency: manual
    command: "./scripts/slow.sh"
    timeout: "30s"
    sla: "5s"
    notify:
      on_sla_breach:
        channel: webhook:http://127.0.0.1:9999/sla
        message: "{{ job.name }} is still running after {{ run.elapsed }}"
```
