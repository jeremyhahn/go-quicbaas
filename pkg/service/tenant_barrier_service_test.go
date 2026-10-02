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

package service

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// fakeEncMigrator is a stub qrdbsdk.EncryptionMigrator that records whether it
// was invoked and with which scope, so tenant-binding tests can assert the
// binding fires BEFORE the migrator is reached.
type fakeEncMigrator struct {
	called bool
	scope  qrdbsdk.BarrierScope
	err    error
}

func (f *fakeEncMigrator) EnableEncryption(_ context.Context, scope qrdbsdk.BarrierScope, _ qrdbsdk.MigrationOptions) error {
	f.called = true
	f.scope = scope
	return f.err
}

// TestBarrierService_EnableEncryption_CrossTenantDenied pins the security fix:
// a caller bound to tenant "acme" may NOT enable-encryption on tenant "beta"
// even holding tenant_barrier:enable_encryption. The binding (shared by all
// transports via BarrierService.EnableEncryption) must reject before the
// destructive, irreversible migration reaches the migrator.
func TestBarrierService_EnableEncryption_CrossTenantDenied(t *testing.T) {
	mig := &fakeEncMigrator{}
	svc := NewBarrierService(nil)
	svc.SetMigrator(mig)

	err := svc.EnableEncryption(tenantCtx("acme"), "beta",
		qrdbsdk.MigrationOptions{TargetStrategy: qrdbsdk.StrategyPassphrase})
	if !isForbidden(err) {
		t.Fatalf("cross-tenant enable-encryption: expected ErrForbidden, got %v", err)
	}
	if mig.called {
		t.Error("migrator was invoked despite cross-tenant denial; binding must fire before delegation")
	}
}

// TestBarrierService_EnableEncryption_SameTenantAllowed confirms a tenant may
// enable-encryption on its OWN barrier and the migrator receives ScopeTenant.
func TestBarrierService_EnableEncryption_SameTenantAllowed(t *testing.T) {
	mig := &fakeEncMigrator{}
	svc := NewBarrierService(nil)
	svc.SetMigrator(mig)

	if err := svc.EnableEncryption(tenantCtx("acme"), "acme",
		qrdbsdk.MigrationOptions{TargetStrategy: qrdbsdk.StrategyPassphrase}); err != nil {
		t.Fatalf("same-tenant enable-encryption: %v", err)
	}
	if !mig.called || mig.scope.Kind != qrdbsdk.ScopeTenant || mig.scope.TenantID != "acme" {
		t.Errorf("expected migrator called with ScopeTenant/acme, got called=%v scope=%+v", mig.called, mig.scope)
	}
}

// TestBarrierService_EnableEncryption_SystemScopeNoBinding confirms the system
// scope (empty tenantID) is admin-gated, not tenant-bound: it reaches the
// migrator with ScopeSystem even without a tenant subject in context.
func TestBarrierService_EnableEncryption_SystemScopeNoBinding(t *testing.T) {
	mig := &fakeEncMigrator{}
	svc := NewBarrierService(nil)
	svc.SetMigrator(mig)

	if err := svc.EnableEncryption(context.Background(), "",
		qrdbsdk.MigrationOptions{TargetStrategy: qrdbsdk.StrategyPassphrase}); err != nil {
		t.Fatalf("system-scope enable-encryption: %v", err)
	}
	if !mig.called || mig.scope.Kind != qrdbsdk.ScopeSystem {
		t.Errorf("expected migrator called with ScopeSystem, got called=%v scope=%+v", mig.called, mig.scope)
	}
}

// mockTenantClient implements qrdbtransport.TenantClient, capturing the
// barrier requests it receives and returning configured responses/errors.
type mockTenantClient struct {
	initReq    *qrdbtransport.TenantBarrierInitRequest
	initResp   *qrdbtransport.TenantBarrierInitResponse
	initErr    error
	unsealReq  *qrdbtransport.TenantBarrierUnsealRequest
	unsealErr  error
	shareReq   *qrdbtransport.TenantBarrierUnsealShareRequest
	shareResp  *qrdbtransport.TenantBarrierUnsealProgress
	shareErr   error
	sealReq    *qrdbtransport.TenantBarrierSealRequest
	sealErr    error
	statusReq  *qrdbtransport.TenantBarrierStatusRequest
	statusResp *qrdbtransport.TenantBarrierStatusResponse
	statusErr  error
}

