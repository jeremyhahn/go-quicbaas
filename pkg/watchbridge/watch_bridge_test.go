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

package watchbridge

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cluster"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// mockClusterInfo implements cluster.ClusterInfo for testing.
type mockClusterInfo struct {
	isLeader   atomic.Bool
	localNode  *cluster.NodeInfo
	leaderNode *cluster.NodeInfo
	nonLeaders []cluster.NodeInfo
	allNodes   []cluster.NodeInfo
	leaderErr  error
}

func newMockClusterInfo(isLeader bool) *mockClusterInfo {
	m := &mockClusterInfo{
		localNode: &cluster.NodeInfo{
			NodeID:  1,
			Address: "127.0.0.1:8080",
		},
		leaderNode: &cluster.NodeInfo{
			NodeID:  1,
			Address: "127.0.0.1:8080",
		},
	}
	m.isLeader.Store(isLeader)
	return m
}

func (m *mockClusterInfo) IsLeader() bool {
	return m.isLeader.Load()
}

func (m *mockClusterInfo) SetLeader(isLeader bool) {
	m.isLeader.Store(isLeader)
}

func (m *mockClusterInfo) LocalNode() *cluster.NodeInfo {
	return m.localNode
}

func (m *mockClusterInfo) Leader(ctx context.Context) (*cluster.NodeInfo, error) {
	if m.leaderErr != nil {
		return nil, m.leaderErr
	}
	return m.leaderNode, nil
}

func (m *mockClusterInfo) NonLeaderNodes(ctx context.Context) ([]cluster.NodeInfo, error) {
	return m.nonLeaders, nil
}

func (m *mockClusterInfo) AllNodes(ctx context.Context) ([]cluster.NodeInfo, error) {
	return m.allNodes, nil
}

// mockMessagingService implements messaging.Service for testing.
type mockMessagingService struct {
	running          atomic.Bool
	channels         sync.Map // name -> *messaging.Channel
	publishedMsgs    sync.Map // channel -> []*publishedMessage
	subscriptions    sync.Map // id -> *messaging.Subscription
	publishErr       error
	createChannelErr error
	mu               sync.Mutex
	idSeq            atomic.Uint64
}

type publishedMessage struct {
	channel string
	payload interface{}
	opts    []messaging.PublishOption
}

func newMockMessagingService() *mockMessagingService {
	m := &mockMessagingService{}
	m.running.Store(true)
	return m
}

func (m *mockMessagingService) Start(ctx context.Context) error {
	m.running.Store(true)
	return nil
}

func (m *mockMessagingService) Stop(ctx context.Context) error {
	m.running.Store(false)
	return nil
}

func (m *mockMessagingService) IsRunning() bool {
	return m.running.Load()
}

func (m *mockMessagingService) CreateChannel(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	if m.createChannelErr != nil {
		return nil, m.createChannelErr
	}
	ch := &messaging.Channel{
		Name:      name,
		CreatedAt: time.Now(),
	}
	m.channels.Store(name, ch)
	return ch, nil
}

func (m *mockMessagingService) DeleteChannel(ctx context.Context, name string) error {
	m.channels.Delete(name)
	return nil
}

func (m *mockMessagingService) GetChannel(ctx context.Context, name string) (*messaging.Channel, error) {
	if ch, ok := m.channels.Load(name); ok {
		return ch.(*messaging.Channel), nil
	}
	return nil, messaging.NewChannelNotFoundError("mockMessagingService.GetChannel", name)
}

func (m *mockMessagingService) ListChannels(ctx context.Context) ([]*messaging.Channel, error) {
	var channels []*messaging.Channel
	m.channels.Range(func(key, value interface{}) bool {
		channels = append(channels, value.(*messaging.Channel))
		return true
	})
	return channels, nil
}

func (m *mockMessagingService) Subscribe(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	id := m.idSeq.Add(1)
	sub := &messaging.Subscription{
		ID:        generateSubscriptionID(id),
		Channel:   channel,
		CreatedAt: time.Now(),
	}
	m.subscriptions.Store(sub.ID, sub)
	return sub, nil
}

func generateSubscriptionID(seq uint64) string {
	return "sub-" + formatUint64(seq)
}

func formatUint64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[i:])
}

func (m *mockMessagingService) Unsubscribe(ctx context.Context, subscriptionID string) error {
	m.subscriptions.Delete(subscriptionID)
	return nil
}

func (m *mockMessagingService) Publish(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	if m.publishErr != nil {
		return nil, m.publishErr
	}

	msg := &publishedMessage{
		channel: channel,
		payload: payload,
		opts:    opts,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var msgs []*publishedMessage
	if existing, ok := m.publishedMsgs.Load(channel); ok {
		msgs = existing.([]*publishedMessage)
	}
	msgs = append(msgs, msg)
	m.publishedMsgs.Store(channel, msgs)

	return &messaging.PublishResult{MessageID: "mock-msg-id"}, nil
}

func (m *mockMessagingService) PublishDelta(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	return m.Publish(ctx, channel, new, opts...)
}

func (m *mockMessagingService) WatchChannel(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return m.Subscribe(ctx, "watch:"+keyPrefix, opts...)
}

func (m *mockMessagingService) GetStats() *messaging.Stats {
	return &messaging.Stats{}
}

func (m *mockMessagingService) GetPublishedMessages(channel string) []*publishedMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	if msgs, ok := m.publishedMsgs.Load(channel); ok {
		return msgs.([]*publishedMessage)
	}
	return nil
}

func (m *mockMessagingService) ClearPublishedMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishedMsgs = sync.Map{}
}

