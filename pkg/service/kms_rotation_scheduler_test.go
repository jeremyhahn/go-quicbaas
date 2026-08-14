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
	"sync/atomic"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// =============================================================================
// Test helpers
// =============================================================================

// rotationMockAuditLogger implements KMSAuditLogger + KMSAutoRotationAuditLogger.
type rotationMockAuditLogger struct {
	mu              sync.Mutex
	autoRotateCalls []rotationAuditCall
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
	m.autoRotateCalls = append(m.autoRotateCalls, rotationAuditCall{
		keyID:     keyID,
		algorithm: algorithm,
		success:   success,
		errMsg:    errMsg,
	})
	m.mu.Unlock()
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
func (m *rotationKMSManager) ImportKey(_ context.Context, _ string, _ []byte, _ string) (interface{}, error) {
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
func buildRotationSchedulerKMS(mgr *rotationKMSManager) *kmsServiceImpl {
	return newKMSService(mgr, nil)
}

// waitFor polls cond up to timeout, returning true when cond is satisfied.
func waitForCond(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// =============================================================================
// Unit tests for KMSRotationScheduler
// =============================================================================

// TestKMSRotationScheduler_AutoRotate_WhenExpired verifies that a key with
// AutoRotate=true and a past NextRotation is rotated by the scheduler.
func TestKMSRotationScheduler_AutoRotate_WhenExpired(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:     "key-expired",
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     24 * time.Hour,
			NextRotation: time.Now().Add(-1 * time.Hour), // already expired
		},
	})

	kmsImpl := buildRotationSchedulerKMS(mgr)
	sched := newKMSRotationScheduler(kmsImpl, nil, 10*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	if !waitForCond(2*time.Second, func() bool { return mgr.rotationCount() >= 1 }) {
		t.Fatal("expected key-expired to be rotated, but RotateKey was not called")
	}
}

// TestKMSRotationScheduler_NoRotate_WhenNotExpired verifies that a key whose
// NextRotation is in the future is NOT rotated.
func TestKMSRotationScheduler_NoRotate_WhenNotExpired(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:     "key-future",
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     24 * time.Hour,
			NextRotation: time.Now().Add(1 * time.Hour), // not yet expired
		},
	})

	kmsImpl := buildRotationSchedulerKMS(mgr)
	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	// Give the scheduler several ticks and confirm no rotation happened.
	time.Sleep(80 * time.Millisecond)
	if mgr.rotationCount() != 0 {
		t.Fatalf("expected 0 rotations for future key, got %d", mgr.rotationCount())
	}
}

// TestKMSRotationScheduler_NoRotate_WhenAutoRotateFalse verifies that a key
// with AutoRotate=false is not rotated even if NextRotation is in the past.
func TestKMSRotationScheduler_NoRotate_WhenAutoRotateFalse(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:     "key-no-auto",
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   false, // explicitly disabled
			Interval:     24 * time.Hour,
			NextRotation: time.Now().Add(-1 * time.Hour),
		},
	})

	kmsImpl := buildRotationSchedulerKMS(mgr)
	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	time.Sleep(80 * time.Millisecond)
	if mgr.rotationCount() != 0 {
		t.Fatalf("expected 0 rotations for AutoRotate=false key, got %d", mgr.rotationCount())
	}
}

// TestKMSRotationScheduler_NoRotate_NilPolicy verifies that keys without a
// RotationPolicy are never rotated.
func TestKMSRotationScheduler_NoRotate_NilPolicy(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:          "key-no-policy",
		Algorithm:      "AES-256-GCM",
		Version:        1,
		State:          kms.KeyStateEnabled,
		RotationPolicy: nil,
	})

	kmsImpl := buildRotationSchedulerKMS(mgr)
	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	time.Sleep(80 * time.Millisecond)
	if mgr.rotationCount() != 0 {
		t.Fatalf("expected 0 rotations for nil policy key, got %d", mgr.rotationCount())
	}
}

