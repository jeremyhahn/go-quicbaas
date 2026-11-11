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
	"sync"
	"testing"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// fakeNodeTenantServicer stands in for the node's own tenant servicer. It
// records each call and fails the test if an unseal ever reaches it.
type fakeNodeTenantServicer struct {
	qrdbsdk.ServiceTenantServicer
	t *testing.T

	mu     sync.Mutex
	calls  []string
	probe  qrdbtenant.TenantDataProbe
	status *qrdbtenant.TenantBarrierStatus
	info   *qrdbtenant.TenantInfo
	vote   *qrdbtenant.TenantBarrierUnsealProgress
}

func (f *fakeNodeTenantServicer) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeNodeTenantServicer) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeNodeTenantServicer) GetTenant(_ context.Context, tenantID string) (*qrdbtenant.TenantInfo, error) {
	f.record("GetTenant " + tenantID)
	return f.info, nil
}

func (f *fakeNodeTenantServicer) TenantBarrierStatus(_ context.Context, tenantID string) (*qrdbtenant.TenantBarrierStatus, error) {
	f.record("TenantBarrierStatus " + tenantID)
	return f.status, nil
}

func (f *fakeNodeTenantServicer) TenantBarrierSeal(_ context.Context, tenantID string) error {
	f.record("TenantBarrierSeal " + tenantID)
	return errors.New("seal result")
}

func (f *fakeNodeTenantServicer) TenantBarrierShamirSealVote(_ context.Context, tenantID, share string) (*qrdbtenant.TenantBarrierUnsealProgress, error) {
	f.record("TenantBarrierShamirSealVote " + tenantID + " " + share)
	return f.vote, nil
}

func (f *fakeNodeTenantServicer) TenantBarrierUnseal(context.Context, string, []byte) error {
	f.t.Error("an unseal reached the node's servicer around the facade")
	return nil
}

func (f *fakeNodeTenantServicer) TenantBarrierUnsealShare(context.Context, string, string) (*qrdbtenant.TenantBarrierUnsealProgress, error) {
	f.t.Error("an unseal-share reached the node's servicer around the facade")
	return nil, nil
}

func (f *fakeNodeTenantServicer) SetTenantDataProbe(p qrdbtenant.TenantDataProbe) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probe = p
}

func (f *fakeNodeTenantServicer) currentProbe() qrdbtenant.TenantDataProbe {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probe
}

// fakeShareBarrier is a per-tenant barrier facade that answers unseal-share.
type fakeShareBarrier struct {
	service.TenantBarrierServicer
	progress *qrdbtransport.TenantBarrierUnsealProgress
	err      error
}

func (f fakeShareBarrier) TenantBarrierUnsealShare(context.Context, string, string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	return f.progress, f.err
}

// stubDataProbe is a distinguishable tenant data probe.
type stubDataProbe struct {
	qrdbtenant.TenantDataProbe
	name string
}

// hookedUnsealRouter returns a router whose barrier is the hooked facade over
// an unsealing tenant client, and the tenants its unseal observer saw.
func hookedUnsealRouter(t *testing.T) (*tenantUnsealRouter, func() []string) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []string
	)
	hooks := &service.TenantUnsealHooks{}
	hooks.Register(tenantUnsealFunc(func(_ context.Context, tenantID string) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, tenantID)
	}))
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(hooks))
	inner := &fakeNodeTenantServicer{t: t}
	router := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return inner },
		func() service.TenantBarrierServicer { return barrier },
	)
	return router, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestTenantUnsealRouter_NativeUnsealRunsTheHookedFacade(t *testing.T) {
	router, seen := hookedUnsealRouter(t)
	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "admin", TenantID: "acme"})

	require.NoError(t, router.TenantBarrierUnseal(ctx, "acme", []byte("key")))
	require.Equal(t, []string{"acme"}, seen())
}

