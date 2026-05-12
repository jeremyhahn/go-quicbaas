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
// It manages the lifecycle of all components including gRPC, REST, QUIC, and MCP servers.
package server

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	ggrpc "google.golang.org/grpc"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/fips"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
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

// Server orchestrates all QuicBaaS components.
type Server struct {
	config *config.Config
	logger *logger.Logger

	// Functional options provided at construction time
	opts serverOptions

	// Core components
	localBackend *LocalBackend // Used in pebble mode
	fileBackend  *FileBackend  // Used in file mode

	// DevCluster management - coordinates local multi-node development clusters
	devClusterMgr *devcluster.Manager

	// Time series storage - provided via interfaces.TimeSeriesStorage
	timeSeriesStorage interfaces.TimeSeriesStorage

	// KMS and Secrets components
	kmsManager     *kms.KMSService
	secretsManager *secrets.Manager
	vectorService  service.VectorService
	vectorStorage  interfaces.VectorStorage

	// Cache components
	cacheManager *cache.CacheManager

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

	// Agent fleet registry - shared between IAM enrollment (publisher on
	// redeem) and orchestration (publisher on heartbeat + consumer for
	// dispatch). Constructed during IAM init so the enrollment service can
	// publish on redemption; the orchestration layer receives the same
	// instance via SetAgentRegistry so dispatch sees enrollment-time
	// registrations immediately.
	agentRegistry orchestration.AgentRegistry

	// iamComponents holds the initialized IAM subsystem components.
	// Set by lifecycle.go after initializeIAMComponents succeeds and used by
	// wireIAMMTLSAuthenticators to obtain the AppStorage without a runtime
	// type assertion on quicBaaSService.
	iamComponents service.IAMComponents

	// orchestratorDaemonClient holds the OAuth2 credentials the central uses
	// to authenticate its outbound calls to remote agent daemons. Populated
	// by ensureOrchestratorDaemonClient during IAM init; consumed by the
	// orchestration layer's RemoteBackendResolver to mint JWTs for daemon
	// RPCs. See pkg/server/iam_init.go for bootstrap details.
	orchestratorDaemonClient *OrchestratorDaemonClient

	// tenantDEKManager provides per-tenant Data Encryption Keys for
	// at-rest encryption of session events and other tenant-scoped state.
	// Populated during IAM init when KMS is available; nil on restricted
	// deployments that run without a KMS backend.
	tenantDEKManager security.TenantDEKManager

	// tlsConfig holds the in-memory TLS configuration backed by OpaqueKeys.
	// Created by initializeCA() and used by all transport layers.
	tlsConfig *tls.Config

	// Seal management - uses the interfaces.SealManager abstraction backed
	// by qrdb's StorageBarrier via the BarrierSealManager adapter.
	sealManager interfaces.SealManager

	// ceremonyServices holds the bootstrap ceremony service stack
	// (credential, ceremony, and the underlying core ceremony.CeremonyService).
	// Populated during startService() via buildBootstrapCeremonyServices and
	// consumed by the firstBootBootstrap path to bind the SO certificate
	// SPKI thumbprint to the bootstrap admin once an officer completes the
	// ceremony. Nil when ceremony wiring is unavailable for the current
	// transport (network-only modes that don't run the init flow).
	ceremonyServices *CeremonyServices

	// bootstrapAdminUserID and bootstrapAdminTenantID identify the pending
	// admin user created by firstBootBootstrap. BindAdminToSOCert reads these
	// to pin the SO certificate's SPKI thumbprint against the right user
	// without re-querying the identity store. Empty before the first-boot
	// path runs, or on subsequent boots where the admin already exists.
	bootstrapAdminUserID   string
	bootstrapAdminTenantID string

	// Raft node - standalone no-op implementation for BaaS client mode.
	// Satisfies interfaces.RaftNode for the service builder without
	// requiring real Raft consensus infrastructure.
	raftNode interfaces.RaftNode

	// Service facade - used by all API servers
	quicBaaSService service.QuicBaaS

	// Bootstrap components
	discoveryManager *discovery.Manager

	// API servers
	grpcServer       *ggrpc.Server
	grpcListener     net.Listener
	grpcUnixListener net.Listener // Unix domain socket listener for gRPC
	grpcAPI          *grpcapi.Server
	restServer       *restapi.Server
	quicServer       *quicapi.Server
	mcpServer        *mcpapi.Server

	// Health checking
	health *HealthChecker

	// Messaging and WatchBridge components
	messagingService *messaging.PushService
	watchBridge      *watchbridge.WatchBridge

	// QRDB sharded client - manages per-shard SDK clients for multi-shard routing.
	// Each shard gets its own embedded-transport SDK client on first access.
	qrdbClient *ShardedClient

	// Embedded agent state — non-nil when the in-process agent runtime is
	// running. Initialized by startEmbeddedAgent; cleaned up by
	// stopEmbeddedAgent during graceful shutdown.
	embeddedAgentState *embeddedAgent

	// embeddedDialerRegistry maps AgentRegistration.Host → bufconn dialer
	// for embedded agents. The remote backend pool consults this registry
	// before attempting a real TCP dial.
	embeddedDialerRegistry *EmbeddedDialerRegistry

	// embeddedBackendDialers caches per-agent ConnDialer closures keyed by
	// agentID. Used internally by installEmbeddedBackendDialer.
	embeddedBackendDialers map[string]func(context.Context, string) (*ggrpc.ClientConn, error)

	// Shutdown coordination
	shutdownChan chan struct{}
	shutdownOnce sync.Once

	// State tracking
	mu      sync.RWMutex
	started bool
	closed  bool

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

	// Apply functional options
	var options serverOptions
	for _, opt := range opts {
		opt(&options)
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
	}

	// Create health checker
	s.health = NewHealthChecker(s, log)

	log.Info("Server instance created (components not yet started)")
	return s, nil
}

