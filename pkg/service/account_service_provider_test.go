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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// setProviderRegistry installs registry on an AccountService built by
// newTestAccountService, mirroring what server bring-up does.
func setProviderRegistry(t *testing.T, svc AccountService, registry *orchestration.AccountProviderRegistry) {
	t.Helper()
	wirer, ok := svc.(interface {
		SetProviderRegistry(*orchestration.AccountProviderRegistry)
	})
	if !ok {
		t.Fatalf("AccountService %T does not expose SetProviderRegistry", svc)
	}
	wirer.SetProviderRegistry(registry)
}

// TestAccountService_CreateAccount_OAuthWithoutResolvableEndpoint proves the
// failure an operator used to discover only when a live agent session tried to
// start now happens at write time.
func TestAccountService_CreateAccount_OAuthWithoutResolvableEndpoint(t *testing.T) {
	svc := newTestAccountService(t)

	_, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		Credential:    &orchestration.AccountCredential{AccessToken: "at", RefreshToken: "rt"},
	})

	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("CreateAccount() error = %v (%T), want *AccountInvalidError", err, err)
	}
	if invalid.Field != "credential.token_endpoint" {
		t.Fatalf("error field = %q, want credential.token_endpoint", invalid.Field)
	}
	if !errors.Is(err, orchestration.ErrAccountProviderEndpointUnresolved) {
		t.Fatalf("errors.Is(err, ErrAccountProviderEndpointUnresolved) = false for %v", err)
	}
}

// TestAccountService_CreateAccount_OAuthEndpointFromProviderRegistry proves a
// configured provider default satisfies the same rule, so an operator can
// declare the endpoint once instead of on every pooled account.
func TestAccountService_CreateAccount_OAuthEndpointFromProviderRegistry(t *testing.T) {
	svc := newTestAccountService(t)
	registry, err := orchestration.NewAccountProviderRegistry(orchestration.ProviderDescriptor{
		Provider:      orchestration.AccountProviderClaude,
		TokenEndpoint: "https://auth.example/token",
	})
	if err != nil {
		t.Fatalf("NewAccountProviderRegistry() error = %v", err)
	}
	setProviderRegistry(t, svc, registry)

	resp, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		Credential:    &orchestration.AccountCredential{AccessToken: "at", RefreshToken: "rt"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if resp.Account.RefreshPolicy != orchestration.AccountRefreshOAuth2 {
		t.Fatalf("RefreshPolicy = %q, want oauth2", resp.Account.RefreshPolicy)
	}
}

// TestAccountService_CreateAccount_OAuthEndpointOnTheAccount proves a
// per-account endpoint still works with no provider configuration at all.
func TestAccountService_CreateAccount_OAuthEndpointOnTheAccount(t *testing.T) {
	svc := newTestAccountService(t)

	if _, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		Credential: &orchestration.AccountCredential{
			AccessToken:   "at",
			RefreshToken:  "rt",
			TokenEndpoint: "https://account.example/token",
		},
	}); err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
}

// TestAccountService_CreateAccount_CredentialModeRules covers the two mode
// rules the write path preserves.
func TestAccountService_CreateAccount_CredentialModeRules(t *testing.T) {
	tests := []struct {
		name    string
		req     *CreateAccountRequest
		wantErr error
	}{
		{
			name: "oauth account without a refresh token",
			req: &CreateAccountRequest{
				TenantID:      "tenant-a",
				Provider:      orchestration.AccountProviderClaude,
				RefreshPolicy: orchestration.AccountRefreshOAuth2,
				Credential:    &orchestration.AccountCredential{AccessToken: "at", TokenEndpoint: "https://account.example/token"},
			},
			wantErr: orchestration.ErrAccountRefreshTokenRequired,
		},
		{
			name: "static account carrying a refresh token",
			req: &CreateAccountRequest{
				TenantID:      "tenant-a",
				Provider:      orchestration.AccountProviderClaude,
				RefreshPolicy: orchestration.AccountRefreshNone,
				Credential:    &orchestration.AccountCredential{AccessToken: "at", RefreshToken: "rt"},
			},
			wantErr: orchestration.ErrAccountStaticTokenRefreshable,
		},
		{
			name: "unrecognized mode",
			req: &CreateAccountRequest{
				TenantID:      "tenant-a",
				Provider:      orchestration.AccountProviderClaude,
				RefreshPolicy: "sometimes",
				Credential:    &orchestration.AccountCredential{AccessToken: "at"},
			},
			wantErr: orchestration.ErrAccountCredentialModeInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestAccountService(t)
			_, err := svc.CreateAccount(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateAccount() error = %v, want %v", err, tt.wantErr)
			}
			var invalid *AccountInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("CreateAccount() error = %v (%T), want it to also be an *AccountInvalidError so transports answer 400", err, err)
			}
		})
	}
}

