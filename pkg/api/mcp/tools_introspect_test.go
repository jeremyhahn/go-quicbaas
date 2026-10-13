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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const introspectTool = "quicbaas_introspecttoken"

func newIntrospectRegistry(t *testing.T) (*ToolRegistry, *MockQuicBaaS) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	svc := &MockQuicBaaS{}
	t.Cleanup(func() { svc.AssertExpectations(t) })
	return NewToolRegistry(svc, log), svc
}

func TestIntrospectTokenTool_ActiveCarriesClaims(t *testing.T) {
	reg, svc := newIntrospectRegistry(t)
	svc.On("IntrospectToken", mock.Anything, &service.IntrospectTokenRequest{Token: "live"}).
		Return(&service.TokenIntrospection{
			Active:       true,
			Subject:      "user-1",
			ExpiresAt:    time.Unix(3000, 0).UTC(),
			AttestLevel:  "hardware",
			AttestAAGUID: "cb69481e8ff7403993ec0a2729a154a8",
		}, nil).Once()

	got, err := reg.ExecuteTool(context.Background(), introspectTool, map[string]interface{}{"token": "live"})
	require.NoError(t, err)
	claims, ok := got.(map[string]interface{})
	require.True(t, ok, "expected a JSON object, got %T", got)
	assert.Equal(t, true, claims["Active"])
	assert.Equal(t, "user-1", claims["Subject"])
	assert.Equal(t, "hardware", claims["AttestLevel"])
	assert.Equal(t, "cb69481e8ff7403993ec0a2729a154a8", claims["AttestAAGUID"])
}

func TestIntrospectTokenTool_InactiveCarriesOnlyActive(t *testing.T) {
	reg, svc := newIntrospectRegistry(t)
	svc.On("IntrospectToken", mock.Anything, &service.IntrospectTokenRequest{Token: "garbage"}).
		Return(&service.TokenIntrospection{Active: false}, nil).Once()

	got, err := reg.ExecuteTool(context.Background(), introspectTool, map[string]interface{}{"token": "garbage"})
	require.NoError(t, err)
	claims, ok := got.(map[string]interface{})
	require.True(t, ok, "expected a JSON object, got %T", got)
	assert.Equal(t, false, claims["Active"])
	assert.Empty(t, claims["Subject"])
	assert.Empty(t, claims["AttestLevel"])
}

func TestIntrospectTokenTool_MissingTokenIsInvalidInput(t *testing.T) {
	reg, svc := newIntrospectRegistry(t)
	svc.On("IntrospectToken", mock.Anything, &service.IntrospectTokenRequest{}).
		Return(nil, &qberrors.QBError{
			Code: qberrors.ErrInvalidInput,
			Op:   "service.IntrospectToken",
			Err:  &api.RequiredFieldError{Field: "token"},
		}).Once()

	got, err := reg.ExecuteTool(context.Background(), introspectTool, map[string]interface{}{})
	var qbErr *qberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, qberrors.ErrInvalidInput, qbErr.Code)
	assert.Nil(t, got)
}

// TestIntrospectTokenTool_RequiresAuthenticatedGrant keeps MCP aligned with
// REST, which refuses an uncredentialed caller: the tool is never public and
// carries the grant the gRPC IntrospectToken RPC declares.
func TestIntrospectTokenTool_RequiresAuthenticatedGrant(t *testing.T) {
	assert.Equal(t, qrdbToolResource{"user_authfactors", "login"}, qrdbToolExactMapping[introspectTool])

	for _, spec := range ModuleTools() {
		if spec.Name != introspectTool {
			continue
		}
		assert.False(t, spec.Public, "introspection must not be reachable unauthenticated")
		assert.Equal(t, "user_authfactors", spec.Resource)
		assert.Equal(t, "login", spec.Action)
		return
	}
	t.Fatalf("%s is not exposed as a module tool", introspectTool)
}
