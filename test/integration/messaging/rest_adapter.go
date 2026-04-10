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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// restAdapter implements MessagingCommands using REST API.
type restAdapter struct {
	baseURL    string
	httpClient *http.Client
}

// newRESTAdapter creates a new REST adapter.
func newRESTAdapter() *restAdapter {
	endpoints := helpers.GetRESTEndpoints()
	baseURL := "https://localhost:8080"
	if len(endpoints) > 0 {
		baseURL = endpoints[0]
	}
	return &restAdapter{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				TLSHandshakeTimeout: 10 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
	}
}

// Name returns the protocol name.
func (a *restAdapter) Name() string {
	return "REST"
}

// Setup initializes the REST client.
func (a *restAdapter) Setup(ctx context.Context) error {
	// REST client doesn't require explicit setup
	return nil
}

// Teardown cleans up the REST client.
func (a *restAdapter) Teardown() error {
	return nil
}

// doRequest performs an HTTP request and returns the response body.
func (a *restAdapter) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, &dragonerrors.WrappedMessageError{Message: "failed to marshal request body", Cause: err}
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	url := a.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to read response body", Cause: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &dragonerrors.MessageError{Message: "request failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return respBody, nil
}

// CreateChannel creates a notification channel via REST.
func (a *restAdapter) CreateChannel(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error) {
	body := map[string]interface{}{
		"name":            req.Name,
		"buffer_size":     req.BufferSize,
		"max_subscribers": req.MaxSubscribers,
		"persistent":      req.Persistent,
		"ttl_seconds":     req.TTLSeconds,
	}

	respBody, err := a.doRequest(ctx, http.MethodPost, "/api/v1/messaging/channels", body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Channel *restChannelResponse `json:"channel"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	return &grpc.MessagingCreateChannelResponse{
		Channel:   response.Channel.toChannel(),
		Timestamp: time.Now(),
	}, nil
}

// DeleteChannel deletes a notification channel via REST.
func (a *restAdapter) DeleteChannel(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
	path := "/api/v1/messaging/channels/" + req.Name
	respBody, err := a.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Name    string `json:"name"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	return &grpc.MessagingDeleteChannelResponse{
		Name:      response.Name,
		Deleted:   response.Deleted,
		Timestamp: time.Now(),
	}, nil
}

// GetChannel retrieves channel information via REST.
func (a *restAdapter) GetChannel(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error) {
	path := "/api/v1/messaging/channels/" + req.Name
	respBody, err := a.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Channel *restChannelResponse `json:"channel"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	if response.Channel == nil {
		return nil, &dragonerrors.MessageError{Message: "channel not found: " + req.Name}
	}

	return &grpc.MessagingGetChannelResponse{
		Channel:   response.Channel.toChannel(),
		Timestamp: time.Now(),
	}, nil
}

// ListChannels lists all channels via REST.
func (a *restAdapter) ListChannels(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error) {
	path := "/api/v1/messaging/channels"
	if req.Limit > 0 || req.NamePrefix != "" {
		path += "?"
		if req.Limit > 0 {
			path += "limit=" + strconv.FormatInt(int64(req.Limit), 10)
		}
		if req.NamePrefix != "" {
			if req.Limit > 0 {
				path += "&"
			}
			path += "prefix=" + req.NamePrefix
		}
	}

	respBody, err := a.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Channels   []*restChannelResponse `json:"channels"`
		NextCursor string                 `json:"next_cursor"`
		HasMore    bool                   `json:"has_more"`
		TotalCount int32                  `json:"total_count"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	channels := make([]*grpc.MessagingChannel, len(response.Channels))
	for i, ch := range response.Channels {
		channels[i] = ch.toChannel()
	}

	return &grpc.MessagingListChannelsResponse{
		Channels:   channels,
		NextCursor: response.NextCursor,
		HasMore:    response.HasMore,
		TotalCount: response.TotalCount,
		Timestamp:  time.Now(),
	}, nil
}

// Subscribe creates a subscription via REST.
func (a *restAdapter) Subscribe(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
	body := map[string]interface{}{
		"channel":               req.Channel,
		"delta_mode":            req.DeltaMode,
		"dedup_metadata":        req.DedupMetadata,
		"dedup_skip_duplicates": req.DedupSkipDuplicates,
		"buffer_size":           req.BufferSize,
	}

	if req.Filter != nil {
		body["filter"] = map[string]interface{}{
			"event_types": req.Filter.EventTypes,
			"key_prefix":  req.Filter.KeyPrefix,
		}
	}

	respBody, err := a.doRequest(ctx, http.MethodPost, "/api/v1/messaging/subscribe", body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Subscription *struct {
			ID        string `json:"id"`
			Channel   string `json:"channel"`
			ClientID  string `json:"client_id"`
			CreatedAt int64  `json:"created_at"`
		} `json:"subscription"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	if response.Subscription == nil {
		return nil, &dragonerrors.MessageError{Message: "failed to create subscription"}
	}

	return &grpc.MessagingSubscribeResponse{
		Subscription: &grpc.MessagingSubscription{
			ID:        response.Subscription.ID,
			Channel:   response.Subscription.Channel,
			ClientID:  response.Subscription.ClientID,
			CreatedAt: time.Unix(response.Subscription.CreatedAt, 0),
		},
		Timestamp: time.Now(),
	}, nil
}

// Unsubscribe removes a subscription via REST.
func (a *restAdapter) Unsubscribe(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
	path := "/api/v1/messaging/subscribe/" + req.SubscriptionID
	respBody, err := a.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		SubscriptionID string `json:"subscription_id"`
		Unsubscribed   bool   `json:"unsubscribed"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	return &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: response.SubscriptionID,
		Unsubscribed:   response.Unsubscribed,
		Timestamp:      time.Now(),
	}, nil
}

// Publish publishes a message via REST.
func (a *restAdapter) Publish(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error) {
	path := "/api/v1/messaging/publish/" + req.Channel
	body := map[string]interface{}{
		"payload":   string(req.Payload),
		"dedup_key": req.DedupKey,
		"metadata":  req.Metadata,
	}

	respBody, err := a.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}

	var response struct {
		MessageID   string `json:"message_id"`
		Channel     string `json:"channel"`
		IsDuplicate bool   `json:"is_duplicate"`
		Timestamp   int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	return &grpc.MessagingPublishResponse{
		MessageID:   response.MessageID,
		Channel:     response.Channel,
		IsDuplicate: response.IsDuplicate,
		Timestamp:   time.Unix(response.Timestamp, 0),
	}, nil
}

// PublishDelta publishes a delta message via REST.
func (a *restAdapter) PublishDelta(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error) {
	path := "/api/v1/messaging/publish-delta/" + req.Channel
	body := map[string]interface{}{
		"old":       string(req.OldValue),
		"new":       string(req.NewValue),
		"dedup_key": req.DedupKey,
		"metadata":  req.Metadata,
	}

	respBody, err := a.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}

	var response struct {
		MessageID   string `json:"message_id"`
		Channel     string `json:"channel"`
		IsDuplicate bool   `json:"is_duplicate"`
		Timestamp   int64  `json:"timestamp"`
		Delta       *struct {
			EntityID   string `json:"entity_id"`
			EntityType string `json:"entity_type"`
			Version    uint64 `json:"version"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	var delta *grpc.MessagingDeltaPayload
	if response.Delta != nil {
		delta = &grpc.MessagingDeltaPayload{
			EntityID:   response.Delta.EntityID,
			EntityType: response.Delta.EntityType,
			Version:    response.Delta.Version,
		}
	}

	return &grpc.MessagingPublishDeltaResponse{
		MessageID:   response.MessageID,
		Channel:     response.Channel,
		Delta:       delta,
		IsDuplicate: response.IsDuplicate,
		Timestamp:   time.Unix(response.Timestamp, 0),
	}, nil
}

// GetChannelStats retrieves per-channel statistics via REST.
func (a *restAdapter) GetChannelStats(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
	path := "/api/v1/messaging/stats/" + req.Name
	respBody, err := a.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Name              string `json:"name"`
		Subscribers       int64  `json:"subscribers"`
		MessagesPublished int64  `json:"messages_published"`
		Timestamp         int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	return &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:              response.Name,
			Subscribers:       response.Subscribers,
			MessagesPublished: response.MessagesPublished,
		},
		Timestamp: time.Now(),
	}, nil
}

// GetDedupStats retrieves deduplication statistics via REST.
func (a *restAdapter) GetDedupStats(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error) {
	path := "/api/v1/messaging/dedup/stats/" + req.Channel
	respBody, err := a.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Channel         string `json:"channel"`
		TotalMessages   int64  `json:"total_messages"`
		TotalDuplicates int64  `json:"total_duplicates"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	return &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         response.Channel,
			TotalMessages:   response.TotalMessages,
			TotalDuplicates: response.TotalDuplicates,
		},
		Timestamp: time.Now(),
	}, nil
}

