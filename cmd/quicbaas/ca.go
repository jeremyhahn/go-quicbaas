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

// Package main implements the `quicbaas ca` CLI verb tree.
//
// The CA commands delegate to Security().Certificate() over the active
// SDK transport (rest / grpc / quic / mcp / unix / embedded). Verbs are
// the canonical set from docs/08-bootstrap-auth-architecture.md §6:
//
//	ca info                Show the active issuing CA's parsed metadata
//	ca bundle              Print the CA trust-chain PEM to stdout
//	ca list                List certificates stored in the CA cert store
//	ca issue               Issue a fresh certificate (key generated server-side)
//	ca revoke <serial>     Revoke a previously-issued certificate by serial
//	ca export-bundle <f>   Write the CA bundle PEM to a file on disk
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	certclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// =============================================================================
// Per-command flags
// =============================================================================

var (
	caOutputFmt string

	// ca issue flags.
	caIssueCommonName   string
	caIssueOrganization string
	caIssueDNSNames     []string
	caIssueIPAddresses  []string
	caIssueValidityDays int
	caIssueUsage        string
	caIssueKeyAlgorithm string
	caIssueKeySize      int

	// ca list flags.
	caListCommonNameFilter string
	caListIncludeRevoked   bool
	caListIncludeExpired   bool
	caListLimit            int
)

// =============================================================================
// Errors
// =============================================================================

// CAArgRequiredError is returned by ca verbs that take a positional
// argument when the argument is missing.
type CAArgRequiredError struct {
	Verb string
	Arg  string
}

// Error implements error.
func (e *CAArgRequiredError) Error() string {
	return "ca " + e.Verb + ": <" + e.Arg + "> argument is required"
}

// CAFlagRequiredError is returned when a ca verb is invoked without a
// required flag.
type CAFlagRequiredError struct {
	Verb string
	Flag string
}

// Error implements error.
func (e *CAFlagRequiredError) Error() string {
	return "ca " + e.Verb + ": --" + e.Flag + " is required"
}

// CAExportWriteError wraps a filesystem error from `ca export-bundle`.
type CAExportWriteError struct {
	Path string
	Err  error
}

// Error implements error.
func (e *CAExportWriteError) Error() string {
	return "ca export-bundle: write " + e.Path + ": " + e.Err.Error()
}

// Unwrap exposes the underlying error for errors.Is / errors.As.
func (e *CAExportWriteError) Unwrap() error { return e.Err }

// =============================================================================
// SDK factory
// =============================================================================

// caClientFactory is the indirection point CLI tests use to swap in a
// fake CertificateClient. Production code returns the SDK-backed
// quicbaas.CertificateClient bound to the active transport.
var caClientFactory = createCASDKCertificateClient

// createCASDKCertificateClient resolves the active SDK client and
// returns its Security().Certificate() handle.
func createCASDKCertificateClient() (quicbaas.CertificateClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().Certificate(), nil
}

// =============================================================================
// info
// =============================================================================

// newCACmd builds the canonical `quicbaas ca` command tree. It is the
// production replacement for the prior stub in canonical.go.
func newCACmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ca",
		Short: "Certificate Authority operations",
		Long: `Certificate Authority operations against the active QuicBaaS daemon.

The verb set is the canonical surface per docs/08-bootstrap-auth-architecture.md §6:

  info           Show the active issuing CA's parsed metadata
  bundle         Print the CA trust-chain PEM to stdout
  list           List certificates stored in the CA cert store
  issue          Issue a fresh certificate (key generated server-side)
  revoke         Revoke a previously-issued certificate by hex serial
  export-bundle  Write the CA bundle PEM to a file on disk

All verbs route through Security().Certificate() on the active SDK
transport (rest, grpc, quic, mcp, unix, embedded).`,
	}
	cmd.PersistentFlags().StringVar(&caOutputFmt, "output", "table", "Output format (json, table)")

	cmd.AddCommand(newCAInfoCmd())
	cmd.AddCommand(newCABundleCmd())
	cmd.AddCommand(newCAListCmd())
	cmd.AddCommand(newCAIssueCmd())
	cmd.AddCommand(newCARevokeCmd())
	cmd.AddCommand(newCAExportBundleCmd())
	return cmd
}

