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

package grpc

import (
	"context"
	stderrors "errors"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/messagingwatch"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// MessagingServer wraps the messaging service for gRPC endpoints.
type MessagingServer struct {
	pb.UnimplementedMessagingServiceServer
	messagingSvc messaging.Service
	logger       *logger.Logger
}

// NewMessagingServer creates a new gRPC server for messaging operations.
func NewMessagingServer(messagingSvc messaging.Service, log *logger.Logger) *MessagingServer {
	return &MessagingServer{
		messagingSvc: messagingSvc,
		logger:       log.WithField("component", "messaging-grpc-server"),
	}
}

// CreateChannel creates a new notification channel.
func (s *MessagingServer) CreateChannel(ctx context.Context, req *pb.MessagingCreateChannelRequest) (*pb.MessagingCreateChannelResponse, error) {
	s.logger.WithField("name", req.Name).Debug("Processing CreateChannel request")

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "channel name is required")
	}

	opts := buildCreateChannelOptions(req)

	channel, err := s.messagingSvc.CreateChannel(ctx, req.Name, opts...)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	protoChannel, err := serviceChannelToProto(channel)
	if err != nil {
		return nil, err
	}

	return &pb.MessagingCreateChannelResponse{
		Channel:   protoChannel,
		Timestamp: time.Now().Unix(),
	}, nil
}

// DeleteChannel deletes a notification channel.
func (s *MessagingServer) DeleteChannel(ctx context.Context, req *pb.MessagingDeleteChannelRequest) (*pb.MessagingDeleteChannelResponse, error) {
	s.logger.WithField("name", req.Name).Debug("Processing DeleteChannel request")

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "channel name is required")
	}

	err := s.messagingSvc.DeleteChannel(ctx, req.Name)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	return &pb.MessagingDeleteChannelResponse{
		Name:      req.Name,
		Deleted:   true,
		Timestamp: time.Now().Unix(),
	}, nil
}

// GetChannel retrieves channel information.
func (s *MessagingServer) GetChannel(ctx context.Context, req *pb.MessagingGetChannelRequest) (*pb.MessagingGetChannelResponse, error) {
	s.logger.WithField("name", req.Name).Debug("Processing GetChannel request")

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "channel name is required")
	}

	channel, err := s.messagingSvc.GetChannel(ctx, req.Name)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	protoChannel, err := serviceChannelToProto(channel)
	if err != nil {
		return nil, err
	}

	return &pb.MessagingGetChannelResponse{
		Channel:   protoChannel,
		Timestamp: time.Now().Unix(),
	}, nil
}

// ListChannels lists all notification channels, optionally filtering by name prefix.
func (s *MessagingServer) ListChannels(ctx context.Context, req *pb.MessagingListChannelsRequest) (*pb.MessagingListChannelsResponse, error) {
	s.logger.Debug("Processing ListChannels request")

	channels, err := s.messagingSvc.ListChannels(ctx)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	// Apply name prefix filter if specified
	if req != nil && req.NamePrefix != "" {
		filtered := make([]*messaging.Channel, 0, len(channels))
		for _, ch := range channels {
			if strings.HasPrefix(ch.Name, req.NamePrefix) {
				filtered = append(filtered, ch)
			}
		}
		channels = filtered
	}

	// Apply limit if specified
	if req != nil && req.Limit > 0 && int(req.Limit) < len(channels) {
		channels = channels[:req.Limit]
	}

	protoChannels := make([]*pb.Channel, len(channels))
	for i, ch := range channels {
		protoChannels[i], err = serviceChannelToProto(ch)
		if err != nil {
			return nil, err
		}
	}

	totalCount, err := responseInt32("total_count", len(channels))
	if err != nil {
		return nil, err
	}

	return &pb.MessagingListChannelsResponse{
		Channels:   protoChannels,
		TotalCount: totalCount,
		HasMore:    false,
		Timestamp:  time.Now().Unix(),
	}, nil
}

// Subscribe subscribes to a notification channel.
func (s *MessagingServer) Subscribe(ctx context.Context, req *pb.MessagingSubscribeRequest) (*pb.MessagingSubscribeResponse, error) {
	s.logger.WithField("channel", req.Channel).Debug("Processing Subscribe request")

	if req.Channel == "" {
		return nil, status.Error(codes.InvalidArgument, "channel name is required")
	}

	opts := buildSubscribeOptions(req)

	subscription, err := s.messagingSvc.Subscribe(ctx, req.Channel, opts...)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	return &pb.MessagingSubscribeResponse{
		Subscription: serviceSubscriptionToProto(subscription),
		Timestamp:    time.Now().Unix(),
	}, nil
}

