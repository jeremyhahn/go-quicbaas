// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package service

import (
	"context"
	"crypto"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	schedulerstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// =============================================================================
// Test helpers
// =============================================================================

// rotationMockAuditLogger implements KMSAuditLogger + KMSAutoRotationAuditLogger.
type rotationMockAuditLogger struct {
	mu              sync.Mutex
	autoRotateCalls []rotationAuditCall

	// events, when non-nil, receives every auto-rotation audit call so
	// end-to-end tests can wait for a scan without polling.
	events chan rotationAuditCall
}

type rotationAuditCall struct {
	keyID     string
	algorithm string
	success   bool
	errMsg    string
}

func (m *rotationMockAuditLogger) LogKeyExport(_ context.Context, _, _, _, _, _, _, _ string, _ bool) error {
	return nil
}

func (m *rotationMockAuditLogger) LogKeyGenerate(_ context.Context, _, _, _, _, _, _ string, _ bool) error {
	return nil
}

func (m *rotationMockAuditLogger) LogKeyImport(_ context.Context, _, _, _, _, _, _ string, _ bool) error {
	return nil
}

func (m *rotationMockAuditLogger) LogKeyDeletion(_ context.Context, _, _, _, _ string, _ bool) error {
	return nil
}

func (m *rotationMockAuditLogger) LogGracePeriodDisable(_ context.Context, _ string, _ uint64, _ string, _ bool, _ string) error {
	return nil
}

func (m *rotationMockAuditLogger) LogKeyAutoRotation(_ context.Context, keyID, algorithm string, success bool, errMsg string) error {
	m.mu.Lock()
	call := rotationAuditCall{
		keyID:     keyID,
		algorithm: algorithm,
		success:   success,
		errMsg:    errMsg,
	}
	m.autoRotateCalls = append(m.autoRotateCalls, call)
	m.mu.Unlock()
	if m.events != nil {
		m.events <- call
	}
	return nil
}

func (m *rotationMockAuditLogger) autoRotateCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.autoRotateCalls)
}

// rotationKMSManager is a minimal KMSManager for rotation scheduler tests.
// It tracks RotateKey calls and UpdateStoredMetadata calls.
type rotationKMSManager struct {
	mu sync.Mutex

	keys         map[string]*kms.KeyMetadata
	rotations    []string // keyIDs that were rotated
	metadataUpds []*kms.KeyMetadata

	listErr   error
	rotateErr error
}

func newRotationKMSManager() *rotationKMSManager {
	return &rotationKMSManager{
		keys: make(map[string]*kms.KeyMetadata),
	}
}

func (m *rotationKMSManager) addKey(meta *kms.KeyMetadata) {
	m.mu.Lock()
	m.keys[meta.KeyID] = meta
	m.mu.Unlock()
}

// Close implements interfaces.KMSManager.
func (m *rotationKMSManager) Close() error { return nil }

// GenerateKey implements interfaces.KMSManager.
func (m *rotationKMSManager) GenerateKey(_ context.Context, _ *xkmssdk.KeyAttributes) (interface{}, error) {
	return nil, nil
}

// ImportKey implements interfaces.KMSManager.
func (m *rotationKMSManager) ImportKey(_ context.Context, _ string, _ []byte, _ string, _ bool) (interface{}, error) {
	return nil, nil
}

// GetKey implements interfaces.KMSManager.
func (m *rotationKMSManager) GetKey(_ context.Context, keyID string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if meta, ok := m.keys[keyID]; ok {
		return meta, nil
	}
	return nil, stderrors.New("key not found: " + keyID)
}

// ListKeys implements interfaces.KMSManager.
func (m *rotationKMSManager) ListKeys(_ context.Context, _ string) ([]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	result := make([]interface{}, 0, len(m.keys))
	for _, meta := range m.keys {
		// Deep-copy so the caller gets its own copy.
		cp := *meta
		if meta.RotationPolicy != nil {
			rp := *meta.RotationPolicy
			cp.RotationPolicy = &rp
		}
		result = append(result, &cp)
	}
	return result, nil
}