// =============================================================================
// info
// =============================================================================

func newCAInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show metadata for the active issuing CA",
		Long: `Display parsed metadata for the active issuing CA: Common Name,
subject/issuer, serial, validity window, key type/size, signature
algorithm, plus the CA leaf PEM and the full trust-chain bundle PEM.`,
		RunE: runCAInfo,
	}
}

func runCAInfo(cmd *cobra.Command, _ []string) error {
	client, err := caClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.CAInfo(ctx, &certclient.CAInfoRequest{})
	if err != nil {
		return formatError(err)
	}
	return writeCAInfo(cmd.OutOrStdout(), resp, caOutputFmt)
}

// =============================================================================
// bundle
// =============================================================================

func newCABundleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bundle",
		Short: "Print the CA trust-chain bundle PEM to stdout",
		Long: `Print the PEM-encoded CA trust-chain bundle.

The output is the raw concatenated certificate chain (issuer-first).
Pipe to a file or use 'quicbaas ca export-bundle <path>' to write
directly to disk.`,
		RunE: runCABundle,
	}
}

func runCABundle(cmd *cobra.Command, _ []string) error {
	client, err := caClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GetCABundle(ctx, &certclient.GetCABundleRequest{})
	if err != nil {
		return formatError(err)
	}
	if caOutputFmt == "json" {
		out, jerr := json.MarshalIndent(resp, "", "  ")
		if jerr != nil {
			return formatError(jerr)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), resp.BundlePEM)
	if !strings.HasSuffix(resp.BundlePEM, "\n") {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

// =============================================================================
// list
// =============================================================================

func newCAListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List certificates stored in the CA certificate store",
		Long: `Enumerate certificates currently held in the CA cert store.

By default only currently-valid (non-revoked, non-expired) certificates
are returned. Use --include-revoked / --include-expired to broaden the
result set, --common-name to filter by Common Name substring, and
--limit to cap the response.`,
		RunE: runCAList,
	}
	cmd.Flags().StringVar(&caListCommonNameFilter, "common-name", "", "Substring filter on Common Name (case-insensitive)")
	cmd.Flags().BoolVar(&caListIncludeRevoked, "include-revoked", false, "Include revoked certificates in the response")
	cmd.Flags().BoolVar(&caListIncludeExpired, "include-expired", false, "Include expired certificates in the response")
	cmd.Flags().IntVar(&caListLimit, "limit", 0, "Maximum certificates to return (0 = no caller-supplied limit)")
	return cmd
}

func runCAList(cmd *cobra.Command, _ []string) error {
	if caListLimit < 0 {
		return formatError(&CAFlagRequiredError{Verb: "list", Flag: "limit (must not be negative)"})
	}
	client, err := caClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.ListCertificates(ctx, &certclient.ListCertificatesRequest{
		CommonNameFilter: caListCommonNameFilter,
		IncludeRevoked:   caListIncludeRevoked,
		IncludeExpired:   caListIncludeExpired,
		Limit:            caListLimit,
	})
	if err != nil {
		return formatError(err)
	}
	return writeCertificateList(cmd.OutOrStdout(), resp, caOutputFmt)
}

// =============================================================================
// issue
// =============================================================================

func newCAIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Issue a fresh certificate (key generated server-side)",
		Long: `Issue a fresh certificate from the active CA.

The server generates a new keypair and signs a certificate with the
supplied subject. The response includes the certificate PEM, chain
PEM, and (server-generated) private-key PEM.

Examples:
  quicbaas ca issue --common-name api.example.com --dns api.example.com --validity-days 90
  quicbaas ca issue --common-name client-1 --usage client --key-algorithm ecdsa`,
		RunE: runCAIssue,
	}
	cmd.Flags().StringVar(&caIssueCommonName, "common-name", "", "Certificate Common Name (required)")
	cmd.Flags().StringVar(&caIssueOrganization, "organization", "", "Subject Organization")
	cmd.Flags().StringSliceVar(&caIssueDNSNames, "dns", nil, "DNS SAN entries (repeatable)")
	cmd.Flags().StringSliceVar(&caIssueIPAddresses, "ip", nil, "IP SAN entries (repeatable)")
	cmd.Flags().IntVar(&caIssueValidityDays, "validity-days", 0, "Certificate validity in days (0 = profile default)")
	cmd.Flags().StringVar(&caIssueUsage, "usage", "", "Usage profile (client | server | both)")
	cmd.Flags().StringVar(&caIssueKeyAlgorithm, "key-algorithm", "", "Key algorithm (ecdsa | rsa)")
	cmd.Flags().IntVar(&caIssueKeySize, "key-size", 0, "Key size in bits (0 = algorithm default)")
	return cmd
}