func (m *mockTenantClient) TenantBarrierInit(_ context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	m.initReq = req
	return m.initResp, m.initErr
}

func (m *mockTenantClient) TenantBarrierUnseal(_ context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error {
	m.unsealReq = req
	return m.unsealErr
}

func (m *mockTenantClient) TenantBarrierUnsealShare(_ context.Context, req *qrdbtransport.TenantBarrierUnsealShareRequest) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	m.shareReq = req
	return m.shareResp, m.shareErr
}

func (m *mockTenantClient) TenantBarrierSeal(_ context.Context, req *qrdbtransport.TenantBarrierSealRequest) error {
	m.sealReq = req
	return m.sealErr
}

func (m *mockTenantClient) TenantBarrierStatus(_ context.Context, req *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	m.statusReq = req
	return m.statusResp, m.statusErr
}

// Tenant officer/share-claim methods are unused by the facade.
func (m *mockTenantClient) TenantBarrierOfficerRegister(context.Context, *qrdbtransport.TenantOfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) TenantBarrierOfficerList(context.Context, string) ([]*qrdbtransport.OfficerStatus, error) {
	return nil, nil
}
func (m *mockTenantClient) TenantBarrierOfficerClaimStatus(context.Context, string) ([]*qrdbtransport.OfficerStatus, error) {
	return nil, nil
}
func (m *mockTenantClient) TenantBarrierShareClaimBegin(context.Context, *qrdbtransport.TenantShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) TenantBarrierShareClaimComplete(context.Context, *qrdbtransport.TenantShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) TenantBarrierShamirSealVote(context.Context, *qrdbtransport.TenantShamirSealVoteRequest) (*qrdbtransport.ShamirSealVoteResponse, error) {
	return nil, nil
}

// Non-barrier TenantClient methods are unused by the facade.
func (m *mockTenantClient) CreateTenant(context.Context, *qrdbtransport.CreateTenantRequest) (*qrdbtransport.CreateTenantResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) GetTenant(context.Context, string) (*qrdbtransport.GetTenantResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) ListTenants(context.Context) (*qrdbtransport.ListTenantsResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) DeleteTenant(context.Context, string) error { return nil }
func (m *mockTenantClient) ListDeletedTenants(context.Context, *qrdbtransport.ListDeletedTenantsRequest) (*qrdbtransport.ListDeletedTenantsResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) ReconcileDeletedTenants(context.Context) (*qrdbtransport.ReconcileDeletedTenantsResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) UpdateTenant(context.Context, *qrdbtransport.UpdateTenantRequest) (*qrdbtransport.UpdateTenantResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) UpdateTenantQuotas(context.Context, *qrdbtransport.UpdateTenantQuotasRequest) (*qrdbtransport.UpdateTenantQuotasResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) UpdateTenantEntitlements(context.Context, *qrdbtransport.UpdateTenantEntitlementsRequest) (*qrdbtransport.UpdateTenantEntitlementsResponse, error) {
	return nil, nil
}

// Compile-time check that the mock satisfies the qrdb TenantClient interface.
var _ qrdbtransport.TenantClient = (*mockTenantClient)(nil)

// tenantCtx returns a context carrying an authenticated subject with the given
// tenantID. It is the standard helper for tests that need a tenant-bound caller.
func tenantCtx(tenantID string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:            "user-1",
		Username:      "tenant-admin",
		TenantID:      tenantID,
		Authenticated: true,
	})
}

// isForbidden returns true when err is a QBError with ErrForbidden code.
func isForbidden(err error) bool {
	var qe *pkgerrors.QBError
	return errors.As(err, &qe) && qe.Code == pkgerrors.ErrForbidden
}

