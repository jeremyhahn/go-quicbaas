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
	"io"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// MessagingChannel represents a notification channel.
type MessagingChannel struct {
	Name        string
	Codec       string
	CreatedAt   time.Time
	Subscribers int64
	Persistent  bool
	MaxHistory  int32
	BufferSize  int32
	TTLSeconds  int64
}

// MessagingSubscription represents a client's subscription to a channel.
type MessagingSubscription struct {
	ID        string
	Channel   string
	ClientID  string
	CreatedAt time.Time
	Filter    *MessagingSubscriptionFilter
}

// MessagingSubscriptionFilter defines filtering criteria for subscriptions.
type MessagingSubscriptionFilter struct {
	EventTypes []string
	KeyPrefix  string
}

// MessagingMessage represents a messaging notification message.
type MessagingMessage struct {
	ID        string
	Type      string
	Channel   string
	Payload   []byte
	Timestamp time.Time
	Source    string
	Metadata  map[string]string
	Dedup     *MessagingDedupInfo
}

// MessagingDedupInfo contains deduplication metadata for a message.
type MessagingDedupInfo struct {
	IsDuplicate bool
	Count       int64
	FirstSeen   time.Time
}

// MessagingNotification represents a real-time messaging notification in the stream.
type MessagingNotification struct {
	Message        *MessagingMessage
	SubscriptionID string
	SequenceNumber uint64
}

// MessagingStats contains statistics about the messaging service.
type MessagingStats struct {
	TotalChannels      int64
	TotalSubscriptions int64
	MessagesPublished  int64
	MessagesDelivered  int64
	DedupHits          int64
	DroppedMessages    int64
	BufferUtilization  float64
}

// MessagingChannelStats contains statistics for a specific channel.
type MessagingChannelStats struct {
	Name              string
	Subscribers       int64
	MessagesPublished int64
	MessagesInHistory int32
}

// MessagingDedupEntry stores deduplication metadata for a message.
type MessagingDedupEntry struct {
	Count       int64
	FirstSeen   time.Time
	LastSeen    time.Time
	MessageID   string
	PayloadHash string
}

// MessagingDedupStats provides statistics about deduplication for a channel.
type MessagingDedupStats struct {
	Channel         string
	TotalMessages   int64
	TotalDuplicates int64
	TopDuplicates   []*MessagingDedupEntry
}

// MessagingDeltaPayload represents changes to an entity for efficient delta updates.
type MessagingDeltaPayload struct {
	EntityID    string
	EntityType  string
	Version     uint64
	PrevVersion uint64
	Timestamp   time.Time
	Changes     []*MessagingFieldChange
}

// MessagingFieldChange represents a single field modification in a delta payload.
type MessagingFieldChange struct {
	Path      string
	Operation string
	Value     []byte
	OldValue  []byte
}

// Request types for messaging operations.

// MessagingCreateChannelRequest contains parameters for creating a channel.
type MessagingCreateChannelRequest struct {
	Name           string
	Codec          string
	Persistent     bool
	MaxHistory     int32
	BufferSize     int32
	MaxSubscribers int32
	TTLSeconds     int64
}

// MessagingCreateChannelResponse contains the result of creating a channel.
type MessagingCreateChannelResponse struct {
	Channel   *MessagingChannel
	Timestamp time.Time
}

// MessagingDeleteChannelRequest contains parameters for deleting a channel.
type MessagingDeleteChannelRequest struct {
	Name string
}

// MessagingDeleteChannelResponse contains the result of deleting a channel.
type MessagingDeleteChannelResponse struct {
	Name      string
	Deleted   bool
	Timestamp time.Time
}

// MessagingGetChannelRequest contains parameters for getting channel information.
type MessagingGetChannelRequest struct {
	Name string
}

// MessagingGetChannelResponse contains the channel information.
type MessagingGetChannelResponse struct {
	Channel   *MessagingChannel
	Timestamp time.Time
}

// MessagingListChannelsRequest contains parameters for listing channels.
type MessagingListChannelsRequest struct {
	Limit      int32
	Cursor     string
	NamePrefix string
}

// MessagingListChannelsResponse contains the list of channels.
type MessagingListChannelsResponse struct {
	Channels   []*MessagingChannel
	NextCursor string
	HasMore    bool
	TotalCount int32
	Timestamp  time.Time
}