// TestAccountService_CreateAccount_DefaultsToStaticMode proves an account that
// declares no mode is still accepted and is stored as the static one.
func TestAccountService_CreateAccount_DefaultsToStaticMode(t *testing.T) {
	svc := newTestAccountService(t)

	resp, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID:   "tenant-a",
		Provider:   orchestration.AccountProviderCodex,
		Credential: &orchestration.AccountCredential{Env: map[string]string{"OPENAI_API_KEY": "sk-test"}},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if resp.Account.RefreshPolicy != orchestration.AccountRefreshNone {
		t.Fatalf("RefreshPolicy = %q, want none", resp.Account.RefreshPolicy)
	}
}

// TestAccountService_RotateAccountCredential_ValidatesAgainstAccountMode
// proves a rotation cannot slip past the rules creation enforced.
func TestAccountService_RotateAccountCredential_ValidatesAgainstAccountMode(t *testing.T) {
	ctx := context.Background()
	svc := newTestAccountService(t)

	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		Credential: &orchestration.AccountCredential{
			AccessToken: "at", RefreshToken: "rt", TokenEndpoint: "https://account.example/token",
		},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	t.Run("rejects a replacement with no refresh token", func(t *testing.T) {
		_, rotateErr := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
			TenantID:   "tenant-a",
			AccountID:  created.Account.ID,
			Credential: &orchestration.AccountCredential{AccessToken: "at2", TokenEndpoint: "https://account.example/token"},
		})
		if !errors.Is(rotateErr, orchestration.ErrAccountRefreshTokenRequired) {
			t.Fatalf("RotateAccountCredential() error = %v, want ErrAccountRefreshTokenRequired", rotateErr)
		}
	})

	t.Run("rejects a replacement with no resolvable endpoint", func(t *testing.T) {
		_, rotateErr := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
			TenantID:   "tenant-a",
			AccountID:  created.Account.ID,
			Credential: &orchestration.AccountCredential{AccessToken: "at2", RefreshToken: "rt2"},
		})
		if !errors.Is(rotateErr, orchestration.ErrAccountProviderEndpointUnresolved) {
			t.Fatalf("RotateAccountCredential() error = %v, want ErrAccountProviderEndpointUnresolved", rotateErr)
		}
	})

	t.Run("accepts a valid replacement", func(t *testing.T) {
		if _, rotateErr := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
			TenantID:  "tenant-a",
			AccountID: created.Account.ID,
			Credential: &orchestration.AccountCredential{
				AccessToken: "at2", RefreshToken: "rt2", TokenEndpoint: "https://account.example/token",
			},
		}); rotateErr != nil {
			t.Fatalf("RotateAccountCredential() error = %v", rotateErr)
		}
	})
}

// TestAccountService_UpdateAccount_RejectsUnrecognizedMode proves the metadata
// update path cannot write a mode nothing understands.
func TestAccountService_UpdateAccount_RejectsUnrecognizedMode(t *testing.T) {
	ctx := context.Background()
	svc := newTestAccountService(t)

	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID:   "tenant-a",
		Provider:   orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "at"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	_, err = svc.UpdateAccount(ctx, &UpdateAccountRequest{
		TenantID:      "tenant-a",
		AccountID:     created.Account.ID,
		RefreshPolicy: "sometimes",
	})
	if !errors.Is(err, orchestration.ErrAccountCredentialModeInvalid) {
		t.Fatalf("UpdateAccount() error = %v, want ErrAccountCredentialModeInvalid", err)
	}

	// A recognized mode still applies.
	updated, err := svc.UpdateAccount(ctx, &UpdateAccountRequest{
		TenantID:      "tenant-a",
		AccountID:     created.Account.ID,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
	})
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if updated.Account.RefreshPolicy != orchestration.AccountRefreshOAuth2 {
		t.Fatalf("RefreshPolicy = %q, want oauth2", updated.Account.RefreshPolicy)
	}
}

// TestAccountService_SetProviderRegistry_IgnoresNil proves the wiring seam
// cannot accidentally disable validation by passing nil.
func TestAccountService_SetProviderRegistry_IgnoresNil(t *testing.T) {
	svc := newTestAccountService(t)
	setProviderRegistry(t, svc, nil)

	_, err := svc.CreateAccount(context.Background(), &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshOAuth2,
		Credential:    &orchestration.AccountCredential{AccessToken: "at", RefreshToken: "rt"},
	})
	if !errors.Is(err, orchestration.ErrAccountProviderEndpointUnresolved) {
		t.Fatalf("CreateAccount() error = %v, want the built-in registry still validating", err)
	}
}
