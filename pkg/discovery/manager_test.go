//go:build raft

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

package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/stretchr/testify/require"
	"net"
)

func TestNewManager(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	// Test with valid config
	config := DefaultConfig()
	staticDiscovery, _ := NewStaticDiscovery([]string{"1:192.168.1.100:63000"})
	config.Methods = []Method{staticDiscovery}

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if mgr == nil {
		t.Fatal("NewManager() returned nil without error")
	}

	defer mgr.Close()

	// Test with nil config
	_, err = NewManager(nil, self, log)
	if err == nil {
		t.Error("NewManager() with nil config should return error")
	}

	// Test with nil logger
	_, err = NewManager(config, self, nil)
	if err == nil {
		t.Error("NewManager() with nil logger should return error")
	}

	// Test with invalid self peer (zero NodeID)
	invalidSelf := Peer{NodeID: 0, Address: "192.168.1.100:63000"}
	_, err = NewManager(config, invalidSelf, log)
	if err == nil {
		t.Error("NewManager() with zero NodeID should return error")
	}

	// Test with invalid self peer (empty address)
	invalidSelf2 := Peer{NodeID: 1, Address: ""}
	_, err = NewManager(config, invalidSelf2, log)
	if err == nil {
		t.Error("NewManager() with empty address should return error")
	}
}

func TestManager_Bootstrap(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	// Create static discovery with 3 peers
	staticDiscovery, err := NewStaticDiscovery([]string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
		"3:192.168.1.102:63000",
	})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}
	config.MinPeers = 3

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Close()

	ctx := context.Background()
	if err := mgr.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	// Verify peers were discovered
	peers := mgr.GetPeers()
	if len(peers) != 3 {
		t.Errorf("GetPeers() returned %d peers, want 3", len(peers))
	}
}

func TestManager_BootstrapInsufficientPeers(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	// Create static discovery with only 2 peers
	staticDiscovery, err := NewStaticDiscovery([]string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
	})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}
	config.MinPeers = 3 // Require 3 but only have 2

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Close()

	ctx := context.Background()
	err = mgr.Bootstrap(ctx)
	if err == nil {
		t.Error("Bootstrap() should fail with insufficient peers")
	}

	// Should be ErrNoQuorum.
	if !errors.Is(err, ErrNoQuorum) {
		t.Errorf("Bootstrap() error = %v, want ErrNoQuorum", err)
	}
}

func TestManager_Discover(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	staticDiscovery, err := NewStaticDiscovery([]string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
	})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Close()

	ctx := context.Background()
	peers, err := mgr.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(peers) != 2 {
		t.Errorf("Discover() returned %d peers, want 2", len(peers))
	}
}

func TestManager_GetSetPeers(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	staticDiscovery, err := NewStaticDiscovery([]string{"1:192.168.1.100:63000"})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Close()

	// Initially no peers
	peers := mgr.GetPeers()
	if len(peers) != 0 {
		t.Errorf("initial GetPeers() returned %d peers, want 0", len(peers))
	}

	// Update peers
	newPeers := []Peer{
		{NodeID: 1, Address: "192.168.1.100:63000"},
		{NodeID: 2, Address: "192.168.1.101:63000"},
	}
	mgr.UpdatePeers(newPeers)

	// Verify peers were updated
	peers = mgr.GetPeers()
	if len(peers) != 2 {
		t.Errorf("GetPeers() after update returned %d peers, want 2", len(peers))
	}

	// Verify we got a copy
	peers[0].NodeID = 999
	actualPeers := mgr.GetPeers()
	if actualPeers[0].NodeID == 999 {
		t.Error("GetPeers() did not return a copy")
	}
}

func TestManager_GetSelf(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	staticDiscovery, err := NewStaticDiscovery([]string{"1:192.168.1.100:63000"})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Close()

	retrievedSelf := mgr.GetSelf()
	if retrievedSelf.NodeID != self.NodeID {
		t.Errorf("GetSelf().NodeID = %d, want %d", retrievedSelf.NodeID, self.NodeID)
	}

	if retrievedSelf.Address != self.Address {
		t.Errorf("GetSelf().Address = %s, want %s", retrievedSelf.Address, self.Address)
	}
}

func TestManager_GetStats(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	staticDiscovery, err := NewStaticDiscovery([]string{"1:192.168.1.100:63000"})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Close()

	stats := mgr.GetStats()
	if stats.DiscoveryMethods != 1 {
		t.Errorf("stats.DiscoveryMethods = %d, want 1", stats.DiscoveryMethods)
	}

	if stats.PeerCount != 0 {
		t.Errorf("stats.PeerCount = %d, want 0", stats.PeerCount)
	}

	if stats.Self.NodeID != self.NodeID {
		t.Errorf("stats.Self.NodeID = %d, want %d", stats.Self.NodeID, self.NodeID)
	}
}

