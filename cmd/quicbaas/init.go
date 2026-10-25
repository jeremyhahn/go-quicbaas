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

// Package main provides Init / Ceremony CLI commands for QuicBaaS.
//
// The "init" command group hosts the post-bootstrap ceremony verbs that
// turn a fresh-started QuicBaaS into an operational system. The verb
// set mirrors xkmsctl init verb-for-verb so operators get a single,
// consistent surface across QuicBaaS / XKMS / qrdb clients.
//
// Verbs:
//
//	status                   Show the current ceremony state
//	claim-share              Claim the officer's Shamir share (challenge-response)
//	sign-csr                 SO-authorised CSR signing
//
// All commands route through the SDK CeremonyClient
// (sdk/go.Client.BarrierCeremony()), which speaks to the
// /api/v1/init/* REST surface (or the protocol-equivalent
// gRPC/QUIC/Unix/MCP/embedded endpoint). No transport-specific code
// lives in this file.
package main

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// init command flags
// =============================================================================

var (
	initServer      string
	initUsername    string
	initOutputPath  string
	initTLSCertPath string
	initTLSKeyPath  string
	initSOPin       string
	initCSRPath     string
	initRole        string
	initOutputFmt   string
)

// =============================================================================
// CeremonyClient interface and factory
// =============================================================================

// CeremonyClient is the subset of the qrdb transport.CeremonyClient
// surface the CLI needs. The CLI never instantiates this directly;
// instead it goes through ceremonyClientFactory which wraps the SDK
// BarrierCeremony() call. Tests substitute their own factory to inject
// mocks.
type CeremonyClient interface {
	CeremonyInitialize(ctx context.Context, req *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error)
	CeremonyStatus(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error)
	CeremonySignCSR(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error)
}

// CeremonyClientFactory creates CeremonyClient instances.
type CeremonyClientFactory func() (CeremonyClient, error)

// ceremonyClientFactory creates CeremonyClient instances. Defaults to the
// SDK-backed implementation; tests override this for injection.
var ceremonyClientFactory CeremonyClientFactory = createSDKCeremonyClient

// createSDKCeremonyClient returns the SDK CeremonyClient. The SDK chooses
// the active transport (grpc, rest, quic, unix, mcp, embedded) per the
// global --transport flag.
func createSDKCeremonyClient() (CeremonyClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().Ceremony()
}

// =============================================================================
// Errors
// =============================================================================

// InitCSRKeySignError is returned when claim-share cannot sign the
// challenge nonce with the supplied key.
type InitCSRKeySignError struct {
	Path string
	Err  error
}

// Error returns a human-readable description.
func (e *InitCSRKeySignError) Error() string {
	return "init claim-share: failed to sign challenge with " +
		e.Path + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *InitCSRKeySignError) Unwrap() error { return e.Err }

// InitWriteFileError is returned when an init verb cannot
// write its output file.
type InitWriteFileError struct {
	Path string
	Err  error
}

// Error returns a human-readable description.
func (e *InitWriteFileError) Error() string {
	return "init: failed to write output to " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *InitWriteFileError) Unwrap() error { return e.Err }

// =============================================================================
// Command tree
// =============================================================================

// newInitCommand creates the canonical "init" command group.
//
// IMPORTANT: This is the post-Phase-12 canonical surface. It coexists
// with the legacy single-call "quicbaas init" (canonical.go's
// newCanonicalInitCmd). The legacy single-call form is registered
// directly on rootCmd; this group is registered as the CLI surface
// users reach via 'quicbaas init <subcommand>'.
//
// Cobra resolves a subcommand path before falling back to the parent's
// RunE, so registering this group under the same Use as the legacy
// single-call form does not work. Phase 12 attaches THIS group under
// rootCmd while the legacy single-call form is reachable via
// 'quicbaas init-bundle' if a future phase decides to keep it.
func newInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap-time ceremony commands (status, officer, claim-share, ...)",
		Long: `Bootstrap-time ceremony commands.

These commands drive the initialization ceremony against a RUNNING daemon.
The verb set mirrors xkmsctl init.

For an M-of-N (shamir) barrier the daemon boots SEALED and serves these
verbs from its sealed window. The order follows go-qrdb's Security Officer
distribution model — shares are never written to disk and are released to
each officer exactly once, live, over their own mTLS certificate:

  1. officer register        Enrol each officer's CSR (returns their
                             CA-issued cert, never share bytes)
  2. initialize              Run the ceremony: seal one share envelope
                             per officer
  3. claim-share             Each officer claims their share envelope
  4. quicbaas barrier quorum A quorum submits shares to unseal

Verbs:
  initialize               Run the initialization ceremony
  status                   Show the current ceremony state
  officer register|list     Manage Security Officer enrolment
  claim-share              Claim the officer's share envelope (challenge-response)
  sign-csr                 SO-authorised CSR signing`,
	}

	cmd.PersistentFlags().StringVar(&initOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newInitInitializeCommand())
	cmd.AddCommand(newInitStatusCommand())
	cmd.AddCommand(newInitOfficerCommand())
	cmd.AddCommand(newInitClaimShareCommand())
	cmd.AddCommand(newInitSignCSRCommand())

	return cmd
}

