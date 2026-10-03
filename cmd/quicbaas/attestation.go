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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// `quicbaas attestation` — the attestation Verifier (RFC 9334): endorsements,
// signed reference values and quote appraisal. Every verb routes through the
// active SDK transport and acts on the authenticated tenant.
// =============================================================================

// AttestationClientFactory creates attestation service clients.
type AttestationClientFactory func() (quicbaas.AttestationService, error)

// attestationClientFactory creates attestation clients. Defaults to
// createSDKAttestationClient.
var attestationClientFactory AttestationClientFactory = createSDKAttestationClient

// createSDKAttestationClient returns the active SDK client's attestation
// service.
func createSDKAttestationClient() (quicbaas.AttestationService, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().Attestation(), nil
}

var (
	attestationOutputFmt   string
	attestationKind        string
	attestationCertFile    string
	attestationKeyFile     string
	attestationCoRIMFile   string
	attestationAKCertFile  string
	attestationQuoteFile   string
	attestationSigFile     string
	attestationNonceHex    string
	attestationEventLog    string
	attestationRefValues   string
	attestationPCRs        []string
	attestationCoRIMOutput string
)

// InvalidPCRFlagError reports a --pcr value that is not index=hexdigest.
type InvalidPCRFlagError struct {
	Value string
}

// Error returns a human-readable description.
func (e *InvalidPCRFlagError) Error() string {
	return "attestation: --pcr " + strconv.Quote(e.Value) + " must be index=hexdigest"
}

// AttestationFileError reports an evidence or anchor file that cannot be read.
type AttestationFileError struct {
	Flag  string
	Cause error
}

// Error returns a human-readable description.
func (e *AttestationFileError) Error() string {
	return "attestation: --" + e.Flag + ": " + e.Cause.Error()
}

// Unwrap returns the read failure.
func (e *AttestationFileError) Unwrap() error { return e.Cause }

func newAttestationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attestation",
		Short: "Attestation Verifier: endorsements, signed reference values, quote appraisal",
		Long: `Manage what the attestation Verifier trusts and appraise TPM quote evidence.

Endorsements are trust anchors: an ak-root is the CA attestation-key
certificates must chain to; a reference-signer is the CA (or pinned public
key) signed reference values must verify under. Reference values are accepted
only as a signed CoRIM a stored reference-signer verifies — never trusted on
first use.

Examples:
  quicbaas attestation endorsement put ak-ca --kind ak-root --certificate ak-ca.pem
  quicbaas attestation endorsement put rvp --kind reference-signer --certificate rvp-ca.pem
  quicbaas attestation reference-values put golden --corim golden.corim
  quicbaas attestation appraise-quote --ak-cert ak.pem --quote quote.bin --signature quote.sig \
    --nonce 6e6f6e6365 --pcr 0=<hex> --pcr 7=<hex> --reference-values golden`,
	}
	cmd.PersistentFlags().StringVar(&attestationOutputFmt, "output", "table", "Output format (json, yaml, table)")
	cmd.AddCommand(newAttestationEndorsementCommand())
	cmd.AddCommand(newAttestationReferenceValuesCommand())
	cmd.AddCommand(newAttestationAppraiseQuoteCommand())
	return cmd
}

func newAttestationEndorsementCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "endorsement", Short: "Manage Verifier trust anchors"}

	put := &cobra.Command{
		Use:   "put <name>",
		Short: "Store (or replace) an endorsement",
		Args:  cobra.ExactArgs(1),
		RunE:  runAttestationEndorsementPut,
	}
	put.Flags().StringVar(&attestationKind, "kind", "", "ak-root or reference-signer (required)")
	put.Flags().StringVar(&attestationCertFile, "certificate", "", "PEM CA certificate file")
	put.Flags().StringVar(&attestationKeyFile, "public-key", "", "PEM public key file (reference-signer only)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List endorsements",
		Args:  cobra.NoArgs,
		RunE:  runAttestationEndorsementList,
	}
	list.Flags().StringVar(&attestationKind, "kind", "", "Only this kind")

	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an endorsement",
		Args:  cobra.ExactArgs(1),
		RunE:  runAttestationEndorsementDelete,
	}
	cmd.AddCommand(put, list, del)
	return cmd
}

func newAttestationReferenceValuesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "reference-values", Short: "Manage signed reference values"}

	put := &cobra.Command{
		Use:   "put <name>",
		Short: "Store a signed CoRIM a stored reference-signer verifies",
		Args:  cobra.ExactArgs(1),
		RunE:  runAttestationReferenceValuesPut,
	}
	put.Flags().StringVar(&attestationCoRIMFile, "corim", "", "COSE_Sign1 signed CoRIM file (required)")

	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Show stored reference values",
		Args:  cobra.ExactArgs(1),
		RunE:  runAttestationReferenceValuesGet,
	}
	get.Flags().StringVar(&attestationCoRIMOutput, "corim-out", "", "Write the signed CoRIM to this file")

	list := &cobra.Command{
		Use:   "list",
		Short: "List stored reference values",
		Args:  cobra.NoArgs,
		RunE:  runAttestationReferenceValuesList,
	}
	del := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete stored reference values",
		Args:  cobra.ExactArgs(1),
		RunE:  runAttestationReferenceValuesDelete,
	}
	cmd.AddCommand(put, get, list, del)
	return cmd
}

func newAttestationAppraiseQuoteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "appraise-quote",
		Short: "Appraise TPM quote evidence against stored, signed reference values",
		Args:  cobra.NoArgs,
		RunE:  runAttestationAppraiseQuote,
	}
	cmd.Flags().StringVar(&attestationAKCertFile, "ak-cert", "", "PEM attestation-key certificate (leaf first, then intermediates)")
	cmd.Flags().StringVar(&attestationQuoteFile, "quote", "", "TPMS_ATTEST file")
	cmd.Flags().StringVar(&attestationSigFile, "signature", "", "Quote signature file")
	cmd.Flags().StringVar(&attestationNonceHex, "nonce", "", "Qualifying data the quote must carry, hex")
	cmd.Flags().StringArrayVar(&attestationPCRs, "pcr", nil, "Quoted PCR value as index=hexdigest (repeatable)")
	cmd.Flags().StringVar(&attestationEventLog, "event-log", "", "TCG event log file (optional)")
	cmd.Flags().StringVar(&attestationRefValues, "reference-values", "", "Name of the stored reference values (required)")
	return cmd
}

// attestationCall runs fn with a client and the global timeout.
func attestationCall(fn func(ctx context.Context, svc quicbaas.AttestationService) error) error {
	svc, err := attestationClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	if err := fn(ctx, svc); err != nil {
		return formatError(err)
	}
	return nil
}

func runAttestationEndorsementPut(cmd *cobra.Command, args []string) error {
	cert, err := readOptionalFile("certificate", attestationCertFile)
	if err != nil {
		return formatError(err)
	}
	key, err := readOptionalFile("public-key", attestationKeyFile)
	if err != nil {
		return formatError(err)
	}
	return attestationCall(func(ctx context.Context, svc quicbaas.AttestationService) error {
		e, err := svc.PutEndorsement(ctx, &quicbaas.AttestationPutEndorsementRequest{Name: args[0], Kind: attestationKind, CertificatePEM: cert, PublicKeyPEM: key})
		if err != nil {
			return err
		}
		return writeAttestationOutput(cmd.OutOrStdout(), e, func(tw io.Writer) {
			fmt.Fprintf(tw, "Name:\t%s\nKind:\t%s\nCreated:\t%s\n", e.Name, e.Kind, e.CreatedAt)
		})
	})
}

func runAttestationEndorsementList(cmd *cobra.Command, _ []string) error {
	return attestationCall(func(ctx context.Context, svc quicbaas.AttestationService) error {
		resp, err := svc.ListEndorsements(ctx, attestationKind)
		if err != nil {
			return err
		}
		return writeAttestationOutput(cmd.OutOrStdout(), resp, func(tw io.Writer) {
			fmt.Fprintf(tw, "NAME\tKIND\tCREATED\n")
			for _, e := range resp.Endorsements {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, e.Kind, e.CreatedAt)
			}
		})
	})
}

