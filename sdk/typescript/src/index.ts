// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Factory, transport detection, and environment helpers.
export {
  createClient,
  getClient,
  isDesktop,
  isWailsRuntime,
  createBarrierClient,
  createCeremonyClient,
  createBootstrapClient,
} from './factory.js';

// Client interfaces — orchestration and connection.
export type {
  AuthClient,
  Client,
  ConnectionClient,
  OrchestratorClient,
  IAMClient,
  KMSClient,
  KMSKeyVersion,
  BarrierClient,
  TenantBarrierClient,
  CeremonyClient,
  BootstrapClient,
  ClusterClient,
  /** @deprecated Use BarrierClient. */
  SealClient,
  SecretsClient,
  UpdateUserRequest,
  UpdateTenantRequest,
  CreateRoleRequest,
  UpdateRoleRequest,
  CreatePolicyRequest,
  UpdatePolicyRequest,
  SSHKeysClient,
  SSHKnownHostsClient,
  SSHAuthorizedKeysClient,
  SSHClient,
  SelfRegisterBeginRequest,
  SelfRegisterBeginResponse,
  SelfRegisterFinishRequest,
  SelfRegisterFinishResponse,
} from './interfaces.js';

// DATA sub-client interfaces and types.
export type {
  DataClient,
  KVEntry,
  KVListResponse,
  VectorsClient,
  VectorNamespace,
  VectorSearchResult,
  TimeSeriesClient,
  TSPoint,
  TSQueryResponse,
  ObjectsClient,
  ObjectEntry,
  ObjectListResponse,
} from './interfaces.js';

// AI/ML + ASYNC sub-client interfaces and types.
export type {
  RAGClient,
  RAGQueryRequest,
  RAGQueryResponse,
  RAGBuildContextRequest,
  RAGBuildContextResponse,
  RAGCitation,
  RAGResultInput,
  RAGDocument,
  RAGListDocumentsResponse,
  RAGReindexRequest,
} from './rag.js';
export type {
  EmbeddingsClient,
  EmbeddingProviderInfo,
  EmbeddingMetadata,
  GenerateEmbeddingRequest,
  GenerateEmbeddingResponse,
  ListProvidersRequest,
  ListProvidersResponse,
  SetProviderStatusResponse,
} from './embedding_sdk.js';
export type {
  MessagingClient,
  MessagingChannel,
  MessagingMessage,
  ListChannelsRequest,
  ListChannelsResponse,
  CreateChannelRequest,
  CreateChannelResponse,
  DeleteChannelResponse,
  PublishRequest,
  PublishResponse,
} from './messaging_sdk.js';
export type {
  SchedulerClient,
  SchedulerJob,
  SchedulerStatus,
  ListJobsRequest,
  ListJobsResponse,
  CreateJobRequest,
  CreateJobResponse,
  DeleteJobResponse,
  TriggerJobResponse,
} from './scheduler_sdk.js';
export type {
  ProjectsClient,
  Project,
  ProjectRole,
  ContextRepo,
  CreateProjectRequest,
  CreateProjectResponse,
  GetProjectResponse,
  UpdateProjectRequest,
  UpdateProjectResponse,
  DeleteProjectResponse,
  ListProjectsResponse,
  GrantAgentRoleRequest,
  GrantAgentRoleResponse,
  RevokeAgentRoleRequest,
  RevokeAgentRoleResponse,
} from './projects.js';
export type {
  SdlcClient,
  ForgeConfig,
  ReviewPolicy,
  WorkerConfig,
  RankingPolicy,
  ProjectBinding,
  DeliveryPullRequest,
  ReviewRecord,
  CheckRecord,
  Delivery,
  OnboardingConfig,
  OnboardingReport,
  ConformanceReport,
  ConformanceFinding,
  ConformanceCheckResult,
} from './sdlc.js';
export type {
  AccountsClient,
  Account,
  AccountProvider,
  AccountStatus,
  AccountRefreshPolicy,
  AccountCredential,
  CreateAccountRequest,
  CreateAccountResponse,
  GetAccountResponse,
  ListAccountsRequest,
  ListAccountsResponse,
  UpdateAccountRequest,
  UpdateAccountResponse,
  DeleteAccountResponse,
  RotateAccountCredentialRequest,
  RotateAccountCredentialResponse,
  RevokeAccountRequest,
  RevokeAccountResponse,
  SetDefaultAccountResponse,
  AccountUsage,
  GetAccountUsageResponse,
} from './accounts.js';
export {
  parseJSONPreservingLargeIntegers,
  quoteUnrepresentableIntegers,
} from './json.js';
export type {
  CAClient,
  CertificateUsage,
  CertificateKeyAlgorithm,
  IssueCertificateRequest,
  IssueCertificateResponse,
  SignCSRRequest,
  SignCSRResponse,
  RevokeCertificateResponse,
  VerifyCertificateResponse,
  GetCACertificateResponse,
  CAInfoResponse,
  CABundleResponse,
  CertificateSummary,
  ListCertificatesRequest,
  ListCertificatesResponse,
  GenerateCRLResponse,
  IsRevokedResponse,
} from './ca.js';
export type {
  TrustClient,
  CrossSignedRootPoolResponse,
  AutoImportAIARequest,
  AutoImportAIAResponse,
  PublishDANERequest,
  PublishDANEResponse,
} from './trust.js';
export type {
  CryptoClient,
  HashAlgorithm as CryptoHashAlgorithm,
  CryptoEncryptRequest,
  CryptoEncryptResponse,
  CryptoDecryptRequest,
  CryptoDecryptResponse,
  CryptoRewrapRequest,
  CryptoRewrapResponse,
  CryptoSignRequest,
  CryptoSignResponse,
  CryptoVerifyRequest,
  CryptoVerifyResponse,
  CryptoHashRequest,
  CryptoHashResponse,
  CryptoRandomResponse,
  CryptoDataKeyRequest,
  CryptoDataKeyResponse,
} from './crypto.js';
export type {
  DEKClient,
  DEKAlgorithm,
  DEKMetadata,
  CreateDEKRequest,
  CreateDEKResponse,
  GetDEKResponse,
  ListDEKsRequest,
  ListDEKsResponse,
  RotateDEKRequest,
  RotateDEKResponse,
  DeleteDEKRequest,
  DeleteDEKResponse,
  ReencryptionProgress,
  GetDEKStatusResponse,
  SetDefaultDEKResponse,
  SetDEKSafetyRequest,
  DEKSafetyConfig,
  SetDEKSafetyResponse,
  ClearDEKSafetyResponse,
  StartReencryptionRequest,
  StartReencryptionResponse,
  GetReencryptionProgressResponse,
} from './dek.js';
export type {
  DynamicSecretsClient,
  DynamicSecretRole,
  CreateRoleRequest as DynamicSecretCreateRoleRequest,
  ListRolesResponse as ListDynamicSecretRolesResponse,
  DeleteRoleResponse,
  GenerateCredentialRequest,
  GenerateCredentialResponse,
  RenewLeaseRequest,
  RenewLeaseResponse,
  RevokeLeaseResponse,
  LeaseInfo,
  ListLeasesResponse,
} from './dynamicsecrets.js';

