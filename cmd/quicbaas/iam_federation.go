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

// Package main provides IAM federation CLI commands for QuicBaaS.
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
)

var (
	// Provider command flags.
	providerID        string
	providerName      string
	providerType      string
	providerClientID  string
	providerSecret    string
	providerIssuerURL string
	providerScopes    []string
	providerEnabled   bool
	autoProvision     bool
	allowLinking      bool
	defaultRoles      []string
	defaultGroups     []string
	providerPreset    string
	providerDomain    string

	// Identity command flags.
	identityUserID     string
	identityProviderID string
	identityAuthCode   string
)

// ProviderPreset defines a preset configuration for a known identity provider.
type ProviderPreset struct {
	Name           string   // Display name for the provider
	Type           string   // Provider type (oidc, oauth2)
	IssuerURL      string   // Issuer URL (may contain {domain} placeholder)
	Scopes         []string // Default scopes
	RequiresDomain bool     // Whether this provider requires a domain/tenant parameter
	DomainHelp     string   // Help text for domain parameter
}

// ProviderPresets contains preset configurations for known identity providers.
var ProviderPresets = map[string]ProviderPreset{
	"google": {
		Name:           "Google",
		Type:           "oidc",
		IssuerURL:      "https://accounts.google.com",
		Scopes:         []string{"openid", "email", "profile"},
		RequiresDomain: false,
	},
	"microsoft": {
		Name:           "Microsoft",
		Type:           "oidc",
		IssuerURL:      "https://login.microsoftonline.com/{domain}/v2.0",
		Scopes:         []string{"openid", "email", "profile", "User.Read"},
		RequiresDomain: true,
		DomainHelp:     "Azure AD tenant ID (e.g., 'common', 'organizations', or your tenant GUID)",
	},
	"azure": {
		Name:           "Azure AD",
		Type:           "oidc",
		IssuerURL:      "https://login.microsoftonline.com/{domain}/v2.0",
		Scopes:         []string{"openid", "email", "profile", "User.Read"},
		RequiresDomain: true,
		DomainHelp:     "Azure AD tenant ID (e.g., 'common', 'organizations', or your tenant GUID)",
	},
	"okta": {
		Name:           "Okta",
		Type:           "oidc",
		IssuerURL:      "https://{domain}.okta.com",
		Scopes:         []string{"openid", "email", "profile", "groups"},
		RequiresDomain: true,
		DomainHelp:     "Okta subdomain (e.g., 'acme' for acme.okta.com)",
	},
	"auth0": {
		Name:           "Auth0",
		Type:           "oidc",
		IssuerURL:      "https://{domain}.auth0.com",
		Scopes:         []string{"openid", "email", "profile"},
		RequiresDomain: true,
		DomainHelp:     "Auth0 tenant domain (e.g., 'acme' for acme.auth0.com)",
	},
	"github": {
		Name:           "GitHub",
		Type:           "oauth2",
		IssuerURL:      "https://github.com",
		Scopes:         []string{"read:user", "user:email"},
		RequiresDomain: false,
	},
	"gitlab": {
		Name:           "GitLab",
		Type:           "oidc",
		IssuerURL:      "https://gitlab.com",
		Scopes:         []string{"openid", "email", "profile", "read_user"},
		RequiresDomain: false,
	},
	"keycloak": {
		Name:           "Keycloak",
		Type:           "oidc",
		IssuerURL:      "https://{domain}/realms/master",
		Scopes:         []string{"openid", "email", "profile"},
		RequiresDomain: true,
		DomainHelp:     "Keycloak server URL and realm (e.g., 'keycloak.example.com/realms/myrealm')",
	},
}

