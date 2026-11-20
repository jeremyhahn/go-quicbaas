//go:build rest

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// IAM invitations take the tenant and the user from the authenticated
// principal and the invite record, never from a request body (#1832, #1836,
// owner ruling 133). A body that names another tenant or user is ignored on
// create, and an invite resolves only under the tenant it was issued in.
func TestCreateInvite_TenantAndUserComeFromPrincipalNotBody(t *testing.T) {
	handler, mem, cleanup := setupUserReadHandler(t)
	defer cleanup()
	_, err := iam.NewDefaultTenantManager(mem).CreateTenant(context.Background(),
		iam.CreateTenantOptions{ID: "globex", Name: "Globex"})
	require.NoError(t, err)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/users/",
		strings.NewReader(`{"tenant_id":"globex","user_id":"9001","id":9001,"email":"eve@acme.com","username":"eve"}`))
	req = req.WithContext(callerRouteContext(req.Context(), rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CreateUser(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "CreateUser failed: %s", w.Body.String())

	var created struct {
		ID          string `json:"id"`
		TenantID    string `json:"tenant_id"`
		InviteToken string `json:"invite_token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.InviteToken)
	assert.Equal(t, "acme", created.TenantID, "the body's tenant_id reached the created user")
	assert.NotEqual(t, "9001", created.ID, "the body's user id reached the created user")

	user, err := mem.GetUserByEmail(context.Background(), "acme", "eve@acme.com")
	require.NoError(t, err, "the user was not created in the principal's tenant")
	_, err = mem.GetUserByEmail(context.Background(), "globex", "eve@acme.com")
	assert.Error(t, err, "the body's tenant_id created a user in another tenant")

	getInvite := func(tenant string) *httptest.ResponseRecorder {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("tenant_id", tenant)
		rctx.URLParams.Add("token", created.InviteToken)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/"+tenant+"/invites/"+created.InviteToken, nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		handler.GetInvite(w, req)
		return w
	}

	// Accept: the invite is the invitee's credential and resolves only in the
	// tenant that issued it, for the user the record names.
	foreign := getInvite("globex")
	assert.NotEqual(t, http.StatusOK, foreign.Code, "acme's invite resolved under globex: %s", foreign.Body.String())

	own := getInvite("acme")
	require.Equal(t, http.StatusOK, own.Code, "GetInvite failed: %s", own.Body.String())
	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal(own.Body.Bytes(), &meta))
	assert.Equal(t, strconv.FormatUint(user.ID, 10), meta["user_id"], "the invite does not name the created user: %s", own.Body.String())
}
