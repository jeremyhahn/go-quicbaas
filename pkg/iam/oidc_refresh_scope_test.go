package iam

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"
)

// refreshScopeGrant is the scope the refresh credentials in these tests were
// originally granted.
var refreshScopeGrant = []string{"openid", "profile", "email"}

// refreshScopeFixture registers a confidential client and mints one refresh
// credential for it, granted refreshScopeGrant. It returns the provider, its
// storage, the client's credentials and the refresh token value.
func refreshScopeFixture(t *testing.T) (*OIDCProvider, *MemoryStorage, string, string, string) {
	t.Helper()

	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()
	user := seedCeremonyUser(t, storage, sessionIDTenant, sessionIDUser, []string{"operator"})

	const clientID, clientSecret = "app_refresh_scope_client", "s3cret-for-the-test"
	if err := provider.appStorage.CreateApp(ctx, &Application{
		ClientID:     clientID,
		ClientSecret: currentSecretHash(t, clientSecret),
		TenantID:     sessionIDTenant,
		GrantTypes:   []string{GrantTypeRefreshToken},
	}); err != nil {
		t.Fatalf("register app: %v", err)
	}

	refreshToken, err := provider.tokenService.GenerateRefreshToken(ctx, SessionRefreshOptions{
		Subject:  strconv.FormatUint(user.ID, 10),
		TenantID: user.TenantID,
		ClientID: clientID,
		Scopes:   refreshScopeGrant,
		Session:  TokenSessionContext{UserID: user.ID},
	})
	if err != nil {
		t.Fatalf("mint refresh token: %v", err)
	}
	return provider, storage, clientID, clientSecret, refreshToken
}

// TestRefreshTokenGrant_RefusesScopeWiderThanTheGrant covers OAuth 2.1
// §4.3.1: a requested scope that includes a scope not originally granted is
// refused with invalid_scope, no token is issued, and the presented refresh
// credential is not consumed by the refusal.
func TestRefreshTokenGrant_RefusesScopeWiderThanTheGrant(t *testing.T) {
	t.Parallel()

	provider, storage, clientID, clientSecret, refreshToken := refreshScopeFixture(t)
	ctx := context.Background()

	resp, err := provider.handleRefreshTokenGrant(ctx, &TokenRequest{
		GrantType:    GrantTypeRefreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refreshToken,
		Scope:        []string{"openid", "admin"},
	})
	var oauthErr *OAuthError
	if !errors.As(err, &oauthErr) || oauthErr.ErrorCode != ErrOAuthInvalidScope {
		t.Fatalf("refusal = %v, want an %s OAuth error", err, ErrOAuthInvalidScope)
	}
	if resp != nil {
		t.Fatalf("a refused refresh issued a token response: %+v", resp)
	}

	original, err := storage.GetTokenByValue(ctx, refreshToken)
	if err != nil {
		t.Fatalf("resolve the presented credential: %v", err)
	}
	if original.Revoked {
		t.Fatal("a refused refresh consumed the presented refresh token")
	}
}

// TestRefreshTokenGrant_NarrowsToARequestedSubset covers the permitted half of
// §4.3.1: a strict subset of the original grant is issued exactly on the new
// access token, while the rotated refresh credential keeps the original grant
// (draft-ietf-oauth-v2-1-16 §4.3.3: a new refresh token's scope MUST be
// identical to that of the refresh token the client presented).
func TestRefreshTokenGrant_NarrowsToARequestedSubset(t *testing.T) {
	t.Parallel()

	provider, storage, clientID, clientSecret, refreshToken := refreshScopeFixture(t)
	ctx := context.Background()

	subset := []string{"openid", "email"}
	resp, err := provider.handleRefreshTokenGrant(ctx, &TokenRequest{
		GrantType:    GrantTypeRefreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refreshToken,
		Scope:        subset,
	})
	if err != nil {
		t.Fatalf("refresh_token grant: %v", err)
	}
	if resp.Scope != joinScopes(subset) {
		t.Fatalf("issued scope = %q, want %q", resp.Scope, joinScopes(subset))
	}

	rotated, err := storage.GetTokenByValue(ctx, resp.RefreshToken)
	if err != nil {
		t.Fatalf("resolve the rotated credential: %v", err)
	}
	if !reflect.DeepEqual(rotated.Scopes, refreshScopeGrant) {
		t.Fatalf("rotated refresh scope = %v, want the original %v", rotated.Scopes, refreshScopeGrant)
	}
}

// TestRefreshTokenGrant_OmittedScopeKeepsTheOriginalGrant guards the default:
// a refresh without a scope parameter is issued the scope originally granted.
func TestRefreshTokenGrant_OmittedScopeKeepsTheOriginalGrant(t *testing.T) {
	t.Parallel()

	provider, _, clientID, clientSecret, refreshToken := refreshScopeFixture(t)

	resp, err := provider.handleRefreshTokenGrant(context.Background(), &TokenRequest{
		GrantType:    GrantTypeRefreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refreshToken,
	})
	if err != nil {
		t.Fatalf("refresh_token grant: %v", err)
	}
	if resp.Scope != joinScopes(refreshScopeGrant) {
		t.Fatalf("issued scope = %q, want %q", resp.Scope, joinScopes(refreshScopeGrant))
	}
}

