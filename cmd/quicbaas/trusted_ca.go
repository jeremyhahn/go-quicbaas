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

// trusted_ca.go implements the `quicbaas trusted-ca` CLI verb tree.
//
// Every verb routes through the go-xkms CA re-exports, Security().TrustedCA()
// and Security().TCGCA(), over the active SDK transport (rest / grpc / quic /
// mcp / unix / embedded). The go-xkms request and response types are passed
// through unchanged: the CLI adds no wire types of its own.
//
// `quicbaas ca` stays on Security().Certificate(); this tree does not replace
// it.

package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	xkmstransport "github.com/jeremyhahn/go-xkms/pkg/api/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// Flags
// =============================================================================

var (
	trustedCAOutputFmt string

	trustedCABundleStoreType string
	trustedCABundleAlgorithm string
	trustedCAIdentity        string

	trustedCACSRFile      string
	trustedCAProfile      string
	trustedCAValidityDays int

	trustedCACommonName   string
	trustedCAOrganization string
	trustedCADNSNames     []string
	trustedCAAlgorithm    string

	trustedCARevokeReason int

	trustedCATCGCommonName   string
	trustedCATCGOrganization string
	trustedCATCGPublicKey    string
	trustedCATCGCSRFile      string
)

// =============================================================================
// Typed errors
// =============================================================================

// TrustedCAFlagRequiredError is returned when a trusted-ca verb is invoked
// without a required flag.
type TrustedCAFlagRequiredError struct {
	Verb string
	Flag string
}

// Error implements the error interface.
func (e *TrustedCAFlagRequiredError) Error() string {
	return fmt.Sprintf("trusted-ca %s: --%s is required", e.Verb, e.Flag)
}

// TrustedCAArgRequiredError is returned when a trusted-ca verb is invoked
// without its positional argument.
type TrustedCAArgRequiredError struct {
	Verb string
	Arg  string
}

// Error implements the error interface.
func (e *TrustedCAArgRequiredError) Error() string {
	return fmt.Sprintf("trusted-ca %s: the <%s> argument is required", e.Verb, e.Arg)
}

// TrustedCAReadError wraps a filesystem error reading a verb's input file.
type TrustedCAReadError struct {
	Path string
	Err  error
}

// Error implements the error interface.
func (e *TrustedCAReadError) Error() string {
	return fmt.Sprintf("trusted-ca: read %s: %v", e.Path, e.Err)
}

// Unwrap exposes the underlying I/O error.
func (e *TrustedCAReadError) Unwrap() error { return e.Err }

// TrustedCAPEMError is returned when an input file holds no PEM block.
type TrustedCAPEMError struct {
	Path string
}

// Error implements the error interface.
func (e *TrustedCAPEMError) Error() string {
	return fmt.Sprintf("trusted-ca: %s holds no PEM block", e.Path)
}

// =============================================================================
// SDK factories
// =============================================================================

// TrustedCAClientFactory constructs the go-xkms CA service client.
type TrustedCAClientFactory func() (quicbaas.TrustedCAService, error)

// TCGCAClientFactory constructs the go-xkms TCG CA service client.
type TCGCAClientFactory func() (quicbaas.TCGCAService, error)

// trustedCAClientFactory resolves Security().TrustedCA(). Tests swap it.
var trustedCAClientFactory TrustedCAClientFactory = createSDKTrustedCAClient

// tcgCAClientFactory resolves Security().TCGCA(). Tests swap it.
var tcgCAClientFactory TCGCAClientFactory = createSDKTCGCAClient

// createSDKTrustedCAClient returns the go-xkms CA service bound to the active
// transport.
func createSDKTrustedCAClient() (quicbaas.TrustedCAService, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().TrustedCA(), nil
}

// createSDKTCGCAClient returns the go-xkms TCG CA service bound to the active
// transport.
func createSDKTCGCAClient() (quicbaas.TCGCAService, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().TCGCA(), nil
}

// =============================================================================
// Command tree
// =============================================================================

