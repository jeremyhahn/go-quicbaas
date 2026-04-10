//go:build rest

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

package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// =============================================================================
// Route Registration
// =============================================================================

// registerMessagingRoutes registers all messaging notification REST API routes.
func registerMessagingRoutes(s *Server, r chi.Router) {
	r.Route("/messaging", func(r chi.Router) {
		// Channel management routes
		r.Get("/channels", s.HandleListMessagingChannels)            // List channels
		r.Post("/channels", s.HandleCreateMessagingChannel)          // Create channel
		r.Get("/channels/{name}", s.HandleGetMessagingChannel)       // Get channel
		r.Delete("/channels/{name}", s.HandleDeleteMessagingChannel) // Delete channel

		// Subscription routes
		r.Post("/subscribe", s.HandleMessagingSubscribe)          // Subscribe to channel
		r.Delete("/subscribe/{id}", s.HandleMessagingUnsubscribe) // Unsubscribe

		// Publishing routes
		r.Post("/publish/{channel}", s.HandleMessagingPublish)            // Publish message
		r.Post("/publish-delta/{channel}", s.HandleMessagingPublishDelta) // Publish delta

		// Stats routes
		r.Get("/stats", s.HandleMessagingStats)                      // Get messaging service stats
		r.Get("/stats/{channel}", s.HandleMessagingChannelStats)     // Get per-channel stats
		r.Get("/dedup/stats/{channel}", s.HandleMessagingDedupStats) // Get dedup stats for channel
	})
}

// =============================================================================
// Request/Response Types for Messaging REST API
// =============================================================================

// CreateMessagingChannelRESTRequest represents the JSON request body for creating a channel.
type CreateMessagingChannelRESTRequest struct {
	Name           string `json:"name"`
	Codec          string `json:"codec,omitempty"`
	Persistent     bool   `json:"persistent,omitempty"`
	MaxHistory     int    `json:"max_history,omitempty"`
	BufferSize     int    `json:"buffer_size,omitempty"`
	MaxSubscribers int    `json:"max_subscribers,omitempty"`
	TTLSeconds     int64  `json:"ttl_seconds,omitempty"`
}

// CreateMessagingChannelRESTResponse represents the JSON response for channel creation.
type CreateMessagingChannelRESTResponse struct {
	Channel   *MessagingChannelRESTResponse `json:"channel"`
	Timestamp int64                         `json:"timestamp"`
}

// MessagingChannelRESTResponse represents a channel in REST response.
type MessagingChannelRESTResponse struct {
	Name        string `json:"name"`
	Codec       string `json:"codec"`
	CreatedAt   int64  `json:"created_at"`
	Subscribers int64  `json:"subscribers"`
	Persistent  bool   `json:"persistent"`
	MaxHistory  int    `json:"max_history"`
	BufferSize  int    `json:"buffer_size"`
	TTLSeconds  int64  `json:"ttl_seconds"`
}

// GetMessagingChannelRESTResponse represents the JSON response for getting a channel.
type GetMessagingChannelRESTResponse struct {
	Channel   *MessagingChannelRESTResponse `json:"channel"`
	Timestamp int64                         `json:"timestamp"`
}

// ListMessagingChannelsRESTResponse represents the JSON response for listing channels.
type ListMessagingChannelsRESTResponse struct {
	Channels  []*MessagingChannelRESTResponse `json:"channels"`
	Total     int                             `json:"total"`
	Timestamp int64                           `json:"timestamp"`
}

// DeleteMessagingChannelRESTResponse represents the JSON response for deleting a channel.
type DeleteMessagingChannelRESTResponse struct {
	Name      string `json:"name"`
	Deleted   bool   `json:"deleted"`
	Timestamp int64  `json:"timestamp"`
}

// MessagingSubscribeRESTRequest represents the JSON request body for subscribing.
type MessagingSubscribeRESTRequest struct {
	Channel             string   `json:"channel"`
	SessionID           string   `json:"session_id,omitempty"`
	DeltaMode           bool     `json:"delta_mode,omitempty"`
	IncludeOldValues    bool     `json:"include_old_values,omitempty"`
	DedupMetadata       bool     `json:"dedup_metadata,omitempty"`
	DedupSkipDuplicates bool     `json:"dedup_skip_duplicates,omitempty"`
	BufferSize          int      `json:"buffer_size,omitempty"`
	EventTypes          []string `json:"event_types,omitempty"`
	KeyPrefix           string   `json:"key_prefix,omitempty"`
}

