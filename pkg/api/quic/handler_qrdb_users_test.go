//go:build quic

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

package quic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Mock UserServicer
// =============================================================================

type quicMockUserServicer struct {
	getFn           func(ctx context.Context, id uint64) (*qrdbsdk.User, error)
	getByUsernameFn func(ctx context.Context, tenantID, username string) (*qrdbsdk.User, error)
	byThumbprintFn  func(ctx context.Context, thumbprint string) (*qrdbsdk.User, error)
}

func quicTestUser() *qrdbsdk.User {
	return &qrdbsdk.User{
		ID:        42,
		Username:  "alice",
		TenantID:  "tenant-a",
		RoleIDs:   []string{"role-1"},
		Enabled:   true,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
	}
}

func (m *quicMockUserServicer) CreateUser(context.Context, *qrdbsdk.CreateUserRequest) (*qrdbsdk.User, error) {
	return nil, errors.New("not implemented")
}
func (m *quicMockUserServicer) GetUser(ctx context.Context, id uint64) (*qrdbsdk.User, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return quicTestUser(), nil
}
func (m *quicMockUserServicer) GetUserByUsername(ctx context.Context, tenantID, username string) (*qrdbsdk.User, error) {
	if m.getByUsernameFn != nil {
		return m.getByUsernameFn(ctx, tenantID, username)
	}
	return quicTestUser(), nil
}
func (m *quicMockUserServicer) UserByCertThumbprint(ctx context.Context, thumbprint string) (*qrdbsdk.User, error) {
	if m.byThumbprintFn != nil {
		return m.byThumbprintFn(ctx, thumbprint)
	}
	return quicTestUser(), nil
}
func (m *quicMockUserServicer) AddCertBinding(context.Context, uint64, string, int64) (*qrdbsdk.User, error) {
	return nil, errors.New("not implemented")
}
func (m *quicMockUserServicer) UpdateUser(context.Context, *qrdbsdk.UpdateUserRequest) (*qrdbsdk.User, error) {
	return nil, errors.New("not implemented")
}
func (m *quicMockUserServicer) DeleteUser(context.Context, uint64) error {
	return errors.New("not implemented")
}
func (m *quicMockUserServicer) ListUsers(context.Context, *qrdbsdk.ListUsersRequest) (*qrdbsdk.ListUsersResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *quicMockUserServicer) EnableUser(context.Context, uint64) error {
	return errors.New("not implemented")
}
func (m *quicMockUserServicer) DisableUser(context.Context, uint64) error {
	return errors.New("not implemented")
}

var _ qrdbsdk.UserServicer = (*quicMockUserServicer)(nil)

// =============================================================================
// Mock UserAuthFactorServicer
// =============================================================================

