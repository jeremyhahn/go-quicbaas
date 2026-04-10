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

package iam

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestNewAppClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewAppClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestAppClient_Register(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != appServiceName {
					t.Errorf("expected service %s, got %s", appServiceName, service)
				}
				if method != "Register" {
					t.Errorf("expected method Register, got %s", method)
				}
				regReq, ok := req.(*registerAppRequest)
				if !ok {
					t.Error("expected registerAppRequest")
				}
				if regReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", regReq.TenantID)
				}
				if regReq.App.Name != "test-app" {
					t.Errorf("expected name test-app, got %s", regReq.App.Name)
				}
				return &Application{
					ClientID:     "client-123",
					TenantID:     regReq.TenantID,
					Name:         regReq.App.Name,
					ClientSecret: "secret-xyz",
					Enabled:      true,
					CreatedAt:    now,
					UpdatedAt:    now,
				}, nil
			},
		}

		client := NewAppClient(mt)
		app, err := client.Register(context.Background(), "tenant-123", &RegisterAppRequest{
			Name:       "test-app",
			GrantTypes: []GrantType{GrantTypeClientCredentials},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if app.ClientID != "client-123" {
			t.Errorf("expected client ID client-123, got %s", app.ClientID)
		}
		if app.ClientSecret != "secret-xyz" {
			t.Errorf("expected client secret secret-xyz, got %s", app.ClientSecret)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Register(nil, "tenant-123", &RegisterAppRequest{
			Name:       "test-app",
			GrantTypes: []GrantType{GrantTypeClientCredentials},
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Register(context.Background(), "", &RegisterAppRequest{
			Name:       "test-app",
			GrantTypes: []GrantType{GrantTypeClientCredentials},
		})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Register(context.Background(), "tenant-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Register(context.Background(), "tenant-123", &RegisterAppRequest{
			GrantTypes: []GrantType{GrantTypeClientCredentials},
		})
		if err == nil {
			t.Error("expected error for empty name")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty grant types", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Register(context.Background(), "tenant-123", &RegisterAppRequest{
			Name: "test-app",
		})
		if err == nil {
			t.Error("expected error for empty grant types")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("authorization code requires redirect URIs", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Register(context.Background(), "tenant-123", &RegisterAppRequest{
			Name:       "test-app",
			GrantTypes: []GrantType{GrantTypeAuthorizationCode},
		})
		if err == nil {
			t.Error("expected error for missing redirect URIs")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("implicit grant requires redirect URIs", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Register(context.Background(), "tenant-123", &RegisterAppRequest{
			Name:       "test-app",
			GrantTypes: []GrantType{GrantTypeImplicit},
		})
		if err == nil {
			t.Error("expected error for missing redirect URIs")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewUnavailable("test", "service unavailable")
			},
		}

		client := NewAppClient(mt)
		_, err := client.Register(context.Background(), "tenant-123", &RegisterAppRequest{
			Name:       "test-app",
			GrantTypes: []GrantType{GrantTypeClientCredentials},
		})
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsUnavailable(err) {
			t.Errorf("expected unavailable error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.Register(context.Background(), "tenant-123", &RegisterAppRequest{
			Name:       "test-app",
			GrantTypes: []GrantType{GrantTypeClientCredentials},
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.Register(context.Background(), "tenant-123", &RegisterAppRequest{
			Name:       "test-app",
			GrantTypes: []GrantType{GrantTypeClientCredentials},
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAppClient_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Get" {
					t.Errorf("expected method Get, got %s", method)
				}
				getReq, ok := req.(*getAppRequest)
				if !ok {
					t.Error("expected getAppRequest")
				}
				if getReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", getReq.TenantID)
				}
				if getReq.ClientID != "client-123" {
					t.Errorf("expected client ID client-123, got %s", getReq.ClientID)
				}
				return &Application{
					ClientID: "client-123",
					TenantID: "tenant-123",
					Name:     "test-app",
					Enabled:  true,
				}, nil
			},
		}

		client := NewAppClient(mt)
		app, err := client.Get(context.Background(), "tenant-123", "client-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if app.ClientID != "client-123" {
			t.Errorf("expected client ID client-123, got %s", app.ClientID)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Get(nil, "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Get(context.Background(), "", "client-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty client ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Get(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty client ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.Get(context.Background(), "tenant-123", "nonexistent")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.Get(context.Background(), "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAppClient_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Update" {
					t.Errorf("expected method Update, got %s", method)
				}
				updateReq, ok := req.(*updateAppRequest)
				if !ok {
					t.Error("expected updateAppRequest")
				}
				if updateReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", updateReq.TenantID)
				}
				if updateReq.ClientID != "client-123" {
					t.Errorf("expected client ID client-123, got %s", updateReq.ClientID)
				}
				return &Application{
					ClientID: "client-123",
					TenantID: "tenant-123",
					Name:     updateReq.App.Name,
					Enabled:  true,
				}, nil
			},
		}

		client := NewAppClient(mt)
		app, err := client.Update(context.Background(), "tenant-123", "client-123", &UpdateAppRequest{
			Name: "updated-app",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if app.Name != "updated-app" {
			t.Errorf("expected name updated-app, got %s", app.Name)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Update(nil, "tenant-123", "client-123", &UpdateAppRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Update(context.Background(), "", "client-123", &UpdateAppRequest{})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty client ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", "", &UpdateAppRequest{})
		if err == nil {
			t.Error("expected error for empty client ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", "client-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.Update(context.Background(), "tenant-123", "client-123", &UpdateAppRequest{})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.Update(context.Background(), "tenant-123", "client-123", &UpdateAppRequest{})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAppClient_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Delete" {
					t.Errorf("expected method Delete, got %s", method)
				}
				deleteReq, ok := req.(*deleteAppRequest)
				if !ok {
					t.Error("expected deleteAppRequest")
				}
				if deleteReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", deleteReq.TenantID)
				}
				if deleteReq.ClientID != "client-123" {
					t.Errorf("expected client ID client-123, got %s", deleteReq.ClientID)
				}
				return nil, nil
			},
		}

		client := NewAppClient(mt)
		err := client.Delete(context.Background(), "tenant-123", "client-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &deleteAppResponse{}, nil
			},
		}

		client := NewAppClient(mt)
		err := client.Delete(context.Background(), "tenant-123", "client-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		err := client.Delete(nil, "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		err := client.Delete(context.Background(), "", "client-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty client ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		err := client.Delete(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty client ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("test", "app not found")
			},
		}

		client := NewAppClient(mt)
		err := client.Delete(context.Background(), "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAppClient(mt)
		err := client.Delete(context.Background(), "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAppClient_List(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "List" {
					t.Errorf("expected method List, got %s", method)
				}
				listReq, ok := req.(*listAppsRequest)
				if !ok {
					t.Error("expected listAppsRequest")
				}
				if listReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", listReq.TenantID)
				}
				return &ListAppsResponse{
					Applications: []Application{
						{ClientID: "app-1", Name: "App 1"},
						{ClientID: "app-2", Name: "App 2"},
					},
					TotalCount: 2,
				}, nil
			},
		}

		client := NewAppClient(mt)
		resp, err := client.List(context.Background(), "tenant-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Applications) != 2 {
			t.Errorf("expected 2 apps, got %d", len(resp.Applications))
		}
		if resp.TotalCount != 2 {
			t.Errorf("expected total count 2, got %d", resp.TotalCount)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.List(nil, "tenant-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.List(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewAppClient(mt)
		resp, err := client.List(context.Background(), "tenant-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Applications) != 0 {
			t.Errorf("expected 0 apps, got %d", len(resp.Applications))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.List(context.Background(), "tenant-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAppClient_RotateSecret(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "RotateSecret" {
					t.Errorf("expected method RotateSecret, got %s", method)
				}
				rotateReq, ok := req.(*rotateSecretRequest)
				if !ok {
					t.Error("expected rotateSecretRequest")
				}
				if rotateReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", rotateReq.TenantID)
				}
				if rotateReq.ClientID != "client-123" {
					t.Errorf("expected client ID client-123, got %s", rotateReq.ClientID)
				}
				return &RotateSecretResponse{
					ClientID:                 "client-123",
					ClientSecret:             "new-secret-xyz",
					ClientSecretExpiresAt:    now.Add(365 * 24 * time.Hour),
					PreviousSecretValidUntil: now.Add(24 * time.Hour),
				}, nil
			},
		}

		client := NewAppClient(mt)
		resp, err := client.RotateSecret(context.Background(), "tenant-123", "client-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ClientSecret != "new-secret-xyz" {
			t.Errorf("expected new secret new-secret-xyz, got %s", resp.ClientSecret)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.RotateSecret(nil, "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.RotateSecret(context.Background(), "", "client-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty client ID", func(t *testing.T) {
		client := NewAppClient(&mockTransport{})
		_, err := client.RotateSecret(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty client ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.RotateSecret(context.Background(), "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAppClient(mt)
		_, err := client.RotateSecret(context.Background(), "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("test", "app not found")
			},
		}

		client := NewAppClient(mt)
		_, err := client.RotateSecret(context.Background(), "tenant-123", "client-123")
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

func TestGrantType_String(t *testing.T) {
	tests := []struct {
		grantType GrantType
		expected  string
	}{
		{GrantTypeAuthorizationCode, "authorization_code"},
		{GrantTypeClientCredentials, "client_credentials"},
		{GrantTypeRefreshToken, "refresh_token"},
		{GrantTypeDeviceCode, "urn:ietf:params:oauth:grant-type:device_code"},
		{GrantTypeImplicit, "implicit"},
		{GrantTypePassword, "password"},
		{GrantType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.grantType.String(); got != tt.expected {
				t.Errorf("GrantType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTokenEndpointAuthMethod_String(t *testing.T) {
	tests := []struct {
		authMethod TokenEndpointAuthMethod
		expected   string
	}{
		{TokenEndpointAuthClientSecretBasic, "client_secret_basic"},
		{TokenEndpointAuthClientSecretPost, "client_secret_post"},
		{TokenEndpointAuthClientSecretJWT, "client_secret_jwt"},
		{TokenEndpointAuthPrivateKeyJWT, "private_key_jwt"},
		{TokenEndpointAuthNone, "none"},
		{TokenEndpointAuthMethod(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.authMethod.String(); got != tt.expected {
				t.Errorf("TokenEndpointAuthMethod.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestApplicationType_String(t *testing.T) {
	tests := []struct {
		appType  ApplicationType
		expected string
	}{
		{ApplicationTypeWeb, "web"},
		{ApplicationTypeNative, "native"},
		{ApplicationTypeSPA, "spa"},
		{ApplicationTypeService, "service"},
		{ApplicationType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.appType.String(); got != tt.expected {
				t.Errorf("ApplicationType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}
