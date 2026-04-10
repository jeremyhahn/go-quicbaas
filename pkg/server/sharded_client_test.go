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

package server

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestShardedClient_ForShard_Closed(t *testing.T) {
	sc := NewShardedClient(nil)
	_ = sc.Close()
	_, err := sc.ForShard(1)
	if err == nil {
		t.Fatal("expected error from ForShard after Close, got nil")
	}
	if !errors.Is(err, ErrShardedClientClosed) {
		t.Errorf("expected ErrShardedClientClosed, got %v", err)
	}
}

func TestShardedClient_Close_Idempotent(t *testing.T) {
	sc := NewShardedClient(nil)
	err1 := sc.Close()
	err2 := sc.Close()
	if err1 != nil {
		t.Fatalf("first Close returned error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second Close returned error: %v", err2)
	}
}

func TestShardedClient_IsEmbedded_Default(t *testing.T) {
	sc := NewShardedClient(nil)
	defer sc.Close()
	if !sc.IsEmbedded() {
		t.Error("expected isEmbedded() = true for default config")
	}
}

func TestShardedClient_IsEmbedded_ExplicitEmbedded(t *testing.T) {
	cfg := config.QRDBConfig{Transport: config.QRDBTransportEmbedded}
	sc := NewShardedClient(nil, WithTransportConfig(&cfg))
	defer sc.Close()
	if !sc.IsEmbedded() {
		t.Error("expected isEmbedded() = true for explicit embedded config")
	}
}

func TestShardedClient_IsEmbedded_NetworkTransport(t *testing.T) {
	cfg := config.QRDBConfig{
		Transport: config.QRDBTransportGRPC,
		Address:   "localhost:50051",
	}
	sc := NewShardedClient(nil, WithTransportConfig(&cfg))
	defer sc.Close()
	if sc.IsEmbedded() {
		t.Error("expected isEmbedded() = false for gRPC config")
	}
}

func TestShardedClient_WithTransportConfig(t *testing.T) {
	cfg := config.QRDBConfig{
		Transport: config.QRDBTransportREST,
		Address:   "https://localhost:8443",
	}
	sc := NewShardedClient(nil, WithTransportConfig(&cfg))
	defer sc.Close()

	if sc.transportCfg == nil {
		t.Fatal("transportCfg should not be nil")
	}
	if sc.transportCfg.Transport != config.QRDBTransportREST {
		t.Errorf("transportCfg.Transport = %q, want %q", sc.transportCfg.Transport, config.QRDBTransportREST)
	}
	if sc.transportCfg.Address != "https://localhost:8443" {
		t.Errorf("transportCfg.Address = %q, want %q", sc.transportCfg.Address, "https://localhost:8443")
	}
}

func TestShardedClient_ForShard_NetworkMissingAddress(t *testing.T) {
	cfg := config.QRDBConfig{
		Transport: config.QRDBTransportGRPC,
		// No address - should fail validation
	}
	sc := NewShardedClient(nil, WithTransportConfig(&cfg))
	defer sc.Close()

	_, err := sc.ForShard(1)
	if err == nil {
		t.Fatal("expected error for network transport without address, got nil")
	}
	if !errors.Is(err, ErrNetworkTransportMissingAddress) {
		t.Errorf("expected ErrNetworkTransportMissingAddress, got %v", err)
	}
}

func TestShardedClient_ForShard_UnixMissingSocket(t *testing.T) {
	cfg := config.QRDBConfig{
		Transport: config.QRDBTransportUnix,
		// No socket path - should fail validation
	}
	sc := NewShardedClient(nil, WithTransportConfig(&cfg))
	defer sc.Close()

	_, err := sc.ForShard(1)
	if err == nil {
		t.Fatal("expected error for unix transport without socket, got nil")
	}
	if !errors.Is(err, ErrUnixTransportMissingSocket) {
		t.Errorf("expected ErrUnixTransportMissingSocket, got %v", err)
	}
}

func TestShardedClient_ForShard_UnsupportedTransport(t *testing.T) {
	cfg := config.QRDBConfig{
		Transport: "unknown",
		Address:   "localhost:1234",
	}
	sc := NewShardedClient(nil, WithTransportConfig(&cfg))
	defer sc.Close()

	_, err := sc.ForShard(1)
	if err == nil {
		t.Fatal("expected error for unsupported transport, got nil")
	}
	if !errors.Is(err, ErrUnsupportedTransport) {
		t.Errorf("expected ErrUnsupportedTransport, got %v", err)
	}
}

func TestNewShardedClient_NilTransportCfg_DefaultsToEmbedded(t *testing.T) {
	sc := NewShardedClient(nil)
	defer sc.Close()
	if sc.factory == nil {
		t.Fatal("factory should not be nil")
	}
	if !sc.IsEmbedded() {
		t.Error("nil transport config should default to embedded")
	}
}
