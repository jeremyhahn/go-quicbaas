# IAM — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/iam/`.

Enterprise-grade Identity and Access Management built into QuicBaaS.

## Overview

QuicBaaS IAM provides complete authentication and authorization capabilities as an integrated module within QuicBaaS.

## Key Features

| Feature | Description |
|---------|-------------|
| **Multi-Tenancy** | Complete tenant isolation with key prefix separation |
| **ABAC Policies** | Tenant policy administration; decisions are go-qrdb's ([ABAC](../../../go-qrdb/docs/auth/abac.md)) |
| **WebAuthn/FIDO2, PIV** | Passwordless login; the authenticators are go-xkms's ([WebAuthn](../../../go-xkms/docs/webauthn/user.md), [PIV](../../../go-xkms/docs/piv-smartcard/user.md)) and enrolled as go-qrdb [auth factors](../../../go-qrdb/docs/auth-factors/user.md) |
| **MFA/TOTP** | Multi-factor authentication with backup codes |
| **OAuth2/OIDC Provider** | Issue tokens to your applications |
| **OAuth2/OIDC Consumer** | Social login (Google, GitHub, Azure, etc.) |
| **SSO** | Single Sign-On across all registered applications |
| **PEP Service** | gRPC and REST access checks for external services ([API](api.md#pep-grpc-api)) |
| **GDPR Compliance** | Consent management, data export, and deletion |
| **Delegated Admin** | Role-based admin assignments with impersonation |
| **Audit Logging** | Complete security event tracking with admin-only deletion |

## Architecture Decision

**Single QuicBaaS product with integrated IAM module** - NOT a separate project.

| Aspect | Decision |
|--------|----------|
| Architecture | IAM built into QuicBaaS as optional module |
| Binary | Single `quicbaas` binary |
| Activation | Always on; there is no `iam.enabled` switch |
| Deployment | Runs in every node; no separate IAM cluster ([Deployment](#deployment)) |

**Rationale**:
- One binary is simpler to deploy and maintain
- IAM data gets same Raft replication guarantees
- Shared runtime eliminates inter-process overhead
- Single documentation set and unified CLI

## Quick Start

### Configure the relying party

IAM is always on. Passkey login needs the WebAuthn relying party; every key is
in [engineering: Configuration](engineering.md#configuration).

```yaml
# quicbaas.yaml
iam:
  webauthn:
    rp_id: "example.com"
    rp_display_name: "Example Application"
    rp_origins: ["https://example.com"]
    attestation_roots_file: "/etc/quicbaasd/fido-attestation-roots.pem"
  aaguid:
    allowlist_file: "/etc/quicbaasd/aaguid.json"
```

### Register an Application

```bash
quicbaas iam app register \
    --name "my-app" \
    --tenant "my-tenant" \
    --redirect-uris "https://myapp.com/callback"
```

### Create a Policy

```yaml
apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: basic-access
  tenant: my-tenant

spec:
  rules:
    - name: read-own-data
      effect: allow
      actions: [read]
      resources:
        - keys/users/${subject.id}/**
      subjects:
        authenticated: true
```

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | System architecture and components |
| Configuration | Complete configuration reference |
| [Deployment](#deployment) | Where IAM runs |
| MCP Integration | OAuth 2.1 for MCP, IdP federation |
| [ABAC Policy Syntax](../authorization/engineering.md) | Complete policy language reference |
| [Developer Guide](engineering.md) | Integrating apps with QuicBaaS IAM |
| [API Reference](api.md) | REST and gRPC API documentation |
| [WebAuthn Guide](engineering.md) | Passwordless authentication with passkeys and security keys |
| [Session Renewal](engineering.md) | Short access tokens, rotating refresh credentials, SPA/CLI renewal |
| [GDPR Compliance](engineering.md) | Consent, data export, and deletion |
| [Admin & Impersonation](architecture.md) | Delegated administration and user impersonation |
| [Audit Logging](../audit-logging/architecture.md) | Security event tracking and permissions |

## External Dependencies

| Library | Purpose | License |
|---------|---------|---------|
| [zitadel/oidc](https://github.com/zitadel/oidc) | OIDC provider + consumer | Apache 2.0 |
| [go-codec](https://github.com/jeremyhahn/go-codec) | Policy serialization | Apache 2.0 |
| [go-xkms](https://github.com/jeremyhahn/go-xkms) | WebAuthn/FIDO2 and PIV | AGPL-3.0 |

## Comparison with AWS IAM/Cognito

| Aspect | AWS IAM/Cognito | QuicBaaS IAM |
|--------|-----------------|--------------|
| Deployment | Cloud-only | Self-hosted |
| Policy Syntax | JSON (verbose) | YAML (clean) |
| Multi-tenancy | Via Organizations | Native |
| Pricing | Per MAU | Free (self-hosted) |
| Vendor Lock-in | AWS only | None |


---

## Deployment

IAM has no deployment mode of its own. It runs inside every `quicbaas` node,
and its data replicates with the node's embedded go-qrdb store. There is no
dedicated IAM cluster, remote-PEP mode or PEP sync port, and the `iam:` block
has no `mode`, `remote`, `pep` or `oidc` key; the keys that do exist are listed in
[engineering: Configuration](engineering.md#configuration). Authorization
decisions are go-qrdb's
([ABAC Authorization Decorator](../../../go-qrdb/docs/auth/abac.md)).
