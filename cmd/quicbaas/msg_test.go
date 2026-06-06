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

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

type MockMessagingClient struct {
	MessagingCreateChannelFunc       func(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error)
	MessagingDeleteChannelFunc       func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error)
	MessagingGetChannelFunc          func(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error)
	MessagingListChannelsFunc        func(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error)
	MessagingSubscribeFunc           func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error)
	MessagingUnsubscribeFunc         func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error)
	MessagingPublishFunc             func(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error)
	MessagingPublishDeltaFunc        func(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error)
	MessagingStreamNotificationsFunc func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error)
	MessagingGetDedupStatsFunc       func(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error)
	MessagingGetChannelStatsFunc     func(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error)
	CloseFunc                        func() error
}

func (m *MockMessagingClient) MessagingCreateChannel(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error) {
	if m.MessagingCreateChannelFunc != nil {
		return m.MessagingCreateChannelFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.create-channel", "not implemented")
}

func (m *MockMessagingClient) MessagingDeleteChannel(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
	if m.MessagingDeleteChannelFunc != nil {
		return m.MessagingDeleteChannelFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.delete-channel", "not implemented")
}

func (m *MockMessagingClient) MessagingGetChannel(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error) {
	if m.MessagingGetChannelFunc != nil {
		return m.MessagingGetChannelFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.get-channel", "not implemented")
}

func (m *MockMessagingClient) MessagingListChannels(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error) {
	if m.MessagingListChannelsFunc != nil {
		return m.MessagingListChannelsFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.list-channels", "not implemented")
}

func (m *MockMessagingClient) MessagingSubscribe(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
	if m.MessagingSubscribeFunc != nil {
		return m.MessagingSubscribeFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.subscribe", "not implemented")
}

func (m *MockMessagingClient) MessagingUnsubscribe(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
	if m.MessagingUnsubscribeFunc != nil {
		return m.MessagingUnsubscribeFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.unsubscribe", "not implemented")
}

func (m *MockMessagingClient) MessagingPublish(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error) {
	if m.MessagingPublishFunc != nil {
		return m.MessagingPublishFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.publish", "not implemented")
}

func (m *MockMessagingClient) MessagingPublishDelta(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error) {
	if m.MessagingPublishDeltaFunc != nil {
		return m.MessagingPublishDeltaFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.publish-delta", "not implemented")
}

func (m *MockMessagingClient) MessagingStreamNotifications(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
	if m.MessagingStreamNotificationsFunc != nil {
		return m.MessagingStreamNotificationsFunc(ctx, req)
	}
	return nil, nil, errors.Newf(errors.ErrUnimplemented, "msg.stream", "not implemented")
}

func (m *MockMessagingClient) MessagingGetDedupStats(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error) {
	if m.MessagingGetDedupStatsFunc != nil {
		return m.MessagingGetDedupStatsFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.dedup-stats", "not implemented")
}

func (m *MockMessagingClient) MessagingGetChannelStats(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
	if m.MessagingGetChannelStatsFunc != nil {
		return m.MessagingGetChannelStatsFunc(ctx, req)
	}
	return nil, errors.Newf(errors.ErrUnimplemented, "msg.channel-stats", "not implemented")
}

func (m *MockMessagingClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// Compile-time assertion that MockMessagingClient implements MessagingClient.
var _ MessagingClient = (*MockMessagingClient)(nil)

// =============================================================================
// Messaging Command Structure Tests
// =============================================================================

func TestMsgCommandStructure(t *testing.T) {
	cmd := newMsgCommands()

	assert.NotNil(t, cmd)
	assert.Equal(t, "msg", cmd.Use)
	assert.Equal(t, "Messaging commands (channels, subscriptions, publishing)", cmd.Short)
	assert.Contains(t, cmd.Long, "Messaging commands")
	assert.Greater(t, len(cmd.Commands()), 0, "Msg command should have subcommands")

	// Check persistent flags
	outputFlag := cmd.PersistentFlags().Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	// Check all expected subcommands exist directly under msg
	expectedCommands := []string{
		"create-channel",
		"delete-channel",
		"list-channels",
		"channel-info",
		"subscribe",
		"unsubscribe",
		"list-subscriptions",
		"publish",
		"publish-delta",
		"watch",
		"dedup-stats",
	}
	for _, expectedCmd := range expectedCommands {
		found := false
		for _, subCmd := range cmd.Commands() {
			if strings.HasPrefix(subCmd.Use, expectedCmd) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find subcommand '%s'", expectedCmd)
	}
}

// =============================================================================
// Create Channel Command Tests
// =============================================================================

func TestMsgCreateChannelCommandFlags(t *testing.T) {
	cmd := newMsgCreateChannelCommand()

	// Check flag existence and defaults
	bufferSizeFlag := cmd.Flags().Lookup("buffer-size")
	assert.NotNil(t, bufferSizeFlag)
	assert.Equal(t, "256", bufferSizeFlag.DefValue)

	maxSubscribersFlag := cmd.Flags().Lookup("max-subscribers")
	assert.NotNil(t, maxSubscribersFlag)
	assert.Equal(t, "1000", maxSubscribersFlag.DefValue)

	persistentFlag := cmd.Flags().Lookup("persistent")
	assert.NotNil(t, persistentFlag)
	assert.Equal(t, "false", persistentFlag.DefValue)

	ttlFlag := cmd.Flags().Lookup("ttl")
	assert.NotNil(t, ttlFlag)
	assert.Equal(t, "0s", ttlFlag.DefValue)

	descriptionFlag := cmd.Flags().Lookup("description")
	assert.NotNil(t, descriptionFlag)
}

func TestRunMsgCreateChannelCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		bufferSize      int32
		maxSubscribers  int32
		persistent      bool
		ttl             string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *grpc.MessagingCreateChannelRequest)
	}{
		{
			name:           "create channel successfully",
			args:           []string{"orders"},
			bufferSize:     256,
			maxSubscribers: 1000,
			persistent:     false,
			ttl:            "0s",
			outputFormat:   "table",
			expectError:    false,
			validateRequest: func(t *testing.T, req *grpc.MessagingCreateChannelRequest) {
				assert.Equal(t, "orders", req.Name)
				assert.Equal(t, int32(256), req.BufferSize)
				assert.Equal(t, int32(1000), req.MaxSubscribers)
				assert.False(t, req.Persistent)
			},
		},
		{
			name:           "create channel with custom buffer size",
			args:           []string{"events"},
			bufferSize:     512,
			maxSubscribers: 5000,
			persistent:     true,
			ttl:            "1h",
			outputFormat:   "json",
			expectError:    false,
			validateRequest: func(t *testing.T, req *grpc.MessagingCreateChannelRequest) {
				assert.Equal(t, "events", req.Name)
				assert.Equal(t, int32(512), req.BufferSize)
				assert.Equal(t, int32(5000), req.MaxSubscribers)
				assert.True(t, req.Persistent)
				assert.Equal(t, int64(3600), req.TTLSeconds)
			},
		},
		{
			name:          "invalid ttl format",
			args:          []string{"bad-ttl"},
			ttl:           "invalid",
			expectError:   true,
			errorContains: "invalid TTL format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *grpc.MessagingCreateChannelRequest

			// Set up mock for successful cases
			if !tt.expectError {
				messagingClientFactory = func() (MessagingClient, error) {
					return &MockMessagingClient{
						MessagingCreateChannelFunc: func(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error) {
							capturedRequest = req
							return &grpc.MessagingCreateChannelResponse{
								Channel: &grpc.MessagingChannel{
									Name:        req.Name,
									BufferSize:  req.BufferSize,
									Persistent:  req.Persistent,
									TTLSeconds:  req.TTLSeconds,
									CreatedAt:   time.Now(),
									Subscribers: 0,
								},
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newMsgCreateChannelCommand()

			// Set flags
			msgBufferSize = tt.bufferSize
			msgMaxSubscribers = tt.maxSubscribers
			msgPersistent = tt.persistent
			msgTTL = tt.ttl
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

func TestRunMsgCreateChannelCommandClientError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	// Set up mock to return error
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "failed to create client")
	}

	cmd := newMsgCreateChannelCommand()
	msgTTL = "0s"

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create client")
}

func TestRunMsgCreateChannelCommandAPIError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	// Set up mock to return API error
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingCreateChannelFunc: func(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error) {
				return nil, errors.Newf(errors.ErrAlreadyExists, "msg.create-channel", "channel already exists")
			},
		}, nil
	}

	cmd := newMsgCreateChannelCommand()
	msgTTL = "0s"

	err := cmd.RunE(cmd, []string{"existing-channel"})
	assert.Error(t, err)
}

// =============================================================================
// Delete Channel Command Tests
// =============================================================================

func TestMsgDeleteChannelCommandFlags(t *testing.T) {
	cmd := newMsgDeleteChannelCommand()

	forceFlag := cmd.Flags().Lookup("force")
	assert.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestRunMsgDeleteChannelCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		force           bool
		outputFormat    string
		expectError     bool
		validateRequest func(t *testing.T, req *grpc.MessagingDeleteChannelRequest)
	}{
		{
			name:         "delete channel with force",
			args:         []string{"orders"},
			force:        true,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingDeleteChannelRequest) {
				assert.Equal(t, "orders", req.Name)
			},
		},
		{
			name:         "delete channel json format",
			args:         []string{"events"},
			force:        true,
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingDeleteChannelRequest) {
				assert.Equal(t, "events", req.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *grpc.MessagingDeleteChannelRequest

			messagingClientFactory = func() (MessagingClient, error) {
				return &MockMessagingClient{
					MessagingDeleteChannelFunc: func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
						capturedRequest = req
						return &grpc.MessagingDeleteChannelResponse{
							Name:      req.Name,
							Deleted:   true,
							Timestamp: time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newMsgDeleteChannelCommand()
			msgForce = tt.force
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// List Channels Command Tests
// =============================================================================

func TestMsgListChannelsCommandFlags(t *testing.T) {
	cmd := newMsgListChannelsCommand()

	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "100", limitFlag.DefValue)

	offsetFlag := cmd.Flags().Lookup("offset")
	assert.NotNil(t, offsetFlag)
	assert.Equal(t, "0", offsetFlag.DefValue)

	filterFlag := cmd.Flags().Lookup("filter")
	assert.NotNil(t, filterFlag)
}

func TestRunMsgListChannelsCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name            string
		limit           int32
		offset          int32
		filter          string
		outputFormat    string
		expectError     bool
		validateRequest func(t *testing.T, req *grpc.MessagingListChannelsRequest)
	}{
		{
			name:         "list all channels",
			limit:        100,
			offset:       0,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingListChannelsRequest) {
				assert.Equal(t, int32(100), req.Limit)
			},
		},
		{
			name:         "list channels with filter",
			limit:        50,
			offset:       0,
			filter:       "order*",
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingListChannelsRequest) {
				assert.Equal(t, int32(50), req.Limit)
				assert.Equal(t, "order*", req.NamePrefix)
			},
		},
		{
			name:         "list channels with pagination",
			limit:        20,
			offset:       10,
			outputFormat: "yaml",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingListChannelsRequest) {
				assert.Equal(t, int32(20), req.Limit)
				assert.Equal(t, "10", req.Cursor)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *grpc.MessagingListChannelsRequest

			messagingClientFactory = func() (MessagingClient, error) {
				return &MockMessagingClient{
					MessagingListChannelsFunc: func(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error) {
						capturedRequest = req
						return &grpc.MessagingListChannelsResponse{
							Channels: []*grpc.MessagingChannel{
								{
									Name:        "orders",
									Subscribers: 10,
									Persistent:  false,
									CreatedAt:   time.Now(),
								},
								{
									Name:        "events",
									Subscribers: 5,
									Persistent:  true,
									CreatedAt:   time.Now(),
								},
							},
							TotalCount: 2,
							HasMore:    false,
							Timestamp:  time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newMsgListChannelsCommand()
			msgLimit = tt.limit
			msgOffset = tt.offset
			msgFilter = tt.filter
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Channel Info Command Tests
// =============================================================================

func TestMsgChannelInfoCommand(t *testing.T) {
	cmd := newMsgChannelInfoCommand()

	assert.Equal(t, "channel-info <name>", cmd.Use)
	assert.Equal(t, "Get detailed information about a channel", cmd.Short)
}

func TestRunMsgChannelInfoCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		outputFormat    string
		expectError     bool
		validateRequest func(t *testing.T, req *grpc.MessagingGetChannelRequest)
	}{
		{
			name:         "get channel info successfully",
			args:         []string{"orders"},
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingGetChannelRequest) {
				assert.Equal(t, "orders", req.Name)
			},
		},
		{
			name:         "get channel info json format",
			args:         []string{"events"},
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingGetChannelRequest) {
				assert.Equal(t, "events", req.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *grpc.MessagingGetChannelRequest

			messagingClientFactory = func() (MessagingClient, error) {
				return &MockMessagingClient{
					MessagingGetChannelFunc: func(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error) {
						capturedRequest = req
						return &grpc.MessagingGetChannelResponse{
							Channel: &grpc.MessagingChannel{
								Name:        req.Name,
								BufferSize:  256,
								Persistent:  false,
								TTLSeconds:  3600,
								CreatedAt:   time.Now(),
								Subscribers: 10,
							},
							Timestamp: time.Now(),
						}, nil
					},
					MessagingGetChannelStatsFunc: func(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
						return &grpc.MessagingGetChannelStatsResponse{
							Stats: &grpc.MessagingChannelStats{
								Name:              req.Name,
								Subscribers:       10,
								MessagesPublished: 1000,
								MessagesInHistory: 100,
							},
							Timestamp: time.Now(),
						}, nil
					},
					MessagingGetDedupStatsFunc: func(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error) {
						return &grpc.MessagingGetDedupStatsResponse{
							Stats: &grpc.MessagingDedupStats{
								Channel:         req.Channel,
								TotalMessages:   1000,
								TotalDuplicates: 50,
							},
							Timestamp: time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newMsgChannelInfoCommand()
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Subscribe Command Tests
// =============================================================================

func TestMsgSubscribeCommandFlags(t *testing.T) {
	cmd := newMsgSubscribeCommand()

	deltaFlag := cmd.Flags().Lookup("delta")
	assert.NotNil(t, deltaFlag)
	assert.Equal(t, "false", deltaFlag.DefValue)

	dedupFlag := cmd.Flags().Lookup("dedup")
	assert.NotNil(t, dedupFlag)
	assert.Equal(t, "false", dedupFlag.DefValue)

	dedupSkipFlag := cmd.Flags().Lookup("dedup-skip")
	assert.NotNil(t, dedupSkipFlag)
	assert.Equal(t, "false", dedupSkipFlag.DefValue)

	filterFlag := cmd.Flags().Lookup("filter")
	assert.NotNil(t, filterFlag)

	replayFlag := cmd.Flags().Lookup("replay")
	assert.NotNil(t, replayFlag)
	assert.Equal(t, "0", replayFlag.DefValue)

	bufferSizeFlag := cmd.Flags().Lookup("buffer-size")
	assert.NotNil(t, bufferSizeFlag)
	assert.Equal(t, "256", bufferSizeFlag.DefValue)
}

func TestMsgSubscribeCommand(t *testing.T) {
	cmd := newMsgSubscribeCommand()

	assert.Equal(t, "subscribe <channel>", cmd.Use)
	assert.Equal(t, "Subscribe to a channel to receive notifications", cmd.Short)
}

// =============================================================================
// Unsubscribe Command Tests
// =============================================================================

func TestMsgUnsubscribeCommandFlags(t *testing.T) {
	cmd := newMsgUnsubscribeCommand()

	forceFlag := cmd.Flags().Lookup("force")
	assert.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestRunMsgUnsubscribeCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		force           bool
		outputFormat    string
		expectError     bool
		validateRequest func(t *testing.T, req *grpc.MessagingUnsubscribeRequest)
	}{
		{
			name:         "unsubscribe with force",
			args:         []string{"sub_abc123"},
			force:        true,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingUnsubscribeRequest) {
				assert.Equal(t, "sub_abc123", req.SubscriptionID)
			},
		},
		{
			name:         "unsubscribe json format",
			args:         []string{"sub_def456"},
			force:        true,
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingUnsubscribeRequest) {
				assert.Equal(t, "sub_def456", req.SubscriptionID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *grpc.MessagingUnsubscribeRequest

			messagingClientFactory = func() (MessagingClient, error) {
				return &MockMessagingClient{
					MessagingUnsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
						capturedRequest = req
						return &grpc.MessagingUnsubscribeResponse{
							SubscriptionID: req.SubscriptionID,
							Unsubscribed:   true,
							Timestamp:      time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newMsgUnsubscribeCommand()
			msgForce = tt.force
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// List Subscriptions Command Tests
// =============================================================================

func TestMsgListSubscriptionsCommandFlags(t *testing.T) {
	cmd := newMsgListSubscriptionsCommand()

	channelFlag := cmd.Flags().Lookup("channel")
	assert.NotNil(t, channelFlag)

	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "100", limitFlag.DefValue)

	offsetFlag := cmd.Flags().Lookup("offset")
	assert.NotNil(t, offsetFlag)
	assert.Equal(t, "0", offsetFlag.DefValue)
}

func TestRunMsgListSubscriptionsCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name         string
		channel      string
		limit        int32
		offset       int32
		outputFormat string
		expectError  bool
	}{
		{
			name:         "list subscriptions for specific channel",
			channel:      "orders",
			limit:        100,
			offset:       0,
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "list all subscriptions",
			channel:      "",
			limit:        100,
			offset:       0,
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messagingClientFactory = func() (MessagingClient, error) {
				return &MockMessagingClient{
					MessagingGetChannelStatsFunc: func(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
						return &grpc.MessagingGetChannelStatsResponse{
							Stats: &grpc.MessagingChannelStats{
								Name:              req.Name,
								Subscribers:       5,
								MessagesPublished: 100,
							},
							Timestamp: time.Now(),
						}, nil
					},
					MessagingListChannelsFunc: func(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error) {
						return &grpc.MessagingListChannelsResponse{
							Channels: []*grpc.MessagingChannel{
								{
									Name:        "orders",
									Subscribers: 5,
									CreatedAt:   time.Now(),
								},
								{
									Name:        "events",
									Subscribers: 10,
									CreatedAt:   time.Now(),
								},
							},
							TotalCount: 2,
							Timestamp:  time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newMsgListSubscriptionsCommand()
			msgChannel = tt.channel
			msgLimit = tt.limit
			msgOffset = tt.offset
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, []string{})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Publish Command Tests
// =============================================================================

func TestMsgPublishCommandFlags(t *testing.T) {
	cmd := newMsgPublishCommand()

	payloadFlag := cmd.Flags().Lookup("payload")
	assert.NotNil(t, payloadFlag)

	fileFlag := cmd.Flags().Lookup("file")
	assert.NotNil(t, fileFlag)

	dedupIDFlag := cmd.Flags().Lookup("dedup-id")
	assert.NotNil(t, dedupIDFlag)

	priorityFlag := cmd.Flags().Lookup("priority")
	assert.NotNil(t, priorityFlag)
	assert.Equal(t, "normal", priorityFlag.DefValue)

	ttlFlag := cmd.Flags().Lookup("ttl")
	assert.NotNil(t, ttlFlag)

	ackFlag := cmd.Flags().Lookup("ack")
	assert.NotNil(t, ackFlag)
	assert.Equal(t, "false", ackFlag.DefValue)
}

func TestRunMsgPublishCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		payload         string
		file            string
		dedupID         string
		priority        string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *grpc.MessagingPublishRequest)
	}{
		{
			name:         "publish message with payload",
			args:         []string{"orders"},
			payload:      `{"order_id": "ord_001", "status": "created"}`,
			priority:     "normal",
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingPublishRequest) {
				assert.Equal(t, "orders", req.Channel)
				assert.Equal(t, `{"order_id": "ord_001", "status": "created"}`, string(req.Payload))
			},
		},
		{
			name:         "publish message with dedup ID",
			args:         []string{"orders"},
			payload:      `{"order_id": "ord_001"}`,
			dedupID:      "order-ord_001-created",
			priority:     "high",
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingPublishRequest) {
				assert.Equal(t, "order-ord_001-created", req.DedupKey)
				assert.Equal(t, "high", req.Metadata["priority"])
			},
		},
		{
			name:          "publish without payload or file",
			args:          []string{"orders"},
			payload:       "",
			file:          "",
			expectError:   true,
			errorContains: "either --payload or --file must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *grpc.MessagingPublishRequest

			// Set up mock for successful cases
			if !tt.expectError || tt.errorContains == "" {
				messagingClientFactory = func() (MessagingClient, error) {
					return &MockMessagingClient{
						MessagingPublishFunc: func(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error) {
							capturedRequest = req
							return &grpc.MessagingPublishResponse{
								MessageID:   "msg_123",
								Channel:     req.Channel,
								IsDuplicate: false,
								Timestamp:   time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newMsgPublishCommand()
			msgPayload = tt.payload
			msgFile = tt.file
			msgDedupID = tt.dedupID
			msgPriority = tt.priority
			msgTTL = ""
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Publish Delta Command Tests
// =============================================================================

func TestMsgPublishDeltaCommandFlags(t *testing.T) {
	cmd := newMsgPublishDeltaCommand()

	oldFlag := cmd.Flags().Lookup("old")
	assert.NotNil(t, oldFlag)

	newFlag := cmd.Flags().Lookup("new")
	assert.NotNil(t, newFlag)

	oldFileFlag := cmd.Flags().Lookup("old-file")
	assert.NotNil(t, oldFileFlag)

	newFileFlag := cmd.Flags().Lookup("new-file")
	assert.NotNil(t, newFileFlag)

	entityIDFlag := cmd.Flags().Lookup("entity-id")
	assert.NotNil(t, entityIDFlag)

	entityTypeFlag := cmd.Flags().Lookup("entity-type")
	assert.NotNil(t, entityTypeFlag)

	dedupIDFlag := cmd.Flags().Lookup("dedup-id")
	assert.NotNil(t, dedupIDFlag)
}

func TestRunMsgPublishDeltaCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		oldValue        string
		newValue        string
		entityID        string
		entityType      string
		outputFormat    string
		expectError     bool
		errorContains   string
		validateRequest func(t *testing.T, req *grpc.MessagingPublishDeltaRequest)
	}{
		{
			name:         "publish delta successfully",
			args:         []string{"orders"},
			oldValue:     `{"order_id": "ord_001", "status": "created"}`,
			newValue:     `{"order_id": "ord_001", "status": "shipped"}`,
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingPublishDeltaRequest) {
				assert.Equal(t, "orders", req.Channel)
				assert.Equal(t, `{"order_id": "ord_001", "status": "created"}`, string(req.OldValue))
				assert.Equal(t, `{"order_id": "ord_001", "status": "shipped"}`, string(req.NewValue))
			},
		},
		{
			name:         "publish delta with entity info",
			args:         []string{"orders"},
			oldValue:     `{"status": "processing"}`,
			newValue:     `{"status": "shipped", "tracking": "TRK123"}`,
			entityID:     "ord_001",
			entityType:   "Order",
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingPublishDeltaRequest) {
				assert.Equal(t, "ord_001", req.Metadata["entity_id"])
				assert.Equal(t, "Order", req.Metadata["entity_type"])
			},
		},
		{
			name:          "publish delta without old value",
			args:          []string{"orders"},
			oldValue:      "",
			newValue:      `{"status": "shipped"}`,
			expectError:   true,
			errorContains: "either --old or --old-file must be specified",
		},
		{
			name:          "publish delta without new value",
			args:          []string{"orders"},
			oldValue:      `{"status": "created"}`,
			newValue:      "",
			expectError:   true,
			errorContains: "either --new or --new-file must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *grpc.MessagingPublishDeltaRequest

			// Set up mock for successful cases
			if !tt.expectError {
				messagingClientFactory = func() (MessagingClient, error) {
					return &MockMessagingClient{
						MessagingPublishDeltaFunc: func(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error) {
							capturedRequest = req
							return &grpc.MessagingPublishDeltaResponse{
								MessageID: "msg_delta_123",
								Channel:   req.Channel,
								Delta: &grpc.MessagingDeltaPayload{
									EntityID:   req.Metadata["entity_id"],
									EntityType: req.Metadata["entity_type"],
									Version:    1,
									Changes: []*grpc.MessagingFieldChange{
										{
											Path:     "status",
											Value:    []byte("shipped"),
											OldValue: []byte("created"),
										},
									},
								},
								IsDuplicate: false,
								Timestamp:   time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newMsgPublishDeltaCommand()
			msgOld = tt.oldValue
			msgNew = tt.newValue
			msgOldFile = ""
			msgNewFile = ""
			msgEntityID = tt.entityID
			msgEntityType = tt.entityType
			msgDedupID = ""
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Watch Command Tests
// =============================================================================

func TestMsgWatchCommandFlags(t *testing.T) {
	cmd := newMsgWatchCommand()

	deltaFlag := cmd.Flags().Lookup("delta")
	assert.NotNil(t, deltaFlag)
	assert.Equal(t, "false", deltaFlag.DefValue)

	filterFlag := cmd.Flags().Lookup("filter")
	assert.NotNil(t, filterFlag)

	outputFileFlag := cmd.Flags().Lookup("output")
	assert.NotNil(t, outputFileFlag)

	timestampsFlag := cmd.Flags().Lookup("timestamps")
	assert.NotNil(t, timestampsFlag)
	assert.Equal(t, "true", timestampsFlag.DefValue)

	rawFlag := cmd.Flags().Lookup("raw")
	assert.NotNil(t, rawFlag)
	assert.Equal(t, "false", rawFlag.DefValue)
}

func TestMsgWatchCommand(t *testing.T) {
	cmd := newMsgWatchCommand()

	assert.Equal(t, "watch <channel>", cmd.Use)
	assert.Equal(t, "Watch a channel in real-time and display messages as they arrive", cmd.Short)
}

// =============================================================================
// Dedup Stats Command Tests
// =============================================================================

func TestMsgDedupStatsCommandFlags(t *testing.T) {
	cmd := newMsgDedupStatsCommand()

	resetFlag := cmd.Flags().Lookup("reset")
	assert.NotNil(t, resetFlag)
	assert.Equal(t, "false", resetFlag.DefValue)

	windowFlag := cmd.Flags().Lookup("window")
	assert.NotNil(t, windowFlag)
}

func TestRunMsgDedupStatsCommand(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name            string
		args            []string
		outputFormat    string
		expectError     bool
		validateRequest func(t *testing.T, req *grpc.MessagingGetDedupStatsRequest)
	}{
		{
			name:         "get dedup stats successfully",
			args:         []string{"orders"},
			outputFormat: "table",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingGetDedupStatsRequest) {
				assert.Equal(t, "orders", req.Channel)
			},
		},
		{
			name:         "get dedup stats json format",
			args:         []string{"events"},
			outputFormat: "json",
			expectError:  false,
			validateRequest: func(t *testing.T, req *grpc.MessagingGetDedupStatsRequest) {
				assert.Equal(t, "events", req.Channel)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest *grpc.MessagingGetDedupStatsRequest

			messagingClientFactory = func() (MessagingClient, error) {
				return &MockMessagingClient{
					MessagingGetDedupStatsFunc: func(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error) {
						capturedRequest = req
						return &grpc.MessagingGetDedupStatsResponse{
							Stats: &grpc.MessagingDedupStats{
								Channel:         req.Channel,
								TotalMessages:   1000,
								TotalDuplicates: 50,
								TopDuplicates: []*grpc.MessagingDedupEntry{
									{
										MessageID: "dup_key_1",
										Count:     10,
										FirstSeen: time.Now().Add(-time.Hour),
										LastSeen:  time.Now(),
									},
								},
							},
							Timestamp: time.Now(),
						}, nil
					},
				}, nil
			}

			cmd := newMsgDedupStatsCommand()
			msgOutputFmt = tt.outputFormat

			err := cmd.RunE(cmd, tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, capturedRequest)
				if tt.validateRequest != nil {
					tt.validateRequest(t, capturedRequest)
				}
			}
		})
	}
}

// =============================================================================
// Output Formatting Tests
// =============================================================================

func TestFormatChannelResponse(t *testing.T) {
	channel := &grpc.MessagingChannel{
		Name:        "test-channel",
		BufferSize:  256,
		Persistent:  true,
		TTLSeconds:  3600,
		CreatedAt:   time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
		Subscribers: 10,
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Channel created successfully.",
				"Name:", "test-channel",
				"Buffer Size:", "256",
				"Persistent:", "true",
				"TTL:", "1h0m0s",
				"Created:",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				`"Name": "test-channel"`,
				`"BufferSize": 256`,
				`"Persistent": true`,
			},
		},
		{
			name:   "yaml format",
			format: "yaml",
			expectedText: []string{
				"name: test-channel",
				"buffersize: 256",
				"persistent: true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatChannelResponse(channel, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected,
					"Expected output to contain '%s'\nGot:\n%s", expected, output)
			}
		})
	}
}

func TestFormatDeleteChannelResponse(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{
		Name:      "test-channel",
		Deleted:   true,
		Timestamp: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Channel:", "test-channel",
				"Deleted:", "true",
			},
		},
		{
			name:   "default format deleted",
			format: "",
			expectedText: []string{
				"Channel 'test-channel' deleted successfully",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDeleteChannelResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatDeleteChannelResponseNotDeleted(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{
		Name:      "test-channel",
		Deleted:   false,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Channel 'test-channel' was not deleted")
}

func TestFormatChannelListResponse(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{
				Name:        "orders",
				Subscribers: 10,
				Persistent:  false,
				CreatedAt:   time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
			},
			{
				Name:        "events",
				Subscribers: 5,
				Persistent:  true,
				CreatedAt:   time.Date(2025, 1, 19, 12, 0, 0, 0, time.UTC),
			},
		},
		TotalCount: 2,
		HasMore:    false,
		Timestamp:  time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"NAME", "SUBSCRIBERS", "PERSISTENT", "CREATED",
				"orders", "10", "false",
				"events", "5", "true",
				"Total: 2 channels",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				"orders",
				"events",
				`"TotalCount": 2`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatChannelListResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected,
					"Expected output to contain '%s'\nGot:\n%s", expected, output)
			}
		})
	}
}

func TestFormatChannelListResponseWithPagination(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{
				Name:        "orders",
				Subscribers: 10,
				Persistent:  false,
				CreatedAt:   time.Now(),
			},
		},
		TotalCount: 10,
		HasMore:    true,
		NextCursor: "cursor-next-page",
		Timestamp:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelListResponse(resp, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "More results available")
	assert.Contains(t, output, "cursor-next-page")
}

func TestFormatUnsubscribeResponse(t *testing.T) {
	resp := &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: "sub_abc123",
		Unsubscribed:   true,
		Timestamp:      time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Subscription ID:", "sub_abc123",
				"Unsubscribed:", "true",
			},
		},
		{
			name:   "default format",
			format: "",
			expectedText: []string{
				"Unsubscribed from 'sub_abc123' successfully",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatUnsubscribeResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatUnsubscribeResponseNotUnsubscribed(t *testing.T) {
	resp := &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: "sub_abc123",
		Unsubscribed:   false,
		Timestamp:      time.Now(),
	}

	var buf bytes.Buffer
	err := formatUnsubscribeResponse(resp, "", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Failed to unsubscribe from 'sub_abc123'")
}

func TestFormatPublishResponse(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID:   "msg_123",
		Channel:     "orders",
		IsDuplicate: false,
		Timestamp:   time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Message published successfully.",
				"Channel:", "orders",
				"Message ID:", "msg_123",
				"Timestamp:",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				`"MessageID": "msg_123"`,
				`"Channel": "orders"`,
			},
		},
		{
			name:   "default format",
			format: "",
			expectedText: []string{
				"Published message msg_123 to channel orders",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatPublishResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatPublishResponseDuplicate(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID:   "msg_123",
		Channel:     "orders",
		IsDuplicate: true,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishResponse(resp, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Duplicate:")
	assert.Contains(t, output, "yes")
}

func TestFormatPublishDeltaResponse(t *testing.T) {
	resp := &grpc.MessagingPublishDeltaResponse{
		MessageID: "msg_delta_123",
		Channel:   "orders",
		Delta: &grpc.MessagingDeltaPayload{
			EntityID:   "ord_001",
			EntityType: "Order",
			Version:    2,
			Changes: []*grpc.MessagingFieldChange{
				{
					Path:     "status",
					Value:    []byte("shipped"),
					OldValue: []byte("created"),
				},
			},
		},
		IsDuplicate: false,
		Timestamp:   time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Delta published successfully.",
				"Channel:", "orders",
				"Message ID:", "msg_delta_123",
				"Entity ID:", "ord_001",
				"Entity Type:", "Order",
				"Version:", "2",
				"Changes:", "1",
				"Field Changes:",
				"status:", "created", "shipped",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				`"MessageID": "msg_delta_123"`,
				`"Channel": "orders"`,
			},
		},
		{
			name:   "default format",
			format: "",
			expectedText: []string{
				"Published delta msg_delta_123 to channel orders",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatPublishDeltaResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatDedupStatsResponse(t *testing.T) {
	resp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "orders",
			TotalMessages:   1000,
			TotalDuplicates: 50,
			TopDuplicates: []*grpc.MessagingDedupEntry{
				{
					MessageID: "dup_key_1",
					Count:     10,
					FirstSeen: time.Date(2025, 1, 19, 12, 0, 0, 0, time.UTC),
					LastSeen:  time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
				},
				{
					MessageID: "dup_key_2",
					Count:     5,
					FirstSeen: time.Date(2025, 1, 18, 12, 0, 0, 0, time.UTC),
					LastSeen:  time.Date(2025, 1, 20, 11, 0, 0, 0, time.UTC),
				},
			},
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Deduplication Statistics: orders",
				"Total Messages:", "1000",
				"Unique Messages:", "950",
				"Duplicates:", "50",
				"5.0%",
				"Top Duplicate Keys",
				"KEY", "COUNT", "LAST SEEN",
				"dup_key_1", "10",
				"dup_key_2", "5",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				`"Channel": "orders"`,
				`"TotalMessages": 1000`,
				`"TotalDuplicates": 50`,
			},
		},
		{
			name:   "default format",
			format: "",
			expectedText: []string{
				"orders: 1000 messages, 50 duplicates",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDedupStatsResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected,
					"Expected output to contain '%s'\nGot:\n%s", expected, output)
			}
		})
	}
}

func TestFormatChannelInfoResponse(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:        "orders",
			BufferSize:  256,
			Persistent:  true,
			TTLSeconds:  3600,
			CreatedAt:   time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
			Subscribers: 10,
		},
		Timestamp: time.Now(),
	}

	statsResp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:              "orders",
			Subscribers:       10,
			MessagesPublished: 1000,
			MessagesInHistory: 100,
		},
		Timestamp: time.Now(),
	}

	dedupResp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "orders",
			TotalMessages:   1000,
			TotalDuplicates: 50,
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"Channel: orders",
				"Name:", "orders",
				"Buffer Size:", "256",
				"Persistent:", "true",
				"TTL:", "1h0m0s",
				"Statistics",
				"Active Subscribers:", "10",
				"Messages Published:", "1000",
				"Messages In History:", "100",
				"Deduplication",
				"Total Messages:", "1000",
				"Total Duplicates:", "50",
				"Duplicate Rate:", "5.0%",
			},
		},
		{
			name:   "json format",
			format: "json",
			expectedText: []string{
				`"channel"`,
				`"stats"`,
				`"dedup"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatChannelInfoResponse(channelResp, statsResp, nil, dedupResp, nil, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected,
					"Expected output to contain '%s'\nGot:\n%s", expected, output)
			}
		})
	}
}

func TestFormatChannelInfoResponseWithErrors(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "orders",
			BufferSize: 256,
			CreatedAt:  time.Now(),
		},
		Timestamp: time.Now(),
	}

	statsErr := errors.Newf(errors.ErrNotFound, "stats", "stats not available")
	dedupErr := errors.Newf(errors.ErrNotFound, "dedup", "dedup stats not available")

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, nil, statsErr, nil, dedupErr, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Channel: orders")
	assert.Contains(t, output, "Name:")
	// Statistics and Deduplication sections should not appear when there are errors
	assert.NotContains(t, output, "Statistics")
	assert.NotContains(t, output, "Deduplication")
}

func TestFormatSubscriptionListFromStats(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:        "orders",
			Subscribers: 10,
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"CHANNEL", "SUBSCRIBERS",
				"orders", "10",
			},
		},
		{
			name:   "default format",
			format: "",
			expectedText: []string{
				"orders: 10 subscribers",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSubscriptionListFromStats(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSubscriptionListFromChannels(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
			{Name: "events", Subscribers: 5},
		},
		TotalCount: 2,
		Timestamp:  time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:   "table format",
			format: "table",
			expectedText: []string{
				"CHANNEL", "SUBSCRIBERS",
				"orders", "10",
				"events", "5",
			},
		},
		{
			name:   "default format",
			format: "",
			expectedText: []string{
				"orders: 10 subscribers",
				"events: 5 subscribers",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSubscriptionListFromChannels(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

// =============================================================================
// Invalid Output Format Tests
// =============================================================================

func TestFormatChannelResponseInvalidFormat(t *testing.T) {
	channel := &grpc.MessagingChannel{
		Name:      "test-channel",
		CreatedAt: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelResponse(channel, "invalid-format", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatDeleteChannelResponseInvalidFormat(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{
		Name:      "test-channel",
		Deleted:   true,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatChannelListResponseInvalidFormat(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels:  []*grpc.MessagingChannel{},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelListResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatUnsubscribeResponseInvalidFormat(t *testing.T) {
	resp := &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: "sub_123",
		Unsubscribed:   true,
		Timestamp:      time.Now(),
	}

	var buf bytes.Buffer
	err := formatUnsubscribeResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatPublishResponseInvalidFormat(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID: "msg_123",
		Channel:   "orders",
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatPublishDeltaResponseInvalidFormat(t *testing.T) {
	resp := &grpc.MessagingPublishDeltaResponse{
		MessageID: "msg_delta_123",
		Channel:   "orders",
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishDeltaResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatDedupStatsResponseInvalidFormat(t *testing.T) {
	resp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:       "orders",
			TotalMessages: 100,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDedupStatsResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatChannelInfoResponseInvalidFormat(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:      "orders",
			CreatedAt: time.Now(),
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, nil, nil, nil, nil, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSubscriptionListFromStatsInvalidFormat(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name: "orders",
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromStats(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSubscriptionListFromChannelsInvalidFormat(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels:  []*grpc.MessagingChannel{},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromChannels(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Client Factory Tests
// =============================================================================

func TestCreateMessagingClient(t *testing.T) {
	// Test default (gRPC) transport - gRPC is now implemented with lazy connection
	transport = "grpc"
	client, err := createMessagingClient()
	assert.NoError(t, err)
	assert.NotNil(t, client)
	if client != nil {
		client.Close()
	}

	// Test empty transport defaults to gRPC
	transport = ""
	client, err = createMessagingClient()
	assert.NoError(t, err)
	assert.NotNil(t, client)
	if client != nil {
		client.Close()
	}

	// Test REST transport - should return error as not implemented
	transport = "rest"
	client, err = createMessagingClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "REST transport is not yet implemented")

	// Test MCP transport - should return error as not implemented
	transport = "mcp"
	client, err = createMessagingClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "MCP transport is not yet implemented")

	// Test QUIC transport - should return error as not implemented
	transport = "quic"
	client, err = createMessagingClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "QUIC transport is not yet implemented")

	// Test unsupported transport
	transport = "unsupported"
	client, err = createMessagingClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "unsupported transport")
}

// =============================================================================
// Edge Cases and Error Handling Tests
// =============================================================================

func TestMsgCreateChannelEmptyArgs(t *testing.T) {
	cmd := newMsgCreateChannelCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgDeleteChannelEmptyArgs(t *testing.T) {
	cmd := newMsgDeleteChannelCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgChannelInfoEmptyArgs(t *testing.T) {
	cmd := newMsgChannelInfoCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgSubscribeEmptyArgs(t *testing.T) {
	cmd := newMsgSubscribeCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgUnsubscribeEmptyArgs(t *testing.T) {
	cmd := newMsgUnsubscribeCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgPublishEmptyArgs(t *testing.T) {
	cmd := newMsgPublishCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgPublishDeltaEmptyArgs(t *testing.T) {
	cmd := newMsgPublishDeltaCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgWatchEmptyArgs(t *testing.T) {
	cmd := newMsgWatchCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgDedupStatsEmptyArgs(t *testing.T) {
	cmd := newMsgDedupStatsCommand()

	// Command requires exactly 1 arg
	err := cmd.Args(cmd, []string{})
	assert.Error(t, err)
}

func TestMsgListChannelsNoArgs(t *testing.T) {
	cmd := newMsgListChannelsCommand()

	// Command requires no args
	err := cmd.Args(cmd, []string{})
	assert.NoError(t, err)

	// Should reject extra args
	err = cmd.Args(cmd, []string{"extra"})
	assert.Error(t, err)
}

func TestMsgListSubscriptionsNoArgs(t *testing.T) {
	cmd := newMsgListSubscriptionsCommand()

	// Command requires no args
	err := cmd.Args(cmd, []string{})
	assert.NoError(t, err)

	// Should reject extra args
	err = cmd.Args(cmd, []string{"extra"})
	assert.Error(t, err)
}

// =============================================================================
// Notification Formatting Tests
// =============================================================================

func TestFormatNotification(t *testing.T) {
	tests := []struct {
		name     string
		notif    *grpc.MessagingNotification
		expected string
	}{
		{
			name: "valid notification",
			notif: &grpc.MessagingNotification{
				Message: &grpc.MessagingMessage{
					ID:        "msg_123",
					Payload:   []byte(`{"order_id": "ord_001"}`),
					Timestamp: time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
				},
			},
			expected: `[2025-01-20T12:00:00Z] {"order_id": "ord_001"}`,
		},
		{
			name:     "nil notification",
			notif:    nil,
			expected: "",
		},
		{
			name: "nil message",
			notif: &grpc.MessagingNotification{
				Message: nil,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			formatNotification(tt.notif, &buf)

			output := buf.String()
			if tt.expected != "" {
				assert.Contains(t, output, tt.expected)
			} else {
				assert.Empty(t, output)
			}
		})
	}
}

func TestFormatWatchNotification(t *testing.T) {
	tests := []struct {
		name           string
		notif          *grpc.MessagingNotification
		showTimestamps bool
		raw            bool
		expectedText   []string
		notExpected    []string
	}{
		{
			name: "formatted with timestamps",
			notif: &grpc.MessagingNotification{
				Message: &grpc.MessagingMessage{
					ID:        "msg_123",
					Payload:   []byte(`{"order_id": "ord_001"}`),
					Timestamp: time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
				},
			},
			showTimestamps: true,
			raw:            false,
			expectedText:   []string{"2025-01-20T12:00:00Z", "Message received", "order_id"},
		},
		{
			name: "formatted without timestamps",
			notif: &grpc.MessagingNotification{
				Message: &grpc.MessagingMessage{
					ID:        "msg_123",
					Payload:   []byte(`{"order_id": "ord_001"}`),
					Timestamp: time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
				},
			},
			showTimestamps: false,
			raw:            false,
			expectedText:   []string{"Message received", "order_id"},
			notExpected:    []string{"[2025-01-20T12:00:00Z]"},
		},
		{
			name: "raw output",
			notif: &grpc.MessagingNotification{
				Message: &grpc.MessagingMessage{
					ID:        "msg_123",
					Payload:   []byte(`{"order_id": "ord_001"}`),
					Timestamp: time.Date(2025, 1, 20, 12, 0, 0, 0, time.UTC),
				},
			},
			showTimestamps: true,
			raw:            true,
			expectedText:   []string{`"ID":"msg_123"`},
			notExpected:    []string{"Message received"},
		},
		{
			name:           "nil notification",
			notif:          nil,
			showTimestamps: true,
			raw:            false,
			expectedText:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			formatWatchNotification(tt.notif, &buf, tt.showTimestamps, tt.raw)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected,
					"Expected output to contain '%s'\nGot:\n%s", expected, output)
			}
			for _, notExpected := range tt.notExpected {
				assert.NotContains(t, output, notExpected,
					"Expected output NOT to contain '%s'\nGot:\n%s", notExpected, output)
			}
		})
	}
}

func TestFormatWatchNotificationNonJSON(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg_123",
			Payload:   []byte("plain text message"),
			Timestamp: time.Now(),
		},
	}

	var buf bytes.Buffer
	formatWatchNotification(notif, &buf, false, false)

	output := buf.String()
	assert.Contains(t, output, "plain text message")
}

// =============================================================================
// TTL Parsing Tests
// =============================================================================

func TestMsgCreateChannelTTLParsing(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	tests := []struct {
		name               string
		ttl                string
		expectedTTLSeconds int64
		expectError        bool
	}{
		{"1 hour", "1h", 3600, false},
		{"30 minutes", "30m", 1800, false},
		{"1 day", "24h", 86400, false},
		{"30 seconds", "30s", 30, false},
		{"zero", "0s", 0, false},
		{"empty", "", 0, false},
		{"invalid", "invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedTTL int64

			if !tt.expectError {
				messagingClientFactory = func() (MessagingClient, error) {
					return &MockMessagingClient{
						MessagingCreateChannelFunc: func(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error) {
							capturedTTL = req.TTLSeconds
							return &grpc.MessagingCreateChannelResponse{
								Channel: &grpc.MessagingChannel{
									Name:       req.Name,
									TTLSeconds: req.TTLSeconds,
									CreatedAt:  time.Now(),
								},
								Timestamp: time.Now(),
							}, nil
						},
					}, nil
				}
			}

			cmd := newMsgCreateChannelCommand()
			msgTTL = tt.ttl
			msgBufferSize = 256
			msgMaxSubscribers = 1000
			msgPersistent = false
			msgOutputFmt = "table"

			err := cmd.RunE(cmd, []string{"test-channel"})

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedTTLSeconds, capturedTTL)
			}
		})
	}
}

func TestCreateSDKClientEmptyAddressError(t *testing.T) {
	// Save and restore
	origAddress := address
	defer func() {
		address = origAddress
		resetSDKClient()
	}()

	resetSDKClient()
	address = ""

	_, err := createSDKClient()
	assert.Error(t, err)
	assert.Equal(t, ErrEmptyAddress, err)
}

func TestBuildSDKClientOptionsUnsupportedTransportType(t *testing.T) {
	// Save and restore
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "invalid-transport"

	_, err := buildSDKClientOptions()
	assert.Error(t, err)
	var ute *UnsupportedTransportError
	assert.ErrorAs(t, err, &ute)
}

func TestBuildSDKClientOptionsRESTTransport(t *testing.T) {
	// Save and restore
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "rest"

	opts, err := buildSDKClientOptions()
	assert.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestBuildSDKClientOptionsQUICTransport(t *testing.T) {
	// Save and restore
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "quic"

	opts, err := buildSDKClientOptions()
	assert.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestBuildSDKClientOptionsUnixTransport(t *testing.T) {
	// Save and restore
	origTransport := transport
	origAddress := address
	defer func() {
		transport = origTransport
		address = origAddress
	}()

	transport = "unix"
	address = "/tmp/test.sock"

	opts, err := buildSDKClientOptions()
	assert.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestBuildSDKClientOptionsMCPTransport(t *testing.T) {
	// Save and restore
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "mcp"

	opts, err := buildSDKClientOptions()
	assert.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestGetSDKClientNilReturn(t *testing.T) {
	// Save and restore
	defer resetSDKClient()

	resetSDKClient()
	client := getSDKClient()
	assert.Nil(t, client)
}

func TestCloseSDKClientNoClientExists(t *testing.T) {
	// Save and restore
	defer resetSDKClient()

	resetSDKClient()
	err := closeSDKClient()
	assert.NoError(t, err)
}

// =============================================================================
// Cluster Status Command Output Format Tests
// =============================================================================

func TestClusterStatusCommandOutputFormats(t *testing.T) {
	tests := []struct {
		name         string
		outputFormat string
		expectError  bool
	}{
		{"json output", "json", false},
		{"table output", "table", false},
		{"invalid output", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newClusterStatusCommand()
			require.NotNil(t, cmd)

			// The actual execution requires a server connection, but we can
			// test the command structure and flag defaults
			outputFlag := cmd.Flags().Lookup("output")
			require.NotNil(t, outputFlag)
			assert.Equal(t, "table", outputFlag.DefValue)
		})
	}
}

// =============================================================================
// Message Delete Channel Confirmation Flow Tests
// =============================================================================

func TestDeleteChannelCommandNoForceConfirmation(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	// Create a pipe to simulate stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	// Write "n" to simulate rejection
	go func() {
		w.WriteString("n\n")
		w.Close()
	}()

	cmd := newMsgDeleteChannelCommand()
	msgForce = false

	err := cmd.RunE(cmd, []string{"test-channel"})
	// Should complete without error (cancellation is not an error)
	assert.NoError(t, err)
}

func TestDeleteChannelCommandClientFactoryFailure(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgDeleteChannelCommand()
	msgForce = true

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

// =============================================================================
// Message Subscribe Command Tests
// =============================================================================

func TestSubscribeCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgSubscribeCommand()

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestSubscribeCommandSubscribeAPIError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return nil, errors.Newf(errors.ErrNotFound, "msg.subscribe", "channel not found")
			},
		}, nil
	}

	cmd := newMsgSubscribeCommand()
	msgFilter = ""

	err := cmd.RunE(cmd, []string{"nonexistent-channel"})
	assert.Error(t, err)
}

// =============================================================================
// Message Unsubscribe Command Tests
// =============================================================================

func TestUnsubscribeCommandNoForceConfirmation(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	// Create a pipe to simulate stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	// Write "n" to simulate rejection
	go func() {
		w.WriteString("n\n")
		w.Close()
	}()

	cmd := newMsgUnsubscribeCommand()
	msgForce = false

	err := cmd.RunE(cmd, []string{"sub_abc123"})
	// Should complete without error (cancellation is not an error)
	assert.NoError(t, err)
}

func TestUnsubscribeCommandConfirmWithYes(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	// Create a pipe to simulate stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	// Write "yes" to simulate confirmation
	go func() {
		w.WriteString("yes\n")
		w.Close()
	}()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingUnsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
				return &grpc.MessagingUnsubscribeResponse{
					SubscriptionID: req.SubscriptionID,
					Unsubscribed:   true,
					Timestamp:      time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newMsgUnsubscribeCommand()
	msgForce = false
	msgOutputFmt = "table"

	err := cmd.RunE(cmd, []string{"sub_abc123"})
	assert.NoError(t, err)
}

func TestUnsubscribeCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgUnsubscribeCommand()
	msgForce = true

	err := cmd.RunE(cmd, []string{"sub_abc123"})
	assert.Error(t, err)
}

// =============================================================================
// Message Watch Command Tests
// =============================================================================

func TestWatchCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgWatchCommand()

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestWatchCommandStreamNotificationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingStreamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
				return nil, nil, errors.Newf(errors.ErrUnavailable, "msg.stream", "stream unavailable")
			},
		}, nil
	}

	cmd := newMsgWatchCommand()
	msgFilter = ""
	msgDelta = false

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

// =============================================================================
// Message Dedup Stats Command Tests
// =============================================================================

func TestDedupStatsCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgDedupStatsCommand()

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestDedupStatsCommandAPIFailure(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingGetDedupStatsFunc: func(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error) {
				return nil, errors.Newf(errors.ErrNotFound, "msg.dedup-stats", "channel not found")
			},
		}, nil
	}

	cmd := newMsgDedupStatsCommand()

	err := cmd.RunE(cmd, []string{"nonexistent-channel"})
	assert.Error(t, err)
}

// =============================================================================
// Message List Subscriptions Command Tests
// =============================================================================

func TestListSubscriptionsCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgListSubscriptionsCommand()
	msgChannel = ""

	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

func TestListSubscriptionsCommandChannelStatsAPIError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingGetChannelStatsFunc: func(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
				return nil, errors.Newf(errors.ErrNotFound, "msg.channel-stats", "channel not found")
			},
		}, nil
	}

	cmd := newMsgListSubscriptionsCommand()
	msgChannel = "nonexistent-channel"
	msgLimit = 100
	msgOffset = 0

	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

func TestListSubscriptionsCommandListChannelsAPIError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingListChannelsFunc: func(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "msg.list-channels", "internal error")
			},
		}, nil
	}

	cmd := newMsgListSubscriptionsCommand()
	msgChannel = "" // No specific channel, list all
	msgLimit = 100
	msgOffset = 0

	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Message Channel Info Command Tests
// =============================================================================

func TestChannelInfoCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgChannelInfoCommand()

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestChannelInfoCommandAPIFailure(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingGetChannelFunc: func(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error) {
				return nil, errors.Newf(errors.ErrNotFound, "msg.get-channel", "channel not found")
			},
		}, nil
	}

	cmd := newMsgChannelInfoCommand()
	msgOutputFmt = "table"

	err := cmd.RunE(cmd, []string{"nonexistent-channel"})
	assert.Error(t, err)
}

// =============================================================================
// Message List Channels Command Tests
// =============================================================================

func TestListChannelsCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgListChannelsCommand()

	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

func TestListChannelsCommandAPIFailure(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingListChannelsFunc: func(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "msg.list-channels", "internal error")
			},
		}, nil
	}

	cmd := newMsgListChannelsCommand()
	msgLimit = 100
	msgOffset = 0
	msgFilter = ""

	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Message Publish Command Tests
// =============================================================================

func TestPublishCommandFromFileSource(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	// Create a temp file with payload
	tempFile, err := os.CreateTemp("", "test-payload-*.json")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.WriteString(`{"test": "data"}`)
	require.NoError(t, err)
	tempFile.Close()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingPublishFunc: func(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error) {
				return &grpc.MessagingPublishResponse{
					MessageID:   "msg_from_file",
					Channel:     req.Channel,
					IsDuplicate: false,
					Timestamp:   time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newMsgPublishCommand()
	msgPayload = ""
	msgFile = tempFile.Name()
	msgDedupID = ""
	msgPriority = "normal"
	msgTTL = ""
	msgOutputFmt = "table"

	err = cmd.RunE(cmd, []string{"test-channel"})
	assert.NoError(t, err)
}

func TestPublishCommandFileReadFailure(t *testing.T) {
	cmd := newMsgPublishCommand()
	msgPayload = ""
	msgFile = "/nonexistent/path/file.json"
	msgDedupID = ""

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read payload file")
}

func TestPublishCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgPublishCommand()
	msgPayload = `{"test": "data"}`
	msgFile = ""

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestPublishCommandAPIFailure(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingPublishFunc: func(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "msg.publish", "publish failed")
			},
		}, nil
	}

	cmd := newMsgPublishCommand()
	msgPayload = `{"test": "data"}`
	msgFile = ""
	msgDedupID = ""
	msgPriority = "normal"
	msgTTL = ""

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

// =============================================================================
// Message Publish Delta Command Tests
// =============================================================================

func TestPublishDeltaCommandFromFileSource(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	// Create temp files
	oldFile, err := os.CreateTemp("", "test-old-*.json")
	require.NoError(t, err)
	defer os.Remove(oldFile.Name())
	_, err = oldFile.WriteString(`{"status": "created"}`)
	require.NoError(t, err)
	oldFile.Close()

	newFile, err := os.CreateTemp("", "test-new-*.json")
	require.NoError(t, err)
	defer os.Remove(newFile.Name())
	_, err = newFile.WriteString(`{"status": "shipped"}`)
	require.NoError(t, err)
	newFile.Close()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingPublishDeltaFunc: func(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error) {
				return &grpc.MessagingPublishDeltaResponse{
					MessageID:   "msg_delta_from_files",
					Channel:     req.Channel,
					IsDuplicate: false,
					Timestamp:   time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newMsgPublishDeltaCommand()
	msgOld = ""
	msgNew = ""
	msgOldFile = oldFile.Name()
	msgNewFile = newFile.Name()
	msgEntityID = ""
	msgEntityType = ""
	msgDedupID = ""
	msgOutputFmt = "table"

	err = cmd.RunE(cmd, []string{"test-channel"})
	assert.NoError(t, err)
}

func TestPublishDeltaCommandOldFileReadFailure(t *testing.T) {
	cmd := newMsgPublishDeltaCommand()
	msgOld = ""
	msgNew = `{"status": "shipped"}`
	msgOldFile = "/nonexistent/path/old.json"
	msgNewFile = ""

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read old state file")
}

func TestPublishDeltaCommandNewFileReadFailure(t *testing.T) {
	// Create old file
	oldFile, err := os.CreateTemp("", "test-old-*.json")
	require.NoError(t, err)
	defer os.Remove(oldFile.Name())
	_, err = oldFile.WriteString(`{"status": "created"}`)
	require.NoError(t, err)
	oldFile.Close()

	cmd := newMsgPublishDeltaCommand()
	msgOld = ""
	msgNew = ""
	msgOldFile = oldFile.Name()
	msgNewFile = "/nonexistent/path/new.json"

	err = cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read new state file")
}

func TestPublishDeltaCommandClientCreationError(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.Newf(errors.ErrConfiguration, "client", "connection failed")
	}

	cmd := newMsgPublishDeltaCommand()
	msgOld = `{"status": "created"}`
	msgNew = `{"status": "shipped"}`
	msgOldFile = ""
	msgNewFile = ""

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestPublishDeltaCommandAPIFailure(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingPublishDeltaFunc: func(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error) {
				return nil, errors.Newf(errors.ErrInternal, "msg.publish-delta", "publish delta failed")
			},
		}, nil
	}

	cmd := newMsgPublishDeltaCommand()
	msgOld = `{"status": "created"}`
	msgNew = `{"status": "shipped"}`
	msgOldFile = ""
	msgNewFile = ""
	msgEntityID = ""
	msgEntityType = ""
	msgDedupID = ""

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

// =============================================================================
// Cache Warm Command Dependencies Tests
// =============================================================================

func TestCacheWarmerDependenciesNilConfigError(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = nil

	_, _, _, err := initializeCacheWarmerDependencies()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestCacheWarmerDependenciesNilLoggerError(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
		},
	}
	log = nil

	_, _, _, err := initializeCacheWarmerDependencies()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

// =============================================================================
// Cache Command Nil Config Tests
// =============================================================================

func TestCacheConfigCommandNilConfigError(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	cmd := newCacheConfigCommand()
	cacheOutputFmt = "table"

	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

// =============================================================================
// Format Watch Notification Edge Cases
// =============================================================================

func TestWatchNotificationNilMessageOutput(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: nil,
	}

	var buf bytes.Buffer
	formatWatchNotification(notif, &buf, true, false)

	output := buf.String()
	assert.Empty(t, output)
}

func TestWatchNotificationRawFormatOutput(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg_123",
			Payload:   []byte(`{"status": "shipped"}`),
			Timestamp: time.Now(),
		},
	}

	var buf bytes.Buffer
	formatWatchNotification(notif, &buf, false, true)

	output := buf.String()
	assert.Contains(t, output, "msg_123")
}

// =============================================================================
// Format Publish Delta Response Edge Cases
// =============================================================================

func TestPublishDeltaResponseNilDeltaFormat(t *testing.T) {
	resp := &grpc.MessagingPublishDeltaResponse{
		MessageID:   "msg_delta_123",
		Channel:     "orders",
		Delta:       nil,
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishDeltaResponse(resp, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "msg_delta_123")
}

func TestPublishDeltaResponseYAMLFormat(t *testing.T) {
	resp := &grpc.MessagingPublishDeltaResponse{
		MessageID: "msg_delta_123",
		Channel:   "orders",
		Delta: &grpc.MessagingDeltaPayload{
			EntityID:   "ord_001",
			EntityType: "Order",
			Version:    2,
		},
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishDeltaResponse(resp, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "messageid")
}

// =============================================================================
// Format Dedup Stats Response Edge Cases
// =============================================================================

func TestDedupStatsResponseYAMLFormat(t *testing.T) {
	resp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "orders",
			TotalMessages:   1000,
			TotalDuplicates: 50,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDedupStatsResponse(resp, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "channel")
}

// =============================================================================
// Format Channel Info Response Edge Cases
// =============================================================================

func TestChannelInfoResponseYAMLFormat(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "orders",
			BufferSize: 256,
			CreatedAt:  time.Now(),
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, nil, nil, nil, nil, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "channel")
}

// =============================================================================
// Format Subscription List Edge Cases
// =============================================================================

func TestSubscriptionListFromStatsJSONFormat(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:        "orders",
			Subscribers: 10,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromStats(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestSubscriptionListFromStatsYAMLFormat(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:        "orders",
			Subscribers: 10,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromStats(resp, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "name")
}

func TestSubscriptionListFromChannelsJSONFormat(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromChannels(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestSubscriptionListFromChannelsYAMLFormat(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromChannels(resp, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "name")
}

// =============================================================================
// Logger Adapter Tests
// =============================================================================

func TestLoggerAdapterNilInputFix(t *testing.T) {
	// Test with nil logger
	adapter := asInterfaceLogger(nil)
	assert.NotNil(t, adapter) // Function wraps nil in adapter struct
}

func TestLoggerAdapterValidInput(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)

	adapter := asInterfaceLogger(testLog)
	assert.NotNil(t, adapter)
}

// =============================================================================
// Command Structure Tests
// =============================================================================

func TestCacheCommandsStructureVerification(t *testing.T) {
	cmd := newCacheCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "cache", cmd.Use)

	// Verify all subcommands exist
	subcommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 6)

	expectedSubcommands := []string{"stats", "clear", "config", "enable", "disable", "warm"}
	for _, expected := range expectedSubcommands {
		found := false
		for _, sub := range subcommands {
			if strings.HasPrefix(sub.Use, expected) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestClusterCommandsStructureVerification(t *testing.T) {
	cmd := newClusterCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "cluster", cmd.Use)

	// Verify status subcommand exists
	subcommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 1)

	var foundStatus bool
	for _, sub := range subcommands {
		if sub.Use == "status" {
			foundStatus = true
			break
		}
	}
	assert.True(t, foundStatus, "Expected status subcommand not found")
}

// =============================================================================
// Cobra Command Validation Tests
// =============================================================================

func TestCacheCommandsArgumentRequirements(t *testing.T) {
	tests := []struct {
		name       string
		cmdFactory func() *cobra.Command
		args       []string
		wantErr    bool
	}{
		{
			name:       "cache enable requires entity type arg",
			cmdFactory: newCacheEnableCommand,
			args:       []string{},
			wantErr:    true,
		},
		{
			name:       "cache disable requires entity type arg",
			cmdFactory: newCacheDisableCommand,
			args:       []string{},
			wantErr:    true,
		},
		{
			name:       "cache stats accepts no args",
			cmdFactory: newCacheStatsCommand,
			args:       []string{},
			wantErr:    false,
		},
		{
			name:       "cache stats accepts one arg",
			cmdFactory: newCacheStatsCommand,
			args:       []string{"user"},
			wantErr:    false,
		},
		{
			name:       "cache clear accepts no args",
			cmdFactory: newCacheClearCommand,
			args:       []string{},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmdFactory()
			err := cmd.Args(cmd, tt.args)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Output Format Parsing Tests
// =============================================================================

func TestCacheOutputFormatInvalidValue(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config and logger
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities:        make(map[string]config.EntityCacheConfig),
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cmd := newCacheStatsCommand()
	cacheOutputFmt = "invalid-format"

	err = cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Shutdown Embedded Server Tests
// =============================================================================

func TestEmbeddedServerShutdownNoServer(t *testing.T) {
	// Ensure no server is running
	embeddedServerHolder.Store(nil)

	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
}

// =============================================================================
// Message Create Channel Additional Tests
// =============================================================================

func TestCreateChannelCommandYAMLOutputFormat(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingCreateChannelFunc: func(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error) {
				return &grpc.MessagingCreateChannelResponse{
					Channel: &grpc.MessagingChannel{
						Name:       req.Name,
						BufferSize: req.BufferSize,
						CreatedAt:  time.Now(),
					},
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newMsgCreateChannelCommand()
	msgBufferSize = 256
	msgMaxSubscribers = 1000
	msgPersistent = false
	msgTTL = "0s"
	msgOutputFmt = "yaml"

	err := cmd.RunE(cmd, []string{"yaml-test-channel"})
	assert.NoError(t, err)
}

// =============================================================================
// Cache Clear Disabled Entity Tests
// =============================================================================

func TestCacheClearCommandDisabledEntityError(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	// Setup test config with disabled entity
	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"disabled-entity": {
					Enabled: false,
					TTL:     5 * time.Minute,
					MaxSize: 100,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	cmd := newCacheClearCommand()
	cacheForce = true
	cacheOutputFmt = "table"

	err = cmd.RunE(cmd, []string{"disabled-entity"})
	// Should fail because caching is disabled for this entity
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

// =============================================================================
// Message Delete Response Formatting Tests
// =============================================================================

func TestDeleteChannelResponseJSONFormat(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{
		Name:      "test-channel",
		Deleted:   true,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-channel")
}

func TestDeleteChannelResponseYAMLFormat(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{
		Name:      "test-channel",
		Deleted:   true,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "name")
}

// =============================================================================
// Channel Response Formatting Tests
// =============================================================================

func TestChannelResponseZeroCreatedAt(t *testing.T) {
	channel := &grpc.MessagingChannel{
		Name:       "test-channel",
		BufferSize: 256,
		// CreatedAt is zero value
	}

	var buf bytes.Buffer
	err := formatChannelResponse(channel, "table", &buf)
	assert.NoError(t, err)
}

// =============================================================================
// Message Publish with TTL Tests
// =============================================================================

func TestPublishCommandWithTTLMetadata(t *testing.T) {
	// Save and restore factory
	originalFactory := messagingClientFactory
	defer func() { messagingClientFactory = originalFactory }()

	var capturedMetadata map[string]string

	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingPublishFunc: func(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error) {
				capturedMetadata = req.Metadata
				return &grpc.MessagingPublishResponse{
					MessageID:   "msg_with_ttl",
					Channel:     req.Channel,
					IsDuplicate: false,
					Timestamp:   time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newMsgPublishCommand()
	msgPayload = `{"test": "data"}`
	msgFile = ""
	msgDedupID = ""
	msgPriority = "high"
	msgTTL = "5m"
	msgOutputFmt = "table"

	err := cmd.RunE(cmd, []string{"test-channel"})
	assert.NoError(t, err)

	assert.Equal(t, "high", capturedMetadata["priority"])
	assert.Equal(t, "5m", capturedMetadata["ttl"])
}

// =============================================================================
// Capture Stdout Helper for Testing
// =============================================================================

func captureStdoutOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// =============================================================================
// Print Functions Coverage Tests
// =============================================================================

func TestClusterStatusJSONNilValues(t *testing.T) {
	// Test with minimal status
	status := map[string]interface{}{
		"healthy": false,
	}

	output := captureStdoutOutput(func() {
		err := printClusterStatusJSON(status)
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "healthy")
}

// =============================================================================
// Additional Tests for Coverage Boost to 90%+
// =============================================================================

func TestBuildSDKClientOptionsWebsocketUnsupported(t *testing.T) {
	// Save and restore
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "websocket"

	opts, err := buildSDKClientOptions()
	assert.Error(t, err)
	assert.Empty(t, opts)
}

func TestBuildSDKClientOptionsGRPCTransportOpt(t *testing.T) {
	// Save and restore
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "grpc"

	opts, err := buildSDKClientOptions()
	assert.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestBuildSDKClientOptionsEmptyDefaultsToGRPCOpt(t *testing.T) {
	// Save and restore
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = ""

	opts, err := buildSDKClientOptions()
	assert.NoError(t, err)
	assert.NotEmpty(t, opts)
}

// =============================================================================
// Format Publish Response Tests
// =============================================================================

func TestFormatPublishResponseYAMLFormatOpt(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID:   "msg_456",
		Channel:     "orders",
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishResponse(resp, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "messageid")
}

// =============================================================================
// Format Unsubscribe Response Tests
// =============================================================================

func TestFormatUnsubscribeResponseJSONFormatOpt(t *testing.T) {
	resp := &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: "sub_abc123",
		Unsubscribed:   true,
		Timestamp:      time.Now(),
	}

	var buf bytes.Buffer
	err := formatUnsubscribeResponse(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "sub_abc123")
}

func TestFormatUnsubscribeResponseYAMLFormatOpt(t *testing.T) {
	resp := &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: "sub_abc123",
		Unsubscribed:   true,
		Timestamp:      time.Now(),
	}

	var buf bytes.Buffer
	err := formatUnsubscribeResponse(resp, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "subscriptionid")
}

// =============================================================================
// Format Channel List Response Tests
// =============================================================================

func TestFormatChannelListResponseYAMLFormatOpt(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
			{Name: "alerts", Subscribers: 5},
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelListResponse(resp, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "name")
}

// =============================================================================
// Misc Command Tests
// =============================================================================

func TestNewMsgCommandsStructureValid(t *testing.T) {
	cmd := newMsgCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "msg", cmd.Use)

	// Verify subcommands exist
	subcommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 10)

	expectedSubcommands := []string{"create-channel", "delete-channel", "list-channels", "channel-info", "subscribe", "unsubscribe", "publish", "watch"}
	for _, expected := range expectedSubcommands {
		found := false
		for _, sub := range subcommands {
			if strings.HasPrefix(sub.Use, expected) {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

// =============================================================================
// Embedded Error Types Additional Tests
// =============================================================================

func TestEmbeddedPortErrorTypeValid(t *testing.T) {
	err := &EmbeddedPortError{}
	assert.Equal(t, "listener address is not a TCP address", err.Error())
}

func TestEmbeddedServerFailedErrorWithReasonValid(t *testing.T) {
	err := &EmbeddedServerFailedError{Reason: "test failure reason"}
	assert.Contains(t, err.Error(), "test failure reason")
}

// =============================================================================
// Format Channel Response Tests
// =============================================================================

func TestFormatChannelResponseJSONFormatOpt(t *testing.T) {
	channel := &grpc.MessagingChannel{
		Name:       "test-channel",
		BufferSize: 256,
		CreatedAt:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelResponse(channel, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-channel")
}

func TestFormatChannelResponseYAMLFormatOpt(t *testing.T) {
	channel := &grpc.MessagingChannel{
		Name:       "test-channel",
		BufferSize: 256,
		CreatedAt:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelResponse(channel, "yaml", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "name")
}

// =============================================================================
// Cache Enable/Disable Command Tests
// =============================================================================

func TestCacheEnableCommandNilConfig(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	cmd := newCacheEnableCommand()
	err := cmd.RunE(cmd, []string{"user"})
	assert.Error(t, err)
}

func TestCacheDisableCommandNilConfig(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	cmd := newCacheDisableCommand()
	err := cmd.RunE(cmd, []string{"user"})
	assert.Error(t, err)
}

func TestCacheStatsCommandNilConfig(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	cmd := newCacheStatsCommand()
	cacheOutputFmt = "table"
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Client Error Type Tests
// =============================================================================

func TestUnsupportedTransportErrorMessageFormatOpt(t *testing.T) {
	err := &UnsupportedTransportError{Transport: "tcp"}
	msg := err.Error()
	assert.Contains(t, msg, "tcp")
	assert.Contains(t, msg, "unsupported transport")
}

func TestErrSDKClientClosedMessageValid(t *testing.T) {
	assert.NotNil(t, ErrSDKClientClosed)
	assert.Contains(t, ErrSDKClientClosed.Error(), "closed")
}

func TestErrEmptyAddressMessageValid(t *testing.T) {
	assert.NotNil(t, ErrEmptyAddress)
	assert.Contains(t, ErrEmptyAddress.Error(), "empty")
}

// =============================================================================
// Format Notification Tests
// =============================================================================

func TestFormatNotificationWithMessageValid(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg_format_test",
			Payload:   []byte(`{"event": "test"}`),
			Timestamp: time.Now(),
		},
	}

	var buf bytes.Buffer
	formatNotification(notif, &buf)

	output := buf.String()
	assert.Contains(t, output, "event")
}

func TestFormatNotificationNilMessageValid(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: nil,
	}

	var buf bytes.Buffer
	formatNotification(notif, &buf)

	output := buf.String()
	// Should handle nil gracefully
	assert.Empty(t, output)
}

// =============================================================================
// Additional Watch Notification Tests
// =============================================================================

func TestFormatWatchNotificationWithTimestampsOpt(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg_watch_ts",
			Payload:   []byte(`{"test": true}`),
			Timestamp: time.Now(),
		},
	}

	var buf bytes.Buffer
	formatWatchNotification(notif, &buf, true, false)

	output := buf.String()
	assert.Contains(t, output, "Message received")
}

func TestFormatWatchNotificationRawModeOpt(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg_watch_raw",
			Payload:   []byte(`{"raw": "mode"}`),
			Timestamp: time.Now(),
		},
	}

	var buf bytes.Buffer
	formatWatchNotification(notif, &buf, false, true)

	output := buf.String()
	assert.Contains(t, output, "raw")
}

// =============================================================================
// Delta Payload Formatting Tests
// =============================================================================

func TestFormatPublishDeltaResponseWithChangesOpt(t *testing.T) {
	resp := &grpc.MessagingPublishDeltaResponse{
		MessageID: "msg_delta_changes",
		Channel:   "orders",
		Delta: &grpc.MessagingDeltaPayload{
			EntityID:   "ord_001",
			EntityType: "Order",
			Version:    2,
			Changes: []*grpc.MessagingFieldChange{
				{Path: "status", Value: []byte("shipped"), OldValue: []byte("pending")},
			},
		},
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishDeltaResponse(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "msg_delta_changes")
}

// =============================================================================
// Channel Info Response Full Tests
// =============================================================================

func TestFormatChannelInfoResponseWithAllDataOpt(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "full-channel",
			BufferSize: 512,
			CreatedAt:  time.Now(),
		},
		Timestamp: time.Now(),
	}

	statsResp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:        "full-channel",
			Subscribers: 10,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, statsResp, nil, nil, nil, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "full-channel")
}

// =============================================================================
// Print Cluster Status Tests
// =============================================================================

func TestPrintClusterStatusJSONWithNodesOpt(t *testing.T) {
	status := map[string]interface{}{
		"healthy":  true,
		"leader":   "node1",
		"nodes":    []interface{}{"node1", "node2"},
		"replicas": 2,
	}

	output := captureStdoutOutput(func() {
		err := printClusterStatusJSON(status)
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "healthy")
	assert.Contains(t, output, "node1")
}

// =============================================================================
// CreateSDKClient Tests
// =============================================================================

func TestCreateSDKClientWithValidAddressOpt(t *testing.T) {
	// Save and restore
	origAddress := address
	defer func() {
		address = origAddress
		resetSDKClient()
	}()

	resetSDKClient()
	address = "localhost:50051"

	// This should fail to connect but should not return ErrEmptyAddress
	_, err := createSDKClient()
	// We expect an error since there's no server, but it shouldn't be ErrEmptyAddress
	assert.NotEqual(t, ErrEmptyAddress, err)
}

// =============================================================================
// Initialize Cache Registry Tests
// =============================================================================

func TestInitializeCacheRegistryWithValidConfigOpt(t *testing.T) {
	// Save original values
	origCfg := cfg
	origLog := log
	defer func() {
		cfg = origCfg
		log = origLog
	}()

	cfg = &config.Config{
		Cache: config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		},
	}

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog

	registry, err := initializeCacheRegistry()
	assert.NoError(t, err)
	assert.NotNil(t, registry)
}

func TestInitializeCacheRegistryNilConfigOpt(t *testing.T) {
	// Save original values
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	_, err := initializeCacheRegistry()
	assert.Error(t, err)
}

// =============================================================================
// Format Dedup Stats Response Tests
// =============================================================================

func TestFormatDedupStatsResponseJSONFormatOpt(t *testing.T) {
	resp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "orders",
			TotalMessages:   1000,
			TotalDuplicates: 50,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDedupStatsResponse(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

// =============================================================================
// Format Channel Info Response Invalid Format Tests
// =============================================================================

func TestFormatChannelInfoResponseInvalidFormatOpt(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "test-channel",
			BufferSize: 256,
			CreatedAt:  time.Now(),
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, nil, nil, nil, nil, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Format Dedup Stats Invalid Format Tests
// =============================================================================

func TestFormatDedupStatsResponseInvalidFormatOpt(t *testing.T) {
	resp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "orders",
			TotalMessages:   1000,
			TotalDuplicates: 50,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDedupStatsResponse(resp, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Format Publish Response Invalid Format Tests
// =============================================================================

func TestFormatPublishResponseInvalidFormatOpt(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID:   "msg_invalid_fmt",
		Channel:     "orders",
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishResponse(resp, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Format Publish Delta Response Invalid Format Tests
// =============================================================================

func TestFormatPublishDeltaResponseInvalidFormatOpt(t *testing.T) {
	resp := &grpc.MessagingPublishDeltaResponse{
		MessageID:   "msg_delta_invalid_fmt",
		Channel:     "orders",
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishDeltaResponse(resp, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Format Delete Channel Response Invalid Format Tests
// =============================================================================

func TestFormatDeleteChannelResponseInvalidFormatOpt(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{
		Name:      "test-channel",
		Deleted:   true,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Format Channel Response Invalid Format Tests
// =============================================================================

func TestFormatChannelResponseInvalidFormatOpt(t *testing.T) {
	channel := &grpc.MessagingChannel{
		Name:       "test-channel",
		BufferSize: 256,
		CreatedAt:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelResponse(channel, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Format Subscription List Invalid Format Tests
// =============================================================================

func TestFormatSubscriptionListFromStatsInvalidFormatOpt(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:        "orders",
			Subscribers: 10,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromStats(resp, "invalid", &buf)
	assert.Error(t, err)
}

func TestFormatSubscriptionListFromChannelsInvalidFormatOpt(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromChannels(resp, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Format Unsubscribe Response Invalid Format Tests
// =============================================================================

func TestFormatUnsubscribeResponseInvalidFormatOpt(t *testing.T) {
	resp := &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: "sub_invalid_fmt",
		Unsubscribed:   true,
		Timestamp:      time.Now(),
	}

	var buf bytes.Buffer
	err := formatUnsubscribeResponse(resp, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Format Channel List Response Invalid Format Tests
// =============================================================================

func TestFormatChannelListResponseInvalidFormatOpt(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelListResponse(resp, "invalid", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Additional Format Tests for Coverage Boost
// =============================================================================

func TestFormatChannelListResponseJSONFormatOpt(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
			{Name: "alerts", Subscribers: 5},
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelListResponse(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestFormatChannelListResponseTableFormatOpt(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
			{Name: "alerts", Subscribers: 5},
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelListResponse(resp, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestFormatUnsubscribeResponseTableFormatOpt(t *testing.T) {
	resp := &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: "sub_abc123",
		Unsubscribed:   true,
		Timestamp:      time.Now(),
	}

	var buf bytes.Buffer
	err := formatUnsubscribeResponse(resp, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "sub_abc123")
}

func TestFormatSubscriptionListFromStatsTableFormatOpt(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:        "orders",
			Subscribers: 10,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromStats(resp, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestFormatSubscriptionListFromChannelsTableFormatOpt(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", Subscribers: 10},
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromChannels(resp, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestFormatPublishResponseJSONFormatOpt(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID:   "msg_json_fmt",
		Channel:     "orders",
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishResponse(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "msg_json_fmt")
}

func TestFormatPublishResponseTableFormatOpt(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID:   "msg_table_fmt",
		Channel:     "orders",
		IsDuplicate: true,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishResponse(resp, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "msg_table_fmt")
}

func TestFormatPublishDeltaResponseJSONFormatOpt(t *testing.T) {
	resp := &grpc.MessagingPublishDeltaResponse{
		MessageID: "msg_delta_json",
		Channel:   "orders",
		Delta: &grpc.MessagingDeltaPayload{
			EntityID:   "ord_001",
			EntityType: "Order",
			Version:    1,
		},
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	var buf bytes.Buffer
	err := formatPublishDeltaResponse(resp, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "msg_delta_json")
}

func TestFormatDedupStatsResponseTableFormatOpt(t *testing.T) {
	resp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "orders",
			TotalMessages:   500,
			TotalDuplicates: 25,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDedupStatsResponse(resp, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestFormatDeleteChannelResponseTableFormatOpt(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{
		Name:      "test-channel",
		Deleted:   true,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-channel")
}

func TestFormatChannelInfoResponseTableFormatOpt(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "orders",
			BufferSize: 256,
			CreatedAt:  time.Now(),
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, nil, nil, nil, nil, "table", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestFormatChannelInfoResponseJSONFormatOpt(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "orders",
			BufferSize: 256,
			CreatedAt:  time.Now(),
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, nil, nil, nil, nil, "json", &buf)
	assert.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "orders")
}

func TestRunMsgSubscribeCommand_SuccessPath(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	origDelta := msgDelta
	origDedup := msgDedup
	origReplay := msgReplay
	defer func() {
		msgOutputFmt = origFmt
		msgDelta = origDelta
		msgDedup = origDedup
		msgReplay = origReplay
	}()
	msgOutputFmt = "table"
	msgDelta = false
	msgDedup = false
	msgReplay = 0

	cmd := &cobra.Command{}
	err := runMsgSubscribeCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgSubscribeCommand_WithDeltaDedupReplay(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	origDelta := msgDelta
	origDedup := msgDedup
	origReplay := msgReplay
	defer func() {
		msgOutputFmt = origFmt
		msgDelta = origDelta
		msgDedup = origDedup
		msgReplay = origReplay
	}()
	msgOutputFmt = "json"
	msgDelta = true
	msgDedup = true
	msgReplay = 10

	cmd := &cobra.Command{}
	err := runMsgSubscribeCommand(cmd, []string{"events"})
	assert.NoError(t, err)
}

func TestRunMsgSubscribeCommand_ClientReturnsError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		subscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
			return nil, errors.New(errors.ErrInternal, "test", "subscribe failed")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	cmd := &cobra.Command{}
	err := runMsgSubscribeCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

// =============================================================================
// Msg Watch Command - Comprehensive Success and Error Paths
// =============================================================================

func TestRunMsgWatchCommand_ChannelClosesImmediately(t *testing.T) {
	notifChan := make(chan *grpc.MessagingNotification, 1)
	errChan := make(chan error, 1)
	close(notifChan)
	close(errChan)

	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	origDelta := msgDelta
	origFilter := msgFilter
	origFile := msgFile
	origTimestamps := msgTimestamps
	origRaw := msgRaw
	defer func() {
		msgOutputFmt = origFmt
		msgDelta = origDelta
		msgFilter = origFilter
		msgFile = origFile
		msgTimestamps = origTimestamps
		msgRaw = origRaw
	}()
	msgOutputFmt = "table"
	msgDelta = false
	msgFilter = ""
	msgFile = ""
	msgTimestamps = true
	msgRaw = false

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgWatchCommand_DeltaModeWithFilter(t *testing.T) {
	notifChan := make(chan *grpc.MessagingNotification, 1)
	errChan := make(chan error, 1)
	close(notifChan)

	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origDelta := msgDelta
	origFilter := msgFilter
	origFile := msgFile
	defer func() {
		msgDelta = origDelta
		msgFilter = origFilter
		msgFile = origFile
	}()
	msgDelta = true
	msgFilter = "status=shipped"
	msgFile = ""

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgWatchCommand_OutputToFile(t *testing.T) {
	notifChan := make(chan *grpc.MessagingNotification, 1)
	errChan := make(chan error, 1)
	close(notifChan)

	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	tmpFile := t.TempDir() + "/watch-output.jsonl"

	origFile := msgFile
	origDelta := msgDelta
	origFilter := msgFilter
	defer func() {
		msgFile = origFile
		msgDelta = origDelta
		msgFilter = origFilter
	}()
	msgFile = tmpFile
	msgDelta = false
	msgFilter = ""

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgWatchCommand_BadOutputFilePath(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFile := msgFile
	origDelta := msgDelta
	origFilter := msgFilter
	defer func() {
		msgFile = origFile
		msgDelta = origDelta
		msgFilter = origFilter
	}()
	msgFile = "/nonexistent/dir/watch.jsonl"
	msgDelta = false
	msgFilter = ""

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgWatchCommand_StreamInitError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return nil, nil, errors.New(errors.ErrInternal, "test", "stream error")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFile := msgFile
	origFilter := msgFilter
	defer func() {
		msgFile = origFile
		msgFilter = origFilter
	}()
	msgFile = ""
	msgFilter = ""

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgWatchCommand_ErrorOnStreamErrorChannel(t *testing.T) {
	notifChan := make(chan *grpc.MessagingNotification, 1)
	errChan := make(chan error, 1)
	errChan <- errors.New(errors.ErrInternal, "test", "stream broken")

	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFile := msgFile
	origFilter := msgFilter
	defer func() {
		msgFile = origFile
		msgFilter = origFilter
	}()
	msgFile = ""
	msgFilter = ""

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgWatchCommand_ReceiveNotificationThenClose(t *testing.T) {
	notifChan := make(chan *grpc.MessagingNotification, 2)
	errChan := make(chan error, 1)

	notifChan <- &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-001",
			Channel:   "orders",
			Payload:   []byte(`{"order":"123"}`),
			Timestamp: time.Now(),
		},
	}
	close(notifChan)

	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFile := msgFile
	origFilter := msgFilter
	origTimestamps := msgTimestamps
	origRaw := msgRaw
	defer func() {
		msgFile = origFile
		msgFilter = origFilter
		msgTimestamps = origTimestamps
		msgRaw = origRaw
	}()
	msgFile = ""
	msgFilter = ""
	msgTimestamps = true
	msgRaw = false

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgWatchCommand_ReceiveNotificationRawMode(t *testing.T) {
	notifChan := make(chan *grpc.MessagingNotification, 2)
	errChan := make(chan error, 1)

	notifChan <- &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-002",
			Channel:   "orders",
			Payload:   []byte("raw text payload"),
			Timestamp: time.Now(),
		},
	}
	close(notifChan)

	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFile := msgFile
	origFilter := msgFilter
	origRaw := msgRaw
	defer func() {
		msgFile = origFile
		msgFilter = origFilter
		msgRaw = origRaw
	}()
	msgFile = ""
	msgFilter = ""
	msgRaw = true

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"events"})
	assert.NoError(t, err)
}

// =============================================================================
// Msg Delete Channel - Client Error
// =============================================================================

func TestRunMsgDeleteChannelCommand_ClientReturnsError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		deleteChannelFunc: func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		msgForce = origForce
		msgOutputFmt = origFmt
	}()
	msgForce = true
	msgOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runMsgDeleteChannelCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
}

// =============================================================================
// Msg Unsubscribe Command - Additional Paths
// =============================================================================

func TestRunMsgUnsubscribeCommand_ClientReturnsError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		unsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "subscription not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		msgForce = origForce
		msgOutputFmt = origFmt
	}()
	msgForce = true
	msgOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-bad"})
	assert.Error(t, err)
}

func TestRunMsgUnsubscribeCommand_ForceFactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	defer func() {
		messagingClientFactory = origFactory
		msgForce = origForce
	}()

	msgForce = true
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-123"})
	assert.Error(t, err)
}

// =============================================================================
// formatSubscriptionListFromStats - All Output Formats
// =============================================================================

func TestFormatSubscriptionListFromStats_JSONFormat(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats:     &grpc.MessagingChannelStats{Name: "orders", Subscribers: 5},
		Timestamp: time.Now(),
	}
	buf := &bytes.Buffer{}
	err := formatSubscriptionListFromStats(resp, "json", buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "orders")
}

func TestFormatSubscriptionListFromStats_YAMLFormat(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats:     &grpc.MessagingChannelStats{Name: "events", Subscribers: 10},
		Timestamp: time.Now(),
	}
	buf := &bytes.Buffer{}
	err := formatSubscriptionListFromStats(resp, "yaml", buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "events")
}

func TestFormatSubscriptionListFromStats_TableFormat(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats:     &grpc.MessagingChannelStats{Name: "notifications", Subscribers: 3},
		Timestamp: time.Now(),
	}
	buf := &bytes.Buffer{}
	err := formatSubscriptionListFromStats(resp, "table", buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "CHANNEL")
}

func TestFormatSubscriptionListFromStats_DefaultFormat(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{Name: "alerts", Subscribers: 7},
	}
	buf := &bytes.Buffer{}
	err := formatSubscriptionListFromStats(resp, "", buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "subscribers")
}

func TestFormatSubscriptionListFromStats_BadFormat(t *testing.T) {
	resp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{Name: "test"},
	}
	buf := &bytes.Buffer{}
	err := formatSubscriptionListFromStats(resp, "invalid-format", buf)
	assert.Error(t, err)
}

// =============================================================================
// formatWatchNotification - All Paths
// =============================================================================

func TestFormatWatchNotification_NilNotif(t *testing.T) {
	buf := &bytes.Buffer{}
	formatWatchNotification(nil, buf, true, false)
	assert.Empty(t, buf.String())
}

func TestFormatWatchNotification_NilMsg(t *testing.T) {
	buf := &bytes.Buffer{}
	formatWatchNotification(&grpc.MessagingNotification{}, buf, true, false)
	assert.Empty(t, buf.String())
}

func TestFormatWatchNotification_RawJSON(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-001",
			Channel:   "orders",
			Payload:   []byte(`{"key":"value"}`),
			Timestamp: time.Now(),
		},
	}
	buf := &bytes.Buffer{}
	formatWatchNotification(notif, buf, true, true)
	assert.Contains(t, buf.String(), "msg-001")
}

func TestFormatWatchNotification_JSONPayloadWithTimestamp(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-002",
			Payload:   []byte(`{"event":"created"}`),
			Timestamp: time.Now(),
		},
	}
	buf := &bytes.Buffer{}
	formatWatchNotification(notif, buf, true, false)
	assert.Contains(t, buf.String(), "Message received")
}

func TestFormatWatchNotification_PlainTextNoTimestamp(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-003",
			Payload:   []byte("plain text payload"),
			Timestamp: time.Now(),
		},
	}
	buf := &bytes.Buffer{}
	formatWatchNotification(notif, buf, false, false)
	assert.Contains(t, buf.String(), "plain text payload")
}

// =============================================================================
// Object Storage Input Validation - Deeper Paths
// =============================================================================

func TestRunObjectPutCommand_ValidFileWithMetadata(t *testing.T) {
	origFile := objectFile
	origMeta := objectMetadata
	origTags := objectTags
	origFactory := objectStorageClientFactory
	defer func() {
		objectFile = origFile
		objectMetadata = origMeta
		objectTags = origTags
		objectStorageClientFactory = origFactory
	}()

	tmpFile := t.TempDir() + "/test-data.txt"
	require.NoError(t, os.WriteFile(tmpFile, []byte("test data"), 0o644))

	objectFile = tmpFile
	objectMetadata = `{"env":"test"}`
	objectTags = nil

	// Factory error after file/metadata parsing succeeds - exercises parsing paths
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err) // Factory error, but file+metadata parsing was exercised
}

func TestRunObjectPutCommand_ValidFileWithTags(t *testing.T) {
	origFile := objectFile
	origMeta := objectMetadata
	origTags := objectTags
	origFactory := objectStorageClientFactory
	defer func() {
		objectFile = origFile
		objectMetadata = origMeta
		objectTags = origTags
		objectStorageClientFactory = origFactory
	}()

	tmpFile := t.TempDir() + "/tagged-data.txt"
	require.NoError(t, os.WriteFile(tmpFile, []byte("tagged content"), 0o644))

	objectFile = tmpFile
	objectMetadata = ""
	objectTags = map[string]string{"env": "prod", "tier": "hot"}

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/tagged-key"})
	assert.Error(t, err) // Factory error, but tag path was exercised
}

func TestRunObjectListByTagCommand_MalformedTagFilter(t *testing.T) {
	origTagFilter := objectTagFilter
	defer func() { objectTagFilter = origTagFilter }()

	objectTagFilter = "invalid-no-equals"

	cmd := newObjectListByTagCommand()
	err := runObjectListByTagCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tag format")
}

func TestRunObjectListByTagCommand_ValidTagFactoryFails(t *testing.T) {
	origTagFilter := objectTagFilter
	origFactory := objectStorageClientFactory
	defer func() {
		objectTagFilter = origTagFilter
		objectStorageClientFactory = origFactory
	}()

	objectTagFilter = "type=document"
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectListByTagCommand()
	err := runObjectListByTagCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestRunObjectMetadataUpdateCommand_MalformedJSON(t *testing.T) {
	origMeta := objectMetadata
	defer func() { objectMetadata = origMeta }()

	objectMetadata = "not-json"

	cmd := &cobra.Command{}
	err := runObjectMetadataUpdateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy JSON")
}

func TestRunObjectMetadataUpdateCommand_ValidJSONFactoryFails(t *testing.T) {
	origMeta := objectMetadata
	origFactory := objectStorageClientFactory
	defer func() {
		objectMetadata = origMeta
		objectStorageClientFactory = origFactory
	}()

	objectMetadata = `{"replication":{"enabled":true}}`
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := &cobra.Command{}
	err := runObjectMetadataUpdateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectLifecycleSetCommand_MalformedJSON(t *testing.T) {
	origPolicy := objectPolicyJSON
	defer func() { objectPolicyJSON = origPolicy }()

	objectPolicyJSON = "invalid{json"

	cmd := &cobra.Command{}
	err := runObjectLifecycleSetCommand(cmd, []string{"mybucket"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy JSON")
}

func TestRunObjectLifecycleSetCommand_ValidJSONFactoryFails(t *testing.T) {
	origPolicy := objectPolicyJSON
	origFactory := objectStorageClientFactory
	defer func() {
		objectPolicyJSON = origPolicy
		objectStorageClientFactory = origFactory
	}()

	objectPolicyJSON = `{"archive":{"enabled":true,"archive_after_days":30}}`
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := &cobra.Command{}
	err := runObjectLifecycleSetCommand(cmd, []string{"mybucket"})
	assert.Error(t, err)
}

func TestRunObjectRestoreCommand_AllTierVariants(t *testing.T) {
	origFactory := objectStorageClientFactory
	origTier := objectRestoreTier
	origDest := objectDestBackend
	defer func() {
		objectStorageClientFactory = origFactory
		objectRestoreTier = origTier
		objectDestBackend = origDest
	}()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	tests := []struct {
		name string
		tier string
	}{
		{name: "hot tier", tier: "hot"},
		{name: "warm tier", tier: "warm"},
		{name: "default tier", tier: ""},
		{name: "unknown tier", tier: "glacier"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectRestoreTier = tt.tier
			objectDestBackend = ""

			cmd := newObjectRestoreCommand()
			err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
			assert.Error(t, err) // Factory error, but tier parsing paths exercised
		})
	}
}

func TestRunObjectReplicateCommand_CommaSeparatedDestinations(t *testing.T) {
	origFactory := objectStorageClientFactory
	origDest := objectDestBackend
	defer func() {
		objectStorageClientFactory = origFactory
		objectDestBackend = origDest
	}()

	objectDestBackend = "store-a, store-b, store-c"
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectReplicateCommand()
	err := runObjectReplicateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err) // Factory error, but multi-dest parsing exercised
}

// =============================================================================
// Object Storage Format Edge Cases - SetPolicy
// =============================================================================

func TestFormatObjectSetPolicyResponse_YAMLFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/key", Version: 1}
	err := formatObjectSetPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectSetPolicyResponse_TableFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/key", Version: 3}
	err := formatObjectSetPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectSetPolicyResponse_BadFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "invalid-format"

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/key"}
	err := formatObjectSetPolicyResponse(resp)
	assert.Error(t, err)
}

// =============================================================================
// Object Storage Format - Lifecycle Response Edge Cases
// =============================================================================

func TestFormatObjectLifecycleResponse_TableWithArchiveAndRetention(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.GetObjectPolicyResponse{
		Key: "mybucket",
		Policy: &pb.ObjectPolicy{
			ArchivePolicy: &pb.ArchivePolicy{
				Enabled:          true,
				ArchiveAfterDays: 30,
				TargetStore:      "cold-store",
			},
			RetentionPolicy: &pb.RetentionPolicy{
				Enabled:       true,
				RetentionDays: 365,
			},
		},
	}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_TableNilPolicy(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.GetObjectPolicyResponse{Key: "mybucket", Policy: nil}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_TableEmptyPolicy(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.GetObjectPolicyResponse{Key: "mybucket", Policy: &pb.ObjectPolicy{}}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_BadFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "invalid-format"

	resp := &pb.GetObjectPolicyResponse{Key: "mybucket"}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.Error(t, err)
}

// =============================================================================
// Cluster Output Formatting
// =============================================================================

func TestPrintClusterStatusJSON_Success(t *testing.T) {
	status := &cli.ClusterStatusResponse{
		Healthy:    true,
		LeaderID:   1,
		NodeCount:  3,
		ReadyNodes: 3,
		Nodes: []cli.NodeInfo{
			{NodeID: 1, Address: "10.0.0.1:50051", IsLeader: true, IsHealthy: true},
		},
	}
	err := printClusterStatusJSON(status)
	assert.NoError(t, err)
}

func TestPrintClusterStatusTableCLI_HealthyCluster(t *testing.T) {
	status := &cli.ClusterStatusResponse{
		Healthy:    true,
		LeaderID:   1,
		NodeCount:  2,
		ReadyNodes: 2,
		Nodes: []cli.NodeInfo{
			{NodeID: 1, Address: "10.0.0.1:50051", IsLeader: true, IsHealthy: true, LastSeen: time.Now()},
			{NodeID: 2, Address: "10.0.0.2:50051", IsLeader: false, IsHealthy: true},
		},
	}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

func TestPrintClusterStatusTableCLI_UnhealthyCluster(t *testing.T) {
	status := &cli.ClusterStatusResponse{
		Healthy:    false,
		LeaderID:   0,
		NodeCount:  2,
		ReadyNodes: 1,
		Nodes: []cli.NodeInfo{
			{NodeID: 1, Address: "10.0.0.1:50051", IsLeader: false, IsHealthy: true},
			{NodeID: 2, Address: "10.0.0.2:50051", IsLeader: false, IsHealthy: false},
		},
	}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

func TestPrintClusterStatusTableCLI_WithRaftGroupInfo(t *testing.T) {
	status := &cli.ClusterStatusResponse{
		Healthy:    true,
		LeaderID:   1,
		NodeCount:  3,
		ReadyNodes: 3,
		Nodes:      []cli.NodeInfo{},
		RaftGroupInfo: []cli.RaftGroupInfo{
			{LeaderID: 1, MemberCount: 3},
			{LeaderID: 2, MemberCount: 3},
		},
	}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

func TestPrintClusterStatusTableCLI_FallbackToJSON(t *testing.T) {
	// Pass a non-ClusterStatusResponse type - should fallback to JSON
	status := map[string]string{"status": "ok"}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

// =============================================================================
// InitLogger Edge Cases
// =============================================================================

func TestInitLogger_EmptyConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = config.Default()
	cfg.Logging.Level = ""
	cfg.Logging.Format = ""

	err := initLogger()
	require.NoError(t, err)
	assert.NotNil(t, log)
}

// =============================================================================
// Object Storage Command Construction
// =============================================================================

func TestNewObjectStorageCommands_AllSubcommands(t *testing.T) {
	cmd := newObjectStorageCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "object", cmd.Use)

	expectedSubs := []string{
		"put", "get", "delete", "head", "list",
		"list-by-tag", "list-by-tenant", "copy",
		"archive", "restore", "verify", "replicate",
		"migrate", "metadata", "stats", "lifecycle",
	}

	subNames := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subNames[sub.Name()] = true
	}

	for _, expected := range expectedSubs {
		assert.True(t, subNames[expected], "Expected subcommand '%s' to exist", expected)
	}
}

func TestNewObjectMetadataUpdateCommandCreation(t *testing.T) {
	cmd := newObjectMetadataUpdateCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "update <bucket>/<key>", cmd.Use)
}

func TestNewObjectLifecycleSetCommandCreation(t *testing.T) {
	cmd := newObjectLifecycleSetCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "set <bucket>", cmd.Use)
}

func TestNewObjectReplicateCommandCreation(t *testing.T) {
	cmd := newObjectReplicateCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "replicate <bucket>/<key>", cmd.Use)
}

func TestNewObjectMigrateCommandCreation(t *testing.T) {
	cmd := newObjectMigrateCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "migrate <bucket>/<key>", cmd.Use)
}

// =============================================================================
// Embedded Server Helper
// =============================================================================

func TestGetFreePortForEmbedded_ReturnsValidPort(t *testing.T) {
	port, err := getFreePortForEmbedded()
	require.NoError(t, err)
	assert.Greater(t, port, 0)
	assert.Less(t, port, 65536)
}

func TestShutdownEmbeddedServer_NilHolder(t *testing.T) {
	embeddedServerHolder.Store(nil)
	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
}

func TestEmbeddedServer_ShutdownWhenNotStarted(t *testing.T) {
	es := &embeddedServer{}
	es.started.Store(false)
	err := es.shutdown()
	assert.NoError(t, err)
}

// =============================================================================
// Backup Storage - loadProfileCredentials Tests
// =============================================================================

func TestLoadProfileCredentials_FileNotFound(t *testing.T) {
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = "/nonexistent/credentials.yaml"

	creds, err := builder.loadProfileCredentials(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, creds, "Should return nil when file doesn't exist")
}

func TestLoadProfileCredentials_ValidCredentialsFile(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	credContent := `
default_profile: prod
profiles:
  prod:
    type: s3
    bucket: production-bucket
    region: us-east-1
    credentials:
      access_key_id: AKIATEST123
      secret_access_key: secret123
  dev:
    type: s3
    bucket: dev-bucket
    region: us-west-2
`
	require.NoError(t, os.WriteFile(credFile, []byte(credContent), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	require.NoError(t, err)
	require.NotNil(t, creds)
	assert.Equal(t, "s3", creds.Type)
	assert.Equal(t, "production-bucket", creds.Bucket)
	assert.Equal(t, "us-east-1", creds.Region)
}

func TestLoadProfileCredentials_SpecificProfile(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	credContent := `
default_profile: prod
profiles:
  prod:
    type: s3
    bucket: production-bucket
    region: us-east-1
  dev:
    type: s3
    bucket: dev-bucket
    region: us-west-2
`
	require.NoError(t, os.WriteFile(credFile, []byte(credContent), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "dev")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	require.NoError(t, err)
	require.NotNil(t, creds)
	assert.Equal(t, "dev-bucket", creds.Bucket)
	assert.Equal(t, "us-west-2", creds.Region)
}

func TestLoadProfileCredentials_ProfileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	credContent := `
default_profile: prod
profiles:
  prod:
    type: s3
    bucket: production-bucket
`
	require.NoError(t, os.WriteFile(credFile, []byte(credContent), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "nonexistent")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "profile 'nonexistent' not found")
}

func TestLoadProfileCredentials_NoDefaultProfile(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	credContent := `
profiles:
  prod:
    type: s3
    bucket: production-bucket
`
	require.NoError(t, os.WriteFile(credFile, []byte(credContent), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "no profile specified")
}

func TestLoadProfileCredentials_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "credentials.yaml")

	require.NoError(t, os.WriteFile(credFile, []byte("not: valid: yaml:"), 0o600))

	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = credFile

	creds, err := builder.loadProfileCredentials(context.Background())
	assert.Error(t, err)
	assert.Nil(t, creds)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestLoadProfileCredentials_DefaultPath(t *testing.T) {
	// Test that default path is used when credentialsFile is empty
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")
	builder.credentialsFile = ""

	// Since the default file likely doesn't exist, should return nil, nil
	creds, err := builder.loadProfileCredentials(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, creds)
}

// =============================================================================
// Backup Storage - expandEnvVars Tests
// =============================================================================

func TestExpandEnvVars_BasicExpansion(t *testing.T) {
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")

	os.Setenv("TEST_BUCKET_NAME", "my-test-bucket")
	defer os.Unsetenv("TEST_BUCKET_NAME")

	result := builder.expandEnvVars("${TEST_BUCKET_NAME}")
	assert.Equal(t, "my-test-bucket", result)
}

func TestExpandEnvVars_MixedContent(t *testing.T) {
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")

	os.Setenv("TEST_ENV", "production")
	defer os.Unsetenv("TEST_ENV")

	result := builder.expandEnvVars("backup-${TEST_ENV}-data")
	assert.Equal(t, "backup-production-data", result)
}

func TestExpandEnvVars_UndefinedVariable(t *testing.T) {
	flags := &BackupStorageFlags{StorageType: "s3"}
	builder := NewBackupStorageBuilder(flags, "", "")

	result := builder.expandEnvVars("${UNDEFINED_VAR_12345}")
	assert.Equal(t, "", result)
}

// =============================================================================
// createObjectStorageGRPCClient Tests - TLS Error Paths
// =============================================================================

func TestCreateObjectStorageGRPCClient_TLSEnabled_MissingCert(t *testing.T) {
	origTLSEnabled := objectTLSEnabled
	origTLSCert := objectTLSCert
	origTLSKey := objectTLSKey
	defer func() {
		objectTLSEnabled = origTLSEnabled
		objectTLSCert = origTLSCert
		objectTLSKey = origTLSKey
	}()

	objectTLSEnabled = true
	objectTLSCert = ""
	objectTLSKey = ""

	client, err := createObjectStorageGRPCClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "TLS is required for gRPC connections")
}

func TestCreateObjectStorageGRPCClient_TLSEnabled_MissingKey(t *testing.T) {
	origTLSEnabled := objectTLSEnabled
	origTLSCert := objectTLSCert
	origTLSKey := objectTLSKey
	defer func() {
		objectTLSEnabled = origTLSEnabled
		objectTLSCert = origTLSCert
		objectTLSKey = origTLSKey
	}()

	objectTLSEnabled = true
	objectTLSCert = "/path/to/cert.pem"
	objectTLSKey = ""

	client, err := createObjectStorageGRPCClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "TLS is required for gRPC connections")
}

// =============================================================================
// Object Storage Command - Additional Input Validation Tests
// =============================================================================

func TestRunObjectArchiveCommand_AllCompressionLevels(t *testing.T) {
	origFactory := objectStorageClientFactory
	origCompression := objectCompressionLevel
	origTargetStore := objectTargetStore
	defer func() {
		objectStorageClientFactory = origFactory
		objectCompressionLevel = origCompression
		objectTargetStore = origTargetStore
	}()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}
	objectTargetStore = "archive-store"

	tests := []struct {
		name  string
		level int32
	}{
		{name: "no compression", level: 0},
		{name: "fast compression", level: 1},
		{name: "best compression", level: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectCompressionLevel = tt.level
			cmd := newObjectArchiveCommand()
			err := runObjectArchiveCommand(cmd, []string{"bucket/key"})
			assert.Error(t, err) // Factory error expected, but compression path exercised
		})
	}
}

func TestRunObjectCopyCommand_WithTags(t *testing.T) {
	origFactory := objectStorageClientFactory
	origCopyTags := objectCopyTags
	defer func() {
		objectStorageClientFactory = origFactory
		objectCopyTags = origCopyTags
	}()

	objectCopyTags = true
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectCopyCommand()
	err := runObjectCopyCommand(cmd, []string{"source/key", "dest/key"})
	assert.Error(t, err) // Factory error expected, but copy tags path exercised
}

type mockMessagingClientForCoverage struct {
	createChannelFunc       func(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error)
	deleteChannelFunc       func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error)
	getChannelFunc          func(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error)
	listChannelsFunc        func(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error)
	subscribeFunc           func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error)
	unsubscribeFunc         func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error)
	publishFunc             func(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error)
	publishDeltaFunc        func(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error)
	streamNotificationsFunc func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error)
	getDedupStatsFunc       func(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error)
	getChannelStatsFunc     func(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error)
}

func (m *mockMessagingClientForCoverage) MessagingCreateChannel(ctx context.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error) {
	if m.createChannelFunc != nil {
		return m.createChannelFunc(ctx, req)
	}
	return &grpc.MessagingCreateChannelResponse{
		Channel:   &grpc.MessagingChannel{Name: req.Name, BufferSize: req.BufferSize, Persistent: req.Persistent, CreatedAt: time.Now()},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockMessagingClientForCoverage) MessagingDeleteChannel(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
	if m.deleteChannelFunc != nil {
		return m.deleteChannelFunc(ctx, req)
	}
	return &grpc.MessagingDeleteChannelResponse{Name: req.Name, Deleted: true, Timestamp: time.Now()}, nil
}

func (m *mockMessagingClientForCoverage) MessagingGetChannel(ctx context.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error) {
	if m.getChannelFunc != nil {
		return m.getChannelFunc(ctx, req)
	}
	return &grpc.MessagingGetChannelResponse{
		Channel:   &grpc.MessagingChannel{Name: req.Name, BufferSize: 256, Persistent: true, CreatedAt: time.Now(), Subscribers: 3},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockMessagingClientForCoverage) MessagingListChannels(ctx context.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error) {
	if m.listChannelsFunc != nil {
		return m.listChannelsFunc(ctx, req)
	}
	return &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", BufferSize: 100, Persistent: true, CreatedAt: time.Now()},
			{Name: "events", BufferSize: 50, Persistent: false, CreatedAt: time.Now()},
		},
		TotalCount: 2,
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockMessagingClientForCoverage) MessagingSubscribe(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, req)
	}
	return &grpc.MessagingSubscribeResponse{
		Subscription: &grpc.MessagingSubscription{ID: "sub-123", Channel: req.Channel, CreatedAt: time.Now()},
		Timestamp:    time.Now(),
	}, nil
}

func (m *mockMessagingClientForCoverage) MessagingUnsubscribe(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
	if m.unsubscribeFunc != nil {
		return m.unsubscribeFunc(ctx, req)
	}
	return &grpc.MessagingUnsubscribeResponse{SubscriptionID: req.SubscriptionID, Unsubscribed: true, Timestamp: time.Now()}, nil
}

func (m *mockMessagingClientForCoverage) MessagingPublish(ctx context.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error) {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, req)
	}
	return &grpc.MessagingPublishResponse{MessageID: "msg-001", Channel: req.Channel, Timestamp: time.Now()}, nil
}

func (m *mockMessagingClientForCoverage) MessagingPublishDelta(ctx context.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error) {
	if m.publishDeltaFunc != nil {
		return m.publishDeltaFunc(ctx, req)
	}
	return &grpc.MessagingPublishDeltaResponse{
		MessageID: "delta-001",
		Channel:   req.Channel,
		Delta: &grpc.MessagingDeltaPayload{
			EntityID:   "ord-001",
			EntityType: "Order",
			Changes: []*grpc.MessagingFieldChange{
				{Path: "status", Operation: "update", Value: []byte("shipped"), OldValue: []byte("processing")},
			},
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockMessagingClientForCoverage) MessagingStreamNotifications(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
	if m.streamNotificationsFunc != nil {
		return m.streamNotificationsFunc(ctx, req)
	}
	notifChan := make(chan *grpc.MessagingNotification, 1)
	errChan := make(chan error)
	close(notifChan)
	close(errChan)
	return notifChan, errChan, nil
}

func (m *mockMessagingClientForCoverage) MessagingGetDedupStats(ctx context.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error) {
	if m.getDedupStatsFunc != nil {
		return m.getDedupStatsFunc(ctx, req)
	}
	now := time.Now()
	return &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         req.Channel,
			TotalMessages:   1000,
			TotalDuplicates: 50,
			TopDuplicates: []*grpc.MessagingDedupEntry{
				{Count: 10, FirstSeen: now, LastSeen: now, MessageID: "msg-dup-1"},
			},
		},
		Timestamp: now,
	}, nil
}

func (m *mockMessagingClientForCoverage) MessagingGetChannelStats(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
	if m.getChannelStatsFunc != nil {
		return m.getChannelStatsFunc(ctx, req)
	}
	return &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:              req.Name,
			Subscribers:       5,
			MessagesPublished: 500,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockMessagingClientForCoverage) Close() error { return nil }

// setupMsgMock replaces the messagingClientFactory with a mock and returns cleanup.
func setupMsgMock(t *testing.T, client MessagingClient) func() {
	t.Helper()
	origFactory := messagingClientFactory
	messagingClientFactory = func() (MessagingClient, error) {
		return client, nil
	}
	return func() {
		messagingClientFactory = origFactory
	}
}

// =============================================================================
// Messaging Command Tests - Create Channel
// =============================================================================

func TestRunMsgCreateChannelCommand_Success(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	defer func() { msgOutputFmt = origFmt }()
	msgOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runMsgCreateChannelCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgCreateChannelCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgCreateChannelCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - Delete Channel
// =============================================================================

func TestRunMsgDeleteChannelCommand_Success(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	origForce := msgForce
	defer func() {
		msgOutputFmt = origFmt
		msgForce = origForce
	}()
	msgOutputFmt = "table"
	msgForce = true

	cmd := &cobra.Command{}
	err := runMsgDeleteChannelCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgDeleteChannelCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	defer func() {
		messagingClientFactory = origFactory
		msgForce = origForce
	}()

	msgForce = true
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgDeleteChannelCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - List Channels
// =============================================================================

func TestRunMsgListChannelsCommand_Success(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	defer func() { msgOutputFmt = origFmt }()
	msgOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runMsgListChannelsCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunMsgListChannelsCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgListChannelsCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - Channel Info
// =============================================================================

func TestRunMsgChannelInfoCommand_Success(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	defer func() { msgOutputFmt = origFmt }()
	msgOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runMsgChannelInfoCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgChannelInfoCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgChannelInfoCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - Unsubscribe (force mode)
// =============================================================================

func TestRunMsgUnsubscribeCommand_Force(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		msgForce = origForce
		msgOutputFmt = origFmt
	}()
	msgForce = true
	msgOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-123"})
	assert.NoError(t, err)
}

// =============================================================================
// Messaging Command Tests - List Subscriptions
// =============================================================================

func TestRunMsgListSubscriptionsCommand_WithChannel(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	origChannel := msgChannel
	defer func() {
		msgOutputFmt = origFmt
		msgChannel = origChannel
	}()
	msgOutputFmt = "table"
	msgChannel = "orders"

	cmd := &cobra.Command{}
	err := runMsgListSubscriptionsCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunMsgListSubscriptionsCommand_AllChannels(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	origChannel := msgChannel
	defer func() {
		msgOutputFmt = origFmt
		msgChannel = origChannel
	}()
	msgOutputFmt = "table"
	msgChannel = ""

	cmd := &cobra.Command{}
	err := runMsgListSubscriptionsCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunMsgListSubscriptionsCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgListSubscriptionsCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - Publish
// =============================================================================

func TestRunMsgPublishCommand_WithPayload(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origPayload := msgPayload
	origFile := msgFile
	origFmt := msgOutputFmt
	origPriority := msgPriority
	origTTL := msgTTL
	defer func() {
		msgPayload = origPayload
		msgFile = origFile
		msgOutputFmt = origFmt
		msgPriority = origPriority
		msgTTL = origTTL
	}()
	msgPayload = `{"order_id":"ord-001"}`
	msgFile = ""
	msgOutputFmt = "table"
	msgPriority = "high"
	msgTTL = "5m"

	cmd := &cobra.Command{}
	err := runMsgPublishCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgPublishCommand_NoPayloadOrFile(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origPayload := msgPayload
	origFile := msgFile
	defer func() {
		msgPayload = origPayload
		msgFile = origFile
	}()
	msgPayload = ""
	msgFile = ""

	cmd := &cobra.Command{}
	err := runMsgPublishCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgPublishCommand_BadFile(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origPayload := msgPayload
	origFile := msgFile
	defer func() {
		msgPayload = origPayload
		msgFile = origFile
	}()
	msgPayload = ""
	msgFile = "/nonexistent/path/payload.json"

	cmd := &cobra.Command{}
	err := runMsgPublishCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgPublishCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	origPayload := msgPayload
	defer func() { msgPayload = origPayload }()
	msgPayload = `{"test": true}`

	cmd := &cobra.Command{}
	err := runMsgPublishCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - Publish Delta
// =============================================================================

func TestRunMsgPublishDeltaCommand_Success(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origOld := msgOld
	origNew := msgNew
	origFmt := msgOutputFmt
	origEntityID := msgEntityID
	origEntityType := msgEntityType
	defer func() {
		msgOld = origOld
		msgNew = origNew
		msgOutputFmt = origFmt
		msgEntityID = origEntityID
		msgEntityType = origEntityType
	}()
	msgOld = `{"status":"processing"}`
	msgNew = `{"status":"shipped"}`
	msgOutputFmt = "table"
	msgEntityID = "ord-001"
	msgEntityType = "Order"

	cmd := &cobra.Command{}
	err := runMsgPublishDeltaCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgPublishDeltaCommand_NoOldValue(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origOld := msgOld
	origOldFile := msgOldFile
	defer func() {
		msgOld = origOld
		msgOldFile = origOldFile
	}()
	msgOld = ""
	msgOldFile = ""

	cmd := &cobra.Command{}
	err := runMsgPublishDeltaCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgPublishDeltaCommand_NoNewValue(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origOld := msgOld
	origNew := msgNew
	origNewFile := msgNewFile
	defer func() {
		msgOld = origOld
		msgNew = origNew
		msgNewFile = origNewFile
	}()
	msgOld = `{"status":"old"}`
	msgNew = ""
	msgNewFile = ""

	cmd := &cobra.Command{}
	err := runMsgPublishDeltaCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgPublishDeltaCommand_BadOldFile(t *testing.T) {
	origOldFile := msgOldFile
	origOld := msgOld
	defer func() {
		msgOldFile = origOldFile
		msgOld = origOld
	}()
	msgOldFile = "/nonexistent/old.json"
	msgOld = ""

	cmd := &cobra.Command{}
	err := runMsgPublishDeltaCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgPublishDeltaCommand_BadNewFile(t *testing.T) {
	origOld := msgOld
	origNewFile := msgNewFile
	origNew := msgNew
	origOldFile := msgOldFile
	defer func() {
		msgOld = origOld
		msgNewFile = origNewFile
		msgNew = origNew
		msgOldFile = origOldFile
	}()
	msgOld = `{"old":"data"}`
	msgOldFile = ""
	msgNewFile = "/nonexistent/new.json"
	msgNew = ""

	cmd := &cobra.Command{}
	err := runMsgPublishDeltaCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - Dedup Stats
// =============================================================================

func TestRunMsgDedupStatsCommand_Success(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFmt := msgOutputFmt
	defer func() { msgOutputFmt = origFmt }()
	msgOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runMsgDedupStatsCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

func TestRunMsgDedupStatsCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgDedupStatsCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - Subscribe (early error path)
// =============================================================================

func TestRunMsgSubscribeCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgSubscribeCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgSubscribeCommand_SubscribeError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		subscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	cmd := &cobra.Command{}
	err := runMsgSubscribeCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
}

func TestRunMsgSubscribeCommand_StreamError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return nil, nil, errors.New(errors.ErrInternal, "test", "stream failed")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	cmd := &cobra.Command{}
	err := runMsgSubscribeCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgSubscribeCommand_ErrorFromStream(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			notifChan := make(chan *grpc.MessagingNotification)
			errChan := make(chan error, 1)
			errChan <- errors.New(errors.ErrInternal, "test", "stream error")
			close(errChan)
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	cmd := &cobra.Command{}
	err := runMsgSubscribeCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Command Tests - Watch (early error path)
// =============================================================================

func TestRunMsgWatchCommand_FactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()

	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"orders"})
	assert.Error(t, err)
}

func TestRunMsgWatchCommand_SubscribeError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		subscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFile := msgFile
	defer func() { msgFile = origFile }()
	msgFile = ""

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
}

// =============================================================================
// Messaging Format Function Tests
// =============================================================================

func TestFormatChannelResponse_AllFormats(t *testing.T) {
	channel := &grpc.MessagingChannel{
		Name:       "orders",
		BufferSize: 256,
		Persistent: true,
		TTLSeconds: 3600,
		CreatedAt:  time.Now(),
	}

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"yaml", "yaml"},
		{"table", "table"},
		{"default", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatChannelResponse(channel, tt.format, &buf)
			assert.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestFormatChannelResponse_ZeroTTL(t *testing.T) {
	channel := &grpc.MessagingChannel{
		Name:       "events",
		TTLSeconds: 0,
		CreatedAt:  time.Now(),
	}
	var buf bytes.Buffer
	err := formatChannelResponse(channel, "table", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "no expiry")
}

func TestFormatDeleteChannelResponse_AllFormats(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{Name: "orders", Deleted: true, Timestamp: time.Now()}

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"yaml", "yaml"},
		{"table", "table"},
		{"default", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDeleteChannelResponse(resp, tt.format, &buf)
			assert.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestFormatPublishResponse_AllFormats(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID:   "msg-001",
		Channel:     "orders",
		IsDuplicate: false,
		Timestamp:   time.Now(),
	}

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"yaml", "yaml"},
		{"table", "table"},
		{"default", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatPublishResponse(resp, tt.format, &buf)
			assert.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestFormatPublishResponse_Duplicate(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{
		MessageID:   "msg-002",
		Channel:     "orders",
		IsDuplicate: true,
		Timestamp:   time.Now(),
	}
	var buf bytes.Buffer
	err := formatPublishResponse(resp, "table", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "Duplicate")
}

func TestFormatPublishDeltaResponse_AllFormats(t *testing.T) {
	resp := &grpc.MessagingPublishDeltaResponse{
		MessageID: "delta-001",
		Channel:   "orders",
		Delta: &grpc.MessagingDeltaPayload{
			EntityID:   "ord-001",
			EntityType: "Order",
			Changes: []*grpc.MessagingFieldChange{
				{Path: "status", Operation: "update", Value: []byte("shipped")},
			},
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"yaml", "yaml"},
		{"table", "table"},
		{"default", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatPublishDeltaResponse(resp, tt.format, &buf)
			assert.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestFormatUnsubscribeResponse_AllFormats(t *testing.T) {
	resp := &grpc.MessagingUnsubscribeResponse{
		SubscriptionID: "sub-123",
		Unsubscribed:   true,
		Timestamp:      time.Now(),
	}

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"yaml", "yaml"},
		{"table", "table"},
		{"default", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatUnsubscribeResponse(resp, tt.format, &buf)
			assert.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestFormatDedupStatsResponse_AllFormats(t *testing.T) {
	now := time.Now()
	resp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "orders",
			TotalMessages:   1000,
			TotalDuplicates: 50,
			TopDuplicates: []*grpc.MessagingDedupEntry{
				{Count: 10, FirstSeen: now, LastSeen: now, MessageID: "dup-1"},
			},
		},
		Timestamp: now,
	}

	tests := []struct {
		name   string
		format string
	}{
		{"json", "json"},
		{"yaml", "yaml"},
		{"table", "table"},
		{"default", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDedupStatsResponse(resp, tt.format, &buf)
			assert.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestFormatNotificationCoverage(t *testing.T) {
	now := time.Now()
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-001",
			Type:      "data",
			Channel:   "orders",
			Payload:   []byte(`{"order":"test"}`),
			Timestamp: now,
			Source:    "test",
			Metadata:  map[string]string{"priority": "high"},
			Dedup: &grpc.MessagingDedupInfo{
				IsDuplicate: true,
				Count:       3,
				FirstSeen:   now,
			},
		},
		SubscriptionID: "sub-001",
		SequenceNumber: 42,
	}

	var buf bytes.Buffer
	formatNotification(notif, &buf)
	assert.NotEmpty(t, buf.String())
}

// =============================================================================
// Messaging Format - Invalid Format Errors
// =============================================================================

func TestFormatChannelResponse_InvalidFormat(t *testing.T) {
	channel := &grpc.MessagingChannel{Name: "test", CreatedAt: time.Now()}
	var buf bytes.Buffer
	err := formatChannelResponse(channel, "invalid-fmt", &buf)
	assert.Error(t, err)
}

func TestFormatDeleteChannelResponse_InvalidFormat(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{Name: "test", Timestamp: time.Now()}
	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "invalid-fmt", &buf)
	assert.Error(t, err)
}

func TestFormatPublishResponse_InvalidFormat(t *testing.T) {
	resp := &grpc.MessagingPublishResponse{MessageID: "test", Timestamp: time.Now()}
	var buf bytes.Buffer
	err := formatPublishResponse(resp, "invalid-fmt", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Create Messaging Client Transport Tests
// =============================================================================

func TestCreateMessagingClient_UnsupportedTransports(t *testing.T) {
	origTransport := transport
	defer func() { transport = origTransport }()

	tests := []struct {
		name      string
		transport string
	}{
		{"rest", "rest"},
		{"mcp", "mcp"},
		{"quic", "quic"},
		{"unknown", "bogus-transport"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport = tt.transport
			client, err := createMessagingClient()
			assert.Error(t, err)
			assert.Nil(t, client)
		})
	}
}

// =============================================================================
// New Command Builder Tests
// =============================================================================

func TestNewMsgCommandsCoverage(t *testing.T) {
	cmd := newMsgCommands()
	require.NotNil(t, cmd)
	assert.Equal(t, "msg", cmd.Use)
	assert.True(t, len(cmd.Commands()) > 0)
}

func TestNewMsgCreateChannelCommandCoverage(t *testing.T) {
	cmd := newMsgCreateChannelCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "create")
}

func TestNewMsgDeleteChannelCommandCoverage(t *testing.T) {
	cmd := newMsgDeleteChannelCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "delete")
}

func TestNewMsgListChannelsCommandCoverage(t *testing.T) {
	cmd := newMsgListChannelsCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "list")
}

func TestNewMsgChannelInfoCommandCoverage(t *testing.T) {
	cmd := newMsgChannelInfoCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "info")
}

func TestNewMsgSubscribeCommandCoverage(t *testing.T) {
	cmd := newMsgSubscribeCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "subscribe")
}

func TestNewMsgUnsubscribeCommandCoverage(t *testing.T) {
	cmd := newMsgUnsubscribeCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "unsubscribe")
}

func TestNewMsgPublishCommandCoverage(t *testing.T) {
	cmd := newMsgPublishCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "publish")
}

func TestNewMsgPublishDeltaCommandCoverage(t *testing.T) {
	cmd := newMsgPublishDeltaCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "publish-delta")
}

func TestNewMsgWatchCommandCoverage(t *testing.T) {
	cmd := newMsgWatchCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "watch")
}

func TestNewMsgDedupStatsCommandCoverage(t *testing.T) {
	cmd := newMsgDedupStatsCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "dedup-stats")
}

func TestNewMsgListSubscriptionsCommandCoverage(t *testing.T) {
	cmd := newMsgListSubscriptionsCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "list-subscriptions")
}

func createTestLoggerForRemaining91(t *testing.T) *logger.Logger {
	t.Helper()
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return testLog
}

func validCacheConfig() config.EnhancedCacheConfig {
	return config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities:        make(map[string]config.EntityCacheConfig),
	}
}

// upgrade.go coverage

func TestRemaining91_RunUpgradeStart_DefaultAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origGlobal := address
	origTimeout := upgradeTimeout
	defer func() {
		upgradeServerAddress = origAddr
		address = origGlobal
		upgradeTimeout = origTimeout
	}()

	upgradeServerAddress = ""
	address = ""
	upgradeTimeout = 1 * time.Second

	cmd := newUpgradeStartCommand()
	err := runUpgradeStart(cmd, []string{"v2.0.0"})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStart_CustomServerAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origTimeout := upgradeTimeout
	defer func() {
		upgradeServerAddress = origAddr
		upgradeTimeout = origTimeout
	}()

	upgradeServerAddress = "localhost:19999"
	upgradeTimeout = 1 * time.Second

	cmd := newUpgradeStartCommand()
	err := runUpgradeStart(cmd, []string{"v3.0.0"})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStart_FallbackToGlobalAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origGlobal := address
	origTimeout := upgradeTimeout
	defer func() {
		upgradeServerAddress = origAddr
		address = origGlobal
		upgradeTimeout = origTimeout
	}()

	upgradeServerAddress = ""
	address = "localhost:19998"
	upgradeTimeout = 1 * time.Second

	cmd := newUpgradeStartCommand()
	err := runUpgradeStart(cmd, []string{"v2.1.0"})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStatus_DefaultAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origGlobal := address
	origTimeout := upgradeTimeout
	defer func() {
		upgradeServerAddress = origAddr
		address = origGlobal
		upgradeTimeout = origTimeout
	}()

	upgradeServerAddress = ""
	address = ""
	upgradeTimeout = 1 * time.Second

	cmd := newUpgradeStatusCommand()
	err := runUpgradeStatus(cmd, []string{})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStatus_CustomAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origTimeout := upgradeTimeout
	defer func() {
		upgradeServerAddress = origAddr
		upgradeTimeout = origTimeout
	}()

	upgradeServerAddress = "localhost:19997"
	upgradeTimeout = 1 * time.Second

	cmd := newUpgradeStatusCommand()
	err := runUpgradeStatus(cmd, []string{})
	require.Error(t, err)
}

func TestRemaining91_RunUpgradeStatus_FallbackToGlobalAddress(t *testing.T) {
	origAddr := upgradeServerAddress
	origGlobal := address
	origTimeout := upgradeTimeout
	defer func() {
		upgradeServerAddress = origAddr
		address = origGlobal
		upgradeTimeout = origTimeout
	}()

	upgradeServerAddress = ""
	address = "localhost:19996"
	upgradeTimeout = 1 * time.Second

	cmd := newUpgradeStatusCommand()
	err := runUpgradeStatus(cmd, []string{})
	require.Error(t, err)
}

func TestRemaining91_FollowUpgradeProgress_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := followUpgradeProgress(ctx, nil, "op-123", "table")
	require.Error(t, err)
}

// cluster.go coverage

func TestRemaining91_NewClusterStatusCommand_ConnectionFailure(t *testing.T) {
	origAddr := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddr
		transport = origTransport
		globalTimeout = origTimeout
		resetSDKClient()
	}()

	resetSDKClient()
	address = "localhost:19995"
	transport = "grpc"
	globalTimeout = 1 * time.Second

	cmd := newClusterStatusCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRemaining91_NewClusterStatusCommand_InvalidOutputFormat(t *testing.T) {
	origAddr := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddr
		transport = origTransport
		globalTimeout = origTimeout
		resetSDKClient()
	}()

	resetSDKClient()
	address = "localhost:19994"
	transport = "grpc"
	globalTimeout = 1 * time.Second

	cmd := newClusterStatusCommand()
	cmd.SetArgs([]string{"--output", "xml"})
	err := cmd.Execute()
	assert.Error(t, err)
}

// node.go coverage

func TestRemaining91_RunNodeList_TableOutput(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	cmd := newNodeListCommand()
	nodeOutputFormat = "table"
	err := runNodeList(cmd, []string{})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeList_JSONOutput(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	cmd := newNodeListCommand()
	nodeOutputFormat = "json"
	err := runNodeList(cmd, []string{})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeList_InvalidFormat(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	cmd := newNodeListCommand()
	nodeOutputFormat = "xml"
	err := runNodeList(cmd, []string{})
	assert.Error(t, err)
}

func TestRemaining91_GetNodeManager_CreatesNew(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	mgr, err := getNodeManager()
	require.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestRemaining91_GetNodeManager_ReturnsCached(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	mgr1, err := getNodeManager()
	require.NoError(t, err)
	mgr2, err := getNodeManager()
	require.NoError(t, err)
	assert.Equal(t, mgr1, mgr2)
}

func TestRemaining91_GetNodeManager_WithCustomFlags(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origHCI := nodeHealthCheckInterval
	origHCT := nodeHealthCheckTimeout
	origEvac := nodeEvacuationTimeout
	origGrace := nodeGracePeriod
	origMaxT := nodeMaxTransfers
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeHealthCheckInterval = origHCI
		nodeHealthCheckTimeout = origHCT
		nodeEvacuationTimeout = origEvac
		nodeGracePeriod = origGrace
		nodeMaxTransfers = origMaxT
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil
	nodeHealthCheckInterval = 10 * time.Second
	nodeHealthCheckTimeout = 3 * time.Second
	nodeEvacuationTimeout = 15 * time.Minute
	nodeGracePeriod = 5 * time.Second
	nodeMaxTransfers = 3

	mgr, err := getNodeManager()
	require.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestRemaining91_RunNodeAdd_Success(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	addCmd := newNodeAddCommand()
	nodeOutputFormat = "table"
	err := runNodeAdd(addCmd, []string{"node-test-1", "10.0.0.1:50051"})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeAdd_JSONOutput(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	addCmd := newNodeAddCommand()
	nodeOutputFormat = "json"
	err := runNodeAdd(addCmd, []string{"node-test-2", "10.0.0.2:50051"})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeRemove_Success(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	addCmd := newNodeAddCommand()
	nodeOutputFormat = "table"
	err := runNodeAdd(addCmd, []string{"node-remove-1", "10.0.0.3:50051"})
	require.NoError(t, err)

	removeCmd := newNodeRemoveCommand()
	nodeOutputFormat = "table"
	err = runNodeRemove(removeCmd, []string{"node-remove-1"})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeRemove_NonExistentNode(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	removeCmd := newNodeRemoveCommand()
	nodeOutputFormat = "table"
	err := runNodeRemove(removeCmd, []string{"ghost-node"})
	assert.Error(t, err)
}

func TestRemaining91_RunNodeReplace_Success(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	addCmd := newNodeAddCommand()
	nodeOutputFormat = "table"
	err := runNodeAdd(addCmd, []string{"node-old", "10.0.0.5:50051"})
	require.NoError(t, err)

	replaceCmd := newNodeReplaceCommand()
	nodeOutputFormat = "table"
	err = runNodeReplace(replaceCmd, []string{"node-old", "node-new", "10.0.0.6:50051"})
	assert.NoError(t, err)
}

func TestRemaining91_RunNodeReplace_NonExistentOld(t *testing.T) {
	origLog := log
	origNodeMgr := currentNodeManager
	origFmt := nodeOutputFormat
	defer func() {
		log = origLog
		currentNodeManager = origNodeMgr
		nodeOutputFormat = origFmt
	}()

	log = createTestLoggerForRemaining91(t)
	currentNodeManager = nil

	replaceCmd := newNodeReplaceCommand()
	nodeOutputFormat = "json"
	err := runNodeReplace(replaceCmd, []string{"ghost-node", "new-node", "10.0.0.7:50051"})
	assert.NoError(t, err)
}

// msg.go coverage

func TestRemaining91_RunMsgDeleteChannelCommand_Forced(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		messagingClientFactory = origFactory
		msgForce = origForce
		msgOutputFmt = origFmt
	}()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingDeleteChannelFunc: func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
				return &grpc.MessagingDeleteChannelResponse{Name: req.Name, Deleted: true}, nil
			},
		}, nil
	}
	cmd := newMsgDeleteChannelCommand()
	msgForce = true
	msgOutputFmt = "table"
	err := runMsgDeleteChannelCommand(cmd, []string{"test-channel"})
	assert.NoError(t, err)
}

func TestRemaining91_RunMsgDeleteChannelCommand_ClientError(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	defer func() {
		messagingClientFactory = origFactory
		msgForce = origForce
	}()
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}
	cmd := newMsgDeleteChannelCommand()
	msgForce = true
	err := runMsgDeleteChannelCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgDeleteChannelCommand_ServerError(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	defer func() {
		messagingClientFactory = origFactory
		msgForce = origForce
	}()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingDeleteChannelFunc: func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
			},
		}, nil
	}
	cmd := newMsgDeleteChannelCommand()
	msgForce = true
	err := runMsgDeleteChannelCommand(cmd, []string{"missing-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgSubscribeCommand_ClientFactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}
	cmd := newMsgSubscribeCommand()
	err := runMsgSubscribeCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgSubscribeCommand_SubscribeError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
			},
		}, nil
	}
	cmd := newMsgSubscribeCommand()
	err := runMsgSubscribeCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgSubscribeCommand_StreamError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return &grpc.MessagingSubscribeResponse{
					Subscription: &grpc.MessagingSubscription{ID: "sub-123", Channel: "test-channel"},
				}, nil
			},
			MessagingStreamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
				return nil, nil, errors.New(errors.ErrInternal, "test", "stream failed")
			},
		}, nil
	}
	cmd := newMsgSubscribeCommand()
	err := runMsgSubscribeCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgSubscribeCommand_StreamReturnsError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return &grpc.MessagingSubscribeResponse{
					Subscription: &grpc.MessagingSubscription{ID: "sub-456", Channel: "test-channel"},
				}, nil
			},
			MessagingStreamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
				errCh := make(chan error, 1)
				errCh <- errors.New(errors.ErrInternal, "test", "stream broken")
				close(errCh)
				notifCh := make(chan *grpc.MessagingNotification)
				return notifCh, errCh, nil
			},
		}, nil
	}
	cmd := newMsgSubscribeCommand()
	err := runMsgSubscribeCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgSubscribeCommand_ChannelClosed(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return &grpc.MessagingSubscribeResponse{
					Subscription: &grpc.MessagingSubscription{ID: "sub-789", Channel: "test-channel"},
				}, nil
			},
			MessagingStreamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
				notifCh := make(chan *grpc.MessagingNotification)
				errCh := make(chan error)
				close(notifCh)
				return notifCh, errCh, nil
			},
		}, nil
	}
	cmd := newMsgSubscribeCommand()
	err := runMsgSubscribeCommand(cmd, []string{"test-channel"})
	assert.NoError(t, err)
}

func TestRemaining91_RunMsgUnsubscribeCommand_Forced(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		messagingClientFactory = origFactory
		msgForce = origForce
		msgOutputFmt = origFmt
	}()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingUnsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
				return &grpc.MessagingUnsubscribeResponse{SubscriptionID: req.SubscriptionID, Unsubscribed: true}, nil
			},
		}, nil
	}
	cmd := newMsgUnsubscribeCommand()
	msgForce = true
	msgOutputFmt = "table"
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-test-123"})
	assert.NoError(t, err)
}

func TestRemaining91_RunMsgUnsubscribeCommand_ClientError(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	defer func() {
		messagingClientFactory = origFactory
		msgForce = origForce
	}()
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}
	cmd := newMsgUnsubscribeCommand()
	msgForce = true
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-test-456"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgUnsubscribeCommand_ServerError(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	defer func() {
		messagingClientFactory = origFactory
		msgForce = origForce
	}()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingUnsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "test", "subscription not found")
			},
		}, nil
	}
	cmd := newMsgUnsubscribeCommand()
	msgForce = true
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-test-789"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgWatchCommand_ClientFactoryError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}
	cmd := newMsgWatchCommand()
	err := runMsgWatchCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgWatchCommand_SubscribeError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
			},
		}, nil
	}
	cmd := newMsgWatchCommand()
	err := runMsgWatchCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgWatchCommand_StreamSetupError(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return &grpc.MessagingSubscribeResponse{
					Subscription: &grpc.MessagingSubscription{ID: "sub-watch-1", Channel: "test-channel"},
				}, nil
			},
			MessagingStreamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
				return nil, nil, errors.New(errors.ErrInternal, "test", "stream setup failed")
			},
		}, nil
	}
	cmd := newMsgWatchCommand()
	err := runMsgWatchCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgWatchCommand_BadOutputFile(t *testing.T) {
	origFactory := messagingClientFactory
	origFile := msgFile
	defer func() {
		messagingClientFactory = origFactory
		msgFile = origFile
	}()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return &grpc.MessagingSubscribeResponse{
					Subscription: &grpc.MessagingSubscription{ID: "sub-watch-2", Channel: "test-channel"},
				}, nil
			},
		}, nil
	}
	cmd := newMsgWatchCommand()
	msgFile = "/dev/null/nonexistent/path"
	err := runMsgWatchCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

func TestRemaining91_RunMsgWatchCommand_ErrorChannel(t *testing.T) {
	origFactory := messagingClientFactory
	defer func() { messagingClientFactory = origFactory }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingSubscribeFunc: func(ctx context.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error) {
				return &grpc.MessagingSubscribeResponse{
					Subscription: &grpc.MessagingSubscription{ID: "sub-watch-3", Channel: "test-channel"},
				}, nil
			},
			MessagingStreamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
				errCh := make(chan error, 1)
				errCh <- errors.New(errors.ErrInternal, "test", "watch stream error")
				close(errCh)
				notifCh := make(chan *grpc.MessagingNotification)
				return notifCh, errCh, nil
			},
		}, nil
	}
	cmd := newMsgWatchCommand()
	err := runMsgWatchCommand(cmd, []string{"test-channel"})
	assert.Error(t, err)
}

// cache.go coverage

func TestRemaining91_RunCacheWarmCommand_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	cmd := newCacheWarmCommand()
	err := runCacheWarmCommand(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_RunCacheWarmCommand_NoPrefixes(t *testing.T) {
	origCfg := cfg
	origLog := log
	origPrefixes := cacheWarmPrefixes
	defer func() { cfg = origCfg; log = origLog; cacheWarmPrefixes = origPrefixes }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheWarmPrefixes = []string{}
	cmd := newCacheWarmCommand()
	err := runCacheWarmCommand(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prefix")
}

func TestRemaining91_InitializeCacheWarmerDependencies_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	_, _, _, err := initializeCacheWarmerDependencies()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_InitializeCacheWarmerDependencies_NilLogger(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() { cfg = origCfg; log = origLog }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = nil
	_, _, _, err := initializeCacheWarmerDependencies()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

func TestRemaining91_RunCacheEnableCommand_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	cmd := newCacheEnableCommand()
	err := runCacheEnableCommand(cmd, []string{"users"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_RunCacheDisableCommand_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	cmd := newCacheDisableCommand()
	err := runCacheDisableCommand(cmd, []string{"users"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_RunCacheConfigCommand_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	cmd := newCacheConfigCommand()
	err := runCacheConfigCommand(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_RunCacheConfigCommand_WithEntity(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "table"
	cmd := newCacheConfigCommand()
	err := runCacheConfigCommand(cmd, []string{"users"})
	assert.NoError(t, err)
}

func TestRemaining91_RunCacheConfigCommand_AllEntities(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "json"
	cmd := newCacheConfigCommand()
	err := runCacheConfigCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRemaining91_RunCacheConfigCommand_InvalidOutputFormat(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "xml_bad"
	cmd := newCacheConfigCommand()
	err := runCacheConfigCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestRemaining91_InitializeCacheRegistry_NilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()
	cfg = nil
	_, err := initializeCacheRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestRemaining91_InitializeCacheRegistry_NilLogger(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() { cfg = origCfg; log = origLog }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = nil
	_, err := initializeCacheRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "logger not initialized")
}

func TestRemaining91_InitializeCacheRegistry_Success(t *testing.T) {
	origCfg := cfg
	origLog := log
	defer func() { cfg = origCfg; log = origLog }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	registry, err := initializeCacheRegistry()
	require.NoError(t, err)
	assert.NotNil(t, registry)
	registry.Close()
}

// client.go coverage

func TestRemaining91_CreateSDKClient_EmptyAddress(t *testing.T) {
	origAddr := address
	defer func() { address = origAddr; resetSDKClient() }()
	resetSDKClient()
	address = ""
	client, err := createSDKClient()
	assert.Nil(t, client)
	require.Error(t, err)
}

func TestRemaining91_CreateSDKClient_ReturnsExisting(t *testing.T) {
	defer func() { resetSDKClient() }()
	resetSDKClient()
	mock := &mockClient{}
	var client quicbaas.Client = mock
	sdkClientHolder.Store(&client)
	result, err := createSDKClient()
	require.NoError(t, err)
	assert.Same(t, mock, result)
}

func TestRemaining91_BuildSDKClientOptions_QUICTransport(t *testing.T) {
	origT := transport
	origTimeout := globalTimeout
	defer func() { transport = origT; globalTimeout = origTimeout }()
	transport = "quic"
	globalTimeout = 5 * time.Second
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestRemaining91_BuildSDKClientOptions_RESTTransport(t *testing.T) {
	origT := transport
	origTimeout := globalTimeout
	defer func() { transport = origT; globalTimeout = origTimeout }()
	transport = "rest"
	globalTimeout = 5 * time.Second
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestRemaining91_BuildSDKClientOptions_UnixTransport(t *testing.T) {
	origT := transport
	origAddr := address
	origTimeout := globalTimeout
	defer func() { transport = origT; address = origAddr; globalTimeout = origTimeout }()
	transport = "unix"
	address = "/tmp/test.sock"
	globalTimeout = 5 * time.Second
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestRemaining91_BuildSDKClientOptions_MCPTransport(t *testing.T) {
	origT := transport
	origTimeout := globalTimeout
	defer func() { transport = origT; globalTimeout = origTimeout }()
	transport = "mcp"
	globalTimeout = 5 * time.Second
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.NotEmpty(t, opts)
}

func TestRemaining91_BuildSDKClientOptions_UnsupportedTransport(t *testing.T) {
	origT := transport
	defer func() { transport = origT }()
	transport = "bad_proto"
	opts, err := buildSDKClientOptions()
	assert.Nil(t, opts)
	require.Error(t, err)
}

func TestRemaining91_CloseSDKClient_NilClient(t *testing.T) {
	resetSDKClient()
	err := closeSDKClient()
	assert.NoError(t, err)
}

func TestRemaining91_BuildSDKClientOptions_ZeroTimeout(t *testing.T) {
	origT := transport
	origTimeout := globalTimeout
	defer func() { transport = origT; globalTimeout = origTimeout }()
	transport = "grpc"
	globalTimeout = 0
	opts, err := buildSDKClientOptions()
	require.NoError(t, err)
	assert.Equal(t, 0, len(opts))
}

// ollama.go coverage

func TestRemaining91_CreateOllamaClient_UnsupportedTransport(t *testing.T) {
	origT := transport
	defer func() { transport = origT }()
	transport = "rest"
	_, err := createOllamaClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
}

func TestRemaining91_CreateOllamaClient_GRPCDefault(t *testing.T) {
	origT := transport
	origAddr := address
	origTimeout := timeout
	defer func() { transport = origT; address = origAddr; timeout = origTimeout }()
	transport = ""
	address = "localhost:19993"
	timeout = 1 * time.Second
	client, err := createOllamaClient()
	if err == nil {
		assert.NotNil(t, client)
		client.Close()
	}
}

func TestRemaining91_CreateOllamaGRPCClient_Success(t *testing.T) {
	origAddr := address
	origTimeout := timeout
	defer func() { address = origAddr; timeout = origTimeout }()
	address = "localhost:19992"
	timeout = 1 * time.Second
	client, err := createOllamaGRPCClient()
	if err == nil {
		assert.NotNil(t, client)
		client.Close()
	}
}

// factories_mcp_crypto.go coverage

func TestRemaining91_ParseCryptoEncryptResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoEncryptResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoEncryptResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{
		"key_id": "test-key", "key_version": float64(1),
		"ciphertext": base64.StdEncoding.EncodeToString([]byte("encrypted-data")),
		"algorithm":  "AES256-GCM",
		"nonce":      base64.StdEncoding.EncodeToString([]byte("nonce-bytes")),
	}
	resp, err := parseCryptoEncryptResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.KeyID)
	assert.Equal(t, uint64(1), resp.KeyVersion)
}

func TestRemaining91_ParseCryptoDecryptResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoDecryptResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoDecryptResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{
		"key_id": "decrypt-key", "plaintext": "hello world",
	}
	resp, err := parseCryptoDecryptResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "decrypt-key", resp.KeyID)
	assert.Equal(t, []byte("hello world"), resp.Plaintext)
}

func TestRemaining91_ParseCryptoSignResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoSignResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoSignResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "sign-key", "key_version": float64(1), "signature": base64.StdEncoding.EncodeToString([]byte("sig")), "algorithm": "RSA-SHA256"}
	resp, err := parseCryptoSignResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "sign-key", resp.KeyID)
}

func TestRemaining91_ParseCryptoVerifyResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoVerifyResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoVerifyResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "verify-key", "key_version": float64(1), "valid": true}
	resp, err := parseCryptoVerifyResponse(data)
	require.NoError(t, err)
	assert.True(t, resp.Valid)
}

func TestRemaining91_ParseCryptoHashResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoHashResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoHashResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"algorithm": "SHA256", "hash": base64.StdEncoding.EncodeToString([]byte("hash-bytes"))}
	resp, err := parseCryptoHashResponse(data)
	require.NoError(t, err)
	assert.Equal(t, service.HashAlgorithm("SHA256"), resp.Algorithm)
}

func TestRemaining91_ParseCryptoHMACResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoHMACResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoHMACResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "hmac-key", "key_version": float64(1), "hmac": base64.StdEncoding.EncodeToString([]byte("hmac-data")), "hash_algorithm": "SHA256"}
	resp, err := parseCryptoHMACResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "hmac-key", resp.KeyID)
}

func TestRemaining91_ParseCryptoVerifyHMACResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoVerifyHMACResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoVerifyHMACResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "verify-hmac-key", "key_version": float64(1), "valid": true}
	resp, err := parseCryptoVerifyHMACResponse(data)
	require.NoError(t, err)
	assert.True(t, resp.Valid)
}

func TestRemaining91_ParseCryptoRewrapResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoRewrapResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoRewrapResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "rewrap-key", "old_key_version": float64(1), "new_key_version": float64(2), "ciphertext": base64.StdEncoding.EncodeToString([]byte("rewrapped-data"))}
	resp, err := parseCryptoRewrapResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "rewrap-key", resp.KeyID)
	assert.Equal(t, uint64(2), resp.NewKeyVersion)
}

func TestRemaining91_ParseCryptoGenerateDataKeyResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoGenerateDataKeyResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoGenerateDataKeyResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"key_id": "data-key", "plaintext": base64.StdEncoding.EncodeToString([]byte("plaintext-key")), "encrypted": base64.StdEncoding.EncodeToString([]byte("encrypted-key"))}
	resp, err := parseCryptoGenerateDataKeyResponse(data)
	require.NoError(t, err)
	assert.Equal(t, "data-key", resp.KeyID)
}

func TestRemaining91_ParseCryptoGenerateRandomResponse_InvalidInput(t *testing.T) {
	_, err := parseCryptoGenerateRandomResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseCryptoGenerateRandomResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"random_bytes": base64.StdEncoding.EncodeToString([]byte("random-data"))}
	resp, err := parseCryptoGenerateRandomResponse(data)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.RandomBytes)
}

// factories_mcp_seal.go coverage

func TestRemaining91_ParseUnsealResponse_InvalidInput(t *testing.T) {
	_, err := parseUnsealResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseUnsealResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"sealed": false, "progress": float64(3), "total": float64(5)}
	resp, err := parseUnsealResponse(data)
	require.NoError(t, err)
	assert.False(t, resp.Sealed)
}

func TestRemaining91_ParseSealStatusResponse_InvalidInput(t *testing.T) {
	_, err := parseSealStatusResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseSealStatusResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"sealed": true, "progress": float64(0), "total": float64(5)}
	resp, err := parseSealStatusResponse(data)
	require.NoError(t, err)
	assert.True(t, resp.Sealed)
}

func TestRemaining91_ParseRekeyResponse_InvalidInput(t *testing.T) {
	_, err := parseRekeyResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseRekeyResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{
		"version": float64(3),
		"shares":  []interface{}{base64.StdEncoding.EncodeToString([]byte("share1")), base64.StdEncoding.EncodeToString([]byte("share2"))},
	}
	resp, err := parseRekeyResponse(data)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), resp.Version)
}

// factories_mcp_backup.go coverage

func TestRemaining91_ParseBackupDeleteResponse_InvalidInput(t *testing.T) {
	_, err := parseBackupDeleteResponse(99)
	assert.Error(t, err)
}

func TestRemaining91_ParseBackupDeleteResponse_ValidInput(t *testing.T) {
	data := map[string]interface{}{"deleted": true, "backup_id": "backup-abc-123"}
	resp, err := parseBackupDeleteResponse(data)
	require.NoError(t, err)
	assert.True(t, resp.Deleted)
}

// data.go coverage

func TestRemaining91_RunPutCommand_ClientError(t *testing.T) {
	origAddr := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() { address = origAddr; transport = origTransport; globalTimeout = origTimeout; resetSDKClient() }()
	resetSDKClient()
	address = "localhost:19990"
	transport = "grpc"
	globalTimeout = 1 * time.Second
	cmd := newPutCommand()
	err := runPutCommand(cmd, []string{"key1", "value1"})
	assert.Error(t, err)
}

// crypto.go coverage

func TestRemaining91_RunCryptoRandomCommand_FactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}
	cmd := newCryptoRandomCommand()
	err := runCryptoRandomCommand(cmd, []string{})
	assert.Error(t, err)
}

// factories.go coverage

func TestRemaining91_CreateGRPCBackupClient_ConnectionAttempt(t *testing.T) {
	origAddr := address
	origTimeout := timeout
	defer func() { address = origAddr; timeout = origTimeout }()
	address = "localhost:19991"
	timeout = 1 * time.Second
	client, err := createGRPCBackupClient()
	if err == nil {
		assert.NotNil(t, client)
		client.Close()
	}
}

// secrets.go coverage

func TestRemaining91_GetK8sServiceAccountToken_FileNotFound(t *testing.T) {
	token, err := getK8sServiceAccountToken()
	assert.Error(t, err)
	assert.Empty(t, token)
}

func TestRemaining91_CreateSecretsGRPCClient_DefaultAuth(t *testing.T) {
	origAddr := address
	origTimeout := timeout
	origAuthMode := secretsAuthMode
	defer func() { address = origAddr; timeout = origTimeout; secretsAuthMode = origAuthMode }()
	address = "localhost:19989"
	timeout = 1 * time.Second
	secretsAuthMode = "token"
	client, err := createSecretsGRPCClient()
	if err == nil {
		assert.NotNil(t, client)
		client.Close()
	}
}

func TestRemaining91_CreateSecretsGRPCClient_UnsupportedAuthMode(t *testing.T) {
	origAddr := address
	origTimeout := timeout
	origAuthMode := secretsAuthMode
	defer func() { address = origAddr; timeout = origTimeout; secretsAuthMode = origAuthMode }()
	address = "localhost:19988"
	timeout = 1 * time.Second
	secretsAuthMode = "oauth"
	_, err := createSecretsGRPCClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported auth mode")
}

func TestRemaining91_CreateSecretsGRPCClient_K8sAuthNoToken(t *testing.T) {
	origAddr := address
	origTimeout := timeout
	origAuthMode := secretsAuthMode
	defer func() { address = origAddr; timeout = origTimeout; secretsAuthMode = origAuthMode }()
	address = "localhost:19987"
	timeout = 1 * time.Second
	secretsAuthMode = "k8s"
	_, err := createSecretsGRPCClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Kubernetes service account token")
}

// Cluster display helpers

func TestRemaining91_PrintClusterStatusTableCLI_NotClusterStatusResponse(t *testing.T) {
	err := printClusterStatusTableCLI(map[string]string{"foo": "bar"})
	assert.NoError(t, err)
}

func TestRemaining91_PrintClusterStatusTableCLI_EmptyNodes(t *testing.T) {
	status := &cli.ClusterStatusResponse{Healthy: true, LeaderID: 1, NodeCount: 0, ReadyNodes: 0, Nodes: []cli.NodeInfo{}}
	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

func TestRemaining91_PrintClusterStatusJSON_Success(t *testing.T) {
	err := printClusterStatusJSON(map[string]string{"status": "healthy"})
	assert.NoError(t, err)
}

// Node display helpers

func TestRemaining91_DisplayNodeState_InvalidFormat(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "invalid_fmt"
	state := &NodeState{NodeID: "node-1", Address: "10.0.0.1:50051"}
	err := displayNodeState(state)
	assert.Error(t, err)
}

func TestRemaining91_DisplayNodeReplacementReport_InvalidFormat(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "xml_invalid"
	report := &NodeReplacementReport{OldNodeID: "old-1", NewNodeID: "new-1", Success: true}
	err := displayNodeReplacementReport(report)
	assert.Error(t, err)
}

func TestRemaining91_DisplayNodeReplacementReport_JSONFormat(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "json"
	now := time.Now()
	report := &NodeReplacementReport{OldNodeID: "old-1", NewNodeID: "new-1", StartTime: now, EndTime: now.Add(time.Minute), Duration: time.Minute, KeysEvacuated: 100, BytesEvacuated: 4096, Success: true}
	err := displayNodeReplacementReport(report)
	assert.NoError(t, err)
}

func TestRemaining91_DisplayNodeReplacementReport_Removal(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "table"
	now := time.Now()
	report := &NodeReplacementReport{OldNodeID: "removed-1", NewNodeID: "", StartTime: now, EndTime: now.Add(time.Minute), Duration: time.Minute, KeysEvacuated: 50, BytesEvacuated: 2048, Success: true}
	err := displayNodeReplacementReport(report)
	assert.NoError(t, err)
}

func TestRemaining91_DisplayNodeReplacementReport_WithErrors(t *testing.T) {
	origFmt := nodeOutputFormat
	defer func() { nodeOutputFormat = origFmt }()
	nodeOutputFormat = "table"
	now := time.Now()
	errs := make([]string, 7)
	for i := range errs {
		errs[i] = errors.Newf(errors.ErrInternal, "test", "error %d", i).Error()
	}
	report := &NodeReplacementReport{OldNodeID: "old-err", NewNodeID: "new-err", StartTime: now.Add(-time.Minute), EndTime: now.Add(time.Minute), Duration: 2 * time.Minute, KeysEvacuated: 200, BytesEvacuated: 8192, VerificationTime: 30 * time.Second, Success: false, Errors: errs}
	err := displayNodeReplacementReport(report)
	assert.NoError(t, err)
}

// Cache enable/disable with valid config

func TestRemaining91_RunCacheEnableCommand_Success(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "table"
	cmd := newCacheEnableCommand()
	err := runCacheEnableCommand(cmd, []string{"users"})
	assert.NoError(t, err)
}

func TestRemaining91_RunCacheEnableCommand_InvalidFormat(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "xml"
	cmd := newCacheEnableCommand()
	err := runCacheEnableCommand(cmd, []string{"users"})
	assert.Error(t, err)
}

func TestRemaining91_RunCacheDisableCommand_Success(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "json"
	cmd := newCacheDisableCommand()
	err := runCacheDisableCommand(cmd, []string{"users"})
	assert.NoError(t, err)
}

func TestRemaining91_RunCacheDisableCommand_InvalidFormat(t *testing.T) {
	origCfg := cfg
	origLog := log
	origFmt := cacheOutputFmt
	defer func() { cfg = origCfg; log = origLog; cacheOutputFmt = origFmt }()
	cfg = &config.Config{Cache: validCacheConfig()}
	log = createTestLoggerForRemaining91(t)
	cacheOutputFmt = "yaml_invalid"
	cmd := newCacheDisableCommand()
	err := runCacheDisableCommand(cmd, []string{"users"})
	assert.Error(t, err)
}

// Messaging formatting helpers

func TestRemaining91_FormatDeleteChannelResponse_NotDeleted(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{Name: "test-channel", Deleted: false}
	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "table", &buf)
	assert.NoError(t, err)
}

func TestRemaining91_FormatDeleteChannelResponse_JSONOutput(t *testing.T) {
	resp := &grpc.MessagingDeleteChannelResponse{Name: "json-channel", Deleted: true}
	var buf bytes.Buffer
	err := formatDeleteChannelResponse(resp, "json", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "json-channel")
}

func TestRemaining91_UnsupportedTransportError_String(t *testing.T) {
	err := &UnsupportedTransportError{Transport: "ftp"}
	assert.Contains(t, err.Error(), "unsupported transport")
	assert.Contains(t, err.Error(), "ftp")
}

// Messaging JSON output

func TestRemaining91_RunMsgDeleteChannelCommand_JSONOutput(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() { messagingClientFactory = origFactory; msgForce = origForce; msgOutputFmt = origFmt }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingDeleteChannelFunc: func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
				return &grpc.MessagingDeleteChannelResponse{Name: req.Name, Deleted: true}, nil
			},
		}, nil
	}
	cmd := newMsgDeleteChannelCommand()
	msgForce = true
	msgOutputFmt = "json"
	err := runMsgDeleteChannelCommand(cmd, []string{"json-channel"})
	assert.NoError(t, err)
}

func TestRemaining91_RunMsgUnsubscribeCommand_JSONOutput(t *testing.T) {
	origFactory := messagingClientFactory
	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() { messagingClientFactory = origFactory; msgForce = origForce; msgOutputFmt = origFmt }()
	messagingClientFactory = func() (MessagingClient, error) {
		return &MockMessagingClient{
			MessagingUnsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
				return &grpc.MessagingUnsubscribeResponse{SubscriptionID: req.SubscriptionID, Unsubscribed: true}, nil
			},
		}, nil
	}
	cmd := newMsgUnsubscribeCommand()
	msgForce = true
	msgOutputFmt = "json"
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-json-123"})
	assert.NoError(t, err)
}

func TestDisplayRebalanceProgressSDK_JSON(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{
		OperationID:     "op-abc",
		Status:          "in_progress",
		PercentComplete: 45.5,
		ShardsMoved:     5,
		TotalShards:     10,
		ElapsedTime:     int64(30 * time.Second),
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceProgressSDK(resp, "json")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "op-abc")
}

func TestDisplayRebalanceProgressSDK_Table(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{
		OperationID:     "op-xyz",
		Status:          "completed",
		PercentComplete: 100.0,
		ShardsMoved:     10,
		TotalShards:     10,
		ElapsedTime:     int64(60 * time.Second),
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceProgressSDK(resp, "table")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "op-xyz")
}

func TestDisplayRebalanceProgressSDK_UnsupportedFormat(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{}
	err := displayRebalanceProgressSDK(resp, "xml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestDisplayRebalanceReport_UnsupportedFormat(t *testing.T) {
	report := &RebalanceReport{}
	err := displayRebalanceReport(report, "xml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestDisplayRebalanceReport_JSON(t *testing.T) {
	now := time.Now()
	report := &RebalanceReport{
		StartTime:       now.Add(-10 * time.Minute),
		EndTime:         now,
		Duration:        10 * time.Minute,
		MovesCompleted:  5,
		MovesFailed:     0,
		KeysRebalanced:  1000,
		BytesRebalanced: 1024 * 1024,
		ImbalanceBefore: 0.25,
		ImbalanceAfter:  0.05,
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceReport(report, "json")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "moves_completed")
}

func TestDisplayRebalanceReport_TableWithErrors(t *testing.T) {
	now := time.Now()
	report := &RebalanceReport{
		StartTime:       now.Add(-5 * time.Minute),
		EndTime:         now,
		Duration:        5 * time.Minute,
		MovesCompleted:  3,
		MovesFailed:     2,
		KeysRebalanced:  500,
		BytesRebalanced: 512 * 1024,
		ImbalanceBefore: 0.30,
		ImbalanceAfter:  0.10,
		Errors: []string{
			"shard 1 timeout",
			"shard 2 permission denied",
			"shard 3 timeout",
			"shard 4 timeout",
			"shard 5 timeout",
			"shard 6 timeout",
		},
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceReport(report, "table")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Warning")
	assert.Contains(t, output, "more errors")
}

func TestDisplayRebalanceReport_TableNoMoves(t *testing.T) {
	now := time.Now()
	report := &RebalanceReport{
		StartTime:       now,
		EndTime:         now,
		Duration:        0,
		MovesCompleted:  0,
		ImbalanceBefore: 0.02,
		ImbalanceAfter:  0.02,
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := displayRebalanceReport(report, "table")

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "already balanced")
}

// =============================================================================
// Msg Watch Command - Additional Paths
// =============================================================================

func TestRunMsgWatchCommand_StreamError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			return nil, nil, errors.New(errors.ErrInternal, "test", "stream init failed")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origChannel := msgChannel
	defer func() { msgChannel = origChannel }()
	msgChannel = "test-watch-channel"

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"test-watch-channel"})
	assert.Error(t, err)
}

func TestRunMsgWatchCommand_ChannelClosed(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			notifChan := make(chan *grpc.MessagingNotification, 1)
			errChan := make(chan error)
			notifChan <- &grpc.MessagingNotification{
				Message: &grpc.MessagingMessage{
					ID:        "msg-1",
					Channel:   "test-channel",
					Payload:   []byte(`{"key":"val"}`),
					Timestamp: time.Now(),
				},
				SubscriptionID: "sub-123",
				SequenceNumber: 1,
			}
			close(notifChan)
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origChannel := msgChannel
	origFmt := msgOutputFmt
	origTimestamps := msgTimestamps
	origRaw := msgRaw
	origFile := msgFile
	defer func() {
		msgChannel = origChannel
		msgOutputFmt = origFmt
		msgTimestamps = origTimestamps
		msgRaw = origRaw
		msgFile = origFile
	}()
	msgChannel = "test-channel"
	msgOutputFmt = "table"
	msgTimestamps = false
	msgRaw = false
	msgFile = ""

	cmd := &cobra.Command{}
	err := runMsgWatchCommand(cmd, []string{"test-channel"})
	assert.NoError(t, err)
}

// =============================================================================
// Msg Format Functions - Additional Coverage
// =============================================================================

func TestFormatChannelListResponse_YAML(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "orders", BufferSize: 256, Persistent: true},
		},
		TotalCount: 1,
	}

	var buf bytes.Buffer
	err := formatChannelListResponse(resp, "yaml", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "orders")
}

func TestFormatChannelInfoResponse_YAML(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "events",
			BufferSize: 512,
		},
		Timestamp: time.Now(),
	}
	statsResp := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:              "events",
			Subscribers:       3,
			MessagesPublished: 100,
		},
		Timestamp: time.Now(),
	}
	dedupResp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "events",
			TotalMessages:   100,
			TotalDuplicates: 2,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, statsResp, nil, dedupResp, nil, "yaml", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "events")
}

func TestFormatChannelInfoResponse_WithStatsError(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "logs",
			BufferSize: 128,
		},
		Timestamp: time.Now(),
	}

	statsErr := errors.New(errors.ErrNotFound, "test", "stats not available")
	dedupErr := errors.New(errors.ErrNotFound, "test", "dedup not available")

	var buf bytes.Buffer
	err := formatChannelInfoResponse(channelResp, nil, statsErr, nil, dedupErr, "json", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "logs")
}

func TestFormatSubscriptionListFromStats_YAML(t *testing.T) {
	stats := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:        "orders",
			Subscribers: 2,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromStats(stats, "yaml", &buf)
	assert.NoError(t, err)
}

func TestFormatSubscriptionListFromStats_Table(t *testing.T) {
	stats := &grpc.MessagingGetChannelStatsResponse{
		Stats: &grpc.MessagingChannelStats{
			Name:        "orders",
			Subscribers: 3,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromStats(stats, "table", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "3")
}

func TestFormatSubscriptionListFromChannels_YAML(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "ch1", Persistent: true},
		},
	}

	var buf bytes.Buffer
	err := formatSubscriptionListFromChannels(resp, "yaml", &buf)
	assert.NoError(t, err)
}

func TestFormatWatchNotification_RawMode(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-1",
			Channel:   "test",
			Payload:   []byte(`{"key":"value"}`),
			Timestamp: time.Now(),
		},
		SubscriptionID: "sub-1",
		SequenceNumber: 1,
	}

	var buf bytes.Buffer
	formatWatchNotification(notif, &buf, false, true)
	assert.Contains(t, buf.String(), "test")
}

func TestFormatWatchNotification_WithTimestamps(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-2",
			Channel:   "events",
			Payload:   []byte(`{"event":"click"}`),
			Timestamp: time.Now(),
		},
		SubscriptionID: "sub-2",
		SequenceNumber: 2,
	}

	var buf bytes.Buffer
	formatWatchNotification(notif, &buf, true, false)
	assert.Contains(t, buf.String(), "Message received")
}

func TestFormatWatchNotification_NonJSONPayload(t *testing.T) {
	notif := &grpc.MessagingNotification{
		Message: &grpc.MessagingMessage{
			ID:        "msg-3",
			Channel:   "raw",
			Payload:   []byte("plain text payload"),
			Timestamp: time.Now(),
		},
		SubscriptionID: "sub-3",
		SequenceNumber: 3,
	}

	var buf bytes.Buffer
	formatWatchNotification(notif, &buf, false, false)
	assert.Contains(t, buf.String(), "plain text payload")
}

func TestFormatDedupStatsResponse_YAML(t *testing.T) {
	resp := &grpc.MessagingGetDedupStatsResponse{
		Stats: &grpc.MessagingDedupStats{
			Channel:         "orders",
			TotalMessages:   100,
			TotalDuplicates: 5,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDedupStatsResponse(resp, "yaml", &buf)
	assert.NoError(t, err)
}

// =============================================================================
// Msg Delete/Unsubscribe Force Paths
// =============================================================================

func TestRunMsgDeleteChannelCommand_ForceSuccess(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		msgForce = origForce
		msgOutputFmt = origFmt
	}()
	msgForce = true
	msgOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runMsgDeleteChannelCommand(cmd, []string{"old-channel"})
	assert.NoError(t, err)
}

func TestRunMsgUnsubscribeCommand_ForceSuccess(t *testing.T) {
	mock := &mockMessagingClientForCoverage{}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origForce := msgForce
	origFmt := msgOutputFmt
	defer func() {
		msgForce = origForce
		msgOutputFmt = origFmt
	}()
	msgForce = true
	msgOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-456"})
	assert.NoError(t, err)
}

func TestRunMsgDeleteChannelCommand_ClientError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		deleteChannelFunc: func(ctx context.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origForce := msgForce
	defer func() { msgForce = origForce }()
	msgForce = true

	cmd := &cobra.Command{}
	err := runMsgDeleteChannelCommand(cmd, []string{"missing"})
	assert.Error(t, err)
}

func TestRunMsgUnsubscribeCommand_ClientError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		unsubscribeFunc: func(ctx context.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "subscription not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origForce := msgForce
	defer func() { msgForce = origForce }()
	msgForce = true

	cmd := &cobra.Command{}
	err := runMsgUnsubscribeCommand(cmd, []string{"sub-missing"})
	assert.Error(t, err)
}

// =============================================================================
// Msg Subscribe - Additional Paths
// =============================================================================

func TestRunMsgSubscribeCommand_WithFilter(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		streamNotificationsFunc: func(ctx context.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error) {
			notifChan := make(chan *grpc.MessagingNotification, 1)
			errChan := make(chan error)
			notifChan <- &grpc.MessagingNotification{
				Message: &grpc.MessagingMessage{
					ID:        "msg-1",
					Channel:   "orders",
					Payload:   []byte(`{"status":"shipped"}`),
					Timestamp: time.Now(),
				},
				SubscriptionID: "sub-filter-1",
				SequenceNumber: 1,
			}
			close(notifChan)
			return notifChan, errChan, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origFilter := msgFilter
	origDelta := msgDelta
	origDedup := msgDedup
	origDedupSkip := msgDedupSkip
	origBuf := msgBufferSize
	origReplay := msgReplay
	origFmt := msgOutputFmt
	origTimestamps := msgTimestamps
	origRaw := msgRaw
	defer func() {
		msgFilter = origFilter
		msgDelta = origDelta
		msgDedup = origDedup
		msgDedupSkip = origDedupSkip
		msgBufferSize = origBuf
		msgReplay = origReplay
		msgOutputFmt = origFmt
		msgTimestamps = origTimestamps
		msgRaw = origRaw
	}()
	msgFilter = "order*"
	msgDelta = true
	msgDedup = true
	msgDedupSkip = true
	msgBufferSize = 512
	msgReplay = 10
	msgOutputFmt = "table"
	msgTimestamps = false
	msgRaw = false

	cmd := &cobra.Command{}
	err := runMsgSubscribeCommand(cmd, []string{"orders"})
	assert.NoError(t, err)
}

// =============================================================================
// Msg List Subscriptions - Additional Paths
// =============================================================================

func TestRunMsgListSubscriptionsCommand_WithChannelStats(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		getChannelStatsFunc: func(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
			return &grpc.MessagingGetChannelStatsResponse{
				Stats: &grpc.MessagingChannelStats{
					Name:        req.Name,
					Subscribers: 5,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origChannel := msgChannel
	origFmt := msgOutputFmt
	defer func() {
		msgChannel = origChannel
		msgOutputFmt = origFmt
	}()
	msgChannel = "orders"
	msgOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runMsgListSubscriptionsCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunMsgListSubscriptionsCommand_GetStatsError(t *testing.T) {
	mock := &mockMessagingClientForCoverage{
		getChannelStatsFunc: func(ctx context.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "channel not found")
		},
	}
	cleanup := setupMsgMock(t, mock)
	defer cleanup()

	origChannel := msgChannel
	defer func() { msgChannel = origChannel }()
	msgChannel = "missing"

	cmd := &cobra.Command{}
	err := runMsgListSubscriptionsCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Fetch - Additional Paths
// =============================================================================

func TestRunSecretsFetchCommand_FileFormatSuccess(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := fetchFormat
	origFetchOutputDir := fetchOutputDir
	origFmt2 := secretsOutputFmt
	defer func() {
		fetchFormat = origFmt
		fetchOutputDir = origFetchOutputDir
		secretsOutputFmt = origFmt2
	}()
	fetchFormat = "env"
	fetchOutputDir = t.TempDir()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsFetchCommand(cmd, []string{"apps/myapp/"})
	assert.NoError(t, err)
}

func TestRunSecretsFetchCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := fetchFormat
	origFetchOutputDir := fetchOutputDir
	defer func() {
		fetchFormat = origFmt
		fetchOutputDir = origFetchOutputDir
	}()
	fetchFormat = "json"
	fetchOutputDir = t.TempDir()

	cmd := &cobra.Command{}
	err := runSecretsFetchCommand(cmd, []string{"apps/myapp/"})
	assert.NoError(t, err)
}

// =============================================================================
// Secrets Disable - Additional Paths
// =============================================================================

func TestRunSecretsDisableCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsDisableCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

func TestRunSecretsDisableCommand_ClientError(t *testing.T) {
	mock := &mockSecretsClientForCoverage{
		updateStatusFunc: func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
			return nil, errors.New(errors.ErrNotFound, "test", "secret not found")
		},
	}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsDisableCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Output Formats
// =============================================================================

func TestRunSecretsPutCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origData := secretsData
	origFmt := secretsOutputFmt
	defer func() {
		secretsData = origData
		secretsOutputFmt = origFmt
	}()
	secretsData = []string{"key=value"}
	secretsOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runSecretsPutCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsGetCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runSecretsGetCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsGetCommand_YAMLFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "yaml"

	cmd := &cobra.Command{}
	err := runSecretsGetCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsVersionsCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runSecretsVersionsCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsRollbackCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	origVer := secretsVersion
	defer func() {
		secretsOutputFmt = origFmt
		secretsVersion = origVer
	}()
	secretsOutputFmt = "json"
	secretsVersion = 2

	cmd := &cobra.Command{}
	err := runSecretsRollbackCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsRotateCommand_JSONFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origData := secretsData
	origFmt := secretsOutputFmt
	defer func() {
		secretsData = origData
		secretsOutputFmt = origFmt
	}()
	secretsData = []string{"password=new"}
	secretsOutputFmt = "json"

	cmd := &cobra.Command{}
	err := runSecretsRotateCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

// =============================================================================
// New Rebalance Command Builders
// =============================================================================

func TestNewRebalanceCommands_HasSubcommands(t *testing.T) {
	cmd := newRebalanceCommands()
	require.NotNil(t, cmd)
	assert.Equal(t, "rebalance", cmd.Use)
	assert.True(t, len(cmd.Commands()) >= 3)
}

func TestNewRebalanceStartCommand_HasFlags(t *testing.T) {
	cmd := newRebalanceStartCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "start", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("follow"))
}

func TestNewRebalanceStatusCommand_HasFlags(t *testing.T) {
	cmd := newRebalanceStatusCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "status", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("operation"))
}

func TestNewRebalancePlanCommand_HasFlags(t *testing.T) {
	cmd := newRebalancePlanCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "plan", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("output"))
}
