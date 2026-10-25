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

// Package main provides Barrier Management CLI commands for QuicBaaS.
//
// The barrier commands are the canonical replacement for the legacy
// "quicbaas seal" tree per docs/08-bootstrap-auth-architecture.md §6.
// The verb tree and flag set mirror qrdbctl / xkmsctl 1:1 so operators
// get a single, consistent surface across the entire stack:
//
//	barrier status                                       (read-only)
//	barrier init           --barrier-strategy <none|passphrase|shamir>
//	                       [--passphrase-stdin]         (required for passphrase/shamir)
//	                       [--passphrase <passphrase>]
//	                       [--so-pin <pin>]
//	                       [--user-pin <pin>]           (requires --so-pin)
//	barrier init-shamir    --threshold N --total M [--policy <designated|any-enrolled>]
//	                       [--so-pin] [--user-pin]
//	barrier unseal         --passphrase-stdin | --passphrase <passphrase>
//	                                                    (strategy passphrase replay)
//	barrier seal           [--so-pin-stdin] [-y|--assume-yes]
//	barrier rekey          [--domain <unseal|recovery>] --threshold N [--policy P] [--so-pin <pin>]
//	barrier officer-revoke [--domain <unseal|recovery>] --username U [--threshold N] [--so-pin <pin>]
//	barrier quorum begin   [--domain D] --op O --username U [--factor F]
//	barrier quorum submit  [--domain D] --op O --username U --share-file <path>
//	                       [--challenge-id C --factor F --response-file <path>]
//	barrier recovery generate   --threshold N [--policy P] [--so-pin <pin>]
//
// Every verb that takes a passphrase reads it from stdin (--passphrase-stdin,
// preferred), the QUICBAAS_BARRIER_PASSPHRASE environment variable, or
// --passphrase, in that precedence order; see passphrase.go. --passphrase is
// kept for compatibility and its help text warns that argv is world-readable
// through /proc for the life of the call.
//
// All commands route through the SDK BarrierClient
// (sdk/go.Client.Security().Barrier()), which speaks to the
// /api/v1/barrier REST surface (or the protocol-equivalent gRPC / QUIC /
// Unix / MCP / embedded endpoint). No transport-specific code lives in
// this file.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Canonical barrier-strategy set
// =============================================================================

// barrierStrategyInit enumerates the strategies accepted by
// `barrier init`. Map lookup is O(1); kept in lock-step with qrdbctl's
// canonical surface (qrdb/cmd/qrdb/barrier.go).
var barrierStrategyInit = map[string]struct{}{
	"none":       {},
	"passphrase": {},
	"shamir":     {},
}

// =============================================================================
// Barrier command flags
// =============================================================================

var (
	barrierSecret       string
	barrierThreshold    int
	barrierTotalShares  int
	barrierOutputFmt    string
	barrierAssumeYes    bool
	barrierStrategy     string
	barrierSOPin        string
	barrierUserPin      string
	barrierPolicy       string
	barrierDomain       string
	barrierTenantID     string
	barrierMechanism    string
	barrierAvailability string
)

// =============================================================================
// BarrierClient interface and factory
// =============================================================================

