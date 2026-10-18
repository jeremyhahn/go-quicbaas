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

// Package service implements the unified service facade for QuicBaaS.
// This file contains the concrete implementation that orchestrates all backend components.
package service

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// kmsGracePeriodAdapter wraps kmsServiceImpl to satisfy handlers.KMSKeyStatusUpdater.
// This adapter bridges the service and handlers packages to avoid import cycles.
type kmsGracePeriodAdapter struct {
	kms   *kmsServiceImpl
	jobID string // Job ID for audit logging
}

// SetJobID sets the job ID for audit logging.
func (a *kmsGracePeriodAdapter) SetJobID(jobID string) {
	a.jobID = jobID
}

// UpdateKeyStatusForGracePeriod implements handlers.KMSKeyStatusUpdater.
func (a *kmsGracePeriodAdapter) UpdateKeyStatusForGracePeriod(ctx context.Context, keyID string, version uint64) (*schedulerhandlers.GracePeriodResult, error) {
	resp, err := a.kms.disableKeyVersion(ctx, keyID, version)

	// Log the grace period disable event to audit logger
	if a.kms.auditLogger != nil {
		var errorMsg string
		if err != nil {
			errorMsg = err.Error()
		}
		_ = a.kms.auditLogger.LogGracePeriodDisable(ctx, keyID, version, a.jobID, err == nil, errorMsg)
	}

	if err != nil {
		return nil, err
	}

	return &schedulerhandlers.GracePeriodResult{
		KeyID:     resp.KeyID,
		Version:   resp.Version,
		OldStatus: string(resp.OldStatus),
		NewStatus: string(resp.NewStatus),
	}, nil
}

// schedulerLoggerAdapter wraps interfaces.Logger to satisfy scheduler.Logger.
type schedulerLoggerAdapter struct {
	logger interfaces.Logger
}

func (a *schedulerLoggerAdapter) Debugf(format string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Debugf(format, args...)
	}
}

func (a *schedulerLoggerAdapter) Warnf(format string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Warnf(format, args...)
	}
}

func (a *schedulerLoggerAdapter) Errorf(format string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Errorf(format, args...)
	}
}

// QRDBClientProvider provides per-shard qrdb SDK clients. This interface
// decouples the service layer from the concrete ShardedClient in the server package.
type QRDBClientProvider interface {
	ForShard(shardID uint64) (*qrdbsdk.Client, error)
	Close() error
}

// quicBaaS is the concrete implementation of QuicBaaS.
// It orchestrates all backend components and applies cross-cutting concerns.
type quicBaaS struct {
	// Configuration
	config *ServiceConfig

	// Backend components (use proper interfaces)
	raftNode     interfaces.RaftNode
	storage      interfaces.Storage
	cacheManager interfaces.CacheManager
	backupMgr    interfaces.BackupManager
	logger       interfaces.Logger

	// secretRegistry is the unified secret resolver registry built by the
	// server; wired via SetSecretRegistry so embedded SDK clients can adopt it.
	secretRegistry *qrdbsecret.ResolverRegistry

	// QRDB SDK client provider for type-safe access to core KV operations
	qrdbClient QRDBClientProvider

	// KVStore provider for DAO factory
	kvStoreProvider func(shardID uint64) interfaces.KVStore

	// Sub-services for specialized operations
	kvService          *kvService
	batchService       *batchService
	transactionService *transactionService
	queryService       *queryService
	clusterService     *clusterService
	backupService      *backupService
	authService        *authService
	rbacService        *rbacService
	cacheService       *cacheService
	healthService      *healthService
	timeSeriesService  *timeSeriesService
	operationsService  *operationsService
	watchManager       *watch.Manager

	// KMS, Secrets, Crypto services (optional)
	kmsManager                interfaces.KMSManager
	kmsService                KMSService
	secretsService            SecretsService
	cryptoService             CryptoService
	vectorService             VectorService
	embeddingService          EmbeddingService
	ollamaService             OllamaService
	certificateService        CertificateService
	trustService              TrustService
	attestationService        AttestationService
	registryService           RegistryService
	pivService                PIVService
	fido2Service              FIDO2Service
	webauthnCredentialService WebAuthnCredentialService
	inviteActivationService   InviteActivationService
	acmeService               ACMEServicer
	dynamicSecretsService     DynamicSecretsService
	objectStorageService      ObjectStorageService
	objectStorageAdminService ObjectStorageAdminService
	parameterStoreService     ParameterStoreService
	objectTransferService     ObjectTransferService
	schedulerService          SchedulerService
	placementService          PlacementService
	projectService            ProjectService
	accountService            AccountService
	fleetAdminService         FleetAdminService
	agentService              orchestration.AgentService
	sdlcService               *SDLCService
	promptService             atomic.Pointer[PromptService]
	messagingService          messaging.Service
	databaseService           DatabaseService
	schedulerEngine           interfaces.SchedulerEngine
	sshClient                 xkmsssh.Client
	iamService                *iamServiceProvider

	// loginService is the unified login-ceremony facade
	// (BeginLogin / FinishLogin). Nil until SetLoginService wires it.
	loginService LoginServicer

	// IAM provider - stores IAMComponents for REST handlers
	// Uses IAMServiceProvider interface for proper type safety and testability
	iamProvider IAMServiceProvider

	// DAO factory for application-level data access
	daoFactory *daoFactory

	// Metrics
	metrics *serviceMetrics

	// Keyspace resolver for multi-tenant key isolation
	keyspaceResolver KeyspaceResolver

	// Lifecycle management
	mu        sync.RWMutex
	closed    bool
	startTime time.Time
}

// serviceMetrics holds Prometheus metrics for the service facade.
type serviceMetrics struct {
	requestsTotal    *metrics.CounterVec
	requestDuration  *metrics.HistogramVec
	activeRequests   *metrics.Gauge
	consistencyLevel *metrics.CounterVec
	errorsByType     *metrics.CounterVec
}

// qrdbForShard returns the qrdb SDK client for the given shard.
// The QRDBClientProvider must be injected via WithQRDBClient().
func (d *quicBaaS) qrdbForShard(shardID uint64) (*qrdbsdk.Client, error) {
	if d.qrdbClient == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.qrdbForShard",
			Err:  &api.OperationFailedError{Operation: "qrdb client lookup", Cause: &QRDBClientNotSetError{}},
		}
	}
	return d.qrdbClient.ForShard(shardID)
}

// QRDBClientNotSetError indicates the qrdb client provider was not configured.
type QRDBClientNotSetError struct{}

// Error implements the error interface.
func (e *QRDBClientNotSetError) Error() string {
	return "qrdb client provider not configured"
}

// isNilInterface reports whether v is either a nil interface or a typed-nil
// pointer stored in an interface. Using == nil on an interface{} that holds a
// typed-nil (e.g., (*ShardedClient)(nil)) returns false, so this helper uses
// reflection to distinguish the two cases.
func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// newQuicBaaS creates a new QuicBaaS service from the builder.
// This is called by ServiceBuilder.Build() in service.go.
func newQuicBaaS(b *serviceBuilder) (QuicBaaS, error) {
	// Validate required dependencies.
	// raftNode is required unless a concrete qrdbClient is provided — when
	// the caller has a real qrdb engine node the SDK client handles all
	// data-plane operations and the standalone no-op raft node is not
	// needed. Use reflect to detect typed-nil interface values so that
	// a nil *ShardedClient stored as interface{} is treated as absent.
	qrdbClientPresent := !isNilInterface(b.qrdbClient)
	if b.raftNode == nil && !qrdbClientPresent {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.newQuicBaaS",
			Err:  &api.RequiredFieldError{Field: "raft node"},
		}
	}

	if b.config == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.newQuicBaaS",
			Err:  &api.RequiredFieldError{Field: "config"},
		}
	}

	// Type assert raftNode only when it is provided.
	var raftNode interfaces.RaftNode
	if b.raftNode != nil {
		var ok bool
		raftNode, ok = b.raftNode.(interfaces.RaftNode)
		if !ok {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.newQuicBaaS",
				Err:  &api.InvalidTypeError{Field: "raft node", Expected: "interfaces.RaftNode"},
			}
		}
	}

	var storage interfaces.Storage
	if b.storage != nil {
		var ok bool
		storage, ok = b.storage.(interfaces.Storage)
		if !ok {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.newQuicBaaS",
				Err:  &api.InvalidTypeError{Field: "storage", Expected: "interfaces.Storage"},
			}
		}
	}

	var cacheManager interfaces.CacheManager
	if b.cacheManager != nil {
		var ok bool
		cacheManager, ok = b.cacheManager.(interfaces.CacheManager)
		if !ok {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.newQuicBaaS",
				Err:  &api.InvalidTypeError{Field: "cache manager", Expected: "interfaces.CacheManager"},
			}
		}
	}

	var backupMgr interfaces.BackupManager
	if b.backupManager != nil {
		var ok bool
		backupMgr, ok = b.backupManager.(interfaces.BackupManager)
		if !ok {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.newQuicBaaS",
				Err:  &api.InvalidTypeError{Field: "backup manager", Expected: "interfaces.BackupManager"},
			}
		}
	}

	var logger interfaces.Logger
	if b.logger != nil {
		var ok bool
		logger, ok = b.logger.(interfaces.Logger)
		if !ok {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.newQuicBaaS",
				Err:  &api.InvalidTypeError{Field: "logger", Expected: "interfaces.Logger"},
			}
		}
	}

	// Handle KMS manager
	var kmsManager interfaces.KMSManager
	if b.kmsManager != nil {
		var ok bool
		kmsManager, ok = b.kmsManager.(interfaces.KMSManager)
		if !ok {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.newQuicBaaS",
				Err:  &api.InvalidTypeError{Field: "KMS manager", Expected: "interfaces.KMSManager"},
			}
		}
	}

	// Create service instance
	svc := &quicBaaS{
		config:           b.config,
		raftNode:         raftNode,
		storage:          storage,
		cacheManager:     cacheManager,
		backupMgr:        backupMgr,
		logger:           logger,
		keyspaceResolver: NewKeyspaceResolver(),
		startTime:        time.Now(),
	}

	// Wire qrdb SDK client provider if provided.
	if b.qrdbClient != nil {
		if provider, ok := b.qrdbClient.(QRDBClientProvider); ok {
			svc.qrdbClient = provider
		}
	}

	// Initialize KVStore provider for DAO factory
	if b.kvStoreProvider != nil {
		svc.kvStoreProvider = func(shardID uint64) interfaces.KVStore {
			result := b.kvStoreProvider(shardID)
			if kvStore, ok := result.(interfaces.KVStore); ok {
				return kvStore
			}
			return nil
		}
		svc.daoFactory = &daoFactory{
			kvStoreProvider: svc.kvStoreProvider,
		}
	}

	// Initialize metrics if enabled
	if b.config.EnableMetrics {
		svc.initMetrics()
	}

	// Initialize sub-services
	svc.kvService = &kvService{svc: svc}
	svc.batchService = &batchService{svc: svc}
	svc.transactionService = &transactionService{svc: svc}
	svc.queryService = &queryService{svc: svc}
	svc.clusterService = &clusterService{svc: svc}
	svc.backupService = &backupService{svc: svc}
	svc.authService = &authService{svc: svc}
	svc.rbacService = &rbacService{svc: svc}
	svc.cacheService = &cacheService{svc: svc}
	svc.healthService = newHealthService(svc)
	// Initialize timeseries storage if provided
	var tsStorage interfaces.TimeSeriesStorage
	if b.tsStorage != nil {
		var ok bool
		tsStorage, ok = b.tsStorage.(interfaces.TimeSeriesStorage)
		if !ok {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.newQuicBaaS",
				Err:  &api.InvalidTypeError{Field: "timeseries storage", Expected: "interfaces.TimeSeriesStorage"},
			}
		}
	}
	// Resolve time series shard ID from config (default: 1 for backward compatibility).
	tsShardID := uint64(1)
	if svc.config != nil && svc.config.TimeSeriesShardID > 0 {
		tsShardID = svc.config.TimeSeriesShardID
	}
	svc.timeSeriesService = &timeSeriesService{svc: svc, tsStorage: tsStorage, shardID: tsShardID}
	svc.operationsService = newOperationsService(svc)

	// Initialize watch manager for key change notifications
	svc.watchManager = watch.NewManager()

	// The SSH client is composed by the server (see WithSSHClient). Nil
	// leaves the SSH surface absent: every verb fails closed with
	// *SSHNotConfiguredError.
	svc.sshClient = b.sshClient

	// Initialize KMS service if KMS manager is provided
	if kmsManager != nil {
		svc.kmsManager = kmsManager
		svc.kmsService = newKMSService(kmsManager, logger)
	}

	// Initialize Scheduler if enabled
	if b.config.EnableScheduler && svc.kvStoreProvider != nil {
		schedulerConfig := &scheduler.SchedulerConfig{
			TickInterval:      b.config.SchedulerTickInterval,
			MaxConcurrentJobs: b.config.SchedulerMaxConcurrentJobs,
			DefaultTimeout:    30 * time.Second,
			DefaultRetries:    3,
			RetryBackoff:      5 * time.Second,
		}
		if schedulerConfig.TickInterval == 0 {
			schedulerConfig.TickInterval = time.Second
		}
		if schedulerConfig.MaxConcurrentJobs == 0 {
			schedulerConfig.MaxConcurrentJobs = 10
		}
		// Wrap the service logger for the scheduler
		if logger != nil {
			schedulerConfig.Logger = &schedulerLoggerAdapter{logger: logger}
		}

		// Create scheduler storage using KVStore for persistence
		// Use shard 1 for scheduler data
		kvStore := svc.kvStoreProvider(1)
		if kvStore != nil {
			// A clustered boot MUST get the clustered scheduler. Falling back
			// to the single-node engine here is what docs/18 V4 records: a
			// nil LeaderChecker makes shouldExecute answer true on every node,
			// so every node fires every cluster-wide job on every tick. A
			// clustered boot that cannot build the clustered scheduler
			// therefore refuses to start rather than running wrong.
			schedulerEngine, schedulerErr := buildSchedulerEngine(
				schedulerConfig, kvStore, b.config.Mode, b.schedulerClustering)
			if schedulerErr != nil {
				return nil, schedulerErr
			}
			svc.schedulerEngine = schedulerEngine

			// Create scheduler adapter for the SchedulerService interface
			schedulerAdapter := NewSchedulerAdapter(svc.schedulerEngine)
			svc.schedulerService = schedulerAdapter

			// Register KMS grace period handler if KMS service is available
			if kmsImpl, ok := svc.kmsService.(*kmsServiceImpl); ok {
				// Create adapter to bridge service and handlers packages
				kmsAdapter := &kmsGracePeriodAdapter{kms: kmsImpl}
				graceHandler := schedulerhandlers.NewKMSGracePeriodHandler(kmsAdapter)
				if err := svc.schedulerEngine.RegisterHandler(graceHandler.Name(), graceHandler.Handler()); err != nil {
					if logger != nil {
						logger.Warnf("Failed to register KMS grace period handler: %v", err)
					}
				}

				// Wire scheduler adapter to KMS service for grace period job scheduling
				kmsImpl.SetScheduler(schedulerAdapter)

				// Wire KVStore for persistent version status replication across cluster nodes
				kmsImpl.SetKVStore(kvStore)

				// Cryptoperiod enforcement (PCI DSS 3.6) is a scheduler
				// system job, so a cluster rotates each due key exactly once.
				if err := registerKMSRotation(schedulerEngine, kmsImpl, b.config.KMSRotationScanInterval); err != nil {
					return nil, err
				}
			}

			// Register standard test handlers for integration testing
			for _, handler := range schedulerhandlers.StandardTestHandlers() {
				if err := svc.schedulerEngine.RegisterHandler(handler.Name(), handler.Handler()); err != nil {
					if logger != nil {
						logger.Warnf("Failed to register test handler '%s': %v", handler.Name(), err)
					}
				}
			}

			// Start the scheduler synchronously. SchedulerEngine.Start does
			// not block -- it launches its own poll loop and returns -- so
			// there is nothing to gain from deferring it to a goroutine, and
			// something to lose: a queued start is a start this builder cannot
			// order against the owner's teardown. If the caller aborted its
			// startup and closed everything between this line and the queued
			// goroutine being scheduled, Stop() ran against an engine that had
			// not started yet and the loop came up afterwards, polling storage
			// that was already closed (go-quicbaas#62). Starting here makes the
			// engine either running or failed by the time Build returns, so
			// every later Stop has something definite to stop.
			//
			// The error stays non-fatal, as it was when this ran in a
			// goroutine: a scheduler that will not start must not fail the
			// construction of every other service on the facade.
			if err := svc.schedulerEngine.Start(context.Background()); err != nil {
				if logger != nil {
					logger.Errorf("Scheduler failed to start: %v", err)
				}
			}

			if logger != nil {
				logger.Info("Scheduler service initialized and started")
			}
		}
	}

	// Initialize Crypto service if CryptoManager is provided
	// The crypto service requires both a CryptoManager and KMSManager
	// Note: KMSManager and CryptoManager are separate interfaces with different Sign methods,
	// so CryptoManager must be provided explicitly - it cannot be derived from KMSManager
	if b.cryptoService != nil {
		if cryptoMgr, ok := b.cryptoService.(interfaces.CryptoManager); ok {
			svc.cryptoService = newCryptoService(cryptoMgr, kmsManager, logger, nil)
		}
	} else if b.cryptoManager != nil {
		// CryptoManager provided explicitly via builder
		svc.cryptoService = newCryptoService(b.cryptoManager, kmsManager, logger, nil)
	}

	// Initialize Secrets service if secrets manager is provided
	if b.secretsManager != nil {
		if secretsMgr, ok := b.secretsManager.(interfaces.SecretsManager); ok {
			svc.secretsService = newSecretsService(secretsMgr, logger)
		}
	}

	// Initialize Certificate service if CA service is provided
	if b.caService != nil {
		if caService, ok := b.caService.(xkmstransport.Client); ok {
			svc.certificateService = NewCertificateService(caService)
		}
		// Wire the Trust composition service when the CA adapter exposes
		// the cross-sign / AIA capability. The adapter (pkg/server) is the
		// same instance handed to WithCAService; it satisfies TrustCA by
		// delegating to the embedded xkms TrustedCA. Backends that do not
		// expose cross-sign/AIA (TrustCA) leave Trust unconfigured.
		if trustCA, ok := b.caService.(TrustCA); ok {
			svc.trustService = NewTrustService(trustCA, b.danePublisher)
		}
	}

	// The Attestation service is always wired: its verifier half composes
	// go-xkms pkg/attestation and needs nothing from the deployment, while
	// its orchestration half reports ErrAttestationNoAttestor until an
	// application registers a ContainerAttestor. Constructing it
	// unconditionally means Security().Attestation() never returns nil and
	// every transport can route to it.
	svc.attestationService = NewAttestationService(b.attestationTrustAnchors, b.containerAttestor,
		WithAttestationStore(NewQRDBAttestationStore(func() (*qrdbsdk.Client, error) {
			return svc.qrdbForShard(attestationShardID)
		})))

	// The service registry is always wired. It resolves the platform shard's
	// qrdb client per call, so a server whose store is not up yet answers
	// with a typed unavailable error instead of a nil service.
	svc.registryService = newRegistryService(func(ctx context.Context) (*registry.Registry, error) {
		client, err := svc.qrdbForShard(registryShardID)
		if err != nil {
			return nil, err
		}
		return registry.NewFromClient(ctx, client)
	})

	// The PIV and FIDO2 services are always wired. PIV is a stateless
	// delegate to the composed go-xkms service singleton (typed
	// not-initialized errors come from go-xkms until the singleton is up).
	// FIDO2 resolves the per-tenant xkms relying-party service through the
	// IAM WebAuthn factory at call time and reports ErrFIDO2NotConfigured
	// until that manager is wired.
	svc.pivService = NewPIVService()
	svc.fido2Service = NewFIDO2Service(svc)
	// User-addressed WebAuthn credential administration. Like FIDO2 it
	// resolves at call time, so it is wired unconditionally and reports
	// ErrWebAuthnCredentialNotConfigured until IAM is up.
	svc.webauthnCredentialService = NewWebAuthnCredentialService(svc, svc)

	// Invite activation resolves the IAM components per call too, so it is
	// wired unconditionally and reports the missing component until IAM is up.
	svc.inviteActivationService = NewInviteActivationService(svc)

	// Wire the composed ACME (RFC 8555) servicer when pkg/server built one.
	// ACME composition (CA + qrdb-backed storage + base URL) lives in
	// pkg/server where those dependencies are available; the builder only
	// carries the finished servicer onto the facade.
	if b.acmeService != nil {
		svc.acmeService = b.acmeService
	}

	// Initialize IAM service if IAM manager is provided
	iamSvc, err := initializeIAMService(b)
	if err != nil {
		return nil, err
	}
	svc.iamService = iamSvc

	// Initialize IAM provider for REST handlers (iam only)
	svc.iamProvider = initializeIAMProvider(b)

	// Initialize Object Storage service if all dependencies are provided
	if b.catalogManager != nil && b.backendManager != nil && b.objectStoreConfigResolver != nil {
		objStorageSvc, err := NewObjectStorageService(
			b.catalogManager,
			b.backendManager,
			b.objectStoreConfigResolver,
			b.workerManager,
			logger,
		)
		if err != nil {
			if logger != nil {
				logger.Warnf("Failed to initialize object storage service: %v", err)
			}
		} else {
			svc.objectStorageService = objStorageSvc
		}
	}
	// Initialize Object Storage Admin service if all dependencies are provided
	if b.catalogManager != nil && b.backendManager != nil && b.objectStoreConfigResolver != nil {
		objStorageAdminSvc, err := NewObjectStorageAdminService(
			b.catalogManager,
			b.backendManager,
			b.objectStoreConfigResolver,
			logger,
		)
		if err != nil {
			if logger != nil {
				logger.Warnf("Failed to initialize object storage admin service: %v", err)
			}
		} else {
			svc.objectStorageAdminService = objStorageAdminSvc
		}
	}
	// Initialize Parameter Store service using the qrdb SDK client
	if svc.qrdbClient != nil {
		paramMgr := newKVParameterManager(svc.qrdbClient, 1, logger) // Use shard 1 for parameters
		paramStoreSvc, err := NewParameterStoreService(paramMgr, logger)
		if err != nil {
			if logger != nil {
				logger.Warnf("Failed to initialize parameter store service: %v", err)
			}
		} else {
			svc.parameterStoreService = paramStoreSvc
			if logger != nil {
				logger.Info("Parameter store service initialized with KV backend")
			}
		}
	}

	// The scheduler's placement engine runs on the qrdb SDK client: the
	// lease, KV and transaction planes of shard 1, with the rest of the
	// scheduler's records. The shard client is resolved on first use, so a
	// node that never places holds none.
	if svc.qrdbClient != nil {
		svc.placementService = newLazyPlacementService(svc.qrdbClient, 1)
	}

	// Initialize Vector service if provided.
	// The vector service uses direct Raft proposals for writes
	// (via raftNode.SyncPropose), not the SDK embedded transport.
	if b.vectorService != nil {
		svc.vectorService = b.vectorService
	}

	// Initialize Embedding service if embedding manager is provided
	if b.embeddingManager != nil {
		svc.embeddingService = NewEmbeddingService(b.embeddingManager, logger)
	}

	// Initialize Messaging service if provided
	if b.messagingService != nil {
		svc.messagingService = b.messagingService
	}

	// Initialize Ollama service if provided
	if b.ollamaService != nil {
		svc.ollamaService = b.ollamaService
	}

	// Initialize the orchestration stores. Both bind to the same injected KV,
	// so whether project and agent state survives a restart is decided purely
	// by which store the caller supplied — the store code is identical either
	// way. The server injects its tenant-routed store, which puts a
	// barrier-isolated tenant's records inside that tenant's own barrier.
	orchKV := b.orchestrationKVStore
	if orchKV == nil {
		orchKV = kvstore.NewMemory()
		if logger != nil {
			logger.Warn("No orchestration KV store configured; projects and agent registrations are in-memory and will not survive a restart")
		}
	}
	orchOpts := []orchestration.StoreOption{orchestration.WithStoreAuditor(b.orchestrationAuditor)}

	svc.projectService = NewProjectService(orchestration.NewProjectStore(orchKV, orchOpts...))
	svc.fleetAdminService = NewFleetAdminService(orchestration.NewFleetRegistry(orchKV, orchOpts...))
	svc.wireProjectReferences()

	// Pooled provider accounts. Metadata lives in the same orchestration KV
	// as projects/agents; the credential envelope lives behind the platform
	// SecretsManager (tenant-specific DEK — see
	// orchestration/account_secret_store.go), never in the KV record. When no
	// SecretsManager is configured the secret half is nil and every
	// credential-bearing verb fails closed with an AccountStoreError rather
	// than silently persisting an unencrypted envelope.
	var accountSecrets *orchestration.AccountSecretStore
	if b.secretsManager != nil {
		if secretsMgr, ok := b.secretsManager.(interfaces.SecretsManager); ok {
			accountSecrets = orchestration.NewAccountSecretStore(secretsMgr)
		}
	}
	svc.accountService = NewAccountService(orchestration.NewAccountStore(orchKV, orchOpts...), accountSecrets)

	// An account may name a tenant secret for its credential instead of
	// holding one inline (credential_secret_ref). The write path checks the
	// reference resolves, through the SAME SecretsService every other tenant
	// secret is read through.
	if svc.secretsService != nil {
		if refWirer, refOK := svc.accountService.(interface {
			SetSecretResolver(orchestration.AccountSecretResolver)
		}); refOK {
			refWirer.SetSecretResolver(NewAccountSecretResolver(svc.secretsService))
		}
	}

	// The credential-release verb records one audit event per attempt, on the
	// SAME sink the account store's own mutations land on, so a release and
	// the rotation that preceded it sit on one trail. Wired here rather than
	// taken as a constructor argument for the same reason the usage tracker
	// is: the service container is the one place that already holds both.
	if releaseWirer, wirerOK := svc.accountService.(interface {
		SetAuditor(orchestration.StoreAuditor)
		SetLogger(interfaces.Logger)
	}); wirerOK {
		releaseWirer.SetAuditor(b.orchestrationAuditor)
		releaseWirer.SetLogger(logger)
	}

	if svc.logger != nil {
		svc.logger.WithFields(map[string]interface{}{
			"node_id":        b.config.NodeID,
			"version":        b.config.Version,
			"enable_auth":    b.config.EnableAuth,
			"enable_metrics": b.config.EnableMetrics,
		}).Info("QuicBaaS service initialized")
	}

	return svc, nil
}

