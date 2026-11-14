// Package service implements the KMS service facade for QuicBaaS.
// This file contains the concrete implementation of the KMSService interface.
package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// KMSScheduler defines the minimal interface needed by KMS for grace period job scheduling.
// This interface is satisfied by SchedulerAdapter to bridge between service and scheduler packages.
type KMSScheduler interface {
	// ScheduleJob schedules a new job for execution.
	ScheduleJob(ctx context.Context, job *SchedulerJob) error
}

// =============================================================================
// KMS Dispatch Maps
// =============================================================================

// validUpdateStatuses defines the set of key statuses that are valid targets for update operations.
var validUpdateStatuses = map[KeyStatus]struct{}{
	KeyStatusEnabled:  {},
	KeyStatusDisabled: {},
}

// dedicatedOpStatuses defines key statuses that require dedicated operations (not direct update).
var dedicatedOpStatuses = map[KeyStatus]struct{}{
	KeyStatusPendingDeletion: {},
	KeyStatusDeleted:         {},
	KeyStatusRotating:        {},
	KeyStatusCompromised:     {},
}

// symmetricAlgorithmMap provides O(1) lookup from symmetric algorithm type to KeyAlgorithm.
var symmetricAlgorithmMap = map[kctypes.SymmetricAlgorithm]KeyAlgorithm{
	kctypes.SymmetricAES128GCM:         KeyAlgAES128GCM,
	kctypes.SymmetricAES192GCM:         KeyAlgAES192GCM,
	kctypes.SymmetricAES256GCM:         KeyAlgAES256GCM,
	kctypes.SymmetricChaCha20Poly1305:  KeyAlgChaCha20Poly1305,
	kctypes.SymmetricXChaCha20Poly1305: KeyAlgXChaCha20Poly1305,
}

// rsaKeySizeMap provides O(1) lookup from RSA key size to KeyAlgorithm.
var rsaKeySizeMap = map[int]KeyAlgorithm{
	2048: KeyAlgRSA2048,
	3072: KeyAlgRSA3072,
	4096: KeyAlgRSA4096,
}

// ecdsaCurveMap provides O(1) lookup from ECDSA curve name to KeyAlgorithm.
var ecdsaCurveMap = map[string]KeyAlgorithm{
	"P-256": KeyAlgECDSAP256,
	"P-384": KeyAlgECDSAP384,
	"P-521": KeyAlgECDSAP521,
}

// KMSSchedulerQuerier extends KMSScheduler with query capabilities.
// This optional interface is used to derive version status from completed
// grace period jobs stored in Raft-replicated scheduler storage, ensuring
// cross-node visibility in multi-node clusters.
type KMSSchedulerQuerier interface {
	// GetCompletedGracePeriodVersions returns a map of version -> completion time
	// for all completed grace period jobs matching the given key ID.
	GetCompletedGracePeriodVersions(ctx context.Context, keyID string) (map[uint64]time.Time, error)
}

// reencryptionState tracks the state of an ongoing re-encryption operation.
type reencryptionState struct {
	mu                  sync.RWMutex
	tenantID            string // tenant that started the operation; "" is the system scope
	operationID         string
	inProgress          bool
	status              string
	startedAt           time.Time
	estimatedCompletion time.Time
	totalKeys           uint64
	processedKeys       atomic.Uint64
	failedKeys          atomic.Uint64
	errorMessage        string
	cancelFunc          context.CancelFunc
}

// hiddenFrom reports whether the recorded operation belongs to a tenant other
// than tenantID. The system scope ("") sees every operation. The caller holds mu.
func (s *reencryptionState) hiddenFrom(tenantID string) bool {
	return s.operationID != "" && tenantID != "" && s.tenantID != tenantID
}

// dekEntry holds DEK metadata and the wrapped key material.
type dekEntry struct {
	metadata   *DEKMetadata
	wrappedKey []byte
}

// dekStore provides thread-safe storage for DEK entries.
type dekStore struct {
	mu         sync.RWMutex
	entries    map[string]*dekEntry
	defaultDEK map[string]string // tenant scope prefix -> scoped path of that tenant's default DEK
}

// newDEKStore creates a new DEK store.
func newDEKStore() *dekStore {
	return &dekStore{
		entries: make(map[string]*dekEntry),
	}
}

// get retrieves a DEK entry by path.
func (s *dekStore) get(path string) (*dekEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[path]
	return entry, ok
}

// set stores a DEK entry.
func (s *dekStore) set(path string, entry *dekEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[path] = entry
}

// delete removes a DEK entry.
func (s *dekStore) delete(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[path]; ok {
		delete(s.entries, path)
		return true
	}
	return false
}

// list returns all DEK entries matching the optional prefix.
func (s *dekStore) list(prefix string) []*DEKMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*DEKMetadata, 0, len(s.entries))
	for path, entry := range s.entries {
		if prefix == "" || strings.HasPrefix(path, prefix) {
			result = append(result, entry.metadata)
		}
	}
	return result
}

// setDefault sets the default DEK path.
func (s *dekStore) setDefault(scope, path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.defaultDEK == nil {
		s.defaultDEK = make(map[string]string)
	}
	prev := s.defaultDEK[scope]
	s.defaultDEK[scope] = path
	// Update the isDefault flag on the entries of this scope only, so one
	// tenant's choice never clears another tenant's default.
	for p, entry := range s.entries {
		if dekPathInScope(scope, p) {
			entry.metadata.IsDefault = (p == path)
		}
	}
	return prev
}

// dekPathInScope reports whether a scoped DEK path belongs to the tenant
// scope prefix (tenantListPrefix). The platform scope ("") owns every path
// outside the tenant namespace.
func dekPathInScope(scope, path string) bool {
	if scope == "" {
		return !strings.HasPrefix(path, kmsTenantKeyPrefix)
	}
	return strings.HasPrefix(path, scope)
}

// count returns the number of DEK entries.
// kmsServiceImpl implements the KMSService interface.
// It provides cryptographic key lifecycle management operations.
type kmsServiceImpl struct {
	kmsManager        interfaces.KMSManager
	logger            interfaces.Logger
	metrics           *kmsMetrics
	safetyTracker     *AEADSafetyTracker
	auditLogger       KMSAuditLogger
	backendRegistry   *kms.BackendRegistry
	reencryptionState *reencryptionState
	dekStore          *dekStore
	keyNames          sync.Map           // tracks key names to prevent duplicates
	defaultKeyNames   sync.Map           // tenant scope prefix -> that tenant's default key name
	scheduler         KMSScheduler       // scheduler for grace period jobs
	versionStatus     sync.Map           // local cache: "keyID:version" -> *versionStatusInfo
	kvStore           interfaces.KVStore // persistent store for version status and key policies (Raft-replicated)
	authzEnabled      bool               // when true, per-key authorization is enforced on crypto ops
}

// versionStatusInfo stores status information for a specific key version.
type versionStatusInfo struct {
	Status     KeyStatus
	DisabledAt *time.Time
}

// kmsMetrics holds Prometheus metrics for KMS operations.
type kmsMetrics struct {
	keysGeneratedTotal        *metrics.Counter
	keysRotatedTotal          *metrics.Counter
	encryptionOperationsTotal *metrics.CounterVec
	decryptionOperationsTotal *metrics.CounterVec
	keyOperationDuration      *metrics.HistogramVec
	activeKeys                *metrics.Gauge
	errorsByType              *metrics.CounterVec
	statusUpdatesTotal        *metrics.CounterVec
	deksCreatedTotal          *metrics.Counter
	deksRotatedTotal          *metrics.Counter
	activeDEKs                *metrics.Gauge
}

// newKMSServiceImpl creates a new KMS service implementation.
// This is the recommended way to create a KMS service instance.
//
// Parameters:
//   - kmsManager: The KMS manager backend implementation
//   - logger: Logger for service operations
//
// Returns:
//   - *kmsServiceImpl: The initialized KMS service
func newKMSService(kmsManager interfaces.KMSManager, logger interfaces.Logger) *kmsServiceImpl {
	svc := &kmsServiceImpl{
		kmsManager:        kmsManager,
		logger:            logger,
		safetyTracker:     NewAEADSafetyTracker(),
		reencryptionState: &reencryptionState{status: "idle"},
		dekStore:          newDEKStore(),
	}

	// Initialize metrics
	svc.initMetrics()

	return svc
}

// SetScheduler sets the scheduler for scheduling grace period jobs.
// This must be called after both KMS and Scheduler services are initialized
// to avoid circular dependencies.
func (k *kmsServiceImpl) SetScheduler(scheduler KMSScheduler) {
	k.scheduler = scheduler
}

// SetKVStore sets the persistent KVStore for version status replication.
// When set, version status is persisted to the Raft-replicated KVStore,
// making it visible across all nodes in the cluster.
func (k *kmsServiceImpl) SetKVStore(kv interfaces.KVStore) {
	k.kvStore = kv
}

// initMetrics initializes Prometheus metrics for KMS operations.
func (k *kmsServiceImpl) initMetrics() {
	k.metrics = &kmsMetrics{
		keysGeneratedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "keys_generated_total",
			Help:      "Total number of keys generated",
		}),

		keysRotatedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "keys_rotated_total",
			Help:      "Total number of keys rotated",
		}),

		encryptionOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "encryption_operations_total",
			Help:      "Total number of encryption operations",
		}, []string{"algorithm", "status"}),

		decryptionOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "decryption_operations_total",
			Help:      "Total number of decryption operations",
		}, []string{"algorithm", "status"}),

		keyOperationDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "key_operation_duration_seconds",
			Help:      "Duration of key operations in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"operation"}),

		activeKeys: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "active_keys",
			Help:      "Number of active cryptographic keys",
		}),

		errorsByType: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "errors_total",
			Help:      "Total errors by type",
		}, []string{"error_type", "operation"}),

		statusUpdatesTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "status_updates_total",
			Help:      "Total number of key status updates",
		}, []string{"old_status", "new_status"}),

		deksCreatedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "deks_created_total",
			Help:      "Total number of DEKs created",
		}),

		deksRotatedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "deks_rotated_total",
			Help:      "Total number of DEKs rotated",
		}),

		activeDEKs: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "kms",
			Name:      "active_deks",
			Help:      "Number of active data encryption keys",
		}),
	}
}

// GetSafetyTracker returns the AEAD safety tracker for this service.
func (k *kmsServiceImpl) GetSafetyTracker() *AEADSafetyTracker {
	return k.safetyTracker
}

// SafetyCheck performs a safety check and returns keys/DEKs above the threshold.
func (k *kmsServiceImpl) SafetyCheck(ctx context.Context, req *SafetyCheckRequest) (*SafetyCheckResponse, error) {
	threshold := 80 // Default to 80%
	if req != nil && req.ThresholdPercent > 0 {
		threshold = int(req.ThresholdPercent)
	}

	// The tracker is shared by every tenant: report only the caller's own.
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	results := safetyResultsForTenant(tenantID, k.safetyTracker.SafetyCheck(threshold))

	items := make([]SafetyCheckItem, 0, len(results))
	keyCount := 0
	dekCount := 0
	for _, r := range results {
		items = append(items, SafetyCheckItem{
			ID:             r.ID,
			Type:           r.Type,
			OperationCount: r.OperationCount,
			BytesProcessed: r.BytesProcessed,
			UsagePercent:   float64(r.UsagePercent),
			MaxOperations:  r.MaxOperations,
			MaxBytes:       r.MaxBytes,
		})
		if r.Type == "key" {
			keyCount++
		} else {
			dekCount++
		}
	}

	return &SafetyCheckResponse{
		Items:            items,
		ThresholdPercent: float64(threshold),
		TotalKeysChecked: keyCount,
		TotalDEKsChecked: dekCount,
		Timestamp:        time.Now(),
	}, nil
}

// SafetyReport generates a comprehensive safety report for all keys/DEKs.
func (k *kmsServiceImpl) SafetyReport(ctx context.Context, req *SafetyReportRequest) (*SafetyReportResponse, error) {
	// The tracker is shared by every tenant: report only the caller's own.
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	results := safetyResultsForTenant(tenantID, k.safetyTracker.SafetyReport())

	items := make([]SafetyCheckItem, 0, len(results))
	var totalOps uint64
	var totalBytes uint64

	for _, r := range results {
		items = append(items, SafetyCheckItem{
			ID:             r.ID,
			Type:           r.Type,
			OperationCount: r.OperationCount,
			BytesProcessed: r.BytesProcessed,
			UsagePercent:   float64(r.UsagePercent),
			MaxOperations:  r.MaxOperations,
			MaxBytes:       r.MaxBytes,
		})
		totalOps += r.OperationCount
		totalBytes += r.BytesProcessed
	}

	defaultConfig := DefaultAEADSafetyConfig()

	return &SafetyReportResponse{
		Items:               items,
		TotalOperations:     totalOps,
		TotalBytesProcessed: totalBytes,
		ServerConfig: AEADServerConfig{
			Enforce:           k.safetyTracker.enforceDefaults,
			MaxOperations:     defaultConfig.MaxOperations,
			MaxBytes:          defaultConfig.MaxBytes,
			WarningPercent:    defaultConfig.WarningPercent,
			AutoRotatePercent: defaultConfig.AutoRotatePercent,
		},
		Timestamp: time.Now(),
	}, nil
}