// newIAMProviderCommand creates the provider command group.
func newIAMProviderCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "External identity provider management commands",
		Long: `Manage external identity providers for OIDC/OAuth2/SAML federation.

External providers allow users to authenticate using third-party identity
providers like Google, Okta, Azure AD, or any OIDC-compliant provider.

Features include:
- OIDC Consumer federation with automatic discovery
- JIT (Just-In-Time) user provisioning
- Account linking to existing QuicBaaS users
- Claim mapping for attribute synchronization

All provider commands require the --tenant flag to specify the tenant context.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMProviderCreateCommand())
	cmd.AddCommand(newIAMProviderGetCommand())
	cmd.AddCommand(newIAMProviderUpdateCommand())
	cmd.AddCommand(newIAMProviderDeleteCommand())
	cmd.AddCommand(newIAMProviderListCommand())
	cmd.AddCommand(newIAMProviderOperatorCommand())

	return cmd
}

// operatorTenantID is the reserved control-plane / system tenant that hosts
// operator (staff) identities. It mirrors the server-side reserved "system"
// tenant: an operator SSO IdP is, by definition, system-tenant federation, and
// the server gates its registration on a bootstrap admin already existing. The
// CLI forces this tenant so an operator IdP cannot be accidentally registered
// against a data-plane tenant.
const operatorTenantID = "system"

// newIAMProviderOperatorCommand creates the operator-SSO provider registration
// command. It is a thin convenience over `provider create` that forces the
// system tenant so staff/operator IdPs land on the control-plane tenant. The
// server enforces the bootstrap-ordering invariant: this registration fails
// until the bootstrap admin (the always-available local-factor break-glass
// login) exists.
func newIAMProviderOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Register an operator (system-tenant) SSO identity provider",
		Long: `Register an external identity provider for OPERATOR / staff SSO.

This is a convenience over 'provider create' that forces the reserved control
plane "system" tenant, so the IdP authenticates operators against the control
plane rather than a data-plane tenant. It uses the same provider presets and
flags as 'provider create'.

The server enforces the bootstrap-ordering invariant: an operator IdP can only
be registered once the bootstrap admin exists. The bootstrap admin's
local-factor login (mTLS/WebAuthn/PIV) is the always-available break-glass path
and never depends on federation, so a misconfigured or unreachable IdP can never
lock the control plane out.

Examples:
  # Register an Okta operator IdP using the preset
  quicbaas iam provider operator     --provider okta --domain acme     --client-id "0oaxxxxx" --client-secret "secret"     --auto-provision --default-roles operator

  # Register a custom OIDC operator IdP
  quicbaas iam provider operator     --provider-id corp-idp --name "Corp IdP" --type oidc     --client-id "client-id" --client-secret "secret"     --issuer-url "https://idp.example.com"     --scopes openid,email,profile --auto-provision`,
		RunE: runIAMProviderOperatorCommand,
	}

	// Mirror the create command's flags so operators get the same ergonomics.
	cmd.Flags().StringVar(&providerPreset, "provider", "", "Use preset configuration (google, microsoft, azure, okta, auth0, github, gitlab, keycloak)")
	cmd.Flags().StringVar(&providerDomain, "domain", "", "Domain/tenant for provider (required for okta, auth0, azure, keycloak)")
	cmd.Flags().StringVar(&providerID, "provider-id", "", "Unique provider identifier (auto-generated from --provider if not set)")
	cmd.Flags().StringVar(&providerName, "name", "", "Provider display name (auto-set from --provider if not set)")
	cmd.Flags().StringVar(&providerType, "type", "", "Provider type: oidc, oauth2, saml (auto-set from --provider if not set)")
	cmd.Flags().StringVar(&providerClientID, "client-id", "", "OAuth2/OIDC client ID (required)")
	cmd.Flags().StringVar(&providerSecret, "client-secret", "", "OAuth2/OIDC client secret")
	cmd.Flags().StringVar(&providerIssuerURL, "issuer-url", "", "OIDC issuer URL (auto-set from --provider if not set)")
	cmd.Flags().StringSliceVar(&providerScopes, "scopes", nil, "OAuth2 scopes (auto-set from --provider if not set)")
	cmd.Flags().BoolVar(&autoProvision, "auto-provision", false, "Enable JIT user provisioning")
	cmd.Flags().BoolVar(&allowLinking, "allow-linking", false, "Allow linking to existing accounts by email")
	cmd.Flags().StringSliceVar(&defaultRoles, "default-roles", nil, "Default roles for auto-provisioned operators")
	cmd.Flags().StringSliceVar(&defaultGroups, "default-groups", nil, "Default groups for auto-provisioned operators")

	return cmd
}

// runIAMProviderOperatorCommand registers an operator (system-tenant) SSO IdP.
// It forces the reserved system tenant, then delegates to the shared provider
// registration path so it shares validation, presets, and output formatting
// with 'provider create' (DRY).
func runIAMProviderOperatorCommand(cmd *cobra.Command, args []string) error {
	// Operator SSO is always system-tenant federation. Overriding the global
	// --tenant here means the caller cannot smuggle a data-plane tenant past
	// the operator semantics; the server independently forces / gates this too.
	globalTenantID = operatorTenantID
	return runIAMProviderCreate(cmd, "iam.provider.operator")
}

// newIAMProviderCreateCommand creates the provider create command.
func newIAMProviderCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new external identity provider",
		Long: `Create a new external identity provider for OIDC/OAuth2/SAML federation.

