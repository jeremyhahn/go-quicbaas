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

package cli

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// =============================================================================
// Auth Command Definitions
// =============================================================================

// authTransportsToSkip defines which transports to skip for auth operations.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
// QUIC does not have auth service implementation.
var authTransportsToSkip = []Transport{TransportQUIC}

// getTestUsername returns the username for auth tests.
// Uses environment variable if set, otherwise defaults to "admin".
func getTestUsername() string {
	if username := os.Getenv("QUICBAAS_TEST_USERNAME"); username != "" {
		return username
	}
	return "admin"
}

// getTestPassword returns the password for auth tests.
// Uses environment variable if set, otherwise defaults to "admin".
func getTestPassword() string {
	if password := os.Getenv("QUICBAAS_TEST_PASSWORD"); password != "" {
		return password
	}
	return "admin"
}

// AuthLoginCommand returns a command to authenticate with username and password.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLoginCommand(username, password string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("auth login %s", username),
		Args:           []string{"auth", "login", username, "--password", password},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Authentication successful", "Token stored"},
		Timeout:        30 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthLoginNoStoreCommand returns a command to authenticate without storing the token.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLoginNoStoreCommand(username, password string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("auth login %s --no-store", username),
		Args:           []string{"auth", "login", username, "--password", password, "--no-store"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"access_token", "refresh_token", "expires_at"},
		Timeout:        30 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthLoginCustomTokenFileCommand returns a command to authenticate with a custom token file.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLoginCustomTokenFileCommand(username, password, tokenFile string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("auth login %s --token-file", username),
		Args:           []string{"auth", "login", username, "--password", password, "--token-file", tokenFile},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Authentication successful", "Token stored", tokenFile},
		Timeout:        30 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthLoginInvalidPasswordCommand returns a command to test login with invalid password.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLoginInvalidPasswordCommand(username string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("auth login %s invalid password", username),
		Args:           []string{"auth", "login", username, "--password", "invalid-password-12345"},
		ExpectSuccess:  false,
		Timeout:        30 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthLoginInvalidUserCommand returns a command to test login with non-existent user.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLoginInvalidUserCommand() CommandDef {
	return CommandDef{
		Name:           "auth login invalid user",
		Args:           []string{"auth", "login", "nonexistent-user-12345", "--password", "any-password"},
		ExpectSuccess:  false,
		Timeout:        30 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthLoginEmptyPasswordCommand returns a command to test login with empty password.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLoginEmptyPasswordCommand(username string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("auth login %s empty password", username),
		Args:           []string{"auth", "login", username, "--password", ""},
		ExpectSuccess:  false,
		ExpectError:    []string{"password"},
		Timeout:        10 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthLogoutCommand returns a command to logout and invalidate the current token.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLogoutCommand() CommandDef {
	return CommandDef{
		Name:           "auth logout",
		Args:           []string{"auth", "logout"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Logged out"},
		Timeout:        10 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthLogoutCustomTokenFileCommand returns a command to logout with a custom token file.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLogoutCustomTokenFileCommand(tokenFile string) CommandDef {
	return CommandDef{
		Name:           "auth logout --token-file",
		Args:           []string{"auth", "logout", "--token-file", tokenFile},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Logged out"},
		Timeout:        10 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthLogoutNoSessionCommand returns a command to test logout when not logged in.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthLogoutNoSessionCommand() CommandDef {
	return CommandDef{
		Name:           "auth logout no session",
		Args:           []string{"auth", "logout"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"No active session"},
		Timeout:        10 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthStatusCommand returns a command to check authentication status.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthStatusCommand() CommandDef {
	return CommandDef{
		Name:           "auth status",
		Args:           []string{"auth", "status"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Authentication Status"},
		Timeout:        10 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthStatusJSONCommand returns a command to check authentication status in JSON format.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthStatusJSONCommand() CommandDef {
	return CommandDef{
		Name:           "auth status --output json",
		Args:           []string{"auth", "status", "--output", "json"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"authenticated"},
		Timeout:        10 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthStatusCustomTokenFileCommand returns a command to check status with a custom token file.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthStatusCustomTokenFileCommand(tokenFile string) CommandDef {
	return CommandDef{
		Name:           "auth status --token-file",
		Args:           []string{"auth", "status", "--token-file", tokenFile},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthStatusNotAuthenticatedCommand returns a command to check status when not authenticated.
// Auth operations are supported via gRPC, REST, Unix, and MCP transports.
func AuthStatusNotAuthenticatedCommand() CommandDef {
	return CommandDef{
		Name:           "auth status not authenticated",
		Args:           []string{"auth", "status"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Not authenticated"},
		Timeout:        10 * time.Second,
		SkipTransports: authTransportsToSkip,
	}
}

// AuthHelpCommand returns a command to show auth help.
func AuthHelpCommand() CommandDef {
	return CommandDef{
		Name:          "auth help",
		Args:          []string{"auth", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"login", "logout", "status"},
		Timeout:       10 * time.Second,
	}
}

// AuthLoginHelpCommand returns a command to show auth login help.
func AuthLoginHelpCommand() CommandDef {
	return CommandDef{
		Name:          "auth login help",
		Args:          []string{"auth", "login", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"username", "password", "token-file", "no-store"},
		Timeout:       10 * time.Second,
	}
}

// AuthLogoutHelpCommand returns a command to show auth logout help.
func AuthLogoutHelpCommand() CommandDef {
	return CommandDef{
		Name:          "auth logout help",
		Args:          []string{"auth", "logout", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"token-file"},
		Timeout:       10 * time.Second,
	}
}

// AuthStatusHelpCommand returns a command to show auth status help.
func AuthStatusHelpCommand() CommandDef {
	return CommandDef{
		Name:          "auth status help",
		Args:          []string{"auth", "status", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"token-file", "output"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Auth Test Suites
// =============================================================================

// AuthHelpSuite returns help commands for auth.
func AuthHelpSuite() []CommandDef {
	return []CommandDef{
		AuthHelpCommand(),
		AuthLoginHelpCommand(),
		AuthLogoutHelpCommand(),
		AuthStatusHelpCommand(),
	}
}

// AuthErrorSuite returns error case commands for auth.
func AuthErrorSuite() []CommandDef {
	return []CommandDef{
		AuthLoginInvalidUserCommand(),
		AuthLoginInvalidPasswordCommand(getTestUsername()),
	}
}

// =============================================================================
// Auth Lifecycle Tests
// =============================================================================

// TestAuthHelp tests the auth help commands.
func TestAuthHelp(t *testing.T) {
	RunForAllTransports(t, "AuthHelp", func(t *testing.T, runner *TestRunner) {
		suite := AuthHelpSuite()
		runner.RunSuite(suite)
	})
}

// TestAuthLoginLogoutLifecycle tests the complete auth login/logout lifecycle.
func TestAuthLoginLogoutLifecycle(t *testing.T) {
	RunForAllTransports(t, "AuthLoginLogoutLifecycle", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()

		// Login
		loginCmd := AuthLoginCommand(username, password)
		runner.RunAndAssert(loginCmd)

		// Check status after login
		statusCmd := AuthStatusCommand()
		runner.RunAndAssert(statusCmd)

		// Logout
		logoutCmd := AuthLogoutCommand()
		runner.RunAndAssert(logoutCmd)
	})
}

// TestAuthLogin tests authentication with username and password.
func TestAuthLogin(t *testing.T) {
	RunForAllTransports(t, "AuthLogin", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()

		// Login
		loginCmd := AuthLoginCommand(username, password)
		runner.RunAndAssert(loginCmd)

		// Cleanup - logout
		logoutCmd := AuthLogoutCommand()
		runner.Run(logoutCmd)
	})
}

// TestAuthLoginNoStore tests authentication without storing the token.
func TestAuthLoginNoStore(t *testing.T) {
	RunForAllTransports(t, "AuthLoginNoStore", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()

		// Login without storing token
		loginCmd := AuthLoginNoStoreCommand(username, password)
		runner.RunAndAssert(loginCmd)

		// No cleanup needed since token was not stored
	})
}

// TestAuthLoginCustomTokenFile tests authentication with a custom token file.
func TestAuthLoginCustomTokenFile(t *testing.T) {
	RunForAllTransports(t, "AuthLoginCustomTokenFile", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()
		tokenFile := fmt.Sprintf("/tmp/quicbaas-test-token-%d", time.Now().UnixNano())

		// Login with custom token file
		loginCmd := AuthLoginCustomTokenFileCommand(username, password, tokenFile)
		runner.RunAndAssert(loginCmd)

		// Cleanup - logout with custom token file
		logoutCmd := AuthLogoutCustomTokenFileCommand(tokenFile)
		runner.Run(logoutCmd)

		// Remove the token file
		os.Remove(tokenFile)
	})
}

// TestAuthLoginInvalidPassword tests login with invalid password.
func TestAuthLoginInvalidPassword(t *testing.T) {
	RunForAllTransports(t, "AuthLoginInvalidPassword", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()

		// Try to login with invalid password
		loginCmd := AuthLoginInvalidPasswordCommand(username)
		runner.RunAndAssert(loginCmd)
	})
}

// TestAuthLoginInvalidUser tests login with non-existent user.
func TestAuthLoginInvalidUser(t *testing.T) {
	RunForAllTransports(t, "AuthLoginInvalidUser", func(t *testing.T, runner *TestRunner) {
		// Try to login with invalid user
		loginCmd := AuthLoginInvalidUserCommand()
		runner.RunAndAssert(loginCmd)
	})
}

// TestAuthLogout tests logout functionality.
func TestAuthLogout(t *testing.T) {
	RunForAllTransports(t, "AuthLogout", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()

		// Login first
		loginCmd := AuthLoginCommand(username, password)
		runner.RunAndAssert(loginCmd)

		// Logout
		logoutCmd := AuthLogoutCommand()
		runner.RunAndAssert(logoutCmd)
	})
}

// TestAuthLogoutNoSession tests logout when not logged in.
func TestAuthLogoutNoSession(t *testing.T) {
	RunForAllTransports(t, "AuthLogoutNoSession", func(t *testing.T, runner *TestRunner) {
		// Ensure we're logged out first by using a non-existent token file
		tokenFile := fmt.Sprintf("/tmp/quicbaas-nonexistent-token-%d", time.Now().UnixNano())

		// Try to logout with non-existent token file
		// This should show "No active session" message
		logoutCmd := CommandDef{
			Name:           "auth logout no session",
			Args:           []string{"auth", "logout", "--token-file", tokenFile},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"No active session"},
			Timeout:        10 * time.Second,
			SkipTransports: authTransportsToSkip,
		}
		runner.RunAndAssert(logoutCmd)
	})
}

// TestAuthStatus tests authentication status checking.
func TestAuthStatus(t *testing.T) {
	RunForAllTransports(t, "AuthStatus", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()

		// Login first
		loginCmd := AuthLoginCommand(username, password)
		runner.RunAndAssert(loginCmd)

		// Check status
		statusCmd := AuthStatusCommand()
		runner.RunAndAssert(statusCmd)

		// Cleanup - logout
		logoutCmd := AuthLogoutCommand()
		runner.Run(logoutCmd)
	})
}

// TestAuthStatusJSON tests authentication status in JSON format.
func TestAuthStatusJSON(t *testing.T) {
	RunForAllTransports(t, "AuthStatusJSON", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()

		// Login first
		loginCmd := AuthLoginCommand(username, password)
		runner.RunAndAssert(loginCmd)

		// Check status in JSON format
		statusCmd := AuthStatusJSONCommand()
		runner.RunAndAssert(statusCmd)

		// Cleanup - logout
		logoutCmd := AuthLogoutCommand()
		runner.Run(logoutCmd)
	})
}

// TestAuthStatusNotAuthenticated tests status when not authenticated.
func TestAuthStatusNotAuthenticated(t *testing.T) {
	RunForAllTransports(t, "AuthStatusNotAuthenticated", func(t *testing.T, runner *TestRunner) {
		// Use a non-existent token file to ensure not authenticated
		tokenFile := fmt.Sprintf("/tmp/quicbaas-nonexistent-token-%d", time.Now().UnixNano())

		// Check status with non-existent token file
		statusCmd := CommandDef{
			Name:           "auth status not authenticated",
			Args:           []string{"auth", "status", "--token-file", tokenFile},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"Not authenticated"},
			Timeout:        10 * time.Second,
			SkipTransports: authTransportsToSkip,
		}
		runner.RunAndAssert(statusCmd)
	})
}

// TestAuthStatusCustomTokenFile tests status with a custom token file.
func TestAuthStatusCustomTokenFile(t *testing.T) {
	RunForAllTransports(t, "AuthStatusCustomTokenFile", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()
		tokenFile := fmt.Sprintf("/tmp/quicbaas-test-token-%d", time.Now().UnixNano())

		// Login with custom token file
		loginCmd := AuthLoginCustomTokenFileCommand(username, password, tokenFile)
		runner.RunAndAssert(loginCmd)

		// Check status with custom token file
		statusCmd := AuthStatusCustomTokenFileCommand(tokenFile)
		runner.RunAndAssert(statusCmd)

		// Cleanup - logout with custom token file
		logoutCmd := AuthLogoutCustomTokenFileCommand(tokenFile)
		runner.Run(logoutCmd)

		// Remove the token file
		os.Remove(tokenFile)
	})
}

// TestAuthErrorCases tests various auth error scenarios.
func TestAuthErrorCases(t *testing.T) {
	RunForAllTransports(t, "AuthErrorCases", func(t *testing.T, runner *TestRunner) {
		suite := AuthErrorSuite()
		runner.RunSuite(suite)
	})
}

// TestAuthMultipleLogins tests that multiple logins update the token.
func TestAuthMultipleLogins(t *testing.T) {
	RunForAllTransports(t, "AuthMultipleLogins", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()

		// First login
		loginCmd1 := AuthLoginCommand(username, password)
		runner.RunAndAssert(loginCmd1)

		// Check status
		statusCmd1 := AuthStatusCommand()
		runner.RunAndAssert(statusCmd1)

		// Second login (should update the token)
		loginCmd2 := AuthLoginCommand(username, password)
		runner.RunAndAssert(loginCmd2)

		// Check status again
		statusCmd2 := AuthStatusCommand()
		runner.RunAndAssert(statusCmd2)

		// Cleanup - logout
		logoutCmd := AuthLogoutCommand()
		runner.Run(logoutCmd)
	})
}

// TestAuthLoginLogoutMultipleTimes tests multiple login/logout cycles.
func TestAuthLoginLogoutMultipleTimes(t *testing.T) {
	RunForAllTransports(t, "AuthLoginLogoutMultipleTimes", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()

		// Perform multiple login/logout cycles
		for i := 0; i < 3; i++ {
			// Login
			loginCmd := AuthLoginCommand(username, password)
			runner.RunAndAssert(loginCmd)

			// Verify authenticated
			statusCmd := AuthStatusCommand()
			runner.RunAndAssert(statusCmd)

			// Logout
			logoutCmd := AuthLogoutCommand()
			runner.RunAndAssert(logoutCmd)
		}
	})
}

// TestAuthTokenFileIsolation tests that different token files are isolated.
func TestAuthTokenFileIsolation(t *testing.T) {
	RunForAllTransports(t, "AuthTokenFileIsolation", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()
		tokenFile1 := fmt.Sprintf("/tmp/quicbaas-test-token1-%d", time.Now().UnixNano())
		tokenFile2 := fmt.Sprintf("/tmp/quicbaas-test-token2-%d", time.Now().UnixNano())

		// Login with first token file
		loginCmd1 := AuthLoginCustomTokenFileCommand(username, password, tokenFile1)
		runner.RunAndAssert(loginCmd1)

		// Login with second token file
		loginCmd2 := AuthLoginCustomTokenFileCommand(username, password, tokenFile2)
		runner.RunAndAssert(loginCmd2)

		// Check status with first token file
		statusCmd1 := AuthStatusCustomTokenFileCommand(tokenFile1)
		runner.RunAndAssert(statusCmd1)

		// Check status with second token file
		statusCmd2 := AuthStatusCustomTokenFileCommand(tokenFile2)
		runner.RunAndAssert(statusCmd2)

		// Logout from first token file
		logoutCmd1 := AuthLogoutCustomTokenFileCommand(tokenFile1)
		runner.RunAndAssert(logoutCmd1)

		// Second token file should still be valid
		statusCmd3 := AuthStatusCustomTokenFileCommand(tokenFile2)
		runner.RunAndAssert(statusCmd3)

		// Cleanup
		logoutCmd2 := AuthLogoutCustomTokenFileCommand(tokenFile2)
		runner.Run(logoutCmd2)

		os.Remove(tokenFile1)
		os.Remove(tokenFile2)
	})
}

// TestAuthStatusAfterLogout tests status after logout.
func TestAuthStatusAfterLogout(t *testing.T) {
	RunForAllTransports(t, "AuthStatusAfterLogout", func(t *testing.T, runner *TestRunner) {
		username := getTestUsername()
		password := getTestPassword()
		tokenFile := fmt.Sprintf("/tmp/quicbaas-test-token-%d", time.Now().UnixNano())

		// Login with custom token file
		loginCmd := AuthLoginCustomTokenFileCommand(username, password, tokenFile)
		runner.RunAndAssert(loginCmd)

		// Verify authenticated
		statusCmd1 := AuthStatusCustomTokenFileCommand(tokenFile)
		runner.RunAndAssert(statusCmd1)

		// Logout
		logoutCmd := AuthLogoutCustomTokenFileCommand(tokenFile)
		runner.RunAndAssert(logoutCmd)

		// Check status - should show not authenticated
		statusCmd2 := CommandDef{
			Name:           "auth status after logout",
			Args:           []string{"auth", "status", "--token-file", tokenFile},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"Not authenticated"},
			Timeout:        10 * time.Second,
			SkipTransports: authTransportsToSkip,
		}
		runner.RunAndAssert(statusCmd2)

		// Cleanup
		os.Remove(tokenFile)
	})
}

// TestAuthLoginMissingUsername tests login without username argument.
func TestAuthLoginMissingUsername(t *testing.T) {
	RunForAllTransports(t, "AuthLoginMissingUsername", func(t *testing.T, runner *TestRunner) {
		// Try to login without username
		loginCmd := CommandDef{
			Name:          "auth login missing username",
			Args:          []string{"auth", "login"},
			ExpectSuccess: false,
			Timeout:       10 * time.Second,
		}
		runner.RunAndAssert(loginCmd)
	})
}
