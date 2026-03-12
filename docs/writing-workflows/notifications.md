---
title: Notifications
sidebar_label: Notifications
description: Notification events, integrations, templates, and log attachments.
---

# Notifications

Husky can notify on four lifecycle events:

- `on_failure`
- `on_success`
- `on_retry`
- `on_sla_breach`

## Shorthand form

```yaml
notify:
  on_failure: webhook:http://127.0.0.1:9999/failure
```

## Full object form

```yaml
notify:
  on_failure:
    channel: webhook:http://127.0.0.1:9999/failure
    message: "{{ job.name }} failed on attempt {{ run.attempt }}"
    attach_logs: last_30_lines
  on_success:
    channel: webhook:http://127.0.0.1:9999/success
    message: "{{ job.name }} completed successfully"
    only_after_failure: true
```

## Channels

Supported channel prefixes are:

- `slack:`
- `pagerduty:`
- `discord:`
- `webhook:`
- `email:`

## Integrations block

```yaml
integrations:
  webhook:
    webhook_url: "${env:WEBHOOK_URL}"
  slack_ops:
    provider: slack
    webhook_url: "${env:SLACK_OPS_WEBHOOK}"
```

## Template variables

Useful variables include:

- `{{ job.name }}`
- `{{ job.description }}`
- `{{ job.retries }}`
- `{{ job.sla }}`
- `{{ run.id }}`
- `{{ run.attempt }}`
- `{{ run.duration }}`
- `{{ run.elapsed }}`
- `{{ run.exit_code }}`
- `{{ run.trigger }}`
- `{{ run.started_at }}`
- `{{ run.reason }}`

## Log attachments

`attach_logs` accepts:

- `none`
- `last_N_lines`, for example `last_30_lines`
- `all`

## Recovery-only success alerts

Use `only_after_failure: true` on `on_success` to reduce noise.

This sends a success notification only when the previous completed run failed.

## CLI support

```bash
husky integrations list
husky integrations test webhook
```

## Operational behavior

- alert delivery attempts are recorded in the `alerts` table
- failed deliveries are retried with backoff
- missing credentials surface as configuration or send errors