// initMetrics initializes Prometheus metrics for the service.
func (s *quicBaaS) initMetrics() {
	s.metrics = &serviceMetrics{
		requestsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "service",
			Name:      "requests_total",
			Help:      "Total number of service requests",
		}, []string{"operation", "status"}),

		requestDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "service",
			Name:      "request_duration_seconds",
			Help:      "Duration of service requests in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"operation"}),

		activeRequests: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "service",
			Name:      "active_requests",
			Help:      "Number of active service requests",
		}),

		consistencyLevel: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "service",
			Name:      "consistency_level_total",
			Help:      "Total requests by consistency level",
		}, []string{"level"}),

		errorsByType: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "service",
			Name:      "errors_total",
			Help:      "Total errors by type",
		}, []string{"error_type"}),
	}
}

// =============================================================================
// KV Service Implementation
// =============================================================================

type kvService struct {
	svc *quicBaaS
}

// contextError returns a typed error wrapping ctx.Err() when ctx is cancelled
// or past its deadline, and nil while ctx is live. KV verbs call it before any
// read or write so a cancelled caller never commits or observes data.
func contextError(ctx context.Context, op string) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	code := errors.ErrCanceled
	if stderrors.Is(err, context.DeadlineExceeded) {
		code = errors.ErrDeadlineExceeded
	}
	return &errors.QBError{Code: code, Op: op, Err: err}
}

func (k *kvService) Put(ctx context.Context, req *PutRequest) (*PutResponse, error) {
	start := time.Now()
	if k.svc.metrics != nil {
		k.svc.metrics.activeRequests.Inc()
		defer func() {
			k.svc.metrics.activeRequests.Dec()
			k.svc.metrics.requestDuration.With("Put").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validatePutRequest(req); err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Put", "invalid").Inc()
		}
		return nil, err
	}
	if err := contextError(ctx, "service.Put"); err != nil {
		return nil, err
	}

	// Track consistency level
	if k.svc.metrics != nil {
		k.svc.metrics.consistencyLevel.With(req.Consistency.String()).Inc()
	}

	// Determine shard ID (default to 1 for single-shard setup)
	shardID := req.ShardID
	if shardID == 0 {
		shardID = 1
	}

	// Build tenant-aware key using keyspace resolver
	key, err := k.svc.keyspaceResolver.BuildKey(ctx, req.EntityType, req.ID)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Put", "invalid").Inc()
		}
		return nil, err
	}
	// Propose to Raft via qrdb SDK
	client, err := k.svc.qrdbForShard(shardID)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Put", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Put",
			Err:  err,
		}
	}
	if err := client.KV().Put(ctx, string(key), req.Data); err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Put", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Put",
			Err:  err,
		}
	}

	// Get leader ID via SDK
	leaderID := k.svc.getLeaderIDFromSDK(ctx, shardID)

	resp := &PutResponse{
		ID:        req.ID,
		Version:   1, // Initial version (MVCC revision tracking requires state machine changes)
		Timestamp: time.Now().UnixNano(),
		LeaderID:  leaderID,
	}

	if k.svc.metrics != nil {
		k.svc.metrics.requestsTotal.With("Put", "success").Inc()
	}

	// Update cache if available (use the same tenant-aware key)
	if k.svc.cacheManager != nil {
		_ = k.svc.cacheManager.Set(context.Background(), string(key), req.Data)
	}

	return resp, nil
}

func (k *kvService) Get(ctx context.Context, req *GetRequest) (*GetResponse, error) {
	start := time.Now()
	if k.svc.metrics != nil {
		k.svc.metrics.activeRequests.Inc()
		defer func() {
			k.svc.metrics.activeRequests.Dec()
			k.svc.metrics.requestDuration.With("Get").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateGetRequest(req); err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Get", "invalid").Inc()
		}
		return nil, err
	}
	if err := contextError(ctx, "service.Get"); err != nil {
		return nil, err
	}

	// Track consistency level
	if k.svc.metrics != nil {
		k.svc.metrics.consistencyLevel.With(req.Consistency.String()).Inc()
	}

	// Build tenant-aware key using keyspace resolver
	key, err := k.svc.keyspaceResolver.BuildKey(ctx, req.EntityType, req.ID)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Get", "invalid").Inc()
		}
		return nil, err
	}

	// Check cache first (if consistency allows)
	if req.Consistency == ConsistencyLevelEventual && k.svc.cacheManager != nil {
		if data, err := k.svc.cacheManager.Get(context.Background(), string(key)); err == nil && len(data) > 0 {
			return &GetResponse{
				ID:        req.ID,
				Data:      data,
				Version:   1,
				Timestamp: time.Now().UnixNano(),
				Found:     true,
			}, nil
		}
	}

	// Determine shard ID
	shardID := req.ShardID
	if shardID == 0 {
		shardID = 1
	}

	// Build read options from request
	readTimeout, err := readTimeoutDuration(req.TimeoutMs)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Get", "invalid").Inc()
		}
		return nil, &errors.QBError{Code: errors.ErrInvalidInput, Op: "service.Get", Err: err}
	}

	// Use the shorter of: parent context deadline, or per-read timeout
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		remaining := time.Until(deadline)
		if readTimeout < remaining {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, readTimeout)
			defer cancel()
		}
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, readTimeout)
		defer cancel()
	}

	// Read via qrdb SDK
	client, err := k.svc.qrdbForShard(shardID)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Get", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Get",
			Err:  err,
		}
	}
	data, err := client.KV().Get(ctx, string(key))
	if err != nil {
		// Key-not-found from the transport is not a Raft error; return the
		// not-found sentinel so the handler maps it to HTTP 404.
		var knf *qrdbtransport.KeyNotFoundError
		if stderrors.As(err, &knf) {
			if k.svc.metrics != nil {
				k.svc.metrics.requestsTotal.With("Get", "not_found").Inc()
			}
			return nil, &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "service.Get",
				Err:  &api.NotFoundError{Resource: req.EntityType, ID: fmt.Sprintf("%d", req.ID)},
			}
		}
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Get", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Get",
			Err:  err,
		}
	}

	// Parse result
	resp := &GetResponse{
		ID:        req.ID,
		Version:   1,
		Timestamp: time.Now().UnixNano(),
		Found:     false,
	}

	if data != nil {
		resp.Data = data
		resp.Found = true

		// Update cache (only for non-empty values to avoid cache pollution)
		if k.svc.cacheManager != nil && len(data) > 0 {
			_ = k.svc.cacheManager.Set(context.Background(), string(key), data)
		}
	}

	if !resp.Found {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Get", "not_found").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "service.Get",
			Err:  &api.NotFoundError{Resource: req.EntityType, ID: fmt.Sprintf("%d", req.ID)},
		}
	}

	if k.svc.metrics != nil {
		k.svc.metrics.requestsTotal.With("Get", "success").Inc()
	}

	return resp, nil
}

func (k *kvService) Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error) {
	start := time.Now()
	if k.svc.metrics != nil {
		k.svc.metrics.activeRequests.Inc()
		defer func() {
			k.svc.metrics.activeRequests.Dec()
			k.svc.metrics.requestDuration.With("Delete").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateDeleteRequest(req); err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Delete", "invalid").Inc()
		}
		return nil, err
	}
	if err := contextError(ctx, "service.Delete"); err != nil {
		return nil, err
	}

	// Determine shard ID
	shardID := req.ShardID
	if shardID == 0 {
		shardID = 1
	}

	// Build tenant-aware key using keyspace resolver
	key, err := k.svc.keyspaceResolver.BuildKey(ctx, req.EntityType, req.ID)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Delete", "invalid").Inc()
		}
		return nil, err
	}
	// Delete via qrdb SDK
	client, err := k.svc.qrdbForShard(shardID)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Delete", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Delete",
			Err:  err,
		}
	}
	if err := client.KV().Delete(ctx, string(key)); err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Delete", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Delete",
			Err:  err,
		}
	}

	// Invalidate cache (use the same tenant-aware key)
	if k.svc.cacheManager != nil {
		_ = k.svc.cacheManager.Delete(context.Background(), string(key))
	}

	resp := &DeleteResponse{
		Deleted:   true,
		Timestamp: time.Now().UnixNano(),
	}

	if k.svc.metrics != nil {
		k.svc.metrics.requestsTotal.With("Delete", "success").Inc()
	}

	return resp, nil
}

func (k *kvService) List(ctx context.Context, req *ListRequest, stream chan<- *ListResponse) error {
	defer close(stream)

	start := time.Now()
	if k.svc.metrics != nil {
		k.svc.metrics.activeRequests.Inc()
		defer func() {
			k.svc.metrics.activeRequests.Dec()
			k.svc.metrics.requestDuration.With("List").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateListRequest(req); err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("List", "invalid").Inc()
		}
		return err
	}

	// Determine shard ID
	shardID := req.ShardID
	if shardID == 0 {
		shardID = 1
	}

	// Build tenant-aware prefix using keyspace resolver
	prefix, err := k.svc.keyspaceResolver.BuildPrefix(ctx, req.EntityType)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("List", "invalid").Inc()
		}
		return err
	}

	// Scan via qrdb SDK
	client, err := k.svc.qrdbForShard(shardID)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("List", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.List",
			Err:  err,
		}
	}
	scanResult, err := client.KV().Scan(ctx, string(prefix))
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("List", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.List",
			Err:  err,
		}
	}

	// Parse results from scan
	var entities []*Entity
	for keyStr, data := range scanResult {
		// Extract ID from key using keyspace resolver
		_, _, id, parseErr := k.svc.keyspaceResolver.ParseKey([]byte(keyStr))
		if parseErr != nil {
			// Skip keys that can't be parsed
			continue
		}

		entities = append(entities, &Entity{
			EntityType: req.EntityType,
			ID:         id,
			Data:       data,
			Version:    1,
			Timestamp:  time.Now().UnixNano(),
		})
	}

	// Sort by ID so that page boundaries are stable across calls.
	slices.SortFunc(entities, func(a, b *Entity) int {
		return cmp.Compare(a.ID, b.ID)
	})

	offset, err := listPageOffset(entities, req.PageToken)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("List", "invalid").Inc()
		}
		return err
	}

	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	end := offset + min(conv.ClampUint64ToInt(pageSize), len(entities)-offset)

	resp := &ListResponse{
		Entities:   entities[offset:end],
		TotalCount: uint64(len(entities)),
	}
	if end < len(entities) {
		resp.NextPageToken = encodeListPageToken(entities[end-1].ID)
	}

	select {
	case stream <- resp:
	case <-ctx.Done():
		return ctx.Err()
	}

	if k.svc.metrics != nil {
		k.svc.metrics.requestsTotal.With("List", "success").Inc()
	}

	return nil
}