// GenerateKey creates a new cryptographic key with specified parameters.
func (k *kmsServiceImpl) GenerateKey(ctx context.Context, req *GenerateKeyRequest) (*GenerateKeyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("generate_key").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateGenerateKeyRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "generate_key").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Use the provided name as the key ID, scoped by tenant if applicable
	// The name must be unique within the KMS/tenant namespace
	keyID := tenantScopedKeyID(tenantID, req.Name)
	if _, exists := k.keyNames.Load(keyID); exists {
		return nil, NewError(errors.ErrAlreadyExists, "kms.GenerateKey", "key with name '"+req.Name+"' already exists")
	}

	// Prepare attributes with key ID
	attrs := req.KeyAttributes
	if attrs == nil {
		attrs = &kctypes.KeyAttributes{}
	}
	attrs.CN = keyID

	// Set default KeyType if not specified.
	// This is required by go-xkms for key generation.
	// When requests come via JSON (e.g., QUIC protocol), the KeyType may be
	// the zero value if not explicitly set in the request.
	var zeroKeyType kctypes.KeyType
	if attrs.KeyType == zeroKeyType {
		// Determine KeyType based on algorithm: symmetric = encryption, asymmetric = signing
		if attrs.SymmetricAlgorithm != "" {
			attrs.KeyType = kctypes.KeyTypeEncryption
		} else {
			attrs.KeyType = kctypes.KeyTypeSigning
		}
	}

	// Call KMS manager to generate key
	keyMetadata, err := k.kmsManager.GenerateKey(ctx, attrs)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("kms_error", "generate_key").Inc()
		}
		return nil, WrapError(err, errors.ErrInternal, "kms.GenerateKey", "failed to generate key")
	}

	// Convert manager response to service response
	// Return the original (unscoped) key name to callers
	// Pre-compute the rotation policy for the response so it is always
	// present in resp.Metadata regardless of whether the backend returns a
	// typed *kms.KeyMetadata (the kmsMeta block below also persists it).
	var resolvedRotationPolicy *RotationPolicy
	if req.RotationPolicy != nil {
		p := *req.RotationPolicy
		if p.AutoRotate && p.Interval > 0 && p.NextRotation.IsZero() {
			p.NextRotation = time.Now().Add(p.Interval)
		}
		resolvedRotationPolicy = &p
	}

	resp := &GenerateKeyResponse{
		Key: &KeyHandle{
			KeyID:     req.Name,
			Version:   1,
			Algorithm: KeyAlgorithm(attrsToAlgorithm(req.KeyAttributes)),
			Status:    KeyStatusEnabled,
			CreatedAt: time.Now(),
		},
		Metadata: &KeyMetadata{
			KeyID:              req.Name,
			Name:               req.Name,
			Description:        req.Description,
			Algorithm:          KeyAlgorithm(attrsToAlgorithm(req.KeyAttributes)),
			Status:             KeyStatusEnabled,
			Usage:              req.Usage,
			Version:            1,
			Exportable:         req.Exportable,
			DeletionProtection: req.DeletionProtection,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
			Tags:               req.Tags,
			Owner:              req.Owner,
			RotationPolicy:     resolvedRotationPolicy,
		},
		Timestamp: time.Now(),
	}

	// Persist application-level metadata (description, exportable, tags, owner,
	// and optional rotation policy) that the crypto backend does not store natively.
	if kmsMeta, ok := keyMetadata.(*kms.KeyMetadata); ok {
		kmsMeta.Name = req.Name
		kmsMeta.Description = req.Description
		kmsMeta.Exportable = req.Exportable
		kmsMeta.DeletionProtection = req.DeletionProtection
		kmsMeta.Tags = req.Tags
		kmsMeta.Owner = req.Owner
		kmsMeta.Usage = serviceUsageToKMS(req.Usage)

		// policyPersisted records that this call wrote the rotation policy to
		// the KVStore, so the rollback below removes exactly that entry.
		policyPersisted := false

		// Apply rotation policy if supplied at creation time.
		if resolvedRotationPolicy != nil {
			kmsMeta.RotationPolicy = serviceRotationToKMS(resolvedRotationPolicy)

			// Persist in KVStore for distributed replication.
			if persistErr := k.persistRotationPolicy(ctx, keyID, resolvedRotationPolicy); persistErr != nil {
				if k.logger != nil {
					k.logger.WithFields(map[string]interface{}{
						"key_id": req.Name,
						"error":  persistErr.Error(),
					}).Warn("GenerateKey: failed to persist rotation policy to KVStore")
				}
			} else {
				policyPersisted = true
			}
		}

		// A key without its metadata must not exist: delete the key this
		// call created (the name check above refused an existing one) and
		// the rotation policy it persisted, then fail, so the caller can
		// retry the same name and the retry never inherits this attempt's
		// policy. A cleanup that fails is carried in the returned error.
		if storeErr := k.kmsManager.UpdateStoredMetadata(kmsMeta); storeErr != nil {
			if k.metrics != nil {
				k.metrics.errorsByType.With("kms_error", "generate_key").Inc()
			}
			cause := storeErr
			if delErr := k.kmsManager.DeleteKey(ctx, keyID); delErr != nil {
				cause = stderrors.Join(cause, delErr)
			}
			if policyPersisted {
				if policyErr := k.persistRotationPolicy(ctx, keyID, nil); policyErr != nil {
					cause = stderrors.Join(cause, policyErr)
				}
			}
			return nil, WrapError(cause, errors.ErrInternal, "kms.GenerateKey", "failed to store key metadata")
		}
	}

	if k.metrics != nil {
		k.metrics.keysGeneratedTotal.Inc()
		k.metrics.activeKeys.Inc()
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":    req.Name,
			"algorithm": KeyAlgorithm(attrsToAlgorithm(req.KeyAttributes)),
			"owner":     req.Owner,
		}).Info("Generated new cryptographic key")
	}

	// Track the tenant-scoped key name to prevent duplicates within a tenant
	k.keyNames.Store(keyID, keyID)

	return resp, nil
}

// ImportKey imports an externally generated key into the KMS.
func (k *kmsServiceImpl) ImportKey(ctx context.Context, req *ImportKeyRequest) (*ImportKeyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("import_key").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateImportKeyRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "import_key").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Use the provided name as the key ID, scoped by tenant if applicable
	keyID := tenantScopedKeyID(tenantID, req.Name)

	// Get algorithm string from attributes
	algorithm := attrsToAlgorithm(req.KeyAttributes)

	// Call KMS manager to import key
	keyMetadata, err := k.kmsManager.ImportKey(ctx, keyID, req.KeyMaterial, algorithm)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("kms_error", "import_key").Inc()
		}
		return nil, WrapError(err, importKeyErrorCode(err), "kms.ImportKey", "failed to import key")
	}

	// Convert manager response to service response
	// Return the original (unscoped) key name to callers
	resp := &ImportKeyResponse{
		Key: &KeyHandle{
			KeyID:     req.Name,
			Version:   1,
			Algorithm: KeyAlgorithm(attrsToAlgorithm(req.KeyAttributes)),
			Status:    KeyStatusEnabled,
			CreatedAt: time.Now(),
		},
		Metadata: &KeyMetadata{
			KeyID:              req.Name,
			Name:               req.Name,
			Description:        req.Description,
			Algorithm:          KeyAlgorithm(attrsToAlgorithm(req.KeyAttributes)),
			Status:             KeyStatusEnabled,
			Usage:              req.Usage,
			Version:            1,
			Exportable:         req.KeyAttributes.Exportable,
			DeletionProtection: req.DeletionProtection,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
			Tags:               req.Tags,
		},
		Timestamp: time.Now(),
	}

	if k.metrics != nil {
		k.metrics.activeKeys.Inc()
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":    keyID,
			"algorithm": KeyAlgorithm(attrsToAlgorithm(req.KeyAttributes)),
		}).Info("Imported cryptographic key")
	}

	// Persist application-level metadata (description, exportable, tags, owner)
	// that the crypto backend does not store natively.
	if kmsMeta, ok := keyMetadata.(*kms.KeyMetadata); ok {
		kmsMeta.Name = req.Name
		kmsMeta.Description = req.Description
		kmsMeta.Exportable = req.KeyAttributes.Exportable
		kmsMeta.DeletionProtection = req.DeletionProtection
		kmsMeta.Tags = req.Tags
		kmsMeta.Usage = serviceUsageToKMS(req.Usage)
		if err := k.kmsManager.UpdateStoredMetadata(kmsMeta); err != nil {
			if k.logger != nil {
				k.logger.WithFields(map[string]interface{}{
					"key_id": req.Name,
					"error":  err.Error(),
				}).Warn("Failed to persist application-level key metadata")
			}
		}
	}

	return resp, nil
}

// importKeyClassifiedCodes are the manager failure codes ImportKey passes
// through unchanged. WrapError always stamps the supplied code, so without
// this set an import of an existing key (AlreadyExists) or of bad material
// (InvalidInput) would be clobbered with ErrInternal and answer 500.
var importKeyClassifiedCodes = map[errors.ErrorCode]struct{}{
	errors.ErrAlreadyExists: {},
	errors.ErrNotFound:      {},
	errors.ErrInvalidInput:  {},
}

// importKeyErrorCode returns the code ImportKey reports for a manager
// failure: the manager's own code when it classified the failure, not-found
// for a missing key, and ErrInternal for anything unclassified.
func importKeyErrorCode(err error) errors.ErrorCode {
	if stderrors.Is(err, kms.ErrKeyNotFound) {
		return errors.ErrNotFound
	}
	var qbErr *errors.QBError
	if stderrors.As(err, &qbErr) {
		if _, ok := importKeyClassifiedCodes[qbErr.Code]; ok {
			return qbErr.Code
		}
	}
	return errors.ErrInternal
}

// GetKey retrieves key material or public key by key ID.
// If a specific version is requested, it validates that the version exists.
// If no version is specified (version=0), the current version is returned.
func (k *kmsServiceImpl) GetKey(ctx context.Context, req *GetKeyRequest) (*GetKeyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("get_key").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateGetKeyRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "get_key").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the key ID with tenant prefix for storage lookup
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Call KMS manager to get key metadata
	keyMetadata, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "get_key").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.GetKey", "key not found")
	}

	// Extract actual values from key metadata
	var name string
	currentVersion := uint64(1)
	algorithm := KeyAlgAES256GCM
	status := KeyStatusEnabled
	createdAt := time.Now()
	updatedAt := time.Now()

	// Variables for application-level metadata fields
	var description string
	var exportable bool
	var deletionProtection bool
	var tags map[string]string
	var owner string
	var usage []KeyUsage

	// Try to extract metadata from kms.KeyMetadata (the type returned by kmsManager.GetKey)
	if kmsMeta, ok := keyMetadata.(*kms.KeyMetadata); ok {
		name = kmsMeta.Name
		currentVersion, err = kmsKeyVersion("kms.GetKey", kmsMeta.Version)
		if err != nil {
			return nil, err
		}
		algorithm = KeyAlgorithm(kmsMeta.Algorithm)
		status = kmsStateToServiceStatus(kmsMeta.State)
		createdAt = kmsMeta.Created
		updatedAt = kmsMeta.Updated
		description = kmsMeta.Description
		exportable = kmsMeta.Exportable
		deletionProtection = kmsMeta.DeletionProtection
		tags = kmsMeta.Tags
		owner = kmsMeta.Owner
		usage = kmsUsageToService(kmsMeta.Usage)
	} else if km, ok := keyMetadata.(*KeyMetadata); ok {
		// Also handle service.KeyMetadata if returned
		name = km.Name
		currentVersion = km.Version
		algorithm = km.Algorithm
		status = km.Status
		createdAt = km.CreatedAt
		updatedAt = km.UpdatedAt
		description = km.Description
		exportable = km.Exportable
		deletionProtection = km.DeletionProtection
		tags = km.Tags
		owner = km.Owner
		usage = km.Usage
	}

	// Determine which version to return
	requestedVersion := req.Version
	if requestedVersion == 0 {
		// No version specified, use current version
		requestedVersion = currentVersion
	} else if requestedVersion > currentVersion {
		// Validate that the requested version exists
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "get_key").Inc()
		}
		return nil, NewError(errors.ErrNotFound, "kms.GetKey",
			fmt.Sprintf("key version %d not found (current version is %d)", requestedVersion, currentVersion))
	}

	// Determine the status for the requested version
	// Check if this version has been explicitly disabled (e.g., via grace period)
	versionStatus := status
	if requestedVersion < currentVersion {
		// Check if this old version has been explicitly disabled
		if explicitStatus, _ := k.getVersionStatus(ctx, scopedKeyID, requestedVersion); explicitStatus != "" {
			versionStatus = explicitStatus
		}
		// If no explicit status, keep as enabled (default for old versions during grace period)
	}

	// Convert manager response to service response
	resp := &GetKeyResponse{
		Key: &KeyHandle{
			KeyID:     req.KeyID,
			Version:   requestedVersion,
			Algorithm: algorithm,
			Status:    versionStatus,
			CreatedAt: createdAt,
		},
		Metadata: &KeyMetadata{
			KeyID:              req.KeyID,
			Name:               name,
			Description:        description,
			Version:            requestedVersion,
			Algorithm:          algorithm,
			Status:             versionStatus,
			Exportable:         exportable,
			DeletionProtection: deletionProtection,
			Usage:              usage,
			Tags:               tags,
			Owner:              owner,
			CreatedAt:          createdAt,
			UpdatedAt:          updatedAt,
		},
		Timestamp: time.Now(),
	}

	return resp, nil
}

// GetPublicKey retrieves the public key component of an asymmetric key.
func (k *kmsServiceImpl) GetPublicKey(ctx context.Context, req *GetPublicKeyRequest) (*GetPublicKeyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("get_public_key").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "get_public_key").Inc()
		}
		return nil, NewError(errors.ErrInvalidInput, "kms.GetPublicKey", "request is required")
	}
	if req.KeyID == "" {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "get_public_key").Inc()
		}
		return nil, NewError(errors.ErrInvalidInput, "kms.GetPublicKey", "key ID is required")
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Call KMS manager to get public key
	publicKeyBytes, err := k.kmsManager.GetPublicKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "get_public_key").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.GetPublicKey", "failed to get public key")
	}

	// Get key metadata for algorithm and version info
	keyMetadata, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "get_public_key").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.GetPublicKey", "key not found")
	}

	// Extract version and algorithm from metadata
	currentVersion := uint64(1)
	algorithm := KeyAlgECDSAP256 // Default for asymmetric

	if kmsMeta, ok := keyMetadata.(*kms.KeyMetadata); ok {
		currentVersion, err = kmsKeyVersion("kms.GetPublicKey", kmsMeta.Version)
		if err != nil {
			return nil, err
		}
		algorithm = KeyAlgorithm(kmsMeta.Algorithm)
	} else if km, ok := keyMetadata.(*KeyMetadata); ok {
		currentVersion = km.Version
		algorithm = km.Algorithm
	}

	resp := &GetPublicKeyResponse{
		KeyID:     req.KeyID,
		PublicKey: publicKeyBytes,
		Algorithm: algorithm,
		Version:   currentVersion,
		Timestamp: time.Now(),
	}

	return resp, nil
}