// MessagingSubscribeRESTResponse represents the JSON response for subscribing.
type MessagingSubscribeRESTResponse struct {
	Subscription *MessagingSubscriptionRESTResponse `json:"subscription"`
	Timestamp    int64                              `json:"timestamp"`
}

// MessagingSubscriptionRESTResponse represents a subscription in REST response.
type MessagingSubscriptionRESTResponse struct {
	ID        string `json:"id"`
	Channel   string `json:"channel"`
	ClientID  string `json:"client_id"`
	CreatedAt int64  `json:"created_at"`
}

// MessagingUnsubscribeRESTResponse represents the JSON response for unsubscribing.
type MessagingUnsubscribeRESTResponse struct {
	SubscriptionID string `json:"subscription_id"`
	Unsubscribed   bool   `json:"unsubscribed"`
	Timestamp      int64  `json:"timestamp"`
}

// MessagingPublishRESTRequest represents the JSON request body for publishing.
type MessagingPublishRESTRequest struct {
	Payload   json.RawMessage   `json:"payload"`
	MessageID string            `json:"message_id,omitempty"`
	DedupKey  string            `json:"dedup_key,omitempty"`
	Codec     string            `json:"codec,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// MessagingPublishRESTResponse represents the JSON response for publishing.
type MessagingPublishRESTResponse struct {
	Channel     string `json:"channel"`
	Published   bool   `json:"published"`
	MessageID   string `json:"message_id,omitempty"`
	IsDuplicate bool   `json:"is_duplicate,omitempty"`
	Timestamp   int64  `json:"timestamp"`
}

// MessagingPublishDeltaRESTRequest represents the JSON request body for publishing delta.
type MessagingPublishDeltaRESTRequest struct {
	Old       json.RawMessage   `json:"old"`
	New       json.RawMessage   `json:"new"`
	MessageID string            `json:"message_id,omitempty"`
	DedupKey  string            `json:"dedup_key,omitempty"`
	Codec     string            `json:"codec,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// MessagingPublishDeltaRESTResponse represents the JSON response for publishing delta.
type MessagingPublishDeltaRESTResponse struct {
	Channel     string `json:"channel"`
	Published   bool   `json:"published"`
	MessageID   string `json:"message_id,omitempty"`
	IsDuplicate bool   `json:"is_duplicate,omitempty"`
	Timestamp   int64  `json:"timestamp"`
}

// MessagingStatsRESTResponse represents the JSON response for messaging service stats.
type MessagingStatsRESTResponse struct {
	TotalChannels      int64   `json:"total_channels"`
	TotalSubscriptions int64   `json:"total_subscriptions"`
	MessagesPublished  int64   `json:"messages_published"`
	MessagesDelivered  int64   `json:"messages_delivered"`
	DedupHits          int64   `json:"dedup_hits"`
	DroppedMessages    int64   `json:"dropped_messages"`
	BufferUtilization  float64 `json:"buffer_utilization"`
	Timestamp          int64   `json:"timestamp"`
}

// MessagingChannelStatsRESTResponse represents the JSON response for per-channel stats.
type MessagingChannelStatsRESTResponse struct {
	Name              string `json:"name"`
	Subscribers       int64  `json:"subscribers"`
	MessagesPublished int64  `json:"messages_published"`
	Timestamp         int64  `json:"timestamp"`
}

// MessagingDedupStatsRESTResponse represents the JSON response for dedup stats.
type MessagingDedupStatsRESTResponse struct {
	Channel         string                         `json:"channel"`
	TotalMessages   int64                          `json:"total_messages"`
	TotalDuplicates int64                          `json:"total_duplicates"`
	TopDuplicates   []*MessagingDedupEntryResponse `json:"top_duplicates"`
	Timestamp       int64                          `json:"timestamp"`
}

// MessagingDedupEntryResponse represents a dedup entry in REST response.
type MessagingDedupEntryResponse struct {
	Count       int64  `json:"count"`
	FirstSeen   int64  `json:"first_seen"`
	LastSeen    int64  `json:"last_seen"`
	MessageID   string `json:"message_id"`
	PayloadHash string `json:"payload_hash"`
}

// MessagingServiceNotConfiguredError represents an error when messaging service is not configured.
type MessagingServiceNotConfiguredError struct{}

// Error implements the error interface.
func (e *MessagingServiceNotConfiguredError) Error() string {
	return "messaging notification service is not configured"
}

// =============================================================================
// Handler Functions
// =============================================================================

// getMessagingService returns the messaging service or responds with 501 Not Implemented.
func (s *Server) getMessagingService(w http.ResponseWriter, r *http.Request) messaging.Service {
	messagingService := s.service.Messaging()
	if messagingService == nil {
		RespondError(w, r, http.StatusNotImplemented, &MessagingServiceNotConfiguredError{})
		return nil
	}
	return messagingService
}

// HandleListMessagingChannels handles GET /api/v1/messaging/channels - List all channels.
func (s *Server) HandleListMessagingChannels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	channels, err := messagingService.ListChannels(ctx)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Apply name prefix filter from query parameter
	namePrefix := r.URL.Query().Get("name_prefix")

	restChannels := make([]*MessagingChannelRESTResponse, 0, len(channels))
	for _, ch := range channels {
		if namePrefix != "" && !strings.HasPrefix(ch.Name, namePrefix) {
			continue
		}
		restChannels = append(restChannels, messagingChannelToREST(ch))
	}

	resp := &ListMessagingChannelsRESTResponse{
		Channels:  restChannels,
		Total:     len(restChannels),
		Timestamp: time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCreateMessagingChannel handles POST /api/v1/messaging/channels - Create a channel.
func (s *Server) HandleCreateMessagingChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req CreateMessagingChannelRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Name == "" {
		RespondBadRequest(w, r, "channel name is required")
		return
	}

	// Build create channel options
	var opts []messaging.CreateChannelOption
	if req.Codec != "" {
		opts = append(opts, messaging.WithCodec(req.Codec))
	}
	if req.Persistent {
		opts = append(opts, messaging.WithPersistence(true))
	}
	if req.MaxHistory > 0 {
		opts = append(opts, messaging.WithMaxHistory(req.MaxHistory))
	}
	if req.BufferSize > 0 {
		opts = append(opts, messaging.WithChannelBufferSize(req.BufferSize))
	}
	if req.MaxSubscribers > 0 {
		opts = append(opts, messaging.WithChannelMaxSubscribers(req.MaxSubscribers))
	}
	if req.TTLSeconds > 0 {
		opts = append(opts, messaging.WithChannelTTL(time.Duration(req.TTLSeconds)*time.Second))
	}

	channel, err := messagingService.CreateChannel(ctx, req.Name, opts...)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &CreateMessagingChannelRESTResponse{
		Channel:   messagingChannelToREST(channel),
		Timestamp: time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleGetMessagingChannel handles GET /api/v1/messaging/channels/{name} - Get a channel.
func (s *Server) HandleGetMessagingChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "channel name is required")
		return
	}

	// URL-decode the name in case it was encoded
	if decodedName, err := url.PathUnescape(name); err == nil {
		name = decodedName
	}

	channel, err := messagingService.GetChannel(ctx, name)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &GetMessagingChannelRESTResponse{
		Channel:   messagingChannelToREST(channel),
		Timestamp: time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteMessagingChannel handles DELETE /api/v1/messaging/channels/{name} - Delete a channel.
func (s *Server) HandleDeleteMessagingChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "channel name is required")
		return
	}

	// URL-decode the name in case it was encoded
	if decodedName, err := url.PathUnescape(name); err == nil {
		name = decodedName
	}

	err := messagingService.DeleteChannel(ctx, name)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &DeleteMessagingChannelRESTResponse{
		Name:      name,
		Deleted:   true,
		Timestamp: time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleMessagingSubscribe handles POST /api/v1/messaging/subscribe - Subscribe to a channel.
func (s *Server) HandleMessagingSubscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req MessagingSubscribeRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Channel == "" {
		RespondBadRequest(w, r, "channel name is required")
		return
	}

	// Build subscribe options
	var opts []messaging.SubscribeOption
	if req.SessionID != "" {
		opts = append(opts, messaging.WithSession(req.SessionID))
	}
	if req.DeltaMode {
		opts = append(opts, messaging.WithDeltaMode(true))
	}
	if req.IncludeOldValues {
		opts = append(opts, messaging.WithIncludeOldValues(true))
	}
	if req.DedupMetadata {
		opts = append(opts, messaging.WithDedupMetadata(true))
	}
	if req.DedupSkipDuplicates {
		opts = append(opts, messaging.WithDedupSkipDuplicates(true))
	}
	if req.BufferSize > 0 {
		opts = append(opts, messaging.WithSubscriptionBufferSize(req.BufferSize))
	}

	// Build filter if event types or key prefix specified
	if len(req.EventTypes) > 0 || req.KeyPrefix != "" {
		filter := &messaging.SubscriptionFilter{
			KeyPrefix: req.KeyPrefix,
		}
		for _, et := range req.EventTypes {
			switch et {
			case "PUT", "put":
				filter.EventTypes = append(filter.EventTypes, messaging.EventTypePut)
			case "DELETE", "delete":
				filter.EventTypes = append(filter.EventTypes, messaging.EventTypeDelete)
			}
		}
		opts = append(opts, messaging.WithFilter(filter))
	}

	subscription, err := messagingService.Subscribe(ctx, req.Channel, opts...)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &MessagingSubscribeRESTResponse{
		Subscription: &MessagingSubscriptionRESTResponse{
			ID:        subscription.ID,
			Channel:   subscription.Channel,
			ClientID:  subscription.ClientID,
			CreatedAt: subscription.CreatedAt.Unix(),
		},
		Timestamp: time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleMessagingUnsubscribe handles DELETE /api/v1/messaging/subscribe/{id} - Unsubscribe.
func (s *Server) HandleMessagingUnsubscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	subscriptionID := chi.URLParam(r, "id")
	if subscriptionID == "" {
		RespondBadRequest(w, r, "subscription ID is required")
		return
	}

	// URL-decode the ID in case it was encoded
	if decodedID, err := url.PathUnescape(subscriptionID); err == nil {
		subscriptionID = decodedID
	}

	err := messagingService.Unsubscribe(ctx, subscriptionID)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &MessagingUnsubscribeRESTResponse{
		SubscriptionID: subscriptionID,
		Unsubscribed:   true,
		Timestamp:      time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleMessagingPublish handles POST /api/v1/messaging/publish/{channel} - Publish a message.
func (s *Server) HandleMessagingPublish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	channelName := chi.URLParam(r, "channel")
	if channelName == "" {
		RespondBadRequest(w, r, "channel name is required")
		return
	}

	// URL-decode the channel name in case it was encoded
	if decodedName, err := url.PathUnescape(channelName); err == nil {
		channelName = decodedName
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req MessagingPublishRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(req.Payload) == 0 {
		RespondBadRequest(w, r, "payload is required")
		return
	}

	// Build publish options
	var opts []messaging.PublishOption
	if req.MessageID != "" {
		opts = append(opts, messaging.WithMessageID(req.MessageID))
	}
	if req.DedupKey != "" {
		opts = append(opts, messaging.WithDedupKey(req.DedupKey))
	}
	if req.Codec != "" {
		opts = append(opts, messaging.WithPublishCodec(req.Codec))
	}
	if len(req.Metadata) > 0 {
		opts = append(opts, messaging.WithMetadata(req.Metadata))
	}

	result, err := messagingService.Publish(ctx, channelName, []byte(req.Payload), opts...)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &MessagingPublishRESTResponse{
		Channel:     channelName,
		Published:   true,
		MessageID:   result.MessageID,
		IsDuplicate: result.IsDuplicate,
		Timestamp:   time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleMessagingPublishDelta handles POST /api/v1/messaging/publish-delta/{channel} - Publish delta.
func (s *Server) HandleMessagingPublishDelta(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	channelName := chi.URLParam(r, "channel")
	if channelName == "" {
		RespondBadRequest(w, r, "channel name is required")
		return
	}

	// URL-decode the channel name in case it was encoded
	if decodedName, err := url.PathUnescape(channelName); err == nil {
		channelName = decodedName
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req MessagingPublishDeltaRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(req.Old) == 0 {
		RespondBadRequest(w, r, "old value is required")
		return
	}

	if len(req.New) == 0 {
		RespondBadRequest(w, r, "new value is required")
		return
	}

	// Build publish options
	var opts []messaging.PublishOption
	if req.MessageID != "" {
		opts = append(opts, messaging.WithMessageID(req.MessageID))
	}
	if req.DedupKey != "" {
		opts = append(opts, messaging.WithDedupKey(req.DedupKey))
	}
	if req.Codec != "" {
		opts = append(opts, messaging.WithPublishCodec(req.Codec))
	}
	if len(req.Metadata) > 0 {
		opts = append(opts, messaging.WithMetadata(req.Metadata))
	}

	// Parse old and new values as interface{} for delta computation
	var oldValue, newValue interface{}
	if err := json.Unmarshal(req.Old, &oldValue); err != nil {
		RespondBadRequest(w, r, "invalid old value JSON")
		return
	}
	if err := json.Unmarshal(req.New, &newValue); err != nil {
		RespondBadRequest(w, r, "invalid new value JSON")
		return
	}

	result, err := messagingService.PublishDelta(ctx, channelName, oldValue, newValue, opts...)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &MessagingPublishDeltaRESTResponse{
		Channel:     channelName,
		Published:   true,
		MessageID:   result.MessageID,
		IsDuplicate: result.IsDuplicate,
		Timestamp:   time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleMessagingStats handles GET /api/v1/messaging/stats - Get messaging service stats.
func (s *Server) HandleMessagingStats(w http.ResponseWriter, r *http.Request) {
	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	stats := messagingService.GetStats()

	resp := &MessagingStatsRESTResponse{
		TotalChannels:      stats.TotalChannels,
		TotalSubscriptions: stats.TotalSubscriptions,
		MessagesPublished:  stats.MessagesPublished,
		MessagesDelivered:  stats.MessagesDelivered,
		DedupHits:          stats.DedupHits,
		DroppedMessages:    stats.DroppedMessages,
		BufferUtilization:  stats.BufferUtilization,
		Timestamp:          time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleMessagingChannelStats handles GET /api/v1/messaging/stats/{channel} - Get per-channel stats.
func (s *Server) HandleMessagingChannelStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	channelName := chi.URLParam(r, "channel")
	if channelName == "" {
		RespondBadRequest(w, r, "channel name is required")
		return
	}

	if decodedName, err := url.PathUnescape(channelName); err == nil {
		channelName = decodedName
	}

	channel, err := messagingService.GetChannel(ctx, channelName)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := &MessagingChannelStatsRESTResponse{
		Name:              channel.Name,
		Subscribers:       channel.Subscribers,
		MessagesPublished: channel.MessagesPublished,
		Timestamp:         time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleMessagingDedupStats handles GET /api/v1/messaging/dedup/stats/{channel} - Get dedup stats.
func (s *Server) HandleMessagingDedupStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	messagingService := s.getMessagingService(w, r)
	if messagingService == nil {
		return
	}

	channelName := chi.URLParam(r, "channel")
	if channelName == "" {
		RespondBadRequest(w, r, "channel name is required")
		return
	}

	// URL-decode the channel name in case it was encoded
	if decodedName, err := url.PathUnescape(channelName); err == nil {
		channelName = decodedName
	}

	// Validate the channel exists
	if _, chErr := messagingService.GetChannel(ctx, channelName); chErr != nil {
		s.handleServiceError(w, r, chErr)
		return
	}

	// Get the underlying MessagingService implementation to access dedup store.
	// Cast to *messaging.PushService to access GetDedupStore method.
	msgServiceImpl, ok := messagingService.(*messaging.PushService)
	if !ok {
		RespondError(w, r, http.StatusNotImplemented, &MessagingServiceNotConfiguredError{})
		return
	}

	dedupStore := msgServiceImpl.GetDedupStore()
	if dedupStore == nil {
		RespondBadRequest(w, r, "deduplication is not enabled")
		return
	}

	stats, err := dedupStore.GetStats(ctx, channelName)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	topDuplicates := make([]*MessagingDedupEntryResponse, 0, len(stats.TopDuplicates))
	for _, entry := range stats.TopDuplicates {
		topDuplicates = append(topDuplicates, &MessagingDedupEntryResponse{
			Count:       entry.Count,
			FirstSeen:   entry.FirstSeen.Unix(),
			LastSeen:    entry.LastSeen.Unix(),
			MessageID:   entry.MessageID,
			PayloadHash: entry.PayloadHash,
		})
	}

	resp := &MessagingDedupStatsRESTResponse{
		Channel:         stats.Channel,
		TotalMessages:   stats.TotalMessages,
		TotalDuplicates: stats.TotalDuplicates,
		TopDuplicates:   topDuplicates,
		Timestamp:       time.Now().Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// messagingChannelToREST converts a messaging.Channel to REST response format.
func messagingChannelToREST(ch *messaging.Channel) *MessagingChannelRESTResponse {
	if ch == nil {
		return nil
	}
	return &MessagingChannelRESTResponse{
		Name:        ch.Name,
		Codec:       ch.Codec,
		CreatedAt:   ch.CreatedAt.Unix(),
		Subscribers: ch.Subscribers,
		Persistent:  ch.Persistent,
		MaxHistory:  ch.MaxHistory,
		BufferSize:  ch.BufferSize,
		TTLSeconds:  int64(ch.TTL.Seconds()),
	}
}
