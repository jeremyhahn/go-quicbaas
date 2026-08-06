//go:build mcp

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

package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Mock UserServicer
// =============================================================================

type mcpMockUserServicer struct {
	getFn           func(ctx context.Context, id uint64) (*qrdbsdk.User, error)
	getByUsernameFn func(ctx context.Context, tenantID, username string) (*qrdbsdk.User, error)
	byThumbprintFn  func(ctx context.Context, thumbprint string) (*qrdbsdk.User, error)
}

func mcpTestUser() *qrdbsdk.User {
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

func (m *mcpMockUserServicer) CreateUser(context.Context, *qrdbsdk.CreateUserRequest) (*qrdbsdk.User, error) {
	return nil, errors.New("not implemented")
}
func (m *mcpMockUserServicer) GetUser(ctx context.Context, id uint64) (*qrdbsdk.User, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return mcpTestUser(), nil
}
func (m *mcpMockUserServicer) GetUserByUsername(ctx context.Context, tenantID, username string) (*qrdbsdk.User, error) {
	if m.getByUsernameFn != nil {
		return m.getByUsernameFn(ctx, tenantID, username)
	}
	return mcpTestUser(), nil
}
func (m *mcpMockUserServicer) UserByCertThumbprint(ctx context.Context, thumbprint string) (*qrdbsdk.User, error) {
	if m.byThumbprintFn != nil {
		return m.byThumbprintFn(ctx, thumbprint)
	}
	return mcpTestUser(), nil
}
func (m *mcpMockUserServicer) AddCertBinding(context.Context, uint64, string, int64) (*qrdbsdk.User, error) {
	return nil, errors.New("not implemented")
}
func (m *mcpMockUserServicer) UpdateUser(context.Context, *qrdbsdk.UpdateUserRequest) (*qrdbsdk.User, error) {
	return nil, errors.New("not implemented")
}
func (m *mcpMockUserServicer) DeleteUser(context.Context, uint64) error {
	return errors.New("not implemented")
}
func (m *mcpMockUserServicer) ListUsers(context.Context, *qrdbsdk.ListUsersRequest) (*qrdbsdk.ListUsersResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *mcpMockUserServicer) EnableUser(context.Context, uint64) error {
	return errors.New("not implemented")
}
func (m *mcpMockUserServicer) DisableUser(context.Context, uint64) error {
	return errors.New("not implemented")
}

var _ qrdbsdk.UserServicer = (*mcpMockUserServicer)(nil)

// =============================================================================
// Mock UserAuthFactorServicer
// =============================================================================

type mcpMockUserAuthFactorServicer struct {
	addFn     func(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error
	listFn    func(ctx context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error)
	getFn     func(ctx context.Context, factorID string) (*qrdbsdk.AuthFactor, error)
	lookupFn  func(ctx context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error)
	removeFn  func(ctx context.Context, factorID string, force bool) error
	rotateFn  func(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error
	relabelFn func(ctx context.Context, factorID, label string) error
}

func mcpTestFactor() *qrdbsdk.AuthFactor {
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

func (m *mcpMockUserAuthFactorServicer) Add(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	if m.addFn != nil {
		return m.addFn(ctx, userID, factor)
	}
	factor.ID = "factor-new"
	factor.RegisteredAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return nil
}
func (m *mcpMockUserAuthFactorServicer) List(ctx context.Context, userID uint64) ([]*qrdbsdk.AuthFactor, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID)
	}
	return []*qrdbsdk.AuthFactor{mcpTestFactor()}, nil
}
func (m *mcpMockUserAuthFactorServicer) Get(ctx context.Context, factorID string) (*qrdbsdk.AuthFactor, error) {
	if m.getFn != nil {
		return m.getFn(ctx, factorID)
	}
	return mcpTestFactor(), nil
}
func (m *mcpMockUserAuthFactorServicer) LookupByCredentialKey(ctx context.Context, t qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
	if m.lookupFn != nil {
		return m.lookupFn(ctx, t, key)
	}
	return mcpTestFactor(), nil
}
func (m *mcpMockUserAuthFactorServicer) Remove(ctx context.Context, factorID string, force bool) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, factorID, force)
	}
	return nil
}
func (m *mcpMockUserAuthFactorServicer) Rotate(ctx context.Context, oldID string, newFactor *qrdbsdk.AuthFactor) error {
	if m.rotateFn != nil {
		return m.rotateFn(ctx, oldID, newFactor)
	}
	newFactor.ID = "factor-rotated"
	newFactor.RegisteredAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return nil
}
func (m *mcpMockUserAuthFactorServicer) Relabel(ctx context.Context, factorID, label string) error {
	if m.relabelFn != nil {
		return m.relabelFn(ctx, factorID, label)
	}
	return nil
}
func (m *mcpMockUserAuthFactorServicer) MarkUsed(context.Context, string, time.Time) error {
	return nil
}

