---
title: Tags, audit, and timezones
sidebar_label: Tags, audit, and timezones
description: Grouping, filtering, run reasons, and per-job timezone behavior.
---

# Tags, audit, and timezones

## Tags

Tags group jobs for filtering and bulk operations.

```yaml
jobs:
  ingest_users:
    tags: [critical, data-pipeline, nightly]
```

Validation rules:

- lowercase alphanumeric and hyphens only
- maximum 10 tags per job
- maximum 32 characters per tag

### CLI examples

```bash
husky tags list
husky status --tag critical
husky run --tag nightly
husky pause --tag maintenance
husky resume --tag maintenance
husky logs --tag data-pipeline --tail
```

### API examples

```bash
curl "http://127.0.0.1:8420/api/jobs?tag=critical"
curl "http://127.0.0.1:8420/api/tags"
```

## Audit trail

Husky records searchable run history across jobs.

Each run can carry:

- trigger type
- status
- reason
- created time

### Manual reason

```bash
husky run manual_job --reason "testing audit trail feature"
```

Reason text is stored with the run and appears in:

- `husky audit`
- `husky history`
- notification templates
- `/api/audit`

### CLI examples

```bash
husky audit
husky audit --job manual_job
husky audit --trigger manual
husky audit --status failed
husky audit --reason "testing audit"
husky audit --tag auditable
husky audit --export csv
```

## Timezones

Each job can choose an IANA timezone:

```yaml
defaults:
  timezone: "UTC"

jobs:
  tokyo_midnight:
    frequency: daily
    time: "0000"
    timezone: "Asia/Tokyo"
```

Resolution order:

1. job `timezone`
2. `defaults.timezone`
3. system timezone

Husky validates timezone identifiers at parse time and logs resolved local and UTC run times on daemon startup.