func runCAIssue(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(caIssueCommonName) == "" {
		return formatError(&CAFlagRequiredError{Verb: "issue", Flag: "common-name"})
	}
	client, err := caClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &certclient.IssueCertificateRequest{
		CommonName:   caIssueCommonName,
		Organization: caIssueOrganization,
		DNSNames:     append([]string(nil), caIssueDNSNames...),
		IPAddresses:  append([]string(nil), caIssueIPAddresses...),
		ValidityDays: caIssueValidityDays,
		Usage:        caIssueUsage,
		KeyAlgorithm: caIssueKeyAlgorithm,
		KeySize:      caIssueKeySize,
	}

	resp, err := client.IssueCertificate(ctx, req)
	if err != nil {
		return formatError(err)
	}
	return writeIssuedCertificate(cmd.OutOrStdout(), resp, caOutputFmt)
}

// =============================================================================
// revoke
// =============================================================================

func newCARevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <serial>",
		Short: "Revoke a certificate by hex serial number",
		Long: `Revoke a previously-issued certificate by serial.

The serial is the hex-encoded big-integer serial returned by
'quicbaas ca issue' / 'quicbaas ca list'. The optional 0x prefix is
accepted but not required.

Example:
  quicbaas ca revoke 7a4f3c1d`,
		Args: cobra.ExactArgs(1),
		RunE: runCARevoke,
	}
}

func runCARevoke(cmd *cobra.Command, args []string) error {
	serial := strings.TrimSpace(args[0])
	if serial == "" {
		return formatError(&CAArgRequiredError{Verb: "revoke", Arg: "serial"})
	}
	client, err := caClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RevokeCertificate(ctx, &certclient.RevokeCertificateRequest{
		SerialNumber: serial,
	})
	if err != nil {
		return formatError(err)
	}
	if caOutputFmt == "json" {
		out, jerr := json.MarshalIndent(resp, "", "  ")
		if jerr != nil {
			return formatError(jerr)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	if resp.Success {
		fmt.Fprintf(cmd.OutOrStdout(), "certificate %s revoked\n", serial)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "certificate %s revocation reported by server as unsuccessful\n", serial)
	}
	return nil
}

// =============================================================================
// export-bundle
// =============================================================================

func newCAExportBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-bundle <path>",
		Short: "Export the CA bundle PEM to a file on disk",
		Long: `Write the PEM-encoded CA trust-chain bundle to <path>.

The file is created with 0644 permissions, overwriting any existing
file at that path. Use '-' to write the bundle to stdout.`,
		Args: cobra.ExactArgs(1),
		RunE: runCAExportBundle,
	}
	return cmd
}

func runCAExportBundle(cmd *cobra.Command, args []string) error {
	path := strings.TrimSpace(args[0])
	if path == "" {
		return formatError(&CAArgRequiredError{Verb: "export-bundle", Arg: "path"})
	}
	client, err := caClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GetCABundle(ctx, &certclient.GetCABundleRequest{})
	if err != nil {
		return formatError(err)
	}

	if path == "-" {
		fmt.Fprint(cmd.OutOrStdout(), resp.BundlePEM)
		if !strings.HasSuffix(resp.BundlePEM, "\n") {
			fmt.Fprintln(cmd.OutOrStdout())
		}
		return nil
	}

	if werr := os.WriteFile(path, []byte(resp.BundlePEM), 0o644); werr != nil { // #nosec G306 -- an exported CA bundle is public material written to an operator-chosen path.
		return formatError(&CAExportWriteError{Path: path, Err: werr})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "CA bundle written to %s (%d bytes)\n", path, len(resp.BundlePEM))
	return nil
}

// =============================================================================
// Output formatting
// =============================================================================

