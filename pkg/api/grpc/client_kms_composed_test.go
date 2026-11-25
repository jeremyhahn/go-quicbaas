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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

const composedKMSBufSize = 1 << 20

// newComposedKMSClient serves quicbaas.kms.v1 KeyManagement, backed by the
// real KMS server and tenant-aware KMS service, and returns a Client on it.
// No xkms.v1 KeystoreService is registered.
func newComposedKMSClient(t *testing.T) *Client {
	t.Helper()
	svc := kmstenant.NewService(t)
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	lis := bufconn.Listen(composedKMSBufSize)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		return h(contextWithAuthorizedSubject(ctx, &platformtoken.AuthorizedContext{Subject: "caller", TenantID: kmstenant.OwnerTenant}), req)
	}))
	pb.RegisterKeyManagementServer(server, NewKMSServerWithService(nil, svc, log))
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		server.Stop()
		require.NoError(t, <-serveDone)
	})
	return &Client{conn: conn, connected: true, logger: log}
}

func listedKeyIDs(t *testing.T, client *Client) []string {
	t.Helper()
	resp, err := client.KMSListKeys(context.Background(), nil)
	require.NoError(t, err)
	ids := make([]string, 0, len(resp.Keys))
	for _, key := range resp.Keys {
		ids = append(ids, key.KeyID)
	}
	return ids
}

func TestClientKMS_GenerateListDeleteRegenerateThroughTheKMSService(t *testing.T) {
	client := newComposedKMSClient(t)
	ctx := context.Background()

	const name = "composed-key"
	generate := func() error {
		_, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
			Name:          name,
			KeyAttributes: &xkmssdk.KeyAttributes{CN: name, SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
			Usage:         []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		})
		return err
	}

	require.NoError(t, generate())

	ids := listedKeyIDs(t, client)
	require.Contains(t, ids, name)
	require.Contains(t, ids, kmstenant.KeyName)

	_, err := client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{KeyID: name})
	require.NoError(t, err)

	ids = listedKeyIDs(t, client)
	require.NotContains(t, ids, name)
	require.Contains(t, ids, kmstenant.KeyName)

	// The delete released the tenant's key name, so it can be generated again.
	require.NoError(t, generate())
}
