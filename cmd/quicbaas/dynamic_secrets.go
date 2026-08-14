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

// dynamic_secrets.go implements the `quicbaas dynamic-secrets` CLI verb tree.
//
// Every verb routes through Security().DynamicSecrets()
// (sdk/go/client/dynamicsecrets). The verb set mirrors that SDK client
// interface exactly — four role verbs, one generation verb, three lease verbs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
)

// =============================================================================
// Flags
// =============================================================================

var (
	dynSecretsOutputFmt string

	// role create.
	dynSecretsGeneratorType string
	dynSecretsConfig        []string
	dynSecretsDefaultTTL    time.Duration
	dynSecretsMaxTTL        time.Duration

	// role list / lease list.
	dynSecretsPrefix string

	// generate.
	dynSecretsTTL time.Duration

	// lease renew.
	dynSecretsIncrement time.Duration

	// generate output control.
	dynSecretsRevealSecret bool
)

// =============================================================================
// Typed errors
// =============================================================================

// DynamicSecretsArgRequiredError is returned when a verb is missing its
// positional argument.
type DynamicSecretsArgRequiredError struct {
	Verb string
	Arg  string
}

// Error implements the error interface.
func (e *DynamicSecretsArgRequiredError) Error() string {
	return "dynamic-secrets " + e.Verb + ": <" + e.Arg + "> argument is required"
}

// DynamicSecretsFlagInvalidError is returned when a flag carries an unusable
// value.
type DynamicSecretsFlagInvalidError struct {
	Verb   string
	Flag   string
	Reason string
}

// Error implements the error interface.
func (e *DynamicSecretsFlagInvalidError) Error() string {
	return "dynamic-secrets " + e.Verb + ": --" + e.Flag + ": " + e.Reason
}

// DynamicSecretsConfigFormatError is returned when --config is not in
// "key=value" form.
type DynamicSecretsConfigFormatError struct {
	Value string
}

// Error implements the error interface.
func (e *DynamicSecretsConfigFormatError) Error() string {
	return "dynamic-secrets role create: invalid --config " + e.Value +
		` (expected "key=value")`
}

// =============================================================================
// SDK factory
// =============================================================================

// DynamicSecretsClientFactory constructs the SDK dynamic secrets client.
type DynamicSecretsClientFactory func() (quicbaas.DynamicSecretsClient, error)

// dynamicSecretsClientFactory resolves Security().DynamicSecrets(). Tests
// swap it.
var dynamicSecretsClientFactory DynamicSecretsClientFactory = createSDKDynamicSecretsClient

// createSDKDynamicSecretsClient returns the SDK dynamic secrets client bound
// to the active transport.
func createSDKDynamicSecretsClient() (quicbaas.DynamicSecretsClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().DynamicSecrets(), nil
}

// =============================================================================
// Command tree
// =============================================================================

// newDynamicSecretsCommand builds the `quicbaas dynamic-secrets` command tree.
func newDynamicSecretsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dynamic-secrets",
		Aliases: []string{"dynsec"},
		Short:   "Dynamic secrets: credential-generation roles and leases",
		Long: `Dynamic secrets management against the active QuicBaaS daemon.

A role is a template that a generator uses to mint short-lived credentials.
Every generated credential carries a lease that can be renewed or revoked.

Verbs:
  role create    Create a credential-generation role
  role get       Show a role
  role list      List roles
  role delete    Remove a role
  generate       Generate a credential from a role
  lease renew    Extend a credential lease
  lease revoke   Revoke a credential lease immediately
  lease list     List active leases

All verbs route through Security().DynamicSecrets() on the active SDK
transport.

SECURITY: 'generate' returns live credential material. It is redacted by
default; pass --reveal to print it, and prefer '--output json' piped into a
consumer over leaving secrets in a terminal scrollback.`,
	}
	cmd.PersistentFlags().StringVar(&dynSecretsOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newDynamicSecretsRoleCommand())
	cmd.AddCommand(newDynamicSecretsGenerateCommand())
	cmd.AddCommand(newDynamicSecretsLeaseCommand())
	return cmd
}

// =============================================================================
// role
// =============================================================================

func newDynamicSecretsRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "Credential-generation role management",
	}
	cmd.AddCommand(newDynamicSecretsRoleCreateCommand())
	cmd.AddCommand(newDynamicSecretsRoleGetCommand())
	cmd.AddCommand(newDynamicSecretsRoleListCommand())
	cmd.AddCommand(newDynamicSecretsRoleDeleteCommand())
	return cmd
}

func newDynamicSecretsRoleCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a credential-generation role",
		Long: `Create a role that a generator uses to mint credentials.

--config is repeatable and takes "key=value" pairs passed verbatim to the
named generator.

Examples:
  quicbaas dynamic-secrets role create readonly-db \
    --generator-type postgres --default-ttl 1h --max-ttl 24h \
    --config host=db.internal --config role=readonly`,
		Args: cobra.ExactArgs(1),
		RunE: runDynamicSecretsRoleCreate,
	}
	cmd.Flags().StringVar(&dynSecretsGeneratorType, "generator-type", "", "Credential generator to use (required)")
	cmd.Flags().StringArrayVar(&dynSecretsConfig, "config", nil, `Generator config as "key=value" (repeatable)`)
	cmd.Flags().DurationVar(&dynSecretsDefaultTTL, "default-ttl", 0, "Default credential lifetime (required, e.g. 1h)")
	cmd.Flags().DurationVar(&dynSecretsMaxTTL, "max-ttl", 0, "Maximum credential lifetime (required, e.g. 24h)")
	return cmd
}

func runDynamicSecretsRoleCreate(cmd *cobra.Command, args []string) error {
	name, err := dynamicSecretsArg("role create", "name", args)
	if err != nil {
		return formatError(err)
	}
	if strings.TrimSpace(dynSecretsGeneratorType) == "" {
		return formatError(&DynamicSecretsFlagInvalidError{
			Verb: "role create", Flag: "generator-type", Reason: "is required",
		})
	}
	if dynSecretsDefaultTTL <= 0 {
		return formatError(&DynamicSecretsFlagInvalidError{
			Verb: "role create", Flag: "default-ttl", Reason: "must be a positive duration",
		})
	}
	if dynSecretsMaxTTL <= 0 {
		return formatError(&DynamicSecretsFlagInvalidError{
			Verb: "role create", Flag: "max-ttl", Reason: "must be a positive duration",
		})
	}
	if dynSecretsDefaultTTL > dynSecretsMaxTTL {
		return formatError(&DynamicSecretsFlagInvalidError{
			Verb: "role create", Flag: "default-ttl", Reason: "cannot exceed --max-ttl",
		})
	}
	config, err := parseDynamicSecretsConfig(dynSecretsConfig)
	if err != nil {
		return formatError(err)
	}

	client, err := dynamicSecretsClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.CreateRole(ctx, &dsclient.CreateRoleRequest{
		Name:          name,
		GeneratorType: dynSecretsGeneratorType,
		Config:        config,
		DefaultTTL:    dynSecretsDefaultTTL,
		MaxTTL:        dynSecretsMaxTTL,
	})
	if err != nil {
		return formatError(err)
	}
	var role *dsclient.Role
	if resp != nil {
		role = resp.Role
	}
	return formatDynamicSecretsRole(role, dynSecretsOutputFmt, cmd.OutOrStdout())
}

func newDynamicSecretsRoleGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Show a credential-generation role",
		Long: `Retrieve a single role's definition.

Examples:
  quicbaas dynamic-secrets role get readonly-db
  quicbaas dynamic-secrets role get readonly-db --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runDynamicSecretsRoleGet,
	}
}

func runDynamicSecretsRoleGet(cmd *cobra.Command, args []string) error {
	name, err := dynamicSecretsArg("role get", "name", args)
	if err != nil {
		return formatError(err)
	}

	client, err := dynamicSecretsClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GetRole(ctx, &dsclient.GetRoleRequest{Name: name})
	if err != nil {
		return formatError(err)
	}
	var role *dsclient.Role
	if resp != nil {
		role = resp.Role
	}
	return formatDynamicSecretsRole(role, dynSecretsOutputFmt, cmd.OutOrStdout())
}

func newDynamicSecretsRoleListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List credential-generation roles",
		Long: `List the credential-generation roles visible to the caller.

Examples:
  quicbaas dynamic-secrets role list
  quicbaas dynamic-secrets role list --prefix readonly-`,
		Args: cobra.NoArgs,
		RunE: runDynamicSecretsRoleList,
	}
	cmd.Flags().StringVar(&dynSecretsPrefix, "prefix", "", "Only list roles whose name has this prefix")
	return cmd
}

func runDynamicSecretsRoleList(cmd *cobra.Command, _ []string) error {
	client, err := dynamicSecretsClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.ListRoles(ctx, &dsclient.ListRolesRequest{Prefix: dynSecretsPrefix})
	if err != nil {
		return formatError(err)
	}
	return formatDynamicSecretsRoleList(resp, dynSecretsOutputFmt, cmd.OutOrStdout())
}

func newDynamicSecretsRoleDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Remove a credential-generation role",
		Long: `Delete a role. Existing leases minted from it are unaffected; revoke them
