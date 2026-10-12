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

// Package server provides the main orchestration layer for QuicBaaS.
// It manages the lifecycle of all components including gRPC, REST, and MCP servers.
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	ggrpc "google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	clustercasdk "github.com/jeremyhahn/go-xkms/sdk/go/clusterca"
	dane "github.com/jeremyhahn/go-xkms/sdk/go/dane"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/fips"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/server/devcluster"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/version"
	"github.com/jeremyhahn/go-quicbaas/pkg/watchbridge"
)

// serverOptions holds optional configuration provided via functional options
// at construction time. These options allow external systems to inject
// components that would otherwise be created internally.
type serverOptions struct {
	// externalCA is an externally managed Certificate Authority. When set,
	// initializeCA() skips internal CA bootstrap and uses this CA for TLS
	// certificate issuance. The caller is responsible for having completed
	// Load() or Init() on the CA before passing it here.
	externalCA xkmssdk.TrustedCA

	// tlsCertificate is a pre-issued TLS certificate. When set alongside
	// externalCA, the server skips TLS certificate issuance and uses this
	// certificate directly for all transport layers.
	tlsCertificate *tls.Certificate

	// sealPassphrase is the passphrase used to auto-unseal the qrdb node's
	// software seal barrier on startup. When set alongside a passphrase seal
	// strategy (cfg.Seal.Strategy == "passphrase"), the qrdb node builder
	// receives node.WithSealPassphrase so the barrier is unsealed
	// automatically during Start. The caller is responsible for wiping the
	// slice after construction. Only consulted when cfg.Seal.Enabled is true.
	sealPassphrase []byte

	// danePublisher is the application-supplied DANE TLSA publisher used by
	// Security().Trust().PublishDANE. xkms defines the DANEPublisher
	// interface but ships NO DNS implementation; an application registers
	// one here. When nil, PublishDANE returns a typed not-configured error.
	danePublisher dane.DANEPublisher

	// containerAttestor is the application-supplied orchestration hook used
	// by Security().Attestation().AttestContainer. QuicBaaS ships the
	// standalone trust tiers itself (image digest pinning, host TPM quote,
	// measured launch — see pkg/orchestration); this seam exists so a fleet
	// manager such as go-trusted-platform can substitute registry-backed
	// attestation with continuous re-attestation and quarantine. When nil,
	// AttestContainer returns a typed not-configured error.
	containerAttestor service.ContainerAttestor

	// attestationTrustAnchors are the root certificates every attestation
	// verification is anchored against. When empty, only the per-request
	// anchors supplied in the verify options apply.
	attestationTrustAnchors []*x509.Certificate

	// hostModules are capability modules a downstream host (e.g.
	// go-trusted-platform) mounts via WithModule. They are appended to the
	// unified qrdb node's module set after quicbaas's own module and
	// go-xkms's, so one node serves all three across gRPC, unix, REST and
	// MCP under a single authorizer. See module_host.go.
	hostModules []qrdbsdk.ServiceModule
}

// ServerOption configures optional behavior on the Server during construction.
type ServerOption func(*serverOptions)

// WithCA returns a ServerOption that injects an external Certificate Authority.
// When provided, the server skips its internal CA bootstrap during initialization
// and uses the given CA for TLS certificate operations. The CA must have already
// been initialized (Load or Init called) before being passed here.
func WithCA(ca xkmssdk.TrustedCA) ServerOption {
	return func(o *serverOptions) { o.externalCA = ca }
}

// WithTLSCertificate returns a ServerOption that provides a pre-issued TLS
// certificate for the server. This is typically used together with WithCA to
// skip both CA bootstrap and certificate issuance. The certificate must contain
// both the leaf certificate and private key.
func WithTLSCertificate(cert *tls.Certificate) ServerOption {
	return func(o *serverOptions) { o.tlsCertificate = cert }
}

// WithSealPassphrase returns a ServerOption that injects the passphrase used
// to auto-unseal the qrdb node's software seal barrier during server startup.
// When supplied and cfg.Seal.Strategy is "passphrase", the qrdb node is
// constructed with node.WithSealPassphrase so the barrier is unsealed
// automatically — the server comes up fully operational without requiring
// an explicit "barrier unseal" API call.
//
// This is the preferred injection point for the quicbaasd init path (where
// the admin passphrase is available at boot time) and for test helpers that
// need a fully unsealed node. For production deployments that require operator
// intervention at every start, omit this option: a daemon boot then enters
// the sealed-window phase (all five transports serve only the canonical
// barrier/ceremony/health surface until an operator drives the unseal — see
// sealed_window.go), and an embedded boot is refused with the typed
// EmbeddedSealedBootError.
//
// The caller is responsible for zeroing the slice after the server is
// constructed. The server does not retain a reference beyond node creation.
func WithSealPassphrase(passphrase []byte) ServerOption {
	return func(o *serverOptions) { o.sealPassphrase = passphrase }
}

// WithDANEPublisher returns a ServerOption that registers the
// application-supplied DANE TLSA publisher used by
// Security().Trust().PublishDANE. xkms defines the DANEPublisher interface
// (cert→TLSA derivation is delegated to go-truststrap) but provides NO DNS
// implementation: the DNS provider, zone, credentials, and update protocol
// are deployment concerns owned by the embedding application. Until a
// publisher is registered, PublishDANE returns a typed not-configured error
// across every transport.
func WithDANEPublisher(publisher dane.DANEPublisher) ServerOption {
	return func(o *serverOptions) { o.danePublisher = publisher }
}

// WithContainerAttestor returns a ServerOption that registers the
// application-supplied container attestor behind
// Security().Attestation().AttestContainer.
//
// This is the Tier 3 seam of the standalone trusted-agent model. QuicBaaS
// implements Tiers 0-2 on its own — image digest pinning with post-launch
// verification, host TPM quote against expected PCRs, and measured launch —
// and can fill this seam with them via
// service.NewRuntimeContainerAttestor(runtime). What it deliberately does NOT
// implement is fleet automation: a device registry, scheduled
// re-attestation, quarantine on drift, golden-PCR policy management and
// network isolation. Those belong to go-trusted-platform, which registers its
// own attestor here.
//
// Until an attestor is registered, AttestContainer returns a typed
// not-configured error across every transport — the same contract
// WithDANEPublisher uses.
func WithContainerAttestor(attestor service.ContainerAttestor) ServerOption {
	return func(o *serverOptions) { o.containerAttestor = attestor }
}

// WithAttestationTrustAnchors returns a ServerOption that sets the root
// certificates every Security().Attestation() verification is anchored
// against. Callers may still supply additional per-request anchors in the
// verify options; these are the deployment-wide defaults.
func WithAttestationTrustAnchors(anchors []*x509.Certificate) ServerOption {
	return func(o *serverOptions) { o.attestationTrustAnchors = anchors }
}