func TestTenantUnsealRouter_NativeUnsealKeepsTheTenantBinding(t *testing.T) {
	router, seen := hookedUnsealRouter(t)

	other := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "admin", TenantID: "other"})
	err := router.TenantBarrierUnseal(other, "acme", []byte("key"))
	var qe *pkgerrors.QBError
	require.True(t, errors.As(err, &qe) && qe.Code == pkgerrors.ErrForbidden, "cross-tenant unseal: expected ErrForbidden, got %v", err)

	require.Error(t, router.TenantBarrierUnseal(context.Background(), "acme", []byte("key")))
	require.Empty(t, seen())
}

func TestTenantUnsealRouter_UnsealShareCopiesProgress(t *testing.T) {
	inner := &fakeNodeTenantServicer{t: t}
	var barrier fakeShareBarrier
	router := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return inner },
		func() service.TenantBarrierServicer { return barrier },
	)
	ctx := context.Background()

	barrier = fakeShareBarrier{progress: &qrdbtransport.TenantBarrierUnsealProgress{
		TenantID: "acme", Required: 3, Submitted: 2, Complete: false, Sealed: true,
	}}
	progress, err := router.TenantBarrierUnsealShare(ctx, "acme", "share")
	require.NoError(t, err)
	require.Equal(t, &qrdbtenant.TenantBarrierUnsealProgress{
		TenantID: "acme", Required: 3, Submitted: 2, Complete: false, Sealed: true,
	}, progress)

	barrier = fakeShareBarrier{}
	progress, err = router.TenantBarrierUnsealShare(ctx, "acme", "share")
	require.NoError(t, err)
	require.Nil(t, progress)

	shareErr := errors.New("share refused")
	barrier = fakeShareBarrier{err: shareErr}
	progress, err = router.TenantBarrierUnsealShare(ctx, "acme", "share")
	require.ErrorIs(t, err, shareErr)
	require.Nil(t, progress)
}

func TestTenantUnsealRouter_FailsClosedWithoutFacadeOrNode(t *testing.T) {
	ctx := context.Background()

	inner := &fakeNodeTenantServicer{t: t}
	noFacade := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return inner },
		func() service.TenantBarrierServicer { return nil },
	)
	require.ErrorIs(t, noFacade.TenantBarrierUnseal(ctx, "acme", []byte("key")), errTenantServiceUnavailable)
	progress, err := noFacade.TenantBarrierUnsealShare(ctx, "acme", "share")
	require.ErrorIs(t, err, errTenantServiceUnavailable)
	require.Nil(t, progress)
	require.Empty(t, inner.recorded())

	noNode := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return nil },
		func() service.TenantBarrierServicer { return nil },
	)
	require.NotPanics(t, func() {
		info, err := noNode.GetTenant(ctx, "acme")
		require.ErrorIs(t, err, errTenantServiceUnavailable)
		require.Nil(t, info)
		status, err := noNode.TenantBarrierStatus(ctx, "acme")
		require.ErrorIs(t, err, errTenantServiceUnavailable)
		require.Nil(t, status)
	})
}

func TestTenantUnsealRouter_ForwardsOtherCalls(t *testing.T) {
	inner := &fakeNodeTenantServicer{
		t:      t,
		info:   &qrdbtenant.TenantInfo{},
		status: &qrdbtenant.TenantBarrierStatus{},
		vote:   &qrdbtenant.TenantBarrierUnsealProgress{TenantID: "acme", Required: 2, Submitted: 1, Sealed: false},
	}
	router := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer { return inner },
		func() service.TenantBarrierServicer { return nil },
	)
	ctx := context.Background()

	info, err := router.GetTenant(ctx, "acme")
	require.NoError(t, err)
	require.Same(t, inner.info, info)

	status, err := router.TenantBarrierStatus(ctx, "acme")
	require.NoError(t, err)
	require.Same(t, inner.status, status)

	require.EqualError(t, router.TenantBarrierSeal(ctx, "acme"), "seal result")

	vote, err := router.TenantBarrierShamirSealVote(ctx, "acme", "share-1")
	require.NoError(t, err)
	require.Same(t, inner.vote, vote)

	require.Equal(t, []string{
		"GetTenant acme",
		"TenantBarrierStatus acme",
		"TenantBarrierSeal acme",
		"TenantBarrierShamirSealVote acme share-1",
	}, inner.recorded())
}