Use --provider to quickly configure a known provider with preset settings.
Supported presets: google, microsoft, azure, okta, auth0, github, gitlab, keycloak

For OIDC providers, the issuer URL is used for automatic discovery of endpoints.
The client ID and secret are obtained from your identity provider's console.

Examples:
  # Create Google provider using preset (simplest approach)
  quicbaas iam provider create --tenant acme-corp \
    --provider google \
    --client-id "xxx.apps.googleusercontent.com" \
    --client-secret "secret" \
    --auto-provision --allow-linking

  # Create Okta provider using preset (requires --domain)
  quicbaas iam provider create --tenant acme-corp \
    --provider okta --domain acme \
    --client-id "0oaxxxxx" \
    --client-secret "secret" \
    --auto-provision

  # Create Azure AD provider using preset
  quicbaas iam provider create --tenant acme-corp \
    --provider azure --domain "your-tenant-id" \
    --client-id "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" \
    --client-secret "secret" \
    --auto-provision --allow-linking

  # Create custom OIDC provider (manual configuration)
  quicbaas iam provider create --tenant acme-corp \
    --provider-id custom-idp --name "Custom IdP" --type oidc \
    --client-id "client-id" --client-secret "secret" \
    --issuer-url "https://idp.example.com" \
    --scopes openid,email,profile \
    --auto-provision`,
		RunE: runIAMProviderCreateCommand,
	}

	// Provider preset flags
	cmd.Flags().StringVar(&providerPreset, "provider", "", "Use preset configuration (google, microsoft, azure, okta, auth0, github, gitlab, keycloak)")
	cmd.Flags().StringVar(&providerDomain, "domain", "", "Domain/tenant for provider (required for okta, auth0, azure, keycloak)")

	// Manual configuration flags
	cmd.Flags().StringVar(&providerID, "provider-id", "", "Unique provider identifier (auto-generated from --provider if not set)")
	cmd.Flags().StringVar(&providerName, "name", "", "Provider display name (auto-set from --provider if not set)")
	cmd.Flags().StringVar(&providerType, "type", "", "Provider type: oidc, oauth2, saml (auto-set from --provider if not set)")
	cmd.Flags().StringVar(&providerClientID, "client-id", "", "OAuth2/OIDC client ID (required)")
	cmd.Flags().StringVar(&providerSecret, "client-secret", "", "OAuth2/OIDC client secret")
	cmd.Flags().StringVar(&providerIssuerURL, "issuer-url", "", "OIDC issuer URL (auto-set from --provider if not set)")
	cmd.Flags().StringSliceVar(&providerScopes, "scopes", nil, "OAuth2 scopes (auto-set from --provider if not set)")
	cmd.Flags().BoolVar(&autoProvision, "auto-provision", false, "Enable JIT user provisioning")
	cmd.Flags().BoolVar(&allowLinking, "allow-linking", false, "Allow linking to existing accounts by email")
	cmd.Flags().StringSliceVar(&defaultRoles, "default-roles", nil, "Default roles for auto-provisioned users")
	cmd.Flags().StringSliceVar(&defaultGroups, "default-groups", nil, "Default groups for auto-provisioned users")

	return cmd
}

// runIAMProviderCreateCommand executes the provider create command.
func runIAMProviderCreateCommand(cmd *cobra.Command, args []string) error {
	return runIAMProviderCreate(cmd, "iam.provider.create")
}

// runIAMProviderCreate is the shared provider-registration path used by both
// 'provider create' and 'provider operator'. The op string is threaded into
// returned errors so each subcommand reports its own operation context.
func runIAMProviderCreate(cmd *cobra.Command, op string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, op, "--tenant flag is required")
	}

	// Apply provider preset if specified
	if providerPreset != "" {
		if err := applyProviderPreset(cmd); err != nil {
			return err
		}
	}

	// Validate that either --provider or --provider-id is specified
	if providerPreset == "" && providerID == "" {
		return errors.Newf(errors.ErrInvalidInput, op, "either --provider or --provider-id is required")
	}

	// Set default type if not specified
	if providerType == "" {
		providerType = "oidc"
	}

	// Validate OIDC-specific requirements
	if providerType == "oidc" || providerType == "oauth2" {
		if providerClientID == "" {
			return errors.Newf(errors.ErrInvalidInput, op, "--client-id is required for OIDC/OAuth2 providers")
		}
		if providerIssuerURL == "" && providerType == "oidc" {
			return errors.Newf(errors.ErrInvalidInput, op, "--issuer-url is required for OIDC providers")
		}
	}

	// Set default scopes if not specified
	if len(providerScopes) == 0 {
		providerScopes = []string{"openid", "email", "profile"}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create provider
	log.Infof("Creating provider: %s in tenant: %s", providerID, globalTenantID)
	provider, err := iamClient.CreateProvider(ctx, &cli.CreateProviderOptions{
		TenantID:      globalTenantID,
		ProviderID:    providerID,
		Name:          providerName,
		Type:          providerType,
		ClientID:      providerClientID,
		ClientSecret:  providerSecret,
		IssuerURL:     providerIssuerURL,
		Scopes:        providerScopes,
		AutoProvision: autoProvision,
		AllowLinking:  allowLinking,
		DefaultRoles:  defaultRoles,
		DefaultGroups: defaultGroups,
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
		return encoder.Encode(provider)

	default:
		fmt.Println("Provider created successfully:")
		fmt.Printf("  ID: %s\n", provider.ID)
		fmt.Printf("  Name: %s\n", provider.Name)
		fmt.Printf("  Type: %s\n", provider.Type)
		fmt.Printf("  Enabled: %v\n", provider.Enabled)
		fmt.Printf("  Issuer URL: %s\n", provider.IssuerURL)
		fmt.Printf("  Auto-Provision: %v\n", provider.AutoProvision)
		fmt.Printf("  Allow Linking: %v\n", provider.AllowLinking)
		if len(provider.DefaultRoles) > 0 {
			fmt.Printf("  Default Roles: %s\n", strings.Join(provider.DefaultRoles, ", "))
		}
		if len(provider.DefaultGroups) > 0 {
			fmt.Printf("  Default Groups: %s\n", strings.Join(provider.DefaultGroups, ", "))
		}
		fmt.Printf("  Created: %s\n", provider.Created.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMProviderGetCommand creates the provider get command.
func newIAMProviderGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get provider details",
		Long: `Retrieve detailed information about a specific external identity provider.

