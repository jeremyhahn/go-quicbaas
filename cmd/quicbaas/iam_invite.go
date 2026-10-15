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

package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Operation names the invite verbs report in their errors.
const (
	inviteShowOp        = "iam.invite.show"
	inviteActivatePIVOp = "iam.invite.activate-piv"
)

var (
	inviteToken    string
	inviteCertPath string
)

// newIAMInviteCommand builds 'quicbaas iam invite'.
func newIAMInviteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invite",
		Short: "Inspect and redeem one-time user invites",
		Long: `Inspect and redeem the one-time invite tokens that onboard a pending user,
such as the tenant admin created by "iam tenant provision --admin-enrollment invite".

The root --tenant names the tenant the invite belongs to. The token is
single-use; treat it as a credential.

Examples:
  # Show who an invite is for and how it may be activated
  quicbaas iam invite show --tenant acme-corp --token <token>

  # Activate the invited user with a PIV certificate
  quicbaas iam invite activate-piv --tenant acme-corp --token <token> --cert piv.crt`,
	}

	cmd.PersistentFlags().StringVar(&inviteToken, "token", "", "Invite token (required)")

	cmd.AddCommand(newIAMInviteShowCommand())
	cmd.AddCommand(newIAMInviteActivatePIVCommand())

	return cmd
}

// newIAMInviteShowCommand builds 'quicbaas iam invite show'.
func newIAMInviteShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show a pending invite's public metadata",
		Long: `Show the user a pending invite activates, the methods it may be activated
with, and when it expires. Showing an invite does not consume it.

Example:
  quicbaas iam invite show --tenant acme-corp --token <token>`,
		Args: cobra.NoArgs,
		RunE: runIAMInviteShowCommand,
	}
}

// runIAMInviteShowCommand executes the invite show command.
func runIAMInviteShowCommand(cmd *cobra.Command, _ []string) error {
	if err := requireInviteScope(inviteShowOp); err != nil {
		return err
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	// The token is a credential: log only the tenant ID.
	log.Debugf("Showing invite in tenant: %s", globalTenantID)
	invite, err := iamClient.Invite().GetInvite(ctx, globalTenantID, inviteToken)
	if err != nil {
		return formatError(err)
	}

	if format == cli.OutputFormatJSON {
		return writeInviteJSON(cmd.OutOrStdout(), invite)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Invite:")
	fmt.Fprintf(out, "  User ID: %s\n", invite.UserID)
	if invite.Email != "" {
		fmt.Fprintf(out, "  Email: %s\n", invite.Email)
	}
	if invite.Name != "" {
		fmt.Fprintf(out, "  Name: %s\n", invite.Name)
	}
	fmt.Fprintf(out, "  Auth Methods: %s\n", strings.Join(invite.AuthMethods, ", "))
	fmt.Fprintf(out, "  Expires: %s\n", invite.ExpiresAt.Format("2006-01-02 15:04:05"))
	return nil
}

// newIAMInviteActivatePIVCommand builds 'quicbaas iam invite activate-piv'.
func newIAMInviteActivatePIVCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activate-piv",
		Short: "Activate an invited user with a PIV certificate",
		Long: `Redeem an invite by binding a PIV X.509 certificate to the pending user,
which activates the user. The certificate must chain to a root the server
trusts. The invite is consumed on success.

Example:
  quicbaas iam invite activate-piv --tenant acme-corp --token <token> --cert piv.crt`,
		Args: cobra.NoArgs,
		RunE: runIAMInviteActivatePIVCommand,
	}

	cmd.Flags().StringVar(&inviteCertPath, "cert", "", "Path to the PEM-encoded PIV certificate (required)")

	return cmd
}

// runIAMInviteActivatePIVCommand executes the invite activate-piv command.
func runIAMInviteActivatePIVCommand(cmd *cobra.Command, _ []string) error {
	if err := requireInviteScope(inviteActivatePIVOp); err != nil {
		return err
	}

	certPEM, err := readPIVCertificate(inviteCertPath)
	if err != nil {
		return err
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	log.Debugf("Activating invite with PIV in tenant: %s", globalTenantID)
	resp, err := iamClient.Invite().ActivatePIV(ctx, globalTenantID, inviteToken, certPEM)
	if err != nil {
		return formatError(err)
	}

	if format == cli.OutputFormatJSON {
		return writeInviteJSON(cmd.OutOrStdout(), resp)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Invite activated:")
	fmt.Fprintf(out, "  User ID: %s\n", resp.UserID)
	if resp.Message != "" {
		fmt.Fprintf(out, "  Message: %s\n", resp.Message)
	}
	if resp.Warning != "" {
		fmt.Fprintf(out, "  Warning: %s\n", resp.Warning)
	}
	return nil
}

// requireInviteScope refuses a call missing the tenant or token before any
// client is built.
func requireInviteScope(op string) error {
	if globalTenantID == "" {
		return errors.New(errors.ErrInvalidInput, op, "--tenant is required")
	}
	if inviteToken == "" {
		return errors.New(errors.ErrInvalidInput, op, "--token is required")
	}
	return nil
}

// readPIVCertificate reads path and returns its contents once they parse as
// a PEM X.509 certificate, so a wrong file is refused locally rather than
// spending the invite's round trip. The server's chain validation stays
// authoritative.
func readPIVCertificate(path string) (string, error) {
	if path == "" {
		return "", errors.New(errors.ErrInvalidInput, inviteActivatePIVOp, "--cert is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errors.Wrapf(err, errors.ErrInvalidInput, inviteActivatePIVOp, "failed to read certificate file %s", path)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.Newf(errors.ErrInvalidInput, inviteActivatePIVOp, "%s does not hold a PEM CERTIFICATE block", path)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return "", errors.Wrapf(err, errors.ErrInvalidInput, inviteActivatePIVOp, "%s does not hold a valid X.509 certificate", path)
	}
	return string(data), nil
}

// writeInviteJSON writes v as indented JSON.
func writeInviteJSON(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
