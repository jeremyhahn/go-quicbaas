//go:build iam

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
	"errors"
	"strings"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ============================================================================
// MemoryProviderStorage Tests
// ============================================================================

func TestMemoryProviderStorage_CreateProvider(t *testing.T) {
	tests := []struct {
		name        string
		provider    *ExternalProvider
		setup       func(s *MemoryProviderStorage)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful_create",
			provider: &ExternalProvider{
				ID:        "provider-1",
				TenantID:  "tenant-1",
				Name:      "Google",
				Type:      ProviderTypeOIDC,
				Enabled:   true,
				ClientID:  "client-123",
				IssuerURL: "https://accounts.google.com",
				Scopes:    []string{"openid", "email", "profile"},
			},
			wantErr: false,
		},
		{
			name: "duplicate_id_returns_error",
			provider: &ExternalProvider{
				ID:        "provider-1",
				TenantID:  "tenant-1",
				Name:      "Google Duplicate",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-456",
				IssuerURL: "https://accounts.google.com",
			},
			setup: func(s *MemoryProviderStorage) {
				existing := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), existing)
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name: "duplicate_name_returns_error",
			provider: &ExternalProvider{
				ID:        "provider-2",
				TenantID:  "tenant-1",
				Name:      "Google",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-789",
				IssuerURL: "https://accounts.google.com",
			},
			setup: func(s *MemoryProviderStorage) {
				existing := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), existing)
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name: "missing_id_validation_fails",
			provider: &ExternalProvider{
				TenantID:  "tenant-1",
				Name:      "Test",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-123",
				IssuerURL: "https://issuer.example.com",
			},
			wantErr:     true,
			errContains: "provider ID is required",
		},
		{
			name: "missing_tenant_id_validation_fails",
			provider: &ExternalProvider{
				ID:        "provider-1",
				Name:      "Test",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-123",
				IssuerURL: "https://issuer.example.com",
			},
			wantErr:     true,
			errContains: "tenant ID is required",
		},
		{
			name: "missing_name_validation_fails",
			provider: &ExternalProvider{
				ID:        "provider-1",
				TenantID:  "tenant-1",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-123",
				IssuerURL: "https://issuer.example.com",
			},
			wantErr:     true,
			errContains: "provider name is required",
		},
		{
			name: "missing_type_validation_fails",
			provider: &ExternalProvider{
				ID:        "provider-1",
				TenantID:  "tenant-1",
				Name:      "Test",
				ClientID:  "client-123",
				IssuerURL: "https://issuer.example.com",
			},
			wantErr:     true,
			errContains: "provider type is required",
		},
		{
			name: "oidc_missing_client_id_fails",
			provider: &ExternalProvider{
				ID:        "provider-1",
				TenantID:  "tenant-1",
				Name:      "Test",
				Type:      ProviderTypeOIDC,
				IssuerURL: "https://issuer.example.com",
			},
			wantErr:     true,
			errContains: "client ID is required",
		},
		{
			name: "oidc_missing_issuer_url_fails",
			provider: &ExternalProvider{
				ID:       "provider-1",
				TenantID: "tenant-1",
				Name:     "Test",
				Type:     ProviderTypeOIDC,
				ClientID: "client-123",
			},
			wantErr:     true,
			errContains: "issuer URL is required",
		},
		{
			name: "same_name_different_tenant_succeeds",
			provider: &ExternalProvider{
				ID:        "provider-2",
				TenantID:  "tenant-2",
				Name:      "Google",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-789",
				IssuerURL: "https://accounts.google.com",
			},
			setup: func(s *MemoryProviderStorage) {
				existing := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), existing)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryProviderStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			err := storage.CreateProvider(context.Background(), tt.provider)
			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateProvider() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CreateProvider() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("CreateProvider() unexpected error = %v", err)
				return
			}

			// Verify provider was stored
			stored, err := storage.GetProvider(context.Background(), tt.provider.TenantID, tt.provider.ID)
			if err != nil {
				t.Errorf("GetProvider() after create failed: %v", err)
				return
			}
			if stored.ID != tt.provider.ID {
				t.Errorf("GetProvider() ID = %v, want %v", stored.ID, tt.provider.ID)
			}
			if stored.Created.IsZero() {
				t.Error("CreateProvider() should set Created timestamp")
			}
			if stored.Updated.IsZero() {
				t.Error("CreateProvider() should set Updated timestamp")
			}
		})
	}
}

func TestMemoryProviderStorage_GetProvider(t *testing.T) {
	tests := []struct {
		name       string
		tenantID   string
		providerID string
		setup      func(s *MemoryProviderStorage)
		wantErr    bool
		wantErrIs  error
	}{
		{
			name:       "existing_provider_found",
			tenantID:   "tenant-1",
			providerID: "provider-1",
			setup: func(s *MemoryProviderStorage) {
				provider := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), provider)
			},
			wantErr: false,
		},
		{
			name:       "non_existent_provider_returns_error",
			tenantID:   "tenant-1",
			providerID: "non-existent",
			wantErr:    true,
			wantErrIs:  ErrProviderNotFound,
		},
		{
			name:       "wrong_tenant_returns_error",
			tenantID:   "tenant-2",
			providerID: "provider-1",
			setup: func(s *MemoryProviderStorage) {
				provider := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), provider)
			},
			wantErr:   true,
			wantErrIs: ErrProviderNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryProviderStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			provider, err := storage.GetProvider(context.Background(), tt.tenantID, tt.providerID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetProvider() expected error, got nil")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("GetProvider() error = %v, want error matching %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Errorf("GetProvider() unexpected error = %v", err)
				return
			}
			if provider == nil {
				t.Error("GetProvider() returned nil provider")
			}
		})
	}
}

