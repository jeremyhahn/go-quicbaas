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
//	barrier init-shamir    --passphrase-stdin | --passphrase <passphrase>
//	                       --threshold N
//	                       --total M
//	                       [--so-pin] [--user-pin]
//	barrier unseal         --passphrase-stdin | --passphrase <passphrase>
//	                                                    (strategy passphrase replay)
//	barrier unseal-shares  --share <s1> [--share <s2> ...]   (repeatable)
//	                       | --shares-file <path>       ('-' = stdin)
//	barrier seal           [--so-pin-stdin] [-y|--assume-yes]
//	barrier rekey          [--so-pin <pin>]
//	                       [--threshold N --total M]    (paired; for Shamir rekey)
//	barrier recovery generate   --threshold N --total M [--so-pin <pin>]
//	barrier recovery recover    --share <s1> [--share <s2> ...]   (repeatable)
//	                            | --shares-file <path>       ('-' = stdin)
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
	barrierSecret         string
	barrierShares         []string
	barrierSharesFile     string
	barrierThreshold      int
	barrierTotalShares    int
	barrierOutputFmt      string
	barrierAssumeYes      bool
	barrierStrategy       string
	barrierSOPin          string
	barrierUserPin        string
	barrierRecoveryShares []string
	// barrierRecoverySharesFile is separate from barrierSharesFile on purpose:
	// `barrier recovery recover` collects a different quorum from
	// `unseal-shares` / `root-token`, and one shared variable would let a
	// future verb read a path another verb parsed.
	barrierRecoverySharesFile string
	barrierTenantID           string
	barrierMechanism          string
	barrierAvailability       string
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
	BarrierUnsealWithShares(ctx context.Context, req *qrdbtransport.BarrierUnsealSharesRequest) error
	BarrierSeal(ctx context.Context) error
	BarrierRekey(ctx context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error)
	BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error)
	BarrierRecoverWithKeys(ctx context.Context, req *qrdbtransport.BarrierRecoverWithKeysRequest) error

	// BarrierGenerateRootToken mints a root token from a Shamir quorum. The
	// token is a break-glass credential: it is returned exactly once and is
	// never recoverable from the server afterwards.
	BarrierGenerateRootToken(ctx context.Context, req *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error)

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

	// OfficerList lists the enrolled Security Officers (public metadata only).
	OfficerList(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error)

	// OfficerClaimStatus reports per-officer enrolment and claim state. It
	// never returns share bytes.
	OfficerClaimStatus(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error)
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

// BarrierSharesFileError is returned when --shares-file cannot be read.
type BarrierSharesFileError struct {
	Path string
	// Op names the verb the operator invoked, for the same reason
	// BarrierShareInputError carries one: three verbs now read a share file
	// through readSharesFile, and a hard-coded name sends an operator who ran
	// `barrier root-token` or `barrier recovery recover` off to debug
	// `barrier unseal-shares`.
	Op  string
	Err error
}

// Error returns a human-readable description.
func (e *BarrierSharesFileError) Error() string {
	return sharesFileOp(e.Op) + ": failed to read --shares-file " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *BarrierSharesFileError) Unwrap() error { return e.Err }

// BarrierSharesFileEmptyError is returned when --shares-file contained
// no non-empty lines.
type BarrierSharesFileEmptyError struct {
	Path string
	// Op names the invoking verb; see BarrierSharesFileError.Op.
	Op string
}

// Error returns a human-readable description.
func (e *BarrierSharesFileEmptyError) Error() string {
	return sharesFileOp(e.Op) + ": --shares-file " + e.Path + " contains no shares"
}

// sharesFileOp supplies the historical verb name when a caller left Op unset,
// so an error value built without it still reads as a sentence.
func sharesFileOp(op string) string {
	if op == "" {
		return "barrier unseal-shares"
	}
	return op
}

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

// BarrierShareInputError is returned when the --share / --shares-file
// invariants of `barrier unseal-shares` are violated.
type BarrierShareInputError struct {
	// Op names the verb the operator invoked. Several verbs collect a share
	// quorum, so a hard-coded name here would send an operator who ran
	// `barrier root-token` off to debug `barrier unseal-shares`.
	Op     string
	Reason string
}

