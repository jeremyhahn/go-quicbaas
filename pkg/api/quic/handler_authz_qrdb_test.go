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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func TestGetQRDBPermission_Mapped(t *testing.T) {
	cases := []struct {
		opcode   byte
		resource string
		action   string
	}{
		{OpGet, "kv", "get"},
		{OpPut, "kv", "put"},
		{OpDelete, "kv", "delete"},
		{OpTransaction, "transaction", "execute"},
		{OpKMSGenerateKey, "keys", "create"},
		{OpCryptoSign, "keys", "sign"},
		{OpSecretsStore, "credentials", "store"},
		{OpCertIssue, "certs", "generate"},
		{OpBarrier, "tenant_barrier", "status"},
		{OpCeremony, "ceremony", "status"},
		{OpVectorSearchSimilar, "vector", "search_similar"},
		{OpCreateUser, "users", "create"},
	}
	for _, c := range cases {
		res, act, ok := getQRDBPermission(c.opcode)
		require.True(t, ok, "opcode 0x%02X", c.opcode)
		assert.Equal(t, c.resource, res, "opcode 0x%02X", c.opcode)
		assert.Equal(t, c.action, act, "opcode 0x%02X", c.opcode)
	}
}

func TestGetQRDBPermission_Unmapped(t *testing.T) {
	// Surfaces with no qrdb permission family must be unmapped (default-deny).
	for _, op := range []byte{
		OpOSPutObject,
		OpOrchestrator,
		OpParameterSet,
		OpEmbeddingGenerate,
		OpOllamaEmbed,
		OpDynamicSecretsGenerate,
	} {
		_, _, ok := getQRDBPermission(op)
		assert.False(t, ok, "opcode 0x%02X should be unmapped", op)
	}
}

// fakeQUICAuthzService permits a configured (resource, action) and denies the
// rest, recording what it saw.
type fakeQUICAuthzService struct {
	allowResource string
	allowAction   string
	lastReq       *interfaces.AuthorizationRequest
}

func (f *fakeQUICAuthzService) Authorize(_ context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	f.lastReq = req
	if req.Resource == f.allowResource && req.Action == f.allowAction {
		return &interfaces.AuthorizationDecision{Allowed: true}, nil
	}
	return &interfaces.AuthorizationDecision{Allowed: false, Reason: "denied"}, nil
}

func (f *fakeQUICAuthzService) BatchAuthorize(_ context.Context, _ []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	return nil, nil
}
func (f *fakeQUICAuthzService) GetProvider() interfaces.AuthorizationProvider { return nil }
func (f *fakeQUICAuthzService) GetProviderType() string                       { return "fake" }
func (f *fakeQUICAuthzService) Close() error                                  { return nil }

// fakeQUICValidator validates a fixed token to a fixed identity.
type fakeQUICValidator struct {
	token string
	id    *AuthzIdentity
}

func (f *fakeQUICValidator) ValidateAuthzToken(_ context.Context, token string) (*AuthzIdentity, error) {
	if token != f.token {
		return nil, assertErr
	}
	return f.id, nil
}

var assertErr = &validatorError{}

type validatorError struct{}

func (*validatorError) Error() string { return "invalid token" }

func newAuthzHandler(authz interfaces.AuthorizationService, v AuthzTokenValidator) *Handler {
	return NewHandler(nil, nil, nil, nil, nil,
		WithHandlerAuthorizationService(authz),
		WithHandlerTokenValidator(v),
	)
}

func TestQRDBAuthorizeAndInject_AllowAndDeny(t *testing.T) {
	validator := &fakeQUICValidator{token: "good", id: &AuthzIdentity{ID: "u1", TenantID: "system", Roles: []string{"admin"}}}

	// Allow: Put maps to kv:put, which the authz service permits.
	allowSvc := &fakeQUICAuthzService{allowResource: "kv", allowAction: "put"}
	h := newAuthzHandler(allowSvc, validator)
	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpPut, Token: "good"})
	assert.Nil(t, resp, "permitted op must be allowed")
	require.NotNil(t, allowSvc.lastReq)
	assert.Equal(t, "kv", allowSvc.lastReq.Resource)
	assert.Equal(t, "put", allowSvc.lastReq.Action)
	assert.Equal(t, "system", allowSvc.lastReq.Subject.TenantID)

	// Deny: Delete maps to kv:delete, which the same service does not permit.
	h2 := newAuthzHandler(&fakeQUICAuthzService{allowResource: "kv", allowAction: "put"}, validator)
	_, resp2 := h2.authorizeAndInject(context.Background(), &Request{Opcode: OpDelete, Token: "good"})
	require.NotNil(t, resp2)
	assert.Equal(t, StatusPermissionDenied, resp2.Status)
}

func TestQRDBAuthorizeAndInject_UnmappedDenied(t *testing.T) {
	validator := &fakeQUICValidator{token: "good", id: &AuthzIdentity{ID: "u1", TenantID: "system", Roles: []string{"admin"}}}
	// authz service would allow anything, but the opcode has no qrdb permission.
	svc := &fakeQUICAuthzService{allowResource: "*", allowAction: "*"}
	h := newAuthzHandler(svc, validator)
	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpOSPutObject, Token: "good"})
	require.NotNil(t, resp)
	assert.Equal(t, StatusPermissionDenied, resp.Status)
	assert.Nil(t, svc.lastReq, "unmapped opcode must not reach the authz service")
}

func TestQRDBAuthorizeAndInject_MissingToken(t *testing.T) {
	validator := &fakeQUICValidator{token: "good", id: &AuthzIdentity{ID: "u1", TenantID: "system"}}
	h := newAuthzHandler(&fakeQUICAuthzService{allowResource: "kv", allowAction: "put"}, validator)
	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpPut, Token: ""})
	require.NotNil(t, resp)
	assert.Equal(t, StatusUnauthorized, resp.Status)
}

func TestQRDBAuthorizeAndInject_InvalidToken(t *testing.T) {
	validator := &fakeQUICValidator{token: "good", id: &AuthzIdentity{ID: "u1", TenantID: "system"}}
	h := newAuthzHandler(&fakeQUICAuthzService{allowResource: "kv", allowAction: "put"}, validator)
	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpPut, Token: "bad"})
	require.NotNil(t, resp)
	assert.Equal(t, StatusUnauthorized, resp.Status)
}

func TestQRDBAuthorizeAndInject_RequireAuthzOff_NoNewDenial(t *testing.T) {
	// With NO authz service, NO validator, and NO auth manager — today's
	// RequireAuthz=false posture — every non-public op is allowed (allow-all).
	h := NewHandler(nil, nil, nil, nil, nil)
	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpPut, Token: ""})
	assert.Nil(t, resp, "RequireAuthz=false must preserve allow-all (no token, no denial)")

	// A public op is always allowed.
	_, respPub := h.authorizeAndInject(context.Background(), &Request{Opcode: OpHealth})
	assert.Nil(t, respPub)
}
