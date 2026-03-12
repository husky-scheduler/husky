---
title: Notifications guide
sidebar_label: Notifications
description: Configure integrations, lifecycle hooks, templates, and delivery behavior for Husky notifications.
sidebar_position: 5
---

# Husky notifications guide

Husky can notify external systems when runs fail, succeed, retry, or breach SLA expectations.

Notifications are configured in two layers:

- `integrations:` — provider credentials and reusable integration definitions
- per-job `notify:` — lifecycle hooks and event behavior

## Supported providers

Husky currently supports:

- Slack
- Discord
- generic webhook
- PagerDuty
- SMTP email

## Integration definitions

Example:

```yaml
integrations:
  slack:
    webhook_url: "${env:SLACK_WEBHOOK_URL}"

  pagerduty:
    routing_key: "${env:PAGERDUTY_ROUTING_KEY}"

  smtp:
    host: smtp.example.com
    port: 587
    username: "${env:SMTP_USERNAME}"
    password: "${env:SMTP_PASSWORD}"
    from: husky@example.com
```

You can also define multiple integrations of the same provider with explicit names:

```yaml
integrations:
  slack_ops:
    provider: slack
    webhook_url: "${env:SLACK_OPS_WEBHOOK}"

  slack_data:
    provider: slack
    webhook_url: "${env:SLACK_DATA_WEBHOOK}"
```

## Per-job notify hooks

Supported lifecycle hooks:

- `on_failure`
- `on_success`
- `on_sla_breach`
- `on_retry`

### Shorthand form

```yaml
notify:
  on_failure: slack:#ops
```

### Object form

```yaml
notify:
  on_success:
    channel: webhook:https://example.test/hook
    message: "job={{ job.name }} status={{ run.status }}"
    attach_logs: last_30_lines
    only_after_failure: true
```

## Channel syntax

Channels use `<provider>:<target>` syntax.

Examples:

- `slack:#ops`
- `discord:deployments`
- `pagerduty:p1`
- `webhook:https://hooks.example.test/deploy`
- `smtp:team@example.com`

How they resolve:

- Husky uses the provider prefix to select the delivery backend
- the provider-specific integration config is taken from the named or inferred integration entry
- the target portion is interpreted according to the provider

## Template variables

Notification message templates support `job` and `run` values.

Available `job` fields:

- `job.name`
- `job.description`
- `job.frequency`
- `job.tags`

Available `run` fields:

- `run.id`
- `run.status`
- `run.attempt`
- `run.trigger`
- `run.reason`
- `run.sla_breached`

Example:

```yaml
message: "job={{ job.name }} status={{ run.status }} reason={{ run.reason }}"
```

Husky accepts both `{{ job.name }}` and Go-template-style `{{ .job.name }}` forms.

## Log attachments

`attach_logs` controls how much log output to include.

Supported values:

- `none`
- `all`
- `last_<N>_lines` such as `last_30_lines`

Examples:

```yaml
attach_logs: none
attach_logs: all
attach_logs: last_50_lines
```

Recommendations:

- use `last_<N>_lines` for concise operational alerts
- use `all` only when logs are known to be small and non-sensitive

## `only_after_failure`

This flag only matters on `on_success` notifications.

When enabled, Husky suppresses the success notification unless the previous completed run of that job failed.

This is useful for “recovered” messages without generating noise for every normal success.

## Event-specific behavior

### `on_failure`

Fires after retries are exhausted and the job enters terminal failure handling.

### `on_success`

Fires on successful completion.

### `on_sla_breach`

Fires when a running job exceeds its `sla` duration.

Fallback behavior:

- if `on_sla_breach` is not defined, Husky falls back to `on_failure`

SLA notifications are informational only; they do not kill or retry the run.

### `on_retry`

Fires at the start of each retry attempt.

## Delivery behavior and alert persistence

Husky records alert delivery state in the `alerts` table.

Tracked fields include:

- job name
- run ID
- event
- channel
- delivery status
- attempts
- last attempt timestamp
- payload
- last error

This gives operators a persistent audit trail of notification activity.

## Provider notes

### Slack / Discord / generic webhook

These are webhook-style JSON deliveries.

### PagerDuty

PagerDuty uses the Events API v2 routing key. The target can encode severity-like intent such as `p1`.

### SMTP

SMTP sends mail using the configured host and `from` address. The target portion of the channel becomes the recipient email address.

## Good patterns

### High-signal failure alert

```yaml
notify:
  on_failure:
    channel: slack:#ops
    message: "{{ job.name }} failed on attempt {{ run.attempt }}"
    attach_logs: last_40_lines
```

### Recovery-only success notice

```yaml
notify:
  on_success:
    channel: slack:#ops
    message: "{{ job.name }} recovered"
    only_after_failure: true
```

### SLA alerting without killing the job

```yaml
sla: "15m"
notify:
  on_sla_breach:
    channel: pagerduty:p2
    message: "{{ job.name }} is still running past SLA"
```

## Security reminders

- keep credentials in environment variables, not committed YAML
- be careful with attached logs if output may contain secrets
- use auth and TLS if operators trigger test deliveries through exposed APIs

## Troubleshooting notifications

If notifications do not arrive:

1. verify the integration exists and validates
2. confirm `${env:VAR}` values are present
3. review the `alerts` table or dashboard alerts view
4. run `husky integrations test <name>`
5. inspect daemon logs for delivery errors

## Related commands

```bash
husky integrations list
husky integrations test <name>
husky audit --job my_job
```

## Related docs

- [configuration.md](configuration.md)
- [security.md](security.md)
- [dashboard.md](dashboard.md)
