// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package main provides IAM WebAuthn credential administration CLI commands
// for QuicBaaS.
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
	// WebAuthn credential command flags.
	webauthnUserID       string
	webauthnCredentialID string
	webauthnAll          bool
)

// newIAMWebAuthnCommand creates the WebAuthn credential administration command
// group.
//
// This is the operator's window into the WebAuthn relying party's credential
// store. That store is ADDRESSED by (tenant, email) and KEYED by the qrdb user
// id, and is separate from the IAM user record, which is why a credential can
// outlive the account it was registered for: deleting the record leaves the
// id-keyed blobs behind with nothing naming them. A survivor that the next
// registrant does resolve is listed in the ceremony's excludeCredentials and
// the authenticator holding it then refuses to enrol. Before these verbs the
// store had only ceremony endpoints, so the only way out was to give the person
// a different email address.
//
// Renaming a user is NOT such a case: the credentials are keyed by the id,
// which a rename does not change, so they follow the account to its new address
// (go-quicbaas#582).
func newIAMWebAuthnCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webauthn",
		Short: "WebAuthn credential administration",
		Long: `Inspect and remove the WebAuthn authenticators (passkeys, security keys)
registered to a user.

These are administrative verbs over another user's credentials, authorized
like the other user-admin verbs — not the self-service registration ceremony.

Use them when an authenticator refuses to register against an account with
"contains one of the credentials already registered with the relying party":
that means a credential for the account's email is already stored, and this
is where you see it and remove it.

All commands require the --tenant and --user flags.`,
	}

	cmd.PersistentFlags().StringVar(&webauthnUserID, "user", "", "User ID (required)")

	cmd.AddCommand(newWebAuthnListCommand())
	cmd.AddCommand(newWebAuthnDeleteCommand())

	return cmd
}

// newWebAuthnListCommand creates the credential list command.
func newWebAuthnListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List a user's registered WebAuthn credentials",
		Long: `List the WebAuthn authenticators registered to a user.

The credential ID shown here is what 'webauthn delete --credential' takes.

Examples:
  # List a user's registered authenticators
  quicbaas iam webauthn list --tenant acme-corp --user user_abc123

  # As JSON
  quicbaas iam webauthn list --tenant acme-corp --user user_abc123 --output json`,
		RunE: runWebAuthnListCommand,
	}
}

// runWebAuthnListCommand executes the credential list command.
func runWebAuthnListCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.webauthn.list", "--tenant flag is required")
	}
	if webauthnUserID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.webauthn.list", "--user flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	log.Debugf("Listing WebAuthn credentials for user: %s in tenant: %s", webauthnUserID, globalTenantID)
	credentials, err := iamClient.ListWebAuthnCredentials(ctx, globalTenantID, webauthnUserID)
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	if format == cli.OutputFormatJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(credentials)
	}

	if len(credentials) == 0 {
		fmt.Printf("No WebAuthn credentials registered to user '%s'\n", webauthnUserID)
		return nil
	}

	fmt.Printf("WebAuthn credentials for user '%s':\n", webauthnUserID)
	for _, credential := range credentials {
		fmt.Printf("\n  ID: %s\n", credential.ID)
		if credential.AAGUID != "" {
			fmt.Printf("  AAGUID: %s\n", credential.AAGUID)
		}
		if credential.AttestationType != "" {
			fmt.Printf("  Attestation: %s\n", credential.AttestationType)
		}
		if len(credential.Transports) > 0 {
			fmt.Printf("  Transports: %s\n", strings.Join(credential.Transports, ", "))
		}
		fmt.Printf("  Sign Count: %d\n", credential.SignCount)
		fmt.Printf("  Backed Up: %t\n", credential.BackupState)
		if credential.CloneWarning {
			fmt.Println("  WARNING: the authenticator's signature counter went backwards (possible clone)")
		}
	}

	return nil
}

// newWebAuthnDeleteCommand creates the credential delete command.
func newWebAuthnDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a user's WebAuthn credential",
		Long: `Delete one of a user's registered WebAuthn authenticators, or all of them.

Deleting a credential also revokes the fido2 login factor enrolled alongside
it, so the user cannot be left with a factor pointing at a passkey that no
longer exists.

Examples:
  # Delete one credential by its ID (from 'webauthn list')
  quicbaas iam webauthn delete --tenant acme-corp --user user_abc123 \
      --credential AbCdEf0123

  # Clear every credential so the account can register a passkey again
  quicbaas iam webauthn delete --tenant acme-corp --user user_abc123 --all

  # Skip the confirmation prompt
  quicbaas iam webauthn delete --tenant acme-corp --user user_abc123 --all --yes`,
		RunE: runWebAuthnDeleteCommand,
	}

	cmd.Flags().StringVar(&webauthnCredentialID, "credential", "", "Credential ID to delete")
	cmd.Flags().BoolVar(&webauthnAll, "all", false, "Delete every credential registered to the user")

	return cmd
}

// runWebAuthnDeleteCommand executes the credential delete command.
func runWebAuthnDeleteCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.webauthn.delete", "--tenant flag is required")
	}
	if webauthnUserID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.webauthn.delete", "--user flag is required")
	}
	// Requiring exactly one of the two keeps "delete everything" an explicit
	// request: a mistyped --credential must not silently widen into --all.
	if webauthnCredentialID == "" && !webauthnAll {
		return errors.Newf(errors.ErrInvalidInput, "iam.webauthn.delete",
			"one of --credential or --all is required")
	}
	if webauthnCredentialID != "" && webauthnAll {
		return errors.Newf(errors.ErrInvalidInput, "iam.webauthn.delete",
			"--credential and --all are mutually exclusive")
	}

	if !iamForce {
		if webauthnAll {
			fmt.Printf("Delete ALL WebAuthn credentials for user '%s'? The user will have to register a passkey again. (y/N): ",
				webauthnUserID)
		} else {
			fmt.Printf("Delete WebAuthn credential '%s' for user '%s'? (y/N): ",
				webauthnCredentialID, webauthnUserID)
		}
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Delete cancelled")
			return nil
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if webauthnAll {
		log.Debugf("Deleting all WebAuthn credentials for user: %s in tenant: %s", webauthnUserID, globalTenantID)
		deleted, dErr := iamClient.DeleteWebAuthnCredentials(ctx, globalTenantID, webauthnUserID)
		if dErr != nil {
			return formatError(dErr)
		}
		fmt.Printf("Deleted %d WebAuthn credential(s) for user '%s'\n", deleted, webauthnUserID)
		return nil
	}

	log.Debugf("Deleting WebAuthn credential %s for user: %s in tenant: %s",
		webauthnCredentialID, webauthnUserID, globalTenantID)
	if dErr := iamClient.DeleteWebAuthnCredential(ctx, globalTenantID, webauthnUserID, webauthnCredentialID); dErr != nil {
		return formatError(dErr)
	}

	fmt.Printf("Deleted WebAuthn credential '%s' for user '%s'\n", webauthnCredentialID, webauthnUserID)
	return nil
}
