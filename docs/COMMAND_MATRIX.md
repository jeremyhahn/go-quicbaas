# QuicBaaS Command Coverage Matrix

This document provides a comprehensive overview of all CLI commands in QuicBaaS, their protocol support, and test coverage status.

## Summary

| Metric | Count |
|--------|-------|
| Total CLI Commands | 308 |
| QUIC Protocol Operations | 150+ |
| REST API Endpoints | 180+ |
| MCP Tools | 270+ |
| Unix Socket Operations | 35+ |

## Protocol Legend

| Symbol | Meaning |
|--------|---------|
| Y | Full support |
| P | Partial support |
| - | Not implemented |
| N/A | Not applicable |

## Test Coverage Legend

| Symbol | Meaning |
|--------|---------|
| U | Unit tests |
| I | Integration tests |
| B | Both unit and integration |
| - | No tests |

---

## Auth Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `auth login` | Y | Y | Y | - | Y | B | Complete |
| `auth logout` | Y | Y | Y | - | Y | B | Complete |
| `auth status` | Y | Y | Y | - | Y | B | Complete |

## Backup Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `backup create` | Y | Y | Y | - | Y | B | Complete |
| `backup list` | Y | Y | Y | - | Y | B | Complete |
| `backup restore` | Y | Y | Y | - | Y | B | Complete |
| `backup delete` | Y | Y | Y | - | Y | B | Complete |
| `backup verify` | Y | Y | Y | - | Y | B | Complete |
| `backup status` | Y | Y | Y | - | Y | B | Complete |

## Cache Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `cache stats` | Y | Y | Y | - | Y | B | Complete |
| `cache clear` | Y | Y | Y | - | Y | B | Complete |
| `cache config` | Y | Y | Y | - | Y | B | Complete |
| `cache enable` | Y | Y | Y | - | Y | U | Complete |
| `cache disable` | Y | Y | Y | - | Y | U | Complete |
| `cache warm` | Y | Y | Y | - | Y | U | Complete |

## Cluster Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `cluster status` | Y | Y | Y | - | Y | B | Complete |

## Crypto Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `crypto encrypt` | Y | Y | Y | - | Y | B | Complete |
| `crypto decrypt` | Y | Y | Y | - | Y | B | Complete |
| `crypto rewrap` | Y | Y | Y | - | Y | B | Complete |
| `crypto sign` | Y | Y | Y | - | Y | B | Complete |
| `crypto verify` | Y | Y | Y | - | Y | B | Complete |
| `crypto hash` | Y | Y | Y | - | Y | B | Complete |
| `crypto random` | Y | Y | Y | - | Y | B | Complete |
| `crypto datakey` | Y | Y | Y | - | Y | B | Complete |

## Data Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `data put` | Y | Y | Y | - | Y | B | Complete |
| `data get` | Y | Y | Y | - | Y | B | Complete |
| `data delete` | Y | Y | Y | - | Y | B | Complete |
| `data list` | Y | Y | Y | - | Y | B | Complete |
| `data query` | Y | Y | Y | - | Y | B | Complete |

## Embedding Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `embedding generate` | Y | Y | Y | - | Y | B | Complete |
| `embedding batch` | Y | Y | Y | - | Y | B | Complete |
| `embedding providers` | Y | Y | Y | - | Y | B | Complete |
| `embedding provider info` | Y | Y | Y | - | Y | B | Complete |
| `embedding provider set-default` | Y | Y | Y | - | Y | U | Complete |
| `embedding cache stats` | Y | Y | Y | - | Y | B | Complete |
| `embedding cache clear` | Y | Y | Y | - | Y | B | Complete |

## IAM Module