// TestKMSRotationScheduler_NextRotation_Advances verifies that after a
// successful rotation the scheduler persists an advanced NextRotation via
// UpdateStoredMetadata.
func TestKMSRotationScheduler_NextRotation_Advances(t *testing.T) {
	interval := 24 * time.Hour
	expiredAt := time.Now().Add(-1 * time.Hour)

	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:     "key-advance",
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     interval,
			NextRotation: expiredAt,
		},
	})

	kmsImpl := buildRotationSchedulerKMS(mgr)
	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	// Wait for both the rotation and the metadata update.
	if !waitForCond(2*time.Second, func() bool {
		return mgr.rotationCount() >= 1 && mgr.lastMetadataUpdate() != nil
	}) {
		t.Fatal("timed out waiting for rotation + metadata update")
	}

	upd := mgr.lastMetadataUpdate()
	if upd == nil {
		t.Fatal("expected metadata update, got nil")
	}
	if upd.RotationPolicy == nil {
		t.Fatal("expected updated RotationPolicy, got nil")
	}
	if !upd.RotationPolicy.NextRotation.After(time.Now()) {
		t.Fatalf("expected NextRotation to be in the future, got %v", upd.RotationPolicy.NextRotation)
	}
	// The new deadline should be approximately expiredAt + N*interval where N>=1.
	diff := upd.RotationPolicy.NextRotation.Sub(expiredAt)
	if diff < interval {
		t.Fatalf("NextRotation advanced by less than one interval: diff=%v", diff)
	}
}

// TestKMSRotationScheduler_AuditLogged_OnSuccess verifies the audit logger
// receives a LogKeyAutoRotation call on successful rotation.
func TestKMSRotationScheduler_AuditLogged_OnSuccess(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:     "key-audit",
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     time.Hour,
			NextRotation: time.Now().Add(-10 * time.Minute),
		},
	})

	auditLogger := &rotationMockAuditLogger{}
	kmsImpl := buildRotationSchedulerKMS(mgr)
	kmsImpl.auditLogger = auditLogger

	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	if !waitForCond(2*time.Second, func() bool { return auditLogger.autoRotateCount() >= 1 }) {
		t.Fatal("expected at least one auto-rotation audit log event")
	}

	auditLogger.mu.Lock()
	calls := make([]rotationAuditCall, len(auditLogger.autoRotateCalls))
	copy(calls, auditLogger.autoRotateCalls)
	auditLogger.mu.Unlock()

	if len(calls) == 0 {
		t.Fatal("no auto-rotation audit calls recorded")
	}
	if calls[0].keyID != "key-audit" {
		t.Errorf("audit call keyID: want key-audit, got %s", calls[0].keyID)
	}
	if !calls[0].success {
		t.Errorf("audit call success: want true, got false (err: %s)", calls[0].errMsg)
	}
}

// TestKMSRotationScheduler_AuditLogged_OnFailure verifies the audit logger
// receives a failure event when RotateKey returns an error.
func TestKMSRotationScheduler_AuditLogged_OnFailure(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:     "key-fail-audit",
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     time.Hour,
			NextRotation: time.Now().Add(-10 * time.Minute),
		},
	})
	mgr.rotateErr = stderrors.New("rotation backend unavailable")

	auditLogger := &rotationMockAuditLogger{}
	kmsImpl := buildRotationSchedulerKMS(mgr)
	kmsImpl.auditLogger = auditLogger

	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	if !waitForCond(2*time.Second, func() bool { return auditLogger.autoRotateCount() >= 1 }) {
		t.Fatal("expected at least one auto-rotation audit log event on failure")
	}

	auditLogger.mu.Lock()
	calls := make([]rotationAuditCall, len(auditLogger.autoRotateCalls))
	copy(calls, auditLogger.autoRotateCalls)
	auditLogger.mu.Unlock()

	if calls[0].success {
		t.Error("audit call success: want false, got true")
	}
	if calls[0].errMsg == "" {
		t.Error("expected non-empty errMsg on failure")
	}
}

// TestKMSRotationScheduler_CleanStop verifies the scheduler exits cleanly when
// Stop is called, with no goroutine leak (Stop must return, not hang).
func TestKMSRotationScheduler_CleanStop(t *testing.T) {
	mgr := newRotationKMSManager()
	kmsImpl := buildRotationSchedulerKMS(mgr)

	sched := newKMSRotationScheduler(kmsImpl, nil, 50*time.Millisecond)
	sched.Start(context.Background())

	done := make(chan struct{})
	go func() {
		sched.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK — Stop returned promptly.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2 seconds — goroutine leak suspected")
	}
}

// TestKMSRotationScheduler_IdempotentStart verifies that calling Start twice
// does not spawn duplicate goroutines (WaitGroup count stays 1).
func TestKMSRotationScheduler_IdempotentStart(t *testing.T) {
	mgr := newRotationKMSManager()
	kmsImpl := buildRotationSchedulerKMS(mgr)
	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)

	// Call Start twice — the second must be a no-op.
	sched.Start(context.Background())
	sched.Start(context.Background())

	done := make(chan struct{})
	go func() {
		sched.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Stop returned cleanly, confirming only one goroutine was spawned.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return — double-start may have leaked a goroutine")
	}
}

