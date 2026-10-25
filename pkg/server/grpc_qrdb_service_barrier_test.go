//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
)

// qrdbBarrierServicer returns nil when no qrdb client is wired, so the qrdb
// gRPC barrier handler falls back to codes.Unimplemented.
func TestQRDBBarrierServicer_NilClient(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if s.qrdbBarrierServicer() != nil {
		t.Fatal("qrdbBarrierServicer should be nil without a qrdb client")
	}
}

// With an embedded node, qrdbBarrierServicer passes through the node's own
// barrier servicer — the instance its SDK client and gRPC server use — rather
// than a quicbaas-side adapter.
func TestQRDBBarrierServicer_PassesThroughTheNodeServicer(t *testing.T) {
	node := buildSealedMemoryNode(t)
	s := &Server{logger: buildTestLogger(t), qrdbNode: node}

	want := node.Services().Barrier
	if want == nil {
		t.Fatal("the standalone node exposes no barrier servicer")
	}
	if got := s.qrdbBarrierServicer(); got != want {
		t.Fatalf("qrdbBarrierServicer = %T, want the node's own servicer", got)
	}
}

// With a node, the qrdb servicer accessors resolve the node's service graph.
func TestServer_QRDBAccessors_NodeBacked(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbNode:   node,
		qrdbClient: newNodeBackedShardedClient(node),
	}

	if s.GetQRDBNodeClient() == nil {
		t.Error("GetQRDBNodeClient should be non-nil with a node")
	}
	// KV / Admin servicers come from the node's service graph; they may be nil
	// until services are wired, but the accessor must execute the node-backed
	// branch without panicking.
	_ = s.GetQRDBKVServicer()
	_ = s.GetQRDBAdminServicer()
	_ = s.GetQRDBUserServicer()
	_ = s.GetQRDBUserAuthFactorServicer()
}

// With seal disabled, the bootstrap ceremony client wraps s.ceremonyServices,
// so qrdbBarrierServicer serves that same barrier instance and not the node's.
func TestQRDBBarrierServicer_BootstrapServesTheCeremonyBarrier(t *testing.T) {
	bootstrap := buildSealedMemoryNode(t).Services().Barrier
	if bootstrap == nil {
		t.Fatal("the sealed node exposes no barrier servicer")
	}

	s := &Server{
		logger:                  buildTestLogger(t),
		ceremonyServices:        &CeremonyServices{Barrier: bootstrap},
		bootstrapCeremonyClient: &qrdbsdk.Client{},
	}
	if got := s.qrdbBarrierServicer(); got != bootstrap {
		t.Fatalf("qrdbBarrierServicer = %T, want the bootstrap ceremony barrier", got)
	}
}

// buildSealedMemoryNode boots a seal-enabled memory node. With no passphrase
// it stays in the sealed window, where the node exposes its seal-layer
// services — the barrier servicer among them.
func buildSealedMemoryNode(t *testing.T) *qrdbnode.Node {
	t.Helper()
	cfg := memoryNodeConfig(t)
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"
	node := buildMemoryStandaloneNodeFrom(t, cfg)
	t.Cleanup(func() { _ = node.Close() })
	return node
}
