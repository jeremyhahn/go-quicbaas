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

//go:build iam

// Package main provides OAuth2 client management CLI commands for QuicBaaS.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	sdkiam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// oauth2ClientFlags holds flag values for the oauth2 client subcommands.
var (
	oauth2ClientName       string
	oauth2ClientAuthMethod string
	oauth2ClientGrantType  string
	oauth2ClientCertPEM    string
	oauth2ClientThumbprint string
	oauth2ClientScopes     []string
)

// newOAuth2Command creates the top-level oauth2 command group.
func newOAuth2Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth2",
		Short: "OAuth2 client and token management",
		Long:  "Manage OAuth2 clients and tokens for QuicBaaS service-account access.",
	}

	cmd.AddCommand(newOAuth2ClientCommand())

	return cmd
}

// newOAuth2ClientCommand creates the oauth2 client subcommand group.
func newOAuth2ClientCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "OAuth2 client management",
		Long: `Manage OAuth2 clients (applications) in a tenant.

Supports all four authentication methods defined by RFC 8705 and RFC 6749:
  client_secret_basic        — HTTP Basic authentication (default)
  client_secret_post         — Client secret as POST form parameter
  tls_client_auth            — PKI-validated mTLS (RFC 8705 §2.1)
  self_signed_tls_client_auth — Thumbprint-pinned mTLS (RFC 8705 §2.2)

For mTLS methods, use --cert-pem to provide the PEM-encoded certificate.
The CLI computes and pins the SHA-256 thumbprint for you.`,
	}

	cmd.AddCommand(newOAuth2ClientRegisterCommand())
	cmd.AddCommand(newOAuth2ClientListCommand())
	cmd.AddCommand(newOAuth2ClientShowCommand())
	cmd.AddCommand(newOAuth2ClientDeleteCommand())
	cmd.AddCommand(newOAuth2ClientPinCertCommand())

	return cmd
}

// newOAuth2ClientRegisterCommand creates the register subcommand.
func newOAuth2ClientRegisterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a new OAuth2 client",
		Long: `Register a new OAuth2 client in a tenant.

Examples:
  # Register a secret-based client (default)
  quicbaas oauth2 client register --tenant acme --name "my-service"

  # Register a self-signed mTLS client
  quicbaas oauth2 client register --tenant acme --name "gpu-agent" \
    --auth-method self_signed_tls_client_auth --cert-pem /path/to/client.pem

  # Register a PKI mTLS client by thumbprint
  quicbaas oauth2 client register --tenant acme --name "fleet-agent" \
    --auth-method tls_client_auth --cert-pem /path/to/client.pem

  # Register with scopes
  quicbaas oauth2 client register --tenant acme --name "api-client" \
    --scope read --scope write`,
		RunE: runOAuth2ClientRegisterCommand,
	}

	cmd.Flags().StringVar(&oauth2ClientName, "name", "", "Client display name (required)")
	cmd.Flags().StringVar(&oauth2ClientGrantType, "grant-type", "client_credentials", "OAuth2 grant type")
	cmd.Flags().StringVar(&oauth2ClientAuthMethod, "auth-method", "client_secret_basic",
		"Token endpoint auth method: client_secret_basic, client_secret_post, tls_client_auth, self_signed_tls_client_auth")
	cmd.Flags().StringVar(&oauth2ClientCertPEM, "cert-pem", "",
		"Path to PEM-encoded client certificate (required for tls_client_auth and self_signed_tls_client_auth)")
	cmd.Flags().StringVar(&oauth2ClientThumbprint, "cert-thumbprint", "",
		"SHA-256 hex thumbprint of the client cert (alternative to --cert-pem for self_signed_tls_client_auth)")
	cmd.Flags().StringArrayVar(&oauth2ClientScopes, "scope", nil, "Allowed scope (repeatable)")
	cmd.MarkFlagRequired("name")

	return cmd
}

// runOAuth2ClientRegisterCommand executes the register command.
func runOAuth2ClientRegisterCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.register", "--tenant flag is required")
	}

	authMethod, req, err := buildRegisterRequest(cmd)
	if err != nil {
		return err
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	app, err := iamClient.App().Register(ctx, globalTenantID, req)
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(app)
	default:
		fmt.Println("OAuth2 client registered successfully:")
		fmt.Printf("  Client ID: %s\n", app.ClientID)
		fmt.Printf("  Auth Method: %s\n", authMethod)
		fmt.Printf("  Tenant: %s\n", app.TenantID)
		if app.MTLSCertThumbprint != "" {
			fmt.Printf("  Cert Thumbprint (SHA-256): %s\n", app.MTLSCertThumbprint)
		}
		// For secret-based methods, print the secret (one-time display).
		if app.ClientSecret != "" {
			fmt.Printf("  Client Secret: %s\n", app.ClientSecret)
			fmt.Println("\nWARNING: Store the client secret securely. It will not be shown again.")
		}
	}

	return nil
}