// ListKeys returns all keys accessible to the requester with pagination.
func (k *kmsServiceImpl) ListKeys(ctx context.Context, req *ListKeysRequest) (*ListKeysResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("list_keys").Observe(time.Since(start).Seconds())
		}()
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, listTenantErr := prepareTenantContext(ctx)
	if listTenantErr != nil {
		return nil, listTenantErr
	}

	// Extract prefix from filter if provided
	prefix := ""
	if req != nil && req.Filter != nil {
		if p, ok := req.Filter["prefix"]; ok {
			prefix = p
		}
	}

	// Scope prefix with tenant namespace for isolation
	scopedPrefix := tenantListPrefix(tenantID) + prefix

	// Call KMS manager to list keys with prefix filter
	keys, err := k.kmsManager.ListKeys(ctx, scopedPrefix)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("kms_error", "list_keys").Inc()
		}
		return nil, WrapError(err, errors.ErrInternal, "kms.ListKeys", "failed to list keys")
	}

	// Convert manager response to service response
	keyHandles := make([]*KeyHandle, 0, len(keys))
	for _, key := range keys {
		// Try to extract kms.KeyMetadata from the returned interface
		if kmsMeta, ok := key.(*kms.KeyMetadata); ok {
			version, versionErr := kmsKeyVersion("kms.ListKeys", kmsMeta.Version)
			if versionErr != nil {
				return nil, versionErr
			}
			keyHandles = append(keyHandles, &KeyHandle{
				KeyID:     kmsMeta.KeyID,
				Version:   version,
				Algorithm: KeyAlgorithm(kmsMeta.Algorithm),
				Status:    kmsStateToServiceStatus(kmsMeta.State),
				CreatedAt: kmsMeta.Created,
			})
		} else if keyMeta, ok := key.(*KeyMetadata); ok {
			// Also handle service.KeyMetadata if returned
			keyHandles = append(keyHandles, &KeyHandle{
				KeyID:     keyMeta.KeyID,
				Version:   keyMeta.Version,
				Algorithm: keyMeta.Algorithm,
				Status:    keyMeta.Status,
				CreatedAt: keyMeta.CreatedAt,
			})
		} else if key != nil {
			// Fallback: try to extract fields via reflection for unknown types
			handle := &KeyHandle{
				Algorithm: KeyAlgAES256GCM,
				Status:    KeyStatusEnabled,
				CreatedAt: time.Now(),
			}

			// Use reflection to extract KeyID if available
			val := reflect.ValueOf(key)
			if val.Kind() == reflect.Pointer && !val.IsNil() {
				val = val.Elem()
			}
			if val.Kind() == reflect.Struct {
				if keyIDField := val.FieldByName("KeyID"); keyIDField.IsValid() && keyIDField.Kind() == reflect.String {
					handle.KeyID = keyIDField.String()
				}
				if versionField := val.FieldByName("Version"); versionField.IsValid() {
					switch versionField.Kind() {
					case reflect.Int, reflect.Int32, reflect.Int64:
						version, versionErr := conv.SafeInt64ToUint64(versionField.Int())
						if versionErr != nil {
							return nil, WrapError(versionErr, errors.ErrInternal, "kms.ListKeys", "invalid key version in metadata")
						}
						handle.Version = version
					case reflect.Uint, reflect.Uint32, reflect.Uint64:
						handle.Version = versionField.Uint()
					}
				}
				if algoField := val.FieldByName("Algorithm"); algoField.IsValid() && algoField.Kind() == reflect.String {
					handle.Algorithm = KeyAlgorithm(algoField.String())
				}
				if stateField := val.FieldByName("State"); stateField.IsValid() && stateField.Kind() == reflect.String {
					handle.Status = kmsStateToServiceStatus(kms.KeyState(stateField.String()))
				}
			}

			if k.logger != nil {
				k.logger.WithFields(map[string]interface{}{
					"key_type": fmt.Sprintf("%T", key),
					"key_id":   handle.KeyID,
					"via":      "reflection_fallback",
				}).Warn("ListKeys: used reflection fallback for unexpected type")
			}

			keyHandles = append(keyHandles, handle)
		}
	}

	// Strip tenant prefix from key IDs in the response
	if tenantID != "" {
		for _, handle := range keyHandles {
			handle.KeyID = tenantStripKeyID(tenantID, handle.KeyID)
		}
	}

	// Apply limit if specified
	limit := req.Limit
	if limit <= 0 {
		limit = 100 // Default limit
	}
	if len(keyHandles) > limit {
		keyHandles = keyHandles[:limit]
	}

	resp := &ListKeysResponse{
		Keys:       keyHandles,
		NextCursor: "",
		HasMore:    len(keys) > limit,
		TotalCount: len(keys),
		Timestamp:  time.Now(),
	}

	return resp, nil
}

// RotateKey creates a new version of an existing key.
func (k *kmsServiceImpl) RotateKey(ctx context.Context, req *RotateKeyRequest) (*RotateKeyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("rotate_key").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateRotateKeyRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "rotate_key").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the key ID with tenant prefix for storage lookup
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Get current version before rotation
	oldResult, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("kms_error", "rotate_key").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.RotateKey", "key not found")
	}

	oldMetadata, ok := oldResult.(*kms.KeyMetadata)
	if !ok {
		return nil, WrapError(nil, errors.ErrInternal, "kms.RotateKey", "unexpected key metadata type")
	}
	oldVersion, err := kmsKeyVersion("kms.RotateKey", oldMetadata.Version)
	if err != nil {
		return nil, err
	}

	// Call KMS manager to rotate key
	newResult, err := k.kmsManager.RotateKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("kms_error", "rotate_key").Inc()
		}
		// A key outside the caller's tenant is absent from its scope and
		// surfaces as not-found, mirroring GetKey and DeleteKey.
		if stderrors.Is(err, kms.ErrKeyNotFound) {
			return nil, WrapError(err, errors.ErrNotFound, "kms.RotateKey", "key not found")
		}
		return nil, WrapError(err, errors.ErrInternal, "kms.RotateKey", "failed to rotate key")
	}

	newMetadata, ok := newResult.(*kms.KeyMetadata)
	if !ok {
		return nil, WrapError(nil, errors.ErrInternal, "kms.RotateKey", "unexpected rotated key metadata type")
	}
	newVersion, err := kmsKeyVersion("kms.RotateKey", newMetadata.Version)
	if err != nil {
		return nil, err
	}

	resp := &RotateKeyResponse{
		KeyID:      req.KeyID,
		OldVersion: oldVersion,
		NewVersion: newVersion,
		Timestamp:  time.Now(),
	}

	// Schedule grace period job if configured
	if req.GracePeriodSeconds > 0 && k.scheduler != nil {
		gracePeriodDisableAt := time.Now().Add(time.Duration(req.GracePeriodSeconds) * time.Second)

		// Create payload for the grace period handler
		// The handler addresses the version by its tenant-scoped key ID, the
		// same ID GetKey, UpdateKeyStatus and ListKeyVersions read it under.
		payload := fmt.Sprintf(`{"key_id":%q,"version":%d}`, scopedKeyID, resp.OldVersion)

		// A tenant's grace job is that tenant's own job, filed in its
		// namespace, so the tenant that rotated the key can read and cancel
		// the job ID it is handed. The system tenant's stays delegated.
		ownerType, ownerID := SchedulerOwnerTypeDelegated, ""
		if engineTenant := schedulerEngineTenant(tenantID); engineTenant != "" {
			ownerType, ownerID = SchedulerOwnerTypeTenant, engineTenant
		}

		job := &SchedulerJob{
			Name:           fmt.Sprintf("kms-grace-%s-v%d", scopedKeyID, resp.OldVersion),
			Description:    fmt.Sprintf("Disable key %s version %d after grace period", req.KeyID, resp.OldVersion),
			OwnerType:      ownerType,
			OwnerID:        ownerID,
			TargetTenantID: ownerID,
			Type:           SchedulerJobTypeOneShot,
			ExecuteAt:      gracePeriodDisableAt,
			Handler:        "kms.grace_period",
			Payload:        []byte(payload),
			Status:         SchedulerJobStatusPending,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		if err := k.scheduler.ScheduleJob(ctx, job); err != nil {
			// Log the error but don't fail the rotation
			if k.logger != nil {
				k.logger.WithFields(map[string]interface{}{
					"key_id":       req.KeyID,
					"old_version":  resp.OldVersion,
					"grace_period": req.GracePeriodSeconds,
					"error":        err.Error(),
				}).Warn("Failed to schedule grace period job")
			}
		} else {
			resp.GracePeriodDisableAt = gracePeriodDisableAt.Unix()
			resp.GracePeriodJobID = fmt.Sprintf("%d", job.ID)

			if k.logger != nil {
				k.logger.WithFields(map[string]interface{}{
					"key_id":            req.KeyID,
					"old_version":       resp.OldVersion,
					"grace_period_secs": req.GracePeriodSeconds,
					"disable_at":        gracePeriodDisableAt,
					"job_id":            job.ID,
				}).Info("Scheduled grace period job for key version")
			}
		}
	}

	if k.metrics != nil {
		k.metrics.keysRotatedTotal.Inc()
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":      req.KeyID,
			"old_version": resp.OldVersion,
			"new_version": resp.NewVersion,
		}).Info("Rotated cryptographic key")
	}

	return resp, nil
}

// RevokeKey marks a key version as revoked and unusable.
func (k *kmsServiceImpl) RevokeKey(ctx context.Context, req *RevokeKeyRequest) (*RevokeKeyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("revoke_key").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateRevokeKeyRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "revoke_key").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Call KMS manager to revoke key
	err = k.kmsManager.RevokeKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("kms_error", "revoke_key").Inc()
		}
		if stderrors.Is(err, kms.ErrKeyNotFound) {
			return nil, WrapError(err, errors.ErrNotFound, "kms.RevokeKey", "key not found")
		}
		return nil, WrapError(err, errors.ErrInternal, "kms.RevokeKey", "failed to revoke key")
	}

	resp := &RevokeKeyResponse{
		KeyID:     req.KeyID,
		Status:    KeyStatusCompromised,
		Timestamp: time.Now(),
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id": req.KeyID,
			"reason": req.Reason,
		}).Warn("Revoked cryptographic key")
	}

	return resp, nil
}

// DeleteKey permanently removes a key and all its versions.
func (k *kmsServiceImpl) DeleteKey(ctx context.Context, req *DeleteKeyRequest) (*DeleteKeyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("delete_key").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateDeleteKeyRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "delete_key").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Scope the key ID with tenant prefix for storage lookup
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Call KMS manager to delete key
	err = k.kmsManager.DeleteKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("kms_error", "delete_key").Inc()
		}
		// A missing key must surface as not-found (HTTP 404), mirroring the
		// GetKey path. WrapError always stamps the supplied code, so without
		// this branch the manager's ErrNotFound would be clobbered with
		// ErrInternal and deleting an already-deleted key would return 500.
		if stderrors.Is(err, kms.ErrKeyNotFound) {
			return nil, WrapError(err, errors.ErrNotFound, "kms.DeleteKey", "key not found")
		}
		return nil, WrapError(err, errors.ErrInternal, "kms.DeleteKey", "failed to delete key")
	}

	// Release the name in the caller's tenant only; another tenant's key of
	// the same name keeps its entry.
	k.keyNames.Delete(scopedKeyID)

	// Calculate deletion date based on pending window
	pendingDays := req.PendingWindowDays
	if pendingDays == 0 {
		pendingDays = 7 // Default to 7 days
	}
	if pendingDays < 7 {
		pendingDays = 7 // Minimum 7 days
	}
	if pendingDays > 30 {
		pendingDays = 30 // Maximum 30 days
	}

	deletionDate := time.Now().Add(time.Duration(pendingDays) * 24 * time.Hour)

	resp := &DeleteKeyResponse{
		KeyID:        req.KeyID,
		Status:       KeyStatusPendingDeletion,
		DeletionDate: deletionDate,
		Timestamp:    time.Now(),
	}

	if k.metrics != nil {
		k.metrics.activeKeys.Dec()
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":        req.KeyID,
			"deletion_date": deletionDate,
			"force":         req.Force,
		}).Warn("Scheduled key for deletion")
	}

	return resp, nil
}

// GetKeyMetadata retrieves metadata for a key without exposing key material.
func (k *kmsServiceImpl) GetKeyMetadata(ctx context.Context, req *GetKeyMetadataRequest) (*GetKeyMetadataResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("get_key_metadata").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateGetKeyMetadataRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "get_key_metadata").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Call KMS manager to get key metadata
	keyData, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "get_key_metadata").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.GetKeyMetadata", "key not found")
	}

	metadata, err := extractKeyMetadata(req.KeyID, keyData)
	if err != nil {
		return nil, err
	}

	resp := &GetKeyMetadataResponse{
		Metadata:  metadata,
		Timestamp: time.Now(),
	}

	return resp, nil
}

// UpdateKeyMetadata updates mutable metadata fields for a key.
func (k *kmsServiceImpl) UpdateKeyMetadata(ctx context.Context, req *UpdateKeyMetadataRequest) (*UpdateKeyMetadataResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("update_key_metadata").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateUpdateKeyMetadataRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "update_key_metadata").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Retrieve existing key metadata to merge with updates
	keyData, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "update_key_metadata").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.UpdateKeyMetadata", "key not found")
	}

	// Apply updates and persist based on the concrete type returned by the manager.
	// The KMS manager store expects *kms.KeyMetadata, so we must work with that
	// type for persistence, then convert to service types for the response.
	if kmsMeta, ok := keyData.(*kms.KeyMetadata); ok {
		kmsMeta.Updated = time.Now()
		if req.Name != nil {
			kmsMeta.Name = *req.Name
		}
		if req.Description != nil {
			kmsMeta.Description = *req.Description
		}
		if len(req.Tags) > 0 {
			if kmsMeta.Tags == nil {
				kmsMeta.Tags = make(map[string]string, len(req.Tags))
			}
			for tagKey, tagVal := range req.Tags {
				kmsMeta.Tags[tagKey] = tagVal
			}
		}
		for _, tagKey := range req.RemoveTags {
			delete(kmsMeta.Tags, tagKey)
		}
		if updateErr := k.kmsManager.UpdateStoredMetadata(kmsMeta); updateErr != nil {
			if k.metrics != nil {
				k.metrics.errorsByType.With("internal", "update_key_metadata").Inc()
			}
			return nil, WrapError(updateErr, errors.ErrInternal, "kms.UpdateKeyMetadata", "failed to persist metadata")
		}
	}

	// Build the service-layer response from the (now updated) key data
	metadata, err := extractKeyMetadata(req.KeyID, keyData)
	if err != nil {
		return nil, err
	}

	resp := &UpdateKeyMetadataResponse{
		Metadata:  metadata,
		Timestamp: time.Now(),
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id": req.KeyID,
		}).Info("Updated key metadata")
	}

	return resp, nil
}