// Server orchestrates all QuicBaaS components.
type Server struct {
	config *config.Config
	logger *logger.Logger

	// Functional options provided at construction time
	opts serverOptions

	// DevCluster management - coordinates local multi-node development clusters
	devClusterMgr *devcluster.Manager

	// Time series storage - provided via interfaces.TimeSeriesStorage
	timeSeriesStorage interfaces.TimeSeriesStorage

	// KMS and Secrets components
	kmsManager     *kms.KMSService
	secretsManager *secrets.Manager
	vectorService  service.VectorService
	vectorStorage  interfaces.VectorStorage

	// ownsXKMS records that THIS server's initXKMS call created the
	// process-global xkms service (initializeCA or startKMS), so Shutdown
	// must close it. A service someone else initialized is left alone.
	// Written during initialize() and read by Shutdown after initWG.Wait,
	// so the WaitGroup orders the two.
	ownsXKMS bool

	// Cache components
	cacheManager *cache.CacheManager

	// Database management - the qrdb tenant database Manager built by
	// startDatabases. Retained on the Server rather than left local to that
	// function because NOTHING downstream owns it: it is handed to a
	// qrdb DatabaseServicer and on to the service facade, and the facade's
	// Close closes the cache manager, the backup manager, the storage, the
	// qrdb client and the SSH client -- but never this. Its OpenAll holds
	// every recovered tenant database open, so without an owner here a
	// Shutdown (after a failed Start or a clean one) left them all open.
	// Closed in Shutdown; see the Close call there.
	databaseManager *qrdbsdk.Manager

	// RAG system - provides Retrieval-Augmented Generation capabilities
	ragSystem *rag.System

	// Embedding manager - provides vector embedding generation via configurable providers
	embeddingManager *embedding.Manager

	// Ollama service - provides LLM embedding and model management via Ollama
	ollamaService service.OllamaService

	// Backup manager - handles backup and restore operations
	backupManager *backup.BackupManager
	// backupStorage provides the interfaces.Storage needed by the backup manager.
	// Created during startBackupManager and closed during stopBackupManager.
	backupStorage *backupStorageAdapter

	// Object storage catalog components
	catalogManager catalog.CatalogManager
	backendManager catalog.BackendManager
	workerManager  *catalog.WorkerManager

	// Certificate Authority - manages TLS certificate issuance
	xkmsCA xkmssdk.TrustedCA

	// caAdapter is the live CA service adapter wired into the QuicBaaS service
	// at startService() time. Keeping a reference allows UpdateCA() to swap
	// the backing CA post-start without rebuilding the service.
	caAdapter *caServiceAdapter

	// xkmsKeyStorage is the file-backed XKMS key store created by
	// initializeCA. startKMS reuses this same storage root so that the CA
	// chain and the KMS symmetric-key chain share a SINGLE on-disk key
	// universe. This eliminates the two-chain KEK mismatch that caused
	// "cipher: message authentication failed" on every fresh boot.
	// When TLS is disabled (no CA chain), startKMS falls back to an
	// in-memory or freshly-created file-backed store.
	xkmsKeyStorage xkmssdk.StorageBackend

	// xkmsKeyVersions is the key-version record store the process-global
	// xkms service rotates keys through. It is built once (keyVersionStore)
	// and shared by every initXKMS call, so whichever registers the
	// singleton wires the same store.
	xkmsKeyVersions *xkmssdk.KeyVersionStore

	// Fleet registry - shared between IAM enrollment (publisher on
	// redeem) and orchestration (publisher on heartbeat + consumer for
	// dispatch). Constructed during IAM init so the enrollment service can
	// publish on redemption; the orchestration layer receives the same
	// instance via SetFleetRegistry so dispatch sees enrollment-time
	// registrations immediately.
	fleetRegistry orchestration.FleetRegistry

	// orchTaskStore is the orchestration task store startOrchestration built
	// and handed to the agent service.
	orchTaskStore orchestration.TaskStore

	// orchJobLeaseStore is the job lease store startOrchestration built, and
	// orchLeaseTTL the TTL it was built with. The task-source poller and the
	// task dispatcher read them to count how many jobs a project is running.
	orchJobLeaseStore orchestration.JobLeaseStore
	orchLeaseTTL      time.Duration

	// orchAccountStore is the pooled-account store startOrchestration built.
	// The SDLC module's attention inbox reads it directly (not through
	// service.AccountService, whose request/response shape does not match
	// the inbox's ListByTenant) for the account_error alert — a pooled
	// credential whose last refresh failed.
	orchAccountStore orchestration.AccountStore

	// orchAgentDirectory is the tenant AI agent catalog startOrchestration
	// built: every slot's Agent or pool resolves against it. Nil when
	// orchestration is off.
	orchAgentDirectory *orchestration.AgentDirectory

	// promptStore holds every tenant's prompt records, and promptSeeder copies
	// the system set into tenants and converges it onto this build's defaults.
	// Built once by ensurePromptStore; nil without an embedded qrdb node, in
	// which case every job resolves its prompts to the embedded defaults.
	promptStore  *prompts.Store
	promptSeeder *prompts.Seeder

	// tenantUnsealHooks is told by every per-tenant barrier facade after a
	// tenant's barrier is unsealed; the boot migrations register here to
	// reach the tenants that were sealed at boot.
	tenantUnsealHooks service.TenantUnsealHooks

	// orchFleetCapacity answers admission's fleet axis: how many worker slots
	// the deployment has. Built once in startOrchestration and shared by BOTH
	// enforcement points — the sweeps' admission plan and the lease store's
	// last-ditch refusal — so the two cannot bound the fleet by different
	// numbers. See orchestration/fleet_capacity.go.
	orchFleetCapacity orchestration.FleetCapacityReader

	// orchPoolHeadroom answers admission's provider axis: whether a project's
	// pooled provider accounts can serve another session. Built beside the
	// account store and the quota authority in startOrchestration, and nil in
	// a deployment that pools no accounts — in which case admission simply
	// leaves that axis out (see orchestration.AdmissionControllerConfig).
	orchPoolHeadroom orchestration.PoolHeadroomReader

	// orchScopeSpend answers admission's spend axis: whether a project's and
	// its tenant's own spend limits admit more work. Built beside the
	// consumption ledger in startOrchestration; nil leaves the axis out.
	orchScopeSpend orchestration.ScopeSpendReader

	// taskSourcePollerStop ends the task-source poller startOrchestration
	// started; taskSourcePollerDone closes when it has returned. Both are
	// nil when no poller runs (orchestration disabled, or no facade to
	// create tasks through).
	taskSourcePoller     *orchestration.TaskSourcePoller
	taskSourcePollerStop context.CancelFunc
	taskSourcePollerDone chan struct{}

	// taskDispatcher starts jobs for the pending tasks of every autonomous
	// project. It runs beside the poller, at the same cadence, and stops
	// with it.
	taskDispatcher *orchestration.TaskDispatcher

	// taskSpawner is what the dispatcher will start tasks through. It is
	// captured when the engine starts and handed to the dispatcher only by the
	// startupTaskDispatcher step, AFTER interrupted jobs are recovered.
	taskSpawner orchestration.TaskSpawner

	// dispatchGate closes once every job recovery resumed holds its lease
	// (or ended); the dispatcher's first sweep waits on it. Nil when no
	// recovery ran, which gates nothing.
	dispatchGate <-chan struct{}

	taskDispatcherStop context.CancelFunc
	taskDispatcherDone chan struct{}

	// orchSweeps is the job reaper and stall detector's sweep goroutine,
	// started by startOrchestration and stopped at shutdown.
	orchSweeps *orchestrationSweeps

	// orchContainerReaper is the orchestrator's sandbox-container reaper
	// goroutine, started with the container runtime and stopped at shutdown.
	orchContainerReaper *orchestrationSweeps

	// orchWorkspaceSweep is the periodic sweep of concluded tasks' session
	// workspaces, stopped at shutdown.
	orchWorkspaceSweep *orchestrationSweeps

	// sdlcLabelSweeps is the SDLC module's forge-label reconciliation
	// goroutine, started by startSDLC and stopped before the module detaches.
	sdlcLabelSweeps *sdlcLabelSweeps

	// sdlcBackstops is the goroutine driving the SDLC module's two
	// missed-delivery backstops — the triage pass and the review floor; see
	// lifecycle_sdlc_backstop.go.
	sdlcBackstops *sdlcBackstops

	// sdlcWorkerImage is the worker image refresh loop, started by startSDLC
	// when the node has a container engine; see lifecycle_sdlc_worker_image.go.
	sdlcWorkerImage *sdlcWorkerImageLoop

	// orchContainerRuntime is the container engine the orchestration service
	// spawns sandboxed sessions on, nil on a node without docker or podman.
	// The worker image refresh builds on the same engine, so the image it
	// builds is the image this node's workers run.
	orchContainerRuntime *orchestration.ExecContainerRuntime

	// gitProvisioner is the managed-workspace git path wireProjectWorkspaces
	// composed (secrets-store tokens, xkms-held SSH keys and host-key pins).
	// The SDLC module's workspace reuses it rather than composing a second.
	gitProvisioner *service.GitProvisioner

	// iamComponents holds the initialized IAM subsystem components.
	// Set by lifecycle.go after initializeIAMComponents succeeds and used by
	// wireIAMMTLSAuthenticators to obtain the AppStorage without a runtime
	// type assertion on quicBaaSService.
	iamComponents service.IAMComponents

	// loginSessionIssuer is the late-bound SessionIssuer injected into the
	// composed qrdb node so a successful unified login mints an OIDC bearer.
	// It is created (provider unbound) before node construction and has its
	// OIDC provider attached after IAM init completes — see
	// buildLoginSessionIssuer / bindLoginCeremonyProvider. Nil when IAM is
	// not compiled (the !iam stub leaves login issuance unwired).
	loginSessionIssuer qrdbembedded.SessionIssuer

	// loginFactorVerifiers are the late-bound hardware FactorVerifiers (PIV +
	// tenant-aware WebAuthn) injected into the composed qrdb node so the
	// in-process login ceremony can verify PIV and WebAuthn proofs. Like
	// loginSessionIssuer they are constructed (dependencies unbound) before
	// node construction and have their dependencies attached after IAM init
	// via bindLoginCeremonyProvider. Empty when IAM is not compiled (the !iam
	// stub leaves factor verification unwired). The verification crypto is
	// always delegated to the xkms verifiers; these shells only resolve
	// dependencies and enforce tenant isolation.
	loginFactorVerifiers []qrdbembedded.FactorVerifier

	// webauthnFactory is the per-tenant WebAuthn relying-party factory,
	// published for consumers that are composed BEFORE IAM initialisation
	// runs and can therefore only reach it late. The composed go-xkms
	// ServiceModule's credential-store decorator is one: it is handed to
	// go-xkms while the qrdb node is still being constructed, and resolves
	// the factory on the first registration it serves
	// (pkg/iam NewWebAuthnModuleCredentialStoreDecorator). Nil until
	// bindXKMSModuleWebAuthnFactory stores one; every consumer fails closed
	// while it is.
	webauthnFactory atomic.Pointer[iam.WebAuthnServiceFactory]

	// orchestratorDaemonClient holds the OAuth2 credentials the central uses
	// to authenticate its outbound calls to remote fleet node daemons. Populated
	// by ensureOrchestratorDaemonClient during IAM init; consumed by the
	// orchestration layer's RemoteBackendResolver to mint JWTs for daemon
	// RPCs. See pkg/server/iam_init.go for bootstrap details.
	orchestratorDaemonClient *OrchestratorDaemonClient

	// tlsConfig holds the in-memory TLS configuration backed by OpaqueKeys.
	// Created by initializeCA() and used by all transport layers.
	tlsConfig *tls.Config

	// Seal management - uses the interfaces.SealManager abstraction backed
	// by qrdb's StorageBarrier via the BarrierSealManager adapter.
	sealManager interfaces.SealManager //nolint:staticcheck // SA1019: legacy seal compatibility surface, deliberately retained until the seal API is removed.

	// dataPlaneBarrier is the concrete StorageBarrier used to encrypt the
	// data-plane stores (KV, time-series, vector). Set during startService()
	// when buildDataPlaneBarrierService succeeds. Nil when seal is disabled or
	// the barrier could not be constructed — all stores fall back to plaintext.
	dataPlaneBarrier *qrdbsdk.StorageBarrier

	// dataPlaneEncryptor is the single atrest.Encryptor built from
	// dataPlaneBarrier. It is threaded into every persistent Pebble store
	// (KV shards, time-series, vector) so their sstable/WAL/manifest files
	// are transparently encrypted at rest. A nil barrier produces a no-op
	// Encryptor; plaintext operation is preserved when seal is not configured.
	dataPlaneEncryptor *qrdbsdk.AtRestEncryptor

	// cachedDataPlaneBarrierSvc holds the BaasBarrierService built by
	// initDataPlaneEncryptor for injection into the sharded client in
	// startService. Avoids rebuilding the barrier twice.
	cachedDataPlaneBarrierSvc BaasBarrierService

	// dataPlaneBarrierCloser closes the PebbleDB seal sub-store that backs
	// the data-plane StorageBarrier. Non-nil when buildDataPlaneBarrierService
	// successfully opened the seal store in startService(). Must be closed
	// during Shutdown to release the PebbleDB file locks.
	dataPlaneBarrierCloser io.Closer

	// bootstrapSealPassphrase is a copy of the operator's seal passphrase
	// retained for the first-boot bootstrap orchestrator
	// (qrdbsdk.BootstrapSystem), which initializes a still-sealed
	// passphrase-strategy system barrier during daemon init. The node builder
	// consumes and zeroizes opts.sealPassphrase before first-boot runs, so
	// this copy carries the credential across that gap. It is zeroized as
	// soon as first-boot completes.
	bootstrapSealPassphrase []byte

	// ceremonyServices holds the bootstrap ceremony service stack
	// (credential, ceremony, and the underlying core ceremony.CeremonyService).
	// Populated during startService() via buildBootstrapCeremonyServices.
	// The SO identity is CSR-based; the server never holds officer private
	// keys. Nil when ceremony wiring is unavailable for the current
	// transport (network-only modes that don't run the init flow).
	ceremonyServices *CeremonyServices

	// ceremonyCA is the late-bound qrdbsdk.CeremonyCA supplied to the embedded
	// qrdb node (via qrdbsdk.WithCeremonyCA) at construction time and resolved
	// to the platform xkms CA once initializeCA has run. See ceremony_ca.go for
	// why the binding must be late.
	ceremonyCA *lateBoundCeremonyCA

	// pinBackend is the SO/user PIN backend injected into the embedded qrdb
	// node (qrdbsdk.WithPINBackend). Held here so the init ceremony can enroll
	// the operator-supplied PINs into the very backend the node verifies
	// against — see EnrollInitPINs.
	pinBackend qrdbsdk.PINBackend

	// bootstrapCeremonyClient is the in-process qrdb SDK client built over
	// the bootstrap ceremony service stack via the qrdb embedded transport.
	// Non-nil ONLY when the embedded qrdb node runs without a seal barrier
	// (seal disabled) — in that state the node's own SDK client carries no
	// Barrier/Ceremony/Credential servicers, so barrier, ceremony, and
	// credential verbs on every transport are served through this client
	// instead (see startService and buildBarrierCeremonyServices). It wraps
	// the exact servicer instances stored in ceremonyServices, keeping all
	// transports on one barrier with no split-brain state.
	bootstrapCeremonyClient *qrdbsdk.Client

	// tenantSDKClient is the in-process qrdb SDK client whose embedded
	// transport carries the node's TenantServicer. It backs the per-tenant
	// barrier facade shared by every transport (see tenant_barrier_wiring.go).
	// The node's own SDK client does not register a tenant servicer on its
	// embedded transport, so quicbaas composes this dedicated client, built
	// lazily on first use during the single-threaded boot and closed during
	// Shutdown.
	tenantSDKClient *qrdbsdk.Client

	// sealedWindowClient is the in-process qrdb SDK client composed over the
	// sealed node's OWN Barrier/Ceremony/Credential servicers during the
	// sealed-window boot phase (see sealed_window.go). Non-nil ONLY while
	// the sealed window is being served; closed and cleared by
	// stopSealedTransports before the full boot resumes.
	sealedWindowClient *qrdbsdk.Client

	// sealedWindowFacade is the minimal service.QuicBaaS facade the
	// sealed-phase transport servers are composed over. Non-nil only while
	// the sealed window is being served.
	sealedWindowFacade service.QuicBaaS

	// bootstrapAdminUserID and bootstrapAdminTenantID identify the pending
	// admin user created by firstBootBootstrap, letting later admin binding
	// paths find the right user without re-querying the identity store.
	// Empty before the first-boot path runs, or on subsequent boots where
	// the admin already exists.
	bootstrapAdminUserID   string
	bootstrapAdminTenantID string

	// Raft node - standalone no-op implementation for BaaS client mode.
	// Satisfies interfaces.RaftNode for the service builder without
	// requiring real Raft consensus infrastructure.
	raftNode interfaces.RaftNode

	// qrdbNode is the real embedded qrdb engine used in standalone mode.
	// When non-nil (standalone path only), it owns the storage backend,
	// seal barrier, at-rest encryptor, and all KV/MVCC/TS/Vector services.
	// The data-plane encryptor, barrier, and barrier closer on this Server
	// are NOT populated for standalone — the node owns all of those.
	// Nil for cluster mode (until Phase C).
	qrdbNode *qrdbnode.Node

	// nodeAuthorizer is the embedded node's composed RBAC[+ABAC] authorizer,
	// recorded at node construction by nodeAuthorizerCapture. Read through
	// capturedNodeAuthorizer; nil until the node is built.
	nodeAuthorizer atomic.Pointer[qrdbsdk.Authorizer]

	// qrdbAuditSink is the compliance-grade audit sink injected into the
	// embedded qrdb node (qrdb_node.go newQRDBAuditSink,
	// qrdbsdk.WithAuditLogger) — the ONE audit plane every module/emit site
	// (quicbaas's own IAM/OIDC/federation/orchestration/PIV-login/
	// introspection auditing, and go-xkms's module) is migrating onto
	// (P5.2 of the login-plane plan). Retained here so Shutdown can close it
	// after the node (no in-flight emit races the close) and so the audit
	// read-back path can Query it directly.
	qrdbAuditSink qrdbsdk.NodeAuditLogger

	// raftClusterAdvertiser is the running Noise NK bootstrap server started
	// during Raft cluster-TLS auto-enrollment on the first node of a new
	// cluster. Non-nil only when the software or hardware enrollment path ran
	// and this node bootstrapped the cluster CA (rather than joining). Must be
	// stopped in Shutdown before the qrdb node closes.
	raftClusterAdvertiser interface{ Stop() }

	// clusterCAConfig is the CA hierarchy (root + dedicated cluster
	// intermediate) the cluster ACME first-node pre-stage built and the runtime
	// CA (initializeCA) loads. It is built once during cluster provisioning so
	// the pre-stage and the runtime CA agree on identities and the injected
	// KeyProtection keystore password. nil except on a first node with cluster
	// ACME enrollment enabled.
	clusterCAConfig *xkmssdk.TrustedCAMultiIdentityConfig

	// clusterCAKeyStrategy is the cluster CA key custody strategy (barrier / hsm
	// / tpm-kek) built during cluster ACME provisioning. It is stateful (the
	// barrier strategy retains its bootstrap key-storage handle for promotion),
	// so the SAME instance is used for the pre-node BootstrapBackends and the
	// post-node RuntimeBackend initializeCA signs from — so any node can issue.
	// nil except when cluster ACME enrollment provisioned the cluster mTLS.
	clusterCAKeyStrategy clustercasdk.ClusterCAKeyStrategy

	// secretRegistry is the unified secret resolver registry built during
	// server initialization after the qrdb node is online. It dispatches
	// credential://, kms://, and remote-kms:// resolution. Nil
	// before initializeCA runs. Accessible via GetSecretRegistry().
	secretRegistry *qrdbsecret.ResolverRegistry

	// sealStrategy is the hardware/cloud seal strategy built ONCE during node
	// init (buildSealStrategyOnce) and injected into the embedded qrdb node via
	// WithSealStrategy. Nil for software/none/shamir seal (the node builds its
	// own). It owns the underlying TPM/HSM backend; do not close it separately —
	// the qrdb node releases it on shutdown.
	sealStrategy qrdbsdk.SealingStrategy

	// sealTPM2Backend is the open TPM2 backend held inside sealStrategy when the
	// seal strategy is TPM-backed. It is shared with the PIN backend (P3) so the
	// TPM is opened EXACTLY ONCE for both seal and PIN verification. Nil when the
	// seal strategy is not TPM-backed. Same instance as inside sealStrategy — not
	// closed independently.
	sealTPM2Backend *xkmssdk.TPM2Backend

	// Service facade - used by all API servers
	quicBaaSService service.QuicBaaS

	// leaseManager is the single lease authority in the process: the dynamic
	// secrets service issues credential leases through it, and the agent
	// account credential lifecycle leases its provider-credential DELIVERIES
	// through the same one, so both are visible to the same ListLeases and
	// revocable by the same RevokeLease. Nil until startDynamicSecrets runs.
	leaseManager lease.LeaseManager

	// generatorRegistry holds the dynamic secrets credential generators.
	// Nil until startDynamicSecrets runs.
	generatorRegistry *credentials.GeneratorRegistry

	// sdlc is the SDLC module's runtime, nil when the module is disabled or
	// has not been started. One value rather than loose fields: its parts are
	// meaningless apart, so a single nil check decides availability.
	sdlc *sdlcRuntime

	// API servers
	grpcServer       *ggrpc.Server
	grpcListener     net.Listener
	grpcUnixListener net.Listener // Unix domain socket listener for gRPC
	grpcAPI          *grpcapi.Server
	restServer       *restapi.Server

	// Unified-API-server module bindings (pkg/apimodule): lazily-constructed
	// non-listening REST/MCP servers backing the module surfaces when the
	// legacy listeners are disabled. See ModuleRESTServer / ModuleMCPServer.
	moduleREST     *restapi.Server
	moduleRESTOnce sync.Once
	moduleMCP      *mcpapi.Server
	moduleMCPOnce  sync.Once

	// Health checking
	health *HealthChecker

	// Messaging and WatchBridge components
	messagingService *messaging.PushService
	watchBridge      *watchbridge.WatchBridge

	// QRDB sharded client - manages per-shard SDK clients for multi-shard routing.
	// Each shard gets its own embedded-transport SDK client on first access.
	qrdbClient *ShardedClient

	// Embedded agent state — non-nil when the in-process agent runtime is
	// running. Initialized by startEmbeddedFleetNode; cleaned up by
	// stopEmbeddedFleetNode during graceful shutdown.
	embeddedFleetNodeState *embeddedFleetNode

	// sessionWorkspaces is the orchestrator's manager over the projects
	// root, nil until startOrchestration builds one. The embedded fleet node
	// provisions through the same manager, so the holds its sessions take
	// are the ones the concluded-workspace sweep consults.
	sessionWorkspaces *orchestration.WorkspaceManager

	// embeddedDialerRegistry maps FleetNode.Host → bufconn dialer
	// for embedded agents. The remote backend pool consults this registry
	// before attempting a real TCP dial.
	embeddedDialerRegistry *EmbeddedDialerRegistry

	// embeddedBackendDialers caches per-agent ConnDialer closures keyed by
	// agentID. Used internally by installEmbeddedBackendDialer.
	embeddedBackendDialers map[string]func(context.Context, string) (*ggrpc.ClientConn, error)

	// Shutdown coordination
	shutdownChan chan struct{}
	shutdownOnce sync.Once

	// triggerOnce guards the shutdownChan close SEPARATELY from the
	// teardown in Shutdown. The two used to share shutdownOnce, which
	// deadlocked a signal-driven stop during the sealed window (#139):
	// Shutdown consumed the once with the teardown body and then blocked in
	// initWG.Wait() for initialize() to return, while initialize() sat in
	// awaitUnseal() selecting on a shutdownChan that the consumed once
	// guaranteed nobody would ever close.
	triggerOnce sync.Once

	// State tracking
	mu      sync.RWMutex
	started bool
	closed  bool

	// initWG is held for the duration of initialize() (Storage -> ... ->
	// REST, including initializeIAMComponents). Shutdown waits on it before
	// touching any component so a caller that races Shutdown against a
	// still-running Start (e.g. a test that treats GetQRDBNodeClient()!=nil
	// as "fully booted", when it only means the qrdb node itself came up --
	// IAM component init runs after that and still writes to storage) can
	// never close storage out from under an in-flight initialization step.
	initWG sync.WaitGroup

	startTime time.Time
}

