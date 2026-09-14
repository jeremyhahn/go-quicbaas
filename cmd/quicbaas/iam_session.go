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

// Package main provides IAM session management CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

var (
	// Session command flags.
	sessionKeepCurrent bool
	sessionLimit       int
	sessionUserID      string
)

// newIAMSessionCommand creates the session command group.
func newIAMSessionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Session management commands",
		Long: `Manage user sessions within a tenant.

Sessions represent active authentication contexts for users. Each session
tracks login information including device details, IP address, and activity.

All session commands require the --tenant flag to specify the tenant context.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMSessionListCommand())
	cmd.AddCommand(newIAMSessionGetCommand())
	cmd.AddCommand(newIAMSessionRevokeCommand())
	cmd.AddCommand(newIAMSessionRevokeAllCommand())

	return cmd
}

// newIAMSessionListCommand creates the session list command.
func newIAMSessionListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all active sessions for the current user",
		Long: `List all active sessions for the current user within a tenant.

Displays session information including creation time, last activity,
IP address, and user agent.

Examples:
  # List all sessions
  quicbaas iam session list --tenant acme-corp

  # List sessions with limit
  quicbaas iam session list --tenant acme-corp --limit 10

  # List sessions as JSON
  quicbaas iam session list --tenant acme-corp --output json`,
		RunE: runIAMSessionListCommand,
	}

	cmd.Flags().IntVar(&sessionLimit, "limit", 0, "Maximum number of sessions to return (0 = unlimited)")
	cmd.Flags().StringVar(&sessionUserID, "user", "", "User ID to list sessions for (defaults to current user from token)")

	return cmd
}

// runIAMSessionListCommand executes the session list command.
func runIAMSessionListCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.session.list", "--tenant flag is required")
	}

	// Get user ID from flag or token
	userID := sessionUserID
	if userID == "" {
		// Try to get from token
		var err error
		userID, err = getCurrentUserID()
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "iam.session.list", "failed to get current user ID; use --user flag to specify user")
		}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// List sessions using SDK sub-client
	log.Infof("Listing sessions in tenant: %s", globalTenantID)
	resp, err := iamClient.Session().List(ctx, &iam.ListSessionsRequest{
		TenantID: globalTenantID,
		UserID:   userID,
		Limit:    sessionLimit,
	})
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp.Sessions)

	case cli.OutputFormatYAML:
		data, err := yaml.Marshal(resp.Sessions)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil

	default:
		if len(resp.Sessions) == 0 {
			fmt.Println("No active sessions found")
			return nil
		}

		fmt.Printf("%-36s %-20s %-20s %-15s %s\n", "SESSION ID", "CREATED", "LAST ACTIVE", "IP ADDRESS", "USER AGENT")
		fmt.Println(strings.Repeat("-", 130))
		for _, session := range resp.Sessions {
			userAgent := session.UserAgent
			if len(userAgent) > 40 {
				userAgent = userAgent[:37] + "..."
			}
			fmt.Printf("%-36s %-20s %-20s %-15s %s\n",
				session.ID,
				session.Created.Format("2006-01-02 15:04"),
				session.LastActive.Format("2006-01-02 15:04"),
				session.IPAddress,
				userAgent)
		}
		fmt.Printf("\nTotal: %d sessions\n", len(resp.Sessions))
	}

	return nil
}

// newIAMSessionGetCommand creates the session get command.
func newIAMSessionGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get details about a specific session",
		Long: `Retrieve detailed information about a specific session.

Examples:
  # Get session details
  quicbaas iam session get sess_abc123 --tenant acme-corp

  # Get session as JSON
  quicbaas iam session get sess_abc123 --tenant acme-corp --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMSessionGetCommand,
	}

	return cmd
}

// runIAMSessionGetCommand executes the session get command.
func runIAMSessionGetCommand(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.session.get", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Get session using SDK sub-client
	log.Infof("Getting session: %s in tenant: %s", sessionID, globalTenantID)
	session, err := iamClient.Session().Get(ctx, sessionID)
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(session)

	default:
		fmt.Println("Session Details:")
		fmt.Printf("  ID: %s\n", session.ID)
		fmt.Printf("  User ID: %s\n", session.UserID)
		fmt.Printf("  Tenant: %s\n", session.TenantID)
		fmt.Printf("  Created: %s\n", session.Created.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Last Active: %s\n", session.LastActive.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Expires At: %s\n", session.ExpiresAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  MFA Verified: %t\n", session.MFAVerified)
		fmt.Printf("  IP Address: %s\n", session.IPAddress)
		fmt.Printf("  User Agent: %s\n", session.UserAgent)
		if session.DeviceInfo != "" {
			fmt.Printf("  Device Info: %s\n", session.DeviceInfo)
		}
	}

	return nil
}

// newIAMSessionRevokeCommand creates the session revoke command.
func newIAMSessionRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a specific session",
		Long: `Revoke a specific session, logging out that session.

WARNING: This will immediately invalidate the session and require
the user to authenticate again.

Examples:
  # Revoke session with confirmation
  quicbaas iam session revoke sess_abc123 --tenant acme-corp

  # Revoke session without confirmation
  quicbaas iam session revoke sess_abc123 --tenant acme-corp --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMSessionRevokeCommand,
	}

	return cmd
}

// runIAMSessionRevokeCommand executes the session revoke command.
func runIAMSessionRevokeCommand(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.session.revoke", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to revoke session '%s'? This will log out that session. (y/N): ", sessionID)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Revoke cancelled")
			return nil
		}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Revoke session using SDK sub-client
	log.Infof("Revoking session: %s in tenant: %s", sessionID, globalTenantID)
	if err := iamClient.Session().Revoke(ctx, sessionID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Session '%s' revoked successfully\n", sessionID)
	return nil
}

// newIAMSessionRevokeAllCommand creates the session revoke-all command.
func newIAMSessionRevokeAllCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke-all",
		Short: "Revoke all sessions (logout everywhere)",
		Long: `Revoke all active sessions for the current user.