### IAM Admin

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam admin assignment create` | Y | Y | Y | - | Y | B | Complete |
| `iam admin assignment get` | Y | Y | Y | - | Y | B | Complete |
| `iam admin assignment update` | Y | Y | Y | - | Y | B | Complete |
| `iam admin assignment delete` | Y | Y | Y | - | Y | B | Complete |
| `iam admin assignment list` | Y | Y | Y | - | Y | B | Complete |
| `iam admin check-permission` | Y | Y | Y | - | Y | B | Complete |
| `iam admin impersonate start` | Y | Y | Y | - | Y | U | Complete |
| `iam admin impersonate end` | Y | Y | Y | - | Y | U | Complete |
| `iam admin impersonate list` | Y | Y | Y | - | Y | U | Complete |

### IAM App

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam app register` | Y | Y | Y | - | Y | B | Complete |
| `iam app get` | Y | Y | Y | - | Y | B | Complete |
| `iam app update` | Y | Y | Y | - | Y | B | Complete |
| `iam app delete` | Y | Y | Y | - | Y | B | Complete |
| `iam app list` | Y | Y | Y | - | Y | B | Complete |
| `iam app rotate-secret` | Y | Y | Y | - | Y | U | Complete |

### IAM Audit

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam audit query` | Y | Y | Y | - | Y | U | Complete |
| `iam audit get` | Y | Y | Y | - | Y | U | Complete |

### IAM Federation

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam federation provider create` | Y | Y | Y | - | Y | B | Complete |
| `iam federation provider get` | Y | Y | Y | - | Y | B | Complete |
| `iam federation provider update` | Y | Y | Y | - | Y | B | Complete |
| `iam federation provider delete` | Y | Y | Y | - | Y | B | Complete |
| `iam federation provider list` | Y | Y | Y | - | Y | B | Complete |
| `iam federation identity link` | Y | Y | Y | - | Y | U | Complete |
| `iam federation identity unlink` | Y | Y | Y | - | Y | U | Complete |
| `iam federation identity list` | Y | Y | Y | - | Y | U | Complete |

### IAM GDPR

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam gdpr consent grant` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr consent withdraw` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr consent get` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr consent list` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr export request` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr export status` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr deletion request` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr deletion cancel` | Y | Y | Y | - | Y | B | Complete |
| `iam gdpr deletion status` | Y | Y | Y | - | Y | B | Complete |

### IAM Group

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam group create` | Y | Y | Y | - | Y | B | Complete |
| `iam group get` | Y | Y | Y | - | Y | B | Complete |
| `iam group update` | Y | Y | Y | - | Y | B | Complete |
| `iam group delete` | Y | Y | Y | - | Y | B | Complete |
| `iam group list` | Y | Y | Y | - | Y | B | Complete |
| `iam group add-member` | Y | Y | Y | - | Y | B | Complete |
| `iam group remove-member` | Y | Y | Y | - | Y | B | Complete |
| `iam group members` | Y | Y | Y | - | Y | B | Complete |

### IAM MFA

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam mfa status` | Y | Y | Y | - | Y | B | Complete |
| `iam mfa enroll` | Y | Y | Y | - | Y | B | Complete |
| `iam mfa verify` | Y | Y | Y | - | Y | B | Complete |
| `iam mfa disable` | Y | Y | Y | - | Y | U | Complete |
| `iam mfa backup-codes status` | Y | Y | Y | - | Y | U | Complete |
| `iam mfa backup-codes generate` | Y | Y | Y | - | Y | U | Complete |

### IAM PEP (Policy Enforcement Point)

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam pep check` | Y | Y | Y | - | Y | B | Complete |
| `iam pep check-batch` | Y | Y | Y | - | Y | B | Complete |

### IAM Policy

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam policy apply` | Y | Y | Y | - | Y | B | Complete |
| `iam policy get` | Y | Y | Y | - | Y | B | Complete |
| `iam policy delete` | Y | Y | Y | - | Y | B | Complete |
| `iam policy list` | Y | Y | Y | - | Y | B | Complete |
| `iam policy validate` | Y | Y | Y | - | Y | U | Complete |
| `iam policy test` | Y | Y | Y | - | Y | U | Complete |