Examples:
  # Get provider details
  quicbaas iam provider get --tenant acme-corp --provider-id google

  # Get provider as JSON
  quicbaas iam provider get --tenant acme-corp --provider-id google --output json`,
		RunE: runIAMProviderGetCommand,
	}

	cmd.Flags().StringVar(&providerID, "provider-id", "", "Provider identifier (required)")
	cmd.MarkFlagRequired("provider-id")

	return cmd
}

// runIAMProviderGetCommand executes the provider get command.
func runIAMProviderGetCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.provider.get", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get provider
	log.Infof("Getting provider: %s in tenant: %s", providerID, globalTenantID)
	provider, err := iamClient.GetProvider(ctx, globalTenantID, providerID)
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
		return encoder.Encode(provider)

	default:
		fmt.Println("Provider Details:")
		fmt.Printf("  ID: %s\n", provider.ID)
		fmt.Printf("  Name: %s\n", provider.Name)
		fmt.Printf("  Type: %s\n", provider.Type)
		fmt.Printf("  Enabled: %v\n", provider.Enabled)
		fmt.Printf("  Client ID: %s\n", provider.ClientID)
		fmt.Printf("  Issuer URL: %s\n", provider.IssuerURL)
		fmt.Printf("  Scopes: %s\n", strings.Join(provider.Scopes, ", "))
		fmt.Printf("  Auto-Provision: %v\n", provider.AutoProvision)
		fmt.Printf("  Allow Linking: %v\n", provider.AllowLinking)
		if len(provider.DefaultRoles) > 0 {
			fmt.Printf("  Default Roles: %s\n", strings.Join(provider.DefaultRoles, ", "))
		}
		if len(provider.DefaultGroups) > 0 {
			fmt.Printf("  Default Groups: %s\n", strings.Join(provider.DefaultGroups, ", "))
		}
		fmt.Printf("  Created: %s\n", provider.Created.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated: %s\n", provider.Updated.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMProviderUpdateCommand creates the provider update command.
func newIAMProviderUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update provider settings",
		Long: `Update an existing external identity provider's settings.