var _ qrdbsdk.UserAuthFactorServicer = (*mcpMockUserAuthFactorServicer)(nil)

// =============================================================================
// Helpers
// =============================================================================

func newQRDBUserRegistry(t *testing.T, userSvc qrdbsdk.UserServicer, factorSvc qrdbsdk.UserAuthFactorServicer) *ToolRegistry {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(&mockService{}, log)
	if userSvc != nil {
		reg.SetUserService(userSvc)
	}
	if factorSvc != nil {
		reg.SetUserAuthFactorService(factorSvc)
	}
	return reg
}

// =============================================================================
// Typed errors
// =============================================================================

func TestQRDBUserToolTypedErrors(t *testing.T) {
	assert.Equal(t, "qrdb user service not configured", (&UserServiceUnavailableError{}).Error())
	assert.Equal(t, "qrdb user auth-factor service not configured", (&UserAuthFactorServiceUnavailableError{}).Error())
	assert.Equal(t, "user_id is required", (&UserToolMissingArgumentError{Arg: "user_id"}).Error())
	assert.Equal(t, "unknown qrdb user tool: nope", NewUnknownQRDBUserToolError("nope").Error())

	assert.Equal(t, "invalid argument: credential_b64", (&UserToolInvalidArgumentError{Arg: "credential_b64"}).Error())
	wrapped := errors.New("boom")
	inv := &UserToolInvalidArgumentError{Arg: "metadata_json", Err: wrapped}
	assert.Contains(t, inv.Error(), "boom")
	assert.ErrorIs(t, inv, wrapped)
}

// =============================================================================
// Registration + membership + dispatch
// =============================================================================

func TestRegisterQRDBUserTools_AllRegistered(t *testing.T) {
	reg := newQRDBUserRegistry(t, nil, nil)
	names := make(map[string]bool)
	for _, tool := range reg.ListTools() {
		names[tool.Name] = true
	}
	for name := range qrdbUserToolNames {
		assert.True(t, names[name], "tool %s must be registered", name)
	}
}

func TestIsQRDBUserTool_MembershipMatchesDispatch(t *testing.T) {
	// Bidirectional guard: every member dispatches and every dispatch entry
	// is a member — the table cannot drift.
	for name := range qrdbUserToolNames {
		_, ok := qrdbUserToolDispatch[name]
		assert.True(t, ok, "member %s missing from dispatch table", name)
	}
	for name := range qrdbUserToolDispatch {
		assert.True(t, isQRDBUserTool(name), "dispatch entry %s missing from membership", name)
	}
	// IAM-owned RBAC user tools must NOT be captured by this surface.
	assert.False(t, isQRDBUserTool("qrdb_user_create"))
	assert.False(t, isQRDBUserTool("qrdb_user_delete"))
	assert.False(t, isQRDBUserTool("qrdb_user_list"))
	assert.False(t, isQRDBUserTool("quicbaas_user_create"))
}

func TestExecuteQRDBUserTool_UnknownName(t *testing.T) {
	reg := newQRDBUserRegistry(t, &mcpMockUserServicer{}, &mcpMockUserAuthFactorServicer{})
	_, err := reg.executeQRDBUserTool(context.Background(), "qrdb_user_bogus", nil)
	var unknown *UnknownQRDBUserToolError
	assert.ErrorAs(t, err, &unknown)
}

// =============================================================================
// User lookup tools — happy paths + wire-shape pinning
// =============================================================================

