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
	"sync"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
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
}

// SecurityService provides security and encryption operations including
// authentication, sealing, key management, secrets, and certificates.
//
// Auth() returns an AuthService composed of the existing AuthClient plus
// the M10 factor-typed sub-services (FIDO2, WebAuthn, PIV, Factors). Code
// that previously called Security().Auth().Login(...) keeps working
// because AuthService embeds AuthClient.
//
// ACME(), Trust(), and CertificateExt() expose M10 primitives sourced
// from go-xkms (ACME, TrustedCA, TCG identity issuance) and re-published
// through the QuicBaaS SDK so consumers never need to import go-xkms
// directly.
type SecurityService interface {
	Auth() AuthService
	Seal() SealClient
	KMS() KMSClient
	Secrets() SecretsClient
	Crypto() CryptoClient
	Certificate() CertificateClient
	CertificateExt() CertificateExtensions
	ClusterDEK() ClusterDEKClient
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

	// CustodianGroup returns the custodian group client for managing
	// custodian groups used in Shamir secret sharing.
	CustodianGroup() (QRDBCustodianGroupClient, error)

	// Share returns the share client for submitting and tracking
	// Shamir shares.
	Share() (QRDBShareClient, error)
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

// securityService delegates SecurityService methods to the underlying client.
// The new Auth() returns an AuthService that composes the existing AuthClient
// plus FIDO2/WebAuthn/PIV/Factors sub-services. ACME(), Trust(), and
// CertificateExt() expose new M10 primitives.
type securityService struct {
	c *client

	authOnce    sync.Once
	authSvc     AuthService
	acmeOnce    sync.Once
	acmeSvc     ACMEService
	trustOnce   sync.Once
	trustSvc    TrustService
	certExtOnce sync.Once
	certExt     CertificateExtensions
}

func (s *securityService) Auth() AuthService {
	s.authOnce.Do(func() {
		s.authSvc = newAuthService(s.c)
	})
	return s.authSvc
}

func (s *securityService) Seal() SealClient                     { return s.c.Seal() }
func (s *securityService) KMS() KMSClient                       { return s.c.KMS() }
func (s *securityService) Secrets() SecretsClient               { return s.c.Secrets() }
func (s *securityService) Crypto() CryptoClient                 { return s.c.Crypto() }
func (s *securityService) Certificate() CertificateClient       { return s.c.Certificate() }
func (s *securityService) ClusterDEK() ClusterDEKClient         { return s.c.ClusterDEK() }
func (s *securityService) DEK() DEKClient                       { return s.c.DEK() }
func (s *securityService) DynamicSecrets() DynamicSecretsClient { return s.c.DynamicSecrets() }
func (s *securityService) RBAC() RBACClient                     { return s.c.RBAC() }

func (s *securityService) SealStore() (QRDBSealStoreClient, error) {
	return s.c.SealStore()
}

func (s *securityService) ACME() ACMEService {
	s.acmeOnce.Do(func() {
		s.acmeSvc = newACMEService(s.c.transport)
	})
	return s.acmeSvc
}

func (s *securityService) Trust() TrustService {
	s.trustOnce.Do(func() {
		s.trustSvc = newTrustService(s.c.transport)
	})
	return s.trustSvc
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

func (s *identityService) IAM() IAMClient { return s.c.IAM() }

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
	return s.c.BarrierCeremony()
}

func (s *barrierCeremonyService) CustodianGroup() (QRDBCustodianGroupClient, error) {
	return s.c.CustodianGroup()
}

func (s *barrierCeremonyService) Share() (QRDBShareClient, error) {
	return s.c.Share()
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
// access to Auth, Seal, KMS, Secrets, Crypto, Certificate, ClusterDEK,
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

// Barrier returns the QRDB BarrierClient for cryptographic barrier
// operations. The QRDB client is lazily initialized on first call.
func (c *client) Barrier() (QRDBBarrierClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.Barrier(), nil
}

// BarrierCeremony returns the QRDB CeremonyClient for managing the barrier
// ceremony lifecycle. The QRDB client is lazily initialized on first call.
func (c *client) BarrierCeremony() (QRDBCeremonyClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.Ceremony(), nil
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

// CustodianGroup returns the QRDB CustodianGroupClient for managing
// custodian groups used in Shamir secret sharing. The QRDB client is
// lazily initialized on first call.
func (c *client) CustodianGroup() (QRDBCustodianGroupClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.CustodianGroups(), nil
}

// Share returns the QRDB ShareClient for submitting and tracking
// Shamir shares. The QRDB client is lazily initialized on first call.
func (c *client) Share() (QRDBShareClient, error) {
	qrdbClient, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	return qrdbClient.Shares(), nil
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

// XKMSBarrierService is a re-export of the go-xkms BarrierService interface
// for cryptographic barrier management.
type XKMSBarrierService = xkmstransport.BarrierService

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
