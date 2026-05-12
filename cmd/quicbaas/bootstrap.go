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

// Package main provides Bootstrap CLI commands for QuicBaaS.
//
// Bootstrap commands drive trust-bootstrap of the CA bundle from a
// fresh QuicBaaS deployment. The verb set mirrors xkmsctl bootstrap
// verb-for-verb so operators get a single, consistent surface across
// QuicBaaS / XKMS / qrdb clients.
//
// QuicBaaS itself does not own bootstrap; the canonical truststrap
// implementation lives in go-truststrap and is exercised by xkmsctl.
// This file exposes the same CLI shape but routes through the SDK so a
// future SDK BootstrapClient can drop in without operator-visible
// changes.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// =============================================================================
// bootstrap command flags
// =============================================================================

var (
	bootstrapMethod    string
	bootstrapServerURL string
	bootstrapBundleOut string
	bootstrapOutputFmt string
)

// validBootstrapMethods is the canonical --method set.
var validBootstrapMethods = map[string]struct{}{
	"auto":   {},
	"dane":   {},
	"noise":  {},
	"spki":   {},
	"direct": {},
}

// =============================================================================
// BootstrapClient interface and factory
// =============================================================================

// BootstrapAutoRequest captures the parameters for /api/v1/bootstrap/auto.
// Values that are zero are sent as their JSON omitempty defaults so the
// server can fall back to its configuration.
type BootstrapAutoRequest struct {
	// Method selects the bootstrap method (auto, dane, noise, spki, direct).
	Method string `json:"method"`

	// ServerURL is the target QuicBaaS server. When empty the SDK uses the
	// global --address.
	ServerURL string `json:"server_url,omitempty"`
}

// BootstrapAutoResponse is the JSON shape returned by
// /api/v1/bootstrap/auto. Fields are lifted from the truststrap
// AutoFetch result so the CLI can render either a CA bundle PEM or a
// human-readable status.
type BootstrapAutoResponse struct {
	// Method is the bootstrap method that succeeded (echoes input for
	// non-auto runs; for auto it is one of dane/noise/spki/direct).
	Method string `json:"method"`

	// CABundlePEM is the discovered CA bundle, PEM-encoded.
	CABundlePEM string `json:"ca_bundle_pem,omitempty"`

	// SPKIPin is the SPKI pin observed during the bootstrap (audit only).
	SPKIPin string `json:"spki_pin,omitempty"`
}

// BootstrapClient is the SDK surface needed by 'quicbaas bootstrap'.
type BootstrapClient interface {
	BootstrapAuto(ctx context.Context, req *BootstrapAutoRequest) (*BootstrapAutoResponse, error)
}

// BootstrapClientFactory creates BootstrapClient instances.
type BootstrapClientFactory func() (BootstrapClient, error)

// bootstrapClientFactory creates BootstrapClient instances. Defaults to
// the SDK-backed implementation; tests override this for injection.
var bootstrapClientFactory BootstrapClientFactory = createSDKBootstrapClient

// createSDKBootstrapClient returns a BootstrapClient. Until the SDK
// exposes a first-class bootstrap surface, this returns a sentinel
// pendingBootstrapClient that emits a typed "pending SDK" error so the
// canonical CLI shape is locked even before the server-side handler
// lands.
func createSDKBootstrapClient() (BootstrapClient, error) {
	return &pendingBootstrapClient{}, nil
}

// =============================================================================
// Errors
// =============================================================================

// BootstrapInvalidMethodError is returned when --method is not one of
// auto / dane / noise / spki / direct.
type BootstrapInvalidMethodError struct {
	Got string
}

// Error returns a human-readable description.
func (e *BootstrapInvalidMethodError) Error() string {
	return "bootstrap: invalid --method " + e.Got +
		" (must be one of auto, dane, noise, spki, direct)"
}

// BootstrapPendingSDKError signals that the SDK BootstrapClient surface
// has not yet been wired. The CLI shape is locked; the SDK call lands
// in a follow-up phase.
type BootstrapPendingSDKError struct {
	Op string
}

// Error returns a human-readable description.
func (e *BootstrapPendingSDKError) Error() string {
	return "bootstrap " + e.Op +
		": SDK BootstrapClient not yet wired (track in plan Phase 12+)"
}

