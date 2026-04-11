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
	"fmt"
	"testing"
	"time"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

// embeddedAdapter implements MessagingCommands using the embedded SDK client.
type embeddedAdapter struct {
	client quicbaas.Client
	t      *testing.T
}

// newEmbeddedAdapter creates a new embedded adapter.
func newEmbeddedAdapter(t *testing.T) *embeddedAdapter {
	return &embeddedAdapter{
		t: t,
	}
}

// Name returns the protocol name.
func (a *embeddedAdapter) Name() string {
	return "Embedded"
}

// Setup initializes the embedded client.
func (a *embeddedAdapter) Setup(ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(a.t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}

	client, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}

	a.client = client
	return nil
}

// Teardown cleans up the embedded client.
func (a *embeddedAdapter) Teardown() error {
	if a.client != nil {
		return a.client.Close()
	}
	return nil
}

// CreateChannel creates a notification channel via embedded SDK.
func (a *embeddedAdapter) CreateChannel(ctx context.Context, req *grpcclient.MessagingCreateChannelRequest) (*grpcclient.MessagingCreateChannelResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	channel, err := a.client.Messaging().CreateChannel(ctx, &quicbaas.MessagingCreateChannelRequest{
		Name:       req.Name,
		BufferSize: int(req.BufferSize),
		MaxSubs:    int(req.MaxSubscribers),
		Persistent: req.Persistent,
		TTL:        time.Duration(req.TTLSeconds) * time.Second,
	})
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingCreateChannelResponse{
		Channel: &grpcclient.MessagingChannel{
			Name:        channel.Name,
			CreatedAt:   channel.CreatedAt,
			Subscribers: channel.Subscribers,
			Persistent:  channel.Persistent,
			BufferSize:  int32(channel.BufferSize),
		},
	}, nil
}

