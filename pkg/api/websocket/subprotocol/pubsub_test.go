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

package subprotocol

import (
	"sync"
	"testing"
)

func TestNewInMemoryPubSubHub(t *testing.T) {
	hub := NewInMemoryPubSubHub()

	if hub == nil {
		t.Fatal("NewInMemoryPubSubHub() returned nil")
	}
	if hub.subscriptions == nil {
		t.Error("subscriptions map should be initialized")
	}
	if hub.callbacks == nil {
		t.Error("callbacks map should be initialized")
	}
	if hub.retained == nil {
		t.Error("retained map should be initialized")
	}
}

func TestInMemoryPubSubHub_Subscribe(t *testing.T) {
	t.Run("single subscription", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		err := hub.Subscribe("client-1", "topic/a", 0)
		if err != nil {
			t.Fatalf("Subscribe() error = %v", err)
		}

		hub.mu.RLock()
		defer hub.mu.RUnlock()

		subs, ok := hub.subscriptions["client-1"]
		if !ok {
			t.Fatal("client-1 subscriptions not found")
		}
		qos, ok := subs["topic/a"]
		if !ok {
			t.Fatal("topic/a subscription not found")
		}
		if qos != 0 {
			t.Errorf("QoS = %d, want 0", qos)
		}
	})

	t.Run("multiple subscriptions same client", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.Subscribe("client-1", "topic/a", 0)
		hub.Subscribe("client-1", "topic/b", 1)
		hub.Subscribe("client-1", "topic/c", 2)

		hub.mu.RLock()
		defer hub.mu.RUnlock()

		subs := hub.subscriptions["client-1"]
		if len(subs) != 3 {
			t.Errorf("subscription count = %d, want 3", len(subs))
		}
	})

	t.Run("overwrite existing subscription QoS", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.Subscribe("client-1", "topic/a", 0)
		hub.Subscribe("client-1", "topic/a", 2)

		hub.mu.RLock()
		defer hub.mu.RUnlock()

		qos := hub.subscriptions["client-1"]["topic/a"]
		if qos != 2 {
			t.Errorf("QoS after overwrite = %d, want 2", qos)
		}
	})

	t.Run("multiple clients", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.Subscribe("client-1", "topic/a", 0)
		hub.Subscribe("client-2", "topic/a", 1)

		hub.mu.RLock()
		defer hub.mu.RUnlock()

		if len(hub.subscriptions) != 2 {
			t.Errorf("client count = %d, want 2", len(hub.subscriptions))
		}
	})
}

func TestInMemoryPubSubHub_Unsubscribe(t *testing.T) {
	t.Run("existing subscription", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		hub.Subscribe("client-1", "topic/a", 0)
		hub.Subscribe("client-1", "topic/b", 1)

		err := hub.Unsubscribe("client-1", "topic/a")
		if err != nil {
			t.Fatalf("Unsubscribe() error = %v", err)
		}

		hub.mu.RLock()
		defer hub.mu.RUnlock()

		if _, ok := hub.subscriptions["client-1"]["topic/a"]; ok {
			t.Error("topic/a should have been removed")
		}
		if _, ok := hub.subscriptions["client-1"]["topic/b"]; !ok {
			t.Error("topic/b should still exist")
		}
	})

	t.Run("nonexistent client", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		err := hub.Unsubscribe("no-such-client", "topic/a")
		if err != nil {
			t.Fatalf("Unsubscribe() on nonexistent client should not error, got %v", err)
		}
	})

	t.Run("nonexistent topic", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()
		hub.Subscribe("client-1", "topic/a", 0)

		err := hub.Unsubscribe("client-1", "topic/nonexistent")
		if err != nil {
			t.Fatalf("Unsubscribe() on nonexistent topic should not error, got %v", err)
		}

		hub.mu.RLock()
		defer hub.mu.RUnlock()

		if _, ok := hub.subscriptions["client-1"]["topic/a"]; !ok {
			t.Error("existing subscription should not be affected")
		}
	})
}