// GetStats retrieves global messaging statistics via REST.
func (a *restAdapter) GetStats(ctx context.Context) (*grpc.MessagingGetStatsResponse, error) {
	respBody, err := a.doRequest(ctx, http.MethodGet, "/api/v1/messaging/stats", nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		TotalChannels      int64   `json:"total_channels"`
		TotalSubscriptions int64   `json:"total_subscriptions"`
		MessagesPublished  int64   `json:"messages_published"`
		MessagesDelivered  int64   `json:"messages_delivered"`
		DedupHits          int64   `json:"dedup_hits"`
		DroppedMessages    int64   `json:"dropped_messages"`
		BufferUtilization  float64 `json:"buffer_utilization"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, &dragonerrors.WrappedMessageError{Message: "failed to parse response", Cause: err}
	}

	return &grpc.MessagingGetStatsResponse{
		Stats: &grpc.MessagingStats{
			TotalChannels:      response.TotalChannels,
			TotalSubscriptions: response.TotalSubscriptions,
			MessagesPublished:  response.MessagesPublished,
			MessagesDelivered:  response.MessagesDelivered,
			DedupHits:          response.DedupHits,
			DroppedMessages:    response.DroppedMessages,
			BufferUtilization:  response.BufferUtilization,
		},
		IsRunning: true,
		Timestamp: time.Now(),
	}, nil
}

// restChannelResponse represents the REST JSON response for a channel.
type restChannelResponse struct {
	Name        string `json:"name"`
	Codec       string `json:"codec"`
	CreatedAt   int64  `json:"created_at"`
	Subscribers int64  `json:"subscribers"`
	Persistent  bool   `json:"persistent"`
	MaxHistory  int32  `json:"max_history"`
	BufferSize  int32  `json:"buffer_size"`
	TTLSeconds  int64  `json:"ttl_seconds"`
}

// toChannel converts the REST response to a MessagingChannel.
func (r *restChannelResponse) toChannel() *grpc.MessagingChannel {
	return &grpc.MessagingChannel{
		Name:        r.Name,
		Codec:       r.Codec,
		CreatedAt:   time.Unix(r.CreatedAt, 0),
		Subscribers: r.Subscribers,
		Persistent:  r.Persistent,
		MaxHistory:  r.MaxHistory,
		BufferSize:  r.BufferSize,
		TTLSeconds:  r.TTLSeconds,
	}
}
