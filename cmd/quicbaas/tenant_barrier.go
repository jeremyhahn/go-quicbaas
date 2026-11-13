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

// tenant_barrier.go exposes the PER-TENANT barrier: the tenant-admin
// self-service surface for a dedicated, tenant-owned encryption barrier.
//
// This is a different barrier from `quicbaas barrier`, and deliberately so.
// The system barrier protects the control plane and is the system admin's;
// a tenant barrier protects one tenant's data with that tenant's OWN seal
// strategy and credentials, so sealing it crypto-shreds that tenant's data
// and the infrastructure operator — holding neither the passphrase nor a
// Shamir quorum — cannot unseal it.
//
// quicbaas implements none of this. Per-tenant barriers are delegated
// entirely to the embedded go-qrdb node (BarrierRegistry / TenantBarrier),
// with the seal strategy selected from the xkms StrategyRegistry injected at
// composition time. These verbs are a thin pass-through to the SDK's tenant
// client, which is what keeps the CLI, the five network transports, and the
// embedded transport describing one implementation rather than several.
//
// Unlike the system barrier's ceremony, this surface is a documented
// sealed-window EXCLUSION: a tenant barrier is post-unseal, tenant-admin
// self-service, so it is served by the regular listener once the system is
// live (see pkg/api/sealedwindow).

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Flags
// =============================================================================

var (
	tenantBarrierTenantID  string
	tenantBarrierStrategy  string
	tenantBarrierSecret    string
	tenantBarrierThreshold int
	tenantBarrierShares    int
	tenantBarrierShare     string
	tenantBarrierOutputFmt string
)

// =============================================================================
// Client interface and factory
// =============================================================================

// TenantBarrierClient is the subset of the qrdb TenantClient surface the
// per-tenant barrier verbs need. The CLI never instantiates it directly;
// tests substitute their own factory.
type TenantBarrierClient interface {
	TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error)
	TenantBarrierUnseal(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error
	TenantBarrierUnsealShare(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealShareRequest) (*qrdbtransport.TenantBarrierUnsealProgress, error)
	TenantBarrierSeal(ctx context.Context, req *qrdbtransport.TenantBarrierSealRequest) error
	TenantBarrierStatus(ctx context.Context, req *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error)
	TenantBarrierOfficerList(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error)
	TenantBarrierOfficerClaimStatus(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error)
}

// TenantBarrierClientFactory creates TenantBarrierClient instances.
type TenantBarrierClientFactory func() (TenantBarrierClient, error)

// tenantBarrierClientFactory creates TenantBarrierClient instances. Defaults
// to the SDK-backed implementation; tests override this for injection.
var tenantBarrierClientFactory TenantBarrierClientFactory = createSDKTenantBarrierClient

// createSDKTenantBarrierClient returns the SDK tenant client. The SDK selects
// the active transport per the global --transport flag, so every verb below
// works identically across rest, grpc, quic, mcp, unix, and embedded.
func createSDKTenantBarrierClient() (TenantBarrierClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.TenantMgmtService().Tenant()
}

// =============================================================================
// Command tree
// =============================================================================

// newTenantBarrierCommand builds the 'quicbaas iam tenant barrier' group.
func newTenantBarrierCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "barrier",
		Short: "Manage a tenant's own encryption barrier",
		Long: `Manage a tenant's dedicated encryption barrier.

A tenant barrier is the tenant's own, not the operator's: it is protected
by that tenant's chosen seal strategy and credentials. Sealing it
crypto-shreds the tenant's data, and an infrastructure operator holding
neither the passphrase nor a Shamir quorum cannot unseal it.

This is separate from ` + "`quicbaas barrier`" + `, which manages the SYSTEM
barrier protecting the control plane.

A tenant on a SHARED barrier has no barrier of its own to manage; those
calls are rejected as entitlement violations, not authorization failures.

Verbs:
  status         Show the tenant barrier's state and strategy
  init           Initialize the tenant barrier with a seal strategy
  unseal         Unseal with a passphrase or key
  unseal-share   Submit one Shamir share toward the quorum
  seal           Seal the barrier (crypto-shreds until unsealed again)
  officer        List tenant Security Officers / claim status`,
	}

	cmd.PersistentFlags().StringVar(&tenantBarrierTenantID, "tenant", "",
		"Tenant whose barrier is being managed (required)")
	cmd.PersistentFlags().StringVar(&tenantBarrierOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newTenantBarrierStatusCommand())
	cmd.AddCommand(newTenantBarrierInitCommand())
	cmd.AddCommand(newTenantBarrierUnsealCommand())
	cmd.AddCommand(newTenantBarrierUnsealShareCommand())
	cmd.AddCommand(newTenantBarrierSealCommand())
	cmd.AddCommand(newTenantBarrierOfficerCommand())

	return cmd
}