// =============================================================================
// status
// =============================================================================

// newInitStatusCommand builds 'quicbaas init status'.
func newInitStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the current ceremony state",
		Long: `Display the current state of the initialization ceremony.

States: not_started, awaiting_officers, awaiting_shares, complete.

Example:
  quicbaas init status`,
		RunE: runInitStatusCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	return cmd
}

// runInitStatusCommand executes 'init status'.
func runInitStatusCommand(cmd *cobra.Command, _ []string) error {
	client, err := ceremonyClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.CeremonyStatus(ctx)
	if err != nil {
		return formatError(err)
	}

	return formatInitStatusResponse(resp, initOutputFmt, cmd.OutOrStdout())
}

// signShareClaimWithKeyFile loads a PEM-encoded private key from path and
// signs the share-claim message the qrdb ceremony verifier checks: the
// domain's claim label, the holder's username and the hex-encoded nonce,
// bound together by qrdbsdk.SignShareClaim. Returns the raw signature bytes.
func signShareClaimWithKeyFile(path, label, username, nonceHex string) ([]byte, error) {
	keyPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, &InitCSRKeySignError{Path: path, Err: err}
	}

	key, err := parsePrivateKeyPEM(keyPEM)
	if err != nil {
		return nil, &InitCSRKeySignError{Path: path, Err: err}
	}

	nonce, err := hex.DecodeString(nonceHex)
	if err != nil {
		return nil, &InitCSRKeySignError{Path: path, Err: err}
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, &InitCSRKeySignError{
			Path: path,
			Err:  errors.Newf(errors.ErrInvalidInput, "init.claim-share", "key does not implement crypto.Signer"),
		}
	}

	sig, err := qrdbsdk.SignShareClaim(signer, label, username, nonce)
	if err != nil {
		return nil, &InitCSRKeySignError{Path: path, Err: err}
	}
	return sig, nil
}

// parsePrivateKeyPEM decodes a PEM-encoded private key block into a
// concrete *ecdsa / *rsa / ed25519 key.
func parsePrivateKeyPEM(data []byte) (interface{}, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "init.claim-share", "no PEM block found")
	}
	switch block.Type {
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		// Fall back to PKCS8 for forward compatibility.
		if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			return k, nil
		}
		return nil, errors.Newf(errors.ErrInvalidInput,
			"init.claim-share", "unsupported PEM block type "+block.Type)
	}
}

// =============================================================================
// claim-share
// =============================================================================

// newInitClaimShareCommand builds 'quicbaas init claim-share'.
func newInitClaimShareCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claim-share",
		Short: "Claim the officer's Shamir share (challenge-response)",
		Long: `Two-step Shamir share claim:

1. Begin: server returns a challenge nonce (identity proven via mTLS with
   the officer's issued cert + key).
2. Officer signs the nonce with their certificate private key (--tls-key).
3. Complete: server verifies the signature and releases the share
   envelope, sealed to the officer's enrolled key.

The envelope is written to --output. --domain recovery claims a recovery
share instead of an unseal share.

Example:
  quicbaas init claim-share \
      --server https://quicbaas.example.com:8443 \
      --tls-cert alice.crt \
      --tls-key alice.key \
      --username alice \
      --output alice.share.json`,
		RunE: runInitClaimShareCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	cmd.Flags().StringVar(&initTLSCertPath, "tls-cert", "",
		"Path to the officer's mTLS client certificate (required)")
	cmd.Flags().StringVar(&initTLSKeyPath, "tls-key", "",
		"Path to the officer's mTLS client key (required)")
	cmd.Flags().StringVar(&initUsername, "username", "",
		"Officer username (required)")
	cmd.Flags().StringVar(&initOutputPath, "output", "",
		"Path where the share envelope should be written (required)")
	addBarrierDomainFlag(cmd)
	return cmd
}

