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

package embedded

import (
	"context"
	stderrors "errors"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/messagingwatch"
	pkgmessaging "github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	messagingSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const messagingServiceName = "quicbaas.Messaging"

// messagingErrorCodes maps the messaging service's error families onto SDK
// error codes, in the order the gRPC server checks them.
var messagingErrorCodes = []struct {
	is   func(error) bool
	code errors.ErrorCode
}{
	{pkgmessaging.IsChannelNotFound, errors.CodeNotFound},
	{pkgmessaging.IsSubscriptionNotFound, errors.CodeNotFound},
	{pkgmessaging.IsChannelExists, errors.CodeAlreadyExists},
	{pkgmessaging.IsInvalidChannelName, errors.CodeInvalidInput},
	{pkgmessaging.IsInvalidCodec, errors.CodeInvalidInput},
	{pkgmessaging.IsInvalidMessageID, errors.CodeInvalidInput},
	{pkgmessaging.IsServiceNotRunning, errors.CodeUnavailable},
	{pkgmessaging.IsMaxSubscriptionsReached, errors.CodeResourceExhausted},
	{pkgmessaging.IsHubBufferFull, errors.CodeResourceExhausted},
	{pkgmessaging.IsClientSlow, errors.CodeResourceExhausted},
	{isRequiredField, errors.CodeInvalidInput},
}

// isRequiredField reports whether err is the shared watch mapping's refusal
// of a request that leaves a required field empty.
func isRequiredField(err error) bool {
	var required *api.RequiredFieldError
	return stderrors.As(err, &required)
}

// messagingError wraps a messaging service error in an SDK error whose code
// matches what the gRPC transport would report for the same failure.
func messagingError(method string, err error) error {
	code := errors.CodeInternal
	for _, class := range messagingErrorCodes {
		if class.is(err) {
			code = class.code
			break
		}
	}
	return errors.Wrap(err, code, "embedded."+messagingServiceName+"."+method, "messaging request failed")
}

// messagingService returns the server's messaging service, or an error when
// the server runs without one.
func messagingService(svc service.QuicBaaS) (pkgmessaging.Service, error) {
	msgSvc := svc.Messaging()
	if msgSvc == nil {
		return nil, NewServiceNotConfiguredError(messagingServiceName)
	}
	return msgSvc, nil
}

func handleMessagingCreateChannel(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingSDK.CreateChannelRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "CreateChannel",
			"*messaging.CreateChannelRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	var opts []pkgmessaging.CreateChannelOption
	if r.Codec != "" {
		opts = append(opts, pkgmessaging.WithCodec(r.Codec))
	}
	if r.Persistent {
		opts = append(opts, pkgmessaging.WithPersistence(r.Persistent))
	}
	if r.MaxHistory > 0 {
		opts = append(opts, pkgmessaging.WithMaxHistory(r.MaxHistory))
	}
	if r.BufferSize > 0 {
		opts = append(opts, pkgmessaging.WithChannelBufferSize(r.BufferSize))
	}
	if r.MaxSubs > 0 {
		opts = append(opts, pkgmessaging.WithChannelMaxSubscribers(r.MaxSubs))
	}
	if r.TTL > 0 {
		opts = append(opts, pkgmessaging.WithChannelTTL(r.TTL))
	}
	channel, err := msgSvc.CreateChannel(ctx, r.Name, opts...)
	if err != nil {
		return nil, messagingError("CreateChannel", err)
	}
	return messagingChannelToSDK(channel), nil
}

func handleMessagingDeleteChannel(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingSDK.DeleteChannelRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "DeleteChannel",
			"*messaging.DeleteChannelRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	if err := msgSvc.DeleteChannel(ctx, r.Name); err != nil {
		return nil, messagingError("DeleteChannel", err)
	}
	return nil, nil
}

func handleMessagingGetChannel(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingSDK.GetChannelRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "GetChannel",
			"*messaging.GetChannelRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	channel, err := msgSvc.GetChannel(ctx, r.Name)
	if err != nil {
		return nil, messagingError("GetChannel", err)
	}
	return messagingChannelToSDK(channel), nil
}

