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
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// handleCreateChannel handles the CreateChannel operation.
func (h *Handler) handleCreateChannel(ctx context.Context, reqData []byte) ([]byte, error) {
	var req CreateChannelRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	// Build options from request
	opts := make([]messaging.CreateChannelOption, 0, 6)
	if req.Codec != "" {
		opts = append(opts, messaging.WithCodec(req.Codec))
	}
	if req.BufferSize > 0 {
		opts = append(opts, messaging.WithChannelBufferSize(req.BufferSize))
	}
	if req.MaxSubs > 0 {
		opts = append(opts, messaging.WithChannelMaxSubscribers(req.MaxSubs))
	}
	if req.Persistent {
		opts = append(opts, messaging.WithPersistence(req.Persistent))
	}
	if req.MaxHistory > 0 {
		opts = append(opts, messaging.WithMaxHistory(req.MaxHistory))
	}
	if req.TTL > 0 {
		opts = append(opts, messaging.WithChannelTTL(req.TTL))
	}

	channel, err := h.service.CreateChannel(ctx, req.Name, opts...)
	if err != nil {
		return nil, err
	}

	resp := &CreateChannelResponse{
		Channel: channelToInfo(channel),
	}

	return EncodePayload(resp)
}

// handleDeleteChannel handles the DeleteChannel operation.
func (h *Handler) handleDeleteChannel(ctx context.Context, reqData []byte) ([]byte, error) {
	var req DeleteChannelRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	if err := h.service.DeleteChannel(ctx, req.Name); err != nil {
		return nil, err
	}

	resp := &DeleteChannelResponse{
		Success: true,
	}

	return EncodePayload(resp)
}

// handleGetChannel handles the GetChannel operation.
func (h *Handler) handleGetChannel(ctx context.Context, reqData []byte) ([]byte, error) {
	var req GetChannelRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	channel, err := h.service.GetChannel(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	resp := &GetChannelResponse{
		Channel: channelToInfo(channel),
	}

	return EncodePayload(resp)
}

// handleListChannels handles the ListChannels operation.
func (h *Handler) handleListChannels(ctx context.Context, _ []byte) ([]byte, error) {
	channels, err := h.service.ListChannels(ctx)
	if err != nil {
		return nil, err
	}

	channelInfos := make([]*ChannelInfo, 0, len(channels))
	for _, ch := range channels {
		channelInfos = append(channelInfos, channelToInfo(ch))
	}

	resp := &ListChannelsResponse{
		Channels: channelInfos,
	}

	return EncodePayload(resp)
}
