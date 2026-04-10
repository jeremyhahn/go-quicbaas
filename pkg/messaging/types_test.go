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

package messaging

import (
	"testing"
)

func TestEventType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		et       EventType
		expected string
	}{
		{
			name:     "EventTypePut returns PUT",
			et:       EventTypePut,
			expected: "PUT",
		},
		{
			name:     "EventTypeDelete returns DELETE",
			et:       EventTypeDelete,
			expected: "DELETE",
		},
		{
			name:     "unknown event type returns UNKNOWN",
			et:       EventType(99),
			expected: "UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.et.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOpType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		op       OpType
		expected string
	}{
		{
			name:     "OpSet returns set",
			op:       OpSet,
			expected: "set",
		},
		{
			name:     "OpDelete returns delete",
			op:       OpDelete,
			expected: "delete",
		},
		{
			name:     "OpAppend returns append",
			op:       OpAppend,
			expected: "append",
		},
		{
			name:     "OpIncrement returns increment",
			op:       OpIncrement,
			expected: "increment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.op.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSubscriptionFilter_MatchesEventType(t *testing.T) {
	t.Parallel()

	t.Run("nil filter matches all event types", func(t *testing.T) {
		t.Parallel()
		var filter *SubscriptionFilter
		if !filter.MatchesEventType(EventTypePut) {
			t.Error("nil filter should match EventTypePut")
		}
		if !filter.MatchesEventType(EventTypeDelete) {
			t.Error("nil filter should match EventTypeDelete")
		}
	})

	t.Run("empty EventTypes matches all event types", func(t *testing.T) {
		t.Parallel()
		filter := &SubscriptionFilter{
			EventTypes: []EventType{},
		}
		if !filter.MatchesEventType(EventTypePut) {
			t.Error("empty EventTypes should match EventTypePut")
		}
		if !filter.MatchesEventType(EventTypeDelete) {
			t.Error("empty EventTypes should match EventTypeDelete")
		}
	})

	t.Run("filter matches specified event types only", func(t *testing.T) {
		t.Parallel()
		filter := &SubscriptionFilter{
			EventTypes: []EventType{EventTypePut},
		}
		if !filter.MatchesEventType(EventTypePut) {
			t.Error("filter should match EventTypePut")
		}
		if filter.MatchesEventType(EventTypeDelete) {
			t.Error("filter should not match EventTypeDelete")
		}
	})

	t.Run("filter with multiple event types", func(t *testing.T) {
		t.Parallel()
		filter := &SubscriptionFilter{
			EventTypes: []EventType{EventTypePut, EventTypeDelete},
		}
		if !filter.MatchesEventType(EventTypePut) {
			t.Error("filter should match EventTypePut")
		}
		if !filter.MatchesEventType(EventTypeDelete) {
			t.Error("filter should match EventTypeDelete")
		}
	})
}

func TestSubscriptionFilter_MatchesKey(t *testing.T) {
	t.Parallel()

	t.Run("nil filter matches all keys", func(t *testing.T) {
		t.Parallel()
		var filter *SubscriptionFilter
		if !filter.MatchesKey("any-key") {
			t.Error("nil filter should match any key")
		}
		if !filter.MatchesKey("") {
			t.Error("nil filter should match empty key")
		}
	})

	t.Run("empty KeyPrefix matches all keys", func(t *testing.T) {
		t.Parallel()
		filter := &SubscriptionFilter{
			KeyPrefix: "",
		}
		if !filter.MatchesKey("any-key") {
			t.Error("empty KeyPrefix should match any key")
		}
		if !filter.MatchesKey("/users/123") {
			t.Error("empty KeyPrefix should match /users/123")
		}
	})

	t.Run("filter matches keys with matching prefix", func(t *testing.T) {
		t.Parallel()
		filter := &SubscriptionFilter{
			KeyPrefix: "/users/",
		}
		if !filter.MatchesKey("/users/123") {
			t.Error("filter should match /users/123")
		}
		if !filter.MatchesKey("/users/abc/def") {
			t.Error("filter should match /users/abc/def")
		}
		if filter.MatchesKey("/orders/123") {
			t.Error("filter should not match /orders/123")
		}
	})

	t.Run("filter does not match shorter keys", func(t *testing.T) {
		t.Parallel()
		filter := &SubscriptionFilter{
			KeyPrefix: "/users/",
		}
		if filter.MatchesKey("/use") {
			t.Error("filter should not match key shorter than prefix")
		}
		if filter.MatchesKey("") {
			t.Error("filter should not match empty key")
		}
	})

	t.Run("filter exact match", func(t *testing.T) {
		t.Parallel()
		filter := &SubscriptionFilter{
			KeyPrefix: "/users/123",
		}
		if !filter.MatchesKey("/users/123") {
			t.Error("filter should match exact key")
		}
		if !filter.MatchesKey("/users/123/details") {
			t.Error("filter should match key with exact prefix")
		}
	})
}

func TestDedupInfo(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		var info DedupInfo
		if info.IsDuplicate {
			t.Error("zero value IsDuplicate should be false")
		}
		if info.Count != 0 {
			t.Errorf("zero value Count should be 0, got %d", info.Count)
		}
	})
}