// isInvalidInput returns true when err is a QBError with ErrInvalidInput code.
func isInvalidInput(err error) bool {
	var qe *pkgerrors.QBError
	return errors.As(err, &qe) && qe.Code == pkgerrors.ErrInvalidInput
}

// ---------------------------------------------------------------------------
// Delegation tests (happy path) — existing behaviour
// ---------------------------------------------------------------------------

func TestTenantBarrierService_Init_DelegatesAndReturnsShares(t *testing.T) {
	mock := &mockTenantClient{
		initResp: &qrdbtransport.TenantBarrierInitResponse{
			TenantID: "acme", Strategy: "shamir",
			Threshold: 2, TotalShares: 3, ShareCount: 3,
		},
	}
	svc := NewTenantBarrierService(mock)

	resp, err := svc.TenantBarrierInit(tenantCtx("acme"), "acme", []byte("pw"), 2, 3, "shamir")
	if err != nil {
		t.Fatalf("TenantBarrierInit: %v", err)
	}
	// Request was built correctly from the clean args.
	if mock.initReq == nil || mock.initReq.TenantID != "acme" || string(mock.initReq.Passphrase) != "pw" ||
		mock.initReq.Threshold != 2 || mock.initReq.Shares != 3 || mock.initReq.Strategy != "shamir" {
		t.Fatalf("unexpected init request: %+v", mock.initReq)
	}
	// Response passed through unchanged.
	if resp == nil || resp.Threshold != 2 || resp.TotalShares != 3 {
		t.Fatalf("unexpected init response: %+v", resp)
	}
}

func TestTenantBarrierService_Init_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	svc := NewTenantBarrierService(&mockTenantClient{initErr: sentinel})
	if _, err := svc.TenantBarrierInit(tenantCtx("acme"), "acme", nil, 0, 0, "none"); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestTenantBarrierService_Unseal_Delegates(t *testing.T) {
	mock := &mockTenantClient{}
	svc := NewTenantBarrierService(mock)
	if err := svc.TenantBarrierUnseal(tenantCtx("acme"), "acme", []byte("key-material")); err != nil {
		t.Fatalf("TenantBarrierUnseal: %v", err)
	}
	if mock.unsealReq == nil || mock.unsealReq.TenantID != "acme" || string(mock.unsealReq.Key) != "key-material" {
		t.Fatalf("unexpected unseal request: %+v", mock.unsealReq)
	}
}

func TestTenantBarrierService_UnsealShare_DelegatesAndReturnsProgress(t *testing.T) {
	mock := &mockTenantClient{
		shareResp: &qrdbtransport.TenantBarrierUnsealProgress{
			TenantID: "acme", Required: 2, Submitted: 1, Complete: false, Sealed: true,
		},
	}
	svc := NewTenantBarrierService(mock)
	prog, err := svc.TenantBarrierUnsealShare(tenantCtx("acme"), "acme", "share-1")
	if err != nil {
		t.Fatalf("TenantBarrierUnsealShare: %v", err)
	}
	if mock.shareReq == nil || mock.shareReq.TenantID != "acme" || mock.shareReq.Share != "share-1" {
		t.Fatalf("unexpected unseal-share request: %+v", mock.shareReq)
	}
	if prog == nil || prog.Required != 2 || prog.Submitted != 1 || prog.Complete || !prog.Sealed {
		t.Fatalf("unexpected progress: %+v", prog)
	}
}

func TestTenantBarrierService_Seal_Delegates(t *testing.T) {
	mock := &mockTenantClient{}
	svc := NewTenantBarrierService(mock)
	if err := svc.TenantBarrierSeal(tenantCtx("acme"), "acme"); err != nil {
		t.Fatalf("TenantBarrierSeal: %v", err)
	}
	if mock.sealReq == nil || mock.sealReq.TenantID != "acme" {
		t.Fatalf("unexpected seal request: %+v", mock.sealReq)
	}
}

