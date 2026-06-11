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

// Package main provides IAM application management CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

var (
	// Application command flags.
	iamAppName           string
	iamAppRedirectURIs   []string
	iamAppRedirectURI    string
	iamAppResourcePrefix string
)

// newIAMAppCommand creates the application command group.
func newIAMAppCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "OAuth application management commands",
		Long: `Manage OAuth 2.0 / OIDC applications within a tenant.

Applications represent OAuth clients that can authenticate users
and access QuicBaaS resources on behalf of users or themselves
using client credentials.

All app commands require the --tenant flag to specify the tenant context.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMAppRegisterCommand())
	cmd.AddCommand(newIAMAppGetCommand())
	cmd.AddCommand(newIAMAppUpdateCommand())
	cmd.AddCommand(newIAMAppDeleteCommand())
	cmd.AddCommand(newIAMAppListCommand())
	cmd.AddCommand(newIAMAppRotateSecretCommand())

	return cmd
}

// newIAMAppRegisterCommand creates the app register command.
func newIAMAppRegisterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a new OAuth application",
		Long: `Register a new OAuth 2.0 / OIDC application within a tenant.

The application will be issued a client_id and client_secret that can
be used for OAuth authentication flows.

Examples:
  # Register basic application
  quicbaas iam app register --name my-app --tenant acme-corp

  # Register with single redirect URI
  quicbaas iam app register --name my-app --tenant acme-corp --redirect-uri "https://app.com/callback"

  # Register with multiple redirect URIs
  quicbaas iam app register --name my-app --tenant acme-corp --redirect-uris "https://app.com/callback,https://app.com/callback2"

  # Register with resource prefix
  quicbaas iam app register --name order-service --tenant acme-corp --resource-prefix "orders://"`,
		RunE: runIAMAppRegisterCommand,
	}

	cmd.Flags().StringVar(&iamAppName, "name", "", "Application name (required)")
	cmd.Flags().StringVar(&iamAppRedirectURI, "redirect-uri", "", "OAuth redirect URI (single URI)")
	cmd.Flags().StringSliceVar(&iamAppRedirectURIs, "redirect-uris", []string{}, "OAuth redirect URIs (comma-separated)")
	cmd.Flags().StringVar(&iamAppResourcePrefix, "resource-prefix", "", "Resource prefix for ABAC policies")
	cmd.MarkFlagRequired("name")

	return cmd
}

// runIAMAppRegisterCommand executes the app register command.
func runIAMAppRegisterCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.app.register", "--tenant flag is required")
	}

	// Merge redirect URI flags - singular takes precedence if both provided
	redirectURIs := iamAppRedirectURIs
	if iamAppRedirectURI != "" {
		redirectURIs = append([]string{iamAppRedirectURI}, redirectURIs...)
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Register application using SDK sub-client
	log.Debugf("Registering application: %s in tenant: %s", iamAppName, globalTenantID)
	app, err := iamClient.App().Register(ctx, globalTenantID, &iam.RegisterAppRequest{
		Name:         iamAppName,
		RedirectURIs: redirectURIs,
		GrantTypes:   []iam.GrantType{iam.GrantTypeClientCredentials},
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
		return encoder.Encode(app)

	default:
		fmt.Println("Application registered successfully:")
		fmt.Printf("  Client ID: %s\n", app.ClientID)
		fmt.Printf("  Client Secret: %s\n", app.ClientSecret)
		fmt.Printf("  Name: %s\n", app.Name)
		fmt.Printf("  Tenant: %s\n", app.TenantID)
		if len(app.RedirectURIs) > 0 {
			fmt.Printf("  Redirect URIs:\n")
			for _, uri := range app.RedirectURIs {
				fmt.Printf("    - %s\n", uri)
			}
		}
		fmt.Printf("  Created: %s\n", app.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Println("\nWARNING: Store the client secret securely. It will not be shown again.")
	}

	return nil
}

// newIAMAppGetCommand creates the app get command.
func newIAMAppGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <client-id>",
		Short: "Get application details",
		Long: `Retrieve detailed information about a specific application.

Note: The client secret will not be returned for security reasons.

Examples:
  # Get app details
  quicbaas iam app get app_abc123

  # Get app as JSON
  quicbaas iam app get app_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMAppGetCommand,
	}

	return cmd
}

// runIAMAppGetCommand executes the app get command.
func runIAMAppGetCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get application using SDK sub-client
	log.Debugf("Getting application: %s (tenant: %s)", clientID, globalTenantID)
	app, err := iamClient.App().Get(ctx, globalTenantID, clientID)
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
		return encoder.Encode(app)

	default:
		fmt.Println("Application Details:")
		fmt.Printf("  Client ID: %s\n", app.ClientID)
		fmt.Printf("  Name: %s\n", app.Name)
		fmt.Printf("  Tenant: %s\n", app.TenantID)
		if len(app.RedirectURIs) > 0 {
			fmt.Printf("  Redirect URIs:\n")
			for _, uri := range app.RedirectURIs {
				fmt.Printf("    - %s\n", uri)
			}
		}
		fmt.Printf("  Created: %s\n", app.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated: %s\n", app.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMAppUpdateCommand creates the app update command.
func newIAMAppUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <client-id>",
		Short: "Update application settings",
		Long: `Update an existing application's settings.

At least one flag must be specified to update the application.

Examples:
  # Update app name
  quicbaas iam app update app_abc123 --name "My Updated App"

  # Update redirect URI (single)
  quicbaas iam app update app_abc123 --redirect-uri "https://newapp.com/callback"

  # Update redirect URIs (multiple)
  quicbaas iam app update app_abc123 --redirect-uris "https://newapp.com/callback"`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMAppUpdateCommand,
	}

	cmd.Flags().StringVar(&iamAppName, "name", "", "New application name")
	cmd.Flags().StringVar(&iamAppRedirectURI, "redirect-uri", "", "New redirect URI (single URI)")
	cmd.Flags().StringSliceVar(&iamAppRedirectURIs, "redirect-uris", nil, "New redirect URIs (comma-separated)")

	return cmd
}

