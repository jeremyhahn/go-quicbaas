//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// seedPendingUser inserts a user into the IAM memory storage so the
// approval/rejection handlers can transition its lifecycle state.
func seedPendingUser(t *testing.T, storage *iam.MemoryStorage, tenantID, userID, email string) {
	t.Helper()
	err := storage.CreateUser(context.Background(), &iam.User{
		ID:        userID,
		TenantID:  tenantID,
		Email:     email,
		Name:      "Pending User",
		Lifecycle: iam.LifecyclePending,
	})
	require.NoError(t, err)
}

// TestApproveUser_Success drives the happy path of ApproveUser: an existing
// pending user is transitioned to active and a 200 status with the approval
// envelope is returned.
func TestApproveUser_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	const tenantID = "acme"
	const userID = "75"
	seedPendingUser(t, memStorage, tenantID, userID, "pending-1@example.com")

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/iam/users/"+userID+"/approve?tenant_id="+tenantID, nil)
	req = withChiParams(req, map[string]string{"user_id": userID})
	w := httptest.NewRecorder()

	handler.ApproveUser(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "approved", resp["status"])
	assert.Equal(t, userID, resp["user_id"])
}

// TestRejectUser_Success drives the happy path of RejectUser: an existing
// pending user is transitioned to disabled and a 200 status is returned, with
// the optional JSON reason body parsed without error.
func TestRejectUser_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	const tenantID = "acme"
	const userID = "76"
	seedPendingUser(t, memStorage, tenantID, userID, "pending-2@example.com")

	body := strings.NewReader(`{"reason":"spam signup"}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/iam/users/"+userID+"/reject?tenant_id="+tenantID, body)
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"user_id": userID})
	w := httptest.NewRecorder()

	handler.RejectUser(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "rejected", resp["status"])
	assert.Equal(t, userID, resp["user_id"])
}