func TestMemoryProviderStorage_GetProviderByName(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		pName     string
		setup     func(s *MemoryProviderStorage)
		wantErr   bool
		wantErrIs error
	}{
		{
			name:     "existing_provider_found_by_name",
			tenantID: "tenant-1",
			pName:    "Google",
			setup: func(s *MemoryProviderStorage) {
				provider := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), provider)
			},
			wantErr: false,
		},
		{
			name:      "non_existent_name_returns_error",
			tenantID:  "tenant-1",
			pName:     "NonExistent",
			wantErr:   true,
			wantErrIs: ErrProviderNotFound,
		},
		{
			name:     "wrong_tenant_returns_error",
			tenantID: "tenant-2",
			pName:    "Google",
			setup: func(s *MemoryProviderStorage) {
				provider := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), provider)
			},
			wantErr:   true,
			wantErrIs: ErrProviderNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryProviderStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			provider, err := storage.GetProviderByName(context.Background(), tt.tenantID, tt.pName)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetProviderByName() expected error, got nil")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("GetProviderByName() error = %v, want error matching %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Errorf("GetProviderByName() unexpected error = %v", err)
				return
			}
			if provider.Name != tt.pName {
				t.Errorf("GetProviderByName() Name = %v, want %v", provider.Name, tt.pName)
			}
		})
	}
}

func TestMemoryProviderStorage_UpdateProvider(t *testing.T) {
	tests := []struct {
		name        string
		provider    *ExternalProvider
		setup       func(s *MemoryProviderStorage)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful_update",
			provider: &ExternalProvider{
				ID:        "provider-1",
				TenantID:  "tenant-1",
				Name:      "Google Updated",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-456",
				IssuerURL: "https://accounts.google.com",
			},
			setup: func(s *MemoryProviderStorage) {
				existing := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), existing)
			},
			wantErr: false,
		},
		{
			name: "non_existent_provider_returns_error",
			provider: &ExternalProvider{
				ID:        "non-existent",
				TenantID:  "tenant-1",
				Name:      "Test",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-123",
				IssuerURL: "https://issuer.example.com",
			},
			wantErr:     true,
			errContains: "not found",
		},
		{
			name: "name_conflict_returns_error",
			provider: &ExternalProvider{
				ID:        "provider-1",
				TenantID:  "tenant-1",
				Name:      "Azure",
				Type:      ProviderTypeOIDC,
				ClientID:  "client-123",
				IssuerURL: "https://accounts.google.com",
			},
			setup: func(s *MemoryProviderStorage) {
				google := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), google)
				azure := &ExternalProvider{
					ID:        "provider-2",
					TenantID:  "tenant-1",
					Name:      "Azure",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-456",
					IssuerURL: "https://login.microsoftonline.com",
				}
				_ = s.CreateProvider(context.Background(), azure)
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name: "validation_error",
			provider: &ExternalProvider{
				ID:       "provider-1",
				TenantID: "tenant-1",
				Name:     "",
				Type:     ProviderTypeOIDC,
				ClientID: "client-123",
			},
			setup: func(s *MemoryProviderStorage) {
				existing := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), existing)
			},
			wantErr:     true,
			errContains: "provider name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryProviderStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			err := storage.UpdateProvider(context.Background(), tt.provider)
			if tt.wantErr {
				if err == nil {
					t.Errorf("UpdateProvider() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("UpdateProvider() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("UpdateProvider() unexpected error = %v", err)
				return
			}

			// Verify update was applied
			stored, err := storage.GetProvider(context.Background(), tt.provider.TenantID, tt.provider.ID)
			if err != nil {
				t.Errorf("GetProvider() after update failed: %v", err)
				return
			}
			if stored.Name != tt.provider.Name {
				t.Errorf("UpdateProvider() Name = %v, want %v", stored.Name, tt.provider.Name)
			}
		})
	}
}

func TestMemoryProviderStorage_DeleteProvider(t *testing.T) {
	tests := []struct {
		name       string
		tenantID   string
		providerID string
		setup      func(s *MemoryProviderStorage)
		wantErr    bool
		wantErrIs  error
	}{
		{
			name:       "successful_delete",
			tenantID:   "tenant-1",
			providerID: "provider-1",
			setup: func(s *MemoryProviderStorage) {
				provider := &ExternalProvider{
					ID:        "provider-1",
					TenantID:  "tenant-1",
					Name:      "Google",
					Type:      ProviderTypeOIDC,
					ClientID:  "client-123",
					IssuerURL: "https://accounts.google.com",
				}
				_ = s.CreateProvider(context.Background(), provider)
			},
			wantErr: false,
		},
		{
			name:       "non_existent_provider_returns_error",
			tenantID:   "tenant-1",
			providerID: "non-existent",
			wantErr:    true,
			wantErrIs:  ErrProviderNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryProviderStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			err := storage.DeleteProvider(context.Background(), tt.tenantID, tt.providerID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("DeleteProvider() expected error, got nil")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("DeleteProvider() error = %v, want error matching %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Errorf("DeleteProvider() unexpected error = %v", err)
				return
			}

			// Verify provider was deleted
			_, err = storage.GetProvider(context.Background(), tt.tenantID, tt.providerID)
			if !errors.Is(err, ErrProviderNotFound) {
				t.Errorf("GetProvider() after delete should return ErrProviderNotFound, got %v", err)
			}
		})
	}
}