// Start initializes and starts all server components in order:
// Storage -> TimeSeriesStorage -> Indexes -> KMS -> Secrets -> Vector -> TimeSeries -> RAG -> Embedding -> Catalog -> Backup -> gRPC -> REST -> QUIC -> MCP.
//
// Behavior depends on the Server.Embedded configuration:
//   - Embedded mode (Server.Embedded=true): Returns immediately after initialization.
//     Use this when embedding QuicBaaS as a library.
//   - Standalone mode (Server.Embedded=false): Blocks until shutdown signal is received.
//     Use this when running QuicBaaS as a standalone service.
//
// Storage backends supported: memory, file, pebble.
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
	setupSignalHandlers(s.shutdownChan, s.logger)

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

// initialize performs the actual server initialization.
// This is the single source of truth for component startup order.
// All components are started synchronously in the following order:
// Storage -> Barrier -> TimeSeriesStorage -> Indexes -> KMS -> Secrets -> Vector -> TimeSeries -> RAG -> Embedding -> Catalog -> Backup -> Service -> Messaging -> gRPC -> REST -> QUIC -> MCP.
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

	s.logger.Info("Starting server components...")
	s.logger.Infof("Server mode: %s", s.getEffectiveMode())
	s.logger.Infof("Storage backend: %s", s.config.Storage.Backend)

	// Generate TLS certificates if needed (auto-generate on first startup)
	if err := initializeCAFn(s); err != nil {
		return err
	}

	// Start components in order
	if err := s.startStorage(); err != nil {
		return err
	}

	// Initialize seal barrier if enabled (must be after storage is available)
	if err := s.initBarrier(); err != nil {
		return err
	}

	// Initialize vector storage if enabled
	if s.config.Vector.Enabled {
		if err := s.initVectorStorage(); err != nil {
			return err
		}
	}

	// Initialize time series storage if enabled
	if s.config.TimeSeries.Enabled {
		if err := s.initTimeSeriesStorage(); err != nil {
			return err
		}
	}

	// Initialize raft node based on server mode
	switch s.getEffectiveMode() {
	case config.ServerModeCluster:
		// Cluster mode: Raft consensus is managed externally via QuicRaft.
		// Use a standalone raft node as the local shard transport until
		// the cluster Raft infrastructure is fully integrated.
		s.raftNode = newStandaloneRaftNode(s.config.Raft.NodeID, s.config.Raft.RaftAddress)
		s.logger.Infof("Cluster mode: raft node initialized (id: %s)", s.raftNode.ID())
	default:
		// Standalone mode: no Raft consensus, direct storage access
		s.raftNode = newStandaloneRaftNode(s.config.Raft.NodeID, s.config.Raft.RaftAddress)
		s.logger.Infof("Standalone mode: raft node initialized (id: %s)", s.raftNode.ID())
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

	// Initialize database management subsystem (requires service facade)
	if err := s.startDatabases(); err != nil {
		return err
	}

	// Initialize messaging system if enabled
	if s.config.Messaging.Enabled {
		if err := s.startMessaging(); err != nil {
			return err
		}
	}

	// Initialize agent orchestration service if enabled
	if s.config.Orchestration.Enabled {
		if err := s.startOrchestration(); err != nil {
			return err
		}
	}

	// Initialize embedded agent runtime if enabled. Must start after
	// orchestration so the fleet registry and agent service are available.
	if s.config.Server.EmbeddedAgent.Enabled {
		if err := s.startEmbeddedAgent(); err != nil {
			return err
		}
	}

	if err := s.startGRPCServer(); err != nil {
		return err
	}

	if err := s.startRESTServer(); err != nil {
		return err
	}

	if err := s.startQUICServer(); err != nil {
		return err
	}

	if err := s.startMCPServer(); err != nil {
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
		if s.quicServer != nil {
			s.logger.Infof("QUIC server listening on %s:%d", s.config.Server.Host, s.config.Server.QUIC.Port)
		}
		if s.mcpServer != nil {
			s.logger.Infof("MCP server listening on %s:%d", s.config.Server.Host, s.config.Server.MCP.Port)
		}
	} else {
		s.logger.Info("Running in embedded mode - access via Server.Service()")
	}

	return nil
}

