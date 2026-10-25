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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"reflect"
	"strings"
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
	quorumReq  *qrdbtransport.TenantQuorumSubmitRequest
	quorumResp *qrdbtransport.QuorumOutcome
	quorumErr  error
	sealReq    *qrdbtransport.TenantBarrierSealRequest
	sealErr    error
	statusReq  *qrdbtransport.TenantBarrierStatusRequest
	statusResp *qrdbtransport.TenantBarrierStatusResponse
	statusErr  error
	tenantResp *qrdbtransport.GetTenantResponse
	tenantErr  error
}

func (m *mockTenantClient) TenantBarrierInit(_ context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	m.initReq = req
	return m.initResp, m.initErr
}

func (m *mockTenantClient) TenantBarrierUnseal(_ context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error {
	m.unsealReq = req
	return m.unsealErr
}

func (m *mockTenantClient) TenantBarrierQuorumSubmit(_ context.Context, req *qrdbtransport.TenantQuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
	m.quorumReq = req
	return m.quorumResp, m.quorumErr
}

// unsealSubmit is a tenant unseal-quorum submission carrying share.
func unsealSubmit(tenantID, share string) *qrdbtransport.TenantQuorumSubmitRequest {
	return &qrdbtransport.TenantQuorumSubmitRequest{TenantID: tenantID, Op: qrdbtransport.OpUnseal, Share: []byte(share)}
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
func (m *mockTenantClient) TenantBarrierShareClaimBegin(context.Context, *qrdbtransport.TenantShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) TenantBarrierShareClaimComplete(context.Context, *qrdbtransport.TenantShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	return nil, nil
}

// Non-barrier TenantClient methods are unused by the facade.
func (m *mockTenantClient) CreateTenant(context.Context, *qrdbtransport.CreateTenantRequest) (*qrdbtransport.CreateTenantResponse, error) {
	return nil, nil
}
func (m *mockTenantClient) GetTenant(context.Context, string) (*qrdbtransport.GetTenantResponse, error) {
	return m.tenantResp, m.tenantErr
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

	resp, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: []byte("pw"), Threshold: 2, Shares: 3, Strategy: "shamir"})
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
	if _, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: nil, Threshold: 0, Shares: 0, Strategy: "none"}); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// raftTenant is a GetTenant reply for a raft-isolated tenant.
func raftTenant(id string) *qrdbtransport.GetTenantResponse {
	return &qrdbtransport.GetTenantResponse{Tenant: qrdbtransport.TenantInfo{ID: id, IsolationType: raftIsolation}}
}

// TestTenantBarrierService_Init_RaftTenantWithoutSignerRefused pins Codex
// 6757 finding 1: a remote transport's request has no Signer, so go-qrdb
// would init a raft-isolated tenant with no signed init statement. The
// service refuses it typed and never calls init.
func TestTenantBarrierService_Init_RaftTenantWithoutSignerRefused(t *testing.T) {
	mock := &mockTenantClient{tenantResp: raftTenant("acme")}
	svc := NewTenantBarrierService(mock)
	_, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Strategy: "none"})
	var unsupported *TenantBarrierInitStatementUnsupportedError
	if !errors.As(err, &unsupported) || unsupported.TenantID != "acme" {
		t.Fatalf("expected *TenantBarrierInitStatementUnsupportedError for acme, got %v", err)
	}
	if mock.initReq != nil {
		t.Fatal("init reached go-qrdb without a signed init statement")
	}
	// The refusal names the path that can sign the statement.
	if !strings.Contains(err.Error(), "quicbaas iam tenant barrier init") {
		t.Fatalf("refusal does not name the supported path: %v", err)
	}
}

// TestTenantBarrierService_Init_RaftTenantWithSignerDelegates confirms an
// in-process caller that supplies the owner's key still initializes.
func TestTenantBarrierService_Init_RaftTenantWithSignerDelegates(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	mock := &mockTenantClient{tenantResp: raftTenant("acme")}
	svc := NewTenantBarrierService(mock)
	req := &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Strategy: "none", Signer: key}
	if _, err := svc.TenantBarrierInit(tenantCtx("acme"), req); err != nil {
		t.Fatalf("init with signer: %v", err)
	}
	if mock.initReq != req {
		t.Fatal("init with signer did not reach go-qrdb")
	}
}

