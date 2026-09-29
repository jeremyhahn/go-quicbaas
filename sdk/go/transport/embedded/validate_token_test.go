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

package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	authclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
)

// validatedBearer is what the service's session authority reports for a
// bearer issued into tenant "acme".
func validatedBearer(_ context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	if req.AccessToken != "login-bearer" {
		return nil, errors.New("unexpected token")
	}
	return &service.ValidateTokenResponse{
		Valid:     true,
		UserID:    "user-1",
		Username:  "alice@example.com",
		TenantID:  "acme",
		Roles:     []string{"admin"},
		ExpiresAt: 1700003600,
	}, nil
}

// The embedded handler must answer with *service.TokenClaims — the type every
// remote transport produces and the one the auth client asserts — rather than
// the service's *service.ValidateTokenResponse (go-quicbaas#955).
func TestHandleValidateToken_ReturnsTokenClaims(t *testing.T) {
	tr, err := New(&mockQuicBaaS{validateTokenFunc: validatedBearer})
	require.NoError(t, err)

	resp, err := tr.Request(context.Background(), "quicbaas.Auth", "ValidateToken",
		&service.ValidateTokenRequest{AccessToken: "login-bearer"})
	require.NoError(t, err)

	claims, ok := resp.(*service.TokenClaims)
	require.True(t, ok, "response must be *service.TokenClaims, got %T", resp)
	assert.True(t, claims.Valid)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "alice@example.com", claims.Username)
	assert.Equal(t, "acme", claims.TenantID)
	assert.Equal(t, []string{"admin"}, claims.Roles)
	assert.True(t, time.Unix(1700003600, 0).Equal(claims.ExpiresAt))
}

func TestHandleValidateToken_PropagatesServiceError(t *testing.T) {
	rejected := errors.New("token rejected")
	tr, err := New(&mockQuicBaaS{
		validateTokenFunc: func(context.Context, *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
			return nil, rejected
		},
	})
	require.NoError(t, err)

	resp, err := tr.Request(context.Background(), "quicbaas.Auth", "ValidateToken",
		&service.ValidateTokenRequest{AccessToken: "login-bearer"})
	require.ErrorIs(t, err, rejected)
	assert.Nil(t, resp)
}

// The consumer path the issue names: the SDK auth client over the embedded
// transport validates a bearer and keeps its tenant.
func TestAuthClientValidateToken_OverEmbeddedTransport(t *testing.T) {
	tr, err := New(&mockQuicBaaS{validateTokenFunc: validatedBearer})
	require.NoError(t, err)

	claims, err := authclient.NewClient(tr).ValidateToken(context.Background(), "login-bearer")
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.True(t, claims.Valid)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "acme", claims.TenantID)
}

func TestHandleValidateToken_NilServiceResponse(t *testing.T) {
	tr, err := New(&mockQuicBaaS{
		validateTokenFunc: func(context.Context, *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
			return nil, nil
		},
	})
	require.NoError(t, err)

	_, err = authclient.NewClient(tr).ValidateToken(context.Background(), "login-bearer")
	require.Error(t, err, "a nil service response must surface as an error, not a panic")
}
