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
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// messagingServicePrefix is the service name prefix for messaging service routing.
const messagingServicePrefix = "quicbaas.Messaging"

// isMessagingService checks if the service name is a Messaging service.
func isMessagingService(svc string) bool {
	return strings.HasPrefix(svc, messagingServicePrefix)
}

// messagingMethodFunc converts one SDK messaging request onto its
// MessagingService RPC and converts the reply back to SDK types.
type messagingMethodFunc func(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error)

// messagingMethods maps every method the messaging SDK client sends to its
// MessagingService stub for O(1) dispatch.
var messagingMethods = map[string]messagingMethodFunc{
	"CreateChannel":   messagingCreateChannel,
	"DeleteChannel":   messagingDeleteChannel,
	"GetChannel":      messagingGetChannel,
	"ListChannels":    messagingListChannels,
	"Subscribe":       messagingSubscribe,
	"Unsubscribe":     messagingUnsubscribe,
	"Publish":         messagingPublish,
	"PublishDelta":    messagingPublishDelta,
	"WatchChannel":    messagingWatchChannel,
	"GetStats":        messagingGetStats,
	"GetDedupStats":   messagingGetDedupStats,
	"GetChannelStats": messagingGetChannelStats,
}

// messagingHandler handles Messaging service requests by converting between
// SDK types and protobuf types.
type messagingHandler struct {
	conn *grpc.ClientConn
}

// newMessagingHandler creates a new messaging handler.
func newMessagingHandler(conn *grpc.ClientConn) *messagingHandler {
	return &messagingHandler{conn: conn}
}

// handleRequest routes a Messaging request to the RPC registered for method.
func (h *messagingHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	call, ok := messagingMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("messagingHandler.handleRequest",
			"unsupported method: "+method)
	}
	return call(ctx, pb.NewMessagingServiceClient(h.conn), req)
}