### IAM Role

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam role create` | Y | Y | Y | - | Y | B | Complete |
| `iam role get` | Y | Y | Y | - | Y | B | Complete |
| `iam role update` | Y | Y | Y | - | Y | B | Complete |
| `iam role delete` | Y | Y | Y | - | Y | B | Complete |
| `iam role list` | Y | Y | Y | - | Y | B | Complete |
| `iam role assign` | Y | Y | Y | - | Y | B | Complete |
| `iam role revoke` | Y | Y | Y | - | Y | B | Complete |

### IAM Session

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam session list` | Y | Y | Y | - | Y | U | Complete |
| `iam session get` | Y | Y | Y | - | Y | U | Complete |
| `iam session revoke` | Y | Y | Y | - | Y | U | Complete |
| `iam session revoke-all` | Y | Y | Y | - | Y | U | Complete |

### IAM Tenant

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam tenant create` | Y | Y | Y | - | Y | B | Complete |
| `iam tenant get` | Y | Y | Y | - | Y | B | Complete |
| `iam tenant update` | Y | Y | Y | - | Y | B | Complete |
| `iam tenant delete` | Y | Y | Y | - | Y | B | Complete |
| `iam tenant list` | Y | Y | Y | - | Y | B | Complete |

### IAM User

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `iam user create` | Y | Y | Y | - | Y | B | Complete |
| `iam user get` | Y | Y | Y | - | Y | B | Complete |
| `iam user update` | Y | Y | Y | - | Y | B | Complete |
| `iam user delete` | Y | Y | Y | - | Y | B | Complete |
| `iam user list` | Y | Y | Y | - | Y | B | Complete |
| `iam user role assign` | Y | Y | Y | - | Y | B | Complete |
| `iam user role revoke` | Y | Y | Y | - | Y | B | Complete |

## KMS Module

### KMS Key Management

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `kms key generate` | Y | Y | Y | - | Y | B | Complete |
| `kms key get` | Y | Y | Y | - | Y | B | Complete |
| `kms key list` | Y | Y | Y | - | Y | B | Complete |
| `kms key rotate` | Y | Y | Y | - | Y | B | Complete |
| `kms key delete` | Y | Y | Y | - | Y | B | Complete |
| `kms key import` | Y | Y | Y | - | Y | B | Complete |
| `kms key enable` | Y | Y | Y | - | Y | B | Complete |
| `kms key disable` | Y | Y | Y | - | Y | B | Complete |
| `kms key versions` | Y | Y | Y | - | Y | B | Complete |
| `kms key export` | Y | Y | Y | - | Y | B | Complete |
| `kms key unwrap` | Y | Y | Y | - | Y | B | Complete |
| `kms key set-safety` | Y | Y | Y | - | Y | B | Complete |
| `kms encrypt` | Y | Y | Y | - | Y | B | Complete |
| `kms decrypt` | Y | Y | Y | - | Y | B | Complete |

### KMS DEK Management

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `kms dek generate` | Y | Y | Y | - | Y | B | Complete |
| `kms dek get` | Y | Y | Y | - | Y | B | Complete |
| `kms dek list` | Y | Y | Y | - | Y | B | Complete |
| `kms dek rotate` | Y | Y | Y | - | Y | B | Complete |
| `kms dek delete` | Y | Y | Y | - | Y | B | Complete |
| `kms dek status` | Y | Y | Y | - | Y | B | Complete |
| `kms dek set-default` | Y | Y | Y | - | Y | B | Complete |
| `kms dek set-safety` | Y | Y | Y | - | Y | B | Complete |

### KMS Backend

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `kms backend status` | Y | Y | Y | - | Y | B | Complete |
| `kms backend test` | Y | Y | Y | - | Y | B | Complete |

### KMS Safety

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `kms safety check` | Y | Y | Y | - | Y | U | Complete |
| `kms safety report` | Y | Y | Y | - | Y | U | Complete |

## Migrate Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `migrate start` | Y | Y | Y | - | Y | B | Complete |
| `migrate status` | Y | Y | Y | - | Y | B | Complete |
| `migrate cancel` | Y | Y | Y | - | Y | U | Complete |

## Node Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `node add` | Y | Y | Y | - | Y | B | Complete |
| `node remove` | Y | Y | Y | - | Y | B | Complete |
| `node replace` | Y | Y | Y | - | Y | B | Complete |
| `node list` | Y | Y | Y | - | Y | B | Complete |

## Object Storage Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `object put` | Y | Y | Y | Y | Y | B | Complete |
| `object get` | Y | Y | Y | Y | Y | B | Complete |
| `object delete` | Y | Y | Y | Y | Y | B | Complete |
| `object head` | Y | Y | Y | Y | Y | B | Complete |
| `object list` | Y | Y | Y | Y | Y | B | Complete |
| `object list-by-tag` | Y | Y | Y | Y | Y | B | Complete |
| `object list-by-tenant` | Y | Y | Y | Y | Y | B | Complete |
| `object copy` | Y | Y | Y | Y | Y | B | Complete |
| `object archive` | Y | Y | Y | Y | Y | U | Complete |
| `object restore` | Y | Y | Y | Y | Y | U | Complete |
| `object verify` | Y | Y | Y | Y | Y | U | Complete |
| `object replicate` | Y | Y | Y | Y | Y | U | Complete |
| `object migrate` | Y | Y | Y | Y | Y | U | Complete |
| `object metadata get` | Y | Y | Y | Y | Y | B | Complete |
| `object metadata update` | Y | Y | Y | Y | Y | B | Complete |
| `object stats` | Y | Y | Y | Y | Y | U | Complete |
| `object lifecycle set` | Y | Y | Y | Y | Y | U | Complete |
| `object lifecycle get` | Y | Y | Y | Y | Y | U | Complete |

## Ollama Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `ollama health` | Y | Y | Y | - | Y | B | Complete |
| `ollama models` | Y | Y | Y | - | Y | B | Complete |
| `ollama model info` | Y | Y | Y | - | Y | B | Complete |
| `ollama embed` | Y | Y | Y | - | Y | B | Complete |
| `ollama embed-batch` | Y | Y | Y | - | Y | B | Complete |

## RAG Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `rag query` | Y | Y | Y | - | Y | B | Complete |
| `rag context` | Y | Y | Y | - | Y | B | Complete |

## Rebalance Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `rebalance start` | Y | Y | Y | - | Y | B | Complete |
| `rebalance status` | Y | Y | Y | - | Y | B | Complete |
| `rebalance plan` | Y | Y | Y | - | Y | U | Complete |

## Role Module (Legacy)

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `role create` | Y | Y | Y | - | Y | B | Complete |
| `role delete` | Y | Y | Y | - | Y | B | Complete |
| `role list` | Y | Y | Y | - | Y | B | Complete |
| `role show` | Y | Y | Y | - | Y | U | Complete |
| `role assign` | Y | Y | Y | - | Y | B | Complete |
| `role unassign` | Y | Y | Y | - | Y | U | Complete |
| `role permissions` | Y | Y | Y | - | Y | U | Complete |
| `role grant` | Y | Y | Y | - | Y | U | Complete |
| `role revoke` | Y | Y | Y | - | Y | U | Complete |
| `role list-permissions` | Y | Y | Y | - | Y | U | Complete |

## Scheduler Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `scheduler create` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler list` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler get` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler delete` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler trigger` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler pause` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler resume` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler cancel` | Y | Y | Y | Y | Y | B | Complete |
| `scheduler history` | Y | Y | Y | Y | Y | U | Complete |
| `scheduler status` | Y | Y | Y | Y | Y | B | Complete |