// TestQRDBUserLookupTools_HappyPaths_WireShape pins the camelCase user
// response shape the qrdb SDK MCP transport decodes (serverUser).
func TestQRDBUserLookupTools_HappyPaths_WireShape(t *testing.T) {
	reg := newQRDBUserRegistry(t, &mcpMockUserServicer{}, nil)
	ctx := context.Background()

	assertUserMap := func(t *testing.T, result interface{}) {
		t.Helper()
		m, ok := result.(map[string]interface{})
		require.True(t, ok, "result must be a map")
		for _, key := range []string{"id", "username", "tenantID", "roleIDs", "enabled", "system", "createdAt", "updatedAt"} {
			_, present := m[key]
			assert.True(t, present, "missing wire key %q", key)
		}
		assert.Len(t, m, 8, "unexpected extra keys in user response")
		assert.Equal(t, uint64(42), m["id"])
		assert.Equal(t, "alice", m["username"])
		assert.Equal(t, "tenant-a", m["tenantID"])
		assert.Equal(t, "2026-01-02T03:04:05Z", m["createdAt"])
	}

	// user_id arrives as a decimal string from the qrdb MCP transport.
	got, err := reg.executeQRDBUserTool(ctx, "qrdb_user_get", map[string]interface{}{"id": "42"})
	require.NoError(t, err)
	assertUserMap(t, got)

	got, err = reg.executeQRDBUserTool(ctx, "qrdb_user_get_by_username", map[string]interface{}{
		"tenant_id": "tenant-a", "username": "alice",
	})
	require.NoError(t, err)
	assertUserMap(t, got)

	got, err = reg.executeQRDBUserTool(ctx, "qrdb_user_get_by_cert_thumbprint", map[string]interface{}{
		"thumbprint": "aa",
	})
	require.NoError(t, err)
	assertUserMap(t, got)
}

func TestQRDBUserGetByUsername_ForwardsTenantAndUsername(t *testing.T) {
	var gotTenant, gotUsername string
	svc := &mcpMockUserServicer{
		getByUsernameFn: func(_ context.Context, tenantID, username string) (*qrdbsdk.User, error) {
			gotTenant, gotUsername = tenantID, username
			return mcpTestUser(), nil
		},
	}
	reg := newQRDBUserRegistry(t, svc, nil)
	_, err := reg.executeQRDBUserTool(context.Background(), "qrdb_user_get_by_username", map[string]interface{}{
		"tenant_id": "t-9", "username": "bob",
	})
	require.NoError(t, err)
	assert.Equal(t, "t-9", gotTenant)
	assert.Equal(t, "bob", gotUsername)
}

func TestQRDBUserGet_NilRoleIDsBecomesEmptySlice(t *testing.T) {
	svc := &mcpMockUserServicer{
		getFn: func(context.Context, uint64) (*qrdbsdk.User, error) {
			u := mcpTestUser()
			u.RoleIDs = nil
			return u, nil
		},
	}
	reg := newQRDBUserRegistry(t, svc, nil)
	got, err := reg.executeQRDBUserTool(context.Background(), "qrdb_user_get", map[string]interface{}{"id": "42"})
	require.NoError(t, err)
	m := got.(map[string]interface{})
	assert.Equal(t, []string{}, m["roleIDs"], "roleIDs must marshal as [] not null")
}

// =============================================================================
// Factor tools — happy paths + wire-shape pinning
// =============================================================================