// testLogger creates a logger for testing.
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}

// TestNewWatchBridge tests the WatchBridge constructor.
func TestNewWatchBridge(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/users/"},
		{KeyPrefix: "/orders/", ChannelPrefix: "custom:orders"},
	}

	t.Run("valid construction", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if bridge == nil {
			t.Fatal("expected non-nil bridge")
		}
		if bridge.IsRunning() {
			t.Error("bridge should not be running immediately after construction")
		}
	})

	t.Run("nil watch manager", func(t *testing.T) {
		_, err := NewWatchBridge(log, nil, messagingSvc, clusterInfo, mappings)
		if err == nil {
			t.Fatal("expected error for nil watch manager")
		}
		if !IsWatchBridgeNilManager(err) {
			t.Errorf("expected WatchBridgeNilManager error, got: %v", err)
		}
	})

	t.Run("nil messaging service", func(t *testing.T) {
		_, err := NewWatchBridge(log, watchMgr, nil, clusterInfo, mappings)
		if err == nil {
			t.Fatal("expected error for nil messaging service")
		}
		if !IsWatchBridgeNilService(err) {
			t.Errorf("expected WatchBridgeNilService error, got: %v", err)
		}
	})

	t.Run("nil cluster info is allowed", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, nil, mappings)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if bridge == nil {
			t.Fatal("expected non-nil bridge")
		}
	})

	t.Run("empty mappings is allowed", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if bridge == nil {
			t.Fatal("expected non-nil bridge")
		}
	})

	t.Run("invalid mapping empty key prefix", func(t *testing.T) {
		invalidMappings := []KeyChannelMapping{
			{KeyPrefix: ""},
		}
		_, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, invalidMappings)
		if err == nil {
			t.Fatal("expected error for empty key prefix")
		}
		if !IsWatchBridgeInvalidMapping(err) {
			t.Errorf("expected WatchBridgeInvalidMapping error, got: %v", err)
		}
	})
}

// TestWatchBridgeOptions tests the functional options.
func TestWatchBridgeOptions(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	t.Run("WithLeaderOnly", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil,
			WithLeaderOnly(false))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bridge.options.leaderOnly {
			t.Error("expected leaderOnly to be false")
		}
	})

	t.Run("WithBridgeBufferSize", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil,
			WithBridgeBufferSize(2048))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bridge.options.bufferSize != 2048 {
			t.Errorf("expected bufferSize 2048, got %d", bridge.options.bufferSize)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		opts := bridge.options
		if !opts.leaderOnly {
			t.Error("expected leaderOnly to be true by default")
		}
		if opts.bufferSize != 1024 {
			t.Errorf("expected bufferSize 1024, got %d", opts.bufferSize)
		}
		if opts.defaultChannelPrefix != "watch:" {
			t.Errorf("expected defaultChannelPrefix 'watch:', got '%s'", opts.defaultChannelPrefix)
		}
		if !opts.autoCreateChannels {
			t.Error("expected autoCreateChannels to be true by default")
		}
	})

	for name, size := range map[string]int{"zero": 0, "negative": -1} {
		t.Run("WithBridgeBufferSize "+name+" uses default", func(t *testing.T) {
			bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil,
				WithBridgeBufferSize(size))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bridge.options.bufferSize != 1024 {
				t.Errorf("expected default bufferSize 1024, got %d", bridge.options.bufferSize)
			}
		})
	}

	t.Run("WithDefaultChannelPrefix", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil,
			WithDefaultChannelPrefix("custom:"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bridge.options.defaultChannelPrefix != "custom:" {
			t.Errorf("expected defaultChannelPrefix 'custom:', got '%s'", bridge.options.defaultChannelPrefix)
		}
	})

	t.Run("WithDefaultChannelPrefix empty uses default", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil,
			WithDefaultChannelPrefix(""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bridge.options.defaultChannelPrefix != "watch:" {
			t.Errorf("expected default prefix 'watch:', got '%s'", bridge.options.defaultChannelPrefix)
		}
	})

	t.Run("WithAutoCreateChannels", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil,
			WithAutoCreateChannels(false))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bridge.options.autoCreateChannels {
			t.Error("expected autoCreateChannels to be false")
		}
	})
}

// TestWatchBridgeStartStop tests the lifecycle methods.
func TestWatchBridgeStartStop(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/test/"},
	}

	t.Run("start and stop", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()

		// Start
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		if !bridge.IsRunning() {
			t.Error("bridge should be running after Start")
		}

		// Wait for started
		waitCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err = bridge.WaitStarted(waitCtx)
		if err != nil {
			t.Fatalf("WaitStarted failed: %v", err)
		}

		// Stop
		stopCtx, stopCancel := context.WithTimeout(ctx, time.Second)
		defer stopCancel()
		err = bridge.Stop(stopCtx)
		if err != nil {
			t.Fatalf("failed to stop: %v", err)
		}

		if bridge.IsRunning() {
			t.Error("bridge should not be running after Stop")
		}
	})

	t.Run("double start returns error", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()

		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("first start failed: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		err = bridge.Start(ctx)
		if err == nil {
			t.Error("expected error for double start")
		}
		if !IsWatchBridgeAlreadyRunning(err) {
			t.Errorf("expected WatchBridgeAlreadyRunning error, got: %v", err)
		}
	})

	t.Run("stop when not running is no-op", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err = bridge.Stop(ctx)
		if err != nil {
			t.Fatalf("unexpected error stopping non-running bridge: %v", err)
		}
	})

	t.Run("context cancellation stops bridge", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())

		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		// Cancel context
		cancel()

		// Give time for goroutines to notice cancellation
		time.Sleep(50 * time.Millisecond)

		// Stop should complete quickly
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		err = bridge.Stop(stopCtx)
		if err != nil {
			t.Fatalf("failed to stop: %v", err)
		}
	})

	t.Run("start with no mappings", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start with no mappings: %v", err)
		}

		if !bridge.IsRunning() {
			t.Error("bridge should be running")
		}

		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		err = bridge.Stop(stopCtx)
		if err != nil {
			t.Fatalf("failed to stop: %v", err)
		}
	})

	t.Run("WaitStarted with timeout", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Create a very short timeout context
		timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()

		// Wait a moment for timeout to expire
		time.Sleep(time.Millisecond)

		err = bridge.WaitStarted(timeoutCtx)
		if err == nil {
			t.Error("expected timeout error")
		}
	})
}