// newTrustedCACommand builds the `quicbaas trusted-ca` command tree.
func newTrustedCACommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trusted-ca",
		Short: "go-xkms certificate authority operations (CA and TCG CA)",
		Long: `Certificate authority operations against the active QuicBaaS daemon,
through the go-xkms CA re-exports.

Verbs:
  bundle        Fetch the CA certificate bundle
  certificate   Fetch the CA certificate
  sign-csr      Sign a PKCS#10 certificate signing request
  issue         Issue a certificate (the CA generates the key)
  revoke        Revoke a certificate by serial number
  is-revoked    Report whether a serial number is revoked
  crl           Generate the certificate revocation list
  tcg           TCG CA operations (EK/AK certificates, TCG-CSR, enrollment)

Every verb routes through Security().TrustedCA() (and the tcg verbs through
Security().TCGCA()) on the active SDK transport (rest, grpc, quic, mcp, unix,
embedded). The table output omits PEM and DER material; use --output json to
get it.`,
	}
	cmd.PersistentFlags().StringVar(&trustedCAOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newTrustedCABundleCommand())
	cmd.AddCommand(newTrustedCACertificateCommand())
	cmd.AddCommand(newTrustedCASignCSRCommand())
	cmd.AddCommand(newTrustedCAIssueCommand())
	cmd.AddCommand(newTrustedCARevokeCommand())
	cmd.AddCommand(newTrustedCAIsRevokedCommand())
	cmd.AddCommand(newTrustedCACRLCommand())
	cmd.AddCommand(newTrustedCATCGCommand())
	return cmd
}

func newTrustedCABundleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Fetch the CA certificate bundle",
		Args:  cobra.NoArgs,
		RunE:  runTrustedCABundle,
	}
	cmd.Flags().StringVar(&trustedCABundleStoreType, "store-type", "", "Filter by key store type (software, tpm2, pkcs11)")
	cmd.Flags().StringVar(&trustedCABundleAlgorithm, "algorithm", "", "Filter by key algorithm (RSA, ECDSA, Ed25519)")
	return cmd
}

func newTrustedCACertificateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "certificate",
		Short: "Fetch the CA certificate",
		Args:  cobra.NoArgs,
		RunE:  runTrustedCACertificate,
	}
	cmd.Flags().StringVar(&trustedCAIdentity, "identity", "", "CA identity (CN); empty for the default CA")
	return cmd
}

func newTrustedCASignCSRCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sign-csr",
		Short: "Sign a PEM-encoded PKCS#10 certificate signing request",
		Args:  cobra.NoArgs,
		RunE:  runTrustedCASignCSR,
	}
	cmd.Flags().StringVar(&trustedCACSRFile, "csr", "", "Path to the PEM-encoded CSR (required)")
	cmd.Flags().StringVar(&trustedCAProfile, "profile", "", "Certificate profile (server, client, idevid, ...)")
	cmd.Flags().IntVar(&trustedCAValidityDays, "validity-days", 0, "Validity in days (0 for the CA default)")
	return cmd
}

func newTrustedCAIssueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Issue a certificate; the CA generates the key pair",
		Args:  cobra.NoArgs,
		RunE:  runTrustedCAIssue,
	}
	cmd.Flags().StringVar(&trustedCACommonName, "common-name", "", "Subject common name (required)")
	cmd.Flags().StringVar(&trustedCAOrganization, "organization", "", "Subject organization")
	cmd.Flags().StringSliceVar(&trustedCADNSNames, "dns", nil, "DNS subject alternative name (repeatable)")
	cmd.Flags().StringVar(&trustedCAProfile, "profile", "", "Certificate profile (server, client, idevid, ...)")
	cmd.Flags().IntVar(&trustedCAValidityDays, "validity-days", 0, "Validity in days (0 for the CA default)")
	cmd.Flags().StringVar(&trustedCAAlgorithm, "algorithm", "", "Key algorithm (ecdsa-p256, rsa2048, ed25519)")
	return cmd
}

func newTrustedCARevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <serial>",
		Short: "Revoke a certificate by serial number",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runTrustedCARevoke,
	}
	cmd.Flags().IntVar(&trustedCARevokeReason, "reason", 0, "RFC 5280 revocation reason code")
	return cmd
}

func newTrustedCAIsRevokedCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "is-revoked <serial>",
		Short: "Report whether a certificate serial number is revoked",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runTrustedCAIsRevoked,
	}
}

func newTrustedCACRLCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "crl",
		Short: "Generate the certificate revocation list",
		Args:  cobra.NoArgs,
		RunE:  runTrustedCACRL,
	}
}