// SetKeyPolicy sets the access control policy for a key and persists it in
// the Raft-replicated KVStore so that every node evaluates the same policy.
// The policy is validated, the key is verified to exist, and then the policy
// is stored under kms:policy:<keyID>.
func (k *kmsServiceImpl) SetKeyPolicy(ctx context.Context, req *SetKeyPolicyRequest) (*SetKeyPolicyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("set_key_policy").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateSetKeyPolicyRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "set_key_policy").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Verify key exists
	_, err = k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "set_key_policy").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.SetKeyPolicy", "key not found")
	}

	// Stamp the policy with the updated timestamp and incremented version.
	newPolicy := *req.Policy
	newPolicy.KeyID = scopedKeyID
	newPolicy.Version = req.Policy.Version + 1
	newPolicy.UpdatedAt = time.Now()

	// Persist the policy via the Raft-backed KVStore, under the tenant-scoped
	// key ID so that one tenant's policy never lands on another's key.
	if err := k.persistKeyPolicy(ctx, &newPolicy); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("storage_error", "set_key_policy").Inc()
		}
		return nil, WrapError(err, errors.ErrStorage, "kms.SetKeyPolicy", "failed to persist key policy")
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":         req.KeyID,
			"policy_version": newPolicy.Version,
		}).Info("Updated key policy")
	}

	return &SetKeyPolicyResponse{
		KeyID:         req.KeyID,
		PolicyVersion: newPolicy.Version,
		Timestamp:     newPolicy.UpdatedAt,
	}, nil
}

// GetKeyPolicy retrieves the persisted access control policy for a key.
// When no policy has been stored, a default open policy is returned so that
// callers can distinguish "key exists but no policy" from "key not found".
func (k *kmsServiceImpl) GetKeyPolicy(ctx context.Context, req *GetKeyPolicyRequest) (*GetKeyPolicyResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("get_key_policy").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateGetKeyPolicyRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "get_key_policy").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Verify key exists
	_, err = k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "get_key_policy").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.GetKeyPolicy", "key not found")
	}

	// Load persisted policy.
	policy, err := k.loadKeyPolicy(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("storage_error", "get_key_policy").Inc()
		}
		return nil, WrapError(err, errors.ErrStorage, "kms.GetKeyPolicy", "failed to load key policy")
	}

	// No policy set yet — return a default open policy.
	if policy == nil {
		policy = &KeyPolicy{
			Version:           0,
			AllowedOperations: []KeyUsage{},
			UpdatedAt:         time.Now(),
		}
	}
	// The policy is stored under the tenant-scoped key ID; the caller sees
	// the ID it asked for.
	policy.KeyID = req.KeyID

	return &GetKeyPolicyResponse{
		Policy:    policy,
		Timestamp: time.Now(),
	}, nil
}

// DeleteKeyPolicy removes the access control policy for a key, restoring the
// default-allow-all behaviour for authenticated principals.
func (k *kmsServiceImpl) DeleteKeyPolicy(ctx context.Context, req *DeleteKeyPolicyRequest) (*DeleteKeyPolicyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.DeleteKeyPolicy", "request is nil")
	}
	if req.KeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.DeleteKeyPolicy", "key_id is required")
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Verify key exists
	_, err = k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		return nil, WrapError(err, errors.ErrNotFound, "kms.DeleteKeyPolicy", "key not found")
	}

	if err := k.deleteKeyPolicy(ctx, scopedKeyID); err != nil {
		return nil, WrapError(err, errors.ErrStorage, "kms.DeleteKeyPolicy", "failed to delete key policy")
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id": req.KeyID,
		}).Info("Deleted key policy")
	}

	return &DeleteKeyPolicyResponse{
		KeyID:     req.KeyID,
		Timestamp: time.Now(),
	}, nil
}

// =============================================================================
// Key Rotation Policy Operations
// =============================================================================

// kmsRotationPolicyPrefix is the KVStore namespace for persisted per-key rotation policies.
const kmsRotationPolicyPrefix = "kms:rotpolicy:"

// kmsRotationPolicyKey returns the KVStore key for a given keyID's rotation policy.
func kmsRotationPolicyKey(keyID string) string {
	return kmsRotationPolicyPrefix + keyID
}

// persistRotationPolicy serialises and stores the rotation policy in the KVStore.
// When kvStore is nil (not configured) the policy is stored only in the
// in-memory metadata via UpdateStoredMetadata so single-node deployments work.
func (k *kmsServiceImpl) persistRotationPolicy(ctx context.Context, keyID string, policy *RotationPolicy) error {
	if k.kvStore == nil {
		return nil
	}
	if policy == nil {
		// A nil policy means "delete" — remove the key if it exists.
		return k.kvStore.Delete(ctx, kmsRotationPolicyKey(keyID))
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return NewError(errors.ErrInternal, "kms.persistRotationPolicy", "failed to marshal rotation policy")
	}
	if putErr := k.kvStore.Put(ctx, kmsRotationPolicyKey(keyID), data); putErr != nil {
		return WrapError(putErr, errors.ErrStorage, "kms.persistRotationPolicy", "failed to store rotation policy")
	}
	return nil
}

// loadRotationPolicy reads the persisted rotation policy for keyID.
// Returns (nil, nil) when no policy has been stored.
func (k *kmsServiceImpl) loadRotationPolicy(ctx context.Context, keyID string) (*RotationPolicy, error) {
	if k.kvStore == nil {
		return nil, nil
	}
	data, err := k.kvStore.Get(ctx, kmsRotationPolicyKey(keyID))
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, WrapError(err, errors.ErrStorage, "kms.loadRotationPolicy", "failed to load rotation policy")
	}
	if len(data) == 0 {
		return nil, nil
	}
	var policy RotationPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, NewError(errors.ErrInternal, "kms.loadRotationPolicy", "failed to unmarshal rotation policy")
	}
	return &policy, nil
}

// SetKeyRotationPolicy sets or replaces the automatic rotation policy for a key.
// Validates: AutoRotate requires Interval > 0. Computes NextRotation from
// Interval when not explicitly supplied. Gated by per-key authz (key-management
// operations use the "set_rotation_policy" operation string which is not mapped
// to a crypto operation — the authz gate allows it unless DeniedPrincipals matches).
func (k *kmsServiceImpl) SetKeyRotationPolicy(ctx context.Context, req *SetKeyRotationPolicyRequest) (*SetKeyRotationPolicyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeyRotationPolicy", "request is nil")
	}
	if req.KeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeyRotationPolicy", "key_id is required")
	}
	if req.Policy == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeyRotationPolicy", "policy is required")
	}
	if req.Policy.AutoRotate && req.Policy.Interval <= 0 {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeyRotationPolicy", "interval must be positive when auto_rotate is true")
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Per-key authz gate: key-management operation.
	if err := k.checkKeyAuthz(ctx, scopedKeyID, "set_rotation_policy"); err != nil {
		return nil, err
	}

	// Verify key exists.
	keyData, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		return nil, WrapError(err, errors.ErrNotFound, "kms.SetKeyRotationPolicy", "key not found")
	}

	// Build the policy with a computed NextRotation.
	policy := *req.Policy
	if policy.AutoRotate && policy.Interval > 0 && policy.NextRotation.IsZero() {
		policy.NextRotation = time.Now().Add(policy.Interval)
	}

	// Persist to KVStore (distributed replication).
	if err := k.persistRotationPolicy(ctx, scopedKeyID, &policy); err != nil {
		return nil, WrapError(err, errors.ErrStorage, "kms.SetKeyRotationPolicy", "failed to persist rotation policy")
	}

	// Also embed in key metadata so the scheduler can read it from ListKeys/GetKey.
	if kmsMeta, ok := keyData.(*kms.KeyMetadata); ok {
		kmsMeta.Updated = time.Now()
		kmsMeta.RotationPolicy = serviceRotationToKMS(&policy)
		if updateErr := k.kmsManager.UpdateStoredMetadata(kmsMeta); updateErr != nil {
			if k.logger != nil {
				k.logger.WithFields(map[string]interface{}{
					"key_id": req.KeyID,
					"error":  updateErr.Error(),
				}).Warn("SetKeyRotationPolicy: failed to embed rotation policy in key metadata")
			}
		}
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":      req.KeyID,
			"auto_rotate": policy.AutoRotate,
			"interval":    policy.Interval.String(),
		}).Info("Set key rotation policy")
	}

	return &SetKeyRotationPolicyResponse{
		KeyID:     req.KeyID,
		Policy:    &policy,
		Timestamp: time.Now(),
	}, nil
}

// GetKeyRotationPolicy retrieves the current automatic rotation policy for a key.
// Reads from KVStore first; falls back to key metadata for single-node deployments.
func (k *kmsServiceImpl) GetKeyRotationPolicy(ctx context.Context, req *GetKeyRotationPolicyRequest) (*GetKeyRotationPolicyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.GetKeyRotationPolicy", "request is nil")
	}
	if req.KeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.GetKeyRotationPolicy", "key_id is required")
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Per-key authz gate: read operation.
	if err := k.checkKeyAuthz(ctx, scopedKeyID, "get_rotation_policy"); err != nil {
		return nil, err
	}

	// Verify key exists.
	keyData, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		return nil, WrapError(err, errors.ErrNotFound, "kms.GetKeyRotationPolicy", "key not found")
	}

	// Prefer KVStore (replicated); fall back to embedded metadata.
	policy, err := k.loadRotationPolicy(ctx, scopedKeyID)
	if err != nil {
		return nil, WrapError(err, errors.ErrStorage, "kms.GetKeyRotationPolicy", "failed to load rotation policy")
	}
	if policy == nil {
		// Fall back to metadata embedded in the key.
		if kmsMeta, ok := keyData.(*kms.KeyMetadata); ok && kmsMeta.RotationPolicy != nil {
			policy = kmsRotationToService(kmsMeta.RotationPolicy)
		}
	}

	return &GetKeyRotationPolicyResponse{
		KeyID:     req.KeyID,
		Policy:    policy,
		Timestamp: time.Now(),
	}, nil
}

// DeleteKeyRotationPolicy removes the rotation policy for a key, disabling
// automatic rotation. The scheduler will no longer rotate this key.
func (k *kmsServiceImpl) DeleteKeyRotationPolicy(ctx context.Context, req *DeleteKeyRotationPolicyRequest) (*DeleteKeyRotationPolicyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.DeleteKeyRotationPolicy", "request is nil")
	}
	if req.KeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.DeleteKeyRotationPolicy", "key_id is required")
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Per-key authz gate: key-management operation.
	if err := k.checkKeyAuthz(ctx, scopedKeyID, "delete_rotation_policy"); err != nil {
		return nil, err
	}

	// Verify key exists.
	keyData, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		return nil, WrapError(err, errors.ErrNotFound, "kms.DeleteKeyRotationPolicy", "key not found")
	}

	// Remove from KVStore.
	if err := k.persistRotationPolicy(ctx, scopedKeyID, nil); err != nil {
		return nil, WrapError(err, errors.ErrStorage, "kms.DeleteKeyRotationPolicy", "failed to remove rotation policy")
	}

	// Clear from key metadata.
	if kmsMeta, ok := keyData.(*kms.KeyMetadata); ok {
		kmsMeta.Updated = time.Now()
		kmsMeta.RotationPolicy = nil
		if updateErr := k.kmsManager.UpdateStoredMetadata(kmsMeta); updateErr != nil {
			if k.logger != nil {
				k.logger.WithFields(map[string]interface{}{
					"key_id": req.KeyID,
					"error":  updateErr.Error(),
				}).Warn("DeleteKeyRotationPolicy: failed to clear rotation policy from key metadata")
			}
		}
	}

	if k.logger != nil {
		k.logger.WithField("key_id", req.KeyID).Info("Deleted key rotation policy")
	}

	return &DeleteKeyRotationPolicyResponse{
		KeyID:     req.KeyID,
		Timestamp: time.Now(),
	}, nil
}

// serviceRotationToKMS converts a service RotationPolicy to the kms package RotationPolicy.
func serviceRotationToKMS(p *RotationPolicy) *kms.RotationPolicy {
	if p == nil {
		return nil
	}
	return &kms.RotationPolicy{
		Enabled:        p.Enabled,
		Interval:       p.Interval,
		NextRotation:   p.NextRotation,
		RetainVersions: p.RetainVersions,
		AutoRotate:     p.AutoRotate,
	}
}

// kmsRotationToService converts a kms RotationPolicy to the service RotationPolicy.
func kmsRotationToService(p *kms.RotationPolicy) *RotationPolicy {
	if p == nil {
		return nil
	}
	return &RotationPolicy{
		Enabled:        p.Enabled,
		Interval:       p.Interval,
		NextRotation:   p.NextRotation,
		RetainVersions: p.RetainVersions,
		AutoRotate:     p.AutoRotate,
	}
}

// =============================================================================
// Request Validation Methods
// =============================================================================

func (k *kmsServiceImpl) validateGenerateKeyRequest(req *GenerateKeyRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateGenerateKeyRequest", "request is nil")
	}

	if strings.TrimSpace(req.Name) == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateGenerateKeyRequest", "key name is required")
	}

	if !hasExplicitAlgorithm(req.KeyAttributes) {
		return NewError(errors.ErrInvalidInput, "kms.validateGenerateKeyRequest", "algorithm is required")
	}

	if len(req.Usage) == 0 {
		return NewError(errors.ErrInvalidInput, "kms.validateGenerateKeyRequest", "at least one key usage is required")
	}

	return nil
}

