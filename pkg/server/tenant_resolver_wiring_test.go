//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"testing"
)

// buildHomeRealmDirectory returns nil when no qrdb client is wired: HRD is
// disabled.
func TestBuildHomeRealmDirectory_NilClient(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if s.buildHomeRealmDirectory() != nil {
		t.Fatal("buildHomeRealmDirectory should be nil without a qrdb client")
	}
}

// When the sharded client is closed, ForShard(PlatformShardID) fails, so
// buildHomeRealmDirectory must disable HRD (return nil) rather than panic.
func TestBuildHomeRealmDirectory_ForShardError(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	sc := newNodeBackedShardedClient(node)
	// Closing the sharded client makes every ForShard call fail.
	_ = sc.Close()

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbClient: sc,
	}

	if s.buildHomeRealmDirectory() != nil {
		t.Fatal("buildHomeRealmDirectory must be nil when the platform shard is unavailable")
	}
}

// With a node-backed sharded client, buildHomeRealmDirectory resolves the
// platform shard and returns a live directory.
func TestBuildHomeRealmDirectory_NodeBacked(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:     buildTestLogger(t),
		qrdbClient: newNodeBackedShardedClient(node),
	}

	if s.buildHomeRealmDirectory() == nil {
		t.Fatal("buildHomeRealmDirectory should be non-nil for a node-backed client")
	}
}
