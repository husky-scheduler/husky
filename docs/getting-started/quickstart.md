---
title: Quickstart
sidebar_label: Quickstart
description: Create a small Husky pipeline and run it locally.
---

# Quickstart

This example creates a two-step pipeline:

1. a scheduled ingest job
2. a downstream report job that uses captured output

## 1. Create `husky.yaml`

```yaml
version: "1"
defaults:
  timeout: "10m"
  retries: 1
  retry_delay: exponential
  default_run_time: "0900"
  timezone: "UTC"

jobs:
  ingest_events:
    description: "Download fresh events and print the output file path"
    frequency: every:30s
    command: "./scripts/ingest.sh"
    output:
      file_path: last_line

  generate_report:
    description: "Generate a report from the latest ingest output"
    frequency: after:ingest_events
    command: "./scripts/report.sh {{ outputs.ingest_events.file_path }}"
    on_failure: stop
```

## 2. Add example scripts

```bash
mkdir -p scripts
cat > scripts/ingest.sh <<'EOF'
#!/usr/bin/env sh
echo "[ingest] downloading events"
echo "/tmp/husky/events.json"
EOF
chmod +x scripts/ingest.sh

cat > scripts/report.sh <<'EOF'
#!/usr/bin/env sh
echo "[report] using input: $1"
EOF
chmod +x scripts/report.sh
```

## 3. Validate config

```bash
husky validate
```

## 4. Start the daemon

```bash
husky start
```

## 5. Inspect status

```bash
husky status
husky dag
```

## 6. Trigger the root job manually

```bash
husky run ingest_events --reason "quickstart smoke test"
```

## 7. Watch output

```bash
husky logs ingest_events
husky logs generate_report
husky history ingest_events
husky audit
```

## 8. Open the dashboard
```bash
husky dash
```

Or visit the address stored in `.husky/api.addr`.

## 9. Stop Husky

```bash
husky stop
```

## Next steps

- Learn the CLI in [CLI overview](../getting-started/cli.md)
- Author richer jobs in [Workflow authoring](../writing-workflows/overview.md)
- Review runtime operations in [Operations](../operations/overview.md)
