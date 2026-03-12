---
title: Configuration reference
sidebar_label: Configuration
description: Complete reference for husky.yaml and huskyd.yaml, including scheduling, orchestration, runtime, and validation behavior.
sidebar_position: 2
---

# Husky configuration reference

This document is the authoritative reference for both configuration files used by Husky:

- `husky.yaml` — job definitions and product behavior
- `huskyd.yaml` — daemon runtime behavior

## 1. `husky.yaml`

`husky.yaml` is the source of truth for scheduled jobs, pipelines, retries, notifications, tags, healthchecks, outputs, and timezone-aware execution.

### Top-level structure

```yaml
version: "1"
defaults: {}
integrations: {}
jobs: {}
```

### `version`

- required
- currently only `"1"` is valid

### `defaults`

Values under `defaults` are applied to jobs that do not override them.

Supported fields:

| Field | Type | Notes |
|---|---|---|
| `timeout` | duration string | e.g. `30m`, `1h`, `90s` |
| `retries` | integer | inherited when a job does not set retries |
| `retry_delay` | string | `exponential` or `fixed:<duration>` |
| `notify_on_failure` | boolean | legacy compatibility field |
| `on_failure` | string | `alert`, `skip`, `stop`, `ignore` |
| `default_run_time` | `HHMM` string | fallback time for `weekly`, `weekdays`, `weekends`, and `on:[...]` jobs that omit `time`; built-in default is `0300` |
| `timezone` | IANA timezone | fallback for jobs without their own timezone |

### `integrations`

Defines named notification integrations. Keys may be provider names or arbitrary names when `provider` is set explicitly.

Supported providers today:

- `slack`
- `discord`
- `webhook`
- `pagerduty`
- `smtp`

Provider fields:

| Field | Used by | Notes |
|---|---|---|
| `provider` | all | optional when inferred from the map key |
| `webhook_url` | slack, discord, webhook | required for those providers |
| `routing_key` | pagerduty | required |
| `host` | smtp | required |
| `port` | smtp | defaults to 587 when unset |
| `username` | smtp | optional |
| `password` | smtp | optional |
| `from` | smtp | required |

Environment interpolation is supported in integration fields via `${env:VAR}`.

### `jobs`

Each key in `jobs:` becomes a job name.

```yaml
jobs:
  my_job:
    description: "What the job does"
    frequency: daily
    time: "0200"
    command: "./scripts/run.sh"
```

### Job fields

#### Required fields

| Field | Type | Description |
|---|---|---|
| `description` | string | human-readable purpose of the job |
| `frequency` | string | scheduling mode |
| `command` | string | shell command to execute |

#### Scheduling fields

| Field | Type | Description |
|---|---|---|
| `frequency` | string | `hourly`, `daily`, `weekly`, `monthly`, `weekdays`, `weekends`, `manual`, `after:<job>`, `every:<interval>`, or `on:[day[,day...]]` |
| `time` | `HHMM` string | required for `daily` and `monthly`; optional for `weekly`, `weekdays`, `weekends`, and `on:[...]` |
| `timezone` | IANA timezone string | per-job timezone; falls back to `defaults.timezone`, then system timezone |
| `catchup` | boolean | run missed scheduled jobs after daemon restart |

Notes:

- `time` is ignored for `manual`, `hourly`, `every:<interval>`, and `after:<job>` frequencies
- `every:<interval>` accepts Go duration syntax such as `every:15s`, `every:15m`, or `every:2h`; intervals must be positive and less than 24 hours
- `on:[...]` accepts full weekday names such as `on:[monday,friday]`
- `weekly` means every Monday and is an alias for `on:[monday]`
- `weekdays` is an alias for `on:[monday,tuesday,wednesday,thursday,friday]`
- `weekends` is an alias for `on:[saturday,sunday]`
- when `weekly`, `weekdays`, `weekends`, or `on:[...]` omits `time`, Husky uses `defaults.default_run_time` or the built-in default of `0300`
- `monthly` means the 1st day of each month at the configured `time`
- `frequency` is still a closed set in v1: Husky does not accept cron expressions or parameterized calendar rules such as `last_day_of_month`
- timezone validation uses embedded tzdata, so Husky does not depend on system tzdata being present
- DST gaps and overlaps are handled explicitly by the scheduler

#### DAG and orchestration fields

| Field | Type | Description |
|---|---|---|
| `depends_on` | list of job names | explicit upstream dependencies |
| `frequency: after:<job>` | string | implicit dependency edge plus dependency-triggered execution |
| `concurrency` | string | `allow`, `forbid`, or `replace` |
| `working_dir` | string | working directory for subprocess execution |

Dependency rules:

- the graph must be acyclic
- cycles are rejected at startup and reload time
- downstream jobs only dispatch after upstream success

#### Reliability fields

