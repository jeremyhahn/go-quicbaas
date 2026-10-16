/**
 * QuicBaaS JavaScript SDK
 *
 * A complete JavaScript client library for QuicBaaS - a high-performance,
 * multi-faceted, distributed key-value database built on multi-Raft consensus.
 *
 * @module @quicbaas/client
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 *
 * @example
 * // ESM import
 * import { QuicBaaS } from '@quicbaas/client';
 *
 * // Create client and authenticate
 * const db = new QuicBaaS('https://localhost:8080');
 * await db.auth().login('admin', 'password');
 *
 * // Key-Value operations
 * await db.kv().put('users', 'user1', { name: 'John', email: 'john@example.com' });
 * const user = await db.kv().get('users', 'user1');
 *
 * // Vector similarity search
 * const results = await db.vector().searchSimilar('embeddings', embedding, { k: 10 });
 *
 * // Time-series data
 * await db.timeSeries().writePoint('temperature', 72.5, new Date(), { location: 'office' });
 *
 * // Secrets management
 * await db.secrets().storeSecret('/app/db/password', 'secret123');
 */

// Export the main QuicBaaS client
export { QuicBaaS, default } from './quicbaas.js';

// Export transport
export { HttpTransport } from './transport/http.js';

// Export all errors
export * from './errors/index.js';

// Export all clients
export { default as KVClient, ConsistencyLevel, WriteMode, EventType } from './clients/kv.js';
export { default as BatchClient } from './clients/batch.js';
export { default as TransactionClient, CompareTarget, CompareResult, TxnOpType } from './clients/transaction.js';
export { default as QueryClient, Operator, eq, neq, gt, gte, lt, lte, between, prefix } from './clients/query.js';
export { default as AuthClient } from './clients/auth.js';
export { default as SealClient } from './clients/seal.js';
export { default as ClusterClient, NodeState, RebalanceStrategy, RebalanceStatus } from './clients/cluster.js';
export { default as HealthClient, HealthStatus } from './clients/health.js';
export { default as OperationsClient, MigrationStatus, UpgradeStatus, ReplaceNodeStatus } from './clients/operations.js';
export { default as KMSClient, KeyAlgorithm, KeyStatus, KeyUsage } from './clients/kms.js';
export { default as SecretsClient, SecretStatus, RotationInterval } from './clients/secrets.js';
export { default as CryptoClient, HashAlgorithm, SignatureAlgorithm, KeyDerivationFunction } from './clients/crypto.js';
export { default as DynamicSecretsClient, CredentialType } from './clients/dynamicSecrets.js';
export { default as CertificateClient, CertificateUsage, KeyAlgorithm as CertKeyAlgorithm, TLSVersion } from './clients/certificate.js';
export { default as ClusterDEKClient, NodeDEKSyncStatus, ReencryptionState, RecoveryMethod } from './clients/clusterDek.js';
export { default as IAMClient, TenantStatus, UserStatus, ExportFormat, AuditEventType } from './clients/iam.js';
export { default as VectorClient, DistanceMetric, IndexType } from './clients/vector.js';
export { default as TimeSeriesClient, Aggregation, Interval } from './clients/timeSeries.js';
export { default as BackupClient, BackupType, BackupStatus, RestoreStatus } from './clients/backup.js';
export { default as ObjectStorageClient, ReplicationMode, ObjectStatus, StorageTier } from './clients/objectStorage.js';
export { default as ObjectStorageAdminClient, BackendType, AdminStorageTier, GarbageCollectionStatus } from './clients/objectStorageAdmin.js';
export { default as ObjectTransferClient } from './clients/objectTransfer.js';
export { default as ParameterStoreClient, ParameterType } from './clients/parameterStore.js';
export { default as EmbeddingClient, Provider as EmbeddingProvider, InputType, Status as EmbeddingStatus } from './clients/embedding.js';
export { default as OllamaClient } from './clients/ollama.js';
export { default as CacheClient } from './clients/cache.js';
export { default as MessagingClient, MessageCodec, MessagingEventType } from './clients/messaging.js';
export { default as SchedulerClient, JobType, JobStatus, OwnerType } from './clients/scheduler.js';

// Export validation utilities
export * from './utils/validation.js';
