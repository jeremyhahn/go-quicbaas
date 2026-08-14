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

// acme.go implements the `quicbaas acme` CLI verb tree.
//
// Every verb routes through Security().ACME() — the server-control surface
// (Server(): register-challenge, issue) and the broker-as-CA protocol-client
// surface (Client(): new-order, finalize-order, renew).
//
// TRANSPORT EXCEPTION: ACME is an HTTP-native protocol (RFC 8555). Per
// pkg/service/acme_service.go there are deliberately NO gRPC / QUIC / MCP
// opcodes for ACME, so this command group is a documented exception to the
// multi-protocol parity rule. The SDK compensates: when --transport is grpc,
// quic, unix, or mcp, Security().ACME() lazily builds a dedicated REST
// transport to the same --address. That requires an address a REST client can
// dial; when one cannot be resolved the SDK surfaces a typed transport error
// naming the reason instead of silently doing nothing.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// Flags
// =============================================================================

var (
	acmeOutputFmt string

	// issue / finalize-order
	acmeCSRFile        string
	acmeChallengeType  string
	acmeChallengeValue string
	acmeOrderURL       string

	// new-order
	acmeIdentifiers []string
	acmeCertOut     string
)

// =============================================================================
// Typed errors
// =============================================================================

// ACMEFlagRequiredError is returned when an acme verb is invoked without a
// required flag.
type ACMEFlagRequiredError struct {
	Verb string
	Flag string
}

// Error implements the error interface.
func (e *ACMEFlagRequiredError) Error() string {
	return "acme " + e.Verb + ": --" + e.Flag + " is required"
}

// ACMEArgRequiredError is returned when an acme verb is missing a positional
// argument.
type ACMEArgRequiredError struct {
	Verb string
	Arg  string
}

// Error implements the error interface.
func (e *ACMEArgRequiredError) Error() string {
	return "acme " + e.Verb + ": <" + e.Arg + "> argument is required"
}

// ACMEUnknownChallengeError is returned when --challenge-type / the
// register-challenge argument names a challenge QuicBaaS has no canonical
// verifier for.
type ACMEUnknownChallengeError struct {
	Name string
}

// Error implements the error interface.
func (e *ACMEUnknownChallengeError) Error() string {
	return "acme register-challenge: unknown challenge type " + e.Name +
		" (valid: " + strings.Join(acmeChallengeNames(), ", ") + ")"
}

// ACMEIdentifierFormatError is returned when --identifier is not in
// "<type>:<value>" form.
type ACMEIdentifierFormatError struct {
	Value string
}

// Error implements the error interface.
func (e *ACMEIdentifierFormatError) Error() string {
	return "acme new-order: invalid --identifier " + e.Value +
		` (expected "<type>:<value>", e.g. dns:api.example.com)`
}

// ACMEReadError wraps a filesystem error reading a CSR from disk or stdin.
type ACMEReadError struct {
	Source string
	Err    error
}

// Error implements the error interface.
func (e *ACMEReadError) Error() string {
	return "acme: failed to read " + e.Source + ": " + e.Err.Error()
}

// Unwrap exposes the underlying I/O error.
func (e *ACMEReadError) Unwrap() error { return e.Err }

// ACMEWriteError wraps a filesystem error writing an issued certificate.
type ACMEWriteError struct {
	Path string
	Err  error
}

// Error implements the error interface.
func (e *ACMEWriteError) Error() string {
	return "acme: failed to write " + e.Path + ": " + e.Err.Error()
}

// Unwrap exposes the underlying I/O error.
func (e *ACMEWriteError) Unwrap() error { return e.Err }

// ACMEEnrollCAUnsupportedError is returned by the enroll-01 verifier when the
// certificate authority it is handed cannot verify TCG-CSR-IDEVID structures.
type ACMEEnrollCAUnsupportedError struct{}

// Error implements the error interface.
func (e *ACMEEnrollCAUnsupportedError) Error() string {
	return "acme enroll-01: certificate authority does not support TCG-CSR-IDEVID verification"
}

// =============================================================================
// Challenge vocabulary
// =============================================================================

