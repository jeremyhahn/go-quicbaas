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
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	sdkiam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// oauth2ClientFlags holds flag values for the oauth2 client subcommands.
var (
	oauth2ClientName           string
	oauth2ClientAuthMethod     string
	oauth2ClientGrantTypes     []string
	oauth2ClientResponseTypes  []string
	oauth2ClientRedirectURIs   []string
	oauth2ClientResourcePrefix string
	oauth2ClientCertPEM        string
	oauth2ClientThumbprint     string
	oauth2ClientScopes         []string
)

// UnsupportedGrantTypeError indicates an unrecognized --grant-type value. The
// SDK's ParseGrantType falls back to authorization_code for unknown input, which
// would silently register a client with the wrong flow; the CLI rejects it
// instead.
type UnsupportedGrantTypeError struct {
	GrantType string
}

// Error implements the error interface.
func (e *UnsupportedGrantTypeError) Error() string {
	return "unsupported --grant-type: " + e.GrantType +
		" (valid: " + strings.Join(oauth2GrantTypeNames(), ", ") + ")"
}

// oauth2GrantTypes is the canonical grant-type lookup used to validate
// --grant-type. Map-based dispatch keeps validation O(1).
var oauth2GrantTypes = map[string]sdkiam.GrantType{
	"authorization_code": sdkiam.GrantTypeAuthorizationCode,
	"client_credentials": sdkiam.GrantTypeClientCredentials,
	"refresh_token":      sdkiam.GrantTypeRefreshToken,
	"urn:ietf:params:oauth:grant-type:device_code": sdkiam.GrantTypeDeviceCode,
	"implicit": sdkiam.GrantTypeImplicit,
	"password": sdkiam.GrantTypePassword,
}

// oauth2GrantTypeNames returns the valid grant-type names in a stable order for
// help text and error messages.
func oauth2GrantTypeNames() []string {
	names := make([]string, 0, len(oauth2GrantTypes))
	for name := range oauth2GrantTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// parseOAuth2GrantTypes validates every --grant-type value against the
// canonical vocabulary.
func parseOAuth2GrantTypes(values []string) ([]sdkiam.GrantType, error) {
	if len(values) == 0 {
		return nil, nil
	}
	parsed := make([]sdkiam.GrantType, 0, len(values))
	for _, v := range values {
		grant, ok := oauth2GrantTypes[strings.ToLower(strings.TrimSpace(v))]
		if !ok {
			return nil, &UnsupportedGrantTypeError{GrantType: v}
		}
		parsed = append(parsed, grant)
	}
	return parsed, nil
}

// newOAuth2Command creates the top-level oauth2 command group.
func newOAuth2Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth2",
		Short: "OAuth2 client and token management",
		Long:  "Manage OAuth2 clients and tokens for QuicBaaS service-account access.",
	}

	// The oauth2 verbs render output and confirm destructive actions through
	// the same helpers as the `iam` tree; registering the flags here is what
	// makes `--output json` / `--yes` reachable from this command group.
	cmd.PersistentFlags().StringVar(&iamOutputFormat, "output", "table", "Output format (json, yaml, table)")
	cmd.PersistentFlags().BoolVarP(&iamForce, "yes", "y", false, "Skip confirmation prompts")

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
	cmd.AddCommand(newOAuth2ClientUpdateCommand())
	cmd.AddCommand(newOAuth2ClientRotateSecretCommand())
	cmd.AddCommand(newOAuth2ClientDeleteCommand())
	cmd.AddCommand(newOAuth2ClientPinCertCommand())

	return cmd
}

