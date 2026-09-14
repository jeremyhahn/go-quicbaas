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

package orchestration

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// In-Memory KVStore for Peer Messaging Tests
// =============================================================================

// peerTestKVStore is a simple in-memory KVStore for peer messaging unit tests.
type peerTestKVStore struct {
	data map[string][]byte
	mu   sync.RWMutex
}

func newPeerTestKVStore() *peerTestKVStore {
	return &peerTestKVStore{
		data: make(map[string][]byte),
	}
}

func (m *peerTestKVStore) Put(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value == nil {
		m.data[key] = nil
		return nil
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return nil
}

func (m *peerTestKVStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, &qerrors.QBError{
			Code: qerrors.ErrNotFound,
			Op:   "peerTestKVStore.Get",
			Err:  &qerrors.MessageError{Message: "key not found: " + key},
		}
	}
	if v == nil {
		return nil, nil
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *peerTestKVStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *peerTestKVStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	m.mu.RLock()
	// Collect keys to iterate over sorted (deterministic order).
	var keys []string
	for k := range m.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	m.mu.RUnlock()

	sort.Strings(keys)
	for _, k := range keys {
		m.mu.RLock()
		v := m.data[k]
		m.mu.RUnlock()
		if err := fn(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (m *peerTestKVStore) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *peerTestKVStore) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

// PutIfAbsent and CompareAndDelete are the guarded verbs peer delivery is
// decided by. They are implemented HERE, under this fixture's own lock, rather
// than emulated as Exists-then-Put by the caller: the emulation is the exact
// race the guarded verbs exist to remove, so a fixture that faked it would test
// nothing. A mutex is honest atomicity for a store whose consistency domain is
// one process, which is all a test fixture ever is.
func (m *peerTestKVStore) PutIfAbsent(_ context.Context, key string, value []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.data[key]; exists {
		return false, nil
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return true, nil
}

func (m *peerTestKVStore) CompareAndDelete(_ context.Context, key string, expected []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, exists := m.data[key]
	if !exists || !bytes.Equal(held, expected) {
		return false, nil
	}
	delete(m.data, key)
	return true, nil
}

// =============================================================================
// Test Logger
// =============================================================================

// peerTestLogger implements interfaces.Logger for peer messaging tests.
type peerTestLogger struct{}

func (l *peerTestLogger) Debug(string, ...any)                                {}
func (l *peerTestLogger) Debugf(string, ...interface{})                       {}
func (l *peerTestLogger) Info(string, ...any)                                 {}
func (l *peerTestLogger) Infof(string, ...interface{})                        {}
func (l *peerTestLogger) Warn(string, ...any)                                 {}
func (l *peerTestLogger) Warnf(string, ...interface{})                        {}
func (l *peerTestLogger) Error(string, ...any)                                {}
func (l *peerTestLogger) Errorf(string, ...interface{})                       {}
func (l *peerTestLogger) Fatal(string)                                        {}
func (l *peerTestLogger) Fatalf(string, ...interface{})                       {}
func (l *peerTestLogger) WithField(string, interface{}) interfaces.Logger     { return l }
func (l *peerTestLogger) WithFields(map[string]interface{}) interfaces.Logger { return l }
func (l *peerTestLogger) WithError(error) interfaces.Logger                   { return l }

// =============================================================================
// Test Helpers
// =============================================================================

const testTenantID = "test-tenant"

func newTestPeerService(policy InboxPolicy) (PeerMessagingService, *peerTestKVStore) {
	store := newPeerTestKVStore()
	svc := NewPeerMessagingService(store, policy, &peerTestLogger{})
	return svc, store
}

func defaultPolicy() InboxPolicy {
	return InboxPolicy{
		MaxMessages:    100,
		MaxMessageSize: 10240,
		OverflowAction: "reject",
	}
}

// testMessage creates a PeerMessage with the test tenant in metadata.
func testMessage(id, from, to, action string, ts int64) *PeerMessage {
	return &PeerMessage{
		ID:        id,
		From:      from,
		To:        to,
		Action:    action,
		Channel:   "test-channel",
		Content:   "test content",
		Metadata:  map[string]string{"tenant_id": testTenantID},
		Timestamp: ts,
	}
}

// =============================================================================
// Send Tests
// =============================================================================

func TestPeerMessaging_Send_Success(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	msg := testMessage("msg-1", "agent-a", "agent-b", "advisory", 1000)
	err := svc.Send(ctx, msg)
	require.NoError(t, err)

	// Verify message was written to inbox.
	keys, listErr := store.List(ctx, "inbox:"+testTenantID+":agent-b:")
	require.NoError(t, listErr)
	assert.Len(t, keys, 1)

	// Verify dedup key was written.
	exists, existsErr := store.Exists(ctx, "dedup:"+testTenantID+":agent-b:msg-1")
	require.NoError(t, existsErr)
	assert.True(t, exists)
}

func TestPeerMessaging_Send_DefaultTenant(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	// Message without tenant_id metadata should use "default" tenant.
	msg := &PeerMessage{
		ID:        "msg-default",
		From:      "agent-a",
		To:        "agent-b",
		Action:    "advisory",
		Channel:   "ch",
		Content:   "hello",
		Timestamp: 1000,
	}
	err := svc.Send(ctx, msg)
	require.NoError(t, err)

	keys, listErr := store.List(ctx, "inbox:default:agent-b:")
	require.NoError(t, listErr)
	assert.Len(t, keys, 1)
}

func TestPeerMessaging_Send_MissingFields(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	tests := []struct {
		name  string
		msg   *PeerMessage
		field string
	}{
		{
			name:  "nil message",
			msg:   nil,
			field: "message",
		},
		{
			name:  "missing ID",
			msg:   &PeerMessage{From: "a", To: "b", Action: "advisory"},
			field: "id",
		},
		{
			name:  "missing From",
			msg:   &PeerMessage{ID: "1", To: "b", Action: "advisory"},
			field: "from",
		},
		{
			name:  "missing To",
			msg:   &PeerMessage{ID: "1", From: "a", Action: "advisory"},
			field: "to",
		},
		{
			name:  "missing Action",
			msg:   &PeerMessage{ID: "1", From: "a", To: "b"},
			field: "action",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.Send(ctx, tc.msg)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrPeerMessageValidation))
			var valErr *PeerMessageValidationError
			require.True(t, errors.As(err, &valErr))
			assert.Equal(t, tc.field, valErr.Field)
		})
	}
}

func TestPeerMessaging_Send_MessageTooLarge(t *testing.T) {
	policy := InboxPolicy{
		MaxMessages:    100,
		MaxMessageSize: 50, // Very small limit.
		OverflowAction: "reject",
	}
	svc, _ := newTestPeerService(policy)
	ctx := context.Background()

	msg := testMessage("msg-1", "agent-a", "agent-b", "advisory", 1000)
	msg.Content = "this content is definitely larger than fifty bytes when serialized as json"

	err := svc.Send(ctx, msg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMessageTooLarge))

	var sizeErr *MessageTooLargeError
	require.True(t, errors.As(err, &sizeErr))
	assert.Equal(t, 50, sizeErr.Max)
}

