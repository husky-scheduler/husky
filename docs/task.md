---
title: Task tracker
sidebar_label: Task tracker
description: Internal implementation tracker for Husky phases and feature delivery.
---

# Husky — Task Tracker

*Local-First Job Scheduler with Dependency Graphs*

---

## Phase 0 — Project Bootstrap
> Weeks 0–1 · Set up the repository, toolchain, and all dependencies before any feature work begins.

### Repository & Tooling
- [x] Initialise Git repository and add `.gitignore` (Go, binaries, `.env`)
- [x] Create top-level directory structure (`cmd/`, `internal/`, `web/`, `docs/`, `scripts/`, `tests/`)
- [x] Initialise Go module (`go mod init github.com/husky-scheduler/husky`)
- [x] Pin Go toolchain version in `go.mod` / `.tool-versions` (go 1.24.0)
- [x] Add `Makefile` with targets: `build`, `test`, `lint`, `clean`, `run`
- [x] Configure `golangci-lint` (`.golangci.yml`)
- [x] Add `pre-commit` hooks (lint + `go vet` on staged files)
- [x] Set up GitHub Actions CI skeleton (lint + test on push)

### Dependencies
- [x] Add YAML parser — `gopkg.in/yaml.v3`
- [x] Add SQLite driver — `modernc.org/sqlite` (pure-Go, CGO-free)
- [x] Add JSON Schema validator for `husky.yaml` validation — `santhosh-tekuri/jsonschema/v5`
- [x] Add structured logger — `log/slog` (stdlib, Go 1.21+)
- [x] Add CLI framework — `spf13/cobra`
- [x] Add test assertion library — `testify/assert` + `testify/require`
- [x] Run `go mod tidy` and commit `go.sum`

### Skeleton
- [x] Create embedded daemon runtime entry point (initially `cmd/huskyd/main.go`, later invoked via `husky daemon run`)
- [x] Create `cmd/husky/main.go` entry point (CLI)
- [x] Add placeholder `VERSION` constant and `--version` flag (`internal/version` package)
- [x] Create `internal/config/` package stub
- [x] Create `internal/store/` package stub
- [x] Verify `make build` produces the `husky` binary without errors

---

## Phase 1 — Core Engine
> Weeks 1–3 · YAML parser, schedule evaluator, SQLite schema, basic executor, and core CLI commands.

### Step 1: YAML Parser & Config
- [x] Define `Config` and `Job` Go structs matching the `husky.yaml` schema (§2.1, §2.4)
- [x] Implement YAML unmarshalling into `Config` struct
- [x] Implement JSON Schema validation of `husky.yaml` at load time
- [x] Validate `frequency` enum values (§2.2.1)
- [x] Validate `time` field — 4-char military format, valid hour/minute range (§2.2.2)
- [x] Validate `on_failure` enum (`alert | skip | stop | ignore`)
- [x] Validate `retry_delay` format (`exponential` or `fixed:<duration>`)
- [x] Validate `concurrency` enum (`allow | forbid | replace`)
- [x] Interpolate `${env:HOST_VAR}` references in `env` map at runtime (§7.3)
- [x] Apply `defaults` block to all jobs that do not override a field (§2.1)
- [x] Return structured parse errors with file location context
- [x] Add `timezone` field to `Job` struct — IANA timezone identifier string (Feature 7)
- [x] Add `timezone` field to `Defaults` struct as global fallback (Feature 7)
- [x] Validate timezone identifier at parse time using Go's embedded `time/tzdata` package; reject unknown identifiers (Feature 7)
- [x] Add `sla` field to `Job` struct — optional duration string (Feature 1)
- [x] Validate at parse time that `sla < timeout` when both are set; return structured error if not (Feature 1)
- [x] Add `tags` field to `Job` struct — list of strings (Feature 4)
- [x] Validate tags: lowercase alphanumeric with hyphens only, max 10 tags per job, max 32 characters per tag (Feature 4)
- [x] Expand `Notify` struct to full rich notification schema — `on_failure`, `on_success`, `on_sla_breach`, `on_retry` sub-objects each with `channel`, `message`, `attach_logs`, and `only_after_failure` fields (Feature 3)
- [x] Maintain backward compatibility: shorthand string form (`notify.on_failure: slack:#channel`) continues to parse correctly (Feature 3)
- [x] Add `healthcheck` block to `Job` struct: `command` string, `timeout` duration string, `on_fail` enum (`mark_failed` | `warn_only`) (Feature 6)
- [x] Add `output` block to `Job` struct: map of variable name → capture mode string (`last_line`, `first_line`, `json_field:<key>`, `regex:<pattern>`, `exit_code`) (Feature 2)
- [x] Add `Integration` struct with provider-specific credential fields (`webhook_url`, `routing_key`, `host`, `port`, `username`, `password`, `from`) (Feature 3)
- [x] Add `Integrations map[string]*Integration` top-level field to `Config` struct (Feature 3)
- [x] Infer provider from map key when key matches a known provider name (`slack`, `pagerduty`, `discord`, `smtp`, `webhook`); require explicit `provider` field for custom key names (Feature 3)
- [x] Validate each integration: required credential fields per provider (`webhook_url` for slack/discord/webhook, `routing_key` for pagerduty, `host`+`from` for smtp), SMTP port range 1–65535 (Feature 3)
- [x] Extend `${env:VAR}` interpolation to cover all integration credential fields (`webhook_url`, `routing_key`, `username`, `password`) (Feature 3)
- [x] Implement `LoadDotEnv(dir)` — source `.env` file from same directory as `husky.yaml` before env interpolation; non-destructive (process env vars take precedence) (Feature 3)
- [x] Update JSON Schema to allow `integrations:` top-level block with `integration` $def (Feature 3)

### Step 2: Schedule Evaluator
- [x] Implement `NextRunTime(job Job, now time.Time) time.Time` for all `frequency` values
  - [x] `hourly` → next `:00`
  - [x] `daily`, `weekly`, `monthly`, `weekdays`, `weekends` → next matching wall-clock tick using `time` field
  - [x] `manual` → never schedules automatically
  - [x] `after:<job>` → dependency-driven (no time calculation)
- [x] Log resolved UTC equivalent of every scheduled time at daemon startup
- [x] Implement 1-second ticker loop that evaluates all jobs each tick
- [x] Resolve `time` field relative to per-job `timezone`; fall back to `defaults.timezone`, then system timezone (Feature 7)
- [x] Use Go's embedded `time/tzdata` for timezone resolution — no system tzdata dependency (Feature 7)
- [x] Handle DST gap: when the scheduled wall-clock time does not exist, run at the next valid time after the gap and log a warning (Feature 7)
- [x] Handle DST overlap: when the scheduled time occurs twice, run once on the first occurrence and log a warning (Feature 7)
- [x] Include timezone abbreviation (e.g. `EST`, `JST`) alongside the UTC equivalent in daemon startup log (Feature 7)

### Step 3: SQLite State Store
- [x] Implement `store` package with WAL-mode SQLite connection
- [x] Create schema migrations for `job_runs`, `job_state`, `run_logs`, `alerts` tables (§4.1)
- [x] Add `reason TEXT` and `triggered_by TEXT DEFAULT "scheduler"` columns to `job_runs` (Feature 5)
- [x] Add `sla_breached INTEGER DEFAULT 0` column to `job_runs` (Feature 1)
- [x] Add `hc_status TEXT` column to `job_runs` — values: `null | pass | fail | warn` (Feature 6)
- [x] Create `run_outputs` table: `(id, run_id, job_name, var_name, value, cycle_id, created_at)` with index on `cycle_id` (Feature 2)
- [x] Implement serialised writer goroutine channel for all writes
- [x] Implement `RecordRunStart`, `RecordRunEnd`, `RecordLog`, `GetJobState` operations
- [x] Implement `UpdateJobState` (last_success, last_failure, next_run, lock_pid)