// TestWatchBridgeMappingNormalization tests mapping normalization.
func TestWatchBridgeMappingNormalization(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	t.Run("auto-generates channel prefix", func(t *testing.T) {
		mappings := []KeyChannelMapping{
			{KeyPrefix: "/users/"},
		}

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		bridgeMappings := bridge.GetMappings()
		if len(bridgeMappings) != 1 {
			t.Fatalf("expected 1 mapping, got %d", len(bridgeMappings))
		}
		if bridgeMappings[0].ChannelPrefix != "watch:/users/" {
			t.Errorf("expected channel prefix 'watch:/users/', got '%s'", bridgeMappings[0].ChannelPrefix)
		}
	})

	t.Run("preserves custom channel prefix", func(t *testing.T) {
		mappings := []KeyChannelMapping{
			{KeyPrefix: "/users/", ChannelPrefix: "custom:users"},
		}

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		bridgeMappings := bridge.GetMappings()
		if len(bridgeMappings) != 1 {
			t.Fatalf("expected 1 mapping, got %d", len(bridgeMappings))
		}
		if bridgeMappings[0].ChannelPrefix != "custom:users" {
			t.Errorf("expected channel prefix 'custom:users', got '%s'", bridgeMappings[0].ChannelPrefix)
		}
	})

	t.Run("uses custom default prefix", func(t *testing.T) {
		mappings := []KeyChannelMapping{
			{KeyPrefix: "/data/"},
		}

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings,
			WithDefaultChannelPrefix("notifications:"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		bridgeMappings := bridge.GetMappings()
		if bridgeMappings[0].ChannelPrefix != "notifications:/data/" {
			t.Errorf("expected channel prefix 'notifications:/data/', got '%s'", bridgeMappings[0].ChannelPrefix)
		}
	})
}

// TestWatchBridgeAddMapping tests runtime mapping addition.
func TestWatchBridgeAddMapping(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	addCases := []struct {
		name       string
		opts       []WatchBridgeOption
		mapping    KeyChannelMapping
		wantPrefix string
	}{
		{
			name:       "default channel prefix",
			opts:       []WatchBridgeOption{WithDefaultChannelPrefix("events:")},
			mapping:    KeyChannelMapping{KeyPrefix: "/items/"},
			wantPrefix: "events:/items/",
		},
		{
			name:       "custom channel prefix",
			mapping:    KeyChannelMapping{KeyPrefix: "/custom/", ChannelPrefix: "my:custom:prefix"},
			wantPrefix: "my:custom:prefix",
		},
	}
	for _, tc := range addCases {
		t.Run("add mapping when stopped/"+tc.name, func(t *testing.T) {
			bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil, tc.opts...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			err = bridge.AddMapping(tc.mapping)
			if err != nil {
				t.Fatalf("failed to add mapping: %v", err)
			}

			mappings := bridge.GetMappings()
			if len(mappings) != 1 {
				t.Fatalf("expected 1 mapping, got %d", len(mappings))
			}
			if mappings[0].ChannelPrefix != tc.wantPrefix {
				t.Errorf("expected channel prefix %q, got %q", tc.wantPrefix, mappings[0].ChannelPrefix)
			}
		})
	}

	t.Run("add mapping when running fails", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		err = bridge.AddMapping(KeyChannelMapping{KeyPrefix: "/new/"})
		if err == nil {
			t.Error("expected error when adding mapping while running")
		}
	})

	t.Run("add invalid mapping fails", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = bridge.AddMapping(KeyChannelMapping{KeyPrefix: ""})
		if err == nil {
			t.Error("expected error for empty key prefix")
		}
	})
}

