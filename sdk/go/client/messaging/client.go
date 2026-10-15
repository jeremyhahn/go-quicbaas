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
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Messaging"
)

// Client provides messaging and push notification operations.
type Client interface {
	// CreateChannel creates a new notification channel.
	CreateChannel(ctx context.Context, req *CreateChannelRequest) (*Channel, error)

	// DeleteChannel deletes a notification channel.
	DeleteChannel(ctx context.Context, name string) error

	// GetChannel returns channel information by name.
	GetChannel(ctx context.Context, name string) (*Channel, error)

	// ListChannels returns all notification channels.
	ListChannels(ctx context.Context) (*ListChannelsResponse, error)

	// Subscribe subscribes to a notification channel.
	Subscribe(ctx context.Context, req *SubscribeRequest) (*Subscription, error)

	// Unsubscribe removes a subscription.
	Unsubscribe(ctx context.Context, subscriptionID string) error

	// Publish publishes a message to a channel.
	Publish(ctx context.Context, req *PublishRequest) error

	// PublishDelta publishes a delta message to a channel.
	PublishDelta(ctx context.Context, req *PublishDeltaRequest) error

	// WatchChannel creates a subscription that watches for changes to keys with the given prefix.
	WatchChannel(ctx context.Context, req *WatchChannelRequest) (*Subscription, error)

	// GetStats returns statistics about the push service.
	GetStats(ctx context.Context) (*Stats, error)

	// GetDedupStats returns deduplication statistics for a channel.
	GetDedupStats(ctx context.Context, channel string) (*DedupStats, error)

	// GetChannelStats returns statistics for a specific channel.
	GetChannelStats(ctx context.Context, channel string) (*ChannelStats, error)
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new messaging client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// CreateChannel creates a new notification channel.
func (c *client) CreateChannel(ctx context.Context, req *CreateChannelRequest) (*Channel, error) {
	if err := validateCreateChannelRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("messaging.CreateChannel", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateChannel", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("messaging.CreateChannel", "nil response from server")
	}

	channel, ok := resp.(*Channel)
	if !ok {
		return nil, errors.NewInternal("messaging.CreateChannel", "invalid response type")
	}

	return channel, nil
}

// DeleteChannel deletes a notification channel.
func (c *client) DeleteChannel(ctx context.Context, name string) error {
	if name == "" {
		return errors.NewInvalidInput("messaging.DeleteChannel", "channel name is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("messaging.DeleteChannel", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteChannel", &DeleteChannelRequest{Name: name})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*deleteChannelResponse); !ok {
		return errors.NewInternal("messaging.DeleteChannel", "invalid response type")
	}

	return nil
}

// GetChannel returns channel information by name.
func (c *client) GetChannel(ctx context.Context, name string) (*Channel, error) {
	if name == "" {
		return nil, errors.NewInvalidInput("messaging.GetChannel", "channel name is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("messaging.GetChannel", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetChannel", &GetChannelRequest{Name: name})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("messaging.GetChannel", "channel not found")
	}

	channel, ok := resp.(*Channel)
	if !ok {
		return nil, errors.NewInternal("messaging.GetChannel", "invalid response type")
	}

	return channel, nil
}

// ListChannels returns all notification channels.
func (c *client) ListChannels(ctx context.Context) (*ListChannelsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("messaging.ListChannels", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListChannels", &ListChannelsRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListChannelsResponse{Channels: []*Channel{}}, nil
	}

	listResp, ok := resp.(*ListChannelsResponse)
	if !ok {
		return nil, errors.NewInternal("messaging.ListChannels", "invalid response type")
	}

	return listResp, nil
}

// Subscribe subscribes to a notification channel.
func (c *client) Subscribe(ctx context.Context, req *SubscribeRequest) (*Subscription, error) {
	if err := validateSubscribeRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("messaging.Subscribe", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Subscribe", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("messaging.Subscribe", "nil response from server")
	}

	subscription, ok := resp.(*Subscription)
	if !ok {
		return nil, errors.NewInternal("messaging.Subscribe", "invalid response type")
	}

	return subscription, nil
}

// Unsubscribe removes a subscription.
func (c *client) Unsubscribe(ctx context.Context, subscriptionID string) error {
	if subscriptionID == "" {
		return errors.NewInvalidInput("messaging.Unsubscribe", "subscription ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("messaging.Unsubscribe", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Unsubscribe", &UnsubscribeRequest{SubscriptionID: subscriptionID})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*unsubscribeResponse); !ok {
		return errors.NewInternal("messaging.Unsubscribe", "invalid response type")
	}

	return nil
}

// Publish publishes a message to a channel.
func (c *client) Publish(ctx context.Context, req *PublishRequest) error {
	if err := validatePublishRequest(req); err != nil {
		return err
	}

	if ctx == nil {
		return errors.NewInvalidInput("messaging.Publish", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Publish", req)
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*publishResponse); !ok {
		return errors.NewInternal("messaging.Publish", "invalid response type")
	}

	return nil
}

// PublishDelta publishes a delta message to a channel.
func (c *client) PublishDelta(ctx context.Context, req *PublishDeltaRequest) error {
	if err := validatePublishDeltaRequest(req); err != nil {
		return err
	}

	if ctx == nil {
		return errors.NewInvalidInput("messaging.PublishDelta", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "PublishDelta", req)
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*publishDeltaResponse); !ok {
		return errors.NewInternal("messaging.PublishDelta", "invalid response type")
	}

	return nil
}

// WatchChannel creates a subscription that watches for changes to keys with the given prefix.
func (c *client) WatchChannel(ctx context.Context, req *WatchChannelRequest) (*Subscription, error) {
	if err := validateWatchChannelRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("messaging.WatchChannel", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "WatchChannel", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("messaging.WatchChannel", "nil response from server")
	}

	subscription, ok := resp.(*Subscription)
	if !ok {
		return nil, errors.NewInternal("messaging.WatchChannel", "invalid response type")
	}

	return subscription, nil
}

// GetStats returns statistics about the push service.
func (c *client) GetStats(ctx context.Context) (*Stats, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("messaging.GetStats", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetStats", &getStatsRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("messaging.GetStats", "nil response from server")
	}

	stats, ok := resp.(*Stats)
	if !ok {
		return nil, errors.NewInternal("messaging.GetStats", "invalid response type")
	}

	return stats, nil
}

// GetDedupStats returns deduplication statistics.
func (c *client) GetDedupStats(ctx context.Context, channel string) (*DedupStats, error) {
	if channel == "" {
		return nil, errors.NewInvalidInput("messaging.GetDedupStats", "channel name is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("messaging.GetDedupStats", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetDedupStats", &GetDedupStatsRequest{Channel: channel})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("messaging.GetDedupStats", "nil response from server")
	}

	stats, ok := resp.(*DedupStats)
	if !ok {
		return nil, errors.NewInternal("messaging.GetDedupStats", "invalid response type")
	}

	return stats, nil
}

// GetChannelStats returns statistics for a specific channel.
func (c *client) GetChannelStats(ctx context.Context, channel string) (*ChannelStats, error) {
	if channel == "" {
		return nil, errors.NewInvalidInput("messaging.GetChannelStats", "channel name is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("messaging.GetChannelStats", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetChannelStats", &getChannelStatsRequest{Channel: channel})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("messaging.GetChannelStats", "channel not found")
	}

	stats, ok := resp.(*ChannelStats)
	if !ok {
		return nil, errors.NewInternal("messaging.GetChannelStats", "invalid response type")
	}

	return stats, nil
}

// Validation functions

func validateCreateChannelRequest(req *CreateChannelRequest) error {
	if req == nil {
		return errors.NewInvalidInput("messaging.CreateChannel", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("messaging.CreateChannel", "channel name is required")
	}

	// Validate channel name format
	if len(req.Name) > 256 {
		return errors.NewInvalidInput("messaging.CreateChannel", "channel name exceeds maximum length of 256 characters")
	}

	if req.BufferSize < 0 {
		return errors.NewInvalidInput("messaging.CreateChannel", "buffer size cannot be negative")
	}

	if req.MaxHistory < 0 {
		return errors.NewInvalidInput("messaging.CreateChannel", "max history cannot be negative")
	}

	if req.MaxSubs < 0 {
		return errors.NewInvalidInput("messaging.CreateChannel", "max subscribers cannot be negative")
	}

	if req.TTL < 0 {
		return errors.NewInvalidInput("messaging.CreateChannel", "TTL cannot be negative")
	}

	// Validate codec if specified
	if req.Codec != "" {
		switch req.Codec {
		case CodecJSON, CodecMsgpack, CodecProtobuf:
			// Valid codecs
		default:
			return errors.NewInvalidInput("messaging.CreateChannel", "invalid codec").
				WithContext("codec", req.Codec)
		}
	}

	return nil
}

func validateSubscribeRequest(req *SubscribeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("messaging.Subscribe", "request is required")
	}
	if req.Channel == "" {
		return errors.NewInvalidInput("messaging.Subscribe", "channel name is required")
	}

	if req.BufferSize < 0 {
		return errors.NewInvalidInput("messaging.Subscribe", "buffer size cannot be negative")
	}

	return nil
}

func validatePublishRequest(req *PublishRequest) error {
	if req == nil {
		return errors.NewInvalidInput("messaging.Publish", "request is required")
	}
	if req.Channel == "" {
		return errors.NewInvalidInput("messaging.Publish", "channel name is required")
	}

	// Validate codec if specified
	if req.Codec != "" {
		switch req.Codec {
		case CodecJSON, CodecMsgpack, CodecProtobuf:
			// Valid codecs
		default:
			return errors.NewInvalidInput("messaging.Publish", "invalid codec").
				WithContext("codec", req.Codec)
		}
	}

	return nil
}

func validatePublishDeltaRequest(req *PublishDeltaRequest) error {
	if req == nil {
		return errors.NewInvalidInput("messaging.PublishDelta", "request is required")
	}
	if req.Channel == "" {
		return errors.NewInvalidInput("messaging.PublishDelta", "channel name is required")
	}

	// Validate codec if specified
	if req.Codec != "" {
		switch req.Codec {
		case CodecJSON, CodecMsgpack, CodecProtobuf:
			// Valid codecs
		default:
			return errors.NewInvalidInput("messaging.PublishDelta", "invalid codec").
				WithContext("codec", req.Codec)
		}
	}

	return nil
}

func validateWatchChannelRequest(req *WatchChannelRequest) error {
	if req == nil {
		return errors.NewInvalidInput("messaging.WatchChannel", "request is required")
	}
	if req.KeyPrefix == "" {
		return errors.NewInvalidInput("messaging.WatchChannel", "key prefix is required")
	}

	if req.BufferSize < 0 {
		return errors.NewInvalidInput("messaging.WatchChannel", "buffer size cannot be negative")
	}

	return nil
}