// TestQRDBUserFactorTools_HappyPaths_WireShape pins the snake_case,
// base64-credential factor response shape the qrdb SDK MCP transport decodes
// (mcpFactorRecord).
func TestQRDBUserFactorTools_HappyPaths_WireShape(t *testing.T) {
	ctx := context.Background()
	credB64 := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})

	assertFactorMap := func(t *testing.T, raw interface{}) {
		t.Helper()
		m, ok := raw.(map[string]interface{})
		require.True(t, ok, "factor must be a map")
		assert.Equal(t, "factor-1", m["id"])
		assert.Equal(t, uint64(42), m["user_id"])
		assert.Equal(t, "mtls", m["type"])
		assert.Equal(t, "work laptop", m["label"])
		assert.Equal(t, credB64, m["credential"], "credential must be base64-encoded")
		assert.Equal(t, base64.StdEncoding.EncodeToString([]byte{0x03, 0x04}), m["spki_credential"])
		assert.Equal(t, "2026-01-02T03:04:05Z", m["registered_at"])
		assert.Equal(t, "2026-02-03T04:05:06Z", m["last_used_at"])
		assert.Equal(t, "admin", m["created_by"])
		_, hasRevoked := m["revoked_at"]
		assert.False(t, hasRevoked, "revoked_at must be omitted when nil")
	}

	t.Run("add", func(t *testing.T) {
		var gotUserID uint64
		svc := &mcpMockUserAuthFactorServicer{
			addFn: func(_ context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
				gotUserID = userID
				factor.ID = "factor-new"
				factor.RegisteredAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
				return nil
			},
		}
		reg := newQRDBUserRegistry(t, nil, svc)
		got, err := reg.executeQRDBUserTool(ctx, "qrdb_user_authfactor_add", map[string]interface{}{
			"user_id":        "42",
			"type":           "mtls",
			"label":          "work laptop",
			"credential_b64": credB64,
			"metadata_json":  `{"serial":"abc"}`,
			"created_by":     "admin",
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(42), gotUserID)
		resp := got.(map[string]interface{})
		factor := resp["factor"].(map[string]interface{})
		assert.Equal(t, "factor-new", factor["id"], "add must echo the service-assigned ID")
		assert.Equal(t, credB64, factor["credential"])
		assert.Equal(t, "admin", factor["created_by"])
	})

	t.Run("list", func(t *testing.T) {
		reg := newQRDBUserRegistry(t, nil, &mcpMockUserAuthFactorServicer{})
		got, err := reg.executeQRDBUserTool(ctx, "qrdb_user_authfactor_list", map[string]interface{}{"user_id": "42"})
		require.NoError(t, err)
		resp := got.(map[string]interface{})
		factors := resp["factors"].([]map[string]interface{})
		require.Len(t, factors, 1)
		assert.Equal(t, 1, resp["total"])
		assertFactorMap(t, factors[0])
	})

	t.Run("get", func(t *testing.T) {
		reg := newQRDBUserRegistry(t, nil, &mcpMockUserAuthFactorServicer{})
		got, err := reg.executeQRDBUserTool(ctx, "qrdb_user_authfactor_get", map[string]interface{}{"factor_id": "factor-1"})
		require.NoError(t, err)
		resp := got.(map[string]interface{})
		assertFactorMap(t, resp["factor"])
	})

	t.Run("remove", func(t *testing.T) {
		var gotForce bool
		svc := &mcpMockUserAuthFactorServicer{
			removeFn: func(_ context.Context, _ string, force bool) error {
				gotForce = force
				return nil
			},
		}
		reg := newQRDBUserRegistry(t, nil, svc)
		got, err := reg.executeQRDBUserTool(ctx, "qrdb_user_authfactor_remove", map[string]interface{}{
			"factor_id": "factor-1", "force": true,
		})
		require.NoError(t, err)
		assert.True(t, gotForce)
		resp := got.(map[string]interface{})
		assert.Equal(t, "revoked", resp["status"])
		assert.Equal(t, "factor-1", resp["factor_id"])
	})

	t.Run("rotate", func(t *testing.T) {
		reg := newQRDBUserRegistry(t, nil, &mcpMockUserAuthFactorServicer{})
		got, err := reg.executeQRDBUserTool(ctx, "qrdb_user_authfactor_rotate", map[string]interface{}{
			"old_factor_id":  "factor-1",
			"type":           "mtls",
			"credential_b64": credB64,
		})
		require.NoError(t, err)
		resp := got.(map[string]interface{})
		// MCP rotate responds under "factor" (the QUIC wire uses "new_factor").
		factor, ok := resp["factor"].(map[string]interface{})
		require.True(t, ok, "rotate response must use the factor envelope key")
		assert.Equal(t, "factor-rotated", factor["id"])
	})

	t.Run("relabel", func(t *testing.T) {
		var gotLabel string
		svc := &mcpMockUserAuthFactorServicer{
			relabelFn: func(_ context.Context, _ string, label string) error {
				gotLabel = label
				return nil
			},
		}
		reg := newQRDBUserRegistry(t, nil, svc)
		got, err := reg.executeQRDBUserTool(ctx, "qrdb_user_authfactor_relabel", map[string]interface{}{
			"factor_id": "factor-1", "label": "new label",
		})
		require.NoError(t, err)
		assert.Equal(t, "new label", gotLabel)
		resp := got.(map[string]interface{})
		assertFactorMap(t, resp["factor"])
	})

	t.Run("login", func(t *testing.T) {
		var gotType qrdbsdk.AuthFactorType
		var gotKey []byte
		svc := &mcpMockUserAuthFactorServicer{
			lookupFn: func(_ context.Context, typ qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
				gotType, gotKey = typ, key
				return mcpTestFactor(), nil
			},
		}
		reg := newQRDBUserRegistry(t, nil, svc)
		got, err := reg.executeQRDBUserTool(ctx, "qrdb_user_authfactor_login", map[string]interface{}{
			"type":           "mtls",
			"lookup_key_b64": base64.StdEncoding.EncodeToString([]byte{0x0a, 0x0b}),
		})
		require.NoError(t, err)
		assert.Equal(t, qrdbsdk.AuthFactorType("mtls"), gotType)
		assert.Equal(t, []byte{0x0a, 0x0b}, gotKey)
		resp := got.(map[string]interface{})
		assert.Equal(t, uint64(42), resp["user_id"])
		assert.Equal(t, true, resp["active"])
		assertFactorMap(t, resp["factor"])
	})
}