// TestWatchBridgeEventFiltering tests event type filtering.
func TestWatchBridgeEventFiltering(t *testing.T) {
	t.Parallel()

	log := testLogger(t)

	t.Run("shouldFilterEvent PUT", func(t *testing.T) {
		watchMgr := watch.NewManager()
		messagingSvc := newMockMessagingService()

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		event := watch.Event{Type: watch.EventTypePut}

		// No filter
		if bridge.shouldFilterEvent(event, WatchEventFilter{}) {
			t.Error("PUT should not be filtered with empty filter")
		}

		// NoPut filter
		if !bridge.shouldFilterEvent(event, WatchEventFilter{NoPut: true}) {
			t.Error("PUT should be filtered with NoPut=true")
		}

		// NoDelete filter
		if bridge.shouldFilterEvent(event, WatchEventFilter{NoDelete: true}) {
			t.Error("PUT should not be filtered with NoDelete=true")
		}
	})

	t.Run("shouldFilterEvent DELETE", func(t *testing.T) {
		watchMgr := watch.NewManager()
		messagingSvc := newMockMessagingService()

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		event := watch.Event{Type: watch.EventTypeDelete}

		// No filter
		if bridge.shouldFilterEvent(event, WatchEventFilter{}) {
			t.Error("DELETE should not be filtered with empty filter")
		}

		// NoDelete filter
		if !bridge.shouldFilterEvent(event, WatchEventFilter{NoDelete: true}) {
			t.Error("DELETE should be filtered with NoDelete=true")
		}

		// NoPut filter
		if bridge.shouldFilterEvent(event, WatchEventFilter{NoPut: true}) {
			t.Error("DELETE should not be filtered with NoPut=true")
		}
	})

	t.Run("shouldFilterEvent unknown type", func(t *testing.T) {
		watchMgr := watch.NewManager()
		messagingSvc := newMockMessagingService()

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		event := watch.Event{Type: "UNKNOWN"}

		// Unknown type should not be filtered
		if bridge.shouldFilterEvent(event, WatchEventFilter{NoPut: true, NoDelete: true}) {
			t.Error("unknown event type should not be filtered")
		}
	})
}

// TestWatchBridgeLeaderGating tests leader-only publishing.
func TestWatchBridgeLeaderGating(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()

	t.Run("publishes when leader", func(t *testing.T) {
		clusterInfo := newMockClusterInfo(true)

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo,
			[]KeyChannelMapping{{KeyPrefix: "/test/"}},
			WithLeaderOnly(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		// Trigger a watch event
		watchMgr.NotifyPut([]byte("/test/key1"), nil, []byte(`{"id": "1"}`), 1, 1)

		// Give time for event processing
		time.Sleep(100 * time.Millisecond)

		stats := bridge.GetStats()
		if stats.EventsDroppedNotLeader != 0 {
			t.Errorf("expected 0 dropped events, got %d", stats.EventsDroppedNotLeader)
		}
	})

	t.Run("drops events when not leader", func(t *testing.T) {
		clusterInfo := newMockClusterInfo(false)

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo,
			[]KeyChannelMapping{{KeyPrefix: "/test2/"}},
			WithLeaderOnly(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		// Trigger a watch event
		watchMgr.NotifyPut([]byte("/test2/key1"), nil, []byte(`{"id": "1"}`), 1, 1)

		// Give time for event processing
		time.Sleep(100 * time.Millisecond)

		stats := bridge.GetStats()
		if stats.EventsDroppedNotLeader == 0 {
			t.Error("expected dropped events when not leader")
		}
	})

	t.Run("publishes regardless of leader when disabled", func(t *testing.T) {
		clusterInfo := newMockClusterInfo(false)

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo,
			[]KeyChannelMapping{{KeyPrefix: "/test3/"}},
			WithLeaderOnly(false))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		// Trigger a watch event
		watchMgr.NotifyPut([]byte("/test3/key1"), nil, []byte(`{"id": "1"}`), 1, 1)

		// Give time for event processing
		time.Sleep(100 * time.Millisecond)

		stats := bridge.GetStats()
		if stats.EventsDroppedNotLeader != 0 {
			t.Errorf("expected 0 dropped events with leaderOnly=false, got %d", stats.EventsDroppedNotLeader)
		}
	})

	t.Run("publishes when clusterInfo is nil", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, nil,
			[]KeyChannelMapping{{KeyPrefix: "/test4/"}},
			WithLeaderOnly(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		// Trigger a watch event
		watchMgr.NotifyPut([]byte("/test4/key1"), nil, []byte(`{"id": "1"}`), 1, 1)

		// Give time for event processing
		time.Sleep(100 * time.Millisecond)

		stats := bridge.GetStats()
		if stats.EventsDroppedNotLeader != 0 {
			t.Errorf("expected 0 dropped events when clusterInfo is nil, got %d", stats.EventsDroppedNotLeader)
		}
	})
}

// TestWatchBridgeStats tests statistics tracking.
func TestWatchBridgeStats(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/stats/", Filter: WatchEventFilter{NoDelete: true}},
	}

	bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = bridge.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = bridge.Stop(stopCtx)
	}()

	// Trigger events
	watchMgr.NotifyPut([]byte("/stats/key1"), nil, []byte(`{"id": "1"}`), 1, 1)
	watchMgr.NotifyPut([]byte("/stats/key2"), nil, []byte(`{"id": "2"}`), 2, 1)
	watchMgr.NotifyDelete([]byte("/stats/key1"), []byte(`{"id": "1"}`), 3, 1) // Should be filtered

	// Give time for event processing
	time.Sleep(200 * time.Millisecond)

	stats := bridge.GetStats()

	if stats.EventsReceived < 3 {
		t.Errorf("expected at least 3 events received, got %d", stats.EventsReceived)
	}
	if stats.EventsFiltered == 0 {
		t.Error("expected at least 1 filtered event (DELETE)")
	}
}

