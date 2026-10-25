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

package quicbaas

import (
	"context"
	"sync"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Service Sub-Interfaces
// =============================================================================

// CoreDataService provides access to core key-value data operations
// including batching, transactions, and queries.
type CoreDataService interface {
	KV() KVClient
	Batch() BatchClient
	Transaction() TransactionClient
	Query() QueryClient
}

// ClusterManagementService provides cluster operations including health checks,
// operational tasks, and cache management.
type ClusterManagementService interface {
	Cluster() ClusterClient
	Health() HealthClient
	Operations() OperationsClient
	Cache() CacheClient
	Registry() RegistryClient
}

// SecurityService provides security and encryption operations including
// authentication, sealing, key management, secrets, and certificates.
//
// Auth() returns an AuthService composed of the existing AuthClient plus
// the factor-typed sub-services: FIDO2() (the canonical WebAuthn ceremony
// client in go-xkms's verb shape), PIV() (the xkms backend-scoped PIV
// surface), Devices() (client-LOCAL CTAP2 discovery), and Factors()
// (multi-factor enrollment). Code that previously called
// Security().Auth().Login(...) keeps working because AuthService embeds
// AuthClient.
//
// ACME(), Trust(), and CertificateExt() expose M10 primitives sourced
// from go-xkms (ACME, TrustedCA, TCG identity issuance) and re-published
// through the QuicBaaS SDK so consumers never need to import go-xkms
// directly.
type SecurityService interface {
	Auth() AuthService
	KMS() KMSClient
	Secrets() SecretsClient
	Crypto() CryptoClient
	Certificate() CertificateClient
	CertificateExt() CertificateExtensions
	DEK() DEKClient
	DynamicSecrets() DynamicSecretsClient
	RBAC() RBACClient
	SealStore() (QRDBSealStoreClient, error)

	// ACME exposes server-side challenge verifiers and the ACME protocol
	// client (RFC 8555 + ARI + cross-sign + TPM enrollment).
	ACME() ACMEService

	// Trust exposes DANE TLSA publication, cross-signed root pools, and
	// AIA-driven root import.
	Trust() TrustService

	// Attestation exposes the standalone trusted-computing surface:
	// verification of hardware attestation statements and certificate
	// chains (composed from go-xkms pkg/attestation), the Tier 0 image
	// endorsement check, and the container attestation orchestration
	// seam. It is always non-nil; AttestContainer reports a typed
	// not-configured error until an embedder registers an attestor.
	Attestation() AttestationService

	// SSH exposes SSH key, known-hosts, and authorized-keys management.
	// This is the single canonical SDK entry point for the SSH surface —
	// callers never import go-xkms's SSH client directly.
	SSH() SSHService

	// Barrier exposes the qrdb StorageBarrier lifecycle (initialize,
	// unseal, seal, status, rekey, Shamir share management). This is
	// the single canonical SDK entry point — there is no flat
	// Client.Barrier() shortcut, in keeping with the spec's "one
	// canonical home per capability" rule.
	Barrier() (QRDBBarrierClient, error)

	// Ceremony exposes the qrdb initialization ceremony (Shamir M-of-N
	// share enrollment, officer cert claims, share claims, CSR
	// signing). This is the single canonical SDK entry point — there
	// is no flat Client.BarrierCeremony() shortcut.
	Ceremony() (QRDBCeremonyClient, error)

	// Credentials exposes the qrdb named-credential service:
	// non-versioned (Submit/Strategy/Delete) + versioned-rotation
	// (List/Rotate/RetrieveVersion/ListVersions/GCExpired). Backed by
	// the canonical CredentialClient implementation in qrdb across all
	// six transports (embedded, REST, gRPC, QUIC, MCP, Unix) — no
	// transport returns NotImplementedError.
	Credentials() (QRDBCredentialClient, error)

	// SecretResolver returns the unified secret resolver registry populated
	// during server initialization. It dispatches credential://, kms://, and
	// remote-kms:// resolution in registration order. Returns
	// nil when the client was constructed without WithSecretRegistry (e.g.
	// a pure network client that has no access to the in-process server).
	// go-trusted-platform consumes this as the single canonical entry point
	// for secret resolution — no direct import of pkg/config/secrets needed.
	SecretResolver() *qrdbsecret.ResolverRegistry
}

// IdentityService provides identity and access management operations.
type IdentityService interface {
	IAM() IAMClient
}

// SearchService provides search and analytics operations including
// vector similarity search and time series queries.
type SearchService interface {
	Vector() VectorClient
	TimeSeries() TimeSeriesClient
}

// StorageService provides object storage, backup, and parameter store operations.
type StorageService interface {
	Backup() BackupClient
	ObjectStorage() ObjectStorageClient
	ObjectStorageAdmin() ObjectStorageAdminClient
	ObjectTransfer() ObjectTransferClient
	ParameterStore() ParameterStoreClient
}

// AsyncService provides asynchronous processing operations including
// job scheduling and pub/sub messaging.
type AsyncService interface {
	Scheduler() SchedulerClient
	Messaging() MessagingClient
}

// AIService provides AI and machine learning operations including
// embedding generation, Ollama integration, and retrieval-augmented generation.
type AIService interface {
	Embedding() EmbeddingClient
	Ollama() OllamaClient
	RAG() RAGClient
}

// BarrierCeremonyService provides barrier ceremony operations including
// initialization, certificate claims, share distribution, and custodian
// group management via the QRDB transport layer.
type BarrierCeremonyService interface {
	// Ceremony returns the ceremony client for managing the initialization
	// ceremony lifecycle.
	Ceremony() (QRDBCeremonyClient, error)
}

// TenantService provides multi-tenant management operations including
// tenant lifecycle and per-tenant barrier management via the QRDB
// transport layer.
type TenantService interface {
	// Tenant returns the tenant client for managing multi-tenant
	// operations and per-tenant barriers.
	Tenant() (QRDBTenantClient, error)
}

// =============================================================================
// Sub-Interface Wrapper Implementations
// =============================================================================

// coreDataService delegates CoreDataService methods to the underlying client.
type coreDataService struct {
	c *client
}

func (s *coreDataService) KV() KVClient                   { return s.c.KV() }
func (s *coreDataService) Batch() BatchClient             { return s.c.Batch() }
func (s *coreDataService) Transaction() TransactionClient { return s.c.Transaction() }
func (s *coreDataService) Query() QueryClient             { return s.c.Query() }

// clusterManagementService delegates ClusterManagementService methods to the underlying client.
type clusterManagementService struct {
	c *client
}

func (s *clusterManagementService) Cluster() ClusterClient       { return s.c.Cluster() }
func (s *clusterManagementService) Health() HealthClient         { return s.c.Health() }
func (s *clusterManagementService) Operations() OperationsClient { return s.c.Operations() }
func (s *clusterManagementService) Cache() CacheClient           { return s.c.Cache() }
func (s *clusterManagementService) Registry() RegistryClient     { return s.c.Registry() }

// securityService delegates SecurityService methods to the underlying client.
// The new Auth() returns an AuthService that composes the existing AuthClient
// plus FIDO2/PIV/Devices/Factors sub-services. ACME(), Trust(), and
// CertificateExt() expose new M10 primitives.
type securityService struct {
	c *client

	authOnce  sync.Once
	authSvc   AuthService
	acmeOnce  sync.Once
	acmeSvc   ACMEService
	trustOnce sync.Once
	trustSvc  TrustService

	attestationOnce sync.Once
	attestationSvc  AttestationService

	sshOnce     sync.Once
	sshSvc      SSHService
	certExtOnce sync.Once
	certExt     CertificateExtensions
}

func (s *securityService) Auth() AuthService {
	s.authOnce.Do(func() {
		s.authSvc = newAuthService(s.c)
	})
	return s.authSvc
}

func (s *securityService) KMS() KMSClient                       { return s.c.kms() }
func (s *securityService) Secrets() SecretsClient               { return s.c.secrets() }
func (s *securityService) Crypto() CryptoClient                 { return s.c.crypto() }
func (s *securityService) Certificate() CertificateClient       { return s.c.certificate() }
func (s *securityService) DEK() DEKClient                       { return s.c.dek() }
func (s *securityService) DynamicSecrets() DynamicSecretsClient { return s.c.dynamicSecrets() }
func (s *securityService) RBAC() RBACClient                     { return s.c.rbac() }

func (s *securityService) SealStore() (QRDBSealStoreClient, error) {
	return s.c.SealStore()
}

func (s *securityService) ACME() ACMEService {
	s.acmeOnce.Do(func() {
		// ACME is HTTP-native; acmeTransport() routes over REST/embedded
		// regardless of the client's configured main transport.
		s.acmeSvc = newACMEService(s.c.acmeTransport())
	})
	return s.acmeSvc
}

func (s *securityService) Trust() TrustService {
	s.trustOnce.Do(func() {
		s.trustSvc = newTrustService(s.c.transport)
	})
	return s.trustSvc
}

// Attestation returns the trusted-computing sub-service. The returned
// client routes over the configured transport, so the same verbs behave
// identically on embedded, REST, gRPC, QUIC, MCP, and Unix.
func (s *securityService) Attestation() AttestationService {
	s.attestationOnce.Do(func() {
		s.attestationSvc = newAttestationService(s.c.transport)
	})
	return s.attestationSvc
}

// SSH returns the SSH sub-service (keys, known hosts, authorized keys).
// The returned client routes over the configured transport, so the same
// verbs behave identically on embedded, REST, gRPC, QUIC, MCP, and Unix.
func (s *securityService) SSH() SSHService {
	s.sshOnce.Do(func() {
		s.sshSvc = newSSHService(s.c.transport)
	})
	return s.sshSvc
}

// Barrier returns the QRDB BarrierClient. The QRDB transport is
// lazily initialized on first call (delegated to client.qrdbClient).
func (s *securityService) Barrier() (QRDBBarrierClient, error) {
	qrdbClient, err := s.c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return &systemBarrierClient{QRDBBarrierClient: qrdbClient.Barrier()}, nil
}

// systemBarrierClient is the qrdb barrier client behind Security().Barrier().
// It refuses system init-shamir on every transport before anything is sent:
// the initialization ceremony is the only bootstrap path for the system
// Shamir barrier. Every other verb passes through unchanged.
type systemBarrierClient struct {
	QRDBBarrierClient
}

// BarrierInitializeShamir returns the typed ceremony refusal.
func (c *systemBarrierClient) BarrierInitializeShamir(_ context.Context, _ *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	return nil, sdkerrors.NewSystemShamirInitRefused("barrier.InitializeShamir")
}

// Ceremony returns the QRDB CeremonyClient. The QRDB transport is
// lazily initialized on first call (delegated to client.qrdbClient).
func (s *securityService) Ceremony() (QRDBCeremonyClient, error) {
	qrdbClient, err := s.c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.Ceremony(), nil
}

// Credentials returns the QRDB CredentialClient. The QRDB transport
// is lazily initialized on first call (delegated to client.qrdbClient).
func (s *securityService) Credentials() (QRDBCredentialClient, error) {
	qrdbClient, err := s.c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.Credentials(), nil
}

// SecretResolver returns the unified secret resolver registry injected via
// WithSecretRegistry. Returns nil when the option was not provided (e.g.
// pure network clients that have no access to the in-process server registry).
func (s *securityService) SecretResolver() *qrdbsecret.ResolverRegistry {
	return s.c.opts.secretRegistry
}

func (s *securityService) CertificateExt() CertificateExtensions {
	s.certExtOnce.Do(func() {
		s.certExt = newCertificateExtensions(s.c.transport)
	})
	return s.certExt
}

// identityService delegates IdentityService methods to the underlying client.
type identityService struct {
	c *client
}

func (s *identityService) IAM() IAMClient { return s.c.iam() }

// searchService delegates SearchService methods to the underlying client.
type searchService struct {
	c *client
}

func (s *searchService) Vector() VectorClient         { return s.c.Vector() }
func (s *searchService) TimeSeries() TimeSeriesClient { return s.c.TimeSeries() }

// storageService delegates StorageService methods to the underlying client.
type storageService struct {
	c *client
}

func (s *storageService) Backup() BackupClient               { return s.c.Backup() }
func (s *storageService) ObjectStorage() ObjectStorageClient { return s.c.ObjectStorage() }
func (s *storageService) ObjectStorageAdmin() ObjectStorageAdminClient {
	return s.c.ObjectStorageAdmin()
}
func (s *storageService) ObjectTransfer() ObjectTransferClient { return s.c.ObjectTransfer() }
func (s *storageService) ParameterStore() ParameterStoreClient { return s.c.ParameterStore() }

// asyncService delegates AsyncService methods to the underlying client.
type asyncService struct {
	c *client
}

func (s *asyncService) Scheduler() SchedulerClient { return s.c.Scheduler() }
func (s *asyncService) Messaging() MessagingClient { return s.c.Messaging() }

// aiService delegates AIService methods to the underlying client.
type aiService struct {
	c *client
}

func (s *aiService) Embedding() EmbeddingClient { return s.c.Embedding() }
func (s *aiService) Ollama() OllamaClient       { return s.c.Ollama() }
func (s *aiService) RAG() RAGClient             { return s.c.RAG() }

// barrierCeremonyService delegates BarrierCeremonyService methods to the
// underlying client.
type barrierCeremonyService struct {
	c *client
}

func (s *barrierCeremonyService) Ceremony() (QRDBCeremonyClient, error) {
	qrdbClient, err := s.c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.Ceremony(), nil
}

// tenantService delegates TenantService methods to the underlying client.
type tenantService struct {
	c *client
}

func (s *tenantService) Tenant() (QRDBTenantClient, error) {
	return s.c.TenantMgmt()
}

// =============================================================================
// Sub-Interface Accessor Methods on client
// =============================================================================

// CoreData returns the CoreDataService sub-interface providing organized
// access to KV, Batch, Transaction, and Query operations.
func (c *client) CoreData() CoreDataService {
	return &coreDataService{c: c}
}

// ClusterMgmt returns the ClusterManagementService sub-interface providing
// organized access to Cluster, Health, Operations, and Cache operations.
func (c *client) ClusterMgmt() ClusterManagementService {
	return &clusterManagementService{c: c}
}

// Security returns the SecurityService sub-interface providing organized
// access to Auth, KMS, Secrets, Crypto, Certificate,
// DynamicSecrets, RBAC, SealStore, ACME, Trust, and CertificateExt operations.
func (c *client) Security() SecurityService {
	return c.securitySvc()
}

// securitySvc returns a lazily-initialized securityService bound to this
// client. Initialization is per-client so the sub-services are stable
// across multiple Security() calls.
func (c *client) securitySvc() *securityService {
	c.securityOnce.Do(func() {
		c.security = &securityService{c: c}
	})
	return c.security
}

// Identity returns the IdentityService sub-interface providing organized
// access to IAM operations.
func (c *client) Identity() IdentityService {
	return &identityService{c: c}
}

// Search returns the SearchService sub-interface providing organized
// access to Vector and TimeSeries operations.
func (c *client) Search() SearchService {
	return &searchService{c: c}
}

// Storage returns the StorageService sub-interface providing organized
// access to Backup, ObjectStorage, ObjectStorageAdmin, ObjectTransfer,
// and ParameterStore operations.
func (c *client) Storage() StorageService {
	return &storageService{c: c}
}

// Async returns the AsyncService sub-interface providing organized
// access to Scheduler and Messaging operations.
func (c *client) Async() AsyncService {
	return &asyncService{c: c}
}

// AI returns the AIService sub-interface providing organized access
// to Embedding, Ollama, and RAG operations.
func (c *client) AI() AIService {
	return &aiService{c: c}
}

// BarrierCeremonyMgmt returns the BarrierCeremonyService sub-interface
// providing organized access to ceremony, custodian group, and share
// operations via the QRDB transport.
func (c *client) BarrierCeremonyMgmt() BarrierCeremonyService {
	return &barrierCeremonyService{c: c}
}

// TenantMgmtService returns the TenantService sub-interface providing
// organized access to tenant lifecycle and per-tenant barrier operations
// via the QRDB transport.
func (c *client) TenantMgmtService() TenantService {
	return &tenantService{c: c}
}

// SealStore returns the QRDB SealStoreClient for managing
// platform-local sealed credentials. The QRDB client is lazily
// initialized on first call.
func (c *client) SealStore() (QRDBSealStoreClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.SealStore(), nil
}

// TenantMgmt returns the QRDB TenantClient for managing multi-tenant
// operations and per-tenant barriers. The QRDB client is lazily
// initialized on first call.
func (c *client) TenantMgmt() (QRDBTenantClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.Tenants(), nil
}

// =============================================================================
// go-xkms Sub-Interface Re-exports
// =============================================================================
//
// The following type aliases re-export go-xkms service sub-interfaces,
// allowing QuicBaaS SDK users to reference XKMS service contracts
// without importing the go-xkms package directly.

// XKMSKeyService is a re-export of the go-xkms KeyService interface
// for key lifecycle management operations.
type XKMSKeyService = xkmstransport.KeyService

// XKMSCryptoService is a re-export of the go-xkms CryptoService interface
// for cryptographic operations (sign, verify, encrypt, decrypt).
type XKMSCryptoService = xkmstransport.CryptoService

// XKMSCertService is a re-export of the go-xkms CertService interface
// for certificate management operations.
type XKMSCertService = xkmstransport.CertService

// XKMSSealService is a re-export of the go-xkms SealService interface
// for data sealing and unsealing operations.
type XKMSSealService = xkmstransport.SealService

// XKMSPIVService is a re-export of the go-xkms PIVService interface
// for Personal Identity Verification operations.
type XKMSPIVService = xkmstransport.PIVService

// XKMSFIDO2Service is a re-export of the go-xkms FIDO2Service interface
// for FIDO2/WebAuthn operations.
type XKMSFIDO2Service = xkmstransport.FIDO2Service

// TrustedCAService is a re-export of the go-xkms CAService interface
// for Certificate Authority operations.
type TrustedCAService = xkmstransport.CAService

// XKMSPINService is a re-export of the go-xkms PINService interface
// for PIN management operations.
type XKMSPINService = xkmstransport.PINService

// XKMSUserService is a re-export of the go-xkms UserService interface
// for user management operations.
type XKMSUserService = xkmstransport.UserService

// XKMSConnectionService is a re-export of the go-xkms ConnectionService interface
// for connection lifecycle and health checks.
type XKMSConnectionService = xkmstransport.ConnectionService

// XKMSBackendService is a re-export of the go-xkms BackendService interface
// for backend discovery and information.
type XKMSBackendService = xkmstransport.BackendService
