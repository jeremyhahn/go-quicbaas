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
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// grpcAdapter implements MessagingCommands using gRPC client.
type grpcAdapter struct {
	client   *grpcclient.Client
	endpoint string
	creds    credentials.PerRPCCredentials
}

// newGRPCAdapter creates a new gRPC adapter whose RPCs carry the tenant
// operator's bearer. The Operator role holds the messaging permissions the
// MessagingService RPCs require, and the REST adapter acts as the same
// principal, so every protocol works in one tenant.
func newGRPCAdapter(t *testing.T) *grpcAdapter {
	t.Helper()
	endpoints := helpers.GetClusterEndpoints()
	endpoint := "localhost:50051"
	if len(endpoints) > 0 {
		endpoint = endpoints[0]
	}
	return &grpcAdapter{
		endpoint: endpoint,
		creds:    helpers.TenantOperatorGRPCCredentials(t),
	}
}

// Name returns the protocol name.
func (a *grpcAdapter) Name() string {
	return "gRPC"
}

// Setup initializes the gRPC client.
func (a *grpcAdapter) Setup(ctx context.Context) error {
	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return err
	}
	client, err := grpcclient.NewClient(a.endpoint, grpcclient.WithTLSConfig(tlsCfg),
		grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(a.creds)))
	if err != nil {
		return err
	}
	a.client = client
	return nil
}

// Teardown cleans up the gRPC client.
func (a *grpcAdapter) Teardown() error {
	if a.client != nil {
		return a.client.Close()
	}
	return nil
}

// CreateChannel creates a notification channel via gRPC.
func (a *grpcAdapter) CreateChannel(ctx context.Context, req *grpcclient.MessagingCreateChannelRequest) (*grpcclient.MessagingCreateChannelResponse, error) {
	return a.client.MessagingCreateChannel(ctx, req)
}

// DeleteChannel deletes a notification channel via gRPC.
func (a *grpcAdapter) DeleteChannel(ctx context.Context, req *grpcclient.MessagingDeleteChannelRequest) (*grpcclient.MessagingDeleteChannelResponse, error) {
	return a.client.MessagingDeleteChannel(ctx, req)
}

// GetChannel retrieves channel information via gRPC.
func (a *grpcAdapter) GetChannel(ctx context.Context, req *grpcclient.MessagingGetChannelRequest) (*grpcclient.MessagingGetChannelResponse, error) {
	return a.client.MessagingGetChannel(ctx, req)
}

// ListChannels lists all channels via gRPC.
func (a *grpcAdapter) ListChannels(ctx context.Context, req *grpcclient.MessagingListChannelsRequest) (*grpcclient.MessagingListChannelsResponse, error) {
	return a.client.MessagingListChannels(ctx, req)
}

// Subscribe creates a subscription via gRPC.
func (a *grpcAdapter) Subscribe(ctx context.Context, req *grpcclient.MessagingSubscribeRequest) (*grpcclient.MessagingSubscribeResponse, error) {
	return a.client.MessagingSubscribe(ctx, req)
}

// Unsubscribe removes a subscription via gRPC.
func (a *grpcAdapter) Unsubscribe(ctx context.Context, req *grpcclient.MessagingUnsubscribeRequest) (*grpcclient.MessagingUnsubscribeResponse, error) {
	return a.client.MessagingUnsubscribe(ctx, req)
}

// Publish publishes a message via gRPC.
func (a *grpcAdapter) Publish(ctx context.Context, req *grpcclient.MessagingPublishRequest) (*grpcclient.MessagingPublishResponse, error) {
	return a.client.MessagingPublish(ctx, req)
}

// PublishDelta publishes a delta message via gRPC.
func (a *grpcAdapter) PublishDelta(ctx context.Context, req *grpcclient.MessagingPublishDeltaRequest) (*grpcclient.MessagingPublishDeltaResponse, error) {
	return a.client.MessagingPublishDelta(ctx, req)
}

// GetChannelStats retrieves channel statistics via gRPC.
func (a *grpcAdapter) GetChannelStats(ctx context.Context, req *grpcclient.MessagingGetChannelStatsRequest) (*grpcclient.MessagingGetChannelStatsResponse, error) {
	return a.client.MessagingGetChannelStats(ctx, req)
}

// GetDedupStats retrieves deduplication statistics via gRPC.
func (a *grpcAdapter) GetDedupStats(ctx context.Context, req *grpcclient.MessagingGetDedupStatsRequest) (*grpcclient.MessagingGetDedupStatsResponse, error) {
	return a.client.MessagingGetDedupStats(ctx, req)
}

// GetStats retrieves global messaging statistics via gRPC.
func (a *grpcAdapter) GetStats(ctx context.Context) (*grpcclient.MessagingGetStatsResponse, error) {
	return a.client.MessagingGetStats(ctx)
}