// newTrustedCATCGCommand builds the `quicbaas trusted-ca tcg` subtree.
func newTrustedCATCGCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tcg",
		Short: "TCG CA operations (EK/AK certificates, TCG-CSR signing, enrollment)",
		Long: `TCG CA operations through Security().TCGCA() on the active SDK transport.

Verbs:
  ek        Issue an Endorsement Key certificate for a PEM public key
  ak        Issue an Attestation Key certificate for a PEM public key
  sign-csr  Sign a packed TCG-CSR-IDEVID
  enroll    Enroll a device from a packed TCG-CSR-IDEVID

The server issues TCG certificates in the system tenant only.`,
	}
	cmd.AddCommand(newTrustedCATCGKeyCommand("ek", "Issue an Endorsement Key certificate", runTrustedCATCGEK))
	cmd.AddCommand(newTrustedCATCGKeyCommand("ak", "Issue an Attestation Key certificate", runTrustedCATCGAK))
	cmd.AddCommand(newTrustedCATCGCSRCommand("sign-csr", "Sign a packed TCG-CSR-IDEVID", runTrustedCATCGSignCSR))
	cmd.AddCommand(newTrustedCATCGCSRCommand("enroll", "Enroll a device from a packed TCG-CSR-IDEVID", runTrustedCATCGEnroll))
	return cmd
}

func addTrustedCATCGSubjectFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&trustedCATCGCommonName, "common-name", "", "Subject common name (required)")
	cmd.Flags().StringVar(&trustedCATCGOrganization, "organization", "", "Subject organization")
}

func newTrustedCATCGKeyCommand(use, short string, run func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: run}
	addTrustedCATCGSubjectFlags(cmd)
	cmd.Flags().StringVar(&trustedCATCGPublicKey, "public-key", "", "Path to the PEM-encoded public key (required)")
	return cmd
}

func newTrustedCATCGCSRCommand(use, short string, run func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: run}
	addTrustedCATCGSubjectFlags(cmd)
	cmd.Flags().StringVar(&trustedCATCGCSRFile, "tcg-csr", "", "Path to the packed TCG-CSR-IDEVID (required)")
	return cmd
}

// =============================================================================
// CAService verbs
// =============================================================================