// BarrierClient is the subset of the qrdb transport.BarrierClient surface
// the CLI needs. The CLI never instantiates this directly; instead it
// goes through barrierClientFactory which wraps the SDK Barrier() call.
// Tests substitute their own factory to inject mocks.
type BarrierClient interface {
	BarrierStatus(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error)
	BarrierInitialize(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error
	BarrierInitializeShamir(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error)
	BarrierUnseal(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error
	BarrierSeal(ctx context.Context) error
	BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error)

	// BarrierQuorumBegin opens one holder's factor challenge for a quorum
	// operation (unseal, seal, root-token, rekey or recover).
	BarrierQuorumBegin(ctx context.Context, req *qrdbtransport.QuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error)

	// BarrierQuorumSubmit answers a challenge with the holder's factor
	// response and share. The outcome carries a root token exactly once,
	// when a root-token round completes.
	BarrierQuorumSubmit(ctx context.Context, req *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error)

	// BarrierRekeyStart opens a rekey round; holders then complete it
	// through BarrierQuorumSubmit.
	BarrierRekeyStart(ctx context.Context, req *qrdbtransport.RekeyStartRequest) (*qrdbtransport.QuorumRound, error)

	// BarrierOfficerRevoke opens a round that rekeys the domain without the
	// named holder.
	BarrierOfficerRevoke(ctx context.Context, req *qrdbtransport.OfficerRevokeRequest) (*qrdbtransport.QuorumRound, error)

	// BarrierEnableEncryption migrates a previously-unencrypted barrier to an
	// encrypted one. It requires an UNSEALED data plane, which is why it is
	// deliberately excluded from the sealed-window allowlist.
	BarrierEnableEncryption(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error)

	// OfficerRegister enrols a Security Officer from their CSR. The ceremony
	// CA signs it and binds the officer to the issued certificate's SPKI
	// thumbprint; the response carries the certificate and the officer's share
	// index, never share bytes. This is step 1 of go-qrdb's Security Officer
	// distribution model and is reachable while the barrier is still sealed.
	OfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error)

	// RecoveryOfficerRegister enrols a recovery officer the same way.
	RecoveryOfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error)

	// OfficerList lists the enrolled Security Officers (public metadata only).
	OfficerList(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error)

	// ShareClaimBegin and ShareClaimComplete run an unseal officer's
	// challenge-response claim of their sealed share envelope; the Recovery*
	// pair runs the same handshake for a recovery officer.
	ShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error)
	ShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error)
	RecoveryShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error)
	RecoveryShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error)
}

// BarrierClientFactory is a function type for creating BarrierClient instances.
type BarrierClientFactory func() (BarrierClient, error)

// barrierClientFactory creates BarrierClient instances. Defaults to the
// SDK-backed implementation; tests override this for injection.
var barrierClientFactory BarrierClientFactory = createSDKBarrierClient

// createSDKBarrierClient returns the SDK BarrierClient. The SDK chooses
// the active transport (grpc, rest, quic, unix, mcp, embedded) per the
// global --transport flag.
func createSDKBarrierClient() (BarrierClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().Barrier()
}

// =============================================================================
// Errors
// =============================================================================

// BarrierScopeMismatchError is returned by `barrier enable-encryption
// --tenant` when the server reports it migrated a barrier other than that
// tenant's. It is the client half of go-quicbaas#990: a server that drops the
// body tenant_id migrates the SYSTEM barrier, and the verb must not report
// that as the migration the operator asked for.
type BarrierScopeMismatchError struct {
	// TenantID is the tenant the operator asked to migrate.
	TenantID string
	// Scope is the scope the server reports it migrated.
	Scope string
}

// Error returns a human-readable description.
func (e *BarrierScopeMismatchError) Error() string {
	return "barrier enable-encryption: asked to migrate tenant " + e.TenantID +
		" but the server reports scope " + strconv.Quote(e.Scope) +
		"; check `quicbaas barrier status` before retrying"
}

// PINsNotAllowedError is returned when SO/User PINs are supplied to a
// barrier verb whose configured strategy forbids PIN authorization
// (currently --barrier-strategy=none). The structured form lets callers
// program around the case without string-matching.
type PINsNotAllowedError struct {
	Strategy string
	Op       string
}

// Error returns a human-readable description.
func (e *PINsNotAllowedError) Error() string {
	return "barrier " + e.Op + ": --so-pin/--user-pin are not allowed when --barrier-strategy=" + e.Strategy
}

// =============================================================================
// Command tree
// =============================================================================

