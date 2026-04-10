# Security Architecture

## Barrier (`pkg/seal/` + QuicRaft's `crypto.Barrier`)
- QuicBaaS's SealManager wraps QuicRaft's crypto.Barrier
- QuicRaft Barrier: AES-256-GCM encryption with epoch-based key rotation, HKDF-SHA256 derivation
- QuicBaaS Seal: Adds Shamir secret sharing, TPM backing, Raft-coordinated auto-unseal
- Modes: Manual (operator shares) or Auto (Raft-coordinated)

## Auth (`pkg/auth/`)
- JWT-based authentication
- Password auth (integration testing)
- Bootstrap admin creation on startup

## IAM (`pkg/iam/`)
- RBAC/ABAC policy engine
- Tenant isolation (strict mode)
- OIDC provider
- WebAuthn support

## KMS (`pkg/kms/`)
- Software backend (default)
- Auto-generate encryption key on startup
- Dedicated Raft shard (1000)

## Secrets (`pkg/secrets/`)
- Secrets engine with encryption
- Dedicated Raft shard (1001)
- Default encryption key from KMS

## TLS
- TLS 1.3 enforced (FIPS minimum)
- Auto-generated CA certs per node
- Mutual TLS optional for Raft transport
