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

// Package main provides the deprecated 'quicbaas seal' alias group.
//
// Per docs/08-bootstrap-auth-architecture.md §6 the seal command tree
// has been renamed to 'quicbaas barrier'. This file keeps 'quicbaas
// seal …' working for one release as an alias that:
//
//  1. prints a deprecation warning to stderr, and
//  2. forwards to the equivalent 'quicbaas barrier …' command.
//
// All real implementation lives in cmd/quicbaas/barrier.go. The
// remaining helpers in this file (formatSealStatusResponse,
// executeSealStatus, etc., and the seal* package-level flag variables)
// are kept ONLY because pre-existing tests (seal_recovery_keys_test.go,
// secrets_test.go) still reference them. New code MUST NOT call into
// these helpers.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// =============================================================================
// Deprecation flag variables
//
// These keep prior tests compiling (seal_recovery_keys_test.go etc.) but are
// not bound to any command flag. New code must NOT reference these.
// =============================================================================

var (
	sealShares            int
	sealThreshold         int
	sealRecoveryShares    int
	sealRecoveryThreshold int
	sealShare             string
	sealOutputFmt         string
)

// =============================================================================
// Alias command tree
// =============================================================================

const sealDeprecationPrefix = "WARNING: 'quicbaas seal "
const sealDeprecationSuffix = "' is deprecated; use 'quicbaas barrier "

// emitSealDeprecation writes the canonical deprecation warning to
// cmd.ErrOrStderr() and returns. Verbs supplied here must match the
// barrier verb the alias forwards to.
func emitSealDeprecation(cmd *cobra.Command, verb string) {
	fmt.Fprintf(cmd.ErrOrStderr(),
		"%s%s%s%s'\n", sealDeprecationPrefix, verb, sealDeprecationSuffix, verb)
}

// newSealCommand returns the deprecated 'seal' command group. Each
// subcommand prints a deprecation warning to stderr and forwards to
// the equivalent 'quicbaas barrier' subcommand.
func newSealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "DEPRECATED: use 'quicbaas barrier' instead",
		Long: `DEPRECATED: the 'seal' command tree has been renamed to 'barrier'.

Each 'quicbaas seal …' invocation prints a deprecation warning and
forwards to the equivalent 'quicbaas barrier …' command. The alias
will be removed in a future release.`,
	}

	cmd.AddCommand(newSealStatusCommand())
	cmd.AddCommand(newSealInitCommand())
	cmd.AddCommand(newSealUnsealCommand())
	cmd.AddCommand(newSealSealCommand())
	cmd.AddCommand(newSealRekeyCommand())

	return cmd
}

// newSealStatusCommand is the deprecated alias for 'quicbaas barrier status'.
func newSealStatusCommand() *cobra.Command {
	cmd := newBarrierStatusCommand()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		emitSealDeprecation(cmd, "status")
		return runBarrierStatusCommand(cmd, args)
	}
	cmd.Short = "DEPRECATED alias for 'quicbaas barrier status'"
	return cmd
}

// newSealInitCommand is the deprecated alias for 'quicbaas barrier init'.
func newSealInitCommand() *cobra.Command {
	cmd := newBarrierInitCommand()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		emitSealDeprecation(cmd, "init")
		return runBarrierInitCommand(cmd, args)
	}
	cmd.Short = "DEPRECATED alias for 'quicbaas barrier init'"
	return cmd
}

// newSealUnsealCommand is the deprecated alias for 'quicbaas barrier unseal'.
func newSealUnsealCommand() *cobra.Command {
	cmd := newBarrierUnsealCommand()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		emitSealDeprecation(cmd, "unseal")
		return runBarrierUnsealCommand(cmd, args)
	}
	cmd.Short = "DEPRECATED alias for 'quicbaas barrier unseal'"
	return cmd
}

// newSealSealCommand is the deprecated alias for 'quicbaas barrier seal'.
func newSealSealCommand() *cobra.Command {
	cmd := newBarrierSealCommand()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		emitSealDeprecation(cmd, "seal")
		return runBarrierSealCommand(cmd, args)
	}
	cmd.Short = "DEPRECATED alias for 'quicbaas barrier seal'"
	return cmd
}

// newSealRekeyCommand is the deprecated alias for 'quicbaas barrier rekey'.
func newSealRekeyCommand() *cobra.Command {
	cmd := newBarrierRekeyCommand()
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		emitSealDeprecation(cmd, "rekey")
		return runBarrierRekeyCommand(cmd, args)
	}
	cmd.Short = "DEPRECATED alias for 'quicbaas barrier rekey'"
	return cmd
}

