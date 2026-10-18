/**
 * QuicBaaS JavaScript SDK - Main Client
 *
 * @module @quicbaas/client
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { HttpTransport } from './transport/http.js';
import { ValidationError } from './errors/index.js';

// Import all client classes
import KVClient, { ConsistencyLevel, WriteMode, EventType } from './clients/kv.js';
import BatchClient from './clients/batch.js';
import TransactionClient, { CompareTarget, CompareResult, TxnOpType } from './clients/transaction.js';
import QueryClient, { Operator } from './clients/query.js';
import AuthClient from './clients/auth.js';
import ClusterClient, { NodeState, RebalanceStrategy, RebalanceStatus } from './clients/cluster.js';
import HealthClient, { HealthStatus } from './clients/health.js';
import OperationsClient, { MigrationStatus, UpgradeStatus, ReplaceNodeStatus } from './clients/operations.js';
import KMSClient, { KeyAlgorithm, KeyStatus, KeyUsage } from './clients/kms.js';
import SecretsClient, { SecretStatus, RotationInterval } from './clients/secrets.js';
import CryptoClient, { HashAlgorithm, SignatureAlgorithm, KeyDerivationFunction } from './clients/crypto.js';
import DynamicSecretsClient, { CredentialType } from './clients/dynamicSecrets.js';
import CertificateClient, { CertificateUsage, KeyAlgorithm as CertKeyAlgorithm, TLSVersion } from './clients/certificate.js';
import IAMClient, { TenantStatus, UserStatus, ExportFormat, AuditEventType } from './clients/iam.js';
import VectorClient, { DistanceMetric, IndexType } from './clients/vector.js';
import TimeSeriesClient, { Aggregation, Interval } from './clients/timeSeries.js';
import BackupClient, { BackupType, BackupStatus, RestoreStatus } from './clients/backup.js';
import ObjectStorageClient, { ReplicationMode, ObjectStatus, StorageTier } from './clients/objectStorage.js';
import ObjectStorageAdminClient, { BackendType, AdminStorageTier, GarbageCollectionStatus } from './clients/objectStorageAdmin.js';
import ObjectTransferClient from './clients/objectTransfer.js';
import ParameterStoreClient, { ParameterType } from './clients/parameterStore.js';
import EmbeddingClient, { Provider as EmbeddingProvider, InputType, Status as EmbeddingStatus } from './clients/embedding.js';
import OllamaClient from './clients/ollama.js';
import CacheClient from './clients/cache.js';
import MessagingClient, { MessageCodec, MessagingEventType } from './clients/messaging.js';
import SchedulerClient, { JobType, JobStatus, OwnerType } from './clients/scheduler.js';

/**
 * Default configuration options for the QuicBaaS client.
 */
const DEFAULT_OPTIONS = {
  timeout: 30000,
  retries: 3,
  retryDelay: 1000,
  retryBackoff: 2,
};

/**
 * QuicBaaS Client - Main entry point for the JavaScript SDK.
 *
 * Provides access to all QuicBaaS services through a unified interface.
 * The client uses lazy initialization for sub-clients to minimize overhead.
 *
 * @example
 * // Create a client
 * const db = new QuicBaaS('https://localhost:8080', {
 *   timeout: 30000,
 * });
 *
 * // Authenticate
 * await db.auth().login('admin', 'password');
 *
 * // Use KV operations
 * await db.kv().put('users', 'user1', { name: 'John' });
 * const user = await db.kv().get('users', 'user1');
 *
 * // Use vector search
 * const results = await db.vector().searchSimilar('embeddings', [0.1, 0.2, ...], { k: 10 });
 */
export class QuicBaaS {
  #transport;
  #options;

  // Cached client instances (lazy initialization)
  #kvClient = null;
  #batchClient = null;
  #transactionClient = null;
  #queryClient = null;
  #authClient = null;
  #clusterClient = null;
  #healthClient = null;
  #operationsClient = null;
  #kmsClient = null;
  #secretsClient = null;
  #cryptoClient = null;
  #dynamicSecretsClient = null;
  #certificateClient = null;
  #iamClient = null;
  #vectorClient = null;
  #timeSeriesClient = null;
  #backupClient = null;
  #objectStorageClient = null;
  #objectStorageAdminClient = null;
  #objectTransferClient = null;
  #parameterStoreClient = null;
  #embeddingClient = null;
  #ollamaClient = null;
  #cacheClient = null;
  #messagingClient = null;
  #schedulerClient = null;