// runIAMAppUpdateCommand executes the app update command.
func runIAMAppUpdateCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	// Check if at least one update flag is provided
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("redirect-uris") && !cmd.Flags().Changed("redirect-uri") {
		return errors.Newf(errors.ErrInvalidInput, "iam.app.update", "at least one of --name, --redirect-uri, or --redirect-uris must be specified")
	}

	// Merge redirect URI flags - singular takes precedence if both provided
	redirectURIs := iamAppRedirectURIs
	if cmd.Flags().Changed("redirect-uri") && iamAppRedirectURI != "" {
		redirectURIs = append([]string{iamAppRedirectURI}, redirectURIs...)
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Update application using SDK sub-client
	log.Debugf("Updating application: %s (tenant: %s)", clientID, globalTenantID)
	updateReq := &iam.UpdateAppRequest{}
	if cmd.Flags().Changed("name") {
		updateReq.Name = iamAppName
	}
	if len(redirectURIs) > 0 {
		updateReq.RedirectURIs = redirectURIs
	}
	app, err := iamClient.App().Update(ctx, globalTenantID, clientID, updateReq)
	if err != nil {
		return formatError(err)
	}

	fmt.Println("Application updated successfully:")
	fmt.Printf("  Client ID: %s\n", app.ClientID)
	fmt.Printf("  Name: %s\n", app.Name)
	if len(app.RedirectURIs) > 0 {
		fmt.Printf("  Redirect URIs: %s\n", strings.Join(app.RedirectURIs, ", "))
	}

	return nil
}

// newIAMAppDeleteCommand creates the app delete command.
func newIAMAppDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <client-id>",
		Short: "Delete an application",
		Long: `Delete an application and revoke all its credentials.

WARNING: This operation is irreversible and will revoke all
access tokens issued to this application.

Examples:
  # Delete app with confirmation
  quicbaas iam app delete app_abc123

  # Delete app without confirmation
  quicbaas iam app delete app_abc123 --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMAppDeleteCommand,
	}

	return cmd
}

// runIAMAppDeleteCommand executes the app delete command.
func runIAMAppDeleteCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to delete application '%s'? This cannot be undone. (y/N): ", clientID)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Delete cancelled")
			return nil
		}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Delete application using SDK sub-client
	log.Debugf("Deleting application: %s (tenant: %s)", clientID, globalTenantID)
	if err := iamClient.App().Delete(ctx, globalTenantID, clientID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Application '%s' deleted successfully\n", clientID)
	return nil
}

// newIAMAppListCommand creates the app list command.
func newIAMAppListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List applications in a tenant",
		Long: `List all applications within a tenant.

Examples:
  # List all applications
  quicbaas iam app list --tenant acme-corp

  # List applications as JSON
  quicbaas iam app list --tenant acme-corp --output json`,
		RunE: runIAMAppListCommand,
	}

	return cmd
}

// runIAMAppListCommand executes the app list command.
func runIAMAppListCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.app.list", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List applications using SDK sub-client
	log.Debugf("Listing applications in tenant: %s", globalTenantID)
	resp, err := iamClient.App().List(ctx, globalTenantID)
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
		return encoder.Encode(resp.Applications)

	default:
		if len(resp.Applications) == 0 {
			fmt.Println("No applications found")
			return nil
		}

		fmt.Printf("%-30s %-30s %-20s %s\n", "CLIENT ID", "NAME", "TYPE", "CREATED")
		fmt.Println(strings.Repeat("-", 110))
		for _, app := range resp.Applications {
			fmt.Printf("%-30s %-30s %-20s %s\n",
				app.ClientID,
				app.Name,
				app.Type.String(),
				app.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d applications\n", len(resp.Applications))
	}

	return nil
}

// newIAMAppRotateSecretCommand creates the rotate secret command.
func newIAMAppRotateSecretCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate-secret <client-id>",
		Short: "Rotate application client secret",
		Long: `Generate a new client secret for an application.

This invalidates the previous client secret immediately. Make sure
to update your application configuration with the new secret.

Examples:
  # Rotate client secret
  quicbaas iam app rotate-secret app_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMAppRotateSecretCommand,
	}

	return cmd
}

// runIAMAppRotateSecretCommand executes the rotate secret command.
func runIAMAppRotateSecretCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Rotate secret using SDK sub-client
	log.Debugf("Rotating secret for application: %s (tenant: %s)", clientID, globalTenantID)
	resp, err := iamClient.App().RotateSecret(ctx, globalTenantID, clientID)
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
		return encoder.Encode(resp)

	default:
		fmt.Println("Client secret rotated successfully:")
		fmt.Printf("  Client ID: %s\n", resp.ClientID)
		fmt.Printf("  New Client Secret: %s\n", resp.ClientSecret)
		fmt.Println("\nWARNING: Store the new secret securely. The old secret is now invalid.")
	}

	return nil
}
