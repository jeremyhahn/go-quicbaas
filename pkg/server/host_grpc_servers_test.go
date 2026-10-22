//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// lateIAMFacade is the inventory facade whose federation manager and session
// storage appear only once SetIAMProvider runs, as the production facade's
// do. storeServed counts the calls that handed out the real session store.
type lateIAMFacade struct {
	*inventoryFacade
	attached    atomic.Bool
	storeServed atomic.Int32
}

func (f *lateIAMFacade) SetIAMProvider(_ service.IAMComponents) {
	f.attached.Store(true)
}

func (f *lateIAMFacade) GetFederationManager(ctx context.Context) (iam.FederationManager, error) {
	if !f.attached.Load() {
		return nil, nil
	}
	return f.inventoryFacade.GetFederationManager(ctx)
}

func (f *lateIAMFacade) GetSessionStorage(_ context.Context) (iam.SessionStorage, error) {
	if !f.attached.Load() {
		return nil, nil
	}
	f.storeServed.Add(1)
	return f.sessionStore, nil
}

// startNodeModule serves the production quicbaas module over an in-memory
// listener, as the node's TCP gRPC server mounts it.
func startNodeModule(t *testing.T, srv *Server) *ggrpc.ClientConn {
	t.Helper()
	module, err := srv.newQuicBaaSModule()
	require.NoError(t, err)
	node := ggrpc.NewServer()
	module.RegisterGRPC(node)
	listener := bufconn.Listen(1024 * 1024)
	served := make(chan error, 1)
	go func() { served <- node.Serve(listener) }()
	t.Cleanup(func() {
		node.Stop()
		require.NoError(t, <-served)
	})
	conn, err := ggrpc.NewClient("passthrough:///node",
		ggrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return conn
}

// TestHostGRPCServers_NotBuiltBeforeIAMAttaches is the regression for the
// Codex review on #1767: the node accepts requests once startService has
// published the facade, before attachIAMComponents runs. A federation or
// session RPC in that window must answer Unavailable and cache nothing;
// otherwise a server with no federation manager and an empty in-memory
// session store is pinned for good and the Unix listener reuses it.
func TestHostGRPCServers_NotBuiltBeforeIAMAttaches(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.Unix.Socket = ""
	facade := &lateIAMFacade{inventoryFacade: newInventoryFacade()}
	srv.quicBaaSService = facade

	seamMu.Lock()
	validateRoutingFn = func(_ []uint64, _ map[string]uint64) error { return nil }
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) { return &stubIAMComponents{}, nil }
	seamMu.Unlock()

	conn := startNodeModule(t, srv)
	sessions := iampb.NewSessionServiceClient(conn)
	federation := iampb.NewFederationServiceClient(conn)

	// Facade published, IAM not yet attached.
	_, err := sessions.ListSessions(context.Background(), &iampb.ListSessionsRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err), "session before IAM: got %v", err)
	_, err = federation.ListProviders(context.Background(), &iampb.ListProvidersRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err), "federation before IAM: got %v", err)
	_, err = srv.hostGRPCServers()
	var notReady *HostGRPCNotReadyError
	require.True(t, errors.As(err, &notReady), "hostGRPCServers before IAM: got %v", err)
	require.Nil(t, srv.hostGRPC.Load(), "nothing may be cached before IAM attaches")

	require.NoError(t, srv.attachIAMComponents(facade))

	_, err = sessions.ListSessions(context.Background(), &iampb.ListSessionsRequest{})
	assert.NotEqual(t, codes.Unavailable, status.Code(err), "session after IAM: got %v", err)
	built := srv.hostGRPC.Load()
	require.NotNil(t, built, "the first call after IAM attaches builds the shared servers")
	require.NotNil(t, built.Federation, "built over the real federation manager")
	require.NotNil(t, built.Session)
	require.Equal(t, int32(1), facade.storeServed.Load(), "built once, over the real session storage")

	// The Unix listener starts after startService and registers the same
	// instances the node module serves.
	require.NoError(t, srv.startGRPCServer())
	t.Cleanup(srv.grpcServer.Stop)
	unix := srv.grpcServer.GetServiceInfo()
	require.Contains(t, unix, iampb.FederationService_ServiceDesc.ServiceName)
	require.Contains(t, unix, iampb.SessionService_ServiceDesc.ServiceName)
	assert.Same(t, built, srv.hostGRPC.Load(), "the Unix listener reuses the node's instances")
	assert.Equal(t, int32(1), facade.storeServed.Load(), "the Unix listener built nothing new")
}

// TestHostGRPCNotReadyError_IsUnavailable proves the typed error carries the
// gRPC Unavailable code a client retries on.
func TestHostGRPCNotReadyError_IsUnavailable(t *testing.T) {
	err := error(&HostGRPCNotReadyError{})
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Contains(t, err.Error(), "IAM initialisation has not completed")
}
