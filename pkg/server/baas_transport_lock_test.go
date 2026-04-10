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

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-qrdb/pkg/lock"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// Test Helpers
// =============================================================================

// testLockServiceError is a typed error returned by the mock lock service.
type testLockServiceError struct {
	Op string
}

func (e *testLockServiceError) Error() string {
	return "test lock service error: " + e.Op
}

// mockLockService implements BaasLockService for testing.
type mockLockService struct {
	lockFn    func(ctx context.Context, key, holderID string, ttl time.Duration, reentrant bool) (*lock.Lock, error)
	tryLockFn func(ctx context.Context, key, holderID string, ttl time.Duration, reentrant bool) (*lock.Lock, error)
	unlockFn  func(ctx context.Context, key string, fencingToken int64) error
	extendFn  func(ctx context.Context, key string, fencingToken int64) (*lock.Lock, error)
	statusFn  func(ctx context.Context, key string) (*lock.Lock, error)
	listFn    func(ctx context.Context, prefix string) ([]*lock.Lock, error)
}

func (m *mockLockService) Lock(ctx context.Context, key, holderID string, ttl time.Duration, reentrant bool) (*lock.Lock, error) {
	return m.lockFn(ctx, key, holderID, ttl, reentrant)
}

func (m *mockLockService) TryLock(ctx context.Context, key, holderID string, ttl time.Duration, reentrant bool) (*lock.Lock, error) {
	return m.tryLockFn(ctx, key, holderID, ttl, reentrant)
}

func (m *mockLockService) Unlock(ctx context.Context, key string, fencingToken int64) error {
	return m.unlockFn(ctx, key, fencingToken)
}

func (m *mockLockService) Extend(ctx context.Context, key string, fencingToken int64) (*lock.Lock, error) {
	return m.extendFn(ctx, key, fencingToken)
}

func (m *mockLockService) Status(ctx context.Context, key string) (*lock.Lock, error) {
	return m.statusFn(ctx, key)
}

func (m *mockLockService) List(ctx context.Context, prefix string) ([]*lock.Lock, error) {
	return m.listFn(ctx, prefix)
}

// newTestLock creates a lock.Lock with all fields populated for testing.
func newTestLock() *lock.Lock {
	return &lock.Lock{
		Key:          "/test/key",
		FencingToken: 42,
		LeaseID:      100,
		HolderID:     "agent-1",
		Reentrant:    true,
		AcquireCount: 1,
		TTL:          30,
		AcquiredAt:   1700000000,
	}
}

// assertLockInfo verifies that a transport.LockInfo matches the expected test lock.
func assertLockInfo(t *testing.T, info *transport.LockInfo) {
	t.Helper()
	if info.Key != "/test/key" {
		t.Errorf("expected Key=/test/key, got %s", info.Key)
	}
	if info.FencingToken != 42 {
		t.Errorf("expected FencingToken=42, got %d", info.FencingToken)
	}
	if info.LeaseID != 100 {
		t.Errorf("expected LeaseID=100, got %d", info.LeaseID)
	}
	if info.HolderID != "agent-1" {
		t.Errorf("expected HolderID=agent-1, got %s", info.HolderID)
	}
	if !info.Reentrant {
		t.Error("expected Reentrant=true")
	}
	if info.AcquireCount != 1 {
		t.Errorf("expected AcquireCount=1, got %d", info.AcquireCount)
	}
	if info.TTL != 30 {
		t.Errorf("expected TTL=30, got %d", info.TTL)
	}
	if info.AcquiredAt != 1700000000 {
		t.Errorf("expected AcquiredAt=1700000000, got %d", info.AcquiredAt)
	}
}

// =============================================================================
// LockAcquire
// =============================================================================