func (k *kmsServiceImpl) validateImportKeyRequest(req *ImportKeyRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateImportKeyRequest", "request is nil")
	}

	if strings.TrimSpace(req.Name) == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateImportKeyRequest", "key name is required")
	}

	// A name inside the tenant key namespace would let the caller address
	// another tenant's keys, so it is refused rather than scoped.
	if strings.HasPrefix(req.Name, kmsTenantKeyPrefix) {
		return NewError(errors.ErrInvalidInput, "kms.validateImportKeyRequest", "key name uses the reserved tenant namespace")
	}

	if len(req.KeyMaterial) == 0 {
		return NewError(errors.ErrInvalidInput, "kms.validateImportKeyRequest", "key material is required")
	}

	if !hasExplicitAlgorithm(req.KeyAttributes) {
		return NewError(errors.ErrInvalidInput, "kms.validateImportKeyRequest", "algorithm is required")
	}

	return nil
}

func (k *kmsServiceImpl) validateGetKeyRequest(req *GetKeyRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateGetKeyRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateGetKeyRequest", "key ID is required")
	}

	return nil
}

func (k *kmsServiceImpl) validateRotateKeyRequest(req *RotateKeyRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateRotateKeyRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateRotateKeyRequest", "key ID is required")
	}

	return nil
}

func (k *kmsServiceImpl) validateRevokeKeyRequest(req *RevokeKeyRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateRevokeKeyRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateRevokeKeyRequest", "key ID is required")
	}

	return nil
}

func (k *kmsServiceImpl) validateDeleteKeyRequest(req *DeleteKeyRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateDeleteKeyRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateDeleteKeyRequest", "key ID is required")
	}

	if req.PendingWindowDays < 0 || req.PendingWindowDays > 30 {
		return NewError(errors.ErrInvalidInput, "kms.validateDeleteKeyRequest", "pending window days must be between 0 and 30")
	}

	return nil
}

func (k *kmsServiceImpl) validateGetKeyMetadataRequest(req *GetKeyMetadataRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateGetKeyMetadataRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateGetKeyMetadataRequest", "key ID is required")
	}

	return nil
}

func (k *kmsServiceImpl) validateUpdateKeyMetadataRequest(req *UpdateKeyMetadataRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateUpdateKeyMetadataRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateUpdateKeyMetadataRequest", "key ID is required")
	}

	// At least one field must be updated
	if req.Name == nil && req.Description == nil && len(req.Tags) == 0 && len(req.RemoveTags) == 0 {
		return NewError(errors.ErrInvalidInput, "kms.validateUpdateKeyMetadataRequest", "at least one field must be updated")
	}

	return nil
}

func (k *kmsServiceImpl) validateSetKeyPolicyRequest(req *SetKeyPolicyRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateSetKeyPolicyRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateSetKeyPolicyRequest", "key ID is required")
	}

	if req.Policy == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateSetKeyPolicyRequest", "policy is required")
	}

	// AllowedOperations is optional: an empty list means all operations are
	// permitted (for any principal passing the AllowedPrincipals check).
	// A non-empty list restricts which operations are allowed.

	return nil
}

func (k *kmsServiceImpl) validateGetKeyPolicyRequest(req *GetKeyPolicyRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateGetKeyPolicyRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateGetKeyPolicyRequest", "key ID is required")
	}

	return nil
}

func (k *kmsServiceImpl) validateUpdateKeyStatusRequest(req *UpdateKeyStatusRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateUpdateKeyStatusRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateUpdateKeyStatusRequest", "key ID is required")
	}

	// Validate status is a valid transition target using set-based lookup
	if _, ok := validUpdateStatuses[req.Status]; ok {
		// Valid status values for update
	} else if _, ok := dedicatedOpStatuses[req.Status]; ok {
		return NewError(errors.ErrInvalidInput, "kms.validateUpdateKeyStatusRequest",
			"invalid status: use dedicated operations for deletion, rotation, or revocation")
	} else {
		return NewError(errors.ErrInvalidInput, "kms.validateUpdateKeyStatusRequest", "invalid status value")
	}

	return nil
}

func (k *kmsServiceImpl) validateListKeyVersionsRequest(req *ListKeyVersionsRequest) error {
	if req == nil {
		return NewError(errors.ErrInvalidInput, "kms.validateListKeyVersionsRequest", "request is nil")
	}

	if req.KeyID == "" {
		return NewError(errors.ErrInvalidInput, "kms.validateListKeyVersionsRequest", "key ID is required")
	}

	return nil
}

// attrsToAlgorithm converts KeyAttributes to a KeyAlgorithm string.
func attrsToAlgorithm(attrs *kctypes.KeyAttributes) string {
	if attrs == nil {
		return string(KeyAlgAES256GCM)
	}

	// Check symmetric algorithm first using map-based dispatch
	if attrs.SymmetricAlgorithm != "" {
		if alg, ok := symmetricAlgorithmMap[attrs.SymmetricAlgorithm]; ok {
			return string(alg)
		}
	}

	// Check asymmetric algorithm (x509.PublicKeyAlgorithm)
	if attrs.KeyAlgorithm == 1 { // x509.RSA
		keySize := 2048
		if attrs.RSAAttributes != nil && attrs.RSAAttributes.KeySize > 0 {
			keySize = attrs.RSAAttributes.KeySize
		}
		if alg, ok := rsaKeySizeMap[keySize]; ok {
			return string(alg)
		}
		return string(KeyAlgRSA2048)
	}

	if attrs.KeyAlgorithm == 3 { // x509.ECDSA
		if attrs.ECCAttributes != nil && attrs.ECCAttributes.Curve != nil {
			curveName := attrs.ECCAttributes.Curve.Params().Name
			if alg, ok := ecdsaCurveMap[curveName]; ok {
				return string(alg)
			}
		}
		return string(KeyAlgECDSAP256) // Default curve
	}

	if attrs.KeyAlgorithm == 4 { // x509.Ed25519
		return string(KeyAlgEd25519)
	}

	// Default to AES-256-GCM
	return string(KeyAlgAES256GCM)
}

// hasExplicitAlgorithm checks if an algorithm was explicitly specified in KeyAttributes.
// Returns false if attrs is nil or no algorithm is set.
func hasExplicitAlgorithm(attrs *kctypes.KeyAttributes) bool {
	if attrs == nil {
		return false
	}

	// Check for symmetric algorithm
	if attrs.SymmetricAlgorithm != "" {
		return true
	}

	// Check for asymmetric algorithm (x509.PublicKeyAlgorithm values 1-4 are valid)
	if attrs.KeyAlgorithm >= 1 && attrs.KeyAlgorithm <= 4 {
		return true
	}

	return false
}

// isValidDEKAlgorithm checks if an algorithm is valid for DEK creation.
func isValidDEKAlgorithm(algorithm KeyAlgorithm) bool {
	switch algorithm {
	case KeyAlgAES128GCM, KeyAlgAES192GCM, KeyAlgAES256GCM,
		KeyAlgChaCha20Poly1305, KeyAlgXChaCha20Poly1305:
		return true
	default:
		return false
	}
}

// GetSupportedAlgorithms returns the list of cryptographic algorithms supported by the current backend.
func (k *kmsServiceImpl) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return []string{
		string(KeyAlgAES128GCM),
		string(KeyAlgAES192GCM),
		string(KeyAlgAES256GCM),
		string(KeyAlgChaCha20Poly1305),
		string(KeyAlgXChaCha20Poly1305),
		string(KeyAlgRSA2048),
		string(KeyAlgRSA3072),
		string(KeyAlgRSA4096),
		string(KeyAlgECDSAP256),
		string(KeyAlgECDSAP384),
		string(KeyAlgECDSAP521),
		string(KeyAlgEd25519),
	}, nil
}

// kmsStateToServiceStatus converts kms.KeyState to service.KeyStatus.
func kmsStateToServiceStatus(state kms.KeyState) KeyStatus {
	switch state {
	case kms.KeyStateEnabled:
		return KeyStatusEnabled
	case kms.KeyStateDisabled:
		return KeyStatusDisabled
	case kms.KeyStatePendingDeletion:
		return KeyStatusPendingDeletion
	case kms.KeyStateDeleted:
		return KeyStatusDeleted
	default:
		return KeyStatusEnabled
	}
}

// kmsUsageToService converts kms.KeyUsage slice to service.KeyUsage slice.
func kmsUsageToService(usages []kms.KeyUsage) []KeyUsage {
	if len(usages) == 0 {
		return nil
	}
	result := make([]KeyUsage, len(usages))
	for i, u := range usages {
		result[i] = KeyUsage(u)
	}
	return result
}

// serviceUsageToKMS converts service.KeyUsage slice to kms.KeyUsage slice.
func serviceUsageToKMS(usages []KeyUsage) []kms.KeyUsage {
	if len(usages) == 0 {
		return nil
	}
	result := make([]kms.KeyUsage, len(usages))
	for i, u := range usages {
		result[i] = kms.KeyUsage(u)
	}
	return result
}

// extractKeyMetadata converts the interface{} returned by kmsManager.GetKey into
// a service-layer KeyMetadata. It handles *kms.KeyMetadata and *service.KeyMetadata
// types, falling back to defaults for unrecognized types. It fails only when a
// *kms.KeyMetadata carries a negative version.
// kmsKeyVersion converts a KMS manager key version to the service layer's
// unsigned version. A negative version means the stored metadata is corrupt,
// so it is reported as an internal error rather than wrapped to a huge value.
func kmsKeyVersion(op string, version int) (uint64, error) {
	v, err := conv.SafeIntToUint64(version)
	if err != nil {
		return 0, WrapError(err, errors.ErrInternal, op, "invalid key version in metadata")
	}
	return v, nil
}

func extractKeyMetadata(keyID string, keyData interface{}) (*KeyMetadata, error) {
	now := time.Now()

	// Try kms.KeyMetadata (returned by xkms backend / kms.KMSService cache)
	if kmsMeta, ok := keyData.(*kms.KeyMetadata); ok {
		version, err := kmsKeyVersion("kms.extractKeyMetadata", kmsMeta.Version)
		if err != nil {
			return nil, err
		}
		metadata := &KeyMetadata{
			KeyID:              keyID,
			Name:               kmsMeta.Name,
			Description:        kmsMeta.Description,
			Algorithm:          KeyAlgorithm(kmsMeta.Algorithm),
			Status:             kmsStateToServiceStatus(kmsMeta.State),
			Version:            version,
			Exportable:         kmsMeta.Exportable,
			DeletionProtection: kmsMeta.DeletionProtection,
			CreatedAt:          kmsMeta.Created,
			UpdatedAt:          kmsMeta.Updated,
			Owner:              kmsMeta.Owner,
		}
		if kmsMeta.Tags != nil {
			metadata.Tags = make(map[string]string, len(kmsMeta.Tags))
			for k, v := range kmsMeta.Tags {
				metadata.Tags[k] = v
			}
		}
		if len(kmsMeta.Usage) > 0 {
			metadata.Usage = make([]KeyUsage, len(kmsMeta.Usage))
			for i, u := range kmsMeta.Usage {
				metadata.Usage[i] = KeyUsage(u)
			}
		}
		if kmsMeta.LastUsedAt != nil {
			t := *kmsMeta.LastUsedAt
			metadata.LastUsedAt = &t
		}
		if kmsMeta.RotationPolicy != nil {
			metadata.RotationPolicy = kmsRotationToService(kmsMeta.RotationPolicy)
		}
		return metadata, nil
	}

	// Try service.KeyMetadata (returned when cached as service type)
	if svcMeta, ok := keyData.(*KeyMetadata); ok {
		metadata := &KeyMetadata{
			KeyID:              keyID,
			Name:               svcMeta.Name,
			Description:        svcMeta.Description,
			Algorithm:          svcMeta.Algorithm,
			Status:             svcMeta.Status,
			Version:            svcMeta.Version,
			Exportable:         svcMeta.Exportable,
			DeletionProtection: svcMeta.DeletionProtection,
			CreatedAt:          svcMeta.CreatedAt,
			UpdatedAt:          svcMeta.UpdatedAt,
			Owner:              svcMeta.Owner,
		}
		if svcMeta.Tags != nil {
			metadata.Tags = make(map[string]string, len(svcMeta.Tags))
			for k, v := range svcMeta.Tags {
				metadata.Tags[k] = v
			}
		}
		if len(svcMeta.Usage) > 0 {
			metadata.Usage = make([]KeyUsage, len(svcMeta.Usage))
			copy(metadata.Usage, svcMeta.Usage)
		}
		if svcMeta.LastUsedAt != nil {
			t := *svcMeta.LastUsedAt
			metadata.LastUsedAt = &t
		}
		if svcMeta.RotationPolicy != nil {
			rp := *svcMeta.RotationPolicy
			metadata.RotationPolicy = &rp
		}
		return metadata, nil
	}

	// Fallback: return minimal metadata with defaults
	return &KeyMetadata{
		KeyID:     keyID,
		Algorithm: KeyAlgAES256GCM,
		Status:    KeyStatusEnabled,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// getVersionStatus returns the status of a specific key version.
// Checks in order: local cache, KVStore (best-effort), scheduler storage (Raft-replicated).
// Returns empty string if no explicit status has been set.
func (k *kmsServiceImpl) getVersionStatus(ctx context.Context, keyID string, version uint64) (KeyStatus, *time.Time) {
	// Ensure we have a valid context with deadline for Raft operations
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	cacheKey := fmt.Sprintf("%s:%d", keyID, version)
	if info, ok := k.versionStatus.Load(cacheKey); ok {
		if vsi, ok := info.(*versionStatusInfo); ok {
			return vsi.Status, vsi.DisabledAt
		}
	}

	// Fall back to persistent KVStore (works when write was on the leader node)
	if k.kvStore != nil {
		data, err := k.kvStore.Get(ctx, versionStatusKey(keyID, version))
		if err == nil && len(data) > 0 {
			var ps persistedVersionStatus
			if err := json.Unmarshal(data, &ps); err == nil {
				status := KeyStatus(ps.Status)
				var disabledAt *time.Time
				if ps.DisabledAt != 0 {
					t := time.Unix(0, ps.DisabledAt)
					disabledAt = &t
				}
				// Populate local cache to avoid repeated lookups
				k.versionStatus.Store(cacheKey, &versionStatusInfo{
					Status:     status,
					DisabledAt: disabledAt,
				})
				return status, disabledAt
			}
		}
	}

	// Fall back to Raft-replicated scheduler storage: completed grace period jobs
	// are visible on all nodes via SyncRead, regardless of which node executed the job.
	if querier, ok := k.scheduler.(KMSSchedulerQuerier); ok {
		versions, err := querier.GetCompletedGracePeriodVersions(ctx, keyID)
		if err == nil {
			if completedAt, found := versions[version]; found {
				// Populate local cache
				k.versionStatus.Store(cacheKey, &versionStatusInfo{
					Status:     KeyStatusDisabled,
					DisabledAt: &completedAt,
				})
				return KeyStatusDisabled, &completedAt
			}
		}
	}

	return "", nil // No explicit status, use default
}

// versionStatusKey returns the KVStore key for a version status entry.
func versionStatusKey(keyID string, version uint64) string {
	return fmt.Sprintf("__sys:kms/version_status/%s/%d", keyID, version)
}

// persistedVersionStatus is the JSON-serializable form of versionStatusInfo.
type persistedVersionStatus struct {
	Status     string `json:"status"`
	DisabledAt int64  `json:"disabled_at"` // unix nanoseconds, 0 if nil
}

// setVersionStatus stores the status of a specific key version.
// Updates both the local cache and persistent KVStore (if available).
func (k *kmsServiceImpl) setVersionStatus(keyID string, version uint64, status KeyStatus, disabledAt *time.Time) {
	cacheKey := fmt.Sprintf("%s:%d", keyID, version)
	k.versionStatus.Store(cacheKey, &versionStatusInfo{
		Status:     status,
		DisabledAt: disabledAt,
	})

	// Persist to Raft-replicated KVStore for cross-node visibility
	if k.kvStore != nil {
		var ts int64
		if disabledAt != nil {
			ts = disabledAt.UnixNano()
		}
		data, err := json.Marshal(&persistedVersionStatus{
			Status:     string(status),
			DisabledAt: ts,
		})
		if err == nil {
			putCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if putErr := k.kvStore.Put(putCtx, versionStatusKey(keyID, version), data); putErr != nil {
				if k.logger != nil {
					k.logger.WithFields(map[string]interface{}{
						"key_id":  keyID,
						"version": version,
						"error":   putErr.Error(),
					}).Warn("Failed to persist version status to KVStore")
				}
			}
		}
	}
}

// disableKeyVersion disables a specific key version without requiring the key
// to exist in the KMS manager. This is used by the grace period handler which
// operates on version-specific status tracked in memory. In a multi-node cluster,
// the XKMS metadata cache may not have the key on the node executing the
// scheduler job, so this method does not require a successful GetKey call.
func (k *kmsServiceImpl) disableKeyVersion(ctx context.Context, keyID string, version uint64) (*UpdateKeyStatusResponse, error) {
	if keyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.disableKeyVersion", "key ID is required")
	}
	if version == 0 {
		return nil, NewError(errors.ErrInvalidInput, "kms.disableKeyVersion", "version must be positive")
	}

	// Get current version status
	oldStatus, _ := k.getVersionStatus(ctx, keyID, version)
	if oldStatus == "" {
		oldStatus = KeyStatusEnabled
	}

	if oldStatus == KeyStatusDisabled {
		return &UpdateKeyStatusResponse{
			KeyID:     keyID,
			Version:   version,
			OldStatus: oldStatus,
			NewStatus: KeyStatusDisabled,
			Status:    KeyStatusDisabled,
			Timestamp: time.Now(),
		}, nil
	}

	now := time.Now()
	k.setVersionStatus(keyID, version, KeyStatusDisabled, &now)

	if k.metrics != nil {
		k.metrics.statusUpdatesTotal.With(string(oldStatus), string(KeyStatusDisabled)).Inc()
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":     keyID,
			"version":    version,
			"old_status": oldStatus,
			"new_status": KeyStatusDisabled,
			"source":     "grace_period",
		}).Info("Disabled key version via grace period")
	}

	return &UpdateKeyStatusResponse{
		KeyID:     keyID,
		Version:   version,
		OldStatus: oldStatus,
		NewStatus: KeyStatusDisabled,
		Status:    KeyStatusDisabled,
		Timestamp: now,
	}, nil
}

