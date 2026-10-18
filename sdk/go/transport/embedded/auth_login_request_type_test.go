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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	authclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestAuthClientLogin_OverEmbeddedTransport drives the real auth client over
// the embedded transport: the client's *service.ExtendedLoginRequest reaches
// the service as a *service.LoginRequest and the service's answer comes back
// as the *service.ExtendedLoginResponse the client asserts (go-quicbaas#1544).
func TestAuthClientLogin_OverEmbeddedTransport(t *testing.T) {
	expiresAt := time.Unix(1_900_000_000, 0)
	var got *service.LoginRequest
	tr, err := New(&mockQuicBaaS{
		loginFunc: func(_ context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
			got = req
			return &service.LoginResponse{
				AccessToken:  "access",
				RefreshToken: "refresh",
				ExpiresAt:    expiresAt.Unix(),
				UserID:       "user-1",
			}, nil
		},
	})
	require.NoError(t, err)

	resp, err := authclient.NewClient(tr).Login(context.Background(),
		&service.ExtendedLoginRequest{Username: "alice"})
	require.NoError(t, err)
	require.NotNil(t, got, "the call must reach the service")
	assert.Equal(t, &service.LoginRequest{Username: "alice"}, got)
	assert.Equal(t, &service.ExtendedLoginResponse{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    expiresAt,
	}, resp)
}

// TestAuthClientLogin_OverEmbeddedTransport_ServiceError asserts the service's
// refusal reaches the caller unchanged.
func TestAuthClientLogin_OverEmbeddedTransport_ServiceError(t *testing.T) {
	refusal := errors.NewInvalidInput("service.Login", "password login refused")
	tr, err := New(&mockQuicBaaS{
		loginFunc: func(context.Context, *service.LoginRequest) (*service.LoginResponse, error) {
			return nil, refusal
		},
	})
	require.NoError(t, err)

	_, err = authclient.NewClient(tr).Login(context.Background(),
		&service.ExtendedLoginRequest{Username: "alice"})
	require.ErrorIs(t, err, refusal)
}

// TestHandleLogin_RefusesFieldsTheServiceCannotCarry asserts an MFA code or a
// tenant is refused rather than dropped: service.LoginRequest carries only
// the username, so forwarding without them would log in without the MFA
// factor or outside the requested tenant.
func TestHandleLogin_RefusesFieldsTheServiceCannotCarry(t *testing.T) {
	tests := map[string]*service.ExtendedLoginRequest{
		"mfa code":  {Username: "alice", MFACode: "123456"},
		"tenant id": {Username: "alice", TenantID: "acme"},
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			reached := false
			svc := &mockQuicBaaS{
				loginFunc: func(context.Context, *service.LoginRequest) (*service.LoginResponse, error) {
					reached = true
					return &service.LoginResponse{}, nil
				},
			}
			_, err := handleLogin(context.Background(), svc, req)
			require.Error(t, err)
			assert.True(t, errors.IsInvalidInput(err), "want invalid input, got %v", err)
			var typeErr *InvalidRequestTypeError
			assert.NotErrorAs(t, err, &typeErr, "the client's type must be accepted")
			assert.False(t, reached, "the service must not be called")
		})
	}
}

// TestHandleLogin_WrongRequestType asserts a request of any other type, the
// bare service request included, is refused with the typed
// InvalidRequestTypeError.
func TestHandleLogin_WrongRequestType(t *testing.T) {
	tests := map[string]interface{}{
		"service request": &service.LoginRequest{Username: "alice"},
		"string":          "alice",
		"nil request":     (*service.ExtendedLoginRequest)(nil),
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := handleLogin(context.Background(), &mockQuicBaaS{}, req)
			var typeErr *InvalidRequestTypeError
			require.ErrorAs(t, err, &typeErr)
			assert.Equal(t, "Login", typeErr.Method)
		})
	}
}

// TestHandleLogin_NilServiceResponse asserts a nil service answer surfaces as
// the client's nil-response error, not a panic.
func TestHandleLogin_NilServiceResponse(t *testing.T) {
	tr, err := New(&mockQuicBaaS{
		loginFunc: func(context.Context, *service.LoginRequest) (*service.LoginResponse, error) {
			return nil, nil
		},
	})
	require.NoError(t, err)

	_, err = authclient.NewClient(tr).Login(context.Background(),
		&service.ExtendedLoginRequest{Username: "alice"})
	require.Error(t, err)
}