// buildRegisterRequest validates flags and constructs the RegisterAppRequest.
func buildRegisterRequest(cmd *cobra.Command) (string, *sdkiam.RegisterAppRequest, error) {
	authMethod := strings.ToLower(strings.TrimSpace(oauth2ClientAuthMethod))

	req := &sdkiam.RegisterAppRequest{
		Name:       oauth2ClientName,
		GrantTypes: []sdkiam.GrantType{sdkiam.ParseGrantType(oauth2ClientGrantType)},
		Scopes:     oauth2ClientScopes,
	}

	switch authMethod {
	case "client_secret_basic":
		req.TokenEndpointAuthMethod = sdkiam.TokenEndpointAuthClientSecretBasic

	case "client_secret_post":
		req.TokenEndpointAuthMethod = sdkiam.TokenEndpointAuthClientSecretPost

	case "tls_client_auth":
		req.TokenEndpointAuthMethod = sdkiam.TokenEndpointAuthTLSClientAuth
		if oauth2ClientCertPEM != "" {
			thumbprint, subjectDN, err := certMetadataFromPEM(oauth2ClientCertPEM)
			if err != nil {
				return "", nil, err
			}
			req.MTLSCertThumbprint = thumbprint
			req.MTLSSubjectDN = subjectDN
		} else if oauth2ClientThumbprint != "" {
			req.MTLSCertThumbprint = oauth2ClientThumbprint
		} else {
			return "", nil, pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.register",
				"--cert-pem or --cert-thumbprint is required for tls_client_auth")
		}

	case "self_signed_tls_client_auth":
		req.TokenEndpointAuthMethod = sdkiam.TokenEndpointAuthSelfSignedTLSClientAuth
		if oauth2ClientCertPEM != "" {
			thumbprint, _, err := certMetadataFromPEM(oauth2ClientCertPEM)
			if err != nil {
				return "", nil, err
			}
			req.MTLSCertThumbprint = thumbprint
		} else if oauth2ClientThumbprint != "" {
			req.MTLSCertThumbprint = oauth2ClientThumbprint
		} else {
			return "", nil, pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.register",
				"--cert-pem or --cert-thumbprint is required for self_signed_tls_client_auth")
		}

	default:
		return "", nil, pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.register",
			"unsupported --auth-method: "+authMethod)
	}

	return authMethod, req, nil
}

// certMetadataFromPEM reads a PEM file and returns the SHA-256 thumbprint and
// Subject DN of the first certificate found.
func certMetadataFromPEM(path string) (thumbprint, subjectDN string, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return "", "", pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.register",
			"failed to read cert PEM file: "+readErr.Error())
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return "", "", pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.register",
			"no PEM block found in "+path)
	}

	cert, parseErr := x509.ParseCertificate(block.Bytes)
	if parseErr != nil {
		return "", "", pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.register",
			"failed to parse certificate: "+parseErr.Error())
	}

	sum := sha256.Sum256(cert.Raw)
	thumbprint = base64.RawURLEncoding.EncodeToString(sum[:])
	subjectDN = cert.Subject.String()
	return thumbprint, subjectDN, nil
}

// newOAuth2ClientListCommand creates the list subcommand.
func newOAuth2ClientListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List OAuth2 clients in a tenant",
		Long: `List all OAuth2 clients registered in the current tenant.

Examples:
  quicbaas oauth2 client list --tenant acme
  quicbaas oauth2 client list --tenant acme --output json`,
		RunE: runOAuth2ClientListCommand,
	}
}

// runOAuth2ClientListCommand executes the list command.
func runOAuth2ClientListCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.list", "--tenant flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := iamClient.App().List(ctx, globalTenantID)
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp.Applications)
	default:
		if len(resp.Applications) == 0 {
			fmt.Println("No OAuth2 clients found")
			return nil
		}
		fmt.Printf("%-36s %-30s %-34s %s\n", "CLIENT ID", "NAME", "AUTH METHOD", "CREATED")
		fmt.Println(strings.Repeat("-", 120))
		for _, app := range resp.Applications {
			fmt.Printf("%-36s %-30s %-34s %s\n",
				app.ClientID,
				app.Name,
				app.TokenEndpointAuthMethod.String(),
				app.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d clients\n", len(resp.Applications))
	}

	return nil
}