// Watch monitors a key or key prefix for changes and streams events to the caller.
// Returns the watch ID and any error that occurred during setup.
func (k *kvService) Watch(ctx context.Context, req *WatchRequest, stream chan<- *WatchResponse) (int64, error) {
	start := time.Now()
	if k.svc.metrics != nil {
		k.svc.metrics.activeRequests.Inc()
		defer func() {
			k.svc.metrics.activeRequests.Dec()
			k.svc.metrics.requestDuration.With("Watch").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Watch", "invalid").Inc()
		}
		return 0, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Watch",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if len(req.Key) == 0 {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Watch", "invalid").Inc()
		}
		return 0, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Watch",
			Err:  &api.RequiredFieldError{Field: "key"},
		}
	}

	if stream == nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Watch", "invalid").Inc()
		}
		return 0, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Watch",
			Err:  &api.RequiredFieldError{Field: "stream channel"},
		}
	}

	// Build watch options
	var opts []watch.WatchOption
	if req.IsPrefix {
		opts = append(opts, watch.WithPrefix())
	}

	// Create watch using the watch manager
	eventCh, err := k.svc.watchManager.Watch(ctx, string(req.Key), opts...)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("Watch", "error").Inc()
		}
		return 0, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.Watch",
			Err:  &api.OperationFailedError{Operation: "create watch", Cause: err},
		}
	}

	// Generate watch ID
	watchID := time.Now().UnixNano()

	// Send created response
	select {
	case stream <- &WatchResponse{
		WatchID: watchID,
		Created: true,
	}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	// Start event forwarding goroutine
	go func() {
		defer close(stream)

		for {
			select {
			case <-ctx.Done():
				// Send canceled response
				stream <- &WatchResponse{
					WatchID:      watchID,
					Canceled:     true,
					CancelReason: ctx.Err().Error(),
				}
				return
			case event, ok := <-eventCh:
				if !ok {
					// Channel closed, watch ended
					stream <- &WatchResponse{
						WatchID:      watchID,
						Canceled:     true,
						CancelReason: "watch closed",
					}
					return
				}

				// Convert event type
				var eventType WatchEventType
				switch event.Type {
				case watch.EventTypePut:
					eventType = WatchEventTypePut
				case watch.EventTypeDelete:
					eventType = WatchEventTypeDelete
				default:
					eventType = WatchEventTypeUnspecified
				}

				// Apply filters
				if len(req.Filters) > 0 {
					skip := false
					for _, filter := range req.Filters {
						if filter == eventType {
							skip = true
							break
						}
					}
					if skip {
						continue
					}
				}

				// Send event to stream
				resp := &WatchResponse{
					WatchID:  watchID,
					Revision: time.Now().UnixNano(), // Use timestamp as revision
					Events: []*WatchEvent{
						{
							Type:      eventType,
							Key:       event.Key,
							Value:     event.Value,
							Timestamp: time.Now().UnixNano(),
						},
					},
				}

				select {
				case stream <- resp:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	if k.svc.metrics != nil {
		k.svc.metrics.requestsTotal.With("Watch", "success").Inc()
	}

	return watchID, nil
}

// ListEntityTypes returns all unique entity types stored in the database.
// It scans through all keys and extracts unique entity type prefixes.
func (k *kvService) ListEntityTypes(ctx context.Context, req *ListEntityTypesRequest) (*ListEntityTypesResponse, error) {
	start := time.Now()
	if k.svc.metrics != nil {
		k.svc.metrics.activeRequests.Inc()
		defer func() {
			k.svc.metrics.activeRequests.Dec()
			k.svc.metrics.requestDuration.With("ListEntityTypes").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		req = &ListEntityTypesRequest{PageSize: 100}
	}
	if req.PageSize <= 0 {
		req.PageSize = 100
	}

	// Determine shard ID
	shardID := req.ShardID
	if shardID == 0 {
		shardID = 1
	}

	// Get the tenant prefix from keyspace resolver
	// This ensures we only scan keys belonging to the current tenant
	prefix, err := k.svc.keyspaceResolver.BuildTenantPrefix(ctx)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("ListEntityTypes", "error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.ListEntityTypes",
			Err:  err,
		}
	}

	// Scan via qrdb SDK
	client, err := k.svc.qrdbForShard(shardID)
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("ListEntityTypes", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.ListEntityTypes",
			Err:  err,
		}
	}
	scanResult, err := client.KV().Scan(ctx, string(prefix))
	if err != nil {
		if k.svc.metrics != nil {
			k.svc.metrics.requestsTotal.With("ListEntityTypes", "error").Inc()
			k.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.ListEntityTypes",
			Err:  err,
		}
	}

	// Extract unique entity types from keys
	entityTypeSet := make(map[string]struct{})
	for keyStr := range scanResult {
		key := []byte(keyStr)

		// Skip system keys
		if k.svc.keyspaceResolver.IsSystemKey(key) {
			continue
		}

		// Extract entity type using the keyspace resolver
		entityType, ok := ExtractEntityTypeFromKey(k.svc.keyspaceResolver, key)
		if ok && entityType != "" {
			entityTypeSet[entityType] = struct{}{}
		}
	}

	// Convert set to sorted slice
	entityTypes := make([]string, 0, len(entityTypeSet))
	for entityType := range entityTypeSet {
		entityTypes = append(entityTypes, entityType)
	}
	sort.Strings(entityTypes)

	// Apply pagination
	totalCount := len(entityTypes)
	startIdx := 0
	if req.PageToken != "" {
		var parsedIdx int
		if _, err := fmt.Sscanf(req.PageToken, "%d", &parsedIdx); err == nil && parsedIdx > 0 && parsedIdx < totalCount {
			startIdx = parsedIdx
		}
	}

	endIdx := startIdx + req.PageSize
	if endIdx > totalCount {
		endIdx = totalCount
	}

	pagedTypes := entityTypes[startIdx:endIdx]
	nextPageToken := ""
	if endIdx < totalCount {
		nextPageToken = fmt.Sprintf("%d", endIdx)
	}

	if k.svc.metrics != nil {
		k.svc.metrics.requestsTotal.With("ListEntityTypes", "success").Inc()
	}

	return &ListEntityTypesResponse{
		EntityTypes:   pagedTypes,
		NextPageToken: nextPageToken,
		TotalCount:    totalCount,
	}, nil
}

// =============================================================================
// Batch Service Implementation
// =============================================================================

// batchProposalMaxBytes bounds the estimated encoded size of a single
// ExecuteBatch call. QuicRaft rejects Raft entries larger than 8 MB
// (proto.MaxEntrySize); half of that leaves headroom for batch framing,
// op envelopes, and tenant keyspace prefixes added below this layer.
const batchProposalMaxBytes = 4 << 20

// batchOpOverheadBytes approximates the per-operation encoding overhead
// (length prefixes and op envelope fields) used when sizing sub-batches.
const batchOpOverheadBytes = 64

type batchService struct {
	svc *quicBaaS
}

// batchOpSize estimates the encoded size of a single batch operation.
func batchOpSize(op qrdbtransport.Op) int {
	return len(op.Key) + len(op.Value) + batchOpOverheadBytes
}

// executeShardBatch applies ops for a single shard through the qrdb SDK
// atomic batch API. Operations are split into size-bounded sub-batches so
// that each Raft proposal stays under the QuicRaft entry limit; every
// sub-batch is committed as one atomic consensus proposal, in input order.
func (b *batchService) executeShardBatch(ctx context.Context, shardID uint64, ops []qrdbtransport.Op, op string) error {
	client, err := b.svc.qrdbForShard(shardID)
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrRaft,
			Op:   op,
			Err:  err,
		}
	}
	for start := 0; start < len(ops); {
		size := batchOpSize(ops[start])
		end := start + 1
		for end < len(ops) && size+batchOpSize(ops[end]) <= batchProposalMaxBytes {
			size += batchOpSize(ops[end])
			end++
		}
		if _, err := client.Batch().ExecuteBatch(ctx, ops[start:end]); err != nil {
			return &errors.QBError{
				Code: errors.ErrRaft,
				Op:   op,
				Err:  err,
			}
		}
		start = end
	}
	return nil
}

func (b *batchService) BatchPut(ctx context.Context, req *BatchPutRequest) (*BatchPutResponse, error) {
	start := time.Now()
	if b.svc.metrics != nil {
		b.svc.metrics.activeRequests.Inc()
		defer func() {
			b.svc.metrics.activeRequests.Dec()
			b.svc.metrics.requestDuration.With("BatchPut").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		if b.svc.metrics != nil {
			b.svc.metrics.requestsTotal.With("BatchPut", "invalid").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.BatchPut",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if len(req.Requests) == 0 {
		if b.svc.metrics != nil {
			b.svc.metrics.requestsTotal.With("BatchPut", "invalid").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.BatchPut",
			Err:  &api.EmptyCollectionError{Field: "batch"},
		}
	}

	// Build per-shard atomic batch operations, grouped by per-request
	// ShardID for correct shard routing. Input order is preserved within
	// each shard group.
	timestamp := time.Now().UnixNano()
	responses := make([]*PutResponse, len(req.Requests))
	shardOrder := make([]uint64, 0, 1)
	shardOps := make(map[uint64][]qrdbtransport.Op)
	for i, putReq := range req.Requests {
		key, err := b.svc.keyspaceResolver.BuildKey(ctx, putReq.EntityType, putReq.ID)
		if err != nil {
			if b.svc.metrics != nil {
				b.svc.metrics.requestsTotal.With("BatchPut", "invalid").Inc()
			}
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.BatchPut",
				Err:  &api.OperationFailedError{Operation: "validate entity type " + putReq.EntityType, Cause: err},
			}
		}
		shardID := putReq.ShardID
		if shardID == 0 {
			shardID = 1
		}
		if _, ok := shardOps[shardID]; !ok {
			shardOrder = append(shardOrder, shardID)
		}
		shardOps[shardID] = append(shardOps[shardID], qrdbtransport.Op{
			Type:  qrdbtransport.OpPut,
			Key:   key,
			Value: putReq.Data,
		})
		responses[i] = &PutResponse{
			ID:        putReq.ID,
			Version:   1,
			Timestamp: timestamp,
		}
	}

	// Commit each shard group through the qrdb SDK atomic batch API.
	// Each size-bounded sub-batch is a single Raft consensus proposal
	// instead of one proposal per operation.
	for _, shardID := range shardOrder {
		if err := b.executeShardBatch(ctx, shardID, shardOps[shardID], "service.BatchPut"); err != nil {
			if b.svc.metrics != nil {
				b.svc.metrics.requestsTotal.With("BatchPut", "error").Inc()
				b.svc.metrics.errorsByType.With("raft_error").Inc()
			}
			return nil, err
		}
	}

	resp := &BatchPutResponse{
		Responses: responses,
		Timestamp: timestamp,
	}

	if b.svc.metrics != nil {
		b.svc.metrics.requestsTotal.With("BatchPut", "success").Inc()
	}

	return resp, nil
}

func (b *batchService) BatchGet(ctx context.Context, req *BatchGetRequest) (*BatchGetResponse, error) {
	start := time.Now()
	if b.svc.metrics != nil {
		b.svc.metrics.activeRequests.Inc()
		defer func() {
			b.svc.metrics.activeRequests.Dec()
			b.svc.metrics.requestDuration.With("BatchGet").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		if b.svc.metrics != nil {
			b.svc.metrics.requestsTotal.With("BatchGet", "invalid").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.BatchGet",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if len(req.Requests) == 0 {
		if b.svc.metrics != nil {
			b.svc.metrics.requestsTotal.With("BatchGet", "invalid").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.BatchGet",
			Err:  &api.EmptyCollectionError{Field: "batch"},
		}
	}

	// Process each get request
	responses := make([]*GetResponse, 0, len(req.Requests))
	for _, getReq := range req.Requests {
		resp, err := b.svc.kvService.Get(ctx, getReq)
		if err != nil {
			// For batch operations, collect errors but continue
			responses = append(responses, &GetResponse{
				ID:    getReq.ID,
				Found: false,
			})
			continue
		}
		responses = append(responses, resp)
	}

	resp := &BatchGetResponse{
		Responses: responses,
	}

	if b.svc.metrics != nil {
		b.svc.metrics.requestsTotal.With("BatchGet", "success").Inc()
	}

	return resp, nil
}

func (b *batchService) BatchDelete(ctx context.Context, req *BatchDeleteRequest) (*BatchDeleteResponse, error) {
	start := time.Now()
	if b.svc.metrics != nil {
		b.svc.metrics.activeRequests.Inc()
		defer func() {
			b.svc.metrics.activeRequests.Dec()
			b.svc.metrics.requestDuration.With("BatchDelete").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		if b.svc.metrics != nil {
			b.svc.metrics.requestsTotal.With("BatchDelete", "invalid").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.BatchDelete",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	if len(req.Requests) == 0 {
		if b.svc.metrics != nil {
			b.svc.metrics.requestsTotal.With("BatchDelete", "invalid").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.BatchDelete",
			Err:  &api.EmptyCollectionError{Field: "batch"},
		}
	}

	// Build per-shard atomic batch operations, grouped by per-request
	// ShardID for correct shard routing. Input order is preserved within
	// each shard group.
	timestamp := time.Now().UnixNano()
	responses := make([]*DeleteResponse, len(req.Requests))
	shardOrder := make([]uint64, 0, 1)
	shardOps := make(map[uint64][]qrdbtransport.Op)
	for i, delReq := range req.Requests {
		key, err := b.svc.keyspaceResolver.BuildKey(ctx, delReq.EntityType, delReq.ID)
		if err != nil {
			if b.svc.metrics != nil {
				b.svc.metrics.requestsTotal.With("BatchDelete", "invalid").Inc()
			}
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "service.BatchDelete",
				Err:  &api.OperationFailedError{Operation: "validate entity type " + delReq.EntityType, Cause: err},
			}
		}
		shardID := delReq.ShardID
		if shardID == 0 {
			shardID = 1
		}
		if _, ok := shardOps[shardID]; !ok {
			shardOrder = append(shardOrder, shardID)
		}
		shardOps[shardID] = append(shardOps[shardID], qrdbtransport.Op{
			Type: qrdbtransport.OpDelete,
			Key:  key,
		})
		responses[i] = &DeleteResponse{
			Deleted:   true,
			Timestamp: timestamp,
		}
	}

	// Commit each shard group through the qrdb SDK atomic batch API.
	// Each size-bounded sub-batch is a single Raft consensus proposal
	// instead of one proposal per operation.
	for _, shardID := range shardOrder {
		if err := b.executeShardBatch(ctx, shardID, shardOps[shardID], "service.BatchDelete"); err != nil {
			if b.svc.metrics != nil {
				b.svc.metrics.requestsTotal.With("BatchDelete", "error").Inc()
				b.svc.metrics.errorsByType.With("raft_error").Inc()
			}
			return nil, err
		}
	}

	resp := &BatchDeleteResponse{
		Responses: responses,
		Timestamp: timestamp,
	}

	if b.svc.metrics != nil {
		b.svc.metrics.requestsTotal.With("BatchDelete", "success").Inc()
	}

	return resp, nil
}

// =============================================================================
// Transaction Service Implementation
// =============================================================================

type transactionService struct {
	svc *quicBaaS
}

func (t *transactionService) Transaction(ctx context.Context, req *TransactionRequest) (*TransactionResponse, error) {
	start := time.Now()
	if t.svc.metrics != nil {
		t.svc.metrics.activeRequests.Inc()
		defer func() {
			t.svc.metrics.activeRequests.Dec()
			t.svc.metrics.requestDuration.With("Transaction").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := t.validateTransactionRequest(req); err != nil {
		if t.svc.metrics != nil {
			t.svc.metrics.requestsTotal.With("Transaction", "invalid").Inc()
		}
		return nil, err
	}

	// Determine shard ID
	shardID := req.ShardID
	if shardID == 0 {
		shardID = 1
	}

	// Evaluate comparison conditions
	succeeded := true
	for _, cmp := range req.Compare {
		result, err := t.evaluateCompare(ctx, shardID, cmp)
		if err != nil {
			if t.svc.metrics != nil {
				t.svc.metrics.requestsTotal.With("Transaction", "error").Inc()
			}
			return nil, err
		}
		if !result {
			succeeded = false
			break
		}
	}

	// Select operations based on comparison result
	var ops []*TransactionOp
	if succeeded {
		ops = req.Success
	} else {
		ops = req.Failure
	}

	// Execute operations
	responses, err := t.executeTransactionOps(ctx, shardID, ops)
	if err != nil {
		if t.svc.metrics != nil {
			t.svc.metrics.requestsTotal.With("Transaction", "error").Inc()
		}
		return nil, err
	}

	resp := &TransactionResponse{
		Succeeded: succeeded,
		Responses: responses,
		Timestamp: time.Now().UnixNano(),
		Message:   fmt.Sprintf("Transaction completed: %d operations executed", len(responses)),
		Revision:  0, // Revision 0 indicates no revision tracking in current transaction
	}

	if t.svc.metrics != nil {
		t.svc.metrics.requestsTotal.With("Transaction", "success").Inc()
	}

	return resp, nil
}

func (t *transactionService) evaluateCompare(ctx context.Context, shardID uint64, cmp *CompareCondition) (bool, error) {
	key := []byte(fmt.Sprintf("%s:%d", cmp.EntityType, cmp.ID))

	// Read via qrdb SDK
	client, err := t.svc.qrdbForShard(shardID)
	if err != nil {
		return false, err
	}
	data, err := client.KV().Get(ctx, string(key))

	// Check if entity exists: error OR nil data means not found
	entityExists := err == nil && data != nil

	if !entityExists {
		if cmp.Target == CompareTargetExists {
			// Entity not found: Equal operator fails (exists != true), NotEqual succeeds (exists == false)
			return cmp.Operator == CompareOperatorNotEqual, nil
		}
		return false, nil
	}

	if cmp.Target == CompareTargetExists {
		// Entity found: Equal operator succeeds (exists == true), NotEqual fails
		return cmp.Operator == CompareOperatorEqual, nil
	}

	switch cmp.Target {
	case CompareTargetVersion:
		return t.compareVersion(1, cmp.Version, cmp.Operator), nil
	case CompareTargetValue:
		return t.compareValue(data, cmp.Value, cmp.Operator), nil
	default:
		return false, &api.InvalidInputError{Message: "unsupported compare target"}
	}
}

func (t *transactionService) compareVersion(actual, expected uint64, op CompareOperator) bool {
	switch op {
	case CompareOperatorEqual:
		return actual == expected
	case CompareOperatorNotEqual:
		return actual != expected
	case CompareOperatorGreater:
		return actual > expected
	case CompareOperatorLess:
		return actual < expected
	case CompareOperatorGreaterEqual:
		return actual >= expected
	case CompareOperatorLessEqual:
		return actual <= expected
	default:
		return false
	}
}

func (t *transactionService) compareValue(actual, expected []byte, op CompareOperator) bool {
	switch op {
	case CompareOperatorEqual:
		return bytes.Equal(actual, expected)
	case CompareOperatorNotEqual:
		return !bytes.Equal(actual, expected)
	default:
		return false
	}
}

func (t *transactionService) executeTransactionOps(ctx context.Context, shardID uint64, ops []*TransactionOp) ([]*TransactionOpResponse, error) {
	if len(ops) == 0 {
		return []*TransactionOpResponse{}, nil
	}

	// Separate write and read operations
	var batchOps []qrdbtransport.Op
	var writeOpsMetadata []*TransactionOp
	var responses []*TransactionOpResponse

	// Process operations
	for _, op := range ops {
		switch op.OpType {
		case TransactionOpTypeGet:
			resp, err := t.executeGetOp(ctx, shardID, op)
			if err != nil {
				responses = append(responses, &TransactionOpResponse{
					OpType:     op.OpType,
					EntityType: op.EntityType,
					ID:         op.ID,
					Success:    false,
					Error:      err.Error(),
				})
			} else {
				responses = append(responses, resp)
			}

		case TransactionOpTypePut:
			key := []byte(fmt.Sprintf("%s:%d", op.EntityType, op.ID))
			batchOps = append(batchOps, qrdbtransport.Op{
				Type:  qrdbtransport.OpPut,
				Key:   key,
				Value: op.Data,
			})
			writeOpsMetadata = append(writeOpsMetadata, op)

		case TransactionOpTypeDelete:
			key := []byte(fmt.Sprintf("%s:%d", op.EntityType, op.ID))
			batchOps = append(batchOps, qrdbtransport.Op{
				Type: qrdbtransport.OpDelete,
				Key:  key,
			})
			writeOpsMetadata = append(writeOpsMetadata, op)
		}
	}

	// Execute all write operations in a single batch via qrdb SDK
	if len(batchOps) > 0 {
		client, err := t.svc.qrdbForShard(shardID)
		if err != nil {
			for _, op := range writeOpsMetadata {
				responses = append(responses, &TransactionOpResponse{
					OpType:     op.OpType,
					EntityType: op.EntityType,
					ID:         op.ID,
					Success:    false,
					Error:      err.Error(),
				})
			}
			return responses, err
		}
		if _, err := client.Batch().ExecuteBatch(ctx, batchOps); err != nil {
			for _, op := range writeOpsMetadata {
				responses = append(responses, &TransactionOpResponse{
					OpType:     op.OpType,
					EntityType: op.EntityType,
					ID:         op.ID,
					Success:    false,
					Error:      err.Error(),
				})
			}
			return responses, err
		}

		timestamp := time.Now().UnixNano()
		for _, op := range writeOpsMetadata {
			responses = append(responses, &TransactionOpResponse{
				OpType:     op.OpType,
				EntityType: op.EntityType,
				ID:         op.ID,
				Success:    true,
				Timestamp:  timestamp,
			})
		}
	}

	return responses, nil
}

func (t *transactionService) executeGetOp(ctx context.Context, shardID uint64, op *TransactionOp) (*TransactionOpResponse, error) {
	key := []byte(fmt.Sprintf("%s:%d", op.EntityType, op.ID))

	// Read via qrdb SDK
	client, err := t.svc.qrdbForShard(shardID)
	if err != nil {
		return nil, err
	}
	data, err := client.KV().Get(ctx, string(key))
	if err != nil {
		return nil, err
	}

	return &TransactionOpResponse{
		OpType:     op.OpType,
		EntityType: op.EntityType,
		ID:         op.ID,
		Data:       data,
		Success:    true,
		Version:    1,
		Timestamp:  time.Now().UnixNano(),
	}, nil
}

func (t *transactionService) validateTransactionRequest(req *TransactionRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Transaction",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	return nil
}

// =============================================================================
// Stub Implementations for Other Services
// =============================================================================

type queryService struct{ svc *quicBaaS }

func (q *queryService) Query(ctx context.Context, req *QueryRequest, stream chan<- *QueryResponse) error {
	defer close(stream)

	start := time.Now()
	if q.svc.metrics != nil {
		q.svc.metrics.activeRequests.Inc()
		defer func() {
			q.svc.metrics.activeRequests.Dec()
			q.svc.metrics.requestDuration.With("Query").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req.EntityType == "" {
		if q.svc.metrics != nil {
			q.svc.metrics.requestsTotal.With("Query", "invalid").Inc()
		}
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Query",
			Err:  &api.RequiredFieldError{Field: "entity_type"},
		}
	}

	// Determine shard ID
	shardID := req.ShardID
	if shardID == 0 {
		shardID = 1
	}

	// Scan via qrdb SDK for prefix scan
	prefix := req.EntityType + ":"
	client, err := q.svc.qrdbForShard(shardID)
	if err != nil {
		if q.svc.metrics != nil {
			q.svc.metrics.requestsTotal.With("Query", "error").Inc()
			q.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Query",
			Err:  err,
		}
	}
	scanResult, err := client.KV().Scan(ctx, prefix)
	if err != nil {
		if q.svc.metrics != nil {
			q.svc.metrics.requestsTotal.With("Query", "error").Inc()
			q.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Query",
			Err:  err,
		}
	}

	// Parse results and filter by value if provided
	var entities []*Entity
	for keyStr, data := range scanResult {
		id, parseErr := strconv.ParseUint(keyStr[strings.LastIndex(keyStr, ":")+1:], 10, 64)
		if parseErr != nil {
			// A key without a numeric entity ID is still returned, with ID 0
			id = 0
		}

		// Filter by index value if requested
		if req.IndexName != "" && req.Value != nil {
			if !matchesIndexValue(data, req.IndexName, req.Value) {
				continue
			}
		}

		entities = append(entities, &Entity{
			EntityType: req.EntityType,
			ID:         id,
			Data:       data,
			Version:    1,
			Timestamp:  time.Now().UnixNano(),
		})
	}

	// Send all entities in a single response
	// In a full implementation, this would support pagination
	resp := &QueryResponse{
		Entities:      entities,
		TotalCount:    uint64(len(entities)),
		NextPageToken: "",
	}

	select {
	case stream <- resp:
	case <-ctx.Done():
		return ctx.Err()
	}

	if q.svc.metrics != nil {
		q.svc.metrics.requestsTotal.With("Query", "success").Inc()
	}

	return nil
}

// matchesIndexValue checks if a JSON document matches an index value
// This is a simple MVP implementation for basic filtering.
func matchesIndexValue(data []byte, indexName string, value []byte) bool {
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false
	}

	// Get the field value
	fieldValue, exists := doc[indexName]
	if !exists {
		return false
	}

	// Convert to string for comparison
	fieldStr := fmt.Sprintf("%v", fieldValue)
	valueStr := string(value)

	return fieldStr == valueStr
}

func (q *queryService) ScanIndex(ctx context.Context, req *ScanRequest, stream chan<- *ScanResponse) error {
	defer close(stream)
	// ScanIndex is not yet implemented - returns NotImplemented error
	return &errors.QBError{
		Code: errors.ErrNotImplemented,
		Op:   "service.ScanIndex",
		Err:  &api.NotImplementedError{Feature: "scan index"},
	}
}

type clusterService struct{ svc *quicBaaS }

func (c *clusterService) ClusterStatus(ctx context.Context, req *ClusterStatusRequest) (*ClusterStatusResponse, error) {
	_ = req

	resp := &ClusterStatusResponse{
		ClusterID: "quicbaas-cluster",
		Nodes:     []*NodeInfo{},
		Shards:    []*ShardInfo{},
		Timestamp: time.Now().UnixNano(),
	}

	// Use SDK client to get topology
	client, err := c.svc.qrdbForShard(1) // Use shard 1 for admin ops
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.ClusterStatus",
			Err:  err,
		}
	}
	topology, err := client.Admin().GetTopology(ctx)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.ClusterStatus",
			Err:  err,
		}
	}

	if topology != nil {
		// Track unique nodes and their leader status across all shards
		nodeMap := make(map[uint64]*NodeInfo)
		leaderNodes := make(map[uint64]bool)

		for _, shardInfo := range topology.Shards {
			replicas := make([]uint64, 0, len(shardInfo.Nodes))
			for nodeID, address := range shardInfo.Nodes {
				replicas = append(replicas, nodeID)

				// Add node to map if not already present
				if _, exists := nodeMap[nodeID]; !exists {
					nodeMap[nodeID] = &NodeInfo{
						NodeID:       nodeID,
						Address:      address,
						IsLeader:     false,
						HealthStatus: HealthStatusHealthy,
					}
				}
			}

			// Track if this shard's leader exists
			if shardInfo.LeaderID > 0 {
				leaderNodes[shardInfo.LeaderID] = true
			}

			resp.Shards = append(resp.Shards, &ShardInfo{
				ShardID:  shardInfo.ShardID,
				LeaderID: shardInfo.LeaderID,
				Replicas: replicas,
				Status:   HealthStatusHealthy,
			})
		}

		// Mark leader nodes
		for nodeID := range leaderNodes {
			if node, exists := nodeMap[nodeID]; exists {
				node.IsLeader = true
			}
		}

		// Convert map to slice
		for _, node := range nodeMap {
			resp.Nodes = append(resp.Nodes, node)
		}
	}

	return resp, nil
}

func (c *clusterService) GetLeader(ctx context.Context, req *GetLeaderRequest) (*GetLeaderResponse, error) {
	client, err := c.svc.qrdbForShard(req.ShardID)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.GetLeader",
			Err:  err,
		}
	}
	state, err := client.Admin().GetShardState(ctx, req.ShardID)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.GetLeader",
			Err:  err,
		}
	}
	return &GetLeaderResponse{
		ShardID:  req.ShardID,
		LeaderID: state.LeaderID,
	}, nil
}

