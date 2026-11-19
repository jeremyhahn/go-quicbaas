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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// subjectRecordingBarrier is a barrier facade that records the subject each
// unseal-share is called with.
type subjectRecordingBarrier struct {
	service.TenantBarrierServicer
	subjects []interfaces.Subject
}

func (b *subjectRecordingBarrier) TenantBarrierUnsealShare(ctx context.Context, tenantID, _ string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	subject, _ := auth.SubjectFromContext(ctx)
	b.subjects = append(b.subjects, subject)
	return &qrdbtransport.TenantBarrierUnsealProgress{TenantID: tenantID, Complete: true}, nil
}

// An in-process caller with no principal of its own unseals the tenant it
// names on the context: the embedded CLI path. The tenant binding still
// refuses a caller that names no tenant, another tenant, or the system
// tenant, and the node's network transports (the router itself) never take a
// tenant named on the context as a principal.
func TestHookedTenantServicer_InProcessCallerActsForTheTenantItNames(t *testing.T) {
	router, seen := hookedUnsealRouter(t)
	raw := &fakeNodeTenantServicer{t: t}
	embedded := hookedTenantServicer(raw, router)
	require.NotNil(t, embedded)
	key := []byte("key")
	acme := tenantctx.WithTenant(context.Background(), "acme")

	refused := map[string]error{
		"no tenant named":             embedded.TenantBarrierUnseal(context.Background(), "acme", key),
		"another tenant named":        embedded.TenantBarrierUnseal(tenantctx.WithTenant(context.Background(), "other"), "acme", key),
		"system tenant named":         embedded.TenantBarrierUnseal(tenantctx.WithSystem(context.Background()), "acme", key),
		"system tenant as the target": embedded.TenantBarrierUnseal(tenantctx.WithSystem(context.Background()), tenantctx.SystemTenantID, key),
		"network transport":           router.TenantBarrierUnseal(acme, "acme", key),
	}
	for name, err := range refused {
		require.True(t, hasErrorCode(err, pkgerrors.ErrForbidden), "%s: expected ErrForbidden, got %v", name, err)
	}
	require.Empty(t, seen())

	require.NoError(t, embedded.TenantBarrierUnseal(acme, "acme", key))
	require.Equal(t, []string{"acme"}, seen())
	require.Empty(t, raw.recorded())
}

// The in-process view binds its principal for unseal-share too, never
// replaces a verified identity, and leaves the router's own calls alone.
func TestHookedTenantServicer_InProcessUnsealShareBindsThePrincipal(t *testing.T) {
	barrier := &subjectRecordingBarrier{}
	raw := &fakeNodeTenantServicer{t: t}
	router := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return raw },
		func() service.TenantBarrierServicer { return barrier },
	)
	embedded := hookedTenantServicer(raw, router)
	require.NotNil(t, embedded)
	acme := tenantctx.WithTenant(context.Background(), "acme")

	progress, err := embedded.TenantBarrierUnsealShare(acme, "acme", "share")
	require.NoError(t, err)
	require.True(t, progress.Complete)

	verified := qrdbsdk.WithIdentity(acme, &qrdbsdk.Identity{Subject: "admin", TenantID: "other"})
	_, err = embedded.TenantBarrierUnsealShare(verified, "acme", "share")
	require.NoError(t, err)

	_, err = router.TenantBarrierUnsealShare(acme, "acme", "share")
	require.NoError(t, err)

	require.Len(t, barrier.subjects, 3)
	require.Equal(t, interfaces.Subject{
		ID: inProcessTenantPrincipal, Username: inProcessTenantPrincipal, TenantID: "acme", Authenticated: true,
	}, barrier.subjects[0])
	require.Equal(t, "other", barrier.subjects[1].TenantID, "a verified identity must not be replaced")
	require.NotEqual(t, inProcessTenantPrincipal, barrier.subjects[1].ID)
	require.False(t, barrier.subjects[2].Authenticated, "the router itself must not bind an in-process principal")
	require.Empty(t, raw.recorded())
}

// The embedded CLI's path end to end: an SDK client over the qrdb embedded
// transport, wired with the server's tenant servicer, unseals the tenant its
// context names and is refused for any other.
func TestHookedTenantServicer_EmbeddedSDKClientUnsealsTheTenantItNames(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })
	svcs := node.Services()
	require.NotNil(t, svcs)

	router, seen := hookedUnsealRouter(t)
	raw := &fakeNodeTenantServicer{t: t}
	client, err := newTenantBarrierClient(svcs.KV, svcs.Admin, hookedTenantServicer(raw, router))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	tenants := client.Tenants()
	require.NotNil(t, tenants)
	unseal := func(ctx context.Context) error {
		return tenants.TenantBarrierUnseal(ctx, &qrdbtransport.TenantBarrierUnsealRequest{TenantID: "acme", Key: []byte("key")})
	}

	err = unseal(context.Background())
	require.True(t, hasErrorCode(err, pkgerrors.ErrForbidden), "no tenant named: expected ErrForbidden, got %v", err)
	err = unseal(tenantctx.WithTenant(context.Background(), "other"))
	require.True(t, hasErrorCode(err, pkgerrors.ErrForbidden), "another tenant named: expected ErrForbidden, got %v", err)
	require.Empty(t, seen())

	require.NoError(t, unseal(tenantctx.WithTenant(context.Background(), "acme")))
	require.Equal(t, []string{"acme"}, seen())
	require.Empty(t, raw.recorded())
}
