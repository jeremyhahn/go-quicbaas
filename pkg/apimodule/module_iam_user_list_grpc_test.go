//go:build rest && mcp

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

package apimodule

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	sdkiam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	sdkgrpc "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
)

// userListGRPCUsers is the size of the tenant the paging test lists: one
// full page of 100 and a partial page of 50.
const userListGRPCUsers = 150

// userListGRPCTransport serves the real IAMServer over the real
// IAMServiceAdapter and an in-memory identity store holding
// userListGRPCUsers users of parityProjectTenant, and returns an SDK gRPC
// transport dialled to it.
func userListGRPCTransport(t *testing.T) transport.Transport {
	t.Helper()

	ctx := context.Background()
	mem := iam.NewMemoryStorage()
	for i := 0; i < userListGRPCUsers; i++ {
		require.NoError(t, mem.CreateUser(ctx, &iam.User{
			TenantID: parityProjectTenant,
			Email:    fmt.Sprintf("u%03d@parity.test", i),
			Username: fmt.Sprintf("u%03d", i),
			Name:     fmt.Sprintf("User %03d", i),
		}))
	}
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})

	serverTLS, clientTLS := parityTLS(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveParityGRPC(t, func(srv grpclib.ServiceRegistrar) {
		iampb.RegisterIAMServiceServer(srv, grpcapi.NewIAMServer(
			grpcapi.NewIAMServiceAdapter(nil, store, nil, nil, nil, nil), testLogger(t)))
	}, serverTLS, lis)

	tr, err := sdkgrpc.New(&transport.Config{
		Address: lis.Addr().String(), Timeout: 5 * time.Second, TLSConfig: clientTLS,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, tr.Close()) })
	return tr
}

// TestIAMUserList_GRPCPagesRealAdapter pages a 150-user tenant through the
// SDK gRPC transport against the production server and adapter, whose Total
// counts only the rows of the page, and requires every user to be reached.
func TestIAMUserList_GRPCPagesRealAdapter(t *testing.T) {
	tr := userListGRPCTransport(t)
	client := sdkiam.NewUserClient(tr)
	ctx := context.Background()

	var sizes []int
	var tokens []string
	seen := map[string]bool{}
	token := ""
	for page := 0; ; page++ {
		require.Less(t, page, 5, "paging did not stop")
		resp, err := client.List(ctx, parityProjectTenant, &sdkiam.ListUsersRequest{PageSize: 100, PageToken: token})
		require.NoError(t, err, "page %d", page)
		sizes = append(sizes, len(resp.Users))
		for _, u := range resp.Users {
			seen[u.ID] = true
		}
		if !resp.Pagination.HasMore {
			assert.Empty(t, resp.Pagination.NextPageToken, "last page token")
			break
		}
		token = resp.Pagination.NextPageToken
		tokens = append(tokens, token)
	}

	assert.Equal(t, []int{100, 50}, sizes, "page sizes")
	assert.Equal(t, []string{"100"}, tokens, "page tokens")
	assert.Len(t, seen, userListGRPCUsers, "distinct users reached")
}
