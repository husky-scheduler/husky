---
title: YAML reference
sidebar_label: YAML reference
description: Reference for the `husky.yaml` workflow file.
---

# YAML reference

## Top-level structure

```yaml
version: "1"
defaults: {}
integrations: {}
jobs: {}
```

## `version`

Currently only `"1"` is valid.

## `defaults`

Fields applied to jobs that do not override them:

```yaml
defaults:
  timeout: "30m"
  retries: 2
  retry_delay: exponential
  on_failure: alert
  default_run_time: "0900"
  timezone: "UTC"
```

Supported fields:

- `timeout`
- `retries`
- `retry_delay`
- `on_failure`
- `default_run_time`
- `timezone`
- `notify_on_failure` exists for compatibility, but richer `notify` blocks are the current model

## `integrations`

Defines reusable notification providers.

```yaml
integrations:
  webhook:
    webhook_url: "${env:WEBHOOK_URL}"
  slack_ops:
    provider: slack
    webhook_url: "${env:SLACK_OPS_WEBHOOK}"
```

Known providers:

- `slack`
- `pagerduty`
- `discord`
- `smtp`
- `webhook`

## `jobs`

Each entry key is the job name.

### Required fields

- `description`
- `frequency`
- `command`

### Common optional fields

- `time`
- `working_dir`
- `depends_on`
- `timeout`
- `retries`
- `retry_delay`
- `concurrency`
- `on_failure`
- `env`
- `notify`
- `catchup`
- `sla`
- `tags`
- `timezone`
- `healthcheck`
- `output`

## Example

```yaml
version: "1"
defaults:
  timeout: "20m"
  retries: 1
  retry_delay: exponential
  on_failure: alert
  default_run_time: "0900"
  timezone: "UTC"

integrations:
  webhook:
    webhook_url: "${env:WEBHOOK_URL}"

jobs:
  ingest_events:
    description: "Download events"
    frequency: every:15s
    command: "./scripts/ingest.sh"
    output:
      file_path: last_line

  transform_events:
    description: "Transform the ingested file"
    frequency: after:ingest_events
    command: "./scripts/transform.sh"
    env:
      INPUT_FILE: "{{ outputs.ingest_events.file_path }}"
    on_failure: stop
    tags: [data-pipeline, transform]
    notify:
      on_failure:
        channel: webhook:http://127.0.0.1:9999/failure
        message: "{{ job.name }} failed on run {{ run.id }}"
```

## Validation rules to know

- `time` must be a 4-digit `HHMM` string
- `sla` must be less than `timeout` when both are set
- `retry_delay` must be `exponential` or `fixed:<duration>`
- `concurrency` must be `allow`, `forbid`, or `replace`
- `on_failure` must be `alert`, `skip`, `stop`, or `ignore`
- timezones must be valid IANA identifiers
- tags must be lowercase alphanumeric or hyphenated
- cycles in the dependency graph are rejected
- output capture modes must be valid