## Seal Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `seal status` | Y | Y | Y | - | Y | B | Complete |
| `seal init` | Y | Y | Y | - | Y | B | Complete |
| `seal unseal` | Y | Y | Y | - | Y | B | Complete |
| `seal seal` | Y | Y | Y | - | Y | B | Complete |
| `seal rekey` | Y | Y | Y | - | Y | B | Complete |

## Secrets Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `secrets put` | Y | Y | Y | - | Y | B | Complete |
| `secrets get` | Y | Y | Y | - | Y | B | Complete |
| `secrets list` | Y | Y | Y | - | Y | B | Complete |
| `secrets delete` | Y | Y | Y | - | Y | B | Complete |
| `secrets destroy` | Y | Y | Y | - | Y | U | Complete |
| `secrets versions` | Y | Y | Y | - | Y | B | Complete |
| `secrets rollback` | Y | Y | Y | - | Y | U | Complete |
| `secrets rotate` | Y | Y | Y | - | Y | U | Complete |
| `secrets enable` | Y | Y | Y | - | Y | U | Complete |
| `secrets disable` | Y | Y | Y | - | Y | U | Complete |
| `secrets get-prefix` | Y | Y | Y | - | Y | B | Complete |
| `secrets watch` | Y | Y | Y | - | Y | U | Complete |
| `secrets fetch` | Y | Y | Y | - | Y | U | Complete |

