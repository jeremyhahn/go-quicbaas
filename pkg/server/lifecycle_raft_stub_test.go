//go:build !integration

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

// lifecycle_raft_stub_test.go covers the BaaS-mode raft stub helpers
// (validateRouting, kvShards) and the lifecycle no-ops (stopBootstrap,
// stopDevCluster). Real Raft mode lives behind a different build tag;
// the BaaS variant is always compiled and must remain correct so the
// lifecycle code path that calls into these helpers stays safe under any
// shard / pin configuration.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRouting_EmptyPinsIsValid(t *testing.T) {
	require.NoError(t, validateRouting([]uint64{1, 2, 3}, nil))
	require.NoError(t, validateRouting([]uint64{1}, map[string]uint64{}))
}

func TestValidateRouting_PinsResolveToConfiguredShards(t *testing.T) {
	err := validateRouting([]uint64{1, 5, 9}, map[string]uint64{
		"workflows": 1,
		"users":     5,
		"docs":      9,
	})
	require.NoError(t, err)
}

func TestValidateRouting_PinReferencingMissingShardFails(t *testing.T) {
	err := validateRouting([]uint64{1, 2}, map[string]uint64{
		"orphan": 99,
	})
	require.Error(t, err)
	var msgErr *ServerMessageError
	require.True(t, errors.As(err, &msgErr))
	assert.Contains(t, msgErr.Message, "orphan")
	assert.Contains(t, msgErr.Message, "99")
}

func TestKVShards_EmptyDefaultsToShard1(t *testing.T) {
	got := kvShards(nil)
	assert.Equal(t, []uint64{1}, got)
}

func TestKVShards_FiltersExcludedShards(t *testing.T) {
	got := kvShards([]uint64{1, 2, 3, 4, 5}, 2, 4)
	assert.Equal(t, []uint64{1, 3, 5}, got)
}

func TestKVShards_AllExcludedFallsBackToShard1(t *testing.T) {
	got := kvShards([]uint64{2, 3}, 2, 3)
	assert.Equal(t, []uint64{1}, got, "all-excluded must fall back to shard 1 so the default KV surface stays usable")
}

func TestKVShards_ZeroExcludeIDsIgnored(t *testing.T) {
	// Zero-valued excludes (representing "no exclusion") must not knock
	// real shards out of the list.
	got := kvShards([]uint64{1, 2, 3}, 0, 0)
	assert.Equal(t, []uint64{1, 2, 3}, got)
}

func TestServer_StopBootstrapAndDevClusterAreNoOps(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.stopBootstrap())
	require.NoError(t, srv.stopDevCluster(context.Background()))
}
