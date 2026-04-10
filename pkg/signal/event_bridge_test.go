//go:build quicbaas_signal

package signal

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-signal/pkg/messages"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// mockMessagingService implements messaging.Service for testing the EventBridge.
// Only Publish is used by EventBridge; all other methods are stubs.
type mockMessagingService struct {
	published  []mockPublishCall
	publishErr error
}

type mockPublishCall struct {
	channel string
	payload interface{}
}

func (m *mockMessagingService) Publish(_ context.Context, channel string, payload interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
	if m.publishErr != nil {
		return nil, m.publishErr
	}
	m.published = append(m.published, mockPublishCall{channel: channel, payload: payload})
	return &messaging.PublishResult{MessageID: "mock-msg-id"}, nil
}

func (m *mockMessagingService) CreateChannel(_ context.Context, _ string, _ ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	return nil, nil
}

func (m *mockMessagingService) DeleteChannel(_ context.Context, _ string) error {
	return nil
}

func (m *mockMessagingService) GetChannel(_ context.Context, _ string) (*messaging.Channel, error) {
	return nil, nil
}

func (m *mockMessagingService) ListChannels(_ context.Context) ([]*messaging.Channel, error) {
	return nil, nil
}

func (m *mockMessagingService) Subscribe(_ context.Context, _ string, _ ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return nil, nil
}

func (m *mockMessagingService) Unsubscribe(_ context.Context, _ string) error {
	return nil
}

func (m *mockMessagingService) PublishDelta(_ context.Context, _ string, _, _ interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
	return nil, nil
}

func (m *mockMessagingService) WatchChannel(_ context.Context, _ string, _ ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return nil, nil
}

func (m *mockMessagingService) Start(_ context.Context) error {
	return nil
}

func (m *mockMessagingService) Stop(_ context.Context) error {
	return nil
}

func (m *mockMessagingService) IsRunning() bool {
	return true
}

func (m *mockMessagingService) GetStats() *messaging.Stats {
	return &messaging.Stats{}
}

// Compile-time check.
var _ messaging.Service = (*mockMessagingService)(nil)

func newEventBridgeTestLogger(t *testing.T) *logger.Logger {
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

func TestNewEventBridge(t *testing.T) {
	ms := &mockMessagingService{}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	if bridge == nil {
		t.Fatal("expected non-nil EventBridge")
	}
}

func TestNewEventBridge_Fields(t *testing.T) {
	ms := &mockMessagingService{}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	if bridge.messaging == nil {
		t.Fatal("expected messaging service to be set")
	}
	if bridge.logger == nil {
		t.Fatal("expected logger to be set")
	}
}

func TestEventBridge_PublishMessageReceived(t *testing.T) {
	ms := &mockMessagingService{}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	ctx := context.Background()

	envelope := &messages.Envelope{
		Type:            messages.EnvelopeTypeCiphertext,
		DestinationUUID: "dest-uuid",
		Content:         []byte("encrypted-content"),
		Timestamp:       12345,
	}

	if err := bridge.PublishMessageReceived(ctx, "account-123", envelope); err != nil {
		t.Fatalf("PublishMessageReceived failed: %v", err)
	}

	if len(ms.published) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(ms.published))
	}
	if ms.published[0].channel != "signal:messages:account-123" {
		t.Fatalf("expected channel signal:messages:account-123, got %s", ms.published[0].channel)
	}
}

func TestEventBridge_PublishMessageReceived_Error(t *testing.T) {
	publishErr := errors.New("publish failed")
	ms := &mockMessagingService{publishErr: publishErr}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	ctx := context.Background()

	envelope := &messages.Envelope{
		Type:    messages.EnvelopeTypeCiphertext,
		Content: []byte("data"),
	}

	err := bridge.PublishMessageReceived(ctx, "account-456", envelope)
	if err == nil {
		t.Fatal("expected error from PublishMessageReceived")
	}
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publish error, got %v", err)
	}
}

func TestEventBridge_PublishAccountEvent(t *testing.T) {
	ms := &mockMessagingService{}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	ctx := context.Background()

	if err := bridge.PublishAccountEvent(ctx, "registered", "account-789"); err != nil {
		t.Fatalf("PublishAccountEvent failed: %v", err)
	}

	if len(ms.published) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(ms.published))
	}
	if ms.published[0].channel != "signal:accounts:registered" {
		t.Fatalf("expected channel signal:accounts:registered, got %s", ms.published[0].channel)
	}
}

func TestEventBridge_PublishAccountEvent_Deleted(t *testing.T) {
	ms := &mockMessagingService{}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	ctx := context.Background()

	if err := bridge.PublishAccountEvent(ctx, "deleted", "account-del"); err != nil {
		t.Fatalf("PublishAccountEvent failed: %v", err)
	}

	if ms.published[0].channel != "signal:accounts:deleted" {
		t.Fatalf("expected channel signal:accounts:deleted, got %s", ms.published[0].channel)
	}
}

func TestEventBridge_PublishAccountEvent_Error(t *testing.T) {
	publishErr := errors.New("account publish failed")
	ms := &mockMessagingService{publishErr: publishErr}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	ctx := context.Background()

	err := bridge.PublishAccountEvent(ctx, "updated", "account-err")
	if err == nil {
		t.Fatal("expected error from PublishAccountEvent")
	}
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publish error, got %v", err)
	}
}

func TestEventBridge_PublishMessageReceived_ChannelFormat(t *testing.T) {
	ms := &mockMessagingService{}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	ctx := context.Background()

	envelope := &messages.Envelope{Content: []byte("data")}

	if err := bridge.PublishMessageReceived(ctx, "uuid-with-dashes-1234", envelope); err != nil {
		t.Fatalf("PublishMessageReceived failed: %v", err)
	}

	expected := messageChannelPrefix + "uuid-with-dashes-1234"
	if ms.published[0].channel != expected {
		t.Fatalf("expected channel %s, got %s", expected, ms.published[0].channel)
	}
}

func TestEventBridge_PublishAccountEvent_ChannelFormat(t *testing.T) {
	ms := &mockMessagingService{}
	log := newEventBridgeTestLogger(t)
	bridge := NewEventBridge(ms, log)
	ctx := context.Background()

	if err := bridge.PublishAccountEvent(ctx, "registered", "abc"); err != nil {
		t.Fatalf("PublishAccountEvent failed: %v", err)
	}

	expected := accountChannelPrefix + "registered"
	if ms.published[0].channel != expected {
		t.Fatalf("expected channel %s, got %s", expected, ms.published[0].channel)
	}
}