| Field | Type | Description |
|---|---|---|
| `timeout` | duration string | hard runtime limit |
| `retries` | integer | retry count |
| `retry_delay` | string | `exponential` or `fixed:<duration>` |
| `on_failure` | string | `alert`, `skip`, `stop`, `ignore` |
| `sla` | duration string | informational runtime budget; must be less than `timeout` when both are set |

Behavior notes:

- `timeout` kills the process group after graceful termination attempts
- `sla` does not kill a job; it marks the run and fires notifications while the job continues
- `on_failure: stop` halts the pipeline by preventing downstream continuation after terminal failure

#### Environment fields

| Field | Type | Description |
|---|---|---|
| `env` | map of string to string | per-job environment values |

Environment values may use `${env:HOST_VAR}` interpolation.

The executor environment precedence is:

1. host environment
2. daemon `executor.global_env` from `huskyd.yaml`
3. per-job `env`

#### Tags

| Field | Type | Description |
|---|---|---|
| `tags` | list of strings | labels for filtering and bulk operations |

Tag constraints:

- lowercase alphanumeric plus hyphen
- max 10 tags per job
- max 32 characters per tag

#### Notifications

`notify` supports four lifecycle hooks:

- `on_failure`
- `on_success`
- `on_sla_breach`
- `on_retry`

Each hook accepts either shorthand or object form.

Shorthand:

```yaml
notify:
  on_failure: slack:#ops
```

Object form:

```yaml
notify:
  on_success:
    channel: webhook:https://example.test/hook
    message: "job={{ job.name }} status={{ run.status }}"
    attach_logs: last_30_lines
    only_after_failure: true
```

`NotifyEvent` fields:

| Field | Type | Meaning |
|---|---|---|
| `channel` | string | destination in `<provider>:<target>` form |
| `message` | string | templated message |
| `attach_logs` | string | `none`, `all`, or `last_<N>_lines` |
| `only_after_failure` | boolean | only meaningful on `on_success` |

See [notifications.md](notifications.md) for behavior details.

#### Healthchecks

```yaml
healthcheck:
  command: "curl -fsS http://127.0.0.1:8080/ready"
  timeout: "30s"
  on_fail: mark_failed
```

Fields:

| Field | Type | Meaning |
|---|---|---|
| `command` | string | command to execute after a successful main command |
| `timeout` | duration string | healthcheck timeout; defaults to `30s` |
| `on_fail` | string | `mark_failed` or `warn_only` |

Behavior:

- healthchecks only run if the main job exits successfully
- `mark_failed` converts the run into a failure and can trigger retries
- `warn_only` keeps the run successful but sets `hc_status=warn`

#### Output capture

```yaml
output:
  file_path: last_line
  record_count: json_field:total
  version: regex:v([0-9.]+)
  exit_status: exit_code
```

Supported capture modes:

| Mode | Meaning |
|---|---|
| `last_line` | final non-empty stdout line |
| `first_line` | first stdout line |
| `json_field:<key>` | parse stdout as JSON and extract key |
| `regex:<pattern>` | capture regex match group from stdout |
| `exit_code` | capture numeric exit code |

See [output-passing.md](output-passing.md) for full details.

### Example `husky.yaml`

```yaml
version: "1"
defaults:
  timeout: "30m"
  retries: 2
  retry_delay: exponential
  timezone: "America/New_York"

integrations:
  slack:
    webhook_url: "${env:SLACK_WEBHOOK_URL}"
  pagerduty:
    routing_key: "${env:PAGERDUTY_ROUTING_KEY}"

jobs:
  ingest:
    description: "Download source data"
    frequency: on:[monday,tuesday,wednesday,thursday,friday]
    time: "0200"
    command: "./scripts/ingest.sh"
    output:
      file_path: last_line
    notify:
      on_failure: slack:#ops

  transform:
    description: "Transform the ingested file"
    frequency: after:ingest
    command: "python transform.py --input {{ outputs.ingest.file_path }}"
    timeout: "20m"
    sla: "10m"
    on_failure: stop
    healthcheck:
      command: "python verify.py"
      timeout: "30s"
      on_fail: mark_failed
    tags: ["etl", "release"]
```

## 2. `huskyd.yaml`

`huskyd.yaml` configures the daemon process itself.

### Top-level sections

| Section | Purpose |
|---|---|
| `api` | bind address, base path, TLS, CORS, timeouts |
| `auth` | auth type and RBAC |
| `log` | structured logging and audit log |
| `storage` | SQLite path and retention |
| `scheduler` | concurrency limits, catchup window, shutdown timeout, jitter |
| `executor` | pool size, shell, working directory, resource limits, global env |
| `metrics` | metrics endpoint surface |
| `tracing` | tracing surface |
| `secrets` | secrets backend surface |
| `alerts` | daemon-level notifications |
| `dashboard` | dashboard runtime customisation |
| `http_client` | outbound HTTP client behavior |
| `process` | PID file and process-level settings |

### `api`