// =============================================================================
// Legacy executeSeal*/formatSeal* helpers
//
// These are retained ONLY because pre-existing tests reference them. The
// canonical execution path now lives entirely on the barrier command tree
// (cmd/quicbaas/barrier.go). New code MUST NOT call these.
// =============================================================================

// executeSealStatus is a legacy helper kept for backwards-compatible tests.
//
// Deprecated: use barrierClientFactory().BarrierStatus().
func executeSealStatus(ctx context.Context, client SealClient) (*service.SealStatusResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "seal.status", "invalid client type")
	}
	return client.SealStatus(ctx)
}

// executeSealInit is a legacy helper kept for backwards-compatible tests.
//
// Deprecated: use barrierClientFactory().BarrierInitializeShamir().
func executeSealInit(ctx context.Context, client SealClient, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "seal.init", "invalid client type")
	}
	return client.SealInit(ctx, req)
}

// executeSealUnseal is a legacy helper kept for backwards-compatible tests.
//
// Deprecated: use barrierClientFactory().BarrierUnsealWithShare().
func executeSealUnseal(ctx context.Context, client SealClient, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "seal.unseal", "invalid client type")
	}
	return client.Unseal(ctx, req)
}

// executeSealSeal is a legacy helper kept for backwards-compatible tests.
//
// Deprecated: use barrierClientFactory().BarrierSeal().
func executeSealSeal(ctx context.Context, client SealClient) error {
	if client == nil {
		return errors.Newf(errors.ErrInvalidInput, "seal.seal", "invalid client type")
	}
	return client.Seal(ctx)
}

// executeSealRekey is a legacy helper kept for backwards-compatible tests.
//
// Deprecated: use barrierClientFactory().BarrierRekey().
func executeSealRekey(ctx context.Context, client SealClient, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "seal.rekey", "invalid client type")
	}
	return client.Rekey(ctx, req)
}

// formatSealStatusResponse renders a legacy SealStatusResponse.
//
// Deprecated: use formatBarrierStatusResponse with the barrier types.
func formatSealStatusResponse(resp *service.SealStatusResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "SEAL STATUS\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "Sealed:\t%v\n", resp.Sealed)
		fmt.Fprintf(tw, "Initialized:\t%v\n", resp.Initialized)
		fmt.Fprintf(tw, "Mode:\t%s\n", resp.Mode)
		if resp.Initialized {
			fmt.Fprintf(tw, "Threshold:\t%d\n", resp.Threshold)
			fmt.Fprintf(tw, "Total Shares:\t%d\n", resp.TotalShares)
			if resp.Sealed {
				fmt.Fprintf(tw, "Shares Provided:\t%d/%d\n", resp.SharesProvided, resp.Threshold)
			}
			fmt.Fprintf(tw, "Version:\t%d\n", resp.Version)
		}
		fmt.Fprintf(tw, "TPM Available:\t%v\n", resp.TPMAvailable)
		fmt.Fprintf(tw, "Cluster Nodes:\t%d\n", resp.ClusterNodes)
		return tw.Flush()

	default:
		if resp.Sealed {
			fmt.Fprintf(w, "Sealed: true (%d/%d shares provided)\n",
				resp.SharesProvided, resp.Threshold)
		} else {
			fmt.Fprintf(w, "Sealed: false (unsealed)\n")
		}
		fmt.Fprintf(w, "Initialized: %v\n", resp.Initialized)
		return nil
	}
}

