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

// trust.go implements the `quicbaas trust` CLI verb tree.
//
// Every verb routes through Security().Trust() over the active SDK transport
// (rest / grpc / quic / mcp / unix / embedded).
//
// The trust-bootstrap dispatcher — Security().Trust().Bootstrap() — is NOT
// duplicated here: it already has a canonical home at `quicbaas bootstrap`.
// This tree covers the three server-side trust-artifact verbs only.
package main

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// Flags
// =============================================================================

var (
	trustOutputFmt string

	// publish-dane.
	trustDANEName     string
	trustDANEPort     int
	trustDANECertFile string

	// auto-import-aia.
	trustAIAChainFile string

	// cross-signed-roots.
	trustRootsOut string
)

// =============================================================================
// Typed errors
// =============================================================================

// TrustFlagRequiredError is returned when a trust verb is invoked without a
// required flag.
type TrustFlagRequiredError struct {
	Verb string
	Flag string
}

// Error implements the error interface.
func (e *TrustFlagRequiredError) Error() string {
	return "trust " + e.Verb + ": --" + e.Flag + " is required"
}

// TrustInvalidPortError is returned when --port is outside the valid TCP range.
type TrustInvalidPortError struct {
	Port int
}

// Error implements the error interface.
func (e *TrustInvalidPortError) Error() string {
	return fmt.Sprintf("trust publish-dane: --port %d is out of range (1-65535)", e.Port)
}

// TrustReadError wraps a filesystem error reading certificate material.
type TrustReadError struct {
	Source string
	Err    error
}

// Error implements the error interface.
func (e *TrustReadError) Error() string {
	return "trust: failed to read " + e.Source + ": " + e.Err.Error()
}

// Unwrap exposes the underlying I/O error.
func (e *TrustReadError) Unwrap() error { return e.Err }

// TrustWriteError wraps a filesystem error writing certificate material.
type TrustWriteError struct {
	Path string
	Err  error
}

// Error implements the error interface.
func (e *TrustWriteError) Error() string {
	return "trust: failed to write " + e.Path + ": " + e.Err.Error()
}

// Unwrap exposes the underlying I/O error.
func (e *TrustWriteError) Unwrap() error { return e.Err }

// TrustNoCertificatesError is returned when a supplied PEM file contains no
// parsable CERTIFICATE blocks.
type TrustNoCertificatesError struct {
	Source string
}

// Error implements the error interface.
func (e *TrustNoCertificatesError) Error() string {
	return "trust: no PEM CERTIFICATE blocks found in " + e.Source
}

// TrustCertificateParseError wraps an x509 parse failure.
type TrustCertificateParseError struct {
	Source string
	Index  int
	Err    error
}

// Error implements the error interface.
func (e *TrustCertificateParseError) Error() string {
	return fmt.Sprintf("trust: %s: certificate #%d is not valid DER: %s", e.Source, e.Index, e.Err.Error())
}

// Unwrap exposes the underlying parse error.
func (e *TrustCertificateParseError) Unwrap() error { return e.Err }

// TrustDANEPublisherError decorates a PublishDANE failure with the reason the
// call almost always fails: QuicBaaS ships no DANE (DNS TLSA) publisher.
// The embedding application registers one — go-trusted-platform does — so a
// bare quicbaasd has nothing to publish through.
type TrustDANEPublisherError struct {
	Err error
}

// Error implements the error interface.
func (e *TrustDANEPublisherError) Error() string {
	return "trust publish-dane: " + e.Err.Error() +
		"\n\nPublishDANE requires a DANE (DNS TLSA) publisher registered on the server." +
		"\nQuicBaaS deliberately ships none — DNS provisioning belongs to the embedding" +
		"\napplication (go-trusted-platform registers one via the server's DANE publisher" +
		"\noption). Against a bare quicbaasd this verb cannot succeed."
}

// Unwrap exposes the underlying transport/service error.
func (e *TrustDANEPublisherError) Unwrap() error { return e.Err }

// =============================================================================
// SDK factory
// =============================================================================

// TrustClientFactory constructs the SDK Trust service client.
type TrustClientFactory func() (quicbaas.TrustService, error)

// trustClientFactory resolves Security().Trust(). Tests swap it.
var trustClientFactory TrustClientFactory = createSDKTrustClient

// createSDKTrustClient returns the SDK Trust service bound to the active
// transport.
func createSDKTrustClient() (quicbaas.TrustService, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().Trust(), nil
}

// =============================================================================
// Command tree
// =============================================================================