// RotateKey implements interfaces.KMSManager.
func (m *rotationKMSManager) RotateKey(_ context.Context, keyID string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rotateErr != nil {
		return nil, m.rotateErr
	}
	meta, ok := m.keys[keyID]
	if !ok {
		return nil, stderrors.New("key not found: " + keyID)
	}
	m.rotations = append(m.rotations, keyID)
	meta.Version++
	return &kms.KeyMetadata{
		KeyID:     meta.KeyID,
		Algorithm: meta.Algorithm,
		Version:   meta.Version,
		State:     kms.KeyStateEnabled,
	}, nil
}

// RevokeKey implements interfaces.KMSManager.
func (m *rotationKMSManager) RevokeKey(_ context.Context, _ string) error { return nil }

// DeleteKey implements interfaces.KMSManager.
func (m *rotationKMSManager) DeleteKey(_ context.Context, _ string) error { return nil }

// EnableKey implements interfaces.KMSManager.
func (m *rotationKMSManager) EnableKey(_ context.Context, _ string) error { return nil }

// DisableKey implements interfaces.KMSManager.
func (m *rotationKMSManager) DisableKey(_ context.Context, _ string) error { return nil }

// Encrypt implements interfaces.KMSManager.
func (m *rotationKMSManager) Encrypt(_ context.Context, _ string, _ []byte, _ []byte) ([]byte, error) {
	return nil, nil
}

// Decrypt implements interfaces.KMSManager.
func (m *rotationKMSManager) Decrypt(_ context.Context, _ string, _ []byte, _ []byte) ([]byte, error) {
	return nil, nil
}

// Sign implements interfaces.KMSManager.
func (m *rotationKMSManager) Sign(_ context.Context, _ string, _ []byte, _ string) ([]byte, error) {
	return nil, nil
}

// Verify implements interfaces.KMSManager.
func (m *rotationKMSManager) Verify(_ context.Context, _ string, _ []byte, _ []byte, _ string) (bool, error) {
	return true, nil
}

// GenerateDataKey implements interfaces.KMSManager.
func (m *rotationKMSManager) GenerateDataKey(_ context.Context, _ *xkmssdk.KeyAttributes, _ int) ([]byte, []byte, error) {
	return nil, nil, nil
}

// GetPublicKey implements interfaces.KMSManager.
func (m *rotationKMSManager) GetPublicKey(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

// GetBackendType implements interfaces.KMSManager.
func (m *rotationKMSManager) GetBackendType() config.BackendType { return "software" }

// IsAvailable implements interfaces.KMSManager.
func (m *rotationKMSManager) IsAvailable(_ context.Context) bool { return true }

// GetSupportedAlgorithms implements interfaces.KMSManager.
func (m *rotationKMSManager) GetSupportedAlgorithms(_ context.Context) ([]string, error) {
	return []string{"AES-256-GCM"}, nil
}

// Signer implements interfaces.KMSManager.
func (m *rotationKMSManager) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, stderrors.New("not implemented")
}

// UpdateStoredMetadata implements interfaces.KMSManager.
func (m *rotationKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if meta, ok := metadata.(*kms.KeyMetadata); ok {
		m.metadataUpds = append(m.metadataUpds, meta)
		// Persist back so subsequent ListKeys see the new NextRotation.
		if existing, ok2 := m.keys[meta.KeyID]; ok2 && meta.RotationPolicy != nil {
			existing.RotationPolicy.NextRotation = meta.RotationPolicy.NextRotation
		}
	}
	return nil
}

func (m *rotationKMSManager) rotationCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rotations)
}

// lastMetadataUpdate returns a snapshot, not the stored pointer. The scheduler
// keeps mutating the metadata it handed in -- UpdateStoredMetadata writes
// RotationPolicy.NextRotation through the same object -- so returning the
// pointer and releasing the lock let a caller read a field that was being
// written from the scheduler goroutine. The race detector reports it against
// waitForCond, which polls exactly that field.
func (m *rotationKMSManager) lastMetadataUpdate() *kms.KeyMetadata {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.metadataUpds) == 0 {
		return nil
	}
	last := m.metadataUpds[len(m.metadataUpds)-1]
	if last == nil {
		return nil
	}
	snapshot := *last
	if last.RotationPolicy != nil {
		policy := *last.RotationPolicy
		snapshot.RotationPolicy = &policy
	}
	return &snapshot
}