// DeleteChannel deletes a notification channel via embedded SDK.
func (a *embeddedAdapter) DeleteChannel(ctx context.Context, req *grpcclient.MessagingDeleteChannelRequest) (*grpcclient.MessagingDeleteChannelResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	err := a.client.Messaging().DeleteChannel(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingDeleteChannelResponse{
		Deleted: true,
		Name:    req.Name,
	}, nil
}

// GetChannel retrieves channel information via embedded SDK.
func (a *embeddedAdapter) GetChannel(ctx context.Context, req *grpcclient.MessagingGetChannelRequest) (*grpcclient.MessagingGetChannelResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	channel, err := a.client.Messaging().GetChannel(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingGetChannelResponse{
		Channel: &grpcclient.MessagingChannel{
			Name:        channel.Name,
			CreatedAt:   channel.CreatedAt,
			Subscribers: channel.Subscribers,
			Persistent:  channel.Persistent,
			BufferSize:  int32(channel.BufferSize),
		},
	}, nil
}

// ListChannels lists all channels via embedded SDK.
func (a *embeddedAdapter) ListChannels(ctx context.Context, req *grpcclient.MessagingListChannelsRequest) (*grpcclient.MessagingListChannelsResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	resp, err := a.client.Messaging().ListChannels(ctx)
	if err != nil {
		return nil, err
	}

	channels := make([]*grpcclient.MessagingChannel, 0, len(resp.Channels))
	for _, ch := range resp.Channels {
		// Apply name prefix filter if specified
		if req.NamePrefix != "" && len(ch.Name) >= len(req.NamePrefix) {
			if ch.Name[:len(req.NamePrefix)] != req.NamePrefix {
				continue
			}
		}

		channels = append(channels, &grpcclient.MessagingChannel{
			Name:        ch.Name,
			CreatedAt:   ch.CreatedAt,
			Subscribers: ch.Subscribers,
			Persistent:  ch.Persistent,
			BufferSize:  int32(ch.BufferSize),
		})

		// Apply limit if specified
		if req.Limit > 0 && int32(len(channels)) >= req.Limit {
			break
		}
	}

	return &grpcclient.MessagingListChannelsResponse{
		Channels: channels,
	}, nil
}

// Subscribe creates a subscription via embedded SDK.
func (a *embeddedAdapter) Subscribe(ctx context.Context, req *grpcclient.MessagingSubscribeRequest) (*grpcclient.MessagingSubscribeResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	subscription, err := a.client.Messaging().Subscribe(ctx, &quicbaas.MessagingSubscribeRequest{
		Channel:             req.Channel,
		DeltaMode:           req.DeltaMode,
		DedupMetadata:       req.DedupMetadata,
		DedupSkipDuplicates: req.DedupSkipDuplicates,
		BufferSize:          int(req.BufferSize),
	})
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingSubscribeResponse{
		Subscription: &grpcclient.MessagingSubscription{
			ID:        subscription.ID,
			Channel:   subscription.Channel,
			ClientID:  subscription.ClientID,
			CreatedAt: subscription.CreatedAt,
		},
	}, nil
}

// Unsubscribe removes a subscription via embedded SDK.
func (a *embeddedAdapter) Unsubscribe(ctx context.Context, req *grpcclient.MessagingUnsubscribeRequest) (*grpcclient.MessagingUnsubscribeResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	err := a.client.Messaging().Unsubscribe(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingUnsubscribeResponse{
		Unsubscribed:   true,
		SubscriptionID: req.SubscriptionID,
	}, nil
}

// Publish publishes a message via embedded SDK.
func (a *embeddedAdapter) Publish(ctx context.Context, req *grpcclient.MessagingPublishRequest) (*grpcclient.MessagingPublishResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	err := a.client.Messaging().Publish(ctx, &quicbaas.MessagingPublishRequest{
		Channel:  req.Channel,
		Payload:  req.Payload,
		DedupKey: req.DedupKey,
		Metadata: req.Metadata,
	})
	if err != nil {
		return nil, err
	}

	// The SDK Publish returns void, so we generate a message ID
	// and assume success (no duplicate detection at this level)
	return &grpcclient.MessagingPublishResponse{
		MessageID:   fmt.Sprintf("emb-%d", time.Now().UnixNano()),
		Channel:     req.Channel,
		IsDuplicate: false, // Duplicate detection happens server-side
	}, nil
}

// PublishDelta publishes a delta message via embedded SDK.
func (a *embeddedAdapter) PublishDelta(ctx context.Context, req *grpcclient.MessagingPublishDeltaRequest) (*grpcclient.MessagingPublishDeltaResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	err := a.client.Messaging().PublishDelta(ctx, &quicbaas.MessagingPublishDeltaRequest{
		Channel:  req.Channel,
		Old:      req.OldValue,
		New:      req.NewValue,
		DedupKey: req.DedupKey,
		Metadata: req.Metadata,
	})
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingPublishDeltaResponse{
		MessageID:   fmt.Sprintf("emb-delta-%d", time.Now().UnixNano()),
		Channel:     req.Channel,
		IsDuplicate: false,
	}, nil
}

// GetChannelStats retrieves channel statistics via embedded SDK.
func (a *embeddedAdapter) GetChannelStats(ctx context.Context, req *grpcclient.MessagingGetChannelStatsRequest) (*grpcclient.MessagingGetChannelStatsResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	stats, err := a.client.Messaging().GetChannelStats(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingGetChannelStatsResponse{
		Stats: &grpcclient.MessagingChannelStats{
			Name:              stats.Name,
			Subscribers:       stats.Subscribers,
			MessagesPublished: stats.MessagesPublished,
			MessagesInHistory: int32(stats.MessagesInHistory),
		},
	}, nil
}

// GetDedupStats retrieves deduplication statistics via embedded SDK.
func (a *embeddedAdapter) GetDedupStats(ctx context.Context, req *grpcclient.MessagingGetDedupStatsRequest) (*grpcclient.MessagingGetDedupStatsResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	stats, err := a.client.Messaging().GetDedupStats(ctx)
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingGetDedupStatsResponse{
		Stats: &grpcclient.MessagingDedupStats{
			Channel:         req.Channel, // Use the requested channel
			TotalMessages:   stats.UniqueMessages,
			TotalDuplicates: stats.DuplicatesDetected,
		},
	}, nil
}

// GetStats retrieves global messaging statistics via embedded SDK.
func (a *embeddedAdapter) GetStats(ctx context.Context) (*grpcclient.MessagingGetStatsResponse, error) {
	if a.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	stats, err := a.client.Messaging().GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &grpcclient.MessagingGetStatsResponse{
		Stats: &grpcclient.MessagingStats{
			TotalChannels:      stats.TotalChannels,
			TotalSubscriptions: stats.TotalSubscriptions,
			MessagesPublished:  stats.MessagesPublished,
			MessagesDelivered:  stats.MessagesDelivered,
			DedupHits:          stats.DedupHits,
			DroppedMessages:    stats.DroppedMessages,
			BufferUtilization:  stats.BufferUtilization,
		},
	}, nil
}