// newOAuth2ClientRegisterCommand creates the register subcommand. "create" is
// accepted as an alias so the verb matches the noun-verb shape used elsewhere
// in the CLI and in the REST API.
func newOAuth2ClientRegisterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "register",
		Aliases: []string{"create"},
		Short:   "Register a new OAuth2 client",
		Long: `Register a new OAuth2 client in a tenant.

The generated client_id is printed on success. Secret-based clients also print
their client_secret exactly once — the server stores only a hash, so a lost
secret can only be replaced with "oauth2 client rotate-secret".

Examples:
  # Machine-to-machine client (client_credentials)
  quicbaas oauth2 client create --tenant acme --name "my-service" \
    --grant-type client_credentials

  # Web application (authorization code + refresh)
  quicbaas oauth2 client create --tenant acme --name "portal" \
    --grant-type authorization_code --grant-type refresh_token \
    --redirect-uri https://portal.acme.com/callback \
    --response-type code

  # Register a self-signed mTLS client
  quicbaas oauth2 client register --tenant acme --name "gpu-agent" \
    --auth-method self_signed_tls_client_auth --cert-pem /path/to/client.pem

  # Register a PKI mTLS client by thumbprint
  quicbaas oauth2 client register --tenant acme --name "fleet-agent" \
    --auth-method tls_client_auth --cert-pem /path/to/client.pem

  # Scope the client to a resource namespace
  quicbaas oauth2 client register --tenant acme --name "order-service" \
    --resource-prefix "orders://" --scope read --scope write`,
		RunE: runOAuth2ClientRegisterCommand,
	}

	addOAuth2ClientMetadataFlags(cmd)
	cmd.Flags().StringVar(&oauth2ClientAuthMethod, "auth-method", "client_secret_basic",
		"Token endpoint auth method: client_secret_basic, client_secret_post, tls_client_auth, self_signed_tls_client_auth")
	cmd.Flags().StringVar(&oauth2ClientCertPEM, "cert-pem", "",
		"Path to PEM-encoded client certificate (required for tls_client_auth and self_signed_tls_client_auth)")
	cmd.Flags().StringVar(&oauth2ClientThumbprint, "cert-thumbprint", "",
		"SHA-256 hex thumbprint of the client cert (alternative to --cert-pem for self_signed_tls_client_auth)")

	if err := cmd.MarkFlagRequired("name"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark name flag as required: %v\n", err)
	}

	return cmd
}

// addOAuth2ClientMetadataFlags registers the client metadata flags shared by
// register and update.
func addOAuth2ClientMetadataFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&oauth2ClientName, "name", "", "Client display name")
	// Registered with a nil default on purpose: register defaults an empty list
	// to client_credentials, while update treats it as "leave unchanged". A
	// cobra-level default would make those two meanings indistinguishable.
	cmd.Flags().StringArrayVar(&oauth2ClientGrantTypes, "grant-type", nil,
		"Allowed OAuth2 grant type (repeatable, default client_credentials): "+
			strings.Join(oauth2GrantTypeNames(), ", "))
	cmd.Flags().StringArrayVar(&oauth2ClientRedirectURIs, "redirect-uri", nil,
		"Allowed redirect URI (repeatable; required for authorization_code clients)")
	cmd.Flags().StringArrayVar(&oauth2ClientResponseTypes, "response-type", nil,
		"Allowed OAuth2 response type (repeatable, e.g. code)")
	cmd.Flags().StringVar(&oauth2ClientResourcePrefix, "resource-prefix", "",
		"Resource namespace the client owns, used by ABAC policies (e.g. orders://)")
	cmd.Flags().StringArrayVar(&oauth2ClientScopes, "scope", nil, "Allowed scope (repeatable)")
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
		if len(app.GrantTypes) > 0 {
			fmt.Printf("  Grant Types: %s\n", strings.Join(grantTypeNames(app.GrantTypes), ", "))
		}
		if len(app.RedirectURIs) > 0 {
			fmt.Printf("  Redirect URIs: %s\n", strings.Join(app.RedirectURIs, ", "))
		}
		if app.ResourcePrefix != "" {
			fmt.Printf("  Resource Prefix: %s\n", app.ResourcePrefix)
		}
		if len(app.Scopes) > 0 {
			fmt.Printf("  Scopes: %s\n", strings.Join(app.Scopes, ", "))
		}
		if app.MTLSCertThumbprint != "" {
			fmt.Printf("  Cert Thumbprint (SHA-256): %s\n", app.MTLSCertThumbprint)
		}
		printOAuth2ClientSecret(app.ClientSecret)
	}

	return nil
}