// Unsubscribe removes a subscription.
func (s *MessagingServer) Unsubscribe(ctx context.Context, req *pb.MessagingUnsubscribeRequest) (*pb.MessagingUnsubscribeResponse, error) {
	s.logger.WithField("subscription_id", req.SubscriptionId).Debug("Processing Unsubscribe request")

	if req.SubscriptionId == "" {
		return nil, status.Error(codes.InvalidArgument, "subscription_id is required")
	}

	err := s.messagingSvc.Unsubscribe(ctx, req.SubscriptionId)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	return &pb.MessagingUnsubscribeResponse{
		SubscriptionId: req.SubscriptionId,
		Unsubscribed:   true,
		Timestamp:      time.Now().Unix(),
	}, nil
}

// Publish publishes a message to a channel.
func (s *MessagingServer) Publish(ctx context.Context, req *pb.MessagingPublishRequest) (*pb.MessagingPublishResponse, error) {
	s.logger.WithField("channel", req.Channel).Debug("Processing Publish request")

	if req.Channel == "" {
		return nil, status.Error(codes.InvalidArgument, "channel name is required")
	}

	opts := buildPublishOptions(req.MessageId, req.DedupKey, req.Metadata)

	result, err := s.messagingSvc.Publish(ctx, req.Channel, req.Payload, opts...)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	return &pb.MessagingPublishResponse{
		MessageId:   result.MessageID,
		Channel:     req.Channel,
		IsDuplicate: result.IsDuplicate,
		Timestamp:   time.Now().Unix(),
	}, nil
}

// PublishDelta publishes a delta message to a channel.
func (s *MessagingServer) PublishDelta(ctx context.Context, req *pb.MessagingPublishDeltaRequest) (*pb.MessagingPublishDeltaResponse, error) {
	s.logger.WithField("channel", req.Channel).Debug("Processing PublishDelta request")

	if req.Channel == "" {
		return nil, status.Error(codes.InvalidArgument, "channel name is required")
	}

	opts := buildPublishOptions(req.MessageId, req.DedupKey, req.Metadata)

	result, err := s.messagingSvc.PublishDelta(ctx, req.Channel, req.OldValue, req.NewValue, opts...)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	return &pb.MessagingPublishDeltaResponse{
		MessageId:   result.MessageID,
		Channel:     req.Channel,
		IsDuplicate: result.IsDuplicate,
		Timestamp:   time.Now().Unix(),
	}, nil
}

// StreamNotifications streams notifications to the client.
func (s *MessagingServer) StreamNotifications(req *pb.MessagingStreamNotificationsRequest, stream grpc.ServerStreamingServer[pb.MessagingNotification]) error {
	s.logger.WithField("subscription_id", req.SubscriptionId).Debug("Processing StreamNotifications request")

	if req.SubscriptionId == "" {
		return status.Error(codes.InvalidArgument, "subscription_id is required")
	}

	// Get the PushService to access the subscription client
	pushSvc, ok := s.messagingSvc.(*messaging.PushService)
	if !ok {
		return status.Error(codes.Internal, "messaging service does not support streaming")
	}

	client, found := pushSvc.GetSubscriptionClient(req.SubscriptionId)
	if !found {
		return status.Error(codes.NotFound, "subscription not found")
	}

	subscription, found := pushSvc.GetSubscription(req.SubscriptionId)
	if !found {
		return status.Error(codes.NotFound, "subscription not found")
	}

	ctx := stream.Context()
	var sequenceNumber atomic.Uint64

	// Stream messages from the client's message channel
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-client.Send:
			if !ok {
				return status.Error(codes.Aborted, "subscription closed")
			}

			seq := sequenceNumber.Add(1)

			notification := &pb.MessagingNotification{
				Message:        serviceMessageToProto(msg),
				SubscriptionId: subscription.ID,
				SequenceNumber: seq,
			}

			if err := stream.Send(notification); err != nil {
				return err
			}
		}
	}
}