func TestPeerMessaging_Send_InboxFull_Reject(t *testing.T) {
	policy := InboxPolicy{
		MaxMessages:    2,
		MaxMessageSize: 10240,
		OverflowAction: "reject",
	}
	svc, _ := newTestPeerService(policy)
	ctx := context.Background()

	// Fill inbox.
	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "a", "b", "advisory", 1000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-2", "a", "b", "advisory", 2000)))

	// Third message should be rejected.
	err := svc.Send(ctx, testMessage("msg-3", "a", "b", "advisory", 3000))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInboxFull))

	var fullErr *InboxFullError
	require.True(t, errors.As(err, &fullErr))
	assert.Equal(t, 2, fullErr.Max)
}

func TestPeerMessaging_Send_InboxFull_DropOldest(t *testing.T) {
	policy := InboxPolicy{
		MaxMessages:    2,
		MaxMessageSize: 10240,
		OverflowAction: "drop_oldest",
	}
	svc, _ := newTestPeerService(policy)
	ctx := context.Background()

	// Fill inbox.
	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "a", "b", "advisory", 1000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-2", "a", "b", "advisory", 2000)))

	// Third message should succeed, dropping msg-1.
	err := svc.Send(ctx, testMessage("msg-3", "a", "b", "advisory", 3000))
	require.NoError(t, err)

	// Inbox should have 2 messages: msg-2 and msg-3.
	size, sizeErr := svc.InboxSize(ctx, testTenantID, "b")
	require.NoError(t, sizeErr)
	assert.Equal(t, 2, size)

	// Poll should return msg-2 and msg-3.
	messages, pollErr := svc.Poll(ctx, testTenantID, "b", 0)
	require.NoError(t, pollErr)
	require.Len(t, messages, 2)
	assert.Equal(t, "msg-2", messages[0].ID)
	assert.Equal(t, "msg-3", messages[1].ID)
}

