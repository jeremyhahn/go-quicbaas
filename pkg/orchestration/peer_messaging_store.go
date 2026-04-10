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
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	// inboxKeyPrefix is the KV key prefix for inbox messages.
	inboxKeyPrefix = "inbox:"

	// dedupKeyPrefix is the KV key prefix for dedup tracking.
	dedupKeyPrefix = "dedup:"

	// subscriberBufferSize is the capacity of per-agent subscriber channels.
	subscriberBufferSize = 100
)

// kvPeerMessagingService is a KV-backed implementation of PeerMessagingService.
// It stores messages as JSON under structured keys and uses dedup keys for
// idempotent delivery. Subscriber notifications are delivered via non-blocking
// channel sends.
type kvPeerMessagingService struct {
	store  interfaces.KVStore
	policy InboxPolicy
	logger interfaces.Logger
	subs   sync.Map // key: "tenantID:agentID" -> value: chan *PeerMessage
}

// NewPeerMessagingService creates a new KV-backed peer messaging service.
func NewPeerMessagingService(store interfaces.KVStore, policy InboxPolicy, logger interfaces.Logger) PeerMessagingService {
	return &kvPeerMessagingService{
		store:  store,
		policy: policy,
		logger: logger,
	}
}

// Send delivers a message to the recipient agent's inbox.
func (s *kvPeerMessagingService) Send(ctx context.Context, msg *PeerMessage) error {
	if err := s.validateMessage(msg); err != nil {
		return err
	}

	// Check message size.
	data, err := json.Marshal(msg)
	if err != nil {
		return &PeerInboxError{Op: "marshal", Cause: err}
	}
	if s.policy.MaxMessageSize > 0 && len(data) > s.policy.MaxMessageSize {
		return &MessageTooLargeError{Size: len(data), Max: s.policy.MaxMessageSize}
	}

	tenantID := s.tenantIDFromMessage(msg)

	// Check dedup key for idempotent delivery.
	dedupKey := s.dedupKey(tenantID, msg.To, msg.ID)
	exists, err := s.store.Exists(ctx, dedupKey)
	if err != nil {
		return &PeerInboxError{Op: "dedup_check", Cause: err}
	}
	if exists {
		return &DuplicateMessageError{TenantID: tenantID, AgentID: msg.To, MsgID: msg.ID}
	}

	// Check inbox capacity.
	size, err := s.InboxSize(ctx, tenantID, msg.To)
	if err != nil {
		return err
	}
	if s.policy.MaxMessages > 0 && size >= s.policy.MaxMessages {
		switch s.policy.OverflowAction {
		case "drop_oldest":
			if err := s.dropOldest(ctx, tenantID, msg.To); err != nil {
				return err
			}
		default:
			// "reject" is the default overflow action.
			return &InboxFullError{
				TenantID: tenantID,
				AgentID:  msg.To,
				Size:     size,
				Max:      s.policy.MaxMessages,
			}
		}
	}

	// Write message to inbox.
	inboxKey := s.inboxKey(tenantID, msg.To, msg.Timestamp, msg.ID)
	if err := s.store.Put(ctx, inboxKey, data); err != nil {
		return &PeerInboxError{Op: "put_message", Cause: err}
	}

	// Write dedup key (empty value).
	if err := s.store.Put(ctx, dedupKey, nil); err != nil {
		return &PeerInboxError{Op: "put_dedup", Cause: err}
	}

	// Notify subscriber (non-blocking).
	s.notifySubscriber(tenantID, msg.To, msg)

	return nil
}

// Poll returns messages in the recipient's inbox with timestamps >= since.
func (s *kvPeerMessagingService) Poll(ctx context.Context, tenantID, agentID string, since int64) ([]*PeerMessage, error) {
	prefix := inboxKeyPrefix + tenantID + ":" + agentID + ":"
	var messages []*PeerMessage

	err := s.store.Scan(ctx, prefix, func(key string, value []byte) error {
		ts, parseErr := s.extractTimestamp(key, tenantID, agentID)
		if parseErr != nil {
			// Skip unparseable keys.
			s.logger.Debugf("peer messaging: skipping unparseable inbox key: %s", key)
			return nil
		}
		if ts < since {
			return nil
		}
		var msg PeerMessage
		if err := json.Unmarshal(value, &msg); err != nil {
			s.logger.Debugf("peer messaging: skipping corrupt inbox entry: %s", key)
			return nil
		}
		messages = append(messages, &msg)
		return nil
	})
	if err != nil {
		return nil, &PeerInboxError{Op: "poll_scan", Cause: err}
	}

	// Sort by timestamp ascending (chronological order).
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].Timestamp == messages[j].Timestamp {
			return messages[i].ID < messages[j].ID
		}
		return messages[i].Timestamp < messages[j].Timestamp
	})

	return messages, nil
}

// Subscribe returns a channel that receives new messages for the agent.
func (s *kvPeerMessagingService) Subscribe(_ context.Context, tenantID, agentID string) (<-chan *PeerMessage, error) {
	if tenantID == "" {
		return nil, &PeerSubscriptionError{TenantID: tenantID, AgentID: agentID, Message: "tenant ID is required"}
	}
	if agentID == "" {
		return nil, &PeerSubscriptionError{TenantID: tenantID, AgentID: agentID, Message: "agent ID is required"}
	}

	ch := make(chan *PeerMessage, subscriberBufferSize)
	subKey := tenantID + ":" + agentID
	s.subs.Store(subKey, ch)

	return ch, nil
}