// Error returns a human-readable description.
func (e *BarrierShareInputError) Error() string {
	op := e.Op
	if op == "" {
		op = "barrier unseal-shares"
	}
	return op + ": " + e.Reason
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
  unseal-shares  Unseal with one or more Shamir shares (--share/--shares-file)
  seal           Seal the barrier
  rekey          Rotate the barrier master key (optionally regenerate Shamir shares)
  recovery       Generate or use recovery keys (generate | recover)
  root-token     Mint a break-glass root token from a Shamir quorum
  enable-encryption
                 Migrate an unencrypted barrier to an encrypted one`,
	}

	cmd.PersistentFlags().StringVar(&barrierOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newBarrierStatusCommand())
	cmd.AddCommand(newBarrierInitCommand())
	cmd.AddCommand(newBarrierInitShamirCommand())
	cmd.AddCommand(newBarrierUnsealCommand())
	cmd.AddCommand(newBarrierUnsealSharesCommand())
	cmd.AddCommand(newBarrierSealCommand())
	cmd.AddCommand(newBarrierRekeyCommand())
	cmd.AddCommand(newBarrierRecoveryCommand())
	cmd.AddCommand(newBarrierRootTokenCommand())
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
                        --so-pin-file so.pin --user-pin-file user.pin`,
		RunE: runBarrierInitCommand,
	}
	cmd.Flags().StringVar(&barrierStrategy, "barrier-strategy", "passphrase",
		"Barrier strategy: none | passphrase | shamir")
	addPassphraseFlags(cmd, &barrierSecret,
		"Master passphrase (required for passphrase and shamir)")
	addPINFlags(cmd, "optional; both or neither; ",
		soPINInput(&barrierSOPin), userPINInput(&barrierUserPin))
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
	// As with the passphrase, "not allowed" keys off what the operator
	// typed: a leftover QUICBAAS_SO_PIN must not fail --barrier-strategy=none.
	if barrierStrategy == "none" && pinFlagsRequested(cmd) {
		return formatError(&PINsNotAllowedError{Strategy: "none", Op: "init"})
	}
	if barrierStrategy != "none" {
		release, err := resolvePINs(cmd, "barrier.init",
			soPINInput(&barrierSOPin), userPINInput(&barrierUserPin))
		if err != nil {
			return formatError(err)
		}
		defer release()
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

	if err := client.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{
		//secure:unborrowed injected-interface barrier transport request must carry bytes; the client owns the request lifetime.
		Secret:          secret.Bytes(),
		BarrierStrategy: barrierStrategy,
		SOPin:           barrierSOPin,
		UserPin:         barrierUserPin,
	}); err != nil {
		return formatError(err)
	}

	return nil
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

The master secret is split into --total shares of which --threshold are
required to unseal. PIN flags are optional but follow the dual-control
rule -- supplying --so-pin requires --user-pin.

The generated shares are printed once; distribute them to custodians
immediately and out-of-band.

` + passphraseInputHelp + `

Example:
  printf '%s' "$SEED" | quicbaas barrier init-shamir --passphrase-stdin \
                                                     --threshold 3 --total 5`,
		RunE: runBarrierInitShamirCommand,
	}
	addPassphraseFlags(cmd, &barrierSecret, "Master passphrase")
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Minimum shares required to unseal (M)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"Total number of shares to generate (N)")
	addPINFlags(cmd, "optional; both or neither; ",
		soPINInput(&barrierSOPin), userPINInput(&barrierUserPin))
	return cmd
}

// runBarrierInitShamirCommand executes 'barrier init-shamir'.
func runBarrierInitShamirCommand(cmd *cobra.Command, _ []string) error {
	secret, err := resolvePassphrase(cmd, "barrier.init-shamir", barrierSecret)
	if err != nil {
		return formatError(err)
	}
	defer secret.Destroy()
	if secret.Size() == 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir",
			"--passphrase is required"+passphraseSourcesHint)
	}
	if barrierThreshold <= 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir", "--threshold must be > 0")
	}
	if barrierTotalShares <= 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir", "--total must be > 0")
	}
	if barrierThreshold > barrierTotalShares {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir", "--threshold cannot exceed --total")
	}
	release, err := resolvePINs(cmd, "barrier.init-shamir",
		soPINInput(&barrierSOPin), userPINInput(&barrierUserPin))
	if err != nil {
		return formatError(err)
	}
	defer release()
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
		//secure:unborrowed injected-interface barrier transport request must carry bytes; the client owns the request lifetime.
		Secret:      secret.Bytes(),
		Threshold:   barrierThreshold,
		TotalShares: barrierTotalShares,
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
// Shamir share submission lives on 'barrier unseal-shares'.
func newBarrierUnsealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unseal",
		Short: "Unseal the barrier using a passphrase",
		Long: `Unseal the barrier using the master passphrase that was supplied at