func TestChannelInfo(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		var info ChannelInfo
		if info.Name != "" {
			t.Error("zero value Name should be empty")
		}
		if info.Codec != "" {
			t.Error("zero value Codec should be empty")
		}
		if info.Subscribers != 0 {
			t.Errorf("zero value Subscribers should be 0, got %d", info.Subscribers)
		}
		if info.Persistent {
			t.Error("zero value Persistent should be false")
		}
		if info.MaxHistory != 0 {
			t.Errorf("zero value MaxHistory should be 0, got %d", info.MaxHistory)
		}
	})
}

func TestSubscription(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		var sub Subscription
		if sub.ID != "" {
			t.Error("zero value ID should be empty")
		}
		if sub.Channel != "" {
			t.Error("zero value Channel should be empty")
		}
		if sub.ClientID != "" {
			t.Error("zero value ClientID should be empty")
		}
		if sub.Filters != nil {
			t.Error("zero value Filters should be nil")
		}
	})
}

func TestDeltaPayload(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		var dp DeltaPayload
		if dp.EntityID != "" {
			t.Error("zero value EntityID should be empty")
		}
		if dp.EntityType != "" {
			t.Error("zero value EntityType should be empty")
		}
		if dp.Version != 0 {
			t.Errorf("zero value Version should be 0, got %d", dp.Version)
		}
		if dp.PrevVersion != 0 {
			t.Errorf("zero value PrevVersion should be 0, got %d", dp.PrevVersion)
		}
		if dp.Changes != nil {
			t.Error("zero value Changes should be nil")
		}
	})
}

func TestFieldChange(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		var fc FieldChange
		if fc.Path != "" {
			t.Error("zero value Path should be empty")
		}
		if fc.Operation != "" {
			t.Errorf("zero value Operation should be empty, got %q", fc.Operation)
		}
		if fc.Value != nil {
			t.Error("zero value Value should be nil")
		}
		if fc.OldValue != nil {
			t.Error("zero value OldValue should be nil")
		}
	})
}

func TestStats(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		var stats Stats
		if stats.TotalChannels != 0 {
			t.Errorf("zero value TotalChannels should be 0, got %d", stats.TotalChannels)
		}
		if stats.TotalSubscriptions != 0 {
			t.Errorf("zero value TotalSubscriptions should be 0, got %d", stats.TotalSubscriptions)
		}
		if stats.MessagesPublished != 0 {
			t.Errorf("zero value MessagesPublished should be 0, got %d", stats.MessagesPublished)
		}
		if stats.MessagesDelivered != 0 {
			t.Errorf("zero value MessagesDelivered should be 0, got %d", stats.MessagesDelivered)
		}
		if stats.DedupHits != 0 {
			t.Errorf("zero value DedupHits should be 0, got %d", stats.DedupHits)
		}
		if stats.DroppedMessages != 0 {
			t.Errorf("zero value DroppedMessages should be 0, got %d", stats.DroppedMessages)
		}
		if stats.BufferUtilization != 0.0 {
			t.Errorf("zero value BufferUtilization should be 0.0, got %f", stats.BufferUtilization)
		}
	})
}

func TestChannelStats(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		var stats ChannelStats
		if stats.Name != "" {
			t.Error("zero value Name should be empty")
		}
		if stats.Subscribers != 0 {
			t.Errorf("zero value Subscribers should be 0, got %d", stats.Subscribers)
		}
		if stats.MessagesPublished != 0 {
			t.Errorf("zero value MessagesPublished should be 0, got %d", stats.MessagesPublished)
		}
		if stats.MessagesInHistory != 0 {
			t.Errorf("zero value MessagesInHistory should be 0, got %d", stats.MessagesInHistory)
		}
	})
}