### Step 4: Executor
- [x] Implement bounded goroutine pool (default: 8 concurrent jobs) (§3.2.3)
- [x] Launch each job as a subprocess via `/bin/sh -c` (or direct exec for array form)
- [x] Set `Setpgid=true` on all subprocesses (§7.4)
- [x] Capture `stdout` and `stderr` line-by-line and write to `run_logs` in real time
- [x] Implement timeout: send `SIGTERM`, then `SIGKILL` after grace period
- [x] Kill entire process group on timeout/cancel

### Step 5: Core CLI (`husky`)
- [x] `husky start` — launch the embedded Husky daemon in background, write `daemon.pid`, detach
- [x] Hidden `husky daemon run` entry point for direct foreground daemon execution by service managers and development workflows
- [x] `husky stop` — graceful shutdown (drain running jobs)
- [x] `husky stop --force` — immediate `SIGKILL` to all running jobs
- [x] `husky status` — tabular display of all job states from `job_state`
- [x] `husky run <job>` — trigger job immediately, bypassing schedule
- [x] Unix socket IPC between `husky` (CLI) and the embedded daemon runtime

### Step 6: Testing
- [x] Unit tests for YAML parser (valid configs, missing required fields, bad enum values)
- [x] Unit tests for `time` field validation (all rows from Table §2.2.2)
- [x] Unit tests for schedule evaluator (`NextRunTime` for each frequency value)
- [x] Unit tests for SQLite store (CRUD, WAL concurrency)
- [x] Integration test: parse full example `husky.yaml` (§2.3) without errors

---

## Phase 2 — DAG + Reliability
> Weeks 4–6 · Dependency resolution, retry state machine, crash recovery, hot-reload, catchup.

### DAG Resolver
- [x] Implement topological sort using Kahn's algorithm on `depends_on` declarations (§3.2.2)
- [x] Detect cycles at startup and emit full cycle path in error message
- [x] Reject daemon start if any cycle is found
- [x] Resolve `after:<job>` frequency as an implicit `depends_on` edge
- [x] At runtime, check all upstream job states are `SUCCESS` before dispatching a dependent job
- [x] `husky dag` — print ASCII DAG of all jobs and their dependencies
- [x] `husky dag --json` — emit machine-readable DAG structure

### Retry FSM
- [x] Implement job run state machine: `PENDING → RUNNING → SUCCESS | FAILED → RETRYING` (§3.2.4)
- [x] Implement exponential backoff with ±25% jitter (doubles each attempt: 30s, 60s, 120s…)
- [x] Implement `fixed:<duration>` retry delay
- [x] On max retries exceeded, execute `on_failure` action:
  - [x] `alert` — fire configured notify channels
  - [x] `skip` — mark run `SKIPPED`, continue other jobs
  - [x] `stop` — halt the entire pipeline (cancel all downstream dependents)
  - [x] `ignore` — log failure silently, take no action
- [x] Implement `concurrency: forbid` — skip run if previous is still running (§2.4)
- [x] Implement `concurrency: replace` — kill running instance and start fresh
- [x] `husky retry <job>` — retry last failed run from scratch
- [x] `husky cancel <job>` — send `SIGTERM` to running job
- [x] `husky skip <job>` — mark pending run as `SKIPPED`

### Healthchecks
- [x] After main command exits with code 0, run `healthcheck.command` as a separate process (Feature 6)
- [x] Apply `healthcheck.timeout` (default: 30s); send `SIGTERM` + `SIGKILL` to healthcheck if exceeded (Feature 6)
- [x] `on_fail: mark_failed` (default): mark run `FAILED`, append healthcheck stderr to `run_logs`, trigger retry policy (Feature 6)
- [x] `on_fail: warn_only`: mark run `SUCCESS` with `hc_status = warn`, fire `on_sla_breach` notify event (Feature 6)
- [x] Skip healthcheck entirely when main command exits non-zero (Feature 6)
- [x] Capture healthcheck stdout/stderr into `run_logs` with `stream = "healthcheck"` stream tag (Feature 6)
- [x] Expose `husky logs <job> --include-healthcheck` flag to include healthcheck log lines (Feature 6)

### Output Passing
- [x] After job completion, evaluate the job's `output` block and write each captured value to `run_outputs` (Feature 2)
- [x] Implement all capture modes: `last_line`, `first_line`, `json_field:<key>`, `regex:<pattern>`, `exit_code` (Feature 2)
- [x] Generate a `cycle_id` UUID at the root of each trigger chain (schedule tick or manual run); propagate to all downstream dependent jobs (Feature 2)
- [x] At job dispatch time, render `{{ outputs.<job_name>.<var_name> }}` template expressions in `command`, `env` values (Feature 2)
- [x] Return a descriptive error at dispatch time if a referenced output variable has no recorded value for the current `cycle_id` (Feature 2)
- [x] Scope all output values to a single cycle; never bleed values across independent trigger chains (Feature 2)

### Crash Recovery
- [x] On startup, acquire `daemon.pid` lock (§6 step 2)
- [x] Detect stale lock (PID dead) vs. live daemon (exit with "daemon already running")
- [x] Reconcile orphaned runs: query `job_state WHERE lock_pid IS NOT NULL`, probe each PID with `kill -0` (§6 step 3)
- [x] For dead PIDs: mark run `FAILED`, increment attempt, schedule retry
- [x] Reconcile missed schedules based on `catchup` flag (§6 step 4)
  - [x] `catchup: true` → trigger missed run immediately
  - [x] `catchup: false` → log skip, advance to next scheduled tick

### Config Hot-Reload (SIGHUP)
- [x] Handle `SIGHUP` — parse full new config before applying
- [x] Atomic swap of running config under mutex
- [x] Ensure running jobs are never interrupted during reload
- [x] Re-run cycle detection on new config; reject reload (keep old config) if invalid
- [x] `husky reload` — send `SIGHUP` to daemon via Unix socket

### Testing
- [x] Unit tests for Kahn's algorithm (linear chain, fan-in, fan-out, cycle detection)
- [x] Unit tests for Retry FSM state transitions
- [x] Unit tests for exponential backoff timing + jitter bounds
- [x] Unit tests for each `on_failure` handler
- [x] Integration test: crash daemon mid-run, restart, verify orphan reconciliation
- [x] Integration test: introduce a cycle into config, verify daemon rejects it
- [x] Unit tests for healthcheck execution flow: main success + healthcheck pass, main success + healthcheck fail (`mark_failed`), main success + healthcheck fail (`warn_only`), main fail skips healthcheck (Feature 6)
- [x] Unit tests for healthcheck timeout enforcement (Feature 6)
- [x] Unit tests for each output capture mode: `last_line`, `first_line`, `json_field`, `regex`, `exit_code` (Feature 2)
- [x] Unit tests for `{{ outputs.<job>.<var> }}` template rendering in `command` and `env` (Feature 2)
- [x] Unit tests for `cycle_id` scoping — values from cycle A are not visible in cycle B (Feature 2)
- [x] Unit tests for timezone scheduling: correct wall-clock resolution for a sample of IANA zones, DST gap and overlap behaviour (Feature 7)
- [x] Unit tests for `sla < timeout` validation at parse time (Feature 1)

---

## Phase 3 — Observability
> Weeks 7–9 · REST API, WebSocket log streaming, embedded web dashboard, webhook alerting.