type quicMockUserAuthFactorServicer struct {
	addFn     func(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error
	listFn    func(ctx context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error)
	getFn     func(ctx context.Context, factorID string) (*qrdbsdk.AuthFactor, error)
	lookupFn  func(ctx context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error)
	removeFn  func(ctx context.Context, factorID string, force bool) error
	rotateFn  func(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error
	relabelFn func(ctx context.Context, factorID, label string) error
}

func quicTestFactor() *qrdbsdk.AuthFactor {
	lastUsed := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	return &qrdbsdk.AuthFactor{
		ID:             "factor-1",
		UserID:         42,
		Type:           qrdbsdk.AuthFactorType("mtls"),
		Label:          "work laptop",
		Credential:     []byte{0x01, 0x02},
		SPKICredential: []byte{0x03, 0x04},
		Metadata:       map[string]string{"serial": "abc"},
		RegisteredAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		LastUsedAt:     &lastUsed,
		CreatedBy:      "admin",
	}
}

func (m *quicMockUserAuthFactorServicer) Add(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	if m.addFn != nil {
		return m.addFn(ctx, userID, factor)
	}
	factor.ID = "factor-new"
	factor.RegisteredAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return nil
}
func (m *quicMockUserAuthFactorServicer) List(ctx context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID)
	}
	return []*qrdbsdk.AuthFactor{quicTestFactor()}, nil
}
func (m *quicMockUserAuthFactorServicer) Get(ctx context.Context, factorID string) (*qrdbsdk.AuthFactor, error) {
	if m.getFn != nil {
		return m.getFn(ctx, factorID)
	}
	return quicTestFactor(), nil
}
func (m *quicMockUserAuthFactorServicer) LookupByCredentialKey(ctx context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
	if m.lookupFn != nil {
		return m.lookupFn(ctx, t, key)
	}
	return quicTestFactor(), nil
}
func (m *quicMockUserAuthFactorServicer) Remove(ctx context.Context, factorID string, force bool) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, factorID, force)
	}
	return nil
}
func (m *quicMockUserAuthFactorServicer) Rotate(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error {
	if m.rotateFn != nil {
		return m.rotateFn(ctx, oldID, newFactor)
	}
	newFactor.ID = "factor-rotated"
	newFactor.RegisteredAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return nil
}
func (m *quicMockUserAuthFactorServicer) Relabel(ctx context.Context, factorID, label string) error {
	if m.relabelFn != nil {
		return m.relabelFn(ctx, factorID, label)
	}
	return nil
}
func (m *quicMockUserAuthFactorServicer) MarkUsed(context.Context, string, time.Time) error {
	return nil
}

var _ qrdbsdk.UserAuthFactorServicer = (*quicMockUserAuthFactorServicer)(nil)

// =============================================================================
// Helpers
// =============================================================================

func newQRDBUserServer(t *testing.T, userSvc qrdbsdk.UserServicer, factorSvc qrdbsdk.UserAuthFactorServicer) *Server {
	t.Helper()
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	server, err := NewServer(&mockService{}, config, createServerTestLogger())
	require.NoError(t, err)
	if userSvc != nil {
		server.SetQRDBUserService(userSvc)
	}
	if factorSvc != nil {
		server.SetQRDBUserAuthFactorService(factorSvc)
	}
	return server
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// =============================================================================
// Typed errors
// =============================================================================

func TestQRDBUserTypedErrors(t *testing.T) {
	assert.Equal(t, "qrdb user service not configured", (&QRDBUserNotConfiguredError{}).Error())
	assert.Equal(t, "qrdb user auth-factor service not configured", (&QRDBUserAuthFactorNotConfiguredError{}).Error())
}

// =============================================================================
// Dispatch table membership
// =============================================================================

func TestBuildQRDBHandlers_UserFactorMethods(t *testing.T) {
	server := newQRDBUserServer(t, &quicMockUserServicer{}, &quicMockUserAuthFactorServicer{})
	handlers := server.buildQRDBHandlers()
	for _, method := range []string{
		"user.Get", "user.GetByUsername", "user.GetByCertThumbprint",
		"userauthfactor.Add", "userauthfactor.List", "userauthfactor.Get",
		"userauthfactor.Remove", "userauthfactor.Rotate",
		"userauthfactor.Relabel", "userauthfactor.Login",
	} {
		_, ok := handlers[method]
		assert.True(t, ok, "missing handler for %s", method)
	}
}

func TestSetQRDBUserServices_Toggle(t *testing.T) {
	server := newQRDBUserServer(t, nil, nil)
	server.SetQRDBUserService(&quicMockUserServicer{})
	server.SetQRDBUserService(nil) // disable; must not panic
	server.SetQRDBUserAuthFactorService(&quicMockUserAuthFactorServicer{})
	server.SetQRDBUserAuthFactorService(nil) // disable; must not panic
}

// =============================================================================
// User lookup handlers — happy paths + wire-shape pinning
// =============================================================================

// TestQRDBUserHandlers_HappyPaths_WireShape pins the camelCase JSON user wire
// contract byte-for-byte against go-qrdb's userWireResponse: the qrdb SDK
// QUIC client decodes exactly these keys.
func TestQRDBUserHandlers_HappyPaths_WireShape(t *testing.T) {
	server := newQRDBUserServer(t, &quicMockUserServicer{}, nil)
	ctx := context.Background()

	assertUserWire := func(t *testing.T, resp []byte) {
		t.Helper()
		var wire map[string]any
		require.NoError(t, json.Unmarshal(resp, &wire))
		// Exact key set of the qrdb user wire response.
		for _, key := range []string{"id", "username", "tenantID", "roleIDs", "enabled", "system", "createdAt", "updatedAt"} {
			_, ok := wire[key]
			assert.True(t, ok, "missing wire key %q", key)
		}
		assert.Len(t, wire, 8, "unexpected extra keys in user wire response")
		assert.Equal(t, float64(42), wire["id"])
		assert.Equal(t, "alice", wire["username"])
		assert.Equal(t, "tenant-a", wire["tenantID"])
		assert.Equal(t, []any{"role-1"}, wire["roleIDs"])
		assert.Equal(t, true, wire["enabled"])
		assert.Equal(t, "2026-01-02T03:04:05Z", wire["createdAt"])
	}

	getOut, err := server.qrdbUserGet(ctx, mustJSON(t, map[string]any{"id": 42}))
	require.NoError(t, err)
	assertUserWire(t, getOut)

	byNameOut, err := server.qrdbUserGetByUsername(ctx, mustJSON(t, map[string]any{"tenantID": "tenant-a", "username": "alice"}))
	require.NoError(t, err)
	assertUserWire(t, byNameOut)

	byThumbOut, err := server.qrdbUserGetByCertThumbprint(ctx, mustJSON(t, map[string]any{"thumbprint": "aa"}))
	require.NoError(t, err)
	assertUserWire(t, byThumbOut)
}

// TestQRDBUserGetByUsername_ParamsForwarded proves the tenant and username
// from the wire request reach the servicer unchanged.
func TestQRDBUserGetByUsername_ParamsForwarded(t *testing.T) {
	var gotTenant, gotUsername string
	svc := &quicMockUserServicer{
		getByUsernameFn: func(_ context.Context, tenantID, username string) (*qrdbsdk.User, error) {
			gotTenant, gotUsername = tenantID, username
			return quicTestUser(), nil
		},
	}
	server := newQRDBUserServer(t, svc, nil)

	_, err := server.qrdbUserGetByUsername(context.Background(),
		mustJSON(t, map[string]any{"tenantID": "t-9", "username": "bob"}))
	require.NoError(t, err)
	assert.Equal(t, "t-9", gotTenant)
	assert.Equal(t, "bob", gotUsername)
}

// TestQRDBUserHandlers_NilRoleIDsBecomesEmptyArray pins the roleIDs
// nil-slice normalization: the wire always carries [] rather than null.
func TestQRDBUserHandlers_NilRoleIDsBecomesEmptyArray(t *testing.T) {
	svc := &quicMockUserServicer{
		getFn: func(context.Context, uint64) (*qrdbsdk.User, error) {
			u := quicTestUser()
			u.RoleIDs = nil
			return u, nil
		},
	}
	server := newQRDBUserServer(t, svc, nil)
	out, err := server.qrdbUserGet(context.Background(), mustJSON(t, map[string]any{"id": 42}))
	require.NoError(t, err)
	assert.Contains(t, string(out), `"roleIDs":[]`)
}

// =============================================================================
// User auth-factor handlers — happy paths + wire-shape pinning
// =============================================================================

// TestQRDBUserFactorHandlers_HappyPaths_WireShape pins the snake_case JSON
// factor wire contract byte-for-byte against go-qrdb's quicUserFactorRecord:
// base64 credentials, RFC 3339 timestamps, and the envelope keys the SDK
// decodes ("factor", "factors"/"total", "new_factor", "status", "user_id").
func TestQRDBUserFactorHandlers_HappyPaths_WireShape(t *testing.T) {
	server := newQRDBUserServer(t, nil, &quicMockUserAuthFactorServicer{})
	ctx := context.Background()

	credB64 := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})

	assertFactorRecord := func(t *testing.T, raw map[string]any) {
		t.Helper()
		assert.Equal(t, "factor-1", raw["id"])
		assert.Equal(t, float64(42), raw["user_id"])
		assert.Equal(t, "mtls", raw["type"])
		assert.Equal(t, "work laptop", raw["label"])
		assert.Equal(t, credB64, raw["credential"], "credential must be base64-encoded")
		assert.Equal(t, base64.StdEncoding.EncodeToString([]byte{0x03, 0x04}), raw["spki_credential"])
		assert.Equal(t, "2026-01-02T03:04:05Z", raw["registered_at"])
		assert.Equal(t, "2026-02-03T04:05:06Z", raw["last_used_at"])
		assert.Equal(t, "admin", raw["created_by"])
		_, hasRevoked := raw["revoked_at"]
		assert.False(t, hasRevoked, "revoked_at must be omitted when nil")
	}

	t.Run("add", func(t *testing.T) {
		out, err := server.qrdbUserFactorAdd(ctx, mustJSON(t, map[string]any{
			"user_id":    42,
			"type":       "mtls",
			"label":      "work laptop",
			"credential": []byte{0x01, 0x02},
		}))
		require.NoError(t, err)
		var resp struct {
			Factor map[string]any `json:"factor"`
		}
		require.NoError(t, json.Unmarshal(out, &resp))
		assert.Equal(t, "factor-new", resp.Factor["id"], "add must echo the service-assigned ID")
		assert.Equal(t, "2026-01-02T03:04:05Z", resp.Factor["registered_at"])
	})

	t.Run("list", func(t *testing.T) {
		out, err := server.qrdbUserFactorList(ctx, mustJSON(t, map[string]any{"user_id": 42}))
		require.NoError(t, err)
		var resp struct {
			Factors []map[string]any `json:"factors"`
			Total   int              `json:"total"`
		}
		require.NoError(t, json.Unmarshal(out, &resp))
		require.Len(t, resp.Factors, 1)
		assert.Equal(t, 1, resp.Total)
		assertFactorRecord(t, resp.Factors[0])
	})

	t.Run("get", func(t *testing.T) {
		out, err := server.qrdbUserFactorGet(ctx, mustJSON(t, map[string]any{"factor_id": "factor-1"}))
		require.NoError(t, err)
		var resp struct {
			Factor map[string]any `json:"factor"`
		}
		require.NoError(t, json.Unmarshal(out, &resp))
		assertFactorRecord(t, resp.Factor)
	})

	t.Run("remove", func(t *testing.T) {
		var gotForce bool
		svc := &quicMockUserAuthFactorServicer{
			removeFn: func(_ context.Context, _ string, force bool) error {
				gotForce = force
				return nil
			},
		}
		server := newQRDBUserServer(t, nil, svc)
		out, err := server.qrdbUserFactorRemove(ctx, mustJSON(t, map[string]any{"factor_id": "factor-1", "force": true}))
		require.NoError(t, err)
		assert.True(t, gotForce)
		assert.JSONEq(t, `{"status":"revoked"}`, string(out))
	})

	t.Run("rotate", func(t *testing.T) {
		out, err := server.qrdbUserFactorRotate(ctx, mustJSON(t, map[string]any{
			"old_factor_id": "factor-1",
			"new_factor": map[string]any{
				"user_id":    42,
				"type":       "mtls",
				"credential": []byte{0x05},
			},
		}))
		require.NoError(t, err)
		var resp struct {
			NewFactor map[string]any `json:"new_factor"`
		}
		require.NoError(t, json.Unmarshal(out, &resp))
		assert.Equal(t, "factor-rotated", resp.NewFactor["id"], "rotate response must use the new_factor envelope key")
	})

	t.Run("relabel", func(t *testing.T) {
		var gotLabel string
		svc := &quicMockUserAuthFactorServicer{
			relabelFn: func(_ context.Context, _ string, label string) error {
				gotLabel = label
				return nil
			},
		}
		server := newQRDBUserServer(t, nil, svc)
		out, err := server.qrdbUserFactorRelabel(ctx, mustJSON(t, map[string]any{"factor_id": "factor-1", "label": "new label"}))
		require.NoError(t, err)
		assert.Equal(t, "new label", gotLabel)
		var resp struct {
			Factor map[string]any `json:"factor"`
		}
		require.NoError(t, json.Unmarshal(out, &resp))
		assertFactorRecord(t, resp.Factor)
	})

	t.Run("login", func(t *testing.T) {
		var gotType qrdbsdk.AuthFactorType
		var gotKey []byte
		svc := &quicMockUserAuthFactorServicer{
			lookupFn: func(_ context.Context, typ qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
				gotType, gotKey = typ, key
				return quicTestFactor(), nil
			},
		}
		server := newQRDBUserServer(t, nil, svc)
		out, err := server.qrdbUserFactorLogin(ctx, mustJSON(t, map[string]any{
			"type":       "mtls",
			"lookup_key": []byte{0x0a, 0x0b},
		}))
		require.NoError(t, err)
		assert.Equal(t, qrdbsdk.AuthFactorType("mtls"), gotType)
		assert.Equal(t, []byte{0x0a, 0x0b}, gotKey)
		var resp struct {
			UserID uint64         `json:"user_id"`
			Active bool           `json:"active"`
			Factor map[string]any `json:"factor"`
		}
		require.NoError(t, json.Unmarshal(out, &resp))
		assert.Equal(t, uint64(42), resp.UserID)
		assert.True(t, resp.Active)
		assertFactorRecord(t, resp.Factor)
	})
}