func (c *clusterService) AddNode(ctx context.Context, req *AddNodeRequest) (*AddNodeResponse, error) {
	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.AddNode",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.ShardID == 0 {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.AddNode",
			Err:  &api.InvalidInputError{Message: "shard_id must be greater than 0"},
		}
	}
	if req.NodeID == 0 {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.AddNode",
			Err:  &api.InvalidInputError{Message: "node_id must be greater than 0"},
		}
	}
	if req.Address == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.AddNode",
			Err:  &api.EmptyCollectionError{Field: "address"},
		}
	}

	// Use SDK to add member
	client, err := c.svc.qrdbForShard(req.ShardID)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.AddNode",
			Err:  err,
		}
	}
	if err := client.Admin().AddMember(ctx, req.ShardID, req.NodeID, req.Address, req.Identity); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.AddNode",
			Err:  &api.OperationFailedError{Operation: "add node to shard", Cause: err},
		}
	}

	return &AddNodeResponse{
		Success: true,
		Message: "node added to shard",
	}, nil
}

func (c *clusterService) RemoveNode(ctx context.Context, req *RemoveNodeRequest) (*RemoveNodeResponse, error) {
	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.RemoveNode",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.ShardID == 0 {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.RemoveNode",
			Err:  &api.InvalidInputError{Message: "shard_id must be greater than 0"},
		}
	}
	if req.NodeID == 0 {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.RemoveNode",
			Err:  &api.InvalidInputError{Message: "node_id must be greater than 0"},
		}
	}

	// Use SDK to remove member
	client, err := c.svc.qrdbForShard(req.ShardID)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.RemoveNode",
			Err:  err,
		}
	}
	if err := client.Admin().RemoveMember(ctx, req.ShardID, req.NodeID); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.RemoveNode",
			Err:  &api.OperationFailedError{Operation: "remove node from shard", Cause: err},
		}
	}

	return &RemoveNodeResponse{
		Success: true,
		Message: "node removed from shard",
	}, nil
}

func (c *clusterService) CreateShard(ctx context.Context, req *CreateShardRequest) (*CreateShardResponse, error) {
	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.CreateShard",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.ShardID == 0 {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.CreateShard",
			Err:  &api.InvalidInputError{Message: "shard_id must be greater than 0"},
		}
	}
	if len(req.InitialMembers) == 0 {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.CreateShard",
			Err:  &api.EmptyCollectionError{Field: "initial_members"},
		}
	}

	// Check if SDK client is available
	if c.svc.qrdbClient == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.CreateShard",
			Err:  &api.ServiceNotConfiguredError{Service: "qrdb client"},
		}
	}

	// CreateShard is currently not fully implemented
	// The full implementation would require:
	// 1. Creating a RaftGroupConfig with the shard ID and initial members
	// 2. Creating a state machine factory for the new shard
	// 3. Calling raftNode.StartShard() with the config and factory
	//
	// For now, return an error indicating this needs to be implemented
	// with the proper state machine setup
	return nil, &errors.QBError{
		Code: errors.ErrNotImplemented,
		Op:   "service.CreateShard",
		Err:  &api.NotImplementedError{Feature: "CreateShard (requires state machine factory setup)"},
	}
}

func (c *clusterService) RebalanceCluster(ctx context.Context, req *RebalanceClusterRequest) (*RebalanceClusterResponse, error) {
	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.RebalanceCluster",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	// Check if SDK client is available
	if c.svc.qrdbClient == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.RebalanceCluster",
			Err:  &api.ServiceNotConfiguredError{Service: "qrdb client"},
		}
	}

	// Generate operation ID
	operationID := fmt.Sprintf("rebalance-%d", time.Now().UnixNano())

	// Log the rebalance request
	if c.svc.logger != nil {
		c.svc.logger.WithFields(map[string]interface{}{
			"operation_id":       operationID,
			"target_shard_count": req.TargetShardCount,
			"strategy":           req.Strategy,
		}).Info("Cluster rebalance initiated")
	}

	return &RebalanceClusterResponse{
		JobID:               operationID,
		Progress:            0.0,
		EstimatedCompletion: time.Now().Add(5 * time.Minute).UnixNano(),
	}, nil
}

func (c *clusterService) GetRebalanceProgress(ctx context.Context, req *GetRebalanceProgressRequest) (*GetRebalanceProgressResponse, error) {
	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.GetRebalanceProgress",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.OperationID == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.GetRebalanceProgress",
			Err:  &api.RequiredFieldError{Field: "operation_id"},
		}
	}

	return nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "service.GetRebalanceProgress",
		Err:  &api.NotFoundError{Resource: "operation", ID: req.OperationID},
	}
}

type backupService struct{ svc *quicBaaS }

func (b *backupService) CreateBackup(ctx context.Context, req *CreateBackupRequest) (*CreateBackupResponse, error) {
	// Check if backup manager is available
	if b.svc.backupMgr == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.CreateBackup",
			Err:  &api.ServiceNotConfiguredError{Service: "backup manager"},
		}
	}

	// Map backup type from string to backup.BackupType
	var backupType backup.BackupType
	switch req.Name {
	case "full":
		backupType = backup.BackupTypeFull
	case "incremental":
		backupType = backup.BackupTypeIncremental
	default:
		backupType = backup.BackupTypeFull // Default to full backup
	}

	// Create backup using backup manager
	backupResultRaw, err := b.svc.backupMgr.CreateBackup(ctx, backupType)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.CreateBackup",
			Err:  &api.OperationFailedError{Operation: "create backup", Cause: err},
		}
	}

	backupResult, ok := backupResultRaw.(*backup.Backup)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.CreateBackup",
			Err:  &api.InvalidTypeError{Field: "backup result", Expected: "*backup.Backup"},
		}
	}

	// Map result to service response
	resp := &CreateBackupResponse{
		BackupID:  backupResult.ID,
		Size:      conv.ClampInt64ToUint64(backupResult.Size),
		Timestamp: backupResult.Timestamp.UnixNano(),
		Status:    "completed",
	}

	return resp, nil
}

func (b *backupService) ListBackups(ctx context.Context, req *ListBackupsRequest) (*ListBackupsResponse, error) {
	// Check if backup manager is available
	if b.svc.backupMgr == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.ListBackups",
			Err:  &api.ServiceNotConfiguredError{Service: "backup manager"},
		}
	}

	// Determine limit
	limit := conv.ClampUint64ToInt(req.Limit)
	if limit == 0 {
		limit = 100 // Default limit
	}

	// List backups using backup manager
	backupListRaw, err := b.svc.backupMgr.ListBackups(ctx, limit)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.ListBackups",
			Err:  &api.OperationFailedError{Operation: "list backups", Cause: err},
		}
	}
	backupList, ok := backupListRaw.(*backup.BackupList)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.ListBackups",
			Err:  &api.InvalidTypeError{Field: "backup list", Expected: "*backup.BackupList"},
		}
	}

	// Map results to service response
	backups := make([]*BackupMetadata, 0, len(backupList.Backups))
	for _, b := range backupList.Backups {
		// Extract shard IDs
		shards := make([]uint64, 0, len(b.Shards))
		for shardID := range b.Shards {
			shards = append(shards, shardID)
		}

		backups = append(backups, &BackupMetadata{
			BackupID:    b.ID,
			Name:        string(b.Type),
			Description: fmt.Sprintf("Backup created at %s", b.Timestamp.Format(time.RFC3339)),
			Size:        conv.ClampInt64ToUint64(b.Size),
			Timestamp:   b.Timestamp.UnixNano(),
			Status:      "completed",
			Shards:      shards,
		})
	}

	resp := &ListBackupsResponse{
		Backups:    backups,
		TotalCount: conv.ClampIntToUint64(backupList.TotalCount),
	}

	return resp, nil
}

func (b *backupService) RestoreBackup(ctx context.Context, req *RestoreBackupRequest) (*RestoreBackupResponse, error) {
	// Check if backup manager is available
	if b.svc.backupMgr == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.RestoreBackup",
			Err:  &api.ServiceNotConfiguredError{Service: "backup manager"},
		}
	}

	// Build restore options
	opts := &backup.RestoreOptions{
		VerifyChecksum: true,
		TargetShards:   req.TargetShards,
		SkipExisting:   false,
	}

	// Restore backup using backup manager
	err := b.svc.backupMgr.RestoreBackup(ctx, req.BackupID, opts)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.RestoreBackup",
			Err:  &api.OperationFailedError{Operation: "restore backup", Cause: err},
		}
	}

	// Map result to service response
	resp := &RestoreBackupResponse{
		JobID:    fmt.Sprintf("restore-%s", req.BackupID),
		Status:   "completed",
		Progress: 100.0,
	}

	return resp, nil
}

func (b *backupService) DeleteBackup(ctx context.Context, req *DeleteBackupRequest) (*DeleteBackupResponse, error) {
	// Check if backup manager is available
	if b.svc.backupMgr == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.DeleteBackup",
			Err:  &api.ServiceNotConfiguredError{Service: "backup manager"},
		}
	}

	// Delete backup using backup manager
	err := b.svc.backupMgr.DeleteBackup(ctx, req.BackupID)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.DeleteBackup",
			Err:  &api.OperationFailedError{Operation: "delete backup", Cause: err},
		}
	}

	// Map result to service response
	resp := &DeleteBackupResponse{
		Success: true,
		Message: fmt.Sprintf("Backup %s deleted successfully", req.BackupID),
	}

	return resp, nil
}

func (b *backupService) VerifyBackup(ctx context.Context, req *VerifyBackupRequest) (*VerifyBackupResponse, error) {
	// Check if backup manager is available
	if b.svc.backupMgr == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.VerifyBackup",
			Err:  &api.ServiceNotConfiguredError{Service: "backup manager"},
		}
	}

	// Verify backup using backup manager
	resultRaw, err := b.svc.backupMgr.VerifyBackup(ctx, req.BackupID)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.VerifyBackup",
			Err:  &api.OperationFailedError{Operation: "verify backup", Cause: err},
		}
	}
	result, ok := resultRaw.(*backup.VerificationResult)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.VerifyBackup",
			Err:  &api.InvalidTypeError{Field: "verification result", Expected: "*backup.VerificationResult"},
		}
	}

	// Map result to service response
	resp := &VerifyBackupResponse{
		Valid:         result.Valid,
		ChecksumMatch: result.ChecksumValid,
		Issues:        result.Errors,
	}

	return resp, nil
}

type authService struct{ svc *quicBaaS }

func (a *authService) Login(_ context.Context, _ *LoginRequest) (*LoginResponse, error) {
	return nil, api.NewNotImplementedError("password-based login; use WebAuthn or PIV authentication")
}

// =============================================================================
// Session token lifecycle (logout / refresh / validate).
//
// These three verbs are backed by the OIDC provider — the single authority that
// minted the session bearer in the first place (login ceremony, WebAuthn/PIV
// hardware login, or OAuth2 grant). They previously routed through an
// interfaces.AuthManager that is never wired in a running daemon, so every call
// failed with "authentication service not configured": the /api/v1/auth/refresh
// endpoint the SPA and CLI depend on could not renew anything and a 15-minute
// access token was effectively the whole session.
// =============================================================================

// sessionAuthority resolves the OIDC provider that owns session tokens.
func (a *authService) sessionAuthority(ctx context.Context) (*iam.OIDCProvider, error) {
	provider, err := a.svc.GetOIDCProvider(ctx)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "service.sessionAuthority",
			Err:  err,
		}
	}
	if provider == nil {
		return nil, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "service.sessionAuthority",
			Err:  &api.ServiceNotConfiguredError{Service: "authentication"},
		}
	}
	return provider, nil
}

func (a *authService) Logout(ctx context.Context, req *LogoutRequest) (*LogoutResponse, error) {
	provider, err := a.sessionAuthority(ctx)
	if err != nil {
		return nil, err
	}
	if err := provider.RevokeSessionToken(ctx, req.AccessToken); err != nil {
		return &LogoutResponse{
			Success: false,
			Message: "failed to revoke token",
		}, &errors.QBError{
			Code: errors.ErrUnauthorized,
			Op:   "service.Logout",
			Err:  err,
		}
	}
	return &LogoutResponse{
		Success: true,
		Message: "Successfully logged out",
	}, nil
}

