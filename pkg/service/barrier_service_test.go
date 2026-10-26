// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Mock transport.BarrierClient for unit testing the BarrierService facade.
// =============================================================================

// errBarrierBackend is a typed sentinel error returned by the mock client to
// exercise the error-propagation branches of the BarrierService facade.
type barrierBackendError struct{}

func (e *barrierBackendError) Error() string { return "barrier backend failure" }

var errBarrierBackend = &barrierBackendError{}

// mockBarrierClient implements transport.BarrierClient. Each method returns the
// configured response/error, allowing tests to drive both happy and error
// paths through the facade without a live qrdb transport. The quorum verbs
// share quorumErr.
type mockBarrierClient struct {
	statusResp     *transport.BarrierStatusResponse
	statusErr      error
	initErr        error
	unsealErr      error
	sealErr        error
	sealCalls      int
	initShamirResp *transport.BarrierInitializeShamirResponse
	initShamirErr  error
	recoveryResp   *transport.BarrierRecoveryKeysResponse
	recoveryErr    error
	delRecoveryErr error
	hasRecResp     *transport.BarrierHasRecoveryKeysResponse
	hasRecErr      error
	challenge      *transport.QuorumChallenge
	outcome        *transport.QuorumOutcome
	round          *transport.QuorumRound
	quorumErr      error
}

func (m *mockBarrierClient) BarrierInitialize(_ context.Context, _ *transport.BarrierInitializeRequest) error {
	return m.initErr
}

func (m *mockBarrierClient) BarrierUnseal(_ context.Context, _ *transport.BarrierUnsealRequest) error {
	return m.unsealErr
}

func (m *mockBarrierClient) BarrierSeal(_ context.Context) error {
	m.sealCalls++
	return m.sealErr
}

func (m *mockBarrierClient) BarrierStatus(_ context.Context) (*transport.BarrierStatusResponse, error) {
	return m.statusResp, m.statusErr
}

func (m *mockBarrierClient) BarrierInitializeShamir(_ context.Context, _ *transport.BarrierInitializeShamirRequest) (*transport.BarrierInitializeShamirResponse, error) {
	return m.initShamirResp, m.initShamirErr
}

func (m *mockBarrierClient) BarrierGenerateRecoveryKeys(_ context.Context, _ *transport.BarrierGenerateRecoveryKeysRequest) (*transport.BarrierRecoveryKeysResponse, error) {
	return m.recoveryResp, m.recoveryErr
}

func (m *mockBarrierClient) BarrierDeleteRecoveryKeys(_ context.Context) error {
	return m.delRecoveryErr
}

func (m *mockBarrierClient) BarrierHasRecoveryKeys(_ context.Context) (*transport.BarrierHasRecoveryKeysResponse, error) {
	return m.hasRecResp, m.hasRecErr
}

func (m *mockBarrierClient) BarrierQuorumBegin(_ context.Context, _ *transport.QuorumBeginRequest) (*transport.QuorumChallenge, error) {
	return m.challenge, m.quorumErr
}

func (m *mockBarrierClient) BarrierQuorumSubmit(_ context.Context, _ *transport.QuorumSubmitRequest) (*transport.QuorumOutcome, error) {
	return m.outcome, m.quorumErr
}

func (m *mockBarrierClient) BarrierRekeyStart(_ context.Context, _ *transport.RekeyStartRequest) (*transport.QuorumRound, error) {
	return m.round, m.quorumErr
}

func (m *mockBarrierClient) BarrierOfficerRevoke(_ context.Context, _ *transport.OfficerRevokeRequest) (*transport.QuorumRound, error) {
	return m.round, m.quorumErr
}

func (*mockBarrierClient) BarrierClusterUnseal(ctx context.Context) (*transport.ClusterSeal, error) {
	return nil, nil
}

func (*mockBarrierClient) BarrierEnableEncryption(ctx context.Context, req *transport.BarrierEnableEncryptionRequest) (*transport.BarrierEnableEncryptionResponse, error) {
	return nil, nil
}

