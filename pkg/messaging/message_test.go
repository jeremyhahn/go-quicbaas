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
	"time"
)

func TestMessage_Marshal(t *testing.T) {
	t.Parallel()

	t.Run("marshals message to JSON", func(t *testing.T) {
		t.Parallel()
		msg := &Message{
			ID:        "msg-123",
			Type:      MessageTypeNotification,
			Channel:   "test-channel",
			Payload:   []byte(`{"key":"value"}`),
			Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		}

		data, err := msg.Marshal()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(data) == 0 {
			t.Error("expected non-empty JSON")
		}
	})

	t.Run("marshals empty message", func(t *testing.T) {
		t.Parallel()
		msg := &Message{}
		data, err := msg.Marshal()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(data) == 0 {
			t.Error("expected non-empty JSON")
		}
	})
}

func TestMessage_Unmarshal(t *testing.T) {
	t.Parallel()

	t.Run("unmarshals JSON to message", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"id":"msg-456","type":"notification","channel":"my-channel"}`)
		msg := &Message{}

		err := msg.Unmarshal(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msg.ID != "msg-456" {
			t.Errorf("expected ID 'msg-456', got %q", msg.ID)
		}
		if msg.Type != MessageTypeNotification {
			t.Errorf("expected type 'notification', got %q", msg.Type)
		}
		if msg.Channel != "my-channel" {
			t.Errorf("expected channel 'my-channel', got %q", msg.Channel)
		}
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		t.Parallel()
		msg := &Message{}
		err := msg.Unmarshal([]byte("not json"))
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestMessage_Clone(t *testing.T) {
	t.Parallel()

	t.Run("clones message without payload or metadata", func(t *testing.T) {
		t.Parallel()
		original := &Message{
			ID:        "original-id",
			Type:      MessageTypePublish,
			Channel:   "original-channel",
			Source:    "original-source",
			Timestamp: time.Now(),
		}

		clone := original.Clone()

		if clone.ID != original.ID {
			t.Errorf("expected ID %q, got %q", original.ID, clone.ID)
		}
		if clone.Type != original.Type {
			t.Errorf("expected Type %q, got %q", original.Type, clone.Type)
		}
		if clone.Channel != original.Channel {
			t.Errorf("expected Channel %q, got %q", original.Channel, clone.Channel)
		}
		if clone.Source != original.Source {
			t.Errorf("expected Source %q, got %q", original.Source, clone.Source)
		}
		if clone.Payload != nil || clone.Metadata != nil || clone.Dedup != nil {
			t.Error("expected nil payload, metadata and dedup on the clone")
		}
	})

	t.Run("clones message with payload", func(t *testing.T) {
		t.Parallel()
		original := &Message{
			ID:      "msg-with-payload",
			Type:    MessageTypeData,
			Payload: []byte("test payload data"),
		}

		clone := original.Clone()

		if string(clone.Payload) != string(original.Payload) {
			t.Errorf("expected payload %q, got %q", string(original.Payload), string(clone.Payload))
		}

		// Verify deep copy - modifying clone should not affect original
		clone.Payload[0] = 'X'
		if original.Payload[0] == 'X' {
			t.Error("payload was not deep copied")
		}
	})

	t.Run("clones message with metadata", func(t *testing.T) {
		t.Parallel()
		original := &Message{
			ID:   "msg-with-metadata",
			Type: MessageTypeNotification,
			Metadata: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		}

		clone := original.Clone()

		if clone.Metadata["key1"] != "value1" {
			t.Errorf("expected metadata key1='value1', got %q", clone.Metadata["key1"])
		}
		if clone.Metadata["key2"] != "value2" {
			t.Errorf("expected metadata key2='value2', got %q", clone.Metadata["key2"])
		}

		// Verify deep copy - modifying clone should not affect original
		clone.Metadata["key1"] = "modified"
		if original.Metadata["key1"] == "modified" {
			t.Error("metadata was not deep copied")
		}
	})

	t.Run("clones message with dedup info", func(t *testing.T) {
		t.Parallel()
		firstSeen := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
		original := &Message{
			ID:   "msg-with-dedup",
			Type: MessageTypeNotification,
			Dedup: &DedupInfo{
				IsDuplicate: true,
				Count:       5,
				FirstSeen:   firstSeen,
			},
		}

		clone := original.Clone()

		if clone.Dedup == nil {
			t.Fatal("expected non-nil Dedup")
		}
		if clone.Dedup.IsDuplicate != true {
			t.Error("expected IsDuplicate to be true")
		}
		if clone.Dedup.Count != 5 {
			t.Errorf("expected Count 5, got %d", clone.Dedup.Count)
		}
		if !clone.Dedup.FirstSeen.Equal(firstSeen) {
			t.Errorf("expected FirstSeen %v, got %v", firstSeen, clone.Dedup.FirstSeen)
		}

		// Verify deep copy - modifying clone should not affect original
		clone.Dedup.Count = 10
		if original.Dedup.Count == 10 {
			t.Error("dedup info was not deep copied")
		}
	})
}

func TestNewSubscribeMessage(t *testing.T) {
	t.Parallel()

	t.Run("creates subscribe message", func(t *testing.T) {
		t.Parallel()
		msg := NewSubscribeMessage("test-channel")

		if msg.Type != MessageTypeSubscribe {
			t.Errorf("expected type 'subscribe', got %q", msg.Type)
		}
		if msg.Channel != "test-channel" {
			t.Errorf("expected channel 'test-channel', got %q", msg.Channel)
		}
		if msg.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	})
}

func TestNewUnsubscribeMessage(t *testing.T) {
	t.Parallel()

	t.Run("creates unsubscribe message", func(t *testing.T) {
		t.Parallel()
		msg := NewUnsubscribeMessage("test-channel")

		if msg.Type != MessageTypeUnsubscribe {
			t.Errorf("expected type 'unsubscribe', got %q", msg.Type)
		}
		if msg.Channel != "test-channel" {
			t.Errorf("expected channel 'test-channel', got %q", msg.Channel)
		}
		if msg.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	})
}

func TestNewPublishMessage(t *testing.T) {
	t.Parallel()

	t.Run("creates publish message", func(t *testing.T) {
		t.Parallel()
		payload := []byte(`{"data":"test"}`)
		msg := NewPublishMessage("test-channel", payload)

		if msg.Type != MessageTypePublish {
			t.Errorf("expected type 'publish', got %q", msg.Type)
		}
		if msg.Channel != "test-channel" {
			t.Errorf("expected channel 'test-channel', got %q", msg.Channel)
		}
		if string(msg.Payload) != string(payload) {
			t.Errorf("expected payload %q, got %q", string(payload), string(msg.Payload))
		}
		if msg.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	})
}

func TestNewNotificationMessage(t *testing.T) {
	t.Parallel()

	t.Run("creates notification message", func(t *testing.T) {
		t.Parallel()
		payload := []byte(`{"notification":"data"}`)
		msg := NewNotificationMessage("notification-channel", payload)

		if msg.Type != MessageTypeNotification {
			t.Errorf("expected type 'notification', got %q", msg.Type)
		}
		if msg.Channel != "notification-channel" {
			t.Errorf("expected channel 'notification-channel', got %q", msg.Channel)
		}
		if string(msg.Payload) != string(payload) {
			t.Errorf("expected payload %q, got %q", string(payload), string(msg.Payload))
		}
	})
}

func TestNewPingMessage(t *testing.T) {
	t.Parallel()

	t.Run("creates ping message", func(t *testing.T) {
		t.Parallel()
		msg := NewPingMessage()

		if msg.Type != MessageTypePing {
			t.Errorf("expected type 'ping', got %q", msg.Type)
		}
		if msg.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	})
}

func TestNewPongMessage(t *testing.T) {
	t.Parallel()

	t.Run("creates pong message", func(t *testing.T) {
		t.Parallel()
		msg := NewPongMessage()

		if msg.Type != MessageTypePong {
			t.Errorf("expected type 'pong', got %q", msg.Type)
		}
		if msg.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	})
}

func TestNewErrorMessage(t *testing.T) {
	t.Parallel()

	t.Run("creates error message", func(t *testing.T) {
		t.Parallel()
		errMsg := "something went wrong"
		msg := NewErrorMessage(errMsg)

		if msg.Type != MessageTypeError {
			t.Errorf("expected type 'error', got %q", msg.Type)
		}
		if string(msg.Payload) != errMsg {
			t.Errorf("expected payload %q, got %q", errMsg, string(msg.Payload))
		}
		if msg.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	})
}

func TestNewDataMessage(t *testing.T) {
	t.Parallel()

	t.Run("creates data message", func(t *testing.T) {
		t.Parallel()
		payload := []byte(`{"internal":"data"}`)
		msg := NewDataMessage("internal-channel", payload)

		if msg.Type != MessageTypeData {
			t.Errorf("expected type 'data', got %q", msg.Type)
		}
		if msg.Channel != "internal-channel" {
			t.Errorf("expected channel 'internal-channel', got %q", msg.Channel)
		}
		if string(msg.Payload) != string(payload) {
			t.Errorf("expected payload %q, got %q", string(payload), string(msg.Payload))
		}
		if msg.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	})
}

func TestMessageTypeConstants(t *testing.T) {
	t.Parallel()

	t.Run("message types have expected values", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			msgType  MessageType
			expected string
		}{
			{MessageTypeSubscribe, "subscribe"},
			{MessageTypeUnsubscribe, "unsubscribe"},
			{MessageTypePublish, "publish"},
			{MessageTypeNotification, "notification"},
			{MessageTypePing, "ping"},
			{MessageTypePong, "pong"},
			{MessageTypeAck, "ack"},
			{MessageTypeError, "error"},
			{MessageTypeData, "data"},
			{MessageTypeControl, "control"},
			{MessageTypeHeartbeat, "heartbeat"},
		}

		for _, tt := range tests {
			if string(tt.msgType) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.msgType))
			}
		}
	})
}