func (a *authService) RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*RefreshTokenResponse, error) {
	if req == nil || req.RefreshToken == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.RefreshToken",
			Err:  &iam.SessionRefreshTokenRequiredError{},
		}
	}
	provider, err := a.sessionAuthority(ctx)
	if err != nil {
		return nil, err
	}

	tokens, err := provider.RefreshSession(ctx, iam.SessionRefreshRequest{
		RefreshToken: req.RefreshToken,
		DPoPProof:    req.DPoPProof,
		DPoPMethod:   req.DPoPMethod,
		DPoPURI:      req.DPoPURI,
	})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrUnauthorized,
			Op:   "service.RefreshToken",
			Err:  err,
		}
	}

	return &RefreshTokenResponse{
		AccessToken:      tokens.AccessToken,
		ExpiresAt:        tokens.ExpiresAt.Unix(),
		RefreshToken:     tokens.RefreshToken,
		RefreshExpiresAt: tokens.RefreshExpiresAt.Unix(),
		TokenType:        tokens.TokenType,
	}, nil
}

func (a *authService) ValidateToken(ctx context.Context, req *ValidateTokenRequest) (*ValidateTokenResponse, error) {
	provider, err := a.sessionAuthority(ctx)
	if err != nil {
		return nil, err
	}

	claims, err := provider.ValidateAccessToken(ctx, req.AccessToken)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrUnauthorized,
			Op:   "service.ValidateToken",
			Err:  err,
		}
	}

	return &ValidateTokenResponse{
		Valid:     true,
		UserID:    claims.Subject,
		Username:  claims.Email,
		TenantID:  claims.TenantID,
		Roles:     claims.Roles,
		ExpiresAt: claims.ExpiresAt.Unix(),
	}, nil
}

// IntrospectToken answers an RFC 7662 introspection for every transport. It
// asks the same OIDC provider the REST /oauth2/introspect endpoint asks, so a
// token is active on one transport exactly when it is active on all of them.
// An inactive token is a result, not an error: the reply is Active=false and
// nothing else, so no claim of a dead token leaks.
func (a *authService) IntrospectToken(ctx context.Context, req *IntrospectTokenRequest) (*TokenIntrospection, error) {
	if req == nil || req.Token == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.IntrospectToken",
			Err:  &api.RequiredFieldError{Field: "token"},
		}
	}
	provider, err := a.sessionAuthority(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := provider.Introspect(ctx, &iam.IntrospectRequest{Token: req.Token})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.IntrospectToken",
			Err:  err,
		}
	}
	return tokenIntrospectionFromIAM(resp), nil
}

// tokenIntrospectionFromIAM converts the provider's RFC 7662 reply into the
// transport-neutral service model. Unix-second times of zero stay zero.
func tokenIntrospectionFromIAM(resp *iam.IntrospectionResponse) *TokenIntrospection {
	if !resp.Active {
		return &TokenIntrospection{Active: false}
	}
	return &TokenIntrospection{
		Active:       true,
		Scope:        resp.Scope,
		ClientID:     resp.ClientID,
		Username:     resp.Username,
		TokenType:    resp.TokenType,
		ExpiresAt:    unixTimeOrZero(resp.ExpiresAt),
		IssuedAt:     unixTimeOrZero(resp.IssuedAt),
		NotBefore:    unixTimeOrZero(resp.NotBefore),
		Subject:      resp.Subject,
		Audience:     resp.Audience,
		Issuer:       resp.Issuer,
		JTI:          resp.JTI,
		AttestLevel:  resp.AttestLevel,
		AttestAAGUID: resp.AttestAAGUID,
	}
}

// unixTimeOrZero converts Unix seconds to a time, keeping 0 as the zero time.
func unixTimeOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// rbacService serves the role half of the RBAC surface. Its verbs all operate
// on the qrdb-backed IdentityStore and live in rbac_role_store.go: there is no
// user half, because users are qrdb's and are reached through the IAM surface
// (Identity().IAM().User() on the SDK, quicbaas_iam_user_* on MCP). The
// CreateUser/DeleteUser/ListUsers trio that used to live here routed through a
// local auth manager that production never wired, so every call failed; it was
// deleted with the parallel identity model in issue #905.
type rbacService struct{ svc *quicBaaS }

// CreateRole, DeleteRole and ListRoles operate on the qrdb-backed
// IdentityStore and live in rbac_role_store.go. See authz-unification
// Stage 1.2.

// AssignRole and RevokeRole bind/unbind a named role to a user by username,
// routed through the qrdb-backed IdentityStore so the user->role binding lands
// in the SAME qrdb user record the policy decision point reads. They live in
// rbac_role_store.go alongside CreateRole/DeleteRole/ListRoles. See
// authz-unification C1-E.

type cacheService struct{ svc *quicBaaS }

func (c *cacheService) GetCacheStats(ctx context.Context, req *GetCacheStatsRequest) (*GetCacheStatsResponse, error) {
	// Check if cache manager is available
	if c.svc.cacheManager == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.GetCacheStats",
			Err:  &api.ServiceNotConfiguredError{Service: "cache manager"},
		}
	}

	// Get stats from cache manager (type assert to concrete type)
	cacheMgr, ok := c.svc.cacheManager.(*cache.CacheManager)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.Cache",
			Err:  &api.InvalidTypeError{Field: "cache manager", Expected: "*cache.CacheManager"},
		}
	}
	stats := cacheMgr.Stats()

	// Map to response
	resp := &GetCacheStatsResponse{
		HitRate:   stats.L1HitRate,
		Size:      conv.ClampIntToUint64(stats.L1Size),
		MaxSize:   conv.ClampIntToUint64(stats.L1Capacity),
		Evictions: conv.ClampInt64ToUint64(stats.L1Evictions),
		Hits:      conv.ClampInt64ToUint64(stats.L1Hits),
		Misses:    conv.ClampInt64ToUint64(stats.L1Misses),
	}

	return resp, nil
}

func (c *cacheService) ClearCache(ctx context.Context, req *ClearCacheRequest) (*ClearCacheResponse, error) {
	// Check if cache manager is available
	if c.svc.cacheManager == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.ClearCache",
			Err:  &api.ServiceNotConfiguredError{Service: "cache manager"},
		}
	}

	// Get current size before clearing (type assert to concrete type)
	cacheMgr, ok := c.svc.cacheManager.(*cache.CacheManager)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.ClearCache",
			Err:  &api.InvalidTypeError{Field: "cache manager", Expected: "*cache.CacheManager"},
		}
	}
	stats := cacheMgr.Stats()
	entriesCleared := conv.ClampIntToUint64(stats.L1Size)

	// Clear the cache
	c.svc.cacheManager.Clear()

	resp := &ClearCacheResponse{
		EntriesCleared: entriesCleared,
	}

	return resp, nil
}

func (c *cacheService) GetCacheConfig(ctx context.Context, req *GetCacheConfigRequest) (*GetCacheConfigResponse, error) {
	// Check if cache manager is available
	if c.svc.cacheManager == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.GetCacheConfig",
			Err:  &api.ServiceNotConfiguredError{Service: "cache manager"},
		}
	}

	// Get config from cache manager (type assert to concrete type)
	cacheMgr, ok := c.svc.cacheManager.(*cache.CacheManager)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.GetCacheConfig",
			Err:  &api.InvalidTypeError{Field: "cache manager", Expected: "*cache.CacheManager"},
		}
	}
	config := cacheMgr.GetConfig()

	resp := &GetCacheConfigResponse{
		L1Size:          config.L1Size,
		L1TTL:           config.L1TTL,
		CleanupInterval: config.CleanupInterval,
		Enabled:         true,
	}

	return resp, nil
}

func (c *cacheService) UpdateCacheConfig(ctx context.Context, req *UpdateCacheConfigRequest) (*UpdateCacheConfigResponse, error) {
	// Check if cache manager is available
	if c.svc.cacheManager == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "service.UpdateCacheConfig",
			Err:  &api.ServiceNotConfiguredError{Service: "cache manager"},
		}
	}

	// Type assert to concrete type for methods not in interface
	cacheMgr, ok := c.svc.cacheManager.(*cache.CacheManager)
	if !ok {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.UpdateCacheConfig",
			Err:  &api.InvalidTypeError{Field: "cache manager", Expected: "*cache.CacheManager"},
		}
	}

	// Get current config
	currentConfig := cacheMgr.GetConfig()

	// Build new config with updates
	newConfig := &cache.Config{
		L1Size:          currentConfig.L1Size,
		L1TTL:           currentConfig.L1TTL,
		CleanupInterval: currentConfig.CleanupInterval,
	}

	// Apply updates from request
	if req.L1Size != nil {
		newConfig.L1Size = *req.L1Size
	}
	if req.L1TTL != nil {
		newConfig.L1TTL = *req.L1TTL
	}
	if req.CleanupInterval != nil {
		newConfig.CleanupInterval = *req.CleanupInterval
	}

	// Update cache manager config
	if err := cacheMgr.UpdateConfig(newConfig); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.UpdateCacheConfig",
			Err:  err,
		}
	}

	// Get updated config
	updatedConfig := cacheMgr.GetConfig()

	resp := &UpdateCacheConfigResponse{
		Updated: true,
		NewConfig: &GetCacheConfigResponse{
			L1Size:          updatedConfig.L1Size,
			L1TTL:           updatedConfig.L1TTL,
			CleanupInterval: updatedConfig.CleanupInterval,
			Enabled:         true,
		},
	}

	return resp, nil
}

type healthService struct {
	svc     *quicBaaS
	checker *qrdbsdk.HealthChecker
}

// newHealthService builds the health service backed by the stack's shared
// health-check framework (qrdb SDK HealthChecker). Component checks are
// registered here; Health executes them and maps the shared results onto
// the service wire types, replacing the previous hardcoded "healthy" report.
func newHealthService(svc *quicBaaS) *healthService {
	h := &healthService{svc: svc, checker: qrdbsdk.NewHealthChecker()}
	h.checker.RegisterCheck("raft", func(ctx context.Context) qrdbsdk.HealthCheckResult {
		ready, reason := h.raftReadiness(ctx)
		if ready {
			return qrdbsdk.HealthCheckResult{
				Name:    "raft",
				Status:  qrdbsdk.HealthCheckStatusHealthy,
				Message: "Raft operational",
			}
		}
		return qrdbsdk.HealthCheckResult{
			Name:    "raft",
			Status:  qrdbsdk.HealthCheckStatusUnhealthy,
			Message: reason,
		}
	})
	h.checker.MarkStarted()
	return h
}

// mapSharedHealthStatus converts the shared framework status to the service
// wire enum (which additionally carries HealthStatusUnknown).
func mapSharedHealthStatus(s qrdbsdk.HealthCheckStatus) HealthStatus {
	switch s {
	case qrdbsdk.HealthCheckStatusHealthy:
		return HealthStatusHealthy
	case qrdbsdk.HealthCheckStatusDegraded:
		return HealthStatusDegraded
	case qrdbsdk.HealthCheckStatusUnhealthy:
		return HealthStatusUnhealthy
	default:
		return HealthStatusUnknown
	}
}

func (h *healthService) Health(ctx context.Context, req *HealthRequest) (*HealthResponse, error) {
	results := h.checker.Ready(ctx)

	resp := &HealthResponse{
		Status:        mapSharedHealthStatus(qrdbsdk.AggregateHealthCheckStatus(results)),
		NodeID:        h.svc.config.NodeID,
		Version:       h.svc.config.Version,
		UptimeSeconds: uint64(time.Since(h.svc.startTime).Seconds()),
	}

	if req.IncludeDetails {
		resp.Details = make(map[string]*ComponentHealth, len(results))
		for _, res := range results {
			resp.Details[res.Name] = &ComponentHealth{
				Status:    mapSharedHealthStatus(res.Status),
				Message:   res.Message,
				LastCheck: time.Now().UnixNano(),
			}
		}
	}

	return resp, nil
}

func (h *healthService) Liveness(ctx context.Context, req *LivenessRequest) (*LivenessResponse, error) {
	return &LivenessResponse{Alive: true}, nil
}

// raftReadiness reports whether the Raft/data plane is ready to serve, and a
// human-readable reason when it is not. It is the single readiness predicate
// shared by the readiness probe and the registered "raft" health check.
func (h *healthService) raftReadiness(ctx context.Context) (bool, string) {
	if h.svc == nil || h.svc.config == nil {
		return false, "service not initialized"
	}

	// In standalone mode, no Raft shard leaders are required
	if h.svc.config.Mode == "" || h.svc.config.Mode == "standalone" {
		return true, ""
	}

	// If no qrdb client, we're in local/memory mode - always ready
	if h.svc.qrdbClient == nil {
		return true, ""
	}

	// Check all configured shards have elected leaders.
	//
	// Use GetTopology (the node-host shard view) rather than per-shard
	// GetShardState. GetTopology reports the same leader/membership state the
	// cluster-status endpoint exposes and that quicraft's NodeHostInfo sees,
	// whereas GetShardState reads the engine's per-shard Raft peer
	// (GetShardMembership), which returns ErrShardNotReady when the peer handle
	// lags the node-host view on an otherwise healthy shard — making readiness
	// flap to 503 on a cluster that has a stable leader. If the SDK client
	// cannot verify topology (e.g., standalone nodes without full consensus),
	// fall back to ready when a local raft node can serve reads.
	client, err := h.svc.qrdbForShard(1) // shard 1 is the admin entry point
	if err != nil {
		if h.svc.raftNode != nil {
			return true, ""
		}
		return false, "shard has no SDK client available"
	}
	topology, err := client.Admin().GetTopology(ctx)
	if err != nil {
		if h.svc.raftNode != nil {
			return true, ""
		}
		return false, "shard state check failed"
	}

	// Require every Raft shard reported by the node-host topology to have an
	// elected leader. The service config also lists non-Raft service shards
	// (e.g. the vector shard, raft_enabled=false), which never appear in the
	// topology — those must NOT be gated here, or readiness stays at 503 on an
	// otherwise-healthy cluster.
	sawShard := false
	for _, si := range topology.Shards {
		sawShard = true
		if si.LeaderID == 0 {
			if h.svc.raftNode != nil {
				continue
			}
			return false, "shard has no leader elected"
		}
	}
	if !sawShard && h.svc.raftNode == nil {
		return false, "no raft shards available"
	}

	return true, ""
}

func (h *healthService) Readiness(ctx context.Context, req *ReadinessRequest) (*ReadinessResponse, error) {
	ready, reason := h.raftReadiness(ctx)
	return &ReadinessResponse{Ready: ready, Reason: reason}, nil
}

type timeSeriesService struct {
	svc       *quicBaaS
	tsStorage interfaces.TimeSeriesStorage
	shardID   uint64
}

func (t *timeSeriesService) Aggregate(ctx context.Context, req *AggregateRequest) (*AggregateResponse, error) {
	start := time.Now()
	if t.svc.metrics != nil {
		t.svc.metrics.activeRequests.Inc()
		defer func() {
			t.svc.metrics.activeRequests.Dec()
			t.svc.metrics.requestDuration.With("Aggregate").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := t.validateAggregateRequest(req); err != nil {
		if t.svc.metrics != nil {
			t.svc.metrics.requestsTotal.With("Aggregate", "invalid").Inc()
		}
		return nil, err
	}

	// Default to shard 1
	shardID := uint64(1)

	// Query entities through qrdb SDK
	client, err := t.svc.qrdbForShard(shardID)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.Aggregate",
			Err:  err,
		}
	}

	entities, err := client.KV().Scan(ctx, req.EntityType)
	if err != nil {
		if t.svc.metrics != nil {
			t.svc.metrics.requestsTotal.With("Aggregate", "error").Inc()
			t.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.Aggregate",
			Err:  err,
		}
	}

	// Parse entities and extract time-series data
	var values []float64
	var count uint64

	for _, data := range entities {
		// Try to parse as JSON to extract timestamp and value
		var entity map[string]interface{}
		if err := json.Unmarshal(data, &entity); err != nil {
			continue // Skip non-JSON entities
		}

		// Extract timestamp field (support multiple formats)
		var timestamp int64
		if ts, ok := entity["timestamp"].(float64); ok {
			timestamp = int64(ts)
		} else if ts, ok := entity["ts"].(float64); ok {
			timestamp = int64(ts)
		} else if ts, ok := entity["time"].(float64); ok {
			timestamp = int64(ts)
		} else {
			continue // Skip if no timestamp
		}

		// Filter by time range
		if timestamp < req.StartTime || timestamp > req.EndTime {
			continue
		}

		// Extract field value
		var value float64
		if req.FieldName != "" {
			if v, ok := entity[req.FieldName].(float64); ok {
				value = v
			} else {
				continue // Skip if field not found or not numeric
			}
		} else {
			// If no field specified, try "value" as default
			if v, ok := entity["value"].(float64); ok {
				value = v
			} else {
				continue
			}
		}

		values = append(values, value)
		count++
	}

	// Apply aggregation function
	var aggregateResult float64
	switch req.Function {
	case AggregateFunctionSum:
		for _, v := range values {
			aggregateResult += v
		}
	case AggregateFunctionAvg:
		if count > 0 {
			sum := 0.0
			for _, v := range values {
				sum += v
			}
			aggregateResult = sum / float64(count)
		}
	case AggregateFunctionMin:
		if count > 0 {
			aggregateResult = math.MaxFloat64
			for _, v := range values {
				if v < aggregateResult {
					aggregateResult = v
				}
			}
		}
	case AggregateFunctionMax:
		if count > 0 {
			aggregateResult = -math.MaxFloat64
			for _, v := range values {
				if v > aggregateResult {
					aggregateResult = v
				}
			}
		}
	case AggregateFunctionCount:
		aggregateResult = float64(count)
	default:
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Aggregate",
			Err:  &api.InvalidInputError{Message: fmt.Sprintf("unsupported aggregation function: %v", req.Function)},
		}
	}

	resp := &AggregateResponse{
		Result:    aggregateResult,
		Count:     count,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}

	if t.svc.metrics != nil {
		t.svc.metrics.requestsTotal.With("Aggregate", "success").Inc()
	}

	if t.svc.logger != nil {
		t.svc.logger.WithFields(map[string]interface{}{
			"entity_type": req.EntityType,
			"field_name":  req.FieldName,
			"function":    req.Function,
			"count":       count,
			"result":      aggregateResult,
		}).Debug("Time-series aggregation completed")
	}

	return resp, nil
}