func handleMessagingListChannels(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	if _, ok := req.(*messagingSDK.ListChannelsRequest); !ok {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "ListChannels",
			"*messaging.ListChannelsRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	channels, err := msgSvc.ListChannels(ctx)
	if err != nil {
		return nil, messagingError("ListChannels", err)
	}
	resp := &messagingSDK.ListChannelsResponse{Channels: make([]*messagingSDK.Channel, 0, len(channels))}
	for _, channel := range channels {
		resp.Channels = append(resp.Channels, messagingChannelToSDK(channel))
	}
	return resp, nil
}

func handleMessagingSubscribe(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingSDK.SubscribeRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "Subscribe",
			"*messaging.SubscribeRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	opts := messagingWatchRequest("", r.SessionID, r.Filter, r.BufferSize).Options()
	if r.DeltaMode {
		opts = append(opts, pkgmessaging.WithDeltaMode(r.DeltaMode))
	}
	if r.IncludeOldValues {
		opts = append(opts, pkgmessaging.WithIncludeOldValues(r.IncludeOldValues))
	}
	if r.DedupMetadata {
		opts = append(opts, pkgmessaging.WithDedupMetadata(r.DedupMetadata))
	}
	if r.DedupSkipDuplicates {
		opts = append(opts, pkgmessaging.WithDedupSkipDuplicates(r.DedupSkipDuplicates))
	}
	sub, err := msgSvc.Subscribe(ctx, r.Channel, opts...)
	if err != nil {
		return nil, messagingError("Subscribe", err)
	}
	return messagingSubscriptionToSDK(sub), nil
}

func handleMessagingUnsubscribe(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingSDK.UnsubscribeRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "Unsubscribe",
			"*messaging.UnsubscribeRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	if err := msgSvc.Unsubscribe(ctx, r.SubscriptionID); err != nil {
		return nil, messagingError("Unsubscribe", err)
	}
	return nil, nil
}

func handleMessagingPublish(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingSDK.PublishRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "Publish",
			"*messaging.PublishRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	opts := messagingPublishOptions(r.MessageID, r.DedupKey, r.Codec, r.Metadata)
	if _, err := msgSvc.Publish(ctx, r.Channel, r.Payload, opts...); err != nil {
		return nil, messagingError("Publish", err)
	}
	return nil, nil
}

func handleMessagingPublishDelta(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingSDK.PublishDeltaRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "PublishDelta",
			"*messaging.PublishDeltaRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	opts := messagingPublishOptions(r.MessageID, r.DedupKey, r.Codec, r.Metadata)
	if _, err := msgSvc.PublishDelta(ctx, r.Channel, r.Old, r.New, opts...); err != nil {
		return nil, messagingError("PublishDelta", err)
	}
	return nil, nil
}

// handleMessagingWatchChannel forwards the same fields the gRPC server
// honours for a watch: session, filter and buffer size.
func handleMessagingWatchChannel(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*messagingSDK.WatchChannelRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(messagingServiceName, "WatchChannel",
			"*messaging.WatchChannelRequest", reflect.TypeOf(req).String())
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	sub, err := messagingwatch.Watch(ctx, msgSvc, messagingWatchRequest(r.KeyPrefix, r.SessionID, r.Filter, r.BufferSize))
	if err != nil {
		return nil, messagingError("WatchChannel", err)
	}
	return messagingSubscriptionToSDK(sub), nil
}

func handleMessagingGetStats(_ context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	stats := msgSvc.GetStats()
	if stats == nil {
		return nil, errors.NewInternal("embedded."+messagingServiceName+".GetStats", "messaging service returned no stats")
	}
	return &messagingSDK.Stats{
		TotalChannels:      stats.TotalChannels,
		TotalSubscriptions: stats.TotalSubscriptions,
		MessagesPublished:  stats.MessagesPublished,
		MessagesDelivered:  stats.MessagesDelivered,
		DedupHits:          stats.DedupHits,
		DroppedMessages:    stats.DroppedMessages,
		BufferUtilization:  stats.BufferUtilization,
	}, nil
}

