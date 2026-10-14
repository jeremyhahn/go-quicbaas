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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// rollbackTestService exposes an IdentityStore to the IAM handlers.
type rollbackTestService struct {
	*testutil.MockQuicBaaS
	identity *iam.IdentityStore
}

func (s *rollbackTestService) GetIdentityStore(context.Context) (*iam.IdentityStore, error) {
	return s.identity, nil
}

// failingInviteStore issues no invites, standing in for the invite store being
// unreachable at the moment a user is created.
type failingInviteStore struct {
	iam.InviteStore
	err error
}

func (f *failingInviteStore) Create(context.Context, string, uint64, time.Duration) (string, *iam.InviteRecord, error) {
	return "", nil, f.err
}

// TestCreateUser_InviteFailureRollsBackTheUser proves REST does not leave a
// user behind when it cannot issue the invite.
//
// REST is the ONE surface that can complete enrollment — every other create
// surface now refuses outright rather than produce an unactivatable account.
// If it persisted a user and then failed to issue the invite, it would produce
// exactly that state itself, and a retry could not repair it because the
// identity already exists.
func TestCreateUser_InviteFailureRollsBackTheUser(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mem := iam.NewMemoryStorage()
	tenants := iam.NewDefaultTenantManager(mem)
	_, err = tenants.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	identity := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: mem,
		Tenants:     tenants,
	})

	mock := testutil.NewMockQuicBaaS()
	mock.InviteStoreOverride = &failingInviteStore{err: errors.New("invite store unreachable")}

	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(&rollbackTestService{MockQuicBaaS: mock, identity: identity}, cfg, log)
	require.NoError(t, err)
	defer server.Close()

	handler := &IAMHandler{server: server}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/users/",
		strings.NewReader(`{"email":"bob@acme.com","username":"bob","name":"Bob"}`))
	req = req.WithContext(callerRouteContext(req.Context(), rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)

	assert.NotEqual(t, http.StatusCreated, w.Code,
		"a create that could not issue an invite reported success")

	// The user must not survive: an account in `pending` with no invite is
	// unactivatable and unrecoverable by retry.
	if _, err := identity.GetUserByEmail(context.Background(), "acme", "bob@acme.com"); err == nil {
		t.Error("the user was left behind after the invite could not be issued")
	}
}

// failingUpdateUserStorage persists the create but fails the update that
// attaches the invite state to the user record.
type failingUpdateUserStorage struct {
	iam.UserStorage
	err error
}

func (f *failingUpdateUserStorage) UpdateUser(context.Context, *iam.User) error { return f.err }

// TestCreateUser_AttachFailureRollsBackTheUser proves the rollback covers the
// LAST post-persist step too: attaching the invite state to the user record.
//
// That step is what puts the account in `pending`, and PIVActivator refuses a
// user that is not pending. Logging and continuing — which is what this used
// to do — returned "created, here is your invite" for an account whose PIV
// activation would later be refused, with nothing in the response saying so.
func TestCreateUser_AttachFailureRollsBackTheUser(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mem := iam.NewMemoryStorage()
	tenants := iam.NewDefaultTenantManager(mem)
	_, err = tenants.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	identity := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: &failingUpdateUserStorage{UserStorage: mem, err: errors.New("user store unavailable")},
		RoleStorage: mem,
		Tenants:     tenants,
	})

	invites := &tokenRecordingInviteStore{InviteStore: iamtest.NewMemoryInviteStore()}
	mock := testutil.NewMockQuicBaaS()
	mock.InviteStoreOverride = invites

	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(&rollbackTestService{MockQuicBaaS: mock, identity: identity}, cfg, log)
	require.NoError(t, err)
	defer server.Close()

	handler := &IAMHandler{server: server}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/users/",
		strings.NewReader(`{"email":"carol@acme.com","username":"carol","name":"Carol"}`))
	req = req.WithContext(callerRouteContext(req.Context(), rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)

	assert.NotEqual(t, http.StatusCreated, w.Code,
		"a create whose invite could not be attached reported success")

	// The rollback reads through the same failing store, so assert against the
	// backing memory store directly.
	if _, err := mem.GetUserByEmail(context.Background(), "acme", "carol@acme.com"); err == nil {
		t.Error("the user was left behind after the invite state could not be attached")
	}

	// The minted token must be spent too. A live invite pointing at a user id
	// that no longer exists is a credential in the wild that names nothing —
	// the same stranded state the rollback exists to close.
	tok := invites.LastToken()
	require.NotEmpty(t, tok, "the fixture never minted an invite")
	if _, err := invites.Redeem(context.Background(), "acme", tok); err == nil {
		t.Error("the minted invite is still redeemable after the create was rolled back")
	}
}

