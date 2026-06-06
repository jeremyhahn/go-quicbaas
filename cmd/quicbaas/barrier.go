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
//	                       [--passphrase <passphrase>]  (required for passphrase/shamir)
//	                       [--so-pin <pin>]
//	                       [--user-pin <pin>]           (requires --so-pin)
//	barrier init-shamir    --passphrase <passphrase>
//	                       --threshold N
//	                       --total M
//	                       [--so-pin] [--user-pin]
//	barrier unseal         --passphrase <passphrase>    (strategy passphrase replay)
//	barrier unseal-shares  --share <s1> [--share <s2> ...]   (repeatable)
//	                       | --shares-file <path>       ('-' = stdin)
//	barrier seal           [--so-pin <pin>] [-y|--assume-yes]
//	barrier rekey          --passphrase <new-passphrase>
//	                       [--so-pin <pin>]
//	                       [--threshold N --total M]    (paired; for Shamir rekey)
//	barrier recovery generate   --threshold N --total M [--so-pin <pin>]
//	barrier recovery recover    --share <s1> [--share <s2> ...]
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
	Err  error
}

// Error returns a human-readable description.
func (e *BarrierSharesFileError) Error() string {
	return "barrier unseal-shares: failed to read --shares-file " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *BarrierSharesFileError) Unwrap() error { return e.Err }

// BarrierSharesFileEmptyError is returned when --shares-file contained
// no non-empty lines.
type BarrierSharesFileEmptyError struct {
	Path string
}

// Error returns a human-readable description.
func (e *BarrierSharesFileEmptyError) Error() string {
	return "barrier unseal-shares: --shares-file " + e.Path + " contains no shares"
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
	Reason string
}

// Error returns a human-readable description.
func (e *BarrierShareInputError) Error() string {
	return "barrier unseal-shares: " + e.Reason
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
  recovery       Generate or use recovery keys (generate | recover)`,
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

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

--passphrase is required when --barrier-strategy is passphrase or shamir. PINs
are optional; when --so-pin is supplied --user-pin must also be supplied
so the resulting PIN pair satisfies FIPS 140-3 / PCI-DSS v4 dual-control.
PINs are rejected entirely when --barrier-strategy=none.

Examples:
  quicbaas barrier init --barrier-strategy passphrase --passphrase '<passphrase>'
  quicbaas barrier init --barrier-strategy none
  quicbaas barrier init --barrier-strategy passphrase --passphrase '<pp>' \
                        --so-pin SO123 --user-pin USR123`,
		RunE: runBarrierInitCommand,
	}
	cmd.Flags().StringVar(&barrierStrategy, "barrier-strategy", "passphrase",
		"Barrier strategy: none | passphrase | shamir")
	cmd.Flags().StringVar(&barrierSecret, "passphrase", "",
		"Master passphrase (required for passphrase and shamir)")
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (optional; requires --user-pin)")
	cmd.Flags().StringVar(&barrierUserPin, "user-pin", "",
		"User PIN (optional; requires --so-pin)")
	return cmd
}

// runBarrierInitCommand executes 'barrier init'.
func runBarrierInitCommand(_ *cobra.Command, _ []string) error {
	if _, ok := barrierStrategyInit[barrierStrategy]; !ok {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init",
			"--barrier-strategy must be one of none, passphrase, shamir")
	}
	if barrierStrategy != "none" && barrierSecret == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init",
			"--passphrase is required when --barrier-strategy=%s", barrierStrategy)
	}
	if barrierStrategy == "none" && barrierSecret != "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init",
			"--passphrase is not allowed when --barrier-strategy=none")
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

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{
		Secret:          barrierSecret,
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

Example:
  quicbaas barrier init-shamir --passphrase '<seed>' --threshold 3 --total 5`,
		RunE: runBarrierInitShamirCommand,
	}
	cmd.Flags().StringVar(&barrierSecret, "passphrase", "",
		"Master passphrase (required)")
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Minimum shares required to unseal (M)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"Total number of shares to generate (N)")
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (optional; requires --user-pin)")
	cmd.Flags().StringVar(&barrierUserPin, "user-pin", "",
		"User PIN (optional; requires --so-pin)")
	return cmd
}

// runBarrierInitShamirCommand executes 'barrier init-shamir'.
func runBarrierInitShamirCommand(cmd *cobra.Command, _ []string) error {
	if barrierSecret == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir", "--passphrase is required")
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

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := client.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{
		Secret:      barrierSecret,
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

Example:
  quicbaas barrier unseal --passphrase '<passphrase>'`,
		RunE: runBarrierUnsealCommand,
	}
	cmd.Flags().StringVar(&barrierSecret, "passphrase", "",
		"Master passphrase (required)")
	return cmd
}

