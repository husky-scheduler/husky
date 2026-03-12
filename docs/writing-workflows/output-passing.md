---
title: Output passing
sidebar_label: Output passing
description: Capture values from one job and pass them to downstream jobs.
---

# Output passing

Output passing lets one job publish values that downstream jobs can consume during the same trigger chain.

## Why it exists

Without output passing, pipelines tend to rely on:

- hard-coded temp paths
- shared global environment variables
- ad hoc files with unclear ownership

Husky gives outputs first-class scope and persistence.

## Declare outputs

```yaml
jobs:
  ingest_events:
    description: "Download events"
    frequency: manual
    command: "./scripts/ingest.sh"
    output:
      file_path: last_line
      exit_code: exit_code
```

## Capture modes

| Mode | Meaning |
| --- | --- |
| `last_line` | Final non-empty stdout line |
| `first_line` | First stdout line |
| `json_field:<key>` | Parse stdout as JSON and extract a field |
| `regex:<pattern>` | Capture the first regex group |
| `exit_code` | Store the process exit code |

## Use outputs in downstream jobs

Outputs can be referenced in:

- `command`
- `env`

```yaml
jobs:
  transform_events:
    description: "Transform the file from ingest"
    frequency: after:ingest_events
    command: "./scripts/transform.sh"
    env:
      INPUT_FILE: "{{ outputs.ingest_events.file_path }}"
```

## Example chain

```yaml
jobs:
  ingest_events:
    description: "Download raw events"
    frequency: manual
    command: "./scripts/ingest.sh"
    output:
      file_path: last_line

  transform_events:
    description: "Transform raw file"
    frequency: after:ingest_events
    command: "./scripts/transform.sh"
    env:
      INPUT_FILE: "{{ outputs.ingest_events.file_path }}"
    output:
      record_count: json_field:count

  generate_report:
    description: "Report on transformed data"
    frequency: after:transform_events
    command: "./scripts/report.sh"
    env:
      RECORD_COUNT: "{{ outputs.transform_events.record_count }}"
```

## Cycle scoping

Outputs are scoped by `cycle_id`. That means:

- one manual run of a root job creates one cycle
- downstream jobs in that chain read outputs only from that same cycle
- later runs do not reuse earlier cycle values

## Failure behavior

If a downstream template references an output that does not exist for the current `cycle_id`, dispatch fails immediately with a descriptive error.

## Inspection

Use:

```bash
husky logs <job>
curl http://127.0.0.1:8420/api/runs/<id>/outputs
curl http://127.0.0.1:8420/api/db/run_outputs
```

The dashboard also shows captured outputs in run detail views.