// UpdateKeyStatus enables or disables a key or key version.
// This method validates the request, retrieves the current key status,
// calls the appropriate KMS manager method (EnableKey or DisableKey),
// and returns the status transition information.
func (k *kmsServiceImpl) UpdateKeyStatus(ctx context.Context, req *UpdateKeyStatusRequest) (*UpdateKeyStatusResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("update_key_status").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateUpdateKeyStatusRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "update_key_status").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Verify key exists
	keyData, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "update_key_status").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.UpdateKeyStatus", "key not found")
	}

	// Determine old status - for version-specific updates, use version status
	var oldStatus KeyStatus
	if req.Version > 0 {
		// Get version-specific status for grace period and similar operations
		versionStatus, _ := k.getVersionStatus(ctx, scopedKeyID, req.Version)
		if versionStatus != "" {
			oldStatus = versionStatus
		} else {
			// Version hasn't been explicitly set, default to enabled
			oldStatus = KeyStatusEnabled
		}
	} else {
		// Get whole key status
		oldStatus = KeyStatusEnabled // Default assumption
		if kmsMeta, ok := keyData.(*kms.KeyMetadata); ok {
			oldStatus = kmsStateToServiceStatus(kmsMeta.State)
		} else if km, ok := keyData.(*KeyMetadata); ok {
			oldStatus = km.Status
		}
	}

	// Check if status is already the same
	if oldStatus == req.Status {
		// No change needed, return current state
		return &UpdateKeyStatusResponse{
			KeyID:     req.KeyID,
			Version:   req.Version,
			OldStatus: oldStatus,
			NewStatus: req.Status,
			Status:    req.Status,
			Timestamp: time.Now(),
		}, nil
	}

	// For version-specific status updates (e.g., grace period handling),
	// only update the version tracking without affecting the whole key.
	// The whole-key status is independent of individual version statuses.
	if req.Version > 0 {
		now := time.Now()
		var disabledAt *time.Time
		if req.Status == KeyStatusDisabled {
			disabledAt = &now
		}
		k.setVersionStatus(scopedKeyID, req.Version, req.Status, disabledAt)
	} else {
		// Call appropriate KMS manager method for whole-key status updates
		// This is used when no specific version is targeted
		switch req.Status {
		case KeyStatusEnabled:
			err = k.kmsManager.EnableKey(ctx, scopedKeyID)
			if err != nil {
				if k.metrics != nil {
					k.metrics.errorsByType.With("kms_error", "update_key_status").Inc()
				}
				return nil, WrapError(err, errors.ErrInternal, "kms.UpdateKeyStatus", "failed to enable key")
			}
		case KeyStatusDisabled:
			err = k.kmsManager.DisableKey(ctx, scopedKeyID)
			if err != nil {
				if k.metrics != nil {
					k.metrics.errorsByType.With("kms_error", "update_key_status").Inc()
				}
				return nil, WrapError(err, errors.ErrInternal, "kms.UpdateKeyStatus", "failed to disable key")
			}
		}
	}

	// Update metrics
	if k.metrics != nil {
		k.metrics.statusUpdatesTotal.With(string(oldStatus), string(req.Status)).Inc()
	}

	// Log the status change
	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":     req.KeyID,
			"version":    req.Version,
			"old_status": oldStatus,
			"new_status": req.Status,
			"reason":     req.Reason,
		}).Info("Updated key status")
	}

	return &UpdateKeyStatusResponse{
		KeyID:     req.KeyID,
		Version:   req.Version,
		OldStatus: oldStatus,
		NewStatus: req.Status,
		Status:    req.Status,
		Timestamp: time.Now(),
	}, nil
}

// ListKeyVersions returns all versions of a key.
// This method retrieves key metadata and constructs version information
// for all available versions of the specified key.
func (k *kmsServiceImpl) ListKeyVersions(ctx context.Context, req *ListKeyVersionsRequest) (*ListKeyVersionsResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("list_key_versions").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if err := k.validateListKeyVersionsRequest(req); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "list_key_versions").Inc()
		}
		return nil, err
	}

	// Extract tenant context for multi-tenant isolation
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)

	// Verify key exists and get metadata
	keyData, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "list_key_versions").Inc()
		}
		return nil, WrapError(err, errors.ErrNotFound, "kms.ListKeyVersions", "key not found")
	}

	// Extract version information from key metadata
	var versions []*KeyVersionInfo
	currentVersion := uint64(1)
	algorithm := KeyAlgAES256GCM
	status := KeyStatusEnabled
	createdAt := time.Now()

	// Try to extract version info from kms.KeyMetadata (the type returned by kmsManager.GetKey)
	if kmsMeta, ok := keyData.(*kms.KeyMetadata); ok {
		currentVersion, err = kmsKeyVersion("kms.ListKeyVersions", kmsMeta.Version)
		if err != nil {
			return nil, err
		}
		algorithm = KeyAlgorithm(kmsMeta.Algorithm)
		status = kmsStateToServiceStatus(kmsMeta.State)
		createdAt = kmsMeta.Created
	} else if km, ok := keyData.(*KeyMetadata); ok {
		// Also handle service.KeyMetadata if returned
		currentVersion = km.Version
		algorithm = km.Algorithm
		status = km.Status
		createdAt = km.CreatedAt
	}

	// Build version list - for now we construct versions based on current version
	// In a full implementation, this would query a version store
	for v := uint64(1); v <= currentVersion; v++ {
		versionInfo := &KeyVersionInfo{
			KeyID:     req.KeyID,
			Version:   v,
			Algorithm: algorithm,
			CreatedAt: createdAt,
		}

		// Check if this version has been explicitly disabled via grace period or other mechanism
		// By default, all versions remain enabled (available for decryption) until explicitly disabled
		explicitStatus, disabledTime := k.getVersionStatus(ctx, scopedKeyID, v)
		switch {
		case explicitStatus != "":
			versionInfo.Status = explicitStatus
			if disabledTime != nil {
				versionInfo.DisabledAt = disabledTime
			}
		case v == currentVersion:
			// Current version uses the status from KMS backend
			versionInfo.Status = status
		default:
			// Older versions default to enabled during grace period
			versionInfo.Status = KeyStatusEnabled
		}

		versions = append(versions, versionInfo)
	}

	// Apply pagination if specified
	limit := req.Limit
	if limit <= 0 {
		limit = 100 // Default limit
	}
	if limit > len(versions) {
		limit = len(versions)
	}

	// For cursor-based pagination, we would parse the cursor and skip to that position
	// For now, we return all versions up to the limit
	paginatedVersions := versions
	hasMore := false
	nextCursor := ""

	if len(versions) > limit {
		paginatedVersions = versions[:limit]
		hasMore = true
		nextCursor = fmt.Sprintf("%d", versions[limit].Version)
	}

	return &ListKeyVersionsResponse{
		KeyID:      req.KeyID,
		Versions:   paginatedVersions,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Timestamp:  time.Now(),
	}, nil
}

// =============================================================================
// DEK (Data Encryption Key) Operations
// =============================================================================

// algorithmToKeySize returns the key size in bytes for a given algorithm.
func algorithmToKeySize(algo KeyAlgorithm) int {
	switch algo {
	case KeyAlgAES128GCM:
		return 16
	case KeyAlgAES192GCM:
		return 24
	case KeyAlgAES256GCM:
		return 32
	case KeyAlgChaCha20Poly1305, KeyAlgXChaCha20Poly1305:
		return 32
	default:
		return 32 // Default to 256-bit
	}
}

// CreateDEK creates a new data encryption key.
// The DEK is generated locally using crypto/rand and stored in the service's
// in-memory DEK store. For production use with external KMS backends that support
// envelope encryption, a KEK (Key Encryption Key) would be used to wrap the DEK.
func (k *kmsServiceImpl) CreateDEK(ctx context.Context, req *CreateDEKRequest) (*CreateDEKResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("create_dek").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.CreateDEK", "request is nil")
	}
	if req.Path == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.CreateDEK", "DEK path is required")
	}
	// Validate algorithm if provided
	if req.Algorithm != "" && !isValidDEKAlgorithm(req.Algorithm) {
		return nil, NewError(errors.ErrInvalidInput, "kms.CreateDEK", "unsupported algorithm: "+string(req.Algorithm))
	}

	// Normalize path
	normalizedPath := normalizeDEKPath(req.Path)

	// Scope the DEK path to the caller's tenant, the same way key IDs are
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := tenantScopedKeyID(tenantID, normalizedPath)

	// Check if DEK already exists
	if _, exists := k.dekStore.get(scopedPath); exists {
		return nil, NewError(errors.ErrAlreadyExists, "kms.CreateDEK", "DEK already exists at path: "+normalizedPath)
	}

	// Determine algorithm (default to AES-256-GCM)
	algorithm := req.Algorithm
	if algorithm == "" {
		algorithm = KeyAlgAES256GCM
	}

	// Refuse an algorithm this process is not allowed to use before any key
	// material exists
	if err := requireFIPSApprovedDEKAlgorithm("kms.CreateDEK", algorithm); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "create_dek").Inc()
		}
		return nil, err
	}

	// Determine key size based on algorithm
	keyBytes := algorithmToKeySize(algorithm)

	// Generate the DEK material directly using crypto/rand
	// For the service layer's in-memory DEK store, we generate keys locally.
	// In a production setup with external KMS supporting envelope encryption,
	// this would use GenerateDataKey with a KEK to wrap the DEK.
	keyMaterial := make([]byte, keyBytes)
	if _, err := rand.Read(keyMaterial); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("crypto_error", "create_dek").Inc()
		}
		return nil, WrapError(err, errors.ErrInternal, "kms.CreateDEK", "failed to generate random key material")
	}

	now := time.Now()

	// Build DEK metadata
	metadata := &DEKMetadata{
		Path:           normalizedPath,
		Algorithm:      algorithm,
		Version:        1,
		Status:         string(DEKStatusActive),
		IsDefault:      false,
		CreatedAt:      now,
		Description:    req.Description,
		MaxUsageCount:  req.MaxOperations,
		MaxBytesUsed:   req.MaxBytes,
		WarningPercent: req.WarningPercent,
	}

	// Store the DEK entry with the key material
	k.dekStore.set(scopedPath, &dekEntry{
		metadata:   metadata,
		wrappedKey: keyMaterial,
	})

	// Set up AEAD safety tracking if limits were provided
	if req.MaxOperations > 0 || req.MaxBytes > 0 {
		config := AEADSafetyConfig{
			MaxOperations:  req.MaxOperations,
			MaxBytes:       req.MaxBytes,
			WarningPercent: req.WarningPercent,
		}
		k.safetyTracker.SetDEKConfig(scopedPath, config)
	}

	// Update metrics
	if k.metrics != nil {
		k.metrics.deksCreatedTotal.Inc()
		k.metrics.activeDEKs.Inc()
	}

	// Log the operation
	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"dek_path":  normalizedPath,
			"algorithm": algorithm,
			"version":   1,
		}).Info("Created new data encryption key")
	}

	return &CreateDEKResponse{
		DEK:       metadata,
		Timestamp: now,
	}, nil
}

