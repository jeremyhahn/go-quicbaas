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
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var (
	// Auth command flags.
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
		Use:   "login",
		Short: "Authenticate using WebAuthn or PIV smartcard",
		Long: `QuicBaaS uses WebAuthn (FIDO2) and PIV smartcard authentication.

Password-based login is not supported. To authenticate:

  - WebAuthn: use the web interface at https://<server>/login/webauthn
  - PIV:      present your PIV smartcard certificate via mTLS

The server will issue a JWT after successful authentication.
Store the resulting token using --token-file or the QUICBAAS_TOKEN env var.

Examples:
  # Show login instructions
  quicbaas auth login`,
		RunE: runAuthLoginCommand,
	}

	cmd.Flags().StringVar(&authTokenFile, "token-file", "", "Token file location (default: ~/.quicbaas/token)")
	cmd.Flags().BoolVar(&authNoStore, "no-store", false, "Don't store token, print to stdout instead")

	return cmd
}

// runAuthLoginCommand explains the supported authentication methods.
func runAuthLoginCommand(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(cmd.OutOrStdout(), "QuicBaaS uses WebAuthn (FIDO2) and PIV smartcard authentication.")
	fmt.Fprintln(cmd.OutOrStdout(), "Password-based login is not supported.")
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "To authenticate:")
	fmt.Fprintln(cmd.OutOrStdout(), "  WebAuthn: open https://<server>/login/webauthn in your browser")
	fmt.Fprintln(cmd.OutOrStdout(), "  PIV:      configure mTLS with your PIV smartcard certificate")
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