// newBarrierCommand creates the "barrier" command group.
func newBarrierCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "barrier",
		Short: "Manage the cryptographic barrier",
		Long: `Manage the cryptographic barrier that protects the data encryption key.

The barrier must be initialized once and unsealed after each restart
before QuicBaaS can process requests. Sealing strategies include
passphrase (single-passphrase) and Shamir (M-of-N split shares).

Verbs mirror qrdbctl / xkmsctl so operators have a consistent surface:
  status         Show current barrier status
  init           Initialize with a single passphrase (or --barrier-strategy=none)
  init-shamir    Initialize with Shamir M-of-N secret sharing
  unseal         Unseal with --passphrase
  seal           Seal the barrier
  rekey          Open a quorum round that rotates a domain's key and share set
  officer-revoke Open a quorum round that rekeys a domain without one holder
  quorum         Answer a holder challenge (begin | submit) for unseal, seal,
                 root-token, rekey or recover
  recovery       Generate recovery keys
  enable-encryption
                 Migrate an unencrypted barrier to an encrypted one`,
	}

	cmd.PersistentFlags().StringVar(&barrierOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newBarrierStatusCommand())
	cmd.AddCommand(newBarrierInitCommand())
	cmd.AddCommand(newBarrierInitShamirCommand())
	cmd.AddCommand(newBarrierUnsealCommand())
	cmd.AddCommand(newBarrierSealCommand())
	cmd.AddCommand(newBarrierRekeyCommand())
	cmd.AddCommand(newBarrierOfficerRevokeCommand())
	cmd.AddCommand(newBarrierQuorumCommand())
	cmd.AddCommand(newBarrierRecoveryCommand())
	cmd.AddCommand(newBarrierEnableEncryptionCommand())

	return cmd
}

// =============================================================================
// status
// =============================================================================

// newBarrierStatusCommand builds 'quicbaas barrier status'.
func newBarrierStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current barrier status",
		Long: `Display the current state of the cryptographic barrier including
sealed/unsealed, the active strategy, and initialization time.

Examples:
  quicbaas barrier status
  quicbaas barrier status --output json`,
		RunE: runBarrierStatusCommand,
	}
}

// runBarrierStatusCommand executes 'barrier status'.
func runBarrierStatusCommand(cmd *cobra.Command, _ []string) error {
	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.BarrierStatus(ctx)
	if err != nil {
		return formatError(err)
	}

	return formatBarrierStatusResponse(resp, barrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// init
// =============================================================================

// newBarrierInitCommand builds 'quicbaas barrier init'.
func newBarrierInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the cryptographic barrier",
		Long: `Initialize the cryptographic barrier.

Strategies:
  none        Identity passthrough; data stored in cleartext (dev only).
  passphrase  Single passphrase-derived master key (default).
  shamir      Single-secret Shamir seed; use 'barrier init-shamir' for M-of-N.

A passphrase is required when --barrier-strategy is passphrase or shamir.
PINs are optional; when --so-pin is supplied --user-pin must also be supplied
so the resulting PIN pair satisfies FIPS 140-3 / PCI-DSS v4 dual-control.
PINs are rejected entirely when --barrier-strategy=none.

` + passphraseInputHelp + `

Examples:
  printf '%s' "$PP" | quicbaas barrier init --barrier-strategy passphrase --passphrase-stdin
  quicbaas barrier init --barrier-strategy none
  quicbaas barrier init --barrier-strategy passphrase --passphrase '<pp>' \
                        --so-pin SO123 --user-pin USR123`,
		RunE: runBarrierInitCommand,
	}
	cmd.Flags().StringVar(&barrierStrategy, "barrier-strategy", "passphrase",
		"Barrier strategy: none | passphrase | shamir")
	addPassphraseFlags(cmd, &barrierSecret,
		"Master passphrase (required for passphrase and shamir)")
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (optional; requires --user-pin)")
	cmd.Flags().StringVar(&barrierUserPin, "user-pin", "",
		"User PIN (optional; requires --so-pin)")
	return cmd
}

