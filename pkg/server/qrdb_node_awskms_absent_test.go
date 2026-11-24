//go:build !integration && !awskms

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// These tests assert the not-compiled branch for the awskms seal strategy, so
// they compile only in builds without the awskms tag.

// TestInitStandaloneQRDBNode_HardwareStrategyNotCompiled covers the
// hardware-seal-strategy error branch of initStandaloneQRDBNode: selecting a
// build-tagged hardware strategy (awskms) that is not compiled into the default
// build makes buildHardwareSealStrategies fail, and the node init must surface a
// typed QRDBNodeError wrapping the StrategyNotCompiledError.
func TestInitStandaloneQRDBNode_HardwareStrategyNotCompiled(t *testing.T) {
	// awskms is build-tagged and absent from the default test build, so its
	// factory must NOT be registered — this is the premise that makes the
	// not-compiled branch reachable. (Guarded as a require, never skipped.)
	if _, registered := hardwareStrategyFactories["awskms"]; registered {
		t.Fatal("awskms must not be compiled into the default build for this test")
	}

	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "awskms" // build-tagged, absent from the default build

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.initStandaloneQRDBNode()
	if err == nil {
		t.Fatal("expected a QRDBNodeError for a not-compiled hardware strategy")
	}
	var nodeErr *QRDBNodeError
	if !errors.As(err, &nodeErr) {
		t.Fatalf("expected *QRDBNodeError, got %T: %v", err, err)
	}
	var notCompiled *StrategyNotCompiledError
	if !errors.As(err, &notCompiled) {
		t.Fatalf("QRDBNodeError must wrap a *StrategyNotCompiledError, got %v", err)
	}
}

// TestInitClusteredQRDBNode_HardwareStrategyNotCompiled covers the
// hardware-seal-strategy error branch of initClusteredQRDBNode: as with the
// standalone path, a not-compiled hardware strategy must surface a typed
// QRDBNodeError before any raft host is constructed.
func TestInitClusteredQRDBNode_HardwareStrategyNotCompiled(t *testing.T) {
	if _, registered := hardwareStrategyFactories["awskms"]; registered {
		t.Fatal("awskms must not be compiled into the default build for this test")
	}

	cfg := clusterTestConfig()
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	cfg.Server.Mode = config.ServerModeCluster
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + itoaPort(getFreePort(t))
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "awskms"

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.initClusteredQRDBNode()
	if err == nil {
		t.Fatal("expected a QRDBNodeError for a not-compiled hardware strategy")
	}
	var notCompiled *StrategyNotCompiledError
	if !errors.As(err, &notCompiled) {
		t.Fatalf("clustered init must wrap a *StrategyNotCompiledError, got %v", err)
	}
}