// TestTenantBarrierService_Init_PrefixTenantWithoutSignerDelegates confirms
// the refusal is limited to raft isolation: go-qrdb asks no statement of a
// prefix tenant.
func TestTenantBarrierService_Init_PrefixTenantWithoutSignerDelegates(t *testing.T) {
	mock := &mockTenantClient{tenantResp: &qrdbtransport.GetTenantResponse{Tenant: qrdbtransport.TenantInfo{ID: "acme", IsolationType: qrdbtransport.TenantIsolationPrefix}}}
	svc := NewTenantBarrierService(mock)
	if _, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Strategy: "none"}); err != nil {
		t.Fatalf("prefix tenant init: %v", err)
	}
	if mock.initReq == nil {
		t.Fatal("prefix tenant init did not reach go-qrdb")
	}
}

// TestTenantBarrierService_Init_TenantLookupErrorPropagates confirms a
// failed isolation lookup is returned and init is not attempted.
func TestTenantBarrierService_Init_TenantLookupErrorPropagates(t *testing.T) {
	sentinel := errors.New("lookup failed")
	mock := &mockTenantClient{tenantErr: sentinel}
	svc := NewTenantBarrierService(mock)
	if _, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Strategy: "none"}); !errors.Is(err, sentinel) {
		t.Fatalf("expected lookup error, got %v", err)
	}
	if mock.initReq != nil {
		t.Fatal("init attempted after a failed isolation lookup")
	}
}

// tenantBarrierTestRoster is a two-officer inline Shamir roster in go-qrdb's
// own request type.
func tenantBarrierTestRoster() []qrdbtransport.OfficerRegisterRequest {
	return []qrdbtransport.OfficerRegisterRequest{
		{Username: "officer-1", Role: "unseal", CSRPEM: "csr-1", Factors: []qrdbtransport.HolderFactorEnrolment{{Name: "mtls"}}},
		{Username: "officer-2", Role: "unseal", CSRPEM: "csr-2", Designated: "mtls"},
	}
}

// A fresh Shamir tenant given an inline roster must reach go-qrdb with the
// roster and its policy, or go-qrdb takes the pre-enrolled-holder path and
// fails on officers that were never registered (Codex 6709 P1).
func TestTenantBarrierService_Init_ForwardsOfficersAndPolicy(t *testing.T) {
	mock := &mockTenantClient{}
	svc := NewTenantBarrierService(mock)

	roster := tenantBarrierTestRoster()
	_, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{
		TenantID:  "acme",
		Strategy:  "shamir",
		Threshold: 2,
		Shares:    2,
		Officers:  roster,
		Policy:    "any-enrolled",
	})
	if err != nil {
		t.Fatalf("TenantBarrierInit: %v", err)
	}
	if mock.initReq == nil {
		t.Fatal("go-qrdb received no init request")
	}
	if !reflect.DeepEqual(mock.initReq.Officers, roster) {
		t.Errorf("officers reaching go-qrdb = %+v, want %+v", mock.initReq.Officers, roster)
	}
	if mock.initReq.Policy != "any-enrolled" {
		t.Errorf("policy reaching go-qrdb = %q, want any-enrolled", mock.initReq.Policy)
	}
}

// go-qrdb's typed refusal of an invalid roster reaches the caller unchanged.
func TestTenantBarrierService_Init_InvalidRosterErrorUnchanged(t *testing.T) {
	refusal := &qrdbtransport.FactorTypeUnsupportedError{Type: "bogus"}
	svc := NewTenantBarrierService(&mockTenantClient{initErr: refusal})

	_, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{
		TenantID:  "acme",
		Strategy:  "shamir",
		Threshold: 1,
		Shares:    1,
		Officers:  []qrdbtransport.OfficerRegisterRequest{{Username: "officer-1", Factors: []qrdbtransport.HolderFactorEnrolment{{Name: "bogus"}}}},
	})
	var got *qrdbtransport.FactorTypeUnsupportedError
	if !errors.As(err, &got) || got != refusal {
		t.Fatalf("expected go-qrdb's FactorTypeUnsupportedError unchanged, got %v", err)
	}
}