  /**
   * Create a new QuicBaaS client.
   *
   * @param {string} baseUrl - Base URL for the QuicBaaS API (e.g., 'https://localhost:8080')
   * @param {Object} [options] - Client configuration options
   * @param {number} [options.timeout=30000] - Request timeout in milliseconds
   * @param {number} [options.retries=3] - Number of retry attempts
   * @param {number} [options.retryDelay=1000] - Initial retry delay in milliseconds
   * @param {number} [options.retryBackoff=2] - Retry backoff multiplier
   * @param {Object} [options.headers] - Default headers to include in requests
   * @throws {ValidationError} If baseUrl is not provided
   */
  constructor(baseUrl, options = {}) {
    if (!baseUrl || typeof baseUrl !== 'string') {
      throw new ValidationError('Base URL is required', 'baseUrl');
    }

    this.#options = { ...DEFAULT_OPTIONS, ...options };
    this.#transport = new HttpTransport(baseUrl, this.#options);
  }

  /**
   * Get the underlying HTTP transport.
   * Useful for advanced configuration like adding interceptors.
   *
   * @returns {HttpTransport}
   */
  get transport() {
    return this.#transport;
  }

  /**
   * Set the authentication token for all requests.
   *
   * @param {string} token - JWT or access token
   * @param {string} [refreshToken] - Optional refresh token
   */
  setAuthToken(token, refreshToken = null) {
    this.#transport.setAuthToken(token, refreshToken);
  }

  /**
   * Clear the authentication token.
   */
  clearAuthToken() {
    this.#transport.clearAuthToken();
  }

  // ============================================================================
  // Core Data Operations
  // ============================================================================

  /**
   * Get the KV (Key-Value) client for basic CRUD operations.
   *
   * @returns {KVClient}
   */
  kv() {
    if (!this.#kvClient) {
      this.#kvClient = new KVClient(this.#transport);
    }
    return this.#kvClient;
  }

  /**
   * Get the Batch client for atomic batch operations.
   *
   * @returns {BatchClient}
   */
  batch() {
    if (!this.#batchClient) {
      this.#batchClient = new BatchClient(this.#transport);
    }
    return this.#batchClient;
  }

  /**
   * Get the Transaction client for compare-and-swap transactions.
   *
   * @returns {TransactionClient}
   */
  transaction() {
    if (!this.#transactionClient) {
      this.#transactionClient = new TransactionClient(this.#transport);
    }
    return this.#transactionClient;
  }

  /**
   * Get the Query client for secondary index queries.
   *
   * @returns {QueryClient}
   */
  query() {
    if (!this.#queryClient) {
      this.#queryClient = new QueryClient(this.#transport);
    }
    return this.#queryClient;
  }

  // ============================================================================
  // Cluster Operations
  // ============================================================================

  /**
   * Get the Cluster client for cluster management.
   *
   * @returns {ClusterClient}
   */
  cluster() {
    if (!this.#clusterClient) {
      this.#clusterClient = new ClusterClient(this.#transport);
    }
    return this.#clusterClient;
  }

  /**
   * Get the Health client for health checks.
   *
   * @returns {HealthClient}
   */
  health() {
    if (!this.#healthClient) {
      this.#healthClient = new HealthClient(this.#transport);
    }
    return this.#healthClient;
  }

  /**
   * Get the Operations client for data migration and node operations.
   *
   * @returns {OperationsClient}
   */
  operations() {
    if (!this.#operationsClient) {
      this.#operationsClient = new OperationsClient(this.#transport);
    }
    return this.#operationsClient;
  }

  // ============================================================================
  // Security Operations
  // ============================================================================