// runBarrierInitCommand executes 'barrier init'.
func runBarrierInitCommand(cmd *cobra.Command, _ []string) error {
	if _, ok := barrierStrategyInit[barrierStrategy]; !ok {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init",
			"--barrier-strategy must be one of none, passphrase, shamir")
	}
	// The "not allowed" case keys off what the operator typed, not off the
	// resolved value: an exported QUICBAAS_BARRIER_PASSPHRASE left over from
	// an unseal must not make --barrier-strategy=none fail.
	if barrierStrategy == "none" && passphraseRequested(cmd) {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init",
			"--passphrase is not allowed when --barrier-strategy=none")
	}
	secret, err := resolvePassphrase(cmd, "barrier.init", barrierSecret)
	if err != nil {
		return formatError(err)
	}
	defer secret.Destroy()
	if barrierStrategy == "none" {
		secret.Destroy()
	}
	if barrierStrategy != "none" && secret.Size() == 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init",
			"--passphrase is required when --barrier-strategy=%s%s",
			barrierStrategy, passphraseSourcesHint)
	}
	if barrierStrategy == "none" && (barrierSOPin != "" || barrierUserPin != "") {
		return formatError(&PINsNotAllowedError{Strategy: "none", Op: "init"})
	}
	if barrierUserPin != "" && barrierSOPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init",
			"--user-pin requires --so-pin")
	}
	if barrierSOPin != "" && barrierUserPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init",
			"--so-pin requires --user-pin (FIPS 140-3 / PCI-DSS v4 dual-control)")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	return formatError(withPassphraseBytes(secret, func(b []byte) error {
		return client.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{
			Secret:          b,
			BarrierStrategy: barrierStrategy,
			SOPin:           barrierSOPin,
			UserPin:         barrierUserPin,
		})
	}))
}

// =============================================================================
// init-shamir
// =============================================================================

// newBarrierInitShamirCommand builds 'quicbaas barrier init-shamir'.
func newBarrierInitShamirCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init-shamir",
		Short: "Initialize the barrier with Shamir M-of-N secret sharing",
		Long: `Initialize the barrier using Shamir M-of-N secret sharing.

The server generates the unseal key and seals --total shares to the
enrolled Security Officers, of which --threshold are required to unseal.
No secret travels on the request and no share is printed: each officer
claims their own share. --policy selects which enrolled officers receive
a share (designated or any-enrolled). PIN flags are optional but follow
the dual-control rule -- supplying --so-pin requires --user-pin.

Example:
  quicbaas barrier init-shamir --threshold 3 --total 5 --policy designated`,
		RunE: runBarrierInitShamirCommand,
	}
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Minimum shares required to unseal (M)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"Total number of shares to generate (N)")
	cmd.Flags().StringVar(&barrierPolicy, "policy", "",
		"Share distribution policy (designated, any-enrolled; server default when empty)")
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (optional; requires --user-pin)")
	cmd.Flags().StringVar(&barrierUserPin, "user-pin", "",
		"User PIN (optional; requires --so-pin)")
	return cmd
}