func TestTenantBarrierService_Init_NilRequestRejected(t *testing.T) {
	mock := &mockTenantClient{}
	svc := NewTenantBarrierService(mock)
	if _, err := svc.TenantBarrierInit(tenantCtx("acme"), nil); !isInvalidInput(err) {
		t.Fatalf("expected ErrInvalidInput for a nil request, got %v", err)
	}
	if mock.initReq != nil {
		t.Error("a nil request must not reach go-qrdb")
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

func TestTenantBarrierService_QuorumSubmit_DelegatesAndReturnsOutcome(t *testing.T) {
	mock := &mockTenantClient{
		quorumResp: &qrdbtransport.QuorumOutcome{Op: qrdbtransport.OpUnseal, Required: 2, Submitted: 1},
	}
	svc := NewTenantBarrierService(mock)
	outcome, err := svc.TenantBarrierQuorumSubmit(tenantCtx("acme"), unsealSubmit("acme", "share-1"))
	if err != nil {
		t.Fatalf("TenantBarrierQuorumSubmit: %v", err)
	}
	if mock.quorumReq == nil || mock.quorumReq.TenantID != "acme" || string(mock.quorumReq.Share) != "share-1" {
		t.Fatalf("unexpected quorum submission: %+v", mock.quorumReq)
	}
	if outcome == nil || outcome.Required != 2 || outcome.Submitted != 1 || outcome.Complete {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
}

func TestTenantBarrierService_QuorumSubmit_PropagatesError(t *testing.T) {
	want := errors.New("bad share")
	svc := NewTenantBarrierService(&mockTenantClient{quorumErr: want})
	if _, err := svc.TenantBarrierQuorumSubmit(tenantCtx("acme"), unsealSubmit("acme", "share-x")); !errors.Is(err, want) {
		t.Fatalf("expected the qrdb refusal, got %v", err)
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

	if _, err := svc.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{TenantID: "beta", Passphrase: nil, Threshold: 1, Shares: 1, Strategy: "none"}); !isForbidden(err) {
		t.Errorf("Init: expected ErrForbidden for cross-tenant, got %v", err)
	}
	if err := svc.TenantBarrierUnseal(ctx, "beta", []byte("k")); !isForbidden(err) {
		t.Errorf("Unseal: expected ErrForbidden for cross-tenant, got %v", err)
	}
	if _, err := svc.TenantBarrierQuorumSubmit(ctx, unsealSubmit("beta", "share")); !isForbidden(err) {
		t.Errorf("QuorumSubmit: expected ErrForbidden for cross-tenant, got %v", err)
	}
	if err := svc.TenantBarrierSeal(ctx, "beta"); !isForbidden(err) {
		t.Errorf("Seal: expected ErrForbidden for cross-tenant, got %v", err)
	}
	if _, err := svc.TenantBarrierStatus(ctx, "beta"); !isForbidden(err) {
		t.Errorf("Status: expected ErrForbidden for cross-tenant, got %v", err)
	}

	// The mock must not have been called — the check rejects before delegating.
	if mock.initReq != nil || mock.unsealReq != nil || mock.quorumReq != nil || mock.sealReq != nil || mock.statusReq != nil {
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

			if _, err := svc.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: nil, Threshold: 1, Shares: 1, Strategy: "none"}); !isForbidden(err) {
				t.Errorf("Init: expected ErrForbidden for system-tenant caller, got %v", err)
			}
			if err := svc.TenantBarrierUnseal(ctx, "acme", []byte("k")); !isForbidden(err) {
				t.Errorf("Unseal: expected ErrForbidden for system-tenant caller, got %v", err)
			}
			if _, err := svc.TenantBarrierQuorumSubmit(ctx, unsealSubmit("acme", "share")); !isForbidden(err) {
				t.Errorf("QuorumSubmit: expected ErrForbidden for system-tenant caller, got %v", err)
			}
			if err := svc.TenantBarrierSeal(ctx, "acme"); !isForbidden(err) {
				t.Errorf("Seal: expected ErrForbidden for system-tenant caller, got %v", err)
			}

			// The mock must never see a mutating request.
			if mock.initReq != nil || mock.unsealReq != nil || mock.quorumReq != nil || mock.sealReq != nil {
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
	for _, tenantID := range []string{qrdbsdk.SystemTenantID} {
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

// TestTenantBarrierService_EmptyTenantCallerDenied pins #348: an authenticated
// subject with an empty tenant is bound to no tenant and is not the system
// tenant, so it may neither read any tenant's barrier status nor mutate a
// barrier — including a request whose target tenant is also empty.
func TestTenantBarrierService_EmptyTenantCallerDenied(t *testing.T) {
	for _, target := range []string{"acme", ""} {
		t.Run("target="+target, func(t *testing.T) {
			mock := &mockTenantClient{
				statusResp: &qrdbtransport.TenantBarrierStatusResponse{TenantID: target},
			}
			svc := NewTenantBarrierService(mock)
			ctx := tenantCtx("")

			if _, err := svc.TenantBarrierStatus(ctx, target); !isForbidden(err) {
				t.Errorf("Status: expected ErrForbidden for empty-tenant caller, got %v", err)
			}
			if _, err := svc.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{TenantID: target, Passphrase: nil, Threshold: 1, Shares: 1, Strategy: "none"}); !isForbidden(err) {
				t.Errorf("Init: expected ErrForbidden for empty-tenant caller, got %v", err)
			}
			if err := svc.TenantBarrierUnseal(ctx, target, []byte("k")); !isForbidden(err) {
				t.Errorf("Unseal: expected ErrForbidden for empty-tenant caller, got %v", err)
			}
			if err := svc.TenantBarrierSeal(ctx, target); !isForbidden(err) {
				t.Errorf("Seal: expected ErrForbidden for empty-tenant caller, got %v", err)
			}
			if mock.statusReq != nil || mock.initReq != nil || mock.unsealReq != nil || mock.sealReq != nil {
				t.Error("mock received a request despite the empty-tenant denial")
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

	if _, err := svc.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: nil, Threshold: 1, Shares: 1, Strategy: "none"}); !isForbidden(err) {
		t.Errorf("Init: expected ErrForbidden for unauthenticated context, got %v", err)
	}
	if err := svc.TenantBarrierUnseal(ctx, "acme", []byte("k")); !isForbidden(err) {
		t.Errorf("Unseal: expected ErrForbidden for unauthenticated context, got %v", err)
	}
	if _, err := svc.TenantBarrierQuorumSubmit(ctx, unsealSubmit("acme", "share")); !isForbidden(err) {
		t.Errorf("QuorumSubmit: expected ErrForbidden for unauthenticated context, got %v", err)
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
	_, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: nil, Threshold: 0, Shares: 3, Strategy: "shamir"})
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
	_, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: nil, Threshold: 2, Shares: 0, Strategy: "shamir"})
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
	_, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: nil, Threshold: 5, Shares: 3, Strategy: "shamir"})
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
	if _, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: nil, Threshold: 3, Shares: 3, Strategy: "shamir"}); err != nil {
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
			if _, err := svc.TenantBarrierInit(tenantCtx("acme"), &qrdbtransport.TenantBarrierInitRequest{TenantID: "acme", Passphrase: []byte("pw"), Threshold: 0, Shares: 0, Strategy: strategy}); err != nil {
				t.Fatalf("strategy=%s: unexpected error for zero threshold/shares: %v", strategy, err)
			}
		})
	}
}

func (*mockTenantClient) AddTrustAnchors(ctx context.Context, req *qrdbtransport.AddTrustAnchorsRequest) (*qrdbtransport.TrustAnchorList, error) {
	return nil, nil
}

func (*mockTenantClient) ListTrustAnchors(ctx context.Context, tenantID string) (*qrdbtransport.TrustAnchorList, error) {
	return nil, nil
}

func (*mockTenantClient) RemoveTrustAnchor(ctx context.Context, tenantID, fingerprint string) error {
	return nil
}

func (*mockTenantClient) TenantBarrierQuorumBegin(ctx context.Context, req *qrdbtransport.TenantQuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	return nil, nil
}

func (*mockTenantClient) TenantBarrierRekeyStart(ctx context.Context, req *qrdbtransport.TenantRekeyStartRequest) (*qrdbtransport.QuorumRound, error) {
	return nil, nil
}

func (*mockTenantClient) TenantBarrierOfficerRevoke(ctx context.Context, req *qrdbtransport.TenantOfficerRevokeRequest) (*qrdbtransport.QuorumRound, error) {
	return nil, nil
}