// NewServer creates a new QuicBaaS server instance. Optional ServerOption
// values can be provided to inject external components such as a Certificate
// Authority or pre-issued TLS certificate.
func NewServer(cfg *config.Config, opts ...ServerOption) (*Server, error) {
	if cfg == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "server.NewServer",
			Err:  &api.NilInputError{Name: "config"},
		}
	}

	// Contribute QuicBaaS's own permission vocabulary to qrdb's global
	// permission registry. go-qrdb owns the single RBAC policy decision point
	// but does not know about broker-layer domain concepts, so the broker
	// registers them here — at the composition root, before any component
	// starts, any built-in role is seeded, and any transport PEP resolves a
	// permission. Without this, an authz table entry such as
	// attestation:verify names a permission no role can hold and the endpoint
	// is unreachable while appearing wired. See pkg/permission for the single
	// list of contributed permissions.
	quicbaaspermission.Register()

	// Apply functional options
	var options serverOptions
	for _, opt := range opts {
		opt(&options)
	}

	// Validate host-supplied capability modules (WithModule) before anything
	// starts. go-qrdb's own WithModule panics on a duplicate module name from
	// deep inside node construction; catching the mistake here instead lets a
	// downstream host see a typed error at its own composition site.
	if err := validateHostModules(options.hostModules); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "server.NewServer",
			Err:  err,
		}
	}

	// Initialize logger from config
	logConfig := logger.LogConfig{
		Level:     cfg.Logging.Level,
		Format:    cfg.Logging.Format,
		Output:    cfg.Logging.Output,
		File:      cfg.Logging.File,
		Component: "server",
	}

	log, err := logger.New(logConfig)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "server.NewServer",
			Err:  &api.ConfigurationError{Action: "initialize logger", Cause: err},
		}
	}

	log.Info("Initializing QuicBaaS server")
	log.Infof("Version: %s", version.Short())
	log.Infof("Node ID: %d", cfg.Raft.NodeID)

	// Validate FIPS mode if enabled
	if cfg.Security.FIPSMode {
		log.Info("FIPS 140-3 mode enabled")
		if err := validateFIPSMode(log); err != nil {
			return nil, err
		}
	}

	s := &Server{
		config:       cfg,
		logger:       log,
		opts:         options,
		shutdownChan: make(chan struct{}),
		startTime:    time.Now(),
		ceremonyCA:   &lateBoundCeremonyCA{},
	}

	// Create health checker
	s.health = NewHealthChecker(s, log)

	log.Info("Server instance created (components not yet started)")
	return s, nil
}