func TestTenantBarrierService_Status_DelegatesAndReturnsShamirParams(t *testing.T) {
	mock := &mockTenantClient{
		statusResp: &qrdbtransport.TenantBarrierStatusResponse{
			TenantID: "acme", Sealed: true, Initialized: true,
			Strategy: "shamir", ShamirThreshold: 2, ShamirTotal: 3,
		},
	}
	svc := NewTenantBarrierService(mock)
	st, err := svc.TenantBarrierStatus(tenantCtx("acme"), "acme")
	if err != nil {
		t.Fatalf("TenantBarrierStatus: %v", err)
	}
	if mock.statusReq == nil || mock.statusReq.TenantID != "acme" {
		t.Fatalf("unexpected status request: %+v", mock.statusReq)
	}
	if st == nil || st.Strategy != "shamir" || st.ShamirThreshold != 2 || st.ShamirTotal != 3 {
		t.Fatalf("unexpected status: %+v", st)
	}
}

// ---------------------------------------------------------------------------
// C-1: Tenant binding tests — cross-tenant and system-tenant denials
// ---------------------------------------------------------------------------

// TestTenantBarrierService_CrossTenantDenied verifies that a caller whose token
// carries tenantID "acme" cannot perform barrier operations on tenant "beta".
// This is enforced at the service facade, covering all four transports.
func TestTenantBarrierService_CrossTenantDenied(t *testing.T) {
	mock := &mockTenantClient{}
	svc := NewTenantBarrierService(mock)

	// Caller is "acme" but target is "beta" — every operation must be forbidden.
	ctx := tenantCtx("acme")

	if _, err := svc.TenantBarrierInit(ctx, "beta", nil, 1, 1, "none"); !isForbidden(err) {
		t.Errorf("Init: expected ErrForbidden for cross-tenant, got %v", err)
	}
	if err := svc.TenantBarrierUnseal(ctx, "beta", []byte("k")); !isForbidden(err) {
		t.Errorf("Unseal: expected ErrForbidden for cross-tenant, got %v", err)
	}
	if _, err := svc.TenantBarrierUnsealShare(ctx, "beta", "share"); !isForbidden(err) {
		t.Errorf("UnsealShare: expected ErrForbidden for cross-tenant, got %v", err)
	}
	if err := svc.TenantBarrierSeal(ctx, "beta"); !isForbidden(err) {
		t.Errorf("Seal: expected ErrForbidden for cross-tenant, got %v", err)
	}
	if _, err := svc.TenantBarrierStatus(ctx, "beta"); !isForbidden(err) {
		t.Errorf("Status: expected ErrForbidden for cross-tenant, got %v", err)
	}

	// The mock must not have been called — the check rejects before delegating.
	if mock.initReq != nil || mock.unsealReq != nil || mock.shareReq != nil || mock.sealReq != nil || mock.statusReq != nil {
		t.Error("mock was called despite cross-tenant denial; check must fire before delegation")
	}
}

// systemCtx returns a context carrying an authenticated system-tenant subject.
// Both canonical representations are produced: the empty TenantID (pre-tenant
// contexts) and the explicit qrdbsdk.SystemTenantID (SPIFFE-derived tokens).
func systemCtx(tenantID string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "admin", Username: "admin", TenantID: tenantID, Authenticated: true,
	})
}

