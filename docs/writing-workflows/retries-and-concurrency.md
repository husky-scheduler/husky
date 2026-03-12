---
title: Retries and concurrency
sidebar_label: Retries and concurrency
description: Failure handling, retry delay modes, and overlapping-run policies.
---

# Retries and concurrency

## Retries

Set `retries` to the number of retry attempts after the initial run.

```yaml
retries: 2
```

That produces up to three total attempts.

## Retry delay modes

### Exponential

```yaml
retry_delay: exponential
```

Exponential backoff starts at roughly `30s` and doubles on each retry, with jitter.

### Fixed delay

```yaml
retry_delay: fixed:10s
```

Retries use the same delay each time.

## Failure policy

When retries are exhausted, `on_failure` decides the final behavior.

| Value | Meaning |
| --- | --- |
| `alert` | Send failure notifications |
| `skip` | Mark the pending path as skipped and continue other work |
| `stop` | Halt downstream pipeline progression |
| `ignore` | Record failure but take no special action |

## Concurrency

Concurrency controls overlapping runs of the same job.

| Value | Meaning |
| --- | --- |
| `allow` | Let multiple runs execute at once |
| `forbid` | Skip a new overlapping run |
| `replace` | Cancel the current run and start a fresh one |

## Example

```yaml
jobs:
  nightly_sync:
    description: "Sync remote data"
    frequency: every:5m
    command: "./scripts/sync.sh"
    timeout: "10m"
    retries: 3
    retry_delay: exponential
    concurrency: replace
    on_failure: alert
```

## Operator commands

```bash
husky retry <job>
husky cancel <job>
husky skip <job>
```

## What to expect in history

Run history records:

- attempt count
- trigger
- duration
- status
- reason
- VS SLA column when an SLA is configured

## Good patterns

- use `forbid` for jobs that must not overlap
- use `replace` for polling jobs where the newest run matters most
- use `stop` for critical DAG branches
- use `ignore` only for clearly non-critical work