| Field | Type | Meaning |
|---|---|---|
| `addr` | string | bind address, e.g. `127.0.0.1:8420` |
| `base_path` | string | URL prefix like `/husky` |
| `tls.enabled` | boolean | enable HTTPS |
| `tls.cert` / `tls.key` | string | PEM files required when TLS enabled |
| `tls.min_version` | string | `1.2` or `1.3` |
| `tls.client_ca` | string | optional mutual TLS CA bundle |
| `cors.allowed_origins` | list | CORS allowlist |
| `cors.allow_credentials` | boolean | allow credentialed CORS |
| `timeouts.*` | duration strings | HTTP server read/write/idle timeouts |

### `auth`

| Field | Type | Meaning |
|---|---|---|
| `type` | string | `none`, `bearer`, `basic`, `oidc` |
| `bearer.token_file` | string | token file, hot-reloaded on SIGHUP |
| `bearer.token` | string | inline token, supported but less preferred |
| `basic.users` | list | username / bcrypt hash entries |
| `oidc.*` | object | declared config surface; not implemented yet |
| `rbac` | list | explicit per-role route grants |

### `log`

| Field | Type | Meaning |
|---|---|---|
| `level` | string | `debug`, `info`, `warn`, `error` |
| `format` | string | `text` or `json` |
| `output` | string | `stdout`, `stderr`, `file` |
| `file.*` | object | file rotation config |
| `audit_log.*` | object | separate audit log file |

### `storage`

| Field | Type | Meaning |
|---|---|---|
| `engine` | string | currently only `sqlite` is supported |
| `sqlite.path` | string | DB path override |
| `sqlite.wal_autocheckpoint` | integer | SQLite pragma |
| `sqlite.busy_timeout` | duration string | SQLite busy timeout |
| `retention.max_age` | duration string | delete completed runs older than this |
| `retention.max_runs_per_job` | integer | cap completed history per job |

### `scheduler`

| Field | Type | Meaning |
|---|---|---|
| `max_concurrent_jobs` | integer | global semaphore ceiling |
| `catchup_window` | duration string | max age for catchup-triggered missed schedules |
| `shutdown_timeout` | duration string | graceful drain timeout |
| `schedule_jitter` | duration string | per-trigger jitter to reduce bursts |

### `executor`

| Field | Type | Meaning |
|---|---|---|
| `pool_size` | integer | bounded worker pool size |
| `shell` | string | shell path used for commands |
| `working_dir` | string | daemon-wide working-dir override |
| `resource_limits.*` | object | process resource settings |
| `global_env` | map | environment injected into every job |

### `alerts`

Daemon-level notification hooks:

- `on_daemon_start`
- `on_sla_breach`
- `on_forced_kill`

These are distinct from per-job `notify` settings.

### `dashboard`

| Field | Type | Meaning |
|---|---|---|
| `enabled` | boolean | disable dashboard while keeping API surface |
| `title` | string | dashboard title override |
| `accent_color` | string | visual theme value |
| `log_backfill_lines` | integer | historical lines sent before live stream |
| `poll_interval` | duration string | client polling cadence |

### `http_client`

Controls outbound HTTP used by notifications and other integrations.

| Field | Type |
|---|---|
| `timeout` | duration string |
| `max_retries` | integer |
| `retry_backoff` | duration string |
| `proxy` | string |
| `ca_bundle` | string |

### `process`

| Field | Type | Notes |
|---|---|---|
| `user` / `group` | string | future-facing / platform-dependent process identity settings |
| `pid_file` | string | PID file override |
| `ulimit_nofile` | integer | process file descriptor limit |
| `watchdog_interval` | duration string | systemd-style watchdog support surface |

## 3. Defaults and validation notes

### Important validation rules

- `version` must be `"1"`
- DAGs must be acyclic
- `time` must be zero-padded `HHMM`
- `timezone` must resolve as an IANA timezone
- `sla` must be less than `timeout` when both are set
- tags must follow the lowercase/hyphen format constraints
- `retry_delay` must be `exponential` or `fixed:<duration>`
- `concurrency` must be `allow`, `forbid`, or `replace`
- `on_failure` must be `alert`, `skip`, `stop`, or `ignore`
- `huskyd.yaml` auth type must be one of `none`, `bearer`, `basic`, `oidc`
- `huskyd.yaml` storage engine is currently limited to `sqlite`

### What is declared but not fully implemented

Husky accepts some future-facing daemon config sections today, but not every declared field is fully wired into the runtime yet. In particular:

- OIDC auth is declared but rejected at startup
- Postgres storage is declared but rejected at startup
- some metrics, tracing, secrets, and process-level fields are currently schema/default surface rather than deeply wired operational features

## 4. Validation commands

Validate config:

```bash
husky validate
husky validate --strict
```

Show effective config:

```bash
husky config show
```

Strict mode adds warnings for documentation/operability issues such as missing descriptions and missing notify configs.
