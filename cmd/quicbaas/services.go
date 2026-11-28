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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	servicesSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
)

// =============================================================================
// `quicbaas services` — the service plane.
//
// Every verb routes through the active SDK transport (rest / grpc / quic /
// mcp / unix / embedded) and is scoped to the authenticated tenant.
// =============================================================================

// ServicesClientFactory creates service plane clients.
type ServicesClientFactory func() (servicesSDK.Client, error)

// servicesClientFactory creates service plane clients. Defaults to
// createSDKServicesClient.
var servicesClientFactory ServicesClientFactory = createSDKServicesClient

// createSDKServicesClient returns the active SDK client's service plane.
func createSDKServicesClient() (servicesSDK.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Services(), nil
}

var (
	servicesOutputFmt string
	servicesManifest  string
	// servicesStdin is where --manifest - reads from; tests replace it.
	servicesStdin io.Reader = os.Stdin
)

// InvalidSignedManifestError reports a --manifest file that is not a signed
// service manifest.
type InvalidSignedManifestError struct {
	// Path is the --manifest value.
	Path string
	// Reason says what is wrong with it.
	Reason string
}

// Error returns a human-readable description.
func (e *InvalidSignedManifestError) Error() string {
	return "services: --manifest " + e.Path + ": " + e.Reason
}

func newServicesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "services",
		Short: "Service plane: deploy and run signed services",
		Long: `Deploy a signed service manifest and drive its lifecycle. Deploy verifies
the signature, grants the service its workload identity, certificate, name,
RBAC role and secrets, and launches it. Every command is scoped to your tenant.

The --manifest file is a signed manifest as JSON:
  {"payload": "<base64 manifest>", "key_id": "<signing key>", "signature": "<base64>"}

Examples:
  quicbaas services deploy --manifest web.signed.json
  quicbaas services redeploy --manifest web.signed.json
  quicbaas services stop web
  quicbaas services start web
  quicbaas services restart web
  quicbaas services resume web
  quicbaas services get web
  quicbaas services list
  quicbaas services undeploy web`,
	}
	cmd.PersistentFlags().StringVar(&servicesOutputFmt, "output", "table", "Output format (json, yaml, table)")
	cmd.AddCommand(newServicesManifestCommand("deploy", "Deploy a signed service manifest",
		func(c servicesSDK.Client) func(context.Context, *servicesSDK.SignedManifest) (*servicesSDK.Record, error) {
			return c.Deploy
		}))
	cmd.AddCommand(newServicesManifestCommand("redeploy", "Move a service to a newer signed manifest",
		func(c servicesSDK.Client) func(context.Context, *servicesSDK.SignedManifest) (*servicesSDK.Record, error) {
			return c.Redeploy
		}))
	for _, v := range []struct {
		use, short string
		verb       func(servicesSDK.Client) func(context.Context, string) (*servicesSDK.Record, error)
	}{
		{"start", "Start a stopped service", func(c servicesSDK.Client) func(context.Context, string) (*servicesSDK.Record, error) { return c.Start }},
		{"stop", "Stop a running service", func(c servicesSDK.Client) func(context.Context, string) (*servicesSDK.Record, error) { return c.Stop }},
		{"resume", "Resume a service whose last verb failed", func(c servicesSDK.Client) func(context.Context, string) (*servicesSDK.Record, error) { return c.Resume }},
		{"restart", "Restart a running or halted service", func(c servicesSDK.Client) func(context.Context, string) (*servicesSDK.Record, error) {
			return c.Restart
		}},
		{"get", "Show one service", func(c servicesSDK.Client) func(context.Context, string) (*servicesSDK.Record, error) { return c.Get }},
	} {
		cmd.AddCommand(newServicesNamedCommand(v.use, v.short, v.verb))
	}
	cmd.AddCommand(newServicesUndeployCommand())
	cmd.AddCommand(newServicesListCommand())
	return cmd
}

