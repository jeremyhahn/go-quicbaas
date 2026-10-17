# QuicBaaS JavaScript SDK

A complete JavaScript client library for QuicBaaS - a high-performance, multi-faceted, distributed key-value database built on multi-Raft consensus.

## Installation

```bash
npm install @quicbaas/client
```

## Quick Start

```javascript
import { QuicBaaS } from '@quicbaas/client';

// Create a client
const db = new QuicBaaS('https://localhost:8080');

// Authenticate
await db.auth().login('admin', 'password');

// Key-Value operations
await db.kv().put('users', 'user1', { name: 'John', email: 'john@example.com' });
const user = await db.kv().get('users', 'user1');
console.log(user.value); // { name: 'John', email: 'john@example.com' }

// Batch operations
await db.batch().batchPut([
  { entityType: 'users', key: 'user2', value: { name: 'Jane' } },
  { entityType: 'users', key: 'user3', value: { name: 'Bob' } },
]);
```

## Features

The SDK provides access to all QuicBaaS services:

### Core Data Operations
- **KV** - Key-value CRUD operations with consistency levels
- **Batch** - Atomic batch operations (put, get, delete)
- **Transaction** - Compare-and-swap transactions
- **Query** - Secondary index queries

### Cluster Operations
- **Cluster** - Cluster status, rebalancing, shard management
- **Health** - Health checks (liveness, readiness)
- **Operations** - Data migration, node replacement/upgrade

### Security
- **Auth** - Authentication (login, logout, token refresh)
- **Seal** - Database seal/unseal operations

### Cryptographic Services
- **KMS** - Key management (generate, rotate, revoke)
- **Secrets** - Secret storage with versioning and rotation
- **Crypto** - Encryption, signing, hashing
- **DynamicSecrets** - Lease-based credential generation
- **Certificate** - PKI operations (issue, sign CSR, revoke)
- **ClusterDEK** - Cluster data encryption key management

### Identity Management
- **IAM** - Multi-tenant identity and access management

### Specialized Data
- **Vector** - Vector embeddings and similarity search
- **TimeSeries** - Time-series data with aggregation
- **Backup** - Backup and restore operations

### Storage
- **ObjectStorage** - Object storage with replication and archival
- **ParameterStore** - Hierarchical parameter management

### AI/ML Services
- **Embedding** - Multi-provider embedding generation
- **Ollama** - Local LLM integration

### Cache Management
- **Cache** - Cache statistics and configuration

## Usage Examples

### Vector Similarity Search

```javascript
// Create a vector namespace
await db.vector().createNamespace('embeddings', 1536, {
  distanceMetric: 'COSINE',
  indexType: 'HNSW',
});

// Upsert vectors
await db.vector().upsertVector('embeddings', 'doc1', embedding, {
  metadata: { title: 'Document 1', category: 'tech' },
});

// Search similar vectors
const results = await db.vector().searchSimilar('embeddings', queryEmbedding, {
  k: 10,
  filter: { category: 'tech' },
  includeMetadata: true,
});
```

### Time-Series Data

```javascript
// Write data points
await db.timeSeries().writePoint('temperature', 72.5, new Date(), {
  location: 'office',
  sensor: 'sensor-1',
});

// Query with aggregation
const data = await db.timeSeries().query({
  metric: 'temperature',
  start: new Date(Date.now() - 3600000), // 1 hour ago
  end: new Date(),
  aggregation: 'AVG',
  interval: 'MINUTE',
  tags: { location: 'office' },
});
```

### Secrets Management

```javascript
// Store a secret
await db.secrets().storeSecret('/app/db/password', 'super-secret', {
  description: 'Database password',
  tags: { env: 'production' },
});

// Retrieve a secret
const secret = await db.secrets().getSecret('/app/db/password');
console.log(secret.value);

// Set rotation policy
await db.secrets().setRotationPolicy('/app/db/password', {
  enabled: true,
  interval: 'MONTHLY',
});
```

### Transactions

```javascript
import { TransactionClient } from '@quicbaas/client';

const txn = db.transaction();

// Atomic compare-and-swap
const result = await txn.transaction({
  conditions: [
    txn.compare('accounts', 'account1', 5), // Check revision
  ],
  success: [
    txn.putOp('accounts', 'account1', { balance: 100 }),
    txn.putOp('audit', 'txn-123', { action: 'debit', amount: 50 }),
  ],
  failure: [
    txn.putOp('errors', 'txn-123', { error: 'Concurrent modification' }),
  ],
});

console.log(result.succeeded); // true if conditions passed
```

### IAM Role Assignment

`db.iam()` assigns and revokes a tenant user's roles. Both calls resolve to
nothing on success and reject with the SDK's error types (for example
`NotFoundError`) when the server refuses; authorization is enforced server-side.

```javascript
// Assign a role to a user: POST /api/v1/tenants/{tenant_id}/users/{user_id}/roles
await db.iam().assignRole('tenant-1', 'user-1', 'role-1');

// Revoke it: DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role}
await db.iam().revokeRole('tenant-1', 'user-1', 'role-1');
```

## Browser Usage

For direct browser usage via script tag:

```html
<script src="https://unpkg.com/@quicbaas/client/dist/quicbaas.browser.min.js"></script>
<script>
  const db = new QuicBaaS.QuicBaaS('https://api.example.com');
  // Use the SDK...
</script>
```

## API Reference

See the [API Documentation](./docs/api.md) for complete reference.

## License

AGPL-3.0-or-later

Copyright (c) 2025 Jeremy Hahn, Automate The Things, LLC
