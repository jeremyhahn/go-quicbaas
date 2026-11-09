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

package iam

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingABACSync is an ABACSync whose reload signals entered and then
// blocks until release is closed, recording the most reloads it saw in
// flight at once.
type blockingABACSync struct {
	entered  chan struct{}
	release  chan struct{}
	inFlight atomic.Int32
	maxSeen  atomic.Int32
}

func newBlockingABACSync() *blockingABACSync {
	return &blockingABACSync{entered: make(chan struct{}, 2), release: make(chan struct{})}
}

func (b *blockingABACSync) Sync(context.Context) error {
	n := b.inFlight.Add(1)
	defer b.inFlight.Add(-1)
	for {
		seen := b.maxSeen.Load()
		if n <= seen || b.maxSeen.CompareAndSwap(seen, n) {
			break
		}
	}
	b.entered <- struct{}{}
	<-b.release
	return nil
}

// TestPolicyEngine_SyncABACSerializesReloads pins that two concurrent ABAC
// reloads never overlap, so an older snapshot cannot be stored over a newer one.
func TestPolicyEngine_SyncABACSerializesReloads(t *testing.T) {
	t.Run("SyncABAC", func(t *testing.T) {
		fake := newBlockingABACSync()
		engine := NewPolicyEngine(PolicyEngineOptions{ABACSync: fake})
		ctx := context.Background()

		errs := make(chan error, 2)
		for range 2 {
			go func() { errs <- engine.SyncABAC(ctx) }()
		}

		<-fake.entered
		select {
		case <-fake.entered:
			t.Fatal("a second reload entered while the first was still running")
		case <-time.After(100 * time.Millisecond):
		}
		close(fake.release)
		for range 2 {
			require.NoError(t, <-errs)
		}
		assert.Equal(t, int32(1), fake.maxSeen.Load())
	})

	t.Run("SyncABACAfter", func(t *testing.T) {
		fake := newBlockingABACSync()
		engine := NewPolicyEngine(PolicyEngineOptions{ABACSync: fake})
		ctx := context.Background()

		var prepared atomic.Bool
		errs := make(chan error, 2)
		go func() { errs <- engine.SyncABAC(ctx) }()
		<-fake.entered
		go func() {
			errs <- engine.SyncABACAfter(ctx, func(context.Context) {
				prepared.Store(true)
			})
		}()

		select {
		case <-fake.entered:
			t.Fatal("a second reload entered while the first was still running")
		case <-time.After(100 * time.Millisecond):
		}
		assert.False(t, prepared.Load(), "prepare ran while another reload held the lock")
		close(fake.release)
		<-fake.entered
		assert.True(t, prepared.Load(), "prepare must run before its reload enters")
		for range 2 {
			require.NoError(t, <-errs)
		}
		assert.Equal(t, int32(1), fake.maxSeen.Load())
	})
}