// buildRegisterRequest validates flags and constructs the RegisterAppRequest.
func buildRegisterRequest(cmd *cobra.Command) (string, *sdkiam.RegisterAppRequest, error) {
	authMethod := strings.ToLower(strings.TrimSpace(oauth2ClientAuthMethod))

	// A client with no grant type can request nothing; default the common
	// machine-to-machine flow rather than registering an unusable client.
	requested := oauth2ClientGrantTypes
	if len(requested) == 0 {
		requested = []string{"client_credentials"}
	}

	grantTypes, err := parseOAuth2GrantTypes(requested)
	if err != nil {
		return "", nil, err
	}

	req := &sdkiam.RegisterAppRequest{
		Name:           oauth2ClientName,
		GrantTypes:     grantTypes,
		RedirectURIs:   oauth2ClientRedirectURIs,
		ResponseTypes:  oauth2ClientResponseTypes,
		ResourcePrefix: oauth2ClientResourcePrefix,
		Scopes:         oauth2ClientScopes,
	}

	switch authMethod {
	case "client_secret_basic":
		req.TokenEndpointAuthMethod = sdkiam.TokenEndpointAuthClientSecretBasic

	case "client_secret_post":
		req.TokenEndpointAuthMethod = sdkiam.TokenEndpointAuthClientSecretPost

	case "tls_client_auth":
		req.TokenEndpointAuthMethod = sdkiam.TokenEndpointAuthTLSClientAuth
		switch {
		case oauth2ClientCertPEM != "":
			thumbprint, subjectDN, err := certMetadataFromPEM(oauth2ClientCertPEM)
			if err != nil {
				return "", nil, err
			}
			req.MTLSCertThumbprint = thumbprint
			req.MTLSSubjectDN = subjectDN
		case oauth2ClientThumbprint != "":
			req.MTLSCertThumbprint = oauth2ClientThumbprint
		default:
			return "", nil, pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.register",
				"--cert-pem or --cert-thumbprint is required for tls_client_auth")
		}

	case "self_signed_tls_client_auth":
		req.TokenEndpointAuthMethod = sdkiam.TokenEndpointAuthSelfSignedTLSClientAuth
		switch {
		case oauth2ClientCertPEM != "":
			thumbprint, _, err := certMetadataFromPEM(oauth2ClientCertPEM)
			if err != nil {
				return "", nil, err
			}
			req.MTLSCertThumbprint = thumbprint
		case oauth2ClientThumbprint != "":
			req.MTLSCertThumbprint = oauth2ClientThumbprint
		default:
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

// =============================================================================
// Shared Output Helpers
// =============================================================================

// printOAuth2ClientSecret prints a freshly issued client secret exactly once,
// with an unmissable warning. The server persists only a hash of the secret, so
// this is the operator's single opportunity to capture it.
func printOAuth2ClientSecret(secret string) {
	if secret == "" {
		return
	}
	fmt.Printf("  Client Secret: %s\n", secret)
	fmt.Println("\n!! STORE THIS SECRET NOW !!")
	fmt.Println("The server keeps only a hash of the client secret. It cannot be")
	fmt.Println("recovered or displayed again — the only remedy for a lost secret is")
	fmt.Println("`quicbaas oauth2 client rotate-secret <client-id>`, which invalidates")
	fmt.Println("the current one.")
}

// grantTypeNames renders SDK grant types as their RFC 6749 string names.
func grantTypeNames(grants []sdkiam.GrantType) []string {
	names := make([]string, 0, len(grants))
	for _, g := range grants {
		names = append(names, g.String())
	}
	return names
}

// =============================================================================
// Update Command
// =============================================================================

// newOAuth2ClientUpdateCommand creates the update subcommand.
func newOAuth2ClientUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <client-id>",
		Short: "Update an OAuth2 client",
		Long: `Update the metadata of an existing OAuth2 client.

Only the flags you pass are applied; every other field keeps its current value.
Repeatable flags replace the stored list rather than appending to it.

Examples:
  quicbaas oauth2 client update app_abc123 --name "Portal (prod)"
  quicbaas oauth2 client update app_abc123 \
    --redirect-uri https://portal.acme.com/callback \
    --redirect-uri https://portal.acme.com/callback2
  quicbaas oauth2 client update app_abc123 --grant-type client_credentials --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runOAuth2ClientUpdateCommand,
	}

	addOAuth2ClientMetadataFlags(cmd)

	return cmd
}

// runOAuth2ClientUpdateCommand executes the update command.
func runOAuth2ClientUpdateCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	if globalTenantID == "" {
		return pkgerrors.Newf(pkgerrors.ErrInvalidInput, "oauth2.client.update", "--tenant flag is required")
	}

	req, err := buildUpdateRequest(cmd)
	if err != nil {
		return err
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	app, err := iamClient.App().Update(ctx, globalTenantID, clientID, req)
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
		fmt.Printf("OAuth2 client '%s' updated successfully\n", app.ClientID)
		if app.Name != "" {
			fmt.Printf("  Name: %s\n", app.Name)
		}
		if len(app.GrantTypes) > 0 {
			fmt.Printf("  Grant Types: %s\n", strings.Join(grantTypeNames(app.GrantTypes), ", "))
		}
		if len(app.RedirectURIs) > 0 {
			fmt.Printf("  Redirect URIs: %s\n", strings.Join(app.RedirectURIs, ", "))
		}
		if app.ResourcePrefix != "" {
			fmt.Printf("  Resource Prefix: %s\n", app.ResourcePrefix)
		}
	}

	return nil
}

// buildUpdateRequest validates the update flags and constructs the
// UpdateAppRequest, leaving unset fields at their zero value so the server
// preserves them.
func buildUpdateRequest(cmd *cobra.Command) (*sdkiam.UpdateAppRequest, error) {
	grantTypes, err := parseOAuth2GrantTypes(oauth2ClientGrantTypes)
	if err != nil {
		return nil, err
	}

	return &sdkiam.UpdateAppRequest{
		Name:           oauth2ClientName,
		GrantTypes:     grantTypes,
		RedirectURIs:   oauth2ClientRedirectURIs,
		ResponseTypes:  oauth2ClientResponseTypes,
		ResourcePrefix: oauth2ClientResourcePrefix,
		Scopes:         oauth2ClientScopes,
	}, nil
}

// =============================================================================
// Rotate Secret Command
// =============================================================================

// newOAuth2ClientRotateSecretCommand creates the rotate-secret subcommand.
func newOAuth2ClientRotateSecretCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-secret <client-id>",
		Short: "Issue a new client secret",
		Long: `Generate a new client secret for an OAuth2 client and invalidate the old one.

The new secret is printed exactly once. Any deployment still using the previous
secret stops authenticating as soon as this command succeeds.

Examples:
  quicbaas oauth2 client rotate-secret app_abc123 --tenant acme
  quicbaas oauth2 client rotate-secret app_abc123 --tenant acme --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runOAuth2ClientRotateSecretCommand,
	}
}

// runOAuth2ClientRotateSecretCommand executes the rotate-secret command.
func runOAuth2ClientRotateSecretCommand(cmd *cobra.Command, args []string) error {
	clientID := args[0]

	if !iamForce {
		fmt.Printf("Rotating the secret for '%s' immediately invalidates the current one. Continue? (y/N): ", clientID)
		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			fmt.Println("Rotation cancelled")
			return nil
		}
		if strings.ToLower(strings.TrimSpace(response)) != "y" {
			fmt.Println("Rotation cancelled")
			return nil
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := iamClient.App().RotateSecret(ctx, globalTenantID, clientID)
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
		return enc.Encode(resp)
	default:
		fmt.Println("OAuth2 client secret rotated:")
		fmt.Printf("  Client ID: %s\n", resp.ClientID)
		printOAuth2ClientSecret(resp.ClientSecret)
	}

	return nil
}