// pendingBootstrapClient is the placeholder returned by
// createSDKBootstrapClient until the SDK exposes a real
// BootstrapClient. Every call returns BootstrapPendingSDKError.
type pendingBootstrapClient struct{}

// BootstrapAuto returns BootstrapPendingSDKError.
func (p *pendingBootstrapClient) BootstrapAuto(_ context.Context, _ *BootstrapAutoRequest) (*BootstrapAutoResponse, error) {
	return nil, &BootstrapPendingSDKError{Op: "auto"}
}

// =============================================================================
// Command tree
// =============================================================================

// newBootstrapCommand creates the canonical "bootstrap" command group.
func newBootstrapCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Trust-bootstrap the CA bundle (auto, dane, noise, spki, direct)",
		Long: `Trust-bootstrap commands fetch the QuicBaaS CA bundle without
prior trust. The verb set mirrors xkmsctl bootstrap so operators have
a single, consistent surface.

Verbs:
  auto    Try methods in priority order (DANE, Noise, SPKI, Direct)`,
	}
	cmd.PersistentFlags().StringVar(&bootstrapOutputFmt, "output", "table",
		"Output format (json, yaml, table)")
	cmd.AddCommand(newBootstrapAutoCommand())
	return cmd
}

// =============================================================================
// auto
// =============================================================================

// newBootstrapAutoCommand builds 'quicbaas bootstrap auto'.
func newBootstrapAutoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auto",
		Short: "Auto-bootstrap using the configured method order",
		Long: `Trust-bootstrap by trying methods in priority order.

Use --method to pin a single method (auto runs DANE → Noise → SPKI →
Direct in that order; dane/noise/spki/direct each pin their respective
method).

Example:
  quicbaas bootstrap auto --method auto`,
		RunE: runBootstrapAutoCommand,
	}
	cmd.Flags().StringVar(&bootstrapMethod, "method", "auto",
		"Bootstrap method: auto, dane, noise, spki, direct")
	cmd.Flags().StringVar(&bootstrapServerURL, "server-url", "",
		"Override the global --address for this call (optional)")
	cmd.Flags().StringVar(&bootstrapBundleOut, "bundle-output", "",
		"Path where the CA bundle PEM should be written (optional, prints to stdout when empty)")
	return cmd
}

// runBootstrapAutoCommand executes 'bootstrap auto'.
func runBootstrapAutoCommand(cmd *cobra.Command, _ []string) error {
	if _, ok := validBootstrapMethods[bootstrapMethod]; !ok {
		return formatError(&BootstrapInvalidMethodError{Got: bootstrapMethod})
	}

	client, err := bootstrapClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := client.BootstrapAuto(ctx, &BootstrapAutoRequest{
		Method:    bootstrapMethod,
		ServerURL: bootstrapServerURL,
	})
	if err != nil {
		return formatError(err)
	}

	if bootstrapBundleOut != "" {
		if err := writeFileSecure(bootstrapBundleOut, []byte(resp.CABundlePEM)); err != nil {
			return formatError(err)
		}
	}

	return formatBootstrapAutoResponse(resp, bootstrapOutputFmt, cmd.OutOrStdout())
}

// formatBootstrapAutoResponse renders a BootstrapAutoResponse.
func formatBootstrapAutoResponse(resp *BootstrapAutoResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"method":        resp.Method,
			"ca_bundle_pem": resp.CABundlePEM,
			"spki_pin":      resp.SPKIPin,
		})
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "BOOTSTRAP RESULT\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "Method:\t%s\n", resp.Method)
		if resp.SPKIPin != "" {
			fmt.Fprintf(tw, "SPKI Pin:\t%s\n", resp.SPKIPin)
		}
		if resp.CABundlePEM != "" && bootstrapBundleOut == "" {
			fmt.Fprintf(tw, "\n--- CA Bundle PEM ---\n%s\n", resp.CABundlePEM)
		}
		return tw.Flush()
	default:
		fmt.Fprintf(w, "Method: %s\n", resp.Method)
		if resp.SPKIPin != "" {
			fmt.Fprintf(w, "SPKI Pin: %s\n", resp.SPKIPin)
		}
		if resp.CABundlePEM != "" && bootstrapBundleOut == "" {
			fmt.Fprint(w, resp.CABundlePEM)
		}
		return nil
	}
}
