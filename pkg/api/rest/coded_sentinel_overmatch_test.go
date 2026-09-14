//go:build rest

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

package rest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// These tests cover go-quicbaas#434: every Err* value in pkg/iam and
// pkg/service is a *dberrors.QBError, an alias for go-qrdb's *QRDBError, whose
// Is method compares the error CODE and not the sentinel. Each test drives a
// handler with an error that carries the SAME CODE as the sentinel the handler
// names, and asserts the sentinel's branch is not taken.

// selfSignedPIVCertPEM returns a syntactically valid client certificate that no
// root vouches for. PIVActivator parses it and then fails chain verification
// against the fixture's empty pool, which is the ErrPIVCertRejected path.
func selfSignedPIVCertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(4340),
		Subject:      pkix.Name{CommonName: "Untrusted PIV Card"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// TestActivatePIV_RejectedCertificateIsNotAnUnusableInvite proves the 422
// branch is reachable.
//
// ErrPIVCertRejected carries Forbidden -- the same code as ErrInviteExpired and
// ErrInviteUsed -- so the code-comparing errors.Is in the first case matched a
// rejected certificate before the piv_cert_rejected case could run. Every
// failed chain verification was reported as an unusable invite, and the branch
// written for it was dead code.
func TestActivatePIV_RejectedCertificateIsNotAnUnusableInvite(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	body, err := json.Marshal(map[string]string{"cert_pem": selfSignedPIVCertPEM(t)})
	require.NoError(t, err)

	w := f.call(t, http.MethodPost, "/activate/piv", string(body), f.handler.ActivatePIV)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code,
		"a rejected PIV certificate was answered as an unusable invite: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "piv_cert_rejected")
	assert.NotContains(t, w.Body.String(), "invite link is not valid")
}

// TestActivatePIV_UnusableInviteStillCollapses is the other half: narrowing the
// classification must not stop a spent token from getting the opaque answer.
func TestActivatePIV_UnusableInviteStillCollapses(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	_, err := f.invites.Redeem(context.Background(), inviteTenant, f.token)
	require.NoError(t, err)

	body, err := json.Marshal(map[string]string{"cert_pem": selfSignedPIVCertPEM(t)})
	require.NoError(t, err)

	w := f.call(t, http.MethodPost, "/activate/piv", string(body), f.handler.ActivatePIV)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "invite link is not valid")
}

// TestRespondInviteLookupError_ForeignCodesKeepTheirOwnStatus proves the lookup
// bridge honours its own contract: only the invitation's own state collapses
// into the opaque answer, and a store failure keeps its status.
//
// The three invite sentinels carry NotFound and Forbidden. errors.Is compared
// codes, so a suspended tenant (Forbidden) and a store answering not-found were
// both reported to the invitee as a bad link -- an outage rendered as a
// mistyped URL, which is exactly what the helper's comment forbids.
func TestRespondInviteLookupError_ForeignCodesKeepTheirOwnStatus(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	const opaqueBody = "invite link is not valid"

	tests := []struct {
		name string
		err  error
	}{
		{"forbidden from the tenant manager", iam.ErrTenantSuspended},
		{"forbidden from the store", &dberrors.QBError{
			Code: dberrors.ErrForbidden,
			Op:   "iam.invite.lookup",
			Err:  &iam.IAMMessageError{Message: "barrier is sealed"},
		}},
		{"not found from the store", &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.invite.lookup",
			Err:  &iam.IAMMessageError{Message: "tenant shard is absent"},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/iam/acme/invites/tok", nil)

			require.True(t, f.handler.respondInviteLookupError(w, r, tc.err),
				"a non-nil error was reported as handled=false")
			assert.NotContains(t, w.Body.String(), opaqueBody,
				"a store failure was reported to the invitee as a bad link")
		})
	}
}

// TestRespondInviteLookupError_InviteStateStillCollapses is the positive half:
// each of the three invite conditions still gets the single opaque answer, so
// the enumeration oracle stays closed.
func TestRespondInviteLookupError_InviteStateStillCollapses(t *testing.T) {
	f, cleanup := setupActivation(t, writeSucceeds)
	defer cleanup()

	bodies := make([]string, 0, 3)
	for _, err := range []error{iam.ErrInviteNotFound, iam.ErrInviteExpired, iam.ErrInviteUsed} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/iam/acme/invites/tok", nil)
		require.True(t, f.handler.respondInviteLookupError(w, r, err))
		assert.Equal(t, http.StatusNotFound, w.Code)
		bodies = append(bodies, w.Body.String())
	}
	assert.Equal(t, bodies[0], bodies[1], "expired is distinguishable from absent")
	assert.Equal(t, bodies[0], bodies[2], "spent is distinguishable from absent")
}

