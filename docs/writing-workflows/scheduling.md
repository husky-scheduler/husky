---
title: Scheduling
sidebar_label: Scheduling
description: Schedule syntax, default run times, intervals, and timezone-aware scheduling.
---

# Scheduling

## Built-in schedule forms

| Frequency | Meaning |
| --- | --- |
| `hourly` | Runs at the next top-of-hour |
| `daily` | Runs once per day at `time` |
| `weekly` | Runs every Monday at `time` or `defaults.default_run_time` |
| `monthly` | Runs on the first of the month at `time` |
| `weekdays` | Runs Monday through Friday |
| `weekends` | Runs Saturday and Sunday |
| `manual` | Never auto-schedules |
| `after:<job>` | Runs after another job succeeds |
| `every:<interval>` | Runs on a fixed interval under 24h |
| `on:[day,...]` | Runs on specific weekdays |

## Time field

`time` is a zero-padded 24-hour string such as:

- `0000`
- `0200`
- `0930`
- `1430`

Invalid examples include:

- `2am`
- `200`
- `25:00`
- `2599`

## Default run time

`weekly`, `weekdays`, `weekends`, and `on:[...]` can omit `time` and inherit `defaults.default_run_time`.

If `defaults.default_run_time` is not set, Husky falls back to `0300`.

## Interval schedules

Use `every:<duration>` for frequent local polling or heartbeat jobs.

Examples:

```yaml
frequency: every:15s
frequency: every:5m
frequency: every:2h
```

Intervals must be:

- valid Go duration strings
- greater than `0`
- less than `24h`

## Day-list schedules

Use `on:[...]` for explicit weekday scheduling.

```yaml
frequency: on:[monday,wednesday,friday]
time: "0100"
```

Accepted day names are full lowercase weekday names.

## Dependency-driven schedules

Use `after:<job>` when the job should run after another job completes successfully.

```yaml
frequency: after:ingest_events
```

This is dependency-driven, not wall-clock scheduling. The `time` field is ignored.

## Timezones

A job can define its own IANA timezone, otherwise it inherits the default timezone or the system timezone.

```yaml
defaults:
  timezone: "UTC"

jobs:
  new_york_morning:
    frequency: daily
    time: "0800"
    timezone: "America/New_York"
```

## DST behavior

Husky handles daylight-saving transitions:

- nonexistent times in a DST gap are advanced to the next valid time
- overlapping times run once on the first occurrence

## Validation tips

Run:

```bash
husky validate
husky status
```

Use status output to confirm the computed next run for each job.
