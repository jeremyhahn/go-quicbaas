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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const exposureToken = "cleartext-invite-token-xyz789"

// identityOnlyService exposes an IdentityStore to the IAM handlers.
type identityOnlyService struct {
	*testutil.MockQuicBaaS
	identity *iam.IdentityStore
}

func (s *identityOnlyService) GetIdentityStore(context.Context) (*iam.IdentityStore, error) {
	return s.identity, nil
}

// TestCreateUser_StillExposesInviteTokenOnce proves the redaction did not cost
// the create response its one-time exposure. The token is minted once and only
// its digest is stored, so if the create response stops carrying it the account
// becomes unactivatable — the opposite failure from the one #294 fixes.
//
// It also pins the JSON precedence the handler relies on: invite_token is
// declared on the outer response struct while the embedded user carries a field
// of the same name, and encoding/json resolves that in favour of the shallower
// one.
func TestCreateUser_StillExposesInviteTokenOnce(t *testing.T) {
	handler, _, cleanup := setupUserReadHandler(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/users/",
		strings.NewReader(`{"email":"bob@acme.com","username":"bob","name":"Bob"}`))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "CreateUser failed: %s", w.Body.String())

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	token, _ := got["invite_token"].(string)
	assert.NotEmpty(t, token, "the create response dropped the one-time invite token: %s", w.Body.String())
	assert.NotEmpty(t, got["invite_url"], "the create response dropped the activation URL")
	assert.NotEmpty(t, got["invite_expires_at"],
		"the create response dropped the invite expiry, which is not redacted")

	// The token in the response is the one the invite store minted, not the
	// empty field left on the redacted user record.
	assert.NotEqual(t, exposureToken, token)
}

// setupUserReadHandler builds a handler over a store holding one pending user
// whose record carries a cleartext invite token, as the create handler leaves
// it.
//
// The MemoryStorage is returned as well so a test can read what was PERSISTED
// rather than what the response said: the two disagreeing is the defect class
// this fixture exists to catch.
func setupUserReadHandler(t *testing.T) (*IAMHandler, *iam.MemoryStorage, func()) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mem := iam.NewMemoryStorage()

	// ListUsers validates the tenant exists before it lists, so the fixture
	// needs a real tenant manager behind the store.
	tenants := iam.NewDefaultTenantManager(mem)
	_, err = tenants.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	// The user record has no invite fields to seed: the invite store owns the
	// token and its expiry. That is what makes the leak below impossible rather
	// than merely absent — there is no copy on the record for a read path to
	// serialize, whichever transport asks.
	require.NoError(t, mem.CreateUser(context.Background(), &iam.User{
		ID:        "77",
		TenantID:  "acme",
		Email:     "alice@acme.com",
		Name:      "alice",
		Lifecycle: iam.LifecyclePending,
	}))

	identity := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: mem,
		Tenants:     tenants,
	})

	mock := testutil.NewMockQuicBaaS()
	mock.InviteStoreOverride = iamtest.NewMemoryInviteStore()

	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(&identityOnlyService{
		MockQuicBaaS: mock,
		identity:     identity,
	}, cfg, log)
	require.NoError(t, err)

	return &IAMHandler{server: server}, mem, func() { server.Close() }
}

// TestGetUser_DoesNotExposeInviteToken is the #294 regression at the transport
// that leaked. GetUser responded with the raw user record, which used to carry
// the cleartext activation token until the account activated — so any caller
// authorized to READ a user could redeem that user's invite and register their
// own passkey against the account. Read authority on a user is not authority to
// become them.
//
// The record no longer holds a token at all, so this now guards the shape of the
// response rather than a redaction step: an invite_token field reappearing on
// the user payload is the defect, whether or not anything fills it in.
func TestGetUser_DoesNotExposeInviteToken(t *testing.T) {
	handler, _, cleanup := setupUserReadHandler(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	rctx.URLParams.Add("user_id", "77")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/acme/users/77", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetUser(w, req)

	require.Equal(t, http.StatusOK, w.Code, "GetUser failed: %s", w.Body.String())
	assertBodyHasNoInviteToken(t, w.Body.String())

	// The account is still visibly pending — the state is not what was removed.
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, string(iam.LifecyclePending), got["lifecycle"])
}