### REST API
- [x] Bind API server to `127.0.0.1:8420` by default (§7.1)
- [x] `GET /api/jobs` — list all jobs with current state
- [x] `GET /api/jobs?tag=<tag>` — filter job list by tag (Feature 4)
- [x] `GET /api/jobs/:name` — single job detail (config + last N runs)
- [x] `POST /api/jobs/:name/run` — trigger manual run; accept optional `reason` body field (Feature 5)
- [x] `POST /api/jobs/:name/cancel` — cancel running job
- [x] `GET /api/runs/:id` — run detail with exit code, duration, `sla_breached`, `hc_status` fields (Feature 1, 6)
- [x] `GET /api/runs/:id/logs` — paginated log lines for a run
- [x] `GET /api/runs/:id/outputs` — output variables captured for a run (Feature 2)
- [x] `GET /api/audit` — searchable run history; query params: `job`, `trigger`, `status`, `since`, `reason`, `tag` (Feature 5)
- [x] `GET /api/tags` — list all defined tags (Feature 4)
- [x] `GET /api/dag` — DAG structure as JSON
- [x] `GET /api/status` — daemon health + uptime

### WebSocket Log Streaming
- [x] `GET /ws/logs/:run_id` — stream live `run_logs` lines over WebSocket as they are written
- [x] Backfill existing log lines on connection open, then stream new lines
- [x] Close WebSocket cleanly when run reaches terminal state (`SUCCESS | FAILED | SKIPPED`)
- [x] `husky logs <job> --tail` — consume WebSocket stream in terminal

### CLI Observability Commands
- [x] `husky logs <job>` — print last run logs
- [x] `husky logs <job> --run=<id>` — logs for a specific run ID
- [x] `husky logs <job> --include-healthcheck` — include healthcheck stream in output (Feature 6)
- [x] `husky logs --tag <tag> --tail` — stream live logs for all jobs matching a tag (Feature 4)
- [x] `husky history <job>` — table of last N runs (status, duration, trigger, reason, vs-SLA columns) (Feature 1, 5)
- [x] `husky history <job> --last=<n>` — configurable run count
- [x] `husky validate` — lint `husky.yaml` (schema, cycles, field types, timezone identifiers, sla < timeout)
- [x] `husky validate --strict` — also warn on missing descriptions and notify configs
- [x] `husky config show` — print effective config with defaults applied
- [x] `husky export --format=json` — export full state snapshot
- [x] `husky status --tag <tag>` — filter job status table by tag (Feature 4)
- [x] `husky pause --tag <tag>` — pause all jobs matching a tag (Feature 4)
- [x] `husky resume --tag <tag>` — resume all paused jobs matching a tag (Feature 4)
- [x] `husky run --tag <tag>` — trigger all jobs matching a tag immediately (Feature 4)
- [x] `husky tags list` — print all defined tags with the count of jobs carrying each one (Feature 4)
- [x] `husky run <job> --reason "<text>"` — attach a free-text annotation (max 500 chars) to the manual run record (Feature 5)
- [x] `husky audit` — searchable run history across all jobs (Feature 5)
- [x] `husky audit --job <name>` — filter by job (Feature 5)
- [x] `husky audit --trigger manual|schedule|dependency` — filter by trigger type (Feature 5)
- [x] `husky audit --status failed|success|skipped` — filter by run status (Feature 5)
- [x] `husky audit --since "<date>"` — filter runs after a given date (Feature 5)
- [x] `husky audit --reason "<text>"` — full-text search on the reason field (Feature 5)
- [x] `husky audit --tag <tag>` — filter by job tag (Feature 5)
- [x] `husky audit --export csv` — export results as CSV (Feature 5)

### Web Dashboard
- [x] Scaffold Preact app in `web/` directory
- [x] DAG view — dependency edges and topological execution order, renders from `GET /api/dag` JSON
- [x] Job list view — all jobs, last status, last run time, next scheduled run; tag filter dropdown; run/cancel actions
- [x] Run history view — recent runs per job in expandable row detail; run pills colour-coded by status
- [x] Extend run history with `vs SLA` column showing ✓ under / ⚠ +Δ (Feature 1)
- [x] Add third run-state colour: yellow for "running but past SLA" in addition to green/red (Feature 1)
- [x] Tag filter — tag dropdown on jobs view filters to matching jobs; tag dropdown on audit view (Feature 4)
- [x] Tag filter sidebar with aggregate health counts per tag (Feature 4)
- [x] Audit log view — searchable, filterable table (job, status, trigger, tag, since) with click-to-expand log viewer (Feature 5)
- [x] Live log viewer — streams WebSocket log output; stderr tinted red; auto-scroll; historic fallback for completed runs (Feature 6)
- [x] Job detail view — show captured output variables per run under a collapsible "Outputs" section (Feature 2)
- [x] Healthcheck output — show healthcheck log lines in a collapsible section below main output in run detail (Feature 6)
- [x] Embed compiled web assets into binary via `go:embed` (§3.3)
- [x] Serve dashboard from `GET /` on API server
- [x] Add `make web` target — `npm ci && vite build` writes Tailwind + Preact bundle to `internal/api/dashboard/`; `make build` depends on `web`
- [x] Style dashboard with Tailwind CSS (purged bundle ≈ 15 KB gzip)
- [x] Add timezone column to job list view (Feature 7)
- [x] Redact `env` var values in job config views (§7.3)

### Rich Notifications
- [x] Implement notification dispatch for all four events: `on_success`, `on_failure`, `on_sla_breach`, `on_retry` (Feature 3)
- [x] Render `message` templates using `{{ job.* }}` and `{{ run.* }}` variables at dispatch time (Feature 3)
- [x] Attach last N lines of `run_logs` to notification when `attach_logs: last_N_lines` is set; send full log when `all` (Feature 3)
- [x] Implement `only_after_failure: true` on `on_success` — suppress notification unless previous completed run was `FAILED` (Feature 3)
- [x] Support `slack:#channel` and `slack:@user` — post formatted message via Slack Incoming Webhook URL (Feature 3)
- [x] Support `pagerduty:<severity>` — trigger PagerDuty Events API v2; severity values: `p1`, `p2`, `p3`, `p4` (Feature 3)
- [x] Support `discord:#channel` — post plain text message via Discord Incoming Webhook URL (Feature 3)
- [x] Support `webhook:<url>` — HTTP POST with JSON payload to custom URL (Feature 3)
- [x] Support `email:<address>` — send SMTP email; requires `smtp` config block in `husky.yaml` defaults (Feature 3)
- [x] Implement SLA breach timer: when a job stays `RUNNING` past its `sla` duration, fire `on_sla_breach` (falls back to `on_failure` if not set); set `sla_breached = 1` on the run record (Feature 1)
- [x] Write each dispatched alert to the `alerts` table (audit log) (§4.1)
- [x] Retry alert delivery up to 3 times with backoff on HTTP error
- [x] Backward compatibility: shorthand string form (`notify.on_failure: slack:#channel`) continues to dispatch correctly (Feature 3)
- [x] Resolve integration credentials at notification dispatch time: look up the integration name embedded in the channel string prefix (e.g. `slack_ops:#alerts` → `cfg.Integrations["slack_ops"]`) (Feature 3)
- [x] Fall back to the provider-named key when channel string uses bare provider prefix (e.g. `slack:#channel` → `cfg.Integrations["slack"]`) (Feature 3)
- [x] Support `husky integrations list` — display all configured integrations as a table: name, provider, and credential status (`set` / `missing`) (Feature 3)
- [x] Support `husky integrations test <name>` — send a live test message/event to the named integration and report success or failure (Feature 3)

