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

// dek_tenant.go implements `quicbaas dek tenant` — the caller's tenant DEK
// keyring held by go-qrdb (status, rotate, retire), reached through
// Security().TenantDEK(). The server accepts only the caller's own tenant,
// so every request carries an empty TenantID and there is no --tenant flag.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// SDKTenantDEKClientFactory returns the caller's tenant DEK keyring client.
type SDKTenantDEKClientFactory func() (quicbaas.QRDBDEKClient, error)

// sdkTenantDEKClientFactory is swapped by tests.
var sdkTenantDEKClientFactory SDKTenantDEKClientFactory = createSDKTenantDEKClient

// dekTenantRetireVersion is the --version flag of `dek tenant retire`.
var dekTenantRetireVersion uint64

// Flags of the encrypt, decrypt and rewrap verbs.
var (
	dekTenantPlaintext  string
	dekTenantCiphertext string
	dekTenantAAD        string
)

func createSDKTenantDEKClient() (quicbaas.QRDBDEKClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().TenantDEK()
}

func newDEKTenantCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Per-tenant DEK keyring of the caller's tenant",
		Long: `Per-tenant DEK keyring of the caller's tenant, held by the database.

Verbs:
  status   Show the keyring's algorithm, current version and versions
  rotate   Create a new current keyring version
  retire   Retire one keyring version (--version N)
  encrypt  Seal data under the current version (--plaintext, --aad)
  decrypt  Open a ciphertext (--ciphertext base64, --aad)
  rewrap   Re-seal a ciphertext under the current version (--ciphertext, --aad)

The server acts only on the caller's own tenant.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newDEKTenantStatusCommand())
	cmd.AddCommand(newDEKTenantRotateCommand())
	cmd.AddCommand(newDEKTenantRetireCommand())
	cmd.AddCommand(newDEKTenantEncryptCommand())
	cmd.AddCommand(newDEKTenantDecryptCommand())
	cmd.AddCommand(newDEKTenantRewrapCommand())
	return cmd
}

func newDEKTenantStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the caller's tenant DEK keyring",
		Example: `  quicbaas dek tenant status
  quicbaas dek tenant status --output json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDEKTenant(cmd, "status", func(ctx context.Context, c quicbaas.QRDBDEKClient) (*qrdbtransport.TenantDEKStatusResponse, error) {
				return c.TenantDEKStatus(ctx, &qrdbtransport.TenantDEKRequest{})
			})
		},
	}
}

func newDEKTenantRotateCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rotate",
		Short:   "Rotate the caller's tenant DEK keyring to a new version",
		Example: `  quicbaas dek tenant rotate`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDEKTenant(cmd, "rotate", func(ctx context.Context, c quicbaas.QRDBDEKClient) (*qrdbtransport.TenantDEKStatusResponse, error) {
				return c.TenantDEKRotate(ctx, &qrdbtransport.TenantDEKRequest{})
			})
		},
	}
}

func newDEKTenantRetireCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "retire",
		Short:   "Retire one version of the caller's tenant DEK keyring",
		Example: `  quicbaas dek tenant retire --version 2`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			version := dekTenantRetireVersion
			if version == 0 {
				return formatError(&DEKFlagInvalidError{Verb: "tenant retire", Flag: "version", Reason: "must be a positive keyring version"})
			}
			return runDEKTenant(cmd, "retire", func(ctx context.Context, c quicbaas.QRDBDEKClient) (*qrdbtransport.TenantDEKStatusResponse, error) {
				return c.TenantDEKRetire(ctx, &qrdbtransport.TenantDEKRetireRequest{Version: version})
			})
		},
	}
	cmd.Flags().Uint64Var(&dekTenantRetireVersion, "version", 0, "Keyring version to retire (required, > 0)")
	return cmd
}

// runDEKTenant resolves the client, runs call with the global timeout and
// prints the keyring status in the --output format.
func runDEKTenant(cmd *cobra.Command, verb string,
	call func(context.Context, quicbaas.QRDBDEKClient) (*qrdbtransport.TenantDEKStatusResponse, error)) error {
	return runDEKTenantCall(cmd, verb, call, formatTenantDEKStatus)
}

