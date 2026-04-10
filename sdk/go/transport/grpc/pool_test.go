// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package grpc

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestDefaultPoolConfig(t *testing.T) {
	cfg := DefaultPoolConfig()

	if cfg.MinSize != 5 {
		t.Errorf("expected MinSize=5, got %d", cfg.MinSize)
	}
	if cfg.MaxSize != 50 {
		t.Errorf("expected MaxSize=50, got %d", cfg.MaxSize)
	}
	if cfg.MaxAge != time.Hour {
		t.Errorf("expected MaxAge=1h, got %v", cfg.MaxAge)
	}
	if cfg.HealthInterval != 30*time.Second {
		t.Errorf("expected HealthInterval=30s, got %v", cfg.HealthInterval)
	}
}

func TestNewPool_NilConfig(t *testing.T) {
	// With nil config, should use defaults - but will fail due to no connection
	_, err := NewPool("localhost:50051", nil, nil)
	if err == nil {
		t.Fatal("expected error when unable to connect")
	}
}

func TestNewPool_InvalidMinSize(t *testing.T) {
	cfg := &PoolConfig{
		MinSize: 0, // Invalid - must be positive
		MaxSize: 10,
	}

	_, err := NewPool("localhost:50051", nil, cfg)
	if err == nil {
		t.Fatal("expected error for MinSize=0")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestNewPool_InvalidMaxSize(t *testing.T) {
	cfg := &PoolConfig{
		MinSize: 10,
		MaxSize: 5, // Invalid - must be >= MinSize
	}

	_, err := NewPool("localhost:50051", nil, cfg)
	if err == nil {
		t.Fatal("expected error for MaxSize < MinSize")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestPool_GetClosed(t *testing.T) {
	p := &Pool{
		cfg: &PoolConfig{MinSize: 1, MaxSize: 10},
	}
	p.closed.Store(true)

	_, err := p.Get()
	if err == nil {
		t.Fatal("expected error for closed pool")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Errorf("expected Connection error, got %v", errors.Code(err))
	}
}

func TestPool_PutClosed(t *testing.T) {
	p := &Pool{
		cfg: &PoolConfig{MinSize: 1, MaxSize: 10},
	}
	p.closed.Store(true)

	// Should not panic
	p.Put(nil)
}

func TestPool_HealthyClosed(t *testing.T) {
	p := &Pool{}
	p.closed.Store(true)

	if p.Healthy() {
		t.Error("expected unhealthy for closed pool")
	}
}

func TestPool_HealthyNoConnections(t *testing.T) {
	p := &Pool{
		cfg:         &PoolConfig{MinSize: 1, MaxSize: 10},
		connections: []*pooledConn{},
	}

	if p.Healthy() {
		t.Error("expected unhealthy with no connections")
	}
}

func TestPool_Size(t *testing.T) {
	p := &Pool{
		connections: make([]*pooledConn, 0),
	}

	if p.Size() != 0 {
		t.Errorf("expected size=0, got %d", p.Size())
	}

	// Add some mock connections (nil conn is ok for size test)
	p.connections = append(p.connections, &pooledConn{})
	p.connections = append(p.connections, &pooledConn{})

	if p.Size() != 2 {
		t.Errorf("expected size=2, got %d", p.Size())
	}
}

func TestPool_CloseAlreadyClosed(t *testing.T) {
	p := &Pool{
		healthStop: make(chan struct{}),
	}
	p.closed.Store(true)

	err := p.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPool_IsExpired(t *testing.T) {
	p := &Pool{
		cfg: &PoolConfig{
			MaxAge: time.Hour,
		},
	}

	// Not expired
	pc := &pooledConn{
		createdAt: time.Now(),
	}
	if p.isExpired(pc) {
		t.Error("expected not expired for new connection")
	}

	// Expired
	pc.createdAt = time.Now().Add(-2 * time.Hour)
	if !p.isExpired(pc) {
		t.Error("expected expired for old connection")
	}

	// MaxAge <= 0 means never expires
	p.cfg.MaxAge = 0
	if p.isExpired(pc) {
		t.Error("expected not expired when MaxAge=0")
	}
}

func TestPool_RemoveConnection(t *testing.T) {
	pc1 := &pooledConn{}
	pc2 := &pooledConn{}
	pc3 := &pooledConn{}

	p := &Pool{
		connections: []*pooledConn{pc1, pc2, pc3},
	}

	// Remove middle element
	p.removeConnection(pc2)

	if len(p.connections) != 2 {
		t.Errorf("expected 2 connections, got %d", len(p.connections))
	}

	// Verify pc2 is removed
	for _, pc := range p.connections {
		if pc == pc2 {
			t.Error("expected pc2 to be removed")
		}
	}
}

func TestPool_RemoveConnectionNotFound(t *testing.T) {
	pc1 := &pooledConn{}
	pc2 := &pooledConn{}
	pcNotInPool := &pooledConn{}

	p := &Pool{
		connections: []*pooledConn{pc1, pc2},
	}

	// Remove element not in pool - should not panic
	p.removeConnection(pcNotInPool)

	if len(p.connections) != 2 {
		t.Errorf("expected 2 connections, got %d", len(p.connections))
	}
}

func TestPool_PerformHealthCheck_Closed(t *testing.T) {
	p := &Pool{}
	p.closed.Store(true)

	// Should return early without panicking
	p.performHealthCheck()
}

func TestPooledConn_InUse(t *testing.T) {
	pc := &pooledConn{}

	if pc.inUse.Load() {
		t.Error("expected inUse=false initially")
	}

	pc.inUse.Store(true)
	if !pc.inUse.Load() {
		t.Error("expected inUse=true after Store")
	}
}