func TestInMemoryPubSubHub_Publish(t *testing.T) {
	t.Run("deliver to matching subscriber", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		received := make(chan struct{}, 1)
		var gotTopic string
		var gotPayload []byte
		var gotQos byte
		var gotRetain bool

		hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {
			gotTopic = topic
			gotPayload = payload
			gotQos = qos
			gotRetain = retain
			received <- struct{}{}
		})
		hub.Subscribe("client-1", "topic/test", 1)

		err := hub.Publish("topic/test", []byte("hello"), 1, false)
		if err != nil {
			t.Fatalf("Publish() error = %v", err)
		}

		<-received

		if gotTopic != "topic/test" {
			t.Errorf("received topic = %q, want %q", gotTopic, "topic/test")
		}
		if string(gotPayload) != "hello" {
			t.Errorf("received payload = %q, want %q", string(gotPayload), "hello")
		}
		if gotQos != 1 {
			t.Errorf("received qos = %d, want 1", gotQos)
		}
		if gotRetain {
			t.Error("received retain = true, want false")
		}
	})

	t.Run("QoS downgrade to subscription QoS", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		received := make(chan byte, 1)

		hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {
			received <- qos
		})
		hub.Subscribe("client-1", "topic/test", 0)

		hub.Publish("topic/test", []byte("msg"), 2, false)

		qos := <-received
		if qos != 0 {
			t.Errorf("effective QoS = %d, want 0 (downgraded from publish QoS 2 to sub QoS 0)", qos)
		}
	})

	t.Run("no matching subscribers", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {
			t.Error("callback should not have been called")
		})
		hub.Subscribe("client-1", "topic/other", 0)

		err := hub.Publish("topic/nomatch", []byte("msg"), 0, false)
		if err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
	})

	t.Run("subscriber with no registered callback", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.Subscribe("client-no-callback", "topic/test", 0)

		err := hub.Publish("topic/test", []byte("msg"), 0, false)
		if err != nil {
			t.Fatalf("Publish() with no callback should not error, got %v", err)
		}
	})

	t.Run("retain stores message", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.Publish("topic/retained", []byte("keep this"), 1, true)

		hub.mu.RLock()
		msg, ok := hub.retained["topic/retained"]
		hub.mu.RUnlock()

		if !ok {
			t.Fatal("retained message not found")
		}
		if string(msg.payload) != "keep this" {
			t.Errorf("retained payload = %q, want %q", string(msg.payload), "keep this")
		}
		if msg.qos != 1 {
			t.Errorf("retained qos = %d, want 1", msg.qos)
		}
	})

	t.Run("retain with empty payload clears retained", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.Publish("topic/retained", []byte("keep"), 0, true)

		hub.mu.RLock()
		_, exists := hub.retained["topic/retained"]
		hub.mu.RUnlock()
		if !exists {
			t.Fatal("retained message should exist before clearing")
		}

		hub.Publish("topic/retained", []byte{}, 0, true)

		hub.mu.RLock()
		_, exists = hub.retained["topic/retained"]
		hub.mu.RUnlock()
		if exists {
			t.Error("retained message should be cleared with empty payload")
		}
	})

	t.Run("deliver to multiple subscribers", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		var mu sync.Mutex
		deliveredTo := make(map[string]bool)
		done := make(chan struct{}, 2)

		hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {
			mu.Lock()
			deliveredTo["client-1"] = true
			mu.Unlock()
			done <- struct{}{}
		})
		hub.Subscribe("client-1", "topic/shared", 0)

		hub.RegisterClient("client-2", func(topic string, payload []byte, qos byte, retain bool) {
			mu.Lock()
			deliveredTo["client-2"] = true
			mu.Unlock()
			done <- struct{}{}
		})
		hub.Subscribe("client-2", "topic/shared", 0)

		hub.Publish("topic/shared", []byte("broadcast"), 0, false)

		<-done
		<-done

		mu.Lock()
		defer mu.Unlock()

		if !deliveredTo["client-1"] {
			t.Error("client-1 should have received the message")
		}
		if !deliveredTo["client-2"] {
			t.Error("client-2 should have received the message")
		}
	})
}

func TestInMemoryPubSubHub_RegisterClient(t *testing.T) {
	t.Run("register callback", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		called := false
		err := hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {
			called = true
		})
		if err != nil {
			t.Fatalf("RegisterClient() error = %v", err)
		}

		hub.mu.RLock()
		cb, ok := hub.callbacks["client-1"]
		hub.mu.RUnlock()

		if !ok {
			t.Fatal("callback not registered")
		}

		cb("test", nil, 0, false)
		if !called {
			t.Error("registered callback was not invoked")
		}
	})

	t.Run("overwrite existing callback", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {})

		secondCalled := false
		hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {
			secondCalled = true
		})

		hub.mu.RLock()
		cb := hub.callbacks["client-1"]
		hub.mu.RUnlock()

		cb("test", nil, 0, false)
		if !secondCalled {
			t.Error("second callback should replace first")
		}
	})
}