// buildRotationSchedulerKMS returns a kmsServiceImpl wired to mgr.
func buildRotationSchedulerKMS(mgr interfaces.KMSManager) *kmsServiceImpl {
	return newKMSService(mgr, nil)
}

// dueKey returns metadata for an auto-rotating key whose cryptoperiod
// expired an hour ago.
func dueKey(keyID string, interval time.Duration) *kms.KeyMetadata {
	return &kms.KeyMetadata{
		KeyID:     keyID,
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     interval,
			NextRotation: time.Now().Add(-time.Hour),
		},
	}
}

// awaitAudit waits for one auto-rotation audit event.
func awaitAudit(t *testing.T, events <-chan rotationAuditCall) rotationAuditCall {
	t.Helper()
	select {
	case call := <-events:
		return call
	case <-time.After(10 * time.Second):
		t.Fatal("no auto-rotation happened")
		return rotationAuditCall{}
	}
}

// =============================================================================
// RotateDueKeys — one scan
// =============================================================================

func TestRotateDueKeys_RotatesExpiredKey(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(dueKey("key-expired", 24*time.Hour))

	result, err := buildRotationSchedulerKMS(mgr).RotateDueKeys(context.Background())
	require.NoError(t, err)

	assert.Equal(t, &schedulerhandlers.KMSRotationResult{Due: 1, Rotated: 1}, result)
	assert.Equal(t, 1, mgr.rotationCount())
}

func TestRotateDueKeys_SkipsKeysThatAreNotDue(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	tests := []struct {
		name   string
		policy *kms.RotationPolicy
	}{
		{"cryptoperiod not expired", &kms.RotationPolicy{Enabled: true, AutoRotate: true, Interval: time.Hour, NextRotation: future}},
		{"auto-rotate off", &kms.RotationPolicy{Enabled: true, AutoRotate: false, Interval: time.Hour, NextRotation: past}},
		{"policy disabled", &kms.RotationPolicy{Enabled: false, AutoRotate: true, Interval: time.Hour, NextRotation: past}},
		{"no deadline", &kms.RotationPolicy{Enabled: true, AutoRotate: true, Interval: time.Hour}},
		{"no policy", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mgr := newRotationKMSManager()
			mgr.addKey(&kms.KeyMetadata{KeyID: "key", Algorithm: "AES-256-GCM", Version: 1, RotationPolicy: tc.policy})

			result, err := buildRotationSchedulerKMS(mgr).RotateDueKeys(context.Background())
			require.NoError(t, err)

			assert.Equal(t, &schedulerhandlers.KMSRotationResult{}, result)
			assert.Zero(t, mgr.rotationCount())
		})
	}
}

func TestRotateDueKeys_AdvancesNextRotationPastNow(t *testing.T) {
	interval := 24 * time.Hour
	key := dueKey("key-advance", interval)
	expiredAt := key.RotationPolicy.NextRotation
	mgr := newRotationKMSManager()
	mgr.addKey(key)

	_, err := buildRotationSchedulerKMS(mgr).RotateDueKeys(context.Background())
	require.NoError(t, err)

	upd := mgr.lastMetadataUpdate()
	require.NotNil(t, upd)
	require.NotNil(t, upd.RotationPolicy)
	assert.True(t, upd.RotationPolicy.NextRotation.After(time.Now()),
		"NextRotation must move into the future")
	assert.Equal(t, expiredAt.Add(interval), upd.RotationPolicy.NextRotation,
		"NextRotation advances from the old deadline, not from now")
}

func TestRotateDueKeys_AuditsSuccess(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(dueKey("key-audit", time.Hour))
	audit := &rotationMockAuditLogger{}
	k := buildRotationSchedulerKMS(mgr)
	k.auditLogger = audit

	_, err := k.RotateDueKeys(context.Background())
	require.NoError(t, err)

	require.Len(t, audit.autoRotateCalls, 1)
	assert.Equal(t, rotationAuditCall{keyID: "key-audit", algorithm: "AES-256-GCM", success: true}, audit.autoRotateCalls[0])
}