At least one flag must be specified to update the provider.

Examples:
  # Update provider name
  quicbaas iam provider update --tenant acme-corp --provider-id google --name "Google SSO"

  # Enable/disable provider
  quicbaas iam provider update --tenant acme-corp --provider-id google --enabled=false

  # Update auto-provision and linking settings
  quicbaas iam provider update --tenant acme-corp --provider-id google \
    --auto-provision --allow-linking

  # Rotate client secret
  quicbaas iam provider update --tenant acme-corp --provider-id google \
    --client-secret "new-secret"`,
		RunE: runIAMProviderUpdateCommand,
	}

	cmd.Flags().StringVar(&providerID, "provider-id", "", "Provider identifier (required)")
	cmd.Flags().StringVar(&providerName, "name", "", "New provider name")
	cmd.Flags().StringVar(&providerClientID, "client-id", "", "New OAuth2/OIDC client ID")
	cmd.Flags().StringVar(&providerSecret, "client-secret", "", "New OAuth2/OIDC client secret")
	cmd.Flags().StringVar(&providerIssuerURL, "issuer-url", "", "New OIDC issuer URL")
	cmd.Flags().StringSliceVar(&providerScopes, "scopes", nil, "New OAuth2 scopes")
	cmd.Flags().BoolVar(&providerEnabled, "enabled", true, "Enable/disable provider")
	cmd.Flags().BoolVar(&autoProvision, "auto-provision", false, "Enable JIT user provisioning")
	cmd.Flags().BoolVar(&allowLinking, "allow-linking", false, "Allow linking to existing accounts")
	cmd.Flags().StringSliceVar(&defaultRoles, "default-roles", nil, "Default roles for auto-provisioned users")
	cmd.Flags().StringSliceVar(&defaultGroups, "default-groups", nil, "Default groups for auto-provisioned users")

	cmd.MarkFlagRequired("provider-id")

	return cmd
}

// runIAMProviderUpdateCommand executes the provider update command.
func runIAMProviderUpdateCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.provider.update", "--tenant flag is required")
	}

	// Check if at least one update flag is provided
	hasUpdate := cmd.Flags().Changed("name") || cmd.Flags().Changed("client-id") ||
		cmd.Flags().Changed("client-secret") || cmd.Flags().Changed("issuer-url") ||
		cmd.Flags().Changed("scopes") || cmd.Flags().Changed("enabled") ||
		cmd.Flags().Changed("auto-provision") || cmd.Flags().Changed("allow-linking") ||
		cmd.Flags().Changed("default-roles") || cmd.Flags().Changed("default-groups")

	if !hasUpdate {
		return errors.Newf(errors.ErrInvalidInput, "iam.provider.update", "at least one update flag must be specified")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build update options
	opts := &cli.UpdateProviderOptions{
		TenantID:   globalTenantID,
		ProviderID: providerID,
	}

	if cmd.Flags().Changed("name") {
		opts.Name = &providerName
	}
	if cmd.Flags().Changed("client-id") {
		opts.ClientID = &providerClientID
	}
	if cmd.Flags().Changed("client-secret") {
		opts.ClientSecret = &providerSecret
	}
	if cmd.Flags().Changed("issuer-url") {
		opts.IssuerURL = &providerIssuerURL
	}
	if cmd.Flags().Changed("scopes") {
		opts.Scopes = providerScopes
	}
	if cmd.Flags().Changed("enabled") {
		opts.Enabled = &providerEnabled
	}
	if cmd.Flags().Changed("auto-provision") {
		opts.AutoProvision = &autoProvision
	}
	if cmd.Flags().Changed("allow-linking") {
		opts.AllowLinking = &allowLinking
	}
	if cmd.Flags().Changed("default-roles") {
		opts.DefaultRoles = defaultRoles
	}
	if cmd.Flags().Changed("default-groups") {
		opts.DefaultGroups = defaultGroups
	}

	// Update provider
	log.Infof("Updating provider: %s in tenant: %s", providerID, globalTenantID)
	provider, err := iamClient.UpdateProvider(ctx, opts)
	if err != nil {
		return formatError(err)
	}

	fmt.Println("Provider updated successfully:")
	fmt.Printf("  ID: %s\n", provider.ID)
	fmt.Printf("  Name: %s\n", provider.Name)
	fmt.Printf("  Type: %s\n", provider.Type)
	fmt.Printf("  Enabled: %v\n", provider.Enabled)
	fmt.Printf("  Auto-Provision: %v\n", provider.AutoProvision)
	fmt.Printf("  Allow Linking: %v\n", provider.AllowLinking)

	return nil
}

// newIAMProviderDeleteCommand creates the provider delete command.
func newIAMProviderDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an external identity provider",
		Long: `Delete an external identity provider from the tenant.