// runBarrierUnsealCommand executes 'barrier unseal'.
func runBarrierUnsealCommand(_ *cobra.Command, _ []string) error {
	if barrierSecret == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.unseal", "--passphrase is required")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return formatError(client.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{
		Secret: barrierSecret,
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
	shares, err := resolveUnsealShares(barrierShares, barrierSharesFile)
	if err != nil {
		return formatError(err)
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return formatError(client.BarrierUnsealWithShares(ctx, &qrdbtransport.BarrierUnsealSharesRequest{
		Shares: shares,
	}))
}

// resolveUnsealShares enforces the --share / --shares-file invariants
// and returns the flattened share list.
func resolveUnsealShares(shares []string, sharesFile string) ([]string, error) {
	hasFlag := len(shares) > 0
	hasFile := sharesFile != ""
	if hasFlag && hasFile {
		return nil, &BarrierShareInputError{
			Reason: "exactly one of --share (repeatable) or --shares-file is allowed",
		}
	}
	if !hasFlag && !hasFile {
		return nil, &BarrierShareInputError{
			Reason: "one of --share (repeatable) or --shares-file is required",
		}
	}
	if hasFlag {
		return shares, nil
	}
	return readSharesFile(sharesFile)
}

// readSharesFile reads one share per line from path. Blank lines and
// '#' comments are skipped. The literal path "-" reads from stdin.
func readSharesFile(path string) ([]string, error) {
	var src io.Reader
	if path == "-" {
		src = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, &BarrierSharesFileError{Path: path, Err: err}
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
		return nil, &BarrierSharesFileError{Path: path, Err: err}
	}
	if len(shares) == 0 {
		return nil, &BarrierSharesFileEmptyError{Path: path}
	}
	return shares, nil
}

// =============================================================================
// seal
// =============================================================================

// newBarrierSealCommand builds 'quicbaas barrier seal'.
//
// The --so-pin flag is parsed for surface parity with the canonical CLI
// tree. PIN propagation rides on the SDK auth context (the same model
// the ceremony surface uses); the qrdb BarrierSealRequest currently
// carries SOPin server-side when a PINBackend is configured. The flag
// is accepted unconditionally to keep the four-CLI surface lock-step,
// and the value is forwarded once the SDK Barrier() interface exposes
// a request-bearing BarrierSeal.
func newBarrierSealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "Seal the cryptographic barrier",
		Long: `Seal the barrier, zeroing the data encryption key.

All cryptographic operations are blocked until the barrier is unsealed
again. By default the command prompts for confirmation; pass
--assume-yes to skip the prompt (e.g. in scripted runbooks).

Operators with a PINBackend configured MUST supply --so-pin.

Examples:
  quicbaas barrier seal -y
  quicbaas barrier seal --so-pin SO123 --assume-yes`,
		RunE: runBarrierSealCommand,
	}
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (required when a PINBackend is configured)")
	cmd.Flags().BoolVarP(&barrierAssumeYes, "assume-yes", "y", false,
		"Skip the interactive confirmation prompt")
	return cmd
}

// runBarrierSealCommand executes 'barrier seal'.
func runBarrierSealCommand(cmd *cobra.Command, _ []string) error {
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

	// --so-pin is parsed for surface parity; propagation is wired once
	// the SDK BarrierSeal exposes a request-bearing form.
	_ = barrierSOPin

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
		Short: "Rotate the barrier master key",
		Long: `Rotate the cryptographic barrier's master key.

--passphrase supplies the NEW passphrase; the server replaces the previously
sealed material. --so-pin is required when a PINBackend is configured.
--threshold and --total are paired and instruct the server to regenerate
Shamir share material with the new M/N.

Examples:
  quicbaas barrier rekey --passphrase '<new-pp>' --so-pin SO123
  quicbaas barrier rekey --passphrase '<new-pp>' --threshold 3 --total 5`,
		RunE: runBarrierRekeyCommand,
	}
	cmd.Flags().StringVar(&barrierSecret, "passphrase", "",
		"New master passphrase (required)")
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (required when a PINBackend is configured)")
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"New Shamir threshold (paired with --total)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"New Shamir total share count (paired with --threshold)")
	return cmd
}