func TestMemoryProviderStorage_ListProviders(t *testing.T) {
	setup := func(s *MemoryProviderStorage) {
		providers := []*ExternalProvider{
			{ID: "provider-1", TenantID: "tenant-1", Name: "Google", Type: ProviderTypeOIDC, ClientID: "c1", IssuerURL: "https://google.com"},
			{ID: "provider-2", TenantID: "tenant-1", Name: "Azure", Type: ProviderTypeOIDC, ClientID: "c2", IssuerURL: "https://azure.com"},
			{ID: "provider-3", TenantID: "tenant-1", Name: "GitHub", Type: ProviderTypeOAuth2, ClientID: "c3", IssuerURL: "https://github.com"},
			{ID: "provider-4", TenantID: "tenant-2", Name: "Okta", Type: ProviderTypeOIDC, ClientID: "c4", IssuerURL: "https://okta.com"},
		}
		for _, p := range providers {
			_ = s.CreateProvider(context.Background(), p)
		}
	}

	tests := []struct {
		name      string
		tenantID  string
		opts      ListOptions
		wantCount int
	}{
		{
			name:      "list_all_tenant1",
			tenantID:  "tenant-1",
			opts:      ListOptions{},
			wantCount: 3,
		},
		{
			name:      "list_all_tenant2",
			tenantID:  "tenant-2",
			opts:      ListOptions{},
			wantCount: 1,
		},
		{
			name:      "list_with_limit",
			tenantID:  "tenant-1",
			opts:      ListOptions{Limit: 2},
			wantCount: 2,
		},
		{
			name:      "list_with_offset",
			tenantID:  "tenant-1",
			opts:      ListOptions{Offset: 2, Limit: 10},
			wantCount: 1,
		},
		{
			name:      "list_with_offset_beyond_count",
			tenantID:  "tenant-1",
			opts:      ListOptions{Offset: 10},
			wantCount: 0,
		},
		{
			name:      "list_empty_tenant",
			tenantID:  "tenant-nonexistent",
			opts:      ListOptions{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryProviderStorage()
			setup(storage)

			providers, err := storage.ListProviders(context.Background(), tt.tenantID, tt.opts)
			if err != nil {
				t.Errorf("ListProviders() unexpected error = %v", err)
				return
			}
			if len(providers) != tt.wantCount {
				t.Errorf("ListProviders() count = %v, want %v", len(providers), tt.wantCount)
			}
		})
	}
}

// ============================================================================
// MemoryFederationStorage Tests
// ============================================================================

func TestMemoryFederationStorage_CreateLink(t *testing.T) {
	tests := []struct {
		name        string
		link        *FederatedIdentity
		setup       func(s *MemoryFederationStorage)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful_create",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "user-1",
				TenantID:   "tenant-1",
				ProviderID: "google",
				ExternalID: "ext-123",
			},
			wantErr: false,
		},
		{
			name: "duplicate_id_returns_error",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "user-2",
				TenantID:   "tenant-1",
				ProviderID: "azure",
				ExternalID: "ext-456",
			},
			setup: func(s *MemoryFederationStorage) {
				existing := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), existing)
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name: "duplicate_external_id_returns_error",
			link: &FederatedIdentity{
				ID:         "link-2",
				UserID:     "user-2",
				TenantID:   "tenant-1",
				ProviderID: "google",
				ExternalID: "ext-123",
			},
			setup: func(s *MemoryFederationStorage) {
				existing := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), existing)
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name: "missing_id_validation_fails",
			link: &FederatedIdentity{
				UserID:     "user-1",
				TenantID:   "tenant-1",
				ProviderID: "google",
				ExternalID: "ext-123",
			},
			wantErr:     true,
			errContains: "identity ID is required",
		},
		{
			name: "missing_user_id_validation_fails",
			link: &FederatedIdentity{
				ID:         "link-1",
				TenantID:   "tenant-1",
				ProviderID: "google",
				ExternalID: "ext-123",
			},
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name: "missing_tenant_id_validation_fails",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "user-1",
				ProviderID: "google",
				ExternalID: "ext-123",
			},
			wantErr:     true,
			errContains: "tenant ID is required",
		},
		{
			name: "missing_provider_id_validation_fails",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "user-1",
				TenantID:   "tenant-1",
				ExternalID: "ext-123",
			},
			wantErr:     true,
			errContains: "provider ID is required",
		},
		{
			name: "missing_external_id_validation_fails",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "user-1",
				TenantID:   "tenant-1",
				ProviderID: "google",
			},
			wantErr:     true,
			errContains: "external ID is required",
		},
		{
			name: "same_external_id_different_provider_succeeds",
			link: &FederatedIdentity{
				ID:         "link-2",
				UserID:     "user-1",
				TenantID:   "tenant-1",
				ProviderID: "azure",
				ExternalID: "ext-123",
			},
			setup: func(s *MemoryFederationStorage) {
				existing := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), existing)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryFederationStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			err := storage.CreateLink(context.Background(), tt.link)
			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateLink() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CreateLink() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("CreateLink() unexpected error = %v", err)
				return
			}

			// Verify link was stored
			stored, err := storage.GetLink(context.Background(), tt.link.ID)
			if err != nil {
				t.Errorf("GetLink() after create failed: %v", err)
				return
			}
			if stored.ID != tt.link.ID {
				t.Errorf("GetLink() ID = %v, want %v", stored.ID, tt.link.ID)
			}
			if stored.Created.IsZero() {
				t.Error("CreateLink() should set Created timestamp")
			}
		})
	}
}

func TestMemoryFederationStorage_GetLink(t *testing.T) {
	tests := []struct {
		name      string
		linkID    string
		setup     func(s *MemoryFederationStorage)
		wantErr   bool
		wantErrIs error
	}{
		{
			name:   "existing_link_found",
			linkID: "link-1",
			setup: func(s *MemoryFederationStorage) {
				link := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), link)
			},
			wantErr: false,
		},
		{
			name:      "non_existent_link_returns_error",
			linkID:    "non-existent",
			wantErr:   true,
			wantErrIs: ErrLinkNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryFederationStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			link, err := storage.GetLink(context.Background(), tt.linkID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetLink() expected error, got nil")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("GetLink() error = %v, want error matching %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Errorf("GetLink() unexpected error = %v", err)
				return
			}
			if link == nil {
				t.Error("GetLink() returned nil link")
			}
		})
	}
}