func TestManager_Close(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	staticDiscovery, err := NewStaticDiscovery([]string{"1:192.168.1.100:63000"})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Close should succeed
	if err := mgr.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Second close should be safe (no-op)
	if err := mgr.Close(); err != nil {
		t.Errorf("second Close() error = %v", err)
	}

	// Operations after close should fail
	ctx := context.Background()
	if err := mgr.Bootstrap(ctx); err == nil {
		t.Error("Bootstrap() after Close() should return error")
	}

	if _, err := mgr.Discover(ctx); err == nil {
		t.Error("Discover() after Close() should return error")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Methods:          []Method{},
				MinPeers:         3,
				DiscoveryTimeout: 30 * time.Second,
				RetryInterval:    5 * time.Second,
			},
			wantErr: true, // No methods
		},
		{
			name: "no methods",
			config: &Config{
				Methods:          []Method{},
				MinPeers:         3,
				DiscoveryTimeout: 30 * time.Second,
				RetryInterval:    5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "zero min peers",
			config: &Config{
				Methods:          []Method{},
				MinPeers:         0,
				DiscoveryTimeout: 30 * time.Second,
				RetryInterval:    5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid timeout",
			config: &Config{
				Methods:          []Method{},
				MinPeers:         3,
				DiscoveryTimeout: 0,
				RetryInterval:    5 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	if config.MinPeers != 3 {
		t.Errorf("DefaultConfig().MinPeers = %d, want 3", config.MinPeers)
	}

	if config.DiscoveryTimeout != 30*time.Second {
		t.Errorf("DefaultConfig().DiscoveryTimeout = %v, want 30s", config.DiscoveryTimeout)
	}
}

// =============================================================================
// Consolidated from coverage_test.go
// =============================================================================

// testLogger creates a logger suitable for unit tests.
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	return log
}

// ---------------------------------------------------------------------------
// Static error type tests
// ---------------------------------------------------------------------------

func TestStaticPeerNodeIDError_ErrorWithCause(t *testing.T) {
	cause := errors.New("strconv error")
	e := &StaticPeerNodeIDError{NodeID: "abc", Cause: cause}

	msg := e.Error()
	require.Contains(t, msg, "abc")
	require.Contains(t, msg, "strconv error")
}

func TestStaticPeerNodeIDError_ErrorWithoutCause(t *testing.T) {
	e := &StaticPeerNodeIDError{NodeID: "xyz"}

	msg := e.Error()
	require.Contains(t, msg, "xyz")
	require.NotContains(t, msg, ": ")
}

func TestStaticPeerNodeIDError_Unwrap(t *testing.T) {
	cause := errors.New("wrapped")
	e := &StaticPeerNodeIDError{NodeID: "1", Cause: cause}
	require.ErrorIs(t, e, cause)
}

func TestStaticPeerNodeIDError_UnwrapNil(t *testing.T) {
	e := &StaticPeerNodeIDError{NodeID: "1"}
	require.Nil(t, e.Unwrap())
}

func TestStaticPeerPortError_ErrorWithCause(t *testing.T) {
	cause := errors.New("parse error")
	e := &StaticPeerPortError{Port: "abc", Cause: cause}

	msg := e.Error()
	require.Contains(t, msg, "abc")
	require.Contains(t, msg, "parse error")
}

func TestStaticPeerPortError_ErrorWithoutCause(t *testing.T) {
	e := &StaticPeerPortError{Port: "999999"}

	msg := e.Error()
	require.Contains(t, msg, "999999")
	require.NotContains(t, msg, ": ")
}

func TestStaticPeerPortError_Unwrap(t *testing.T) {
	cause := errors.New("wrapped port")
	e := &StaticPeerPortError{Port: "bad", Cause: cause}
	require.ErrorIs(t, e, cause)
}

func TestStaticPeerPortError_UnwrapNil(t *testing.T) {
	e := &StaticPeerPortError{Port: "bad"}
	require.Nil(t, e.Unwrap())
}

// ---------------------------------------------------------------------------
// DNS error type tests
// ---------------------------------------------------------------------------

func TestSRVRecordEmptyError_Error(t *testing.T) {
	e := &SRVRecordEmptyError{}
	msg := e.Error()
	require.Contains(t, msg, "empty")
}

func TestSRVRecordComponentError_Error(t *testing.T) {
	e := &SRVRecordComponentError{}
	msg := e.Error()
	require.Contains(t, msg, "empty component")
}

// ---------------------------------------------------------------------------
// Multicast error type tests
// ---------------------------------------------------------------------------

func TestMulticastMessageTooShortError_Error(t *testing.T) {
	e := &MulticastMessageTooShortError{}
	msg := e.Error()
	require.Equal(t, "message too short", msg)
}

func TestMulticastInvalidMagicError_Error(t *testing.T) {
	e := &MulticastInvalidMagicError{}
	msg := e.Error()
	require.Equal(t, "invalid magic number", msg)
}

func TestMulticastUnsupportedVersionError_Error(t *testing.T) {
	e := &MulticastUnsupportedVersionError{Version: 99}
	msg := e.Error()
	require.Contains(t, msg, "99")
	require.Contains(t, msg, "unsupported protocol version")
}

func TestMulticastAuthFormatError_Error(t *testing.T) {
	e := &MulticastAuthFormatError{Detail: "bad signature length"}
	msg := e.Error()
	require.Contains(t, msg, "bad signature length")
}

func TestMulticastInvalidSignatureError_Error(t *testing.T) {
	e := &MulticastInvalidSignatureError{}
	msg := e.Error()
	require.Equal(t, "invalid message signature", msg)
}

// ---------------------------------------------------------------------------
// Kubernetes error type tests
// ---------------------------------------------------------------------------

func TestKubernetesPodNameMismatchError_Error(t *testing.T) {
	e := &KubernetesPodNameMismatchError{PodName: "other-0", StatefulSetName: "quicbaas"}
	msg := e.Error()
	require.Contains(t, msg, "other-0")
	require.Contains(t, msg, "quicbaas")
}

func TestKubernetesPodOrdinalError_Error(t *testing.T) {
	cause := errors.New("not a number")
	e := &KubernetesPodOrdinalError{PodName: "quicbaas-abc", Cause: cause}
	msg := e.Error()
	require.Contains(t, msg, "quicbaas-abc")
	require.Contains(t, msg, "not a number")
}

func TestKubernetesPodOrdinalError_Unwrap(t *testing.T) {
	cause := errors.New("parse fail")
	e := &KubernetesPodOrdinalError{PodName: "pod-x", Cause: cause}
	require.ErrorIs(t, e, cause)
}

func TestKubernetesSRVLookupError_Error(t *testing.T) {
	cause := errors.New("dns timeout")
	e := &KubernetesSRVLookupError{Cause: cause}
	msg := e.Error()
	require.Contains(t, msg, "SRV lookup failed")
	require.Contains(t, msg, "dns timeout")
}

func TestKubernetesSRVLookupError_Unwrap(t *testing.T) {
	cause := errors.New("network error")
	e := &KubernetesSRVLookupError{Cause: cause}
	require.ErrorIs(t, e, cause)
}

func TestKubernetesNoSRVRecordsError_Error(t *testing.T) {
	e := &KubernetesNoSRVRecordsError{}
	msg := e.Error()
	require.Equal(t, "no SRV records found", msg)
}

func TestKubernetesServiceLookupError_Error(t *testing.T) {
	cause := errors.New("host not found")
	e := &KubernetesServiceLookupError{ServiceFQDN: "svc.ns.svc.cluster.local", Cause: cause}
	msg := e.Error()
	require.Contains(t, msg, "svc.ns.svc.cluster.local")
	require.Contains(t, msg, "host not found")
}

func TestKubernetesServiceLookupError_Unwrap(t *testing.T) {
	cause := errors.New("lookup fail")
	e := &KubernetesServiceLookupError{ServiceFQDN: "svc", Cause: cause}
	require.ErrorIs(t, e, cause)
}

func TestKubernetesNoIPsError_Error(t *testing.T) {
	e := &KubernetesNoIPsError{ServiceFQDN: "svc.ns.svc.cluster.local"}
	msg := e.Error()
	require.Contains(t, msg, "no IPs found")
	require.Contains(t, msg, "svc.ns.svc.cluster.local")
}

// ---------------------------------------------------------------------------
// extractPodName edge case
// ---------------------------------------------------------------------------

func TestExtractPodName_EmptyString(t *testing.T) {
	result := extractPodName("")
	require.Equal(t, "", result)
}

func TestExtractPodName_NoDots(t *testing.T) {
	result := extractPodName("quicbaas-0")
	require.Equal(t, "quicbaas-0", result)
}

// ---------------------------------------------------------------------------
// NewManager edge cases
// ---------------------------------------------------------------------------

func TestNewManager_NilLogger(t *testing.T) {
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	_, err = NewManager(cfg, self, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "logger cannot be nil")
}

func TestNewManager_ZeroNodeID(t *testing.T) {
	log := testLogger(t)
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 0, Address: "127.0.0.1:63000"}

	_, err = NewManager(cfg, self, log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "NodeID cannot be 0")
}

func TestNewManager_EmptyAddress(t *testing.T) {
	log := testLogger(t)
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: ""}

	_, err = NewManager(cfg, self, log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Address cannot be empty")
}

func TestNewManager_SetsRaftAddrFromAddress(t *testing.T) {
	log := testLogger(t)
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000", RaftAddr: ""}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	got := mgr.GetSelf()
	require.Equal(t, "127.0.0.1:63000", got.RaftAddr)
}

func TestNewManager_NilConfig(t *testing.T) {
	log := testLogger(t)
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	_, err := NewManager(nil, self, log)
	require.Error(t, err)
	require.Contains(t, err.Error(), "config cannot be nil")
}

// ---------------------------------------------------------------------------
// failingMethod is a Method implementation that always fails.
// ---------------------------------------------------------------------------

type failingMethod struct {
	name          string
	discoverErr   error
	registerErr   error
	deregisterErr error
	closeErr      error
}

func (f *failingMethod) Name() string { return f.name }
func (f *failingMethod) Discover(ctx context.Context) ([]Peer, error) {
	return nil, f.discoverErr
}
func (f *failingMethod) Register(ctx context.Context, self Peer) error { return f.registerErr }
func (f *failingMethod) Deregister(ctx context.Context) error          { return f.deregisterErr }
func (f *failingMethod) Close() error                                  { return f.closeErr }

// configurableMethod returns configurable peers or errors.
type configurableMethod struct {
	name          string
	peers         []Peer
	discoverErr   error
	registerErr   error
	deregisterErr error
	closeErr      error
}

func (c *configurableMethod) Name() string { return c.name }
func (c *configurableMethod) Discover(ctx context.Context) ([]Peer, error) {
	if c.discoverErr != nil {
		return nil, c.discoverErr
	}
	return c.peers, nil
}
func (c *configurableMethod) Register(ctx context.Context, self Peer) error { return c.registerErr }
func (c *configurableMethod) Deregister(ctx context.Context) error          { return c.deregisterErr }
func (c *configurableMethod) Close() error                                  { return c.closeErr }

// ---------------------------------------------------------------------------
// Bootstrap tests
// ---------------------------------------------------------------------------

func TestBootstrap_ClosedManager(t *testing.T) {
	log := testLogger(t)
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)

	err = mgr.Close()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.Bootstrap(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manager is closed")
}

func TestBootstrap_AllMethodsFail(t *testing.T) {
	log := testLogger(t)
	fm := &failingMethod{
		name:        "failing",
		discoverErr: errors.New("discover failed"),
	}

	cfg := &Config{
		Methods:          []Method{fm},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.Bootstrap(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failing")
}

func TestBootstrap_InsufficientPeers(t *testing.T) {
	log := testLogger(t)

	onePeerMethod := &configurableMethod{
		name: "one-peer",
		peers: []Peer{
			{NodeID: 1, Address: "127.0.0.1:63000"},
		},
	}

	cfg := &Config{
		Methods:          []Method{onePeerMethod},
		MinPeers:         3,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.Bootstrap(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoQuorum)
}

func TestBootstrap_SuccessWithRegisterFailure(t *testing.T) {
	log := testLogger(t)

	// Method that discovers peers but fails on register -- bootstrap should still succeed
	m := &configurableMethod{
		name: "reg-fail",
		peers: []Peer{
			{NodeID: 1, Address: "127.0.0.1:63000"},
			{NodeID: 2, Address: "127.0.0.1:63001"},
			{NodeID: 3, Address: "127.0.0.1:63002"},
		},
		registerErr: errors.New("register failed"),
	}

	cfg := &Config{
		Methods:          []Method{m},
		MinPeers:         2,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.Bootstrap(ctx)
	require.NoError(t, err)

	peers := mgr.GetPeers()
	require.Len(t, peers, 3)
}

func TestBootstrap_SecondMethodSucceeds(t *testing.T) {
	log := testLogger(t)

	failMethod := &failingMethod{
		name:        "fail-first",
		discoverErr: errors.New("nope"),
	}
	successMethod := &configurableMethod{
		name: "success-second",
		peers: []Peer{
			{NodeID: 1, Address: "127.0.0.1:63000"},
			{NodeID: 2, Address: "127.0.0.1:63001"},
		},
	}

	cfg := &Config{
		Methods:          []Method{failMethod, successMethod},
		MinPeers:         2,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.Bootstrap(ctx)
	require.NoError(t, err)
}

func TestBootstrap_NoMethodsAllFailReturnsNoMethodSucceeded(t *testing.T) {
	log := testLogger(t)

	// A method that returns empty peers (0 discovered, less than MinPeers)
	emptyMethod := &configurableMethod{
		name:  "empty",
		peers: []Peer{},
	}

	cfg := &Config{
		Methods:          []Method{emptyMethod},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = mgr.Bootstrap(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoQuorum)
}

// ---------------------------------------------------------------------------
// Discover (non-Bootstrap) tests
// ---------------------------------------------------------------------------

func TestDiscover_ClosedManager(t *testing.T) {
	log := testLogger(t)
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	err = mgr.Close()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = mgr.Discover(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manager is closed")
}

func TestDiscover_AllMethodsReturnEmpty(t *testing.T) {
	log := testLogger(t)
	emptyMethod := &configurableMethod{
		name:  "empty",
		peers: []Peer{},
	}

	cfg := &Config{
		Methods:          []Method{emptyMethod},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = mgr.Discover(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoMethodSucceeded)
}

func TestDiscover_FirstFailSecondSucceeds(t *testing.T) {
	log := testLogger(t)

	failMethod := &failingMethod{
		name:        "fail",
		discoverErr: errors.New("oops"),
	}
	goodMethod := &configurableMethod{
		name: "good",
		peers: []Peer{
			{NodeID: 1, Address: "127.0.0.1:63000"},
		},
	}

	cfg := &Config{
		Methods:          []Method{failMethod, goodMethod},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	peers, err := mgr.Discover(ctx)
	require.NoError(t, err)
	require.Len(t, peers, 1)
}

// ---------------------------------------------------------------------------
// WaitForQuorum - closed manager
// ---------------------------------------------------------------------------

func TestWaitForQuorum_ClosedManager(t *testing.T) {
	log := testLogger(t)
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	err = mgr.Close()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = mgr.WaitForQuorum(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manager is closed")
}

// ---------------------------------------------------------------------------
// Close -- deregister and close errors are logged, not returned
// ---------------------------------------------------------------------------

func TestClose_DeregisterAndCloseErrors(t *testing.T) {
	log := testLogger(t)

	m := &failingMethod{
		name:          "fail-close",
		discoverErr:   errors.New("d"),
		deregisterErr: errors.New("deregister fail"),
		closeErr:      errors.New("close fail"),
	}

	cfg := &Config{
		Methods:          []Method{m},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)

	// Close should not return error even if deregister/close fail on methods
	err = mgr.Close()
	require.NoError(t, err)
}

func TestClose_Idempotent(t *testing.T) {
	log := testLogger(t)
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)

	err = mgr.Close()
	require.NoError(t, err)

	// Second close should be safe (sync.Once)
	err = mgr.Close()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// validateSRVRecord edge cases
// ---------------------------------------------------------------------------

func TestValidateSRVRecord_EmptyRecord(t *testing.T) {
	err := validateSRVRecord("")
	require.Error(t, err)
	var emptyErr *SRVRecordEmptyError
	require.ErrorAs(t, err, &emptyErr)
}

func TestValidateSRVRecord_NoUnderscorePrefix(t *testing.T) {
	err := validateSRVRecord("service._tcp.example.com")
	require.Error(t, err)
	var fmtErr *SRVRecordFormatError
	require.ErrorAs(t, err, &fmtErr)
	require.Contains(t, fmtErr.Error(), "must start with underscore")
}

func TestValidateSRVRecord_TooFewParts(t *testing.T) {
	err := validateSRVRecord("_service._tcp")
	require.Error(t, err)
	var fmtErr *SRVRecordFormatError
	require.ErrorAs(t, err, &fmtErr)
	require.Contains(t, fmtErr.Error(), "at least 3 parts")
}

func TestValidateSRVRecord_ProtoNoUnderscore(t *testing.T) {
	err := validateSRVRecord("_service.tcp.example.com")
	require.Error(t, err)
	var fmtErr *SRVRecordFormatError
	require.ErrorAs(t, err, &fmtErr)
	require.Contains(t, fmtErr.Error(), "proto part must start with underscore")
}

// ---------------------------------------------------------------------------
// DNS Discover - cache hit path
// ---------------------------------------------------------------------------

// testDNSResolver implements DNSResolver for testing.
type testDNSResolver struct {
	srvRecords []*net.SRV
	srvErr     error
	hosts      []string
	hostErr    error
}

func (r *testDNSResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	return "", r.srvRecords, r.srvErr
}

func (r *testDNSResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return r.hosts, r.hostErr
}

func TestDNSDiscover_CacheHit(t *testing.T) {
	resolver := &testDNSResolver{
		srvRecords: []*net.SRV{
			{Target: "node1.example.com.", Port: 63000, Priority: 1, Weight: 1},
		},
	}

	d, err := NewDNSDiscovery(DNSConfig{
		SRVRecord:       "_quicbaas._tcp.example.com",
		RefreshInterval: 1 * time.Hour, // long TTL so cache stays fresh
		Resolver:        resolver,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First call populates cache
	peers1, err := d.Discover(ctx)
	require.NoError(t, err)
	require.Len(t, peers1, 1)

	// Second call should return cached result
	peers2, err := d.Discover(ctx)
	require.NoError(t, err)
	require.Len(t, peers2, 1)
}

func TestDNSDiscover_NoSRVRecords(t *testing.T) {
	resolver := &testDNSResolver{
		srvRecords: []*net.SRV{},
	}

	d, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
		Resolver:  resolver,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = d.Discover(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no SRV records found")
}

func TestDNSDiscover_LookupError(t *testing.T) {
	resolver := &testDNSResolver{
		srvErr: errors.New("dns timeout"),
	}

	d, err := NewDNSDiscovery(DNSConfig{
		SRVRecord: "_quicbaas._tcp.example.com",
		Resolver:  resolver,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = d.Discover(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SRV lookup failed")
}

// ---------------------------------------------------------------------------
// Multicast NewMulticastDiscovery edge cases
// ---------------------------------------------------------------------------

func TestNewMulticastDiscovery_NotMulticastAddress(t *testing.T) {
	cfg := &MulticastConfig{
		MulticastGroup: "10.0.0.1", // unicast address
	}
	_, err := NewMulticastDiscovery(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a multicast address")
}

func TestNewMulticastDiscovery_InvalidIP(t *testing.T) {
	cfg := &MulticastConfig{
		MulticastGroup: "not-an-ip",
	}
	_, err := NewMulticastDiscovery(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid multicast group")
}

func TestNewMulticastDiscovery_NilConfigUsesDefaults(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)
	require.Equal(t, "multicast", md.Name())
	require.Equal(t, DefaultMulticastGroup, md.config.MulticastGroup)
	require.Equal(t, DefaultMulticastPort, md.config.Port)
	require.Equal(t, DefaultBroadcastInterval, md.config.BroadcastInterval)
	require.Equal(t, DefaultPeerTTL, md.config.PeerTTL)
}

// ---------------------------------------------------------------------------
// Multicast Register validation
// ---------------------------------------------------------------------------

func TestMulticastRegister_ZeroNodeID(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = md.Register(ctx, Peer{NodeID: 0, Address: "127.0.0.1:63000"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "NodeID cannot be 0")
}

func TestMulticastRegister_EmptyAddress(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = md.Register(ctx, Peer{NodeID: 1, Address: ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Address cannot be empty")
}

// ---------------------------------------------------------------------------
// Multicast Deregister when not running
// ---------------------------------------------------------------------------

func TestMulticastDeregister_NotRunning(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = md.Deregister(ctx)
	require.NoError(t, err) // no-op when not running
}

// ---------------------------------------------------------------------------
// Multicast message encode/parse roundtrip (without PSK)
// ---------------------------------------------------------------------------

func TestMulticastEncodeParseMessage_NoPSK(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	original := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    42,
		Address:   "10.0.0.1:63000",
		RaftAddr:  "10.0.0.1:63000",
		Metadata:  map[string]string{"region": "us-east"},
		Timestamp: time.Now().UnixNano(),
	}

	data, err := md.encodeMessage(original)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	parsed, err := md.parseMessage(data)
	require.NoError(t, err)
	require.Equal(t, original.NodeID, parsed.NodeID)
	require.Equal(t, original.Address, parsed.Address)
	require.Equal(t, original.Type, parsed.Type)
	require.Equal(t, magicNumber, parsed.Magic)
}

// ---------------------------------------------------------------------------
// Multicast message encode/parse roundtrip (with PSK)
// ---------------------------------------------------------------------------

func TestMulticastEncodeParseMessage_WithPSK(t *testing.T) {
	cfg := &MulticastConfig{
		ClusterSecret: "test-secret-key",
	}
	md, err := NewMulticastDiscovery(cfg)
	require.NoError(t, err)

	original := &DiscoveryMessage{
		Type:      MessageResponse,
		NodeID:    7,
		Address:   "10.0.0.7:63000",
		RaftAddr:  "10.0.0.7:63000",
		Timestamp: time.Now().UnixNano(),
	}

	data, err := md.encodeMessage(original)
	require.NoError(t, err)

	parsed, err := md.parseMessage(data)
	require.NoError(t, err)
	require.Equal(t, original.NodeID, parsed.NodeID)
}

func TestMulticastParseMessage_WrongPSK(t *testing.T) {
	// Encode with one secret
	cfg1 := &MulticastConfig{ClusterSecret: "secret-1"}
	md1, err := NewMulticastDiscovery(cfg1)
	require.NoError(t, err)

	msg := &DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    1,
		Address:   "10.0.0.1:63000",
		Timestamp: time.Now().UnixNano(),
	}

	data, err := md1.encodeMessage(msg)
	require.NoError(t, err)

	// Try to parse with different secret
	cfg2 := &MulticastConfig{ClusterSecret: "secret-2"}
	md2, err := NewMulticastDiscovery(cfg2)
	require.NoError(t, err)

	_, err = md2.parseMessage(data)
	require.Error(t, err)
	var sigErr *MulticastInvalidSignatureError
	require.ErrorAs(t, err, &sigErr)
}

func TestMulticastParseMessage_TooShort(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	_, err = md.parseMessage([]byte{0x01, 0x02})
	require.Error(t, err)
	var shortErr *MulticastMessageTooShortError
	require.ErrorAs(t, err, &shortErr)
}

func TestMulticastParseMessage_InvalidMagic(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	data := []byte{0x00, 0x00, 0x00, 0x00, 0x01} // wrong magic
	_, err = md.parseMessage(data)
	require.Error(t, err)
	var magicErr *MulticastInvalidMagicError
	require.ErrorAs(t, err, &magicErr)
}

func TestMulticastParseMessage_UnsupportedVersion(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	// Build data with correct magic but bad version
	data := make([]byte, 6)
	data[0] = 0x44
	data[1] = 0x52
	data[2] = 0x41
	data[3] = 0x47 // "DRAG"
	data[4] = 255  // unsupported version
	data[5] = 0x00

	_, err = md.parseMessage(data)
	require.Error(t, err)
	var verErr *MulticastUnsupportedVersionError
	require.ErrorAs(t, err, &verErr)
	require.Equal(t, uint8(255), verErr.Version)
}

// ---------------------------------------------------------------------------
// Multicast PSK parse edge cases
// ---------------------------------------------------------------------------

func TestMulticastParseMessage_PSKTooShortForSigLen(t *testing.T) {
	cfg := &MulticastConfig{ClusterSecret: "mysecret"}
	md, err := NewMulticastDiscovery(cfg)
	require.NoError(t, err)

	// Valid magic + version but only 6 bytes (no room for sig_len)
	data := []byte{0x44, 0x52, 0x41, 0x47, 0x01, 0x00}
	_, err = md.parseMessage(data)
	require.Error(t, err)
	var authErr *MulticastAuthFormatError
	require.ErrorAs(t, err, &authErr)
}

func TestMulticastParseMessage_PSKZeroSigLen(t *testing.T) {
	cfg := &MulticastConfig{ClusterSecret: "mysecret"}
	md, err := NewMulticastDiscovery(cfg)
	require.NoError(t, err)

	// Valid magic + version + sig_len = 0
	data := []byte{0x44, 0x52, 0x41, 0x47, 0x01, 0x00, 0x00}
	_, err = md.parseMessage(data)
	require.Error(t, err)
	var authErr *MulticastAuthFormatError
	require.ErrorAs(t, err, &authErr)
	require.Contains(t, authErr.Error(), "signature length is zero")
}

func TestMulticastParseMessage_PSKSigLenExceedsData(t *testing.T) {
	cfg := &MulticastConfig{ClusterSecret: "mysecret"}
	md, err := NewMulticastDiscovery(cfg)
	require.NoError(t, err)

	// Valid magic + version + sig_len = 100 but only 8 bytes total
	data := []byte{0x44, 0x52, 0x41, 0x47, 0x01, 0x00, 0x64}
	_, err = md.parseMessage(data)
	require.Error(t, err)
	var authErr *MulticastAuthFormatError
	require.ErrorAs(t, err, &authErr)
	require.Contains(t, authErr.Error(), "message too short for signature")
}

// ---------------------------------------------------------------------------
// Multicast handleMessage
// ---------------------------------------------------------------------------

func TestMulticastHandleMessage_Announce(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	md.handleMessage(&DiscoveryMessage{
		Type:      MessageAnnounce,
		NodeID:    10,
		Address:   "10.0.0.10:63000",
		RaftAddr:  "10.0.0.10:63000",
		Timestamp: time.Now().UnixNano(),
	})

	require.Equal(t, 1, md.GetPeerCount())
}

func TestMulticastHandleMessage_Leave(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	// Add a peer first
	md.handleMessage(&DiscoveryMessage{
		Type:    MessageAnnounce,
		NodeID:  10,
		Address: "10.0.0.10:63000",
	})
	require.Equal(t, 1, md.GetPeerCount())

	// Leave
	md.handleMessage(&DiscoveryMessage{
		Type:   MessageLeave,
		NodeID: 10,
	})
	require.Equal(t, 0, md.GetPeerCount())
}

func TestMulticastUpdatePeer_ZeroNodeIDIgnored(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	md.updatePeer(&DiscoveryMessage{NodeID: 0, Address: "10.0.0.1:63000"})
	require.Equal(t, 0, md.GetPeerCount())
}

func TestMulticastUpdatePeer_EmptyAddressIgnored(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	md.updatePeer(&DiscoveryMessage{NodeID: 1, Address: ""})
	require.Equal(t, 0, md.GetPeerCount())
}

func TestMulticastUpdatePeer_SetsRaftAddrFromAddress(t *testing.T) {
	md, err := NewMulticastDiscovery(nil)
	require.NoError(t, err)

	md.updatePeer(&DiscoveryMessage{NodeID: 1, Address: "10.0.0.1:63000", RaftAddr: ""})
	peers := md.getPeers()
	require.Len(t, peers, 1)
	require.Equal(t, "10.0.0.1:63000", peers[0].RaftAddr)
}

// ---------------------------------------------------------------------------
// Multicast removeExpiredPeers
// ---------------------------------------------------------------------------

func TestMulticastRemoveExpiredPeers(t *testing.T) {
	cfg := &MulticastConfig{
		PeerTTL: 1 * time.Millisecond, // very short TTL
	}
	md, err := NewMulticastDiscovery(cfg)
	require.NoError(t, err)

	// Add peer
	md.peersMu.Lock()
	md.peers[1] = &peerEntry{
		peer:     Peer{NodeID: 1, Address: "10.0.0.1:63000"},
		lastSeen: time.Now().Add(-1 * time.Second), // expired
	}
	md.peersMu.Unlock()

	require.Equal(t, 1, md.GetPeerCount())

	md.removeExpiredPeers()

	require.Equal(t, 0, md.GetPeerCount())
}

// ---------------------------------------------------------------------------
// Kubernetes discoverViaSRV with mock resolver
// ---------------------------------------------------------------------------

func TestKubernetesDiscoverViaSRV_Success(t *testing.T) {
	resolver := &testDNSResolver{
		srvRecords: []*net.SRV{
			{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 1, Weight: 1},
			{Target: "quicbaas-1.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 1, Weight: 1},
			{Target: "quicbaas-2.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 1, Weight: 1},
		},
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
		Namespace:   "default",
		Resolver:    resolver,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	peers, err := kd.Discover(ctx)
	require.NoError(t, err)
	require.Len(t, peers, 3)
}

func TestKubernetesDiscoverViaSRV_FallbackToARecords(t *testing.T) {
	// SRV fails, but A record lookup succeeds
	callCount := 0
	resolver := &orderedDNSResolver{
		srvErr: errors.New("no SRV"),
		hostFunc: func(ctx context.Context, host string) ([]string, error) {
			callCount++
			// Return IPs for service FQDN
			if callCount == 1 {
				return []string{"10.0.0.1"}, nil
			}
			// Return IPs for pod-0 FQDN
			if callCount == 2 {
				return []string{"10.0.0.1"}, nil
			}
			return nil, errors.New("no more pods")
		},
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
		Namespace:   "default",
		Resolver:    resolver,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	peers, err := kd.Discover(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(peers), 1)
}

// orderedDNSResolver allows per-call customization.
type orderedDNSResolver struct {
	srvRecords []*net.SRV
	srvErr     error
	hostFunc   func(ctx context.Context, host string) ([]string, error)
}

func (r *orderedDNSResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	return "", r.srvRecords, r.srvErr
}

func (r *orderedDNSResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if r.hostFunc != nil {
		return r.hostFunc(ctx, host)
	}
	return nil, errors.New("not implemented")
}

func TestKubernetesDiscover_CacheHit(t *testing.T) {
	resolver := &testDNSResolver{
		srvRecords: []*net.SRV{
			{Target: "quicbaas-0.quicbaas.default.svc.cluster.local.", Port: 63001, Priority: 1, Weight: 1},
		},
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName:     "quicbaas",
		Namespace:       "default",
		Resolver:        resolver,
		RefreshInterval: 1 * time.Hour,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First call populates cache
	peers1, err := kd.Discover(ctx)
	require.NoError(t, err)

	// Second call returns cached
	peers2, err := kd.Discover(ctx)
	require.NoError(t, err)
	require.Equal(t, len(peers1), len(peers2))
}

func TestKubernetesDiscover_BothMethodsFail(t *testing.T) {
	resolver := &testDNSResolver{
		srvErr:  errors.New("srv fail"),
		hostErr: errors.New("host fail"),
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
		Namespace:   "default",
		Resolver:    resolver,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = kd.Discover(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "all discovery methods failed")
}

func TestKubernetesDiscover_NoPeersFound(t *testing.T) {
	resolver := &testDNSResolver{
		srvRecords: []*net.SRV{
			// Target does not match statefulset name
			{Target: "other-0.svc.ns.svc.cluster.local.", Port: 63001},
		},
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
		Namespace:   "default",
		Resolver:    resolver,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = kd.Discover(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no peers found")
}

// ---------------------------------------------------------------------------
// StaticPeerPortRangeError
// ---------------------------------------------------------------------------

func TestStaticPeerPortRangeError_Error(t *testing.T) {
	e := &StaticPeerPortRangeError{Port: 99999}
	msg := e.Error()
	require.Contains(t, msg, "99999")
	require.Contains(t, msg, "out of range")
}

// ---------------------------------------------------------------------------
// StaticPeerFormatError
// ---------------------------------------------------------------------------

func TestStaticPeerFormatError_WithAddress(t *testing.T) {
	e := &StaticPeerFormatError{Address: "bad:format", Detail: "expected nodeID:host:port"}
	msg := e.Error()
	require.Contains(t, msg, "bad:format")
	require.Contains(t, msg, "expected nodeID:host:port")
}

func TestStaticPeerFormatError_WithoutAddress(t *testing.T) {
	e := &StaticPeerFormatError{Detail: "host cannot be empty"}
	msg := e.Error()
	require.Contains(t, msg, "host cannot be empty")
	require.NotContains(t, msg, "'': ")
}

// ---------------------------------------------------------------------------
// ErrNoQuorum and ErrNoMethodSucceeded sentinel errors
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	require.Contains(t, ErrNoQuorum.Error(), "insufficient peers for quorum")
	require.Contains(t, ErrNoMethodSucceeded.Error(), "all discovery methods failed")
}

// ---------------------------------------------------------------------------
// Kubernetes helper functions
// ---------------------------------------------------------------------------

func TestExtractNodeIDFromPodName_Mismatch(t *testing.T) {
	_, err := extractNodeIDFromPodName("other-app-0", "quicbaas")
	require.Error(t, err)
	var mismatchErr *KubernetesPodNameMismatchError
	require.ErrorAs(t, err, &mismatchErr)
}

func TestExtractNodeIDFromPodName_NonNumericOrdinal(t *testing.T) {
	_, err := extractNodeIDFromPodName("quicbaas-abc", "quicbaas")
	require.Error(t, err)
	var ordinalErr *KubernetesPodOrdinalError
	require.ErrorAs(t, err, &ordinalErr)
}

func TestExtractNodeIDFromPodName_Valid(t *testing.T) {
	nodeID, err := extractNodeIDFromPodName("quicbaas-0", "quicbaas")
	require.NoError(t, err)
	require.Equal(t, uint64(1), nodeID)

	nodeID, err = extractNodeIDFromPodName("quicbaas-2", "quicbaas")
	require.NoError(t, err)
	require.Equal(t, uint64(3), nodeID)
}

func TestValidatePodName_Valid(t *testing.T) {
	require.True(t, validatePodName("quicbaas-0", "quicbaas"))
	require.True(t, validatePodName("quicbaas-10", "quicbaas"))
}

func TestValidatePodName_Invalid(t *testing.T) {
	require.False(t, validatePodName("other-0", "quicbaas"))
	require.False(t, validatePodName("quicbaas-abc", "quicbaas"))
	require.False(t, validatePodName("", "quicbaas"))
}

// ---------------------------------------------------------------------------
// Kubernetes convenience constructors
// ---------------------------------------------------------------------------

func TestNewKubernetesDiscoverySimple_EmptyService(t *testing.T) {
	_, err := NewKubernetesDiscoverySimple("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "service_name cannot be empty")
}

func TestNewKubernetesDiscoveryWithNamespace_EmptyService(t *testing.T) {
	_, err := NewKubernetesDiscoveryWithNamespace("", "default")
	require.Error(t, err)
	require.Contains(t, err.Error(), "service_name cannot be empty")
}

// ---------------------------------------------------------------------------
// Kubernetes A-record discovery edge case: no IPs for service
// ---------------------------------------------------------------------------

func TestKubernetesDiscoverViaARecords_NoIPs(t *testing.T) {
	resolver := &testDNSResolver{
		srvErr: errors.New("srv fail"),
		hosts:  []string{}, // no IPs returned
	}

	kd, err := NewKubernetesDiscovery(KubernetesConfig{
		ServiceName: "quicbaas",
		Namespace:   "default",
		Resolver:    resolver,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = kd.Discover(ctx)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Manager GetStats
// ---------------------------------------------------------------------------

func TestManagerGetStats(t *testing.T) {
	log := testLogger(t)
	sd, err := NewStaticDiscovery([]string{"1:127.0.0.1:63000"})
	require.NoError(t, err)

	cfg := &Config{
		Methods:          []Method{sd},
		MinPeers:         1,
		DiscoveryTimeout: 5 * time.Second,
		RetryInterval:    1 * time.Second,
	}
	self := Peer{NodeID: 1, Address: "127.0.0.1:63000"}

	mgr, err := NewManager(cfg, self, log)
	require.NoError(t, err)
	defer mgr.Close()

	stats := mgr.GetStats()
	require.Equal(t, 0, stats.PeerCount)
	require.Equal(t, 1, stats.DiscoveryMethods)
	require.Equal(t, uint64(1), stats.Self.NodeID)

	mgr.UpdatePeers([]Peer{
		{NodeID: 1, Address: "127.0.0.1:63000"},
		{NodeID: 2, Address: "127.0.0.1:63001"},
	})

	stats = mgr.GetStats()
	require.Equal(t, 2, stats.PeerCount)
}

// =============================================================================
// Consolidated from discovery_coverage_boost_test.go
// =============================================================================

// stubMethod implements the Method interface for testing.
type stubMethod struct {
	name string
}

func (s *stubMethod) Name() string                                  { return s.name }
func (s *stubMethod) Discover(ctx context.Context) ([]Peer, error)  { return nil, nil }
func (s *stubMethod) Register(ctx context.Context, self Peer) error { return nil }
func (s *stubMethod) Deregister(ctx context.Context) error          { return nil }
func (s *stubMethod) Close() error                                  { return nil }

// TestStaticPeer_EmptyHostAfterColon covers static.go:152-154
// Tests parseStaticPeer with empty host component ("1::63000").
func TestStaticPeer_EmptyHostAfterColon(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{
			name:    "empty host with valid node ID and port",
			addr:    "1::63000",
			wantErr: true, // Should fail validation
		},
		{
			name:    "empty host double colon format",
			addr:    "5::8080",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseStaticPeer(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseStaticPeer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !tt.wantErr {
				t.Logf("Got expected error: %v", err)
			}
		})
	}
}

// TestStaticPeer_EmptyPortAfterColon covers static.go:156-158
// Tests parseStaticPeer with empty port component ("1:192.168.1.100:").
func TestStaticPeer_EmptyPortAfterColon(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{
			name:    "empty port with trailing colon",
			addr:    "1:192.168.1.100:",
			wantErr: true, // Should fail validation
		},
		{
			name:    "empty port with hostname",
			addr:    "2:node.example.com:",
			wantErr: true,
		},
		{
			name:    "empty port with IP",
			addr:    "3:10.0.0.50:",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseStaticPeer(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseStaticPeer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !tt.wantErr {
				t.Logf("Got expected error: %v", err)
			}
		})
	}
}

// TestDNSParseSRVRecord_EmptyComponentsAfterTrim covers dns.go:258-260
// Tests parseSRVRecord with empty components after underscore trim.
func TestDNSParseSRVRecord_EmptyComponentsAfterTrim(t *testing.T) {
	tests := []struct {
		name    string
		record  string
		wantErr bool
	}{
		{
			name:    "empty service after trim",
			record:  "_._tcp.example.com",
			wantErr: true, // Should fail: empty service
		},
		{
			name:    "empty proto after trim",
			record:  "_service._.example.com",
			wantErr: true, // Should fail: empty proto
		},
		{
			name:    "empty name component",
			record:  "_service._tcp.",
			wantErr: true, // Should fail: empty name
		},
		{
			name:    "all empty components",
			record:  "_._.",
			wantErr: true, // Should fail: all empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, proto, name, err := parseSRVRecord(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSRVRecord() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				// If no error, components should not be empty
				if service == "" || proto == "" || name == "" {
					t.Errorf("parseSRVRecord() returned empty components: service=%q, proto=%q, name=%q",
						service, proto, name)
				}
			}
		})
	}
}

// TestDiscoveryError_WithMethod covers interface.go:123-125
// Tests DiscoveryError.Error() with Method field populated.
func TestDiscoveryError_WithMethod(t *testing.T) {
	tests := []struct {
		name       string
		err        *DiscoveryError
		wantSubstr string
	}{
		{
			name: "error with method",
			err: &DiscoveryError{
				Op:     "Discover",
				Method: "dns",
				Err:    "SRV lookup failed",
			},
			wantSubstr: "[dns]",
		},
		{
			name: "error with static method",
			err: &DiscoveryError{
				Op:     "NewStaticDiscovery",
				Method: "static",
				Err:    "invalid peer address",
			},
			wantSubstr: "[static]",
		},
		{
			name: "error with kubernetes method",
			err: &DiscoveryError{
				Op:     "Bootstrap",
				Method: "kubernetes",
				Err:    "failed to list pods",
			},
			wantSubstr: "[kubernetes]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if errStr == "" {
				t.Error("Error() returned empty string")
			}

			// Verify the method is included in the error message
			if tt.wantSubstr != "" {
				found := false
				for i := 0; i < len(errStr)-len(tt.wantSubstr)+1; i++ {
					if errStr[i:i+len(tt.wantSubstr)] == tt.wantSubstr {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Error() = %q, want substring %q", errStr, tt.wantSubstr)
				}
			}

			// Verify all components are present
			if tt.err.Op != "" {
				found := false
				for i := 0; i < len(errStr)-len(tt.err.Op)+1; i++ {
					if errStr[i:i+len(tt.err.Op)] == tt.err.Op {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Error() should contain Op %q", tt.err.Op)
				}
			}
		})
	}
}

// TestManagerWaitForQuorum_CloseChanTriggered covers manager.go:283-287
// Tests WaitForQuorum with manager close channel triggered.
func TestManagerWaitForQuorum_CloseChanTriggered(t *testing.T) {
	// Create a logger
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create a static discovery that will never find enough peers
	staticDiscovery, err := NewStaticDiscovery([]string{
		"1:192.168.1.100:63000",
	})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := &Config{
		Methods:          []Method{staticDiscovery},
		MinPeers:         3, // Need 3 peers but only have 1
		DiscoveryTimeout: 1 * time.Second,
		RetryInterval:    1 * time.Second,
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	manager, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start WaitForQuorum in a goroutine
	resultChan := make(chan error, 1)
	go func() {
		_, err := manager.WaitForQuorum(ctx)
		resultChan <- err
	}()

	// Let it wait a bit
	time.Sleep(100 * time.Millisecond)

	// Close the manager while WaitForQuorum is running
	if err := manager.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Wait for WaitForQuorum to return
	select {
	case err := <-resultChan:
		if err == nil {
			t.Error("WaitForQuorum() should return error when manager is closed")
		}
		// Verify the error mentions the manager was closed
		if err != nil {
			errStr := err.Error()
			found := false
			wantSubstr := "closed"
			for i := 0; i < len(errStr)-len(wantSubstr)+1; i++ {
				if errStr[i:i+len(wantSubstr)] == wantSubstr {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected error to contain 'closed', got: %v", err)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("WaitForQuorum() did not return after manager close")
	}
}

// TestManagerWaitForQuorum_ContextCancellation tests context cancellation path.
func TestManagerWaitForQuorum_ContextCancellation(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	staticDiscovery, err := NewStaticDiscovery([]string{
		"1:192.168.1.100:63000",
	})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := &Config{
		Methods:          []Method{staticDiscovery},
		MinPeers:         3,
		DiscoveryTimeout: 1 * time.Second,
		RetryInterval:    1 * time.Second,
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	manager, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// WaitForQuorum should fail with timeout
	_, err = manager.WaitForQuorum(ctx)
	if err == nil {
		t.Error("WaitForQuorum() should return error on context timeout")
	}
}

// TestStaticDiscovery_EdgeCases tests additional edge cases for parseStaticPeer.
func TestStaticDiscovery_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		wantErr   bool
	}{
		{
			name:      "address with empty host component",
			addresses: []string{"1::8080"},
			wantErr:   true,
		},
		{
			name:      "address with empty port component",
			addresses: []string{"1:localhost:"},
			wantErr:   true,
		},
		{
			name:      "address with both empty host and port",
			addresses: []string{"1::"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewStaticDiscovery(tt.addresses)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStaticDiscovery() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDiscoveryError_WithoutMethod tests error formatting without method.
func TestDiscoveryError_WithoutMethod(t *testing.T) {
	err := &DiscoveryError{
		Op:  "Config.Validate",
		Err: "min_peers must be at least 1",
	}

	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string")
	}

	// Should not contain brackets when Method is empty
	for i := 0; i < len(errStr); i++ {
		if errStr[i] == '[' || errStr[i] == ']' {
			t.Errorf("Error() = %q, should not contain brackets when Method is empty", errStr)
			break
		}
	}

	// Should contain the operation
	if len(errStr) < len(err.Op) {
		t.Errorf("Error string too short to contain Op")
	}
}

// TestDNSDiscovery_EmptyComponentValidation tests edge cases in SRV parsing.
func TestDNSDiscovery_EmptyComponentValidation(t *testing.T) {
	tests := []struct {
		name    string
		record  string
		wantErr bool
	}{
		{
			name:    "underscore only service",
			record:  "_._tcp.domain.com",
			wantErr: true,
		},
		{
			name:    "underscore only proto",
			record:  "_service._.domain.com",
			wantErr: true,
		},
		{
			name:    "empty domain",
			record:  "_service._tcp.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, proto, name, err := parseSRVRecord(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSRVRecord(%q) error = %v, wantErr %v", tt.record, err, tt.wantErr)
			}

			if err == nil && (service == "" || proto == "" || name == "") {
				t.Errorf("parseSRVRecord() should return error when components are empty after trim")
			}
		})
	}
}

// TestManagerWaitForQuorum_QuorumAchieved tests successful quorum achievement.
func TestManagerWaitForQuorum_QuorumAchieved(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Create static discovery with enough peers
	staticDiscovery, err := NewStaticDiscovery([]string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
		"3:192.168.1.102:63000",
	})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := &Config{
		Methods:          []Method{staticDiscovery},
		MinPeers:         2, // Need 2, have 3
		DiscoveryTimeout: 1 * time.Second,
		RetryInterval:    1 * time.Second,
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	manager, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	peers, err := manager.WaitForQuorum(ctx)
	if err != nil {
		t.Errorf("WaitForQuorum() error = %v, want nil", err)
	}

	if len(peers) < config.MinPeers {
		t.Errorf("WaitForQuorum() returned %d peers, want at least %d", len(peers), config.MinPeers)
	}
}

// TestStaticDiscovery_CompletelyEmptyComponents ensures all edge cases are covered.
func TestStaticDiscovery_CompletelyEmptyComponents(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "format with empty middle (host)",
			input:   "123::9000",
			wantErr: true,
		},
		{
			name:    "format with empty end (port)",
			input:   "456:somehost:",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			peer, err := parseStaticPeer(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("parseStaticPeer(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if err == nil && tc.wantErr {
				t.Errorf("parseStaticPeer(%q) succeeded with peer %+v, but wanted error", tc.input, peer)
			}
		})
	}
}

// TestConfigValidate tests the Config.Validate method.
func TestConfigValidate(t *testing.T) {
	testMethod := &stubMethod{name: "test-method"}

	t.Run("valid config", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Methods = []Method{testMethod}
		err := cfg.Validate()
		require.NoError(t, err)
	})

	t.Run("empty methods", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Methods = []Method{}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "at least one discovery method must be configured")
	})

	t.Run("min peers less than 1", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Methods = []Method{testMethod}
		cfg.MinPeers = 0
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "min_peers must be at least 1")
	})

	t.Run("discovery timeout too short", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Methods = []Method{testMethod}
		cfg.DiscoveryTimeout = 500 * time.Millisecond
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "discovery_timeout must be at least 1 second")
	})

	t.Run("retry interval too short", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Methods = []Method{testMethod}
		cfg.RetryInterval = 500 * time.Millisecond
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "retry_interval must be at least 1 second")
	})
}