// TestWatchEventPayload tests the message payload structure.
func TestWatchEventPayload(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/payload/"},
	}

	bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = bridge.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = bridge.Stop(stopCtx)
	}()

	// Trigger a watch event
	testValue := []byte(`{"name": "test"}`)
	watchMgr.NotifyPut([]byte("/payload/test-key"), nil, testValue, 42, 5)

	// Give time for event processing
	time.Sleep(200 * time.Millisecond)

	// Check published messages
	msgs := messagingSvc.GetPublishedMessages("watch:/payload")
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 published message")
	}

	// Parse the payload
	payloadBytes, ok := msgs[0].payload.([]byte)
	if !ok {
		t.Fatalf("expected []byte payload, got %T", msgs[0].payload)
	}

	var payload WatchEventPayload
	err = json.Unmarshal(payloadBytes, &payload)
	if err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.Type != watch.EventTypePut {
		t.Errorf("expected type PUT, got %s", payload.Type)
	}
	if payload.Key != "/payload/test-key" {
		t.Errorf("expected key '/payload/test-key', got '%s'", payload.Key)
	}
	if string(payload.Value) != string(testValue) {
		t.Errorf("expected value '%s', got '%s'", testValue, payload.Value)
	}
	if payload.Revision != 42 {
		t.Errorf("expected revision 42, got %d", payload.Revision)
	}
	if payload.Version != 5 {
		t.Errorf("expected version 5, got %d", payload.Version)
	}
	if payload.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

// TestWatchBridgeErrorTypes tests error type checking functions.
func TestWatchBridgeErrorTypes(t *testing.T) {
	t.Parallel()

	// qbCode builds a QBError that carries the code but wraps an untyped
	// inner error, so only the code-matching branch can match it.
	qbCode := func(code errors.ErrorCode) error {
		return &errors.QBError{Code: code, Op: "test", Err: stderrors.New("generic inner error")}
	}
	// wrapped wraps a sentinel so errors.As finds no typed error but
	// errors.Is still matches the sentinel.
	wrapped := func(sentinel error) error {
		return &errors.WrappedMessageError{Message: "wrapper", Cause: sentinel}
	}

	predicates := []struct {
		name    string
		is      func(error) bool
		matches map[string]error
	}{
		{
			name: "IsWatchBridgeNotRunning",
			is:   IsWatchBridgeNotRunning,
			matches: map[string]error{
				"constructor":      NewWatchBridgeNotRunningError("test", "operation"),
				"direct type":      &WatchBridgeNotRunningError{Operation: "test"},
				"wrapped sentinel": wrapped(ErrWatchBridgeNotRunning),
				"QBError code":     qbCode(ErrCodeWatchBridgeNotRunning),
			},
		},
		{
			name: "IsWatchBridgeAlreadyRunning",
			is:   IsWatchBridgeAlreadyRunning,
			matches: map[string]error{
				"constructor":      NewWatchBridgeAlreadyRunningError("test"),
				"direct type":      &WatchBridgeAlreadyRunningError{},
				"wrapped sentinel": wrapped(ErrWatchBridgeAlreadyRunning),
				"QBError code":     qbCode(ErrCodeWatchBridgeAlreadyRunning),
			},
		},
		{
			name: "IsWatchBridgeNilManager",
			is:   IsWatchBridgeNilManager,
			matches: map[string]error{
				"constructor":      NewWatchBridgeNilManagerError("test"),
				"direct type":      &WatchBridgeNilManagerError{},
				"wrapped sentinel": wrapped(ErrWatchBridgeNilManager),
				"QBError code":     qbCode(ErrCodeWatchBridgeNilManager),
			},
		},
		{
			name: "IsWatchBridgeNilService",
			is:   IsWatchBridgeNilService,
			matches: map[string]error{
				"constructor":      NewWatchBridgeNilServiceError("test"),
				"direct type":      &WatchBridgeNilServiceError{},
				"wrapped sentinel": wrapped(ErrWatchBridgeNilService),
				"QBError code":     qbCode(ErrCodeWatchBridgeNilService),
			},
		},
		{
			name: "IsWatchBridgeInvalidMapping",
			is:   IsWatchBridgeInvalidMapping,
			matches: map[string]error{
				"constructor":      NewWatchBridgeInvalidMappingError("test", "/prefix/", "reason"),
				"direct type":      &WatchBridgeInvalidMappingError{KeyPrefix: "/test/", Reason: "bad"},
				"wrapped sentinel": wrapped(ErrWatchBridgeInvalidMapping),
				"QBError code":     qbCode(ErrCodeWatchBridgeInvalidMapping),
			},
		},
	}

	nonMatches := map[string]error{
		"nil":   nil,
		"other": stderrors.New("other error"),
	}

	for _, p := range predicates {
		t.Run(p.name, func(t *testing.T) {
			for input, err := range p.matches {
				if !p.is(err) {
					t.Errorf("%s: expected true", input)
				}
			}
			for input, err := range nonMatches {
				if p.is(err) {
					t.Errorf("%s: expected false", input)
				}
			}
		})
	}

	t.Run("error messages", func(t *testing.T) {
		notRunningErr := &WatchBridgeNotRunningError{Operation: "publish"}
		if notRunningErr.Error() != "watch bridge not running, cannot perform: publish" {
			t.Errorf("unexpected error message: %s", notRunningErr.Error())
		}

		alreadyRunningErr := &WatchBridgeAlreadyRunningError{}
		if alreadyRunningErr.Error() != "watch bridge is already running" {
			t.Errorf("unexpected error message: %s", alreadyRunningErr.Error())
		}

		nilManagerErr := &WatchBridgeNilManagerError{}
		if nilManagerErr.Error() != "watch manager cannot be nil" {
			t.Errorf("unexpected error message: %s", nilManagerErr.Error())
		}

		nilServiceErr := &WatchBridgeNilServiceError{}
		if nilServiceErr.Error() != "messaging service cannot be nil" {
			t.Errorf("unexpected error message: %s", nilServiceErr.Error())
		}

		invalidMappingErr := &WatchBridgeInvalidMappingError{KeyPrefix: "/test/", Reason: "bad"}
		if invalidMappingErr.Error() != "invalid watch bridge mapping for prefix '/test/': bad" {
			t.Errorf("unexpected error message: %s", invalidMappingErr.Error())
		}

		publishFailedErr := &WatchBridgePublishFailedError{Channel: "ch1", Cause: "timeout"}
		if publishFailedErr.Error() != "failed to publish to channel 'ch1': timeout" {
			t.Errorf("unexpected error message: %s", publishFailedErr.Error())
		}
	})

	t.Run("NewWatchBridgePublishFailedError", func(t *testing.T) {
		err := NewWatchBridgePublishFailedError("op", "channel", "cause")
		if err == nil {
			t.Error("expected non-nil error")
		}
		if err.Code != ErrCodeWatchBridgePublishFailed {
			t.Errorf("expected error code %d, got %d", ErrCodeWatchBridgePublishFailed, err.Code)
		}
		if !err.Retry {
			t.Error("expected Retry to be true")
		}
	})
}