### Testing
- [x] Unit tests for each REST endpoint (table-driven, httptest)
- [x] Integration test: trigger run via REST, stream logs via WebSocket, verify terminal state
- [x] Unit tests for notification template rendering — all `{{ job.* }}` and `{{ run.* }}` variables (Feature 3)
- [x] Unit tests for Slack, PagerDuty, Discord, and custom webhook payload construction (Feature 3)
- [x] Unit tests for integration credential resolution at dispatch time — named key, bare provider fallback, missing integration (Feature 3)
- [x] Integration tests for `husky integrations test slack` and `husky integrations test pagerduty` (Feature 3)
- [x] Unit tests for `only_after_failure` suppression logic (Feature 3)
- [x] Unit tests for SLA breach timer: fires `on_sla_breach` at correct elapsed time, sets `sla_breached` flag (Feature 1)
- [x] Unit tests for tag filtering on `GET /api/jobs` and `husky status` (Feature 4)
- [x] Unit tests for `husky audit` filter combinations (Feature 5)
- [x] Unit tests for `husky run --reason` stores reason in `job_runs.reason` (Feature 5)
- [x] End-to-end test: full pipeline run (§2.3 example), verify all alert rows written
- [x] End-to-end test: output-passing pipeline — ingest captures `file_path`, transform receives it via template (Feature 2)

---

## Phase 3.4 — Dashboard Enhancements
> Complete · All backend endpoints, Data / Alerts / Config, Run Detail page, Enhanced Jobs tab, DAG SVG, Health tab, Integrations panel, and UI quality-of-life items implemented.

### Backend: New API Endpoints

- [x] `GET /api/daemon/info` — return PID, config file path, `started_at` timestamp, SQLite DB path, active job count, and paused job count (§1.3)
- [x] `GET /api/db/job_runs` — paginated, sortable, filterable table of `job_runs`; query params: `job`, `status`, `trigger`, `since`, `until`, `sla_breached`, `page`, `page_size` (§2.1)
- [x] `GET /api/db/job_state` — return the full `job_state` table (§2.2)
- [x] `POST /api/db/job_state/:job/clear_lock` — set `lock_pid = NULL` for a job; return `409` if the job is currently `RUNNING` (§2.2)
- [x] `GET /api/db/run_logs` — full-text searchable log explorer; query params: `run_id`, `job`, `stream` (`stdout` | `stderr` | `healthcheck`), `q`, `limit`, `offset` (§2.3)
- [x] `GET /api/db/alerts` — paginated alerts history; query params: `job`, `status`, `limit`, `offset` (§2.5)
- [x] `POST /api/db/alerts/:id/retry` — re-dispatch a failed alert without re-running the job; return `409` if alert is already `pending` (§2.5)
- [x] `POST /api/jobs/:name/pause` — pause a single job by name (§5.1)
- [x] `POST /api/jobs/:name/resume` — resume a single paused job by name (§5.1)
- [x] `POST /api/jobs/:name/retry` — retry the last failed run for this job (§4.2)
- [x] `POST /api/jobs/:name/skip` — mark a `PENDING` run as `SKIPPED` (§4.2)
- [x] `GET /api/integrations` — list all configured integrations with name, provider, credential status, last test result and timestamp (§8.1)
- [x] `POST /api/integrations/:name/test` — fire a test delivery to the named integration; return HTTP status and response payload (§8.2)

### Dashboard: Daemon Info Panel (§1.3)

- [x] Extend the top bar with a clickable version badge that toggles an info dropdown panel displaying: PID, uptime, config file path, `started_at` full timestamp, SQLite DB path, total / active / paused job counts
- [x] Fetch daemon info from `GET /api/daemon/info` on mount and on each 30-second poll cycle

### Dashboard: Data Tab (§2)

- [x] Add a **Data** tab to the navigation alongside Jobs | Audit | Outputs | Alerts | DAG | Config
- [x] **job_runs sub-tab** (§2.1): paginated table with columns `id | job_name | status | attempt | trigger | triggered_by | reason | started_at | finished_at | exit_code | sla_breached | hc_status`; pagination controls (25 / 50 / 100 rows); filter bar (job, status, trigger, date range, `sla_breached` flag)
- [x] **job_state sub-tab** (§2.2): table with columns `job_name | last_success | last_failure | next_run | lock_pid`; highlight rows with a non-null `lock_pid` using a warning badge; "Clear lock" action button per such row → `POST /api/db/job_state/:job/clear_lock`
- [x] **run_logs sub-tab** (§2.3): searchable log explorer across all runs; filters: run ID, job name, stream type, full-text keyword; rows colour-coded by stream (stderr = red tint, healthcheck = amber tint)

### Dashboard: Alerts Tab Completion (§2.5)

- [x] Add `status` column with colour-coded badges: `delivered` = green, `failed` = red, `pending` = yellow
- [x] Add `attempts`, `last_attempt_at`, and `error` columns to the Alerts table
- [x] Add a “Retry” action button on rows where `status = failed` → `POST /api/db/alerts/:id/retry`; disable the button while the request is in flight
- [x] Add pagination / load-more to the Alerts table (currently loads a fixed number)

### Dashboard: Config Editor Improvements (§3)

- [x] **Inline error markers** (§3.2): when “Validate” returns errors that include line numbers, scroll to and highlight the offending line(s) in the textarea; show the error description below the highlighted line
- [x] **Config diff view** (§3.4): after a successful “Save & Reload”, show a unified diff between the previous config text and the newly saved config; highlight added/removed job blocks so the operator can confirm what changed

### Dashboard: Run Detail Page (§4)

- [x] Implement SPA routing using the hash (`#/runs/:id`); the root `App` component parses `window.location.hash` and renders a `RunDetailView` instead of the tab panel when a run route is matched
- [x] **RunDetailView** (§4.1): full-page view showing — job name (links back to Jobs tab filtered to that job); status badge, attempt, trigger, triggered_by, reason; started_at / finished_at / duration; exit code; SLA progress bar (green if within budget, amber → red if over); `hc_status` badge; `cycle_id` (hyperlinked to Data → run_outputs filtered by cycle); full log viewer with stream colouring and healthcheck toggle; output variables table; previous / next run links for the same job
- [x] **Run actions** (§4.2): Retry button (shown when status is `FAILED`) → `POST /api/jobs/:name/retry`; Skip button (shown when status is `PENDING`) → `POST /api/jobs/:name/skip`; Cancel button (shown when status is `RUNNING`) → `POST /api/jobs/:name/cancel`
- [x] **Deep links** (§4.3): update run pills in the Jobs tab to navigate to `#/runs/:id` instead of expanding inline; update Audit table rows to link to `#/runs/:id`; make `run_id` values in the Data tab clickable links to `#/runs/:id`

### Dashboard: Enhanced Jobs Tab (§5)

- [x] **Per-job pause/resume** (§5.1): add Pause / Resume button to each job row action group (next to Run / Cancel); show a `PAUSED` badge in the Status column for paused jobs; wire to `POST /api/jobs/:name/pause` and `POST /api/jobs/:name/resume`
- [x] **Full job config details** (§5.2): extend the expanded row config panel to include `frequency`, `on_success`, `on_retry`, `on_sla_breach` notification settings, env var keys (values redacted as `***`), output capture rules, and healthcheck command + timeout
- [x] **Run history depth control** (§5.3): add a "Show more" button below the run history pills that fetches the next page; support incremental loading (`?runs=40` then `?runs=80`)
- [x] **Bulk trigger by tag** (§5.4): add a "Run all" button to the tag health strip for each tag; show a confirmation dialog listing the jobs that will be triggered; fire one `POST /api/jobs/:name/run` per matching job on confirm

### Dashboard: DAG Enhancements (§6)

