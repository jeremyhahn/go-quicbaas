// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// messaging_handler.go provides typed REST routing for the quicbaas.Messaging
// service. The generic dispatch path produced /api/v1/messaging/{method} URLs
// that do not exist on the server and returned an opaque map. This handler
// builds the concrete RESTful endpoints (with {name}/{channel}/{id} path
// parameters substituted from the typed SDK request), decodes the REST wire
// response types, and converts them into the SDK messaging types the client
// expects.

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	messagingclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// messagingServicePrefix is the gRPC-style service identifier the SDK uses to
// route Messaging requests to this handler.
const messagingServicePrefix = "quicbaas.Messaging"

// isMessagingService reports whether the supplied service name resolves to the
// Messaging service.
func isMessagingService(service string) bool {
	return trimRESTServicePrefix(service) == "Messaging"
}

// messagingRESTHandler delegates Messaging service calls to the quicbaasd REST
// endpoints.
type messagingRESTHandler struct {
	transport *Transport
}

// newMessagingRESTHandler constructs a handler bound to t.
func newMessagingRESTHandler(t *Transport) *messagingRESTHandler {
	return &messagingRESTHandler{transport: t}
}

// handleRequest dispatches the Messaging service method to the matching REST
// endpoint.
func (h *messagingRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "CreateChannel":
		return h.handleCreateChannel(ctx, req)
	case "GetChannel":
		return h.handleGetChannel(ctx, req)
	case "ListChannels":
		return h.handleListChannels(ctx, req)
	case "DeleteChannel":
		return h.handleDeleteChannel(ctx, req)
	case "Subscribe":
		return h.handleSubscribe(ctx, req)
	case "Unsubscribe":
		return h.handleUnsubscribe(ctx, req)
	case "Publish":
		return h.handlePublish(ctx, req)
	case "PublishDelta":
		return h.handlePublishDelta(ctx, req)
	case "GetStats":
		return h.handleGetStats(ctx, req)
	case "GetChannelStats":
		return h.handleGetChannelStats(ctx, req)
	case "GetDedupStats":
		return h.handleGetDedupStats(ctx, req)
	default:
		return h.transport.requestWithRetry(ctx, messagingServicePrefix, method, req)
	}
}

// =============================================================================
// REST wire types
// =============================================================================

type messagingChannelWire struct {
	Name        string `json:"name"`
	Codec       string `json:"codec"`
	CreatedAt   int64  `json:"created_at"`
	Subscribers int64  `json:"subscribers"`
	Persistent  bool   `json:"persistent"`
	MaxHistory  int    `json:"max_history"`
	BufferSize  int    `json:"buffer_size"`
	TTLSeconds  int64  `json:"ttl_seconds"`
}

type messagingChannelEnvelope struct {
	Channel *messagingChannelWire `json:"channel"`
}

type messagingListChannelsWire struct {
	Channels []*messagingChannelWire `json:"channels"`
	Total    int                     `json:"total"`
}

type messagingSubscriptionWire struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	ClientID  string `json:"client_id"`
	CreatedAt int64  `json:"created_at"`
}

type messagingSubscribeWire struct {
	Subscription *messagingSubscriptionWire `json:"subscription"`
}

type messagingStatsWire struct {
	TotalChannels      int64   `json:"total_channels"`
	TotalSubscriptions int64   `json:"total_subscriptions"`
	MessagesPublished  int64   `json:"messages_published"`
	MessagesDelivered  int64   `json:"messages_delivered"`
	DedupHits          int64   `json:"dedup_hits"`
	DroppedMessages    int64   `json:"dropped_messages"`
	BufferUtilization  float64 `json:"buffer_utilization"`
}

type messagingChannelStatsWire struct {
	Name              string `json:"name"`
	Subscribers       int64  `json:"subscribers"`
	MessagesPublished int64  `json:"messages_published"`
}

type messagingDedupStatsWire struct {
	Channel         string `json:"channel"`
	TotalMessages   int64  `json:"total_messages"`
	TotalDuplicates int64  `json:"total_duplicates"`
}

// =============================================================================
// REST wire request types
// =============================================================================

type messagingCreateChannelRequest struct {
	Name           string `json:"name"`
	Codec          string `json:"codec,omitempty"`
	Persistent     bool   `json:"persistent,omitempty"`
	MaxHistory     int    `json:"max_history,omitempty"`
	BufferSize     int    `json:"buffer_size,omitempty"`
	MaxSubscribers int    `json:"max_subscribers,omitempty"`
	TTLSeconds     int64  `json:"ttl_seconds,omitempty"`
}