func TestMemoryFederationStorage_GetLinkByExternal(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		externalID string
		setup      func(s *MemoryFederationStorage)
		wantErr    bool
		wantErrIs  error
	}{
		{
			name:       "existing_link_found",
			providerID: "google",
			externalID: "ext-123",
			setup: func(s *MemoryFederationStorage) {
				link := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), link)
			},
			wantErr: false,
		},
		{
			name:       "non_existent_external_id_returns_error",
			providerID: "google",
			externalID: "non-existent",
			wantErr:    true,
			wantErrIs:  ErrLinkNotFound,
		},
		{
			name:       "wrong_provider_returns_error",
			providerID: "azure",
			externalID: "ext-123",
			setup: func(s *MemoryFederationStorage) {
				link := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), link)
			},
			wantErr:   true,
			wantErrIs: ErrLinkNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryFederationStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			link, err := storage.GetLinkByExternal(context.Background(), tt.providerID, tt.externalID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetLinkByExternal() expected error, got nil")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("GetLinkByExternal() error = %v, want error matching %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Errorf("GetLinkByExternal() unexpected error = %v", err)
				return
			}
			if link.ExternalID != tt.externalID {
				t.Errorf("GetLinkByExternal() ExternalID = %v, want %v", link.ExternalID, tt.externalID)
			}
		})
	}
}

func TestMemoryFederationStorage_GetLinksByUser(t *testing.T) {
	setup := func(s *MemoryFederationStorage) {
		links := []*FederatedIdentity{
			{ID: "link-1", UserID: "user-1", TenantID: "tenant-1", ProviderID: "google", ExternalID: "g-123"},
			{ID: "link-2", UserID: "user-1", TenantID: "tenant-1", ProviderID: "azure", ExternalID: "a-123"},
			{ID: "link-3", UserID: "user-2", TenantID: "tenant-1", ProviderID: "google", ExternalID: "g-456"},
		}
		for _, l := range links {
			_ = s.CreateLink(context.Background(), l)
		}
	}

	tests := []struct {
		name      string
		userID    string
		wantCount int
	}{
		{
			name:      "user_with_multiple_links",
			userID:    "user-1",
			wantCount: 2,
		},
		{
			name:      "user_with_single_link",
			userID:    "user-2",
			wantCount: 1,
		},
		{
			name:      "user_with_no_links",
			userID:    "user-nonexistent",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryFederationStorage()
			setup(storage)

			links, err := storage.GetLinksByUser(context.Background(), tt.userID)
			if err != nil {
				t.Errorf("GetLinksByUser() unexpected error = %v", err)
				return
			}
			if len(links) != tt.wantCount {
				t.Errorf("GetLinksByUser() count = %v, want %v", len(links), tt.wantCount)
			}
		})
	}
}

func TestMemoryFederationStorage_UpdateLink(t *testing.T) {
	tests := []struct {
		name        string
		link        *FederatedIdentity
		setup       func(s *MemoryFederationStorage)
		wantErr     bool
		errContains string
	}{
		{
			name: "successful_update",
			link: &FederatedIdentity{
				ID:            "link-1",
				UserID:        "user-1",
				TenantID:      "tenant-1",
				ProviderID:    "google",
				ExternalID:    "ext-123",
				ExternalEmail: "updated@example.com",
			},
			setup: func(s *MemoryFederationStorage) {
				existing := &FederatedIdentity{
					ID:            "link-1",
					UserID:        "user-1",
					TenantID:      "tenant-1",
					ProviderID:    "google",
					ExternalID:    "ext-123",
					ExternalEmail: "original@example.com",
				}
				_ = s.CreateLink(context.Background(), existing)
			},
			wantErr: false,
		},
		{
			name: "non_existent_link_returns_error",
			link: &FederatedIdentity{
				ID:         "non-existent",
				UserID:     "user-1",
				TenantID:   "tenant-1",
				ProviderID: "google",
				ExternalID: "ext-123",
			},
			wantErr:     true,
			errContains: "not found",
		},
		{
			name: "external_id_conflict_returns_error",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "user-1",
				TenantID:   "tenant-1",
				ProviderID: "google",
				ExternalID: "ext-456",
			},
			setup: func(s *MemoryFederationStorage) {
				link1 := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), link1)
				link2 := &FederatedIdentity{
					ID:         "link-2",
					UserID:     "user-2",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-456",
				}
				_ = s.CreateLink(context.Background(), link2)
			},
			wantErr:     true,
			errContains: "already exists",
		},
		{
			name: "validation_error",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "",
				TenantID:   "tenant-1",
				ProviderID: "google",
				ExternalID: "ext-123",
			},
			setup: func(s *MemoryFederationStorage) {
				existing := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), existing)
			},
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name: "update_user_id",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "user-2",
				TenantID:   "tenant-1",
				ProviderID: "google",
				ExternalID: "ext-123",
			},
			setup: func(s *MemoryFederationStorage) {
				existing := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), existing)
			},
			wantErr: false,
		},
		{
			name: "update_external_id_and_provider",
			link: &FederatedIdentity{
				ID:         "link-1",
				UserID:     "user-1",
				TenantID:   "tenant-1",
				ProviderID: "azure",
				ExternalID: "azure-ext-123",
			},
			setup: func(s *MemoryFederationStorage) {
				existing := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), existing)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryFederationStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			err := storage.UpdateLink(context.Background(), tt.link)
			if tt.wantErr {
				if err == nil {
					t.Errorf("UpdateLink() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("UpdateLink() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("UpdateLink() unexpected error = %v", err)
				return
			}

			// Verify update was applied
			stored, err := storage.GetLink(context.Background(), tt.link.ID)
			if err != nil {
				t.Errorf("GetLink() after update failed: %v", err)
				return
			}
			if stored.UserID != tt.link.UserID {
				t.Errorf("UpdateLink() UserID = %v, want %v", stored.UserID, tt.link.UserID)
			}
		})
	}
}

