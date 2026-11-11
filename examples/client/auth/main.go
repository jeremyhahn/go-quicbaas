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

// Example: Authentication and Authorization
//
// This example demonstrates:
// - Connecting to QuicBaaS using the SDK with multiprotocol support
// - JWT token authentication (login, validate, refresh, logout)
// - Multi-factor authentication (MFA) handling
// - OAuth2/OIDC federated authentication flow
// - Session management (list, revoke)
// - Token introspection for service-to-service validation
// - OIDC discovery and JWKS retrieval
//
// Prerequisites:
// - A running QuicBaaS server (default: localhost:50051)
// - Server must be initialized and unsealed
// - For OAuth2 examples: configured identity providers
//
// Usage:
//
//	# Run with default gRPC transport
//	go run main.go
//
//	# Run with REST transport
//	go run main.go -transport rest -address http://localhost:8080
//
//	# Run with QUIC transport
//	go run main.go -transport quic -address localhost:50052
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	flag.Parse()

	fmt.Println("=== QuicBaaS Authentication Example ===")
	fmt.Printf("Transport: %s\n", *transportType)
	fmt.Printf("Address:   %s\n", *address)
	fmt.Println()

	// Create client with the specified transport
	var opts []quicbaas.Option
	opts = append(opts, quicbaas.WithTimeout(30*time.Second))
	// Select transport based on flag
	switch *transportType {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
		// default is gRPC, no option needed
	}

	client, err := quicbaas.NewClient(*address, opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Run demonstration of all auth features
	demonstrateJWTAuthentication(ctx, client)
	demonstrateMFAAuthentication(ctx, client)
	demonstrateOAuth2Flow(ctx, client)
	demonstrateSessionManagement(ctx, client)
	demonstrateTokenIntrospection(ctx, client)
	demonstrateOIDCDiscovery(ctx, client)

	printKeyTakeaways()
}

// demonstrateJWTAuthentication shows the basic JWT authentication flow.
func demonstrateJWTAuthentication(ctx context.Context, client quicbaas.Client) {
	fmt.Println("=== Part 1: JWT Token Authentication ===")
	fmt.Println()

	// Step 1: Login with username and password
	fmt.Println("Step 1: Logging in with username/password...")
	// TODO: Password-based login was removed; this example needs to migrate to
	// WebAuthn/PIV smartcard once those flows are wired through the SDK.
	loginResp, err := client.Security().Auth().Login(ctx, &service.ExtendedLoginRequest{
		Username: "admin@example.com",
		TenantID: "default",
	})
	if err != nil {
		log.Printf("  Login failed (expected in demo without server): %v", err)
		fmt.Println("  [Demo] Simulating successful login response...")
		fmt.Println("  Access Token:  eyJhbGciOiJSUzI1NiIs... (truncated)")
		fmt.Println("  Refresh Token: eyJhbGciOiJSUzI1NiIs... (truncated)")
		fmt.Println("  Token Type:    Bearer")
		fmt.Println("  Expires In:    3600 seconds")
		fmt.Println()

		// Continue with demo flow
		demonstrateTokenValidation(ctx, client, "demo-access-token")
		demonstrateTokenRefresh(ctx, client, "demo-refresh-token")
		demonstrateLogout(ctx, client, "demo-access-token")
		return
	}

	// Check if MFA is required
	if loginResp.MFARequired {
		fmt.Println("  MFA Required! Challenge ID:", loginResp.MFAChallenge.ChallengeID)
		fmt.Println("  MFA Type:", loginResp.MFAChallenge.Type.String())
		fmt.Println("  Expires At:", loginResp.MFAChallenge.ExpiresAt.Format(time.RFC3339))
		fmt.Println()
		return
	}

	fmt.Printf("  Access Token:  %s... (truncated)\n", truncateToken(loginResp.AccessToken))
	fmt.Printf("  Refresh Token: %s... (truncated)\n", truncateToken(loginResp.RefreshToken))
	fmt.Printf("  Token Type:    %s\n", loginResp.TokenType)
	fmt.Printf("  Expires At:    %s\n", loginResp.ExpiresAt.Format(time.RFC3339))
	fmt.Println()

	// Continue with the full authentication flow
	demonstrateTokenValidation(ctx, client, loginResp.AccessToken)
	demonstrateTokenRefresh(ctx, client, loginResp.RefreshToken)
	demonstrateLogout(ctx, client, loginResp.AccessToken)
}