type messagingSubscribeRequest struct {
	Channel             string   `json:"channel"`
	SessionID           string   `json:"session_id,omitempty"`
	DeltaMode           bool     `json:"delta_mode,omitempty"`
	IncludeOldValues    bool     `json:"include_old_values,omitempty"`
	DedupMetadata       bool     `json:"dedup_metadata,omitempty"`
	DedupSkipDuplicates bool     `json:"dedup_skip_duplicates,omitempty"`
	BufferSize          int      `json:"buffer_size,omitempty"`
	EventTypes          []string `json:"event_types,omitempty"`
}

type messagingPublishRequest struct {
	Payload   json.RawMessage   `json:"payload"`
	MessageID string            `json:"message_id,omitempty"`
	DedupKey  string            `json:"dedup_key,omitempty"`
	Codec     string            `json:"codec,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type messagingPublishDeltaRequest struct {
	Old       json.RawMessage   `json:"old"`
	New       json.RawMessage   `json:"new"`
	MessageID string            `json:"message_id,omitempty"`
	DedupKey  string            `json:"dedup_key,omitempty"`
	Codec     string            `json:"codec,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// =============================================================================
// Handlers
// =============================================================================

func (h *messagingRESTHandler) handleCreateChannel(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingclient.CreateChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.messaging.CreateChannel", "request is required")
	}
	if r.Name == "" {
		return nil, errors.NewInvalidInput("rest.messaging.CreateChannel", "channel name is required")
	}
	body := &messagingCreateChannelRequest{
		Name:           r.Name,
		Codec:          r.Codec,
		Persistent:     r.Persistent,
		MaxHistory:     r.MaxHistory,
		BufferSize:     r.BufferSize,
		MaxSubscribers: r.MaxSubs,
		TTLSeconds:     int64(r.TTL.Seconds()),
	}
	var wire messagingChannelEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/messaging/channels", body, &wire); err != nil {
		return nil, err
	}
	return messagingWireToChannel(wire.Channel), nil
}

func (h *messagingRESTHandler) handleGetChannel(ctx context.Context, req interface{}) (interface{}, error) {
	name, ok := extractStringField(req, "Name")
	if !ok || name == "" {
		return nil, errors.NewInvalidInput("rest.messaging.GetChannel", "channel name is required")
	}
	endpoint := "/api/v1/messaging/channels/" + url.PathEscape(name)
	var wire messagingChannelEnvelope
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return messagingWireToChannel(wire.Channel), nil
}

func (h *messagingRESTHandler) handleListChannels(ctx context.Context, _ interface{}) (interface{}, error) {
	var wire messagingListChannelsWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/messaging/channels", nil, &wire); err != nil {
		return nil, err
	}
	channels := make([]*messagingclient.Channel, 0, len(wire.Channels))
	for _, c := range wire.Channels {
		channels = append(channels, messagingWireToChannel(c))
	}
	return &messagingclient.ListChannelsResponse{Channels: channels}, nil
}

func (h *messagingRESTHandler) handleDeleteChannel(ctx context.Context, req interface{}) (interface{}, error) {
	name, ok := extractStringField(req, "Name")
	if !ok || name == "" {
		return nil, errors.NewInvalidInput("rest.messaging.DeleteChannel", "channel name is required")
	}
	endpoint := "/api/v1/messaging/channels/" + url.PathEscape(name)
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, endpoint, nil, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *messagingRESTHandler) handleSubscribe(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingclient.SubscribeRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.messaging.Subscribe", "request is required")
	}
	if r.Channel == "" {
		return nil, errors.NewInvalidInput("rest.messaging.Subscribe", "channel is required")
	}
	body := &messagingSubscribeRequest{
		Channel:             r.Channel,
		SessionID:           r.SessionID,
		DeltaMode:           r.DeltaMode,
		IncludeOldValues:    r.IncludeOldValues,
		DedupMetadata:       r.DedupMetadata,
		DedupSkipDuplicates: r.DedupSkipDuplicates,
		BufferSize:          r.BufferSize,
	}
	if r.Filter != nil {
		for _, et := range r.Filter.EventTypes {
			body.EventTypes = append(body.EventTypes, et.String())
		}
	}
	var wire messagingSubscribeWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/messaging/subscribe", body, &wire); err != nil {
		return nil, err
	}
	if wire.Subscription == nil {
		return nil, errors.NewInternal("rest.messaging.Subscribe", "server returned no subscription")
	}
	return &messagingclient.Subscription{
		ID:        wire.Subscription.ID,
		Channel:   wire.Subscription.Channel,
		ClientID:  wire.Subscription.ClientID,
		CreatedAt: time.Unix(wire.Subscription.CreatedAt, 0).UTC(),
	}, nil
}

