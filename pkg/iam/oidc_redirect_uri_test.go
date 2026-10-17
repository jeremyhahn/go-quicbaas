package iam

import (
	"context"
	"errors"
	"testing"
)

// TestRedirectURIMatches covers draft-ietf-oauth-v2-1-16 §2.3.1 (#1438):
// redirect URIs match exactly, except that a registered http loopback IP
// literal matches any port.
func TestRedirectURIMatches(t *testing.T) {
	tests := []struct {
		name       string
		registered string
		requested  string
		want       bool
	}{
		{"exact https", "https://app.example/cb", "https://app.example/cb", true},
		{"https port differs", "https://app.example/cb", "https://app.example:8443/cb", false},
		{"https path differs", "https://app.example/cb", "https://app.example/cb2", false},
		{"ipv4 loopback any port", "http://127.0.0.1/cb", "http://127.0.0.1:51004/cb", true},
		{"ipv4 loopback registered port ignored", "http://127.0.0.1:8080/cb", "http://127.0.0.1:9090/cb", true},
		{"ipv4 loopback exact", "http://127.0.0.1:8080/cb", "http://127.0.0.1:8080/cb", true},
		{"ipv6 loopback any port", "http://[::1]/cb", "http://[::1]:51004/cb", true},
		{"ipv6 loopback registered port ignored", "http://[::1]:8080/cb", "http://[::1]/cb", true},
		{"ipv4 loopback query must match", "http://127.0.0.1/cb?a=1", "http://127.0.0.1:5000/cb?a=1", true},
		{"ipv4 loopback query differs", "http://127.0.0.1/cb?a=1", "http://127.0.0.1:5000/cb?a=2", false},
		{"ipv4 loopback path differs", "http://127.0.0.1/cb", "http://127.0.0.1:5000/other", false},
		{"ipv4 loopback userinfo differs", "http://127.0.0.1/cb", "http://user@127.0.0.1:5000/cb", false},
		{"loopback family differs", "http://127.0.0.1/cb", "http://[::1]:5000/cb", false},
		{"https loopback is exact", "https://127.0.0.1/cb", "https://127.0.0.1:5000/cb", false},
		{"localhost is exact", "http://localhost/cb", "http://localhost:5000/cb", false},
		{"other loopback address is exact", "http://127.0.0.2/cb", "http://127.0.0.2:5000/cb", false},
		{"non-loopback http is exact", "http://app.example/cb", "http://app.example:5000/cb", false},
		{"requested not loopback", "http://127.0.0.1/cb", "http://app.example:5000/cb", false},
		{"unparseable requested", "http://127.0.0.1/cb", "http://127.0.0.1:5000/%zz", false},
		{"empty requested", "http://127.0.0.1/cb", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redirectURIMatches(tt.registered, tt.requested); got != tt.want {
				t.Errorf("redirectURIMatches(%q, %q) = %v, want %v", tt.registered, tt.requested, got, tt.want)
			}
		})
	}
}

// authorizeRedirectTestApp registers a client with the given redirect URIs and
// returns a PKCE authorization request for it without a redirect_uri.
func authorizeRedirectTestApp(t *testing.T, appStorage AppStorage, clientID string, redirectURIs ...string) *AuthorizeRequest {
	t.Helper()
	err := appStorage.CreateApp(context.Background(), &Application{
		ClientID:      clientID,
		TenantID:      "tenant-1",
		RedirectURIs:  redirectURIs,
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	})
	if err != nil {
		t.Fatalf("CreateApp() error = %v", err)
	}
	return authorizeRequestFor(clientID)
}

// authorizeRequestFor returns a PKCE authorization request for clientID
// without a redirect_uri.
func authorizeRequestFor(clientID string) *AuthorizeRequest {
	return &AuthorizeRequest{
		ClientID:            clientID,
		ResponseType:        ResponseTypeCode,
		UserID:              123,
		TenantID:            "tenant-1",
		State:               "state-abc",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: CodeChallengeMethodS256,
	}
}

func TestOIDCAuthorizeLoopbackRedirectPort(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	t.Run("loopback IP accepts any port", func(t *testing.T) {
		req := authorizeRedirectTestApp(t, appStorage, "native-ip", "http://127.0.0.1/callback")
		req.RedirectURI = "http://127.0.0.1:49152/callback"
		resp, err := provider.Authorize(ctx, req)
		if err != nil {
			t.Fatalf("Authorize() error = %v", err)
		}
		if resp.RedirectURI != "http://127.0.0.1:49152/callback" {
			t.Errorf("Authorize() redirect_uri = %q, want the requested port", resp.RedirectURI)
		}
	})

	t.Run("localhost port must match", func(t *testing.T) {
		req := authorizeRedirectTestApp(t, appStorage, "native-host", "http://localhost/callback")
		req.RedirectURI = "http://localhost:49152/callback"
		_, err := provider.Authorize(ctx, req)
		var directErr *AuthorizeDirectError
		if !errors.As(err, &directErr) || directErr.ErrorCode != ErrOAuthInvalidRequest {
			t.Fatalf("Authorize() error = %v, want direct invalid_request", err)
		}
	})
}

