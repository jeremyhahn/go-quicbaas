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

// Package main provides authentication CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

var (
	// Auth command flags.
	authPassword     string
	authTokenFile    string
	authNoStore      bool
	authOutputFormat string
)

// newAuthCommand creates the auth command group.
func newAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication commands (login, logout)",
		Long: `Authentication commands for managing QuicBaaS sessions.

These commands allow you to authenticate with a QuicBaaS server and manage
authentication tokens. Tokens are securely stored in your home directory by default.`,
	}

	// Add subcommands
	cmd.AddCommand(newAuthLoginCommand())
	cmd.AddCommand(newAuthLogoutCommand())
	cmd.AddCommand(newAuthStatusCommand())

	return cmd
}

// newAuthLoginCommand creates the login command.
func newAuthLoginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login <username>",
		Short: "Authenticate and get token",
		Long: `Authenticate with a QuicBaaS server and obtain an access token.

The token will be securely stored in your home directory at ~/.quicbaas/token
and automatically used for subsequent CLI commands that require authentication.

You can optionally specify a custom token file location using the --token-file flag.

Examples:
  # Login with interactive password prompt
  quicbaas auth login admin

  # Login with password from environment variable
  QUICBAAS_PASSWORD=secret quicbaas auth login admin

  # Login and store token in custom location
  quicbaas auth login admin --token-file /path/to/token

  # Login without storing token (print to stdout)
  quicbaas auth login admin --no-store

  # Login to specific server
  quicbaas auth login admin --server quicbaas.example.com:50051

  # Login with TLS
  quicbaas auth login admin --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt`,
		Args: cobra.ExactArgs(1),
		RunE: runAuthLoginCommand,
	}

	cmd.Flags().StringVar(&authPassword, "password", "", "Password (not recommended, use interactive prompt instead)")
	cmd.Flags().StringVar(&authTokenFile, "token-file", "", "Custom token file location (default: ~/.quicbaas/token)")
	cmd.Flags().BoolVar(&authNoStore, "no-store", false, "Don't store token, print to stdout instead")

	return cmd
}

// runAuthLoginCommand executes the login command.
func runAuthLoginCommand(cmd *cobra.Command, args []string) error {
	username := args[0]

	// Get password from flag, environment, or interactive prompt
	password := authPassword
	if password == "" {
		password = os.Getenv("QUICBAAS_PASSWORD")
	}
	if password == "" {
		fmt.Printf("Password for %s: ", username)
		passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println() // New line after password input
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "auth.login", "failed to read password")
		}
		password = string(passwordBytes)
	}

	if password == "" {
		return errors.New(errors.ErrInvalidInput, "auth.login", "password is required")
	}

	// Create SDK auth client directly using the factory from factories.go
	authClient, err := authClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Authenticate using SDK auth client with auth.LoginRequest
	if log != nil {
		log.Debugf("Authenticating user: %s", username)
	}
	loginResp, err := authClient.Login(ctx, &service.ExtendedLoginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		return formatError(err)
	}

	// Convert SDK response to cli.TokenData for storage
	tokenData := &cli.TokenData{
		AccessToken:  loginResp.AccessToken,
		RefreshToken: loginResp.RefreshToken,
		ExpiresAt:    loginResp.ExpiresAt,
		TokenType:    loginResp.TokenType,
	}

	// Store or display token
	if authNoStore {
		// Print token to stdout in JSON format
		output := map[string]interface{}{
			"access_token":  tokenData.AccessToken,
			"refresh_token": tokenData.RefreshToken,
			"expires_at":    tokenData.ExpiresAt,
			"token_type":    tokenData.TokenType,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(output); err != nil {
			return errors.Wrapf(err, errors.ErrInternal, "auth.login", "failed to encode token")
		}
	} else {
		// Store token securely
		tokenFile := authTokenFile
		if tokenFile == "" {
			tokenFile, err = cli.GetDefaultTokenPath()
			if err != nil {
				return err
			}
		}

		if err := cli.SaveToken(tokenFile, tokenData); err != nil {
			return err
		}

		fmt.Printf("Authentication successful!\n")
		fmt.Printf("Token stored at: %s\n", tokenFile)
		fmt.Printf("Token expires at: %s\n", tokenData.ExpiresAt.Format(time.RFC3339))
	}

	return nil
}

