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

// Package messaging provides the push notification and messaging client for
// the QuicBaaS SDK.
//
// The messaging client supports pub/sub operations, delta publishing for
// efficient updates, and channel-based real-time notifications.
//
// # Creating a Channel
//
//	client := messaging.NewClient(transport)
//
//	// Create a persistent channel with history
//	channel, err := client.CreateChannel(ctx, &messaging.CreateChannelRequest{
//	    Name:       "updates:users",
//	    Codec:      messaging.CodecJSON,
//	    Persistent: true,
//	    MaxHistory: 100,
//	    BufferSize: 256,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Subscribing to a Channel
//
//	// Subscribe to receive messages
//	sub, err := client.Subscribe(ctx, &messaging.SubscribeRequest{
//	    Channel:   "updates:users",
//	    SessionID: "session-123",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	subscriptionID := sub.ID
//
// # Publishing Messages
//
//	// Publish a regular message
//	err = client.Publish(ctx, &messaging.PublishRequest{
//	    Channel: "updates:users",
//	    Payload: []byte(`{"user_id": "123", "name": "John"}`),
//	})
//
//	// Publish a delta (only the changes)
//	err = client.PublishDelta(ctx, &messaging.PublishDeltaRequest{
//	    Channel: "updates:users",
//	    Old:     oldUser,
//	    New:     newUser,
//	})
//
// # Watching for Key Changes
//
//	// Watch for changes to keys with a specific prefix
//	sub, err := client.WatchChannel(ctx, &messaging.WatchChannelRequest{
//	    KeyPrefix: "users/",
//	})
//
// # Delta Applier
//
// The DeltaApplier[E] generic type helps apply delta payloads to entities:
//
//	applier := messaging.NewDeltaApplier[User]()
//	updatedUser, err := applier.Apply(user, deltaPayload)
//
// # Message Types
//
// The messaging system supports various message types:
//   - MessageTypeNotification: Push notification from server to client
//   - MessageTypePublish: Publish a message to a channel
//   - MessageTypeSubscribe: Request subscription to a channel
//   - MessageTypeUnsubscribe: Request unsubscription from a channel
//   - MessageTypePing/Pong: Keepalive messages
//
// # Codecs
//
// Supported serialization codecs:
//   - CodecJSON: JSON encoding (default)
//   - CodecMsgpack: MessagePack encoding
//   - CodecProtobuf: Protocol Buffers encoding
package messaging
