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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// barrierQuorumDomains is the set of quorum domains go-qrdb accepts. The CLI
// rejects anything else before dialling so a typo never reaches the server.
var barrierQuorumDomains = map[string]struct{}{
	qrdbtransport.DomainUnseal:   {},
	qrdbtransport.DomainRecovery: {},
}

var (
	barrierUsername         string
	barrierQuorumOp         string
	barrierFactor           string
	barrierChallengeID      string
	barrierShareFile        string
	barrierResponseFile     string
	barrierResponseMetadata map[string]string
)

// validateBarrierQuorumDomain reports an invalid-input error for a domain
// go-qrdb does not define.
func validateBarrierQuorumDomain(op, domain string) error {
	if _, ok := barrierQuorumDomains[domain]; !ok {
		return errors.Newf(errors.ErrInvalidInput, op,
			"--domain must be %s or %s, got %q",
			qrdbtransport.DomainUnseal, qrdbtransport.DomainRecovery, domain)
	}
	return nil
}

// addBarrierDomainFlag binds --domain, defaulting to the unseal domain.
func addBarrierDomainFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&barrierDomain, "domain", qrdbtransport.DomainUnseal,
		"Quorum domain: unseal or recovery")
}

// =============================================================================
// rekey
// =============================================================================

// newBarrierRekeyCommand builds 'quicbaas barrier rekey'.
func newBarrierRekeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rekey",
		Short: "Open a quorum round that rotates a domain's key and share set",
		Long: `Open a rekey round on a quorum domain.

The round completes when --threshold of the current holders submit their
shares with 'barrier quorum submit --op rekey'. The server then generates a
fresh key and seals a new share set, with the new --threshold, to the holders
--policy selects; every previously distributed share is revoked. No secret
material travels on this request. --so-pin is required when a PINBackend is
configured.

Examples:
  quicbaas barrier rekey --threshold 3 --so-pin SO123
  quicbaas barrier rekey --domain recovery --threshold 2 --policy any-enrolled`,
		RunE: runBarrierRekeyCommand,
	}
	addBarrierDomainFlag(cmd)
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Threshold of the new share set (M)")
	cmd.Flags().StringVar(&barrierPolicy, "policy", "",
		"Factor policy (designated, any-enrolled; server default when empty)")
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (required when a PINBackend is configured)")
	return cmd
}

// runBarrierRekeyCommand executes 'barrier rekey'.
func runBarrierRekeyCommand(cmd *cobra.Command, _ []string) error {
	if err := validateBarrierQuorumDomain("barrier.rekey", barrierDomain); err != nil {
		return err
	}
	if barrierThreshold < 2 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.rekey", "--threshold must be >= 2")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	round, err := client.BarrierRekeyStart(ctx, &qrdbtransport.RekeyStartRequest{
		Domain:    barrierDomain,
		Threshold: barrierThreshold,
		Policy:    barrierPolicy,
		SOPin:     barrierSOPin,
	})
	if err != nil {
		return formatError(err)
	}
	return formatBarrierQuorumRound(round, barrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// officer-revoke
// =============================================================================

// newBarrierOfficerRevokeCommand builds 'quicbaas barrier officer-revoke'.
func newBarrierOfficerRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "officer-revoke",
		Short: "Open a quorum round that rekeys a domain without one holder",
		Long: `Open a round that removes --username from a quorum domain.

The round completes when the current holders submit their shares with
'barrier quorum submit --op rekey'; the new share set excludes the revoked
holder. --threshold sets the threshold of the remaining share set (0 keeps
the current one).

Example:
  quicbaas barrier officer-revoke --username alice --so-pin SO123`,
		RunE: runBarrierOfficerRevokeCommand,
	}
	addBarrierDomainFlag(cmd)
	cmd.Flags().StringVar(&barrierUsername, "username", "", "Holder to revoke")
	cmd.Flags().IntVar(&barrierThreshold, "threshold", 0,
		"Threshold of the remaining share set (0 keeps the current one)")
	cmd.Flags().StringVar(&barrierSOPin, "so-pin", "",
		"Security Officer PIN (required when a PINBackend is configured)")
	return cmd
}