// TestKMSRotationScheduler_UsesSystemContext verifies that the context passed
// into RotateKey carries the system context marker set by auth.NewSystemContext.
func TestKMSRotationScheduler_UsesSystemContext(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:     "key-sysctx",
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     time.Hour,
			NextRotation: time.Now().Add(-5 * time.Minute),
		},
	})

	// Build a capturing manager that records the context on RotateKey.
	var capturedCtx context.Context
	var capturedMu sync.Mutex
	var ctxCaptured atomic.Bool

	capMgr := &rotateCapturingManager{
		rotationKMSManager: mgr,
		onRotate: func(ctx context.Context) {
			capturedMu.Lock()
			capturedCtx = ctx
			capturedMu.Unlock()
			ctxCaptured.Store(true)
		},
	}

	kmsImpl := newKMSService(capMgr, nil)

	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	if !waitForCond(2*time.Second, func() bool { return ctxCaptured.Load() }) {
		t.Fatal("RotateKey was not called")
	}

	capturedMu.Lock()
	ctx := capturedCtx
	capturedMu.Unlock()

	if !auth.IsSystemContext(ctx) {
		t.Error("RotateKey was not called under a system context")
	}
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

// =============================================================================
// Unit tests for newKMSRotationScheduler
// =============================================================================

// TestNewKMSRotationScheduler_DefaultInterval verifies the zero interval is
// replaced by defaultRotationScanInterval.
func TestNewKMSRotationScheduler_DefaultInterval(t *testing.T) {
	mgr := newRotationKMSManager()
	kmsImpl := buildRotationSchedulerKMS(mgr)

	sched := newKMSRotationScheduler(kmsImpl, nil, 0)
	if sched.interval != defaultRotationScanInterval {
		t.Errorf("interval: want %v, got %v", defaultRotationScanInterval, sched.interval)
	}
}

// TestNewKMSRotationScheduler_CustomInterval verifies a non-zero interval is
// preserved.
func TestNewKMSRotationScheduler_CustomInterval(t *testing.T) {
	mgr := newRotationKMSManager()
	kmsImpl := buildRotationSchedulerKMS(mgr)

	custom := 7 * time.Minute
	sched := newKMSRotationScheduler(kmsImpl, nil, custom)
	if sched.interval != custom {
		t.Errorf("interval: want %v, got %v", custom, sched.interval)
	}
}

// =============================================================================
// Compile-time interface checks
// =============================================================================

// TestKMSAutoRotationAuditLogger_InterfaceSatisfied verifies the mock satisfies
// both KMSAuditLogger and KMSAutoRotationAuditLogger.
func TestKMSAutoRotationAuditLogger_InterfaceSatisfied(t *testing.T) {
	var _ KMSAuditLogger = (*rotationMockAuditLogger)(nil)
	var _ KMSAutoRotationAuditLogger = (*rotationMockAuditLogger)(nil)
}

// TestKMSRotationScheduler_FallbackLogger_NoAutoRotation verifies the scheduler
// does not panic when auditLogger implements only KMSAuditLogger (not the
// extension interface) and falls back to the structured logger path.
func TestKMSRotationScheduler_FallbackLogger_NoAutoRotation(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(&kms.KeyMetadata{
		KeyID:     "key-fallback",
		Algorithm: "AES-256-GCM",
		Version:   1,
		State:     kms.KeyStateEnabled,
		RotationPolicy: &kms.RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     time.Hour,
			NextRotation: time.Now().Add(-10 * time.Minute),
		},
	})

	// Use a logger that only satisfies KMSAuditLogger, NOT KMSAutoRotationAuditLogger.
	baseAudit := &gracePeriodMockAuditLogger{}
	kmsImpl := buildRotationSchedulerKMS(mgr)
	kmsImpl.auditLogger = baseAudit

	sched := newKMSRotationScheduler(kmsImpl, nil, 20*time.Millisecond)
	sched.Start(context.Background())
	defer sched.Stop()

	// Wait until rotation occurs (no panic/deadlock is the actual assertion here).
	if !waitForCond(2*time.Second, func() bool { return mgr.rotationCount() >= 1 }) {
		t.Fatal("expected rotation with fallback logger, but none occurred")
	}
}
