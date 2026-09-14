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

// Package main provides credential CLI verbs for QuicBaaS.
//
// The credential commands delegate to Security().Credentials() over
// the active SDK transport (rest / grpc / quic / mcp / unix / embedded
// — every transport ships a real-wire implementation; there is no
// PendingCredentialBackendError or NotImplementedError anywhere on
// this path). The verb surface is the canonical surface per
// docs/08-bootstrap-auth-architecture.md §6:
//
//	credential submit   <name> --value <v>            non-versioned set/replace
//	credential list                                   names only (never values)
//	credential rotate   <name> --value <v> [--grace]  versioned rotation w/ grace
//	credential retrieve <name> [--version N]          read current or specific version
//	credential versions <name>                        metadata for current + non-expired grace
//	credential delete   <name>                        wipe credential + all historical versions
//	credential gc       <name>                        reap expired grace versions
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// Per-command flags
// =============================================================================

var (
	credentialValue       string
	credentialGracePeriod time.Duration
	credentialVersion     uint64
	credentialOutputFmt   string
)

// =============================================================================
// Errors
// =============================================================================

// CredentialNameRequiredError is returned when a credential verb is
// invoked without the required <name> positional argument.
type CredentialNameRequiredError struct{ Verb string }

// Error implements error.
func (e *CredentialNameRequiredError) Error() string {
	return "credential " + e.Verb + ": <name> argument is required"
}

// CredentialValueRequiredError is returned when submit or rotate is
// invoked without --value (and without piped stdin support — that is
// reserved for a future revision).
type CredentialValueRequiredError struct{ Verb string }

// Error implements error.
func (e *CredentialValueRequiredError) Error() string {
	return "credential " + e.Verb + ": --value is required"
}

// =============================================================================
// SDK factory
// =============================================================================

// credentialClientFactory is the indirection point CLI tests use to
// swap in a fake client. Production code returns the SDK-backed
// CredentialClient bound to the active transport.
var credentialClientFactory = createSDKCredentialClient

// createSDKCredentialClient returns the SDK CredentialClient. The SDK
// chooses the active transport (grpc / rest / quic / unix / mcp /
// embedded) per the global --transport flag.
func createSDKCredentialClient() (qrdbtransport.CredentialClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().Credentials()
}

// =============================================================================
// submit
// =============================================================================

// newCredentialSubmitCmd builds 'quicbaas credential submit <name>'.
func newCredentialSubmitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit <name>",
		Short: "Submit a named credential value (non-versioned)",
		Long: `Submit a credential value under the given name.

The manual strategy notifies any blocked retrievers; the barrier
strategy persists the value via the StorageBarrier. For a versioned
rotation that keeps the previous value retrievable, use
'quicbaas credential rotate'.

Example:
  quicbaas credential submit my-key --value s3cr3t`,
		Args: cobra.ExactArgs(1),
		RunE: runCredentialSubmit,
	}
	cmd.Flags().StringVar(&credentialValue, "value", "", "Credential value (required)")
	return cmd
}

func runCredentialSubmit(cmd *cobra.Command, args []string) error {
	name := args[0]
	if strings.TrimSpace(name) == "" {
		return formatError(&CredentialNameRequiredError{Verb: "submit"})
	}
	if credentialValue == "" {
		return formatError(&CredentialValueRequiredError{Verb: "submit"})
	}
	client, err := credentialClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	if err := client.CredentialSubmit(ctx, &qrdbtransport.CredentialSubmitRequest{
		Name:  name,
		Value: []byte(credentialValue),
	}); err != nil {
		return formatError(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "credential %q submitted\n", name)
	return nil
}

// =============================================================================
// list
// =============================================================================

// newCredentialListCmd builds 'quicbaas credential list'.
func newCredentialListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the names of every stored credential (names only)",
		Long: `Enumerate credential names. Values are never returned.

Backends that cannot enumerate (future HSM-only strategies) return
an enumeration-not-supported error.`,
		RunE: runCredentialList,
	}
	cmd.Flags().StringVar(&credentialOutputFmt, "output", "table", "Output format (json, table)")
	return cmd
}