// demonstrateTokenValidation shows how to validate and decode JWT claims.
func demonstrateTokenValidation(ctx context.Context, client quicbaas.Client, accessToken string) {
	// Step 2: Validate the access token
	fmt.Println("Step 2: Validating access token...")
	claims, err := client.Security().Auth().ValidateToken(ctx, accessToken)
	if err != nil {
		log.Printf("  Token validation failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating token claims...")
		fmt.Println("  Valid:       true")
		fmt.Println("  User ID:     user-123")
		fmt.Println("  Username:    admin@example.com")
		fmt.Println("  Tenant ID:   default")
		fmt.Println("  Roles:       [admin, developer]")
		fmt.Println("  Permissions: [quicbaas:*:*, secrets:*:*]")
		fmt.Println("  Session ID:  session-456")
		fmt.Println("  Issuer:      https://quicbaas.example.com")
		fmt.Println()
		return
	}

	fmt.Printf("  Valid:       %v\n", claims.Valid)
	fmt.Printf("  User ID:     %s\n", claims.UserID)
	fmt.Printf("  Username:    %s\n", claims.Username)
	fmt.Printf("  Email:       %s\n", claims.Email)
	fmt.Printf("  Tenant ID:   %s\n", claims.TenantID)
	fmt.Printf("  Roles:       %v\n", claims.Roles)
	fmt.Printf("  Groups:      %v\n", claims.Groups)
	fmt.Printf("  Permissions: %v\n", claims.Permissions)
	fmt.Printf("  Session ID:  %s\n", claims.SessionID)
	fmt.Printf("  Issued At:   %s\n", claims.IssuedAt.Format(time.RFC3339))
	fmt.Printf("  Expires At:  %s\n", claims.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("  Issuer:      %s\n", claims.Issuer)
	fmt.Printf("  Audience:    %v\n", claims.Audience)
	fmt.Println()
}

// demonstrateTokenRefresh shows how to refresh an expiring access token.
func demonstrateTokenRefresh(ctx context.Context, client quicbaas.Client, refreshToken string) {
	// Step 3: Refresh the access token
	fmt.Println("Step 3: Refreshing access token...")
	refreshResp, err := client.Security().Auth().RefreshToken(ctx, refreshToken)
	if err != nil {
		log.Printf("  Token refresh failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating token refresh response...")
		fmt.Println("  New Access Token:  eyJhbGciOiJSUzI1NiIs... (truncated)")
		fmt.Println("  New Refresh Token: eyJhbGciOiJSUzI1NiIs... (rotated)")
		fmt.Println("  Token Type:        Bearer")
		fmt.Println("  Expires In:        3600 seconds")
		fmt.Println()
		return
	}

	fmt.Printf("  New Access Token:  %s... (truncated)\n", truncateToken(refreshResp.AccessToken))
	if refreshResp.RefreshToken != "" {
		fmt.Printf("  New Refresh Token: %s... (rotated)\n", truncateToken(refreshResp.RefreshToken))
	}
	fmt.Printf("  Token Type:        %s\n", refreshResp.TokenType)
	fmt.Printf("  Expires At:        %s\n", refreshResp.ExpiresAt.Format(time.RFC3339))
	fmt.Println()
}

// demonstrateLogout shows how to properly end a session.
func demonstrateLogout(ctx context.Context, client quicbaas.Client, accessToken string) {
	// Step 4: Logout (revoke tokens)
	fmt.Println("Step 4: Logging out (revoking tokens)...")
	err := client.Security().Auth().Logout(ctx, accessToken)
	if err != nil {
		log.Printf("  Logout failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating successful logout...")
	}
	fmt.Println("  Session terminated successfully")
	fmt.Println("  Access token and refresh token have been revoked")
	fmt.Println()
}

// demonstrateMFAAuthentication shows multi-factor authentication handling.
func demonstrateMFAAuthentication(ctx context.Context, client quicbaas.Client) {
	fmt.Println("=== Part 2: Multi-Factor Authentication (MFA) ===")
	fmt.Println()

	// Step 1: Attempt login - may trigger MFA challenge
	fmt.Println("Step 1: Login attempt with MFA-enabled account...")
	fmt.Println("  [Demo] Simulating MFA challenge response...")
	fmt.Println("  MFA Required:   true")
	fmt.Println("  Challenge ID:   mfa-challenge-789")
	fmt.Println("  MFA Type:       TOTP (Time-based One-Time Password)")
	fmt.Println("  Expires At:     " + time.Now().Add(5*time.Minute).Format(time.RFC3339))
	fmt.Println()

	// Step 2: Complete MFA verification
	fmt.Println("Step 2: Completing MFA verification...")
	// TODO: Password-based login was removed; MFA flow now relies on WebAuthn/PIV evidence.
	mfaLoginResp, err := client.Security().Auth().Login(ctx, &service.ExtendedLoginRequest{
		Username: "secure-user@example.com",
		MFACode:  "123456", // TOTP code from authenticator app
		TenantID: "default",
	})
	if err != nil {
		log.Printf("  MFA login failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating successful MFA verification...")
		fmt.Println("  MFA Verified:  true")
		fmt.Println("  Access Token:  eyJhbGciOiJSUzI1NiIs... (truncated)")
		fmt.Println("  Session marked as MFA-verified")
		fmt.Println()
		return
	}

	if !mfaLoginResp.MFARequired {
		fmt.Printf("  MFA Verified:  true\n")
		fmt.Printf("  Access Token:  %s... (truncated)\n", truncateToken(mfaLoginResp.AccessToken))
		fmt.Println("  Session marked as MFA-verified")
	}
	fmt.Println()

	// Display supported MFA types
	fmt.Println("Supported MFA Types:")
	fmt.Println("  - TOTP:        Time-based One-Time Password (Google Authenticator, Authy)")
	fmt.Println("  - WebAuthn:    FIDO2/WebAuthn hardware keys (YubiKey, etc.)")
	fmt.Println("  - SMS:         SMS-based verification codes")
	fmt.Println("  - Email:       Email-based verification codes")
	fmt.Println("  - Backup Code: Recovery codes for emergency access")
	fmt.Println()
}

// demonstrateOAuth2Flow shows OIDC/OAuth2 federated authentication.
func demonstrateOAuth2Flow(ctx context.Context, client quicbaas.Client) {
	fmt.Println("=== Part 3: OAuth2/OIDC Federated Authentication ===")
	fmt.Println()

	tenantID := "example-tenant"

	// Step 1: List available identity providers
	fmt.Println("Step 1: Listing configured identity providers...")
	providersResp, err := client.Security().Auth().ListProviders(ctx, tenantID)
	if err != nil {
		log.Printf("  List providers failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating available providers...")
		fmt.Println("  Provider ID: google")
		fmt.Println("    Name:     Google")
		fmt.Println("    Type:     OIDC")
		fmt.Println("    Enabled:  true")
		fmt.Println()
		fmt.Println("  Provider ID: okta")
		fmt.Println("    Name:     Okta Enterprise")
		fmt.Println("    Type:     OIDC")
		fmt.Println("    Enabled:  true")
		fmt.Println()
		fmt.Println("  Provider ID: azure-ad")
		fmt.Println("    Name:     Azure Active Directory")
		fmt.Println("    Type:     OIDC")
		fmt.Println("    Enabled:  true")
		fmt.Println()
	} else {
		fmt.Printf("  Found %d providers:\n", len(providersResp.Providers))
		for _, p := range providersResp.Providers {
			fmt.Printf("  Provider ID: %s\n", p.ID)
			fmt.Printf("    Name:     %s\n", p.Name)
			fmt.Printf("    Type:     %s\n", p.Type.String())
			fmt.Printf("    Enabled:  %v\n", p.Enabled)
			fmt.Println()
		}
	}

	// Step 2: Get details about a specific provider
	fmt.Println("Step 2: Getting provider details...")
	provider, err := client.Security().Auth().GetProvider(ctx, tenantID, "google")
	if err != nil {
		log.Printf("  Get provider failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating Google provider details...")
		fmt.Println("  Provider:       Google")
		fmt.Println("  Type:           OIDC")
		fmt.Println("  Issuer URL:     https://accounts.google.com")
		fmt.Println("  Auth URL:       https://accounts.google.com/o/oauth2/v2/auth")
		fmt.Println("  Token URL:      https://oauth2.googleapis.com/token")
		fmt.Println("  UserInfo URL:   https://openidconnect.googleapis.com/v1/userinfo")
		fmt.Println("  Scopes:         [openid, email, profile]")
		fmt.Println("  Auto Provision: true")
		fmt.Println("  Allow Linking:  true")
		fmt.Println("  Default Roles:  [viewer]")
		fmt.Println()
	} else {
		fmt.Printf("  Provider:       %s\n", provider.Name)
		fmt.Printf("  Type:           %s\n", provider.Type.String())
		fmt.Printf("  Issuer URL:     %s\n", provider.IssuerURL)
		fmt.Printf("  Auth URL:       %s\n", provider.AuthURL)
		fmt.Printf("  Token URL:      %s\n", provider.TokenURL)
		fmt.Printf("  UserInfo URL:   %s\n", provider.UserInfoURL)
		fmt.Printf("  Scopes:         %v\n", provider.Scopes)
		fmt.Printf("  Auto Provision: %v\n", provider.AutoProvision)
		fmt.Printf("  Allow Linking:  %v\n", provider.AllowLinking)
		fmt.Printf("  Default Roles:  %v\n", provider.DefaultRoles)
		fmt.Println()
	}

	// Step 3: Begin OAuth2 login flow
	fmt.Println("Step 3: Initiating OAuth2/OIDC login flow...")
	beginResp, err := client.Security().Auth().BeginFederatedLogin(ctx, &service.BeginFederatedLoginRequest{
		TenantID:    tenantID,
		ProviderID:  "google",
		RedirectURI: "https://myapp.example.com/auth/callback",
		State:       "user-session-state-abc123",
		Scopes:      []string{"openid", "email", "profile"},
		Prompt:      "consent",
		LoginHint:   "user@example.com",
	})
	if err != nil {
		log.Printf("  Begin federated login failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating OAuth2 authorization URL...")
		fmt.Println("  Authorization URL: https://accounts.google.com/o/oauth2/v2/auth?")
		fmt.Println("    client_id=your-client-id")
		fmt.Println("    redirect_uri=https://myapp.example.com/auth/callback")
		fmt.Println("    response_type=code")
		fmt.Println("    scope=openid+email+profile")
		fmt.Println("    state=secure-state-xyz789")
		fmt.Println("    nonce=secure-nonce-abc123")
		fmt.Println()
		fmt.Println("  State: secure-state-xyz789 (save this for callback verification)")
		fmt.Println("  Nonce: secure-nonce-abc123 (save this for ID token verification)")
		fmt.Println()
	} else {
		fmt.Printf("  Authorization URL: %s\n", beginResp.AuthorizationURL)
		fmt.Printf("  State: %s (save this for callback verification)\n", beginResp.State)
		fmt.Printf("  Nonce: %s (save this for ID token verification)\n", beginResp.Nonce)
		fmt.Println()
	}

	// Step 4: Handle callback (simulated)
	fmt.Println("Step 4: Handling OAuth2 callback...")
	fmt.Println("  [Demo] User redirected to IdP and authenticated...")
	fmt.Println("  [Demo] IdP redirects back with authorization code...")
	fmt.Println()

	callbackResp, err := client.Security().Auth().HandleFederatedCallback(ctx, &service.HandleFederatedCallbackRequest{
		TenantID:    tenantID,
		ProviderID:  "google",
		Code:        "authorization-code-from-idp",
		State:       "secure-state-xyz789",
		RedirectURI: "https://myapp.example.com/auth/callback",
	})
	if err != nil {
		log.Printf("  Handle callback failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating successful OAuth2 callback...")
		fmt.Println("  Access Token:   eyJhbGciOiJSUzI1NiIs... (truncated)")
		fmt.Println("  Refresh Token:  eyJhbGciOiJSUzI1NiIs... (truncated)")
		fmt.Println("  Token Type:     Bearer")
		fmt.Println("  User ID:        user-federated-456")
		fmt.Println("  Username:       google_user_12345")
		fmt.Println("  Email:          user@gmail.com")
		fmt.Println("  New User:       false (existing account linked)")
		fmt.Println("  Linked Account: true")
		fmt.Println("  ID Token:       eyJhbGciOiJSUzI1NiIs... (from Google)")
		fmt.Println()
	} else {
		fmt.Printf("  Access Token:   %s... (truncated)\n", truncateToken(callbackResp.AccessToken))
		fmt.Printf("  Refresh Token:  %s... (truncated)\n", truncateToken(callbackResp.RefreshToken))
		fmt.Printf("  Token Type:     %s\n", callbackResp.TokenType)
		fmt.Printf("  User ID:        %s\n", callbackResp.UserID)
		fmt.Printf("  Username:       %s\n", callbackResp.Username)
		fmt.Printf("  Email:          %s\n", callbackResp.Email)
		fmt.Printf("  New User:       %v\n", callbackResp.NewUser)
		fmt.Printf("  Linked Account: %v\n", callbackResp.LinkedAccount)
		fmt.Printf("  ID Token:       %s... (from IdP)\n", truncateToken(callbackResp.IDToken))
		fmt.Println()
	}
}

// demonstrateSessionManagement shows session listing and revocation.
func demonstrateSessionManagement(ctx context.Context, client quicbaas.Client) {
	fmt.Println("=== Part 4: Session Management ===")
	fmt.Println()

	// Step 1: List all active sessions
	fmt.Println("Step 1: Listing all active sessions...")
	sessionsResp, err := client.Security().Auth().ListSessions(ctx)
	if err != nil {
		log.Printf("  List sessions failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating active sessions...")
		fmt.Println()
		fmt.Println("  Session 1:")
		fmt.Println("    ID:          session-001")
		fmt.Println("    Created:     " + time.Now().Add(-24*time.Hour).Format(time.RFC3339))
		fmt.Println("    Last Active: " + time.Now().Add(-5*time.Minute).Format(time.RFC3339))
		fmt.Println("    Expires:     " + time.Now().Add(6*time.Hour).Format(time.RFC3339))
		fmt.Println("    IP Address:  192.0.2.100")
		fmt.Println("    User Agent:  Mozilla/5.0 (Macintosh; Intel Mac OS X)")
		fmt.Println("    Device:      MacBook Pro")
		fmt.Println("    Auth Method: password")
		fmt.Println("    MFA Verified: true")
		fmt.Println()
		fmt.Println("  Session 2:")
		fmt.Println("    ID:          session-002")
		fmt.Println("    Created:     " + time.Now().Add(-2*time.Hour).Format(time.RFC3339))
		fmt.Println("    Last Active: " + time.Now().Add(-30*time.Minute).Format(time.RFC3339))
		fmt.Println("    Expires:     " + time.Now().Add(22*time.Hour).Format(time.RFC3339))
		fmt.Println("    IP Address:  10.0.0.50")
		fmt.Println("    User Agent:  QuicBaaS CLI/1.0")
		fmt.Println("    Device:      CLI Tool")
		fmt.Println("    Auth Method: api_key")
		fmt.Println("    MFA Verified: false")
		fmt.Println()
	} else {
		fmt.Printf("  Found %d active sessions:\n", len(sessionsResp.Sessions))
		for i, s := range sessionsResp.Sessions {
			fmt.Printf("\n  Session %d:\n", i+1)
			fmt.Printf("    ID:          %s\n", s.ID)
			fmt.Printf("    Created:     %s\n", s.CreatedAt.Format(time.RFC3339))
			fmt.Printf("    Last Active: %s\n", s.LastActive.Format(time.RFC3339))
			fmt.Printf("    Expires:     %s\n", s.ExpiresAt.Format(time.RFC3339))
			fmt.Printf("    IP Address:  %s\n", s.IPAddress)
			fmt.Printf("    User Agent:  %s\n", s.UserAgent)
			fmt.Printf("    Device:      %s\n", s.DeviceInfo)
			fmt.Printf("    Auth Method: %s\n", s.AuthMethod)
			fmt.Printf("    MFA Verified: %v\n", s.MFAVerified)
		}
		fmt.Println()
	}

	// Step 2: Get details of a specific session
	fmt.Println("Step 2: Getting specific session details...")
	session, err := client.Security().Auth().GetSession(ctx, "session-001")
	if err != nil {
		log.Printf("  Get session failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Session details retrieved successfully")
		fmt.Println()
	} else {
		fmt.Printf("  Session ID:    %s\n", session.ID)
		fmt.Printf("  User ID:       %s\n", session.UserID)
		fmt.Printf("  Tenant ID:     %s\n", session.TenantID)
		fmt.Printf("  MFA Verified:  %v\n", session.MFAVerified)
		fmt.Println()
	}

	// Step 3: Revoke a specific session
	fmt.Println("Step 3: Revoking a specific session (session-002)...")
	err = client.Security().Auth().RevokeSession(ctx, "session-002")
	if err != nil {
		log.Printf("  Revoke session failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating session revocation...")
	}
	fmt.Println("  Session session-002 has been revoked")
	fmt.Println("  Any tokens associated with this session are now invalid")
	fmt.Println()

	// Step 4: Revoke all sessions (security measure)
	fmt.Println("Step 4: Revoking all sessions (security measure)...")
	fmt.Println("  WARNING: This will log out all devices!")
	fmt.Println("  [Demo] Skipping actual revocation in this example...")
	fmt.Println()
	fmt.Println("  To revoke all sessions, call:")
	fmt.Println("    err := client.Security().Auth().RevokeAllSessions(ctx)")
	fmt.Println()
}

// demonstrateTokenIntrospection shows service-to-service token validation.
func demonstrateTokenIntrospection(ctx context.Context, client quicbaas.Client) {
	fmt.Println("=== Part 5: Token Introspection (Service-to-Service) ===")
	fmt.Println()

	fmt.Println("Token introspection is used by resource servers to validate tokens.")
	fmt.Println("This is typically used in microservice architectures.")
	fmt.Println()

	// Introspect a token
	fmt.Println("Step 1: Introspecting an access token...")
	introspection, err := client.Security().Auth().IntrospectToken(ctx, "demo-access-token")
	if err != nil {
		log.Printf("  Token introspection failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating token introspection response...")
		fmt.Println("  Active:     true")
		fmt.Println("  Scope:      quicbaas:read quicbaas:write")
		fmt.Println("  Client ID:  my-application")
		fmt.Println("  Username:   admin@example.com")
		fmt.Println("  Token Type: Bearer")
		fmt.Println("  Issued At:  " + time.Now().Add(-1*time.Hour).Format(time.RFC3339))
		fmt.Println("  Expires At: " + time.Now().Add(2*time.Hour).Format(time.RFC3339))
		fmt.Println("  Not Before: " + time.Now().Add(-1*time.Hour).Format(time.RFC3339))
		fmt.Println("  Subject:    user-123")
		fmt.Println("  Audience:   [quicbaas-api]")
		fmt.Println("  Issuer:     https://quicbaas.example.com")
		fmt.Println("  JWT ID:     jti-unique-identifier")
		fmt.Println()
	} else {
		fmt.Printf("  Active:     %v\n", introspection.Active)
		fmt.Printf("  Scope:      %s\n", introspection.Scope)
		fmt.Printf("  Client ID:  %s\n", introspection.ClientID)
		fmt.Printf("  Username:   %s\n", introspection.Username)
		fmt.Printf("  Token Type: %s\n", introspection.TokenType)
		fmt.Printf("  Issued At:  %s\n", introspection.IssuedAt.Format(time.RFC3339))
		fmt.Printf("  Expires At: %s\n", introspection.ExpiresAt.Format(time.RFC3339))
		fmt.Printf("  Not Before: %s\n", introspection.NotBefore.Format(time.RFC3339))
		fmt.Printf("  Subject:    %s\n", introspection.Subject)
		fmt.Printf("  Audience:   %v\n", introspection.Audience)
		fmt.Printf("  Issuer:     %s\n", introspection.Issuer)
		fmt.Printf("  JWT ID:     %s\n", introspection.JTI)
		fmt.Println()
	}

	fmt.Println("Use cases for token introspection:")
	fmt.Println("  - Validate tokens received from external clients")
	fmt.Println("  - Check if a token has been revoked")
	fmt.Println("  - Get detailed claims without parsing the JWT locally")
	fmt.Println("  - Centralized token validation for microservices")
	fmt.Println()
}

// demonstrateOIDCDiscovery shows OIDC discovery and JWKS retrieval.
func demonstrateOIDCDiscovery(ctx context.Context, client quicbaas.Client) {
	fmt.Println("=== Part 6: OIDC Discovery and JWKS ===")
	fmt.Println()

	// Step 1: Get OIDC discovery document
	fmt.Println("Step 1: Fetching OIDC discovery document...")
	config, err := client.Security().Auth().GetOpenIDConfiguration(ctx)
	if err != nil {
		log.Printf("  Get OIDC config failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating OIDC discovery document...")
		fmt.Println("  Issuer:                    https://quicbaas.example.com")
		fmt.Println("  Authorization Endpoint:    https://quicbaas.example.com/oauth2/authorize")
		fmt.Println("  Token Endpoint:            https://quicbaas.example.com/oauth2/token")
		fmt.Println("  UserInfo Endpoint:         https://quicbaas.example.com/oauth2/userinfo")
		fmt.Println("  JWKS URI:                  https://quicbaas.example.com/.well-known/jwks.json")
		fmt.Println("  Revocation Endpoint:       https://quicbaas.example.com/oauth2/revoke")
		fmt.Println("  Introspection Endpoint:    https://quicbaas.example.com/oauth2/introspect")
		fmt.Println("  End Session Endpoint:      https://quicbaas.example.com/oauth2/logout")
		fmt.Println()
		fmt.Println("  Scopes Supported:          [openid, profile, email, offline_access]")
		fmt.Println("  Response Types Supported:  [code, token, id_token]")
		fmt.Println("  Grant Types Supported:     [authorization_code, refresh_token, client_credentials]")
		fmt.Println("  Token Auth Methods:        [client_secret_basic, client_secret_post]")
		fmt.Println("  ID Token Signing Algs:     [RS256, RS384, RS512, ES256, ES384]")
		fmt.Println("  PKCE Methods Supported:    [S256, plain]")
		fmt.Println()
		fmt.Println("  MFA Supported:             true")
		fmt.Println("  MFA Methods:               [TOTP, WebAuthn, SMS, Email]")
		fmt.Println()
	} else {
		fmt.Printf("  Issuer:                    %s\n", config.Issuer)
		fmt.Printf("  Authorization Endpoint:    %s\n", config.AuthorizationEndpoint)
		fmt.Printf("  Token Endpoint:            %s\n", config.TokenEndpoint)
		fmt.Printf("  UserInfo Endpoint:         %s\n", config.UserInfoEndpoint)
		fmt.Printf("  JWKS URI:                  %s\n", config.JWKSURI)
		fmt.Printf("  Revocation Endpoint:       %s\n", config.RevocationEndpoint)
		fmt.Printf("  Introspection Endpoint:    %s\n", config.IntrospectionEndpoint)
		fmt.Printf("  End Session Endpoint:      %s\n", config.EndSessionEndpoint)
		fmt.Println()
		fmt.Printf("  Scopes Supported:          %v\n", config.ScopesSupported)
		fmt.Printf("  Response Types Supported:  %v\n", config.ResponseTypesSupported)
		fmt.Printf("  Grant Types Supported:     %v\n", config.GrantTypesSupported)
		fmt.Printf("  Token Auth Methods:        %v\n", config.TokenEndpointAuthMethodsSupported)
		fmt.Printf("  ID Token Signing Algs:     %v\n", config.IDTokenSigningAlgValuesSupported)
		fmt.Printf("  PKCE Methods Supported:    %v\n", config.CodeChallengeMethodsSupported)
		fmt.Println()
		fmt.Printf("  MFA Supported:             %v\n", config.MFASupported)
		fmt.Printf("  MFA Methods:               %v\n", config.MFAMethodsSupported)
		fmt.Println()
	}

	// Step 2: Get JWKS (JSON Web Key Set)
	fmt.Println("Step 2: Fetching JWKS (JSON Web Key Set)...")
	jwks, err := client.Security().Auth().GetJWKS(ctx)
	if err != nil {
		log.Printf("  Get JWKS failed (expected in demo): %v", err)
		fmt.Println("  [Demo] Simulating JWKS response...")
		fmt.Println()
		fmt.Println("  Key 1:")
		fmt.Println("    Key ID (kid):     key-rsa-001")
		fmt.Println("    Key Type (kty):   RSA")
		fmt.Println("    Algorithm (alg):  RS256")
		fmt.Println("    Use:              sig (signature)")
		fmt.Println("    Modulus (n):      3YKl... (truncated)")
		fmt.Println("    Exponent (e):     AQAB")
		fmt.Println()
		fmt.Println("  Key 2:")
		fmt.Println("    Key ID (kid):     key-ec-001")
		fmt.Println("    Key Type (kty):   EC")
		fmt.Println("    Algorithm (alg):  ES256")
		fmt.Println("    Use:              sig (signature)")
		fmt.Println("    Curve (crv):      P-256")
		fmt.Println("    X Coordinate:     f83O... (truncated)")
		fmt.Println("    Y Coordinate:     x_FE... (truncated)")
		fmt.Println()
	} else {
		fmt.Printf("  Found %d keys:\n", len(jwks.Keys))
		for i, key := range jwks.Keys {
			fmt.Printf("\n  Key %d:\n", i+1)
			fmt.Printf("    Key ID (kid):     %s\n", key.KID)
			fmt.Printf("    Key Type (kty):   %s\n", key.KTY)
			fmt.Printf("    Algorithm (alg):  %s\n", key.ALG)
			fmt.Printf("    Use:              %s\n", key.Use)
			switch key.KTY {
			case "RSA":
				fmt.Printf("    Modulus (n):      %s... (truncated)\n", truncateString(key.N, 10))
				fmt.Printf("    Exponent (e):     %s\n", key.E)
			case "EC":
				fmt.Printf("    Curve (crv):      %s\n", key.CRV)
				fmt.Printf("    X Coordinate:     %s... (truncated)\n", truncateString(key.X, 10))
				fmt.Printf("    Y Coordinate:     %s... (truncated)\n", truncateString(key.Y, 10))
			}
		}
		fmt.Println()
	}

	fmt.Println("JWKS is used for:")
	fmt.Println("  - Local JWT signature verification without calling the server")
	fmt.Println("  - Caching public keys for performance")
	fmt.Println("  - Key rotation support (multiple active keys)")
	fmt.Println()
}

// printKeyTakeaways prints the summary of key concepts.
func printKeyTakeaways() {
	fmt.Println("=== Example Complete ===")
	fmt.Println()
	fmt.Println("Key Takeaways:")
	fmt.Println()
	fmt.Println("  1. JWT Authentication:")
	fmt.Println("     - Login returns access and refresh tokens")
	fmt.Println("     - Access tokens are short-lived (typically 1 hour)")
	fmt.Println("     - Refresh tokens enable seamless token renewal")
	fmt.Println("     - Always logout to properly terminate sessions")
	fmt.Println()
	fmt.Println("  2. Multi-Factor Authentication:")
	fmt.Println("     - MFA adds an additional security layer")
	fmt.Println("     - Supports TOTP, WebAuthn, SMS, Email, and Backup Codes")
	fmt.Println("     - MFA challenge must be completed within time limit")
	fmt.Println("     - Sessions are marked as MFA-verified")
	fmt.Println()
	fmt.Println("  3. OAuth2/OIDC Federation:")
	fmt.Println("     - Integrate with external identity providers")
	fmt.Println("     - Supports Google, Okta, Azure AD, and custom OIDC providers")
	fmt.Println("     - Auto-provisioning creates users on first login")
	fmt.Println("     - Account linking associates IdP accounts with existing users")
	fmt.Println()
	fmt.Println("  4. Session Management:")
	fmt.Println("     - View all active sessions across devices")
	fmt.Println("     - Revoke individual sessions for compromised devices")
	fmt.Println("     - RevokeAllSessions for emergency security response")
	fmt.Println("     - Session metadata includes IP, device, and auth method")
	fmt.Println()
	fmt.Println("  5. Token Introspection:")
	fmt.Println("     - Centralized token validation for microservices")
	fmt.Println("     - Check token status, scope, and claims")
	fmt.Println("     - Detect revoked tokens that are still within expiry")
	fmt.Println()
	fmt.Println("  6. OIDC Discovery:")
	fmt.Println("     - Auto-configure clients using discovery document")
	fmt.Println("     - JWKS enables local token verification")
	fmt.Println("     - Supports key rotation and multiple algorithms")
	fmt.Println()
	fmt.Println("  7. Transport Options:")
	fmt.Println("     - gRPC: High performance, binary protocol (default)")
	fmt.Println("     - REST: HTTP/JSON for broad compatibility")
	fmt.Println("     - QUIC: Low latency, multiplexed connections")
	fmt.Println()
}

// truncateToken safely truncates a token for display.
func truncateToken(token string) string {
	if len(token) <= 20 {
		return token
	}
	return token[:20]
}

// truncateString safely truncates a string for display.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
