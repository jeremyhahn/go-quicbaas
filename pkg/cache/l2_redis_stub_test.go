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

//go:build !l2_redis

package cache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestRedisStubReturnsNotImplemented verifies the build-tag fallback used when
// the package is compiled without -tags l2_redis: every Redis L2 operation must
// report ErrNotImplemented rather than silently succeeding.
//
// This test asserts stub-only behaviour, so it is gated to the !l2_redis build.
// Compiled with -tags l2_redis it would exercise the real client instead, dial
// 127.0.0.1:6379, and nil-deref on the zero-value receiver.
func TestRedisStubReturnsNotImplemented(t *testing.T) {
	ctx := context.Background()

	cfg := DefaultRedisL2Config()
	assert.Equal(t, "localhost:6379", cfg.Addr)
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(cfg.Validate()))

	c, err := NewRedisL2Cache(ctx, cfg, testLogger(t))
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(err))

	// Methods on a zero-value stub receiver must all report not-implemented
	// (except Close, a no-op, and PoolStats, which returns nil).
	stub := &RedisL2Cache{}
	_, err = stub.Get(ctx, "k")
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(err))
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(stub.Set(ctx, "k", []byte("v"), 0)))
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(stub.Delete(ctx, "k")))
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(stub.Ping(ctx)))
	assert.NoError(t, stub.Close())
	assert.Nil(t, stub.PoolStats())
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(stub.FlushDB(ctx)))
	_, err = stub.DBSize(ctx)
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(err))
}