// GetDEK retrieves a data encryption key.
func (k *kmsServiceImpl) GetDEK(ctx context.Context, req *GetDEKRequest) (*GetDEKResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("get_dek").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.GetDEK", "request is nil")
	}
	if req.Path == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.GetDEK", "DEK path is required")
	}

	// Normalize path
	normalizedPath := normalizeDEKPath(req.Path)

	// Scope the DEK path to the caller's tenant, the same way key IDs are
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := tenantScopedKeyID(tenantID, normalizedPath)

	// Retrieve DEK from store
	entry, exists := k.dekStore.get(scopedPath)
	if !exists {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "get_dek").Inc()
		}
		return nil, NewError(errors.ErrNotFound, "kms.GetDEK", "DEK not found at path: "+normalizedPath)
	}

	// Check version if specified
	if req.Version > 0 && entry.metadata.Version != req.Version {
		return nil, NewError(errors.ErrNotFound, "kms.GetDEK", fmt.Sprintf("DEK version %d not found", req.Version))
	}

	// Get usage stats from safety tracker
	operationCount, bytesProcessed, _ := k.safetyTracker.GetDEKUsage(scopedPath)
	metadata := entry.metadata
	metadata.UsageCount = operationCount
	metadata.BytesUsed = bytesProcessed

	return &GetDEKResponse{
		DEK:       metadata,
		Timestamp: time.Now(),
	}, nil
}

// ListDEKs lists all data encryption keys.
func (k *kmsServiceImpl) ListDEKs(ctx context.Context, req *ListDEKsRequest) (*ListDEKsResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("list_deks").Observe(time.Since(start).Seconds())
		}()
	}

	// Get prefix (may be empty)
	prefix := ""
	if req != nil {
		prefix = normalizeDEKPath(req.Prefix)
	}

	// List only the caller's tenant's DEKs
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	tenantPrefix := tenantListPrefix(tenantID)
	totalCount := len(k.dekStore.list(tenantPrefix))
	deks := k.dekStore.list(tenantPrefix + prefix)

	// Apply pagination
	limit := 100
	if req != nil && req.Limit > 0 {
		limit = req.Limit
	}

	hasMore := false
	nextCursor := ""
	if len(deks) > limit {
		deks = deks[:limit]
		hasMore = true
		if len(deks) > 0 {
			nextCursor = deks[len(deks)-1].Path
		}
	}

	// Enrich with usage stats
	for _, dek := range deks {
		operationCount, bytesProcessed, _ := k.safetyTracker.GetDEKUsage(tenantScopedKeyID(tenantID, dek.Path))
		dek.UsageCount = operationCount
		dek.BytesUsed = bytesProcessed
	}

	return &ListDEKsResponse{
		DEKs:       deks,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		TotalCount: totalCount,
		Timestamp:  time.Now(),
	}, nil
}

// RotateDEK rotates a data encryption key.
// RotateDEK rotates a data encryption key by generating new key material.
func (k *kmsServiceImpl) RotateDEK(ctx context.Context, req *RotateDEKRequest) (*RotateDEKResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("rotate_dek").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.RotateDEK", "request is nil")
	}
	if req.Path == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.RotateDEK", "DEK path is required")
	}

	// Normalize path
	normalizedPath := normalizeDEKPath(req.Path)

	// Scope the DEK path to the caller's tenant, the same way key IDs are
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := tenantScopedKeyID(tenantID, normalizedPath)

	// Retrieve existing DEK
	entry, exists := k.dekStore.get(scopedPath)
	if !exists {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "rotate_dek").Inc()
		}
		return nil, NewError(errors.ErrNotFound, "kms.RotateDEK", "DEK not found at path: "+normalizedPath)
	}

	// Re-keying carries the existing algorithm forward, so a DEK this process
	// is not allowed to use cannot be rotated into continued service either
	if err := requireFIPSApprovedDEKAlgorithm("kms.RotateDEK", entry.metadata.Algorithm); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("invalid_input", "rotate_dek").Inc()
		}
		return nil, err
	}

	// Store old version
	oldVersion := entry.metadata.Version

	// Determine key size based on algorithm
	keyBytes := algorithmToKeySize(entry.metadata.Algorithm)

	// Generate new DEK material directly using crypto/rand
	newKeyMaterial := make([]byte, keyBytes)
	if _, err := rand.Read(newKeyMaterial); err != nil {
		if k.metrics != nil {
			k.metrics.errorsByType.With("crypto_error", "rotate_dek").Inc()
		}
		return nil, WrapError(err, errors.ErrInternal, "kms.RotateDEK", "failed to generate new key material")
	}

	now := time.Now()
	newVersion := oldVersion + 1

	// Update DEK metadata
	entry.metadata.Version = newVersion
	entry.metadata.RotatedAt = &now
	entry.wrappedKey = newKeyMaterial

	// Reset AEAD safety tracking for the new key version
	k.safetyTracker.ResetDEKUsage(scopedPath)

	// Update metrics
	if k.metrics != nil {
		k.metrics.deksRotatedTotal.Inc()
	}

	// Log the operation
	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"dek_path":    normalizedPath,
			"old_version": oldVersion,
			"new_version": newVersion,
		}).Info("Rotated data encryption key")
	}

	return &RotateDEKResponse{
		Path:       normalizedPath,
		OldVersion: oldVersion,
		NewVersion: newVersion,
		Timestamp:  now,
	}, nil
}

// DeleteDEK deletes a data encryption key.
func (k *kmsServiceImpl) DeleteDEK(ctx context.Context, req *DeleteDEKRequest) (*DeleteDEKResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("delete_dek").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.DeleteDEK", "request is nil")
	}
	if req.Path == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.DeleteDEK", "DEK path is required")
	}

	// Normalize path
	normalizedPath := normalizeDEKPath(req.Path)

	// Scope the DEK path to the caller's tenant, the same way key IDs are
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := tenantScopedKeyID(tenantID, normalizedPath)

	// Check if DEK exists
	entry, exists := k.dekStore.get(scopedPath)
	if !exists {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "delete_dek").Inc()
		}
		return nil, NewError(errors.ErrNotFound, "kms.DeleteDEK", "DEK not found at path: "+normalizedPath)
	}

	// Check if this is the default DEK
	if entry.metadata.IsDefault && !req.Force {
		return nil, NewError(errors.ErrInvalidInput, "kms.DeleteDEK", "cannot delete default DEK without force flag")
	}

	// Delete the DEK
	deleted := k.dekStore.delete(scopedPath)

	// Clear AEAD safety tracking
	k.safetyTracker.ClearDEKConfig(scopedPath)

	// Update metrics
	if k.metrics != nil && deleted {
		k.metrics.activeDEKs.Dec()
	}

	// Log the operation
	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"dek_path": normalizedPath,
			"force":    req.Force,
		}).Warn("Deleted data encryption key")
	}

	return &DeleteDEKResponse{
		Path:      normalizedPath,
		Deleted:   deleted,
		Timestamp: time.Now(),
	}, nil
}

// GetDEKStatus gets the status of a data encryption key.
func (k *kmsServiceImpl) GetDEKStatus(ctx context.Context, req *GetDEKStatusRequest) (*GetDEKStatusResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("get_dek_status").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.GetDEKStatus", "request is nil")
	}
	if req.Path == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.GetDEKStatus", "DEK path is required")
	}

	// Normalize path
	normalizedPath := normalizeDEKPath(req.Path)

	// Scope the DEK path to the caller's tenant, the same way key IDs are
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := tenantScopedKeyID(tenantID, normalizedPath)

	// Retrieve DEK from store
	entry, exists := k.dekStore.get(scopedPath)
	if !exists {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "get_dek_status").Inc()
		}
		return nil, NewError(errors.ErrNotFound, "kms.GetDEKStatus", "DEK not found at path: "+normalizedPath)
	}

	// Get usage stats from safety tracker
	operationCount, bytesProcessed, _ := k.safetyTracker.GetDEKUsage(scopedPath)
	metadata := entry.metadata
	metadata.UsageCount = operationCount
	metadata.BytesUsed = bytesProcessed

	// Get backend type
	if k.kmsManager != nil {
		metadata.BackendType = string(k.kmsManager.GetBackendType())
	}

	return &GetDEKStatusResponse{
		DEK:       metadata,
		Timestamp: time.Now(),
	}, nil
}

// SetDefaultDEK sets the default data encryption key.
func (k *kmsServiceImpl) SetDefaultDEK(ctx context.Context, req *SetDefaultDEKRequest) (*SetDefaultDEKResponse, error) {
	start := time.Now()
	if k.metrics != nil {
		defer func() {
			k.metrics.keyOperationDuration.With("set_default_dek").Observe(time.Since(start).Seconds())
		}()
	}

	// Validate request
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetDefaultDEK", "request is nil")
	}
	if req.Path == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetDefaultDEK", "DEK path is required")
	}

	// Normalize path
	normalizedPath := normalizeDEKPath(req.Path)

	// Scope the DEK path to the caller's tenant, the same way key IDs are
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := tenantScopedKeyID(tenantID, normalizedPath)

	// Verify DEK exists
	if _, exists := k.dekStore.get(scopedPath); !exists {
		if k.metrics != nil {
			k.metrics.errorsByType.With("not_found", "set_default_dek").Inc()
		}
		return nil, NewError(errors.ErrNotFound, "kms.SetDefaultDEK", "DEK not found at path: "+normalizedPath)
	}

	// Set as default
	previousDefault := tenantStripKeyID(tenantID, k.dekStore.setDefault(tenantListPrefix(tenantID), scopedPath))

	// Log the operation
	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"dek_path":         normalizedPath,
			"previous_default": previousDefault,
		}).Info("Set default data encryption key")
	}

	return &SetDefaultDEKResponse{
		Path:            normalizedPath,
		PreviousDefault: previousDefault,
		Timestamp:       time.Now(),
	}, nil
}

// SetDefaultKey marks the named key as the system default.
// The name must correspond to an existing key. The previous default (if any)
// is returned for audit purposes.
func (k *kmsServiceImpl) SetDefaultKey(ctx context.Context, req *SetDefaultKeyRequest) (*SetDefaultKeyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetDefaultKey", "request is nil")
	}
	if req.Name == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetDefaultKey", "key name is required")
	}

	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Verify the key exists within the caller's tenant.
	if _, ok := k.keyNames.Load(tenantScopedKeyID(tenantID, req.Name)); !ok {
		return nil, NewError(errors.ErrNotFound, "kms.SetDefaultKey", "key not found: "+req.Name)
	}

	// Swap in the tenant's new default, capturing its previous value.
	prev := ""
	if v, loaded := k.defaultKeyNames.Swap(tenantListPrefix(tenantID), req.Name); loaded {
		prev, _ = v.(string)
	}

	return &SetDefaultKeyResponse{
		Name:            req.Name,
		IsDefault:       true,
		PreviousDefault: prev,
		Timestamp:       time.Now(),
	}, nil
}

// SetDEKSafety sets AEAD safety limits for a DEK.
func (k *kmsServiceImpl) SetDEKSafety(ctx context.Context, req *SetDEKSafetyRequest) (*SetDEKSafetyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetDEKSafety", "request is nil")
	}
	if req.Path == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetDEKSafety", "DEK path is required")
	}

	// Verify the DEK exists
	normalizedPath := normalizeDEKPath(req.Path)

	// Scope the DEK path to the caller's tenant, the same way key IDs are
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := tenantScopedKeyID(tenantID, normalizedPath)
	_, err = k.GetDEK(ctx, &GetDEKRequest{Path: normalizedPath})
	if err != nil {
		return nil, NewError(errors.ErrNotFound, "kms.SetDEKSafety", "DEK not found: "+normalizedPath)
	}

	// Validate percentage values
	if req.WarningPercent < 0 || req.WarningPercent > 100 {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetDEKSafety", "warning percent must be between 0 and 100")
	}

	// Build the safety configuration
	config := AEADSafetyConfig{
		MaxOperations:  req.MaxOperations,
		MaxBytes:       req.MaxBytes,
		WarningPercent: req.WarningPercent,
	}

	// Apply defaults if not specified
	defaultConfig := DefaultAEADSafetyConfig()
	if config.MaxOperations == 0 {
		config.MaxOperations = defaultConfig.MaxOperations
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = defaultConfig.MaxBytes
	}
	if config.WarningPercent == 0 {
		config.WarningPercent = defaultConfig.WarningPercent
	}

	// Set the configuration in the safety tracker (path already normalized above)
	k.safetyTracker.SetDEKConfig(scopedPath, config)

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"dek_path":        normalizedPath,
			"max_operations":  config.MaxOperations,
			"max_bytes":       config.MaxBytes,
			"warning_percent": config.WarningPercent,
		}).Info("Set AEAD safety configuration for DEK")
	}

	return &SetDEKSafetyResponse{
		Path: normalizedPath,
		Config: &DEKSafetyConfig{
			MaxOperations:  config.MaxOperations,
			MaxBytes:       config.MaxBytes,
			WarningPercent: config.WarningPercent,
		},
		Timestamp: time.Now(),
	}, nil
}