// TestTenantBarrierService_SystemTenantWritesDenied verifies that a
// system-tenant caller (empty TenantID or the explicit "system" tenant) is
// denied for every MUTATING barrier operation. System admins hold no
// per-tenant barrier key (ScopeTenantOnly); tenant admins self-bootstrap
// their own barriers per the locked architecture.
func TestTenantBarrierService_SystemTenantWritesDenied(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("subject_tenant="+tenantID, func(t *testing.T) {
			mock := &mockTenantClient{}
			svc := NewTenantBarrierService(mock)
			ctx := systemCtx(tenantID)

			if _, err := svc.TenantBarrierInit(ctx, "acme", nil, 1, 1, "none"); !isForbidden(err) {
				t.Errorf("Init: expected ErrForbidden for system-tenant caller, got %v", err)
			}
			if err := svc.TenantBarrierUnseal(ctx, "acme", []byte("k")); !isForbidden(err) {
				t.Errorf("Unseal: expected ErrForbidden for system-tenant caller, got %v", err)
			}
			if _, err := svc.TenantBarrierUnsealShare(ctx, "acme", "share"); !isForbidden(err) {
				t.Errorf("UnsealShare: expected ErrForbidden for system-tenant caller, got %v", err)
			}
			if err := svc.TenantBarrierSeal(ctx, "acme"); !isForbidden(err) {
				t.Errorf("Seal: expected ErrForbidden for system-tenant caller, got %v", err)
			}

			// The mock must never see a mutating request.
			if mock.initReq != nil || mock.unsealReq != nil || mock.shareReq != nil || mock.sealReq != nil {
				t.Error("mock received a mutating request despite system-tenant denial")
			}
		})
	}
}

// TestTenantBarrierService_SystemTenantStatusAllowed verifies the owner
// decision: system-tenant callers may READ any tenant barrier's status
// (control-plane observability) while every mutation stays denied. Both
// canonical system-tenant representations are admitted.
func TestTenantBarrierService_SystemTenantStatusAllowed(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("subject_tenant="+tenantID, func(t *testing.T) {
			mock := &mockTenantClient{
				statusResp: &qrdbtransport.TenantBarrierStatusResponse{
					TenantID: "acme", Sealed: true, Initialized: true, Strategy: "shamir",
				},
			}
			svc := NewTenantBarrierService(mock)

			st, err := svc.TenantBarrierStatus(systemCtx(tenantID), "acme")
			if err != nil {
				t.Fatalf("Status: system-tenant read must be allowed, got %v", err)
			}
			if mock.statusReq == nil || mock.statusReq.TenantID != "acme" {
				t.Fatalf("unexpected status request: %+v", mock.statusReq)
			}
			if st == nil || st.TenantID != "acme" || !st.Sealed || !st.Initialized {
				t.Fatalf("unexpected status response: %+v", st)
			}
		})
	}
}

