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

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// bearerRecordingClient starts an in-memory TLS gRPC server that records the
// "authorization" metadata of the last RPC it receives, and returns a Client
// connected to it with opts plus the server's trust. The KeyManagement service
// is left unimplemented: the test reads what arrived, not what was served.
func bearerRecordingClient(t *testing.T, opts ...ClientOption) (*Client, *[]string) {
	t.Helper()
	serverTLS, clientTLS := sealTLS(t)

	var received []string
	record := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			received = md.Get("authorization")
		}
		return handler(ctx, req)
	}

	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)), grpc.UnaryInterceptor(record))
	pb.RegisterKeyManagementServer(server, pb.UnimplementedKeyManagementServer{})
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Logf("bufconn server stopped: %v", err)
		}
	}()
	t.Cleanup(server.Stop)

	dialer := grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	})
	opts = append([]ClientOption{WithTLSConfig(clientTLS), WithDialOptions(dialer)}, opts...)
	client, err := NewClient("passthrough:///localhost", opts...)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Logf("close client: %v", err)
		}
	})
	return client, &received
}

// TestWithBearerToken_ReachesServiceStubs proves the bearer rides on RPCs made
// through a service stub on the client's connection — the path KMSGenerateKey
// takes — and not only on calls that add the client's metadata.
func TestWithBearerToken_ReachesServiceStubs(t *testing.T) {
	client, received := bearerRecordingClient(t, WithBearerToken("platform-token"))

	_, err := pb.NewKeyManagementClient(client.conn).GenerateKey(context.Background(), &pb.GenerateKeyRequest{Name: "k"})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	require.Equal(t, []string{"Bearer platform-token"}, *received)
}

func TestWithBearerToken_AbsentSendsNoAuthorization(t *testing.T) {
	client, received := bearerRecordingClient(t)

	_, err := pb.NewKeyManagementClient(client.conn).GenerateKey(context.Background(), &pb.GenerateKeyRequest{Name: "k"})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	require.Empty(t, *received)
}

func TestWithBearerToken_EmptyTokenRejected(t *testing.T) {
	_, err := NewClient("passthrough:///localhost", WithBearerToken(""))
	require.Error(t, err)
	require.Contains(t, err.Error(), "bearer token cannot be empty")
}