func (*mockBarrierClient) OfficerRegister(ctx context.Context, req *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error) {
	return nil, nil
}

func (*mockBarrierClient) OfficerList(ctx context.Context) ([]*transport.OfficerStatus, error) {
	return nil, nil
}

func (*mockBarrierClient) ShareClaimBegin(ctx context.Context, req *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error) {
	return nil, nil
}

func (*mockBarrierClient) ShareClaimComplete(ctx context.Context, req *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error) {
	return nil, nil
}

func (*mockBarrierClient) RecoveryOfficerRegister(ctx context.Context, req *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error) {
	return nil, nil
}

func (*mockBarrierClient) RecoveryShareClaimBegin(ctx context.Context, req *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error) {
	return nil, nil
}

func (*mockBarrierClient) RecoveryShareClaimComplete(ctx context.Context, req *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error) {
	return nil, nil
}

var _ transport.BarrierClient = (*mockBarrierClient)(nil)

// =============================================================================
// Missing-client guard: every facade method must short-circuit with
// BarrierServiceMissingError when constructed with a nil client.
// =============================================================================

func TestBarrierService_NilClient_ReturnsMissingError(t *testing.T) {
	svc := service.NewBarrierService(nil)
	ctx := context.Background()

	var missing *service.BarrierServiceMissingError
	assertMissing := func(err error) {
		t.Helper()
		require.Error(t, err)
		assert.ErrorAs(t, err, &missing)
		assert.Equal(t, "barrier service not configured", err.Error())
	}

	_, err := svc.BarrierStatus(ctx)
	assertMissing(err)

	assertMissing(svc.BarrierInitialize(ctx, []byte("secret")))
	assertMissing(svc.BarrierUnseal(ctx, []byte("secret")))
	assertMissing(svc.BarrierSeal(ctx, &service.BarrierSealRequest{}))

	// BarrierInitializeShamir refuses with the ceremony guidance instead;
	// see TestBarrierService_BarrierInitializeShamir_RefusedWithoutClient.

	_, err = svc.BarrierGenerateRecoveryKeys(ctx, &transport.BarrierGenerateRecoveryKeysRequest{Threshold: 2})
	assertMissing(err)

	assertMissing(svc.BarrierDeleteRecoveryKeys(ctx))

	_, err = svc.BarrierHasRecoveryKeys(ctx)
	assertMissing(err)

	_, err = svc.BarrierQuorumBegin(ctx, &transport.QuorumBeginRequest{Domain: transport.DomainUnseal})
	assertMissing(err)

	_, err = svc.BarrierQuorumSubmit(ctx, &transport.QuorumSubmitRequest{Domain: transport.DomainUnseal})
	assertMissing(err)

	_, err = svc.BarrierRekeyStart(ctx, &transport.RekeyStartRequest{Domain: transport.DomainUnseal})
	assertMissing(err)

	_, err = svc.BarrierOfficerRevoke(ctx, &transport.OfficerRevokeRequest{Domain: transport.DomainUnseal})
	assertMissing(err)
}

// =============================================================================
// BarrierStatus
// =============================================================================