- [x] **Visual graph** (§6.1): replace the current text edge list with a rendered directed graph using a lightweight library (Dagre-D3, D3-dag, or ELK.js); nodes show job name, current status badge, and last run time; directed edges with arrowheads; node borders colour-coded (green = SUCCESS, red = FAILED, blue = RUNNING, grey = PENDING); support scroll and zoom for large graphs
- [x] **Node actions** (§6.2): clicking a node navigates to `#/runs/:id` for the most recent run of that job; right-click context menu with Trigger / Cancel / View logs options
- [x] **Cycle trace overlay** (§6.3): from the Run Detail page or Data tab, a "Show in DAG" button that highlights all nodes and edges that participated in a specific `cycle_id`, with each node annotated with the outputs it captured in that cycle

### Dashboard: Health & SLA Tab (§7)

- [x] Add a **Health** tab to the navigation
- [x] **SLA compliance table** (§7.1): for all jobs with an SLA configured, render `job | SLA budget | last duration | Δ | breach rate (30 d)`; Δ shown in red when positive (breach) and green when negative (headroom); default sort by breach rate descending to surface worst offenders
- [x] **Timeline swimlane** (§7.2): chronological chart with one row per job showing the last N runs as coloured bars (bar length = duration, bar colour = status, amber border if SLA breached); hovering a bar shows a full run detail tooltip

### Dashboard: Integrations Panel (§8)

- [x] Add an **Integrations** section as a sub-tab within Config or as a dedicated Settings tab
- [x] **Integration health list** (§8.1): display all configured integrations with name, provider, credential status (`configured` / `missing required fields` / `env var unset`), last test result and timestamp; fetched from `GET /api/integrations`
- [x] **Test delivery button** (§8.2): "Test" button per integration row → `POST /api/integrations/:name/test`; display the test payload sent and the HTTP response code / body returned

### Dashboard: UI Quality-of-Life (§9)

- [ ] **Dark mode** (§9.1): *(→ Backlog)*
- [ ] **Toast notifications** (§9.2): ensure every user-initiated action (trigger, cancel, pause/resume, save config, retry alert, clear lock) shows a short-lived success or error toast with descriptive text
- [x] **Keyboard shortcuts** (§9.3): `r` refreshes the current view, `/` focuses the active filter/search input, `Esc` closes expanded rows or modals; register via a `useEffect` on `document` in the root `App` component; show a shortcut hint in the UI
- [ ] **Column and filter persistence** (§9.4): *(→ Backlog)*
- [x] **Copy to clipboard** (§9.5): one-click copy button on run IDs, cycle IDs, and inside the log viewer (copies visible log text) via `navigator.clipboard.writeText`
- [x] **Auto-polling control** (§9.6): toggle button to pause/resume auto-refresh of the jobs table; show "last refreshed N seconds ago" label; when paused, no polling until the user manually refreshes or re-enables

### Testing

- [x] Unit tests: `GET /api/daemon/info` — returns correct PID, config path, start time, and job counts (§1.3)
- [x] Unit tests: `GET /api/db/job_runs` — pagination boundaries, sort by each column, all filter param combinations (§2.1)
- [x] Unit tests: `POST /api/db/job_state/:job/clear_lock` — clears lock when job is idle, returns 409 when job is RUNNING (§2.2)
- [x] Unit tests: `GET /api/db/run_logs` — full-text `q` search, stream filter, pagination (§2.3)
- [x] Unit tests: `POST /api/db/alerts/:id/retry` — re-queues a failed alert; returns 409 if alert is already pending; returns 404 for unknown ID (§2.5)
- [x] Unit tests: `POST /api/jobs/:name/pause` and `/resume` — correct state transitions, 404 on unknown job (§5.1)
- [x] Unit tests: `POST /api/jobs/:name/retry` and `/skip` — correct preconditions enforced (status must be FAILED / PENDING respectively) (§4.2)
- [x] Unit tests: `GET /api/integrations` — returns all configured integrations with correct `status` field (§8.1)
- [x] Unit tests: `POST /api/integrations/:name/test` — fires test delivery, returns response details, 404 on unknown integration (§8.2)
- [x] E2E: hash routing — navigate directly to `#/runs/:id`, verify `RunDetailView` renders with correct data and back-navigation returns to the Jobs tab (§4.3)
- [x] E2E: bulk trigger by tag — confirmation dialog lists correct jobs, all matching jobs reach `RUNNING` state after confirm (§5.4)
- [ ] E2E: dark mode toggle — `dark` class applied to `<html>`, preference survives page reload (§9.1) *(→ Backlog)*

---

## Phase 3.5 — Daemon Runtime Configuration (`huskyd.yaml`)
> Complete · All `huskyd.yaml` parsing, API binding, authentication (bearer, basic auth, RBAC), TLS, CORS, structured logging with rotation, storage retention vacuum, executor shell + global env, scheduler jitter, and full unit + integration test coverage implemented. OIDC, metrics, tracing, secrets backends, and advanced executor resource limits deferred to Phase 5 backlog.

### Dashboard

- [x] **huskyd.yaml subtab**: if the daemon exposes a daemon-config path via `GET /api/daemon/info`, add a second subtab in the Config view displaying its path and raw YAML content (read-only initially)

### Discovery & Parsing

- [x] Auto-assign API port with `127.0.0.1:0` (OS picks free port); write bound address to `<data>/api.addr` on startup; remove file on clean shutdown
- [x] `husky dash` — read `<data>/api.addr` and open dashboard URL in default system browser
- [x] Define `DaemonConfig` Go struct in a new `internal/daemoncfg` package mirroring the full `huskyd.yaml` shape
- [x] Load `huskyd.yaml` from the same directory as `husky.yaml` when the file exists; silently apply all defaults when it does not
- [x] Support `--daemon-config <path>` flag on `huskyd` to override the default discovery path
- [x] Validate `huskyd.yaml` with a JSON Schema at load time; emit structured errors with field path context
- [x] All fields optional — absence of any section falls back to the documented default; no breaking change for v1.0 users
- [x] `husky validate` extends to also validate `huskyd.yaml` when present

### API Server

- [x] `api.addr` — bind the HTTP server to the specified address when set; override the `127.0.0.1:0` default (enables fixed-port deployments and binding to `0.0.0.0` for remote access)
- [x] Emit a prominent startup `WARN` log when `api.addr` binds to `0.0.0.0` without TLS and auth both enabled
- [x] `api.base_path` — mount the dashboard and all API routes under a URL prefix (e.g. `/husky`); strip prefix transparently so internal handlers see clean paths
- [x] `api.tls.enabled` — call `tls.Listen` instead of `net.Listen` when true; refuse to start if cert/key files are missing or unreadable
- [x] `api.tls.cert` / `api.tls.key` — absolute paths to PEM-encoded certificate and private key
- [x] `api.tls.min_version` — enforce minimum TLS version; accept `"1.2"` (default) or `"1.3"`
- [x] `api.tls.client_ca` — when set, require mTLS; reject connections whose client certificate is not signed by the given CA
- [x] `api.cors.allowed_origins` — inject CORS headers for listed origins; wildcard `"*"` accepted but emits a startup warning
- [x] `api.cors.allow_credentials` — set `Access-Control-Allow-Credentials: true` when needed for browser-based OAuth flows
- [x] `api.timeouts.read_header`, `.read`, `.write`, `.idle` — pass through to `http.Server` fields; defaults match current hardcoded values

### Authentication

- [x] `auth.type: none` (default) — no authentication; all endpoints open
- [x] `auth.type: bearer` — require `Authorization: Bearer <token>` on all REST + WebSocket requests
  - [x] `auth.bearer.token_file` — read one token per line; all listed tokens are valid; hot-reload on SIGHUP
  - [x] `auth.bearer.token` — inline token string for simple setups (warn in logs that file-based is preferred)
- [x] `auth.type: basic` — HTTP Basic Auth
  - [x] `auth.basic.users` — list of `{username, password_hash}` entries; `password_hash` is a bcrypt hash; plaintext passwords rejected at load time