// requireTenantID enforces the shared --tenant precondition. Every verb needs
// it, and defaulting to the system tenant would silently point a destructive
// operation at the wrong barrier.
func requireTenantID(op string) error {
	if tenantBarrierTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, op, "--tenant is required")
	}
	return nil
}

// =============================================================================
// status
// =============================================================================

// newTenantBarrierStatusCommand builds 'quicbaas iam tenant barrier status'.
func newTenantBarrierStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the tenant barrier's state and strategy",
		Long: `Show a tenant barrier's state.

Reports whether the barrier is initialized and sealed, which strategy
protects it, whether that strategy is hardware-backed, and the Shamir
M-of-N parameters when applicable.

Example:
  quicbaas iam tenant barrier status --tenant acme`,
		RunE: runTenantBarrierStatusCommand,
	}
}

// runTenantBarrierStatusCommand executes 'quicbaas iam tenant barrier status'.
func runTenantBarrierStatusCommand(cmd *cobra.Command, _ []string) error {
	if err := requireTenantID("tenant.barrier.status"); err != nil {
		return err
	}
	client, err := tenantBarrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.TenantBarrierStatus(ctx, &qrdbtransport.TenantBarrierStatusRequest{
		TenantID: tenantBarrierTenantID,
	})
	if err != nil {
		return formatError(err)
	}
	return formatTenantBarrierStatus(resp, tenantBarrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// init
// =============================================================================

// newTenantBarrierInitCommand builds 'quicbaas iam tenant barrier init'.
func newTenantBarrierInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the tenant barrier",
		Long: `Initialize a tenant's own barrier.

The strategy is selected from the strategy registry the deployment
injected, so a tenant may be protected by a passphrase, a Shamir quorum,
or hardware (TPM / PKCS#11 / cloud KMS) independently of how the SYSTEM
barrier is sealed.

` + passphraseInputHelp + `

Examples:
  printf '%s' "$PP" | quicbaas iam tenant barrier init --tenant acme \
      --strategy passphrase --passphrase-stdin
  quicbaas iam tenant barrier init --tenant acme --strategy shamir --threshold 2 --shares 3`,
		RunE: runTenantBarrierInitCommand,
	}
	cmd.Flags().StringVar(&tenantBarrierStrategy, "strategy", "",
		"Seal strategy: passphrase | shamir | pkcs11 | tpm2 | awskms | gcpkms | azurekv | vault")
	addPassphraseFlags(cmd, &tenantBarrierSecret,
		"Passphrase (required for --strategy=passphrase)")
	cmd.Flags().IntVar(&tenantBarrierThreshold, "threshold", 0,
		"Shamir M-of-N threshold (required for --strategy=shamir)")
	cmd.Flags().IntVar(&tenantBarrierShares, "shares", 0,
		"Total Shamir shares (required for --strategy=shamir)")
	return cmd
}