// ClearDEKSafety clears AEAD safety limits for a DEK, reverting to server defaults.
func (k *kmsServiceImpl) ClearDEKSafety(ctx context.Context, req *ClearDEKSafetyRequest) (*ClearDEKSafetyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.ClearDEKSafety", "request is nil")
	}
	if req.Path == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.ClearDEKSafety", "DEK path is required")
	}

	// Verify the DEK exists
	normalizedPath := normalizeDEKPath(req.Path)

	// Scope the DEK path to the caller's tenant, the same way key IDs are
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedPath := tenantScopedKeyID(tenantID, normalizedPath)
	_, err = k.GetDEK(ctx, &GetDEKRequest{Path: normalizedPath})
	if err != nil {
		return nil, NewError(errors.ErrNotFound, "kms.ClearDEKSafety", "DEK not found: "+normalizedPath)
	}

	// Clear the per-DEK configuration (reverts to server defaults)
	k.safetyTracker.ClearDEKConfig(scopedPath)

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"dek_path": normalizedPath,
		}).Info("Cleared AEAD safety configuration for DEK")
	}

	return &ClearDEKSafetyResponse{
		Path:      normalizedPath,
		Cleared:   true,
		Timestamp: time.Now(),
	}, nil
}

// StartReencryption starts a background re-encryption process.
// This operation runs asynchronously and can be tracked using GetReencryptionProgress.
func (k *kmsServiceImpl) StartReencryption(ctx context.Context, req *StartReencryptionRequest) (*StartReencryptionResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.StartReencryption", "request is nil")
	}

	// Validate request parameters
	if req.FromVersion == 0 {
		return nil, NewError(errors.ErrInvalidInput, "kms.StartReencryption", "from_version is required")
	}
	if req.ToVersion == 0 {
		return nil, NewError(errors.ErrInvalidInput, "kms.StartReencryption", "to_version is required")
	}
	if req.FromVersion >= req.ToVersion {
		return nil, NewError(errors.ErrInvalidInput, "kms.StartReencryption", "to_version must be greater than from_version")
	}

	// The operation covers only the caller's tenant's keys.
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	// Check if re-encryption is already in progress
	k.reencryptionState.mu.Lock()
	if k.reencryptionState.inProgress {
		k.reencryptionState.mu.Unlock()
		return nil, NewError(errors.ErrConflict, "kms.StartReencryption", "re-encryption is already in progress")
	}

	// Generate operation ID
	operationID := fmt.Sprintf("reencrypt-%d", time.Now().UnixNano())
	startedAt := time.Now()

	// Count total keys to re-encrypt
	keys, err := k.kmsManager.ListKeys(ctx, tenantListPrefix(tenantID))
	if err != nil {
		k.reencryptionState.mu.Unlock()
		return nil, WrapError(err, errors.ErrInternal, "kms.StartReencryption", "failed to list keys for re-encryption")
	}
	totalKeys := uint64(len(keys))

	// Set default batch size and concurrency
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	// Estimate completion time based on total keys and batch size
	estimatedSeconds := float64(totalKeys) / float64(batchSize*concurrency)
	if estimatedSeconds < 60 {
		estimatedSeconds = 60 // Minimum 1 minute estimate
	}
	estimatedCompletion := startedAt.Add(time.Duration(estimatedSeconds) * time.Second)

	// Create cancellation context for the re-encryption operation
	reencryptCtx, cancelFunc := context.WithCancel(context.WithoutCancel(ctx)) // #nosec G118 -- cancelFunc stored in reencryptionState, called on cancellation

	// Initialize re-encryption state
	k.reencryptionState.tenantID = tenantID
	k.reencryptionState.operationID = operationID
	k.reencryptionState.inProgress = true
	k.reencryptionState.status = "running"
	k.reencryptionState.startedAt = startedAt
	k.reencryptionState.estimatedCompletion = estimatedCompletion
	k.reencryptionState.totalKeys = totalKeys
	k.reencryptionState.processedKeys.Store(0)
	k.reencryptionState.failedKeys.Store(0)
	k.reencryptionState.errorMessage = ""
	k.reencryptionState.cancelFunc = cancelFunc
	k.reencryptionState.mu.Unlock()

	// Start background re-encryption goroutine
	go k.runReencryption(reencryptCtx, req, keys)

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"operation_id":         operationID,
			"from_version":         req.FromVersion,
			"to_version":           req.ToVersion,
			"total_keys":           totalKeys,
			"batch_size":           batchSize,
			"concurrency":          concurrency,
			"estimated_completion": estimatedCompletion,
		}).Info("Started re-encryption operation")
	}

	return &StartReencryptionResponse{
		OperationID:         operationID,
		Started:             true,
		StartedAt:           startedAt,
		TotalEntities:       int64(len(keys)),
		EstimatedCompletion: estimatedCompletion,
	}, nil
}

// runReencryption performs the actual re-encryption work in a background goroutine.
func (k *kmsServiceImpl) runReencryption(ctx context.Context, req *StartReencryptionRequest, keys []interface{}) {
	defer func() {
		// Ensure we mark re-encryption as complete on exit
		k.reencryptionState.mu.Lock()
		if k.reencryptionState.status == "running" {
			if k.reencryptionState.failedKeys.Load() > 0 {
				k.reencryptionState.status = "completed_with_errors"
			} else {
				k.reencryptionState.status = "completed"
			}
		}
		k.reencryptionState.inProgress = false
		k.reencryptionState.mu.Unlock()
	}()

	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	throttleRate := req.ThrottleRate
	var throttleDelay time.Duration
	if throttleRate > 0 {
		throttleDelay = time.Second / time.Duration(throttleRate)
	}

	// Process keys in batches
	for i := 0; i < len(keys); i += batchSize {
		// Check for cancellation
		select {
		case <-ctx.Done():
			k.reencryptionState.mu.Lock()
			k.reencryptionState.status = "cancelled"
			k.reencryptionState.errorMessage = "re-encryption was cancelled"
			k.reencryptionState.mu.Unlock()
			if k.logger != nil {
				k.logger.Info("Re-encryption operation cancelled")
			}
			return
		default:
		}

		// Calculate batch end index
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}

		// Process batch
		for j := i; j < end; j++ {
			// Apply throttling if configured
			if throttleDelay > 0 {
				select {
				case <-time.After(throttleDelay):
				case <-ctx.Done():
					k.reencryptionState.mu.Lock()
					k.reencryptionState.status = "cancelled"
					k.reencryptionState.errorMessage = "re-encryption was cancelled"
					k.reencryptionState.mu.Unlock()
					return
				}
			}

			// Check for cancellation within batch
			select {
			case <-ctx.Done():
				k.reencryptionState.mu.Lock()
				k.reencryptionState.status = "cancelled"
				k.reencryptionState.errorMessage = "re-encryption was cancelled"
				k.reencryptionState.mu.Unlock()
				return
			default:
			}

			// Perform re-encryption for this key
			// In a real implementation, this would:
			// 1. Read data encrypted with old key version
			// 2. Decrypt with old key
			// 3. Re-encrypt with new key version
			// 4. Store the re-encrypted data
			k.reencryptionState.processedKeys.Add(1)
		}
	}

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"operation_id":   k.reencryptionState.operationID,
			"processed_keys": k.reencryptionState.processedKeys.Load(),
			"failed_keys":    k.reencryptionState.failedKeys.Load(),
		}).Info("Re-encryption operation completed")
	}
}

// GetReencryptionProgress returns the current progress of the re-encryption operation.
func (k *kmsServiceImpl) GetReencryptionProgress(ctx context.Context, req *GetReencryptionProgressRequest) (*GetReencryptionProgressResponse, error) {
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}

	k.reencryptionState.mu.RLock()
	defer k.reencryptionState.mu.RUnlock()

	if k.reencryptionState.hiddenFrom(tenantID) {
		return nil, NewError(errors.ErrNotFound, "kms.GetReencryptionProgress", "re-encryption operation not found")
	}

	processedKeys := k.reencryptionState.processedKeys.Load()
	failedKeys := k.reencryptionState.failedKeys.Load()
	totalKeys := k.reencryptionState.totalKeys

	// Calculate percentage complete
	var percentComplete float64
	if totalKeys > 0 {
		percentComplete = float64(processedKeys) / float64(totalKeys) * 100
	}

	// Build progress response
	progress := &ReencryptionProgress{
		Status:          k.reencryptionState.status,
		InProgress:      k.reencryptionState.inProgress,
		TotalKeys:       totalKeys,
		ProcessedKeys:   processedKeys,
		FailedKeys:      failedKeys,
		PercentComplete: percentComplete,
		ErrorMessage:    k.reencryptionState.errorMessage,
	}

	// Include timing information if operation has started
	if !k.reencryptionState.startedAt.IsZero() {
		startedAt := k.reencryptionState.startedAt
		progress.StartedAt = &startedAt
	}

	// Include estimated completion if still in progress
	if k.reencryptionState.inProgress && !k.reencryptionState.estimatedCompletion.IsZero() {
		estimatedCompletion := k.reencryptionState.estimatedCompletion
		progress.EstimatedCompletion = &estimatedCompletion
	}

	return &GetReencryptionProgressResponse{
		Progress:  progress,
		Timestamp: time.Now(),
	}, nil
}

// CancelReencryption cancels an in-progress re-encryption operation.
func (k *kmsServiceImpl) CancelReencryption(ctx context.Context) error {
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return err
	}

	k.reencryptionState.mu.Lock()
	defer k.reencryptionState.mu.Unlock()

	if k.reencryptionState.hiddenFrom(tenantID) {
		return NewError(errors.ErrNotFound, "kms.CancelReencryption", "re-encryption operation not found")
	}

	if !k.reencryptionState.inProgress {
		return NewError(errors.ErrInvalidInput, "kms.CancelReencryption", "no re-encryption operation in progress")
	}

	if k.reencryptionState.cancelFunc != nil {
		k.reencryptionState.cancelFunc()
	}

	return nil
}

// BackendStatus returns health and capability information about the KMS backend.
func (k *kmsServiceImpl) BackendStatus(ctx context.Context) (*BackendStatusResponse, error) {
	backendType := "software"
	status := "healthy"

	if k.kmsManager != nil {
		backendType = string(k.kmsManager.GetBackendType())
		if !k.kmsManager.IsAvailable(ctx) {
			status = "unavailable"
		}
	} else {
		status = "not_initialized"
	}

	// Determine capabilities based on backend type
	capabilities := map[string]bool{
		"symmetric":      true,
		"asymmetric":     true,
		"signing":        true,
		"wrapping":       true,
		"encryption":     true,
		"decryption":     true,
		"key_generation": true,
		"key_rotation":   true,
	}

	return &BackendStatusResponse{
		BackendType:  backendType,
		Status:       status,
		Capabilities: capabilities,
		Timestamp:    time.Now(),
	}, nil
}

// BackendTest performs a complete test cycle to verify backend functionality.
// This test performs the following steps:
//  1. Generate a test key with a known algorithm (AES-256-GCM)
//  2. Encrypt some test data
//  3. Decrypt and verify the result matches
//  4. Delete the test key
//  5. Return success/failure with timing
func (k *kmsServiceImpl) BackendTest(ctx context.Context) (*BackendTestResponse, error) {
	start := time.Now()

	// Check if KMS manager is available
	if k.kmsManager == nil {
		return &BackendTestResponse{
			Success:   false,
			Duration:  time.Since(start),
			Message:   "KMS manager not initialized",
			Timestamp: time.Now(),
		}, nil
	}

	// Test backend availability
	if !k.kmsManager.IsAvailable(ctx) {
		return &BackendTestResponse{
			Success:   false,
			Duration:  time.Since(start),
			Message:   "KMS backend is not available",
			Timestamp: time.Now(),
		}, nil
	}

	// Step 1: Generate a unique test key with AES-256-GCM
	testKeyID := fmt.Sprintf("test-backend-%d", time.Now().UnixNano())
	attrs := &kctypes.KeyAttributes{
		CN:                 testKeyID,
		SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		Exportable:         false,
	}

	_, err := k.kmsManager.GenerateKey(ctx, attrs)
	if err != nil {
		return &BackendTestResponse{
			Success:   false,
			Duration:  time.Since(start),
			Message:   fmt.Sprintf("Failed to generate test key: %v", err),
			Timestamp: time.Now(),
		}, nil
	}

	// Ensure cleanup happens regardless of test outcome
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if delErr := k.kmsManager.DeleteKey(cleanupCtx, testKeyID); delErr != nil {
			if k.logger != nil {
				k.logger.WithError(delErr).WithField("key_id", testKeyID).Warn("failed to clean up test key after backend test")
			}
		}
	}()

	// Step 2: Generate random test data
	testData := make([]byte, 32)
	if _, err := rand.Read(testData); err != nil {
		return &BackendTestResponse{
			Success:   false,
			Duration:  time.Since(start),
			Message:   fmt.Sprintf("Failed to generate random test data: %v", err),
			Timestamp: time.Now(),
		}, nil
	}

	// Use AAD (Additional Authenticated Data) for the test
	aad := []byte("backend-test-aad")

	// Step 3: Encrypt the test data
	ciphertext, err := k.kmsManager.Encrypt(ctx, testKeyID, testData, aad)
	if err != nil {
		return &BackendTestResponse{
			Success:   false,
			Duration:  time.Since(start),
			Message:   fmt.Sprintf("Failed to encrypt test data: %v", err),
			Timestamp: time.Now(),
		}, nil
	}

	// Verify ciphertext is different from plaintext
	if bytes.Equal(ciphertext, testData) {
		return &BackendTestResponse{
			Success:   false,
			Duration:  time.Since(start),
			Message:   "Encryption failed: ciphertext equals plaintext",
			Timestamp: time.Now(),
		}, nil
	}

	// Step 4: Decrypt the ciphertext
	decrypted, err := k.kmsManager.Decrypt(ctx, testKeyID, ciphertext, aad)
	if err != nil {
		return &BackendTestResponse{
			Success:   false,
			Duration:  time.Since(start),
			Message:   fmt.Sprintf("Failed to decrypt test data: %v", err),
			Timestamp: time.Now(),
		}, nil
	}

	// Step 5: Verify decrypted data matches original
	if !bytes.Equal(decrypted, testData) {
		return &BackendTestResponse{
			Success:   false,
			Duration:  time.Since(start),
			Message:   "Decryption verification failed: decrypted data does not match original",
			Timestamp: time.Now(),
		}, nil
	}

	// Test passed
	duration := time.Since(start)
	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"test_key_id": testKeyID,
			"duration_ms": duration.Milliseconds(),
		}).Info("Backend test completed successfully")
	}

	return &BackendTestResponse{
		Success:   true,
		Duration:  duration,
		Message:   "Backend test passed: key generation, encryption, and decryption verified successfully",
		Timestamp: time.Now(),
	}, nil
}