'barrier init' time.

This verb is reserved for the passphrase / single-secret strategy. For
Shamir M-of-N unseal use 'barrier unseal-shares' with --share or
--shares-file.

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

	return formatError(client.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{
		//secure:unborrowed injected-interface barrier transport request must carry bytes; the client owns the request lifetime.
		Secret: secret.Bytes(),
	}))
}

// =============================================================================
// unseal-shares
// =============================================================================

// newBarrierUnsealSharesCommand builds 'quicbaas barrier unseal-shares'.
// Operators may supply one or more `--share` flags (repeatable) or point
// at a `--shares-file` with one share per line (`-` reads stdin). The
// two forms are mutually exclusive.
func newBarrierUnsealSharesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unseal-shares",
		Short: "Unseal the barrier by submitting Shamir shares",
		Long: `Unseal the barrier using one or more Shamir shares.

Two input modes are supported. Exactly one must be supplied:

  --share <value>           Repeatable. Supply one or more shares as flags.
  --shares-file <path>      Newline-delimited file of shares. Use '-' for stdin.

Submitting a number of shares >= the M threshold unseals the barrier in
one call. Submitting fewer shares accumulates them toward the threshold.

Examples:
  quicbaas barrier unseal-shares --share aaa --share bbb --share ccc
  quicbaas barrier unseal-shares --shares-file /etc/quicbaas/shares.txt
  cat shares.txt | quicbaas barrier unseal-shares --shares-file -`,
		RunE: runBarrierUnsealSharesCommand,
	}
	cmd.Flags().StringSliceVar(&barrierShares, "share", nil,
		"Single Shamir share (repeatable: --share a --share b ...)")
	cmd.Flags().StringVar(&barrierSharesFile, "shares-file", "",
		"Path to a newline-delimited share file; '-' for stdin")
	return cmd
}

// runBarrierUnsealSharesCommand executes 'barrier unseal-shares'.
func runBarrierUnsealSharesCommand(_ *cobra.Command, _ []string) error {
	shares, err := resolveUnsealShares(barrierShares, barrierSharesFile, "barrier unseal-shares")
	if err != nil {
		return formatError(err)
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	return formatError(client.BarrierUnsealWithShares(ctx, &qrdbtransport.BarrierUnsealSharesRequest{
		Shares: shares,
	}))
}

// resolveUnsealShares enforces the --share / --shares-file invariants
// and returns the flattened share list. op names the invoking verb so the
// error points at the command the operator actually ran.
func resolveUnsealShares(shares []string, sharesFile, op string) ([]string, error) {
	hasFlag := len(shares) > 0
	hasFile := sharesFile != ""
	if hasFlag && hasFile {
		return nil, &BarrierShareInputError{
			Op:     op,
			Reason: "exactly one of --share (repeatable) or --shares-file is allowed",
		}
	}
	if !hasFlag && !hasFile {
		return nil, &BarrierShareInputError{
			Op:     op,
			Reason: "one of --share (repeatable) or --shares-file is required",
		}
	}
	if hasFlag {
		return shares, nil
	}
	return readSharesFile(sharesFile, op)
}

// readSharesFile reads one share per line from path. Blank lines and
// '#' comments are skipped. The literal path "-" reads from stdin. op names
// the invoking verb and rides on the returned errors.
func readSharesFile(path, op string) ([]string, error) {
	var src io.Reader
	if path == "-" {
		src = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, &BarrierSharesFileError{Path: path, Op: op, Err: err}
		}
		defer func() { _ = f.Close() }()
		src = f
	}

	var shares []string
	scanner := bufio.NewScanner(src)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		shares = append(shares, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, &BarrierSharesFileError{Path: path, Op: op, Err: err}
	}
	if len(shares) == 0 {
		return nil, &BarrierSharesFileEmptyError{Path: path, Op: op}
	}
	return shares, nil
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
// rekey
// =============================================================================

// newBarrierRekeyCommand builds 'quicbaas barrier rekey'.
func newBarrierRekeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rekey",
		Short: "Rotate the barrier Shamir unseal key and share set",
		Long: `Rotate the barrier's Shamir unseal key and share set.

The server generates a fresh unseal key, re-wraps the root key under it,
and seals new shares to the enrolled Security Officers; all previously
distributed shares are cryptographically revoked. No secret material
travels on the request. --so-pin is required when a PINBackend is
configured. --threshold and --total are paired and instruct the server
to regenerate the share set with a new M/N.

Examples:
  printf '%s\n' "$SO_PIN" | quicbaas barrier rekey --so-pin-file -
  quicbaas barrier rekey --threshold 3 --total 5`,
		RunE: runBarrierRekeyCommand,
	}
	addPINFlags(cmd, "required when a PINBackend is configured; ", soPINInput(&barrierSOPin))
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"New Shamir threshold (paired with --total)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"New Shamir total share count (paired with --threshold)")
	return cmd
}