// Start initializes and starts all server components in order:
// Storage -> TimeSeriesStorage -> Indexes -> KMS -> Secrets -> Vector -> TimeSeries -> RAG -> Embedding -> Catalog -> Backup -> gRPC -> REST -> MCP.
//
// Behavior depends on the Server.Embedded configuration:
//   - Embedded mode (Server.Embedded=true): Returns immediately after initialization.
//     Use this when embedding QuicBaaS as a library.
//   - Standalone mode (Server.Embedded=false): Blocks until shutdown signal is received.
//     Use this when running QuicBaaS as a standalone service.
//
// Storage backends supported: memory, file, pebble.
//
// # Failure contract
//
// Start does NOT unwind on failure. It returns at the first component that
// fails, and every component started before it stays up -- an embedded qrdb
// node, an open audit log, a cache-cleanup goroutine, a lease reaper, a
// scheduler poll loop. A caller that takes the error and walks away leaks
// all of them for the lifetime of its process.
//
// So: a non-nil error from Start MUST be followed by a call to Shutdown.
//
//	if err := srv.Start(); err != nil {
//	    _ = srv.Shutdown(context.Background()) // release the partial start
//	    return err
//	}
//
// Shutdown is safe on a server that never started, on one that started
// fully, and on one that stopped anywhere in between: it releases whatever
// exists and skips whatever does not. The daemon inherits this for free by
// exiting the process, but an embedding application does not.
//
// The alternative -- Start unwinding every failure path itself, so that no
// contract is needed at all -- is a stronger guarantee and remains open.
// It is not taken here because it changes the behaviour of a public entry
// point, which is the owner's call to make (go-quicbaas#62).
func (s *Server) Start() error {
	if err := s.initialize(); err != nil {
		return err
	}

	// In embedded mode, don't block - return immediately
	if s.config.Server.Embedded {
		s.logger.Info("Embedded mode: returning without blocking")
		return nil
	}

	// Setup signal handlers
	setupSignalHandlers(s.TriggerShutdown, s.logger)

	// Block until shutdown signal
	<-s.shutdownChan
	s.logger.Info("Shutdown signal received")

	return nil
}

// getEffectiveMode returns the effective server mode.
// Mode is now REQUIRED - there is no automatic inference from backend.
// When mode is empty, it defaults to standalone mode.
func (s *Server) getEffectiveMode() string {
	if s.config.Server.Mode == "" {
		// Default to standalone mode when not specified
		return config.ServerModeStandalone
	}
	return s.config.Server.Mode
}

// validateServerMode fails closed when the configured server mode is a
// non-empty value outside the recognized set {standalone, cluster,
// devcluster}. An empty mode is permitted and resolves to standalone via
// getEffectiveMode, so it is not rejected here. Unknown non-empty modes
// previously fell through to the standalone branch of the initialization
// switch, masking operator misconfiguration; this guard restores the
// fail-closed contract.
func (s *Server) validateServerMode() error {
	switch s.config.Server.Mode {
	case "",
		config.ServerModeStandalone,
		config.ServerModeCluster,
		config.ServerModeDevCluster:
		return nil
	default:
		return api.NewUnknownServerModeError("server.initialize", s.config.Server.Mode)
	}
}