// WatchChannel creates a subscription that watches for key changes.
func (s *MessagingServer) WatchChannel(ctx context.Context, req *pb.MessagingWatchChannelRequest) (*pb.MessagingWatchChannelResponse, error) {
	s.logger.WithField("key_prefix", req.KeyPrefix).Debug("Processing WatchChannel request")

	if req.KeyPrefix == "" {
		return nil, status.Error(codes.InvalidArgument, "key_prefix is required")
	}

	subscription, err := messagingwatch.Watch(ctx, s.messagingSvc, watchRequestFromProto(req))
	if err != nil {
		return nil, convertMessagingError(err)
	}

	return &pb.MessagingWatchChannelResponse{
		Subscription: serviceSubscriptionToProto(subscription),
		ChannelName:  messagingwatch.ChannelName(req.KeyPrefix),
		Timestamp:    time.Now().Unix(),
	}, nil
}

// GetStats retrieves messaging service statistics.
func (s *MessagingServer) GetStats(_ context.Context, _ *pb.MessagingGetStatsRequest) (*pb.MessagingGetStatsResponse, error) {
	s.logger.Debug("Processing GetStats request")

	stats := s.messagingSvc.GetStats()
	isRunning := s.messagingSvc.IsRunning()

	return &pb.MessagingGetStatsResponse{
		Stats:     serviceStatsToProtoMessaging(stats),
		IsRunning: isRunning,
		Timestamp: time.Now().Unix(),
	}, nil
}