// TestRotateDueKeys_RotationFailureIsCountedAndAudited pins that one key's
// failure neither aborts the scan nor advances the key's deadline: it stays
// due for the next scan.
func TestRotateDueKeys_RotationFailureIsCountedAndAudited(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(dueKey("key-fail", time.Hour))
	mgr.rotateErr = stderrors.New("rotation backend unavailable")
	audit := &rotationMockAuditLogger{}
	k := buildRotationSchedulerKMS(mgr)
	k.auditLogger = audit

	result, err := k.RotateDueKeys(context.Background())
	require.NoError(t, err)

	assert.Equal(t, &schedulerhandlers.KMSRotationResult{Due: 1, Failed: 1}, result)
	require.Len(t, audit.autoRotateCalls, 1)
	assert.False(t, audit.autoRotateCalls[0].success)
	assert.NotEmpty(t, audit.autoRotateCalls[0].errMsg)
	assert.Nil(t, mgr.lastMetadataUpdate(), "a failed rotation must leave the key due")
}

func TestRotateDueKeys_ListFailureIsTypedError(t *testing.T) {
	cause := stderrors.New("storage unavailable")
	mgr := newRotationKMSManager()
	mgr.listErr = cause

	result, err := buildRotationSchedulerKMS(mgr).RotateDueKeys(context.Background())

	assert.Nil(t, result)
	var scanErr *KMSRotationScanError
	require.ErrorAs(t, err, &scanErr)
	assert.ErrorIs(t, err, cause)
	assert.Equal(t, "kms rotation: list keys: storage unavailable", err.Error())
}

func TestRotateDueKeys_UsesSystemContext(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(dueKey("key-sysctx", time.Hour))
	var captured context.Context
	capMgr := &rotateCapturingManager{
		rotationKMSManager: mgr,
		onRotate:           func(ctx context.Context) { captured = ctx },
	}

	_, err := buildRotationSchedulerKMS(capMgr).RotateDueKeys(context.Background())
	require.NoError(t, err)

	require.NotNil(t, captured, "RotateKey was not called")
	assert.True(t, auth.IsSystemContext(captured), "RotateKey must run under a system context")
}

// TestRotateDueKeys_BaseAuditLoggerStillRotates covers an audit logger without
// the auto-rotation extension: the event goes to the structured logger and the
// key is still rotated.
func TestRotateDueKeys_BaseAuditLoggerStillRotates(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(dueKey("key-fallback", time.Hour))
	k := buildRotationSchedulerKMS(mgr)
	k.auditLogger = &gracePeriodMockAuditLogger{}

	result, err := k.RotateDueKeys(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, result.Rotated)
	assert.Equal(t, 1, mgr.rotationCount())
}

// rotateCapturingManager wraps rotationKMSManager and calls onRotate on each
// RotateKey invocation so tests can capture the context passed to it.
type rotateCapturingManager struct {
	*rotationKMSManager
	onRotate func(ctx context.Context)
}

// RotateKey overrides rotationKMSManager.RotateKey to capture the context.
func (m *rotateCapturingManager) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.onRotate != nil {
		m.onRotate(ctx)
	}
	return m.rotationKMSManager.RotateKey(ctx, keyID)
}

// TestKMSAutoRotationAuditLogger_InterfaceSatisfied verifies the mock satisfies
// both KMSAuditLogger and KMSAutoRotationAuditLogger.
func TestKMSAutoRotationAuditLogger_InterfaceSatisfied(t *testing.T) {
	var _ KMSAuditLogger = (*rotationMockAuditLogger)(nil)
	var _ KMSAutoRotationAuditLogger = (*rotationMockAuditLogger)(nil)
}

// =============================================================================
// The rotation system job on the scheduler
// =============================================================================

// rotationSchedulerConfig ticks fast so a one-second scan interval is observed
// promptly.
func rotationSchedulerConfig() *scheduler.SchedulerConfig {
	cfg := scheduler.DefaultConfig()
	cfg.TickInterval = 10 * time.Millisecond
	return cfg
}

func TestRegisterKMSRotation_RejectsSubSecondInterval(t *testing.T) {
	engine := scheduler.New(rotationSchedulerConfig(), schedulerstorage.NewStorage(kvstore.NewMemory()))

	err := registerKMSRotation(engine, buildRotationSchedulerKMS(newRotationKMSManager()), 500*time.Millisecond)

	var invalid scheduler.InvalidJobError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "schedule", invalid.Field)
	assert.Zero(t, engine.HandlerCount(), "nothing is registered for an invalid interval")
}

