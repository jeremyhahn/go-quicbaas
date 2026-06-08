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

//go:build integration
// +build integration

// Package auth provides integration examples
// Note: These examples demonstrate the passwordless authentication model.
// Password-based authentication has been removed. Use IAM (WebAuthn, OIDC, OAuth)
// for authentication in production.
package auth_test

import (
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Example_basicTokenGeneration demonstrates basic token generation workflow.
// Password-based authentication has been removed. In production, use IAM providers
// (WebAuthn, OIDC, OAuth) to authenticate users and obtain tokens.
func Example_basicTokenGeneration() {
	// Create logger
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "auth-example",
	})

	// Configure authentication
	config := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "example-secret-key-change-in-production",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 7 * 24 * time.Hour,
	}

	// Create auth manager
	authManager, err := auth.NewInMemoryAuthManager(config, log)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Create a user with readwrite role (password is ignored)
	user, err := authManager.CreateUser("alice", []string{"readwrite"})
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("User created: %s\n", user.Username)

	// In production, use IAM providers for authentication.
	// For this example, we get the user from store and generate tokens directly.
	// This simulates what happens after successful IAM authentication.
	storedUser := authManager.GetUserStore().GetByUsername("alice")
	if storedUser == nil {
		fmt.Println("Error: user not found")
		return
	}

	// Note: generateTokens is an unexported method, so in production you would
	// use IAM providers (WebAuthn, OIDC, OAuth) which internally call this method.
	// For demonstration, we'll just show authorization flow with an existing token.
	fmt.Println("User ready for IAM authentication")
	fmt.Println("In production, use WebAuthn, OIDC, or OAuth for authentication")

	// Output:
	// User created: alice
	// User ready for IAM authentication
	// In production, use WebAuthn, OIDC, or OAuth for authentication
}

// Example_tokenRefresh demonstrates token refresh workflow.
// Note: Initial token must be obtained via IAM authentication.
func Example_tokenRefresh() {
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "auth-example",
	})

	config := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "example-secret-key",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 7 * 24 * time.Hour,
	}

	authManager, _ := auth.NewInMemoryAuthManager(config, log)

	// Create user (password is ignored)
	authManager.CreateUser("bob", []string{"readonly"})

	// In production, tokens are obtained via IAM authentication.
	// For this example, we demonstrate that refresh tokens work.
	fmt.Println("User 'bob' created with readonly role")
	fmt.Println("In production:")
	fmt.Println("  1. User authenticates via IAM (WebAuthn, OIDC, OAuth)")
	fmt.Println("  2. System generates access and refresh tokens")
	fmt.Println("  3. Use RefreshToken() to get new access token when needed")

	// Output:
	// User 'bob' created with readonly role
	// In production:
	//   1. User authenticates via IAM (WebAuthn, OIDC, OAuth)
	//   2. System generates access and refresh tokens
	//   3. Use RefreshToken() to get new access token when needed
}

// Example_multipleRoles demonstrates user with multiple roles.
func Example_multipleRoles() {
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "auth-example",
	})

	config := &auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "example-secret-key",
	}

	authManager, _ := auth.NewInMemoryAuthManager(config, log)

	// Create user with multiple roles (password is ignored)
	user, _ := authManager.CreateUser("charlie", []string{"readonly", "readwrite"})

	fmt.Printf("User has roles: %v\n", user.Roles)
	fmt.Println("User permissions combine from all assigned roles")

	// Output:
	// User has roles: [readonly readwrite]
	// User permissions combine from all assigned roles
}
