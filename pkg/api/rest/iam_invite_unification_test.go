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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// markUsedFailingInviteStore wraps a MemoryInviteStore but always fails the
// redemption, so the handler's "could not consume a one-time token" path can be
// exercised. That path is security-relevant: an unconsumed token stays
// redeemable, so the handler must NOT report success.
type markUsedFailingInviteStore struct {
	inner *iamtest.MemoryInviteStore
}

func (s *markUsedFailingInviteStore) Lookup(ctx context.Context, tenantID, token string) (*iam.InviteRecord, error) {
	return s.inner.Lookup(ctx, tenantID, token)
}

func (s *markUsedFailingInviteStore) Create(
	ctx context.Context,
	tenantID, userID string,
	ttl time.Duration,
) (string, *iam.InviteRecord, error) {
	return s.inner.Create(ctx, tenantID, userID, ttl)
}

func (s *markUsedFailingInviteStore) Redeem(_ context.Context, _, _ string) (*iam.InviteRecord, error) {
	return nil, errMarkUsedFailed
}

func (s *markUsedFailingInviteStore) GC(ctx context.Context) error {
	return nil
}

type markUsedError struct{}

func (markUsedError) Error() string { return "iam: simulated mark-used failure" }

var errMarkUsedFailed = markUsedError{}

// unificationInviteService exposes the WebAuthn manager, identity store, and a
// caller-supplied invite store through the same seams the production REST server
// uses (GetWebAuthnManager / GetIdentityStore / GetInviteStore).
type unificationInviteService struct {
	*testutil.MockQuicBaaS
	mgr   WebAuthnManager
	store *iam.IdentityStore
}

func (s *unificationInviteService) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	return s.mgr, nil
}

func (s *unificationInviteService) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return s.store, nil
}

// newUnificationInviteHandler builds an IAMHandler whose service exposes the
// supplied invite store. It returns the handler and a pending user seeded in the
// identity store (so email lookups during activation succeed).
func newUnificationInviteHandler(t *testing.T, inviteStore iam.InviteStore) (*IAMHandler, string, string) {
	t.Helper()

	const tenantID = "tenant-unify"
	const userID = "74"
	const email = "unify@example.com"

	memStorage := iam.NewMemoryStorage()
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
		Tenants:     &testTenantManager{storage: memStorage},
	})
	require.NoError(t, memStorage.CreateUser(context.Background(), &iam.User{
		ID: userID, TenantID: tenantID, Email: email,
		AuthMethods: []string{"webauthn"},
	}))

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &unificationInviteService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		mgr:          &fakeWebAuthnManager{user: &iam.User{ID: userID, TenantID: tenantID, Email: email}},
		store:        identityStore,
	}
	mockSvc.InviteStoreOverride = inviteStore

	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	t.Cleanup(func() { server.Close() })

	return NewIAMHandler(IAMHandlerDependencies{Server: server}), tenantID, userID
}

// TestInvite_FirstBootStyleInviteIsRetrievableThroughREST is the authoritative
// regression for the disjoint-store bug: an invite created the way
// firstBootBootstrap creates it (TokenHash via iam.TokenHashExported, written to
// the persistent store) MUST be retrievable through the REST GetInvite handler.
// Before the fix the handler read a private in-memory store and returned 404.
func TestInvite_FirstBootStyleInviteIsRetrievableThroughREST(t *testing.T) {
	persistent := iamtest.NewMemoryInviteStore()
	handler, tenantID, userID := newUnificationInviteHandler(t, persistent)

	// Mint and store an invite exactly as firstBootBootstrap does.
	token, _, err := persistent.Create(context.Background(), tenantID, userID, time.Hour)
	require.NoError(t, err)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	rctx.URLParams.Add("token", token)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+token, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	require.Equal(t, http.StatusOK, w.Code, "banner token must resolve, not 404; body: %s", w.Body.String())
	var resp InviteMetadataResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, userID, resp.UserID)
	assert.NotEmpty(t, resp.AuthMethods)
}

