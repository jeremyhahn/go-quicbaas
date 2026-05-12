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
// Verbs and flags mirror xkmsctl barrier verb-for-verb so operators get
// a single, consistent surface across QuicBaaS / XKMS / qrdb clients.
//
// All commands route through the SDK BarrierClient
// (sdk/go.Client.Barrier()), which speaks to the /api/v1/barrier REST
// surface (or the protocol-equivalent gRPC/QUIC/Unix/MCP/embedded
// endpoint). No transport-specific code lives in this file.
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
// Barrier command flags
// =============================================================================

var (
	barrierSecret      string
	barrierShare       string
	barrierSharesFile  string
	barrierThreshold   int
	barrierTotalShares int
	barrierOutputFmt   string
	barrierAssumeYes   bool
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
	BarrierUnsealWithShare(ctx context.Context, req *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error)
	BarrierUnsealWithShares(ctx context.Context, req *qrdbtransport.BarrierUnsealSharesRequest) error
	BarrierSeal(ctx context.Context) error
	BarrierRekey(ctx context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error)
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
	return sdk.Barrier()
}

// =============================================================================
// Errors
// =============================================================================

// BarrierUnsealNoInputError is returned when 'barrier unseal' is invoked
// with neither --secret nor --share.
type BarrierUnsealNoInputError struct{}

// Error returns a human-readable description.
func (e *BarrierUnsealNoInputError) Error() string {
	return "barrier unseal: one of --secret or --share is required"
}

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
password (single-secret) and Shamir (M-of-N split shares).

Verbs mirror xkmsctl barrier so operators have a consistent surface:
  status         Show current barrier status
  init           Initialize with a single passphrase
  init-shamir    Initialize with Shamir secret sharing
  unseal         Unseal with --secret or a single --share
  unseal-shares  Unseal with multiple shares from a file
  seal           Seal the barrier
  rekey          Re-split the root key with new Shamir shares`,
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
		Short: "Initialize the barrier with a single passphrase",
		Long: `Initialize the barrier using the password (single-secret) strategy.

This is the simplest sealing strategy: a single passphrase protects
the root key. Use 'init-shamir' for M-of-N split-secret sealing.

Examples:
  quicbaas barrier init --secret my-secret`,
		RunE: runBarrierInitCommand,
	}
	cmd.Flags().StringVar(&barrierSecret, "secret", "",
		"Passphrase used to seal the root key (required)")
	return cmd
}

// runBarrierInitCommand executes 'barrier init'.
func runBarrierInitCommand(_ *cobra.Command, _ []string) error {
	if barrierSecret == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init", "--secret is required")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{
		Secret: barrierSecret,
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
		Short: "Initialize the barrier with Shamir secret sharing",
		Long: `Initialize the barrier using M-of-N Shamir secret sharing.

The generated shares MUST be distributed to the security officers and
stored separately. They are the only way to unseal the barrier and are
not stored on the server.

Examples:
  quicbaas barrier init-shamir --secret my-secret --threshold 3 --total 5`,
		RunE: runBarrierInitShamirCommand,
	}
	cmd.Flags().StringVar(&barrierSecret, "secret", "",
		"Passphrase used as the entropy source (required)")
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Minimum shares required to unseal (M)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"Total number of shares to generate (N)")
	return cmd
}

// runBarrierInitShamirCommand executes 'barrier init-shamir'.
func runBarrierInitShamirCommand(cmd *cobra.Command, _ []string) error {
	if barrierSecret == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.init-shamir", "--secret is required")
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
	})
	if err != nil {
		return formatError(err)
	}

	return formatBarrierShamirResult(resp, barrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// unseal
// =============================================================================

// newBarrierUnsealCommand builds 'quicbaas barrier unseal'.
func newBarrierUnsealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unseal",
		Short: "Unseal the barrier with a passphrase or a single Shamir share",
		Long: `Unseal the barrier so cryptographic operations can proceed.

Provide --secret for password-based unsealing or --share for a single
Shamir share (incremental quorum unsealing — repeat until threshold).
Use 'unseal-shares' to submit a complete share batch in one call.

Examples:
  quicbaas barrier unseal --secret my-secret
  quicbaas barrier unseal --share base64-share-value`,
		RunE: runBarrierUnsealCommand,
	}
	cmd.Flags().StringVar(&barrierSecret, "secret", "",
		"Passphrase for password-based unsealing")
	cmd.Flags().StringVar(&barrierShare, "share", "",
		"Single Shamir share for incremental quorum unsealing")
	return cmd
}

// runBarrierUnsealCommand executes 'barrier unseal'.
func runBarrierUnsealCommand(cmd *cobra.Command, _ []string) error {
	if barrierSecret == "" && barrierShare == "" {
		return formatError(&BarrierUnsealNoInputError{})
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if barrierSecret != "" {
		if err := client.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{
			Secret: barrierSecret,
		}); err != nil {
			return formatError(err)
		}
		return nil
	}

	resp, err := client.BarrierUnsealWithShare(ctx, &qrdbtransport.BarrierUnsealShareRequest{
		Share: barrierShare,
	})
	if err != nil {
		return formatError(err)
	}

	return formatBarrierUnsealProgress(resp, barrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// unseal-shares
// =============================================================================

// newBarrierUnsealSharesCommand builds 'quicbaas barrier unseal-shares'.
func newBarrierUnsealSharesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unseal-shares",
		Short: "Unseal the barrier with multiple Shamir shares from a file",
		Long: `Unseal the barrier with a complete batch of Shamir shares.