// =============================================================================
// Unavailable (not wired) errors
// =============================================================================

func TestQRDBUserTools_ServiceUnavailable(t *testing.T) {
	reg := newQRDBUserRegistry(t, nil, nil)
	ctx := context.Background()

	for _, name := range []string{"qrdb_user_get", "qrdb_user_get_by_username", "qrdb_user_get_by_cert_thumbprint"} {
		_, err := reg.executeQRDBUserTool(ctx, name, map[string]interface{}{})
		var unavailable *UserServiceUnavailableError
		assert.ErrorAs(t, err, &unavailable, "%s must fail closed", name)
	}
	for _, name := range []string{
		"qrdb_user_authfactor_add", "qrdb_user_authfactor_list",
		"qrdb_user_authfactor_get", "qrdb_user_authfactor_remove",
		"qrdb_user_authfactor_rotate", "qrdb_user_authfactor_relabel",
		"qrdb_user_authfactor_login",
	} {
		_, err := reg.executeQRDBUserTool(ctx, name, map[string]interface{}{})
		var unavailable *UserAuthFactorServiceUnavailableError
		assert.ErrorAs(t, err, &unavailable, "%s must fail closed", name)
	}
}

// =============================================================================
// Missing / invalid argument errors
// =============================================================================

func TestQRDBUserTools_MissingArguments(t *testing.T) {
	reg := newQRDBUserRegistry(t, &mcpMockUserServicer{}, &mcpMockUserAuthFactorServicer{})
	ctx := context.Background()

	cases := []struct {
		tool string
		args map[string]interface{}
		arg  string
	}{
		{"qrdb_user_get", map[string]interface{}{}, "id"},
		{"qrdb_user_get_by_username", map[string]interface{}{}, "username"},
		{"qrdb_user_get_by_cert_thumbprint", map[string]interface{}{}, "thumbprint"},
		{"qrdb_user_authfactor_add", map[string]interface{}{}, "user_id"},
		{"qrdb_user_authfactor_add", map[string]interface{}{"user_id": "1"}, "type"},
		{"qrdb_user_authfactor_add", map[string]interface{}{"user_id": "1", "type": "mtls"}, "credential_b64"},
		{"qrdb_user_authfactor_list", map[string]interface{}{}, "user_id"},
		{"qrdb_user_authfactor_get", map[string]interface{}{}, "factor_id"},
		{"qrdb_user_authfactor_remove", map[string]interface{}{}, "factor_id"},
		{"qrdb_user_authfactor_rotate", map[string]interface{}{}, "old_factor_id"},
		{"qrdb_user_authfactor_relabel", map[string]interface{}{}, "factor_id"},
		{"qrdb_user_authfactor_relabel", map[string]interface{}{"factor_id": "f"}, "label"},
		{"qrdb_user_authfactor_login", map[string]interface{}{}, "type"},
		{"qrdb_user_authfactor_login", map[string]interface{}{"type": "mtls"}, "lookup_key_b64"},
	}
	for _, tc := range cases {
		t.Run(tc.tool+"/"+tc.arg, func(t *testing.T) {
			_, err := reg.executeQRDBUserTool(ctx, tc.tool, tc.args)
			var missing *UserToolMissingArgumentError
			require.ErrorAs(t, err, &missing)
			assert.Equal(t, tc.arg, missing.Arg)
		})
	}
}