// runBarrierOfficerRevokeCommand executes 'barrier officer-revoke'.
func runBarrierOfficerRevokeCommand(cmd *cobra.Command, _ []string) error {
	if err := validateBarrierQuorumDomain("barrier.officer-revoke", barrierDomain); err != nil {
		return err
	}
	if barrierUsername == "" {
		return errors.Newf(errors.ErrInvalidInput, "barrier.officer-revoke", "--username is required")
	}
	if barrierThreshold < 0 {
		return errors.Newf(errors.ErrInvalidInput, "barrier.officer-revoke", "--threshold must be >= 0")
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	round, err := client.BarrierOfficerRevoke(ctx, &qrdbtransport.OfficerRevokeRequest{
		Domain:    barrierDomain,
		Username:  barrierUsername,
		Threshold: barrierThreshold,
		SOPin:     barrierSOPin,
	})
	if err != nil {
		return formatError(err)
	}
	return formatBarrierQuorumRound(round, barrierOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// quorum
// =============================================================================

// newBarrierQuorumCommand builds the 'quicbaas barrier quorum' group.
func newBarrierQuorumCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quorum",
		Short: "Answer a holder challenge for a barrier quorum operation",
		Long: `Drive one holder's part of a quorum operation.

Every M-of-N operation (unseal, seal, root-token, rekey, recover) completes
through the same two steps per holder:
  quorum begin    Open a factor challenge for the holder
  quorum submit   Answer it with the factor response and the holder's share

The share is the holder's opened share, read raw from --share-file. Open
the envelope claimed through the share-claim flow with the holder's key
before submitting.`,
	}
	cmd.AddCommand(newBarrierQuorumBeginCommand())
	cmd.AddCommand(newBarrierQuorumSubmitCommand())
	return cmd
}

// addBarrierQuorumFlags binds the flags begin and submit share.
func addBarrierQuorumFlags(cmd *cobra.Command) {
	addBarrierDomainFlag(cmd)
	addQuorumHolderFlags(cmd)
}

// addQuorumHolderFlags binds the holder flags every quorum verb takes,
// system or tenant.
func addQuorumHolderFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&barrierQuorumOp, "op", "",
		"Operation: unseal, seal, root-token, rekey or recover")
	cmd.Flags().StringVar(&barrierUsername, "username", "", "Holder username")
}

// addQuorumBeginFlags binds the flags 'quorum begin' adds to the holder flags.
func addQuorumBeginFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&barrierFactor, "factor", "",
		"Factor to answer with (the designated factor when empty)")
}

// addQuorumSubmitFlags binds the flags 'quorum submit' adds to the holder flags.
func addQuorumSubmitFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&barrierChallengeID, "challenge-id", "",
		"Challenge ID from 'quorum begin'")
	cmd.Flags().StringVar(&barrierFactor, "factor", "", "Factor that answered --challenge-id")
	cmd.Flags().StringVar(&barrierResponseFile, "response-file", "",
		"Raw factor response to --challenge-id")
	cmd.Flags().StringToStringVar(&barrierResponseMetadata, "response-metadata", nil,
		"Factor response metadata (key=value, repeatable)")
	cmd.Flags().StringVar(&barrierShareFile, "share-file", "", "The holder's opened share, raw")
}

// validateBarrierQuorumFlags checks the flags begin and submit share.
func validateBarrierQuorumFlags(op string) error {
	if err := validateBarrierQuorumDomain(op, barrierDomain); err != nil {
		return err
	}
	return validateQuorumHolderFlags(op)
}

// validateQuorumHolderFlags checks the holder flags every quorum verb takes.
func validateQuorumHolderFlags(op string) error {
	if barrierQuorumOp == "" {
		return errors.Newf(errors.ErrInvalidInput, op, "--op is required")
	}
	if barrierUsername == "" {
		return errors.Newf(errors.ErrInvalidInput, op, "--username is required")
	}
	return nil
}

// newBarrierQuorumBeginCommand builds 'barrier quorum begin'.
func newBarrierQuorumBeginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "begin",
		Short: "Open a factor challenge for one holder",
		Long: `Open a factor challenge for one holder of a quorum operation.

The challenge ID, nonce and method data are printed; answer them with the
factor's own tool and pass the result to 'barrier quorum submit'.

Example:
  quicbaas barrier quorum begin --op unseal --username alice --factor mtls`,
		RunE: runBarrierQuorumBeginCommand,
	}
	addBarrierQuorumFlags(cmd)
	addQuorumBeginFlags(cmd)
	return cmd
}