// initialize performs the actual server initialization.
// This is the single source of truth for component startup order.
// All components are started synchronously in the following order:
// Storage -> Barrier -> TimeSeriesStorage -> Indexes -> KMS -> Secrets -> Vector -> TimeSeries -> RAG -> Embedding -> Catalog -> Backup -> Service -> Messaging -> gRPC -> REST -> MCP.
func (s *Server) initialize() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return api.NewServerStateError("server.initialize", "server already started")
	}
	if s.closed {
		s.mu.Unlock()
		return api.NewServerStateError("server.initialize", "server is closed")
	}
	s.started = true
	s.mu.Unlock()

	// Held until every synchronous startup step below (through
	// initializeIAMComponents) returns, so Shutdown cannot close storage
	// while this is still writing to it -- see the initWG field doc.
	s.initWG.Add(1)
	defer s.initWG.Done()

	// Fail closed on an unrecognized server mode. An empty mode intentionally
	// defaults to standalone (see getEffectiveMode); any non-empty mode outside
	// the valid set {standalone, cluster, devcluster} is a configuration error
	// and must abort startup rather than being silently treated as standalone.
	if err := s.validateServerMode(); err != nil {
		return err
	}

	s.logger.Info("Starting server components...")
	s.logger.Infof("Server mode: %s", s.getEffectiveMode())
	s.logger.Infof("Storage backend: %s", s.config.Storage.Backend)

	// Emit a loud startup warning when InsecureDevMode is active. This flag
	// bypasses bootstrap-password validation and allows weak dev credentials.
	// It must NEVER be set in production. The warning is emitted unconditionally
	// so it appears in every log (stdout + file) even when the log level is set
	// to error.
	if s.config.Auth.InsecureDevMode {
		const insecureDevBanner = "WARNING: auth.insecure_dev_mode=true is active. " +
			"Bootstrap password validation is disabled and weak credentials are permitted. " +
			"This setting MUST NOT be used in production deployments."
		s.logger.Warn(insecureDevBanner)
		s.logger.Warn("WARNING: auth.insecure_dev_mode=true — INSECURE DEV MODE ACTIVE")
	}

	// Start components in order
	if err := s.startStorage(); err != nil {
		return err
	}

	// Initialize the engine and barrier based on server mode.
	//
	// Standalone mode: construct the real qrdb engine via NewStandaloneNode.
	//   The node owns the storage backend, seal barrier, at-rest encryptor,
	//   and all KV/MVCC/TS/Vector services. We do NOT call initBarrier or
	//   initDataPlaneEncryptor for standalone — those would double-build a
	//   barrier that the qrdb node already owns.
	//
	// Cluster mode: construct the real qrdb clustered node via
	//   NewClusteredNode. The node owns barrier, Raft host, at-rest
	//   encryptor, and all services. initBarrier/initDataPlaneEncryptor/
	//   standaloneRaftNode are NOT called — the node owns all of those.
	// One predicate decides multi-node, shared with config validation and the
	// scheduler wiring. devcluster previously fell through to the standalone
	// node while validation and the scheduler both treated it as clustered —
	// so it booted a node with no Raft transaction plane and every firing
	// claim failed against it. A mode is clustered everywhere or nowhere.
	switch {
	case config.IsClusterLikeMode(s.getEffectiveMode()):
		// Cluster mode: build the real qrdb clustered node. The node owns
		// the Raft host, seal barrier, at-rest encryptor, and all services.
		// s.raftNode is left nil — the service builder and backup manager
		// both accept nil raftNode when qrdbClient is provided.
		if err := s.initClusteredQRDBNode(); err != nil {
			return err
		}
		s.logger.Info("Cluster mode: real qrdb clustered node started")
	default:
		// Standalone mode: build the real qrdb engine. The node manages its
		// own barrier + at-rest; quicbaas does NOT duplicate those here.
		// s.raftNode is left nil — the service builder and backup manager
		// both accept nil raftNode when qrdbClient is provided.
		if err := s.initStandaloneQRDBNode(); err != nil {
			return err
		}
		s.logger.Info("Standalone mode: real qrdb engine started")
	}

	// Generate TLS certificates if needed (auto-generate on first startup).
	// CA initialization runs AFTER the qrdb node is online so that the
	// credential store is available for RootKeyPassword secret resolution.
	// It also runs BEFORE the sealed-window phase: the CA is file-backed
	// under the data dir, and the sealed-window listeners need the TLS
	// material to serve the unseal flow.
	if err := initializeCAFn(s); err != nil {
		return err
	}

	// Resolve the late-bound ceremony CA now that the platform CA exists. The
	// embedded qrdb node was constructed with this wrapper (see
	// buildXKMSCryptoOptions), so from here on every ceremony verb — officer
	// CSR signing, TLS serving cert issuance, CA bundle / SPKI pin — issues
	// from the single platform CA instead of failing closed.
	s.bindCeremonyCA()

	// Sealed boot mode: when the embedded qrdb node booted with seal
	// enabled and no auto-unseal material, it is SEALED — no data plane, no
	// SDK client. Instead of dying (or panicking at the first GetKVStore),
	// serve the sealed window: all five transports come up with ONLY the
	// canonical barrier/ceremony/health surface reachable, and initialize()
	// blocks here until an operator drives the barrier unsealed through any
	// transport. runSealedWindow completes the node boot (node.Unseal) and
	// tears the sealed listeners down before returning, so the full boot
	// below proceeds against a fully-booted node. See sealed_window.go.
	if node := s.getQRDBNode(); node != nil && node.IsSealed() {
		if err := s.runSealedWindow(); err != nil {
			return err
		}
	}

	if err := s.startIndexes(); err != nil {
		return err
	}

	// Initialize KMS if enabled
	if err := s.startKMS(); err != nil {
		return err
	}

	// Initialize Secrets if enabled
	if err := s.startSecrets(); err != nil {
		return err
	}

	// Initialize Vector service (uses pre-created storage)
	if err := s.startVector(); err != nil {
		return err
	}

	// Initialize Time Series service (uses pre-created storage)
	if err := s.startTimeSeries(); err != nil {
		return err
	}

	// Initialize RAG system if enabled
	if err := s.startRAG(); err != nil {
		return err
	}

	// Initialize embedding manager for vector embedding generation
	if err := s.startEmbedding(); err != nil {
		return err
	}

	// Initialize Ollama service for LLM embedding and model management
	if err := s.startOllama(); err != nil {
		return err
	}

	// Initialize catalog manager if object storage is configured
	if err := s.startCatalogManager(); err != nil {
		return err
	}

	// Initialize backup manager if enabled
	if err := s.startBackupManager(); err != nil {
		return err
	}

	// Create QuicBaaS facade before API servers
	if err := s.startService(); err != nil {
		return err
	}

	// The secrets backend upgrade to the persistent KV store happens INSIDE
	// startService, after the qrdb client is created and before the facade
	// captures the manager (issue #138). Upgrading here — after the facade
	// is built — reached no client-facing consumer: the facade kept serving
	// the boot-window in-memory manager, secrets died with the process, and
	// the SDLC module refused webhooks whose secrets REST read back fine.

	// Initialize database management subsystem (requires service facade)
	if err := s.startDatabases(); err != nil {
		return err
	}

	// Compose dynamic secrets over the durable qrdb-backed KV. AFTER the
	// facade (it is attached to it) and BEFORE orchestration, which leases the
	// agent pool's provider-credential deliveries through the same lease
	// manager this creates.
	if err := s.startDynamicSecrets(); err != nil {
		return err
	}

	// Initialize messaging system if enabled
	if s.config.Messaging.Enabled {
		if err := s.startMessaging(); err != nil {
			return err
		}
	}

	// The orchestration plane, in the order orchestrationStartup documents.
	for _, step := range s.orchestrationStartup() {
		if err := step.run(); err != nil {
			return err
		}
	}

	if err := s.startGRPCServer(); err != nil {
		return err
	}

	if err := s.startRESTServer(); err != nil {
		return err
	}

	s.logger.Info("All server components started successfully")

	// Log listener info only when not in embedded mode
	if !s.config.Server.Embedded {
		if s.config.Server.REST.Enabled {
			s.logger.Infof("HTTP server listening on %s:%d", s.config.Server.Host, s.config.Server.REST.Port)
		}
		if s.grpcServer != nil {
			s.logger.Infof("gRPC server listening on %s:%d", s.config.Server.Host, s.config.Server.GRPC.Port)
		}
	} else {
		s.logger.Info("Running in embedded mode - access via Server.Service()")
	}

	return nil
}