WARNING: This operation will disconnect all federated identities linked to this
provider. Users who only authenticate via this provider will lose access.

Examples:
  # Delete provider with confirmation
  quicbaas iam provider delete --tenant acme-corp --provider-id google

  # Delete provider without confirmation
  quicbaas iam provider delete --tenant acme-corp --provider-id google --yes`,
		RunE: runIAMProviderDeleteCommand,
	}

	cmd.Flags().StringVar(&providerID, "provider-id", "", "Provider identifier (required)")
	cmd.MarkFlagRequired("provider-id")

	return cmd
}

// runIAMProviderDeleteCommand executes the provider delete command.
func runIAMProviderDeleteCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.provider.delete", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("WARNING: Deleting provider '%s' will disconnect all federated identities.\n", providerID)
		fmt.Printf("This action cannot be undone. Are you sure? (y/N): ")
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
	defer iamClient.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Delete provider
	log.Infof("Deleting provider: %s from tenant: %s", providerID, globalTenantID)
	if err := iamClient.DeleteProvider(ctx, globalTenantID, providerID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Provider '%s' deleted successfully\n", providerID)
	return nil
}

// newIAMProviderListCommand creates the provider list command.
func newIAMProviderListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List external identity providers",
		Long: `List all external identity providers configured for a tenant.

Examples:
  # List all providers in tenant
  quicbaas iam provider list --tenant acme-corp

  # List providers as JSON
  quicbaas iam provider list --tenant acme-corp --output json`,
		RunE: runIAMProviderListCommand,
	}

	return cmd
}

// runIAMProviderListCommand executes the provider list command.
func runIAMProviderListCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.provider.list", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List providers
	log.Infof("Listing providers in tenant: %s", globalTenantID)
	providers, err := iamClient.ListProviders(ctx, globalTenantID)
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
		return encoder.Encode(providers)

	default:
		if len(providers) == 0 {
			fmt.Println("No external identity providers found")
			return nil
		}

		fmt.Printf("%-20s %-30s %-10s %-8s %-12s %-12s %s\n",
			"ID", "NAME", "TYPE", "ENABLED", "AUTO-PROV", "LINKING", "CREATED")
		fmt.Println(strings.Repeat("-", 120))
		for _, p := range providers {
			fmt.Printf("%-20s %-30s %-10s %-8v %-12v %-12v %s\n",
				p.ID,
				truncateString(p.Name, 30),
				p.Type,
				p.Enabled,
				p.AutoProvision,
				p.AllowLinking,
				p.Created.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d providers\n", len(providers))
	}

	return nil
}

// ============================================================================
// Identity Commands
// ============================================================================

// newIAMIdentityCommand creates the identity command group.
func newIAMIdentityCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identity",
		Short: "Federated identity management commands",
		Long: `Manage federated identity links between QuicBaaS users and external providers.