// runBarrierRekeyCommand executes 'barrier rekey'.
func runBarrierRekeyCommand(cmd *cobra.Command, _ []string) error {
	// Shamir flags are paired: supplying one requires the other.
	if (barrierThreshold > 0) != (barrierTotalShares > 0) {
		return errors.Newf(errors.ErrInvalidInput, "barrier.rekey",
			"--threshold and --total must be supplied together when rekeying Shamir shares")
	}
	if barrierThreshold > 0 && barrierThreshold < 2 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.rekey",
			"--threshold must be >= 2")
	}
	if barrierTotalShares > 0 && barrierTotalShares < barrierThreshold {
		return errors.Newf(errors.ErrInvalidInput, "barrier.rekey",
			"--total must be >= --threshold")
	}
	release, err := resolvePINs(cmd, "barrier.rekey", soPINInput(&barrierSOPin))
	if err != nil {
		return formatError(err)
	}
	defer release()

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{
		SOPin:     barrierSOPin,
		Threshold: barrierThreshold,
		Total:     barrierTotalShares,
	})
	if err != nil {
		return formatError(err)
	}

	return formatBarrierRekeyResult(resp, barrierOutputFmt, cmd.OutOrStdout())
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
  recovery recover    Restore the barrier from recovery keys`,
	}
	cmd.AddCommand(newBarrierRecoveryGenerateCommand())
	cmd.AddCommand(newBarrierRecoveryRecoverCommand())
	return cmd
}

// newBarrierRecoveryGenerateCommand builds 'barrier recovery generate'.
func newBarrierRecoveryGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate M-of-N recovery keys",
		Long: `Generate fresh recovery keys.

Recovery keys are independent of the barrier's unseal shares and live in
a parallel KMS slot. Generation requires a Security Officer PIN when a
PINBackend is configured.

Example:
  quicbaas barrier recovery generate --threshold 3 --total 5 --so-pin-file /run/secrets/so-pin`,
		RunE: runBarrierRecoveryGenerateCommand,
	}
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Minimum keys required for recovery (M)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"Total number of recovery keys (N)")
	addPINFlags(cmd, "required when a PINBackend is configured; ", soPINInput(&barrierSOPin))
	return cmd
}

// runBarrierRecoveryGenerateCommand executes 'barrier recovery generate'.
func runBarrierRecoveryGenerateCommand(cmd *cobra.Command, _ []string) error {
	if barrierThreshold < 2 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.recovery.generate",
			"--threshold must be >= 2")
	}
	if barrierTotalShares < barrierThreshold {
		return errors.Newf(errors.ErrInvalidInput, "barrier.recovery.generate",
			"--total must be >= --threshold")
	}
	release, err := resolvePINs(cmd, "barrier.recovery.generate", soPINInput(&barrierSOPin))
	if err != nil {
		return formatError(err)
	}
	defer release()

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{
		Threshold: barrierThreshold,
		Total:     barrierTotalShares,
		SOPin:     barrierSOPin,
	})
	if err != nil {
		return formatError(err)
	}

	return formatBarrierRecoveryKeys(resp, barrierOutputFmt, cmd.OutOrStdout())
}

// newBarrierRecoveryRecoverCommand builds 'barrier recovery recover'.
func newBarrierRecoveryRecoverCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Recover the barrier using recovery keys",
		Long: `Recover the barrier by submitting M-of-N recovery keys.

Two input modes are supported. Exactly one must be supplied:

  --share <value>           Repeatable. Supply one or more keys as flags.
  --shares-file <path>      Newline-delimited file of keys. Use '-' for stdin.

A recovery key on argv is readable by every process on the host through
/proc/<pid>/cmdline for the life of the call. Prefer --shares-file, and
--shares-file - to keep the quorum off the filesystem as well.