func runCredentialList(cmd *cobra.Command, _ []string) error {
	client, err := credentialClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	resp, err := client.CredentialList(ctx, &qrdbtransport.CredentialListRequest{})
	if err != nil {
		return formatError(err)
	}
	return writeCredentialNames(cmd.OutOrStdout(), resp.Names, credentialOutputFmt)
}

// =============================================================================
// rotate
// =============================================================================

// newCredentialRotateCmd builds 'quicbaas credential rotate <name>'.
func newCredentialRotateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate <name>",
		Short: "Versioned rotation: atomically replace the current value (with optional grace)",
		Long: `Replace the current value of <name> with --value.

By default the previous value is wiped immediately. Use --grace to
keep it retrievable via 'credential retrieve --version N' for a
grace window before automatic reaping.

Example:
  quicbaas credential rotate my-key --value newSecret --grace 24h`,
		Args: cobra.ExactArgs(1),
		RunE: runCredentialRotate,
	}
	cmd.Flags().StringVar(&credentialValue, "value", "", "New credential value (required)")
	cmd.Flags().DurationVar(&credentialGracePeriod, "grace", 0, "Grace period during which the previous version remains retrievable (default: 0 — wipe immediately)")
	cmd.Flags().StringVar(&credentialOutputFmt, "output", "table", "Output format (json, table)")
	return cmd
}

func runCredentialRotate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if strings.TrimSpace(name) == "" {
		return formatError(&CredentialNameRequiredError{Verb: "rotate"})
	}
	if credentialValue == "" {
		return formatError(&CredentialValueRequiredError{Verb: "rotate"})
	}
	client, err := credentialClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	resp, err := client.CredentialRotate(ctx, &qrdbtransport.CredentialRotateRequest{
		Name:               name,
		NewValue:           []byte(credentialValue),
		GracePeriodSeconds: int64(credentialGracePeriod / time.Second),
	})
	if err != nil {
		return formatError(err)
	}
	return writeCredentialRotateResult(cmd.OutOrStdout(), resp, credentialOutputFmt)
}

// =============================================================================
// retrieve
// =============================================================================

// newCredentialRetrieveCmd builds 'quicbaas credential retrieve <name>'.
func newCredentialRetrieveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retrieve <name>",
		Short: "Read a credential value (current version, or specific via --version)",
		Long: `Print the credential value at the requested version.

Defaults to the current version when --version is omitted or 0.
Historical versions remain retrievable while their grace period
has not elapsed.

Example:
  quicbaas credential retrieve my-key             # current
  quicbaas credential retrieve my-key --version 2 # historical`,
		Args: cobra.ExactArgs(1),
		RunE: runCredentialRetrieve,
	}
	cmd.Flags().Uint64Var(&credentialVersion, "version", 0, "Specific version to retrieve (0 = current)")
	return cmd
}

func runCredentialRetrieve(cmd *cobra.Command, args []string) error {
	name := args[0]
	if strings.TrimSpace(name) == "" {
		return formatError(&CredentialNameRequiredError{Verb: "retrieve"})
	}
	client, err := credentialClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	resp, err := client.CredentialRetrieveVersion(ctx, &qrdbtransport.CredentialRetrieveVersionRequest{
		Name:    name,
		Version: credentialVersion,
	})
	if err != nil {
		return formatError(err)
	}
	// %s, not the default []byte verb: qrdb models a credential value as raw
	// bytes so binary material round-trips, and Fprintln would render that as
	// a decimal byte list ("[99 117 ...]") instead of the value. The trailing
	// newline is the documented pipeline contract (docs/cli/credential.md).
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", resp.Value)
	return nil
}

// =============================================================================
// versions
// =============================================================================

// newCredentialVersionsCmd builds 'quicbaas credential versions <name>'.
func newCredentialVersionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions <name>",
		Short: "Show version metadata for a credential (current + non-expired grace)",
		Long: `Display every non-expired version of <name>.

Output is metadata only — no values. Use 'credential retrieve
--version N' to read a specific value.`,
		Args: cobra.ExactArgs(1),
		RunE: runCredentialVersions,
	}
	cmd.Flags().StringVar(&credentialOutputFmt, "output", "table", "Output format (json, table)")
	return cmd
}

