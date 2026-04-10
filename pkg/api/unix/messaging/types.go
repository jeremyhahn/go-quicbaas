//go:build unix

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
	"encoding/json"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// ErrorResponse represents an error response payload.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// CreateChannelRequest represents a request to create a channel.
type CreateChannelRequest struct {
	Name       string        `json:"name"`
	Codec      string        `json:"codec,omitempty"`
	BufferSize int           `json:"buffer_size,omitempty"`
	MaxSubs    int           `json:"max_subscribers,omitempty"`
	Persistent bool          `json:"persistent,omitempty"`
	MaxHistory int           `json:"max_history,omitempty"`
	TTL        time.Duration `json:"ttl,omitempty"`
}

// CreateChannelResponse represents the response from creating a channel.
type CreateChannelResponse struct {
	Channel *ChannelInfo `json:"channel"`
}

// DeleteChannelRequest represents a request to delete a channel.
type DeleteChannelRequest struct {
	Name string `json:"name"`
}

// DeleteChannelResponse represents the response from deleting a channel.
type DeleteChannelResponse struct {
	Success bool `json:"success"`
}

// GetChannelRequest represents a request to get channel information.
type GetChannelRequest struct {
	Name string `json:"name"`
}

// GetChannelResponse represents the response from getting a channel.
type GetChannelResponse struct {
	Channel *ChannelInfo `json:"channel"`
}

// ListChannelsRequest represents a request to list all channels.
type ListChannelsRequest struct {
	// Empty - no parameters needed
}

// ListChannelsResponse represents the response from listing channels.
type ListChannelsResponse struct {
	Channels []*ChannelInfo `json:"channels"`
}