// runBarrierInitShamirCommand executes 'barrier init-shamir'.
func runBarrierInitShamirCommand(cmd *cobra.Command, _ []string) error {
	if barrierThreshold <= 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir", "--threshold must be > 0")
	}
	if barrierTotalShares <= 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir", "--total must be > 0")
	}
	if barrierThreshold > barrierTotalShares {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir", "--threshold cannot exceed --total")
	}
	if barrierUserPin != "" && barrierSOPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir",
			"--user-pin requires --so-pin")
	}
	if barrierSOPin != "" && barrierUserPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir",
			"--so-pin requires --user-pin (FIPS 140-3 / PCI-DSS v4 dual-control)")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{
		Threshold:   barrierThreshold,
		TotalShares: barrierTotalShares,
		Policy:      barrierPolicy,
		SOPin:       barrierSOPin,
		UserPin:     barrierUserPin,
	})
	if err != nil {
		return formatError(err)
	}

	return formatBarrierShamirResult(resp, barrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// unseal (passphrase replay only)
// =============================================================================

// newBarrierUnsealCommand builds 'quicbaas barrier unseal'. The canonical
// surface narrows this verb to the passphrase / single-secret strategy;
// Shamir share submission lives on 'barrier quorum begin' / 'quorum submit'.
func newBarrierUnsealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unseal",
		Short: "Unseal the barrier using a passphrase",
		Long: `Unseal the barrier using the master passphrase that was supplied at
'barrier init' time.

This verb is reserved for the passphrase / single-secret strategy. For
Shamir M-of-N unseal each holder runs 'barrier quorum begin --op unseal'
and then 'barrier quorum submit --op unseal' with their opened share in
--share-file.

` + passphraseInputHelp + `

Examples:
  # Preferred
  printf '%s' "$PP" | quicbaas barrier unseal --passphrase-stdin

  # Environment
  QUICBAAS_BARRIER_PASSPHRASE="$PP" quicbaas barrier unseal

  # Explicit flag (development)
  quicbaas barrier unseal --passphrase '<passphrase>'`,
		RunE: runBarrierUnsealCommand,
	}
	addPassphraseFlags(cmd, &barrierSecret, "Master passphrase")
	return cmd
}

// runBarrierUnsealCommand executes 'barrier unseal'.
func runBarrierUnsealCommand(cmd *cobra.Command, _ []string) error {
	secret, err := resolvePassphrase(cmd, "barrier.unseal", barrierSecret)
	if err != nil {
		return formatError(err)
	}
	defer secret.Destroy()
	if secret.Size() == 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.unseal",
			"--passphrase is required"+passphraseSourcesHint)
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	return formatError(secret.WithBytes(func(b []byte) error {
		return client.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{Secret: b})
	}))
}

// =============================================================================
// seal
// =============================================================================

// newBarrierSealCommand builds 'quicbaas barrier seal'.
//
// The SDK's BarrierSeal takes no request, so the SO PIN rides the call
// context: the verb binds it with WithSOPIN and the server verifies it
// against its PIN backend before sealing (#946). The PIN is read from
// --so-pin-stdin or QUICBAAS_BARRIER_SO_PIN; --so-pin stays accepted to keep
// the flag surface in lock-step with the other barrier verbs, but the help
// never shows it in an example, because a PIN on argv sits in
// /proc/<pid>/cmdline for every local process to read (#491).
//
// TestBarrierSeal_HelpAndBehaviourAgreeOnTheSOPin holds the help and the
// behaviour together: the help asks for a PIN exactly when the verb forwards
// one.
func newBarrierSealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "Seal the cryptographic barrier",
		Long: `Seal the barrier, zeroing the data encryption key.

All cryptographic operations are blocked until the barrier is unsealed
again. By default the command prompts for confirmation; pass
--assume-yes to skip the prompt (e.g. in scripted runbooks).

A Security Officer PIN is required when the server has a PIN backend
configured: a seal without it, or with the wrong one, is refused and the
barrier stays unsealed. With no PIN backend the PIN is not checked.

` + soPINInputHelp + `

The PIN is read before the confirmation prompt, so pair --so-pin-stdin
with --assume-yes.

Examples:
  # Preferred
  printf '%s\n' "$SO_PIN" | quicbaas barrier seal --so-pin-stdin --assume-yes

  # Environment
  QUICBAAS_BARRIER_SO_PIN="$SO_PIN" quicbaas barrier seal -y

  # No PIN backend configured
  quicbaas barrier seal -y`,
		RunE: runBarrierSealCommand,
	}
	addSOPINFlags(cmd, &barrierSOPin)
	cmd.Flags().BoolVarP(&barrierAssumeYes, "assume-yes", "y", false,
		"Skip the interactive confirmation prompt")
	return cmd
}