- [x] `auth.type: oidc` — startup stub: returns a clear error; full OIDC implementation deferred to Phase 5
- [x] `auth.rbac` — define per-role allowed HTTP method + path patterns; built-in roles `viewer`, `operator`, `admin` with sensible defaults; custom roles accepted
- [x] Auth middleware applied uniformly to all routes served by the API server
- [x] Auth disabled entirely when `auth.type: none`; no performance overhead in the default case
- [x] `husky` CLI reads `HUSKY_TOKEN` env var and injects `Authorization: Bearer` header for all HTTP-based commands (`pause`, `resume`, `dash`) when auth is enabled

### Logging

- [x] `log.level` — `debug | info | warn | error`; default `info`; hot-reloadable via SIGHUP
- [x] `log.format: json` — swap the `slog` handler from `slog.NewTextHandler` to `slog.NewJSONHandler`; produces structured log lines consumable by Datadog, CloudWatch, Loki, Splunk
- [x] `log.output: stdout | stderr | file` — default `stdout`; `file` requires `log.file.path`
- [x] `log.file.path` — write logs to this file; create parent directories if absent
- [x] `log.file.max_size_mb`, `.max_backups`, `.max_age_days`, `.compress` — log rotation via `lumberjack` (or equivalent); compressed rotated files when `compress: true`
- [x] `log.audit_log.enabled` — write a separate newline-delimited JSON audit log to `log.audit_log.path`; each line is one job state transition event with full context (job, run ID, status, trigger, duration, reason)
- [x] `log.audit_log.max_size_mb`, `.max_backups` — independent rotation settings for the audit log file

### Storage

- [x] `storage.sqlite.path` — override default `<data>/husky.db` location
- [x] `storage.sqlite.wal_autocheckpoint` — set SQLite `wal_autocheckpoint` pragma; default 1000 pages
- [x] `storage.sqlite.busy_timeout` — set SQLite busy-handler timeout; default `5s`
- [x] `storage.retention.max_age` — background vacuum goroutine deletes `job_runs` (and cascade: `run_logs`, `run_outputs`) older than the specified duration; runs every 24 h
- [x] `storage.retention.max_runs_per_job` — after pruning by age, also enforce a per-job cap; keep only the N most recent completed runs; PENDING and RUNNING runs are never pruned
- [x] Background vacuum goroutine: run once at startup (after a short delay to not contend with catchup) then on a 24 h ticker; log number of rows pruned at `INFO` level
- [x] `storage.engine: postgres` stub — parse the config field and emit a clear `not yet supported` error at startup; prevents a confusing failure mode when users copy a future config forward
- [x] `husky export --format=json` honours the configured storage path when reading state

### Scheduler

- [x] `scheduler.max_concurrent_jobs` — global ceiling on concurrently executing jobs across all job pools; default `32`; per-job `concurrency` setting still applies at the job level
- [x] `scheduler.catchup_window` — maximum look-back window when reconciling missed schedules on restart; overrides per-job `catchup: true` for runs missed longer ago than this; default `24h`
- [x] `scheduler.shutdown_timeout` — grace period given to in-flight jobs when a graceful stop is requested before SIGKILL is sent to remaining jobs; default `60s`
- [x] `scheduler.schedule_jitter` — random jitter (up to this duration) added to each scheduled trigger to avoid thundering-herd bursts when many jobs share the same tick; default `0s` (no jitter)

### Executor

- [x] `executor.pool_size` — size of the bounded goroutine worker pool; default `8`; surfaced in `huskyd.yaml` so production deployments can tune without recompile
- [x] `executor.shell` — shell used to run job commands; default `/bin/sh`; common override `/bin/bash`
- [x] `executor.global_env` — key-value pairs injected into every job's environment; lower priority than per-job `env` in `husky.yaml`; higher priority than host environment

### Testing

- [x] Unit tests: bearer token middleware — valid token accepted, invalid token → 401, missing header → 401, auth disabled → all requests pass
- [x] Unit tests: basic auth middleware — valid credentials, wrong password, unknown user
- [x] Unit tests: RBAC — viewer can GET, viewer cannot POST trigger, operator can trigger, admin can do everything
- [x] Unit tests: storage retention vacuum — rows older than `max_age` deleted, rows within window kept, RUNNING rows never deleted
- [x] Unit tests: `max_runs_per_job` cap — correct rows pruned, most recent N retained
- [x] Unit tests: `executor.global_env` — values present in subprocess env, overridden by per-job env
- [x] Integration test: bearer auth enabled — `husky status` (HTTP commands) fail without `HUSKY_TOKEN`, succeed with correct token
- [x] Integration test: `huskyd.yaml` absent — daemon starts with all defaults; no error or warning beyond "no huskyd.yaml found, using defaults"

---

## Phase 4 — Hardening & Distribution
> Weeks 10–12 · Cross-compilation, packaging, auth, TLS, integration test suite.

### Cross-Compilation
- [x] Build matrix in `Makefile`: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
- [x] Verify `modernc.org/sqlite` CGO-free build works on all five targets
- [x] Produce reproducible builds (embed `VERSION`, `COMMIT`, `BUILD_DATE` via `ldflags`)
- [x] Add `make dist` target that produces a `dist/` directory with all platform archives (`.tar.gz`) — each archive contains the single `husky` binary

### Packaging & Distribution
- [x] Write Homebrew formula `husky.rb` (binary download + checksum)
- [x] Build `.deb` package for Debian/Ubuntu (using `goreleaser` or hand-rolled `dpkg-deb`)
- [x] Build `.rpm` package for RHEL/Fedora
- [x] Create `install.sh` one-liner installer (detects platform, downloads correct binary)
- [x] Write `systemd` unit file example (`huskyd.service`) — `ExecStart` points to `husky daemon run`
- [x] Write `launchd` plist example for macOS autostart (invokes `husky daemon run`)
- [x] Publish GitHub Release via GoReleaser CI step with checksums and signatures

### Token Authentication
> Superseded by Phase 3.5 (`auth.type: bearer` + `auth.type: basic` + RBAC in `huskyd.yaml`).
- [x] Implement token auth middleware for all REST + WebSocket endpoints (§7.2)
- [x] Read token from `auth.token` in `husky.yaml` or `HUSKY_TOKEN` env var
- [ ] Store bcrypt hash of token in `job_state` database; never store plaintext
- [x] Require `Authorization: Bearer <token>` header when auth is enabled
- [x] Token auth disabled by default; emit startup log note when disabled
- [x] `husky` reads token from `HUSKY_TOKEN` env var for socket/HTTP calls

### TLS
> Superseded by Phase 3.5 (`api.tls.*` block in `huskyd.yaml`; `--daemon-config` flag provides override path).
- [x] Support `--tls-cert` and `--tls-key` flags on daemon start (§7.1)
- [x] Verify cert/key pair at startup; refuse to start on invalid files
- [x] Serve HTTPS when TLS flags are provided; HTTP otherwise
- [ ] Document self-signed cert generation steps in `README`

### External Binding
> Superseded by Phase 3.5 (`api.addr` in `huskyd.yaml`).
- [x] Support `--bind` flag to override default `127.0.0.1:8420` (§7.1)
- [x] Emit prominent startup warning when bound to `0.0.0.0` without TLS + auth enabled