// runBarrierRekeyCommand executes 'barrier rekey'.
func runBarrierRekeyCommand(cmd *cobra.Command, _ []string) error {
	if barrierSecret == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.rekey", "--passphrase is required")
	}
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

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := client.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{
		NewSecret: barrierSecret,
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
  quicbaas barrier recovery generate --threshold 3 --total 5 --so-pin SO123`,
		RunE: runBarrierRecoveryGenerateCommand,
	}
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Minimum keys required for recovery (M)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"Total number of recovery keys (N)")
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
	if barrierTotalShares < barrierThreshold {
		return errors.Newf(errors.ErrInvalidInput, "barrier.recovery.generate",
			"--total must be >= --threshold")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

--share is repeatable: pass each recovery key as its own --share flag.

Example:
  quicbaas barrier recovery recover --share key-a --share key-b --share key-c`,
		RunE: runBarrierRecoveryRecoverCommand,
	}
	cmd.Flags().StringSliceVar(&barrierRecoveryShares, "share", nil,
		"Recovery key (repeatable: --share a --share b ...)")
	return cmd
}

// runBarrierRecoveryRecoverCommand executes 'barrier recovery recover'.
func runBarrierRecoveryRecoverCommand(_ *cobra.Command, _ []string) error {
	if len(barrierRecoveryShares) == 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.recovery.recover",
			"--share is required (at least one)")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return formatError(client.BarrierRecoverWithKeys(ctx, &qrdbtransport.BarrierRecoverWithKeysRequest{
		Keys: barrierRecoveryShares,
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

// formatBarrierShamirResult renders a Shamir init response.
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
			"shares":       resp.Shares,
			"threshold":    resp.Threshold,
			"total_shares": resp.TotalShares,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nSHAMIR SECRET SHARING INITIALIZED\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "Threshold:\t%d of %d shares required\n", resp.Threshold, resp.TotalShares)
		fmt.Fprintf(tw, "\n!!! STORE THESE SHARES SECURELY - THEY WILL NOT BE DISPLAYED AGAIN !!!\n\n")
		for i, share := range resp.Shares {
			fmt.Fprintf(tw, "  Share %d:\t%s\n", i+1, share)
		}
		return tw.Flush()
	default:
		fmt.Fprintf(w, "Threshold: %d of %d\n", resp.Threshold, resp.TotalShares)
		for i, share := range resp.Shares {
			fmt.Fprintf(w, "Share %d: %s\n", i+1, share)
		}
		return nil
	}
}

// formatBarrierRekeyResult renders a Rekey response.
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
			"shares":       resp.Shares,
			"threshold":    resp.Threshold,
			"total_shares": resp.TotalShares,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nBARRIER REKEY COMPLETE\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "New Threshold:\t%d of %d\n", resp.Threshold, resp.TotalShares)
		fmt.Fprintf(tw, "\n!!! STORE THESE NEW SHARES SECURELY !!!\n\n")
		for i, share := range resp.Shares {
			fmt.Fprintf(tw, "  Share %d:\t%s\n", i+1, share)
		}
		return tw.Flush()
	default:
		fmt.Fprintf(w, "New Threshold: %d of %d\n", resp.Threshold, resp.TotalShares)
		for i, share := range resp.Shares {
			fmt.Fprintf(w, "Share %d: %s\n", i+1, share)
		}
		return nil
	}
}

// formatBarrierRecoveryKeys renders a recovery-key generation response.
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
			"keys":      resp.Keys,
			"threshold": resp.Threshold,
			"total":     resp.Total,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nRECOVERY KEYS GENERATED\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "Threshold:\t%d of %d keys required\n", resp.Threshold, resp.Total)
		fmt.Fprintf(tw, "\n!!! STORE THESE RECOVERY KEYS SECURELY - THEY WILL NOT BE DISPLAYED AGAIN !!!\n\n")
		for i, key := range resp.Keys {
			fmt.Fprintf(tw, "  Key %d:\t%s\n", i+1, key)
		}
		return tw.Flush()
	default:
		fmt.Fprintf(w, "Threshold: %d of %d\n", resp.Threshold, resp.Total)
		for i, key := range resp.Keys {
			fmt.Fprintf(w, "Key %d: %s\n", i+1, key)
		}
		return nil
	}
}