// tokenRecordingInviteStore remembers the token it minted, so a test can ask
// afterwards whether the rollback spent it.
type tokenRecordingInviteStore struct {
	iam.InviteStore
	last string
}

func (t *tokenRecordingInviteStore) Create(ctx context.Context, tenantID string, userID uint64, ttl time.Duration) (string, *iam.InviteRecord, error) {
	token, rec, err := t.InviteStore.Create(ctx, tenantID, userID, ttl)
	if err == nil {
		t.last = token
	}
	return token, rec, err
}

// LastToken returns the most recently minted token.
func (t *tokenRecordingInviteStore) LastToken() string { return t.last }

// ctxAwareUserStorage refuses writes on a cancelled context, which is what a
// real store does — MemoryStorage ignores the context entirely, so a test
// built on it alone cannot tell a detached rollback from a cancelled one.
type ctxAwareUserStorage struct {
	iam.UserStorage
}

func (c *ctxAwareUserStorage) DeleteUser(ctx context.Context, tenantID string, userID uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.UserStorage.DeleteUser(ctx, tenantID, userID)
}

// cancelOnInviteStore fails the invite-store resolution AND cancels the
// request context first, reproducing the case that matters most: whatever went
// wrong took the caller's context with it.
type cancelOnInviteStore struct {
	iam.InviteStore
	cancel context.CancelFunc
	err    error
}

func (c *cancelOnInviteStore) Create(context.Context, string, uint64, time.Duration) (string, *iam.InviteRecord, error) {
	c.cancel()
	return "", nil, c.err
}

// TestCreateUser_RollbackSurvivesRequestCancellation proves the rollback
// completes even when the request context is already cancelled.
//
// Every reason the rollback is needed — a store gone away, a deadline, a
// client that hung up — can also have cancelled the request context. Running
// the delete on that context would cancel the rollback for the same reason the
// create failed, leaving exactly the stranded unactivatable user this work
// exists to prevent, in the case where it is most likely to be needed.
func TestCreateUser_RollbackSurvivesRequestCancellation(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mem := iam.NewMemoryStorage()
	tenants := iam.NewDefaultTenantManager(mem)
	_, err = tenants.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	identity := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: &ctxAwareUserStorage{UserStorage: mem},
		RoleStorage: mem,
		Tenants:     tenants,
	})

	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock := testutil.NewMockQuicBaaS()
	mock.InviteStoreOverride = &cancelOnInviteStore{
		cancel: cancel,
		err:    errors.New("invite store unreachable"),
	}

	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(&rollbackTestService{MockQuicBaaS: mock, identity: identity}, cfg, log)
	require.NoError(t, err)
	defer server.Close()

	handler := &IAMHandler{server: server}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", "acme")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/users/",
		strings.NewReader(`{"email":"dave@acme.com","username":"dave","name":"Dave"}`))
	req = req.WithContext(callerRouteContext(reqCtx, rctx))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateUser(w, req)

	assert.NotEqual(t, http.StatusCreated, w.Code, "a failed create reported success")

	// The request context is cancelled by now; the rollback must have run anyway.
	require.Error(t, reqCtx.Err(), "the fixture did not actually cancel the request context")
	if _, err := mem.GetUserByEmail(context.Background(), "acme", "dave@acme.com"); err == nil {
		t.Error("the user survived: the rollback was cancelled along with the request")
	}
}