func runCredentialVersions(cmd *cobra.Command, args []string) error {
	name := args[0]
	if strings.TrimSpace(name) == "" {
		return formatError(&CredentialNameRequiredError{Verb: "versions"})
	}
	client, err := credentialClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	resp, err := client.CredentialListVersions(ctx, &qrdbtransport.CredentialListVersionsRequest{Name: name})
	if err != nil {
		return formatError(err)
	}
	return writeCredentialVersions(cmd.OutOrStdout(), resp, credentialOutputFmt)
}

// =============================================================================
// delete
// =============================================================================

// newCredentialDeleteCmd builds 'quicbaas credential delete <name>'.
func newCredentialDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a credential and all of its non-current historical versions",
		Args:  cobra.ExactArgs(1),
		RunE:  runCredentialDelete,
	}
	return cmd
}

func runCredentialDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	if strings.TrimSpace(name) == "" {
		return formatError(&CredentialNameRequiredError{Verb: "delete"})
	}
	client, err := credentialClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	if err := client.CredentialDelete(ctx, name); err != nil {
		return formatError(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "credential %q deleted\n", name)
	return nil
}

// =============================================================================
// gc
// =============================================================================

// newCredentialGCCmd builds 'quicbaas credential gc <name>'.
func newCredentialGCCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gc <name>",
		Short: "Reap historical versions whose grace period has elapsed",
		Args:  cobra.ExactArgs(1),
		RunE:  runCredentialGC,
	}
	return cmd
}

func runCredentialGC(cmd *cobra.Command, args []string) error {
	name := args[0]
	if strings.TrimSpace(name) == "" {
		return formatError(&CredentialNameRequiredError{Verb: "gc"})
	}
	client, err := credentialClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	resp, err := client.CredentialGCExpired(ctx, &qrdbtransport.CredentialGCRequest{Name: name})
	if err != nil {
		return formatError(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "reaped %d expired version(s)\n", resp.Reaped)
	return nil
}

// =============================================================================
// Output formatting
// =============================================================================

func writeCredentialNames(w stringWriter, names []string, format string) error {
	if format == "json" {
		out, err := json.MarshalIndent(map[string][]string{"names": names}, "", "  ")
		if err != nil {
			return formatError(err)
		}
		fmt.Fprintln(w, string(out))
		return nil
	}
	if len(names) == 0 {
		fmt.Fprintln(w, "(no credentials)")
		return nil
	}
	for _, n := range names {
		fmt.Fprintln(w, n)
	}
	return nil
}

func writeCredentialRotateResult(w stringWriter, resp *qrdbtransport.CredentialRotateResponse, format string) error {
	if format == "json" {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return formatError(err)
		}
		fmt.Fprintln(w, string(out))
		return nil
	}
	fmt.Fprintf(w, "rotated %s: previous v%d -> new v%d", resp.Name, resp.PreviousVersion, resp.NewVersion)
	if resp.GraceUntilUnix > 0 {
		fmt.Fprintf(w, " (previous version retrievable until %s)", time.Unix(resp.GraceUntilUnix, 0).Format(time.RFC3339))
	}
	fmt.Fprintln(w)
	return nil
}

func writeCredentialVersions(w stringWriter, resp *qrdbtransport.CredentialListVersionsResponse, format string) error {
	if format == "json" {
		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return formatError(err)
		}
		fmt.Fprintln(w, string(out))
		return nil
	}
	if len(resp.Versions) == 0 {
		fmt.Fprintln(w, "(no versions)")
		return nil
	}
	fmt.Fprintf(w, "%-10s %-9s %-25s %-25s\n", "VERSION", "CURRENT", "CREATED", "GRACE-UNTIL")
	for _, v := range resp.Versions {
		current := "no"
		if v.IsCurrent {
			current = "yes"
		}
		created := "-"
		if v.CreatedAtUnix > 0 {
			created = time.Unix(v.CreatedAtUnix, 0).Format(time.RFC3339)
		}
		grace := "-"
		if v.GraceUntilUnix > 0 {
			grace = time.Unix(v.GraceUntilUnix, 0).Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%-10s %-9s %-25s %-25s\n", strconv.FormatUint(v.Version, 10), current, created, grace)
	}
	return nil
}

// stringWriter is a tiny shim so command output can be redirected in
// tests without dragging in io.Writer's full surface.
type stringWriter interface {
	Write(p []byte) (int, error)
}
