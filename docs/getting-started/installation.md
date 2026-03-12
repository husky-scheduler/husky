---
title: Installation
sidebar_label: Installation
description: How to build, install, and verify Husky.
---

# Installation

## Build from source

From the repository root:

```bash
make build
./bin/husky --version
```

To use the built binary in the current shell:

```bash
export PATH="$PWD/bin:$PATH"
```

## Install with the project script

```bash
curl -fsSL https://raw.githubusercontent.com/husky-scheduler/husky/main/install.sh | sh
```

## Package-based installs

The repository includes packaging assets for:

- Homebrew
- launchd
- systemd
- nfpm-based packaging

See the files under `packaging/` when preparing an OS-native install.

## Verify the binary

Check both the CLI and daemon entrypoints:

```bash
husky version
husky daemon run --help
```

Expected version output includes:

- version
- commit
- build date

## Repo prerequisites

A basic Husky project needs:

- `husky.yaml`
- an optional `huskyd.yaml`
- executable scripts referenced by job commands

## Recommended `.gitignore`

Ignore generated runtime state:

```gitignore
.husky/
```

You should usually commit:

- `husky.yaml`
- `huskyd.yaml` when used
- scripts and assets called by jobs

## First validation step

Before starting the daemon, validate configuration:

```bash
husky validate --strict
```

This checks schema, frequencies, enum values, time fields, timezones, cycle errors, and daemon config when present.
