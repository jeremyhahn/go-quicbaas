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
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// cliAdapter implements MessagingCommands using CLI commands.
type cliAdapter struct {
	executor *helpers.CLIExecutor
}

// newCLIAdapter creates a new CLI adapter for the given transport.
func newCLIAdapter(transport helpers.Transport) *cliAdapter {
	return &cliAdapter{}
}

// Name returns the protocol name.
func (a *cliAdapter) Name() string {
	if a.executor != nil {
		return fmt.Sprintf("CLI-%s", a.executor.Name())
	}
	return "CLI"
}

// Setup initializes the CLI executor.
func (a *cliAdapter) Setup(ctx context.Context) error {
	// CLI adapter is initialized with a specific transport
	return nil
}

// SetExecutor sets the CLI executor for this adapter.
func (a *cliAdapter) SetExecutor(executor *helpers.CLIExecutor) {
	a.executor = executor
}

// Teardown cleans up the CLI adapter.
func (a *cliAdapter) Teardown() error {
	return nil
}

// CreateChannel creates a notification channel via CLI.
func (a *cliAdapter) CreateChannel(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error) {
	args := []string{
		"msg", "create-channel", req.Name,
		"--output", "json",
	}

	if req.BufferSize > 0 {
		args = append(args, "--buffer-size", strconv.Itoa(int(req.BufferSize)))
	}
	if req.MaxSubscribers > 0 {
		args = append(args, "--max-subscribers", strconv.Itoa(int(req.MaxSubscribers)))
	}
	if req.Persistent {
		args = append(args, "--persistent")
	}
	if req.TTLSeconds > 0 {
		args = append(args, "--ttl", fmt.Sprintf("%ds", req.TTLSeconds))
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI create-channel failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI create-channel returned error: " + result.Stderr}
	}

	var response cliChannelResponse
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
	}

	return &grpc.MessagingCreateChannelResponse{
		Channel:   response.toChannel(),
		Timestamp: time.Now(),
	}, nil
}

// DeleteChannel deletes a notification channel via CLI.
func (a *cliAdapter) DeleteChannel(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
	args := []string{
		"msg", "delete-channel", req.Name,
		"--force",
		"--output", "json",
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI delete-channel failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI delete-channel returned error: " + result.Stderr}
	}

	var response struct {
		Name    string `json:"Name"`
		Deleted bool   `json:"Deleted"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
	}

	return &grpc.MessagingDeleteChannelResponse{
		Name:      response.Name,
		Deleted:   response.Deleted,
		Timestamp: time.Now(),
	}, nil
}

// GetChannel retrieves channel information via CLI.
func (a *cliAdapter) GetChannel(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error) {
	args := []string{
		"msg", "channel-info", req.Name,
		"--output", "json",
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI channel-info failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI channel-info returned error: " + result.Stderr}
	}

	var response struct {
		Channel *cliChannelResponse `json:"channel"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
	}

	if response.Channel == nil {
		return nil, &qberrors.MessageError{Message: "channel not found: " + req.Name}
	}

	return &grpc.MessagingGetChannelResponse{
		Channel:   response.Channel.toChannel(),
		Timestamp: time.Now(),
	}, nil
}

// ListChannels lists all channels via CLI.
func (a *cliAdapter) ListChannels(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error) {
	args := []string{
		"msg", "list-channels",
		"--output", "json",
	}

	if req.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(int(req.Limit)))
	}
	if req.NamePrefix != "" {
		args = append(args, "--filter", req.NamePrefix)
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI list-channels failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI list-channels returned error: " + result.Stderr}
	}

	var response struct {
		Channels   []*cliChannelResponse `json:"Channels"`
		NextCursor string                `json:"NextCursor"`
		HasMore    bool                  `json:"HasMore"`
		TotalCount int32                 `json:"TotalCount"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
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

// Subscribe creates a subscription via CLI.
func (a *cliAdapter) Subscribe(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
	// Note: The subscribe command in CLI is interactive (streams notifications).
	// For integration testing, we need a non-interactive subscribe that just
	// creates the subscription and returns. This may require a separate CLI
	// command or flag to support.
	//
	// For now, we use the gRPC client directly for subscription testing.
	return nil, &qberrors.MessageError{Message: "CLI subscribe is interactive; use gRPC adapter for subscription testing"}
}

// Unsubscribe removes a subscription via CLI.
func (a *cliAdapter) Unsubscribe(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
	args := []string{
		"msg", "unsubscribe", req.SubscriptionID,
		"--force",
		"--output", "json",
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI unsubscribe failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI unsubscribe returned error: " + result.Stderr}
	}

	var response struct {
		SubscriptionID string `json:"SubscriptionID"`
		Unsubscribed   bool   `json:"Unsubscribed"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
	}

	return &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: response.SubscriptionID,
		Unsubscribed:   response.Unsubscribed,
		Timestamp:      time.Now(),
	}, nil
}

