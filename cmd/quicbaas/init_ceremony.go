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

// init_ceremony.go implements the two verbs that DRIVE the initialization
// ceremony against a running daemon: officer registration and the ceremony
// itself. Together with the existing claim verbs they complete go-qrdb's
// Security Officer distribution model (docs/seal/shamir.md):
//
//	register -> initialize -> claim-cert -> claim-share -> quorum unseal
//
// None of this happens at `quicbaasd init` time. The M-of-N barrier is the
// ceremony's job — go-qrdb's bootstrap orchestrator refuses to initialize a
// shamir barrier inline because it needs multiple Security Officers — so the
// daemon boots SEALED and serves these verbs from its sealed window until a
// quorum unseals it.
//
// The security property these verbs preserve: no share is ever written to
// disk, and no share is ever returned by registration or initialization.
// Shares live in memguard enclaves inside the running daemon and are released
// to each officer exactly once, over that officer's own mTLS certificate.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// init ceremony flags
// =============================================================================

var (
	initUserPin     string
	initThreshold   int
	initOfficerSpec []string
	initHostname    string
	initSANs        []string
	initCredSeal    string
	initAuthRef     string
)

// =============================================================================
// Errors
// =============================================================================

// InitOfficerSpecError is returned when a --so value does not match the
// "username:/path/to/csr" form.
type InitOfficerSpecError struct {
	Got string
}

// Error returns a human-readable description.
func (e *InitOfficerSpecError) Error() string {
	return "init: --so value " + e.Got + " must be username:/path/to/file.csr"
}

// InitReadCSRError is returned when an officer CSR file cannot be read.
type InitReadCSRError struct {
	Path string
	Err  error
}

// Error returns a human-readable description.
func (e *InitReadCSRError) Error() string {
	return "init: failed to read CSR " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *InitReadCSRError) Unwrap() error { return e.Err }

// =============================================================================
// initialize
// =============================================================================

// newInitInitializeCommand builds 'quicbaas init initialize'.
func newInitInitializeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "initialize",
		Short: "Run the initialization ceremony against a running daemon",
		Long: `Run the initialization ceremony.

With --threshold and one --so per officer this performs the M-of-N Shamir
initialization: the barrier secret is split and ONE SHARE IS SEALED PER
OFFICER into a memguard enclave inside the daemon. No share is returned by
this call and no share is written to disk. Each officer then claims theirs
live with 'quicbaas init claim-cert' followed by 'quicbaas init claim-share'.

Register each officer first so their CSR is signed and bound:
  quicbaas init officer register --username alice --csr alice.csr

Example:
  quicbaas init initialize \
      --so-pin "$SO_PIN" \
      --user-pin "$USER_PIN" \
      --threshold 2 \
      --so alice:alice.csr \
      --so bob:bob.csr \
      --so carol:carol.csr`,
		RunE: runInitInitializeCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	cmd.Flags().StringVar(&initSOPin, "so-pin", "",
		"Security Officer PIN (required)")
	cmd.Flags().StringVar(&initUserPin, "user-pin", "",
		"User PIN (required)")
	cmd.Flags().IntVar(&initThreshold, "threshold", 0,
		"Shamir M-of-N threshold. Omit or set below 2 for single-admin mode")
	cmd.Flags().StringSliceVar(&initOfficerSpec, "so", nil,
		"Officer as username:/path/to/file.csr (repeatable, required for M-of-N)")
	cmd.Flags().StringVar(&initHostname, "hostname", "",
		"FQDN stamped into the issued serving certificate")
	cmd.Flags().StringSliceVar(&initSANs, "sans", nil,
		"Additional DNS / IP SANs for the issued serving certificate")
	cmd.Flags().StringVar(&initCredSeal, "credential-seal-strategy", "",
		"Credential seal strategy (manual | barrier); empty uses the server default")
	return cmd
}

// runInitInitializeCommand executes 'init initialize'.
func runInitInitializeCommand(cmd *cobra.Command, _ []string) error {
	if initSOPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.initialize", "--so-pin is required")
	}
	if initUserPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.initialize", "--user-pin is required")
	}

	officers, err := parseCeremonyOfficers(initOfficerSpec)
	if err != nil {
		return formatError(err)
	}
	if initThreshold >= 2 && len(officers) < initThreshold {
		return errors.Newf(errors.ErrInvalidInput, "init.initialize",
			"--threshold %d requires at least %d --so officers, got %d",
			initThreshold, initThreshold, len(officers))
	}

	client, err := ceremonyClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := client.CeremonyInitialize(ctx, &qrdbtransport.CeremonyInitializeRequest{
		SOPin:                  initSOPin,
		UserPin:                initUserPin,
		CredentialSealStrategy: initCredSeal,
		Threshold:              initThreshold,
		Officers:               officers,
		Hostname:               initHostname,
		SANs:                   initSANs,
	})
	if err != nil {
		return formatError(err)
	}

	return formatCeremonyInitializeResponse(resp, initOutputFmt, cmd.OutOrStdout())
}