func TestPeerMessaging_Send_DuplicateMessage(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	msg := testMessage("msg-dup", "agent-a", "agent-b", "advisory", 1000)
	require.NoError(t, svc.Send(ctx, msg))

	// Send same message ID again.
	msg2 := testMessage("msg-dup", "agent-a", "agent-b", "advisory", 2000)
	err := svc.Send(ctx, msg2)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDuplicateMessage))

	var dupErr *DuplicateMessageError
	require.True(t, errors.As(err, &dupErr))
	assert.Equal(t, "msg-dup", dupErr.MsgID)
}

func TestPeerMessaging_Send_NotifiesSubscriber(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	// Subscribe with testTenantID to match messages sent via testMessage.
	ch, subErr := svc.Subscribe(ctx, testTenantID, "agent-b")
	require.NoError(t, subErr)

	msg := testMessage("msg-notify", "agent-a", "agent-b", "advisory", 1000)
	require.NoError(t, svc.Send(ctx, msg))

	// Subscriber should receive the message.
	received := <-ch
	assert.Equal(t, "msg-notify", received.ID)
	assert.Equal(t, "agent-a", received.From)
}

// =============================================================================
// Poll Tests
// =============================================================================

func TestPeerMessaging_Poll_Success(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "a", "b", "advisory", 1000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-2", "a", "b", "progress", 2000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-3", "a", "b", "finding", 3000)))

	messages, err := svc.Poll(ctx, testTenantID, "b", 0)
	require.NoError(t, err)
	require.Len(t, messages, 3)

	// Verify chronological order.
	assert.Equal(t, "msg-1", messages[0].ID)
	assert.Equal(t, "msg-2", messages[1].ID)
	assert.Equal(t, "msg-3", messages[2].ID)
}

func TestPeerMessaging_Poll_SinceTimestamp(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "a", "b", "advisory", 1000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-2", "a", "b", "progress", 2000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-3", "a", "b", "finding", 3000)))

	// Only messages with timestamp >= 2000.
	messages, err := svc.Poll(ctx, testTenantID, "b", 2000)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, "msg-2", messages[0].ID)
	assert.Equal(t, "msg-3", messages[1].ID)
}

func TestPeerMessaging_Poll_EmptyInbox(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	messages, err := svc.Poll(ctx, "tenant-1", "agent-x", 0)
	require.NoError(t, err)
	assert.Empty(t, messages)
}

// =============================================================================
// Subscribe Tests
// =============================================================================

func TestPeerMessaging_Subscribe_ReceivesMessages(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	ch, err := svc.Subscribe(ctx, testTenantID, "agent-b")
	require.NoError(t, err)
	require.NotNil(t, ch)

	msg := testMessage("msg-sub", "agent-a", "agent-b", "advisory", 5000)
	require.NoError(t, svc.Send(ctx, msg))

	received := <-ch
	assert.Equal(t, "msg-sub", received.ID)
	assert.Equal(t, "advisory", received.Action)
}

func TestPeerMessaging_Subscribe_MissingTenantID(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	_, err := svc.Subscribe(ctx, "", "agent-b")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPeerSubscription))
}

func TestPeerMessaging_Subscribe_MissingAgentID(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	_, err := svc.Subscribe(ctx, "tenant-1", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPeerSubscription))
}

func TestPeerMessaging_Subscribe_ReplacesExisting(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	ch1, err1 := svc.Subscribe(ctx, "default", "agent-b")
	require.NoError(t, err1)

	ch2, err2 := svc.Subscribe(ctx, "default", "agent-b")
	require.NoError(t, err2)

	// New subscription should be a different channel.
	assert.NotEqual(t, ch1, ch2)

	// Send message without tenant metadata; it should go to "default" tenant and ch2.
	msg := &PeerMessage{
		ID:        "msg-replace",
		From:      "agent-a",
		To:        "agent-b",
		Action:    "advisory",
		Channel:   "ch",
		Content:   "hello",
		Timestamp: 1000,
	}
	require.NoError(t, svc.Send(ctx, msg))

	received := <-ch2
	assert.Equal(t, "msg-replace", received.ID)
}

// =============================================================================
// Acknowledge Tests
// =============================================================================

