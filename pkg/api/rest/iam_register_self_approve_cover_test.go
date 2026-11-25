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
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// seedPendingUser inserts a user into the IAM memory storage so the
// approval/rejection handlers can transition its lifecycle state.
func seedPendingUser(t *testing.T, storage *iam.MemoryStorage, tenantID, userID, email string) {
	t.Helper()
	id, err := strconv.ParseUint(userID, 10, 64)
	require.NoError(t, err)
	err = storage.CreateUser(context.Background(), &iam.User{
		ID:        id,
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
	req = req.WithContext(tenantctx.WithTenant(req.Context(), tenantID))
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
	req = req.WithContext(tenantctx.WithTenant(req.Context(), tenantID))
	req = withChiParams(req, map[string]string{"user_id": userID})
	w := httptest.NewRecorder()

	handler.RejectUser(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "rejected", resp["status"])
	assert.Equal(t, userID, resp["user_id"])
}

// TestApproveAndReject_AnotherTenantsUserIsRefused pins that approval and
// rejection act only in the caller's own tenant: a query naming another
// tenant is refused and the user stays pending.
func TestApproveAndReject_AnotherTenantsUserIsRefused(t *testing.T) {
	const userID = "77"
	callers := map[string]func(target string, body io.Reader) *http.Request{
		"tenant caller": func(target string, body io.Reader) *http.Request {
			return tenantRequest("tenant-a", http.MethodPost, target, body)
		},
		"system caller": func(target string, body io.Reader) *http.Request {
			r := httptest.NewRequest(http.MethodPost, target, body)
			return r.WithContext(tenantctx.WithSystem(r.Context()))
		},
	}
	actions := map[string]func(h *IAMHandler) http.HandlerFunc{
		"approve": func(h *IAMHandler) http.HandlerFunc { return h.ApproveUser },
		"reject":  func(h *IAMHandler) http.HandlerFunc { return h.RejectUser },
	}
	for callerName, newRequest := range callers {
		for action, handlerFor := range actions {
			t.Run(callerName+" "+action, func(t *testing.T) {
				_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
				defer cleanup()
				seedPendingUser(t, memStorage, "tenant-b", userID, "other-tenant@example.com")

				req := newRequest("/api/v1/iam/users/"+userID+"/"+action+"?tenant_id=tenant-b",
					strings.NewReader(`{"reason":"cross tenant"}`))
				req.Header.Set("Content-Type", "application/json")
				req = withChiParams(req, map[string]string{"user_id": userID})
				w := httptest.NewRecorder()

				handlerFor(handler)(w, req)

				assert.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
				stored, err := memStorage.GetUser(context.Background(), "tenant-b", 77)
				require.NoError(t, err)
				assert.Equal(t, iam.LifecyclePending, stored.Lifecycle, "another tenant's user must stay pending")
			})
		}
	}
}