### Integration Test Suite
- [x] Test: parse, validate, and execute the full §2.3 example pipeline end-to-end
- [x] Test: DAG execution order is correct (ingest → transform → report)
- [ ] Test: `on_failure: stop` halts downstream jobs
- [ ] Test: retry with exponential backoff reaches max retries and fires alert
- [x] Test: `concurrency: forbid` skips overlapping runs
- [ ] Test: daemon crash mid-run → restart → orphan reconciled → retry fires
- [x] Test: `catchup: true` triggers missed run after restart
- [x] Test: `catchup: false` skips missed run after restart
- [x] Test: `SIGHUP` hot-reload swaps config without interrupting running job
- [x] Test: cycle in config is rejected at startup and reload
- [x] Test: token auth rejects requests without valid bearer token
- [x] Test: `husky validate` catches all invalid field combinations
- [ ] Test: job with `sla` breaches threshold, `on_sla_breach` fires, run completes with `sla_breached = 1` and `SUCCESS` status (Feature 1)
- [x] Test: job with `healthcheck`, main succeeds, healthcheck fails → run marked `FAILED`, retries triggered (Feature 6)
- [x] Test: job with `healthcheck on_fail: warn_only`, healthcheck fails → run marked `SUCCESS` with `hc_status = warn` (Feature 6)
- [x] Test: output-passing pipeline, downstream job receives correct `{{ outputs.* }}` value (Feature 2)
- [x] Test: output values from cycle A are not visible in independently triggered cycle B (Feature 2)
- [x] Test: `husky run <job> --reason` persists reason; `husky audit` returns it; `{{ run.reason }}` renders in notification template (Feature 3, 5)
- [x] Test: per-job timezone — job scheduled at wall-clock time in `America/New_York` fires at correct UTC moment before and after a DST transition (Feature 7)
- [x] Test: `husky pause --tag <tag>` pauses all matching jobs; `husky resume --tag <tag>` resumes them (Feature 4)
- [ ] Benchmark: scheduler tick latency under 100 concurrent jobs < 10 ms

### Documentation
- [x] Write `README.md` — quickstart, install, `husky.yaml`, `huskyd.yaml` example, CLI reference
- [x] Write `docs/configuration.md` — full field reference including all v1.0 fields: `sla`, `tags`, `timezone`, `healthcheck`, `output`, expanded `notify` schema (Features 1–7)
- [x] Write `docs/security.md` — auth, TLS, process isolation guidance
- [x] Write `docs/crash-recovery.md` — startup sequence walkthrough
- [x] Write `docs/output-passing.md` — guide to capture modes, `cycle_id` scoping, and template syntax (Feature 2)
- [x] Write `docs/notifications.md` — all channel formats, template variables, `only_after_failure` behaviour (Feature 3)
- [ ] Add `CHANGELOG.md` for v1.0.0
- [ ] Use Docusaurus to create a documentation website

---

## Phase 5: Backlog 

### Deferred from Phase 3.5
> Items below were descoped from the alpha release. They are fully specified and ready to implement post-launch.

#### OIDC Authentication
- [ ] `auth.type: oidc` — full implementation: fetch and cache JWKS from `auth.oidc.jwks_uri`; verify JWT signatures; refresh on key rotation (background goroutine)
- [ ] `auth.oidc.issuer`, `.client_id`, `.audience`, `.jwks_uri` — standard OIDC discovery fields
- [ ] `auth.oidc.role_claim` — JWT claim name mapped to Husky roles (`viewer`, `operator`, `admin`)
- [ ] `auth.oidc.default_role` — role assigned when claim is absent

#### Executor Advanced Config
- [ ] `executor.working_dir: config_dir | <absolute_path>` — working directory for all child processes; `config_dir` (default) means the directory containing `husky.yaml`
- [ ] `executor.resource_limits.max_memory_mb` — Linux: set `RLIMIT_AS`; macOS: best-effort via `setrlimit`; 0 = unlimited
- [ ] `executor.resource_limits.max_open_files` — set `RLIMIT_NOFILE` on child process; default `1024`
- [ ] `executor.resource_limits.max_pids` — Linux: set `RLIMIT_NPROC`; limits forking within a job

#### Metrics
- [ ] `metrics.enabled` — expose a Prometheus-compatible scrape endpoint; default `false`
- [ ] `metrics.addr` — bind address for the `/metrics` HTTP server; default `127.0.0.1:9091` (separate from dashboard port)
- [ ] `metrics.path` — URL path for the scrape endpoint; default `/metrics`
- [ ] `metrics.auth` — when `true`, protect `/metrics` with the same bearer token as the main API
- [ ] Instrument: `husky_job_runs_total{job,status,trigger}`, `husky_job_duration_seconds{job}`, `husky_job_sla_breaches_total{job}`, `husky_job_retries_total{job}`, `husky_scheduler_tick_duration_seconds`, `husky_running_jobs`, `husky_daemon_uptime_seconds`
- [ ] Use `prometheus/client_golang` — add to `go.mod`
- [ ] Metrics server lifecycle tied to daemon context (shut down cleanly on SIGTERM)

#### Tracing
- [ ] `tracing.enabled` — emit OpenTelemetry spans; default `false`
- [ ] `tracing.exporter` — `otlp | jaeger | zipkin | stdout`; default `otlp`
- [ ] `tracing.endpoint`, `.service_name`, `.sample_rate`
- [ ] Instrument spans for: job dispatch, executor subprocess wait, retry backoff sleep, IPC request handling, notification delivery
- [ ] Use `go.opentelemetry.io/otel` — add to `go.mod`
- [ ] Tracer provider shut down gracefully on daemon context cancellation

#### Secrets Backends
- [ ] `secrets.provider: vault` — resolve `${secret:NAME}` placeholders via HashiCorp Vault KV v2 (`addr`, `token_file`, `mount`, `path_prefix`)
- [ ] `secrets.provider: aws_ssm` — AWS Systems Manager Parameter Store (ambient IAM)
- [ ] `secrets.provider: aws_secrets_manager` — AWS Secrets Manager (ambient IAM)
- [ ] `secrets.provider: gcp_secret_manager` — GCP Secret Manager (Application Default Credentials)
- [ ] Secret resolution at startup + SIGHUP; never written to disk or logs
- [ ] Add `${secret:NAME}` interpolation syntax alongside `${env:VAR}` in `internal/config/env.go`

#### Daemon-Level Alerts
- [ ] `alerts.on_daemon_start` — fire notification(s) when `huskyd` starts successfully
- [ ] `alerts.on_sla_breach` — daemon-level fallback for SLA breach notifications when the job does not define `notify.on_sla_breach`
- [ ] `alerts.on_forced_kill` — fire notification when a job is killed by `shutdown_timeout` SIGKILL

#### Dashboard Customisation
- [ ] `dashboard.enabled: false` — disable dashboard while keeping REST + WebSocket API; respond to `/` with `404`
- [ ] `dashboard.title` — custom title injected into `<title>` and header; served via `GET /api/config`
- [ ] `dashboard.accent_color` — CSS custom property for primary accent colour
- [ ] `dashboard.log_backfill_lines` — historical lines sent on WebSocket connect; default `200`
- [ ] `dashboard.poll_interval` — frontend poll interval; served via `GET /api/config`; default `5s`
- [ ] `GET /api/config` — new endpoint returning dashboard runtime config (no auth required)

#### HTTP Client (Outbound)
- [ ] `http_client.timeout` — global timeout for all outbound HTTP calls; default `15s`
- [ ] `http_client.max_retries` / `.retry_backoff` — notification delivery retries
- [ ] `http_client.proxy` — optional HTTP proxy URL for all outbound calls
- [ ] `http_client.ca_bundle` — PEM CA bundle appended to system cert pool

#### Process / System
- [ ] `process.user` / `process.group` — drop privileges after port bind (Linux `setuid`/`setgid`; warning on macOS/Windows)
- [ ] `process.pid_file` — override PID file location
- [ ] `process.ulimit_nofile` — `setrlimit(RLIMIT_NOFILE)` on daemon process at startup
- [ ] `process.watchdog_interval` — `sd_notify WATCHDOG=1` pings for systemd `WatchdogSec=`

