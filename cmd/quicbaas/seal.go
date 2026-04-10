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

// Package main provides Seal Management CLI commands for QuicBaaS.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

var (
	sealShares            int
	sealThreshold         int
	sealRecoveryShares    int
	sealRecoveryThreshold int
	sealShare             string
	sealOutputFmt         string
)

// newSealCommand creates the seal command group.
func newSealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "Seal management commands",
		Long: `Seal commands for managing QuicBaaS encryption seal.

The seal protects the master encryption key using Shamir's Secret Sharing.
QuicBaaS must be unsealed after startup before it can process requests.`,
	}

	cmd.PersistentFlags().StringVar(&sealOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newSealStatusCommand())
	cmd.AddCommand(newSealInitCommand())
	cmd.AddCommand(newSealUnsealCommand())
	cmd.AddCommand(newSealSealCommand())
	cmd.AddCommand(newSealRekeyCommand())

	return cmd
}

// newSealStatusCommand creates the seal status command.
func newSealStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check seal status",
		Long: `Display the current seal status.

Shows whether the system is sealed, initialized, and seal configuration.

Examples:
  # Check seal status
  quicbaas seal status

  # Check status as JSON
  quicbaas seal status --output json`,
		RunE: runSealStatusCommand,
	}

	return cmd
}

// runSealStatusCommand executes the seal status command.
func runSealStatusCommand(cmd *cobra.Command, args []string) error {
	client, err := sealClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := executeSealStatus(ctx, client)
	if err != nil {
		return formatError(err)
	}

	return formatSealStatusResponse(resp, sealOutputFmt, cmd.OutOrStdout())
}

// newSealInitCommand creates the seal init command.
func newSealInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the seal",
		Long: `Initialize the seal with Shamir's Secret Sharing.

This generates the unseal keys and root token. This can only be done once
unless the seal is completely destroyed and recreated.

Examples:
  # Initialize with 5 shares, 3 required to unseal
  quicbaas seal init --shares 5 --threshold 3

  # Initialize with recovery keys
  quicbaas seal init --shares 5 --threshold 3 --recovery-shares 3 --recovery-threshold 2`,
		RunE: runSealInitCommand,
	}

	cmd.Flags().IntVar(&sealShares, "shares", 5, "Number of unseal key shares")
	cmd.Flags().IntVar(&sealThreshold, "threshold", 3, "Number of shares required to unseal")
	cmd.Flags().IntVar(&sealRecoveryShares, "recovery-shares", 0, "Number of recovery key shares")
	cmd.Flags().IntVar(&sealRecoveryThreshold, "recovery-threshold", 0, "Number of recovery keys required")

	return cmd
}

// runSealInitCommand executes the seal init command.
func runSealInitCommand(cmd *cobra.Command, args []string) error {
	if sealShares < 1 {
		return errors.Newf(errors.ErrInvalidInput, "seal.init", "shares must be at least 1")
	}
	if sealThreshold < 1 || sealThreshold > sealShares {
		return errors.Newf(errors.ErrInvalidInput, "seal.init", "threshold must be between 1 and shares")
	}
	if sealRecoveryShares > 0 && sealRecoveryThreshold < 1 {
		return errors.Newf(errors.ErrInvalidInput, "seal.init", "recovery-threshold required when recovery-shares is set")
	}

	client, err := sealClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &service.SealInitRequest{
		Threshold:           sealThreshold,
		TotalShares:         sealShares,
		RecoveryKeysEnabled: sealRecoveryShares > 0,
		RecoveryThreshold:   sealRecoveryThreshold,
		RecoveryTotalKeys:   sealRecoveryShares,
	}

	resp, err := executeSealInit(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSealInitResponse(resp, sealOutputFmt, cmd.OutOrStdout())
}

// newSealUnsealCommand creates the seal unseal command.
func newSealUnsealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unseal",
		Short: "Provide an unseal key share",
		Long: `Provide an unseal key share to unseal QuicBaaS.

The share can be provided via command line flag or stdin for security.

Examples:
  # Provide share via stdin (recommended)
  quicbaas seal unseal

  # Provide share via flag (less secure)
  quicbaas seal unseal --share base64:encoded-share`,
		RunE: runSealUnsealCommand,
	}

	cmd.Flags().StringVar(&sealShare, "share", "", "Unseal key share (base64 encoded)")

	return cmd
}

