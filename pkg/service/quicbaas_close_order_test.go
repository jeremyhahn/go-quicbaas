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

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// pollOrderKVStore is an in-memory KVStore fake that observes the scheduler
// engine's GetJobsDueForExecution poll loop. Scan signals the first poll via
// a channel (so the test can synchronize without sleeping), tracks in-flight
// scans, and counts any scan that arrives after the qrdb transport closed —
// the exact poll-after-close bug this test pins.
type pollOrderKVStore struct {
	firstPollOnce sync.Once
	firstPoll     chan struct{}

	inFlight                 atomic.Int32
	scans                    atomic.Int64
	scansAfterTransportClose atomic.Int64
	transportClosed          atomic.Bool
}

func newPollOrderKVStore() *pollOrderKVStore {
	return &pollOrderKVStore{firstPoll: make(chan struct{})}
}

func (s *pollOrderKVStore) Put(ctx context.Context, key string, value []byte) error {
	return nil
}

func (s *pollOrderKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, nil
}

func (s *pollOrderKVStore) Delete(ctx context.Context, key string) error {
	return nil
}

func (s *pollOrderKVStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	s.inFlight.Add(1)
	defer s.inFlight.Add(-1)

	s.scans.Add(1)
	if s.transportClosed.Load() {
		s.scansAfterTransportClose.Add(1)
	}
	s.firstPollOnce.Do(func() { close(s.firstPoll) })
	return nil
}

func (s *pollOrderKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}

func (s *pollOrderKVStore) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}

// pollOrderQRDBProvider implements QRDBClientProvider. Its Close records the
// scheduler engine state and the number of in-flight storage polls at the
// exact moment the transport closes, proving the drain-before-close ordering
// inside quicBaaS.Close.
type pollOrderQRDBProvider struct {
	inner *svcBoostMockQRDBProvider
	kv    *pollOrderKVStore

	// engineRunningFn is set by the test after the service is built and
	// before Close is invoked (same goroutine — no race).
	engineRunningFn func() bool

	closed               atomic.Bool
	engineRunningAtClose atomic.Bool
	inFlightAtClose      atomic.Int32
}

func (p *pollOrderQRDBProvider) ForShard(shardID uint64) (*qrdbsdk.Client, error) {
	return p.inner.ForShard(shardID)
}

func (p *pollOrderQRDBProvider) Close() error {
	p.closed.Store(true)
	p.kv.transportClosed.Store(true)
	if p.engineRunningFn != nil {
		p.engineRunningAtClose.Store(p.engineRunningFn())
	}
	p.inFlightAtClose.Store(p.kv.inFlight.Load())
	return p.inner.Close()
}

// TestQuicBaaSClose_SchedulerPollerDrainsBeforeQRDBClientClose proves that
// quicBaaS.Close stops AND drains the scheduler engine's poll loop before it
// closes the qrdb client transport. Regression test for the shutdown leak
// where the GetJobsDueForExecution ticker outlived the storage transport and
// produced a "transport: connection is closed" error storm.
func TestQuicBaaSClose_SchedulerPollerDrainsBeforeQRDBClientClose(t *testing.T) {
	kvStore := newPollOrderKVStore()
	provider := &pollOrderQRDBProvider{
		inner: newSvcBoostMockQRDBProvider(nil, nil),
		kv:    kvStore,
	}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.config.EnableScheduler = true
		b.config.SchedulerTickInterval = time.Millisecond
		b.kvStoreProvider = func(_ uint64) interface{} {
			return kvStore
		}
		b.qrdbClient = provider
	})

	impl := svc.(*quicBaaS)
	require.NotNil(t, impl.schedulerEngine, "scheduler engine must be running for this test")
	provider.engineRunningFn = impl.schedulerEngine.IsRunning

	// Wait (channel-based, no sleeps) until the poll loop has hit storage
	// at least once, proving the poller is live before shutdown begins.
	select {
	case <-kvStore.firstPoll:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler poll loop never reached storage")
	}

	require.NoError(t, svc.Close())

	assert.True(t, provider.closed.Load(),
		"quicBaaS.Close must close the qrdb client")
	assert.False(t, provider.engineRunningAtClose.Load(),
		"scheduler engine must be stopped BEFORE the qrdb client transport closes")
	assert.Zero(t, provider.inFlightAtClose.Load(),
		"no storage poll may be in flight when the transport closes")
	assert.Zero(t, kvStore.scansAfterTransportClose.Load(),
		"no storage poll may occur after the transport closed")
	assert.GreaterOrEqual(t, kvStore.scans.Load(), int64(1),
		"poll loop must have polled storage before shutdown")
}