Federated identities allow users to:
- Link their QuicBaaS account to external identity providers
- Authenticate using their preferred identity provider
- Maintain multiple linked identities for flexibility

All identity commands require the --tenant flag to specify the tenant context.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMIdentityLinkCommand())
	cmd.AddCommand(newIAMIdentityUnlinkCommand())
	cmd.AddCommand(newIAMIdentityListCommand())

	return cmd
}

// newIAMIdentityLinkCommand creates the identity link command.
func newIAMIdentityLinkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Link an external identity to a user",
		Long: `Link an external identity provider account to an existing QuicBaaS user.

The auth-code is obtained by initiating the OAuth flow and capturing the
authorization code from the callback. This is typically done through
a web-based flow.

Examples:
  # Link Google identity to user
  quicbaas iam identity link --tenant acme-corp \
    --user-id user_abc123 --provider-id google \
    --auth-code "4/0AfJohXnB..."

  # Link Okta identity to user
  quicbaas iam identity link --tenant acme-corp \
    --user-id user_abc123 --provider-id okta \
    --auth-code "code_from_callback"`,
		RunE: runIAMIdentityLinkCommand,
	}

	cmd.Flags().StringVar(&identityUserID, "user-id", "", "User ID to link identity to (required)")
	cmd.Flags().StringVar(&identityProviderID, "provider-id", "", "Provider ID to link (required)")
	cmd.Flags().StringVar(&identityAuthCode, "auth-code", "", "Authorization code from OAuth callback (required)")

	cmd.MarkFlagRequired("user-id")
	cmd.MarkFlagRequired("provider-id")
	cmd.MarkFlagRequired("auth-code")

	return cmd
}

// runIAMIdentityLinkCommand executes the identity link command.
func runIAMIdentityLinkCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.identity.link", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Link identity
	log.Infof("Linking identity for user: %s with provider: %s", identityUserID, identityProviderID)
	identity, err := iamClient.LinkIdentity(ctx, &cli.LinkIdentityOptions{
		TenantID:   globalTenantID,
		UserID:     identityUserID,
		ProviderID: identityProviderID,
		AuthCode:   identityAuthCode,
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
		return encoder.Encode(identity)

	default:
		fmt.Println("Identity linked successfully:")
		fmt.Printf("  ID: %s\n", identity.ID)
		fmt.Printf("  User ID: %s\n", identity.UserID)
		fmt.Printf("  Provider ID: %s\n", identity.ProviderID)
		fmt.Printf("  External ID: %s\n", identity.ExternalID)
		if identity.ExternalEmail != "" {
			fmt.Printf("  External Email: %s\n", identity.ExternalEmail)
		}
		if identity.ExternalName != "" {
			fmt.Printf("  External Name: %s\n", identity.ExternalName)
		}
		fmt.Printf("  Created: %s\n", identity.Created.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMIdentityUnlinkCommand creates the identity unlink command.
func newIAMIdentityUnlinkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlink",
		Short: "Unlink an external identity from a user",
		Long: `Unlink an external identity provider account from a QuicBaaS user.

After unlinking, the user will no longer be able to authenticate using
that external identity provider for this account.

Examples:
  # Unlink Google identity from user
  quicbaas iam identity unlink --tenant acme-corp \
    --user-id user_abc123 --provider-id google

  # Unlink with confirmation skip
  quicbaas iam identity unlink --tenant acme-corp \
    --user-id user_abc123 --provider-id google --yes`,
		RunE: runIAMIdentityUnlinkCommand,
	}

	cmd.Flags().StringVar(&identityUserID, "user-id", "", "User ID to unlink identity from (required)")
	cmd.Flags().StringVar(&identityProviderID, "provider-id", "", "Provider ID to unlink (required)")

	cmd.MarkFlagRequired("user-id")
	cmd.MarkFlagRequired("provider-id")

	return cmd
}

// runIAMIdentityUnlinkCommand executes the identity unlink command.
func runIAMIdentityUnlinkCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.identity.unlink", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to unlink provider '%s' from user '%s'? (y/N): ",
			identityProviderID, identityUserID)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Unlink cancelled")
			return nil
		}
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Unlink identity
	log.Infof("Unlinking identity for user: %s from provider: %s", identityUserID, identityProviderID)
	if err := iamClient.UnlinkIdentity(ctx, globalTenantID, identityUserID, identityProviderID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Identity unlinked successfully: user '%s' from provider '%s'\n",
		identityUserID, identityProviderID)
	return nil
}

