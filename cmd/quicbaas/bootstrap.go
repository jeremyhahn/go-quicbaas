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
// QuicBaaS itself does not own bootstrap; the canonical implementation
// lives in go-xkms. These commands are a client-side pre-trust fetch: they
// call go-xkms Fetch on this machine through the SDK re-export
// (sdk/go/xkms/trust/bootstrap) and never call a QuicBaaS API.
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
	trustbootstrap "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/bootstrap"
)

// =============================================================================
// bootstrap command flags
// =============================================================================

var (
	bootstrapMethod    string
	bootstrapServerURL string
	bootstrapBundleOut string
	bootstrapOutputFmt string

	// DANE material.
	bootstrapHostname  string
	bootstrapDNSServer string

	// Noise material.
	bootstrapNoiseAddr          string
	bootstrapNoiseStaticKey     string
	bootstrapNoiseP256StaticKey string
	bootstrapNoiseSuite         string

	// SPKI material.
	bootstrapSPKIPin string
)

// The Noise suite names --noise-suite accepts. They are the same strings the
// REST bootstrap request and the TypeScript SDK use.
const (
	bootstrapNoiseSuiteNK   = "noise_nk"
	bootstrapNoiseSuiteP256 = "p256_aesgcm"
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

// BootstrapAutoRequest captures the parameters of one client-side trust
// bootstrap. Each method reads only its own material; empty fields are left
// out of the go-xkms config.
type BootstrapAutoRequest struct {
	// Method selects the bootstrap method (auto, dane, noise, spki, direct).
	Method string `json:"method"`

	// ServerURL is the https URL of the QuicBaaS server to bootstrap trust
	// from. Required.
	ServerURL string `json:"server_url,omitempty"`

	// Hostname is the DNSSEC zone the server's TLSA records are published
	// under (DANE).
	Hostname string `json:"hostname,omitempty"`

	// DNSServer optionally pins the resolver for the TLSA lookup, e.g.
	// "8.8.8.8:53" (DANE).
	DNSServer string `json:"dns_server,omitempty"`

	// NoiseAddr is the explicit Noise bootstrap address ("host:port"); empty
	// derives it from ServerURL (Noise).
	NoiseAddr string `json:"noise_addr,omitempty"`

	// NoiseStaticKey is the hex-encoded Curve25519 server public key for the
	// Noise_NK suite (Noise).
	NoiseStaticKey string `json:"noise_static_key,omitempty"`

	// NoiseP256StaticKey is the hex-encoded uncompressed P-256 server public
	// point for the approved suite (Noise).
	NoiseP256StaticKey string `json:"noise_p256_static_key,omitempty"`

	// NoiseSuite optionally pins the Noise suite: "noise_nk" or
	// "p256_aesgcm"; empty selects by policy (Noise).
	NoiseSuite string `json:"noise_suite,omitempty"`

	// SPKIPin is the hex-encoded SHA-256 SPKI pin of the server certificate
	// (SPKI).
	SPKIPin string `json:"spki_pin,omitempty"`
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

// createSDKBootstrapClient returns the SDK-backed BootstrapClient, which runs
// the trust bootstrap on this machine through go-xkms Fetch.
func createSDKBootstrapClient() (BootstrapClient, error) {
	return sdkBootstrapClient{}, nil
}

// bootstrapFetch is the seam onto the SDK's client-side trust bootstrap.
// Tests replace it; production calls go-xkms Fetch through the SDK.
var bootstrapFetch = trustbootstrap.Fetch

// sdkBootstrapClient runs the bootstrap on this machine: it builds the
// go-xkms config from the request and fetches the CA bundle before any
// trust exists. It never talks to a QuicBaaS API.
type sdkBootstrapClient struct{}

// BootstrapAuto fetches the CA bundle with the request's method and material.
func (sdkBootstrapClient) BootstrapAuto(ctx context.Context, req *BootstrapAutoRequest) (*BootstrapAutoResponse, error) {
	if req.ServerURL == "" {
		return nil, &BootstrapServerURLRequiredError{}
	}
	cfg := trustbootstrap.Config{
		Mode:             trustbootstrap.Mode(req.Method),
		ServerURL:        req.ServerURL,
		PerMethodTimeout: globalTimeout,
	}
	if req.Hostname != "" {
		cfg.DANE = &trustbootstrap.DANEConfig{Hostname: req.Hostname, DNSServer: req.DNSServer}
	}
	if req.NoiseStaticKey != "" || req.NoiseP256StaticKey != "" {
		suite, err := bootstrapNoiseSuiteByte(req.NoiseSuite)
		if err != nil {
			return nil, err
		}
		cfg.Noise = &trustbootstrap.NoiseConfig{
			ServerAddr:          req.NoiseAddr,
			ServerStaticKey:     req.NoiseStaticKey,
			ServerP256StaticKey: req.NoiseP256StaticKey,
			Suite:               suite,
		}
	}
	if req.SPKIPin != "" {
		cfg.SPKI = &trustbootstrap.SPKIConfig{Pin: req.SPKIPin}
	}
	if req.Method == string(trustbootstrap.ModeDirect) || req.Method == string(trustbootstrap.ModeAuto) {
		cfg.Direct = &trustbootstrap.DirectConfig{}
	}

	bundle, err := bootstrapFetch(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if bundle == nil {
		return nil, &BootstrapEmptyBundleError{Method: req.Method}
	}
	resp := &BootstrapAutoResponse{Method: req.Method, CABundlePEM: string(bundle.PEM)}
	if req.Method == string(trustbootstrap.ModeSPKI) {
		// The pin the bundle was verified against.
		resp.SPKIPin = req.SPKIPin
	}
	return resp, nil
}

// bootstrapNoiseSuiteByte maps a --noise-suite name onto the go-xkms suite
// byte; the empty name selects by policy (0).
func bootstrapNoiseSuiteByte(name string) (byte, error) {
	switch name {
	case "":
		return 0, nil
	case bootstrapNoiseSuiteNK:
		return trustbootstrap.SuiteNoiseNK, nil
	case bootstrapNoiseSuiteP256:
		return trustbootstrap.SuiteP256AESGCM, nil
	default:
		return 0, &BootstrapInvalidNoiseSuiteError{Got: name}
	}
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

// BootstrapServerURLRequiredError is returned, before any fetch, when no
// --server-url is given.
type BootstrapServerURLRequiredError struct{}

// Error returns a human-readable description.
func (e *BootstrapServerURLRequiredError) Error() string {
	return "bootstrap: --server-url is required (an https URL, e.g. https://quicbaas.example.com:8443)"
}

// BootstrapInvalidNoiseSuiteError is returned when --noise-suite is not
// noise_nk or p256_aesgcm.
type BootstrapInvalidNoiseSuiteError struct {
	Got string
}

// Error returns a human-readable description.
func (e *BootstrapInvalidNoiseSuiteError) Error() string {
	return "bootstrap: invalid --noise-suite " + e.Got +
		" (must be " + bootstrapNoiseSuiteNK + " or " + bootstrapNoiseSuiteP256 + ", or empty for policy)"
}

// BootstrapEmptyBundleError is returned when the fetch reports success but
// yields no CA bundle.
type BootstrapEmptyBundleError struct {
	Method string
}

// Error returns a human-readable description.
func (e *BootstrapEmptyBundleError) Error() string {
	return "bootstrap " + e.Method + ": the fetch returned no CA bundle"
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

The fetch runs on this machine, before any trust exists: it needs no
running session or credentials, only --server-url and the chosen
method's material.

Verbs:
  auto    Try methods in priority order (DANE, Noise, SPKI, Direct)
  dane    Verify the server certificate against its DNSSEC TLSA records
  noise   Fetch over Noise (Noise_NK or the approved P-256 suite)
  spki    Verify the server certificate against a SHA-256 SPKI pin
  direct  Fetch over plain HTTPS when the transport already enforces trust`,
	}
	cmd.PersistentFlags().StringVar(&bootstrapOutputFmt, "output", "table",
		"Output format (json, yaml, table)")
	cmd.AddCommand(newBootstrapAutoCommand())
	cmd.AddCommand(newBootstrapMethodCommand("dane", "Bootstrap trust with DANE/TLSA (--hostname)"))
	cmd.AddCommand(newBootstrapMethodCommand("noise", "Bootstrap trust over Noise (--noise-static-key or --noise-p256-static-key)"))
	cmd.AddCommand(newBootstrapMethodCommand("spki", "Bootstrap trust with an SPKI pin (--spki-pin)"))
	cmd.AddCommand(newBootstrapMethodCommand("direct", "Bootstrap trust with a direct HTTPS fetch"))
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
  quicbaas bootstrap auto --server-url https://quicbaas.example.com:8443 \
    --hostname quicbaas.example.com --spki-pin <sha256-hex>`,
		RunE: runBootstrapAutoCommand,
	}
	cmd.Flags().StringVar(&bootstrapMethod, "method", "auto",
		"Bootstrap method: auto, dane, noise, spki, direct")
	addBootstrapFetchFlags(cmd)
	return cmd
}

// newBootstrapMethodCommand builds 'quicbaas bootstrap <method>', which pins
// one method and otherwise runs exactly like 'bootstrap auto --method <method>'.
func newBootstrapMethodCommand(method, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   method,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrapMethod = method
			return runBootstrapAutoCommand(cmd, args)
		},
	}
	addBootstrapFetchFlags(cmd)
	return cmd
}

// addBootstrapFetchFlags registers the server URL, output and per-method
// material flags shared by every bootstrap verb.
func addBootstrapFetchFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&bootstrapServerURL, "server-url", "",
		"QuicBaaS server URL to bootstrap trust from, e.g. https://quicbaas.example.com:8443 (required)")
	f.StringVar(&bootstrapBundleOut, "bundle-output", "",
		"Path where the CA bundle PEM should be written (optional, prints to stdout when empty)")
	f.StringVar(&bootstrapHostname, "hostname", "",
		"DANE: DNSSEC zone the server's TLSA records are published under")
	f.StringVar(&bootstrapDNSServer, "dns-server", "",
		"DANE: resolver for the TLSA lookup, e.g. 8.8.8.8:53 (optional)")
	f.StringVar(&bootstrapNoiseAddr, "noise-addr", "",
		"Noise: bootstrap address host:port (optional, derived from --server-url when empty)")
	f.StringVar(&bootstrapNoiseStaticKey, "noise-static-key", "",
		"Noise: server Curve25519 public key, hex, for the Noise_NK suite")
	f.StringVar(&bootstrapNoiseP256StaticKey, "noise-p256-static-key", "",
		"Noise: server P-256 public key (uncompressed point), hex, for the approved suite")
	f.StringVar(&bootstrapNoiseSuite, "noise-suite", "",
		"Noise: pin the suite, noise_nk or p256_aesgcm (empty selects by policy)")
	f.StringVar(&bootstrapSPKIPin, "spki-pin", "",
		"SPKI: SHA-256 SPKI pin of the server certificate, hex")
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

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.BootstrapAuto(ctx, &BootstrapAutoRequest{
		Method:             bootstrapMethod,
		ServerURL:          bootstrapServerURL,
		Hostname:           bootstrapHostname,
		DNSServer:          bootstrapDNSServer,
		NoiseAddr:          bootstrapNoiseAddr,
		NoiseStaticKey:     bootstrapNoiseStaticKey,
		NoiseP256StaticKey: bootstrapNoiseP256StaticKey,
		NoiseSuite:         bootstrapNoiseSuite,
		SPKIPin:            bootstrapSPKIPin,
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