// Shutdown gracefully shuts down all server components in reverse order:
// SDLC -> Pollers -> Sweeps -> Job drain -> Workers -> Backend -> Catalog -> Backup -> Embedding -> RAG -> WatchBridge -> Messaging -> MCP -> REST -> gRPC -> Indexes -> Service (pollers) -> RaftNode -> BootstrapClient -> ShardedClient -> QRDBNode -> DevCluster -> TimeSeriesStorage.
//
// The orchestration job drain MUST come after everything that starts jobs
// and before everything that closes a store: a control loop still writing
// when its store closes meets a panicking engine, not an error.
//
// The QuicBaaS service facade MUST close before the sharded qrdb client
// and the embedded qrdb node: the facade owns background pollers (the
// scheduler engine's GetJobsDueForExecution loop and the KMS rotation
// scanner) whose storage transport is the sharded client. Closing the
// facade first stops and drains those goroutines while their transport
// is still open, preventing a poll-after-close error storm.
//
// # After a partial start
//
// Shutdown is the other half of Start's failure contract (see Start): it
// is safe, and required, after a Start that returned an error. Every step
// below is guarded on the resource it tears down, so a component that a
// failed Start never reached is skipped rather than dereferenced -- and
// two resources whose intended owner may never have been built are
// released here rather than by that owner:
//
//   - the cache manager, when startService failed before the facade that
//     owns it was built (below);
//   - the qrdb audit sink and the seal strategy, when node construction
//     failed after they were built (stopQRDBNode);
//   - the KMS manager, which has no other owner in any case: startKMS
//     builds it and nothing downstream closes it, so its key-cache
//     cleanup goroutine outlives a clean shutdown as readily as a failed
//     start unless this releases it (below);
//   - the tenant database manager, likewise owner-less: startDatabases
//     builds it and its OpenAll holds every recovered tenant database
//     open, so those handles outlive a clean shutdown too (below).
//
// Two partial-start resources are deliberately NOT in that list, because
// no guard here could reach either one; both are fixed at their source
// instead.
//
// A failed messaging start used to strand its notification hub:
// startMessaging assigns s.messagingService only after PushService.Start
// returns, so on the failing path the field is still nil and there is
// nothing for a guard to test. PushService.Start now cancels and joins the
// hub goroutine before returning an error, so there is nothing left for
// this to release.
//
// The service facade's job scheduler used to be started from a goroutine,
// which put it outside this function's reach in a subtler way: not nil,
// but not yet started. A Shutdown that ran before that goroutine was
// scheduled called Stop on an engine that was not running, got "not
// running" back, and then watched the queued Start bring a poll loop up
// against storage this had already closed. The builder now starts the
// engine synchronously (SchedulerEngine.Start launches its own loop and
// returns, so nothing is lost by it), and SchedulerEngine.Stop is terminal
// even when it arrives first -- so a late Start refuses rather than
// resurrecting the loop. Ordering can no longer decide whether it leaks.
//
// A resource added to a start path is only covered by this contract once
// Shutdown can reach it WITHOUT the component that normally owns it. That
// property is asserted in two places, split by what each tier can run: the
// release itself in pkg/server/start_partial_failure_cleanup_test.go, which
// hands this function each orphan directly and never calls Start (so it can
// stay in memory), and the orphan windows themselves in
// test/integration/lifecycle, which drives a real Start over a real store.
func (s *Server) Shutdown(ctx context.Context) error {
	var shutdownErr error

	// Signal FIRST, tear down second. The initWG.Wait below blocks until a
	// still-running initialize() returns — and during the sealed window
	// initialize() is itself blocked in awaitUnseal() waiting on
	// shutdownChan. Without this signal that pair is a deadlock, measured
	// as SIGTERM/SIGINT being ignored while sealed (#139): the daemon could
	// only be killed.
	s.TriggerShutdown()

	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.closed = true
		s.mu.Unlock()

		// Wait for any in-flight initialize() to fully return before
		// touching a single component. A caller racing Shutdown against a
		// still-running Start (see the initWG field doc) would otherwise
		// close storage while initialize() is mid-step -- e.g. pebble
		// closed under an in-progress initializeIAMComponents write, which
		// panics the whole test binary rather than merely failing a test.
		s.initWG.Wait()

		s.logger.Info("Initiating graceful shutdown...")
		startTime := time.Now()

		// Create error collector
		var errs []error

		// Stop the SDLC module first, so its registered seams answer
		// unavailable before the stores beneath them go away.
		s.stopSDLC()

		// The task-source poller creates tasks through the facade and the
		// dispatcher starts jobs through it; stop both before the facade and
		// the stores beneath them go away.
		s.stopTaskSourcePoller()
		s.stopTaskDispatcher()

		// Stop the job sweep before the stores it reads leases and tasks
		// from go away.
		s.stopOrchestrationSweeps()
		s.stopContainerReaper()
		s.stopWorkspaceSweep()

		// Nothing starts new jobs from here on, so the running ones can be
		// drained: they get a bounded window to conclude while the stores
		// they write to are still open. What does not fit is left running
		// and resumed on the next start; see drainOrchestrationJobs.
		s.drainOrchestrationJobs()

		// Shutdown background workers first (before catalog manager)
		if s.workerManager != nil {
			s.logger.Info("Shutting down background workers...")
			if err := s.stopWorkerManager(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown worker manager")
			} else {
				s.logger.Info("Background workers shutdown complete")
			}
		}

		// Shutdown backend manager (before catalog manager)
		if s.backendManager != nil {
			s.logger.Info("Shutting down backend manager...")
			if err := s.stopBackendManager(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown backend manager")
			} else {
				s.logger.Info("Backend manager shutdown complete")
			}
		}

		// Shutdown catalog manager
		if s.catalogManager != nil {
			s.logger.Info("Shutting down catalog manager...")
			if err := s.stopCatalogManager(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown catalog manager")
			} else {
				s.logger.Info("Catalog manager shutdown complete")
			}
		}

		// Shutdown backup manager (before RAG and storage)
		if s.backupManager != nil {
			s.logger.Info("Shutting down backup manager...")
			if err := s.stopBackupManager(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown backup manager")
			} else {
				s.logger.Info("Backup manager shutdown complete")
			}
		}

		// Shutdown vector storage (before embedding and RAG cleanup)
		if s.vectorStorage != nil {
			s.logger.Info("Shutting down vector storage...")
			if err := s.vectorStorage.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown vector storage")
			} else {
				s.logger.Info("Vector storage shutdown complete")
			}
		}

		// Shutdown embedding manager (before RAG and vector storage)
		if s.embeddingManager != nil {
			s.logger.Info("Shutting down embedding manager...")
			if err := s.stopEmbedding(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown embedding manager")
			} else {
				s.logger.Info("Embedding manager shutdown complete")
			}
		}

		// Shutdown Ollama service (before RAG system)
		if s.ollamaService != nil {
			s.logger.Info("Shutting down Ollama service...")
			if err := s.stopOllama(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown Ollama service")
			} else {
				s.logger.Info("Ollama service shutdown complete")
			}
		}
		// Shutdown RAG system (before vector storage)
		if s.ragSystem != nil {
			s.logger.Info("Shutting down RAG system...")
			if err := s.stopRAG(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown RAG system")
			} else {
				s.logger.Info("RAG system shutdown complete")
			}
		}

		// Shutdown embedded agent before external-facing components.
		// The embedded agent must drain in-flight sessions before the
		// transport servers shut down so the orchestrator's session state
		// remains intact during teardown.
		if s.embeddedFleetNodeState != nil {
			s.logger.Info("Shutting down embedded agent...")
			s.stopEmbeddedFleetNode(ctx)
			s.logger.Info("Embedded agent shutdown complete")
		}

		// Shutdown watch bridge (before messaging service)
		if s.watchBridge != nil {
			s.logger.Info("Shutting down watch bridge...")
			if err := s.watchBridge.Stop(ctx); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown watch bridge")
			} else {
				s.logger.Info("Watch bridge shutdown complete")
			}
		}

		// Shutdown messaging service
		if s.messagingService != nil {
			s.logger.Info("Shutting down messaging service...")
			if err := s.messagingService.Stop(ctx); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown messaging service")
			} else {
				s.logger.Info("Messaging service shutdown complete")
			}
		}

		// Shutdown REST server
		if s.getRESTServer() != nil {
			s.logger.Info("Shutting down REST server...")
			if err := s.stopRESTServer(ctx); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown REST server")
			} else {
				s.logger.Info("REST server shutdown complete")
			}
		}

		// Shutdown gRPC server
		if s.grpcServer != nil {
			s.logger.Info("Shutting down gRPC server...")
			s.stopGRPCServer()
			s.logger.Info("gRPC server shutdown complete")
		}

		// Stop the lease reaper. The leases themselves are durable and stay:
		// a credential delivered before a restart must still be governed
		// after it.
		if err := s.stopDynamicSecrets(); err != nil {
			errs = append(errs, err)
			s.logger.WithError(err).Error("Failed to shutdown dynamic secrets subsystem")
		}

		// Shutdown indexes
		if err := s.stopIndexes(); err != nil {
			errs = append(errs, err)
			s.logger.WithError(err).Error("Failed to shutdown index manager")
		}

		// Close the QuicBaaS service facade BEFORE the qrdb client and
		// node teardown. quicBaaS.Close stops and drains the scheduler
		// engine poll loop and the KMS rotation scanner (both poll qrdb
		// storage through the sharded client), then closes service-owned
		// resources (cache manager, SSH client, its idempotent reference
		// to the sharded client). Without this, the pollers outlive the
		// storage transport and spam transport-closed errors until the
		// process exits.
		if s.quicBaaSService != nil {
			s.logger.Info("Shutting down QuicBaaS service facade...")
			if err := s.quicBaaSService.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown QuicBaaS service facade")
			} else {
				s.logger.Info("QuicBaaS service facade shutdown complete")
			}
		} else if s.cacheManager != nil {
			// The cache manager is created inside startService BEFORE the
			// facade that owns it, and it starts a cleanup goroutine the
			// moment it exists. Every startService failure between the two
			// -- and a builder.Build() failure itself -- leaves it running
			// with the facade that would have closed it never built, so
			// this is the only call that can reach it.
			//
			// Only in that case: CacheManager.Close closes an unbuffered
			// stop channel, so closing one the facade has already closed
			// would panic the caller of Shutdown.
			s.logger.Info("Releasing cache manager orphaned by a partial start...")
			if err := s.cacheManager.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close orphaned cache manager")
			}
			s.cacheManager = nil
		}

		// Close the tenant database manager, AFTER the facade above: the
		// facade's DatabaseService routes tenant database calls into this
		// manager, so closing it first could pull an open database out from
		// under an in-flight request the facade's own Close is still
		// draining.
		//
		// Unconditional on the manager alone, not on the facade, because
		// this is the orphan case as much as the clean one. startDatabases
		// builds the manager, calls OpenAll -- which opens every tenant
		// database found on disk -- and only then attaches a DatabaseService
		// to the facade. Nothing downstream ever closes it: the facade's
		// Close reaches its cache manager, backup manager, storage, qrdb
		// client and SSH client, and stops there. So a Start that failed at
		// any later step, and equally a Start that fully succeeded, left
		// every recovered database open until the process exited. This call
		// is the only one that can reach them.
		//
		// Nil'd afterwards: Manager.Close deletes each database from its
		// registry as it closes it, so a second call is already a no-op, but
		// dropping the reference keeps "released" observable and matches the
		// other release paths here.
		if s.databaseManager != nil {
			s.logger.Info("Closing tenant database manager...")
			if err := s.databaseManager.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close tenant database manager")
			} else {
				s.logger.Info("Tenant database manager closed")
			}
			s.databaseManager = nil
		}

		// Shutdown raft node when present (legacy / test paths only;
		// the qrdb-node path leaves s.raftNode nil).
		if s.raftNode != nil {
			s.logger.Info("Shutting down raft node...")
			if err := s.raftNode.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown raft node")
			} else {
				s.logger.Info("Raft node shutdown complete")
			}
		}

		// Release sealed-window resources when the daemon is shut down while
		// the sealed window is still being served (the transport listeners
		// themselves were already stopped by the regular sequence above,
		// which operates on the same server fields). No-op after a completed
		// sealed window or a normal full boot.
		if s.sealedWindowFacade != nil {
			if err := s.sealedWindowFacade.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close sealed-window facade")
			}
			s.sealedWindowFacade = nil
		}
		if s.sealedWindowClient != nil {
			if err := s.sealedWindowClient.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close sealed-window client")
			}
			s.sealedWindowClient = nil
		}

		// Close the bootstrap ceremony client (in-process qrdb SDK client
		// over the bootstrap ceremony stack). Non-nil only when the node
		// runs without a seal barrier. Closed before the node because its
		// embedded transport references the node's KV/Admin servicers.
		if s.bootstrapCeremonyClient != nil {
			s.logger.Info("Closing bootstrap ceremony client...")
			if err := s.bootstrapCeremonyClient.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close bootstrap ceremony client")
			} else {
				s.logger.Info("Bootstrap ceremony client closed")
			}
		}

		// Close the tenant-barrier SDK client (in-process qrdb SDK client
		// over the node's TenantServicer). Closed before the node because
		// its embedded transport references the node's KV/Admin servicers.
		if s.tenantSDKClient != nil {
			s.logger.Info("Closing tenant barrier client...")
			if err := s.tenantSDKClient.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close tenant barrier client")
			} else {
				s.logger.Info("Tenant barrier client closed")
			}
			s.tenantSDKClient = nil
		}

		// Shutdown sharded qrdb client
		if s.qrdbClient != nil {
			s.logger.Info("Shutting down sharded qrdb client...")
			if err := s.qrdbClient.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown sharded qrdb client")
			} else {
				s.logger.Info("Sharded qrdb client shutdown complete")
			}
		}

		// Stop the Raft cluster enrollment Noise NK advertiser. Non-nil
		// only on the first node that bootstrapped the cluster CA; must be
		// stopped before the qrdb node so no enrollment requests arrive
		// during teardown.
		if s.raftClusterAdvertiser != nil {
			s.logger.Info("Stopping Raft cluster enrollment advertiser...")
			s.raftClusterAdvertiser.Stop()
			s.logger.Info("Raft cluster enrollment advertiser stopped")
		}

		// Close the standalone qrdb node (standalone mode only). The node
		// owns the storage backend, seal barrier, and at-rest encryptor; it
		// must be closed after the sharded client (which holds references to
		// the node's services) and before the data-plane barrier closer
		// (which is nil in standalone mode since the node owns the barrier).
		//
		// Called unconditionally: stopQRDBNode also releases the node audit
		// sink and the seal strategy, both of which are built BEFORE the
		// node and therefore outlive a Start that failed during node
		// construction. A `if node != nil` guard here would skip the only
		// call that can reach them.
		if err := s.stopQRDBNode(); err != nil {
			errs = append(errs, err)
			s.logger.WithError(err).Error("Failed to shutdown embedded qrdb node")
		}

		// Close the data-plane barrier: its PebbleDB seal sub-store AND the
		// sealing strategies built over it, both owned by the single closer
		// buildDataPlaneBarrierService returns (dataPlaneBarrierResources).
		// This must happen after the sharded client is closed (the client
		// holds the barrier service reference) so the PebbleDB file locks are
		// released cleanly.
		//
		// The strategies are in that closer rather than beside it because
		// they had no owner at all before: with a hardware seal strategy
		// configured and node sealing off, startService builds a strategy
		// here that holds an open TPM backend, and it is distinct from the
		// node's own s.sealStrategy that stopQRDBNode releases above. A
		// failed Start left it open for the life of the process.
		//
		// Nil'd afterwards so a second Shutdown cannot re-close it, matching
		// the other release paths here and keeping "released" observable.
		if s.dataPlaneBarrierCloser != nil {
			closer := s.dataPlaneBarrierCloser
			s.dataPlaneBarrierCloser = nil
			s.logger.Info("Closing data-plane barrier seal store and strategies...")
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close data-plane barrier")
			} else {
				s.logger.Info("Data-plane barrier closed")
			}
		}

		// Shutdown DevCluster manager
		if s.devClusterMgr != nil {
			s.logger.Info("Shutting down DevCluster manager...")
			if err := s.stopDevCluster(ctx); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown DevCluster manager")
			} else {
				s.logger.Info("DevCluster manager shutdown complete")
			}
		}

		// Shutdown time series storage (before main storage)
		if err := s.stopTimeSeries(); err != nil {
			errs = append(errs, err)
			s.logger.WithError(err).Error("Failed to shutdown time series storage")
		}

		// Close the KMS manager LAST, once every consumer of it has stopped.
		// It has exactly two: the service facade (which builds the crypto
		// manager and the KMS rotation scanner from it) and the catalog
		// reencrypt worker's DEK provider. Both are torn down above, so no
		// key operation can be routed into a closed manager from here on.
		//
		// This is the only call that reaches it. startKMS creates the
		// manager, and with KMS.Cache.Enabled it carries a KeyCache whose
		// cleanupLoop goroutine runs from the moment the manager exists --
		// but nothing else in the tree closes it: the facade's Close does
		// not, MemoryCryptoManager.Close is a no-op, and KMSDEKProvider only
		// borrows the reference. So the goroutine outlived BOTH a partial
		// start and a clean one until this call existed.
		//
		// Unconditional teardown is safe: KMSService.Close closes the cache
		// (joining cleanupLoop) and then each registered backend, and
		// XKMSBackend.Close is a documented no-op because xkms lifecycle is
		// global -- so this cannot pull the CA's shared key storage out from
		// under anything. Nil'd afterwards so a second Shutdown cannot
		// re-close the cache, which would return ErrCacheClosed.
		if s.kmsManager != nil {
			s.logger.Info("Closing KMS manager...")
			if err := s.kmsManager.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close KMS manager")
			} else {
				s.logger.Info("KMS manager closed")
			}
			s.kmsManager = nil
		}

		// Release the process-global xkms service after the KMS manager, its
		// last consumer. Its backends hold key storage rooted in THIS
		// server's data dir; left registered, the next server in the same
		// process (init followed by start, or one test after another) found
		// it already initialized, kept it, and wrote that server's OIDC
		// signing key into the previous server's data dir -- re-creating it
		// after it was removed (#714). Only a service this server created is
		// closed, and ownership is dropped so a second Shutdown is a no-op.
		if s.ownsXKMS {
			s.ownsXKMS = false
			s.logger.Info("Closing XKMS global service...")
			if err := closeXKMSFn(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to close XKMS global service")
			} else {
				s.logger.Info("XKMS global service closed")
			}
		}

		duration := time.Since(startTime)
		s.logger.Infof("Graceful shutdown completed in %v", duration)

		if len(errs) > 0 {
			shutdownErr = api.NewShutdownError("server.Shutdown", errs)
		}
	})

	return shutdownErr
}

