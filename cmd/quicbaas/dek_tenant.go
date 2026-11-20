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

The server acts only on the caller's own tenant.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newDEKTenantStatusCommand())
	cmd.AddCommand(newDEKTenantRotateCommand())
	cmd.AddCommand(newDEKTenantRetireCommand())
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
	return formatTenantDEKStatus(resp, dekOutputFmt, cmd.OutOrStdout())
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