// Shutdown gracefully shuts down all server components in reverse order:
// Workers -> Backend -> Catalog -> Backup -> Embedding -> RAG -> WatchBridge -> Messaging -> QUIC -> REST -> gRPC -> Indexes -> DevCluster -> Bootstrap -> TimeSeriesStorage -> Storage.
func (s *Server) Shutdown(ctx context.Context) error {
	var shutdownErr error

	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.closed = true
		s.mu.Unlock()

		s.logger.Info("Initiating graceful shutdown...")
		startTime := time.Now()

		// Create error collector
		var errs []error

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
		if s.embeddedAgentState != nil {
			s.logger.Info("Shutting down embedded agent...")
			s.stopEmbeddedAgent(ctx)
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

		// Shutdown QUIC server
		if s.quicServer != nil {
			s.logger.Info("Shutting down QUIC server...")
			if err := s.stopQUICServer(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown QUIC server")
			} else {
				s.logger.Info("QUIC server shutdown complete")
			}
		}

		// Shutdown MCP server
		if s.mcpServer != nil {
			s.logger.Info("Shutting down MCP server...")
			if err := s.stopMCPServer(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown MCP server")
			} else {
				s.logger.Info("MCP server shutdown complete")
			}
		}

		// Shutdown REST server
		if s.restServer != nil {
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

		// Shutdown indexes
		if err := s.stopIndexes(); err != nil {
			errs = append(errs, err)
			s.logger.WithError(err).Error("Failed to shutdown index manager")
		}

		// Shutdown standalone raft node
		if s.raftNode != nil {
			s.logger.Info("Shutting down standalone raft node...")
			if err := s.raftNode.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown standalone raft node")
			} else {
				s.logger.Info("Standalone raft node shutdown complete")
			}
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

		// Shutdown local backends
		if s.localBackend != nil {
			s.logger.Info("Shutting down LocalBackend...")
			if err := s.localBackend.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown LocalBackend")
			} else {
				s.logger.Info("LocalBackend shutdown complete")
			}
		}
		if s.fileBackend != nil {
			s.logger.Info("Shutting down FileBackend...")
			if err := s.fileBackend.Close(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown FileBackend")
			} else {
				s.logger.Info("FileBackend shutdown complete")
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

		// Shutdown bootstrap components
		if s.discoveryManager != nil {
			s.logger.Info("Shutting down bootstrap components...")
			if err := s.stopBootstrap(); err != nil {
				errs = append(errs, err)
				s.logger.WithError(err).Error("Failed to shutdown bootstrap components")
			} else {
				s.logger.Info("Bootstrap components shutdown complete")
			}
		}

		// Shutdown time series storage (before main storage)
		if err := s.stopTimeSeries(); err != nil {
			errs = append(errs, err)
			s.logger.WithError(err).Error("Failed to shutdown time series storage")
		}

		duration := time.Since(startTime)
		s.logger.Infof("Graceful shutdown completed in %v", duration)

		if len(errs) > 0 {
			shutdownErr = api.NewShutdownError("server.Shutdown", errs)
		}
	})

	return shutdownErr
}

// TriggerShutdown triggers a graceful shutdown.
func (s *Server) TriggerShutdown() {
	s.shutdownOnce.Do(func() {
		close(s.shutdownChan)
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
	return s.restServer
}

// GetGRPCServer returns the gRPC server (for health checks).
func (s *Server) GetGRPCServer() *ggrpc.Server {
	return s.grpcServer
}

// GetQUICServer returns the QUIC server (for health checks).
func (s *Server) GetQUICServer() *quicapi.Server {
	return s.quicServer
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
func (s *Server) GetSealManager() interfaces.SealManager {
	return s.sealManager
}

// SetSealManager sets the seal manager on the server. This is used by
// integration points that construct seal managers externally (e.g., gRPC
// seal service initialization).
func (s *Server) SetSealManager(mgr interfaces.SealManager) {
	s.sealManager = mgr
}

// GetQRDBClient returns the sharded qrdb client (nil if not initialized).
// Use ForShard(shardID) on the returned client to obtain a per-shard SDK client.
func (s *Server) GetQRDBClient() *ShardedClient {
	return s.qrdbClient
}

// Service returns the QuicBaaS facade for direct database operations.
func (s *Server) Service() service.QuicBaaS {
	return s.quicBaaSService
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