  /**
   * Get the Auth client for authentication.
   *
   * @returns {AuthClient}
   */
  auth() {
    if (!this.#authClient) {
      this.#authClient = new AuthClient(this.#transport);
    }
    return this.#authClient;
  }

  // ============================================================================
  // Cryptographic Services
  // ============================================================================

  /**
   * Get the KMS client for key management.
   *
   * @returns {KMSClient}
   */
  kms() {
    if (!this.#kmsClient) {
      this.#kmsClient = new KMSClient(this.#transport);
    }
    return this.#kmsClient;
  }

  /**
   * Get the Secrets client for secret storage.
   *
   * @returns {SecretsClient}
   */
  secrets() {
    if (!this.#secretsClient) {
      this.#secretsClient = new SecretsClient(this.#transport);
    }
    return this.#secretsClient;
  }

  /**
   * Get the Crypto client for encryption/signing operations.
   *
   * @returns {CryptoClient}
   */
  crypto() {
    if (!this.#cryptoClient) {
      this.#cryptoClient = new CryptoClient(this.#transport);
    }
    return this.#cryptoClient;
  }

  /**
   * Get the DynamicSecrets client for credential generation.
   *
   * @returns {DynamicSecretsClient}
   */
  dynamicSecrets() {
    if (!this.#dynamicSecretsClient) {
      this.#dynamicSecretsClient = new DynamicSecretsClient(this.#transport);
    }
    return this.#dynamicSecretsClient;
  }

  /**
   * Get the Certificate client for PKI operations.
   *
   * @returns {CertificateClient}
   */
  certificate() {
    if (!this.#certificateClient) {
      this.#certificateClient = new CertificateClient(this.#transport);
    }
    return this.#certificateClient;
  }

  // ============================================================================
  // Identity and Access Management
  // ============================================================================

  /**
   * Get the IAM client for identity and access management.
   *
   * @returns {IAMClient}
   */
  iam() {
    if (!this.#iamClient) {
      this.#iamClient = new IAMClient(this.#transport);
    }
    return this.#iamClient;
  }

  // ============================================================================
  // Specialized Data Operations
  // ============================================================================

  /**
   * Get the Vector client for vector embeddings and similarity search.
   *
   * @returns {VectorClient}
   */
  vector() {
    if (!this.#vectorClient) {
      this.#vectorClient = new VectorClient(this.#transport);
    }
    return this.#vectorClient;
  }

  /**
   * Get the TimeSeries client for time-series data operations.
   *
   * @returns {TimeSeriesClient}
   */
  timeSeries() {
    if (!this.#timeSeriesClient) {
      this.#timeSeriesClient = new TimeSeriesClient(this.#transport);
    }
    return this.#timeSeriesClient;
  }

  /**
   * Get the Backup client for backup and restore operations.
   *
   * @returns {BackupClient}
   */
  backup() {
    if (!this.#backupClient) {
      this.#backupClient = new BackupClient(this.#transport);
    }
    return this.#backupClient;
  }

  // ============================================================================
  // Storage Operations
  // ============================================================================

  /**
   * Get the ObjectStorage client for object storage operations.
   *
   * @returns {ObjectStorageClient}
   */
  objectStorage() {
    if (!this.#objectStorageClient) {
      this.#objectStorageClient = new ObjectStorageClient(this.#transport);
    }
    return this.#objectStorageClient;
  }

  /**
   * Get the ObjectStorageAdmin client for object storage admin operations.
   *
   * @returns {ObjectStorageAdminClient}
   */
  objectStorageAdmin() {
    if (!this.#objectStorageAdminClient) {
      this.#objectStorageAdminClient = new ObjectStorageAdminClient(this.#transport);
    }
    return this.#objectStorageAdminClient;
  }

  /**
   * Get the ObjectTransfer client for object transfer operations.
   *
   * @returns {ObjectTransferClient}
   */
  objectTransfer() {
    if (!this.#objectTransferClient) {
      this.#objectTransferClient = new ObjectTransferClient(this.#transport);
    }
    return this.#objectTransferClient;
  }