// runBarrierSealCommand executes 'barrier seal'.
func runBarrierSealCommand(cmd *cobra.Command, _ []string) error {
	// The PIN is resolved before the prompt so --so-pin-stdin consumes its
	// own line and nothing else.
	soPIN, err := resolveSOPIN(cmd, "barrier.seal", barrierSOPin)
	if err != nil {
		return formatError(err)
	}
	defer soPIN.Destroy()

	if !barrierAssumeYes {
		fmt.Fprint(cmd.OutOrStdout(), "Are you sure you want to seal the barrier? (yes/no): ")
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput,
				"barrier.seal", "failed to read confirmation")
		}
		if strings.TrimSpace(strings.ToLower(response)) != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), "Seal cancelled")
			return nil
		}
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	ctx, release, err := bindSOPIN(ctx, "barrier.seal", soPIN)
	if err != nil {
		return formatError(err)
	}
	defer release()

	if err := client.BarrierSeal(ctx); err != nil {
		return formatError(err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Barrier sealed successfully")
	return nil
}

// =============================================================================
// recovery
// =============================================================================

// newBarrierRecoveryCommand builds the 'quicbaas barrier recovery' group.
func newBarrierRecoveryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recovery",
		Short: "Recovery key management",
		Long: `Generate and use recovery keys for disaster recovery scenarios.

Canonical surface:
  recovery generate   Generate M-of-N recovery keys

Recovery is completed as a quorum round on the recovery domain:
  quicbaas barrier quorum submit --domain recovery --op recover ...`,
	}
	cmd.AddCommand(newBarrierRecoveryGenerateCommand())
	return cmd
}

// newBarrierRecoveryGenerateCommand builds 'barrier recovery generate'.
func newBarrierRecoveryGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate M-of-N recovery keys",
		Long: `Generate fresh recovery keys.

Recovery keys are independent of the barrier's unseal shares. The server
seals one share to each enrolled recovery officer that --policy selects,
so the share count is the enrolled officer count; no key material is
returned. Generation requires a Security Officer PIN when a PINBackend is
configured.

Example:
  quicbaas barrier recovery generate --threshold 3 --policy designated --so-pin SO123`,
		RunE: runBarrierRecoveryGenerateCommand,
	}
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Minimum keys required for recovery (M)")
	cmd.Flags().StringVar(&barrierPolicy, "policy", "",
		"Share distribution policy (designated, any-enrolled; server default when empty)")
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (required when a PINBackend is configured)")
	return cmd
}

// runBarrierRecoveryGenerateCommand executes 'barrier recovery generate'.
func runBarrierRecoveryGenerateCommand(cmd *cobra.Command, _ []string) error {
	if barrierThreshold < 2 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.recovery.generate",
			"--threshold must be >= 2")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{
		Threshold: barrierThreshold,
		Policy:    barrierPolicy,
		SOPin:     barrierSOPin,
	})
	if err != nil {
		return formatError(err)
	}

	return formatBarrierRecoveryKeys(resp, barrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Output formatting
// =============================================================================

// formatBarrierStatusResponse renders a BarrierStatusResponse.
func formatBarrierStatusResponse(resp *qrdbtransport.BarrierStatusResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"sealed":          resp.Sealed,
			"strategy":        resp.Strategy,
			"hardware_backed": resp.HardwareBacked,
			"initialized_at":  resp.InitializedAt,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		state := "unsealed"
		if resp.Sealed {
			state = "sealed"
		}
		fmt.Fprintf(tw, "BARRIER STATUS\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "State:\t%s\n", state)
		fmt.Fprintf(tw, "Strategy:\t%s\n", resp.Strategy)
		fmt.Fprintf(tw, "Hardware Backed:\t%t\n", resp.HardwareBacked)
		if resp.InitializedAt != "" {
			fmt.Fprintf(tw, "Initialized At:\t%s\n", resp.InitializedAt)
		}
		return tw.Flush()
	default:
		fmt.Fprintf(w, "Sealed: %v\n", resp.Sealed)
		fmt.Fprintf(w, "Strategy: %s\n", resp.Strategy)
		return nil
	}
}

// formatBarrierShamirResult renders a Shamir init response. Share material is
// never returned over the wire (qrdb share-free contract): shares are sealed
// to enrolled Security Officers, who claim them individually via the
// share-claim flow.
func formatBarrierShamirResult(resp *qrdbtransport.BarrierInitializeShamirResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"threshold":    resp.Threshold,
			"total_shares": resp.TotalShares,
			"share_count":  resp.ShareCount,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nSHAMIR SECRET SHARING INITIALIZED\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "Threshold:\t%d of %d shares required\n", resp.Threshold, resp.TotalShares)
		fmt.Fprintf(tw, "Shares Sealed:\t%d\n", resp.ShareCount)
		fmt.Fprintf(tw, "\nShares were sealed to the enrolled Security Officers. Each officer\nclaims their share individually via the share-claim flow.\n")
		return tw.Flush()
	default:
		fmt.Fprintf(w, "Threshold: %d of %d\n", resp.Threshold, resp.TotalShares)
		fmt.Fprintf(w, "Shares sealed to officers: %d\n", resp.ShareCount)
		return nil
	}
}