func TestPeerMessaging_Acknowledge_Success(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	msg := testMessage("msg-ack", "a", "b", "advisory", 1000)
	require.NoError(t, svc.Send(ctx, msg))

	// Verify message exists.
	size, sizeErr := svc.InboxSize(ctx, testTenantID, "b")
	require.NoError(t, sizeErr)
	assert.Equal(t, 1, size)

	// Acknowledge.
	err := svc.Acknowledge(ctx, testTenantID, "b", "msg-ack")
	require.NoError(t, err)

	// Verify message removed.
	size, sizeErr = svc.InboxSize(ctx, testTenantID, "b")
	require.NoError(t, sizeErr)
	assert.Equal(t, 0, size)
}

func TestPeerMessaging_Acknowledge_NotFound(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	// Acknowledging a non-existent message should succeed (idempotent).
	err := svc.Acknowledge(ctx, "tenant-1", "agent-b", "nonexistent")
	require.NoError(t, err)
}

func TestPeerMessaging_Acknowledge_DedupRemains(t *testing.T) {
	svc, store := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	msg := testMessage("msg-dedup-stay", "a", "b", "advisory", 1000)
	require.NoError(t, svc.Send(ctx, msg))

	// Acknowledge.
	require.NoError(t, svc.Acknowledge(ctx, testTenantID, "b", "msg-dedup-stay"))

	// Dedup key should still exist.
	exists, err := store.Exists(ctx, "dedup:"+testTenantID+":b:msg-dedup-stay")
	require.NoError(t, err)
	assert.True(t, exists)

	// Re-sending the same message should fail with duplicate.
	msg2 := testMessage("msg-dedup-stay", "a", "b", "advisory", 2000)
	err = svc.Send(ctx, msg2)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDuplicateMessage))
}

// =============================================================================
// InboxSize Tests
// =============================================================================

func TestPeerMessaging_InboxSize_Success(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	require.NoError(t, svc.Send(ctx, testMessage("msg-1", "a", "b", "advisory", 1000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-2", "a", "b", "advisory", 2000)))
	require.NoError(t, svc.Send(ctx, testMessage("msg-3", "a", "b", "advisory", 3000)))

	size, err := svc.InboxSize(ctx, testTenantID, "b")
	require.NoError(t, err)
	assert.Equal(t, 3, size)
}

func TestPeerMessaging_InboxSize_Empty(t *testing.T) {
	svc, _ := newTestPeerService(defaultPolicy())
	ctx := context.Background()

	size, err := svc.InboxSize(ctx, "tenant-1", "agent-x")
	require.NoError(t, err)
	assert.Equal(t, 0, size)
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestPeerMessaging_ErrorTypes_Unwrap(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		sentinel error
	}{
		{
			name:     "InboxFullError",
			err:      &InboxFullError{TenantID: "t", AgentID: "a", Size: 10, Max: 10},
			sentinel: ErrInboxFull,
		},
		{
			name:     "MessageTooLargeError",
			err:      &MessageTooLargeError{Size: 100, Max: 50},
			sentinel: ErrMessageTooLarge,
		},
		{
			name:     "DuplicateMessageError",
			err:      &DuplicateMessageError{TenantID: "t", AgentID: "a", MsgID: "m"},
			sentinel: ErrDuplicateMessage,
		},
		{
			name:     "PeerMessageValidationError",
			err:      &PeerMessageValidationError{Field: "id", Message: "required"},
			sentinel: ErrPeerMessageValidation,
		},
		{
			name:     "PeerInboxError",
			err:      &PeerInboxError{Op: "test", Cause: nil},
			sentinel: ErrPeerInbox,
		},
		{
			name:     "PeerSubscriptionError",
			err:      &PeerSubscriptionError{TenantID: "t", AgentID: "a", Message: "fail"},
			sentinel: ErrPeerSubscription,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, errors.Is(tc.err, tc.sentinel))
			assert.NotEmpty(t, tc.err.Error())
		})
	}
}

func TestPeerMessaging_PeerInboxError_WithCause(t *testing.T) {
	cause := errors.New("underlying error")
	err := &PeerInboxError{Op: "test_op", Cause: cause}
	assert.Contains(t, err.Error(), "underlying error")
	assert.Contains(t, err.Error(), "test_op")
	assert.True(t, errors.Is(err, ErrPeerInbox))
}

func TestPeerMessaging_PeerInboxError_NilCause(t *testing.T) {
	err := &PeerInboxError{Op: "test_op"}
	assert.Contains(t, err.Error(), "test_op")
	assert.NotContains(t, err.Error(), "<nil>")
}