// caWriter is the io.Writer subset used by formatters; mirrors
// stringWriter from credential.go so the two CLIs share a contract.
type caWriter interface {
	Write(p []byte) (int, error)
}

func writeCAInfo(w caWriter, resp *certclient.CAInfoResponse, format string) error {
	if resp == nil {
		fmt.Fprintln(w, "(no CA info)")
		return nil
	}
	if format == "json" {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return formatError(err)
		}
		fmt.Fprintln(w, string(out))
		return nil
	}
	fmt.Fprintf(w, "Common Name:         %s\n", resp.CommonName)
	fmt.Fprintf(w, "Subject:             %s\n", resp.Subject)
	fmt.Fprintf(w, "Issuer:              %s\n", resp.Issuer)
	fmt.Fprintf(w, "Serial Number:       %s\n", resp.SerialNumber)
	fmt.Fprintf(w, "Not Before:          %s\n", resp.NotBefore.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "Not After:           %s\n", resp.NotAfter.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "Is CA:               %t\n", resp.IsCA)
	if resp.KeyBits > 0 {
		fmt.Fprintf(w, "Key Type:            %s (%d bits)\n", resp.KeyType, resp.KeyBits)
	} else {
		fmt.Fprintf(w, "Key Type:            %s\n", resp.KeyType)
	}
	fmt.Fprintf(w, "Signature Algorithm: %s\n", resp.SignatureAlgorithm)
	return nil
}

func writeCertificateList(w caWriter, resp *certclient.ListCertificatesResponse, format string) error {
	if resp == nil {
		fmt.Fprintln(w, "(no certificates)")
		return nil
	}
	if format == "json" {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return formatError(err)
		}
		fmt.Fprintln(w, string(out))
		return nil
	}
	if len(resp.Certificates) == 0 {
		fmt.Fprintf(w, "(no matching certificates; total in store: %d)\n", resp.TotalCount)
		return nil
	}
	fmt.Fprintf(w, "%-32s %-20s %-25s %-25s %-6s\n", "COMMON NAME", "SERIAL", "NOT BEFORE", "NOT AFTER", "IS CA")
	for _, c := range resp.Certificates {
		fmt.Fprintf(w, "%-32s %-20s %-25s %-25s %-6t\n",
			truncate(c.CommonName, 32),
			truncate(c.SerialNumber, 20),
			c.NotBefore.UTC().Format(time.RFC3339),
			c.NotAfter.UTC().Format(time.RFC3339),
			c.IsCA,
		)
	}
	fmt.Fprintf(w, "\n(showing %d of %d total)\n", len(resp.Certificates), resp.TotalCount)
	return nil
}

func writeIssuedCertificate(w caWriter, resp *certclient.IssueCertificateResponse, format string) error {
	if resp == nil {
		fmt.Fprintln(w, "(no response)")
		return nil
	}
	if format == "json" {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return formatError(err)
		}
		fmt.Fprintln(w, string(out))
		return nil
	}
	fmt.Fprintf(w, "Serial Number:    %s\n", resp.SerialNumber)
	fmt.Fprintf(w, "Not Before:       %s\n", resp.NotBefore.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "Not After:        %s\n", resp.NotAfter.UTC().Format(time.RFC3339))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Certificate PEM:")
	fmt.Fprint(w, resp.CertificatePEM)
	if !strings.HasSuffix(resp.CertificatePEM, "\n") {
		fmt.Fprintln(w)
	}
	if resp.ChainPEM != "" {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Chain PEM:")
		fmt.Fprint(w, resp.ChainPEM)
		if !strings.HasSuffix(resp.ChainPEM, "\n") {
			fmt.Fprintln(w)
		}
	}
	if resp.PrivateKeyPEM != "" {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Private Key PEM:")
		fmt.Fprint(w, resp.PrivateKeyPEM)
		if !strings.HasSuffix(resp.PrivateKeyPEM, "\n") {
			fmt.Fprintln(w)
		}
	}
	return nil
}

// truncate returns s if its rune count is below max, otherwise returns
// the first max-1 runes plus an ellipsis. Avoids fmt's truncation
// behaviour (which doesn't add ellipses) for table-style output.
func truncate(s string, max int) string {
	if max <= 1 {
		return s
	}
	if len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}