// runTenantBarrierInitCommand executes 'quicbaas iam tenant barrier init'.
func runTenantBarrierInitCommand(cmd *cobra.Command, _ []string) error {
	if err := requireTenantID("tenant.barrier.init"); err != nil {
		return err
	}
	if tenantBarrierStrategy == "" {
		return errors.Newf(errors.ErrInvalidInput, "tenant.barrier.init",
			"--strategy is required")
	}
	secret, err := resolvePassphrase(cmd, "tenant.barrier.init", tenantBarrierSecret)
	if err != nil {
		return formatError(err)
	}
	defer secret.Destroy()
	secret = passphraseForStrategy(cmd, tenantBarrierStrategy, secret)
	// Validate locally what the server would reject anyway. Initializing a
	// barrier is not idempotent from the tenant's point of view, so a clean
	// local failure is preferable to a half-understood server error.
	if isPassphraseStrategyName(tenantBarrierStrategy) && secret.Size() == 0 {
		return errors.Newf(errors.ErrInvalidInput, "tenant.barrier.init",
			"--passphrase is required when --strategy=passphrase"+passphraseSourcesHint)
	}
	if tenantBarrierStrategy == "shamir" {
		if tenantBarrierThreshold < 2 {
			return errors.Newf(errors.ErrInvalidInput, "tenant.barrier.init",
				"--threshold must be >= 2 when --strategy=shamir")
		}
		if tenantBarrierShares < tenantBarrierThreshold {
			return errors.Newf(errors.ErrInvalidInput, "tenant.barrier.init",
				"--shares must be >= --threshold")
		}
	}

	client, err := tenantBarrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{
		TenantID: tenantBarrierTenantID,
		Strategy: tenantBarrierStrategy,
		//secure:unborrowed injected-interface tenant-barrier transport request must carry bytes; the client owns the request lifetime.
		Passphrase: secret.Bytes(),
		Threshold:  tenantBarrierThreshold,
		Shares:     tenantBarrierShares,
	})
	if err != nil {
		return formatError(err)
	}
	return formatTenantBarrierInit(resp, tenantBarrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// unseal / unseal-share
// =============================================================================

// newTenantBarrierUnsealCommand builds 'quicbaas iam tenant barrier unseal'.
func newTenantBarrierUnsealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unseal",
		Short: "Unseal the tenant barrier with a passphrase",
		Long: `Unseal a tenant barrier.

For a Shamir-sealed tenant barrier use 'unseal-share' instead: a quorum
must each submit their own share.

` + passphraseInputHelp + `

Example:
  printf '%s' "$PP" | quicbaas iam tenant barrier unseal --tenant acme --passphrase-stdin`,
		RunE: runTenantBarrierUnsealCommand,
	}
	addPassphraseFlags(cmd, &tenantBarrierSecret,
		"Passphrase protecting the tenant barrier (required)")
	return cmd
}

// tenantBarrierActingContext returns the parent context of a tenant barrier
// unseal: it names the --tenant the operator acts for. On the embedded
// transport, which has no authenticator, that is the tenant the in-process
// caller acts for, and the server's tenant binding still refuses every other
// tenant. Over a network transport the binding never leaves the process: the
// server takes the caller's tenant from the credential.
func tenantBarrierActingContext() context.Context {
	return tenantctx.WithTenant(context.Background(), tenantBarrierTenantID)
}

// runTenantBarrierUnsealCommand executes 'quicbaas iam tenant barrier unseal'.
func runTenantBarrierUnsealCommand(cmd *cobra.Command, _ []string) error {
	if err := requireTenantID("tenant.barrier.unseal"); err != nil {
		return err
	}
	secret, err := resolvePassphrase(cmd, "tenant.barrier.unseal", tenantBarrierSecret)
	if err != nil {
		return formatError(err)
	}
	defer secret.Destroy()
	if secret.Size() == 0 {
		return errors.Newf(errors.ErrInvalidInput, "tenant.barrier.unseal",
			"--passphrase is required (use 'unseal-share' for a Shamir barrier)"+
				passphraseSourcesHint)
	}

	client, err := tenantBarrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(tenantBarrierActingContext(), globalTimeout)
	defer cancel()

	// Key is []byte, so the protected slice crosses directly: the passphrase
	// never becomes a Go string AND is never copied into unprotected heap
	// memory. This is the one transport field that can be handled with no
	// residue at all.
	//
	// It is safe because no transport retains the request past the call.
	// Traced through all six for go-qrdb/sdk/go@v0.10.0-alpha: rest, quic
	// and mcp marshal into a new buffer before the wire call; grpc and unix
	// hand a pb message to a UNARY RPC whose proto marshal completes inside
	// SendMsg (and neither transport sets a retry policy, so nothing is
	// buffered for replay); embedded forwards the slice in-process and it
	// terminates in an explicit make+copy in go-quicraft's software seal
	// strategy. No goroutine on any path outlives the call.
	//
	// The deferred Destroy above therefore runs strictly after the request
	// is done with the memory. Note this is an invariant of a module this
	// repo does not own — if a transport ever becomes asynchronous or gains
	// a replay buffer, this must go back to a wiped copy.
	if err := client.TenantBarrierUnseal(ctx, &qrdbtransport.TenantBarrierUnsealRequest{
		TenantID: tenantBarrierTenantID,
		//secure:unborrowed injected-interface tenant-barrier transport request must carry bytes; the client owns the request lifetime.
		Key: secret.Bytes(),
	}); err != nil {
		return formatError(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Tenant barrier %s unsealed.\n", tenantBarrierTenantID)
	return nil
}

// newTenantBarrierUnsealShareCommand builds 'quicbaas iam tenant barrier unseal-share'.
func newTenantBarrierUnsealShareCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unseal-share",
		Short: "Submit one Shamir share toward the tenant quorum",
		Long: `Submit a single Shamir share.

Each officer runs this with their own share. The response reports quorum
progress; the barrier unseals when the threshold is met. Shares are
accumulated in memory and discarded once the quorum completes, so a later
unseal requires fresh submissions.

Example:
  printf '%s' "$SHARE" | quicbaas iam tenant barrier unseal-share --tenant acme --share-stdin`,
		RunE: runTenantBarrierUnsealShareCommand,
	}
	cmd.Flags().StringVar(&tenantBarrierShare, "share", "",
		"One Shamir share (visible in the process list; prefer --share-stdin)")
	cmd.Flags().Bool(shareStdinFlag, false,
		"Read the share from standard input (preferred: keeps it out of the process list and the shell history)")
	return cmd
}