// newTrustCommand builds the `quicbaas trust` command tree.
func newTrustCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Trust artifact operations (DANE, cross-signed roots, AIA import)",
		Long: `Trust artifact operations against the active QuicBaaS daemon.

Verbs:
  publish-dane        Publish DANE TLSA records for a (name, port, cert) tuple
  cross-signed-roots  Fetch the cross-signed root pool used to bridge issuers
  auto-import-aia     Walk a chain's AIA extensions and import the referenced CAs

All verbs route through Security().Trust() on the active SDK transport
(rest, grpc, quic, mcp, unix, embedded).

Trust-bootstrap (DANE / Noise / SPKI / Direct fetch of the initial CA bundle)
lives under 'quicbaas bootstrap', not here.

NOTE: 'publish-dane' needs a DANE publisher registered on the server. QuicBaaS
ships none by design; the embedding application supplies it (go-trusted-platform
does). On a bare quicbaasd the verb reports that explicitly.`,
	}
	cmd.PersistentFlags().StringVar(&trustOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newTrustPublishDANECommand())
	cmd.AddCommand(newTrustCrossSignedRootsCommand())
	cmd.AddCommand(newTrustAutoImportAIACommand())
	return cmd
}

// =============================================================================
// publish-dane
// =============================================================================

func newTrustPublishDANECommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish-dane",
		Short: "Publish DANE TLSA records for a certificate",
		Long: `Derive and publish DANE TLSA records for the supplied (name, port,
certificate) tuple.

The server builds the TLSA records and hands them to the registered DANE
publisher. QuicBaaS ships NO publisher — DNS provisioning belongs to the
embedding application — so against a bare quicbaasd this verb reports the
missing-publisher condition rather than a generic failure.

Examples:
  quicbaas trust publish-dane --name api.example.com --port 443 --cert ./api.crt`,
		Args: cobra.NoArgs,
		RunE: runTrustPublishDANE,
	}
	cmd.Flags().StringVar(&trustDANEName, "name", "", "DNS name the TLSA records are published for (required)")
	cmd.Flags().IntVar(&trustDANEPort, "port", 0, "TCP port the TLSA records are published for (required)")
	cmd.Flags().StringVar(&trustDANECertFile, "cert", "", "Path to the PEM-encoded certificate ('-' reads stdin) (required)")
	return cmd
}

func runTrustPublishDANE(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(trustDANEName) == "" {
		return formatError(&TrustFlagRequiredError{Verb: "publish-dane", Flag: "name"})
	}
	if trustDANEPort == 0 {
		return formatError(&TrustFlagRequiredError{Verb: "publish-dane", Flag: "port"})
	}
	if trustDANEPort < 1 || trustDANEPort > 65535 {
		return formatError(&TrustInvalidPortError{Port: trustDANEPort})
	}
	if strings.TrimSpace(trustDANECertFile) == "" {
		return formatError(&TrustFlagRequiredError{Verb: "publish-dane", Flag: "cert"})
	}

	certs, err := readTrustCertificates(cmd, trustDANECertFile)
	if err != nil {
		return formatError(err)
	}

	client, err := trustClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.PublishDANE(ctx, trustDANEName, trustDANEPort, certs[0]); err != nil {
		return formatError(&TrustDANEPublisherError{Err: err})
	}

	return formatTrustAck(
		map[string]interface{}{
			"name":      trustDANEName,
			"port":      trustDANEPort,
			"published": true,
		},
		fmt.Sprintf("DANE TLSA records published for %s:%d\n", trustDANEName, trustDANEPort),
		trustOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// cross-signed-roots
// =============================================================================

func newTrustCrossSignedRootsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cross-signed-roots",
		Short: "Fetch the cross-signed root pool",
		Long: `Fetch the cross-signed root certificate pool used to verify chains that
bridge between issuers.

The SDK returns an *x509.CertPool, whose only enumeration hook is the set of
DER-encoded subjects, so the output is a subject listing rather than PEM
certificates. Use --roots-output to write that listing to a file.

Examples:
  quicbaas trust cross-signed-roots
  quicbaas trust cross-signed-roots --output json`,
		Args: cobra.NoArgs,
		RunE: runTrustCrossSignedRoots,
	}
	cmd.Flags().StringVar(&trustRootsOut, "roots-output", "",
		"Write the root subject listing to this path instead of stdout")
	return cmd
}