This is equivalent to "logout everywhere" and will invalidate all
active sessions. By default, this also revokes the current session.

Use --keep-current to keep the current session active while revoking
all other sessions.

WARNING: This will immediately log out from all devices.

Examples:
  # Revoke all sessions with confirmation
  quicbaas iam session revoke-all --tenant acme-corp

  # Revoke all sessions for a specific user
  quicbaas iam session revoke-all --tenant acme-corp --user user_abc123

  # Revoke all sessions except current
  quicbaas iam session revoke-all --tenant acme-corp --keep-current

  # Revoke all sessions without confirmation
  quicbaas iam session revoke-all --tenant acme-corp --yes`,
		RunE: runIAMSessionRevokeAllCommand,
	}

	cmd.Flags().BoolVar(&sessionKeepCurrent, "keep-current", false, "Keep the current session active")
	cmd.Flags().StringVar(&sessionUserID, "user", "", "User ID to revoke sessions for (defaults to current user from token)")

	return cmd
}

// runIAMSessionRevokeAllCommand executes the session revoke-all command.
func runIAMSessionRevokeAllCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.session.revoke-all", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		msg := "Are you sure you want to revoke all sessions? This will log you out from all devices."
		if sessionKeepCurrent {
			msg = "Are you sure you want to revoke all other sessions? This will log you out from all other devices."
		}
		fmt.Printf("%s (y/N): ", msg)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Revoke cancelled")
			return nil
		}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Revoke sessions based on keep-current flag
	log.Infof("Revoking sessions in tenant: %s (keep-current: %t)", globalTenantID, sessionKeepCurrent)

	if sessionKeepCurrent {
		// Get current session ID from stored token
		currentSessionID, err := getCurrentSessionID()
		if err != nil {
			return errors.Newf(errors.ErrInvalidInput, "iam.session.revoke-all", "failed to get current session ID: %v", err)
		}
		if currentSessionID == "" {
			return errors.Newf(errors.ErrInvalidInput, "iam.session.revoke-all", "no current session found; cannot use --keep-current")
		}

		if err := iamClient.Session().RevokeOthers(ctx, currentSessionID); err != nil {
			return formatError(err)
		}
		fmt.Println("Successfully revoked all other sessions")
	} else {
		// Get user ID from flag or token
		userID := sessionUserID
		if userID == "" {
			var err error
			userID, err = getCurrentUserID()
			if err != nil {
				return errors.Wrapf(err, errors.ErrInvalidInput, "iam.session.revoke-all", "failed to get current user ID; use --user flag to specify user")
			}
		}

		if err := iamClient.Session().RevokeAll(ctx, globalTenantID, userID); err != nil {
			return formatError(err)
		}
		fmt.Println("Successfully revoked all sessions")
	}

	return nil
}

// getCurrentSessionID extracts the session ID from the stored JWT token.
func getCurrentSessionID() (string, error) {
	// Get the default token path
	tokenPath, err := cli.GetDefaultTokenPath()
	if err != nil {
		return "", err
	}

	// Load the stored token
	token, err := cli.LoadToken(tokenPath)
	if err != nil {
		return "", err
	}

	// Parse the JWT to extract session ID from claims
	// JWT format: header.payload.signature
	parts := strings.Split(token.AccessToken, ".")
	if len(parts) != 3 {
		return "", errors.Newf(errors.ErrInvalidInput, "iam.session", "invalid token format")
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.Wrapf(err, errors.ErrInvalidInput, "iam.session", "failed to decode token payload")
	}

	// Parse the claims
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.Wrapf(err, errors.ErrInvalidInput, "iam.session", "failed to parse token claims")
	}

	// Extract session ID (typically stored as "sid" or "session_id" claim)
	if sid, ok := claims["sid"].(string); ok {
		return sid, nil
	}
	if sessionID, ok := claims["session_id"].(string); ok {
		return sessionID, nil
	}

	return "", errors.Newf(errors.ErrInvalidInput, "iam.session", "no session ID found in token")
}

// getCurrentUserID extracts the user ID from the stored JWT token.
func getCurrentUserID() (string, error) {
	// Get the default token path
	tokenPath, err := cli.GetDefaultTokenPath()
	if err != nil {
		return "", err
	}

	// Load the stored token
	token, err := cli.LoadToken(tokenPath)
	if err != nil {
		return "", err
	}

	// Parse the JWT to extract user ID from claims
	// JWT format: header.payload.signature
	parts := strings.Split(token.AccessToken, ".")
	if len(parts) != 3 {
		return "", errors.Newf(errors.ErrInvalidInput, "iam.session", "invalid token format")
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.Wrapf(err, errors.ErrInvalidInput, "iam.session", "failed to decode token payload")
	}

	// Parse the claims
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.Wrapf(err, errors.ErrInvalidInput, "iam.session", "failed to parse token claims")
	}

	// Extract user ID (typically stored as "sub" claim)
	if sub, ok := claims["sub"].(string); ok {
		return sub, nil
	}
	if userID, ok := claims["user_id"].(string); ok {
		return userID, nil
	}

	return "", errors.Newf(errors.ErrInvalidInput, "iam.session", "no user ID found in token")
}
