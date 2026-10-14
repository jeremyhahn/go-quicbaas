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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const (
	mfaOwnerTenant = "tenant-mfa-owner"
	mfaOtherTenant = "tenant-mfa-other"
	mfaChallengeID = "challenge-owner"

	// mfaOwnerUser and mfaOwnerUserParam name the same user: the manager
	// keys by uint64, the URL carries its decimal form.
	mfaOwnerUser      uint64 = 50101
	mfaOwnerUserParam        = "50101"
	mfaOtherUser             = "50102"
)

// setupRealMFAHandler serves the MFA routes from a real MFA manager over an
// in-memory store, returning the store so a test can inspect what survived.
func setupRealMFAHandler(t *testing.T) (*IAMHandler, *iam.DefaultMFAManager, iam.MFAStorage) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	storage := iam.NewMemoryMFAStorage()
	mgr, err := iam.NewMFAManager(iam.WithMFAStorage(storage))
	require.NoError(t, err)

	mockSvc := &mfaMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: mgr}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	t.Cleanup(func() { server.Close() })

	return NewIAMHandler(IAMHandlerDependencies{Server: server}), mgr, storage
}

// TestMFAObjectVerbs_OtherTenantOrUserIsNotFound proves the REST MFA
// complete/cancel/verify verbs only reach an enrollment or challenge owned by
// the tenant and user named in the URL: anyone else gets 404, the record
// survives, and the owner still reaches it.
func TestMFAObjectVerbs_OtherTenantOrUserIsNotFound(t *testing.T) {
	ctx := context.Background()
	handler, mgr, storage := setupRealMFAHandler(t)

	enrollment, err := mgr.BeginEnrollment(ctx, mfaOwnerUser, mfaOwnerTenant, iam.MFAMethodTOTP)
	require.NoError(t, err)
	require.NoError(t, storage.SaveChallenge(ctx, &iam.MFAChallenge{
		ID:           mfaChallengeID,
		UserID:       mfaOwnerUser,
		TenantID:     mfaOwnerTenant,
		SessionToken: "partial-session",
		Method:       iam.MFAMethodTOTP,
		ExpiresAt:    time.Now().Add(5 * time.Minute),
		MaxAttempts:  5,
		Created:      time.Now(),
	}))

	intruders := map[string]map[string]string{
		"other tenant": {"tenant_id": mfaOtherTenant, "user_id": mfaOwnerUserParam},
		"other user":   {"tenant_id": mfaOwnerTenant, "user_id": mfaOtherUser},
	}
	for name, p := range intruders {
		w := httptest.NewRecorder()
		handler.CompleteMFAEnrollment(w, mfaRequest(t, http.MethodPost,
			map[string]string{"enrollment_id": enrollment.EnrollmentID, "code": "000000"}, p))
		assert.Equal(t, http.StatusNotFound, w.Code, "complete as %s: %s", name, w.Body.String())

		w = httptest.NewRecorder()
		handler.VerifyMFAChallenge(w, mfaRequest(t, http.MethodPost,
			map[string]string{"challenge_id": mfaChallengeID, "code": "000000"}, p))
		assert.Equal(t, http.StatusNotFound, w.Code, "verify as %s: %s", name, w.Body.String())

		w = httptest.NewRecorder()
		handler.CancelMFAEnrollment(w, mfaRequest(t, http.MethodDelete, nil,
			map[string]string{"tenant_id": p["tenant_id"], "user_id": p["user_id"], "enrollment_id": enrollment.EnrollmentID}))
		assert.Equal(t, http.StatusNotFound, w.Code, "cancel as %s: %s", name, w.Body.String())
	}

	stored, err := storage.GetEnrollment(ctx, enrollment.EnrollmentID)
	require.NoError(t, err, "refused verbs must leave the enrollment in place")
	assert.Zero(t, stored.Attempts, "refused verbs must not spend the owner's attempts")
	challenge, err := storage.GetChallenge(ctx, mfaChallengeID)
	require.NoError(t, err, "refused verifies must leave the challenge in place")
	assert.Zero(t, challenge.Attempts, "refused verifies must not spend the owner's attempts")

	owner := map[string]string{"tenant_id": mfaOwnerTenant, "user_id": mfaOwnerUserParam}

	// A wrong code from the owner reaches the record and spends an attempt,
	// which only happens once the tenant and user binding has passed.
	w := httptest.NewRecorder()
	handler.VerifyMFAChallenge(w, mfaRequest(t, http.MethodPost,
		map[string]string{"challenge_id": mfaChallengeID, "code": "000000"}, owner))
	assert.NotEqual(t, http.StatusNotFound, w.Code, w.Body.String())
	challenge, err = storage.GetChallenge(ctx, mfaChallengeID)
	require.NoError(t, err)
	assert.Equal(t, 1, challenge.Attempts)

	w = httptest.NewRecorder()
	handler.CancelMFAEnrollment(w, mfaRequest(t, http.MethodDelete, nil,
		map[string]string{"tenant_id": mfaOwnerTenant, "user_id": mfaOwnerUserParam, "enrollment_id": enrollment.EnrollmentID}))
	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	_, err = storage.GetEnrollment(ctx, enrollment.EnrollmentID)
	assert.ErrorIs(t, err, iam.ErrMFAEnrollmentNotFound)
}