The --shares-file argument points to a text file with one share per line.
Empty lines and lines beginning with '#' are ignored.

Example:
  quicbaas barrier unseal-shares --shares-file /etc/quicbaas/shares.txt`,
		RunE: runBarrierUnsealSharesCommand,
	}
	cmd.Flags().StringVar(&barrierSharesFile, "shares-file", "",
		"Path to a file containing one share per line (required)")
	return cmd
}

// runBarrierUnsealSharesCommand executes 'barrier unseal-shares'.
func runBarrierUnsealSharesCommand(_ *cobra.Command, _ []string) error {
	if barrierSharesFile == "" {
		return errors.Newf(errors.ErrInvalidInput,
			"barrier.unseal-shares", "--shares-file is required")
	}

	shares, err := readSharesFile(barrierSharesFile)
	if err != nil {
		return formatError(err)
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.BarrierUnsealWithShares(ctx, &qrdbtransport.BarrierUnsealSharesRequest{
		Shares: shares,
	}); err != nil {
		return formatError(err)
	}
	return nil
}

// readSharesFile reads one share per line from path. Blank lines and
// '#' comments are skipped.
func readSharesFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &BarrierSharesFileError{Path: path, Err: err}
	}
	defer f.Close()

	var shares []string
	scanner := bufio.NewScanner(f)
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
func newBarrierSealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "Seal the barrier",
		Long: `Seal the barrier, zeroing the data encryption key.

All cryptographic operations are blocked until the barrier is unsealed
again. By default the command prompts for confirmation; pass
--assume-yes to skip the prompt (e.g. in scripted runbooks).

Example:
  quicbaas barrier seal --assume-yes`,
		RunE: runBarrierSealCommand,
	}
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
		Short: "Re-split the root key with new Shamir shares",
		Long: `Generate a new set of Shamir shares for the root key.

The barrier must be unsealed. Old shares are invalidated; new shares
are generated with the specified threshold and total count.

Example:
  quicbaas barrier rekey --threshold 3 --total 5`,
		RunE: runBarrierRekeyCommand,
	}
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"New minimum shares required to unseal (M)")
	cmd.Flags().IntVar(&barrierTotalShares, "total", 0,
		"New total number of shares to generate (N)")
	return cmd
}

// runBarrierRekeyCommand executes 'barrier rekey'.
func runBarrierRekeyCommand(cmd *cobra.Command, _ []string) error {
	if barrierThreshold <= 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.rekey", "--threshold must be > 0")
	}
	if barrierTotalShares <= 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.rekey", "--total must be > 0")
	}
	if barrierThreshold > barrierTotalShares {
		return errors.Newf(errors.ErrInvalidInput, "barrier.rekey", "--threshold cannot exceed --total")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := client.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{
		Threshold: barrierThreshold,
		Total:     barrierTotalShares,
	})
	if err != nil {
		return formatError(err)
	}

	return formatBarrierRekeyResult(resp, barrierOutputFmt, cmd.OutOrStdout())
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

// formatBarrierShamirResult renders a Shamir init/rekey response.
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

// formatBarrierUnsealProgress renders an UnsealShareResponse.
func formatBarrierUnsealProgress(resp *qrdbtransport.BarrierUnsealShareResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"required":  resp.Required,
			"submitted": resp.Submitted,
			"complete":  resp.Complete,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if resp.Complete {
			fmt.Fprintf(tw, "Status:\tBarrier unsealed (%d/%d shares submitted)\n",
				resp.Submitted, resp.Required)
		} else {
			fmt.Fprintf(tw, "Status:\tShare accepted, %d/%d submitted\n",
				resp.Submitted, resp.Required)
			fmt.Fprintf(tw, "Remaining:\t%d more share(s) required\n",
				resp.Required-resp.Submitted)
		}
		return tw.Flush()
	default:
		if resp.Complete {
			fmt.Fprintf(w, "Barrier unsealed (%d/%d shares submitted)\n", resp.Submitted, resp.Required)
		} else {
			fmt.Fprintf(w, "Share accepted: %d/%d submitted, need %d more\n",
				resp.Submitted, resp.Required, resp.Required-resp.Submitted)
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
