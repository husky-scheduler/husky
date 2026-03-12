# Changelog

All notable public release changes for Husky will be documented in this file.

Husky follows a lightweight prerelease-first versioning policy:

- `v0.x.y-alpha.z` for public alpha releases
- `v0.x.y-beta.z` for beta releases
- `v0.x.y` for stable releases

Before `v1.0.0`, breaking changes are allowed between prereleases and minor releases when needed to improve the product.

## v0.1.0-alpha.1

First public alpha release.

### What alpha means

- Husky is usable for evaluation, local automation, and early adopter feedback
- breaking changes are still likely before beta
- CLI behavior, APIs, config fields, and packaging details may still change
- this release is not yet recommended for critical production workloads

### Included in this alpha

- source code for the single-binary Husky runtime
- prebuilt GitHub release archives for supported release targets
- Homebrew tap updates via `husky-scheduler/homebrew-husky`
- launchd and systemd packaging assets in `packaging/`
- published docs site from `docs/` and `docs-site/`

### Highlights

- repository prepared for the first public alpha from a clean public-history snapshot
- single-binary CLI and embedded daemon workflow runtime
- SQLite-backed local state with reload and crash-recovery support
- DAG-aware scheduling, retries, healthchecks, SLA tracking, tags, and audit history
- embedded dashboard, REST API, and WebSocket log streaming
- GitHub Actions based release automation for binaries, docs, and Homebrew tap updates

### Known issues and current limits

- Windows artifacts may build, but service-manager packaging and operator documentation are centered on macOS and Linux
- OIDC auth is not implemented end to end yet
- Postgres storage is not ready for production use yet
- metrics, tracing, and secrets backends remain partially declared surfaces rather than complete runtime features
- large-scale performance characterization is still limited compared with the intended post-alpha hardening work

### Upgrade expectations

- treat alpha-to-alpha upgrades as potentially breaking
- re-run `husky validate --strict` before and after upgrades
- review release notes before adopting a newer prerelease