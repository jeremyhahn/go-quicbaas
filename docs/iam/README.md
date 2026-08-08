# QuicBaaS IAM

Enterprise-grade Identity and Access Management built into QuicBaaS.

## Overview

QuicBaaS IAM provides complete authentication and authorization capabilities as an integrated module within QuicBaaS. It can be deployed as a single integrated instance or as dedicated IAM clusters for enterprise scale.

## Key Features

| Feature | Description |
|---------|-------------|
| **Multi-Tenancy** | Complete tenant isolation with key prefix separation |
| **ABAC Policy Engine** | Attribute-Based Access Control with sub-5μs evaluation |
| **WebAuthn/FIDO2** | Passwordless authentication via hardware keys and passkeys |
| **MFA/TOTP** | Multi-factor authentication with backup codes |
| **OAuth2/OIDC Provider** | Issue tokens to your applications |
| **OAuth2/OIDC Consumer** | Social login (Google, GitHub, Azure, etc.) |
| **SSO** | Single Sign-On across all registered applications |
| **PEP Library** | Distributed policy enforcement for external services |
| **GDPR Compliance** | Consent management, data export, and deletion |
| **Delegated Admin** | Role-based admin assignments with impersonation |
| **Audit Logging** | Complete security event tracking with admin-only deletion |

## Architecture Decision

**Single QuicBaaS product with integrated IAM module** - NOT a separate project.

| Aspect | Decision |
|--------|----------|
| Architecture | IAM built into QuicBaaS as optional module |
| Binary | Single `quicbaas` binary |
| Activation | `iam.enabled: true` in config |
| Deployment | Integrated OR dedicated IAM cluster (same binary, different config) |

**Rationale**:
- One binary is simpler to deploy and maintain
- IAM data gets same Raft replication guarantees
- Shared runtime eliminates inter-process overhead
- Single documentation set and unified CLI

## Quick Start

### Enable IAM

```yaml
# quicbaas.yaml
iam:
  enabled: true
  mode: primary

  auth:
    local:
      enabled: true
      webauthn: true

  oidc:
    issuer: "https://iam.example.com"
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
| [Configuration](configuration.md) | Complete configuration reference |
| [Deployment](deployment.md) | Deployment models and scaling |
| [MCP Integration](mcp-integration.md) | OAuth 2.1 for MCP, IdP federation |
| [ABAC Policy Syntax](abac-policy-syntax.md) | Complete policy language reference |
| [Developer Guide](developer-guide.md) | Integrating apps with QuicBaaS IAM |
| [API Reference](api-reference.md) | REST and gRPC API documentation |
| [WebAuthn Guide](webauthn.md) | Passwordless authentication with passkeys and security keys |
| [Session Renewal](session-renewal.md) | Short access tokens, rotating refresh credentials, SPA/CLI renewal |
| [GDPR Compliance](gdpr.md) | Consent, data export, and deletion |
| [Admin & Impersonation](admin-impersonation.md) | Delegated administration and user impersonation |
| [Audit Logging](audit-logging.md) | Security event tracking and permissions |

## External Dependencies

| Library | Purpose | License |
|---------|---------|---------|
| [zitadel/oidc](https://github.com/zitadel/oidc) | OIDC provider + consumer | Apache 2.0 |
| [go-codec](https://github.com/jeremyhahn/go-codec) | Policy serialization | Apache 2.0 |
| [go-webauthn](https://github.com/go-webauthn/webauthn) | WebAuthn/FIDO2 | BSD 3-Clause |

## Comparison with AWS IAM/Cognito

| Aspect | AWS IAM/Cognito | QuicBaaS IAM |
|--------|-----------------|--------------|
| Deployment | Cloud-only | Self-hosted |
| Auth Latency | Network round-trip | Local (~2-5μs) |
| Policy Syntax | JSON (verbose) | YAML (clean) |
| Multi-tenancy | Via Organizations | Native |
| Pricing | Per MAU | Free (self-hosted) |
| Vendor Lock-in | AWS only | None |