// formatSealInitResponse renders a legacy SealInitResponse.
//
// Deprecated: use formatBarrierShamirResult with the barrier types.
func formatSealInitResponse(resp *service.SealInitResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		sharesB64 := make([]string, len(resp.Shares))
		for i, share := range resp.Shares {
			sharesB64[i] = base64.StdEncoding.EncodeToString(share)
		}
		recoveryB64 := make([]string, len(resp.RecoveryKeys))
		for i, key := range resp.RecoveryKeys {
			recoveryB64[i] = base64.StdEncoding.EncodeToString(key)
		}
		output := map[string]interface{}{
			"shares":        sharesB64,
			"recovery_keys": recoveryB64,
			"root_token":    resp.RootToken,
			"version":       resp.Version,
			"timestamp":     resp.Timestamp,
		}
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nSEAL INITIALIZATION COMPLETE\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "\n!!! STORE THESE VALUES SECURELY - THEY CANNOT BE RECOVERED !!!\n\n")
		fmt.Fprintf(tw, "Unseal Shares (%d):\n", len(resp.Shares))
		for i, share := range resp.Shares {
			fmt.Fprintf(tw, "  Share %d:\t%s\n", i+1, base64.StdEncoding.EncodeToString(share))
		}
		if len(resp.RecoveryKeys) > 0 {
			fmt.Fprintf(tw, "\nRecovery Keys (%d):\n", len(resp.RecoveryKeys))
			for i, key := range resp.RecoveryKeys {
				fmt.Fprintf(tw, "  Key %d:\t%s\n", i+1, base64.StdEncoding.EncodeToString(key))
			}
		}
		fmt.Fprintf(tw, "\nRoot Token:\t%s\n", resp.RootToken)
		fmt.Fprintf(tw, "Version:\t%d\n", resp.Version)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "\n=== SEAL INITIALIZED ===\n\n")
		fmt.Fprintf(w, "!!! STORE THESE VALUES SECURELY !!!\n\n")
		fmt.Fprintf(w, "Unseal Shares:\n")
		for i, share := range resp.Shares {
			fmt.Fprintf(w, "%d: %s\n", i+1, base64.StdEncoding.EncodeToString(share))
		}
		if len(resp.RecoveryKeys) > 0 {
			fmt.Fprintf(w, "\nRecovery Keys:\n")
			for i, key := range resp.RecoveryKeys {
				fmt.Fprintf(w, "%d: %s\n", i+1, base64.StdEncoding.EncodeToString(key))
			}
		}
		fmt.Fprintf(w, "\nRoot Token: %s\n", resp.RootToken)
		return nil
	}
}

// formatSealUnsealResponse renders a legacy UnsealResponse.
//
// Deprecated: use formatBarrierUnsealProgress with the barrier types.
func formatSealUnsealResponse(resp *service.UnsealResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if resp.Sealed {
			fmt.Fprintf(tw, "Sealed:\tYes\n")
			fmt.Fprintf(tw, "Progress:\t%d/%d shares\n", resp.SharesProvided, resp.Threshold)
			fmt.Fprintf(tw, "Remaining:\t%d shares needed\n", resp.Threshold-resp.SharesProvided)
		} else {
			fmt.Fprintf(tw, "Sealed:\tNo\n")
			fmt.Fprintf(tw, "Status:\tUnseal complete!\n")
		}
		if resp.Error != "" {
			fmt.Fprintf(tw, "Error:\t%s\n", resp.Error)
		}
		return tw.Flush()

	default:
		if resp.Sealed {
			fmt.Fprintf(w, "Progress: %d/%d shares provided\n", resp.SharesProvided, resp.Threshold)
			fmt.Fprintf(w, "%d more share(s) needed to unseal\n",
				resp.Threshold-resp.SharesProvided)
		} else {
			fmt.Fprintf(w, "QuicBaaS unsealed successfully!\n")
		}
		return nil
	}
}

// formatSealRekeyResponse renders a legacy RekeyResponse.
//
// Deprecated: use formatBarrierRekeyResult with the barrier types.
func formatSealRekeyResponse(resp *service.RekeyResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		sharesB64 := make([]string, len(resp.Shares))
		for i, share := range resp.Shares {
			sharesB64[i] = base64.StdEncoding.EncodeToString(share)
		}
		output := map[string]interface{}{
			"shares":    sharesB64,
			"version":   resp.Version,
			"timestamp": resp.Timestamp,
		}
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\nREKEY COMPLETE\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 80))
		fmt.Fprintf(tw, "\n!!! STORE THESE NEW SHARES SECURELY !!!\n\n")
		fmt.Fprintf(tw, "New Unseal Shares (%d):\n", len(resp.Shares))
		for i, share := range resp.Shares {
			fmt.Fprintf(tw, "  Share %d:\t%s\n", i+1, base64.StdEncoding.EncodeToString(share))
		}
		fmt.Fprintf(tw, "\nVersion:\t%d\n", resp.Version)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "\n=== REKEY COMPLETE ===\n\n")
		fmt.Fprintf(w, "New Unseal Shares:\n")
		for i, share := range resp.Shares {
			fmt.Fprintf(w, "%d: %s\n", i+1, base64.StdEncoding.EncodeToString(share))
		}
		return nil
	}
}

