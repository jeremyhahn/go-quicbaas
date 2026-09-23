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
	"encoding/json"
	"errors"
	"testing"

	transport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// errMCPFIDO2 drives error propagation through the FIDO2 tool handlers.
var errMCPFIDO2 = errors.New("fido2: boom")

// fido2FakeService records its input (including the tenant scope) and
// replays a fixed response.
type fido2FakeService struct {
	failNext bool

	lastTenant     string
	lastBeginReg   *transport.BeginRegistrationRequest
	lastFinishReg  *transport.FinishRegistrationRequest
	lastBeginAuth  *transport.BeginAuthenticationRequest
	lastFinishAuth *transport.FinishAuthenticationRequest
}

func (f *fido2FakeService) BeginRegistration(_ context.Context, tenantID string, req *transport.BeginRegistrationRequest) (*transport.BeginRegistrationResponse, error) {
	f.lastTenant = tenantID
	f.lastBeginReg = req
	if f.failNext {
		return nil, errMCPFIDO2
	}
	return &transport.BeginRegistrationResponse{SessionID: "sess-1", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (f *fido2FakeService) FinishRegistration(_ context.Context, tenantID string, req *transport.FinishRegistrationRequest) (*transport.FinishRegistrationResponse, error) {
	f.lastTenant = tenantID
	f.lastFinishReg = req
	if f.failNext {
		return nil, errMCPFIDO2
	}
	return &transport.FinishRegistrationResponse{Success: true, CredentialID: "cred-1", Token: "tok"}, nil
}

func (f *fido2FakeService) BeginAuthentication(_ context.Context, tenantID string, req *transport.BeginAuthenticationRequest) (*transport.BeginAuthenticationResponse, error) {
	f.lastTenant = tenantID
	f.lastBeginAuth = req
	if f.failNext {
		return nil, errMCPFIDO2
	}
	return &transport.BeginAuthenticationResponse{SessionID: "sess-2", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (f *fido2FakeService) FinishAuthentication(_ context.Context, tenantID string, req *transport.FinishAuthenticationRequest) (*transport.FinishAuthenticationResponse, error) {
	f.lastTenant = tenantID
	f.lastFinishAuth = req
	if f.failNext {
		return nil, errMCPFIDO2
	}
	return &transport.FinishAuthenticationResponse{Success: true, Token: "tok", Username: req.Email}, nil
}

// newFIDO2Registry builds a ToolRegistry whose FIDO2() returns the supplied
// fake.
func newFIDO2Registry(t *testing.T, fake *fido2FakeService) *ToolRegistry {
	t.Helper()
	log := newTestLogger(t)
	return NewToolRegistry(&mockService{fido2Svc: fake}, log)
}

func TestXKMSFIDO2BeginRegistration_HappyPath(t *testing.T) {
	fake := &fido2FakeService{}
	registry := newFIDO2Registry(t, fake)

	result, err := registry.handleXKMSFIDO2BeginRegistration(context.Background(), map[string]interface{}{
		"tenant_id":    "tenant-a",
		"email":        "alice@example.com",
		"display_name": "Alice",
	})
	require.NoError(t, err)

	m, ok := result.(map[string]interface{})
	require.True(t, ok, "expected JSON object result, got %T", result)
	assert.Equal(t, "sess-1", m["session_id"])
	assert.NotEmpty(t, m["options_json"])

	assert.Equal(t, "tenant-a", fake.lastTenant)
	require.NotNil(t, fake.lastBeginReg)
	assert.Equal(t, "alice@example.com", fake.lastBeginReg.Email)
	assert.Equal(t, "Alice", fake.lastBeginReg.DisplayName)
}

func TestXKMSFIDO2BeginRegistration_TenantFromContext(t *testing.T) {
	fake := &fido2FakeService{}
	registry := newFIDO2Registry(t, fake)

	ctx := iam.ContextWithTenant(context.Background(), "ctx-tenant")
	_, err := registry.handleXKMSFIDO2BeginRegistration(ctx, map[string]interface{}{
		"email": "alice@example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, "ctx-tenant", fake.lastTenant)
}

func TestXKMSFIDO2BeginRegistration_MissingEmail(t *testing.T) {
	registry := newFIDO2Registry(t, &fido2FakeService{})

	_, err := registry.handleXKMSFIDO2BeginRegistration(context.Background(), map[string]interface{}{
		"tenant_id": "tenant-a",
	})
	require.Error(t, err)
}

func TestXKMSFIDO2FinishRegistration_HappyPath(t *testing.T) {
	fake := &fido2FakeService{}
	registry := newFIDO2Registry(t, fake)

	result, err := registry.handleXKMSFIDO2FinishRegistration(context.Background(), map[string]interface{}{
		"tenant_id":        "tenant-a",
		"session_id":       "sess-1",
		"attestation_json": `{"id":"cred"}`,
	})
	require.NoError(t, err)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, m["success"])
	assert.Equal(t, "cred-1", m["credential_id"])

	require.NotNil(t, fake.lastFinishReg)
	assert.Equal(t, "sess-1", fake.lastFinishReg.SessionID)
}

func TestXKMSFIDO2FinishRegistration_MissingArgs(t *testing.T) {
	registry := newFIDO2Registry(t, &fido2FakeService{})

	_, err := registry.handleXKMSFIDO2FinishRegistration(context.Background(), map[string]interface{}{
		"session_id": "sess-1",
	})
	require.Error(t, err)

	_, err = registry.handleXKMSFIDO2FinishRegistration(context.Background(), map[string]interface{}{
		"attestation_json": `{}`,
	})
	require.Error(t, err)
}

func TestXKMSFIDO2AuthenticationCeremony_HappyPath(t *testing.T) {
	fake := &fido2FakeService{}
	registry := newFIDO2Registry(t, fake)

	begin, err := registry.handleXKMSFIDO2BeginAuthentication(context.Background(), map[string]interface{}{
		"tenant_id": "tenant-b",
		"email":     "bob@example.com",
	})
	require.NoError(t, err)
	bm, ok := begin.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "sess-2", bm["session_id"])
	require.NotNil(t, fake.lastBeginAuth)
	assert.Equal(t, "bob@example.com", fake.lastBeginAuth.Email)

	finish, err := registry.handleXKMSFIDO2FinishAuthentication(context.Background(), map[string]interface{}{
		"tenant_id":      "tenant-b",
		"session_id":     "sess-2",
		"email":          "bob@example.com",
		"assertion_json": `{"id":"cred"}`,
	})
	require.NoError(t, err)
	fm, ok := finish.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, fm["success"])
	assert.Equal(t, "tok", fm["token"])
	require.NotNil(t, fake.lastFinishAuth)
	assert.Equal(t, `{"id":"cred"}`, fake.lastFinishAuth.AssertionJSON)
}

func TestXKMSFIDO2FinishAuthentication_MissingArgs(t *testing.T) {
	registry := newFIDO2Registry(t, &fido2FakeService{})

	for _, args := range []map[string]interface{}{
		{"email": "bob", "assertion_json": `{}`},    // missing session_id
		{"session_id": "s", "assertion_json": `{}`}, // missing email
		{"session_id": "s", "email": "bob"},         // missing assertion_json
	} {
		_, err := registry.handleXKMSFIDO2FinishAuthentication(context.Background(), args)
		require.Error(t, err, "%v", args)
	}
}

func TestXKMSFIDO2Handlers_ServiceErrorPropagates(t *testing.T) {
	fake := &fido2FakeService{failNext: true}
	registry := newFIDO2Registry(t, fake)

	_, err := registry.handleXKMSFIDO2BeginRegistration(context.Background(), map[string]interface{}{
		"tenant_id": "t", "email": "alice",
	})
	require.Error(t, err)
	var opErr *XKMSOperationError
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, "fido2.beginRegistration", opErr.Operation)
}

func TestXKMSFIDO2Handlers_NilServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	registry := NewToolRegistry(&mockService{}, log)

	_, err := registry.handleXKMSFIDO2BeginAuthentication(context.Background(), map[string]interface{}{
		"tenant_id": "t", "email": "alice",
	})
	require.Error(t, err)
}

// TestXKMSPIVImportExport_NotInitialized pins the fail-closed behavior of the
// two new PIV tools when the composed xkms service singleton is down: the
// initialization guard runs before any argument validation, matching the six
// sibling PIV tools.
func TestXKMSPIVImportExport_NotInitialized(t *testing.T) {
	log := newTestLogger(t)
	registry := NewToolRegistry(&mockService{}, log)

	_, err := registry.handleXKMSPIVImportCert(context.Background(), map[string]interface{}{
		"backend": "pkcs11", "slot": "9a", "certificate": "Y2VydA==", "format": "der",
	})
	require.Error(t, err)
	var initErr *XKMSNotInitializedError
	assert.ErrorAs(t, err, &initErr)

	_, err = registry.handleXKMSPIVExportCert(context.Background(), map[string]interface{}{
		"backend": "pkcs11", "slot": "9a",
	})
	require.Error(t, err)
	assert.ErrorAs(t, err, &initErr)
}

// TestXKMSFIDO2ToolsRegisteredAndMapped verifies registration + authz mapping
// for the six new tools, so none of them ships dead (default-deny filters
// unmapped tools out of tools/list).
func TestXKMSFIDO2ToolsRegisteredAndMapped(t *testing.T) {
	log := newTestLogger(t)
	registry := NewToolRegistry(&mockService{}, log)

	newTools := []string{
		"xkms.piv.importCert",
		"xkms.piv.exportCert",
		"xkms.fido2.beginRegistration",
		"xkms.fido2.finishRegistration",
		"xkms.fido2.beginAuthentication",
		"xkms.fido2.finishAuthentication",
	}

	tools := registry.ListTools()
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}
	for _, name := range newTools {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}

	// Authz mapping parity with the QUIC sub-op tables and REST PEP rules.
	wantPerms := map[string][2]string{
		"xkms.piv.importCert":             {"piv", "write"},
		"xkms.piv.exportCert":             {"piv", "read"},
		"xkms.fido2.beginRegistration":    {"user_authfactors", "add"},
		"xkms.fido2.finishRegistration":   {"user_authfactors", "add"},
		"xkms.fido2.beginAuthentication":  {"user_authfactors", "get"},
		"xkms.fido2.finishAuthentication": {"user_authfactors", "get"},
	}
	for name, want := range wantPerms {
		res, act, ok := getToolQRDBPermission(name)
		require.True(t, ok, "tool %q has no qrdb permission mapping", name)
		assert.Equal(t, want[0], res, name)
		assert.Equal(t, want[1], act, name)
	}
}