  /**
   * Get the ParameterStore client for application parameter management.
   *
   * @returns {ParameterStoreClient}
   */
  parameterStore() {
    if (!this.#parameterStoreClient) {
      this.#parameterStoreClient = new ParameterStoreClient(this.#transport);
    }
    return this.#parameterStoreClient;
  }

  // ============================================================================
  // AI/ML Services
  // ============================================================================

  /**
   * Get the Embedding client for embedding generation.
   *
   * @returns {EmbeddingClient}
   */
  embedding() {
    if (!this.#embeddingClient) {
      this.#embeddingClient = new EmbeddingClient(this.#transport);
    }
    return this.#embeddingClient;
  }

  /**
   * Get the Ollama client for local LLM operations.
   *
   * @returns {OllamaClient}
   */
  ollama() {
    if (!this.#ollamaClient) {
      this.#ollamaClient = new OllamaClient(this.#transport);
    }
    return this.#ollamaClient;
  }

  // ============================================================================
  // Cache Management
  // ============================================================================

  /**
   * Get the Cache client for cache management.
   *
   * @returns {CacheClient}
   */
  cache() {
    if (!this.#cacheClient) {
      this.#cacheClient = new CacheClient(this.#transport);
    }
    return this.#cacheClient;
  }

  // ============================================================================
  // Messaging Operations
  // ============================================================================

  /**
   * Returns the messaging client for pub/sub operations.
   *
   * @returns {MessagingClient}
   */
  messaging() {
    if (!this.#messagingClient) {
      this.#messagingClient = new MessagingClient(this.#transport);
    }
    return this.#messagingClient;
  }

  // ============================================================================
  // Scheduler Operations
  // ============================================================================

  /**
   * Get the Scheduler client for job scheduling operations.
   *
   * @returns {SchedulerClient}
   */
  scheduler() {
    if (!this.#schedulerClient) {
      this.#schedulerClient = new SchedulerClient(this.#transport);
    }
    return this.#schedulerClient;
  }
}

// Export the main class as default
export default QuicBaaS;

// Re-export all enums and constants for convenience
export {
  // Transport
  HttpTransport,

  // Core Data
  ConsistencyLevel,
  WriteMode,
  EventType,
  CompareTarget,
  CompareResult,
  TxnOpType,
  Operator,

  // Cluster
  NodeState,
  RebalanceStrategy,
  RebalanceStatus,
  HealthStatus,
  MigrationStatus,
  UpgradeStatus,
  ReplaceNodeStatus,

  // Cryptographic
  KeyAlgorithm,
  KeyStatus,
  KeyUsage,
  SecretStatus,
  RotationInterval,
  HashAlgorithm,
  SignatureAlgorithm,
  KeyDerivationFunction,
  CredentialType,
  CertificateUsage,
  CertKeyAlgorithm,
  TLSVersion,

  // IAM
  TenantStatus,
  UserStatus,
  ExportFormat,
  AuditEventType,

  // Specialized Data
  DistanceMetric,
  IndexType,
  Aggregation,
  Interval,
  BackupType,
  BackupStatus,
  RestoreStatus,

  // Storage
  ReplicationMode,
  ObjectStatus,
  StorageTier,
  BackendType,
  AdminStorageTier,
  GarbageCollectionStatus,
  ParameterType,

  // AI/ML
  EmbeddingProvider,
  InputType,
  EmbeddingStatus,

  // Messaging
  MessageCodec,
  MessagingEventType,

  // Scheduler
  JobType,
  JobStatus,
  OwnerType,
};

// Re-export all client classes
export {
  KVClient,
  BatchClient,
  TransactionClient,
  QueryClient,
  AuthClient,
  ClusterClient,
  HealthClient,
  OperationsClient,
  KMSClient,
  SecretsClient,
  CryptoClient,
  DynamicSecretsClient,
  CertificateClient,
  IAMClient,
  VectorClient,
  TimeSeriesClient,
  BackupClient,
  ObjectStorageClient,
  ObjectStorageAdminClient,
  ObjectTransferClient,
  ParameterStoreClient,
  EmbeddingClient,
  OllamaClient,
  CacheClient,
  MessagingClient,
  SchedulerClient,
};

// Re-export errors
export * from './errors/index.js';