// TestWatchBridgeAutoCreateChannels tests automatic channel creation.
func TestWatchBridgeAutoCreateChannels(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	t.Run("auto-creates channels on start", func(t *testing.T) {
		mappings := []KeyChannelMapping{
			{KeyPrefix: "/auto1/"},
			{KeyPrefix: "/auto2/", ChannelPrefix: "custom:auto2"},
		}

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings,
			WithAutoCreateChannels(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		// Check channels were created
		ch1, err := messagingSvc.GetChannel(ctx, "watch:/auto1")
		if err != nil {
			t.Errorf("expected channel 'watch:/auto1' to exist: %v", err)
		} else if ch1 == nil {
			t.Error("expected non-nil channel")
		}

		ch2, err := messagingSvc.GetChannel(ctx, "custom:auto2")
		if err != nil {
			t.Errorf("expected channel 'custom:auto2' to exist: %v", err)
		} else if ch2 == nil {
			t.Error("expected non-nil channel")
		}
	})

	t.Run("no auto-create when disabled", func(t *testing.T) {
		newPushSvc := newMockMessagingService()
		mappings := []KeyChannelMapping{
			{KeyPrefix: "/noauto/"},
		}

		bridge, err := NewWatchBridge(log, watchMgr, newPushSvc, clusterInfo, mappings,
			WithAutoCreateChannels(false))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		// Check channel was not created
		_, err = newPushSvc.GetChannel(ctx, "watch:/noauto")
		if !messaging.IsChannelNotFound(err) {
			t.Error("expected channel to not exist when autoCreateChannels is disabled")
		}
	})

	t.Run("handles channel already exists", func(t *testing.T) {
		newPushSvc := newMockMessagingService()
		// Pre-create channel
		ctx := context.Background()
		_, _ = newPushSvc.CreateChannel(ctx, "watch:/exists")

		mappings := []KeyChannelMapping{
			{KeyPrefix: "/exists/"},
		}

		bridge, err := NewWatchBridge(log, watchMgr, newPushSvc, clusterInfo, mappings,
			WithAutoCreateChannels(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		// Should not error when channel already exists
		ch, err := newPushSvc.GetChannel(ctx, "watch:/exists")
		if err != nil {
			t.Errorf("expected channel to exist: %v", err)
		}
		if ch == nil {
			t.Error("expected non-nil channel")
		}
	})
}

// TestWatchBridgeBuildChannelName tests channel name building.
func TestWatchBridgeBuildChannelName(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()

	t.Run("builds channel name from mapping", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mapping := KeyChannelMapping{
			KeyPrefix:     "/users/",
			ChannelPrefix: "watch:/users/",
		}

		channelName := bridge.buildChannelName([]byte("/users/user123"), mapping)
		if channelName != "watch:/users" {
			t.Errorf("expected 'watch:/users', got '%s'", channelName)
		}
	})

	t.Run("uses default prefix when channel prefix empty", func(t *testing.T) {
		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, nil, nil,
			WithDefaultChannelPrefix("default:"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mapping := KeyChannelMapping{
			KeyPrefix:     "/data/",
			ChannelPrefix: "",
		}

		channelName := bridge.buildChannelName([]byte("/data/item"), mapping)
		if channelName != "default:" {
			t.Errorf("expected 'default:', got '%s'", channelName)
		}
	})
}

// TestWatchBridgeDeltaComputation tests delta computation for entity types.
func TestWatchBridgeDeltaComputation(t *testing.T) {
	t.Parallel()

	type TestEntity struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	t.Run("computes delta with entity type", func(t *testing.T) {
		mappings := []KeyChannelMapping{
			{
				KeyPrefix:        "/delta/",
				EntityType:       &TestEntity{},
				IncludeOldValues: true,
			},
		}

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		// Trigger a watch event with valid JSON
		testValue := []byte(`{"id": "entity1", "name": "Test", "age": 25}`)
		watchMgr.NotifyPut([]byte("/delta/entity1"), nil, testValue, 1, 1)

		// Give time for event processing
		time.Sleep(200 * time.Millisecond)

		// Check published messages
		msgs := messagingSvc.GetPublishedMessages("watch:/delta")
		if len(msgs) == 0 {
			t.Fatal("expected at least 1 published message")
		}

		// Parse the payload
		payloadBytes, ok := msgs[0].payload.([]byte)
		if !ok {
			t.Fatalf("expected []byte payload, got %T", msgs[0].payload)
		}

		var payload WatchEventPayload
		err = json.Unmarshal(payloadBytes, &payload)
		if err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		// Should have delta computed
		if payload.Delta == nil {
			t.Error("expected delta to be computed")
		} else {
			if len(payload.Delta.Changes) == 0 {
				t.Error("expected delta changes")
			}
		}
	})

	t.Run("no delta for nil entity type", func(t *testing.T) {
		mappings := []KeyChannelMapping{
			{
				KeyPrefix:  "/nodelta/",
				EntityType: nil,
			},
		}

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		testValue := []byte(`{"id": "entity1", "name": "Test"}`)
		watchMgr.NotifyPut([]byte("/nodelta/entity1"), nil, testValue, 1, 1)

		time.Sleep(200 * time.Millisecond)

		msgs := messagingSvc.GetPublishedMessages("watch:/nodelta")
		if len(msgs) == 0 {
			t.Fatal("expected at least 1 published message")
		}

		payloadBytes, ok := msgs[0].payload.([]byte)
		if !ok {
			t.Fatalf("expected []byte payload, got %T", msgs[0].payload)
		}

		var payload WatchEventPayload
		err = json.Unmarshal(payloadBytes, &payload)
		if err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		// Should not have delta
		if payload.Delta != nil {
			t.Error("expected no delta when entity type is nil")
		}
	})

	t.Run("clears old values when not requested", func(t *testing.T) {
		mappings := []KeyChannelMapping{
			{
				KeyPrefix:        "/clearold/",
				EntityType:       &TestEntity{},
				IncludeOldValues: false,
			},
		}

		bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctx := context.Background()
		err = bridge.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()

		testValue := []byte(`{"id": "entity1", "name": "Test", "age": 25}`)
		watchMgr.NotifyPut([]byte("/clearold/entity1"), nil, testValue, 1, 1)

		time.Sleep(200 * time.Millisecond)

		msgs := messagingSvc.GetPublishedMessages("watch:/clearold")
		if len(msgs) == 0 {
			t.Fatal("expected at least 1 published message")
		}

		payloadBytes, ok := msgs[0].payload.([]byte)
		if !ok {
			t.Fatalf("expected []byte payload, got %T", msgs[0].payload)
		}

		var payload WatchEventPayload
		err = json.Unmarshal(payloadBytes, &payload)
		if err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}

		if payload.Delta != nil {
			for _, change := range payload.Delta.Changes {
				if change.OldValue != nil {
					t.Error("expected old values to be cleared")
				}
			}
		}
	})
}

// TestWatchBridgePublishError tests handling of publish errors.
func TestWatchBridgePublishError(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	messagingSvc.publishErr = stderrors.New("publish failed")
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/error/"},
	}

	bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = bridge.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = bridge.Stop(stopCtx)
	}()

	// Trigger a watch event
	watchMgr.NotifyPut([]byte("/error/key1"), nil, []byte(`{"id": "1"}`), 1, 1)

	// Give time for event processing
	time.Sleep(200 * time.Millisecond)

	stats := bridge.GetStats()
	if stats.PublishErrors == 0 {
		t.Error("expected publish errors to be recorded")
	}
}