// formatBarrierRecoveryKeys renders a recovery-key generation response.
// Recovery key material is never returned over the wire (qrdb share-free
// contract): recovery shares are sealed to enrolled recovery officers, who
// claim them individually via the recovery share-claim flow.
func formatBarrierRecoveryKeys(resp *qrdbtransport.BarrierRecoveryKeysResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"threshold":   resp.Threshold,
			"total":       resp.Total,
			"share_count": resp.ShareCount,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nRECOVERY KEYS GENERATED\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "Threshold:\t%d of %d keys required\n", resp.Threshold, resp.Total)
		fmt.Fprintf(tw, "Keys Sealed:\t%d\n", resp.ShareCount)
		fmt.Fprintf(tw, "\nRecovery keys were sealed to the enrolled recovery officers. Each officer\nclaims their key individually via the recovery share-claim flow.\n")
		return tw.Flush()
	default:
		fmt.Fprintf(w, "Threshold: %d of %d\n", resp.Threshold, resp.Total)
		fmt.Fprintf(w, "Keys sealed to officers: %d\n", resp.ShareCount)
		return nil
	}
}

// =============================================================================
// enable-encryption
// =============================================================================

// newBarrierEnableEncryptionCommand builds 'quicbaas barrier enable-encryption'.
func newBarrierEnableEncryptionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable-encryption",
		Short: "Migrate an unencrypted barrier to an encrypted one",
		Long: `Enable at-rest encryption on a deployment that was initialised
without a barrier.

The migration re-encrypts existing data, so it requires an UNSEALED data
plane and cannot run during the sealed window. Scope it to one tenant
with --tenant, or omit it to migrate the system barrier.

The migration runs asynchronously: the server answers with a status of
"migration_started" and the scope it migrated ("system" or "tenant").
Watch the barrier you migrated for the strategy change:
'quicbaas barrier status' for the system barrier, or
'quicbaas iam tenant barrier status --tenant <id>' for a tenant's ('barrier
status' reads only the system barrier). If --tenant was given and the server reports any other scope, the verb fails
rather than report the wrong barrier's migration as success.

A tenant on a SHARED barrier cannot be migrated independently — the
server rejects that as an entitlement violation, not an auth failure.

` + passphraseInputHelp + `

Example:
  printf '%s' "$PP" | quicbaas barrier enable-encryption \
      --strategy passphrase --passphrase-stdin
  quicbaas barrier enable-encryption --tenant acme --strategy shamir \
      --threshold 2 --total 3`,
		RunE: runBarrierEnableEncryptionCommand,
	}
	cmd.Flags().StringVar(&barrierTenantID, "tenant", "",
		"Tenant to migrate (omit for the system barrier)")
	cmd.Flags().StringVar(&barrierStrategy, "strategy", "",
		"Target seal strategy: none | passphrase | shamir | pkcs11 | tpm2 | awskms | gcpkms | azurekv | vault")
	cmd.Flags().StringVar(&barrierMechanism, "mechanism", "",
		"Backend-specific mechanism (hardware strategies)")
	cmd.Flags().StringVar(&barrierAvailability, "availability", "",
		"Backend availability hint (hardware strategies)")
	addPassphraseFlags(cmd, &barrierSecret,
		"Passphrase (required for --strategy=passphrase)")
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Shamir M-of-N threshold (required for --strategy=shamir)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"Total Shamir shares (required for --strategy=shamir)")
	return cmd
}