func TestRefreshGrantScopes(t *testing.T) {
	t.Parallel()

	granted := []string{"openid", "profile"}
	tests := []struct {
		name      string
		requested []string
		want      []string
		wantErr   bool
	}{
		{name: "omitted keeps the grant", requested: nil, want: granted},
		{name: "equal set", requested: []string{"profile", "openid"}, want: []string{"profile", "openid"}},
		{name: "strict subset", requested: []string{"profile"}, want: []string{"profile"}},
		{name: "one scope beyond the grant", requested: []string{"openid", "email"}, wantErr: true},
		{name: "nothing in common", requested: []string{"admin"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := refreshGrantScopes(tt.requested, granted)
			if tt.wantErr {
				var oauthErr *OAuthError
				if !errors.As(err, &oauthErr) || oauthErr.ErrorCode != ErrOAuthInvalidScope {
					t.Fatalf("err = %v, want an %s OAuth error", err, ErrOAuthInvalidScope)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("scopes = %v, want %v", got, tt.want)
			}
		})
	}
}

// A refresh request may narrow the originally granted scope but never widen it
// (draft-ietf-oauth-v2-1-16 §4.3.1, go-quicbaas#1436).
func TestRefreshTokenGrant_ScopeMayNotWiden(t *testing.T) {
	t.Parallel()

	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()
	user := seedCeremonyUser(t, storage, sessionIDTenant, sessionIDUser, []string{"operator"})

	const clientID, clientSecret = "app_refresh_scope_client", "s3cret-for-the-test"
	if err := provider.appStorage.CreateApp(ctx, &Application{
		ClientID:     clientID,
		ClientSecret: currentSecretHash(t, clientSecret),
		TenantID:     sessionIDTenant,
		GrantTypes:   []string{GrantTypeRefreshToken},
	}); err != nil {
		t.Fatalf("register app: %v", err)
	}

	granted := []string{"openid", "profile", "email"}

	// redeem presents a refresh token with the requested scope.
	redeem := func(t *testing.T, refreshToken string, requested []string) (*TokenResponse, error) {
		t.Helper()
		return provider.handleRefreshTokenGrant(ctx, &TokenRequest{
			GrantType:    GrantTypeRefreshToken,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RefreshToken: refreshToken,
			Scope:        requested,
		})
	}

	// refresh mints a refresh token for the original grant and redeems it.
	refresh := func(t *testing.T, requested []string) (*TokenResponse, error) {
		t.Helper()
		value, err := provider.tokenService.GenerateRefreshToken(ctx, SessionRefreshOptions{
			Subject:  strconv.FormatUint(user.ID, 10),
			TenantID: user.TenantID,
			ClientID: clientID,
			Scopes:   granted,
			Session:  TokenSessionContext{UserID: user.ID},
		})
		if err != nil {
			t.Fatalf("GenerateRefreshToken: %v", err)
		}
		return redeem(t, value, requested)
	}

	// issuedScopes reads the scope the rotated refresh credential carries.
	issuedScopes := func(t *testing.T, resp *TokenResponse) []string {
		t.Helper()
		rotated, err := storage.GetTokenByValue(ctx, resp.RefreshToken)
		if err != nil {
			t.Fatalf("resolve the rotated credential: %v", err)
		}
		return rotated.Scopes
	}

	t.Run("a wider scope is refused with invalid_scope", func(t *testing.T) {
		_, err := refresh(t, []string{"openid", "admin"})
		var oauthErr *OAuthError
		if !errors.As(err, &oauthErr) {
			t.Fatalf("err = %v, want an *OAuthError", err)
		}
		if oauthErr.ErrorCode != ErrOAuthInvalidScope {
			t.Fatalf("error code = %q, want %q", oauthErr.ErrorCode, ErrOAuthInvalidScope)
		}
	})

	t.Run("a strict subset is issued with that subset", func(t *testing.T) {
		resp, err := refresh(t, []string{"openid", "email"})
		if err != nil {
			t.Fatalf("refresh_token grant: %v", err)
		}
		if resp.Scope != "openid email" {
			t.Fatalf("access token scope = %q, want \"openid email\"", resp.Scope)
		}
	})

	// -16 §4.3.3: the rotated refresh token's scope is identical to the
	// presented one's; only the access token carries the narrowed scope.
	t.Run("a narrowed refresh keeps the refresh token's full scope", func(t *testing.T) {
		narrowed, err := refresh(t, []string{"openid"})
		if err != nil {
			t.Fatalf("narrowed refresh_token grant: %v", err)
		}
		if narrowed.Scope != "openid" {
			t.Fatalf("access token scope = %q, want openid", narrowed.Scope)
		}
		if got := issuedScopes(t, narrowed); !slices.Equal(got, granted) {
			t.Fatalf("rotated refresh scope = %v, want the original %v", got, granted)
		}

		again, err := redeem(t, narrowed.RefreshToken, nil)
		if err != nil {
			t.Fatalf("second refresh with no scope: %v", err)
		}
		if got := issuedScopes(t, again); !slices.Equal(got, granted) {
			t.Fatalf("second refresh scope = %v, want the original %v", got, granted)
		}

		dropped, err := redeem(t, again.RefreshToken, []string{"profile"})
		if err != nil {
			t.Fatalf("refresh asking for a scope the first refresh dropped: %v", err)
		}
		if dropped.Scope != "profile" {
			t.Fatalf("access token scope = %q, want profile", dropped.Scope)
		}
	})

	t.Run("no scope keeps the original grant", func(t *testing.T) {
		resp, err := refresh(t, nil)
		if err != nil {
			t.Fatalf("refresh_token grant: %v", err)
		}
		if got := issuedScopes(t, resp); !slices.Equal(got, granted) {
			t.Fatalf("issued scope = %v, want the original %v", got, granted)
		}
	})
}