func runAttestationEndorsementDelete(cmd *cobra.Command, args []string) error {
	return attestationCall(func(ctx context.Context, svc quicbaas.AttestationService) error {
		if err := svc.DeleteEndorsement(ctx, args[0]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Deleted endorsement %s\n", args[0])
		return err
	})
}

func runAttestationReferenceValuesPut(cmd *cobra.Command, args []string) error {
	corim, err := readRequiredFile("corim", attestationCoRIMFile)
	if err != nil {
		return formatError(err)
	}
	return attestationCall(func(ctx context.Context, svc quicbaas.AttestationService) error {
		rv, err := svc.PutReferenceValues(ctx, args[0], corim)
		if err != nil {
			return err
		}
		rv.SignedCoRIM = nil
		return writeAttestationOutput(cmd.OutOrStdout(), rv, func(tw io.Writer) { writeReferenceValuesTable(tw, rv) })
	})
}

func runAttestationReferenceValuesGet(cmd *cobra.Command, args []string) error {
	return attestationCall(func(ctx context.Context, svc quicbaas.AttestationService) error {
		rv, err := svc.GetReferenceValues(ctx, args[0])
		if err != nil {
			return err
		}
		if attestationCoRIMOutput != "" {
			if err := os.WriteFile(attestationCoRIMOutput, rv.SignedCoRIM, 0o600); err != nil {
				return &AttestationFileError{Flag: "corim-out", Cause: err}
			}
		}
		return writeAttestationOutput(cmd.OutOrStdout(), rv, func(tw io.Writer) { writeReferenceValuesTable(tw, rv) })
	})
}

func runAttestationReferenceValuesList(cmd *cobra.Command, _ []string) error {
	return attestationCall(func(ctx context.Context, svc quicbaas.AttestationService) error {
		resp, err := svc.ListReferenceValues(ctx)
		if err != nil {
			return err
		}
		return writeAttestationOutput(cmd.OutOrStdout(), resp, func(tw io.Writer) {
			fmt.Fprintf(tw, "NAME\tMANIFEST\tSIGNER\tPCRS\n")
			for _, rv := range resp.ReferenceValues {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", rv.Name, rv.ManifestID, rv.Signer, rv.PCRIndices)
			}
		})
	})
}

func runAttestationReferenceValuesDelete(cmd *cobra.Command, args []string) error {
	return attestationCall(func(ctx context.Context, svc quicbaas.AttestationService) error {
		if err := svc.DeleteReferenceValues(ctx, args[0]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Deleted reference values %s\n", args[0])
		return err
	})
}

func runAttestationAppraiseQuote(cmd *cobra.Command, _ []string) error {
	req, err := appraiseQuoteRequestFromFlags()
	if err != nil {
		return formatError(err)
	}
	return attestationCall(func(ctx context.Context, svc quicbaas.AttestationService) error {
		result, err := svc.AppraiseQuote(ctx, req)
		if err != nil {
			return err
		}
		return writeAttestationOutput(cmd.OutOrStdout(), result, func(tw io.Writer) {
			fmt.Fprintf(tw, "Verified:\t%t\nCryptographic failure:\t%t\nBaseline:\t%s %s (%s)\n",
				result.Verified, result.CryptographicFailure, result.BaselineSource, result.ReferenceValues, result.ManifestID)
			for _, check := range result.Checks {
				fmt.Fprintf(tw, "  %s:\t%s\t%s\n", check.Name, check.Outcome, check.Reason)
			}
		})
	})
}

// appraiseQuoteRequestFromFlags reads the evidence files and PCR flags.
func appraiseQuoteRequestFromFlags() (*quicbaas.AttestationAppraiseQuoteRequest, error) {
	req := &quicbaas.AttestationAppraiseQuoteRequest{ReferenceValues: attestationRefValues, PCRs: map[uint32][]byte{}}
	var err error
	if req.AKCertificatePEM, err = readRequiredFile("ak-cert", attestationAKCertFile); err != nil {
		return nil, err
	}
	if req.Quote, err = readRequiredFile("quote", attestationQuoteFile); err != nil {
		return nil, err
	}
	if req.Signature, err = readRequiredFile("signature", attestationSigFile); err != nil {
		return nil, err
	}
	if req.EventLog, err = readOptionalFile("event-log", attestationEventLog); err != nil {
		return nil, err
	}
	if req.Nonce, err = hex.DecodeString(attestationNonceHex); err != nil {
		return nil, &AttestationFileError{Flag: "nonce", Cause: err}
	}
	for _, value := range attestationPCRs {
		index, digest, ok := strings.Cut(value, "=")
		if !ok {
			return nil, &InvalidPCRFlagError{Value: value}
		}
		idx, err := strconv.ParseUint(index, 10, 32)
		if err != nil {
			return nil, &InvalidPCRFlagError{Value: value}
		}
		raw, err := hex.DecodeString(digest)
		if err != nil {
			return nil, &InvalidPCRFlagError{Value: value}
		}
		req.PCRs[uint32(idx)] = raw
	}
	return req, nil
}

func writeReferenceValuesTable(tw io.Writer, rv *quicbaas.AttestationReferenceValues) {
	fmt.Fprintf(tw, "Name:\t%s\nManifest:\t%s\nSigner:\t%s\nPCRs:\t%v\nStored:\t%s\n", rv.Name, rv.ManifestID, rv.Signer, rv.PCRIndices, rv.StoredAt)
}

// writeAttestationOutput renders a result in the chosen format.
func writeAttestationOutput(w io.Writer, v interface{}, table func(io.Writer)) error {
	format, err := cli.ParseOutputFormat(attestationOutputFmt)
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

// readOptionalFile reads a file named by a flag, or returns nil when unset.
func readOptionalFile(flag, path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator names the file
	if err != nil {
		return nil, &AttestationFileError{Flag: flag, Cause: err}
	}
	return data, nil
}

// readRequiredFile reads a file named by a required flag.
func readRequiredFile(flag, path string) ([]byte, error) {
	if path == "" {
		return nil, &AttestationFileError{Flag: flag, Cause: os.ErrNotExist}
	}
	return readOptionalFile(flag, path)
}