func TestMemoryFederationStorage_DeleteLink(t *testing.T) {
	tests := []struct {
		name      string
		linkID    string
		setup     func(s *MemoryFederationStorage)
		wantErr   bool
		wantErrIs error
	}{
		{
			name:   "successful_delete",
			linkID: "link-1",
			setup: func(s *MemoryFederationStorage) {
				link := &FederatedIdentity{
					ID:         "link-1",
					UserID:     "user-1",
					TenantID:   "tenant-1",
					ProviderID: "google",
					ExternalID: "ext-123",
				}
				_ = s.CreateLink(context.Background(), link)
			},
			wantErr: false,
		},
		{
			name:      "non_existent_link_returns_error",
			linkID:    "non-existent",
			wantErr:   true,
			wantErrIs: ErrLinkNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryFederationStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			err := storage.DeleteLink(context.Background(), tt.linkID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("DeleteLink() expected error, got nil")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("DeleteLink() error = %v, want error matching %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Errorf("DeleteLink() unexpected error = %v", err)
				return
			}

			// Verify link was deleted
			_, err = storage.GetLink(context.Background(), tt.linkID)
			if !errors.Is(err, ErrLinkNotFound) {
				t.Errorf("GetLink() after delete should return ErrLinkNotFound, got %v", err)
			}
		})
	}
}

func TestMemoryFederationStorage_DeleteUserLinks(t *testing.T) {
	setup := func(s *MemoryFederationStorage) {
		links := []*FederatedIdentity{
			{ID: "link-1", UserID: "user-1", TenantID: "tenant-1", ProviderID: "google", ExternalID: "g-123"},
			{ID: "link-2", UserID: "user-1", TenantID: "tenant-1", ProviderID: "azure", ExternalID: "a-123"},
			{ID: "link-3", UserID: "user-2", TenantID: "tenant-1", ProviderID: "google", ExternalID: "g-456"},
		}
		for _, l := range links {
			_ = s.CreateLink(context.Background(), l)
		}
	}

	tests := []struct {
		name              string
		userID            string
		wantRemainingUser string
		wantRemaining     int
	}{
		{
			name:              "delete_all_user_links",
			userID:            "user-1",
			wantRemainingUser: "user-2",
			wantRemaining:     1,
		},
		{
			name:              "delete_nonexistent_user_links",
			userID:            "user-nonexistent",
			wantRemainingUser: "user-1",
			wantRemaining:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryFederationStorage()
			setup(storage)

			err := storage.DeleteUserLinks(context.Background(), tt.userID)
			if err != nil {
				t.Errorf("DeleteUserLinks() unexpected error = %v", err)
				return
			}

			// Verify user's links were deleted
			links, err := storage.GetLinksByUser(context.Background(), tt.userID)
			if err != nil {
				t.Errorf("GetLinksByUser() after delete failed: %v", err)
				return
			}
			if len(links) != 0 {
				t.Errorf("GetLinksByUser() after delete count = %v, want 0", len(links))
			}

			// Verify other user's links remain
			otherLinks, err := storage.GetLinksByUser(context.Background(), tt.wantRemainingUser)
			if err != nil {
				t.Errorf("GetLinksByUser() for other user failed: %v", err)
				return
			}
			if len(otherLinks) != tt.wantRemaining {
				t.Errorf("GetLinksByUser() for other user count = %v, want %v", len(otherLinks), tt.wantRemaining)
			}
		})
	}
}

// ============================================================================
// MemoryStateStorage Tests
// ============================================================================

func TestMemoryStateStorage_SaveState(t *testing.T) {
	tests := []struct {
		name        string
		state       *LoginState
		wantErr     bool
		errContains string
	}{
		{
			name: "successful_save",
			state: &LoginState{
				Nonce:       "test-nonce-123",
				ProviderID:  "google",
				TenantID:    "tenant-1",
				RedirectURI: "https://example.com/callback",
				ExpiresAt:   time.Now().Add(5 * time.Minute),
			},
			wantErr: false,
		},
		{
			name: "empty_nonce_returns_error",
			state: &LoginState{
				Nonce:       "",
				ProviderID:  "google",
				TenantID:    "tenant-1",
				RedirectURI: "https://example.com/callback",
				ExpiresAt:   time.Now().Add(5 * time.Minute),
			},
			wantErr:     true,
			errContains: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryStateStorage()

			err := storage.SaveState(context.Background(), tt.state)
			if tt.wantErr {
				if err == nil {
					t.Errorf("SaveState() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("SaveState() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("SaveState() unexpected error = %v", err)
				return
			}

			// Verify state was stored
			stored, err := storage.GetState(context.Background(), tt.state.Nonce)
			if err != nil {
				t.Errorf("GetState() after save failed: %v", err)
				return
			}
			if stored.Nonce != tt.state.Nonce {
				t.Errorf("GetState() Nonce = %v, want %v", stored.Nonce, tt.state.Nonce)
			}
		})
	}
}

func TestMemoryStateStorage_GetState(t *testing.T) {
	tests := []struct {
		name      string
		token     string
		setup     func(s *MemoryStateStorage)
		wantErr   bool
		wantErrIs error
	}{
		{
			name:  "existing_state_found",
			token: "test-nonce-123",
			setup: func(s *MemoryStateStorage) {
				state := &LoginState{
					Nonce:       "test-nonce-123",
					ProviderID:  "google",
					TenantID:    "tenant-1",
					RedirectURI: "https://example.com/callback",
					ExpiresAt:   time.Now().Add(5 * time.Minute),
				}
				_ = s.SaveState(context.Background(), state)
			},
			wantErr: false,
		},
		{
			name:      "non_existent_state_returns_error",
			token:     "non-existent",
			wantErr:   true,
			wantErrIs: ErrStateNotFound,
		},
		{
			name:  "expired_state_returns_error",
			token: "expired-nonce",
			setup: func(s *MemoryStateStorage) {
				state := &LoginState{
					Nonce:       "expired-nonce",
					ProviderID:  "google",
					TenantID:    "tenant-1",
					RedirectURI: "https://example.com/callback",
					ExpiresAt:   time.Now().Add(-5 * time.Minute),
				}
				_ = s.SaveState(context.Background(), state)
			},
			wantErr:   true,
			wantErrIs: ErrStateExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryStateStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			state, err := storage.GetState(context.Background(), tt.token)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GetState() expected error, got nil")
					return
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("GetState() error = %v, want error matching %v", err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Errorf("GetState() unexpected error = %v", err)
				return
			}
			if state == nil {
				t.Error("GetState() returned nil state")
			}
		})
	}
}