// acmeVerifiers is the canonical challenge-name -> verifier lookup.
//
// Only the NAME travels over the wire — the server installs its own canonical
// verifier for it (see acme.RegisterChallengeRequest). ACMEServerClient
// .RegisterChallenge nonetheless requires a non-nil verifier, which it keeps
// process-local for callers that also embed an ACME server. The entries below
// are therefore thin signature adapters onto exactly the same SDK-re-exported
// xkms verifier the server installs — no forked verification logic.
//
// Map-based dispatch keeps lookup O(1) as the challenge vocabulary grows.
var acmeVerifiers = map[string]quicbaas.ACMEChallengeVerifierFunc{
	quicbaas.ACMEChallengeTypeHTTP01:    acmeVerifyHTTP01,
	quicbaas.ACMEChallengeTypeDNS01:     acmeVerifyDNS01,
	quicbaas.ACMEChallengeTypeEnroll01:  acmeVerifyEnroll01,
	quicbaas.ACMEChallengeTypeEndorse01: acmeVerifyEndorse01,
}

// acmeVerifyHTTP01 adapts the SDK http-01 verifier, which takes no CA handle,
// to the uniform ACMEChallengeVerifierFunc signature.
func acmeVerifyHTTP01(resolver *net.Resolver, _ quicbaas.ACMECertificateAuthority,
	authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
	return quicbaas.ACMEVerifyHTTP01(resolver, authzValue, challengePort, challengeToken, expectedKeyAuth)
}

// acmeVerifyDNS01 adapts the SDK dns-01 verifier. *net.Resolver satisfies the
// verifier's narrower LookupTXT-only resolver interface.
func acmeVerifyDNS01(resolver *net.Resolver, _ quicbaas.ACMECertificateAuthority,
	authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
	return quicbaas.ACMEVerifyDNS01(resolver, authzValue, challengePort, challengeToken, expectedKeyAuth)
}

// acmeVerifyEndorse01 adapts the SDK endorse-01 verifier, which takes no CA
// handle (issuance is gated by External Account Binding, not by this check).
func acmeVerifyEndorse01(resolver *net.Resolver, _ quicbaas.ACMECertificateAuthority,
	authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
	return quicbaas.ACMEVerifyEndorse01(resolver, authzValue, challengePort, challengeToken, expectedKeyAuth)
}

// acmeVerifyEnroll01 adapts the SDK enroll-01 verifier, which needs the CA to
// support TCG-CSR-IDEVID verification. It fails closed when the supplied CA
// does not.
func acmeVerifyEnroll01(resolver *net.Resolver, ca quicbaas.ACMECertificateAuthority,
	authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
	tcgCA, ok := ca.(quicbaas.ACMEEnroll01TCGCSRVerifier)
	if !ok {
		return &ACMEEnrollCAUnsupportedError{}
	}
	return quicbaas.ACMEVerifyEnroll01(resolver, tcgCA, authzValue, challengePort, challengeToken, expectedKeyAuth)
}

// acmeChallengeNames returns the sorted registered challenge names, used in
// help text and error messages.
func acmeChallengeNames() []string {
	names := make([]string, 0, len(acmeVerifiers))
	for name := range acmeVerifiers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// =============================================================================
// SDK factories
// =============================================================================

// ACMEServerClientFactory constructs the ACME server-control client.
type ACMEServerClientFactory func() (quicbaas.ACMEServerClient, error)

// ACMEProtocolClientFactory constructs the ACME protocol client.
type ACMEProtocolClientFactory func() (quicbaas.ACMEClientAPI, error)

// acmeServerClientFactory resolves Security().ACME().Server(). Tests swap it.
var acmeServerClientFactory ACMEServerClientFactory = createSDKACMEServerClient

// acmeProtocolClientFactory resolves Security().ACME().Client(). Tests swap it.
var acmeProtocolClientFactory ACMEProtocolClientFactory = createSDKACMEProtocolClient

// createSDKACMEServerClient returns the SDK ACME server-control client bound
// to the active transport.
func createSDKACMEServerClient() (quicbaas.ACMEServerClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().ACME().Server(), nil
}

// createSDKACMEProtocolClient returns the SDK ACME protocol client bound to
// the active transport.
func createSDKACMEProtocolClient() (quicbaas.ACMEClientAPI, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().ACME().Client(), nil
}

// =============================================================================
// Command tree
// =============================================================================

// newACMECommand builds the `quicbaas acme` command tree.
func newACMECommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acme",
		Short: "ACME (RFC 8555) certificate operations",
		Long: `ACME operations against the active QuicBaaS daemon.

QuicBaaS is the BROKER-AS-CA: it is itself the certificate authority, so the
protocol-client verbs below are in-process service calls rather than
JWS-signed exchanges against an external ACME directory.

Verbs:
  register-challenge <name>  Install a challenge verifier on the server
  issue                      Server-side issuance from a CSR + validated challenge
  new-order                  Create a broker-as-CA order for one or more identifiers
  finalize-order             Finalize an order by signing the supplied CSR
  renew <cert-url>           Resolve an issued certificate for renewal

TRANSPORT EXCEPTION: ACME is an HTTP-native protocol; by design there are no
gRPC / QUIC / MCP opcodes for it (see pkg/service/acme_service.go). These
verbs therefore always travel over REST or the in-process embedded transport.
When --transport is grpc, quic, unix, or mcp the SDK transparently opens a
REST connection to the same --address; if no dialable REST address can be
derived the verb fails with an explicit transport error rather than silently
succeeding.`,
	}
	cmd.PersistentFlags().StringVar(&acmeOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newACMERegisterChallengeCommand())
	cmd.AddCommand(newACMEIssueCommand())
	cmd.AddCommand(newACMENewOrderCommand())
	cmd.AddCommand(newACMEFinalizeOrderCommand())
	cmd.AddCommand(newACMERenewCommand())
	return cmd
}