// runBarrierEnableEncryptionCommand executes 'barrier enable-encryption'.
func runBarrierEnableEncryptionCommand(cmd *cobra.Command, _ []string) error {
	if barrierStrategy == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.enable-encryption",
			"--strategy is required")
	}
	secret, err := resolvePassphrase(cmd, "barrier.enable-encryption", barrierSecret)
	if err != nil {
		return formatError(err)
	}
	defer secret.Destroy()
	secret = passphraseForStrategy(cmd, barrierStrategy, secret)
	// Fail locally on a combination the server would reject anyway, so the
	// operator learns before a migration is attempted against live data.
	if isPassphraseStrategyName(barrierStrategy) && secret.Size() == 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.enable-encryption",
			"--passphrase is required when --strategy=passphrase"+passphraseSourcesHint)
	}
	if barrierStrategy == "shamir" {
		if barrierThreshold < 2 {
			return errors.Newf(errors.ErrInvalidInput, "barrier.enable-encryption",
				"--threshold must be >= 2 when --strategy=shamir")
		}
		if barrierTotalShares < barrierThreshold {
			return errors.Newf(errors.ErrInvalidInput, "barrier.enable-encryption",
				"--total must be >= --threshold")
		}
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	var resp *qrdbtransport.BarrierEnableEncryptionResponse
	err = withPassphraseBytes(secret, func(b []byte) error {
		var callErr error
		resp, callErr = client.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{
			TenantID:        barrierTenantID,
			Strategy:        barrierStrategy,
			Mechanism:       barrierMechanism,
			Availability:    barrierAvailability,
			Passphrase:      b,
			ShamirThreshold: barrierThreshold,
			ShamirTotal:     barrierTotalShares,
		})
		return callErr
	})
	if err != nil {
		return formatError(err)
	}
	if barrierTenantID != "" && resp != nil && resp.Scope != barrierEnableEncryptionTenantScope {
		return &BarrierScopeMismatchError{TenantID: barrierTenantID, Scope: resp.Scope}
	}
	return formatBarrierEnableEncryption(resp, barrierOutputFmt, cmd.OutOrStdout())
}

// barrierEnableEncryptionTenantScope is the `scope` go-qrdb's wire reports
// for a migration of one tenant's dedicated barrier.
const barrierEnableEncryptionTenantScope = "tenant"

// isPassphraseStrategyName reports whether the strategy name selects the
// passphrase-sealed barrier.
func isPassphraseStrategyName(strategy string) bool {
	return strategy == "passphrase" || strategy == "software"
}

// formatBarrierEnableEncryption renders the migration acknowledgement.
func formatBarrierEnableEncryption(
	resp *qrdbtransport.BarrierEnableEncryptionResponse,
	format string,
	w io.Writer,
) error {
	if resp == nil {
		return errors.Newf(errors.ErrInternal, "barrier.enable-encryption", "empty response")
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
	fmt.Fprintf(tw, "Status:\t%s\n", resp.Status)
	if resp.Scope != "" {
		fmt.Fprintf(tw, "Scope:\t%s\n", resp.Scope)
	}
	return tw.Flush()
}