// notFoundMFAManager answers GetBackupCodeStatus with a chosen error.
type notFoundMFAManager struct {
	*fakeMFAManager
	err error
}

func (m notFoundMFAManager) GetBackupCodeStatus(context.Context, string, string) (*iam.BackupCodeStatus, error) {
	return nil, m.err
}

// TestGetBackupCodeStatus_ForeignNotFoundIsNotZeroCodes proves a failure to
// READ the enrollment is not reported as the enrollment being absent.
//
// ErrMFANotEnrolled carries NotFound, so the code-comparing errors.Is turned
// EVERY not-found the MFA store could raise into an HTTP 200 saying the account
// holds zero backup codes -- a security fact the handler had not established.
func TestGetBackupCodeStatus_ForeignNotFoundIsNotZeroCodes(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	tests := []struct {
		name string
		err  error
	}{
		{"absent user", iam.ErrUserNotFound},
		{"store answered not found", &dberrors.QBError{
			Code: dberrors.ErrNotFound,
			Op:   "iam.mfa.storage.get",
			Err:  &iam.IAMMessageError{Message: "mfa shard is absent"},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mfaMockService{
				MockQuicBaaS: testutil.NewMockQuicBaaS(),
				manager:      notFoundMFAManager{fakeMFAManager: &fakeMFAManager{}, err: tc.err},
			}
			cfg := DefaultConfig()
			cfg.Address = ":0"
			server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
			require.NoError(t, err)
			defer server.Close()
			server.service = mockSvc

			handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
			w := httptest.NewRecorder()
			handler.GetBackupCodeStatus(w, mfaRequest(t, http.MethodGet, nil,
				map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser}))

			assert.NotEqual(t, http.StatusOK, w.Code,
				"a failed backup-code read was reported as success: %s", w.Body.String())
			assert.NotContains(t, w.Body.String(), `"remaining_codes":0`,
				"a failed backup-code read reported zero remaining codes")
		})
	}
}

// TestGetBackupCodeStatus_NotEnrolledStillReportsZero is the positive half: the
// one condition the branch exists for still answers 200 with zeros.
func TestGetBackupCodeStatus_NotEnrolledStillReportsZero(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &mfaMockService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		manager:      notFoundMFAManager{fakeMFAManager: &fakeMFAManager{}, err: iam.ErrMFANotEnrolled},
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	defer server.Close()
	server.service = mockSvc

	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	w := httptest.NewRecorder()
	handler.GetBackupCodeStatus(w, mfaRequest(t, http.MethodGet, nil,
		map[string]string{"tenant_id": mfaTenant, "user_id": mfaUser}))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp BackupCodeStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Zero(t, resp.TotalCodes)
	assert.Zero(t, resp.RemainingCodes)
}

// TestRespondXKMSFIDO2Error_AuthenticationStaysOpaque proves the two named
// exceptions to the opaque-401 rule are exactly the conditions they name.
//
// ErrFIDO2TenantRequired and ErrFIDO2NilRequest carry InvalidInput and
// ErrFIDO2NotConfigured carries Unavailable, so the code-comparing errors.Is
// admitted any failure with those codes into a branch that renders the error
// verbatim -- on an authentication route, where the handler's own contract says
// a failed proof must not describe why it failed.
func TestRespondXKMSFIDO2Error_AuthenticationStaysOpaque(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"invalid input from the ceremony", &dberrors.QBError{
			Code: dberrors.ErrInvalidInput,
			Op:   "webauthn.FinishAuthentication",
			Err:  &iam.IAMMessageError{Message: "assertion is for an unknown credential id"},
		}},
		{"invalid tenant id", iam.ErrInvalidTenantID},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			respondXKMSFIDO2Error(w, "FinishAuthentication", tc.err, true)

			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"a failed authentication answered with something other than the opaque 401")
			assert.Contains(t, w.Body.String(), "authentication failed")
			assert.NotContains(t, strings.ToLower(w.Body.String()), "credential id",
				"the failed proof described why it failed")
		})
	}
}

// TestRespondXKMSFIDO2Error_NamedConditionsKeepTheirStatus is the positive
// half: the three service-level conditions still get 503 and 400, on
// authentication routes included.
func TestRespondXKMSFIDO2Error_NamedConditionsKeepTheirStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not configured", service.ErrFIDO2NotConfigured, http.StatusServiceUnavailable},
		{"tenant required", service.ErrFIDO2TenantRequired, http.StatusBadRequest},
		{"nil request", service.ErrFIDO2NilRequest, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			respondXKMSFIDO2Error(w, "FinishAuthentication", tc.err, true)
			assert.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}
}