func newServicesManifestCommand(use, short string, verb func(servicesSDK.Client) func(context.Context, *servicesSDK.SignedManifest) (*servicesSDK.Record, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			signed, err := readSignedManifest(servicesManifest)
			if err != nil {
				return formatError(err)
			}
			client, err := servicesClientFactory()
			if err != nil {
				return formatError(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			defer cancel()
			rec, err := verb(client)(ctx, signed)
			if err != nil {
				return formatError(err)
			}
			return writeServicesRecord(cmd.OutOrStdout(), rec)
		},
	}
	cmd.Flags().StringVar(&servicesManifest, "manifest", "", "Signed manifest JSON file, or - for stdin (required)")
	return cmd
}

func newServicesNamedCommand(use, short string, verb func(servicesSDK.Client) func(context.Context, string) (*servicesSDK.Record, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use + " NAME",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := servicesClientFactory()
			if err != nil {
				return formatError(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			defer cancel()
			rec, err := verb(client)(ctx, args[0])
			if err != nil {
				return formatError(err)
			}
			return writeServicesRecord(cmd.OutOrStdout(), rec)
		},
	}
}

func newServicesUndeployCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "undeploy NAME",
		Short: "Halt a service and revoke its grants",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := servicesClientFactory()
			if err != nil {
				return formatError(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			defer cancel()
			if err := client.Undeploy(ctx, args[0]); err != nil {
				return formatError(err)
			}
			done := map[string]interface{}{"name": args[0], "undeployed": true}
			return writeServicesOutput(cmd.OutOrStdout(), done, func(tw io.Writer) {
				fmt.Fprintf(tw, "Undeployed:\t%s\n", args[0])
			})
		},
	}
}

func newServicesListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your services",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := servicesClientFactory()
			if err != nil {
				return formatError(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			defer cancel()
			list, err := client.List(ctx)
			if err != nil {
				return formatError(err)
			}
			return writeServicesOutput(cmd.OutOrStdout(), list, func(tw io.Writer) {
				fmt.Fprintln(tw, "NAME\tSTATE\tREVISION\tRESTARTS\tVERB")
				for _, s := range list.Services {
					fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\n", s.Name, s.State, s.Revision, s.Restarts, s.Verb)
				}
			})
		},
	}
}

// readSignedManifest reads and checks a signed manifest file before
// anything reaches the wire.
func readSignedManifest(path string) (*servicesSDK.SignedManifest, error) {
	if path == "" {
		return nil, &InvalidSignedManifestError{Path: `""`, Reason: "is required"}
	}
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(servicesStdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, &InvalidSignedManifestError{Path: path, Reason: err.Error()}
	}
	var signed servicesSDK.SignedManifest
	if err := json.Unmarshal(raw, &signed); err != nil {
		return nil, &InvalidSignedManifestError{Path: path, Reason: "not signed manifest JSON: " + err.Error()}
	}
	if len(signed.Payload) == 0 || signed.KeyID == "" || len(signed.Signature) == 0 {
		return nil, &InvalidSignedManifestError{Path: path, Reason: "payload, key_id and signature are required"}
	}
	return &signed, nil
}

func writeServicesRecord(w io.Writer, rec *servicesSDK.Record) error {
	return writeServicesOutput(w, rec, func(tw io.Writer) {
		fmt.Fprintf(tw, "Name:\t%s\n", rec.Name)
		fmt.Fprintf(tw, "Tenant:\t%s\n", rec.Tenant)
		fmt.Fprintf(tw, "State:\t%s\n", rec.State)
		fmt.Fprintf(tw, "Verb:\t%s\n", rec.Verb)
		fmt.Fprintf(tw, "Revision:\t%d\n", rec.Revision)
		fmt.Fprintf(tw, "Restarts:\t%d\n", rec.Restarts)
		fmt.Fprintf(tw, "Signing key:\t%s\n", rec.KeyID)
		if rec.Reason != "" {
			fmt.Fprintf(tw, "Reason:\t%s\n", rec.Reason)
		}
		if len(rec.Grants) > 0 {
			kinds := make([]string, 0, len(rec.Grants))
			for k := range rec.Grants {
				kinds = append(kinds, k)
			}
			sort.Strings(kinds)
			grants := make([]string, 0, len(kinds))
			for _, k := range kinds {
				grants = append(grants, k+"="+rec.Grants[k])
			}
			fmt.Fprintf(tw, "Grants:\t%s\n", strings.Join(grants, ", "))
		}
	})
}

func writeServicesOutput(w io.Writer, v interface{}, table func(io.Writer)) error {
	format, err := cli.ParseOutputFormat(servicesOutputFmt)
	if err != nil {
		return err
	}
	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(v)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(v)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		table(tw)
		return tw.Flush()
	}
}