// unixSocketAdapter implements MessagingCommands using gRPC over Unix socket.
type unixSocketAdapter struct {
	client     *grpcclient.Client
	socketPath string
	creds      credentials.PerRPCCredentials
}

// newUnixSocketAdapter creates a new Unix socket adapter whose RPCs carry
// the tenant operator's bearer, as the gRPC adapter's do.
func newUnixSocketAdapter(t *testing.T) *unixSocketAdapter {
	t.Helper()
	endpoints := helpers.GetUnixSocketEndpoints()
	socketPath := "/var/run/quicbaas/quicbaas.sock"
	if len(endpoints) > 0 {
		socketPath = endpoints[0]
	}
	return &unixSocketAdapter{
		socketPath: socketPath,
		creds:      helpers.TenantOperatorGRPCCredentials(t),
	}
}

// Name returns the protocol name.
func (a *unixSocketAdapter) Name() string {
	return "UnixSocket"
}

// Setup initializes the gRPC client over Unix socket.
func (a *unixSocketAdapter) Setup(ctx context.Context) error {
	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return err
	}
	client, err := grpcclient.NewClient("unix://"+a.socketPath, grpcclient.WithTLSConfig(tlsCfg),
		grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(a.creds)))
	if err != nil {
		return err
	}
	a.client = client
	return nil
}

// Teardown cleans up the Unix socket client.
func (a *unixSocketAdapter) Teardown() error {
	if a.client != nil {
		return a.client.Close()
	}
	return nil
}

// CreateChannel creates a notification channel via Unix socket.
func (a *unixSocketAdapter) CreateChannel(ctx context.Context, req *grpcclient.MessagingCreateChannelRequest) (*grpcclient.MessagingCreateChannelResponse, error) {
	return a.client.MessagingCreateChannel(ctx, req)
}

// DeleteChannel deletes a notification channel via Unix socket.
func (a *unixSocketAdapter) DeleteChannel(ctx context.Context, req *grpcclient.MessagingDeleteChannelRequest) (*grpcclient.MessagingDeleteChannelResponse, error) {
	return a.client.MessagingDeleteChannel(ctx, req)
}

// GetChannel retrieves channel information via Unix socket.
func (a *unixSocketAdapter) GetChannel(ctx context.Context, req *grpcclient.MessagingGetChannelRequest) (*grpcclient.MessagingGetChannelResponse, error) {
	return a.client.MessagingGetChannel(ctx, req)
}

// ListChannels lists all channels via Unix socket.
func (a *unixSocketAdapter) ListChannels(ctx context.Context, req *grpcclient.MessagingListChannelsRequest) (*grpcclient.MessagingListChannelsResponse, error) {
	return a.client.MessagingListChannels(ctx, req)
}

// Subscribe creates a subscription via Unix socket.
func (a *unixSocketAdapter) Subscribe(ctx context.Context, req *grpcclient.MessagingSubscribeRequest) (*grpcclient.MessagingSubscribeResponse, error) {
	return a.client.MessagingSubscribe(ctx, req)
}

// Unsubscribe removes a subscription via Unix socket.
func (a *unixSocketAdapter) Unsubscribe(ctx context.Context, req *grpcclient.MessagingUnsubscribeRequest) (*grpcclient.MessagingUnsubscribeResponse, error) {
	return a.client.MessagingUnsubscribe(ctx, req)
}

// Publish publishes a message via Unix socket.
func (a *unixSocketAdapter) Publish(ctx context.Context, req *grpcclient.MessagingPublishRequest) (*grpcclient.MessagingPublishResponse, error) {
	return a.client.MessagingPublish(ctx, req)
}

// PublishDelta publishes a delta message via Unix socket.
func (a *unixSocketAdapter) PublishDelta(ctx context.Context, req *grpcclient.MessagingPublishDeltaRequest) (*grpcclient.MessagingPublishDeltaResponse, error) {
	return a.client.MessagingPublishDelta(ctx, req)
}

// GetChannelStats retrieves channel statistics via Unix socket.
func (a *unixSocketAdapter) GetChannelStats(ctx context.Context, req *grpcclient.MessagingGetChannelStatsRequest) (*grpcclient.MessagingGetChannelStatsResponse, error) {
	return a.client.MessagingGetChannelStats(ctx, req)
}

// GetDedupStats retrieves deduplication statistics via Unix socket.
func (a *unixSocketAdapter) GetDedupStats(ctx context.Context, req *grpcclient.MessagingGetDedupStatsRequest) (*grpcclient.MessagingGetDedupStatsResponse, error) {
	return a.client.MessagingGetDedupStats(ctx, req)
}

// GetStats retrieves global messaging statistics via Unix socket.
func (a *unixSocketAdapter) GetStats(ctx context.Context) (*grpcclient.MessagingGetStatsResponse, error) {
	return a.client.MessagingGetStats(ctx)
}
