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

// Package messaging provides a high-performance pub/sub messaging system for QuicBaaS.
//
// The messaging package implements real-time notifications with support for
// multiple codecs, message deduplication, delta payloads, and efficient
// subscription management.
//
// # Architecture
//
//	┌─────────────┐     ┌─────────────┐     ┌─────────────┐
//	│  Publisher  │     │  Publisher  │     │  Publisher  │
//	└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
//	       │                   │                   │
//	       └───────────────────┼───────────────────┘
//	                           │
//	                           ▼
//	                  ┌────────────────────┐
//	                  │  MessagingService  │
//	                  │  - Hub             │
//	                  │  - Channels        │
//	                  │  - Dedup           │
//	                  └────────┬───────────┘
//	                           │
//	       ┌───────────────────┼───────────────────┐
//	       │                   │                   │
//	       ▼                   ▼                   ▼
//	┌──────────────┐   ┌──────────────┐   ┌──────────────┐
//	│ Subscriber 1 │   │ Subscriber 2 │   │ Subscriber N │
//	└──────────────┘   └──────────────┘   └──────────────┘
//
// # Features
//
// Pub/Sub Messaging:
// Publishers send messages to named channels. All subscribers to a channel
// receive the message with guaranteed delivery (within buffer limits).
//
// Multiple Codecs:
// Messages can be serialized using JSON, MessagePack, or Protocol Buffers.
// Each channel can have a default codec, and messages can override it.
//
// Message Deduplication:
// Optional deduplication prevents duplicate message processing using
// configurable time windows and key-based tracking.
//
// Delta Payloads:
// Support for efficient delta updates that only transmit changed fields,
// reducing bandwidth for large entities with small changes.
//
// Watch Integration:
// Channels can be configured to watch QuicBaaS keys and automatically
// publish events when data changes.
//
// # Usage
//
// Basic pub/sub:
//
//	service := messaging.NewPushService(log, storage,
//	    messaging.WithServiceBufferSize(1000),
//	    messaging.WithDefaultCodec("json"),
//	)
//
//	// Create a channel
//	ch, err := service.CreateChannel(ctx, "events",
//	    messaging.WithCodec("msgpack"),
//	    messaging.WithPersistence(true),
//	)
//
//	// Subscribe to channel
//	sub, err := service.Subscribe(ctx, "events",
//	    messaging.WithFilter(&messaging.SubscriptionFilter{
//	        EventTypes: []messaging.EventType{messaging.EventTypePut},
//	    }),
//	)
//
//	// Receive messages
//	for msg := range sub.Messages() {
//	    // Handle message
//	}
//
//	// Publish message
//	err = service.Publish(ctx, "events", payload)
//
// # Delta Mode
//
// Delta mode enables efficient updates for large entities:
//
//	sub, err := service.Subscribe(ctx, "entities",
//	    messaging.WithDeltaMode(true),
//	    messaging.WithIncludeOldValues(true),
//	)
//
//	// Receive delta payloads
//	for msg := range sub.Messages() {
//	    var delta messaging.DeltaPayload
//	    if err := json.Unmarshal(msg.Payload, &delta); err != nil {
//	        continue
//	    }
//	    for _, change := range delta.Changes {
//	        // Apply change.Operation to change.Path with change.Value
//	    }
//	}
//
// # Deduplication
//
// Message deduplication prevents processing duplicates:
//
//	service := messaging.NewPushService(log, storage,
//	    messaging.WithDedup(true),
//	    messaging.WithDedupWindow(5 * time.Minute),
//	)
//
//	// Publish with dedup key
//	err = service.Publish(ctx, "events", payload,
//	    messaging.WithDedupKey("unique-event-id"),
//	)
//
//	// Subscriber can check if message was deduplicated
//	sub, err := service.Subscribe(ctx, "events",
//	    messaging.WithDedupMetadata(true),
//	)
//
// # Performance Characteristics
//
// Message Publishing:
//   - O(N) where N is number of subscribers on channel
//   - Async delivery to prevent blocking publishers
//
// Subscription Management:
//   - O(1) for add/remove operations
//   - O(1) for channel lookup
//
// Deduplication:
//   - O(1) key lookup using map
//   - Periodic cleanup of expired keys
//
// # Thread Safety
//
// All operations are thread-safe and can be called concurrently.
// The implementation uses:
//   - Lock-free channels for message passing
//   - Atomic operations for counters
//   - Fine-grained locking for subscription management
//
// # Error Handling
//
// The package uses typed errors for precise error handling:
//
//	err := service.Publish(ctx, "channel", payload)
//	if errors.Is(err, messaging.ErrChannelNotFound) {
//	    // Handle missing channel
//	}
//	if errors.Is(err, messaging.ErrHubBufferFull) {
//	    // Handle backpressure
//	}
package messaging