func TestQRDBUserTools_InvalidArguments(t *testing.T) {
	reg := newQRDBUserRegistry(t, nil, &mcpMockUserAuthFactorServicer{})
	ctx := context.Background()

	cases := []struct {
		name string
		tool string
		args map[string]interface{}
		arg  string
	}{
		{"add bad credential b64", "qrdb_user_authfactor_add",
			map[string]interface{}{"user_id": "1", "type": "mtls", "credential_b64": "!!!"}, "credential_b64"},
		{"add bad spki b64", "qrdb_user_authfactor_add",
			map[string]interface{}{"user_id": "1", "type": "mtls", "credential_b64": "AQ==", "spki_credential_b64": "!!!"}, "spki_credential_b64"},
		{"add bad metadata json", "qrdb_user_authfactor_add",
			map[string]interface{}{"user_id": "1", "type": "mtls", "credential_b64": "AQ==", "metadata_json": "{bad"}, "metadata_json"},
		{"rotate bad credential b64", "qrdb_user_authfactor_rotate",
			map[string]interface{}{"old_factor_id": "f", "type": "mtls", "credential_b64": "!!!"}, "credential_b64"},
		{"login bad lookup key b64", "qrdb_user_authfactor_login",
			map[string]interface{}{"type": "mtls", "lookup_key_b64": "!!!"}, "lookup_key_b64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reg.executeQRDBUserTool(ctx, tc.tool, tc.args)
			var invalid *UserToolInvalidArgumentError
			require.ErrorAs(t, err, &invalid)
			assert.Equal(t, tc.arg, invalid.Arg)
		})
	}
}

// =============================================================================
// Service-error propagation (sealed-state typed errors travel unchanged)
// =============================================================================

// mcpSealedError stands in for the pkg/server proxy's typed
// QRDBUserServiceUnavailableError (import cycle prevents using it directly).
type mcpSealedError struct{ surface string }

func (e *mcpSealedError) Error() string {
	return "qrdb " + e.surface + " service unavailable (embedded node absent or sealed)"
}

func TestQRDBUserTools_ServiceErrorsPropagateUnchanged(t *testing.T) {
	sealedUser := &mcpSealedError{surface: "user"}
	sealedFactor := &mcpSealedError{surface: "user_authfactor"}

	userSvc := &mcpMockUserServicer{
		getFn:           func(context.Context, uint64) (*qrdbsdk.User, error) { return nil, sealedUser },
		getByUsernameFn: func(context.Context, string, string) (*qrdbsdk.User, error) { return nil, sealedUser },
		byThumbprintFn:  func(context.Context, string) (*qrdbsdk.User, error) { return nil, sealedUser },
	}
	factorSvc := &mcpMockUserAuthFactorServicer{
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
	reg := newQRDBUserRegistry(t, userSvc, factorSvc)
	ctx := context.Background()

	cases := []struct {
		tool string
		args map[string]interface{}
		want error
	}{
		{"qrdb_user_get", map[string]interface{}{"id": "1"}, sealedUser},
		{"qrdb_user_get_by_username", map[string]interface{}{"username": "a"}, sealedUser},
		{"qrdb_user_get_by_cert_thumbprint", map[string]interface{}{"thumbprint": "aa"}, sealedUser},
		{"qrdb_user_authfactor_add", map[string]interface{}{"user_id": "1", "type": "mtls", "credential_b64": "AQ=="}, sealedFactor},
		{"qrdb_user_authfactor_list", map[string]interface{}{"user_id": "1"}, sealedFactor},
		{"qrdb_user_authfactor_get", map[string]interface{}{"factor_id": "f"}, sealedFactor},
		{"qrdb_user_authfactor_remove", map[string]interface{}{"factor_id": "f"}, sealedFactor},
		{"qrdb_user_authfactor_rotate", map[string]interface{}{"old_factor_id": "f", "type": "mtls", "credential_b64": "AQ=="}, sealedFactor},
		{"qrdb_user_authfactor_relabel", map[string]interface{}{"factor_id": "f", "label": "l"}, sealedFactor},
		{"qrdb_user_authfactor_login", map[string]interface{}{"type": "mtls", "lookup_key_b64": "AQ=="}, sealedFactor},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			_, err := reg.executeQRDBUserTool(ctx, tc.tool, tc.args)
			require.ErrorIs(t, err, tc.want, "servicer error must propagate unchanged")
			assert.Contains(t, err.Error(), "absent or sealed")
		})
	}
}