// runTenantBarrierUnsealShareCommand executes 'quicbaas iam tenant barrier unseal-share'.
func runTenantBarrierUnsealShareCommand(cmd *cobra.Command, _ []string) error {
	if err := requireTenantID("tenant.barrier.unseal-share"); err != nil {
		return err
	}
	share, err := resolveTenantBarrierShare(cmd, "tenant.barrier.unseal-share")
	if err != nil {
		return formatError(err)
	}
	defer share.Destroy()
	if share.Size() == 0 {
		return errors.Newf(errors.ErrInvalidInput, "tenant.barrier.unseal-share",
			"a share is required (supply it on stdin with --share-stdin, or on argv with --share)")
	}

	client, err := tenantBarrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(tenantBarrierActingContext(), globalTimeout)
	defer cancel()

	progress, err := client.TenantBarrierUnsealShare(ctx, &qrdbtransport.TenantBarrierUnsealShareRequest{
		TenantID: tenantBarrierTenantID,
		//secure:unborrowed injected-interface the go-qrdb transport request carries the share as a string; the copy is unwipeable (#237).
		Share: string(share.Bytes()),
	})
	if err != nil {
		return formatError(err)
	}
	return formatTenantBarrierProgress(progress, tenantBarrierOutputFmt, cmd.OutOrStdout())
}

// shareStdinFlag selects the standard-input path for the one share
// 'tenant barrier unseal-share' submits.
const shareStdinFlag = "share-stdin"

// resolveTenantBarrierShare returns the share from --share-stdin or, failing
// that, --share. Both at once is refused. The returned Buffer belongs to the
// caller, which must Destroy it; a nil Buffer means no share was supplied.
func resolveTenantBarrierShare(cmd *cobra.Command, op string) (*secure.Buffer, error) {
	fromStdin, err := cmd.Flags().GetBool(shareStdinFlag)
	if err != nil {
		fromStdin = false // a bare command in a unit test has no stdin path
	}
	if fromStdin {
		if tenantBarrierShare != "" {
			return nil, errors.Newf(errors.ErrInvalidInput, op,
				"use one of --share-stdin or --share, not both")
		}
		return readPassphrase(cmd.InOrStdin(), op, passphraseReadTimeout())
	}
	if tenantBarrierShare != "" {
		return protectExposedPassphrase(tenantBarrierShare, op)
	}
	return nil, nil
}

// =============================================================================
// seal
// =============================================================================

// newTenantBarrierSealCommand builds 'quicbaas iam tenant barrier seal'.
func newTenantBarrierSealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "Seal the tenant barrier",
		Long: `Seal a tenant barrier.

Sealing renders that tenant's data unreadable until the barrier is
unsealed again with the tenant's own credentials. Nobody else — including
the infrastructure operator — can reverse it.

Example:
  quicbaas iam tenant barrier seal --tenant acme --yes`,
		RunE: runTenantBarrierSealCommand,
	}
	cmd.Flags().BoolVarP(&barrierAssumeYes, "yes", "y", false,
		"Skip the confirmation prompt")
	return cmd
}