func TestTenantUnsealRouter_ProbeReachesTheNodeServicer(t *testing.T) {
	inner := &fakeNodeTenantServicer{t: t}
	var (
		mu   sync.Mutex
		live bool
	)
	router := newTenantUnsealRouter(
		func() qrdbsdk.ServiceTenantServicer {
			mu.Lock()
			defer mu.Unlock()
			if !live {
				return nil
			}
			return inner
		},
		func() service.TenantBarrierServicer { return nil },
	)

	first := &stubDataProbe{name: "first"}
	router.SetTenantDataProbe(first)
	require.Nil(t, inner.currentProbe())

	mu.Lock()
	live = true
	mu.Unlock()
	router.bind()
	require.Same(t, first, inner.currentProbe())

	second := &stubDataProbe{name: "second"}
	router.SetTenantDataProbe(second)
	require.Same(t, second, inner.currentProbe())
}

// The embedded tenant servicer is the in-process view of the hooked router,
// and only once the node's own servicer exists.
func TestHookedTenantServicer_IsTheRouterOnceTheNodeServicerExists(t *testing.T) {
	router, _ := hookedUnsealRouter(t)
	raw := &fakeNodeTenantServicer{t: t}

	view, ok := hookedTenantServicer(raw, router).(inProcessTenantServicer)
	require.True(t, ok, "the embedded tenant servicer must be the in-process view")
	require.Same(t, router, view.tenantUnsealRouter)
	require.Nil(t, hookedTenantServicer(nil, router))
	require.Nil(t, hookedTenantServicer(raw, nil))
}

// An unseal on the embedded tenant servicer runs the hooked facade and never
// reaches the node's own servicer; a caller with no tenant identity is
// refused.
func TestHookedTenantServicer_EmbeddedUnsealRunsTheHookedFacade(t *testing.T) {
	router, seen := hookedUnsealRouter(t)
	raw := &fakeNodeTenantServicer{t: t}
	embedded := hookedTenantServicer(raw, router)
	require.NotNil(t, embedded)

	require.Error(t, embedded.TenantBarrierUnseal(context.Background(), "acme", []byte("key")))
	other := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "admin", TenantID: "other"})
	err := embedded.TenantBarrierUnseal(other, "acme", []byte("key"))
	var qe *pkgerrors.QBError
	require.True(t, errors.As(err, &qe) && qe.Code == pkgerrors.ErrForbidden, "cross-tenant embedded unseal: expected ErrForbidden, got %v", err)
	require.Empty(t, seen())

	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "admin", TenantID: "acme"})
	require.NoError(t, embedded.TenantBarrierUnseal(ctx, "acme", []byte("key")))
	require.Equal(t, []string{"acme"}, seen())
	require.Empty(t, raw.recorded())
}

// With no node the embedded tenant servicer is nil, and the router's inner
// servicer is the node's own service: a forwarded call fails closed instead
// of calling the router again.
func TestServer_GetQRDBTenantServicer_NoNodeIsNilAndTheRouterDoesNotCallItself(t *testing.T) {
	s := &Server{}
	s.tenantUnsealRouter = newTenantUnsealRouter(s.rawQRDBTenantServicer, func() service.TenantBarrierServicer { return nil })

	require.Nil(t, s.GetQRDBTenantServicer())
	info, err := s.tenantUnsealRouter.GetTenant(context.Background(), "acme")
	require.ErrorIs(t, err, errTenantServiceUnavailable)
	require.Nil(t, info)
}
