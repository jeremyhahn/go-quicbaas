// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"testing"
)

// qrdbBarrierServicer returns nil when no qrdb client is wired, so the qrdb
// gRPC barrier handler falls back to codes.Unimplemented.
func TestQRDBBarrierServicer_NilClient(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if s.qrdbBarrierServicer() != nil {
		t.Fatal("qrdbBarrierServicer should be nil without a qrdb client")
	}
}

// With a node-backed sharded client, qrdbBarrierServicer resolves the platform
// shard's barrier client and returns a live servicer.
func TestQRDBBarrierServicer_NodeBacked(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbClient: newNodeBackedShardedClient(node),
	}

	servicer := s.qrdbBarrierServicer()
	if servicer == nil {
		t.Fatal("qrdbBarrierServicer should be non-nil for a node-backed client")
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

// When the sharded client is closed, ForShard(PlatformShardID) fails, so
// qrdbBarrierServicer must log the unavailability and return nil (rather than
// panicking or returning a dead servicer).
func TestQRDBBarrierServicer_ForShardError(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	sc := newNodeBackedShardedClient(node)
	// Closing the sharded client makes every ForShard call fail with
	// ShardedClientClosedError, exercising the platform-shard-unavailable branch.
	_ = sc.Close()

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbClient: sc,
	}

	if s.qrdbBarrierServicer() != nil {
		t.Fatal("qrdbBarrierServicer must be nil when the platform shard is unavailable")
	}
}