// parseCeremonyOfficers turns each "username:/path/to/file.csr" value into a
// ceremony officer request carrying the PEM CSR.
//
// The CSR travels to the server, which signs it and binds the officer to the
// issued certificate's thumbprint. Only the officer holds the matching private
// key, which is what makes the later share claim provable — and what keeps the
// server from ever being able to impersonate an officer.
func parseCeremonyOfficers(specs []string) ([]qrdbtransport.CeremonyOfficerRequest, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	officers := make([]qrdbtransport.CeremonyOfficerRequest, 0, len(specs))
	for _, spec := range specs {
		username, path, found := strings.Cut(spec, ":")
		if !found || username == "" || path == "" {
			return nil, &InitOfficerSpecError{Got: spec}
		}
		csrPEM, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
		if err != nil {
			return nil, &InitReadCSRError{Path: path, Err: err}
		}
		officers = append(officers, qrdbtransport.CeremonyOfficerRequest{
			Username: username,
			CSRPEM:   string(csrPEM),
		})
	}
	return officers, nil
}

// =============================================================================
// officer
// =============================================================================

// newInitOfficerCommand builds the 'quicbaas init officer' group.
func newInitOfficerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "officer",
		Short: "Manage Security Officer enrolment",
		Long: `Manage Security Officer enrolment.

Registration is step 1 of the Shamir flow: the admin submits each officer's
CSR, the ceremony CA signs it, and the officer is bound to the SHA-256
thumbprint of the issued certificate. The response carries the signed
certificate and the officer's share index — never share bytes.`,
	}
	cmd.AddCommand(newInitOfficerRegisterCommand())
	cmd.AddCommand(newInitOfficerListCommand())
	cmd.AddCommand(newInitOfficerStatusCommand())
	return cmd
}

// newInitOfficerRegisterCommand builds 'quicbaas init officer register'.
func newInitOfficerRegisterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a Security Officer from their CSR",
		Long: `Register a Security Officer.

The officer generates their own keypair and CSR — ideally on a security key
or PIV smartcard, so the private key never exists as a file. This command
submits only the CSR. The issued certificate is written to --output for
delivery to that officer.

Example:
  quicbaas init officer register \
      --username alice \
      --csr alice.csr \
      --output alice.crt`,
		RunE: runInitOfficerRegisterCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	cmd.Flags().StringVar(&initUsername, "username", "",
		"Officer username (required)")
	cmd.Flags().StringVar(&initCSRPath, "csr", "",
		"Path to the officer's PEM-encoded CSR (required)")
	cmd.Flags().StringVar(&initOutputPath, "output", "",
		"Path where the issued officer certificate should be written (optional)")
	cmd.Flags().StringVar(&initAuthFactor, "auth-factor", "",
		"Authenticator type binding the officer (mtls | webauthn | piv)")
	cmd.Flags().StringVar(&initAuthRef, "auth-ref", "",
		"Authenticator reference (e.g. the security key or PIV slot identifier)")
	return cmd
}

// runInitOfficerRegisterCommand executes 'init officer register'.
func runInitOfficerRegisterCommand(cmd *cobra.Command, _ []string) error {
	if initUsername == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.officer.register", "--username is required")
	}
	if initCSRPath == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.officer.register", "--csr is required")
	}
	if initAuthFactor != "" {
		if _, ok := validAuthFactors[initAuthFactor]; !ok {
			return formatError(&InitInvalidAuthFactorError{Got: initAuthFactor})
		}
	}

	csrPEM, err := os.ReadFile(initCSRPath) //nolint:gosec // operator-supplied path
	if err != nil {
		return formatError(&InitReadCSRError{Path: initCSRPath, Err: err})
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := client.OfficerRegister(ctx, &qrdbtransport.OfficerRegisterRequest{
		Username:          initUsername,
		CSRPEM:            csrPEM,
		AuthenticatorType: initAuthFactor,
		AuthenticatorRef:  initAuthRef,
	})
	if err != nil {
		return formatError(err)
	}

	if initOutputPath != "" && len(resp.CertPEM) > 0 {
		if werr := writeFileSecure(initOutputPath, resp.CertPEM); werr != nil {
			return formatError(&InitWriteFileError{Path: initOutputPath, Err: werr})
		}
	}

	return formatOfficerRegisterResponse(resp, initUsername, initOutputPath, initOutputFmt, cmd.OutOrStdout())
}