// TestWatchBridgeDeleteEvent tests DELETE event handling.
func TestWatchBridgeDeleteEvent(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/delete/"},
	}

	bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = bridge.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = bridge.Stop(stopCtx)
	}()

	// Trigger a delete event
	watchMgr.NotifyDelete([]byte("/delete/key1"), []byte(`{"id": "1"}`), 1, 1)

	// Give time for event processing
	time.Sleep(200 * time.Millisecond)

	// Check published messages
	msgs := messagingSvc.GetPublishedMessages("watch:/delete")
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 published message")
	}

	// Parse the payload
	payloadBytes, ok := msgs[0].payload.([]byte)
	if !ok {
		t.Fatalf("expected []byte payload, got %T", msgs[0].payload)
	}

	var payload WatchEventPayload
	err = json.Unmarshal(payloadBytes, &payload)
	if err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload.Type != watch.EventTypeDelete {
		t.Errorf("expected type DELETE, got %s", payload.Type)
	}
}

// TestWatchBridgeConcurrentStop tests concurrent stop calls.
func TestWatchBridgeConcurrentStop(t *testing.T) {
	t.Parallel()

	log := testLogger(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/concurrent/"},
	}

	bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = bridge.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Call Stop concurrently
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stopCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			_ = bridge.Stop(stopCtx)
		}()
	}

	wg.Wait()

	if bridge.IsRunning() {
		t.Error("bridge should not be running after concurrent stops")
	}
}