func TestMemoryStateStorage_DeleteState(t *testing.T) {
	tests := []struct {
		name  string
		token string
		setup func(s *MemoryStateStorage)
	}{
		{
			name:  "delete_existing_state",
			token: "test-nonce-123",
			setup: func(s *MemoryStateStorage) {
				state := &LoginState{
					Nonce:       "test-nonce-123",
					ProviderID:  "google",
					TenantID:    "tenant-1",
					RedirectURI: "https://example.com/callback",
					ExpiresAt:   time.Now().Add(5 * time.Minute),
				}
				_ = s.SaveState(context.Background(), state)
			},
		},
		{
			name:  "delete_non_existent_state_is_noop",
			token: "non-existent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryStateStorage()
			if tt.setup != nil {
				tt.setup(storage)
			}

			err := storage.DeleteState(context.Background(), tt.token)
			if err != nil {
				t.Errorf("DeleteState() unexpected error = %v", err)
				return
			}

			// Verify state was deleted
			_, err = storage.GetState(context.Background(), tt.token)
			if !errors.Is(err, ErrStateNotFound) {
				t.Errorf("GetState() after delete should return ErrStateNotFound, got %v", err)
			}
		})
	}
}

func TestMemoryStateStorage_CleanupExpired(t *testing.T) {
	storage := NewMemoryStateStorage()

	// Add mixed states
	states := []*LoginState{
		{Nonce: "expired-1", ProviderID: "google", TenantID: "t1", RedirectURI: "https://example.com/callback", ExpiresAt: time.Now().Add(-10 * time.Minute)},
		{Nonce: "expired-2", ProviderID: "google", TenantID: "t1", RedirectURI: "https://example.com/callback", ExpiresAt: time.Now().Add(-5 * time.Minute)},
		{Nonce: "valid-1", ProviderID: "google", TenantID: "t1", RedirectURI: "https://example.com/callback", ExpiresAt: time.Now().Add(5 * time.Minute)},
		{Nonce: "valid-2", ProviderID: "google", TenantID: "t1", RedirectURI: "https://example.com/callback", ExpiresAt: time.Now().Add(10 * time.Minute)},
	}

	for _, s := range states {
		_ = storage.SaveState(context.Background(), s)
	}

	count, err := storage.CleanupExpired(context.Background())
	if err != nil {
		t.Errorf("CleanupExpired() unexpected error = %v", err)
		return
	}
	if count != 2 {
		t.Errorf("CleanupExpired() count = %v, want 2", count)
	}

	// Verify expired states were deleted
	for _, nonce := range []string{"expired-1", "expired-2"} {
		_, err := storage.GetState(context.Background(), nonce)
		if !errors.Is(err, ErrStateNotFound) {
			t.Errorf("GetState(%s) after cleanup should return ErrStateNotFound, got %v", nonce, err)
		}
	}

	// Verify valid states remain
	for _, nonce := range []string{"valid-1", "valid-2"} {
		_, err := storage.GetState(context.Background(), nonce)
		if err != nil {
			t.Errorf("GetState(%s) after cleanup should succeed, got %v", nonce, err)
		}
	}
}

func TestMemoryStateStorage_CleanupExpired_NoExpiredStates(t *testing.T) {
	storage := NewMemoryStateStorage()

	// Add only valid states
	states := []*LoginState{
		{Nonce: "valid-1", ProviderID: "google", TenantID: "t1", RedirectURI: "https://example.com/callback", ExpiresAt: time.Now().Add(5 * time.Minute)},
		{Nonce: "valid-2", ProviderID: "google", TenantID: "t1", RedirectURI: "https://example.com/callback", ExpiresAt: time.Now().Add(10 * time.Minute)},
	}

	for _, s := range states {
		_ = storage.SaveState(context.Background(), s)
	}

	count, err := storage.CleanupExpired(context.Background())
	if err != nil {
		t.Errorf("CleanupExpired() unexpected error = %v", err)
		return
	}
	if count != 0 {
		t.Errorf("CleanupExpired() count = %v, want 0", count)
	}
}

func TestMemoryStateStorage_CleanupExpired_EmptyStorage(t *testing.T) {
	storage := NewMemoryStateStorage()

	count, err := storage.CleanupExpired(context.Background())
	if err != nil {
		t.Errorf("CleanupExpired() unexpected error = %v", err)
		return
	}
	if count != 0 {
		t.Errorf("CleanupExpired() count = %v, want 0", count)
	}
}

// ============================================================================
// Error Factory Tests
// ============================================================================

func TestNewLinkNotFoundError(t *testing.T) {
	err := NewLinkNotFoundError("link-123")
	if err == nil {
		t.Fatal("NewLinkNotFoundError() returned nil")
	}

	dragonErr, ok := err.(*dberrors.DragonError)
	if !ok {
		t.Fatalf("NewLinkNotFoundError() returned wrong type: %T", err)
	}

	if dragonErr.Code != dberrors.ErrNotFound {
		t.Errorf("NewLinkNotFoundError() Code = %v, want %v", dragonErr.Code, dberrors.ErrNotFound)
	}

	if !strings.Contains(err.Error(), "link-123") {
		t.Errorf("NewLinkNotFoundError() error should contain linkID, got: %v", err.Error())
	}

	if dragonErr.Context == nil || dragonErr.Context["link_id"] != "link-123" {
		t.Error("NewLinkNotFoundError() should have link_id in context")
	}
}