// =============================================================================
// register-challenge
// =============================================================================

func newACMERegisterChallengeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "register-challenge <name>",
		Short: "Install an ACME challenge verifier on the server",
		Long: `Enable a challenge type on the server's ACME directory.

The server installs its own canonical verifier for the named challenge; the
name is the only thing that travels over the wire.

Valid names: ` + strings.Join(acmeChallengeNames(), ", ") + `

Examples:
  quicbaas acme register-challenge http-01
  quicbaas acme register-challenge enroll-01 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runACMERegisterChallenge,
	}
}

func runACMERegisterChallenge(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	if name == "" {
		return formatError(&ACMEArgRequiredError{Verb: "register-challenge", Arg: "name"})
	}
	verifier, ok := acmeVerifiers[name]
	if !ok {
		return formatError(&ACMEUnknownChallengeError{Name: name})
	}

	client, err := acmeServerClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.RegisterChallenge(ctx, name, verifier); err != nil {
		return formatError(err)
	}

	return formatACMEAck(
		map[string]interface{}{"challenge": name, "registered": true},
		fmt.Sprintf("challenge %s registered\n", name),
		acmeOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// issue
// =============================================================================

func newACMEIssueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Server-side ACME issuance from a CSR",
		Long: `Issue a certificate from a PEM-encoded CSR using an already-validated
challenge. The server signs the CSR with the composed CA and returns the
leaf certificate plus the issuing chain.

Examples:
  quicbaas acme issue --csr ./api.csr --challenge-type http-01 --challenge-value api.example.com
  quicbaas acme issue --csr - --challenge-type dns-01 --challenge-value _acme-challenge.example.com`,
		Args: cobra.NoArgs,
		RunE: runACMEIssue,
	}
	cmd.Flags().StringVar(&acmeCSRFile, "csr", "", "Path to the PEM-encoded CSR ('-' reads stdin) (required)")
	cmd.Flags().StringVar(&acmeChallengeType, "challenge-type", "", "Challenge type used for validation (required)")
	cmd.Flags().StringVar(&acmeChallengeValue, "challenge-value", "", "Challenge value (identifier) that was validated")
	cmd.Flags().StringVar(&acmeCertOut, "cert-output", "", "Write the issued certificate PEM to this path instead of stdout")
	return cmd
}

func runACMEIssue(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(acmeCSRFile) == "" {
		return formatError(&ACMEFlagRequiredError{Verb: "issue", Flag: "csr"})
	}
	if strings.TrimSpace(acmeChallengeType) == "" {
		return formatError(&ACMEFlagRequiredError{Verb: "issue", Flag: "challenge-type"})
	}

	csrPEM, err := readACMEPEM(cmd, acmeCSRFile)
	if err != nil {
		return formatError(err)
	}

	client, err := acmeServerClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.Issue(ctx, csrPEM, acmeChallengeType, acmeChallengeValue)
	if err != nil {
		return formatError(err)
	}
	if acmeCertOut != "" && resp != nil {
		if werr := os.WriteFile(acmeCertOut, resp.CertificatePEM, 0o644); werr != nil {
			return formatError(&ACMEWriteError{Path: acmeCertOut, Err: werr})
		}
	}
	return formatACMEIssue(resp, acmeOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// new-order
// =============================================================================

func newACMENewOrderCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new-order",
		Short: "Create a broker-as-CA ACME order",
		Long: `Create a new ACME order for one or more identifiers.