func (h *messagingRESTHandler) handleUnsubscribe(ctx context.Context, req interface{}) (interface{}, error) {
	id, ok := extractStringField(req, "SubscriptionID")
	if !ok || id == "" {
		return nil, errors.NewInvalidInput("rest.messaging.Unsubscribe", "subscription ID is required")
	}
	endpoint := "/api/v1/messaging/subscribe/" + url.PathEscape(id)
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, endpoint, nil, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *messagingRESTHandler) handlePublish(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingclient.PublishRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.messaging.Publish", "request is required")
	}
	if r.Channel == "" {
		return nil, errors.NewInvalidInput("rest.messaging.Publish", "channel is required")
	}
	payload, err := messagingRawPayload(r.Payload)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "rest.messaging.Publish",
			"failed to encode payload")
	}
	body := &messagingPublishRequest{
		Payload:   payload,
		MessageID: r.MessageID,
		DedupKey:  r.DedupKey,
		Codec:     r.Codec,
		Metadata:  r.Metadata,
	}
	endpoint := "/api/v1/messaging/publish/" + url.PathEscape(r.Channel)
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *messagingRESTHandler) handlePublishDelta(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingclient.PublishDeltaRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.messaging.PublishDelta", "request is required")
	}
	if r.Channel == "" {
		return nil, errors.NewInvalidInput("rest.messaging.PublishDelta", "channel is required")
	}
	oldRaw, err := json.Marshal(r.Old)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "rest.messaging.PublishDelta",
			"failed to encode old value")
	}
	newRaw, err := json.Marshal(r.New)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "rest.messaging.PublishDelta",
			"failed to encode new value")
	}
	body := &messagingPublishDeltaRequest{
		Old:       oldRaw,
		New:       newRaw,
		MessageID: r.MessageID,
		DedupKey:  r.DedupKey,
		Codec:     r.Codec,
		Metadata:  r.Metadata,
	}
	endpoint := "/api/v1/messaging/publish-delta/" + url.PathEscape(r.Channel)
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *messagingRESTHandler) handleGetStats(ctx context.Context, _ interface{}) (interface{}, error) {
	var wire messagingStatsWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/messaging/stats", nil, &wire); err != nil {
		return nil, err
	}
	return &messagingclient.Stats{
		TotalChannels:      wire.TotalChannels,
		TotalSubscriptions: wire.TotalSubscriptions,
		MessagesPublished:  wire.MessagesPublished,
		MessagesDelivered:  wire.MessagesDelivered,
		DedupHits:          wire.DedupHits,
		DroppedMessages:    wire.DroppedMessages,
		BufferUtilization:  wire.BufferUtilization,
	}, nil
}

func (h *messagingRESTHandler) handleGetChannelStats(ctx context.Context, req interface{}) (interface{}, error) {
	channel, ok := extractStringField(req, "Channel")
	if !ok || channel == "" {
		return nil, errors.NewInvalidInput("rest.messaging.GetChannelStats", "channel is required")
	}
	endpoint := "/api/v1/messaging/stats/" + url.PathEscape(channel)
	var wire messagingChannelStatsWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return &messagingclient.ChannelStats{
		Name:              wire.Name,
		Subscribers:       wire.Subscribers,
		MessagesPublished: wire.MessagesPublished,
	}, nil
}

func (h *messagingRESTHandler) handleGetDedupStats(ctx context.Context, req interface{}) (interface{}, error) {
	channel, ok := extractStringField(req, "Channel")
	if !ok || channel == "" {
		return nil, errors.NewInvalidInput("rest.messaging.GetDedupStats", "channel is required")
	}
	endpoint := "/api/v1/messaging/dedup/stats/" + url.PathEscape(channel)
	var wire messagingDedupStatsWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return &messagingclient.DedupStats{
		TotalChecks:        wire.TotalMessages,
		DuplicatesDetected: wire.TotalDuplicates,
		UniqueMessages:     wire.TotalMessages - wire.TotalDuplicates,
	}, nil
}

// messagingWireToChannel converts a REST channel wire type into the SDK Channel.
func messagingWireToChannel(wire *messagingChannelWire) *messagingclient.Channel {
	if wire == nil {
		return nil
	}
	return &messagingclient.Channel{
		Name:        wire.Name,
		Codec:       wire.Codec,
		CreatedAt:   time.Unix(wire.CreatedAt, 0).UTC(),
		Subscribers: wire.Subscribers,
		Persistent:  wire.Persistent,
		MaxHistory:  wire.MaxHistory,
		BufferSize:  wire.BufferSize,
		TTL:         time.Duration(wire.TTLSeconds) * time.Second,
	}
}

// messagingRawPayload normalizes a publish payload to a JSON RawMessage. Bytes
// that are already valid JSON are forwarded verbatim; otherwise they are
// encoded as a JSON value so the server can store them.
func messagingRawPayload(payload []byte) (json.RawMessage, error) {
	if len(payload) == 0 {
		return json.RawMessage("null"), nil
	}
	if json.Valid(payload) {
		return json.RawMessage(payload), nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