func TestNewStateExpiredError(t *testing.T) {
	err := NewStateExpiredError("token-abc")
	if err == nil {
		t.Fatal("NewStateExpiredError() returned nil")
	}

	dragonErr, ok := err.(*dberrors.DragonError)
	if !ok {
		t.Fatalf("NewStateExpiredError() returned wrong type: %T", err)
	}

	if dragonErr.Code != dberrors.ErrForbidden {
		t.Errorf("NewStateExpiredError() Code = %v, want %v", dragonErr.Code, dberrors.ErrForbidden)
	}

	if !strings.Contains(err.Error(), "token-abc") {
		t.Errorf("NewStateExpiredError() error should contain token, got: %v", err.Error())
	}

	if dragonErr.Context == nil || dragonErr.Context["token"] != "token-abc" {
		t.Error("NewStateExpiredError() should have token in context")
	}
}

// ============================================================================
// Interface Satisfaction Tests
// ============================================================================

func TestFederationStorageInterfaceSatisfaction(t *testing.T) {
	// These tests verify compile-time interface satisfaction
	var _ ProviderStorage = (*MemoryProviderStorage)(nil)
	var _ FederationStorage = (*MemoryFederationStorage)(nil)
	var _ StateStorage = (*MemoryStateStorage)(nil)
}

// ============================================================================
// Concurrent Access Tests
// ============================================================================