// runTenantBarrierSealCommand executes 'quicbaas iam tenant barrier seal'.
func runTenantBarrierSealCommand(cmd *cobra.Command, _ []string) error {
	if err := requireTenantID("tenant.barrier.seal"); err != nil {
		return err
	}
	// Sealing is effectively destructive from the tenant's perspective, so it
	// is confirmed by default rather than on request.
	if !barrierAssumeYes {
		return errors.Newf(errors.ErrInvalidInput, "tenant.barrier.seal",
			"sealing tenant %q makes its data unreadable until unsealed with the tenant's own credentials; re-run with --yes to confirm",
			tenantBarrierTenantID)
	}

	client, err := tenantBarrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.TenantBarrierSeal(ctx, &qrdbtransport.TenantBarrierSealRequest{
		TenantID: tenantBarrierTenantID,
	}); err != nil {
		return formatError(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Tenant barrier %s sealed.\n", tenantBarrierTenantID)
	return nil
}

// =============================================================================
// officer
// =============================================================================

// newTenantBarrierOfficerCommand builds 'quicbaas iam tenant barrier officer'.
func newTenantBarrierOfficerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "officer",
		Short: "Inspect the tenant's Security Officers",
		Long: `Inspect a tenant's Security Officers.

Reports public metadata only — username, share index, auth factor,
certificate thumbprint. Share bytes are never returned by any verb here.`,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the tenant's enrolled Security Officers",
		RunE:  runTenantBarrierOfficerListCommand,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show per-officer claim status for the tenant",
		RunE:  runTenantBarrierOfficerStatusCommand,
	})
	return cmd
}

// runTenantBarrierOfficerListCommand executes 'quicbaas iam tenant barrier officer list'.
func runTenantBarrierOfficerListCommand(cmd *cobra.Command, _ []string) error {
	if err := requireTenantID("tenant.barrier.officer.list"); err != nil {
		return err
	}
	client, err := tenantBarrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	officers, err := client.TenantBarrierOfficerList(ctx, tenantBarrierTenantID)
	if err != nil {
		return formatError(err)
	}
	return formatOfficerStatusList(officers, tenantBarrierOutputFmt, cmd.OutOrStdout())
}

// runTenantBarrierOfficerStatusCommand executes 'quicbaas iam tenant barrier officer status'.
func runTenantBarrierOfficerStatusCommand(cmd *cobra.Command, _ []string) error {
	if err := requireTenantID("tenant.barrier.officer.status"); err != nil {
		return err
	}
	client, err := tenantBarrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	officers, err := client.TenantBarrierOfficerClaimStatus(ctx, tenantBarrierTenantID)
	if err != nil {
		return formatError(err)
	}
	return formatOfficerStatusList(officers, tenantBarrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Output formatting
// =============================================================================

// formatTenantBarrierStatus renders a tenant barrier's state.
func formatTenantBarrierStatus(
	resp *qrdbtransport.TenantBarrierStatusResponse,
	format string,
	w io.Writer,
) error {
	if resp == nil {
		return errors.Newf(errors.ErrInternal, "tenant.barrier.status", "empty response")
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
	fmt.Fprintf(tw, "TENANT BARRIER\n")
	fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
	fmt.Fprintf(tw, "Tenant:\t%s\n", resp.TenantID)
	fmt.Fprintf(tw, "Initialized:\t%t\n", resp.Initialized)
	fmt.Fprintf(tw, "Sealed:\t%t\n", resp.Sealed)
	fmt.Fprintf(tw, "Strategy:\t%s\n", resp.Strategy)
	fmt.Fprintf(tw, "Hardware-backed:\t%t\n", resp.HardwareBacked)
	if resp.ShamirThreshold > 0 {
		fmt.Fprintf(tw, "Shamir:\t%d-of-%d\n", resp.ShamirThreshold, resp.ShamirTotal)
	}
	return tw.Flush()
}

// formatTenantBarrierInit renders the initialization result.
func formatTenantBarrierInit(
	resp *qrdbtransport.TenantBarrierInitResponse,
	format string,
	w io.Writer,
) error {
	if resp == nil {
		return errors.Newf(errors.ErrInternal, "tenant.barrier.init", "empty response")
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
	fmt.Fprintf(tw, "Tenant:\t%s\n", resp.TenantID)
	fmt.Fprintf(tw, "Strategy:\t%s\n", resp.Strategy)
	if resp.Threshold > 0 {
		fmt.Fprintf(tw, "Shamir:\t%d-of-%d\n", resp.Threshold, resp.TotalShares)
	}
	return tw.Flush()
}

// formatTenantBarrierProgress renders Shamir quorum progress.
func formatTenantBarrierProgress(
	progress *qrdbtransport.TenantBarrierUnsealProgress,
	format string,
	w io.Writer,
) error {
	if progress == nil {
		return errors.Newf(errors.ErrInternal, "tenant.barrier.unseal-share", "empty response")
	}
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if outputFormat == cli.OutputFormatJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(progress)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Tenant:\t%s\n", progress.TenantID)
	fmt.Fprintf(tw, "Shares:\t%d of %d submitted\n", progress.Submitted, progress.Required)
	fmt.Fprintf(tw, "Complete:\t%t\n", progress.Complete)
	return tw.Flush()
}