func (t *timeSeriesService) TimeSeriesQuery(ctx context.Context, req *TimeSeriesQueryRequest, stream chan<- *TimeSeriesQueryResponse) error {
	defer close(stream)

	start := time.Now()
	if t.svc.metrics != nil {
		t.svc.metrics.activeRequests.Inc()
		defer func() {
			t.svc.metrics.activeRequests.Dec()
			t.svc.metrics.requestDuration.With("TimeSeriesQuery").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := t.validateTimeSeriesQueryRequest(req); err != nil {
		if t.svc.metrics != nil {
			t.svc.metrics.requestsTotal.With("TimeSeriesQuery", "invalid").Inc()
		}
		return err
	}

	// Default to shard 1
	shardID := uint64(1)

	// Query entities through qrdb SDK
	client, err := t.svc.qrdbForShard(shardID)
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "service.TimeSeriesQuery",
			Err:  err,
		}
	}

	entities, err := client.KV().Scan(ctx, req.EntityType)
	if err != nil {
		if t.svc.metrics != nil {
			t.svc.metrics.requestsTotal.With("TimeSeriesQuery", "error").Inc()
			t.svc.metrics.errorsByType.With("raft_error").Inc()
		}
		return &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "service.TimeSeriesQuery",
			Err:  err,
		}
	}

	// Parse entities and extract time-series data points
	var dataPoints []*TimeSeriesPoint

	for _, data := range entities {
		// Try to parse as JSON to extract timestamp and value
		var entity map[string]interface{}
		if err := json.Unmarshal(data, &entity); err != nil {
			continue // Skip non-JSON entities
		}

		// Extract timestamp field
		var timestamp int64
		if ts, ok := entity["timestamp"].(float64); ok {
			timestamp = int64(ts)
		} else if ts, ok := entity["ts"].(float64); ok {
			timestamp = int64(ts)
		} else if ts, ok := entity["time"].(float64); ok {
			timestamp = int64(ts)
		} else {
			continue // Skip if no timestamp
		}

		// Filter by time range
		if timestamp < req.StartTime || timestamp > req.EndTime {
			continue
		}

		// Extract value field
		var value float64
		if v, ok := entity["value"].(float64); ok {
			value = v
		} else {
			continue // Skip if no value
		}

		entityID := timeSeriesEntityID(entity)

		// Extract metadata (exclude reserved fields)
		metadata := make(map[string]string)
		for k, v := range entity {
			if k != "timestamp" && k != "ts" && k != "time" && k != "value" && k != "id" && k != "entity_id" {
				if strVal, ok := v.(string); ok {
					metadata[k] = strVal
				}
			}
		}

		dataPoints = append(dataPoints, &TimeSeriesPoint{
			ID:        entityID,
			Timestamp: timestamp,
			Value:     value,
			Data:      data, // Preserve raw serialized entity data
			Metadata:  metadata,
		})
	}

	// Apply limit if specified
	limit := req.Limit
	if limit == 0 {
		limit = 1000 // Default limit
	}

	totalCount := uint64(len(dataPoints))
	limited := dataPoints[:min(len(dataPoints), conv.ClampUint64ToInt(limit))]
	offset := 0

	// Send data points in batches
	batchSize := 100
	for offset < len(limited) {
		end := offset + min(batchSize, len(limited)-offset)

		batch := limited[offset:end]
		nextPageToken := ""
		if end < len(limited) {
			nextPageToken = fmt.Sprintf("%d", end)
		}

		resp := &TimeSeriesQueryResponse{
			DataPoints:    batch,
			TotalCount:    totalCount,
			NextPageToken: nextPageToken,
		}

		select {
		case stream <- resp:
		case <-ctx.Done():
			return ctx.Err()
		}

		offset = end
	}

	if t.svc.metrics != nil {
		t.svc.metrics.requestsTotal.With("TimeSeriesQuery", "success").Inc()
	}

	if t.svc.logger != nil {
		t.svc.logger.WithFields(map[string]interface{}{
			"entity_type": req.EntityType,
			"start_time":  req.StartTime,
			"end_time":    req.EndTime,
			"count":       len(dataPoints),
		}).Debug("Time-series query completed")
	}

	return nil
}

// maxJSONEntityID is 2^64, the first float64 too large for a uint64 entity ID.
const maxJSONEntityID = 1 << 64

// timeSeriesEntityID returns the entity ID a decoded time-series entity
// carries in its "id" or "entity_id" field. JSON numbers decode as float64;
// an ID that is absent, negative, fractional or beyond uint64 is reported
// as 0, the same as an entity without an ID.
func timeSeriesEntityID(entity map[string]interface{}) uint64 {
	id, ok := entity["id"].(float64)
	if !ok {
		id, ok = entity["entity_id"].(float64)
	}
	if !ok || id < 0 || id >= maxJSONEntityID || id != math.Trunc(id) {
		return 0
	}
	return uint64(id)
}

func (t *timeSeriesService) WritePoint(ctx context.Context, req *WritePointRequest) (*WritePointResponse, error) {
	if t.tsStorage == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "timeSeriesService.WritePoint",
			Err:  &api.ServiceNotConfiguredError{Service: "timeseries storage"},
		}
	}

	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.WritePoint",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Metric == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.WritePoint",
			Err:  &api.RequiredFieldError{Field: "metric"},
		}
	}

	_, scopedMetric, err := tenantScopedMetric(ctx, "timeSeriesService.WritePoint", req.Metric)
	if err != nil {
		return nil, err
	}

	// Build storage data point for routing and local fallback
	point := toTimeSeriesDataPoint(scopedMetric, req.Tags, req.Value, req.Timestamp)

	// Delegate through qrdb SDK if raft is available, otherwise write directly to storage
	if t.svc.qrdbClient != nil {
		sdkPoint := toSDKDataPointFromRequest(scopedMetric, req.Tags, req.Value, point.Timestamp)

		client, err := t.svc.qrdbForShard(t.shardID)
		if err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrRaft,
				Op:   "timeSeriesService.WritePoint",
				Err:  err,
			}
		}
		if err := client.TimeSeries().TSWritePoint(ctx, sdkPoint); err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrRaft,
				Op:   "timeSeriesService.WritePoint",
				Err:  err,
			}
		}
	} else {
		// Fallback to direct storage write when Raft not available
		if err := t.tsStorage.WritePoint(ctx, point); err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "timeSeriesService.WritePoint",
				Err:  err,
			}
		}
	}

	return &WritePointResponse{
		Success:   true,
		Timestamp: time.Now(),
	}, nil
}

func (t *timeSeriesService) WriteBatch(ctx context.Context, req *WriteBatchRequest) (*WriteBatchResponse, error) {
	if t.tsStorage == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "timeSeriesService.WriteBatch",
			Err:  &api.ServiceNotConfiguredError{Service: "timeseries storage"},
		}
	}

	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.WriteBatch",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if len(req.Points) == 0 {
		return &WriteBatchResponse{
			Success:       true,
			PointsWritten: 0,
			Timestamp:     time.Now(),
		}, nil
	}

	// The error already carries its code: a missing tenant is ErrForbidden,
	// a malformed one ErrInvalidInput. Re-wrapping it would hide the first.
	tenantID, err := prepareTimeSeriesTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to storage data points for routing and local fallback
	points := make([]*tsDataPoint, len(req.Points))
	for i, p := range req.Points {
		if p == nil {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "timeSeriesService.WriteBatch",
				Err:  &api.BatchItemError{Index: i, Message: "point is nil"},
			}
		}
		if p.Metric == "" {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "timeSeriesService.WriteBatch",
				Err:  &api.BatchItemError{Index: i, Field: "metric", Message: "metric is required"},
			}
		}
		ts := p.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		points[i] = &tsDataPoint{
			Metric:    tsTenantScopedMetric(tenantID, p.Metric),
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: ts,
		}
	}

	// Delegate through qrdb SDK if raft is available, otherwise write directly to storage
	if t.svc.qrdbClient != nil {
		// Write all points to default shard through qrdb SDK
		sdkPoints := localPointsToSDK(points)
		client, err := t.svc.qrdbForShard(t.shardID)
		if err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrRaft,
				Op:   "timeSeriesService.WriteBatch",
				Err:  err,
			}
		}
		if err := client.TimeSeries().TSWriteBatch(ctx, sdkPoints); err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrRaft,
				Op:   "timeSeriesService.WriteBatch",
				Err:  err,
			}
		}
	} else {
		// Fallback to direct storage write when Raft not available
		valuePoints := localPointsToStorageBatch(points)
		if err := t.tsStorage.WriteBatch(valuePoints); err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "timeSeriesService.WriteBatch",
				Err:  err,
			}
		}
	}

	return &WriteBatchResponse{
		Success:       true,
		PointsWritten: len(points),
		Timestamp:     time.Now(),
	}, nil
}

// toSDKDataPointFromRequest builds a qrdb SDK transport TSDataPoint directly from request fields.
func toSDKDataPointFromRequest(metric string, tags map[string]string, value float64, ts time.Time) *qrdbtransport.TSDataPoint {
	return &qrdbtransport.TSDataPoint{
		Metric:      metric,
		Value:       value,
		TimestampNs: ts.UnixNano(),
		Tags:        mapToSDKTags(tags),
	}
}

// mapToSDKTags converts a map of string tags to a slice of qrdb SDK transport TSTag values.
func mapToSDKTags(m map[string]string) []qrdbtransport.TSTag {
	if len(m) == 0 {
		return nil
	}
	tags := make([]qrdbtransport.TSTag, 0, len(m))
	for k, v := range m {
		tags = append(tags, qrdbtransport.TSTag{Key: k, Value: v})
	}
	return tags
}

func (t *timeSeriesService) TSQuery(ctx context.Context, req *TSQueryRequest) (*TSQueryResponse, error) {
	if t.tsStorage == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "timeSeriesService.TSQuery",
			Err:  &api.ServiceNotConfiguredError{Service: "timeseries storage"},
		}
	}

	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.TSQuery",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Metric == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.TSQuery",
			Err:  &api.RequiredFieldError{Field: "metric"},
		}
	}
	if req.StartTime.IsZero() {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.TSQuery",
			Err:  &api.RequiredFieldError{Field: "start_time"},
		}
	}
	if req.EndTime.IsZero() {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.TSQuery",
			Err:  &api.RequiredFieldError{Field: "end_time"},
		}
	}

	tenantID, scopedMetric, err := tenantScopedMetric(ctx, "timeSeriesService.TSQuery", req.Metric)
	if err != nil {
		return nil, err
	}

	// Convert aggregation string to AggFunc
	agg := parseAggFunc(req.Aggregation)

	// Build storage query request and execute
	storageReq := newTimeSeriesQueryRequest(
		scopedMetric, req.Tags, req.StartTime, req.EndTime,
		agg, time.Duration(req.IntervalSec)*time.Second,
		req.GroupBy, req.Limit, req.Percentile,
	)

	// Execute query
	result, err := t.tsStorage.QueryRich(ctx, storageReq)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "timeSeriesService.TSQuery",
			Err:  err,
		}
	}

	// Convert result to service response. Metric names are returned as the
	// tenant wrote them, without the internal scope.
	resp := &TSQueryResponse{
		Metric:          tsTenantStripMetric(tenantID, result.Metric),
		Tags:            result.Tags,
		AggregatedValue: result.AggregatedValue,
		Timestamp:       time.Now(),
	}

	// Convert data points
	if len(result.Points) > 0 {
		resp.Points = make([]*TSDataPoint, len(result.Points))
		for i, p := range result.Points {
			resp.Points[i] = &TSDataPoint{
				Metric:    tsTenantStripMetric(tenantID, p.Metric),
				Tags:      p.Tags,
				Value:     p.Value,
				Timestamp: p.Timestamp,
			}
		}
	}

	// Convert grouped results
	if len(result.GroupedResults) > 0 {
		resp.GroupedResults = make([]*TSGroupedResult, len(result.GroupedResults))
		for i, g := range result.GroupedResults {
			resp.GroupedResults[i] = &TSGroupedResult{
				Tags:            g.Tags,
				AggregatedValue: g.AggregatedValue,
			}
			if len(g.Points) > 0 {
				resp.GroupedResults[i].Points = make([]*TSDataPoint, len(g.Points))
				for j, p := range g.Points {
					resp.GroupedResults[i].Points[j] = &TSDataPoint{
						Metric:    tsTenantStripMetric(tenantID, p.Metric),
						Tags:      p.Tags,
						Value:     p.Value,
						Timestamp: p.Timestamp,
					}
				}
			}
		}
	}

	return resp, nil
}

func (t *timeSeriesService) DeleteSeries(ctx context.Context, req *TSDeleteRequest) (*TSDeleteResponse, error) {
	if t.tsStorage == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "timeSeriesService.DeleteSeries",
			Err:  &api.ServiceNotConfiguredError{Service: "timeseries storage"},
		}
	}

	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.DeleteSeries",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Metric == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.DeleteSeries",
			Err:  &api.RequiredFieldError{Field: "metric"},
		}
	}

	_, scopedMetric, err := tenantScopedMetric(ctx, "timeSeriesService.DeleteSeries", req.Metric)
	if err != nil {
		return nil, err
	}

	// Build storage delete request
	storageReq := newTimeSeriesDeleteRequest(scopedMetric, req.Tags, req.StartTime, req.EndTime)

	// Execute delete
	if err := t.tsStorage.DeleteSeries(ctx, storageReq); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "timeSeriesService.DeleteSeries",
			Err:  err,
		}
	}

	return &TSDeleteResponse{
		Success:   true,
		Timestamp: time.Now(),
	}, nil
}

func (t *timeSeriesService) GetRetentionPolicy(ctx context.Context, req *TSGetRetentionPolicyRequest) (*TSGetRetentionPolicyResponse, error) {
	if t.tsStorage == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "timeSeriesService.GetRetentionPolicy",
			Err:  &api.ServiceNotConfiguredError{Service: "timeseries storage"},
		}
	}

	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.GetRetentionPolicy",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Metric == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.GetRetentionPolicy",
			Err:  &api.RequiredFieldError{Field: "metric"},
		}
	}

	_, scopedMetric, err := tenantScopedMetric(ctx, "timeSeriesService.GetRetentionPolicy", req.Metric)
	if err != nil {
		return nil, err
	}

	// Get retention policy from storage
	policy, err := t.tsStorage.GetRetentionPolicy(scopedMetric)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "timeSeriesService.GetRetentionPolicy",
			Err:  err,
		}
	}

	// Convert to service response
	var servicePolicy *TSRetentionPolicy
	if policy != nil {
		servicePolicy = &TSRetentionPolicy{
			Duration:    policy.Duration,
			Resolution:  policy.Resolution,
			Aggregation: policy.Aggregation.String(),
		}
	}

	return &TSGetRetentionPolicyResponse{
		Metric:    req.Metric,
		Policy:    servicePolicy,
		Timestamp: time.Now(),
	}, nil
}

func (t *timeSeriesService) SetRetentionPolicy(ctx context.Context, req *TSSetRetentionPolicyRequest) (*TSSetRetentionPolicyResponse, error) {
	if t.tsStorage == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "timeSeriesService.SetRetentionPolicy",
			Err:  &api.ServiceNotConfiguredError{Service: "timeseries storage"},
		}
	}

	// Validate request
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.SetRetentionPolicy",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.Metric == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.SetRetentionPolicy",
			Err:  &api.RequiredFieldError{Field: "metric"},
		}
	}
	if req.Policy == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeSeriesService.SetRetentionPolicy",
			Err:  &api.RequiredFieldError{Field: "policy"},
		}
	}

	_, scopedMetric, err := tenantScopedMetric(ctx, "timeSeriesService.SetRetentionPolicy", req.Metric)
	if err != nil {
		return nil, err
	}

	// Convert to storage policy and set
	storagePolicy := newTimeSeriesRetentionPolicy(req.Policy.Duration, req.Policy.Resolution, parseAggFunc(req.Policy.Aggregation))

	// Set retention policy in storage
	if err := t.tsStorage.SetRetentionPolicy(scopedMetric, storagePolicy); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "timeSeriesService.SetRetentionPolicy",
			Err:  err,
		}
	}

	return &TSSetRetentionPolicyResponse{
		Success:   true,
		Timestamp: time.Now(),
	}, nil
}

func (t *timeSeriesService) ListMetrics(ctx context.Context, req *TSListMetricsRequest) (*TSListMetricsResponse, error) {
	if t.tsStorage == nil {
		return nil, &errors.QBError{
			Code: errors.ErrNotImplemented,
			Op:   "timeSeriesService.ListMetrics",
			Err:  &api.ServiceNotConfiguredError{Service: "timeseries storage"},
		}
	}

	// Prepare tenant context for multi-tenant isolation
	tenantID, err := prepareTimeSeriesTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Validate request
	if req == nil {
		req = &TSListMetricsRequest{}
	}

	// Set default limit if not specified
	limit := req.Limit
	if limit <= 0 {
		limit = 1000 // Default limit
	}

	// Scope the prefix to the tenant namespace. The tenant list prefix is
	// prepended so storage only scans keys belonging to this tenant. The
	// caller-supplied prefix is appended to further narrow the results.
	scopedPrefix := tsTenantListPrefix(tenantID) + req.Prefix

	// Get metrics from storage
	metrics, err := t.tsStorage.ListMetrics(scopedPrefix, limit)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "timeSeriesService.ListMetrics",
			Err:  err,
		}
	}

	// Strip tenant prefix from returned metric names so callers see
	// their own namespace without the internal tenant scoping prefix.
	metrics = tsFilterMetricsByTenant(tenantID, metrics)

	return &TSListMetricsResponse{
		Metrics:   metrics,
		Timestamp: time.Now(),
	}, nil
}

// Validation methods.
func (t *timeSeriesService) validateAggregateRequest(req *AggregateRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateAggregateRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.EntityType == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateAggregateRequest",
			Err:  &api.RequiredFieldError{Field: "entity_type"},
		}
	}
	if req.StartTime <= 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateAggregateRequest",
			Err:  &api.InvalidInputError{Message: "start_time must be greater than 0"},
		}
	}
	if req.EndTime <= 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateAggregateRequest",
			Err:  &api.InvalidInputError{Message: "end_time must be greater than 0"},
		}
	}
	if req.EndTime < req.StartTime {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateAggregateRequest",
			Err:  &api.InvalidInputError{Message: "end_time must be greater than or equal to start_time"},
		}
	}
	return nil
}

func (t *timeSeriesService) validateTimeSeriesQueryRequest(req *TimeSeriesQueryRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateTimeSeriesQueryRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.EntityType == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateTimeSeriesQueryRequest",
			Err:  &api.RequiredFieldError{Field: "entity_type"},
		}
	}
	if req.StartTime <= 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateTimeSeriesQueryRequest",
			Err:  &api.InvalidInputError{Message: "start_time must be greater than 0"},
		}
	}
	if req.EndTime <= 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateTimeSeriesQueryRequest",
			Err:  &api.InvalidInputError{Message: "end_time must be greater than 0"},
		}
	}
	if req.EndTime < req.StartTime {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.validateTimeSeriesQueryRequest",
			Err:  &api.InvalidInputError{Message: "end_time must be greater than or equal to start_time"},
		}
	}
	return nil
}

// operationsService handles operational management tasks like migrations and upgrades.
// It tracks operations in-memory with unique job IDs for async operation monitoring.
type operationsService struct {
	svc *quicBaaS

	// In-memory job tracking
	jobs   map[string]*operationJob
	jobsMu sync.RWMutex
}

// operationJob represents an asynchronous operation (migration, upgrade, node replacement).
type operationJob struct {
	ID        string
	Type      string // "migration", "upgrade", "replacement"
	Status    string // "initiated", "running", "completed", "failed"
	Progress  float64
	StartTime time.Time

	// Migration-specific fields
	SourceShard   uint64
	TargetShard   uint64
	ItemsMigrated uint64
	TotalItems    uint64

	// Upgrade-specific fields
	NodeID         uint64
	CurrentVersion string
	TargetVersion  string
}

func newOperationsService(svc *quicBaaS) *operationsService {
	return &operationsService{
		svc:  svc,
		jobs: make(map[string]*operationJob),
	}
}

func (o *operationsService) MigrateData(ctx context.Context, req *MigrateDataRequest) (*MigrateDataResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.MigrateData",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	// Generate unique migration ID
	migrationID := fmt.Sprintf("migration-%s", uuid.New().String())

	// Create job entry
	job := &operationJob{
		ID:          migrationID,
		Type:        "migration",
		Status:      "initiated",
		Progress:    0.0,
		StartTime:   time.Now(),
		SourceShard: req.SourceShard,
		TargetShard: req.TargetShard,
		TotalItems:  10000, // Mock estimate
	}

	o.jobsMu.Lock()
	o.jobs[migrationID] = job
	o.jobsMu.Unlock()

	// Log the operation
	if o.svc.logger != nil {
		o.svc.logger.WithFields(map[string]interface{}{
			"migration_id": migrationID,
			"source_shard": req.SourceShard,
			"target_shard": req.TargetShard,
			"concurrency":  req.Concurrency,
		}).Info("Migration initiated")
	}

	// In production, this would kick off an async goroutine to perform the migration
	// For now, we just return the initiated status

	return &MigrateDataResponse{
		MigrationID:       migrationID,
		Status:            "initiated",
		EstimatedDuration: 3600, // 1 hour estimate in seconds
	}, nil
}