## Time Series Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `timeseries write` | Y | Y | Y | - | Y | B | Complete |
| `timeseries write-batch` | Y | Y | Y | - | Y | B | Complete |
| `timeseries query` | Y | Y | Y | - | Y | B | Complete |
| `timeseries delete` | Y | Y | Y | - | Y | U | Complete |
| `timeseries get-retention` | Y | Y | Y | - | Y | U | Complete |
| `timeseries set-retention` | Y | Y | Y | - | Y | U | Complete |
| `timeseries list-metrics` | Y | Y | Y | - | Y | U | Complete |

## Transaction Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `txn compare` | Y | Y | Y | - | Y | B | Complete |
| `txn success` | Y | Y | Y | - | Y | B | Complete |
| `txn failure` | Y | Y | Y | - | Y | B | Complete |
| `txn execute` | Y | Y | Y | - | Y | B | Complete |
| `txn reset` | Y | Y | Y | - | Y | U | Complete |
| `txn show` | Y | Y | Y | - | Y | U | Complete |

## Upgrade Module

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `upgrade start` | Y | Y | Y | - | Y | U | Complete |
| `upgrade status` | Y | Y | Y | - | Y | U | Complete |

## User Module (Legacy)

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `user create` | Y | Y | Y | - | Y | B | Complete |
| `user delete` | Y | Y | Y | - | Y | B | Complete |
| `user list` | Y | Y | Y | - | Y | B | Complete |
| `user show` | Y | Y | Y | - | Y | U | Complete |
| `user update` | Y | Y | Y | - | Y | U | Complete |

## Vector Module

### Vector Namespace

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `vector namespace create` | Y | Y | Y | - | Y | B | Complete |
| `vector namespace delete` | Y | Y | Y | - | Y | B | Complete |
| `vector namespace list` | Y | Y | Y | - | Y | B | Complete |
| `vector namespace info` | Y | Y | Y | - | Y | B | Complete |

### Vector Index

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `vector index upsert` | Y | Y | Y | - | Y | B | Complete |
| `vector index batch-upsert` | Y | Y | Y | - | Y | B | Complete |
| `vector index get` | Y | Y | Y | - | Y | B | Complete |
| `vector index delete` | Y | Y | Y | - | Y | B | Complete |
| `vector index batch-delete` | Y | Y | Y | - | Y | B | Complete |
| `vector index list` | Y | Y | Y | - | Y | B | Complete |
| `vector index update-metadata` | Y | Y | Y | - | Y | U | Complete |

### Vector Search

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `vector search similar` | Y | Y | Y | - | Y | B | Complete |
| `vector search by-id` | Y | Y | Y | - | Y | B | Complete |
| `vector search hybrid` | Y | Y | Y | - | Y | B | Complete |
| `vector search by-metadata` | Y | Y | Y | - | Y | B | Complete |

### Vector Admin

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `vector admin rebuild` | Y | Y | Y | - | Y | B | Complete |
| `vector admin progress` | Y | Y | Y | - | Y | U | Complete |
| `vector admin stats` | Y | Y | Y | - | Y | B | Complete |
| `vector admin export` | Y | Y | Y | - | Y | U | Complete |
| `vector admin import` | Y | Y | Y | - | Y | U | Complete |
| `vector admin compact` | Y | Y | Y | - | Y | U | Complete |

## Version Command

| Command | gRPC | REST | QUIC | Unix | MCP | Tests | Status |
|---------|------|------|------|------|-----|-------|--------|
| `version` | N/A | N/A | N/A | N/A | N/A | U | Complete |

