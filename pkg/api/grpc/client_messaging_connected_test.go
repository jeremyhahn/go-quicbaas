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
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

const messagingBufSize = 1024 * 1024

// =============================================================================
// Minimal Mock MessagingService Server
// =============================================================================

type minimalMessagingServer struct {
	pb.UnimplementedMessagingServiceServer
}

func (m *minimalMessagingServer) CreateChannel(_ context.Context, req *pb.MessagingCreateChannelRequest) (*pb.MessagingCreateChannelResponse, error) {
	return &pb.MessagingCreateChannelResponse{
		Channel: &pb.Channel{
			Name:  req.Name,
			Codec: req.Codec,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) DeleteChannel(_ context.Context, req *pb.MessagingDeleteChannelRequest) (*pb.MessagingDeleteChannelResponse, error) {
	return &pb.MessagingDeleteChannelResponse{
		Name:      req.Name,
		Deleted:   true,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) GetChannel(_ context.Context, req *pb.MessagingGetChannelRequest) (*pb.MessagingGetChannelResponse, error) {
	return &pb.MessagingGetChannelResponse{
		Channel: &pb.Channel{
			Name: req.Name,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) ListChannels(_ context.Context, _ *pb.MessagingListChannelsRequest) (*pb.MessagingListChannelsResponse, error) {
	return &pb.MessagingListChannelsResponse{
		Channels:   []*pb.Channel{},
		NextCursor: "",
		HasMore:    false,
		TotalCount: 0,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) Subscribe(_ context.Context, req *pb.MessagingSubscribeRequest) (*pb.MessagingSubscribeResponse, error) {
	return &pb.MessagingSubscribeResponse{
		Subscription: &pb.Subscription{
			Id:      "sub-1",
			Channel: req.Channel,
		},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) Unsubscribe(_ context.Context, req *pb.MessagingUnsubscribeRequest) (*pb.MessagingUnsubscribeResponse, error) {
	return &pb.MessagingUnsubscribeResponse{
		SubscriptionId: req.SubscriptionId,
		Unsubscribed:   true,
		Timestamp:      time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) Publish(_ context.Context, req *pb.MessagingPublishRequest) (*pb.MessagingPublishResponse, error) {
	return &pb.MessagingPublishResponse{
		MessageId:   "msg-1",
		Channel:     req.Channel,
		IsDuplicate: false,
		Timestamp:   time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) PublishDelta(_ context.Context, req *pb.MessagingPublishDeltaRequest) (*pb.MessagingPublishDeltaResponse, error) {
	return &pb.MessagingPublishDeltaResponse{
		MessageId:   "msg-delta-1",
		Channel:     req.Channel,
		IsDuplicate: false,
		Timestamp:   time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) WatchChannel(_ context.Context, _ *pb.MessagingWatchChannelRequest) (*pb.MessagingWatchChannelResponse, error) {
	return &pb.MessagingWatchChannelResponse{
		Subscription: &pb.Subscription{
			Id:      "watch-sub-1",
			Channel: "watch-channel",
		},
		ChannelName: "watch-channel",
		Timestamp:   time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) GetStats(_ context.Context, _ *pb.MessagingGetStatsRequest) (*pb.MessagingGetStatsResponse, error) {
	return &pb.MessagingGetStatsResponse{
		Stats: &pb.Stats{
			TotalChannels:      5,
			TotalSubscriptions: 10,
		},
		IsRunning: true,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) GetDedupStats(_ context.Context, _ *pb.MessagingGetDedupStatsRequest) (*pb.MessagingGetDedupStatsResponse, error) {
	return &pb.MessagingGetDedupStatsResponse{
		Stats:     &pb.DedupStats{},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *minimalMessagingServer) GetChannelStats(_ context.Context, _ *pb.MessagingGetChannelStatsRequest) (*pb.MessagingGetChannelStatsResponse, error) {
	return &pb.MessagingGetChannelStatsResponse{
		Stats:     &pb.ChannelStats{},
		Timestamp: time.Now().Unix(),
	}, nil
}

// =============================================================================
// Test Server Setup
// =============================================================================

func setupMessagingTestServer(t *testing.T) (*grpc.Server, *bufconn.Listener) {
	t.Helper()
	lis := bufconn.Listen(messagingBufSize)
	server := grpc.NewServer()
	pb.RegisterMessagingServiceServer(server, &minimalMessagingServer{})
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func setupConnectedMessagingClient(t *testing.T) (*Client, *grpc.Server) {
	t.Helper()
	server, lis := setupMessagingTestServer(t)
	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	client := &Client{
		conn: conn,
		opts: &clientOptions{
			timeout:         5 * time.Second,
			maxRetries:      1,
			retryBackoff:    10 * time.Millisecond,
			maxRetryBackoff: 100 * time.Millisecond,
		},
		connected: true,
	}

	return client, server
}

// =============================================================================
// Messaging Client Connected Tests
// =============================================================================

func TestClientMessagingCreateChannel_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingCreateChannel(context.Background(), &MessagingCreateChannelRequest{
		Name:  "test-channel",
		Codec: "json",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Channel)
	assert.Equal(t, "test-channel", resp.Channel.Name)
}

func TestClientMessagingDeleteChannel_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingDeleteChannel(context.Background(), &MessagingDeleteChannelRequest{
		Name: "test-channel",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	assert.Equal(t, "test-channel", resp.Name)
}

func TestClientMessagingGetChannel_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingGetChannel(context.Background(), &MessagingGetChannelRequest{
		Name: "test-channel",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Channel)
	assert.Equal(t, "test-channel", resp.Channel.Name)
}

func TestClientMessagingListChannels_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingListChannels(context.Background(), &MessagingListChannelsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientMessagingSubscribe_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingSubscribe(context.Background(), &MessagingSubscribeRequest{
		Channel:   "test-channel",
		SessionID: "session-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Subscription)
	assert.Equal(t, "sub-1", resp.Subscription.ID)
}

func TestClientMessagingUnsubscribe_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingUnsubscribe(context.Background(), &MessagingUnsubscribeRequest{
		SubscriptionID: "sub-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Unsubscribed)
}

func TestClientMessagingPublish_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingPublish(context.Background(), &MessagingPublishRequest{
		Channel: "test-channel",
		Payload: []byte("hello"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "msg-1", resp.MessageID)
	assert.Equal(t, "test-channel", resp.Channel)
}

func TestClientMessagingPublishDelta_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingPublishDelta(context.Background(), &MessagingPublishDeltaRequest{
		Channel:  "test-channel",
		OldValue: []byte("old"),
		NewValue: []byte("new"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "msg-delta-1", resp.MessageID)
}

func TestClientMessagingWatchChannel_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingWatchChannel(context.Background(), &MessagingWatchChannelRequest{
		KeyPrefix: "test/",
		SessionID: "session-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "watch-channel", resp.ChannelName)
}

func TestClientMessagingGetStats_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingGetStats(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.IsRunning)
}

func TestClientMessagingGetDedupStats_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingGetDedupStats(context.Background(), &MessagingGetDedupStatsRequest{
		Channel: "test-channel",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClientMessagingGetChannelStats_Connected(t *testing.T) {
	client, server := setupConnectedMessagingClient(t)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.MessagingGetChannelStats(context.Background(), &MessagingGetChannelStatsRequest{
		Name: "test-channel",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}