Each --identifier is "<type>:<value>"; the type is usually "dns" or "ip".
The returned order carries the finalize URL to pass to
'quicbaas acme finalize-order --order-url'.

Examples:
  quicbaas acme new-order --identifier dns:api.example.com --challenge-type http-01
  quicbaas acme new-order --identifier dns:a.example.com --identifier dns:b.example.com`,
		Args: cobra.NoArgs,
		RunE: runACMENewOrder,
	}
	cmd.Flags().StringArrayVar(&acmeIdentifiers, "identifier", nil,
		`Order identifier as "<type>:<value>" (repeatable, required)`)
	cmd.Flags().StringVar(&acmeChallengeType, "challenge-type", "",
		"Challenge family to authorize the order with")
	return cmd
}

func runACMENewOrder(cmd *cobra.Command, _ []string) error {
	if len(acmeIdentifiers) == 0 {
		return formatError(&ACMEFlagRequiredError{Verb: "new-order", Flag: "identifier"})
	}
	identifiers, err := parseACMEIdentifiers(acmeIdentifiers)
	if err != nil {
		return formatError(err)
	}

	client, err := acmeProtocolClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	order, err := client.NewOrder(ctx, identifiers, acmeChallengeType)
	if err != nil {
		return formatError(err)
	}
	return formatACMEOrder(order, acmeOutputFmt, cmd.OutOrStdout())
}

// parseACMEIdentifiers converts "<type>:<value>" flag values into SDK order
// identifiers.
func parseACMEIdentifiers(raw []string) ([]quicbaas.ACMEOrderIdentifier, error) {
	identifiers := make([]quicbaas.ACMEOrderIdentifier, 0, len(raw))
	for _, entry := range raw {
		typ, value, found := strings.Cut(strings.TrimSpace(entry), ":")
		typ = strings.TrimSpace(typ)
		value = strings.TrimSpace(value)
		if !found || typ == "" || value == "" {
			return nil, &ACMEIdentifierFormatError{Value: entry}
		}
		identifiers = append(identifiers, quicbaas.ACMEOrderIdentifier{Type: typ, Value: value})
	}
	return identifiers, nil
}

// =============================================================================
// finalize-order
// =============================================================================

func newACMEFinalizeOrderCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "finalize-order",
		Short: "Finalize an ACME order by signing the supplied CSR",
		Long: `Finalize a previously-created order. The broker-as-CA signs the CSR and
returns the issued leaf certificate PEM.

Examples:
  quicbaas acme finalize-order --order-url /acme/order/42 --csr ./api.csr
  quicbaas acme finalize-order --order-url /acme/order/42 --csr - --cert-output ./api.crt`,
		Args: cobra.NoArgs,
		RunE: runACMEFinalizeOrder,
	}
	cmd.Flags().StringVar(&acmeOrderURL, "order-url", "", "Order URL returned by 'acme new-order' (required)")
	cmd.Flags().StringVar(&acmeCSRFile, "csr", "", "Path to the PEM-encoded CSR ('-' reads stdin) (required)")
	cmd.Flags().StringVar(&acmeCertOut, "cert-output", "", "Write the issued certificate PEM to this path instead of stdout")
	return cmd
}

func runACMEFinalizeOrder(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(acmeOrderURL) == "" {
		return formatError(&ACMEFlagRequiredError{Verb: "finalize-order", Flag: "order-url"})
	}
	if strings.TrimSpace(acmeCSRFile) == "" {
		return formatError(&ACMEFlagRequiredError{Verb: "finalize-order", Flag: "csr"})
	}

	csrPEM, err := readACMEPEM(cmd, acmeCSRFile)
	if err != nil {
		return formatError(err)
	}

	client, err := acmeProtocolClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	certPEM, err := client.FinalizeOrder(ctx, acmeOrderURL, csrPEM)
	if err != nil {
		return formatError(err)
	}
	if acmeCertOut != "" {
		if werr := os.WriteFile(acmeCertOut, certPEM, 0o644); werr != nil {
			return formatError(&ACMEWriteError{Path: acmeCertOut, Err: werr})
		}
	}
	return formatACMECertificate(certPEM, acmeOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// renew
// =============================================================================

func newACMERenewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "renew <cert-url>",
		Short: "Resolve an issued certificate for renewal",
		Long: `Resolve a previously-issued certificate by its ACME certificate URL.