// runBarrierQuorumBeginCommand executes 'barrier quorum begin'.
func runBarrierQuorumBeginCommand(cmd *cobra.Command, _ []string) error {
	if err := validateBarrierQuorumFlags("barrier.quorum.begin"); err != nil {
		return err
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	challenge, err := client.BarrierQuorumBegin(ctx, &qrdbtransport.QuorumBeginRequest{
		Domain:   barrierDomain,
		Op:       barrierQuorumOp,
		Username: barrierUsername,
		Factor:   barrierFactor,
	})
	if err != nil {
		return formatError(err)
	}
	return formatBarrierQuorumChallenge(challenge, barrierOutputFmt, cmd.OutOrStdout())
}

// newBarrierQuorumSubmitCommand builds 'barrier quorum submit'.
func newBarrierQuorumSubmitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit one holder's share to the open quorum round",
		Long: `Submit one holder's share, authenticated by a factor response.

--challenge-id, --factor and --response-file carry the answer to the
challenge 'barrier quorum begin' opened. A completed root-token round
prints the root token exactly once; it is never recoverable afterwards.

Example:
  quicbaas barrier quorum submit --op unseal --username alice \
      --challenge-id C1 --factor mtls --response-file sig.bin --share-file share.bin`,
		RunE: runBarrierQuorumSubmitCommand,
	}
	addBarrierQuorumFlags(cmd)
	addQuorumSubmitFlags(cmd)
	return cmd
}

// runBarrierQuorumSubmitCommand executes 'barrier quorum submit'.
func runBarrierQuorumSubmitCommand(cmd *cobra.Command, _ []string) error {
	const op = "barrier.quorum.submit"
	if err := validateBarrierQuorumDomain(op, barrierDomain); err != nil {
		return err
	}
	response, share, err := readQuorumSubmission(op)
	if err != nil {
		return err
	}
	defer clear(share)

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	outcome, err := client.BarrierQuorumSubmit(ctx, &qrdbtransport.QuorumSubmitRequest{
		Domain:           barrierDomain,
		Op:               barrierQuorumOp,
		Username:         barrierUsername,
		ChallengeID:      barrierChallengeID,
		Factor:           barrierFactor,
		Response:         response,
		ResponseMetadata: barrierResponseMetadata,
		Share:            share,
	})
	if err != nil {
		return formatError(err)
	}
	return formatBarrierQuorumOutcome(outcome, barrierOutputFmt, cmd.OutOrStdout())
}

// readQuorumSubmission validates the holder and submit flags and reads the
// factor response and the share they name. The caller clears the share.
func readQuorumSubmission(op string) (response, share []byte, err error) {
	if err := validateQuorumHolderFlags(op); err != nil {
		return nil, nil, err
	}
	if barrierChallengeID == "" || barrierFactor == "" || barrierResponseFile == "" {
		return nil, nil, errors.Newf(errors.ErrInvalidInput, op,
			"--challenge-id, --factor and --response-file are required")
	}
	if barrierShareFile == "" {
		return nil, nil, errors.Newf(errors.ErrInvalidInput, op, "--share-file is required")
	}
	response, err = readBarrierQuorumFile(op, "--response-file", barrierResponseFile)
	if err != nil {
		return nil, nil, err
	}
	share, err = readBarrierQuorumFile(op, "--share-file", barrierShareFile)
	if err != nil {
		return nil, nil, err
	}
	return response, share, nil
}

// readBarrierQuorumFile reads a non-empty file named by flag.
func readBarrierQuorumFile(op, flag, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInvalidInput, op, "failed to read %s %s", flag, path)
	}
	if len(data) == 0 {
		return nil, errors.Newf(errors.ErrInvalidInput, op, "%s %s is empty", flag, path)
	}
	return data, nil
}

// =============================================================================
// formatters
// =============================================================================