---

## Protocol Support Summary by Module

| Module | gRPC | REST | QUIC | Unix | MCP |
|--------|------|------|------|------|-----|
| Auth | 100% | 100% | 100% | - | 100% |
| Backup | 100% | 100% | 100% | - | 100% |
| Cache | 100% | 100% | 100% | - | 100% |
| Cluster | 100% | 100% | 100% | - | 100% |
| Crypto | 100% | 100% | 100% | - | 100% |
| Data | 100% | 100% | 100% | - | 100% |
| Embedding | 100% | 100% | 100% | - | 100% |
| IAM | 100% | 100% | 100% | - | 100% |
| KMS | 100% | 100% | 100% | - | 100% |
| Migrate | 100% | 100% | 100% | - | 100% |
| Node | 100% | 100% | 100% | - | 100% |
| Object Storage | 100% | 100% | 100% | 100% | 100% |
| Ollama | 100% | 100% | 100% | - | 100% |
| RAG | 100% | 100% | 100% | - | 100% |
| Rebalance | 100% | 100% | 100% | - | 100% |
| Role | 100% | 100% | 100% | - | 100% |
| Scheduler | 100% | 100% | 100% | 100% | 100% |
| Seal | 100% | 100% | 100% | - | 100% |
| Secrets | 100% | 100% | 100% | - | 100% |
| Time Series | 100% | 100% | 100% | - | 100% |
| Transaction | 100% | 100% | 100% | - | 100% |
| Upgrade | 100% | 100% | 100% | - | 100% |
| User | 100% | 100% | 100% | - | 100% |
| Vector | 100% | 100% | 100% | - | 100% |

## Test Coverage Summary

| Category | Unit Tests | Integration Tests |
|----------|-----------|-------------------|
| CLI Commands | 308/308 | 250+ |
| gRPC Client | 100% | 100% |
| REST Handlers | 100% | 100% |
| QUIC Protocol | 100% | 100% |
| MCP Tools | 100% | 100% |
| Unix Socket | 100% | 100% |

## Integration Test Locations

| Module | Test Directory |
|--------|---------------|
| API (gRPC) | `test/integration/api/grpc/` |
| API (REST) | `test/integration/api/rest/` |
| API (QUIC) | `test/integration/api/quic/` |
| CLI | `test/integration/cli/` |
| Auth | `test/integration/auth/` |
| Backup | `test/integration/backup/` |
| Batch | `test/integration/batch/` |
| Cache | `test/integration/cache/` |
| Cluster | `test/integration/cluster/` |
| DAO | `test/integration/dao/` |
| DEK | `test/integration/dek/` |
| Embedding | `test/integration/embedding/` |
| IAM | `test/integration/iam/` |
| KMS | `test/integration/kms/` |
| KVStore | `test/integration/kvstore/` |
| MCP | `test/integration/mcp/` |
| Object Store | `test/integration/objectstore/` |
| Parameter Store | `test/integration/parameter_store/` |
| Raft | `test/integration/raft/` |
| RAG | `test/integration/rag/` |
| Scheduler | `test/integration/scheduler/` |
| Seal | `test/integration/seal/` |
| Secrets | `test/integration/secrets/` |
| Time Series | `test/integration/timeseries/` |
| Transactions | `test/integration/transactions/` |
| Vector | `test/integration/vector/` |

## Notes

1. **Unix Socket Support**: Currently implemented for Object Storage and Scheduler modules. These use high-performance binary protocols for inter-node communication.

2. **MCP (Model Context Protocol)**: Full tool support for AI agent integration. All commands are available as MCP tools with proper input validation and JSON responses.

3. **QUIC Protocol**: Uses binary opcodes for high-performance communication. All 150+ operations are implemented with full feature parity.

4. **REST API**: Complete OpenAPI documentation available at `/docs/openapi.yaml`. Supports both JSON and protocol buffer encoding.

5. **gRPC**: Default protocol for CLI communication. Supports Unix socket, TCP, and TLS connections.