func TestBarrierService_BarrierStatus_Success(t *testing.T) {
	initAt := "2026-05-29T10:00:00Z"
	mc := &mockBarrierClient{
		statusResp: &transport.BarrierStatusResponse{
			Sealed:         true,
			Strategy:       "shamir",
			HardwareBacked: true,
			InitializedAt:  initAt,
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierStatus(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.True(t, info.Sealed)
	assert.Equal(t, "shamir", info.Strategy)
	assert.True(t, info.HardwareBacked)

	want, parseErr := time.Parse(time.RFC3339, initAt)
	require.NoError(t, parseErr)
	assert.True(t, info.InitializedAt.Equal(want))
}

// TestBarrierService_BarrierStatus_SealedKeepsInitialized covers qrdb's
// sealed system-barrier status: it reports sealed and initialized with no
// initialized_at, so the service must carry the explicit flag (#1916).
func TestBarrierService_BarrierStatus_SealedKeepsInitialized(t *testing.T) {
	mc := &mockBarrierClient{
		statusResp: &transport.BarrierStatusResponse{
			Sealed:      true,
			Initialized: true,
			Strategy:    "shamir",
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierStatus(context.Background())
	require.NoError(t, err)
	assert.True(t, info.Sealed)
	assert.True(t, info.Initialized)
	assert.True(t, info.InitializedAt.IsZero())
}

func TestBarrierService_BarrierStatus_Uninitialized(t *testing.T) {
	mc := &mockBarrierClient{
		statusResp: &transport.BarrierStatusResponse{Sealed: true, Strategy: "shamir"},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierStatus(context.Background())
	require.NoError(t, err)
	assert.False(t, info.Initialized)
}

func TestBarrierService_BarrierStatus_InvalidTimestampIgnored(t *testing.T) {
	mc := &mockBarrierClient{
		statusResp: &transport.BarrierStatusResponse{
			Sealed:        false,
			Strategy:      "passphrase",
			InitializedAt: "not-a-timestamp",
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierStatus(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	// An unparseable timestamp must leave InitializedAt at its zero value
	// rather than propagating a parse error.
	assert.True(t, info.InitializedAt.IsZero())
}

func TestBarrierService_BarrierStatus_Error(t *testing.T) {
	mc := &mockBarrierClient{statusErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierStatus(context.Background())
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errBarrierBackend)
}

// =============================================================================
// BarrierInitialize / Unseal / Seal
// =============================================================================

func TestBarrierService_BarrierInitialize(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierInitialize(context.Background(), []byte("secret")))

	mc.initErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierInitialize(context.Background(), []byte("secret")), errBarrierBackend)
}

func TestBarrierService_BarrierUnseal(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierUnseal(context.Background(), []byte("secret")))

	mc.unsealErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierUnseal(context.Background(), []byte("secret")), errBarrierBackend)
}

func TestBarrierService_BarrierSeal(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierSeal(context.Background(), nil))
	assert.Equal(t, 1, mc.sealCalls)

	mc.sealErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierSeal(context.Background(), &service.BarrierSealRequest{}), errBarrierBackend)
}

// newSOPINBackend returns an in-memory qrdb software PIN backend, with
// soPIN set as its SO PIN when soPIN is not empty.
func newSOPINBackend(t *testing.T, soPIN string) qrdbsdk.PINBackend {
	t.Helper()
	backend, err := qrdbsdk.NewSoftwareBackend(nil, qrdbsdk.DefaultHashConfig())
	require.NoError(t, err)
	if soPIN != "" {
		require.NoError(t, backend.SetSOPIN("", soPIN))
	}
	return backend
}

func TestBarrierService_BarrierSeal_VerifiesSOPIN(t *testing.T) {
	const soPIN = "so-pin-1234"
	backend := newSOPINBackend(t, soPIN)

	tests := []struct {
		name    string
		req     *service.BarrierSealRequest
		wantErr error
	}{
		{name: "right PIN seals", req: &service.BarrierSealRequest{SOPIN: []byte(soPIN)}},
		{name: "nil request is refused", req: nil, wantErr: qrdbsdk.ErrSOPINRequired},
		{name: "empty PIN is refused", req: &service.BarrierSealRequest{}, wantErr: qrdbsdk.ErrSOPINRequired},
		{name: "wrong PIN is refused", req: &service.BarrierSealRequest{SOPIN: []byte("not-the-pin")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockBarrierClient{}
			svc := service.NewBarrierService(mc)
			svc.SetPINBackend(backend)

			err := svc.BarrierSeal(context.Background(), tt.req)
			if tt.req != nil && string(tt.req.SOPIN) == soPIN {
				require.NoError(t, err)
				assert.Equal(t, 1, mc.sealCalls)
				return
			}
			var qbErr *dberrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, dberrors.ErrForbidden, qbErr.Code)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.Zero(t, mc.sealCalls, "a refused seal must leave the barrier untouched")
		})
	}
}

func TestBarrierService_BarrierSeal_NoSOPINSetIsUngated(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	svc.SetPINBackend(newSOPINBackend(t, ""))

	require.NoError(t, svc.BarrierSeal(context.Background(), &service.BarrierSealRequest{}))
	assert.Equal(t, 1, mc.sealCalls)
}

func TestBarrierService_SetPINBackend_NilClearsTheGate(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	svc.SetPINBackend(newSOPINBackend(t, "so-pin-1234"))
	require.Error(t, svc.BarrierSeal(context.Background(), nil))

	svc.SetPINBackend(nil)
	require.NoError(t, svc.BarrierSeal(context.Background(), nil))
	assert.Equal(t, 1, mc.sealCalls)
}

// =============================================================================
// Shamir initialize
// =============================================================================

// assertSystemShamirInitRefused checks that err is the typed ceremony refusal
// carried by a PreconditionFailed QBError.
func assertSystemShamirInitRefused(t *testing.T, err error) {
	t.Helper()
	var refused *sdkerrors.SystemShamirInitRefusedError
	require.ErrorAs(t, err, &refused)
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrPreconditionFailed, qbErr.Code)
	assert.Contains(t, err.Error(), "quicbaas init initialize")
}

func TestBarrierService_BarrierInitializeShamir_RefusedWithClient(t *testing.T) {
	mc := &mockBarrierClient{
		initShamirResp: &transport.BarrierInitializeShamirResponse{Threshold: 2, TotalShares: 3, ShareCount: 3},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierInitializeShamir(context.Background(), &transport.BarrierInitializeShamirRequest{Threshold: 2, TotalShares: 3})
	assert.Nil(t, info)
	assertSystemShamirInitRefused(t, err)
}

func TestBarrierService_BarrierInitializeShamir_RefusedWithoutClient(t *testing.T) {
	svc := service.NewBarrierService(nil)

	info, err := svc.BarrierInitializeShamir(context.Background(), &transport.BarrierInitializeShamirRequest{Threshold: 2, TotalShares: 3})
	assert.Nil(t, info)
	assertSystemShamirInitRefused(t, err)
}

// =============================================================================
// Quorum rounds: unseal, root token, rekey, recovery, holder revocation
// =============================================================================

func TestBarrierService_BarrierQuorumBegin(t *testing.T) {
	want := &transport.QuorumChallenge{ChallengeID: "c-1", Domain: transport.DomainUnseal, Op: transport.OpUnseal}
	mc := &mockBarrierClient{challenge: want}
	svc := service.NewBarrierService(mc)

	got, err := svc.BarrierQuorumBegin(context.Background(), &transport.QuorumBeginRequest{
		Domain: transport.DomainUnseal, Op: transport.OpUnseal, Username: "alice", Factor: "mtls",
	})
	require.NoError(t, err)
	assert.Same(t, want, got)

	mc.quorumErr = errBarrierBackend
	_, err = svc.BarrierQuorumBegin(context.Background(), &transport.QuorumBeginRequest{Domain: transport.DomainUnseal})
	assert.ErrorIs(t, err, errBarrierBackend)
}

func TestBarrierService_BarrierQuorumSubmit(t *testing.T) {
	want := &transport.QuorumOutcome{Domain: transport.DomainUnseal, Op: transport.OpRootToken, Required: 2, Submitted: 2, Complete: true, RootToken: "root-token-hex"}
	mc := &mockBarrierClient{outcome: want}
	svc := service.NewBarrierService(mc)

	got, err := svc.BarrierQuorumSubmit(context.Background(), &transport.QuorumSubmitRequest{
		Domain: transport.DomainUnseal, Op: transport.OpRootToken, Username: "alice", ChallengeID: "c-1",
	})
	require.NoError(t, err)
	assert.Same(t, want, got)

	mc.quorumErr = errBarrierBackend
	got, err = svc.BarrierQuorumSubmit(context.Background(), &transport.QuorumSubmitRequest{Domain: transport.DomainUnseal})
	assert.ErrorIs(t, err, errBarrierBackend)
	assert.Same(t, want, got, "the facade returns qrdb's answer verbatim")
}

func TestBarrierService_BarrierRekeyStart(t *testing.T) {
	want := &transport.QuorumRound{Domain: transport.DomainUnseal, Op: transport.OpRekey, Required: 2, NewThreshold: 3}
	mc := &mockBarrierClient{round: want}
	svc := service.NewBarrierService(mc)

	got, err := svc.BarrierRekeyStart(context.Background(), &transport.RekeyStartRequest{Domain: transport.DomainUnseal, Threshold: 3})
	require.NoError(t, err)
	assert.Same(t, want, got)

	mc.quorumErr = errBarrierBackend
	_, err = svc.BarrierRekeyStart(context.Background(), &transport.RekeyStartRequest{Domain: transport.DomainUnseal})
	assert.ErrorIs(t, err, errBarrierBackend)
}

func TestBarrierService_BarrierOfficerRevoke(t *testing.T) {
	want := &transport.QuorumRound{Domain: transport.DomainRecovery, Op: transport.OpRekey, Remove: []string{"bob"}}
	mc := &mockBarrierClient{round: want}
	svc := service.NewBarrierService(mc)

	got, err := svc.BarrierOfficerRevoke(context.Background(), &transport.OfficerRevokeRequest{Domain: transport.DomainRecovery, Username: "bob"})
	require.NoError(t, err)
	assert.Same(t, want, got)

	mc.quorumErr = errBarrierBackend
	_, err = svc.BarrierOfficerRevoke(context.Background(), &transport.OfficerRevokeRequest{Domain: transport.DomainRecovery})
	assert.ErrorIs(t, err, errBarrierBackend)
}

// =============================================================================
// Recovery keys
// =============================================================================

func TestBarrierService_BarrierGenerateRecoveryKeys_Success(t *testing.T) {
	mc := &mockBarrierClient{
		recoveryResp: &transport.BarrierRecoveryKeysResponse{
			Threshold:  2,
			Total:      3,
			ShareCount: 3,
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierGenerateRecoveryKeys(context.Background(), &transport.BarrierGenerateRecoveryKeysRequest{Threshold: 2})
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, 2, info.Threshold)
	assert.Equal(t, 3, info.Total)
	assert.Equal(t, 3, info.ShareCount)
}

func TestBarrierService_BarrierGenerateRecoveryKeys_Error(t *testing.T) {
	mc := &mockBarrierClient{recoveryErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierGenerateRecoveryKeys(context.Background(), &transport.BarrierGenerateRecoveryKeysRequest{Threshold: 2})
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errBarrierBackend)
}

func TestBarrierService_BarrierDeleteRecoveryKeys(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierDeleteRecoveryKeys(context.Background()))

	mc.delRecoveryErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierDeleteRecoveryKeys(context.Background()), errBarrierBackend)
}

func TestBarrierService_BarrierHasRecoveryKeys_Success(t *testing.T) {
	mc := &mockBarrierClient{
		hasRecResp: &transport.BarrierHasRecoveryKeysResponse{HasKeys: true},
	}
	svc := service.NewBarrierService(mc)

	has, err := svc.BarrierHasRecoveryKeys(context.Background())
	require.NoError(t, err)
	assert.True(t, has)
}

func TestBarrierService_BarrierHasRecoveryKeys_Error(t *testing.T) {
	mc := &mockBarrierClient{hasRecErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	has, err := svc.BarrierHasRecoveryKeys(context.Background())
	require.Error(t, err)
	assert.False(t, has)
	assert.ErrorIs(t, err, errBarrierBackend)
}

// Guard against the sentinel being accidentally reclassified as a wrapped
// chain error; the facade returns it verbatim.
func TestBarrierService_ErrorIsVerbatim(t *testing.T) {
	mc := &mockBarrierClient{statusErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)
	_, err := svc.BarrierStatus(context.Background())
	assert.True(t, errors.Is(err, errBarrierBackend))
}