// =============================================================================
// Not-configured errors
// =============================================================================

func TestQRDBUserHandlers_NotConfigured(t *testing.T) {
	server := newQRDBUserServer(t, nil, nil)
	ctx := context.Background()

	userHandlers := []func(context.Context, []byte) ([]byte, error){
		server.qrdbUserGet,
		server.qrdbUserGetByUsername,
		server.qrdbUserGetByCertThumbprint,
	}
	for i, h := range userHandlers {
		_, err := h(ctx, []byte(`{}`))
		var notConfigured *QRDBUserNotConfiguredError
		assert.ErrorAs(t, err, &notConfigured, "user handler %d must fail closed", i)
	}

	factorHandlers := []func(context.Context, []byte) ([]byte, error){
		server.qrdbUserFactorAdd,
		server.qrdbUserFactorList,
		server.qrdbUserFactorGet,
		server.qrdbUserFactorRemove,
		server.qrdbUserFactorRotate,
		server.qrdbUserFactorRelabel,
		server.qrdbUserFactorLogin,
	}
	for i, h := range factorHandlers {
		_, err := h(ctx, []byte(`{}`))
		var notConfigured *QRDBUserAuthFactorNotConfiguredError
		assert.ErrorAs(t, err, &notConfigured, "factor handler %d must fail closed", i)
	}
}