// MessagingSubscribeRequest contains parameters for subscribing to a channel.
type MessagingSubscribeRequest struct {
	Channel             string
	SessionID           string
	Filter              *MessagingSubscriptionFilter
	DeltaMode           bool
	IncludeOldValues    bool
	DedupMetadata       bool
	DedupSkipDuplicates bool
	BufferSize          int32
}

// MessagingSubscribeResponse contains the result of subscribing to a channel.
type MessagingSubscribeResponse struct {
	Subscription *MessagingSubscription
	Timestamp    time.Time
}

// MessagingUnsubscribeRequest contains parameters for unsubscribing from a channel.
type MessagingUnsubscribeRequest struct {
	SubscriptionID string
}

// MessagingUnsubscribeResponse contains the result of unsubscribing from a channel.
type MessagingUnsubscribeResponse struct {
	SubscriptionID string
	Unsubscribed   bool
	Timestamp      time.Time
}

// MessagingPublishRequest contains parameters for publishing a message.
type MessagingPublishRequest struct {
	Channel   string
	Payload   []byte
	MessageID string
	DedupKey  string
	Codec     string
	Metadata  map[string]string
}

// MessagingPublishResponse contains the result of publishing a message.
type MessagingPublishResponse struct {
	MessageID   string
	Channel     string
	IsDuplicate bool
	Timestamp   time.Time
}

// MessagingPublishDeltaRequest contains parameters for publishing a delta message.
type MessagingPublishDeltaRequest struct {
	Channel   string
	OldValue  []byte
	NewValue  []byte
	MessageID string
	DedupKey  string
	Codec     string
	Metadata  map[string]string
}

// MessagingPublishDeltaResponse contains the result of publishing a delta message.
type MessagingPublishDeltaResponse struct {
	MessageID   string
	Channel     string
	Delta       *MessagingDeltaPayload
	IsDuplicate bool
	Timestamp   time.Time
}

// MessagingStreamNotificationsRequest contains parameters for streaming notifications.
type MessagingStreamNotificationsRequest struct {
	SubscriptionID string
	StartSequence  uint64
	IncludeHistory bool
	HistoryLimit   int32
}

// MessagingWatchChannelRequest contains parameters for watching key changes.
type MessagingWatchChannelRequest struct {
	KeyPrefix  string
	SessionID  string
	Filter     *MessagingSubscriptionFilter
	BufferSize int32
}

// MessagingWatchChannelResponse contains the result of watching key changes.
type MessagingWatchChannelResponse struct {
	Subscription *MessagingSubscription
	ChannelName  string
	Timestamp    time.Time
}

// MessagingGetStatsResponse contains the messaging service statistics.
type MessagingGetStatsResponse struct {
	Stats     *MessagingStats
	IsRunning bool
	Timestamp time.Time
}

// MessagingGetDedupStatsRequest contains parameters for getting dedup stats.
type MessagingGetDedupStatsRequest struct {
	Channel string
}

// MessagingGetDedupStatsResponse contains the deduplication statistics.
type MessagingGetDedupStatsResponse struct {
	Stats     *MessagingDedupStats
	Timestamp time.Time
}

// MessagingGetChannelStatsRequest contains parameters for getting channel stats.
type MessagingGetChannelStatsRequest struct {
	Name string
}

// MessagingGetChannelStatsResponse contains the channel statistics.
type MessagingGetChannelStatsResponse struct {
	Stats     *MessagingChannelStats
	Timestamp time.Time
}