// TriggerShutdown signals shutdown to everything waiting on it: Start's
// post-boot wait and the sealed window's awaitUnseal both select on
// shutdownChan. Idempotent; it performs no teardown itself — that is
// Shutdown's job, and Shutdown calls this first.
func (s *Server) TriggerShutdown() {
	s.triggerOnce.Do(func() {
		// Nil only on a hand-assembled Server (white-box tests); NewServer
		// always allocates the channel. Nothing can be waiting on a channel
		// that never existed, so there is nothing to signal.
		if s.shutdownChan != nil {
			close(s.shutdownChan)
		}
	})
}

// IsStarted returns true if the server has been started.
func (s *Server) IsStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// IsClosed returns true if the server has been closed.
func (s *Server) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// getQRDBNode returns the current qrdb node, or nil before Start() reaches
// node construction and after stopQRDBNode clears it. Guarded by s.mu: the
// node is legitimately read (RESTAddr, health checks, IAM wiring, tenant
// KV routing) by callers that may run concurrently with the write in
// initStandaloneQRDBNode/initClusteredQRDBNode -- e.g. a caller polling
// RESTAddr() while Start() is still initializing, which is a deliberately
// supported pattern (server_test.go's sealed-boot tests), not a bug in the
// caller. Runner-log-proven data race (go-quicbaas task 2981/2972): the
// unguarded field read in RESTAddr() raced the unguarded write in
// initStandaloneQRDBNode.
func (s *Server) getQRDBNode() *qrdbnode.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.qrdbNode
}

// setQRDBNode sets (or clears, with nil) the current qrdb node under the
// same lock getQRDBNode reads through.
func (s *Server) setQRDBNode(node *qrdbnode.Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.qrdbNode = node
}

// getRESTServer and setRESTServer are s.restServer's equivalent of
// getQRDBNode/setQRDBNode -- same rationale (runner-log-proven data race,
// go-quicbaas task 2981/2972, second race exposed once the qrdbNode one was
// fixed: RESTAddr()'s read of s.restServer against
// startSealedRESTServer's/startRESTServer's write, both legitimately
// concurrent with a caller polling RESTAddr() during startup).
func (s *Server) getRESTServer() *restapi.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restServer
}

func (s *Server) setRESTServer(srv *restapi.Server) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restServer = srv
}

// Uptime returns the server uptime.
func (s *Server) Uptime() time.Duration {
	return time.Since(s.startTime)
}

// GetConfig returns the server configuration.
func (s *Server) GetConfig() *config.Config {
	return s.config
}

// GetRaftNode returns the raft node (nil if not yet initialized).
func (s *Server) GetRaftNode() interfaces.RaftNode {
	return s.raftNode
}

// GetRESTServer returns the REST server (for health checks).
func (s *Server) GetRESTServer() *restapi.Server {
	return s.getRESTServer()
}

// RESTAddr returns the actual network address serving REST, or nil when REST
// is disabled, running in embedded mode, or not yet listening. Callers that
// configure the REST port as 0 (ephemeral) must use this to learn the
// OS-assigned port rather than reading config.Server.REST.Port.
//
// REST network serving now runs exclusively on the embedded qrdb node's own
// unified-server listener (see (*config.Config).NetworkServing); s.restServer
// backs only the non-listening handler surface RegisterHTTP binds to (see
// lifecycle_rest.go's startRESTServer), so a live s.restServer.Addr() (the
// sealed-window phase, or a caller-injected listener in tests) takes priority
// and the qrdb node's own reported address is the fallback for the normal
// fully-booted path.
func (s *Server) RESTAddr() net.Addr {
	if srv := s.getRESTServer(); srv != nil {
		if addr := srv.Addr(); addr != nil {
			return addr
		}
	}
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	qsrv := node.Server()
	if qsrv == nil {
		return nil
	}
	addrStr := qsrv.RESTAddr()
	if addrStr == "" {
		return nil
	}
	tcpAddr, err := net.ResolveTCPAddr("tcp", addrStr)
	if err != nil {
		return nil
	}
	return tcpAddr
}

// GetGRPCServer returns the gRPC server (for health checks).
func (s *Server) GetGRPCServer() *ggrpc.Server {
	return s.grpcServer
}

// GetCatalogManager returns the catalog manager for object storage operations.
func (s *Server) GetCatalogManager() catalog.CatalogManager {
	return s.catalogManager
}

// GetBackendManager returns the backend manager for object storage backends.
func (s *Server) GetBackendManager() catalog.BackendManager {
	return s.backendManager
}

// GetWorkerManager returns the worker manager for background task operations.
func (s *Server) GetWorkerManager() *catalog.WorkerManager {
	return s.workerManager
}

// GetDevClusterManager returns the DevCluster manager (nil if not in devcluster mode).
func (s *Server) GetDevClusterManager() *devcluster.Manager {
	return s.devClusterMgr
}

// GetRAGSystem returns the RAG system (nil if not enabled or not compiled).
func (s *Server) GetRAGSystem() *rag.System {
	return s.ragSystem
}

// GetEmbeddingManager returns the embedding manager (nil if not enabled or not compiled).
func (s *Server) GetEmbeddingManager() *embedding.Manager {
	return s.embeddingManager
}

// GetWatchBridge returns the WatchBridge (nil if not enabled).
func (s *Server) GetWatchBridge() *watchbridge.WatchBridge {
	return s.watchBridge
}

// GetBackupManager returns the backup manager (nil if not enabled).
func (s *Server) GetBackupManager() *backup.BackupManager {
	return s.backupManager
}

// CA returns the Certificate Authority (nil if TLS is disabled).
func (s *Server) CA() xkmssdk.TrustedCA {
	return s.xkmsCA
}

// GetCertificateAuthority returns the CA (nil if TLS is disabled).
func (s *Server) GetCertificateAuthority() xkmssdk.TrustedCA {
	return s.xkmsCA
}

// UpdateCA replaces the Certificate Authority used by the QuicBaaS
// CertificateService with the supplied ca. The swap is atomic: in-flight
// certificate operations complete against the previous CA before the new one
// takes effect. Returns an error when the server has not yet been started or
// when the CA adapter was not wired (TLS disabled). Passing nil is a no-op.
func (s *Server) UpdateCA(ca xkmssdk.TrustedCA) error {
	if ca == nil {
		return nil
	}
	if s.caAdapter == nil {
		return UpdateCANotReadyError{}
	}
	s.xkmsCA = ca
	s.caAdapter.SetCA(ca)
	return nil
}

