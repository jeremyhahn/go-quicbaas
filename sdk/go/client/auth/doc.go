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

// Package auth provides authentication and authorization operations for QuicBaaS.
//
// The auth client supports multiple authentication methods:
//
// # JWT Token Authentication
//
// Username/password authentication with JWT access and refresh tokens:
//
//	client := auth.NewClient(transport)
//
//	// Login with credentials
//	resp, err := client.Login(ctx, &auth.LoginRequest{
//	    Username: "user@example.com",
//	    Password: "secret",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Use the access token for API calls
//	fmt.Println("Access Token:", resp.AccessToken)
//
//	// Refresh the token before it expires
//	refreshResp, err := client.RefreshToken(ctx, resp.RefreshToken)
//
//	// Validate a token and get claims
//	claims, err := client.ValidateToken(ctx, resp.AccessToken)
//	if err == nil && claims.Valid {
//	    fmt.Println("User:", claims.Username)
//	    fmt.Println("Roles:", claims.Roles)
//	}
//
// # OIDC/OAuth2 Federated Authentication
//
// Integrate with external identity providers (Google, Okta, Azure AD, etc.):
//
//	// List available identity providers
//	providers, err := client.ListProviders(ctx, tenantID)
//
//	// Start OIDC login flow
//	beginResp, err := client.BeginFederatedLogin(ctx, &auth.BeginFederatedLoginRequest{
//	    TenantID:    "tenant-123",
//	    ProviderID:  "google",
//	    RedirectURI: "https://myapp.com/auth/callback",
//	})
//
//	// Redirect user to: beginResp.AuthorizationURL
//
//	// Handle the callback when user returns
//	callbackResp, err := client.HandleFederatedCallback(ctx, &auth.HandleFederatedCallbackRequest{
//	    TenantID:    "tenant-123",
//	    ProviderID:  "google",
//	    Code:        authorizationCode,
//	    State:       state,
//	    RedirectURI: "https://myapp.com/auth/callback",
//	})
//
// # Session Management
//
// Manage user sessions across devices:
//
//	// List all active sessions
//	sessions, err := client.ListSessions(ctx)
//	for _, s := range sessions.Sessions {
//	    fmt.Printf("Session %s from %s\n", s.ID, s.IPAddress)
//	}
//
//	// Revoke a specific session (e.g., from another device)
//	err = client.RevokeSession(ctx, sessionID)
//
//	// Revoke all sessions (security measure)
//	err = client.RevokeAllSessions(ctx)
//
// # Token Introspection
//
// For resource servers to validate tokens:
//
//	introspection, err := client.IntrospectToken(ctx, accessToken)
//	if introspection.Active {
//	    fmt.Println("Token is valid for user:", introspection.Username)
//	}
//
// # JWKS (JSON Web Key Set)
//
// Get public keys for local token verification:
//
//	jwks, err := client.GetJWKS(ctx)
//	for _, key := range jwks.Keys {
//	    fmt.Printf("Key ID: %s, Algorithm: %s\n", key.KID, key.ALG)
//	}
//
// # OIDC Discovery
//
// Discover authentication server endpoints and capabilities:
//
//	config, err := client.GetOpenIDConfiguration(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Println("Issuer:", config.Issuer)
//	fmt.Println("Authorization Endpoint:", config.AuthorizationEndpoint)
//	fmt.Println("Token Endpoint:", config.TokenEndpoint)
//	fmt.Println("JWKS URI:", config.JWKSURI)
//	fmt.Println("Scopes Supported:", config.ScopesSupported)
//	fmt.Println("Response Types Supported:", config.ResponseTypesSupported)
//
// # Multi-Factor Authentication (MFA)
//
// When MFA is enabled, login may require additional verification:
//
//	resp, err := client.Login(ctx, &auth.LoginRequest{
//	    Username: "user@example.com",
//	    Password: "secret",
//	})
//	if resp.MFARequired {
//	    // Prompt user for MFA code
//	    resp, err = client.Login(ctx, &auth.LoginRequest{
//	        Username: "user@example.com",
//	        Password: "secret",
//	        MFACode:  totpCode,
//	    })
//	}
//
// # mTLS Authentication
//
// QuicBaaS also supports mutual TLS (mTLS) for authentication at the transport level:
//
//	client, err := quicbaas.NewClient("localhost:50051",
//	    quicbaas.WithTLS(tlsConfig),
//	)
//
// This provides certificate-based authentication without additional credentials.
package auth