func runTrustCrossSignedRoots(cmd *cobra.Command, _ []string) error {
	client, err := trustClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	pool, err := client.CrossSignedRootPool(ctx)
	if err != nil {
		return formatError(err)
	}

	subjects := crossSignedRootSubjects(pool)
	if trustRootsOut != "" {
		body := strings.Join(subjects, "\n") + "\n"
		if werr := os.WriteFile(trustRootsOut, []byte(body), 0o644); werr != nil {
			return formatError(&TrustWriteError{Path: trustRootsOut, Err: werr})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%d cross-signed root subject(s) written to %s\n",
			len(subjects), trustRootsOut)
		return nil
	}

	return formatTrustRoots(subjects, trustOutputFmt, cmd.OutOrStdout())
}

// crossSignedRootSubjects renders each root in the pool as an RFC 2253-style
// subject string.
//
// x509.CertPool exposes no certificate iterator; Subjects() is the only
// enumeration hook. It is deprecated solely because it returns nothing for
// pools sourced from SystemCertPool — this pool is always built by the SDK
// from explicit PEM/DER, so the results are complete.
func crossSignedRootSubjects(pool *x509.CertPool) []string {
	if pool == nil {
		return nil
	}
	raw := pool.Subjects() //nolint:staticcheck // complete for non-system pools
	subjects := make([]string, 0, len(raw))
	for _, der := range raw {
		var rdn pkix.RDNSequence
		if _, err := asn1.Unmarshal(der, &rdn); err != nil {
			subjects = append(subjects, "<unparsable subject>")
			continue
		}
		var name pkix.Name
		name.FillFromRDNSequence(&rdn)
		subjects = append(subjects, name.String())
	}
	return subjects
}

// =============================================================================
// auto-import-aia
// =============================================================================

func newTrustAutoImportAIACommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auto-import-aia",
		Short: "Import authority certificates referenced by a chain's AIA extensions",
		Long: `Walk the AuthorityInformationAccess extensions of the supplied certificate
chain and import every referenced authority certificate into the server's
trust store.

--chain accepts a PEM file holding one or more concatenated certificates
('-' reads stdin).

Examples:
  quicbaas trust auto-import-aia --chain ./leaf-and-intermediates.pem
  cat chain.pem | quicbaas trust auto-import-aia --chain -`,
		Args: cobra.NoArgs,
		RunE: runTrustAutoImportAIA,
	}
	cmd.Flags().StringVar(&trustAIAChainFile, "chain", "",
		"Path to the PEM-encoded certificate chain ('-' reads stdin) (required)")
	return cmd
}

func runTrustAutoImportAIA(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(trustAIAChainFile) == "" {
		return formatError(&TrustFlagRequiredError{Verb: "auto-import-aia", Flag: "chain"})
	}

	chain, err := readTrustCertificates(cmd, trustAIAChainFile)
	if err != nil {
		return formatError(err)
	}

	client, err := trustClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.AutoImportAIA(ctx, chain); err != nil {
		return formatError(err)
	}

	return formatTrustAck(
		map[string]interface{}{
			"chain_length": len(chain),
			"imported":     true,
		},
		fmt.Sprintf("AIA import completed for a %d-certificate chain\n", len(chain)),
		trustOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// Input helpers
// =============================================================================

// readTrustCertificates reads a PEM bundle from path (or the command's stdin
// when path is "-") and parses every CERTIFICATE block it contains.
func readTrustCertificates(cmd *cobra.Command, path string) ([]*x509.Certificate, error) {
	source := path
	var (
		data []byte
		err  error
	)
	if path == "-" {
		source = "stdin"
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, &TrustReadError{Source: source, Err: err}
	}
	return parseTrustCertificates(source, data)
}

// parseTrustCertificates decodes every CERTIFICATE block in a PEM bundle.
func parseTrustCertificates(source string, data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, &TrustCertificateParseError{Source: source, Index: len(certs), Err: err}
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, &TrustNoCertificatesError{Source: source}
	}
	return certs, nil
}

// =============================================================================
// Formatting
// =============================================================================

// formatTrustRoots renders the cross-signed root subject listing.
func formatTrustRoots(subjects []string, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	payload := map[string]interface{}{
		"count":    len(subjects),
		"subjects": subjects,
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(payload)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(payload)

	case cli.OutputFormatTable:
		if len(subjects) == 0 {
			fmt.Fprintln(w, "No cross-signed roots")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "#\tSUBJECT")
		for i, s := range subjects {
			fmt.Fprintf(tw, "%d\t%s\n", i+1, s)
		}
		return tw.Flush()

	default:
		for _, s := range subjects {
			fmt.Fprintln(w, s)
		}
		return nil
	}
}

// formatTrustAck renders the acknowledgement returned by verbs with no body.
func formatTrustAck(payload map[string]interface{}, humanLine, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(payload)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(payload)

	default:
		fmt.Fprint(w, humanLine)
		return nil
	}
}
