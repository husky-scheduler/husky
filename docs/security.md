---
title: Security guide
sidebar_label: Security
description: Authentication, RBAC, TLS, secret handling, and hardening guidance for Husky deployments.
sidebar_position: 8
---

# Husky security guide

Husky is designed to be safe by default for local use, but its HTTP API, dashboard, and subprocess execution model still deserve deliberate hardening in production-like environments.

## Security model summary

By default, Husky is optimized for local-first workflows:

- the daemon typically binds the API to localhost only
- the CLI uses a local Unix socket for daemon control
- state is stored in a local SQLite database
- subprocesses run on the same machine as the daemon

If you expose Husky beyond the local machine, treat it like any privileged process-control API.

## API exposure

### Default posture

If `huskyd.yaml` leaves `api.addr` empty, the daemon binds to `127.0.0.1:0` and chooses a free local port. This is the safest default.

### If you bind to a stable or non-local address

If you set something like:

```yaml
api:
  addr: 0.0.0.0:8420
```

also enable:

- authentication
- RBAC when multiple users/roles are involved
- TLS when traffic crosses trust boundaries

## Authentication

Husky currently supports:

- `none` — no auth
- `bearer` — static bearer tokens
- `basic` — HTTP Basic auth with bcrypt password hashes

OIDC is declared in config but not implemented.

### Bearer auth

Recommended pattern:

```yaml
auth:
  type: bearer
  bearer:
    token_file: /etc/husky/tokens.txt
```

Why token files are preferred:

- easier secret rotation
- avoids committing inline tokens to config
- tokens can be hot-reloaded on SIGHUP / `husky reload`

The CLI can also use `HUSKY_TOKEN` when making authenticated HTTP requests.

### Basic auth

```yaml
auth:
  type: basic
  basic:
    users:
      - username: admin
        password_hash: "$2a$...bcrypt-hash..."
```

Important:

- Husky rejects plaintext passwords at load time
- use bcrypt hashes only
- use HTTPS if credentials cross a network

## RBAC

RBAC is layered on top of auth.

Built-in defaults when auth is enabled and no explicit rules are defined:

- `admin` — all methods and paths
- `operator` — all methods and paths
- `viewer` — `GET`, `HEAD`, `OPTIONS` only

With explicit `rbac` rules, your rules fully replace the defaults.

Example:

```yaml
auth:
  type: bearer
  bearer:
    token_file: /etc/husky/tokens.txt
  rbac:
    - role: viewer
      methods: [GET, HEAD, OPTIONS]
      paths: ["/api/*", "/ws/*"]
    - role: admin
      methods: [GET, POST, PUT, PATCH, DELETE, OPTIONS]
      paths: ["*"]
```

## TLS

Enable TLS when exposing Husky to other machines or networks.

```yaml
api:
  addr: 0.0.0.0:8420
  tls:
    enabled: true
    cert: /etc/husky/tls/server.crt
    key: /etc/husky/tls/server.key
    min_version: "1.2"
```

Optional mutual TLS is supported via `client_ca`.

Recommendations:

- use TLS 1.2 or 1.3 only
- keep certificates and keys outside the repo
- combine TLS with auth, not instead of auth

## CORS and browser access

If you use browser-based access outside same-origin local usage, configure CORS deliberately.

```yaml
api:
  cors:
    allowed_origins:
      - https://husky.internal.example
    allow_credentials: true
```

Do not use `*` unless you fully understand the exposure.

## Secret handling

### In `husky.yaml`

Use `${env:VAR}` instead of embedding secrets directly.

Examples:

```yaml
integrations:
  slack:
    webhook_url: "${env:SLACK_WEBHOOK_URL}"

jobs:
  deploy:
    env:
      API_KEY: "${env:DEPLOY_API_KEY}"
```

Husky also supports loading a `.env` file from the same directory as `husky.yaml`, while keeping explicit process environment variables higher priority.

### In `huskyd.yaml`

Prefer file references for tokens and certificates over inline secret values.

## Process isolation and execution

Husky runs shell commands on the local machine. Treat job definitions as code execution.

Recommendations:

- restrict write access to `husky.yaml` and `huskyd.yaml`
- run Husky under a dedicated service account where practical
- keep the data directory owned by the Husky service user
- review scripts invoked by jobs just like application code
- use separate OS users or containers if jobs have very different trust levels

Executor-related controls in `huskyd.yaml` include:

- `executor.resource_limits.max_memory_mb`
- `executor.resource_limits.max_open_files`
- `executor.resource_limits.max_pids`
- `executor.global_env`

These help contain jobs but are not a substitute for full sandboxing.

## Network-facing notifications

Notification integrations may send data off-host.

Be deliberate about:

- what message templates include
- whether logs are attached
- whether secrets might appear in command output
- who receives `on_failure` or `on_sla_breach` events

If logs may contain sensitive data, avoid `attach_logs: all`.

## Data at rest

Husky stores operational data in SQLite:

- run metadata
- reasons and triggered-by fields
- logs
- captured outputs
- alert delivery history

That means your database can contain sensitive operational information. Protect the data directory accordingly.

Recommendations:

- set restrictive file permissions on the data directory
- include the database in host-level backup and encryption policies as appropriate
- use retention settings to reduce long-lived sensitive history

## Audit logs

Optional audit log output can write newline-delimited JSON to a dedicated file. If enabled, protect that file with the same care as the database.

## Secure deployment checklist

For any shared or remote environment:

- bind to a deliberate address
- enable bearer or basic auth
- add RBAC if you need read-only vs admin users
- enable TLS
- store secrets outside version control
- run under a dedicated user
- restrict access to the data directory
- validate config before reloads
- keep retention bounded if logs may contain sensitive output

## Known limitations

- OIDC auth is not yet implemented
- Postgres storage is not yet implemented
- not every future-facing `process`, `metrics`, `tracing`, or `secrets` field is fully wired operationally yet

Until those surfaces are completed, rely on the implemented local-first controls above.
