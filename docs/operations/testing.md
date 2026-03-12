---
title: Testing
sidebar_label: Testing
description: Automated and manual validation for Husky workflows and runtime behavior.
---

# Testing

Husky includes automated coverage and a large manual scenario suite under `testproject/`.

## Fast validation loop

For normal development:

```bash
make build
make test
husky validate --strict
```

## Automated coverage areas

The repository includes tests for:

- config parsing and validation
- schedule evaluation
- DAG cycle detection
- retry state transitions
- output capture modes
- healthcheck behavior
- REST API handlers
- integration and crash-recovery scenarios

## Manual scenario suite

The main operator guide is:

- `testproject/MANUAL_TEST_GUIDE.md`

It covers end-to-end scenarios such as:

- basic scheduling
- DAG pipelines and output passing
- retry and concurrency controls
- SLA budgets and healthchecks
- notifications
- tags and bulk operations
- audit trail
- timezones
- crash recovery and catchup
- validation errors
- REST API spot checks
- dashboard workflows
- WebSocket log streaming

## Useful scenario directories

- `testproject/01-basic`
- `testproject/02-pipeline`
- `testproject/03-reliability`
- `testproject/04-sla-healthcheck`
- `testproject/05-notifications`
- `testproject/06-tags`
- `testproject/07-audit-trail`
- `testproject/08-timezone`
- `testproject/09-catchup`
- `testproject/10-validation-errors`

## Suggested release checklist

Before shipping a change:

1. run build and automated tests
2. validate config fixtures
3. exercise at least one manual scenario for the changed subsystem
4. verify dashboard and API behavior if operator-facing changes were made
5. verify crash or reload behavior for scheduler and executor changes