Examples:
  quicbaas barrier recovery recover --share key-a --share key-b --share key-c
  quicbaas barrier recovery recover --shares-file /etc/quicbaas/recovery.txt
  cat recovery.txt | quicbaas barrier recovery recover --shares-file -`,
		RunE: runBarrierRecoveryRecoverCommand,
	}
	cmd.Flags().StringSliceVar(&barrierRecoveryShares, "share", nil,
		"Recovery key (repeatable: --share a --share b ...)")
	cmd.Flags().StringVar(&barrierRecoverySharesFile, "shares-file", "",
		"Path to a newline-delimited recovery-key file; '-' for stdin")
	return cmd
}

// runBarrierRecoveryRecoverCommand executes 'barrier recovery recover'.
func runBarrierRecoveryRecoverCommand(_ *cobra.Command, _ []string) error {
	keys, err := resolveUnsealShares(barrierRecoveryShares, barrierRecoverySharesFile,
		"barrier recovery recover")
	if err != nil {
		return formatError(err)
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	return formatError(client.BarrierRecoverWithKeys(ctx, &qrdbtransport.BarrierRecoverWithKeysRequest{
		Keys: keys,
	}))
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

// formatBarrierRekeyResult renders a Rekey response. New share material is
// never returned over the wire (qrdb share-free contract): shares are sealed
// to enrolled Security Officers, who claim them individually via the
// share-claim flow.
func formatBarrierRekeyResult(resp *qrdbtransport.BarrierRekeyResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "\nBARRIER REKEY COMPLETE\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "New Threshold:\t%d of %d\n", resp.Threshold, resp.TotalShares)
		fmt.Fprintf(tw, "Shares Sealed:\t%d\n", resp.ShareCount)
		fmt.Fprintf(tw, "\nNew shares were sealed to the enrolled Security Officers. Each officer\nclaims their share individually via the share-claim flow.\n")
		return tw.Flush()
	default:
		fmt.Fprintf(w, "New Threshold: %d of %d\n", resp.Threshold, resp.TotalShares)
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
// root-token
// =============================================================================

// newBarrierRootTokenCommand builds 'quicbaas barrier root-token'.
func newBarrierRootTokenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "root-token",
		Short: "Mint a break-glass root token from a Shamir quorum",
		Long: `Mint a root token.

A quorum of Security Officers each supply their share; the server
reconstructs the secret and issues a single root token. This is a
break-glass credential — it is returned EXACTLY ONCE and cannot be
retrieved from the server afterwards. Capture it at the point of issue
and store it under the same controls as the shares themselves.

Example:
  quicbaas barrier root-token --share "$SHARE_A" --share "$SHARE_B"`,
		RunE: runBarrierRootTokenCommand,
	}
	cmd.Flags().StringSliceVar(&barrierShares, "share", nil,
		"Shamir share (repeatable; a quorum is required)")
	cmd.Flags().StringVar(&barrierSharesFile, "shares-file", "",
		"File containing one share per line ('-' reads stdin)")
	return cmd
}

// runBarrierRootTokenCommand executes 'barrier root-token'.
func runBarrierRootTokenCommand(cmd *cobra.Command, _ []string) error {
	shares, err := resolveUnsealShares(barrierShares, barrierSharesFile, "barrier root-token")
	if err != nil {
		return formatError(err)
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.BarrierGenerateRootToken(ctx, &qrdbtransport.BarrierGenerateRootTokenRequest{
		Shares: shares,
	})
	if err != nil {
		return formatError(err)
	}
	return formatBarrierRootToken(resp, barrierOutputFmt, cmd.OutOrStdout())
}

// formatBarrierRootToken renders the minted token.
func formatBarrierRootToken(
	resp *qrdbtransport.BarrierRootTokenResponse,
	format string,
	w io.Writer,
) error {
	if resp == nil {
		return errors.Newf(errors.ErrInternal, "barrier.root-token", "empty response")
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
	fmt.Fprintf(tw, "ROOT TOKEN\n")
	fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
	fmt.Fprintf(tw, "Token:\t%s\n", resp.Token)
	if resp.CreatedAt != "" {
		fmt.Fprintf(tw, "Created:\t%s\n", resp.CreatedAt)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This token is shown ONCE and cannot be retrieved again.")
	return nil
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

	resp, err := client.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{
		TenantID:     barrierTenantID,
		Strategy:     barrierStrategy,
		Mechanism:    barrierMechanism,
		Availability: barrierAvailability,
		//secure:unborrowed injected-interface first-boot request must carry bytes; the SDK owns the request lifetime.
		Passphrase:      secret.Bytes(),
		ShamirThreshold: barrierThreshold,
		ShamirTotal:     barrierTotalShares,
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