// TestTenantBarrierService_SystemTenantStatusPropagatesError verifies the
// system-tenant read path propagates delegate errors unchanged (no allow-path
// masking of qrdb failures).
func TestTenantBarrierService_SystemTenantStatusPropagatesError(t *testing.T) {
	sentinel := errors.New("qrdb down")
	svc := NewTenantBarrierService(&mockTenantClient{statusErr: sentinel})
	if _, err := svc.TenantBarrierStatus(systemCtx(qrdbsdk.SystemTenantID), "acme"); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// TestTenantBarrierService_UnauthenticatedDenied verifies that a context with
// no authenticated subject is denied for all barrier operations.
func TestTenantBarrierService_UnauthenticatedDenied(t *testing.T) {
	svc := NewTenantBarrierService(&mockTenantClient{})
	ctx := context.Background() // no subject injected

	if _, err := svc.TenantBarrierInit(ctx, "acme", nil, 1, 1, "none"); !isForbidden(err) {
		t.Errorf("Init: expected ErrForbidden for unauthenticated context, got %v", err)
	}
	if err := svc.TenantBarrierUnseal(ctx, "acme", []byte("k")); !isForbidden(err) {
		t.Errorf("Unseal: expected ErrForbidden for unauthenticated context, got %v", err)
	}
	if _, err := svc.TenantBarrierUnsealShare(ctx, "acme", "share"); !isForbidden(err) {
		t.Errorf("UnsealShare: expected ErrForbidden for unauthenticated context, got %v", err)
	}
	if err := svc.TenantBarrierSeal(ctx, "acme"); !isForbidden(err) {
		t.Errorf("Seal: expected ErrForbidden for unauthenticated context, got %v", err)
	}
	if _, err := svc.TenantBarrierStatus(ctx, "acme"); !isForbidden(err) {
		t.Errorf("Status: expected ErrForbidden for unauthenticated context, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// M-1: Shamir parameter validation tests
// ---------------------------------------------------------------------------

// TestTenantBarrierService_Init_ShamirValidation_ThresholdZero verifies that
// threshold == 0 is rejected with ErrInvalidInput before delegating to qrdb.
func TestTenantBarrierService_Init_ShamirValidation_ThresholdZero(t *testing.T) {
	mock := &mockTenantClient{}
	svc := NewTenantBarrierService(mock)
	_, err := svc.TenantBarrierInit(tenantCtx("acme"), "acme", nil, 0, 3, "shamir")
	if !isInvalidInput(err) {
		t.Fatalf("expected ErrInvalidInput for threshold=0, got %v", err)
	}
	if mock.initReq != nil {
		t.Error("qrdb must not be called when shamir parameters are invalid")
	}
}

// TestTenantBarrierService_Init_ShamirValidation_SharesZero verifies that
// shares == 0 is rejected with ErrInvalidInput.
func TestTenantBarrierService_Init_ShamirValidation_SharesZero(t *testing.T) {
	mock := &mockTenantClient{}
	svc := NewTenantBarrierService(mock)
	_, err := svc.TenantBarrierInit(tenantCtx("acme"), "acme", nil, 2, 0, "shamir")
	if !isInvalidInput(err) {
		t.Fatalf("expected ErrInvalidInput for shares=0, got %v", err)
	}
	if mock.initReq != nil {
		t.Error("qrdb must not be called when shamir parameters are invalid")
	}
}

// TestTenantBarrierService_Init_ShamirValidation_ThresholdExceedsShares verifies
// that threshold > shares is rejected with ErrInvalidInput.
func TestTenantBarrierService_Init_ShamirValidation_ThresholdExceedsShares(t *testing.T) {
	mock := &mockTenantClient{}
	svc := NewTenantBarrierService(mock)
	_, err := svc.TenantBarrierInit(tenantCtx("acme"), "acme", nil, 5, 3, "shamir")
	if !isInvalidInput(err) {
		t.Fatalf("expected ErrInvalidInput for threshold(5) > shares(3), got %v", err)
	}
	if mock.initReq != nil {
		t.Error("qrdb must not be called when shamir parameters are invalid")
	}
}

// TestTenantBarrierService_Init_ShamirValidation_EqualThresholdAndShares verifies
// that threshold == shares is valid (M-of-M quorum).
func TestTenantBarrierService_Init_ShamirValidation_EqualThresholdAndShares(t *testing.T) {
	mock := &mockTenantClient{initResp: &qrdbtransport.TenantBarrierInitResponse{TenantID: "acme", Strategy: "shamir"}}
	svc := NewTenantBarrierService(mock)
	if _, err := svc.TenantBarrierInit(tenantCtx("acme"), "acme", nil, 3, 3, "shamir"); err != nil {
		t.Fatalf("threshold==shares should be valid: %v", err)
	}
}

// TestTenantBarrierService_Init_ShamirValidation_NotAppliedToOtherStrategies verifies
// that the shamir parameter validation is NOT applied to none/passphrase strategies,
// where threshold/shares carry zero values.
func TestTenantBarrierService_Init_ShamirValidation_NotAppliedToOtherStrategies(t *testing.T) {
	for _, strategy := range []string{"none", "passphrase"} {
		t.Run(strategy, func(t *testing.T) {
			mock := &mockTenantClient{initResp: &qrdbtransport.TenantBarrierInitResponse{TenantID: "acme", Strategy: strategy}}
			svc := NewTenantBarrierService(mock)
			// threshold=0, shares=0 — valid for non-shamir strategies.
			if _, err := svc.TenantBarrierInit(tenantCtx("acme"), "acme", []byte("pw"), 0, 0, strategy); err != nil {
				t.Fatalf("strategy=%s: unexpected error for zero threshold/shares: %v", strategy, err)
			}
		})
	}
}