separately with 'dynamic-secrets lease revoke'.

Examples:
  quicbaas dynamic-secrets role delete readonly-db`,
		Args: cobra.ExactArgs(1),
		RunE: runDynamicSecretsRoleDelete,
	}
}

func runDynamicSecretsRoleDelete(cmd *cobra.Command, args []string) error {
	name, err := dynamicSecretsArg("role delete", "name", args)
	if err != nil {
		return formatError(err)
	}

	client, err := dynamicSecretsClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.DeleteRole(ctx, &dsclient.DeleteRoleRequest{Name: name})
	if err != nil {
		return formatError(err)
	}
	var deletedAt time.Time
	if resp != nil {
		deletedAt = resp.DeletedAt
	}
	return formatDynamicSecretsAck(
		map[string]interface{}{"name": name, "deleted_at": deletedAt},
		fmt.Sprintf("role %s deleted\n", name),
		dynSecretsOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// generate
// =============================================================================

func newDynamicSecretsGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate <role>",
		Short: "Generate a credential from a role",
		Long: `Mint a short-lived credential from the named role.

The credential values are REDACTED in the default output. Pass --reveal to
print them, or use '--output json' and pipe the result into the consumer that
needs it.

Examples:
  quicbaas dynamic-secrets generate readonly-db
  quicbaas dynamic-secrets generate readonly-db --ttl 30m --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runDynamicSecretsGenerate,
	}
	cmd.Flags().DurationVar(&dynSecretsTTL, "ttl", 0, "Requested credential lifetime (0 = the role's default)")
	cmd.Flags().BoolVar(&dynSecretsRevealSecret, "reveal", false, "Print credential values in table output instead of redacting them")
	return cmd
}

func runDynamicSecretsGenerate(cmd *cobra.Command, args []string) error {
	role, err := dynamicSecretsArg("generate", "role", args)
	if err != nil {
		return formatError(err)
	}
	if dynSecretsTTL < 0 {
		return formatError(&DynamicSecretsFlagInvalidError{
			Verb: "generate", Flag: "ttl", Reason: "must not be negative",
		})
	}

	client, err := dynamicSecretsClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GenerateCredential(ctx, &dsclient.GenerateCredentialRequest{
		RoleName: role,
		TTL:      dynSecretsTTL,
	})
	if err != nil {
		return formatError(err)
	}
	return formatDynamicSecretsCredential(resp, dynSecretsRevealSecret, dynSecretsOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// lease
// =============================================================================

func newDynamicSecretsLeaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lease",
		Short: "Credential lease management",
	}
	cmd.AddCommand(newDynamicSecretsLeaseRenewCommand())
	cmd.AddCommand(newDynamicSecretsLeaseRevokeCommand())
	cmd.AddCommand(newDynamicSecretsLeaseListCommand())
	return cmd
}

func newDynamicSecretsLeaseRenewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "renew <lease-id>",
		Short: "Extend a credential lease",
		Long: `Push a lease's expiry out. The server clamps the result to the role's
max TTL.

Examples:
  quicbaas dynamic-secrets lease renew lease_abc123
  quicbaas dynamic-secrets lease renew lease_abc123 --increment 1h`,
		Args: cobra.ExactArgs(1),
		RunE: runDynamicSecretsLeaseRenew,
	}
	cmd.Flags().DurationVar(&dynSecretsIncrement, "increment", 0, "Duration to add to the lease expiry (0 = server default)")
	return cmd
}

func runDynamicSecretsLeaseRenew(cmd *cobra.Command, args []string) error {
	leaseID, err := dynamicSecretsArg("lease renew", "lease-id", args)
	if err != nil {
		return formatError(err)
	}
	if dynSecretsIncrement < 0 {
		return formatError(&DynamicSecretsFlagInvalidError{
			Verb: "lease renew", Flag: "increment", Reason: "must not be negative",
		})
	}

	client, err := dynamicSecretsClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RenewLease(ctx, &dsclient.RenewLeaseRequest{
		LeaseID:   leaseID,
		Increment: dynSecretsIncrement,
	})
	if err != nil {
		return formatError(err)
	}
	var lease *dsclient.Lease
	if resp != nil {
		lease = resp.Lease
	}
	return formatDynamicSecretsLease(lease, dynSecretsOutputFmt, cmd.OutOrStdout())
}

func newDynamicSecretsLeaseRevokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <lease-id>",
		Short: "Revoke a credential lease immediately",
		Long: `Revoke a lease and invalidate the credential it grants.

Examples:
  quicbaas dynamic-secrets lease revoke lease_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runDynamicSecretsLeaseRevoke,
	}
}

func runDynamicSecretsLeaseRevoke(cmd *cobra.Command, args []string) error {
	leaseID, err := dynamicSecretsArg("lease revoke", "lease-id", args)
	if err != nil {
		return formatError(err)
	}

	client, err := dynamicSecretsClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RevokeLease(ctx, &dsclient.RevokeLeaseRequest{LeaseID: leaseID})
	if err != nil {
		return formatError(err)
	}
	var revokedAt time.Time
	if resp != nil {
		revokedAt = resp.RevokedAt
	}
	return formatDynamicSecretsAck(
		map[string]interface{}{"lease_id": leaseID, "revoked_at": revokedAt},
		fmt.Sprintf("lease %s revoked\n", leaseID),
		dynSecretsOutputFmt, cmd.OutOrStdout(),
	)
}

func newDynamicSecretsLeaseListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active credential leases",
		Long: `List the active leases visible to the caller.

Examples:
  quicbaas dynamic-secrets lease list
  quicbaas dynamic-secrets lease list --prefix database/`,
		Args: cobra.NoArgs,
		RunE: runDynamicSecretsLeaseList,
	}
	cmd.Flags().StringVar(&dynSecretsPrefix, "prefix", "", "Only list leases whose secret path has this prefix")
	return cmd
}

func runDynamicSecretsLeaseList(cmd *cobra.Command, _ []string) error {
	client, err := dynamicSecretsClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.ListLeases(ctx, &dsclient.ListLeasesRequest{Prefix: dynSecretsPrefix})
	if err != nil {
		return formatError(err)
	}
	return formatDynamicSecretsLeaseList(resp, dynSecretsOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Input helpers
// =============================================================================

// dynamicSecretsArg validates the single positional argument shared by the
// name/role/lease-id verbs.
func dynamicSecretsArg(verb, argName string, args []string) (string, error) {
	if len(args) == 0 {
		return "", &DynamicSecretsArgRequiredError{Verb: verb, Arg: argName}
	}
	value := strings.TrimSpace(args[0])
	if value == "" {
		return "", &DynamicSecretsArgRequiredError{Verb: verb, Arg: argName}
	}
	return value, nil
}

// parseDynamicSecretsConfig converts repeated "key=value" flags into the
// generator config map.
func parseDynamicSecretsConfig(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	config := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, found := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, &DynamicSecretsConfigFormatError{Value: entry}
		}
		config[key] = value
	}
	return config, nil
}

// =============================================================================
// Formatting
// =============================================================================

// formatDynamicSecretsRole renders a single role.
func formatDynamicSecretsRole(role *dsclient.Role, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if role == nil {
		fmt.Fprintln(w, "(no role)")
		return nil
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(role)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(role)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Name:\t%s\n", role.Name)
		fmt.Fprintf(tw, "Generator Type:\t%s\n", role.GeneratorType)
		fmt.Fprintf(tw, "Default TTL:\t%s\n", role.DefaultTTL)
		fmt.Fprintf(tw, "Max TTL:\t%s\n", role.MaxTTL)
		for _, key := range sortedKeys(role.Config) {
			fmt.Fprintf(tw, "Config[%s]:\t%s\n", key, role.Config[key])
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s (%s)\n", role.Name, role.GeneratorType)
		return nil
	}
}

// formatDynamicSecretsRoleList renders a role listing.
func formatDynamicSecretsRoleList(resp *dsclient.ListRolesResponse, format string, w io.Writer) error {
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

	case cli.OutputFormatTable:
		if resp == nil || len(resp.Roles) == 0 {
			fmt.Fprintln(w, "No roles found")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tGENERATOR\tDEFAULT TTL\tMAX TTL")
		for _, role := range resp.Roles {
			if role == nil {
				continue
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				role.Name, role.GeneratorType, role.DefaultTTL, role.MaxTTL)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(w, "\n(%d role(s))\n", resp.Count)
		return nil

	default:
		if resp == nil {
			return nil
		}
		for _, role := range resp.Roles {
			if role != nil {
				fmt.Fprintln(w, role.Name)
			}
		}
		return nil
	}
}

// formatDynamicSecretsCredential renders a generated credential. Table output
// redacts the credential values unless reveal is set; JSON and YAML always
// carry the full payload because they exist to be piped into a consumer.
func formatDynamicSecretsCredential(resp *dsclient.GenerateCredentialResponse, reveal bool, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if resp == nil {
		fmt.Fprintln(w, "(no credential)")
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
		fmt.Fprintf(tw, "Lease ID:\t%s\n", resp.LeaseID)
		if c := resp.Credential; c != nil {
			fmt.Fprintf(tw, "Credential ID:\t%s\n", c.CredentialID)
			if c.Type != "" {
				fmt.Fprintf(tw, "Type:\t%s\n", c.Type)
			}
			if !c.ExpiresAt.IsZero() {
				fmt.Fprintf(tw, "Expires:\t%s\n", c.ExpiresAt.UTC().Format(time.RFC3339))
			}
			for _, key := range sortedKeys(c.Data) {
				value := "<redacted — pass --reveal or use --output json>"
				if reveal {
					value = c.Data[key]
				}
				fmt.Fprintf(tw, "%s:\t%s\n", key, value)
			}
		}
		if l := resp.Lease; l != nil {
			fmt.Fprintf(tw, "Lease TTL:\t%s\n", l.TTL)
			fmt.Fprintf(tw, "Lease Renewable:\t%t\n", l.Renewable)
		}
		return tw.Flush()

	default:
		fmt.Fprintln(w, resp.LeaseID)
		return nil
	}
}

// formatDynamicSecretsLease renders a single lease.
func formatDynamicSecretsLease(lease *dsclient.Lease, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if lease == nil {
		fmt.Fprintln(w, "(no lease)")
		return nil
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(lease)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(lease)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		writeDynamicSecretsLeaseRows(tw, lease)
		return tw.Flush()

	default:
		fmt.Fprintln(w, lease.LeaseID)
		return nil
	}
}

// writeDynamicSecretsLeaseRows emits the shared key/value rows for a lease.
func writeDynamicSecretsLeaseRows(tw io.Writer, lease *dsclient.Lease) {
	fmt.Fprintf(tw, "Lease ID:\t%s\n", lease.LeaseID)
	if lease.SecretPath != "" {
		fmt.Fprintf(tw, "Secret Path:\t%s\n", lease.SecretPath)
	}
	fmt.Fprintf(tw, "TTL:\t%s\n", lease.TTL)
	if !lease.IssuedAt.IsZero() {
		fmt.Fprintf(tw, "Issued:\t%s\n", lease.IssuedAt.UTC().Format(time.RFC3339))
	}
	if !lease.ExpiresAt.IsZero() {
		fmt.Fprintf(tw, "Expires:\t%s\n", lease.ExpiresAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(tw, "Renewable:\t%t\n", lease.Renewable)
	fmt.Fprintf(tw, "Renew Count:\t%d\n", lease.RenewCount)
	if lease.MaxRenewals > 0 {
		fmt.Fprintf(tw, "Max Renewals:\t%d\n", lease.MaxRenewals)
	}
	if lease.RevokedAt != nil {
		fmt.Fprintf(tw, "Revoked:\t%s\n", lease.RevokedAt.UTC().Format(time.RFC3339))
	}
	for _, key := range sortedKeys(lease.Metadata) {
		fmt.Fprintf(tw, "Metadata[%s]:\t%s\n", key, lease.Metadata[key])
	}
}

// formatDynamicSecretsLeaseList renders a lease listing.
func formatDynamicSecretsLeaseList(resp *dsclient.ListLeasesResponse, format string, w io.Writer) error {
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

	case cli.OutputFormatTable:
		if resp == nil || len(resp.Leases) == 0 {
			fmt.Fprintln(w, "No leases found")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "LEASE ID\tSECRET PATH\tTTL\tEXPIRES\tRENEWABLE\tRENEWALS")
		for _, lease := range resp.Leases {
			if lease == nil {
				continue
			}
			expires := "-"
			if !lease.ExpiresAt.IsZero() {
				expires = lease.ExpiresAt.UTC().Format(time.RFC3339)
			}
			path := lease.SecretPath
			if path == "" {
				path = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%d\n",
				lease.LeaseID, path, lease.TTL, expires, lease.Renewable, lease.RenewCount)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(w, "\n(%d lease(s))\n", resp.Count)
		return nil

	default:
		if resp == nil {
			return nil
		}
		for _, lease := range resp.Leases {
			if lease != nil {
				fmt.Fprintln(w, lease.LeaseID)
			}
		}
		return nil
	}
}

// formatDynamicSecretsAck renders the acknowledgement returned by verbs with
// no body.
func formatDynamicSecretsAck(payload map[string]interface{}, humanLine, format string, w io.Writer) error {
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