// newIAMIdentityListCommand creates the identity list command.
func newIAMIdentityListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List user's linked identities",
		Long: `List all external identities linked to a user account.

Examples:
  # List linked identities for a user
  quicbaas iam identity list --tenant acme-corp --user-id user_abc123

  # List identities as JSON
  quicbaas iam identity list --tenant acme-corp --user-id user_abc123 --output json`,
		RunE: runIAMIdentityListCommand,
	}

	cmd.Flags().StringVar(&identityUserID, "user-id", "", "User ID to list identities for (required)")
	cmd.MarkFlagRequired("user-id")

	return cmd
}

// runIAMIdentityListCommand executes the identity list command.
func runIAMIdentityListCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.identity.list", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List identities
	log.Infof("Listing identities for user: %s in tenant: %s", identityUserID, globalTenantID)
	identities, err := iamClient.ListUserIdentities(ctx, globalTenantID, identityUserID)
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
		return encoder.Encode(identities)

	default:
		if len(identities) == 0 {
			fmt.Printf("No linked identities found for user '%s'\n", identityUserID)
			return nil
		}

		fmt.Printf("%-25s %-20s %-35s %-30s %s\n",
			"ID", "PROVIDER", "EXTERNAL ID", "EXTERNAL EMAIL", "LAST LOGIN")
		fmt.Println(strings.Repeat("-", 140))
		for _, i := range identities {
			lastLogin := "Never"
			if !i.LastLogin.IsZero() {
				lastLogin = i.LastLogin.Format("2006-01-02 15:04")
			}
			fmt.Printf("%-25s %-20s %-35s %-30s %s\n",
				truncateString(i.ID, 25),
				i.ProviderID,
				truncateString(i.ExternalID, 35),
				truncateString(i.ExternalEmail, 30),
				lastLogin)
		}
		fmt.Printf("\nTotal: %d linked identities\n", len(identities))
	}

	return nil
}

// truncateString is defined in iam_audit.go

// applyProviderPreset applies preset configuration values for known providers.
// It sets provider ID, name, type, issuer URL, and scopes from the preset.
// User-specified values take precedence over preset values.
func applyProviderPreset(cmd *cobra.Command) error {
	preset, ok := ProviderPresets[strings.ToLower(providerPreset)]
	if !ok {
		validPresets := getValidPresetNames()
		return errors.Newf(errors.ErrInvalidInput, "iam.provider.create",
			"unknown provider preset '%s'. Valid presets: %s", providerPreset, strings.Join(validPresets, ", "))
	}

	// Validate domain requirement
	if preset.RequiresDomain && providerDomain == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.provider.create",
			"--domain is required for provider '%s'. %s", providerPreset, preset.DomainHelp)
	}

	// Apply preset values if not explicitly set by user
	if providerID == "" {
		providerID = strings.ToLower(providerPreset)
	}

	if providerName == "" {
		providerName = preset.Name
	}

	if !cmd.Flags().Changed("type") {
		providerType = preset.Type
	}

	if !cmd.Flags().Changed("issuer-url") {
		issuer := preset.IssuerURL
		if preset.RequiresDomain {
			issuer = strings.ReplaceAll(issuer, "{domain}", providerDomain)
		}
		providerIssuerURL = issuer
	}

	if !cmd.Flags().Changed("scopes") {
		providerScopes = preset.Scopes
	}

	return nil
}

// getValidPresetNames returns a sorted list of valid provider preset names.
func getValidPresetNames() []string {
	names := make([]string, 0, len(ProviderPresets))
	for name := range ProviderPresets {
		names = append(names, name)
	}
	// Sort for consistent output
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// ListProviderPresets returns information about all available provider presets.
// This is useful for CLI help and documentation.
func ListProviderPresets() []ProviderPreset {
	presets := make([]ProviderPreset, 0, len(ProviderPresets))
	for _, preset := range ProviderPresets {
		presets = append(presets, preset)
	}
	return presets
}