// TestQRDBUserFactorRelabel_GetErrorAfterRelabel covers the re-read failure
// branch after a successful relabel.
func TestQRDBUserFactorRelabelTool_GetErrorAfterRelabel(t *testing.T) {
	sentinel := errors.New("get boom")
	svc := &mcpMockUserAuthFactorServicer{
		getFn: func(context.Context, string) (*qrdbsdk.AuthFactor, error) { return nil, sentinel },
	}
	reg := newQRDBUserRegistry(t, nil, svc)
	_, err := reg.executeQRDBUserTool(context.Background(), "qrdb_user_authfactor_relabel",
		map[string]interface{}{"factor_id": "f", "label": "l"})
	require.ErrorIs(t, err, sentinel)
}

// =============================================================================
// Authz mapping — qrdb-canonical vocabulary
// =============================================================================

// TestQRDBUserToolAuthzMappings pins the (resource, action) tuple for every
// qrdb user/factor tool byte-for-byte against the qrdb-registered users:* and
// user_authfactors:* permission families — the same vocabulary the REST scope
// rules use for /users and /v1/users/factors.
func TestQRDBUserToolAuthzMappings(t *testing.T) {
	expected := map[string]struct{ resource, action string }{
		"qrdb_user_get":                    {"users", "read"},
		"qrdb_user_get_by_username":        {"users", "read"},
		"qrdb_user_get_by_cert_thumbprint": {"users", "get_by_cert_thumbprint"},
		"qrdb_user_authfactor_add":         {"user_authfactors", "add"},
		"qrdb_user_authfactor_list":        {"user_authfactors", "list"},
		"qrdb_user_authfactor_get":         {"user_authfactors", "get"},
		"qrdb_user_authfactor_remove":      {"user_authfactors", "remove"},
		"qrdb_user_authfactor_rotate":      {"user_authfactors", "rotate"},
		"qrdb_user_authfactor_relabel":     {"user_authfactors", "relabel"},
		"qrdb_user_authfactor_login":       {"user_authfactors", "login"},
	}
	for tool, want := range expected {
		t.Run(tool, func(t *testing.T) {
			resource, action, ok := getToolQRDBPermission(tool)
			require.True(t, ok, "tool %s must be mapped (default-deny otherwise)", tool)
			assert.Equal(t, want.resource, resource)
			assert.Equal(t, want.action, action)
		})
	}

	// Every registered qrdb user/factor tool must be mapped — an unmapped
	// tool is denied under RequireAuthz.
	for name := range qrdbUserToolNames {
		_, _, ok := getToolQRDBPermission(name)
		assert.True(t, ok, "registered tool %s missing from qrdbToolExactMapping", name)
	}
}

// TestQRDBUserToolLegacyAuthzMappings pins the legacy (interfaces.Resource*)
// mapping treatment: reads map to ActionRead, enroll/rotate/relabel to
// ActionWrite, remove to ActionDelete — mirroring the qrdb_credential_*
// entries. Without exact entries these tools would fall through to the
// cluster-admin default.
func TestQRDBUserToolLegacyAuthzMappings(t *testing.T) {
	cases := map[string]struct{ resource, action string }{
		"qrdb_user_get":                    {"users", "read"},
		"qrdb_user_get_by_username":        {"users", "read"},
		"qrdb_user_get_by_cert_thumbprint": {"users", "read"},
		"qrdb_user_authfactor_list":        {"users", "read"},
		"qrdb_user_authfactor_get":         {"users", "read"},
		"qrdb_user_authfactor_login":       {"users", "read"},
		"qrdb_user_authfactor_add":         {"users", "write"},
		"qrdb_user_authfactor_rotate":      {"users", "write"},
		"qrdb_user_authfactor_relabel":     {"users", "write"},
		"qrdb_user_authfactor_remove":      {"users", "delete"},
	}
	for tool, want := range cases {
		t.Run(tool, func(t *testing.T) {
			resource, action := getToolResourceAction(tool)
			assert.Equal(t, want.resource, resource)
			assert.Equal(t, want.action, action)
		})
	}
}
