package iam

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestRegisterApp_ResponseTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		grantTypes    []string
		responseTypes []string
		wantErr       error
	}{
		{
			name:          "code with authorization_code is stored",
			grantTypes:    []string{GrantTypeAuthorizationCode},
			responseTypes: []string{ResponseTypeCode},
		},
		{
			name:          "code with default grants is stored",
			responseTypes: []string{ResponseTypeCode},
		},
		{
			name:          "unsupported response type is refused",
			grantTypes:    []string{GrantTypeAuthorizationCode},
			responseTypes: []string{"token"},
			wantErr:       ErrInvalidResponseType,
		},
		{
			name:          "code without authorization_code is refused",
			grantTypes:    []string{GrantTypeClientCredentials},
			responseTypes: []string{ResponseTypeCode},
			wantErr:       ErrInvalidResponseType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			mgr := NewAppManager(NewMemoryStorage(), nil)

			app, err := mgr.RegisterApp(ctx, "tenant-1", RegisterAppOptions{
				Name:          "web",
				RedirectURIs:  []string{"https://app.example.com/cb"},
				GrantTypes:    tt.grantTypes,
				ResponseTypes: tt.responseTypes,
			})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("RegisterApp error = %v, want %v", err, tt.wantErr)
				}
				assertInvalidInput(t, err)
				apps, listErr := mgr.ListApps(ctx, "tenant-1", ListOptions{})
				if listErr != nil {
					t.Fatalf("ListApps: %v", listErr)
				}
				if len(apps) != 0 {
					t.Fatalf("refused registration stored %d apps", len(apps))
				}
				return
			}
			if err != nil {
				t.Fatalf("RegisterApp: %v", err)
			}
			stored, err := mgr.GetAppInTenant(ctx, "tenant-1", app.ClientID)
			if err != nil {
				t.Fatalf("GetAppInTenant: %v", err)
			}
			if !slices.Equal(stored.ResponseTypes, tt.responseTypes) {
				t.Fatalf("ResponseTypes = %v, want %v", stored.ResponseTypes, tt.responseTypes)
			}
		})
	}
}

func TestUpdateAppInTenant_ResponseTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		grantTypes    *[]string
		responseTypes []string
		wantErr       error
	}{
		{
			name:          "code with authorization_code is stored",
			grantTypes:    &[]string{GrantTypeAuthorizationCode},
			responseTypes: []string{ResponseTypeCode},
		},
		{
			name:          "unsupported response type is refused",
			grantTypes:    &[]string{GrantTypeAuthorizationCode},
			responseTypes: []string{"id_token"},
			wantErr:       ErrInvalidResponseType,
		},
		{
			name:          "code without authorization_code is refused",
			responseTypes: []string{ResponseTypeCode},
			wantErr:       ErrInvalidResponseType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			// The app is registered for client_credentials only.
			mgr, _, app := newUpdateTestApp(t)

			rts := tt.responseTypes
			_, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{
				GrantTypes:    tt.grantTypes,
				ResponseTypes: &rts,
			})
			stored, getErr := mgr.GetAppInTenant(ctx, "tenant-1", app.ClientID)
			if getErr != nil {
				t.Fatalf("GetAppInTenant: %v", getErr)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("UpdateAppInTenant error = %v, want %v", err, tt.wantErr)
				}
				assertInvalidInput(t, err)
				if !slices.Equal(stored.GrantTypes, app.GrantTypes) || !slices.Equal(stored.ResponseTypes, app.ResponseTypes) {
					t.Fatalf("refused update changed the app: grants %v responses %v", stored.GrantTypes, stored.ResponseTypes)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateAppInTenant: %v", err)
			}
			if !slices.Equal(stored.ResponseTypes, tt.responseTypes) {
				t.Fatalf("ResponseTypes = %v, want %v", stored.ResponseTypes, tt.responseTypes)
			}
		})
	}
}

func TestDiscoveryAdvertisesSupportedResponseTypes(t *testing.T) {
	t.Parallel()
	provider, _, _, _, _ := createTestOIDCProvider(t)
	want := SupportedResponseTypes()
	if got, ok := provider.GetConfiguration()["response_types_supported"].([]string); !ok || !slices.Equal(got, want) {
		t.Fatalf("configuration response_types_supported = %v, want %v", got, want)
	}
	if got := provider.GetOAuthASMetadata().ResponseTypesSupported; !slices.Equal(got, want) {
		t.Fatalf("AS metadata response_types_supported = %v, want %v", got, want)
	}
	for _, rt := range SupportedResponseTypes() {
		if err := validateResponseTypes([]string{rt}, []string{GrantTypeAuthorizationCode}); err != nil {
			t.Fatalf("advertised response type %q refused: %v", rt, err)
		}
	}
	if err := validateResponseTypes([]string{"none"}, []string{GrantTypeAuthorizationCode}); !errors.Is(err, ErrInvalidResponseType) {
		t.Fatalf("validateResponseTypes(none) = %v, want ErrInvalidResponseType", err)
	}
}