// runInitClaimShareCommand executes 'init claim-share'.
func runInitClaimShareCommand(_ *cobra.Command, _ []string) error {
	if initUsername == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.claim-share", "--username is required")
	}
	if initTLSCertPath == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.claim-share", "--tls-cert is required")
	}
	if initTLSKeyPath == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.claim-share", "--tls-key is required")
	}
	if initOutputPath == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.claim-share", "--output is required")
	}
	if err := validateBarrierQuorumDomain("init.claim-share", barrierDomain); err != nil {
		return err
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}
	claim := shareClaimFor(client, barrierDomain)

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	begin, err := claim.begin(ctx, &qrdbtransport.ShareClaimBeginRequest{
		Username: initUsername,
	})
	if err != nil {
		return formatError(err)
	}

	signature, err := signShareClaimWithKeyFile(initTLSKeyPath, claim.label, initUsername, begin.NonceHex)
	if err != nil {
		return formatError(err)
	}

	complete, err := claim.complete(ctx, &qrdbtransport.ShareClaimCompleteRequest{
		Username:  initUsername,
		NonceHex:  begin.NonceHex,
		Signature: signature,
	})
	if err != nil {
		return formatError(err)
	}

	if err := writeFileSecure(initOutputPath, complete.Envelope); err != nil {
		return formatError(err)
	}
	return nil
}

// shareClaim is the claim handshake of one quorum domain and the claim
// label its signature is bound to.
type shareClaim struct {
	label    string
	begin    func(context.Context, *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error)
	complete func(context.Context, *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error)
}

// shareClaimFor returns the claim handshake for domain, which the caller has
// already validated: the recovery domain claims through the Recovery* verbs
// under the recovery label, the unseal domain through the plain ones under
// the system label.
func shareClaimFor(client BarrierClient, domain string) shareClaim {
	if domain == qrdbtransport.DomainRecovery {
		return shareClaim{
			label:    qrdbsdk.RecoveryClaimLabel,
			begin:    client.RecoveryShareClaimBegin,
			complete: client.RecoveryShareClaimComplete,
		}
	}
	return shareClaim{
		label:    qrdbsdk.SystemClaimLabel,
		begin:    client.ShareClaimBegin,
		complete: client.ShareClaimComplete,
	}
}

// =============================================================================
// sign-csr
// =============================================================================

// newInitSignCSRCommand builds 'quicbaas init sign-csr'.
func newInitSignCSRCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sign-csr",
		Short: "Sign a CSR with SO authorisation",
		Long: `Submit a CSR for signing during or after the ceremony.

The Security Officer PIN authorises the operation. The signed cert is
printed to stdout (PEM).

Example:
  quicbaas init sign-csr \
      --username carol \
      --so-pin $SO_PIN \
      --csr carol.csr \
      --role officer`,
		RunE: runInitSignCSRCommand,
	}
	cmd.Flags().StringVar(&initUsername, "username", "",
		"Officer username (required)")
	cmd.Flags().StringVar(&initSOPin, "so-pin", "",
		"Security Officer PIN (required)")
	cmd.Flags().StringVar(&initCSRPath, "csr", "",
		"Path to the PEM-encoded CSR (required)")
	cmd.Flags().StringVar(&initRole, "role", "",
		"Role to assign to the issued cert (required)")
	return cmd
}

// runInitSignCSRCommand executes 'init sign-csr'.
func runInitSignCSRCommand(cmd *cobra.Command, _ []string) error {
	if initUsername == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.sign-csr", "--username is required")
	}
	if initSOPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.sign-csr", "--so-pin is required")
	}
	if initCSRPath == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.sign-csr", "--csr is required")
	}
	if initRole == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.sign-csr", "--role is required")
	}

	csrPEM, err := os.ReadFile(initCSRPath)
	if err != nil {
		return formatError(errors.Wrapf(err, errors.ErrInvalidInput,
			"init.sign-csr", "failed to read CSR file %s", initCSRPath))
	}

	client, err := ceremonyClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.CeremonySignCSR(ctx, &qrdbtransport.SignCSRInitRequest{
		Username: initUsername,
		SOPin:    initSOPin,
		CSRPEM:   string(csrPEM),
		Role:     initRole,
	})
	if err != nil {
		return formatError(err)
	}

	fmt.Fprint(cmd.OutOrStdout(), resp.CertPEM)
	return nil
}

// =============================================================================
// helpers
// =============================================================================

// writeFileSecure writes data to path with mode 0600.
func writeFileSecure(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return &InitWriteFileError{Path: path, Err: err}
	}
	return nil
}

// formatInitStatusResponse renders a CeremonyStatusResponse.
func formatInitStatusResponse(resp *qrdbtransport.CeremonyStatusResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"state":    resp.State,
			"spki_pin": resp.SPKIPin,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "CEREMONY STATUS\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "State:\t%s\n", resp.State)
		if resp.SPKIPin != "" {
			fmt.Fprintf(tw, "SPKI Pin:\t%s\n", resp.SPKIPin)
		}
		return tw.Flush()
	default:
		fmt.Fprintf(w, "State: %s\n", resp.State)
		if resp.SPKIPin != "" {
			fmt.Fprintf(w, "SPKI Pin: %s\n", resp.SPKIPin)
		}
		return nil
	}
}