// =============================================================================
// Unmarshal-error paths
// =============================================================================

func TestQRDBUserHandlers_UnmarshalErrors(t *testing.T) {
	server := newQRDBUserServer(t, &quicMockUserServicer{}, &quicMockUserAuthFactorServicer{})
	ctx := context.Background()
	bad := []byte(`{not json`)

	handlers := []func(context.Context, []byte) ([]byte, error){
		server.qrdbUserGet,
		server.qrdbUserGetByUsername,
		server.qrdbUserGetByCertThumbprint,
		server.qrdbUserFactorAdd,
		server.qrdbUserFactorList,
		server.qrdbUserFactorGet,
		server.qrdbUserFactorRemove,
		server.qrdbUserFactorRotate,
		server.qrdbUserFactorRelabel,
		server.qrdbUserFactorLogin,
	}
	for i, h := range handlers {
		_, err := h(ctx, bad)
		require.Error(t, err, "handler %d should reject bad payload", i)
		var unmarshalErr *QRDBUnmarshalError
		assert.ErrorAs(t, err, &unmarshalErr)
	}
}

// =============================================================================
// Service-error paths (sealed-state typed errors travel unchanged)
// =============================================================================

// quicSealedError stands in for the pkg/server proxy's typed
// QRDBUserServiceUnavailableError (which cannot be imported here without an
// import cycle). The bridge must propagate the servicer error UNCHANGED so
// the sealed-window typed error reaches the wire as its exact message.
type quicSealedError struct{ surface string }