// newInitOfficerListCommand builds 'quicbaas init officer list'.
func newInitOfficerListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the enrolled Security Officers",
		Long: `List the enrolled Security Officers.

Only public metadata is returned: username, certificate thumbprint, share
index, auth factor, and enrolment time. No share material is ever included.`,
		RunE: runInitOfficerListCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	return cmd
}

// runInitOfficerListCommand executes 'init officer list'.
func runInitOfficerListCommand(cmd *cobra.Command, _ []string) error {
	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	officers, err := client.OfficerList(ctx)
	if err != nil {
		return formatError(err)
	}
	return formatOfficerStatusList(officers, initOutputFmt, cmd.OutOrStdout())
}

// newInitOfficerStatusCommand builds 'quicbaas init officer status'.
func newInitOfficerStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show per-officer claim status",
		Long: `Show per-officer enrolment and claim status.

Use this to see which officers have claimed their certificate and share
during the enrolling window. Share bytes are never reported.`,
		RunE: runInitOfficerStatusCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	return cmd
}

// runInitOfficerStatusCommand executes 'init officer status'.
func runInitOfficerStatusCommand(cmd *cobra.Command, _ []string) error {
	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	officers, err := client.OfficerClaimStatus(ctx)
	if err != nil {
		return formatError(err)
	}
	return formatOfficerStatusList(officers, initOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Output formatting
// =============================================================================

// formatCeremonyInitializeResponse renders the ceremony result. The share
// count is reported so the operator can confirm the split happened, but the
// shares themselves are not here and never will be — they are held in the
// daemon until each officer claims theirs.
func formatCeremonyInitializeResponse(
	resp *qrdbtransport.CeremonyInitializeResponse,
	format string,
	w io.Writer,
) error {
	if resp == nil {
		return errors.Newf(errors.ErrInternal, "init.initialize", "empty response")
	}
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if outputFormat == cli.OutputFormatJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "CEREMONY INITIALIZED\n")
	fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
	fmt.Fprintf(tw, "State:\t%s\n", resp.State)
	if resp.SPKIPin != "" {
		fmt.Fprintf(tw, "SPKI Pin:\t%s\n", resp.SPKIPin)
	}
	if resp.ShareCount > 0 {
		fmt.Fprintf(tw, "Shares:\t%d generated\n", resp.ShareCount)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if resp.ShareCount > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Shares are held in memory and released ONCE, to each officer:")
		fmt.Fprintln(w, "  quicbaas init claim-cert   --username <name> --csr-key <key> --output <cert>")
		fmt.Fprintln(w, "  quicbaas init claim-share  --username <name> --cert <cert> --key <key>")
	}
	return nil
}

// formatOfficerRegisterResponse renders one officer registration.
func formatOfficerRegisterResponse(
	resp *qrdbtransport.OfficerRegisterResponse,
	username, outputPath, format string,
	w io.Writer,
) error {
	if resp == nil {
		return errors.Newf(errors.ErrInternal, "init.officer.register", "empty response")
	}
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if outputFormat == cli.OutputFormatJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"username":    username,
			"share_index": resp.ShareIndex,
			"cert_pem":    string(resp.CertPEM),
		})
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Officer:\t%s\n", username)
	fmt.Fprintf(tw, "Share Index:\t%d\n", resp.ShareIndex)
	if outputPath != "" {
		fmt.Fprintf(tw, "Certificate:\t%s\n", outputPath)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if outputPath == "" && len(resp.CertPEM) > 0 {
		fmt.Fprintln(w)
		fmt.Fprint(w, string(resp.CertPEM))
	}
	return nil
}

// formatOfficerStatusList renders the officer roster.
func formatOfficerStatusList(
	officers []*qrdbtransport.OfficerStatus,
	format string,
	w io.Writer,
) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if outputFormat == cli.OutputFormatJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(officers)
	}
	if len(officers) == 0 {
		fmt.Fprintln(w, "No Security Officers enrolled.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "USERNAME\tINDEX\tFACTOR\tTHUMBPRINT\n")
	for _, o := range officers {
		if o == nil {
			continue
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n",
			o.Username, o.ShareIndex, o.AuthFactor, o.CertThumbprint)
	}
	return tw.Flush()
}