func TestBaasTransport_LockAcquire_Success(t *testing.T) {
	svc := &mockLockService{
		lockFn: func(_ context.Context, key, holderID string, ttl time.Duration, reentrant bool) (*lock.Lock, error) {
			if key != "/test/key" {
				t.Errorf("expected key=/test/key, got %s", key)
			}
			if holderID != "agent-1" {
				t.Errorf("expected holderID=agent-1, got %s", holderID)
			}
			if ttl != 30*time.Second {
				t.Errorf("expected ttl=30s, got %v", ttl)
			}
			if !reentrant {
				t.Error("expected reentrant=true")
			}
			return newTestLock(), nil
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	info, err := bt.LockAcquire(context.Background(), &transport.LockAcquireRequest{
		Key:       "/test/key",
		HolderID:  "agent-1",
		TTL:       30,
		Reentrant: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLockInfo(t, info)
}

func TestBaasTransport_LockAcquire_NilService(t *testing.T) {
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, nil)
	_, err := bt.LockAcquire(context.Background(), &transport.LockAcquireRequest{Key: "/test"})
	if err == nil {
		t.Fatal("expected error for nil lock service")
	}
	var notSupported *BaasTransportNotSupportedError
	if !errors.As(err, &notSupported) {
		t.Fatalf("expected BaasTransportNotSupportedError, got %T: %v", err, err)
	}
	if notSupported.Operation != "LockAcquire" {
		t.Errorf("expected operation=LockAcquire, got %s", notSupported.Operation)
	}
}

func TestBaasTransport_LockAcquire_ServiceError(t *testing.T) {
	svc := &mockLockService{
		lockFn: func(_ context.Context, _, _ string, _ time.Duration, _ bool) (*lock.Lock, error) {
			return nil, &testLockServiceError{Op: "Lock"}
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	_, err := bt.LockAcquire(context.Background(), &transport.LockAcquireRequest{Key: "/test"})
	if err == nil {
		t.Fatal("expected error from service")
	}
	var svcErr *testLockServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected testLockServiceError, got %T", err)
	}
}

// =============================================================================
// LockTryLock
// =============================================================================

func TestBaasTransport_LockTryLock_Success(t *testing.T) {
	svc := &mockLockService{
		tryLockFn: func(_ context.Context, key, holderID string, ttl time.Duration, reentrant bool) (*lock.Lock, error) {
			if key != "/test/key" {
				t.Errorf("expected key=/test/key, got %s", key)
			}
			if holderID != "agent-2" {
				t.Errorf("expected holderID=agent-2, got %s", holderID)
			}
			if ttl != 60*time.Second {
				t.Errorf("expected ttl=60s, got %v", ttl)
			}
			if reentrant {
				t.Error("expected reentrant=false")
			}
			return newTestLock(), nil
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	info, err := bt.LockTryLock(context.Background(), &transport.LockAcquireRequest{
		Key:       "/test/key",
		HolderID:  "agent-2",
		TTL:       60,
		Reentrant: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLockInfo(t, info)
}

func TestBaasTransport_LockTryLock_NilService(t *testing.T) {
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, nil)
	_, err := bt.LockTryLock(context.Background(), &transport.LockAcquireRequest{Key: "/test"})
	if err == nil {
		t.Fatal("expected error for nil lock service")
	}
	var notSupported *BaasTransportNotSupportedError
	if !errors.As(err, &notSupported) {
		t.Fatalf("expected BaasTransportNotSupportedError, got %T", err)
	}
}

func TestBaasTransport_LockTryLock_ServiceError(t *testing.T) {
	svc := &mockLockService{
		tryLockFn: func(_ context.Context, _, _ string, _ time.Duration, _ bool) (*lock.Lock, error) {
			return nil, &testLockServiceError{Op: "TryLock"}
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	_, err := bt.LockTryLock(context.Background(), &transport.LockAcquireRequest{Key: "/test"})
	if err == nil {
		t.Fatal("expected error from service")
	}
	var svcErr *testLockServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected testLockServiceError, got %T", err)
	}
}

// =============================================================================
// LockRelease
// =============================================================================

func TestBaasTransport_LockRelease_Success(t *testing.T) {
	svc := &mockLockService{
		unlockFn: func(_ context.Context, key string, fencingToken int64) error {
			if key != "/test/key" {
				t.Errorf("expected key=/test/key, got %s", key)
			}
			if fencingToken != 42 {
				t.Errorf("expected fencingToken=42, got %d", fencingToken)
			}
			return nil
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	err := bt.LockRelease(context.Background(), &transport.LockReleaseRequest{
		Key:          "/test/key",
		FencingToken: 42,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBaasTransport_LockRelease_NilService(t *testing.T) {
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, nil)
	err := bt.LockRelease(context.Background(), &transport.LockReleaseRequest{Key: "/test"})
	if err == nil {
		t.Fatal("expected error for nil lock service")
	}
	var notSupported *BaasTransportNotSupportedError
	if !errors.As(err, &notSupported) {
		t.Fatalf("expected BaasTransportNotSupportedError, got %T", err)
	}
}

func TestBaasTransport_LockRelease_ServiceError(t *testing.T) {
	svc := &mockLockService{
		unlockFn: func(_ context.Context, _ string, _ int64) error {
			return &testLockServiceError{Op: "Unlock"}
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	err := bt.LockRelease(context.Background(), &transport.LockReleaseRequest{Key: "/test"})
	if err == nil {
		t.Fatal("expected error from service")
	}
	var svcErr *testLockServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected testLockServiceError, got %T", err)
	}
}

// =============================================================================
// LockExtend
// =============================================================================

func TestBaasTransport_LockExtend_Success(t *testing.T) {
	svc := &mockLockService{
		extendFn: func(_ context.Context, key string, fencingToken int64) (*lock.Lock, error) {
			if key != "/test/key" {
				t.Errorf("expected key=/test/key, got %s", key)
			}
			if fencingToken != 42 {
				t.Errorf("expected fencingToken=42, got %d", fencingToken)
			}
			return newTestLock(), nil
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	info, err := bt.LockExtend(context.Background(), &transport.LockExtendRequest{
		Key:          "/test/key",
		FencingToken: 42,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLockInfo(t, info)
}

func TestBaasTransport_LockExtend_NilService(t *testing.T) {
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, nil)
	_, err := bt.LockExtend(context.Background(), &transport.LockExtendRequest{Key: "/test"})
	if err == nil {
		t.Fatal("expected error for nil lock service")
	}
	var notSupported *BaasTransportNotSupportedError
	if !errors.As(err, &notSupported) {
		t.Fatalf("expected BaasTransportNotSupportedError, got %T", err)
	}
}

func TestBaasTransport_LockExtend_ServiceError(t *testing.T) {
	svc := &mockLockService{
		extendFn: func(_ context.Context, _ string, _ int64) (*lock.Lock, error) {
			return nil, &testLockServiceError{Op: "Extend"}
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	_, err := bt.LockExtend(context.Background(), &transport.LockExtendRequest{Key: "/test"})
	if err == nil {
		t.Fatal("expected error from service")
	}
	var svcErr *testLockServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected testLockServiceError, got %T", err)
	}
}

// =============================================================================
// LockStatus
// =============================================================================

func TestBaasTransport_LockStatus_Success(t *testing.T) {
	svc := &mockLockService{
		statusFn: func(_ context.Context, key string) (*lock.Lock, error) {
			if key != "/test/key" {
				t.Errorf("expected key=/test/key, got %s", key)
			}
			return newTestLock(), nil
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	info, err := bt.LockStatus(context.Background(), "/test/key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLockInfo(t, info)
}

func TestBaasTransport_LockStatus_NilLock(t *testing.T) {
	svc := &mockLockService{
		statusFn: func(_ context.Context, _ string) (*lock.Lock, error) {
			return nil, nil
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	info, err := bt.LockStatus(context.Background(), "/nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil info for unlocked key, got %+v", info)
	}
}

func TestBaasTransport_LockStatus_NilService(t *testing.T) {
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, nil)
	_, err := bt.LockStatus(context.Background(), "/test")
	if err == nil {
		t.Fatal("expected error for nil lock service")
	}
	var notSupported *BaasTransportNotSupportedError
	if !errors.As(err, &notSupported) {
		t.Fatalf("expected BaasTransportNotSupportedError, got %T", err)
	}
}

func TestBaasTransport_LockStatus_ServiceError(t *testing.T) {
	svc := &mockLockService{
		statusFn: func(_ context.Context, _ string) (*lock.Lock, error) {
			return nil, &testLockServiceError{Op: "Status"}
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	_, err := bt.LockStatus(context.Background(), "/test")
	if err == nil {
		t.Fatal("expected error from service")
	}
	var svcErr *testLockServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected testLockServiceError, got %T", err)
	}
}

// =============================================================================
// LockList
// =============================================================================

func TestBaasTransport_LockList_Success(t *testing.T) {
	svc := &mockLockService{
		listFn: func(_ context.Context, prefix string) ([]*lock.Lock, error) {
			if prefix != "/test/" {
				t.Errorf("expected prefix=/test/, got %s", prefix)
			}
			return []*lock.Lock{newTestLock()}, nil
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	resp, err := bt.LockList(context.Background(), "/test/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Locks) != 1 {
		t.Fatalf("expected 1 lock, got %d", len(resp.Locks))
	}
	assertLockInfo(t, resp.Locks[0])
}

func TestBaasTransport_LockList_Empty(t *testing.T) {
	svc := &mockLockService{
		listFn: func(_ context.Context, _ string) ([]*lock.Lock, error) {
			return []*lock.Lock{}, nil
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	resp, err := bt.LockList(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Locks) != 0 {
		t.Errorf("expected 0 locks, got %d", len(resp.Locks))
	}
}

func TestBaasTransport_LockList_NilService(t *testing.T) {
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, nil)
	_, err := bt.LockList(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for nil lock service")
	}
	var notSupported *BaasTransportNotSupportedError
	if !errors.As(err, &notSupported) {
		t.Fatalf("expected BaasTransportNotSupportedError, got %T", err)
	}
}

func TestBaasTransport_LockList_ServiceError(t *testing.T) {
	svc := &mockLockService{
		listFn: func(_ context.Context, _ string) ([]*lock.Lock, error) {
			return nil, &testLockServiceError{Op: "List"}
		},
	}
	bt := newBaasTransport(NewMemoryKVStore(), nil, nil, nil, svc)
	_, err := bt.LockList(context.Background(), "")
	if err == nil {
		t.Fatal("expected error from service")
	}
	var svcErr *testLockServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("expected testLockServiceError, got %T", err)
	}
}

// =============================================================================
// mapLockToTransport
// =============================================================================

func TestMapLockToTransport_AllFields(t *testing.T) {
	l := newTestLock()
	info := mapLockToTransport(l)
	assertLockInfo(t, info)
}

func TestMapLockToTransport_ZeroValues(t *testing.T) {
	l := &lock.Lock{}
	info := mapLockToTransport(l)
	if info.Key != "" {
		t.Errorf("expected empty Key, got %s", info.Key)
	}
	if info.FencingToken != 0 {
		t.Errorf("expected FencingToken=0, got %d", info.FencingToken)
	}
	if info.LeaseID != 0 {
		t.Errorf("expected LeaseID=0, got %d", info.LeaseID)
	}
	if info.HolderID != "" {
		t.Errorf("expected empty HolderID, got %s", info.HolderID)
	}
	if info.Reentrant {
		t.Error("expected Reentrant=false")
	}
	if info.AcquireCount != 0 {
		t.Errorf("expected AcquireCount=0, got %d", info.AcquireCount)
	}
	if info.TTL != 0 {
		t.Errorf("expected TTL=0, got %d", info.TTL)
	}
	if info.AcquiredAt != 0 {
		t.Errorf("expected AcquiredAt=0, got %d", info.AcquiredAt)
	}
}