<cert-url> is the hex serial the CA assigned at issuance, optionally in
"/acme/cert/<serial>" form.

Because the broker-as-CA holds no subscriber private key, renewal cannot
re-sign a leaf from this verb alone; use 'acme new-order' plus
'acme finalize-order' with a fresh CSR to obtain a replacement.

Examples:
  quicbaas acme renew 7a4f3c1d
  quicbaas acme renew /acme/cert/7a4f3c1d --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runACMERenew,
	}
	cmd.Flags().StringVar(&acmeCertOut, "cert-output", "", "Write the resolved certificate PEM to this path instead of stdout")
	return cmd
}

func runACMERenew(cmd *cobra.Command, args []string) error {
	certURL := strings.TrimSpace(args[0])
	if certURL == "" {
		return formatError(&ACMEArgRequiredError{Verb: "renew", Arg: "cert-url"})
	}

	client, err := acmeProtocolClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	certPEM, err := client.Renew(ctx, certURL)
	if err != nil {
		return formatError(err)
	}
	if acmeCertOut != "" {
		if werr := os.WriteFile(acmeCertOut, certPEM, 0o644); werr != nil {
			return formatError(&ACMEWriteError{Path: acmeCertOut, Err: werr})
		}
	}
	return formatACMECertificate(certPEM, acmeOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Input helpers
// =============================================================================

// readACMEPEM reads PEM material from path, or from the command's stdin when
// path is "-".
func readACMEPEM(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, &ACMEReadError{Source: "stdin", Err: err}
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ACMEReadError{Source: path, Err: err}
	}
	return data, nil
}

// =============================================================================
// Formatting
// =============================================================================

// formatACMEIssue renders an ACME issuance result.
func formatACMEIssue(resp *quicbaas.ACMEIssueResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if resp == nil {
		fmt.Fprintln(w, "(no response)")
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
		fmt.Fprintf(tw, "Serial Number:\t%s\n", resp.SerialNumber)
		if !resp.NotBefore.IsZero() {
			fmt.Fprintf(tw, "Not Before:\t%s\n", resp.NotBefore.UTC().Format(time.RFC3339))
		}
		if !resp.NotAfter.IsZero() {
			fmt.Fprintf(tw, "Not After:\t%s\n", resp.NotAfter.UTC().Format(time.RFC3339))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		writePEMBlock(w, "Certificate PEM", resp.CertificatePEM)
		writePEMBlock(w, "Chain PEM", resp.ChainPEM)
		return nil

	default:
		writeTrailingNewline(w, resp.CertificatePEM)
		return nil
	}
}

// formatACMEOrder renders a broker-as-CA order descriptor.
func formatACMEOrder(order *quicbaas.ACMEOrder, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if order == nil {
		fmt.Fprintln(w, "(no order)")
		return nil
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(order)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(order)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Order ID:\t%d\n", order.ID)
		fmt.Fprintf(tw, "Status:\t%s\n", order.Status)
		fmt.Fprintf(tw, "URL:\t%s\n", order.URL)
		fmt.Fprintf(tw, "Finalize:\t%s\n", order.Finalize)
		for _, id := range order.Identifiers {
			fmt.Fprintf(tw, "Identifier:\t%s:%s\n", id.Type, id.Value)
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%d %s\n", order.ID, order.Status)
		return nil
	}
}

// formatACMECertificate renders a bare certificate PEM result.
func formatACMECertificate(certPEM []byte, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	payload := map[string]string{"certificate_pem": string(certPEM)}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(payload)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(payload)

	default:
		writeTrailingNewline(w, certPEM)
		return nil
	}
}

// formatACMEAck renders the acknowledgement returned by verbs with no body.
func formatACMEAck(payload map[string]interface{}, humanLine, format string, w io.Writer) error {
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

// writePEMBlock prints a labelled PEM section, skipping empty material.
func writePEMBlock(w io.Writer, label string, pem []byte) {
	if len(pem) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s:\n", label)
	writeTrailingNewline(w, pem)
}

// writeTrailingNewline writes b and guarantees it ends with a newline.
func writeTrailingNewline(w io.Writer, b []byte) {
	fmt.Fprint(w, string(b))
	if len(b) == 0 || b[len(b)-1] != '\n' {
		fmt.Fprintln(w)
	}
}
