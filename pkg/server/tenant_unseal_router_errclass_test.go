//go:build !integration

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

package server

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jeremyhahn/go-qrdb/pkg/api/errclass"
	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbmcp "github.com/jeremyhahn/go-qrdb/pkg/api/mcp"
	mcpwire "github.com/jeremyhahn/go-qrdb/pkg/api/mcp/wire"
	qrdbquic "github.com/jeremyhahn/go-qrdb/pkg/api/quic"
	quicwire "github.com/jeremyhahn/go-qrdb/pkg/api/quic/wire"
	qrdbrest "github.com/jeremyhahn/go-qrdb/pkg/api/rest"
	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// nativeAnswer is what the embedded qrdb node's native transports answer an
// error with: its go-qrdb error class and the status each transport's own
// mapping gives it. The Unix socket serves the gRPC tenant barrier service, so
// the gRPC code is its answer too.
type nativeAnswer struct {
	class errclass.Class
	rest  int
	grpc  codes.Code
	quic  uint16
	mcp   mcpwire.Code
}

var (
	nativeUnavailable = nativeAnswer{
		class: errclass.Unavailable,
		rest:  http.StatusServiceUnavailable,
		grpc:  codes.Unavailable,
		quic:  quicwire.StatusUnavailable,
		mcp:   mcpwire.CodeUnavailable,
	}
	nativePermissionDenied = nativeAnswer{
		class: errclass.PermissionDenied,
		rest:  http.StatusForbidden,
		grpc:  codes.PermissionDenied,
		quic:  quicwire.StatusPermissionDenied,
		mcp:   mcpwire.CodePermissionDenied,
	}
)

// nativeAnswerFor runs err through go-qrdb's classifier and through the
// mapping each native transport's handlers use.
func nativeAnswerFor(err error) nativeAnswer {
	return nativeAnswer{
		class: errclass.Classify(err),
		rest:  qrdbrest.ServiceErrorStatus(err),
		grpc:  qrdbgrpc.ServiceErrorCode(err),
		quic:  qrdbquic.ServiceErrorStatus(err),
		mcp:   qrdbmcp.ServiceErrorCode(err),
	}
}

// Every error the router itself, or the facade behind it, refuses a native
// call with reaches the client as what it is: a stopped node as unavailable
// (retryable), a caller the tenant binding refuses as permission denied.
// Neither is answered as an internal error.
func TestTenantUnsealRouter_NativeTransportsAnswerTheErrorClass(t *testing.T) {
	ctx := context.Background()
	acme := qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{Subject: "admin", TenantID: "acme"})
	other := qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{Subject: "admin", TenantID: "other"})
	key := []byte("key")

	node := &fakeNodeTenantServicer{t: t}
	noFacade := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return node },
		func() service.TenantBarrierServicer { return nil },
	)
	noNode := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return nil },
		func() service.TenantBarrierServicer { return nil },
	)
	// The facade outlives the node's tenant client: shutdown stops the client
	// while a call that already holds the facade is still running.
	stopped := service.NewTenantBarrierServiceFrom(func() qrdbtransport.TenantClient { return nil })
	nodeStopped := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return node },
		func() service.TenantBarrierServicer { return stopped },
	)
	hooked, _ := hookedUnsealRouter(t)

	unsealShare := func(callCtx context.Context, r *tenantUnsealRouter) error {
		progress, err := r.TenantBarrierUnsealShare(callCtx, "acme", "share")
		require.Nil(t, progress)
		return err
	}
	getTenant := func(r *tenantUnsealRouter) error {
		info, err := r.GetTenant(ctx, "acme")
		require.Nil(t, info)
		return err
	}

	tests := []struct {
		name string
		err  error
		want nativeAnswer
		is   error
		code pkgerrors.ErrorCode
	}{
		{name: "the sentinel", err: errTenantServiceUnavailable, want: nativeUnavailable, is: errTenantServiceUnavailable},
		{name: "the sentinel, wrapped", err: &ServerCauseError{Message: "unseal", Cause: errTenantServiceUnavailable}, want: nativeUnavailable, is: errTenantServiceUnavailable},
		{name: "unseal, no facade", err: noFacade.TenantBarrierUnseal(acme, "acme", key), want: nativeUnavailable, is: errTenantServiceUnavailable},
		{name: "unseal-share, no facade", err: unsealShare(acme, noFacade), want: nativeUnavailable, is: errTenantServiceUnavailable},
		{name: "forwarded call, no node", err: getTenant(noNode), want: nativeUnavailable, is: errTenantServiceUnavailable},
		{name: "unseal, node stopped under the facade", err: nodeStopped.TenantBarrierUnseal(acme, "acme", key), want: nativeUnavailable, code: pkgerrors.ErrUnavailable},
		{name: "unseal-share, node stopped under the facade", err: unsealShare(acme, nodeStopped), want: nativeUnavailable, code: pkgerrors.ErrUnavailable},
		{name: "unseal, no principal", err: hooked.TenantBarrierUnseal(ctx, "acme", key), want: nativePermissionDenied, code: pkgerrors.ErrForbidden},
		{name: "unseal-share, another tenant's principal", err: unsealShare(other, hooked), want: nativePermissionDenied, code: pkgerrors.ErrForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.err)
			require.Equal(t, tt.want, nativeAnswerFor(tt.err))
			if tt.is != nil {
				require.ErrorIs(t, tt.err, tt.is)
				return
			}
			// The facade's own code and message survive the class.
			require.True(t, hasErrorCode(tt.err, tt.code), "expected code %v, got %v", tt.code, tt.err)
			var qe *pkgerrors.QBError
			require.ErrorAs(t, tt.err, &qe)
			require.Equal(t, qe.Error(), tt.err.Error())
		})
	}
	require.Empty(t, node.recorded())
}

// The router adds a class only where go-qrdb has none: an error go-qrdb
// already classifies (the node's own errors pass through the facade) keeps
// its class and is returned as it came, and so is a facade code the router
// has no class for.
func TestTenantUnsealRouter_LeavesOtherFacadeErrorsAsTheyCame(t *testing.T) {
	ctx := context.Background()
	unmapped := service.NewError(pkgerrors.ErrInvalidInput, "tenant_barrier.unseal_share", "bad share")

	tests := []struct {
		name string
		err  error
		want errclass.Class
	}{
		{name: "an error go-qrdb classifies", err: qrdbtenant.ErrEmptyShare, want: errclass.InvalidArgument},
		{name: "a facade code with no class", err: unmapped, want: errclass.Unclassified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			barrier := fakeShareBarrier{err: tt.err}
			router := newTenantUnsealRouter(
				func() qrdbsdk.ServiceTenantServicer { return &fakeNodeTenantServicer{t: t} },
				func() service.TenantBarrierServicer { return barrier },
			)

			progress, err := router.TenantBarrierUnsealShare(ctx, "acme", "share")
			require.Nil(t, progress)
			require.ErrorIs(t, err, tt.err)
			require.Equal(t, tt.want, errclass.Classify(err))
			var classed *classedTenantBarrierError
			require.False(t, errors.As(err, &classed), "the router wrapped %v", err)
		})
	}
	require.NoError(t, classTenantBarrierError(nil))
}