// runSealUnsealCommand executes the seal unseal command.
func runSealUnsealCommand(cmd *cobra.Command, args []string) error {
	var shareBytes []byte
	var err error

	if sealShare == "" {
		fmt.Print("Enter unseal key share: ")
		shareInput, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "seal.unseal", "failed to read share")
		}
		sealShare = string(shareInput)
	}

	sealShare = strings.TrimSpace(sealShare)
	if strings.HasPrefix(sealShare, "base64:") {
		shareBytes, err = base64.StdEncoding.DecodeString(strings.TrimPrefix(sealShare, "base64:"))
	} else {
		shareBytes, err = base64.StdEncoding.DecodeString(sealShare)
	}

	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "seal.unseal", "invalid share format (expected base64)")
	}

	client, err := sealClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &service.UnsealRequest{
		Share: shareBytes,
	}

	resp, err := executeSealUnseal(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSealUnsealResponse(resp, sealOutputFmt, cmd.OutOrStdout())
}

// newSealSealCommand creates the seal seal command.
func newSealSealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "Seal QuicBaaS",
		Long: `Seal QuicBaaS to prevent access to encrypted data.

This requires unsealing again before QuicBaaS can process requests.

Examples:
  # Seal the system
  quicbaas seal seal`,
		RunE: runSealSealCommand,
	}

	return cmd
}

// runSealSealCommand executes the seal seal command.
func runSealSealCommand(cmd *cobra.Command, args []string) error {
	fmt.Print("Are you sure you want to seal QuicBaaS? (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "seal.seal", "failed to read confirmation")
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "yes" {
		fmt.Println("Seal cancelled")
		return nil
	}

	client, err := sealClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err = executeSealSeal(ctx, client)
	if err != nil {
		return formatError(err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "QuicBaaS sealed successfully")
	return nil
}

// newSealRekeyCommand creates the seal rekey command.
func newSealRekeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rekey",
		Short: "Rotate unseal keys",
		Long: `Generate new unseal keys while keeping the same master key.

This requires providing the current unseal shares first.

Examples:
  # Rekey with new configuration
  quicbaas seal rekey --shares 7 --threshold 4`,
		RunE: runSealRekeyCommand,
	}

	cmd.Flags().IntVar(&sealShares, "shares", 5, "Number of new unseal key shares")
	cmd.Flags().IntVar(&sealThreshold, "threshold", 3, "Number of new shares required to unseal")

	return cmd
}

// runSealRekeyCommand executes the seal rekey command.
func runSealRekeyCommand(cmd *cobra.Command, args []string) error {
	if sealShares < 1 {
		return errors.Newf(errors.ErrInvalidInput, "seal.rekey", "shares must be at least 1")
	}
	if sealThreshold < 1 || sealThreshold > sealShares {
		return errors.Newf(errors.ErrInvalidInput, "seal.rekey", "threshold must be between 1 and shares")
	}

	client, err := sealClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := &service.RekeyRequest{
		NewThreshold:   sealThreshold,
		NewTotalShares: sealShares,
	}

	resp, err := executeSealRekey(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSealRekeyResponse(resp, sealOutputFmt, cmd.OutOrStdout())
}

// Seal service execution functions

func executeSealStatus(ctx context.Context, client SealClient) (*service.SealStatusResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "seal.status", "invalid client type")
	}
	return client.SealStatus(ctx)
}

func executeSealInit(ctx context.Context, client SealClient, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "seal.init", "invalid client type")
	}
	return client.SealInit(ctx, req)
}

func executeSealUnseal(ctx context.Context, client SealClient, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "seal.unseal", "invalid client type")
	}
	return client.Unseal(ctx, req)
}

func executeSealSeal(ctx context.Context, client SealClient) error {
	if client == nil {
		return errors.Newf(errors.ErrInvalidInput, "seal.seal", "invalid client type")
	}
	return client.Seal(ctx)
}

func executeSealRekey(ctx context.Context, client SealClient, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "seal.rekey", "invalid client type")
	}
	return client.Rekey(ctx, req)
}

// Formatting functions

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
			fmt.Fprintf(w, "Sealed: true (%d/%d shares provided)\n", resp.SharesProvided, resp.Threshold)
		} else {
			fmt.Fprintf(w, "Sealed: false (unsealed)\n")
		}
		fmt.Fprintf(w, "Initialized: %v\n", resp.Initialized)
		return nil
	}
}

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
			fmt.Fprintf(w, "%d more share(s) needed to unseal\n", resp.Threshold-resp.SharesProvided)
		} else {
			fmt.Fprintf(w, "QuicBaaS unsealed successfully!\n")
		}
		return nil
	}
}

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