// All domain types.
export type * from './types.js';

// SandboxMode const object (not a type — must be a named export, not type-only).
export { SandboxMode } from './types.js';

// SPEND_UNATTRIBUTED is a value, not a type: a caller drilling into a spend
// report's unattributed row supplies it as that level's scope.
export { SPEND_UNATTRIBUTED } from './types.js';

// Paging, sorting and search — the one request and answer shape every list
// endpoint speaks. `adoptPage` and `wholeSetPage` are values a transport (or a
// consumer normalising its own fetch) calls; the rest are types.
export type {
  PageParams,
  PageResult,
  JobPageParams,
  TaskPageParams,
  SessionPageParams,
  AgentPageParams,
  AccountPageParams,
} from './page.js';
export {
  adoptPage,
  appendPageParams,
  appendListParam,
  appendCSVParam,
  wholeSetPage,
  MalformedPageError,
} from './page.js';

// Typed errors.
export {
  QuicBaaSError,
  ConnectionError,
  NotConnectedError,
  APIError,
  DeployNotSupportedError,
  SSHError,
  SSHNotFoundError,
  SSHDuplicateError,
  SSHInvalidError,
  SSHUnavailableError,
  SSHServerError,
  SSHHostMismatchError,
} from './errors.js';

// SSH REST client classes — exported so callers can construct them directly
// or use the top-level RestClient.ssh namespace.
export { RestSSHKeysClient, RestKnownHostsClient, RestAuthorizedKeysClient } from './rest.js';

// Barrier / Ceremony / Bootstrap REST clients — exported so callers can
// construct each sub-client directly without going through the full
// RestClient. The convenience factory functions in factory.ts wrap these.
export {
  RestBarrierClient,
  RestTenantBarrierClient,
  RestCeremonyClient,
  RestBootstrapClient,
  RestClusterClient,
} from './rest.js';

// Certificate authority / trust / crypto REST clients — exported so callers
// can construct each sub-client directly without going through the full
// RestClient.
export { RestCAClient, RestTrustClient, RestCryptoClient } from './rest.js';

// Stub transport error types — exported so callers can catch them explicitly.
export { GrpcWebNotImplementedError } from './grpc_web.js';
export { UnixNotImplementedError } from './unix.js';
export { McpNotImplementedError } from './mcp.js';
export { QuicNotImplementedError } from './quic.js';