// TestInvite_RESTCreatedInviteLandsInSameStore verifies the reverse direction:
// an invite created through the REST CreateUser path lands in the SAME store the
// activation endpoints read, so a REST-issued invite is immediately redeemable.
func TestInvite_RESTCreatedInviteLandsInSameStore(t *testing.T) {
	_, mock, memStorage, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant-rest", Name: "Rest"}))

	body := `{"email":"rest@example.com","username":"restuser","name":"Rest User","auth_methods":["webauthn"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant-rest/users",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withChiParams(req, map[string]string{"tenant_id": "tenant-rest"})
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	// Extract the issued token from the invite_url returned by CreateUser.
	var createResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	inviteURL, _ := createResp["invite_url"].(string)
	require.NotEmpty(t, inviteURL, "CreateUser must return an invite_url")
	// The token is the trailing path segment after the final '/'.
	token := inviteURL
	for i := len(inviteURL) - 1; i >= 0; i-- {
		if inviteURL[i] == '/' {
			token = inviteURL[i+1:]
			break
		}
	}
	require.Len(t, token, 64, "invite token must be 64 hex chars; url=%s", inviteURL)

	// The SAME store instance the activation endpoints resolve must now hold it.
	store, err := handler.getInviteStore(ctx)
	require.NoError(t, err)
	require.Same(t, mock.InviteStoreOverride, store,
		"getInviteStore must resolve the SAME store instance CreateUser wrote to")
	record, err := store.Lookup(ctx, "tenant-rest", token)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.False(t, record.Used)

	// And GetInvite over that token returns 200 (end-to-end redeemability).
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant-rest")
	rctx.URLParams.Add("token", token)
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+token, nil)
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), chi.RouteCtxKey, rctx))
	gw := httptest.NewRecorder()
	handler.GetInvite(gw, getReq)
	assert.Equal(t, http.StatusOK, gw.Code, "REST-created invite must resolve via GetInvite; body: %s", gw.Body.String())
}

// TestInvite_NoInviteStoreIsAWiringFault: an unwired store reports the shared
// iam sentinel every transport uses, while REST keeps answering 500 for it.
func TestInvite_NoInviteStoreIsAWiringFault(t *testing.T) {
	handler, tenantID, _ := newUnificationInviteHandler(t, nil)

	_, err := handler.getInviteStore(context.Background())
	require.ErrorIs(t, err, iam.ErrInviteStoreUnavailable)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	rctx.URLParams.Add("token", "tok")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/tok", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code, "body: %s", w.Body.String())
}

// TestActivateFIDO2Finish_MarkUsedFailureSurfaces verifies the security-relevant
// MarkUsed error path: when the one-time token cannot be recorded as consumed,
// the handler must NOT report success (the invite would otherwise remain live).
func TestActivateFIDO2Finish_MarkUsedFailureSurfaces(t *testing.T) {
	failing := &markUsedFailingInviteStore{inner: iamtest.NewMemoryInviteStore()}
	handler, tenantID, userID := newUnificationInviteHandler(t, failing)

	token, _, err := failing.Create(context.Background(), tenantID, userID, time.Hour)
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/iam/invites/"+token+"/activate/fido2/finish", nil)
	r.Body = io.NopCloser(bytes.NewReader([]byte(`{"credential":{"id":"abc","response":{}}}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	rctx.URLParams.Add("token", token)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ActivateFIDO2Finish(w, r)

	assert.GreaterOrEqual(t, w.Code, 500,
		"a MarkUsed failure on a one-time token must surface as a server error, not a success; body: %s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "activation successful",
		"handler must not report success when the invite could not be marked used")
}

// TestGetInvite_StoreUnavailableFailsClosed verifies there is NO in-memory
// fallback: when the persistent invite store is not wired the handler fails
// closed rather than silently serving from a disjoint store.
func TestGetInvite_StoreUnavailableFailsClosed(t *testing.T) {
	handler, _, _ := newUnificationInviteHandler(t, nil) // nil override -> unavailable

	token := "1111111111111111111111111111111111111111111111111111111111111111"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "tenant-unify")
	rctx.URLParams.Add("token", token)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/invites/"+token, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.GetInvite(w, req)

	assert.GreaterOrEqual(t, w.Code, 500,
		"missing persistent invite store must fail closed; body: %s", w.Body.String())
}