// Publish publishes a message via CLI.
func (a *cliAdapter) Publish(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error) {
	args := []string{
		"msg", "publish", req.Channel,
		"--payload", string(req.Payload),
		"--output", "json",
	}

	if req.DedupKey != "" {
		args = append(args, "--dedup-id", req.DedupKey)
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI publish failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI publish returned error: " + result.Stderr}
	}

	var response struct {
		MessageID   string    `json:"MessageID"`
		Channel     string    `json:"Channel"`
		IsDuplicate bool      `json:"IsDuplicate"`
		Timestamp   time.Time `json:"Timestamp"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
	}

	return &grpc.MessagingPublishResponse{
		MessageID:   response.MessageID,
		Channel:     response.Channel,
		IsDuplicate: response.IsDuplicate,
		Timestamp:   response.Timestamp,
	}, nil
}

// PublishDelta publishes a delta message via CLI.
func (a *cliAdapter) PublishDelta(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error) {
	args := []string{
		"msg", "publish-delta", req.Channel,
		"--old", string(req.OldValue),
		"--new", string(req.NewValue),
		"--output", "json",
	}

	if req.DedupKey != "" {
		args = append(args, "--dedup-id", req.DedupKey)
	}
	if req.Metadata != nil {
		if entityID, ok := req.Metadata["entity_id"]; ok {
			args = append(args, "--entity-id", entityID)
		}
		if entityType, ok := req.Metadata["entity_type"]; ok {
			args = append(args, "--entity-type", entityType)
		}
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI publish-delta failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI publish-delta returned error: " + result.Stderr}
	}

	var response struct {
		MessageID   string                      `json:"MessageID"`
		Channel     string                      `json:"Channel"`
		Delta       *grpc.MessagingDeltaPayload `json:"Delta"`
		IsDuplicate bool                        `json:"IsDuplicate"`
		Timestamp   time.Time                   `json:"Timestamp"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
	}

	return &grpc.MessagingPublishDeltaResponse{
		MessageID:   response.MessageID,
		Channel:     response.Channel,
		Delta:       response.Delta,
		IsDuplicate: response.IsDuplicate,
		Timestamp:   response.Timestamp,
	}, nil
}

// GetChannelStats retrieves channel statistics via CLI.
func (a *cliAdapter) GetChannelStats(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
	args := []string{
		"msg", "channel-info", req.Name,
		"--output", "json",
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI channel-info failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI channel-info returned error: " + result.Stderr}
	}

	var response struct {
		Stats *struct {
			Name              string `json:"Name"`
			Subscribers       int64  `json:"Subscribers"`
			MessagesPublished int64  `json:"MessagesPublished"`
			MessagesInHistory int32  `json:"MessagesInHistory"`
		} `json:"stats"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
	}

	if response.Stats == nil {
		return nil, &qberrors.MessageError{Message: "stats not available for channel: " + req.Name}
	}

	return &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:              response.Stats.Name,
			Subscribers:       response.Stats.Subscribers,
			MessagesPublished: response.Stats.MessagesPublished,
			MessagesInHistory: response.Stats.MessagesInHistory,
		},
		Timestamp: time.Now(),
	}, nil
}

// GetDedupStats retrieves deduplication statistics via CLI.
func (a *cliAdapter) GetDedupStats(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error) {
	args := []string{
		"msg", "dedup-stats", req.Channel,
		"--output", "json",
	}

	result, err := a.executor.Execute(ctx, args...)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "CLI dedup-stats failed", Cause: err}
	}

	if result.ExitCode != 0 {
		return nil, &qberrors.MessageError{Message: "CLI dedup-stats returned error: " + result.Stderr}
	}

	var response struct {
		Stats *struct {
			Channel         string `json:"Channel"`
			TotalMessages   int64  `json:"TotalMessages"`
			TotalDuplicates int64  `json:"TotalDuplicates"`
		} `json:"Stats"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to parse CLI response", Cause: err}
	}

	if response.Stats == nil {
		return nil, &qberrors.MessageError{Message: "dedup stats not available for channel: " + req.Channel}
	}

	return &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         response.Stats.Channel,
			TotalMessages:   response.Stats.TotalMessages,
			TotalDuplicates: response.Stats.TotalDuplicates,
		},
		Timestamp: time.Now(),
	}, nil
}

// GetStats retrieves global messaging statistics via CLI.
func (a *cliAdapter) GetStats(ctx context.Context) (*grpc.MessagingGetStatsResponse, error) {
	// There is no dedicated stats command in the CLI for global messaging stats.
	// This would need to be added to the CLI or we return an error.
	return nil, &qberrors.MessageError{Message: "CLI GetStats not implemented; use gRPC adapter"}
}

// cliChannelResponse represents the CLI JSON response for a channel.
type cliChannelResponse struct {
	Name        string    `json:"Name"`
	Codec       string    `json:"Codec"`
	CreatedAt   time.Time `json:"CreatedAt"`
	Subscribers int64     `json:"Subscribers"`
	Persistent  bool      `json:"Persistent"`
	MaxHistory  int32     `json:"MaxHistory"`
	BufferSize  int32     `json:"BufferSize"`
	TTLSeconds  int64     `json:"TTLSeconds"`
}

// toChannel converts the CLI response to a MessagingChannel.
func (r *cliChannelResponse) toChannel() *grpc.MessagingChannel {
	return &grpc.MessagingChannel{
		Name:        r.Name,
		Codec:       r.Codec,
		CreatedAt:   r.CreatedAt,
		Subscribers: r.Subscribers,
		Persistent:  r.Persistent,
		MaxHistory:  r.MaxHistory,
		BufferSize:  r.BufferSize,
		TTLSeconds:  r.TTLSeconds,
	}
}