func TestRegisterKMSRotation_RejectsSecondRegistration(t *testing.T) {
	engine := scheduler.New(rotationSchedulerConfig(), schedulerstorage.NewStorage(kvstore.NewMemory()))
	k := buildRotationSchedulerKMS(newRotationKMSManager())
	require.NoError(t, registerKMSRotation(engine, k, time.Second))

	err := registerKMSRotation(engine, k, time.Second)

	require.Error(t, err, "the rotation job is declared once per engine")
}

// TestKMSRotation_StandaloneEngineRotates is the standalone path end to end: a
// single-node scheduler installs the rotation system job and its scan rotates
// the due key.
func TestKMSRotation_StandaloneEngineRotates(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(dueKey("key-standalone", time.Hour))
	audit := &rotationMockAuditLogger{events: make(chan rotationAuditCall, 8)}
	k := buildRotationSchedulerKMS(mgr)
	k.auditLogger = audit

	store := schedulerstorage.NewStorage(kvstore.NewMemory())
	engine := scheduler.New(rotationSchedulerConfig(), store)
	require.NoError(t, registerKMSRotation(engine, k, time.Second))
	require.NoError(t, engine.Start(context.Background()))

	call := awaitAudit(t, audit.events)
	require.NoError(t, engine.Stop(context.Background()))

	assert.Equal(t, "key-standalone", call.keyID)
	assert.True(t, call.success)
	assert.Equal(t, 1, mgr.rotationCount())
	job, err := store.GetJob(context.Background(), schedulerhandlers.KMSRotationJobID, "")
	require.NoError(t, err)
	assert.Equal(t, scheduler.OwnerTypeSystem, job.OwnerType)
	assert.Equal(t, schedulerhandlers.HandlerKMSRotation, job.Handler)
}

// TestKMSRotation_ClusterRotatesOnce is the defect this job exists to fix. The
// old rotation loop ran on every node with no leader check or claim, so a
// cluster rotated each due key once per node. Here two clustered nodes share
// one replicated job store and one key store and BOTH believe they lead — the
// leader-churn window, the worst case — and the key must rotate exactly once.
//
// The firing claim is decided by a real qrdb transaction plane, per the
// consensus-arbitration carve-out: a double that agrees with the
// implementation proves nothing about who won.
func TestKMSRotation_ClusterRotatesOnce(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(dueKey("key-cluster", time.Hour))
	audit := &rotationMockAuditLogger{events: make(chan rotationAuditCall, 8)}

	n := qrdbnode.Shared(t)
	store, err := schedulerstorage.NewClusteredStorage(kvstore.NewMemory(), n.Transaction, n.KV)
	require.NoError(t, err)

	nodes := make([]*scheduler.SchedulerEngine, 0, 2)
	for _, nodeID := range []string{"node-A", "node-B"} {
		engine, err := scheduler.NewWithClustering(rotationSchedulerConfig(), store, nodeID, stubLeader{})
		require.NoError(t, err)
		k := buildRotationSchedulerKMS(mgr)
		k.auditLogger = audit
		require.NoError(t, registerKMSRotation(engine, k, time.Second))
		nodes = append(nodes, engine)
	}
	for _, engine := range nodes {
		require.NoError(t, engine.Start(context.Background()))
	}

	call := awaitAudit(t, audit.events)
	assert.Equal(t, "key-cluster", call.keyID)

	// Let the winning run record its outcome, then stop both nodes; Stop
	// waits for every in-flight execution.
	require.Eventually(t, func() bool {
		job, err := store.GetJob(context.Background(), schedulerhandlers.KMSRotationJobID, "")
		return err == nil && job.RunCount >= 1 && job.Status == scheduler.JobStatusPending
	}, 10*time.Second, 10*time.Millisecond)
	for _, engine := range nodes {
		require.NoError(t, engine.Stop(context.Background()))
	}

	assert.Equal(t, 1, mgr.rotationCount(), "a cluster must rotate a due key exactly once")
	assert.Equal(t, 1, audit.autoRotateCount())
	job, err := store.GetJob(context.Background(), schedulerhandlers.KMSRotationJobID, "")
	require.NoError(t, err)
	assert.Equal(t, job.RunCount, nodes[0].FiringClaimsWon()+nodes[1].FiringClaimsWon(),
		"every scan that ran was won by exactly one node")
}
