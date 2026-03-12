---
title: Writing workflows
sidebar_label: Introduction
description: How to think about Husky jobs, schedules, dependencies, and execution behavior.
---

# Writing workflows

Husky workflows live in `husky.yaml`. Each job should answer four questions clearly:

1. what does it do?
2. when does it run?
3. what does it depend on?
4. what happens if it fails?

## Minimal job

```yaml
jobs:
  hourly_ping:
    description: "Check endpoint reachability"
    frequency: hourly
    command: "./scripts/ping.sh"
```

## Typical production-ready job

```yaml
jobs:
  weekday_backup:
    description: "Back up the primary database"
    frequency: on:[monday,tuesday,wednesday,thursday,friday]
    time: "0100"
    command: "./scripts/backup.sh"
    timeout: "30m"
    retries: 2
    retry_delay: exponential
    on_failure: alert
    tags: [critical, maintenance]
    env:
      DB_HOST: "${env:BACKUP_DB_HOST}"
```

## Workflow authoring checklist

When defining a job, consider:

- use a specific, human-readable `description`
- choose a readable `frequency`
- add `time` when wall-clock scheduling is used
- define `timeout`
- define `retries` and `retry_delay`
- choose `on_failure`
- tag jobs for bulk operations
- add `notify` when the job matters operationally
- use `healthcheck` when exit code alone is not enough
- use `output` when downstream jobs need produced values

## Defaults

The `defaults:` block keeps configuration concise and consistent.

Common defaults include:

- `timeout`
- `retries`
- `retry_delay`
- `on_failure`
- `default_run_time`
- `timezone`

## Related guides

- [YAML reference](../writing-workflows/yaml-reference.md)
- [Scheduling](../writing-workflows/scheduling.md)
- [Dependencies](../writing-workflows/dependencies.md)
- [Retries and concurrency](../writing-workflows/retries-and-concurrency.md)
- [Outputs](../writing-workflows/output-passing.md)
- [Healthchecks and SLAs](../writing-workflows/healthchecks-and-slas.md)
- [Notifications](../writing-workflows/notifications.md)
- [Tags, audit, and timezones](../writing-workflows/tags-audit-and-timezones.md)