// GetSealManager returns the seal manager (nil if seal is not enabled).
func (s *Server) GetSealManager() interfaces.SealManager { //nolint:staticcheck // SA1019: legacy seal compatibility surface, deliberately retained until the seal API is removed.
	return s.sealManager
}

// SetSealManager sets the seal manager on the server. This is used by
// integration points that construct seal managers externally (e.g., gRPC
// seal service initialization).
func (s *Server) SetSealManager(mgr interfaces.SealManager) { //nolint:staticcheck // SA1019: legacy seal compatibility surface, deliberately retained until the seal API is removed.
	s.sealManager = mgr
}

// GetQRDBClient returns the sharded qrdb client (nil if not initialized).
// Use ForShard(shardID) on the returned client to obtain a per-shard SDK client.
func (s *Server) GetQRDBClient() *ShardedClient {
	return s.qrdbClient
}

// GetQRDBNodeClient returns the real qrdb SDK client exposed by the embedded
// engine node. On the standalone and clustered node paths (Phase B/C) this is
// the in-process client backed by the qrdb embedded transport; all KV, Admin,
// barrier, ceremony, and credential operations route through it without network
// round-trips.
//
// Returns nil when the server has not yet been started or when no qrdb node was
// constructed (e.g. test stubs with only a mock ShardedClient). Callers should
// prefer GetQRDBNodeClient over GetQRDBClient().ForShard(1) for barrier/ceremony
// operations so that the SDK client is used directly rather than via the
// BaasTransport layer.
func (s *Server) GetQRDBNodeClient() *qrdbsdk.Client {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	return node.Client()
}

// GetQRDBKVServicer returns the qrdb service.KVServicer exposed by the embedded
// engine node's service graph. Returns nil when the node is absent.
func (s *Server) GetQRDBKVServicer() qrdbsdk.KVServicer {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil {
		return nil
	}
	return svcs.KV
}

// GetQRDBAdminServicer returns the qrdb service.AdminServicer exposed by the
// embedded engine node's service graph. Returns nil when the node is absent.
func (s *Server) GetQRDBAdminServicer() qrdbsdk.AdminServicer {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil {
		return nil
	}
	return svcs.Admin
}

// GetQRDBUserServicer returns the qrdb user.UserServicer exposed by the
// embedded engine node. Composers wire this into the SDK's embedded qrdb
// transport so user-login factor enrollment can resolve a subject
// (tenant + username) to a uint64 user id against the same user store the
// login ceremony reads. Returns nil when the node is absent.
func (s *Server) GetQRDBUserServicer() qrdbsdk.UserServicer {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	return node.UserService()
}

// GetQRDBUserAuthFactorServicer returns the UserAuthFactor service INSTANCE
// the embedded node's unified-login ceremony resolves against. Wiring this
// exact instance into the SDK's embedded qrdb transport guarantees factor
// ENROLLMENT writes land in the SAME store login READS — closing the
// split-store gap. Returns nil when the node was built without a User
// service (login ceremony not wired) or before Unseal completes.
func (s *Server) GetQRDBUserAuthFactorServicer() qrdbsdk.UserAuthFactorServicer {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	return node.UserAuthFactorService()
}

// GetQRDBTenantServicer returns the qrdb tenant.TenantServicer exposed by
// the embedded engine node's service graph — the same instance the IAM
// tenant lifecycle provisions through. Composers wire this into the SDK's
// embedded qrdb transport (WithEmbeddedQRDB) so TenantMgmt() and the
// per-tenant barrier surface dispatch against the node's canonical tenant
// service. Returns nil when the node is absent or its service graph is not
// yet built (e.g. while sealed).
func (s *Server) GetQRDBTenantServicer() qrdbsdk.ServiceTenantServicer {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil {
		return nil
	}
	return svcs.Tenant
}

// Service returns the QuicBaaS facade for direct database operations. The
// facade is published under s.mu (see startService in lifecycle.go), so this
// reads through quicBaaSServiceRef rather than the bare field — the unified
// embedded qrdb node's transports can begin serving requests before
// startService finishes composing the facade, and this getter is reachable
// from those request goroutines as well as from the main startup path.
func (s *Server) Service() service.QuicBaaS {
	return s.quicBaaSServiceRef()
}

// GetCeremonyServices returns the bootstrap ceremony service stack composed
// during startService() — a credential service, a ceremony service, and the
// underlying core ceremony.CeremonyService. Returns nil when ceremony wiring
// was skipped (for example, on a cluster that does not run the init flow or
// when buildBootstrapCeremonyServices returned an error during startup).
//
// External callers (most importantly the CLI's embedded-server wrapper) use
// this accessor to wire the qrdb-backed embedded transport for the
// Security().Ceremony() / Security().Credentials() SDK accessors via
// WithEmbeddedQRDB. For the barrier specifically, use GetDataPlaneBarrierService
// which returns the real persistent data-plane barrier rather than the
// bootstrap ceremony's ephemeral in-memory barrier.
func (s *Server) GetCeremonyServices() *CeremonyServices {
	return s.ceremonyServices
}

// GetDataPlaneBarrierService returns the canonical data-plane barrier service.
// On the node path (always active), the barrier comes from the qrdb node's
// seal subsystem and is stored in s.ceremonyServices.Barrier during
// startService(). This is the persistent, strategy-aware barrier shared
// across all five transports.
//
// Returns nil when the barrier was not constructed (seal disabled, data dir
// absent, or construction failed). Callers must handle nil gracefully.
func (s *Server) GetDataPlaneBarrierService() BaasBarrierService {
	if s.ceremonyServices == nil {
		return nil
	}
	return s.ceremonyServices.Barrier
}

// GetEffectiveMode returns the effective server mode for external callers.
// This is a public accessor for the private getEffectiveMode method.
func (s *Server) GetEffectiveMode() string {
	return s.getEffectiveMode()
}

// validateFIPSMode validates FIPS mode requirements.
func validateFIPSMode(log *logger.Logger) error {
	log.Info("Validating FIPS 140-3 mode requirements...")

	// Get current FIPS status for logging
	status := fips.GetStatus()

	// Check if Go FIPS mode is enabled (via crypto/fips140.Enabled())
	if !fips.Enabled() {
		log.Warn("FIPS mode requested but not enabled in this build")
		log.Warn("Build with GOFIPS140=v1.0.0 environment variable to enable FIPS mode")
		log.Warnf("Current status - Kernel FIPS: %v, Kernel cmdline fips=1: %v",
			status.KernelFIPSMode, status.KernelCmdline)
		// Don't fail - allow running without FIPS for development
		return nil
	}

	// Log FIPS status for audit trail
	log.Infof("FIPS 140-3 mode enabled (crypto provider: %s)", status.CryptoProvider)
	log.Infof("Kernel FIPS mode: %v, Kernel cmdline fips=1: %v",
		status.KernelFIPSMode, status.KernelCmdline)

	log.Info("FIPS 140-3 mode validation successful")
	return nil
}

// startupStep is one ordered step of bringing the orchestration plane up.
type startupStep struct {
	// name identifies the step to the ordering test.
	name string

	// run performs it.
	run func() error
}

// Names of the orchestration startup steps, in the order they must run.
const (
	startupRegisterSDLC      = "register_sdlc"
	startupOrchestration     = "orchestration"
	startupEmbeddedFleetNode = "embedded_fleet_node"
	startupSDLC              = "sdlc"
	startupRecoverJobs       = "recover_jobs"
	startupTaskDispatcher    = "task_dispatcher"
)

// orchestrationStartup is the orchestration plane's startup, in order.
//
// THE ORDER IS THE CONTRACT, which is why it is a list a test can read:
//
//   - The SDLC module's SEAMS are registered before the engine starts: the
//     stall detector's idle thresholds are validated against the states every
//     registered runner declares, and startOrchestration is where they are set.
//   - The embedded agent is fully started — its startup sweeps done, its fleet
//     registration and dialer installed — BEFORE anything can dispatch. It
//     shares the daemon's projects root, so its sweep of orphaned workspace
//     specs would delete the spec of a session a recovered job had just
//     spawned; and a recovered job pinned to the embedded agent would be
//     refused as "agent unavailable" before its dialer existed (go-quicbaas#981).
//   - The SDLC module's RUNTIME follows the engine, because its hooks and
//     runner are built over the engine's services, and its backstop sweepers
//     dispatch jobs — so it follows the embedded agent too.
//   - Interrupted jobs are recovered once every module's hooks and every
//     executing host are in place.
//   - The task dispatcher starts LAST, after recovery has re-acquired the
//     leases of the jobs it resumed. Started with the engine, its first sweep
//     raced recovery: it saw a task naming a job that "holds no live lease"
//     (recovery had not re-acquired it yet), released the task back to the
//     queue, and later dispatched a SECOND job for it, which superseded the
//     resumed one and threw its in-flight review or turn away. It happened on
//     every restart (deploys of 2026-09-23, go-quicbaas#1064).
func (s *Server) orchestrationStartup() []startupStep {
	var steps []startupStep
	add := func(enabled bool, name string, run func() error) {
		if enabled {
			steps = append(steps, startupStep{name: name, run: run})
		}
	}
	add(s.config.SDLC.Enabled, startupRegisterSDLC, registerSDLCModule)
	add(s.config.Orchestration.Enabled, startupOrchestration, s.startOrchestration)
	add(s.config.Server.EmbeddedFleetNode.Enabled, startupEmbeddedFleetNode, s.startEmbeddedFleetNode)
	add(s.config.SDLC.Enabled, startupSDLC, s.startSDLC)
	add(s.config.Orchestration.Enabled, startupRecoverJobs, func() error {
		s.recoverOrchestrationJobs()
		return nil
	})
	add(s.config.Orchestration.Enabled, startupTaskDispatcher, func() error {
		if s.taskSpawner == nil {
			s.logger.Info("Task dispatcher not started: the agent service cannot spawn a task")
			return nil
		}
		s.startTaskDispatcher(s.taskSpawner)
		return nil
	})
	return steps
}
