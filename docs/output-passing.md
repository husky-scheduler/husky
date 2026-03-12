---
title: Output passing guide
sidebar_label: Output passing
description: Capture structured job output and pass it safely to downstream jobs using cycle-scoped templates.
sidebar_position: 6
---

# Husky output passing guide

Output passing lets one job capture structured values from its own execution and feed those values into downstream jobs in the same pipeline cycle.

This is one of Husky's most important pipeline features because it removes the need for ad hoc temp files and hard-coded shared paths.

## Core idea

An upstream job declares `output:` capture rules.

A downstream job references those values with:

```text
{{ outputs.<job_name>.<var_name> }}
```

Husky stores captured values in `run_outputs` and scopes them to a `cycle_id`, so outputs from one execution chain do not leak into another.

## Example

```yaml
jobs:
  ingest:
    description: "Ingest source data and print the generated file path"
    frequency: daily
    time: "0200"
    command: "./scripts/ingest.sh"
    output:
      file_path: last_line

  transform:
    description: "Transform the file produced by ingest"
    frequency: after:ingest
    command: "python transform.py --input {{ outputs.ingest.file_path }}"
```

If `ingest` prints `/tmp/data-2026-03-09.json` on its final line, Husky stores that as `outputs.ingest.file_path` for the current cycle and injects it into `transform` at dispatch time.

## Supported capture modes

### `last_line`

Captures the final non-empty stdout line.

```yaml
output:
  file_path: last_line
```

Good for scripts that print a single final artifact path or ID.

### `first_line`

Captures the first stdout line.

```yaml
output:
  build_id: first_line
```

### `json_field:<key>`

Parses stdout as JSON and extracts a top-level key.

```yaml
output:
  count: json_field:total
```

Example stdout:

```json
{"total":42,"ok":true}
```

Captured value becomes `42`.

### `regex:<pattern>`

Extracts the first regex capture group from stdout.

```yaml
output:
  version: regex:v([0-9.]+)
```

Example stdout line:

```text
published version v2.4.1
```

Captured value becomes `2.4.1`.

### `exit_code`

Captures the numeric exit code.

```yaml
output:
  code: exit_code
```

Useful for downstream reporting or conditional logic outside Husky.

## Where templates can be used

Output templates are rendered at dispatch time in:

- `command`
- job `env` values

Examples:

```yaml
command: "node report.js --input {{ outputs.ingest.file_path }}"

env:
  INPUT_FILE: "{{ outputs.ingest.file_path }}"
  RECORD_COUNT: "{{ outputs.transform.count }}"
```

## `cycle_id` scoping

Every root trigger chain gets a generated `cycle_id`.

A root trigger chain can start from:

- a scheduled tick
- a manual run
- a recovery-triggered retry/catchup sequence

That `cycle_id` propagates through downstream dependency execution.

Why it matters:

- outputs from cycle A are not visible to cycle B
- independent manual runs do not cross-contaminate pipeline data
- debugging remains deterministic even for repeated runs of the same jobs

## What happens when data is missing

If a job references `{{ outputs.job.var }}` but there is no matching value for the current `cycle_id`, dispatch fails with a descriptive error.

That failure happens at dispatch time rather than silently rendering an empty string.

## Design constraints and best practices

### Prefer structured stdout for structured data

If a script already knows how to emit JSON, use `json_field:<key>` instead of regex parsing.

### Keep captured values small

Outputs are stored in SQLite and intended for identifiers, artifact paths, counts, and similar small payloads. Avoid dumping large documents or binary-like blobs to stdout just to capture them.

### Make captured values intentional

Design job scripts so the captured line or JSON field is stable and documented.

### Avoid ambiguous stdout

If a job writes lots of human-readable logs, `last_line` can be fragile unless the script intentionally prints the final machine-readable output last.

## Example patterns

### Pass a generated file path

```yaml
output:
  file_path: last_line
```

### Pass a JSON result count

```yaml
output:
  count: json_field:total
```

### Capture a release version from tool output

```yaml
output:
  version: regex:release\s+([0-9.]+)
```

### Use outputs in env

```yaml
env:
  ARTIFACT_PATH: "{{ outputs.build.file_path }}"
```

## Storage model

Captured outputs are stored in the `run_outputs` table with:

- `run_id`
- `job_name`
- `var_name`
- `value`
- `cycle_id`

This allows:

- per-run inspection
- cycle-level debugging
- dashboard and API browsing of pipeline data

## Observability

You can inspect outputs via:

- `GET /api/runs/:id/outputs`
- dashboard run detail
- dashboard data explorer for `run_outputs`

## Relationship to DAG execution

Output passing does not replace DAG validation; it complements it.

Typical pattern:

- use `depends_on` or `after:<job>` to define ordering
- use outputs to pass the values needed by downstream steps

Without a dependency edge, output availability is not guaranteed.

## Testing output passing

Relevant test coverage includes:

- capture modes in `internal/executor/output_test.go`
- cycle scoping in package tests and integration suites
- cross-package integration in `tests/phase4_integration_test.go`

Run focused integration coverage with:

```bash
go test ./tests -run TestIntegration_OutputPassing_CycleIsolation -v
```