func (o *operationsService) GetMigrationProgress(ctx context.Context, req *GetMigrationProgressRequest) (*GetMigrationProgressResponse, error) {
	if req == nil || req.MigrationID == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.GetMigrationProgress",
			Err:  &api.RequiredFieldError{Field: "migration ID"},
		}
	}

	o.jobsMu.RLock()
	job, exists := o.jobs[req.MigrationID]
	o.jobsMu.RUnlock()

	if !exists {
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "service.GetMigrationProgress",
			Err:  &api.NotFoundError{Resource: "migration", ID: req.MigrationID},
		}
	}

	if job.Type != "migration" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.GetMigrationProgress",
			Err:  &api.InvalidInputError{Message: "job " + req.MigrationID + " is not a migration"},
		}
	}

	// Calculate ETA based on progress
	var eta int64
	if job.Progress > 0 && job.Progress < 100 {
		elapsed := time.Since(job.StartTime).Seconds()
		totalEstimate := elapsed / (job.Progress / 100.0)
		eta = int64(totalEstimate - elapsed)
	}

	return &GetMigrationProgressResponse{
		MigrationID:   job.ID,
		Progress:      job.Progress,
		Status:        job.Status,
		ItemsMigrated: job.ItemsMigrated,
		TotalItems:    job.TotalItems,
		ETA:           eta,
	}, nil
}

func (o *operationsService) ReplaceNode(ctx context.Context, req *ReplaceNodeRequest) (*ReplaceNodeResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.ReplaceNode",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	// Generate unique job ID
	jobID := fmt.Sprintf("replace-%s", uuid.New().String())

	// Create job entry
	job := &operationJob{
		ID:        jobID,
		Type:      "replacement",
		Status:    "initiated",
		Progress:  0.0,
		StartTime: time.Now(),
	}

	o.jobsMu.Lock()
	o.jobs[jobID] = job
	o.jobsMu.Unlock()

	// Log the operation
	if o.svc.logger != nil {
		o.svc.logger.WithFields(map[string]interface{}{
			"job_id":      jobID,
			"shard_id":    req.ShardID,
			"old_node_id": req.OldNodeID,
			"new_node_id": req.NewNodeID,
			"new_address": req.NewAddress,
		}).Info("Node replacement initiated")
	}

	// In production, this would:
	// 1. Add new node to cluster
	// 2. Initiate data sync from old node
	// 3. Wait for sync completion
	// 4. Update routing tables
	// 5. Remove old node

	return &ReplaceNodeResponse{
		JobID:  jobID,
		Status: "initiated",
	}, nil
}

func (o *operationsService) UpgradeNode(ctx context.Context, req *UpgradeNodeRequest) (*UpgradeNodeResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.UpgradeNode",
			Err:  &api.NilInputError{Name: "request"},
		}
	}

	// Generate unique job ID
	jobID := fmt.Sprintf("upgrade-%s", uuid.New().String())

	// Create job entry
	job := &operationJob{
		ID:             jobID,
		Type:           "upgrade",
		Status:         "initiated",
		Progress:       0.0,
		StartTime:      time.Now(),
		NodeID:         req.NodeID,
		CurrentVersion: "1.0.0", // Mock current version
		TargetVersion:  req.TargetVersion,
	}

	o.jobsMu.Lock()
	o.jobs[jobID] = job
	o.jobsMu.Unlock()

	// Log the operation
	if o.svc.logger != nil {
		o.svc.logger.WithFields(map[string]interface{}{
			"job_id":         jobID,
			"node_id":        req.NodeID,
			"target_version": req.TargetVersion,
		}).Info("Node upgrade initiated")
	}

	// In production, this would perform a rolling upgrade:
	// 1. Drain connections from node
	// 2. Stop node gracefully
	// 3. Upgrade binary
	// 4. Restart node
	// 5. Verify health
	// 6. Resume traffic

	return &UpgradeNodeResponse{
		JobID:  jobID,
		Status: "initiated",
	}, nil
}

func (o *operationsService) GetUpgradeStatus(ctx context.Context, req *GetUpgradeStatusRequest) (*GetUpgradeStatusResponse, error) {
	if req == nil || req.JobID == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.GetUpgradeStatus",
			Err:  &api.RequiredFieldError{Field: "job ID"},
		}
	}

	o.jobsMu.RLock()
	job, exists := o.jobs[req.JobID]
	o.jobsMu.RUnlock()

	if !exists {
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "service.GetUpgradeStatus",
			Err:  &api.NotFoundError{Resource: "job", ID: req.JobID},
		}
	}

	if job.Type != "upgrade" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.GetUpgradeStatus",
			Err:  &api.InvalidInputError{Message: "job " + req.JobID + " is not an upgrade"},
		}
	}

	return &GetUpgradeStatusResponse{
		JobID:          job.ID,
		Progress:       job.Progress,
		Status:         job.Status,
		CurrentVersion: job.CurrentVersion,
		TargetVersion:  job.TargetVersion,
	}, nil
}

// =============================================================================
// Interface Methods (delegate to sub-services)
// =============================================================================

func (s *quicBaaS) Put(ctx context.Context, req *PutRequest) (*PutResponse, error) {
	return s.kvService.Put(ctx, req)
}

func (s *quicBaaS) Get(ctx context.Context, req *GetRequest) (*GetResponse, error) {
	return s.kvService.Get(ctx, req)
}

func (s *quicBaaS) Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error) {
	return s.kvService.Delete(ctx, req)
}

func (s *quicBaaS) List(ctx context.Context, req *ListRequest, stream chan<- *ListResponse) error {
	return s.kvService.List(ctx, req, stream)
}

func (s *quicBaaS) Watch(ctx context.Context, req *WatchRequest, stream chan<- *WatchResponse) (int64, error) {
	return s.kvService.Watch(ctx, req, stream)
}

func (s *quicBaaS) ListEntityTypes(ctx context.Context, req *ListEntityTypesRequest) (*ListEntityTypesResponse, error) {
	return s.kvService.ListEntityTypes(ctx, req)
}

func (s *quicBaaS) BatchPut(ctx context.Context, req *BatchPutRequest) (*BatchPutResponse, error) {
	return s.batchService.BatchPut(ctx, req)
}

func (s *quicBaaS) BatchGet(ctx context.Context, req *BatchGetRequest) (*BatchGetResponse, error) {
	return s.batchService.BatchGet(ctx, req)
}

func (s *quicBaaS) BatchDelete(ctx context.Context, req *BatchDeleteRequest) (*BatchDeleteResponse, error) {
	return s.batchService.BatchDelete(ctx, req)
}

func (s *quicBaaS) Transaction(ctx context.Context, req *TransactionRequest) (*TransactionResponse, error) {
	return s.transactionService.Transaction(ctx, req)
}

func (s *quicBaaS) Query(ctx context.Context, req *QueryRequest, stream chan<- *QueryResponse) error {
	return s.queryService.Query(ctx, req, stream)
}

func (s *quicBaaS) ScanIndex(ctx context.Context, req *ScanRequest, stream chan<- *ScanResponse) error {
	return s.queryService.ScanIndex(ctx, req, stream)
}

func (s *quicBaaS) ClusterStatus(ctx context.Context, req *ClusterStatusRequest) (*ClusterStatusResponse, error) {
	return s.clusterService.ClusterStatus(ctx, req)
}

func (s *quicBaaS) GetLeader(ctx context.Context, req *GetLeaderRequest) (*GetLeaderResponse, error) {
	return s.clusterService.GetLeader(ctx, req)
}

func (s *quicBaaS) AddNode(ctx context.Context, req *AddNodeRequest) (*AddNodeResponse, error) {
	return s.clusterService.AddNode(ctx, req)
}

func (s *quicBaaS) RemoveNode(ctx context.Context, req *RemoveNodeRequest) (*RemoveNodeResponse, error) {
	return s.clusterService.RemoveNode(ctx, req)
}

func (s *quicBaaS) CreateShard(ctx context.Context, req *CreateShardRequest) (*CreateShardResponse, error) {
	return s.clusterService.CreateShard(ctx, req)
}

func (s *quicBaaS) RebalanceCluster(ctx context.Context, req *RebalanceClusterRequest) (*RebalanceClusterResponse, error) {
	return s.clusterService.RebalanceCluster(ctx, req)
}

func (s *quicBaaS) GetRebalanceProgress(ctx context.Context, req *GetRebalanceProgressRequest) (*GetRebalanceProgressResponse, error) {
	return s.clusterService.GetRebalanceProgress(ctx, req)
}

func (s *quicBaaS) CreateBackup(ctx context.Context, req *CreateBackupRequest) (*CreateBackupResponse, error) {
	return s.backupService.CreateBackup(ctx, req)
}

func (s *quicBaaS) ListBackups(ctx context.Context, req *ListBackupsRequest) (*ListBackupsResponse, error) {
	return s.backupService.ListBackups(ctx, req)
}

func (s *quicBaaS) RestoreBackup(ctx context.Context, req *RestoreBackupRequest) (*RestoreBackupResponse, error) {
	return s.backupService.RestoreBackup(ctx, req)
}

func (s *quicBaaS) DeleteBackup(ctx context.Context, req *DeleteBackupRequest) (*DeleteBackupResponse, error) {
	return s.backupService.DeleteBackup(ctx, req)
}

func (s *quicBaaS) VerifyBackup(ctx context.Context, req *VerifyBackupRequest) (*VerifyBackupResponse, error) {
	return s.backupService.VerifyBackup(ctx, req)
}

func (s *quicBaaS) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	return s.authService.Login(ctx, req)
}

func (s *quicBaaS) Logout(ctx context.Context, req *LogoutRequest) (*LogoutResponse, error) {
	return s.authService.Logout(ctx, req)
}

func (s *quicBaaS) RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*RefreshTokenResponse, error) {
	return s.authService.RefreshToken(ctx, req)
}

func (s *quicBaaS) ValidateToken(ctx context.Context, req *ValidateTokenRequest) (*ValidateTokenResponse, error) {
	return s.authService.ValidateToken(ctx, req)
}

func (s *quicBaaS) IntrospectToken(ctx context.Context, req *IntrospectTokenRequest) (*TokenIntrospection, error) {
	return s.authService.IntrospectToken(ctx, req)
}

func (s *quicBaaS) CreateAuthRole(ctx context.Context, req *AuthCreateRoleRequest) (*AuthCreateRoleResponse, error) {
	return s.rbacService.CreateRole(ctx, req)
}

func (s *quicBaaS) DeleteAuthRole(ctx context.Context, req *AuthDeleteRoleRequest) (*AuthDeleteRoleResponse, error) {
	return s.rbacService.DeleteRole(ctx, req)
}

func (s *quicBaaS) ListAuthRoles(ctx context.Context, req *AuthListRolesRequest) (*AuthListRolesResponse, error) {
	return s.rbacService.ListRoles(ctx, req)
}

func (s *quicBaaS) AssignRole(ctx context.Context, req *AuthAssignRoleRequest) (*AuthAssignRoleResponse, error) {
	return s.rbacService.AssignRole(ctx, req)
}

func (s *quicBaaS) RevokeRole(ctx context.Context, req *AuthRevokeRoleRequest) (*AuthRevokeRoleResponse, error) {
	return s.rbacService.RevokeRole(ctx, req)
}

func (s *quicBaaS) GetCacheStats(ctx context.Context, req *GetCacheStatsRequest) (*GetCacheStatsResponse, error) {
	return s.cacheService.GetCacheStats(ctx, req)
}

func (s *quicBaaS) ClearCache(ctx context.Context, req *ClearCacheRequest) (*ClearCacheResponse, error) {
	return s.cacheService.ClearCache(ctx, req)
}

func (s *quicBaaS) GetCacheConfig(ctx context.Context, req *GetCacheConfigRequest) (*GetCacheConfigResponse, error) {
	return s.cacheService.GetCacheConfig(ctx, req)
}

func (s *quicBaaS) UpdateCacheConfig(ctx context.Context, req *UpdateCacheConfigRequest) (*UpdateCacheConfigResponse, error) {
	return s.cacheService.UpdateCacheConfig(ctx, req)
}

func (s *quicBaaS) Health(ctx context.Context, req *HealthRequest) (*HealthResponse, error) {
	return s.healthService.Health(ctx, req)
}

func (s *quicBaaS) Liveness(ctx context.Context, req *LivenessRequest) (*LivenessResponse, error) {
	return s.healthService.Liveness(ctx, req)
}

func (s *quicBaaS) Readiness(ctx context.Context, req *ReadinessRequest) (*ReadinessResponse, error) {
	return s.healthService.Readiness(ctx, req)
}

func (s *quicBaaS) Aggregate(ctx context.Context, req *AggregateRequest) (*AggregateResponse, error) {
	return s.timeSeriesService.Aggregate(ctx, req)
}

func (s *quicBaaS) TimeSeriesQuery(ctx context.Context, req *TimeSeriesQueryRequest, stream chan<- *TimeSeriesQueryResponse) error {
	return s.timeSeriesService.TimeSeriesQuery(ctx, req, stream)
}

func (s *quicBaaS) WritePoint(ctx context.Context, req *WritePointRequest) (*WritePointResponse, error) {
	return s.timeSeriesService.WritePoint(ctx, req)
}

func (s *quicBaaS) WriteBatch(ctx context.Context, req *WriteBatchRequest) (*WriteBatchResponse, error) {
	return s.timeSeriesService.WriteBatch(ctx, req)
}

func (s *quicBaaS) TSQuery(ctx context.Context, req *TSQueryRequest) (*TSQueryResponse, error) {
	return s.timeSeriesService.TSQuery(ctx, req)
}

func (s *quicBaaS) DeleteSeries(ctx context.Context, req *TSDeleteRequest) (*TSDeleteResponse, error) {
	return s.timeSeriesService.DeleteSeries(ctx, req)
}

func (s *quicBaaS) GetRetentionPolicy(ctx context.Context, req *TSGetRetentionPolicyRequest) (*TSGetRetentionPolicyResponse, error) {
	return s.timeSeriesService.GetRetentionPolicy(ctx, req)
}

func (s *quicBaaS) SetRetentionPolicy(ctx context.Context, req *TSSetRetentionPolicyRequest) (*TSSetRetentionPolicyResponse, error) {
	return s.timeSeriesService.SetRetentionPolicy(ctx, req)
}

func (s *quicBaaS) ListMetrics(ctx context.Context, req *TSListMetricsRequest) (*TSListMetricsResponse, error) {
	return s.timeSeriesService.ListMetrics(ctx, req)
}

func (s *quicBaaS) MigrateData(ctx context.Context, req *MigrateDataRequest) (*MigrateDataResponse, error) {
	return s.operationsService.MigrateData(ctx, req)
}

func (s *quicBaaS) GetMigrationProgress(ctx context.Context, req *GetMigrationProgressRequest) (*GetMigrationProgressResponse, error) {
	return s.operationsService.GetMigrationProgress(ctx, req)
}

func (s *quicBaaS) ReplaceNode(ctx context.Context, req *ReplaceNodeRequest) (*ReplaceNodeResponse, error) {
	return s.operationsService.ReplaceNode(ctx, req)
}

func (s *quicBaaS) UpgradeNode(ctx context.Context, req *UpgradeNodeRequest) (*UpgradeNodeResponse, error) {
	return s.operationsService.UpgradeNode(ctx, req)
}

func (s *quicBaaS) GetUpgradeStatus(ctx context.Context, req *GetUpgradeStatusRequest) (*GetUpgradeStatusResponse, error) {
	return s.operationsService.GetUpgradeStatus(ctx, req)
}

// Close gracefully shuts down the service and all backend components.

// =============================================================================
// Accessor Methods - KMS, Secrets, Crypto Services
// =============================================================================

// KMS returns the KMS service (returns nil if not configured).
func (s *quicBaaS) KMS() KMSService {
	return s.kmsService
}

// KMSManager returns the underlying KMS manager for direct access.
// This provides the interfaces.KMSManager implementation used by API layers.
func (s *quicBaaS) KMSManager() interfaces.KMSManager {
	return s.kmsManager
}

// Secrets returns the Secrets service (returns nil if not configured).
func (s *quicBaaS) Secrets() SecretsService {
	return s.secretsService
}

// Crypto returns the Crypto service (returns nil if not configured).
func (s *quicBaaS) Crypto() CryptoService {
	return s.cryptoService
}

// Vector returns the Vector service (returns nil if not configured).
func (s *quicBaaS) Vector() VectorService {
	return s.vectorService
}

// Embedding returns the Embedding service (returns nil if not configured).
func (s *quicBaaS) Embedding() EmbeddingService {
	return s.embeddingService
}

// Ollama returns the Ollama service (returns nil if not configured).
func (s *quicBaaS) Ollama() OllamaService {
	return s.ollamaService
}

// Certificate returns the Certificate service (returns nil if not configured).
func (s *quicBaaS) Certificate() CertificateService {
	return s.certificateService
}

// Trust returns the Trust composition service (returns nil if not
// configured). It exposes the cross-sign / AIA verbs surfaced under
// Security().Trust(), backed by the xkms TrustedCA via the CA adapter.
func (s *quicBaaS) Trust() TrustService {
	return s.trustService
}

// Attestation returns the Attestation service surfaced under
// Security().Attestation(). It is always non-nil: the verifier half composes
// go-xkms pkg/attestation and has no external dependency, and the
// orchestration half reports its own not-configured error.
func (s *quicBaaS) Attestation() AttestationService {
	return s.attestationService
}

// Registry returns the service registry. It is always non-nil.
func (s *quicBaaS) Registry() RegistryService {
	return s.registryService
}

// PIV returns the backend-scoped PIV composition service surfaced under
// Security().Auth().PIV(). It is always non-nil: every verb delegates to
// the composed go-xkms service, whose typed errors report the
// not-initialized case.
func (s *quicBaaS) PIV() PIVService {
	return s.pivService
}

// FIDO2 returns the tenant-scoped WebAuthn ceremony composition service
// surfaced under Security().Auth().FIDO2(). It is always non-nil: it
// resolves the per-tenant xkms relying-party service at call time and
// reports ErrFIDO2NotConfigured until the xkms-backed WebAuthn manager is
// wired.
func (s *quicBaaS) FIDO2() FIDO2Service {
	return s.fido2Service
}

// WebAuthnCredentials returns the user-scoped WebAuthn credential
// administration service — the surface that lists and removes a user's
// registered authenticators. It is always non-nil and reports
// ErrWebAuthnCredentialNotConfigured until the xkms-backed WebAuthn manager
// is wired, so every transport can resolve it the same way.
func (s *quicBaaS) WebAuthnCredentials() WebAuthnCredentialService {
	return s.webauthnCredentialService
}

// InviteActivation returns the invite activation service — the pre-auth
// surface that reads a pending invite and activates its account by passkey
// or PIV certificate. It is always non-nil.
func (s *quicBaaS) InviteActivation() InviteActivationService {
	return s.inviteActivationService
}