// newOAuth2ClientShowCommand creates the show subcommand.
func newOAuth2ClientShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <client-id>",
		Short: "Show details of an OAuth2 client",
		Long: `Show detailed information about a specific OAuth2 client.

Examples:
  quicbaas oauth2 client show app_abc123 --tenant acme
  quicbaas oauth2 client show app_abc123 --tenant acme --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runOAuth2ClientShowCommand,
	}
}

// runOAuth2ClientShowCommand executes the show command.
func runOAuth2ClientShowCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	app, err := iamClient.App().Get(ctx, globalTenantID, clientID)
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(app)
	default:
		fmt.Println("OAuth2 Client Details:")
		fmt.Printf("  Client ID:   %s\n", app.ClientID)
		fmt.Printf("  Name:        %s\n", app.Name)
		fmt.Printf("  Tenant:      %s\n", app.TenantID)
		fmt.Printf("  Auth Method: %s\n", app.TokenEndpointAuthMethod.String())
		if app.MTLSCertThumbprint != "" {
			fmt.Printf("  Cert Thumbprint (SHA-256): %s\n", app.MTLSCertThumbprint)
		}
		if app.MTLSSubjectDN != "" {
			fmt.Printf("  Subject DN:  %s\n", app.MTLSSubjectDN)
		}
		if app.MTLSSANDNS != "" {
			fmt.Printf("  SAN DNS:     %s\n", app.MTLSSANDNS)
		}
		if len(app.Scopes) > 0 {
			fmt.Printf("  Scopes:      %s\n", strings.Join(app.Scopes, ", "))
		}
		fmt.Printf("  Created:     %s\n", app.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated:     %s\n", app.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newOAuth2ClientDeleteCommand creates the delete subcommand.
func newOAuth2ClientDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <client-id>",
		Short: "Delete an OAuth2 client",
		Long: `Delete an OAuth2 client and revoke all its credentials.

WARNING: This operation is irreversible and immediately revokes all active
access tokens issued to this client.

Examples:
  quicbaas oauth2 client delete app_abc123 --tenant acme
  quicbaas oauth2 client delete app_abc123 --tenant acme --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runOAuth2ClientDeleteCommand,
	}

	return cmd
}

// runOAuth2ClientDeleteCommand executes the delete command.
func runOAuth2ClientDeleteCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	if !iamForce {
		fmt.Printf("Are you sure you want to delete OAuth2 client '%s'? (y/N): ", clientID)
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(strings.TrimSpace(response)) != "y" {
			fmt.Println("Delete cancelled")
			return nil
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := iamClient.App().Delete(ctx, globalTenantID, clientID); err != nil {
		return formatError(err)
	}

	fmt.Printf("OAuth2 client '%s' deleted successfully\n", clientID)
	return nil
}

// newOAuth2ClientPinCertCommand creates the pin-cert subcommand.
func newOAuth2ClientPinCertCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pin-cert <client-id>",
		Short: "Pin or rotate the mTLS certificate for a client",
		Long: `Pin or rotate the mTLS client certificate for a self_signed_tls_client_auth client.

This replaces the current pinned cert thumbprint with the thumbprint of the
new certificate. Active access tokens remain valid until they expire; new
token requests must present the new certificate.

Examples:
  # Pin a new certificate by PEM file
  quicbaas oauth2 client pin-cert app_abc123 --cert-pem /path/to/new-cert.pem

  # Pin by thumbprint (e.g. computed externally)
  quicbaas oauth2 client pin-cert app_abc123 --cert-thumbprint <sha256-base64url>`,
		Args: cobra.ExactArgs(1),
		RunE: runOAuth2ClientPinCertCommand,
	}

	cmd.Flags().StringVar(&oauth2ClientCertPEM, "cert-pem", "",
		"Path to PEM-encoded client certificate")
	cmd.Flags().StringVar(&oauth2ClientThumbprint, "cert-thumbprint", "",
		"SHA-256 base64url thumbprint of the client cert (alternative to --cert-pem)")

	return cmd
}

// runOAuth2ClientPinCertCommand executes the pin-cert command.
func runOAuth2ClientPinCertCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	var thumbprint string
	switch {
	case oauth2ClientCertPEM != "":
		t, _, err := certMetadataFromPEM(oauth2ClientCertPEM)
		if err != nil {
			return err
		}
		thumbprint = t
	case oauth2ClientThumbprint != "":
		thumbprint = oauth2ClientThumbprint
	default:
		return pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.pin-cert",
			"--cert-pem or --cert-thumbprint is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// Fetch the current app, update the thumbprint, and patch.
	app, err := iamClient.App().Get(ctx, globalTenantID, clientID)
	if err != nil {
		return formatError(err)
	}

	if app.TokenEndpointAuthMethod != sdkiam.TokenEndpointAuthSelfSignedTLSClientAuth &&
		app.TokenEndpointAuthMethod != sdkiam.TokenEndpointAuthTLSClientAuth {
		return pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.pin-cert",
			"client is not an mTLS client (auth method: "+app.TokenEndpointAuthMethod.String()+")")
	}

	_, err = iamClient.App().Update(ctx, globalTenantID, clientID, &sdkiam.UpdateAppRequest{
		MTLSCertThumbprint: thumbprint,
	})
	if err != nil {
		return formatError(err)
	}

	fmt.Printf("Certificate thumbprint pinned successfully for client '%s'\n", clientID)
	fmt.Printf("  New thumbprint (SHA-256): %s\n", thumbprint)
	return nil
}