// TestOIDCAuthorizeDefaultRedirectURI covers draft-ietf-oauth-v2-1-16 §4.1.1
// (#1439): redirect_uri may be omitted only when the client registered
// exactly one.
func TestOIDCAuthorizeDefaultRedirectURI(t *testing.T) {
	provider, _, appStorage, tokenStorage, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	t.Run("single registered URI is used", func(t *testing.T) {
		req := authorizeRedirectTestApp(t, appStorage, "single-uri", "https://app.example/cb")
		resp, err := provider.Authorize(ctx, req)
		if err != nil {
			t.Fatalf("Authorize() error = %v", err)
		}
		if resp.RedirectURI != "https://app.example/cb" {
			t.Errorf("Authorize() redirect_uri = %q, want the registered one", resp.RedirectURI)
		}
		code, err := tokenStorage.GetAuthCode(ctx, resp.Code)
		if err != nil {
			t.Fatalf("GetAuthCode() error = %v", err)
		}
		if code.RedirectURI != "https://app.example/cb" {
			t.Errorf("stored code redirect_uri = %q, want the resolved one", code.RedirectURI)
		}
	})

	t.Run("several registered URIs is a direct error", func(t *testing.T) {
		req := authorizeRedirectTestApp(t, appStorage, "multi-uri", "https://app.example/a", "https://app.example/b")
		_, err := provider.Authorize(ctx, req)
		var directErr *AuthorizeDirectError
		if !errors.As(err, &directErr) || directErr.ErrorCode != ErrOAuthInvalidRequest {
			t.Fatalf("Authorize() error = %v, want direct invalid_request", err)
		}
	})

	t.Run("no registered URI is a direct error", func(t *testing.T) {
		req := authorizeRedirectTestApp(t, appStorage, "no-uri")
		_, err := provider.Authorize(ctx, req)
		var directErr *AuthorizeDirectError
		if !errors.As(err, &directErr) || directErr.ErrorCode != ErrOAuthInvalidRequest {
			t.Fatalf("Authorize() error = %v, want direct invalid_request", err)
		}
	})
}

// TestOIDCTokenRedirectURIOptional covers draft-ietf-oauth-v2-1-16 §4.1.3
// (#1434): the token endpoint compares redirect_uri only when the client
// sends it, against the value the authorize step resolved.
func TestOIDCTokenRedirectURIOptional(t *testing.T) {
	provider, _, appStorage, _, memStorage := createTestOIDCProvider(t)
	ctx := context.Background()
	addUserToMemStorage(ctx, memStorage, &User{
		ID:        123,
		TenantID:  "tenant-1",
		Email:     "test@example.com",
		Lifecycle: LifecycleActive,
		Roles:     []string{"user"},
	})
	if err := appStorage.CreateApp(ctx, &Application{
		ClientID:      "token-client",
		ClientSecret:  currentSecretHash(t, "test-secret"),
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://app.example/cb"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	}); err != nil {
		t.Fatalf("CreateApp() error = %v", err)
	}
	authorizeTemplate := authorizeRequestFor("token-client")

	// authorize runs the authorize step without a redirect_uri, so the code
	// carries the resolved one.
	authorize := func(t *testing.T) string {
		t.Helper()
		req := *authorizeTemplate
		resp, err := provider.Authorize(ctx, &req)
		if err != nil {
			t.Fatalf("Authorize() error = %v", err)
		}
		return resp.Code
	}
	tokenRequest := func(code, redirectURI string) *TokenRequest {
		return &TokenRequest{
			GrantType:    GrantTypeAuthorizationCode,
			Code:         code,
			RedirectURI:  redirectURI,
			ClientID:     "token-client",
			ClientSecret: "test-secret",
			CodeVerifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		}
	}

	tests := []struct {
		name        string
		redirectURI string
		wantErr     string
	}{
		{name: "omitted succeeds", redirectURI: ""},
		{name: "resolved value succeeds", redirectURI: "https://app.example/cb"},
		{name: "mismatch is invalid_grant", redirectURI: "https://app.example/other", wantErr: ErrOAuthInvalidGrant},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := provider.Token(ctx, tokenRequest(authorize(t), tt.redirectURI))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Token() error = %v", err)
				}
				if resp.AccessToken == "" {
					t.Error("Token() returned empty access token")
				}
				return
			}
			var oauthErr *OAuthError
			if !errors.As(err, &oauthErr) || oauthErr.ErrorCode != tt.wantErr {
				t.Fatalf("Token() error = %v, want %s", err, tt.wantErr)
			}
		})
	}
}