// ACME returns the ACME (RFC 8555) composition service (returns nil if not
// configured). It exposes the server-side challenge-registration and
// issuance verbs surfaced under Security().ACME().Server(), backed by the
// composed go-xkms ACME server. ACME is HTTP-native; it is reachable over
// REST and the in-process embedded transport only.
func (s *quicBaaS) ACME() ACMEServicer {
	return s.acmeService
}

// DynamicSecrets returns the Dynamic Secrets service (returns nil until
// SetDynamicSecretsService wires it).
func (s *quicBaaS) DynamicSecrets() DynamicSecretsService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dynamicSecretsService
}

// ObjectStorageService returns the Object Storage service (returns nil if not configured).
func (s *quicBaaS) ObjectStorageService() ObjectStorageService {
	return s.objectStorageService
}

// ObjectStorageAdminService returns the Object Storage Admin service (returns nil if not configured).
func (s *quicBaaS) ObjectStorageAdminService() ObjectStorageAdminService {
	return s.objectStorageAdminService
}

// ParameterStoreService returns the Parameter Store service (returns nil if not configured).
func (s *quicBaaS) ParameterStoreService() ParameterStoreService {
	return s.parameterStoreService
}

// ObjectTransferService returns the Object Transfer service (returns nil if not configured).
// ObjectTransferService provides peer-to-peer object data transfer for lazy snapshot recovery.
func (s *quicBaaS) ObjectTransferService() ObjectTransferService {
	return s.objectTransferService
}

// Scheduler returns the Scheduler service (returns nil if not configured).
// Scheduler provides job scheduling and execution with multi-tenant support.
func (s *quicBaaS) Scheduler() SchedulerService {
	return s.schedulerService
}

// Placement returns the scheduler's placement service (nil when no qrdb
// client is configured).
func (s *quicBaaS) Placement() PlacementService {
	return s.placementService
}

// SchedulerEngine returns the raw *scheduler.SchedulerEngine for
// callers that need handler registration (RegisterHandler) or
// direct job-loop control — surface area the service-level
// SchedulerService deliberately omits.
//
// Returns nil when the scheduler is not enabled in config.
//
// Typical use: platformd registers its cert-renewal handler and
// other in-process handlers via this accessor after Build()
// completes. External (over-the-wire) callers should use the
// service-level Scheduler() surface instead.
func (s *quicBaaS) SchedulerEngine() *scheduler.SchedulerEngine {
	if s.schedulerEngine == nil {
		return nil
	}
	// s.schedulerEngine is the concrete type stored via
	// interfaces.SchedulerEngine — type-assert back for callers
	// that need the engine's full surface.
	if eng, ok := s.schedulerEngine.(*scheduler.SchedulerEngine); ok {
		return eng
	}
	return nil
}

// SSHClient returns the SSH key, known-hosts, and authorized-keys
// client supplied through WithSSHClient, or nil when the node has no
// SSH key store. The returned Client is owned by the service and
// closed during Close.
func (s *quicBaaS) SSHClient() xkmsssh.Client {
	return s.sshClient
}

// Project returns the Project service for managing agentic project entities.
func (s *quicBaaS) Project() ProjectService {
	return s.projectService
}

// Account returns the Account service for managing pooled provider accounts.
func (s *quicBaaS) Account() AccountService {
	return s.accountService
}

// FleetAdmin returns the Agent Admin service for global agent lifecycle management.
func (s *quicBaaS) FleetAdmin() FleetAdminService {
	return s.fleetAdminService
}

// Agent returns the Agent service (returns nil if not configured).
// Agent provides AI agent orchestration across multiple backends.
func (s *quicBaaS) Agent() orchestration.AgentService {
	return s.agentService
}

// SetAgentService sets the Agent orchestration service.
// This is called during initialization when orchestration is configured.
func (s *quicBaaS) SetAgentService(agent orchestration.AgentService) {
	s.agentService = agent
}

// SDLC returns the SDLC module's project service (nil when the module is not
// running on this node).
func (s *quicBaaS) SDLC() *SDLCService {
	return s.sdlcService
}

// SetSDLCService attaches the SDLC module's service.
func (s *quicBaaS) SetSDLCService(svc *SDLCService) {
	s.sdlcService = svc
}

// Prompts returns the prompt service (nil when the prompt store is not
// mounted on this node).
func (s *quicBaaS) Prompts() *PromptService {
	return s.promptService.Load()
}

// SetPromptService attaches the prompt service.
func (s *quicBaaS) SetPromptService(svc *PromptService) {
	s.promptService.Store(svc)
}

// SetProjectBindingReader wires the SDLC binding store project deletes are
// checked against, so a bound project is refused rather than deleted out
// from under its binding.
func (s *quicBaaS) SetProjectBindingReader(bindings sdlc.BindingReader) {
	projects, ok := s.projectService.(*projectServiceImpl)
	if !ok {
		return
	}
	projects.SetBindingReader(bindings)
}

// Messaging returns the Messaging notification service (returns nil if not configured).
// Messaging provides real-time push notifications and event streaming.
func (s *quicBaaS) Messaging() messaging.Service {
	return s.messagingService
}

// SetMessagingService sets the Messaging notification service.
// This is called after the messaging service is initialized externally.
func (s *quicBaaS) SetMessagingService(ms messaging.Service) {
	s.messagingService = ms
}

// SetDynamicSecretsService wires the Dynamic Secrets service into the facade
// at runtime. Called by the server once the lease manager, the credential
// generator registry and the durable store behind them exist.
//
// A nil service is ignored: a deployment that could not compose the capability
// keeps the unconfigured accessor rather than having a working one replaced by
// a nil interface that every transport would then dereference.
func (s *quicBaaS) SetDynamicSecretsService(svc DynamicSecretsService) {
	if svc == nil {
		return
	}
	s.mu.Lock()
	s.dynamicSecretsService = svc
	s.mu.Unlock()
}

// Database returns the Database service (returns nil if not configured).
// Database provides tenant-scoped database lifecycle management.
func (s *quicBaaS) Database() DatabaseService {
	return s.databaseService
}

// SetDatabaseService sets the Database lifecycle management service.
// This is called after the database manager is initialized.
func (s *quicBaaS) SetDatabaseService(ds DatabaseService) {
	s.databaseService = ds
}

// SetQRDBClient sets the qrdb SDK client provider for core KV operations.
// This is called after the ShardedClient is initialized.
func (s *quicBaaS) SetQRDBClient(provider QRDBClientProvider) {
	s.qrdbClient = provider
}

// SetSecretRegistry wires the unified secret resolver registry into the
// service. Called by the server after buildSecretRegistry, so embedded SDK
// clients can adopt it (Security().SecretResolver()) without WithSecretRegistry.
func (s *quicBaaS) SetSecretRegistry(reg *qrdbsecret.ResolverRegistry) {
	s.secretRegistry = reg
}

// SecretRegistry returns the unified secret resolver registry, or nil before
// SetSecretRegistry has wired it.
func (s *quicBaaS) SecretRegistry() *qrdbsecret.ResolverRegistry {
	return s.secretRegistry
}

// LoginCeremony returns the unified login-ceremony facade. Returns nil before
// SetLoginService has wired it (e.g. login disabled / IAM absent).
func (s *quicBaaS) LoginCeremony() LoginServicer {
	return s.loginService
}

// SetLoginService wires the unified login-ceremony facade into the service.
// Called by the server once the qrdb node hosting the in-process LoginClient
// is constructed.
func (s *quicBaaS) SetLoginService(svc LoginServicer) {
	s.loginService = svc
}

// WatchManager returns the internal watch.Manager for bridge integration.
func (s *quicBaaS) WatchManager() *watch.Manager {
	return s.watchManager
}

// DAO returns the DAO factory for creating type-safe Generic DAOs.
// Returns nil if no KVStore provider was configured.
func (s *quicBaaS) DAO() DAOFactory {
	if s.daoFactory == nil {
		return nil
	}
	return s.daoFactory
}

// =============================================================================
// KMS Service Delegation Methods
// =============================================================================

// GenerateKey creates a new cryptographic key with specified parameters.
// Delegates to the underlying KMS service.
func (s *quicBaaS) GenerateKey(ctx context.Context, req *GenerateKeyRequest) (*GenerateKeyResponse, error) {
	if s.kmsService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.GenerateKey", "KMS service not initialized")
	}
	return s.kmsService.GenerateKey(ctx, req)
}

// ImportKey imports an externally generated key into the KMS.
// Delegates to the underlying KMS service.
func (s *quicBaaS) ImportKey(ctx context.Context, req *ImportKeyRequest) (*ImportKeyResponse, error) {
	if s.kmsService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.ImportKey", "KMS service not initialized")
	}
	return s.kmsService.ImportKey(ctx, req)
}

// GetKey retrieves key material or public key by key ID.
// Delegates to the underlying KMS service.
func (s *quicBaaS) GetKey(ctx context.Context, req *GetKeyRequest) (*GetKeyResponse, error) {
	if s.kmsService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.GetKey", "KMS service not initialized")
	}
	return s.kmsService.GetKey(ctx, req)
}

// ListKeys returns all keys accessible to the requester with pagination.
// Delegates to the underlying KMS service.
func (s *quicBaaS) ListKeys(ctx context.Context, req *ListKeysRequest) (*ListKeysResponse, error) {
	if s.kmsService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.ListKeys", "KMS service not initialized")
	}
	return s.kmsService.ListKeys(ctx, req)
}

// RotateKey creates a new version of an existing key.
// Delegates to the underlying KMS service.
func (s *quicBaaS) RotateKey(ctx context.Context, req *RotateKeyRequest) (*RotateKeyResponse, error) {
	if s.kmsService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.RotateKey", "KMS service not initialized")
	}
	return s.kmsService.RotateKey(ctx, req)
}

// RevokeKey marks a key version as revoked and unusable.
// Delegates to the underlying KMS service.
func (s *quicBaaS) RevokeKey(ctx context.Context, req *RevokeKeyRequest) (*RevokeKeyResponse, error) {
	if s.kmsService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.RevokeKey", "KMS service not initialized")
	}
	return s.kmsService.RevokeKey(ctx, req)
}

// DeleteKey permanently removes a key and all its versions.
// Delegates to the underlying KMS service.
func (s *quicBaaS) DeleteKey(ctx context.Context, req *DeleteKeyRequest) (*DeleteKeyResponse, error) {
	if s.kmsService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.DeleteKey", "KMS service not initialized")
	}
	return s.kmsService.DeleteKey(ctx, req)
}

// GetKeyMetadata retrieves metadata for a key without exposing key material.
// Delegates to the underlying KMS service.
func (s *quicBaaS) GetKeyMetadata(ctx context.Context, req *GetKeyMetadataRequest) (*GetKeyMetadataResponse, error) {
	if s.kmsService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.GetKeyMetadata", "KMS service not initialized")
	}
	return s.kmsService.GetKeyMetadata(ctx, req)
}

// =============================================================================
// Crypto Service Delegation Methods
// =============================================================================

// Encrypt encrypts data using the specified key.
// Delegates to the underlying Crypto service.
func (s *quicBaaS) Encrypt(ctx context.Context, req *EncryptRequest) (*EncryptResponse, error) {
	if s.cryptoService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.Encrypt", "Crypto service not initialized")
	}
	return s.cryptoService.Encrypt(ctx, req)
}

// Decrypt decrypts data using the specified key.
// Delegates to the underlying Crypto service.
func (s *quicBaaS) Decrypt(ctx context.Context, req *DecryptRequest) (*DecryptResponse, error) {
	if s.cryptoService == nil {
		return nil, errors.New(errors.ErrUnavailable, "quicBaaS.Decrypt", "Crypto service not initialized")
	}
	return s.cryptoService.Decrypt(ctx, req)
}

func (s *quicBaaS) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	if s.logger != nil {
		s.logger.Info("Shutting down QuicBaaS service")
	}

	// Close components in reverse dependency order

	// Stop scheduler first to prevent new job executions
	if s.schedulerEngine != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.schedulerEngine.Stop(ctx); err != nil {
			if s.logger != nil {
				s.logger.Warnf("Failed to stop scheduler gracefully: %v", err)
			}
		}
	}

	var errs []error

	if s.cacheManager != nil {
		if err := s.cacheManager.Close(); err != nil {
			errs = append(errs, &api.OperationFailedError{Operation: "close cache manager", Cause: err})
		}
	}

	if s.backupMgr != nil {
		if err := s.backupMgr.Close(); err != nil {
			errs = append(errs, &api.OperationFailedError{Operation: "close backup manager", Cause: err})
		}
	}

	if s.storage != nil {
		if err := s.storage.Close(); err != nil {
			errs = append(errs, &api.OperationFailedError{Operation: "close storage", Cause: err})
		}
	}

	if s.qrdbClient != nil {
		if err := s.qrdbClient.Close(); err != nil {
			errs = append(errs, &api.OperationFailedError{Operation: "close qrdb client", Cause: err})
		}
	}

	if s.sshClient != nil {
		if err := s.sshClient.Close(); err != nil {
			errs = append(errs, &api.OperationFailedError{Operation: "close ssh client", Cause: err})
		}
	}

	s.closed = true

	if len(errs) > 0 {
		return api.NewShutdownError("service.QuicBaaS.Close", errs)
	}

	if s.logger != nil {
		s.logger.Info("QuicBaaS service shutdown complete")
	}

	return nil
}

// =============================================================================
// Helper Methods
// =============================================================================

// getLeaderIDFromSDK retrieves the leader replica ID for a shard via the
// qrdb SDK's GetShardState endpoint. Returns 0 when the leader cannot be
// determined (e.g., no SDK client, no leader elected, or transient error).
func (s *quicBaaS) getLeaderIDFromSDK(ctx context.Context, shardID uint64) uint64 {
	client, err := s.qrdbForShard(shardID)
	if err != nil {
		return 0
	}
	state, err := client.Admin().GetShardState(ctx, shardID)
	if err != nil || !state.HasLeader {
		return 0
	}
	return state.LeaderID
}

func (k *kvService) convertConsistency(level ConsistencyLevel) qrdbtransport.ConsistencyLevel {
	switch level {
	case ConsistencyLevelEventual:
		return qrdbtransport.ConsistencyEventual
	case ConsistencyLevelLinearizable:
		return qrdbtransport.ConsistencyLinearizable
	case ConsistencyLevelStrong:
		return qrdbtransport.ConsistencyStrong
	default:
		return qrdbtransport.ConsistencyLinearizable
	}
}

func (k *kvService) convertWriteMode(mode WriteMode) qrdbtransport.WriteMode {
	switch mode {
	case WriteModeSync:
		return qrdbtransport.WriteModeSync
	case WriteModeWALDurable:
		return qrdbtransport.WriteModeWALDurable
	case WriteModeBatched:
		return qrdbtransport.WriteModeBatched
	default:
		return qrdbtransport.WriteModeWALDurable
	}
}

// =============================================================================
// Validation Methods
// =============================================================================

func (k *kvService) validatePutRequest(req *PutRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Put",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.EntityType == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Put",
			Err:  &api.RequiredFieldError{Field: "entity type"},
		}
	}
	// Note: Empty data (len == 0) is allowed - this is valid in key-value stores
	// The existence of a key with empty value is semantically different from key not existing
	return nil
}

func (k *kvService) validateGetRequest(req *GetRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Get",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.EntityType == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Get",
			Err:  &api.RequiredFieldError{Field: "entity type"},
		}
	}
	return nil
}

// defaultReadTimeout bounds a read whose request leaves TimeoutMs unset.
const defaultReadTimeout = 5 * time.Second

// readTimeoutDuration converts a read timeout in milliseconds to a duration,
// defaulting to defaultReadTimeout when it is zero. A timeout too large to
// represent as a time.Duration is an error.
func readTimeoutDuration(timeoutMs uint64) (time.Duration, error) {
	if timeoutMs == 0 {
		return defaultReadTimeout, nil
	}
	ms, err := conv.SafeUint64ToInt64(timeoutMs)
	if err != nil {
		return 0, err
	}
	if ms > int64(math.MaxInt64/time.Millisecond) {
		return 0, &conv.ConversionOverflowError{
			FromType: "uint64",
			ToType:   "time.Duration",
			Value:    strconv.FormatUint(timeoutMs, 10),
		}
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func (k *kvService) validateDeleteRequest(req *DeleteRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Delete",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.EntityType == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.Delete",
			Err:  &api.RequiredFieldError{Field: "entity type"},
		}
	}
	return nil
}

func (k *kvService) validateListRequest(req *ListRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.List",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.EntityType == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "service.List",
			Err:  &api.RequiredFieldError{Field: "entity type"},
		}
	}
	return nil
}

// InvalidPageTokenError reports a List page token that does not decode, or
// that points past the last entity of the listing.
type InvalidPageTokenError struct {
	Token string
}

func (e *InvalidPageTokenError) Error() string {
	return fmt.Sprintf("invalid page token %q", e.Token)
}

// encodeListPageToken returns the opaque cursor that resumes a listing after
// the entity with the given ID.
func encodeListPageToken(lastID uint64) string {
	return strconv.FormatUint(lastID, 10)
}

// listPageOffset returns the index of the first entity after the cursor in
// token. entities must be sorted by ID. An empty token starts at index 0.
func listPageOffset(entities []*Entity, token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	invalid := &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   "service.List",
		Err:  &InvalidPageTokenError{Token: token},
	}
	lastID, err := strconv.ParseUint(token, 10, 64)
	if err != nil {
		return 0, invalid
	}
	offset := sort.Search(len(entities), func(i int) bool {
		return entities[i].ID > lastID
	})
	if offset == len(entities) {
		return 0, invalid
	}
	return offset, nil
}

// ConsistencyLevel.String() implementation.
func (c ConsistencyLevel) String() string {
	switch c {
	case ConsistencyLevelUnspecified:
		return "unspecified"
	case ConsistencyLevelEventual:
		return "eventual"
	case ConsistencyLevelLinearizable:
		return "linearizable"
	case ConsistencyLevelStrong:
		return "strong"
	default:
		return "unknown"
	}
}

// aggFuncMap provides O(1) lookup from aggregation name strings to AggFunc values.
var aggFuncMap = map[string]qrdbtransport.AggFunc{
	"avg":        qrdbtransport.AggAvg,
	"sum":        qrdbtransport.AggSum,
	"min":        qrdbtransport.AggMin,
	"max":        qrdbtransport.AggMax,
	"count":      qrdbtransport.AggCount,
	"percentile": qrdbtransport.AggPercentile,
	"first":      qrdbtransport.AggFirst,
	"last":       qrdbtransport.AggLast,
}

// parseAggFunc converts a string aggregation name to a qrdbtransport.AggFunc.
// Returns AggNone for empty or unrecognized strings.
func parseAggFunc(name string) qrdbtransport.AggFunc {
	if name == "" {
		return qrdbtransport.AggNone
	}
	if agg, ok := aggFuncMap[strings.ToLower(name)]; ok {
		return agg
	}
	return qrdbtransport.AggNone
}

// wireProjectReferences binds the stores a project's saved references are
// verified against — the git credential secret and the git signing and access
// keys — onto the project service. A store this facade was built without is
// left unbound, and a save naming a reference into it is refused as
// unavailable rather than stored unverified.
func (s *quicBaaS) wireProjectReferences() {
	projects, ok := s.projectService.(*projectServiceImpl)
	if !ok {
		return
	}
	if s.secretsService != nil {
		projects.SetSecrets(s.secretsService)
	}
	if s.sshClient != nil {
		projects.SetSSHKeys(NewSSHService(s.sshClient, WithSSHAudit(s)))
	}
}