// Acknowledge removes a message from the agent's inbox. The dedup key is
// retained to prevent redelivery.
func (s *kvPeerMessagingService) Acknowledge(ctx context.Context, tenantID, agentID, msgID string) error {
	prefix := inboxKeyPrefix + tenantID + ":" + agentID + ":"
	var targetKey string

	err := s.store.Scan(ctx, prefix, func(key string, _ []byte) error {
		// Keys are formatted as inbox:{tenant}:{agent}:{timestamp}:{msgID}
		// Extract the msgID from the end of the key.
		if s.extractMsgID(key) == msgID {
			targetKey = key
		}
		return nil
	})
	if err != nil {
		return &PeerInboxError{Op: "ack_scan", Cause: err}
	}

	if targetKey == "" {
		// Message not found; this is idempotent, so return nil.
		return nil
	}

	if err := s.store.Delete(ctx, targetKey); err != nil {
		return &PeerInboxError{Op: "ack_delete", Cause: err}
	}

	return nil
}

// InboxSize returns the number of messages currently in the agent's inbox.
func (s *kvPeerMessagingService) InboxSize(ctx context.Context, tenantID, agentID string) (int, error) {
	prefix := inboxKeyPrefix + tenantID + ":" + agentID + ":"
	keys, err := s.store.List(ctx, prefix)
	if err != nil {
		return 0, &PeerInboxError{Op: "inbox_size", Cause: err}
	}
	return len(keys), nil
}

// =============================================================================
// Internal Helpers
// =============================================================================

// validateMessage checks that all required fields are populated.
func (s *kvPeerMessagingService) validateMessage(msg *PeerMessage) error {
	if msg == nil {
		return &PeerMessageValidationError{Field: "message", Message: "message must not be nil"}
	}
	if msg.ID == "" {
		return &PeerMessageValidationError{Field: "id", Message: "message ID is required"}
	}
	if msg.From == "" {
		return &PeerMessageValidationError{Field: "from", Message: "sender is required"}
	}
	if msg.To == "" {
		return &PeerMessageValidationError{Field: "to", Message: "recipient is required"}
	}
	if msg.Action == "" {
		return &PeerMessageValidationError{Field: "action", Message: "action is required"}
	}
	return nil
}

// tenantIDFromMessage extracts the tenant ID from message metadata.
// Falls back to "default" if not specified.
func (s *kvPeerMessagingService) tenantIDFromMessage(msg *PeerMessage) string {
	if msg.Metadata != nil {
		if tid, ok := msg.Metadata["tenant_id"]; ok && tid != "" {
			return tid
		}
	}
	return "default"
}

// inboxKey builds the KV key for an inbox message.
// Format: inbox:{tenantID}:{agentID}:{timestamp_nanos}:{msgID}.
func (s *kvPeerMessagingService) inboxKey(tenantID, agentID string, timestamp int64, msgID string) string {
	return inboxKeyPrefix + tenantID + ":" + agentID + ":" + strconv.FormatInt(timestamp, 10) + ":" + msgID
}

// dedupKey builds the KV key for dedup tracking.
// Format: dedup:{tenantID}:{agentID}:{msgID}.
func (s *kvPeerMessagingService) dedupKey(tenantID, agentID, msgID string) string {
	return dedupKeyPrefix + tenantID + ":" + agentID + ":" + msgID
}

// extractTimestamp parses the timestamp from an inbox key.
// Key format: inbox:{tenantID}:{agentID}:{timestamp}:{msgID}.
func (s *kvPeerMessagingService) extractTimestamp(key, tenantID, agentID string) (int64, error) {
	// Skip past "inbox:{tenantID}:{agentID}:"
	prefixLen := len(inboxKeyPrefix) + len(tenantID) + 1 + len(agentID) + 1
	if prefixLen >= len(key) {
		return 0, &PeerInboxError{Op: "extract_timestamp", Cause: nil}
	}
	rest := key[prefixLen:]
	// Find the colon separating timestamp from msgID.
	for i := 0; i < len(rest); i++ {
		if rest[i] == ':' {
			return strconv.ParseInt(rest[:i], 10, 64)
		}
	}
	// No colon found; entire rest is the timestamp (shouldn't happen with valid keys).
	return strconv.ParseInt(rest, 10, 64)
}

// extractMsgID returns the message ID from an inbox key.
// Key format: inbox:{tenantID}:{agentID}:{timestamp}:{msgID}.
func (s *kvPeerMessagingService) extractMsgID(key string) string {
	// Walk backwards to find the last colon.
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == ':' {
			return key[i+1:]
		}
	}
	return ""
}

// dropOldest removes the oldest message from the agent's inbox.
func (s *kvPeerMessagingService) dropOldest(ctx context.Context, tenantID, agentID string) error {
	prefix := inboxKeyPrefix + tenantID + ":" + agentID + ":"
	keys, err := s.store.List(ctx, prefix)
	if err != nil {
		return &PeerInboxError{Op: "drop_oldest_list", Cause: err}
	}
	if len(keys) == 0 {
		return nil
	}

	// Sort keys to find the oldest (lowest timestamp).
	sort.Strings(keys)
	oldestKey := keys[0]

	if err := s.store.Delete(ctx, oldestKey); err != nil {
		return &PeerInboxError{Op: "drop_oldest_delete", Cause: err}
	}

	s.logger.Debugf("peer messaging: dropped oldest message from inbox: %s", oldestKey)
	return nil
}

// notifySubscriber sends a message to the subscriber channel via non-blocking send.
func (s *kvPeerMessagingService) notifySubscriber(tenantID, agentID string, msg *PeerMessage) {
	subKey := tenantID + ":" + agentID
	val, ok := s.subs.Load(subKey)
	if !ok {
		return
	}
	ch, ok := val.(chan *PeerMessage)
	if !ok {
		return
	}
	// Non-blocking send: drop notification if subscriber buffer is full.
	select {
	case ch <- msg:
	default:
		s.logger.Debugf("peer messaging: subscriber buffer full, dropping notification for %s", subKey)
	}
}
