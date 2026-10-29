//go:build mcp

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
	stderrors "errors"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	mcpservertest "github.com/mark3labs/mcp-go/server/servertest"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// refusingIAM is an IAM service whose ProvisionTenant refuses with a fixed
// error. The embedded interface is nil: no other IAM method is reached.
type refusingIAM struct {
	service.IAMService
	err error
}

func (r *refusingIAM) ProvisionTenant(context.Context, *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	return nil, r.err
}

// iamFacade adds the IAM accessor the MCP IAM tools resolve to the mocked
// QuicBaaS facade.
type iamFacade struct {
	*testutil.MockQuicBaaS
	iam service.IAMService
}

func (f *iamFacade) IAM() service.IAMService { return f.iam }

// newMCPClient serves the module's real MCP tool surface over the
// streamable HTTP transport and returns an SDK MCP transport dialled to it.
func newMCPClient(t *testing.T, svc service.QuicBaaS) transport.Transport {
	t.Helper()
	backend := mcpapi.NewServer(svc, mcpapi.DefaultConfig(), newMCPTestLogger(t))
	return newMCPClientFor(t, func() *mcpapi.Server { return backend })
}

// newMCPClientFor serves the module's MCP surface, resolving its backend
// through resolve, to an SDK MCP transport over streamable HTTP.
func newMCPClientFor(t *testing.T, resolve MCPResolver) transport.Transport {
	t.Helper()
	m := newMCPModule(t, resolve)

	srv := mcpserver.NewMCPServer("quicbaas-test", "1.0.0")
	m.RegisterMCP(srv)
	httpSrv := mcpservertest.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)

	tr, err := mcptransport.New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Close()) })
	return tr
}

// TestMCPToolError_ProvisionRefusalKeepsCode proves a tenant-provision
// refusal reaches the SDK caller over MCP with the code the service gave it,
// not as an internal error.
func TestMCPToolError_ProvisionRefusalKeepsCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want sdkerrors.ErrorCode
	}{
		{
			name: "forbidden",
			err:  qberrors.New(qberrors.ErrForbidden, "iam.ProvisionTenant", "caller is not a system administrator"),
			want: sdkerrors.CodeForbidden,
		},
		{
			name: "already exists",
			err:  qberrors.New(qberrors.ErrAlreadyExists, "iam.ProvisionTenant", "tenant acme already exists"),
			want: sdkerrors.CodeAlreadyExists,
		},
		{
			name: "untyped",
			err:  context.DeadlineExceeded,
			want: sdkerrors.CodeInternal,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &iamFacade{MockQuicBaaS: testutil.NewMockQuicBaaS(), iam: &refusingIAM{err: tc.err}}
			tr := newMCPClient(t, svc)

			_, err := iam.NewTenantClient(tr).Provision(context.Background(),
				&iam.ProvisionTenantRequest{TenantID: "acme", AdminEnrollment: iam.TenantAdminEnrollInvite})
			require.Error(t, err)
			require.Equal(t, tc.want, sdkerrors.Code(err), "error: %v", err)
		})
	}
}

// TestMCPToolError_SecretNotFoundKeepsCode proves a second Security() verb
// keeps its code over MCP: a missing secret is not-found, as on REST.
func TestMCPToolError_SecretNotFoundKeepsCode(t *testing.T) {
	svc := testutil.NewMockQuicBaaS()
	svc.On("GetSecret", mock.Anything, mock.Anything).
		Return(nil, qberrors.NotFound("secrets.Get", "secret")).Once()
	tr := newMCPClient(t, svc)

	_, err := secrets.NewClient(tr).Get(context.Background(), "apps/db")
	require.Error(t, err)
	require.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err), "error: %v", err)
}

// TestMCPToolError_ServerClassTextWithheld pins that a server-class failure's
// internal detail does not reach the SDK caller over MCP: the caller gets the
// code and the sentence REST sends, never the path the store error names.
func TestMCPToolError_ServerClassTextWithheld(t *testing.T) {
	const internal = "open /etc/quicbaas/secrets/tenant-ca.key: permission denied"
	svc := testutil.NewMockQuicBaaS()
	svc.On("GetSecret", mock.Anything, mock.Anything).
		Return(nil, qberrors.Wrap(stderrors.New(internal), qberrors.ErrInternal, "secrets.store.Get")).Once()
	tr := newMCPClient(t, svc)

	_, err := secrets.NewClient(tr).Get(context.Background(), "apps/db")
	require.Error(t, err)
	require.Equal(t, sdkerrors.CodeInternal, sdkerrors.Code(err), "error: %v", err)
	require.NotContains(t, err.Error(), "tenant-ca.key")
	require.NotContains(t, err.Error(), "secrets.store.Get")
	require.Contains(t, err.Error(), qberrors.ServerFailureMessage)
}

// TestMCPToolError_RequestClassMessageKept pins that a request-class failure
// keeps its message over MCP, so the caller learns what to correct.
func TestMCPToolError_RequestClassMessageKept(t *testing.T) {
	svc := testutil.NewMockQuicBaaS()
	svc.On("GetSecret", mock.Anything, mock.Anything).
		Return(nil, qberrors.New(qberrors.ErrForbidden, "secrets.Get", "path apps/db is outside the caller's scope")).Once()
	tr := newMCPClient(t, svc)

	_, err := secrets.NewClient(tr).Get(context.Background(), "apps/db")
	require.Error(t, err)
	require.Equal(t, sdkerrors.CodeForbidden, sdkerrors.Code(err), "error: %v", err)
	require.Contains(t, err.Error(), "path apps/db is outside the caller's scope")
}

// sealedProbe reports the barrier sealed.
type sealedProbe struct{}

func (sealedProbe) IsSealed() bool { return true }

// TestMCPToolError_SealedWindowIsUnavailable pins that a tool refused by the
// sealed-window gate reaches the SDK caller as unavailable, the code REST
// (503) and gRPC (Unavailable) return, with the sealed reason intact.
func TestMCPToolError_SealedWindowIsUnavailable(t *testing.T) {
	backend := mcpapi.NewServer(testutil.NewMockQuicBaaS(), mcpapi.DefaultConfig(), newMCPTestLogger(t))
	backend.SetSealedWindowGate(sealedProbe{}, nil, nil)
	tr := newMCPClientFor(t, func() *mcpapi.Server { return backend })

	_, err := secrets.NewClient(tr).Get(context.Background(), "apps/db")
	require.Error(t, err)
	require.Equal(t, sdkerrors.CodeUnavailable, sdkerrors.Code(err), "error: %v", err)
	require.Contains(t, err.Error(), "barrier sealed")
}

// TestMCPToolError_ComposingSurfaceIsUnavailable pins that a tool called
// before the MCP backend composes reaches the SDK caller as unavailable, the
// code REST (503) and gRPC (Unavailable) return for the same state.
func TestMCPToolError_ComposingSurfaceIsUnavailable(t *testing.T) {
	tr := newMCPClientFor(t, func() *mcpapi.Server { return nil })

	_, err := secrets.NewClient(tr).Get(context.Background(), "apps/db")
	require.Error(t, err)
	require.Equal(t, sdkerrors.CodeUnavailable, sdkerrors.Code(err), "error: %v", err)
	require.Contains(t, err.Error(), "not yet available")
}
