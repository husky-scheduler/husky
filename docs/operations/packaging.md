---
title: Packaging and deployment
sidebar_label: Packaging and deployment
description: Service-manager and packaging artifacts included with the repository.
---

# Packaging and deployment

Husky is a single binary, but the repository includes packaging assets for common deployment targets.

## Included packaging assets

Under `packaging/` you will find examples for:

- Homebrew
- launchd
- systemd
- nfpm

These assets help turn the same binary into a workstation install or managed service.

## Homebrew tap publishing

The release workflow also updates the Homebrew tap repository automatically.

- tag a release as `v*`, for example `v0.1.0-alpha.1`
- the release workflow publishes GitHub release archives with GoReleaser
- a follow-up job downloads `checksums.txt`, renders `packaging/homebrew/husky.rb`, and pushes the formula to the `husky-scheduler/homebrew-husky` tap repo

## Typical deployment modes

### Developer workstation

- build or install the binary
- keep `husky.yaml` in the project root
- run `husky start`
- use the local dashboard and CLI

### Service-managed process

- install the binary into a standard path
- place config files in a stable location
- point the service unit at `husky daemon run`
- persist the data directory somewhere durable for that host

## Operational advice

- keep the data directory writable by the service account
- log to a known file or stdout depending on the service manager
- validate config before reload or restart
- protect non-local API bindings with auth and TLS
