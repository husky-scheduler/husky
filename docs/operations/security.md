---
title: Security
sidebar_label: Security
description: API exposure, authentication, RBAC, TLS, and secrets handling.
---

# Security

Husky is local-first by default, but it still includes controls for securing the API when remote access is needed.

## Default posture

By default Husky binds locally and can run with no auth. That is convenient for workstation use, but not appropriate for broader network exposure.

## API binding

Set the API bind address in `huskyd.yaml`.

```yaml
api:
  addr: "127.0.0.1:8420"
```

If you bind to a non-local interface, enable authentication and TLS.

## Supported auth types

Configured under `auth.type`:

- `none`
- `bearer`
- `basic`
- `oidc`

Current runtime support:

- `none` works
- `bearer` works
- `basic` works
- `oidc` is declared in config but not yet supported at runtime

## Bearer auth

```yaml
auth:
  type: bearer
  bearer:
    token_file: "/etc/husky/tokens.txt"
```

Bearer token files can be hot-reloaded on `husky reload`.

## Basic auth

```yaml
auth:
  type: basic
  basic:
    users:
      - username: admin
        password_hash: "$2a$...bcrypt..."
```

Passwords must be bcrypt hashes.

## RBAC

RBAC rules restrict HTTP methods and paths by role.

Built-in role names are:

- `viewer`
- `operator`
- `admin`

Without explicit custom rules, `viewer` is read-only and the other roles allow full access.

## TLS

Enable HTTPS in daemon config:

```yaml
api:
  tls:
    enabled: true
    cert: "/path/to/cert.pem"
    key: "/path/to/key.pem"
    min_version: "1.2"
```

Mutual TLS is supported through `client_ca`.

## Secrets handling

Do not hardcode provider secrets in workflow files. Use environment interpolation:

```yaml
webhook_url: "${env:WEBHOOK_URL}"
password: "${env:SMTP_PASSWORD}"
```

A local `.env` file can supply values at parse time, while existing process environment variables take precedence.

## Dashboard and API hardening checklist

- bind to loopback unless remote access is required
- enable bearer or basic auth for non-local exposure
- enable TLS for non-local exposure
- store tokens and hashes outside the repo
- commit config, not generated runtime state
- keep `.husky/` ignored