// TestListUsers_DoesNotExposeInviteTokens covers the worst of the read paths:
// one call would otherwise hand back every pending invite in the tenant.
func TestListUsers_DoesNotExposeInviteTokens(t *testing.T) {
	handler, _, cleanup := setupUserReadHandler(t)
	defer cleanup()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/acme/users/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ListUsers(w, req)

	require.Equal(t, http.StatusOK, w.Code, "ListUsers failed: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "77", "the fixture user is missing from the listing")
	assertBodyHasNoInviteToken(t, w.Body.String())
}

func assertBodyHasNoInviteToken(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, exposureToken) {
		t.Errorf("the response carries the cleartext invite token:\n%s", body)
	}
	if strings.Contains(body, `"invite_token"`) {
		t.Errorf("the response carries an invite_token field:\n%s", body)
	}
}

// createPendingUser drives the create endpoint and returns the decoded
// response, so a test can compare what the caller was told against what the
// store holds.
func createPendingUser(t *testing.T, handler *IAMHandler, body string) map[string]interface{} {
	t.Helper()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/users/", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "CreateUser failed: %s", w.Body.String())

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	return got
}

// TestCreateUser_OmittedAuthMethodsAreNotWidenedByTheInviteWrite guards the
// second write this endpoint makes.
//
// Creating a PENDING user is two writes: the create, then an update that
// attaches the invite state. The second one carries auth_methods, and where it
// SOURCES them decides whether it can widen the account's authentication
// policy. Sourced from the request, a caller who omitted the field hands
// UpdateUser a non-nil pointer to an empty slice — which does not mean
// "unchanged", it means "no restriction" — so the write clears the default the
// create had just applied. The account then accepts any credential kind while
// the caller is told they created a webauthn-only one.
//
// Sourcing them from the created record cannot do that, whatever the store
// defaults to. This test therefore asserts the STORED restriction survives the
// pair of writes, and that the response agrees with it.
func TestCreateUser_OmittedAuthMethodsAreNotWidenedByTheInviteWrite(t *testing.T) {
	handler, mem, cleanup := setupUserReadHandler(t)
	defer cleanup()

	got := createPendingUser(t, handler,
		`{"email":"carol@acme.com","username":"carol","name":"Carol"}`)

	id, _ := got["id"].(string)
	require.NotEmpty(t, id, "create response carried no user id: %v", got)

	stored, err := mem.GetUser(context.Background(), "acme", id)
	require.NoError(t, err)
	require.Equal(t, iam.LifecyclePending, stored.Lifecycle,
		"the fixture must exercise the INVITED path, which is the one that writes twice")
	assert.Equal(t, []string{iam.AuthMethodWebAuthn}, stored.AuthMethods,
		"the invite write cleared the authentication restriction the create applied")

	// The response must describe the record that exists. Returning the
	// pre-update object is what made the widening undetectable by the caller.
	assert.Equal(t, []interface{}{iam.AuthMethodWebAuthn}, got["auth_methods"],
		"the response reported auth methods the stored record does not have")
}

// TestCreateUser_SuppliedAuthMethodsSurviveTheInviteWrite is the other
// direction: an explicit restriction must reach the record intact through both
// writes. A fix that simply stopped sending auth_methods on the second write
// would pass the test above and lose nothing visible here either — this pins
// that the caller's own choice is what ends up stored.
func TestCreateUser_SuppliedAuthMethodsSurviveTheInviteWrite(t *testing.T) {
	handler, mem, cleanup := setupUserReadHandler(t)
	defer cleanup()

	got := createPendingUser(t, handler,
		`{"email":"dave@acme.com","username":"dave","name":"Dave","auth_methods":["piv"]}`)

	id, _ := got["id"].(string)
	require.NotEmpty(t, id)

	stored, err := mem.GetUser(context.Background(), "acme", id)
	require.NoError(t, err)
	assert.Equal(t, []string{iam.AuthMethodPIV}, stored.AuthMethods)
	assert.Equal(t, []interface{}{iam.AuthMethodPIV}, got["auth_methods"])
}
