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
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestUpdateUser_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
	require.NoError(t, memStorage.CreateUser(ctx, &iam.User{
		ID: 11, TenantID: "tenant1", Email: "user1@example.com", Name: "User 1",
	}))

	body := `{"name":"Renamed","enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(body)))
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "11"})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestUpdateUser_NotFound(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	require.NoError(t, memStorage.CreateTenant(context.Background(), &iam.Tenant{ID: "tenant1", Name: "T"}))

	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(`{"name":"x"}`)))
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "missing"})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestUpdateUser_Validation(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	// Invalid JSON
	w := httptest.NewRecorder()
	req := withChiParams(httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte("{bad"))),
		map[string]string{"tenant_id": "tenant1", "user_id": "1"})
	handler.UpdateUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing user_id
	w = httptest.NewRecorder()
	req = withChiParams(httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(`{}`))),
		map[string]string{"tenant_id": "tenant1", "user_id": ""})
	handler.UpdateUser(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListUsers_Success(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
	require.NoError(t, memStorage.CreateUser(ctx, &iam.User{ID: 1, TenantID: "tenant1", Email: "u1@example.com"}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/tenant1/users?limit=10&offset=0", nil)
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1"})
	w := httptest.NewRecorder()
	handler.ListUsers(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

// TestUpdateUser_RefusesTheTerminalDeletingState proves the guard at the
// transport that exposes it, not just at the store.
//
// This surface takes `lifecycle` as an ordinary update field, so a caller could
// name `deleting` — the state DeleteUser claims a record with before it removes
// the credentials, group memberships and sessions the account owns. Asserted
// here, the claim landed and the cascade did not: the account was permanently
// unable to authenticate (nothing transitions out of a terminal state) with
// everything it owns still live, and no later update could repair it.
func TestUpdateUser_RefusesTheTerminalDeletingState(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
	require.NoError(t, memStorage.CreateUser(ctx, &iam.User{
		ID: 11, TenantID: "tenant1", Email: "user1@example.com",
		Username: "user1", Name: "User 1", Lifecycle: iam.LifecycleActive,
	}))

	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(`{"lifecycle":"deleting"}`)))
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "11"})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code,
		"a deletion claim was accepted as an ordinary update: %s", w.Body.String())

	// Refused means the account is untouched, not "refused after writing".
	stored, err := memStorage.GetUser(ctx, "tenant1", 11)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, stored.Lifecycle,
		"the account was stranded in a terminal state by a refused update")
	assert.True(t, stored.CanAuthenticate())
}

// TestUpdateUser_StillAcceptsTheReversibleStates is the other half of the
// enumeration: the states that are statements about the account alone, each
// undoable by naming another one, must keep working through this surface. A
// guard that closed the hole by refusing every lifecycle change would pass the
// test above and break account administration.
//
// `pending` is absent deliberately and is covered separately below: it is
// NAMEABLE, but not reachable from `active` — an account that has been
// activated cannot become one that was never activated. That refusal comes from
// qrdb's transition table, which is a different check from this guard and is
// still the one that decides legality from where the record currently sits.
func TestUpdateUser_StillAcceptsTheReversibleStates(t *testing.T) {
	for _, state := range []iam.Lifecycle{
		iam.LifecycleActive,
		iam.LifecycleSuspended,
		iam.LifecycleLocked,
		iam.LifecycleDisabled,
	} {
		t.Run(string(state), func(t *testing.T) {
			_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
			defer cleanup()

			ctx := context.Background()
			require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
			require.NoError(t, memStorage.CreateUser(ctx, &iam.User{
				ID: 11, TenantID: "tenant1", Email: "user1@example.com",
				Username: "user1", Name: "User 1", Lifecycle: iam.LifecycleActive,
			}))

			body := `{"lifecycle":"` + string(state) + `"}`
			req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(body)))
			req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "11"})
			w := httptest.NewRecorder()
			handler.UpdateUser(w, req)
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

			stored, err := memStorage.GetUser(ctx, "tenant1", 11)
			require.NoError(t, err)
			assert.Equal(t, state, stored.Lifecycle)
		})
	}
}

// TestUpdateUser_PendingIsRefusedByTheTransitionTableNotTheGuard keeps the two
// checks distinguishable. The guard decides which states a caller may NAME;
// qrdb's transition table decides which moves are legal from where the record
// is. Conflating them would let a future change satisfy one and quietly drop
// the other, and each catches a case the other does not.
func TestUpdateUser_PendingIsRefusedByTheTransitionTableNotTheGuard(t *testing.T) {
	_, _, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "T"}))
	require.NoError(t, memStorage.CreateUser(ctx, &iam.User{
		ID: 11, TenantID: "tenant1", Email: "user1@example.com",
		Username: "user1", Name: "User 1", Lifecycle: iam.LifecycleActive,
	}))

	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte(`{"lifecycle":"pending"}`)))
	req = withChiParams(req, map[string]string{"tenant_id": "tenant1", "user_id": "11"})
	w := httptest.NewRecorder()
	handler.UpdateUser(w, req)

	require.NotEqual(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "illegal lifecycle transition",
		"pending must be refused as an illegal MOVE, not as an unnameable state")

	stored, err := memStorage.GetUser(ctx, "tenant1", 11)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, stored.Lifecycle)
}