func TestInMemoryPubSubHub_UnregisterClient(t *testing.T) {
	t.Run("remove client completely", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {})
		hub.Subscribe("client-1", "topic/a", 0)
		hub.Subscribe("client-1", "topic/b", 1)

		err := hub.UnregisterClient("client-1")
		if err != nil {
			t.Fatalf("UnregisterClient() error = %v", err)
		}

		hub.mu.RLock()
		defer hub.mu.RUnlock()

		if _, ok := hub.subscriptions["client-1"]; ok {
			t.Error("subscriptions should be removed after unregister")
		}
		if _, ok := hub.callbacks["client-1"]; ok {
			t.Error("callback should be removed after unregister")
		}
	})

	t.Run("unregister nonexistent client", func(t *testing.T) {
		hub := NewInMemoryPubSubHub()

		err := hub.UnregisterClient("no-such-client")
		if err != nil {
			t.Fatalf("UnregisterClient() on nonexistent client should not error, got %v", err)
		}
	})
}

func TestTopicMatches(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		topic  string
		want   bool
	}{
		{
			name:   "exact match",
			filter: "topic/a",
			topic:  "topic/a",
			want:   true,
		},
		{
			name:   "no match different topic",
			filter: "topic/a",
			topic:  "topic/b",
			want:   false,
		},
		{
			name:   "single level wildcard match",
			filter: "topic/+/data",
			topic:  "topic/sensor/data",
			want:   true,
		},
		{
			name:   "single level wildcard no match extra levels",
			filter: "topic/+",
			topic:  "topic/a/b",
			want:   false,
		},
		{
			name:   "multi level wildcard match",
			filter: "topic/#",
			topic:  "topic/a/b/c",
			want:   true,
		},
		{
			name:   "multi level wildcard match single level",
			filter: "topic/#",
			topic:  "topic/a",
			want:   true,
		},
		{
			name:   "root multi level wildcard",
			filter: "#",
			topic:  "anything/goes/here",
			want:   true,
		},
		{
			name:   "filter longer than topic",
			filter: "a/b/c",
			topic:  "a/b",
			want:   false,
		},
		{
			name:   "topic longer than filter no wildcard",
			filter: "a/b",
			topic:  "a/b/c",
			want:   false,
		},
		{
			name:   "single segment exact",
			filter: "test",
			topic:  "test",
			want:   true,
		},
		{
			name:   "single segment no match",
			filter: "test",
			topic:  "other",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := topicMatches(tt.filter, tt.topic)
			if got != tt.want {
				t.Errorf("topicMatches(%q, %q) = %v, want %v", tt.filter, tt.topic, got, tt.want)
			}
		})
	}
}

func TestSplitTopic(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		want  []string
	}{
		{
			name:  "empty string",
			topic: "",
			want:  nil,
		},
		{
			name:  "single segment",
			topic: "test",
			want:  []string{"test"},
		},
		{
			name:  "multi segment",
			topic: "a/b/c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "leading slash",
			topic: "/topic",
			want:  []string{"", "topic"},
		},
		{
			name:  "trailing slash",
			topic: "topic/",
			want:  []string{"topic", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitTopic(tt.topic)

			if tt.want == nil && got != nil {
				t.Fatalf("splitTopic(%q) = %v, want nil", tt.topic, got)
			}
			if tt.want != nil && got == nil {
				t.Fatalf("splitTopic(%q) = nil, want %v", tt.topic, tt.want)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("splitTopic(%q) length = %d, want %d", tt.topic, len(got), len(tt.want))
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("splitTopic(%q)[%d] = %q, want %q", tt.topic, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestInMemoryPubSubHub_ConcurrentAccess(t *testing.T) {
	hub := NewInMemoryPubSubHub()

	var wg sync.WaitGroup
	numClients := 10

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			clientID := "client-" + string(rune('A'+idx))

			hub.RegisterClient(clientID, func(topic string, payload []byte, qos byte, retain bool) {})
			hub.Subscribe(clientID, "topic/shared", 0)
			hub.Publish("topic/shared", []byte("data"), 0, false)
			hub.Unsubscribe(clientID, "topic/shared")
			hub.UnregisterClient(clientID)
		}(i)
	}

	wg.Wait()
}

func TestInMemoryPubSubHub_WildcardPublishDelivery(t *testing.T) {
	hub := NewInMemoryPubSubHub()

	received := make(chan string, 2)

	hub.RegisterClient("client-1", func(topic string, payload []byte, qos byte, retain bool) {
		received <- topic
	})
	hub.Subscribe("client-1", "sensors/+/temp", 0)

	hub.Publish("sensors/room1/temp", []byte("22.5"), 0, false)

	topic := <-received
	if topic != "sensors/room1/temp" {
		t.Errorf("received topic = %q, want %q", topic, "sensors/room1/temp")
	}
}

func TestInMemoryPubSubHub_ImplementsPubSubHub(t *testing.T) {
	var _ PubSubHub = (*InMemoryPubSubHub)(nil)
}
