package apimodule

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// hostSessionServer answers ListSessions so a test can tell the host's
// session server was reached through the module.
type hostSessionServer struct {
	iampb.UnimplementedSessionServiceServer
}

func (hostSessionServer) ListSessions(_ context.Context, _ *iampb.ListSessionsRequest) (*iampb.ListSessionsResponse, error) {
	return &iampb.ListSessionsResponse{Total: 7}, nil
}

// TestModule_HostGRPCServers_ServedThroughTheModule proves the host's
// session server is mounted on the module's server, asked for on each call.
func TestModule_HostGRPCServers_ServedThroughTheModule(t *testing.T) {
	var resolved atomic.Int32
	composed := &mocks.MockQuicBaaS{}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t),
		WithHostGRPCServers(func() (HostGRPCServers, error) {
			resolved.Add(1)
			return HostGRPCServers{Session: hostSessionServer{}}, nil
		}))
	require.NoError(t, err)

	client := iampb.NewSessionServiceClient(startBufconnModule(t, m))
	for range 2 {
		resp, err := client.ListSessions(context.Background(), &iampb.ListSessionsRequest{})
		require.NoError(t, err)
		assert.Equal(t, int32(7), resp.GetTotal())
	}
	assert.Equal(t, int32(2), resolved.Load(), "the module caches nothing: the host owns memoization")
}

// TestModule_HostGRPCServers_NotReadyIsNotCached proves a host that is still
// initialising answers with its own error and that the module caches
// nothing: once the host is ready, the next call reaches its server. Before
// the fix the module memoized the first resolution in its bundle, so a call
// that arrived before IAM attached pinned an empty host for good.
func TestModule_HostGRPCServers_NotReadyIsNotCached(t *testing.T) {
	var ready atomic.Bool
	composed := &mocks.MockQuicBaaS{}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t),
		WithHostGRPCServers(func() (HostGRPCServers, error) {
			if !ready.Load() {
				return HostGRPCServers{}, status.Error(codes.Unavailable, "host initialising")
			}
			return HostGRPCServers{Session: hostSessionServer{}}, nil
		}))
	require.NoError(t, err)
	client := iampb.NewSessionServiceClient(startBufconnModule(t, m))

	_, err = client.ListSessions(context.Background(), &iampb.ListSessionsRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err), "before the host is ready: got %v", err)

	ready.Store(true)
	resp, err := client.ListSessions(context.Background(), &iampb.ListSessionsRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(7), resp.GetTotal())
}

// TestModule_HostGRPCServers_UnavailableWithoutTheOption proves a module
// built without WithHostGRPCServers answers Unavailable for the host
// services instead of Unimplemented or a nil dereference.
func TestModule_HostGRPCServers_UnavailableWithoutTheOption(t *testing.T) {
	composed := &mocks.MockQuicBaaS{}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	require.NoError(t, err)
	conn := startBufconnModule(t, m)

	_, err = iampb.NewSessionServiceClient(conn).ListSessions(context.Background(), &iampb.ListSessionsRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err), "session: got %v", err)

	_, err = iampb.NewFederationServiceClient(conn).ListProviders(context.Background(), &iampb.ListProvidersRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err), "federation: got %v", err)
}

// TestModule_NodeMountedServices_ContributeTheirPermissions proves the
// services mounted for #1696 contribute their permissions, so a role can be
// granted them on the node.
func TestModule_NodeMountedServices_ContributeTheirPermissions(t *testing.T) {
	set := map[string]struct{}{}
	contributeGRPCPermissions(set)
	for _, perm := range []string{
		"agents:read", "agents:write",
		"sdlc:read", "sdlc:bind", "sdlc:unbind", "sdlc:issue_create",
		"service_registry:read", "service_registry:register",
		"users:read", "users:update",
		"tenants:create", "tenants:read", "tenants:delete",
	} {
		_, ok := set[perm]
		assert.True(t, ok, "the gRPC registrations must contribute %s", perm)
	}
}
