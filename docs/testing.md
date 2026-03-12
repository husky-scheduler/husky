---
title: Testing guide
sidebar_label: Testing
description: Unit, integration, frontend, and manual testing strategy for Husky.
sidebar_position: 9
---

# Husky testing guide

Husky uses a layered test strategy:

- package-level unit tests for validation, scheduling, executor logic, store queries, API handlers, auth, and notifications
- integration suites for daemon lifecycle, pipelines, catchup, reload, SLA, healthchecks, and API behavior
- frontend tests for dashboard utilities
- manual end-to-end scenarios under `testproject/`

## Fast local workflow

Run everything:

```bash
make test
```

Run Go tests only:

```bash
go test ./...
```

Run frontend tests only:

```bash
cd web
npm ci --silent
npm test
```

## Integration suites

The main integration-heavy Go packages are:

```bash
go test ./cmd/huskyd ./tests
```

Important files include:

- `cmd/huskyd/recovery_integration_test.go`
- `cmd/huskyd/phase4_integration_test.go`
- `tests/integration_test.go`
- `tests/daemon_integration_test.go`
- `tests/phase4_integration_test.go`

Run one integration test by name:

```bash
go test ./cmd/huskyd -run TestIntegration_FullPipeline_ExecutesEndToEnd -v
go test ./tests -run TestIntegration_RunReason_AuditFilter_AndNotificationTemplate -v
```

## What is covered where

### Config and schema

- `internal/config/*_test.go`
- validates enum values, durations, time formats, timezone handling, tags, integrations, `sla < timeout`, and strict-mode rules

### DAG and scheduling

- `internal/dag/dag_test.go`
- `internal/scheduler/schedule_test.go`
- `internal/scheduler/sla_test.go`

These cover topological ordering, cycle detection, schedule evaluation, timezone logic, and DST anomalies.

### Executor and subprocess behavior

- `internal/executor/executor_test.go`
- `internal/executor/output_test.go`
- `internal/executor/global_env_test.go`

These cover:

- stdout / stderr capture
- timeouts and cancellation
- healthchecks
- output capture modes
- global vs per-job env layering

### Store and persistence

- `internal/store/store_test.go`
- `internal/store/vacuum_test.go`

These cover migrations, CRUD behavior, retention vacuuming, and query semantics.

### API, auth, dashboard, notifications

- `internal/api/server_test.go`
- `internal/api/dashboard_test.go`
- `internal/auth/auth_test.go`
- `internal/notify/dispatcher_test.go`

## Manual scenario project

`testproject/` contains scenario folders for higher-confidence manual validation.

Examples include:

- basic scheduling
- pipelines
- reliability
- SLA and healthchecks
- notifications
- tags
- audit trail
- timezone
- catchup
- validation errors

Use these when changing product behavior that spans multiple subsystems.

## Recommended workflow for feature work

1. run focused unit tests for the package being changed
2. run the targeted integration package
3. run `go test ./...`
4. if dashboard or embedded assets changed, run `make build` or `make web`
5. if behavior is user-facing and cross-cutting, validate against `testproject/`

## Example commands

Build and run full suite:

```bash
make build
go test ./...
```

Target only integration suites:

```bash
go test ./cmd/huskyd ./tests -v
```

Target dashboard source tests:

```bash
cd web && npm test
```

## Notes for documentation-driven development

When changing documented behavior, update:

- root `README.md`
- the relevant file under `docs/`
- `docs/task.md` if a tracked documentation deliverable changed state

`dev-docs/` is archived planning material and should not be treated as the source of truth for current runtime behavior.