// ChannelInfo represents channel information in responses.
type ChannelInfo struct {
	Name        string        `json:"name"`
	Codec       string        `json:"codec,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	Subscribers int64         `json:"subscribers"`
	Persistent  bool          `json:"persistent"`
	MaxHistory  int           `json:"max_history,omitempty"`
	BufferSize  int           `json:"buffer_size,omitempty"`
	TTL         time.Duration `json:"ttl,omitempty"`
}

// SubscribeRequest represents a request to subscribe to a channel.
type SubscribeRequest struct {
	Channel    string              `json:"channel"`
	SessionID  string              `json:"session_id,omitempty"`
	BufferSize int                 `json:"buffer_size,omitempty"`
	Filter     *SubscriptionFilter `json:"filter,omitempty"`
}

// SubscriptionFilter represents filtering criteria for subscriptions.
type SubscriptionFilter struct {
	EventTypes []string `json:"event_types,omitempty"`
	KeyPrefix  string   `json:"key_prefix,omitempty"`
}

// SubscribeResponse represents the response from subscribing.
type SubscribeResponse struct {
	SubscriptionID string              `json:"subscription_id"`
	Channel        string              `json:"channel"`
	ClientID       string              `json:"client_id"`
	CreatedAt      time.Time           `json:"created_at"`
	Filter         *SubscriptionFilter `json:"filter,omitempty"`
}

// UnsubscribeRequest represents a request to unsubscribe.
type UnsubscribeRequest struct {
	SubscriptionID string `json:"subscription_id"`
}

// UnsubscribeResponse represents the response from unsubscribing.
type UnsubscribeResponse struct {
	Success bool `json:"success"`
}

// PublishRequest represents a request to publish a message.
type PublishRequest struct {
	Channel   string            `json:"channel"`
	Payload   json.RawMessage   `json:"payload"`
	MessageID string            `json:"message_id,omitempty"`
	DedupKey  string            `json:"dedup_key,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// PublishResponse represents the response from publishing a message.
type PublishResponse struct {
	Success     bool   `json:"success"`
	MessageID   string `json:"message_id,omitempty"`
	IsDuplicate bool   `json:"is_duplicate,omitempty"`
}

// PublishDeltaRequest represents a request to publish a delta message.
type PublishDeltaRequest struct {
	Channel   string            `json:"channel"`
	OldValue  json.RawMessage   `json:"old_value"`
	NewValue  json.RawMessage   `json:"new_value"`
	MessageID string            `json:"message_id,omitempty"`
	DedupKey  string            `json:"dedup_key,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// PublishDeltaResponse represents the response from publishing a delta.
type PublishDeltaResponse struct {
	Success     bool   `json:"success"`
	MessageID   string `json:"message_id,omitempty"`
	IsDuplicate bool   `json:"is_duplicate,omitempty"`
}

// GetStatsRequest represents a request to get messaging service statistics.
type GetStatsRequest struct {
	// Empty - no parameters needed
}

// GetStatsResponse represents the response with messaging service statistics.
type GetStatsResponse struct {
	Stats *StatsInfo `json:"stats"`
}

// StatsInfo represents messaging service statistics.
type StatsInfo struct {
	TotalChannels      int64   `json:"total_channels"`
	TotalSubscriptions int64   `json:"total_subscriptions"`
	MessagesPublished  int64   `json:"messages_published,omitempty"`
	MessagesDelivered  int64   `json:"messages_delivered,omitempty"`
	DedupHits          int64   `json:"dedup_hits,omitempty"`
	DroppedMessages    int64   `json:"dropped_messages,omitempty"`
	BufferUtilization  float64 `json:"buffer_utilization,omitempty"`
}

// DedupStatsRequest represents a request for deduplication statistics.
type DedupStatsRequest struct {
	Channel string `json:"channel"`
}

// DedupStatsResponse represents the response with deduplication statistics.
type DedupStatsResponse struct {
	Stats *DedupStatsInfo `json:"stats"`
}

// DedupStatsInfo represents deduplication statistics for a channel.
type DedupStatsInfo struct {
	Channel         string            `json:"channel"`
	TotalMessages   int64             `json:"total_messages"`
	TotalDuplicates int64             `json:"total_duplicates"`
	TopDuplicates   []*DedupEntryInfo `json:"top_duplicates,omitempty"`
}

// DedupEntryInfo represents a deduplication entry.
type DedupEntryInfo struct {
	Count       int64     `json:"count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	MessageID   string    `json:"message_id,omitempty"`
	PayloadHash string    `json:"payload_hash,omitempty"`
}

// channelToInfo converts a messaging.Channel to ChannelInfo.
func channelToInfo(ch *messaging.Channel) *ChannelInfo {
	if ch == nil {
		return nil
	}
	return &ChannelInfo{
		Name:        ch.Name,
		Codec:       ch.Codec,
		CreatedAt:   ch.CreatedAt,
		Subscribers: ch.Subscribers,
		Persistent:  ch.Persistent,
		MaxHistory:  ch.MaxHistory,
		BufferSize:  ch.BufferSize,
		TTL:         ch.TTL,
	}
}

// statsToInfo converts messaging.Stats to StatsInfo.
func statsToInfo(s *messaging.Stats) *StatsInfo {
	if s == nil {
		return nil
	}
	return &StatsInfo{
		TotalChannels:      s.TotalChannels,
		TotalSubscriptions: s.TotalSubscriptions,
		MessagesPublished:  s.MessagesPublished,
		MessagesDelivered:  s.MessagesDelivered,
		DedupHits:          s.DedupHits,
		DroppedMessages:    s.DroppedMessages,
		BufferUtilization:  s.BufferUtilization,
	}
}

// dedupStatsToInfo converts messaging.DedupStats to DedupStatsInfo.
func dedupStatsToInfo(s *messaging.DedupStats) *DedupStatsInfo {
	if s == nil {
		return nil
	}
	info := &DedupStatsInfo{
		Channel:         s.Channel,
		TotalMessages:   s.TotalMessages,
		TotalDuplicates: s.TotalDuplicates,
	}
	if len(s.TopDuplicates) > 0 {
		info.TopDuplicates = make([]*DedupEntryInfo, 0, len(s.TopDuplicates))
		for _, entry := range s.TopDuplicates {
			info.TopDuplicates = append(info.TopDuplicates, &DedupEntryInfo{
				Count:       entry.Count,
				FirstSeen:   entry.FirstSeen,
				LastSeen:    entry.LastSeen,
				MessageID:   entry.MessageID,
				PayloadHash: entry.PayloadHash,
			})
		}
	}
	return info
}

// subscriptionToResponse converts a messaging.Subscription to SubscribeResponse.
func subscriptionToResponse(sub *messaging.Subscription) *SubscribeResponse {
	if sub == nil {
		return nil
	}
	resp := &SubscribeResponse{
		SubscriptionID: sub.ID,
		Channel:        sub.Channel,
		ClientID:       sub.ClientID,
		CreatedAt:      sub.CreatedAt,
	}
	if sub.Filters != nil {
		resp.Filter = &SubscriptionFilter{
			KeyPrefix: sub.Filters.KeyPrefix,
		}
		if len(sub.Filters.EventTypes) > 0 {
			resp.Filter.EventTypes = make([]string, 0, len(sub.Filters.EventTypes))
			for _, et := range sub.Filters.EventTypes {
				resp.Filter.EventTypes = append(resp.Filter.EventTypes, et.String())
			}
		}
	}
	return resp
}