// handleMessagingGetDedupStats serves a request that names its channel in a
// Channel field, as the gRPC server requires. A push service without a dedup
// store reports zero counts, also as the gRPC server does.
func handleMessagingGetDedupStats(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	const op = "embedded." + messagingServiceName + ".GetDedupStats"
	channel, ok := kmsStringField(req, "Channel")
	if !ok || channel == "" {
		return nil, errors.NewInvalidInput(op, "channel is required")
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	if _, err := msgSvc.GetChannel(ctx, channel); err != nil {
		return nil, messagingError("GetDedupStats", err)
	}
	pushSvc, ok := msgSvc.(*pkgmessaging.PushService)
	if !ok {
		return nil, errors.NewUnimplemented(op, "messaging service does not support dedup stats")
	}
	dedupStore := pushSvc.GetDedupStore()
	if dedupStore == nil {
		return &messagingSDK.DedupStats{}, nil
	}
	stats, err := dedupStore.GetStats(ctx, channel)
	if err != nil {
		return nil, messagingError("GetDedupStats", err)
	}
	return &messagingSDK.DedupStats{
		TotalChecks:        stats.TotalMessages + stats.TotalDuplicates,
		DuplicatesDetected: stats.TotalDuplicates,
		UniqueMessages:     stats.TotalMessages,
	}, nil
}

func handleMessagingGetChannelStats(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	channelName, ok := kmsStringField(req, "Channel")
	if !ok || channelName == "" {
		return nil, errors.NewInvalidInput("embedded."+messagingServiceName+".GetChannelStats", "channel is required")
	}
	msgSvc, err := messagingService(svc)
	if err != nil {
		return nil, err
	}
	channel, err := msgSvc.GetChannel(ctx, channelName)
	if err != nil {
		return nil, messagingError("GetChannelStats", err)
	}
	return &messagingSDK.ChannelStats{
		Name:              channel.Name,
		Subscribers:       channel.Subscribers,
		MessagesPublished: channel.MessagesPublished,
	}, nil
}

// messagingWatchRequest maps the SDK fields a subscription and a watch share
// onto the request the gRPC, REST and MCP servers map theirs onto, so the
// embedded transport builds the same subscribe options they do.
func messagingWatchRequest(keyPrefix, sessionID string, filter *messagingSDK.SubscriptionFilter, bufferSize int) messagingwatch.Request {
	req := messagingwatch.Request{KeyPrefix: keyPrefix, SessionID: sessionID, BufferSize: bufferSize}
	if filter != nil {
		req.Filter = messagingFilterFromSDK(filter)
	}
	return req
}

func messagingPublishOptions(messageID, dedupKey, codec string, metadata map[string]string) []pkgmessaging.PublishOption {
	var opts []pkgmessaging.PublishOption
	if messageID != "" {
		opts = append(opts, pkgmessaging.WithMessageID(messageID))
	}
	if dedupKey != "" {
		opts = append(opts, pkgmessaging.WithDedupKey(dedupKey))
	}
	if codec != "" {
		opts = append(opts, pkgmessaging.WithPublishCodec(codec))
	}
	if len(metadata) > 0 {
		opts = append(opts, pkgmessaging.WithMetadata(metadata))
	}
	return opts
}

func messagingChannelToSDK(channel *pkgmessaging.Channel) *messagingSDK.Channel {
	return &messagingSDK.Channel{
		Name:        channel.Name,
		Codec:       channel.Codec,
		CreatedAt:   channel.CreatedAt,
		Subscribers: channel.Subscribers,
		Persistent:  channel.Persistent,
		MaxHistory:  channel.MaxHistory,
		BufferSize:  channel.BufferSize,
		TTL:         channel.TTL,
	}
}

func messagingSubscriptionToSDK(sub *pkgmessaging.Subscription) *messagingSDK.Subscription {
	resp := &messagingSDK.Subscription{
		ID:        sub.ID,
		Channel:   sub.Channel,
		ClientID:  sub.ClientID,
		CreatedAt: sub.CreatedAt,
	}
	if sub.Filters != nil {
		eventTypes := make([]messagingSDK.EventType, 0, len(sub.Filters.EventTypes))
		for _, et := range sub.Filters.EventTypes {
			eventTypes = append(eventTypes, messagingSDK.EventType(et))
		}
		resp.Filters = &messagingSDK.SubscriptionFilter{EventTypes: eventTypes, KeyPrefix: sub.Filters.KeyPrefix}
	}
	return resp
}

// messagingFilterFromSDK converts an SDK filter; the two EventType enums
// share their values.
func messagingFilterFromSDK(filter *messagingSDK.SubscriptionFilter) *pkgmessaging.SubscriptionFilter {
	eventTypes := make([]pkgmessaging.EventType, 0, len(filter.EventTypes))
	for _, et := range filter.EventTypes {
		eventTypes = append(eventTypes, pkgmessaging.EventType(et))
	}
	return &pkgmessaging.SubscriptionFilter{EventTypes: eventTypes, KeyPrefix: filter.KeyPrefix}
}