func runTrustedCABundle(cmd *cobra.Command, _ []string) error {
	client, err := trustedCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GetCABundle(ctx, &xkmstransport.GetCABundleRequest{
		StoreType: trustedCABundleStoreType,
		Algorithm: trustedCABundleAlgorithm,
	})
	if err != nil {
		return formatError(err)
	}
	rows := [][2]string{
		{"Certificates", strconv.Itoa(len(resp.Certificates))},
		{"Content-Type", resp.ContentType},
	}
	for i, der := range resp.Certificates {
		subject, notAfter := trustedCACertSummaryDER(der)
		rows = append(rows, [2]string{fmt.Sprintf("Certificate %d", i+1), subject + " (not after " + notAfter + ")"})
	}
	return formatTrustedCA(resp, rows, trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCACertificate(cmd *cobra.Command, _ []string) error {
	client, err := trustedCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GetCACertificate(ctx, &xkmstransport.GetCACertificateRequest{Identity: trustedCAIdentity})
	if err != nil {
		return formatError(err)
	}
	rows := [][2]string{
		{"Subject", resp.Subject},
		{"Issuer", resp.Issuer},
		{"Serial", resp.SerialNumber},
		{"Not-Before", resp.NotBefore},
		{"Not-After", resp.NotAfter},
		{"Is-CA", strconv.FormatBool(resp.IsCA)},
	}
	return formatTrustedCA(resp, rows, trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCASignCSR(cmd *cobra.Command, _ []string) error {
	if trustedCACSRFile == "" {
		return &TrustedCAFlagRequiredError{Verb: "sign-csr", Flag: "csr"}
	}
	csrPEM, err := readTrustedCAFile(trustedCACSRFile)
	if err != nil {
		return err
	}
	client, err := trustedCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.SignCSR(ctx, &xkmstransport.SignCSRRequest{
		CSRPEM:       csrPEM,
		Profile:      trustedCAProfile,
		ValidityDays: trustedCAValidityDays,
	})
	if err != nil {
		return formatError(err)
	}
	return formatTrustedCA(resp, trustedCALeafRows(resp.SerialNumber, resp.CertificatePEM), trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCAIssue(cmd *cobra.Command, _ []string) error {
	if trustedCACommonName == "" {
		return &TrustedCAFlagRequiredError{Verb: "issue", Flag: "common-name"}
	}
	sans := make([]string, 0, len(trustedCADNSNames))
	for _, name := range trustedCADNSNames {
		sans = append(sans, "DNS:"+name)
	}
	client, err := trustedCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.IssueCertificate(ctx, &xkmstransport.IssueCertificateRequest{
		Profile:      trustedCAProfile,
		CommonName:   trustedCACommonName,
		Organization: trustedCAOrganization,
		SANs:         sans,
		ValidityDays: trustedCAValidityDays,
		Algorithm:    trustedCAAlgorithm,
	})
	if err != nil {
		return formatError(err)
	}
	rows := trustedCALeafRows(resp.SerialNumber, resp.CertificatePEM)
	rows = append(rows, [2]string{"Private-Key", strconv.FormatBool(len(resp.PrivateKeyPEM) > 0)})
	return formatTrustedCA(resp, rows, trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCARevoke(cmd *cobra.Command, args []string) error {
	if len(args) == 0 || args[0] == "" {
		return &TrustedCAArgRequiredError{Verb: "revoke", Arg: "serial"}
	}
	client, err := trustedCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RevokeCertificate(ctx, &xkmstransport.RevokeCertificateRequest{
		SerialNumber: args[0],
		Reason:       trustedCARevokeReason,
	})
	if err != nil {
		return formatError(err)
	}
	rows := [][2]string{
		{"Serial", args[0]},
		{"Success", strconv.FormatBool(resp.Success)},
		{"Message", resp.Message},
	}
	return formatTrustedCA(resp, rows, trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCAIsRevoked(cmd *cobra.Command, args []string) error {
	if len(args) == 0 || args[0] == "" {
		return &TrustedCAArgRequiredError{Verb: "is-revoked", Arg: "serial"}
	}
	client, err := trustedCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.IsRevoked(ctx, &xkmstransport.IsRevokedRequest{SerialNumber: args[0]})
	if err != nil {
		return formatError(err)
	}
	rows := [][2]string{
		{"Serial", args[0]},
		{"Revoked", strconv.FormatBool(resp.Revoked)},
		{"Reason", strconv.Itoa(resp.Reason)},
		{"Message", resp.Message},
	}
	return formatTrustedCA(resp, rows, trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCACRL(cmd *cobra.Command, _ []string) error {
	client, err := trustedCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GenerateCRL(ctx, &xkmstransport.GenerateCRLRequest{})
	if err != nil {
		return formatError(err)
	}
	rows := [][2]string{{"CRL-Bytes", strconv.Itoa(len(resp.CRLPEM))}}
	if block, _ := pem.Decode(resp.CRLPEM); block != nil {
		if crl, perr := x509.ParseRevocationList(block.Bytes); perr == nil {
			rows = append(rows,
				[2]string{"Revoked", strconv.Itoa(len(crl.RevokedCertificateEntries))},
				[2]string{"Next-Update", crl.NextUpdate.UTC().Format("2006-01-02T15:04:05Z")})
		}
	}
	return formatTrustedCA(resp, rows, trustedCAOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// TCGCAService verbs
// =============================================================================

// tcgKeyInput validates a tcg ek/ak invocation and returns the DER public key.
func tcgKeyInput(verb string) ([]byte, error) {
	if trustedCATCGCommonName == "" {
		return nil, &TrustedCAFlagRequiredError{Verb: "tcg " + verb, Flag: "common-name"}
	}
	if trustedCATCGPublicKey == "" {
		return nil, &TrustedCAFlagRequiredError{Verb: "tcg " + verb, Flag: "public-key"}
	}
	raw, err := readTrustedCAFile(trustedCATCGPublicKey)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, &TrustedCAPEMError{Path: trustedCATCGPublicKey}
	}
	return block.Bytes, nil
}

// tcgCSRInput validates a tcg sign-csr/enroll invocation and returns the packed
// TCG-CSR-IDEVID.
func tcgCSRInput(verb string) ([]byte, error) {
	if trustedCATCGCommonName == "" {
		return nil, &TrustedCAFlagRequiredError{Verb: "tcg " + verb, Flag: "common-name"}
	}
	if trustedCATCGCSRFile == "" {
		return nil, &TrustedCAFlagRequiredError{Verb: "tcg " + verb, Flag: "tcg-csr"}
	}
	return readTrustedCAFile(trustedCATCGCSRFile)
}

func runTrustedCATCGEK(cmd *cobra.Command, _ []string) error {
	der, err := tcgKeyInput("ek")
	if err != nil {
		return err
	}
	client, err := tcgCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.IssueEKCertificate(ctx, &xkmstransport.IssueEKCertificateRequest{
		CommonName:   trustedCATCGCommonName,
		Organization: trustedCATCGOrganization,
		EKPublicKey:  der,
	})
	if err != nil {
		return formatError(err)
	}
	return formatTrustedCA(resp, trustedCADERRows(resp.SerialNumber, resp.CertificateDER), trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCATCGAK(cmd *cobra.Command, _ []string) error {
	der, err := tcgKeyInput("ak")
	if err != nil {
		return err
	}
	client, err := tcgCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.IssueAKCertificate(ctx, &xkmstransport.IssueAKCertificateRequest{
		CommonName:   trustedCATCGCommonName,
		Organization: trustedCATCGOrganization,
		PublicKey:    der,
	})
	if err != nil {
		return formatError(err)
	}
	return formatTrustedCA(resp, trustedCADERRows(resp.SerialNumber, resp.CertificateDER), trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCATCGSignCSR(cmd *cobra.Command, _ []string) error {
	csr, err := tcgCSRInput("sign-csr")
	if err != nil {
		return err
	}
	client, err := tcgCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.SignTCGCSR(ctx, &xkmstransport.SignTCGCSRRequest{
		CommonName:   trustedCATCGCommonName,
		Organization: trustedCATCGOrganization,
		TCGCSR:       csr,
	})
	if err != nil {
		return formatError(err)
	}
	return formatTrustedCA(resp, trustedCAIdentityRows(resp.IAKCertDER, resp.IDevIDCertDER), trustedCAOutputFmt, cmd.OutOrStdout())
}

func runTrustedCATCGEnroll(cmd *cobra.Command, _ []string) error {
	csr, err := tcgCSRInput("enroll")
	if err != nil {
		return err
	}
	client, err := tcgCAClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.EnrollDevice(ctx, &xkmstransport.EnrollDeviceRequest{
		CommonName:   trustedCATCGCommonName,
		Organization: trustedCATCGOrganization,
		PackedCSR:    csr,
	})
	if err != nil {
		return formatError(err)
	}
	rows := trustedCAIdentityRows(resp.IAKCertDER, resp.IDevIDCertDER)
	rows = append(rows, [2]string{"Credential-Blob-Bytes", strconv.Itoa(len(resp.CredentialBlob))})
	return formatTrustedCA(resp, rows, trustedCAOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Input helpers
// =============================================================================

// readTrustedCAFile reads a verb's input file, wrapping any I/O error.
func readTrustedCAFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- operator-supplied input path for a CLI verb.
	if err != nil {
		return nil, &TrustedCAReadError{Path: path, Err: err}
	}
	return data, nil
}

// =============================================================================
// Formatting
// =============================================================================

// trustedCACertSummaryDER returns the subject and not-after of a DER
// certificate, or "-" for each when it does not parse.
func trustedCACertSummaryDER(der []byte) (string, string) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return "-", "-"
	}
	return cert.Subject.String(), cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z")
}

// trustedCALeafRows renders the scalar fields of a PEM leaf certificate.
func trustedCALeafRows(serial string, certPEM []byte) [][2]string {
	var der []byte
	if block, _ := pem.Decode(certPEM); block != nil {
		der = block.Bytes
	}
	return trustedCADERRows(serial, der)
}

// trustedCADERRows renders the scalar fields of a DER leaf certificate.
func trustedCADERRows(serial string, der []byte) [][2]string {
	subject, notAfter := trustedCACertSummaryDER(der)
	return [][2]string{
		{"Serial", serial},
		{"Subject", subject},
		{"Not-After", notAfter},
	}
}

// trustedCAIdentityRows renders the IAK and IDevID certificates of a TCG
// enrollment.
func trustedCAIdentityRows(iakDER, idevidDER []byte) [][2]string {
	iakSubject, iakNotAfter := trustedCACertSummaryDER(iakDER)
	idevidSubject, idevidNotAfter := trustedCACertSummaryDER(idevidDER)
	return [][2]string{
		{"IAK-Subject", iakSubject},
		{"IAK-Not-After", iakNotAfter},
		{"IDevID-Subject", idevidSubject},
		{"IDevID-Not-After", idevidNotAfter},
	}
}

// formatTrustedCA renders a go-xkms response: JSON and YAML marshal it as is,
// table prints the given scalar rows.
func formatTrustedCA(resp interface{}, rows [][2]string, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, row := range rows {
			fmt.Fprintf(tw, "%s:\t%s\n", row[0], row[1])
		}
		return tw.Flush()
	}
}