// GetDedupStats retrieves deduplication statistics for a channel.
func (s *MessagingServer) GetDedupStats(ctx context.Context, req *pb.MessagingGetDedupStatsRequest) (*pb.MessagingGetDedupStatsResponse, error) {
	s.logger.WithField("channel", req.Channel).Debug("Processing GetDedupStats request")

	if req.Channel == "" {
		return nil, status.Error(codes.InvalidArgument, "channel name is required")
	}

	// Validate the channel exists via the service layer
	_, err := s.messagingSvc.GetChannel(ctx, req.Channel)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	// Get the PushService to access dedup store
	pushSvc, ok := s.messagingSvc.(*messaging.PushService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "messaging service does not support dedup stats")
	}

	dedupStore := pushSvc.GetDedupStore()
	if dedupStore == nil {
		return &pb.MessagingGetDedupStatsResponse{
			Stats: &pb.DedupStats{
				Channel:         req.Channel,
				TotalMessages:   0,
				TotalDuplicates: 0,
			},
			Timestamp: time.Now().Unix(),
		}, nil
	}

	// Retrieve actual dedup stats from the store
	stats, statsErr := dedupStore.GetStats(ctx, req.Channel)
	if statsErr != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve dedup stats: %v", statsErr)
	}

	return &pb.MessagingGetDedupStatsResponse{
		Stats: &pb.DedupStats{
			Channel:         stats.Channel,
			TotalMessages:   stats.TotalMessages,
			TotalDuplicates: stats.TotalDuplicates,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

// GetChannelStats retrieves statistics for a specific channel.
func (s *MessagingServer) GetChannelStats(ctx context.Context, req *pb.MessagingGetChannelStatsRequest) (*pb.MessagingGetChannelStatsResponse, error) {
	s.logger.WithField("name", req.Name).Debug("Processing GetChannelStats request")

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "channel name is required")
	}

	channel, err := s.messagingSvc.GetChannel(ctx, req.Name)
	if err != nil {
		return nil, convertMessagingError(err)
	}

	return &pb.MessagingGetChannelStatsResponse{
		Stats: &pb.ChannelStats{
			Name:              channel.Name,
			Subscribers:       channel.Subscribers,
			MessagesPublished: channel.MessagesPublished,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

// =============================================================================
// Helper Functions - Build Options
// =============================================================================

// buildCreateChannelOptions builds create channel options from the proto request.
func buildCreateChannelOptions(req *pb.MessagingCreateChannelRequest) []messaging.CreateChannelOption {
	var opts []messaging.CreateChannelOption

	if req.Codec != "" {
		opts = append(opts, messaging.WithCodec(req.Codec))
	}
	if req.Persistent {
		opts = append(opts, messaging.WithPersistence(req.Persistent))
	}
	if req.MaxHistory > 0 {
		opts = append(opts, messaging.WithMaxHistory(int(req.MaxHistory)))
	}
	if req.BufferSize > 0 {
		opts = append(opts, messaging.WithChannelBufferSize(int(req.BufferSize)))
	}
	if req.MaxSubscribers > 0 {
		opts = append(opts, messaging.WithChannelMaxSubscribers(int(req.MaxSubscribers)))
	}
	if req.TtlSeconds > 0 {
		opts = append(opts, messaging.WithChannelTTL(time.Duration(req.TtlSeconds)*time.Second))
	}

	return opts
}

// buildSubscribeOptions builds subscribe options from the proto request.
func buildSubscribeOptions(req *pb.MessagingSubscribeRequest) []messaging.SubscribeOption {
	var opts []messaging.SubscribeOption

	if req.SessionId != "" {
		opts = append(opts, messaging.WithSession(req.SessionId))
	}
	if req.Filter != nil {
		opts = append(opts, messaging.WithFilter(protoFilterToService(req.Filter)))
	}
	if req.DeltaMode {
		opts = append(opts, messaging.WithDeltaMode(req.DeltaMode))
	}
	if req.IncludeOldValues {
		opts = append(opts, messaging.WithIncludeOldValues(req.IncludeOldValues))
	}
	if req.DedupMetadata {
		opts = append(opts, messaging.WithDedupMetadata(req.DedupMetadata))
	}
	if req.DedupSkipDuplicates {
		opts = append(opts, messaging.WithDedupSkipDuplicates(req.DedupSkipDuplicates))
	}
	if req.BufferSize > 0 {
		opts = append(opts, messaging.WithSubscriptionBufferSize(int(req.BufferSize)))
	}

	return opts
}

// buildPublishOptions builds publish options from request parameters.
func buildPublishOptions(messageID, dedupKey string, metadata map[string]string) []messaging.PublishOption {
	var opts []messaging.PublishOption

	if messageID != "" {
		opts = append(opts, messaging.WithMessageID(messageID))
	}
	if dedupKey != "" {
		opts = append(opts, messaging.WithDedupKey(dedupKey))
	}
	if len(metadata) > 0 {
		opts = append(opts, messaging.WithMetadata(metadata))
	}

	return opts
}

// watchRequestFromProto decodes the proto request into the transport-neutral
// watch request every server serves WatchChannel from.
func watchRequestFromProto(req *pb.MessagingWatchChannelRequest) messagingwatch.Request {
	return messagingwatch.Request{
		KeyPrefix:  req.KeyPrefix,
		SessionID:  req.SessionId,
		Filter:     protoFilterToService(req.Filter),
		BufferSize: int(req.BufferSize),
	}
}

// =============================================================================
// Type Conversion Helpers - Proto to Service
// =============================================================================

// protoFilterToService converts pb.SubscriptionFilter to messaging.SubscriptionFilter.
func protoFilterToService(filter *pb.SubscriptionFilter) *messaging.SubscriptionFilter {
	if filter == nil {
		return nil
	}

	var eventTypes []messaging.EventType
	for _, et := range filter.EventTypes {
		eventTypes = append(eventTypes, protoEventTypeToService(et))
	}

	return &messaging.SubscriptionFilter{
		EventTypes: eventTypes,
		KeyPrefix:  filter.KeyPrefix,
	}
}

// protoEventTypeToService converts pb.EventType to messaging.EventType.
func protoEventTypeToService(et pb.EventType) messaging.EventType {
	switch et {
	case pb.EventType_EVENT_TYPE_PUT:
		return messaging.EventTypePut
	case pb.EventType_EVENT_TYPE_DELETE:
		return messaging.EventTypeDelete
	default:
		return messaging.EventTypePut
	}
}

// =============================================================================
// Type Conversion Helpers - Service to Proto
// =============================================================================

// serviceChannelToProto converts messaging.Channel to pb.Channel.
func serviceChannelToProto(channel *messaging.Channel) (*pb.Channel, error) {
	if channel == nil {
		return nil, nil
	}

	var narrow responseNarrower
	out := &pb.Channel{
		Name:        channel.Name,
		Codec:       channel.Codec,
		CreatedAt:   channel.CreatedAt.Unix(),
		Subscribers: channel.Subscribers,
		Persistent:  channel.Persistent,
		MaxHistory:  narrow.int32("max_history", channel.MaxHistory),
		BufferSize:  narrow.int32("buffer_size", channel.BufferSize),
		TtlSeconds:  int64(channel.TTL.Seconds()),
	}
	if err := narrow.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// serviceSubscriptionToProto converts messaging.Subscription to pb.Subscription.
func serviceSubscriptionToProto(sub *messaging.Subscription) *pb.Subscription {
	if sub == nil {
		return nil
	}

	return &pb.Subscription{
		Id:        sub.ID,
		Channel:   sub.Channel,
		ClientId:  sub.ClientID,
		CreatedAt: sub.CreatedAt.Unix(),
		Filters:   serviceFilterToProto(sub.Filters),
	}
}

// serviceFilterToProto converts messaging.SubscriptionFilter to pb.SubscriptionFilter.
func serviceFilterToProto(filter *messaging.SubscriptionFilter) *pb.SubscriptionFilter {
	if filter == nil {
		return nil
	}

	var eventTypes []pb.EventType
	for _, et := range filter.EventTypes {
		eventTypes = append(eventTypes, serviceEventTypeToProto(et))
	}

	return &pb.SubscriptionFilter{
		EventTypes: eventTypes,
		KeyPrefix:  filter.KeyPrefix,
	}
}

// serviceEventTypeToProto converts messaging.EventType to pb.EventType.
func serviceEventTypeToProto(et messaging.EventType) pb.EventType {
	switch et {
	case messaging.EventTypePut:
		return pb.EventType_EVENT_TYPE_PUT
	case messaging.EventTypeDelete:
		return pb.EventType_EVENT_TYPE_DELETE
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

// serviceMessageToProto converts messaging.Message to pb.Message.
func serviceMessageToProto(msg *messaging.Message) *pb.Message {
	if msg == nil {
		return nil
	}

	return &pb.Message{
		Id:        msg.ID,
		Type:      serviceMessageTypeToProto(msg.Type),
		Channel:   msg.Channel,
		Payload:   msg.Payload,
		Timestamp: msg.Timestamp.Unix(),
		Metadata:  msg.Metadata,
	}
}

// serviceMessageTypeToProto converts messaging.MessageType to pb.MessageType.
func serviceMessageTypeToProto(mt messaging.MessageType) pb.MessageType {
	switch mt {
	case messaging.MessageTypeSubscribe:
		return pb.MessageType_MESSAGE_TYPE_SUBSCRIBE
	case messaging.MessageTypeUnsubscribe:
		return pb.MessageType_MESSAGE_TYPE_UNSUBSCRIBE
	case messaging.MessageTypePublish:
		return pb.MessageType_MESSAGE_TYPE_PUBLISH
	case messaging.MessageTypeNotification:
		return pb.MessageType_MESSAGE_TYPE_NOTIFICATION
	case messaging.MessageTypePing:
		return pb.MessageType_MESSAGE_TYPE_PING
	case messaging.MessageTypePong:
		return pb.MessageType_MESSAGE_TYPE_PONG
	case messaging.MessageTypeAck:
		return pb.MessageType_MESSAGE_TYPE_ACK
	case messaging.MessageTypeError:
		return pb.MessageType_MESSAGE_TYPE_ERROR
	default:
		return pb.MessageType_MESSAGE_TYPE_UNSPECIFIED
	}
}

// serviceStatsToProtoMessaging converts messaging.Stats to pb.Stats.
func serviceStatsToProtoMessaging(stats *messaging.Stats) *pb.Stats {
	if stats == nil {
		return nil
	}

	return &pb.Stats{
		TotalChannels:      stats.TotalChannels,
		TotalSubscriptions: stats.TotalSubscriptions,
		MessagesPublished:  stats.MessagesPublished,
		MessagesDelivered:  stats.MessagesDelivered,
		DedupHits:          stats.DedupHits,
		DroppedMessages:    stats.DroppedMessages,
		BufferUtilization:  stats.BufferUtilization,
	}
}

// =============================================================================
// Error Handling
// =============================================================================

// convertMessagingError converts internal messaging errors to gRPC status errors.
func convertMessagingError(err error) error {
	if err == nil {
		return nil
	}

	// Check for messaging-specific errors
	if messaging.IsChannelNotFound(err) {
		return status.Error(codes.NotFound, err.Error())
	}
	if messaging.IsChannelExists(err) {
		return status.Error(codes.AlreadyExists, err.Error())
	}
	if messaging.IsSubscriptionNotFound(err) {
		return status.Error(codes.NotFound, err.Error())
	}
	if messaging.IsInvalidChannelName(err) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if messaging.IsServiceNotRunning(err) {
		return status.Error(codes.Unavailable, err.Error())
	}
	if messaging.IsMaxSubscriptionsReached(err) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}
	if messaging.IsHubBufferFull(err) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}
	if messaging.IsClientDisconnected(err) {
		return status.Error(codes.Aborted, err.Error())
	}
	if messaging.IsClientSlow(err) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}

	// Check for generic QBError
	var dErr *errors.QBError
	if !stderrors.As(err, &dErr) {
		return status.Error(codes.Internal, err.Error())
	}

	return status.Error(errorCodeToGRPCCode(dErr.Code), dErr.Error())
}

// Ensure MessagingServer implements the interface.
var _ pb.MessagingServiceServer = (*MessagingServer)(nil)
