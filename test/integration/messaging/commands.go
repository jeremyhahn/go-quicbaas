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

//go:build integration

package messaging

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
)

// MessagingCommands defines the interface for messaging operations.
// This interface abstracts the different protocols for multi-protocol testing.
type MessagingCommands interface {
	// Name returns the protocol name for logging.
	Name() string

	// Setup initializes the protocol client.
	Setup(ctx context.Context) error

	// Teardown cleans up the protocol client.
	Teardown() error

	// Channel operations
	CreateChannel(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error)
	DeleteChannel(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error)
	GetChannel(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error)
	ListChannels(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error)

	// Subscription operations
	Subscribe(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error)
	Unsubscribe(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error)

	// Publish operations
	Publish(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error)
	PublishDelta(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error)

	// Stats operations
	GetChannelStats(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error)
	GetDedupStats(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error)
	GetStats(ctx context.Context) (*grpc.MessagingGetStatsResponse, error)
}

// ChannelConfig holds channel configuration for testing.
type ChannelConfig struct {
	Name           string
	BufferSize     int32
	MaxSubscribers int32
	Persistent     bool
	TTLSeconds     int64
}

// DefaultChannelConfig returns a default channel configuration for testing.
func DefaultChannelConfig(name string) *ChannelConfig {
	return &ChannelConfig{
		Name:           name,
		BufferSize:     256,
		MaxSubscribers: 1000,
		Persistent:     false,
		TTLSeconds:     0,
	}
}

// ToCreateRequest converts ChannelConfig to a CreateChannelRequest.
func (c *ChannelConfig) ToCreateRequest() *grpc.MessagingCreateChannelRequest {
	return &grpc.MessagingCreateChannelRequest{
		Name:           c.Name,
		BufferSize:     c.BufferSize,
		MaxSubscribers: c.MaxSubscribers,
		Persistent:     c.Persistent,
		TTLSeconds:     c.TTLSeconds,
	}
}

// SubscriptionConfig holds subscription configuration for testing.
type SubscriptionConfig struct {
	Channel             string
	Filter              *grpc.MessagingSubscriptionFilter
	DeltaMode           bool
	DedupMetadata       bool
	DedupSkipDuplicates bool
	BufferSize          int32
}

// DefaultSubscriptionConfig returns a default subscription configuration.
func DefaultSubscriptionConfig(channel string) *SubscriptionConfig {
	return &SubscriptionConfig{
		Channel:             channel,
		Filter:              nil,
		DeltaMode:           false,
		DedupMetadata:       false,
		DedupSkipDuplicates: false,
		BufferSize:          256,
	}
}

// ToSubscribeRequest converts SubscriptionConfig to a SubscribeRequest.
func (c *SubscriptionConfig) ToSubscribeRequest() *grpc.MessagingSubscribeRequest {
	return &grpc.MessagingSubscribeRequest{
		Channel:             c.Channel,
		Filter:              c.Filter,
		DeltaMode:           c.DeltaMode,
		DedupMetadata:       c.DedupMetadata,
		DedupSkipDuplicates: c.DedupSkipDuplicates,
		BufferSize:          c.BufferSize,
	}
}

// PublishConfig holds publish configuration for testing.
type PublishConfig struct {
	Channel  string
	Payload  []byte
	DedupKey string
	Metadata map[string]string
}

// DefaultPublishConfig returns a default publish configuration.
func DefaultPublishConfig(channel string, payload []byte) *PublishConfig {
	return &PublishConfig{
		Channel:  channel,
		Payload:  payload,
		DedupKey: "",
		Metadata: nil,
	}
}

// ToPublishRequest converts PublishConfig to a PublishRequest.
func (c *PublishConfig) ToPublishRequest() *grpc.MessagingPublishRequest {
	return &grpc.MessagingPublishRequest{
		Channel:  c.Channel,
		Payload:  c.Payload,
		DedupKey: c.DedupKey,
		Metadata: c.Metadata,
	}
}

// DeltaPublishConfig holds delta publish configuration for testing.
type DeltaPublishConfig struct {
	Channel  string
	OldValue []byte
	NewValue []byte
	DedupKey string
	Metadata map[string]string
}

// DefaultDeltaPublishConfig returns a default delta publish configuration.
func DefaultDeltaPublishConfig(channel string, oldValue, newValue []byte) *DeltaPublishConfig {
	return &DeltaPublishConfig{
		Channel:  channel,
		OldValue: oldValue,
		NewValue: newValue,
		DedupKey: "",
		Metadata: nil,
	}
}

// ToPublishDeltaRequest converts DeltaPublishConfig to a PublishDeltaRequest.
func (c *DeltaPublishConfig) ToPublishDeltaRequest() *grpc.MessagingPublishDeltaRequest {
	return &grpc.MessagingPublishDeltaRequest{
		Channel:  c.Channel,
		OldValue: c.OldValue,
		NewValue: c.NewValue,
		DedupKey: c.DedupKey,
		Metadata: c.Metadata,
	}
}