#### Phase 3.5 Tests (deferred)
- [ ] Unit tests: `DaemonConfig` parsing — all fields, defaults, unknown fields rejected
- [ ] Unit tests: JSON Schema validation of `huskyd.yaml` — valid, invalid, missing optional sections
- [ ] Unit tests: `api.base_path` prefix stripping — all routes reachable under prefix
- [ ] Unit tests: log format switch — structured JSON output when `log.format: json`
- [ ] Unit tests: `scheduler.schedule_jitter` — dispatched times fall within `[tick, tick + jitter]` bounds
- [ ] Unit tests: Prometheus metrics — counters increment on run completion, gauge reflects running count
- [ ] Unit tests: `${secret:NAME}` interpolation — resolved from mock provider, not leaked to logs
- [ ] Unit tests: `dashboard.poll_interval` + `dashboard.title` served by `GET /api/config`
- [ ] Integration test: `api.addr: 127.0.0.1:19999` — verify daemon binds to configured port
- [ ] Integration test: TLS cert + key — `https://` succeeds, `http://` rejected
- [ ] Integration test: SIGHUP with updated `log.level: debug` — debug lines appear without restart
- [ ] Integration test: storage vacuum fires; pruned row count matches `max_age` config

---

- [ ] Distributed execution across multiple machines
- [ ] External queue integrations (Kafka, Redis, RabbitMQ)
- [ ] Dynamic job creation via API at runtime
- [ ] Plugin SDK / extensible executor types
- [ ] Per-job log retention config (default: 30 days / 1,000 runs) with background vacuum
- [ ] Optional log shipping to external sinks
- [ ] DST ambiguous window warnings (§9 Risk Register)

---

### Multi-Project Daemon Mode
> Target: after v1.0 ships and real-world usage patterns are established.
>
> **Goal:** A single `huskyd` process manages multiple projects, each with its own `husky.yaml`, SQLite store, and job namespace. One binary, one process, one dashboard — regardless of how many projects are registered.
>
> **Rationale:** v1.0 ships as a per-project daemon (one daemon per `husky.yaml`). This is correct for the simple case and for developers using Husky locally. For production deployments — ops teams, servers, organisations running many pipelines — a per-project model multiplies resource overhead and eliminates the possibility of a unified operational view. V2 closes this gap without breaking v1.0 users.

### Project Registry

- [ ] Define a top-level `huskyd.yaml` server config file (distinct from per-project `husky.yaml`) — specifies bind address, TLS, auth, log retention, and the list of registered projects
- [ ] Add `projects:` block to `huskyd.yaml`: list of entries with `name` (unique identifier) and `config` (path to project's `husky.yaml`)
- [ ] Implement `husky register <path>` command — adds a project entry to `huskyd.yaml` and hot-reloads the daemon
- [ ] Implement `husky deregister <name>` command — removes project entry; waits for running jobs to complete before tearing down project context
- [ ] Implement `husky projects list` — tabular output: project name, config path, job count, status (`active` / `paused` / `error`)
- [ ] Validate that project names are unique, lowercase alphanumeric with hyphens, max 64 characters
- [ ] Detect duplicate `husky.yaml` paths at registration time; reject with a clear error

### Project Isolation

- [ ] Each registered project runs in an isolated context: independent scheduler goroutine, independent executor pool, independent SQLite database under `<data-dir>/<project-name>/husky.db`
- [ ] Per-project goroutine pool size — inherits global default from `huskyd.yaml` but overridable per project
- [ ] A panic or fatal error in one project context must not crash or stall other projects — recover and mark project as `error` state, emit alert
- [ ] Per-project `SIGHUP`-style config reload — daemon watches each `husky.yaml` for changes and reloads only the affected project context
- [ ] Global file-watcher (using `fsnotify`) replaces per-project polling; each `husky.yaml` change triggers isolated reload pipeline

### Job Namespacing

- [ ] All job identifiers are namespaced as `<project-name>::<job-name>` internally
- [ ] CLI commands accept `--project <name>` flag to scope operations: `husky run --project billing daily-report`
- [ ] Without `--project`, CLI auto-detects project by walking up from CWD until a `husky.yaml` is found, then resolves to the registered project name — preserves v1.0 UX in terminal
- [ ] `after:` dependencies within a project remain unqualified (`after: ingest`) — no breaking change to v1.0 config syntax
- [ ] Cross-project `after:` dependencies expressed as `after: "billing::daily-report"` — optional v2 feature, gated behind a config flag `allow_cross_project_deps: true`
- [ ] `husky status` without `--project` shows only the auto-detected project; `husky status --all` shows all projects

### Unified Web Dashboard

- [ ] Single web dashboard serves all projects — project selector dropdown in top navigation
- [ ] Default landing page shows cross-project summary: total jobs, running count, recent failures across all projects
- [ ] Per-project view mirrors the v1.0 dashboard (job table, log stream, run history)
- [ ] WebSocket log streams are scoped per project; subscribing to a project stream does not receive logs from other projects
- [ ] `GET /api/projects` — list all registered projects with summary stats
- [ ] `GET /api/projects/<name>/jobs` — scoped equivalent of v1.0 `GET /api/jobs`
- [ ] `GET /api/projects/<name>/jobs/<job>/runs` — scoped run history
- [ ] Retain v1.0 unscoped endpoints (`GET /api/jobs`) as aliases that resolve via CWD auto-detection or `X-Husky-Project` header

### Unified Integration Credentials

- [ ] Move Slack, PagerDuty, Discord, SMTP integration credentials to `huskyd.yaml` as global integrations — available to all projects without duplication
- [ ] Per-project `husky.yaml` may still define local integrations that override or supplement global ones — global key takes precedence unless project explicitly overrides
- [ ] `husky integrations list` shows global integrations from `huskyd.yaml`; `husky integrations list --project <name>` shows merged view for that project

### Single Unix Socket

- [ ] Replace per-project Unix sockets with a single socket at a fixed path (e.g. `/var/run/husky/huskyd.sock` or `~/.husky/huskyd.sock`)
- [ ] All IPC messages include a `project` field; daemon routes to the correct project context
- [ ] v1.0 CLI compatibility: if `project` field is absent in IPC message, daemon resolves project from the `cwd` field included in the message

### Migration from V1

- [ ] Document upgrade path in `docs/upgrading-to-v2.md`: existing per-project `huskyd` users move to running a single `huskyd` with a `huskyd.yaml` that registers their project
- [ ] `husky migrate` command — scans CWD for `husky.yaml`, generates a minimal `huskyd.yaml` with the project registered, and prints next steps
- [ ] v1.0 invocation (`huskyd --config ./husky.yaml`) continues to work unchanged as a single-project shorthand — no breaking change for existing users

### Testing

- [ ] Unit tests: project registry — register, deregister, duplicate detection, name validation
- [ ] Unit tests: job namespacing — auto-detect project from CWD, `--project` flag override, cross-project `after:` resolution
- [ ] Integration test: two projects registered, run jobs in both simultaneously, verify isolation (no log cross-contamination, independent retry state)
- [ ] Integration test: one project panics — verify other project continues running unaffected
- [ ] Integration test: reload one project's `husky.yaml` via file-watcher — verify other project is not restarted
- [ ] Integration test: `husky status --all` returns correct aggregated view across projects
- [ ] Integration test: cross-project `after:` dependency — downstream job in project B fires after upstream in project A completes
- [ ] Integration test: `husky migrate` generates valid `huskyd.yaml` from existing `husky.yaml`
- [ ] Benchmark: scheduler tick latency under 10 projects × 100 jobs (1,000 total) < 20 ms