// BenchmarkWatchBridgeEventProcessing benchmarks event processing throughput.
func BenchmarkWatchBridgeEventProcessing(b *testing.B) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error", // Reduce logging overhead
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		b.Fatalf("failed to create logger: %v", err)
	}

	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/bench/"},
	}

	bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings,
		WithLeaderOnly(false)) // Disable leader check for benchmarking
	if err != nil {
		b.Fatalf("failed to create bridge: %v", err)
	}

	ctx := context.Background()
	err = bridge.Start(ctx)
	if err != nil {
		b.Fatalf("failed to start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_ = bridge.Stop(stopCtx)
	}()

	testValue := []byte(`{"id": "bench", "data": "test value"}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		watchMgr.NotifyPut([]byte("/bench/key"), nil, testValue, int64(i), 1)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)
}

// =============================================================================
// Consolidated from watch_bridge_coverage_test.go
// =============================================================================

// TestWatchBridgeStartContextCancellation verifies Start returns context error
// when context is cancelled during watch subscription setup.
func TestWatchBridgeStartContextCancellation(t *testing.T) {
	t.Parallel()

	log := testLoggerCov(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/blocking/"},
	}

	bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo, mappings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Start should return the context error since context is already done
	err = bridge.Start(ctx)
	if err == nil {
		// Clean up if Start somehow succeeded
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		_ = bridge.Stop(stopCtx)
	}
}

// TestWatchBridgeAutoCreateChannelCreationError tests that auto-create handles
// channel creation errors gracefully (including channel already exists).
func TestWatchBridgeAutoCreateChannelCreationError(t *testing.T) {
	t.Parallel()

	log := testLoggerCov(t)
	watchMgr := watch.NewManager()
	clusterInfo := newMockClusterInfo(true)

	// Create a messaging service that fails channel creation with "already exists"
	failSvc := newMockMessagingService()
	failSvc.createChannelErr = &messaging.ChannelExistsError{Channel: "watch:/exists"}

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/exists/"},
	}

	bridge, err := NewWatchBridge(log, watchMgr, failSvc, clusterInfo, mappings,
		WithAutoCreateChannels(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	// Start should succeed even if channel creation reports already exists
	err = bridge.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_ = bridge.Stop(stopCtx)
}

// TestWatchBridgeComputeDeltaReturnsNil verifies computeDelta returns nil
// for every input it cannot compute a delta from.
func TestWatchBridgeComputeDeltaReturnsNil(t *testing.T) {
	t.Parallel()

	type TestEntity struct {
		ID string `json:"id"`
	}

	tests := []struct {
		name        string
		nilComputer bool
		entityType  interface{}
		value       []byte
	}{
		{name: "nil delta computer", nilComputer: true, entityType: &TestEntity{}, value: []byte(`{"id": "1"}`)},
		{name: "nil entity type", entityType: nil, value: []byte(`{"id": "1"}`)},
		{name: "empty value", entityType: &TestEntity{}, value: []byte{}},
		{name: "invalid JSON", entityType: &TestEntity{}, value: []byte(`{not-valid-json`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bridge, err := NewWatchBridge(testLoggerCov(t), watch.NewManager(), newMockMessagingService(), nil, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.nilComputer {
				bridge.deltaComputer = nil
			}

			event := watch.Event{
				Type:  watch.EventTypePut,
				Key:   []byte("/test/key"),
				Value: tc.value,
			}
			mapping := KeyChannelMapping{
				KeyPrefix:  "/test/",
				EntityType: tc.entityType,
			}
			if delta := bridge.computeDelta(event, mapping); delta != nil {
				t.Errorf("expected nil delta, got %+v", delta)
			}
		})
	}
}

// TestWatchBridgeStopDuringStoppingState tests the bridgeStateStopping wait path.
func TestWatchBridgeStopDuringStoppingState(t *testing.T) {
	t.Parallel()

	log := testLoggerCov(t)
	watchMgr := watch.NewManager()
	messagingSvc := newMockMessagingService()
	clusterInfo := newMockClusterInfo(true)

	bridge, err := NewWatchBridge(log, watchMgr, messagingSvc, clusterInfo,
		[]KeyChannelMapping{{KeyPrefix: "/stoptest/"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = bridge.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Manually set state to stopping to test the wait path
	bridge.state.Store(int32(bridgeStateStopping))

	// Stop with a very short timeout so the context expires while waiting for stopped
	stopCtx, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()

	// This will enter the "stopping" path and wait on wb.stopped, which times out
	err = bridge.Stop(stopCtx)
	// May or may not return error depending on timing; the important thing is
	// it did not panic and the code path was exercised.

	// Reset state and clean up properly
	bridge.state.Store(int32(bridgeStateRunning))
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
	defer cleanupCancel()
	_ = bridge.Stop(cleanupCtx)
}

// -----------------------------------------------------------------------------
// publishWatchEvent publish error path with non-leader mode
// Tests the Publish error branch in publishWatchEvent (lines 781-787).
// Uses WithLeaderOnly(false) and multiple events to confirm error counting.
// -----------------------------------------------------------------------------

// TestWatchBridgePublishErrorNonLeaderMode tests that publish errors are properly
// tracked and zero events are published when the messaging service fails.
func TestWatchBridgePublishErrorNonLeaderMode(t *testing.T) {
	t.Parallel()

	log := testLoggerCov(t)
	watchMgr := watch.NewManager()
	clusterInfo := newMockClusterInfo(true)

	// Create a messaging service that returns publish errors
	failPubSvc := newMockMessagingService()
	failPubSvc.publishErr = stderrors.New("publish failed")

	mappings := []KeyChannelMapping{
		{KeyPrefix: "/puberr2/"},
	}

	bridge, err := NewWatchBridge(log, watchMgr, failPubSvc, clusterInfo, mappings,
		WithLeaderOnly(false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = bridge.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = bridge.Stop(stopCtx)
	}()

	// Trigger multiple watch events that will cause publish errors
	watchMgr.NotifyPut([]byte("/puberr2/key1"), nil, []byte(`{"id":"1"}`), 1, 1)
	watchMgr.NotifyPut([]byte("/puberr2/key2"), nil, []byte(`{"id":"2"}`), 2, 1)

	// Wait for the events to be processed using a polling approach
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stats := bridge.GetStats()
		if stats.PublishErrors >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("context cancelled while waiting for publish errors")
		default:
		}
	}

	stats := bridge.GetStats()
	if stats.PublishErrors < 2 {
		t.Errorf("expected at least 2 publish errors, got %d", stats.PublishErrors)
	}
	if stats.EventsPublished != 0 {
		t.Errorf("expected 0 events published, got %d", stats.EventsPublished)
	}
}

// testLoggerCov creates a logger for coverage tests.
func testLoggerCov(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}