// runDEKTenantCall resolves the keyring client, runs one verb under the
// global timeout and prints its result. Errors carry the verb.
func runDEKTenantCall[T any](cmd *cobra.Command, verb string,
	call func(context.Context, quicbaas.QRDBDEKClient) (T, error),
	format func(T, string, io.Writer) error) error {
	client, err := sdkTenantDEKClientFactory()
	if err != nil {
		return formatError(fmt.Errorf("dek tenant %s: %w", verb, err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := call(ctx, client)
	if err != nil {
		return formatError(fmt.Errorf("dek tenant %s: %w", verb, err))
	}
	return format(resp, dekOutputFmt, cmd.OutOrStdout())
}

// tenantDEKCiphertextFlag decodes the base64 --ciphertext flag of verb.
func tenantDEKCiphertextFlag(verb string) ([]byte, error) {
	if dekTenantCiphertext == "" {
		return nil, formatError(&DEKFlagInvalidError{Verb: "tenant " + verb, Flag: "ciphertext", Reason: "is required (base64, as printed by dek tenant encrypt)"})
	}
	ct, err := base64.StdEncoding.DecodeString(dekTenantCiphertext)
	if err != nil {
		return nil, formatError(&DEKFlagInvalidError{Verb: "tenant " + verb, Flag: "ciphertext", Reason: "must be base64: " + err.Error()})
	}
	return ct, nil
}

// tenantDEKAAD returns the --aad flag's bytes, nil when unset.
func tenantDEKAAD() []byte {
	if dekTenantAAD == "" {
		return nil
	}
	return []byte(dekTenantAAD)
}

func newDEKTenantEncryptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encrypt",
		Short: "Seal data under the caller's current tenant DEK version",
		Long: `Seal data under the current version of the caller's tenant DEK keyring.

The ciphertext is printed base64-encoded. --aad, when given, is bound into the
ciphertext and must be presented unchanged to decrypt and rewrap.`,
		Example: `  quicbaas dek tenant encrypt --plaintext "card 4111" --aad order-42
  quicbaas dek tenant encrypt --plaintext secret --output json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("plaintext") {
				return formatError(&DEKFlagInvalidError{Verb: "tenant encrypt", Flag: "plaintext", Reason: "is required"})
			}
			req := &qrdbtransport.TenantDEKEncryptRequest{Plaintext: []byte(dekTenantPlaintext), AAD: tenantDEKAAD()}
			return runDEKTenantCall(cmd, "encrypt", func(ctx context.Context, c quicbaas.QRDBDEKClient) (*qrdbtransport.TenantDEKEncryptResponse, error) {
				return c.TenantDEKEncrypt(ctx, req)
			}, formatTenantDEKEncrypt)
		},
	}
	cmd.Flags().StringVar(&dekTenantPlaintext, "plaintext", "", "Data to seal (required)")
	cmd.Flags().StringVar(&dekTenantAAD, "aad", "", "Associated data bound into the ciphertext")
	return cmd
}

func newDEKTenantDecryptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "decrypt",
		Short:   "Open a ciphertext sealed under the caller's tenant DEK keyring",
		Example: `  quicbaas dek tenant decrypt --ciphertext <base64> --aad order-42`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ct, err := tenantDEKCiphertextFlag("decrypt")
			if err != nil {
				return err
			}
			req := &qrdbtransport.TenantDEKDecryptRequest{Ciphertext: ct, AAD: tenantDEKAAD()}
			return runDEKTenantCall(cmd, "decrypt", func(ctx context.Context, c quicbaas.QRDBDEKClient) (*qrdbtransport.TenantDEKDecryptResponse, error) {
				return c.TenantDEKDecrypt(ctx, req)
			}, formatTenantDEKDecrypt)
		},
	}
	cmd.Flags().StringVar(&dekTenantCiphertext, "ciphertext", "", "Base64 ciphertext from dek tenant encrypt (required)")
	cmd.Flags().StringVar(&dekTenantAAD, "aad", "", "Associated data the ciphertext was sealed with")
	return cmd
}

func newDEKTenantRewrapCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rewrap",
		Short: "Re-seal a ciphertext under the caller's current tenant DEK version",
		Long: `Re-seal a ciphertext under the current version of the caller's tenant DEK
keyring, without returning its plaintext. Rewrap existing ciphertext after a
rotate and before retiring the version it was sealed under.`,
		Example: `  quicbaas dek tenant rewrap --ciphertext <base64> --aad order-42`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ct, err := tenantDEKCiphertextFlag("rewrap")
			if err != nil {
				return err
			}
			req := &qrdbtransport.TenantDEKRewrapRequest{Ciphertext: ct, AAD: tenantDEKAAD()}
			return runDEKTenantCall(cmd, "rewrap", func(ctx context.Context, c quicbaas.QRDBDEKClient) (*qrdbtransport.TenantDEKRewrapResponse, error) {
				return c.TenantDEKRewrap(ctx, req)
			}, formatTenantDEKRewrap)
		},
	}
	cmd.Flags().StringVar(&dekTenantCiphertext, "ciphertext", "", "Base64 ciphertext to move to the current version (required)")
	cmd.Flags().StringVar(&dekTenantAAD, "aad", "", "Associated data the ciphertext was sealed with")
	return cmd
}

// tenantDEKSealedView is the printed form of encrypt and rewrap results:
// ciphertext base64-encoded, so JSON, YAML and text agree.
type tenantDEKSealedView struct {
	Ciphertext  string `json:"ciphertext" yaml:"ciphertext"`
	Version     uint64 `json:"version,omitempty" yaml:"version,omitempty"`
	FromVersion uint64 `json:"from_version,omitempty" yaml:"from_version,omitempty"`
	ToVersion   uint64 `json:"to_version,omitempty" yaml:"to_version,omitempty"`
}

// tenantDEKOpenedView is the printed form of a decrypt result.
type tenantDEKOpenedView struct {
	Plaintext string `json:"plaintext" yaml:"plaintext"`
	Version   uint64 `json:"version" yaml:"version"`
}

// printTenantDEKView prints v as JSON or YAML, or prints text for the table
// and default formats.
func printTenantDEKView(v any, text string, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(v)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(v)
	default:
		_, err := fmt.Fprintln(w, text)
		return err
	}
}

func formatTenantDEKEncrypt(resp *qrdbtransport.TenantDEKEncryptResponse, format string, w io.Writer) error {
	if resp == nil {
		fmt.Fprintln(w, "(no result)")
		return nil
	}
	ct := base64.StdEncoding.EncodeToString(resp.Ciphertext)
	return printTenantDEKView(tenantDEKSealedView{Ciphertext: ct, Version: resp.Version}, ct, format, w)
}

func formatTenantDEKDecrypt(resp *qrdbtransport.TenantDEKDecryptResponse, format string, w io.Writer) error {
	if resp == nil {
		fmt.Fprintln(w, "(no result)")
		return nil
	}
	pt := string(resp.Plaintext)
	return printTenantDEKView(tenantDEKOpenedView{Plaintext: pt, Version: resp.Version}, pt, format, w)
}

func formatTenantDEKRewrap(resp *qrdbtransport.TenantDEKRewrapResponse, format string, w io.Writer) error {
	if resp == nil {
		fmt.Fprintln(w, "(no result)")
		return nil
	}
	ct := base64.StdEncoding.EncodeToString(resp.Ciphertext)
	return printTenantDEKView(tenantDEKSealedView{Ciphertext: ct, FromVersion: resp.FromVersion, ToVersion: resp.ToVersion}, ct, format, w)
}

func formatTenantDEKStatus(resp *qrdbtransport.TenantDEKStatusResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if resp == nil {
		fmt.Fprintln(w, "(no status)")
		return nil
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Tenant:\t%s\n", resp.TenantID)
		fmt.Fprintf(tw, "Algorithm:\t%s\n", resp.Algorithm)
		fmt.Fprintf(tw, "Current Version:\t%d\n", resp.CurrentVersion)
		if len(resp.Versions) > 0 {
			fmt.Fprintf(tw, "\nVERSION\tCREATED\tRETIRED\n")
			for _, v := range resp.Versions {
				retired := "-"
				if v.RetiredAt != nil {
					retired = v.RetiredAt.UTC().Format(time.RFC3339)
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\n", v.Version, v.CreatedAt.UTC().Format(time.RFC3339), retired)
			}
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s v%d (%s)\n", resp.TenantID, resp.CurrentVersion, resp.Algorithm)
		return nil
	}
}