func TestMemoryProviderStorage_ConcurrentAccess(t *testing.T) {
	storage := NewMemoryProviderStorage()
	ctx := context.Background()

	// Create initial provider
	provider := &ExternalProvider{
		ID:        "provider-1",
		TenantID:  "tenant-1",
		Name:      "Google",
		Type:      ProviderTypeOIDC,
		ClientID:  "client-123",
		IssuerURL: "https://accounts.google.com",
	}
	if err := storage.CreateProvider(ctx, provider); err != nil {
		t.Fatalf("CreateProvider() failed: %v", err)
	}

	done := make(chan bool)
	const goroutines = 10
	const iterations = 100

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				_, _ = storage.GetProvider(ctx, "tenant-1", "provider-1")
				_, _ = storage.GetProviderByName(ctx, "tenant-1", "Google")
				_, _ = storage.ListProviders(ctx, "tenant-1", ListOptions{})
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

func TestMemoryFederationStorage_ConcurrentAccess(t *testing.T) {
	storage := NewMemoryFederationStorage()
	ctx := context.Background()

	// Create initial link
	link := &FederatedIdentity{
		ID:         "link-1",
		UserID:     "user-1",
		TenantID:   "tenant-1",
		ProviderID: "google",
		ExternalID: "ext-123",
	}
	if err := storage.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink() failed: %v", err)
	}

	done := make(chan bool)
	const goroutines = 10
	const iterations = 100

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				_, _ = storage.GetLink(ctx, "link-1")
				_, _ = storage.GetLinkByExternal(ctx, "google", "ext-123")
				_, _ = storage.GetLinksByUser(ctx, "user-1")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

func TestMemoryStateStorage_ConcurrentAccess(t *testing.T) {
	storage := NewMemoryStateStorage()
	ctx := context.Background()

	// Create initial state
	state := &LoginState{
		Nonce:       "test-nonce",
		ProviderID:  "google",
		TenantID:    "tenant-1",
		RedirectURI: "https://example.com/callback",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}
	if err := storage.SaveState(ctx, state); err != nil {
		t.Fatalf("SaveState() failed: %v", err)
	}

	done := make(chan bool)
	const goroutines = 10
	const iterations = 100

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				_, _ = storage.GetState(ctx, "test-nonce")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// ============================================================================
// Provider Timestamp Tests
// ============================================================================

func TestMemoryProviderStorage_TimestampHandling(t *testing.T) {
	storage := NewMemoryProviderStorage()
	ctx := context.Background()

	// Test with zero Created timestamp
	t.Run("auto_set_created_timestamp", func(t *testing.T) {
		provider := &ExternalProvider{
			ID:        "provider-time-1",
			TenantID:  "tenant-1",
			Name:      "TestProvider1",
			Type:      ProviderTypeOIDC,
			ClientID:  "client-1",
			IssuerURL: "https://issuer1.example.com",
		}

		beforeCreate := time.Now()
		err := storage.CreateProvider(ctx, provider)
		if err != nil {
			t.Fatalf("CreateProvider() failed: %v", err)
		}
		afterCreate := time.Now()

		stored, err := storage.GetProvider(ctx, provider.TenantID, provider.ID)
		if err != nil {
			t.Fatalf("GetProvider() failed: %v", err)
		}

		if stored.Created.Before(beforeCreate) || stored.Created.After(afterCreate) {
			t.Errorf("Created timestamp out of expected range")
		}
		if stored.Updated.Before(beforeCreate) || stored.Updated.After(afterCreate) {
			t.Errorf("Updated timestamp out of expected range")
		}
	})

	// Test with pre-set Created timestamp
	t.Run("preserve_preset_created_timestamp", func(t *testing.T) {
		presetTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		provider := &ExternalProvider{
			ID:        "provider-time-2",
			TenantID:  "tenant-1",
			Name:      "TestProvider2",
			Type:      ProviderTypeOIDC,
			ClientID:  "client-2",
			IssuerURL: "https://issuer2.example.com",
			Created:   presetTime,
		}

		err := storage.CreateProvider(ctx, provider)
		if err != nil {
			t.Fatalf("CreateProvider() failed: %v", err)
		}

		stored, err := storage.GetProvider(ctx, provider.TenantID, provider.ID)
		if err != nil {
			t.Fatalf("GetProvider() failed: %v", err)
		}

		if !stored.Created.Equal(presetTime) {
			t.Errorf("Created timestamp = %v, want %v", stored.Created, presetTime)
		}
	})

	// Test update timestamp changes
	t.Run("update_changes_updated_timestamp", func(t *testing.T) {
		provider := &ExternalProvider{
			ID:        "provider-time-3",
			TenantID:  "tenant-1",
			Name:      "TestProvider3",
			Type:      ProviderTypeOIDC,
			ClientID:  "client-3",
			IssuerURL: "https://issuer3.example.com",
		}

		err := storage.CreateProvider(ctx, provider)
		if err != nil {
			t.Fatalf("CreateProvider() failed: %v", err)
		}

		stored, _ := storage.GetProvider(ctx, provider.TenantID, provider.ID)
		originalUpdated := stored.Updated

		// Small delay to ensure timestamp difference
		time.Sleep(10 * time.Millisecond)

		provider.Name = "UpdatedName"
		err = storage.UpdateProvider(ctx, provider)
		if err != nil {
			t.Fatalf("UpdateProvider() failed: %v", err)
		}

		updated, _ := storage.GetProvider(ctx, provider.TenantID, provider.ID)
		if !updated.Updated.After(originalUpdated) {
			t.Error("Updated timestamp should be newer after update")
		}
	})
}

// ============================================================================
// Link Timestamp Tests
// ============================================================================

func TestMemoryFederationStorage_TimestampHandling(t *testing.T) {
	storage := NewMemoryFederationStorage()
	ctx := context.Background()

	// Test with zero Created timestamp
	t.Run("auto_set_created_timestamp", func(t *testing.T) {
		link := &FederatedIdentity{
			ID:         "link-time-1",
			UserID:     "user-1",
			TenantID:   "tenant-1",
			ProviderID: "google",
			ExternalID: "ext-time-1",
		}

		beforeCreate := time.Now()
		err := storage.CreateLink(ctx, link)
		if err != nil {
			t.Fatalf("CreateLink() failed: %v", err)
		}
		afterCreate := time.Now()

		stored, err := storage.GetLink(ctx, link.ID)
		if err != nil {
			t.Fatalf("GetLink() failed: %v", err)
		}

		if stored.Created.Before(beforeCreate) || stored.Created.After(afterCreate) {
			t.Errorf("Created timestamp out of expected range")
		}
	})

	// Test with pre-set Created timestamp
	t.Run("preserve_preset_created_timestamp", func(t *testing.T) {
		presetTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		link := &FederatedIdentity{
			ID:         "link-time-2",
			UserID:     "user-1",
			TenantID:   "tenant-1",
			ProviderID: "google",
			ExternalID: "ext-time-2",
			Created:    presetTime,
		}

		err := storage.CreateLink(ctx, link)
		if err != nil {
			t.Fatalf("CreateLink() failed: %v", err)
		}

		stored, err := storage.GetLink(ctx, link.ID)
		if err != nil {
			t.Fatalf("GetLink() failed: %v", err)
		}

		if !stored.Created.Equal(presetTime) {
			t.Errorf("Created timestamp = %v, want %v", stored.Created, presetTime)
		}
	})
}

// ============================================================================
// Data Isolation Tests
// ============================================================================

func TestMemoryProviderStorage_DataIsolation(t *testing.T) {
	storage := NewMemoryProviderStorage()
	ctx := context.Background()

	original := &ExternalProvider{
		ID:        "provider-iso-1",
		TenantID:  "tenant-1",
		Name:      "OriginalName",
		Type:      ProviderTypeOIDC,
		ClientID:  "client-123",
		IssuerURL: "https://issuer.example.com",
	}

	err := storage.CreateProvider(ctx, original)
	if err != nil {
		t.Fatalf("CreateProvider() failed: %v", err)
	}

	// Modify the original after storage
	original.Name = "ModifiedName"

	// Retrieved provider should have original name
	stored, err := storage.GetProvider(ctx, original.TenantID, original.ID)
	if err != nil {
		t.Fatalf("GetProvider() failed: %v", err)
	}

	if stored.Name != "OriginalName" {
		t.Errorf("Storage should return copy, not reference. Name = %v, want OriginalName", stored.Name)
	}

	// Modify retrieved provider
	stored.Name = "AnotherModification"

	// Second retrieval should still have original name
	stored2, _ := storage.GetProvider(ctx, original.TenantID, original.ID)
	if stored2.Name != "OriginalName" {
		t.Errorf("Storage should return independent copy. Name = %v, want OriginalName", stored2.Name)
	}
}

func TestMemoryFederationStorage_DataIsolation(t *testing.T) {
	storage := NewMemoryFederationStorage()
	ctx := context.Background()

	original := &FederatedIdentity{
		ID:            "link-iso-1",
		UserID:        "user-1",
		TenantID:      "tenant-1",
		ProviderID:    "google",
		ExternalID:    "ext-iso-1",
		ExternalEmail: "original@example.com",
	}

	err := storage.CreateLink(ctx, original)
	if err != nil {
		t.Fatalf("CreateLink() failed: %v", err)
	}

	// Modify the original after storage
	original.ExternalEmail = "modified@example.com"

	// Retrieved link should have original email
	stored, err := storage.GetLink(ctx, original.ID)
	if err != nil {
		t.Fatalf("GetLink() failed: %v", err)
	}

	if stored.ExternalEmail != "original@example.com" {
		t.Errorf("Storage should return copy, not reference. Email = %v, want original@example.com", stored.ExternalEmail)
	}
}

func TestMemoryStateStorage_DataIsolation(t *testing.T) {
	storage := NewMemoryStateStorage()
	ctx := context.Background()

	original := &LoginState{
		Nonce:       "iso-nonce",
		ProviderID:  "google",
		TenantID:    "tenant-1",
		RedirectURI: "https://original.example.com/callback",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}

	err := storage.SaveState(ctx, original)
	if err != nil {
		t.Fatalf("SaveState() failed: %v", err)
	}

	// Modify the original after storage
	original.RedirectURI = "https://modified.example.com/callback"

	// Retrieved state should have original redirect URI
	stored, err := storage.GetState(ctx, original.Nonce)
	if err != nil {
		t.Fatalf("GetState() failed: %v", err)
	}

	if stored.RedirectURI != "https://original.example.com/callback" {
		t.Errorf("Storage should return copy, not reference. RedirectURI = %v, want https://original.example.com/callback", stored.RedirectURI)
	}
}