// encodeBarrierJSON writes v as indented JSON. The qrdb transport types carry
// their own wire tags, so JSON output matches the server's wire shape.
func encodeBarrierJSON(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// formatBarrierQuorumRound renders a rekey or officer-revoke round.
func formatBarrierQuorumRound(round *qrdbtransport.QuorumRound, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFormat {
	case cli.OutputFormatJSON:
		return encodeBarrierJSON(w, round)
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "QUORUM ROUND OPENED\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "Domain:\t%s\n", round.Domain)
		fmt.Fprintf(tw, "Operation:\t%s\n", round.Op)
		fmt.Fprintf(tw, "Submitted:\t%d of %d\n", round.Submitted, round.Required)
		if round.NewThreshold > 0 {
			fmt.Fprintf(tw, "New Threshold:\t%d\n", round.NewThreshold)
		}
		if round.Policy != "" {
			fmt.Fprintf(tw, "Policy:\t%s\n", round.Policy)
		}
		if len(round.Remove) > 0 {
			fmt.Fprintf(tw, "Removing:\t%s\n", strings.Join(round.Remove, ", "))
		}
		fmt.Fprintf(tw, "Expires At:\t%s\n", round.ExpiresAt.Format(time.RFC3339))
		return tw.Flush()
	default:
		fmt.Fprintf(w, "%s round on %s: %d of %d submitted\n",
			round.Op, round.Domain, round.Submitted, round.Required)
		return nil
	}
}

// formatBarrierQuorumChallenge renders a holder's factor challenge.
func formatBarrierQuorumChallenge(challenge *qrdbtransport.QuorumChallenge, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFormat {
	case cli.OutputFormatJSON:
		return encodeBarrierJSON(w, challenge)
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "QUORUM CHALLENGE\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "Challenge ID:\t%s\n", challenge.ChallengeID)
		fmt.Fprintf(tw, "Domain:\t%s\n", challenge.Domain)
		fmt.Fprintf(tw, "Operation:\t%s\n", challenge.Op)
		fmt.Fprintf(tw, "Username:\t%s\n", challenge.Username)
		fmt.Fprintf(tw, "Factor:\t%s\n", challenge.Factor)
		fmt.Fprintf(tw, "Nonce:\t%x\n", challenge.Nonce)
		for key, value := range challenge.MethodData {
			fmt.Fprintf(tw, "Method Data %s:\t%s\n", key, value)
		}
		fmt.Fprintf(tw, "Expires At:\t%s\n", challenge.ExpiresAt.Format(time.RFC3339))
		return tw.Flush()
	default:
		fmt.Fprintf(w, "Challenge ID: %s\n", challenge.ChallengeID)
		fmt.Fprintf(w, "Nonce: %x\n", challenge.Nonce)
		return nil
	}
}

// formatBarrierQuorumOutcome renders the result of one submission. A root
// token appears only when a root-token round completed.
func formatBarrierQuorumOutcome(outcome *qrdbtransport.QuorumOutcome, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFormat {
	case cli.OutputFormatJSON:
		return encodeBarrierJSON(w, outcome)
	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "QUORUM SUBMISSION\n")
		fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
		fmt.Fprintf(tw, "Domain:\t%s\n", outcome.Domain)
		fmt.Fprintf(tw, "Operation:\t%s\n", outcome.Op)
		fmt.Fprintf(tw, "Submitted:\t%d of %d\n", outcome.Submitted, outcome.Required)
		fmt.Fprintf(tw, "Complete:\t%t\n", outcome.Complete)
		if outcome.ShareCount > 0 {
			fmt.Fprintf(tw, "New Share Set:\t%d of %d (%d sealed)\n",
				outcome.Threshold, outcome.TotalShares, outcome.ShareCount)
		}
		if outcome.RootToken != "" {
			fmt.Fprintf(tw, "Root Token:\t%s\n", outcome.RootToken)
			fmt.Fprintf(tw, "Created At:\t%s\n", outcome.RootTokenCreatedAt.Format(time.RFC3339))
			fmt.Fprintf(tw, "\nThe root token is shown once and cannot be recovered.\n")
		}
		return tw.Flush()
	default:
		fmt.Fprintf(w, "%s on %s: %d of %d submitted, complete=%t\n",
			outcome.Op, outcome.Domain, outcome.Submitted, outcome.Required, outcome.Complete)
		if outcome.RootToken != "" {
			fmt.Fprintf(w, "Root Token: %s\n", outcome.RootToken)
		}
		return nil
	}
}