func (e *quicSealedError) Error() string {
	return "qrdb " + e.surface + " service unavailable (embedded node absent or sealed)"
}

func TestQRDBUserHandlers_ServiceErrorsPropagateUnchanged(t *testing.T) {
	sealedUser := &quicSealedError{surface: "user"}
	sealedFactor := &quicSealedError{surface: "user_authfactor"}

	userSvc := &quicMockUserServicer{
		getFn:           func(context.Context, uint64) (*qrdbsdk.User, error) { return nil, sealedUser },
		getByUsernameFn: func(context.Context, string, string) (*qrdbsdk.User, error) { return nil, sealedUser },
		byThumbprintFn:  func(context.Context, string) (*qrdbsdk.User, error) { return nil, sealedUser },
	}
	factorSvc := &quicMockUserAuthFactorServicer{
		addFn:  func(context.Context, uint64, *qrdbsdk.AuthFactor) error { return sealedFactor },
		listFn: func(context.Context, uint64) ([]*qrdbsdk.AuthFactor, error) { return nil, sealedFactor },
		getFn:  func(context.Context, string) (*qrdbsdk.AuthFactor, error) { return nil, sealedFactor },
		lookupFn: func(context.Context, qrdbsdk.AuthFactorType, []byte) (*qrdbsdk.AuthFactor, error) {
			return nil, sealedFactor
		},
		removeFn:  func(context.Context, string, bool) error { return sealedFactor },
		rotateFn:  func(context.Context, string, *qrdbsdk.AuthFactor) error { return sealedFactor },
		relabelFn: func(context.Context, string, string) error { return sealedFactor },
	}
	server := newQRDBUserServer(t, userSvc, factorSvc)
	ctx := context.Background()

	cases := []struct {
		name string
		want error
		call func() ([]byte, error)
	}{
		{"user.Get", sealedUser, func() ([]byte, error) {
			return server.qrdbUserGet(ctx, mustJSON(t, map[string]any{"id": 1}))
		}},
		{"user.GetByUsername", sealedUser, func() ([]byte, error) {
			return server.qrdbUserGetByUsername(ctx, mustJSON(t, map[string]any{"username": "a"}))
		}},
		{"user.GetByCertThumbprint", sealedUser, func() ([]byte, error) {
			return server.qrdbUserGetByCertThumbprint(ctx, mustJSON(t, map[string]any{"thumbprint": "aa"}))
		}},
		{"userauthfactor.Add", sealedFactor, func() ([]byte, error) {
			return server.qrdbUserFactorAdd(ctx, mustJSON(t, map[string]any{"user_id": 1, "type": "mtls", "credential": []byte{1}}))
		}},
		{"userauthfactor.List", sealedFactor, func() ([]byte, error) {
			return server.qrdbUserFactorList(ctx, mustJSON(t, map[string]any{"user_id": 1}))
		}},
		{"userauthfactor.Get", sealedFactor, func() ([]byte, error) {
			return server.qrdbUserFactorGet(ctx, mustJSON(t, map[string]any{"factor_id": "f"}))
		}},
		{"userauthfactor.Remove", sealedFactor, func() ([]byte, error) {
			return server.qrdbUserFactorRemove(ctx, mustJSON(t, map[string]any{"factor_id": "f"}))
		}},
		{"userauthfactor.Rotate", sealedFactor, func() ([]byte, error) {
			return server.qrdbUserFactorRotate(ctx, mustJSON(t, map[string]any{"old_factor_id": "f", "new_factor": map[string]any{"type": "mtls", "credential": []byte{1}}}))
		}},
		{"userauthfactor.Relabel", sealedFactor, func() ([]byte, error) {
			return server.qrdbUserFactorRelabel(ctx, mustJSON(t, map[string]any{"factor_id": "f", "label": "l"}))
		}},
		{"userauthfactor.Login", sealedFactor, func() ([]byte, error) {
			return server.qrdbUserFactorLogin(ctx, mustJSON(t, map[string]any{"type": "mtls", "lookup_key": []byte{1}}))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.call()
			require.ErrorIs(t, err, tc.want, "servicer error must propagate unchanged")
			assert.Contains(t, err.Error(), "absent or sealed")
		})
	}
}

// TestQRDBUserFactorRelabel_GetErrorAfterRelabel covers the re-read failure
// branch after a successful relabel.
func TestQRDBUserFactorRelabel_GetErrorAfterRelabel(t *testing.T) {
	sentinel := errors.New("get boom")
	svc := &quicMockUserAuthFactorServicer{
		getFn: func(context.Context, string) (*qrdbsdk.AuthFactor, error) { return nil, sentinel },
	}
	server := newQRDBUserServer(t, nil, svc)
	_, err := server.qrdbUserFactorRelabel(context.Background(),
		mustJSON(t, map[string]any{"factor_id": "f", "label": "l"}))
	require.ErrorIs(t, err, sentinel)
}
