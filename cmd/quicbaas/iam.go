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

// Package main provides IAM CLI commands for QuicBaaS.
package main

import (
	"github.com/spf13/cobra"
)

var (
	// IAM command flags - global for all IAM subcommands.
	iamOutputFormat string
	iamForce        bool
)

// newIAMCommand creates the main IAM command group.
func newIAMCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "iam",
		Short: "Identity and Access Management commands",
		Long: `Manage tenants, users, groups, roles, applications, and policies.

The IAM subsystem provides comprehensive identity and access management
capabilities for QuicBaaS, including multi-tenancy, role-based access control,
OAuth 2.0 / OIDC authentication, and attribute-based access control (ABAC).

Examples:
  # Create a tenant
  quicbaas iam tenant create acme-corp --name "Acme Corporation"

  # Create a user within a tenant
  quicbaas iam user create alice@acme.com --tenant acme-corp --name "Alice"

  # Create a group
  quicbaas iam group create engineering --tenant acme-corp --name "Engineering"

  # Register an OAuth application
  quicbaas iam app register --name my-app --tenant acme-corp

  # Apply a policy
  quicbaas iam policy apply -f policy.yaml

  # List active sessions
  quicbaas iam session list --tenant acme-corp

  # Check access with PEP
  quicbaas iam pep check --tenant acme-corp --subject '{"id":"user1"}' --action read --resource keys/foo

  # Query audit events
  quicbaas iam audit query --tenant acme-corp --category authentication

  # Manage delegated administration
  quicbaas iam admin assignment create --user user_abc --role tenant_admin --tenant-ids acme-corp

  # GDPR compliance
  quicbaas iam gdpr consent grant --tenant acme-corp --user-id user_abc --purpose marketing --version 1.0

  # Create an external identity provider
  quicbaas iam provider create --tenant acme-corp --provider-id google --name "Google" --type oidc \
    --client-id "xxx.apps.googleusercontent.com" --client-secret "secret" \
    --issuer-url "https://accounts.google.com" --auto-provision --allow-linking

  # Link a federated identity
  quicbaas iam identity link --tenant acme-corp --user-id user_abc --provider-id google --auth-code "auth_code"`,
	}

	// Add persistent flags for all IAM commands
	cmd.PersistentFlags().StringVar(&iamOutputFormat, "output", "table", "Output format (json, yaml, table)")
	cmd.PersistentFlags().BoolVarP(&iamForce, "yes", "y", false, "Skip confirmation prompts")

	// Add subcommands
	cmd.AddCommand(newIAMTenantCommand())
	cmd.AddCommand(newIAMUserCommand())
	cmd.AddCommand(newIAMGroupCommand())
	cmd.AddCommand(newIAMRoleCommand())
	cmd.AddCommand(newIAMAppCommand())
	cmd.AddCommand(newIAMPolicyCommand())
	cmd.AddCommand(newIAMMFACommand())
	cmd.AddCommand(newIAMWebAuthnCommand())
	cmd.AddCommand(newIAMSessionCommand())
	cmd.AddCommand(newIAMPEPCommand())
	cmd.AddCommand(newIAMAuditCommand())
	cmd.AddCommand(newIAMAdminCommand())
	cmd.AddCommand(newIAMGDPRCommand())

	// Federation commands
	cmd.AddCommand(newIAMProviderCommand())
	cmd.AddCommand(newIAMIdentityCommand())

	return cmd
}