func messagingCreateChannel(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.CreateChannel"
	r, ok := req.(*messaging.CreateChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	maxHistory, err := int32ToProto(op, "max_history", r.MaxHistory)
	if err != nil {
		return nil, err
	}
	bufferSize, err := int32ToProto(op, "buffer_size", r.BufferSize)
	if err != nil {
		return nil, err
	}
	maxSubscribers, err := int32ToProto(op, "max_subscribers", r.MaxSubs)
	if err != nil {
		return nil, err
	}
	resp, err := client.CreateChannel(ctx, &pb.MessagingCreateChannelRequest{
		Name:           r.Name,
		Codec:          r.Codec,
		Persistent:     r.Persistent,
		MaxHistory:     maxHistory,
		BufferSize:     bufferSize,
		MaxSubscribers: maxSubscribers,
		TtlSeconds:     int64(r.TTL / time.Second),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return messagingChannelFromProto(op, resp.GetChannel())
}

func messagingDeleteChannel(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.DeleteChannel"
	r, ok := req.(*messaging.DeleteChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	if _, err := client.DeleteChannel(ctx, &pb.MessagingDeleteChannelRequest{Name: r.Name}); err != nil {
		return nil, convertGRPCError(err, op)
	}
	return nil, nil
}

func messagingGetChannel(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.GetChannel"
	r, ok := req.(*messaging.GetChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := client.GetChannel(ctx, &pb.MessagingGetChannelRequest{Name: r.Name})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return messagingChannelFromProto(op, resp.GetChannel())
}

func messagingListChannels(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.ListChannels"
	if _, ok := req.(*messaging.ListChannelsRequest); !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := client.ListChannels(ctx, &pb.MessagingListChannelsRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	channels := make([]*messaging.Channel, 0, len(resp.GetChannels()))
	for _, ch := range resp.GetChannels() {
		channel, err := messagingChannelFromProto(op, ch)
		if err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	return &messaging.ListChannelsResponse{Channels: channels}, nil
}

func messagingSubscribe(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.Subscribe"
	r, ok := req.(*messaging.SubscribeRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	bufferSize, err := int32ToProto(op, "buffer_size", r.BufferSize)
	if err != nil {
		return nil, err
	}
	resp, err := client.Subscribe(ctx, &pb.MessagingSubscribeRequest{
		Channel:             r.Channel,
		SessionId:           r.SessionID,
		Filter:              messagingFilterToProto(r.Filter),
		DeltaMode:           r.DeltaMode,
		IncludeOldValues:    r.IncludeOldValues,
		DedupMetadata:       r.DedupMetadata,
		DedupSkipDuplicates: r.DedupSkipDuplicates,
		BufferSize:          bufferSize,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return messagingSubscriptionFromProto(op, resp.GetSubscription())
}

func messagingUnsubscribe(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.Unsubscribe"
	r, ok := req.(*messaging.UnsubscribeRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	if _, err := client.Unsubscribe(ctx, &pb.MessagingUnsubscribeRequest{SubscriptionId: r.SubscriptionID}); err != nil {
		return nil, convertGRPCError(err, op)
	}
	return nil, nil
}

func messagingPublish(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.Publish"
	r, ok := req.(*messaging.PublishRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	_, err := client.Publish(ctx, &pb.MessagingPublishRequest{
		Channel:   r.Channel,
		Payload:   r.Payload,
		MessageId: r.MessageID,
		DedupKey:  r.DedupKey,
		Codec:     r.Codec,
		Metadata:  r.Metadata,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return nil, nil
}

// messagingPublishDelta JSON-encodes the old and new values, the encoding the
// MessagingService PublishDelta RPC and the REST transport both use.
func messagingPublishDelta(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.PublishDelta"
	r, ok := req.(*messaging.PublishDeltaRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	oldValue, err := json.Marshal(r.Old)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, op, "failed to encode old value")
	}
	newValue, err := json.Marshal(r.New)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, op, "failed to encode new value")
	}
	_, err = client.PublishDelta(ctx, &pb.MessagingPublishDeltaRequest{
		Channel:   r.Channel,
		OldValue:  oldValue,
		NewValue:  newValue,
		MessageId: r.MessageID,
		DedupKey:  r.DedupKey,
		Codec:     r.Codec,
		Metadata:  r.Metadata,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return nil, nil
}

// messagingWatchChannel forwards the fields the WatchChannel RPC carries:
// key prefix, session, filter and buffer size.
func messagingWatchChannel(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.WatchChannel"
	r, ok := req.(*messaging.WatchChannelRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	bufferSize, err := int32ToProto(op, "buffer_size", r.BufferSize)
	if err != nil {
		return nil, err
	}
	resp, err := client.WatchChannel(ctx, &pb.MessagingWatchChannelRequest{
		KeyPrefix:  r.KeyPrefix,
		SessionId:  r.SessionID,
		Filter:     messagingFilterToProto(r.Filter),
		BufferSize: bufferSize,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return messagingSubscriptionFromProto(op, resp.GetSubscription())
}

func messagingGetStats(ctx context.Context, client pb.MessagingServiceClient, _ interface{}) (interface{}, error) {
	const op = "messagingHandler.GetStats"
	resp, err := client.GetStats(ctx, &pb.MessagingGetStatsRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	stats := resp.GetStats()
	if stats == nil {
		return nil, errors.NewInternal(op, "server returned no stats")
	}
	return &messaging.Stats{
		TotalChannels:      stats.GetTotalChannels(),
		TotalSubscriptions: stats.GetTotalSubscriptions(),
		MessagesPublished:  stats.GetMessagesPublished(),
		MessagesDelivered:  stats.GetMessagesDelivered(),
		DedupHits:          stats.GetDedupHits(),
		DroppedMessages:    stats.GetDroppedMessages(),
		BufferUtilization:  stats.GetBufferUtilization(),
	}, nil
}

// messagingGetDedupStats serves the per-channel GetDedupStats RPC. The
// request must name a channel, as it must on the REST transport.
func messagingGetDedupStats(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.GetDedupStats"
	channel, ok := stringFieldByName(req, "Channel")
	if !ok || channel == "" {
		return nil, errors.NewInvalidInput(op, "channel is required")
	}
	resp, err := client.GetDedupStats(ctx, &pb.MessagingGetDedupStatsRequest{Channel: channel})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	stats := resp.GetStats()
	if stats == nil {
		return nil, errors.NewInternal(op, "server returned no dedup stats")
	}
	return &messaging.DedupStats{
		TotalChecks:        stats.GetTotalMessages() + stats.GetTotalDuplicates(),
		DuplicatesDetected: stats.GetTotalDuplicates(),
		UniqueMessages:     stats.GetTotalMessages(),
	}, nil
}

func messagingGetChannelStats(ctx context.Context, client pb.MessagingServiceClient, req interface{}) (interface{}, error) {
	const op = "messagingHandler.GetChannelStats"
	channel, ok := stringFieldByName(req, "Channel")
	if !ok || channel == "" {
		return nil, errors.NewInvalidInput(op, "channel is required")
	}
	resp, err := client.GetChannelStats(ctx, &pb.MessagingGetChannelStatsRequest{Name: channel})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	stats := resp.GetStats()
	if stats == nil {
		return nil, errors.NewInternal(op, "server returned no channel stats")
	}
	return &messaging.ChannelStats{
		Name:              stats.GetName(),
		Subscribers:       stats.GetSubscribers(),
		MessagesPublished: stats.GetMessagesPublished(),
		MessagesInHistory: int(stats.GetMessagesInHistory()),
	}, nil
}

// messagingChannelFromProto converts a proto Channel; a reply without one is
// a server fault.
func messagingChannelFromProto(op string, ch *pb.Channel) (*messaging.Channel, error) {
	if ch == nil {
		return nil, errors.NewInternal(op, "server returned no channel")
	}
	return &messaging.Channel{
		Name:        ch.GetName(),
		Codec:       ch.GetCodec(),
		CreatedAt:   messagingUnixTime(ch.GetCreatedAt()),
		Subscribers: ch.GetSubscribers(),
		Persistent:  ch.GetPersistent(),
		MaxHistory:  int(ch.GetMaxHistory()),
		BufferSize:  int(ch.GetBufferSize()),
		TTL:         time.Duration(ch.GetTtlSeconds()) * time.Second,
	}, nil
}

// messagingSubscriptionFromProto converts a proto Subscription; a reply
// without one is a server fault.
func messagingSubscriptionFromProto(op string, sub *pb.Subscription) (*messaging.Subscription, error) {
	if sub == nil {
		return nil, errors.NewInternal(op, "server returned no subscription")
	}
	return &messaging.Subscription{
		ID:        sub.GetId(),
		Channel:   sub.GetChannel(),
		ClientID:  sub.GetClientId(),
		CreatedAt: messagingUnixTime(sub.GetCreatedAt()),
		Filters:   messagingFilterFromProto(sub.GetFilters()),
	}, nil
}

func messagingFilterToProto(filter *messaging.SubscriptionFilter) *pb.SubscriptionFilter {
	if filter == nil {
		return nil
	}
	eventTypes := make([]pb.EventType, 0, len(filter.EventTypes))
	for _, et := range filter.EventTypes {
		eventTypes = append(eventTypes, messagingEventTypeToProto[et])
	}
	return &pb.SubscriptionFilter{EventTypes: eventTypes, KeyPrefix: filter.KeyPrefix}
}

func messagingFilterFromProto(filter *pb.SubscriptionFilter) *messaging.SubscriptionFilter {
	if filter == nil {
		return nil
	}
	eventTypes := make([]messaging.EventType, 0, len(filter.GetEventTypes()))
	for _, et := range filter.GetEventTypes() {
		if sdkType, ok := messagingEventTypeFromProto[et]; ok {
			eventTypes = append(eventTypes, sdkType)
		}
	}
	return &messaging.SubscriptionFilter{EventTypes: eventTypes, KeyPrefix: filter.GetKeyPrefix()}
}

// messagingEventTypeToProto maps SDK event types to their wire enum; an
// unknown SDK value maps to EVENT_TYPE_UNSPECIFIED.
var messagingEventTypeToProto = map[messaging.EventType]pb.EventType{
	messaging.EventTypePut:    pb.EventType_EVENT_TYPE_PUT,
	messaging.EventTypeDelete: pb.EventType_EVENT_TYPE_DELETE,
}

// messagingEventTypeFromProto maps wire event types back to the SDK enum.
var messagingEventTypeFromProto = map[pb.EventType]messaging.EventType{
	pb.EventType_EVENT_TYPE_PUT:    messaging.EventTypePut,
	pb.EventType_EVENT_TYPE_DELETE: messaging.EventTypeDelete,
}

// messagingUnixTime converts the wire's Unix seconds; zero means unset.
func messagingUnixTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