// MessagingCreateChannel creates a new notification channel.
func (c *Client) MessagingCreateChannel(ctx context.Context, req *MessagingCreateChannelRequest) (*MessagingCreateChannelResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingCreateChannel",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingCreateChannelRequest{
		Name:           req.Name,
		Codec:          req.Codec,
		Persistent:     req.Persistent,
		MaxHistory:     req.MaxHistory,
		BufferSize:     req.BufferSize,
		MaxSubscribers: req.MaxSubscribers,
		TtlSeconds:     req.TTLSeconds,
	}

	pbResp, err := messagingClient.CreateChannel(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingCreateChannelResponse{
		Channel:   convertProtoChannel(pbResp.Channel),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingDeleteChannel deletes a notification channel.
func (c *Client) MessagingDeleteChannel(ctx context.Context, req *MessagingDeleteChannelRequest) (*MessagingDeleteChannelResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingDeleteChannel",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingDeleteChannelRequest{
		Name: req.Name,
	}

	pbResp, err := messagingClient.DeleteChannel(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingDeleteChannelResponse{
		Name:      pbResp.Name,
		Deleted:   pbResp.Deleted,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingGetChannel retrieves channel information.
func (c *Client) MessagingGetChannel(ctx context.Context, req *MessagingGetChannelRequest) (*MessagingGetChannelResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingGetChannel",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingGetChannelRequest{
		Name: req.Name,
	}

	pbResp, err := messagingClient.GetChannel(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingGetChannelResponse{
		Channel:   convertProtoChannel(pbResp.Channel),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingListChannels lists all notification channels.
func (c *Client) MessagingListChannels(ctx context.Context, req *MessagingListChannelsRequest) (*MessagingListChannelsResponse, error) {
	if req == nil {
		req = &MessagingListChannelsRequest{}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingListChannelsRequest{
		Limit:      req.Limit,
		Cursor:     req.Cursor,
		NamePrefix: req.NamePrefix,
	}

	pbResp, err := messagingClient.ListChannels(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	channels := make([]*MessagingChannel, len(pbResp.Channels))
	for i, ch := range pbResp.Channels {
		channels[i] = convertProtoChannel(ch)
	}

	return &MessagingListChannelsResponse{
		Channels:   channels,
		NextCursor: pbResp.NextCursor,
		HasMore:    pbResp.HasMore,
		TotalCount: pbResp.TotalCount,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingSubscribe subscribes to a notification channel.
func (c *Client) MessagingSubscribe(ctx context.Context, req *MessagingSubscribeRequest) (*MessagingSubscribeResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingSubscribe",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingSubscribeRequest{
		Channel:             req.Channel,
		SessionId:           req.SessionID,
		Filter:              convertSubscriptionFilterToProto(req.Filter),
		DeltaMode:           req.DeltaMode,
		IncludeOldValues:    req.IncludeOldValues,
		DedupMetadata:       req.DedupMetadata,
		DedupSkipDuplicates: req.DedupSkipDuplicates,
		BufferSize:          req.BufferSize,
	}

	pbResp, err := messagingClient.Subscribe(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingSubscribeResponse{
		Subscription: convertProtoSubscription(pbResp.Subscription),
		Timestamp:    time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingUnsubscribe removes a subscription.
func (c *Client) MessagingUnsubscribe(ctx context.Context, req *MessagingUnsubscribeRequest) (*MessagingUnsubscribeResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingUnsubscribe",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingUnsubscribeRequest{
		SubscriptionId: req.SubscriptionID,
	}

	pbResp, err := messagingClient.Unsubscribe(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingUnsubscribeResponse{
		SubscriptionID: pbResp.SubscriptionId,
		Unsubscribed:   pbResp.Unsubscribed,
		Timestamp:      time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingPublish publishes a message to a channel.
func (c *Client) MessagingPublish(ctx context.Context, req *MessagingPublishRequest) (*MessagingPublishResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingPublish",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingPublishRequest{
		Channel:   req.Channel,
		Payload:   req.Payload,
		MessageId: req.MessageID,
		DedupKey:  req.DedupKey,
		Codec:     req.Codec,
		Metadata:  req.Metadata,
	}

	pbResp, err := messagingClient.Publish(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingPublishResponse{
		MessageID:   pbResp.MessageId,
		Channel:     pbResp.Channel,
		IsDuplicate: pbResp.IsDuplicate,
		Timestamp:   time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingPublishDelta publishes a delta message to a channel.
func (c *Client) MessagingPublishDelta(ctx context.Context, req *MessagingPublishDeltaRequest) (*MessagingPublishDeltaResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingPublishDelta",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingPublishDeltaRequest{
		Channel:   req.Channel,
		OldValue:  req.OldValue,
		NewValue:  req.NewValue,
		MessageId: req.MessageID,
		DedupKey:  req.DedupKey,
		Codec:     req.Codec,
		Metadata:  req.Metadata,
	}

	pbResp, err := messagingClient.PublishDelta(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingPublishDeltaResponse{
		MessageID:   pbResp.MessageId,
		Channel:     pbResp.Channel,
		Delta:       convertProtoDeltaPayload(pbResp.Delta),
		IsDuplicate: pbResp.IsDuplicate,
		Timestamp:   time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingStreamNotifications initiates a notification stream and returns a channel of notifications.
// The returned channel will be closed when the stream ends or an error occurs.
// Use the context to cancel the stream.
func (c *Client) MessagingStreamNotifications(ctx context.Context, req *MessagingStreamNotificationsRequest) (<-chan *MessagingNotification, <-chan error, error) {
	if req == nil {
		return nil, nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingStreamNotifications",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingStreamNotificationsRequest{
		SubscriptionId: req.SubscriptionID,
		StartSequence:  req.StartSequence,
		IncludeHistory: req.IncludeHistory,
		HistoryLimit:   req.HistoryLimit,
	}

	stream, err := messagingClient.StreamNotifications(ctx, pbReq)
	if err != nil {
		return nil, nil, convertError(err)
	}

	notificationChan := make(chan *MessagingNotification, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(notificationChan)
		defer close(errChan)

		for {
			pbNotification, err := stream.Recv()
			if err != nil {
				if stderrors.Is(err, io.EOF) || stderrors.Is(err, context.Canceled) {
					return
				}
				// Check if context was cancelled
				if ctx.Err() != nil {
					return
				}
				errChan <- convertError(err)
				return
			}

			notification := convertProtoNotification(pbNotification)

			select {
			case notificationChan <- notification:
			case <-ctx.Done():
				return
			}
		}
	}()

	return notificationChan, errChan, nil
}

// MessagingStreamNotificationsRaw returns the raw gRPC stream for advanced use cases.
func (c *Client) MessagingStreamNotificationsRaw(ctx context.Context, req *MessagingStreamNotificationsRequest) (grpc.ServerStreamingClient[pb.MessagingNotification], error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingStreamNotificationsRaw",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingStreamNotificationsRequest{
		SubscriptionId: req.SubscriptionID,
		StartSequence:  req.StartSequence,
		IncludeHistory: req.IncludeHistory,
		HistoryLimit:   req.HistoryLimit,
	}

	stream, err := messagingClient.StreamNotifications(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return stream, nil
}

// MessagingWatchChannel creates a subscription that watches for key changes.
func (c *Client) MessagingWatchChannel(ctx context.Context, req *MessagingWatchChannelRequest) (*MessagingWatchChannelResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingWatchChannel",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingWatchChannelRequest{
		KeyPrefix:  req.KeyPrefix,
		SessionId:  req.SessionID,
		Filter:     convertSubscriptionFilterToProto(req.Filter),
		BufferSize: req.BufferSize,
	}

	pbResp, err := messagingClient.WatchChannel(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingWatchChannelResponse{
		Subscription: convertProtoSubscription(pbResp.Subscription),
		ChannelName:  pbResp.ChannelName,
		Timestamp:    time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingGetStats retrieves messaging service statistics.
func (c *Client) MessagingGetStats(ctx context.Context) (*MessagingGetStatsResponse, error) {
	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbResp, err := messagingClient.GetStats(ctx, &pb.MessagingGetStatsRequest{})
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingGetStatsResponse{
		Stats:     convertProtoStats(pbResp.Stats),
		IsRunning: pbResp.IsRunning,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingGetDedupStats retrieves deduplication statistics for a channel.
func (c *Client) MessagingGetDedupStats(ctx context.Context, req *MessagingGetDedupStatsRequest) (*MessagingGetDedupStatsResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingGetDedupStats",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingGetDedupStatsRequest{
		Channel: req.Channel,
	}

	pbResp, err := messagingClient.GetDedupStats(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingGetDedupStatsResponse{
		Stats:     convertProtoDedupStats(pbResp.Stats),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// MessagingGetChannelStats retrieves statistics for a specific channel.
func (c *Client) MessagingGetChannelStats(ctx context.Context, req *MessagingGetChannelStatsRequest) (*MessagingGetChannelStatsResponse, error) {
	if req == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.MessagingGetChannelStats",
			Err:  ErrRequestNil,
		}
	}

	messagingClient := pb.NewMessagingServiceClient(c.conn)

	pbReq := &pb.MessagingGetChannelStatsRequest{
		Name: req.Name,
	}

	pbResp, err := messagingClient.GetChannelStats(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &MessagingGetChannelStatsResponse{
		Stats:     convertProtoChannelStats(pbResp.Stats),
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// Helper functions to convert between proto and client types.

// convertProtoChannel converts proto Channel to MessagingChannel.
func convertProtoChannel(pc *pb.Channel) *MessagingChannel {
	if pc == nil {
		return nil
	}

	return &MessagingChannel{
		Name:        pc.Name,
		Codec:       pc.Codec,
		CreatedAt:   time.Unix(pc.CreatedAt, 0),
		Subscribers: pc.Subscribers,
		Persistent:  pc.Persistent,
		MaxHistory:  pc.MaxHistory,
		BufferSize:  pc.BufferSize,
		TTLSeconds:  pc.TtlSeconds,
	}
}

// convertProtoSubscription converts proto Subscription to MessagingSubscription.
func convertProtoSubscription(ps *pb.Subscription) *MessagingSubscription {
	if ps == nil {
		return nil
	}

	var filter *MessagingSubscriptionFilter
	if ps.Filters != nil {
		eventTypes := make([]string, len(ps.Filters.EventTypes))
		for i, et := range ps.Filters.EventTypes {
			eventTypes[i] = convertProtoEventType(et)
		}
		filter = &MessagingSubscriptionFilter{
			EventTypes: eventTypes,
			KeyPrefix:  ps.Filters.KeyPrefix,
		}
	}

	return &MessagingSubscription{
		ID:        ps.Id,
		Channel:   ps.Channel,
		ClientID:  ps.ClientId,
		CreatedAt: time.Unix(ps.CreatedAt, 0),
		Filter:    filter,
	}
}

// convertSubscriptionFilterToProto converts MessagingSubscriptionFilter to proto.
func convertSubscriptionFilterToProto(filter *MessagingSubscriptionFilter) *pb.SubscriptionFilter {
	if filter == nil {
		return nil
	}

	eventTypes := make([]pb.EventType, len(filter.EventTypes))
	for i, et := range filter.EventTypes {
		eventTypes[i] = convertEventTypeToProto(et)
	}

	return &pb.SubscriptionFilter{
		EventTypes: eventTypes,
		KeyPrefix:  filter.KeyPrefix,
	}
}

// convertProtoEventType converts proto EventType to string.
func convertProtoEventType(et pb.EventType) string {
	switch et {
	case pb.EventType_EVENT_TYPE_PUT:
		return "put"
	case pb.EventType_EVENT_TYPE_DELETE:
		return "delete"
	default:
		return "unspecified"
	}
}

// convertEventTypeToProto converts string to proto EventType.
func convertEventTypeToProto(et string) pb.EventType {
	switch et {
	case "put":
		return pb.EventType_EVENT_TYPE_PUT
	case "delete":
		return pb.EventType_EVENT_TYPE_DELETE
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

// convertProtoMessage converts proto Message to MessagingMessage.
func convertProtoMessage(pm *pb.Message) *MessagingMessage {
	if pm == nil {
		return nil
	}

	var dedup *MessagingDedupInfo
	if pm.Dedup != nil {
		dedup = &MessagingDedupInfo{
			IsDuplicate: pm.Dedup.IsDuplicate,
			Count:       pm.Dedup.Count,
			FirstSeen:   time.Unix(pm.Dedup.FirstSeen, 0),
		}
	}

	return &MessagingMessage{
		ID:        pm.Id,
		Type:      convertProtoMessageType(pm.Type),
		Channel:   pm.Channel,
		Payload:   pm.Payload,
		Timestamp: time.Unix(pm.Timestamp, 0),
		Source:    pm.Source,
		Metadata:  pm.Metadata,
		Dedup:     dedup,
	}
}

// convertProtoMessageType converts proto MessageType to string.
func convertProtoMessageType(mt pb.MessageType) string {
	switch mt {
	case pb.MessageType_MESSAGE_TYPE_SUBSCRIBE:
		return "subscribe"
	case pb.MessageType_MESSAGE_TYPE_UNSUBSCRIBE:
		return "unsubscribe"
	case pb.MessageType_MESSAGE_TYPE_PUBLISH:
		return "publish"
	case pb.MessageType_MESSAGE_TYPE_NOTIFICATION:
		return "notification"
	case pb.MessageType_MESSAGE_TYPE_PING:
		return "ping"
	case pb.MessageType_MESSAGE_TYPE_PONG:
		return "pong"
	case pb.MessageType_MESSAGE_TYPE_ACK:
		return "ack"
	case pb.MessageType_MESSAGE_TYPE_ERROR:
		return "error"
	case pb.MessageType_MESSAGE_TYPE_DATA:
		return "data"
	case pb.MessageType_MESSAGE_TYPE_CONTROL:
		return "control"
	case pb.MessageType_MESSAGE_TYPE_HEARTBEAT:
		return "heartbeat"
	default:
		return "unspecified"
	}
}

// convertProtoNotification converts proto MessagingNotification to MessagingNotification.
func convertProtoNotification(pn *pb.MessagingNotification) *MessagingNotification {
	if pn == nil {
		return nil
	}

	return &MessagingNotification{
		Message:        convertProtoMessage(pn.Message),
		SubscriptionID: pn.SubscriptionId,
		SequenceNumber: pn.SequenceNumber,
	}
}

// convertProtoDeltaPayload converts proto DeltaPayload to MessagingDeltaPayload.
func convertProtoDeltaPayload(pd *pb.DeltaPayload) *MessagingDeltaPayload {
	if pd == nil {
		return nil
	}

	changes := make([]*MessagingFieldChange, len(pd.Changes))
	for i, ch := range pd.Changes {
		changes[i] = &MessagingFieldChange{
			Path:      ch.Path,
			Operation: convertProtoOpType(ch.Operation),
			Value:     ch.Value,
			OldValue:  ch.OldValue,
		}
	}

	return &MessagingDeltaPayload{
		EntityID:    pd.EntityId,
		EntityType:  pd.EntityType,
		Version:     pd.Version,
		PrevVersion: pd.PrevVersion,
		Timestamp:   time.Unix(pd.Timestamp, 0),
		Changes:     changes,
	}
}

// convertProtoOpType converts proto OpType to string.
func convertProtoOpType(ot pb.OpType) string {
	switch ot {
	case pb.OpType_OP_TYPE_SET:
		return "set"
	case pb.OpType_OP_TYPE_DELETE:
		return "delete"
	case pb.OpType_OP_TYPE_APPEND:
		return "append"
	case pb.OpType_OP_TYPE_INCREMENT:
		return "increment"
	default:
		return "unspecified"
	}
}

// convertProtoStats converts proto Stats to MessagingStats.
func convertProtoStats(ps *pb.Stats) *MessagingStats {
	if ps == nil {
		return nil
	}

	return &MessagingStats{
		TotalChannels:      ps.TotalChannels,
		TotalSubscriptions: ps.TotalSubscriptions,
		MessagesPublished:  ps.MessagesPublished,
		MessagesDelivered:  ps.MessagesDelivered,
		DedupHits:          ps.DedupHits,
		DroppedMessages:    ps.DroppedMessages,
		BufferUtilization:  ps.BufferUtilization,
	}
}

// convertProtoChannelStats converts proto ChannelStats to MessagingChannelStats.
func convertProtoChannelStats(pcs *pb.ChannelStats) *MessagingChannelStats {
	if pcs == nil {
		return nil
	}

	return &MessagingChannelStats{
		Name:              pcs.Name,
		Subscribers:       pcs.Subscribers,
		MessagesPublished: pcs.MessagesPublished,
		MessagesInHistory: pcs.MessagesInHistory,
	}
}

// convertProtoDedupStats converts proto DedupStats to MessagingDedupStats.
func convertProtoDedupStats(pds *pb.DedupStats) *MessagingDedupStats {
	if pds == nil {
		return nil
	}

	topDuplicates := make([]*MessagingDedupEntry, len(pds.TopDuplicates))
	for i, de := range pds.TopDuplicates {
		topDuplicates[i] = &MessagingDedupEntry{
			Count:       de.Count,
			FirstSeen:   time.Unix(de.FirstSeen, 0),
			LastSeen:    time.Unix(de.LastSeen, 0),
			MessageID:   de.MessageId,
			PayloadHash: de.PayloadHash,
		}
	}

	return &MessagingDedupStats{
		Channel:         pds.Channel,
		TotalMessages:   pds.TotalMessages,
		TotalDuplicates: pds.TotalDuplicates,
		TopDuplicates:   topDuplicates,
	}
}