// TestXKMSFIDO2Handlers_AcceptSDKSerializedRequests is the regression for the
// login-key spelling across the MCP boundary: the SDK's MCP transport
// forwards the go-xkms request struct as its JSON, and since go-xkms v0.17
// (go-xkms#628) that JSON carries the login key as "email" only. The server
// handlers must accept exactly those arguments, so they are built here the way
// the SDK builds them rather than written by hand.
func TestXKMSFIDO2Handlers_AcceptSDKSerializedRequests(t *testing.T) {
	fake := &fido2FakeService{}
	registry := newFIDO2Registry(t, fake)
	ctx := iam.ContextWithTenant(context.Background(), "tenant-sdk")

	_, err := registry.handleXKMSFIDO2BeginRegistration(ctx, sdkArgs(t, &transport.BeginRegistrationRequest{
		Email: "carol@example.com", DisplayName: "Carol",
	}))
	require.NoError(t, err, "beginRegistration must accept the SDK's arguments")
	assert.Equal(t, "carol@example.com", fake.lastBeginReg.Email)

	_, err = registry.handleXKMSFIDO2BeginAuthentication(ctx, sdkArgs(t, &transport.BeginAuthenticationRequest{
		Email: "carol@example.com",
	}))
	require.NoError(t, err, "beginAuthentication must accept the SDK's arguments")
	assert.Equal(t, "carol@example.com", fake.lastBeginAuth.Email)

	_, err = registry.handleXKMSFIDO2FinishAuthentication(ctx, sdkArgs(t, &transport.FinishAuthenticationRequest{
		SessionID: "sess-2", Email: "carol@example.com", AssertionJSON: `{"id":"cred"}`,
	}))
	require.NoError(t, err, "finishAuthentication must accept the SDK's arguments")
	assert.Equal(t, "carol@example.com", fake.lastFinishAuth.Email)
}

// sdkArgs serializes a go-xkms request into MCP tool arguments the way the
// SDK's MCP transport does (sdk/go/transport/mcp fido2MCPHandler).
func sdkArgs(t *testing.T, req interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(req)
	require.NoError(t, err)
	var args map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &args))
	return args
}
