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
		"audit:query", "audit:read",
	} {
		_, ok := set[perm]
		assert.True(t, ok, "the gRPC registrations must contribute %s", perm)
	}
}

// hostIAMServer answers ListWebAuthnCredentials with a marker so a test can
// tell the host's IAMService instance served the call.
type hostIAMServer struct {
	iampb.UnimplementedIAMServiceServer
}

func (hostIAMServer) ListWebAuthnCredentials(_ context.Context, _ *iampb.ListWebAuthnCredentialsRequest) (*iampb.ListWebAuthnCredentialsResponse, error) {
	return &iampb.ListWebAuthnCredentialsResponse{Total: 41}, nil
}

// TestModule_ServesTheHostIAMService pins that the module mounts the host's
// IAMService, so the node's Unix and TCP servers keep the IAMService the
// legacy Unix listener served, and that a nil IAM field answers Unavailable
// like the other host services.
func TestModule_ServesTheHostIAMService(t *testing.T) {
	composed := &mocks.MockQuicBaaS{}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t),
		WithHostGRPCServers(func() (HostGRPCServers, error) {
			return HostGRPCServers{IAM: hostIAMServer{}}, nil
		}))
	require.NoError(t, err)

	resp, err := iampb.NewIAMServiceClient(startBufconnModule(t, m)).
		ListWebAuthnCredentials(context.Background(), &iampb.ListWebAuthnCredentialsRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(41), resp.GetTotal(), "the host's IAMService must answer")

	nilIAM, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t),
		WithHostGRPCServers(func() (HostGRPCServers, error) {
			return HostGRPCServers{Session: hostSessionServer{}}, nil
		}))
	require.NoError(t, err)
	_, err = iampb.NewIAMServiceClient(startBufconnModule(t, nilIAM)).
		ListWebAuthnCredentials(context.Background(), &iampb.ListWebAuthnCredentialsRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err), "nil IAM: got %v", err)
}

// hostAuditServer is a host-built AuditService whose QueryAuditEvents answers
// a marker total, so a test can tell the host's instance served the call.
type hostAuditServer struct {
	iampb.UnimplementedAuditServiceServer
}

func (hostAuditServer) QueryAuditEvents(_ context.Context, _ *iampb.QueryAuditEventsRequest) (*iampb.QueryAuditEventsResponse, error) {
	return &iampb.QueryAuditEventsResponse{Total: 43}, nil
}

// TestModule_ServesTheHostAuditService verifies the node mounts the
// host-built AuditService, and that a nil Audit field answers Unavailable.
func TestModule_ServesTheHostAuditService(t *testing.T) {
	composed := &mocks.MockQuicBaaS{}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t),
		WithHostGRPCServers(func() (HostGRPCServers, error) {
			return HostGRPCServers{Audit: hostAuditServer{}}, nil
		}))
	require.NoError(t, err)

	resp, err := iampb.NewAuditServiceClient(startBufconnModule(t, m)).
		QueryAuditEvents(context.Background(), &iampb.QueryAuditEventsRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(43), resp.GetTotal(), "the host's AuditService must answer")

	nilAudit, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t),
		WithHostGRPCServers(func() (HostGRPCServers, error) {
			return HostGRPCServers{Session: hostSessionServer{}}, nil
		}))
	require.NoError(t, err)
	_, err = iampb.NewAuditServiceClient(startBufconnModule(t, nilAudit)).
		QueryAuditEvents(context.Background(), &iampb.QueryAuditEventsRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err), "nil Audit: got %v", err)
}

// TestModule_IAMInviteMethodsArePublic pins that the four IAMService invite
// RPCs the legacy interceptor admits without authentication are public here.
func TestModule_IAMInviteMethodsArePublic(t *testing.T) {
	composed := &mocks.MockQuicBaaS{}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	require.NoError(t, err)
	public := m.PublicGRPCMethods()
	for _, method := range []string{
		"/quicbaas.iam.v1.IAMService/GetInvite",
		"/quicbaas.iam.v1.IAMService/ActivateFIDO2Begin",
		"/quicbaas.iam.v1.IAMService/ActivateFIDO2Finish",
		"/quicbaas.iam.v1.IAMService/ActivatePIV",
	} {
		assert.Contains(t, public, method)
	}
}

// TestModule_IAMService_ContributesItsPermissions pins that the permissions
// of IAMService's six annotated RPCs reach the module's grant set.
func TestModule_IAMService_ContributesItsPermissions(t *testing.T) {
	set := map[string]struct{}{}
	contributeGRPCPermissions(set)
	for _, perm := range []string{"users:read", "users:enable", "users:delete", "roles:create", "tenants:create"} {
		_, ok := set[perm]
		assert.True(t, ok, "IAMService must contribute %s", perm)
	}
}