// newAuthLogoutCommand creates the logout command.
func newAuthLogoutCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Invalidate current token",
		Long: `Invalidate the current authentication token and remove it from local storage.

This command will revoke the token on the server side (if the server is reachable)
and remove the token file from your home directory.

Examples:
  # Logout and remove token
  quicbaas auth logout

  # Logout with custom token file
  quicbaas auth logout --token-file /path/to/token

  # Logout from specific server
  quicbaas auth logout --server quicbaas.example.com:50051`,
		RunE: runAuthLogoutCommand,
	}

	cmd.Flags().StringVar(&authTokenFile, "token-file", "", "Custom token file location (default: ~/.quicbaas/token)")

	return cmd
}

// runAuthLogoutCommand executes the logout command.
func runAuthLogoutCommand(cmd *cobra.Command, args []string) error {
	// Get token file path
	tokenFile := authTokenFile
	if tokenFile == "" {
		var err error
		tokenFile, err = cli.GetDefaultTokenPath()
		if err != nil {
			return err
		}
	}

	// Load token
	token, err := cli.LoadToken(tokenFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No active session found")
			return nil
		}
		return err
	}

	// Create SDK auth client directly using the factory from factories.go
	authClient, err := authClientFactory()
	if err != nil {
		// If we can't connect to server, just remove local token
		if log != nil {
			log.Warnf("Cannot connect to server: %v", err)
		}
		fmt.Println("Server unreachable, removing local token only")
	} else {
		// Revoke token on server using SDK auth client
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		if err := authClient.Logout(ctx, token.AccessToken); err != nil {
			if log != nil {
				log.Warnf("Failed to revoke token on server: %v", err)
			}
			fmt.Println("Warning: Failed to revoke token on server, removing local token")
		} else {
			fmt.Println("Token revoked successfully")
		}
	}

	// Remove local token file
	if err := os.Remove(tokenFile); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, errors.ErrInternal, "auth.logout", "failed to remove token file")
	}

	fmt.Println("Logged out successfully")
	return nil
}

// newAuthStatusCommand creates the status command.
func newAuthStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		Long: `Display information about the current authentication session.

This command shows whether you're currently authenticated, token expiration time,
and other authentication-related details.

Examples:
  # Show authentication status
  quicbaas auth status

  # Show status with custom token file
  quicbaas auth status --token-file /path/to/token

  # Show status as JSON
  quicbaas auth status --output json`,
		RunE: runAuthStatusCommand,
	}

	cmd.Flags().StringVar(&authTokenFile, "token-file", "", "Custom token file location (default: ~/.quicbaas/token)")
	cmd.Flags().StringVar(&authOutputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// runAuthStatusCommand executes the status command.
func runAuthStatusCommand(cmd *cobra.Command, args []string) error {
	// Get token file path
	tokenFile := authTokenFile
	if tokenFile == "" {
		var err error
		tokenFile, err = cli.GetDefaultTokenPath()
		if err != nil {
			return err
		}
	}

	// Load token
	token, err := cli.LoadToken(tokenFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Format output for no token case
			format, err := cli.ParseOutputFormat(authOutputFormat)
			if err != nil {
				return err
			}

			switch format {
			case cli.OutputFormatJSON:
				output := map[string]interface{}{
					"authenticated": false,
					"status":        "not authenticated",
				}
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(output)
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "Not authenticated")
			}
			return nil
		}
		return err
	}

	// Check if token is expired
	expired := time.Now().After(token.ExpiresAt)
	status := "valid"
	if expired {
		status = "expired"
	}

	// Format output
	format, err := cli.ParseOutputFormat(authOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"authenticated": !expired,
			"status":        status,
			"expires_at":    token.ExpiresAt,
			"token_type":    token.TokenType,
			"token_file":    tokenFile,
		}
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	default:
		fmt.Fprintln(cmd.OutOrStdout(), "Authentication Status:")
		fmt.Fprintf(cmd.OutOrStdout(), "  Authenticated: %v\n", !expired)
		fmt.Fprintf(cmd.OutOrStdout(), "  Status: %s\n", status)
		fmt.Fprintf(cmd.OutOrStdout(), "  Token Type: %s\n", token.TokenType)
		fmt.Fprintf(cmd.OutOrStdout(), "  Expires At: %s\n", token.ExpiresAt.Format(time.RFC3339))
		if expired {
			fmt.Fprintf(cmd.OutOrStdout(), "  Token expired %s ago\n", time.Since(token.ExpiresAt).Round(time.Second))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  Expires in: %s\n", time.Until(token.ExpiresAt).Round(time.Second))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  Token File: %s\n", tokenFile)
	}

	return nil
}
