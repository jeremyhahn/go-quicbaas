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

// dek.go implements the `quicbaas dek` CLI verb tree — the canonical,
// SDK-backed surface for Data Encryption Key management.
//
// Every verb routes through Security().DEK() (sdk/go/client/dek), so the whole
// tree works identically on rest / grpc / quic / mcp / unix / embedded. The
// verb set mirrors the SDK client interface exactly — eleven methods, no
// invented verbs.
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
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
)

// =============================================================================
// Flags
// =============================================================================

var (
	dekOutputFmt string

	// create
	dekAlgorithm   string
	dekDescription string

	// create / set-safety
	dekMaxOperations  uint64
	dekMaxBytes       uint64
	dekWarningPercent int

	// list
	dekListPrefix    string
	dekListRecursive bool
	dekListLimit     int

	// rotate
	dekRotateBackground bool

	// delete
	dekDeleteForce     bool
	dekDeleteMigrateTo string

	// reencrypt
	dekFromVersion    int
	dekToVersion      int
	dekBatchSize      int
	dekReencryptForce bool
	dekOperationID    string
)

// =============================================================================
// Typed errors
// =============================================================================

// DEKArgRequiredError is returned when a dek verb is missing its positional
// path argument.
type DEKArgRequiredError struct {
	Verb string
	Arg  string
}

// Error implements the error interface.
func (e *DEKArgRequiredError) Error() string {
	return "dek " + e.Verb + ": <" + e.Arg + "> argument is required"
}

// DEKFlagInvalidError is returned when a dek flag carries an unusable value.
type DEKFlagInvalidError struct {
	Verb   string
	Flag   string
	Reason string
}

// Error implements the error interface.
func (e *DEKFlagInvalidError) Error() string {
	return "dek " + e.Verb + ": --" + e.Flag + ": " + e.Reason
}

// =============================================================================
// SDK factory
// =============================================================================

// SDKDEKClientFactory constructs the SDK DEK client. This is the only DEK
// client factory: the gRPC-pinned DEKClientFactory that used to back
// `kms dek` was deleted when that subtree collapsed onto this tree.
type SDKDEKClientFactory func() (quicbaas.DEKClient, error)

// sdkDEKClientFactory resolves Security().DEK(). Tests swap it.
var sdkDEKClientFactory SDKDEKClientFactory = createSDKDEKServiceClient

// createSDKDEKServiceClient returns the SDK DEK client bound to the active
// transport.
func createSDKDEKServiceClient() (quicbaas.DEKClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().DEK(), nil
}

// =============================================================================
// Command tree
// =============================================================================

// newDEKCommand builds the `quicbaas dek` command tree.
func newDEKCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dek",
		Short: "Data Encryption Key management",
		Long: `Data Encryption Key (DEK) management against the active QuicBaaS daemon.

DEKs provide envelope encryption with hierarchical, path-based namespacing and
multi-tenant isolation: data is encrypted with a DEK, and the DEK itself is
wrapped by a KMS master key.

Verbs:
  create              Create a DEK at a path
  get                 Show DEK metadata
  list                List DEKs under a path prefix
  rotate              Create a new version of a DEK
  delete              Remove a DEK
  status              Show detailed status including AEAD usage
  set-default         Mark a DEK as the default for its path level
  set-safety          Configure AEAD safety limits
  clear-safety        Remove custom AEAD safety limits
  reencrypt           Start background re-encryption to a new DEK version
  reencrypt-progress  Show re-encryption progress

All verbs route through Security().DEK() on the active SDK transport
(rest, grpc, quic, mcp, unix, embedded).`,
	}
	cmd.PersistentFlags().StringVar(&dekOutputFmt, "output", "table",
		"Output format (json, yaml, table)")

	cmd.AddCommand(newDEKCreateCommand())
	cmd.AddCommand(newDEKGetCommand())
	cmd.AddCommand(newDEKListCommand())
	cmd.AddCommand(newDEKRotateCommand())
	cmd.AddCommand(newDEKDeleteCommand())
	cmd.AddCommand(newDEKStatusCommand())
	cmd.AddCommand(newDEKSetDefaultCommand())
	cmd.AddCommand(newDEKSetSafetyCommand())
	cmd.AddCommand(newDEKClearSafetyCommand())
	cmd.AddCommand(newDEKReencryptCommand())
	cmd.AddCommand(newDEKReencryptProgressCommand())
	return cmd
}

// =============================================================================
// create
// =============================================================================

func newDEKCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "create <path>",
		// "generate" is the verb the retired `kms dek` subtree used; keeping
		// it as an alias means the deprecated mount is a true superset.
		Aliases: []string{"generate"},
		Short:   "Create a Data Encryption Key",
		Long: `Create a DEK at the supplied hierarchical path.

Examples:
  quicbaas dek create /tenants/acme --algorithm AES-256-GCM
  quicbaas dek create /tenants/acme/db --description "customer database" --max-bytes 1099511627776`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKCreate,
	}
	cmd.Flags().StringVar(&dekAlgorithm, "algorithm", "", "Encryption algorithm (e.g. AES-256-GCM)")
	cmd.Flags().StringVar(&dekDescription, "description", "", "Human-readable description")
	cmd.Flags().Uint64Var(&dekMaxOperations, "max-operations", 0, "Maximum encrypt operations before the DEK must rotate (0 = server default)")
	cmd.Flags().Uint64Var(&dekMaxBytes, "max-bytes", 0, "Maximum bytes encrypted before the DEK must rotate (0 = server default)")
	cmd.Flags().IntVar(&dekWarningPercent, "warning-percent", 0, "Percent of the safety limit that triggers a warning (0 = server default)")
	return cmd
}

func runDEKCreate(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("create", args)
	if err != nil {
		return formatError(err)
	}
	if dekWarningPercent < 0 || dekWarningPercent > 100 {
		return formatError(&DEKFlagInvalidError{Verb: "create", Flag: "warning-percent", Reason: "must be between 0 and 100"})
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.CreateDEK(ctx, &dekclient.CreateDEKRequest{
		Path:           path,
		Algorithm:      dekAlgorithm,
		Description:    dekDescription,
		MaxOperations:  dekMaxOperations,
		MaxBytes:       dekMaxBytes,
		WarningPercent: dekWarningPercent,
	})
	if err != nil {
		return formatError(err)
	}
	var info *dekclient.DEKInfo
	if resp != nil {
		info = resp.DEK
	}
	return formatDEKDetail(info, dekOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// get
// =============================================================================

func newDEKGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <path>",
		Short: "Show DEK metadata",
		Long: `Retrieve the metadata for a single DEK. Key material is never returned.

Examples:
  quicbaas dek get /tenants/acme
  quicbaas dek get /tenants/acme --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKGet,
	}
}

func runDEKGet(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("get", args)
	if err != nil {
		return formatError(err)
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	info, err := client.GetDEK(ctx, path)
	if err != nil {
		return formatError(err)
	}
	return formatDEKDetail(info, dekOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// list
// =============================================================================

func newDEKListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Data Encryption Keys",
		Long: `List the DEKs visible to the caller, optionally filtered by path prefix.

Examples:
  quicbaas dek list
  quicbaas dek list --prefix /tenants/ --recursive
  quicbaas dek list --limit 25 --output json`,
		Args: cobra.NoArgs,
		RunE: runDEKList,
	}
	cmd.Flags().StringVar(&dekListPrefix, "prefix", "", "Only list DEKs under this path prefix")
	cmd.Flags().BoolVar(&dekListRecursive, "recursive", false, "Recurse into nested paths below --prefix")
	cmd.Flags().IntVar(&dekListLimit, "limit", 0, "Maximum DEKs to return (0 = no caller-supplied limit)")
	return cmd
}

func runDEKList(cmd *cobra.Command, _ []string) error {
	if dekListLimit < 0 {
		return formatError(&DEKFlagInvalidError{Verb: "list", Flag: "limit", Reason: "must not be negative"})
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.ListDEKs(ctx, &dekclient.ListDEKsRequest{
		Prefix:    dekListPrefix,
		Recursive: dekListRecursive,
		Limit:     dekListLimit,
	})
	if err != nil {
		return formatError(err)
	}
	return formatDEKList(resp, dekOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// rotate
// =============================================================================

func newDEKRotateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate <path>",
		Short: "Create a new version of a DEK",
		Long: `Rotate a DEK, creating a new version. Existing data stays readable under
the previous version until it is re-encrypted; use 'dek reencrypt' to migrate.

Examples:
  quicbaas dek rotate /tenants/acme
  quicbaas dek rotate /tenants/acme --background`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKRotate,
	}
	cmd.Flags().BoolVar(&dekRotateBackground, "background", false, "Run the rotation asynchronously on the server")
	return cmd
}

func runDEKRotate(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("rotate", args)
	if err != nil {
		return formatError(err)
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RotateDEK(ctx, &dekclient.RotateDEKRequest{
		Path:       path,
		Background: dekRotateBackground,
	})
	if err != nil {
		return formatError(err)
	}
	if resp == nil {
		resp = &dekclient.RotateDEKResponse{Path: path}
	}
	return formatDEKAck(
		map[string]interface{}{
			"path":        resp.Path,
			"old_version": resp.OldVersion,
			"new_version": resp.NewVersion,
		},
		fmt.Sprintf("DEK %s rotated: v%d -> v%d\n", resp.Path, resp.OldVersion, resp.NewVersion),
		dekOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// delete
// =============================================================================

func newDEKDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <path>",
		Short: "Remove a Data Encryption Key",
		Long: `Delete a DEK. The server refuses deletion while data is still encrypted
under it unless --force is supplied or --migrate-to names a replacement.

Examples:
  quicbaas dek delete /tenants/acme
  quicbaas dek delete /tenants/acme --migrate-to /tenants/acme-v2
  quicbaas dek delete /tenants/acme --force`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKDelete,
	}
	cmd.Flags().BoolVar(&dekDeleteForce, "force", false, "Bypass deletion protection")
	cmd.Flags().StringVar(&dekDeleteMigrateTo, "migrate-to", "", "Migrate data to this DEK path before deleting")
	return cmd
}

func runDEKDelete(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("delete", args)
	if err != nil {
		return formatError(err)
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.DeleteDEK(ctx, &dekclient.DeleteDEKRequest{
		Path:      path,
		Force:     dekDeleteForce,
		MigrateTo: dekDeleteMigrateTo,
	})
	if err != nil {
		return formatError(err)
	}
	deleted := resp != nil && resp.Deleted
	line := fmt.Sprintf("DEK %s deleted\n", path)
	if !deleted {
		line = fmt.Sprintf("DEK %s deletion reported by server as unsuccessful\n", path)
	}
	return formatDEKAck(
		map[string]interface{}{"path": path, "deleted": deleted},
		line, dekOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// status
// =============================================================================

func newDEKStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <path>",
		Short: "Show detailed DEK status including AEAD usage",
		Long: `Show a DEK's metadata plus its AEAD usage counters and, when one is
running, the current re-encryption progress.

Examples:
  quicbaas dek status /tenants/acme
  quicbaas dek status /tenants/acme --output yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKStatus,
	}
}

func runDEKStatus(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("status", args)
	if err != nil {
		return formatError(err)
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GetDEKStatus(ctx, path)
	if err != nil {
		return formatError(err)
	}
	return formatDEKStatus(resp, dekOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// set-default
// =============================================================================

func newDEKSetDefaultCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <path>",
		Short: "Mark a DEK as the default for its path level",
		Long: `Make the named DEK the default for its path level, so writes under that
level use it without naming it explicitly.

Examples:
  quicbaas dek set-default /tenants/acme`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKSetDefault,
	}
}

func runDEKSetDefault(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("set-default", args)
	if err != nil {
		return formatError(err)
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.SetDefaultDEK(ctx, path)
	if err != nil {
		return formatError(err)
	}
	previous := ""
	if resp != nil {
		previous = resp.PreviousDefault
	}
	line := fmt.Sprintf("DEK %s is now the default\n", path)
	if previous != "" {
		line = fmt.Sprintf("DEK %s is now the default (was %s)\n", path, previous)
	}
	return formatDEKAck(
		map[string]interface{}{"path": path, "previous_default": previous},
		line, dekOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// set-safety
// =============================================================================

func newDEKSetSafetyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-safety <path>",
		Short: "Configure AEAD safety limits for a DEK",
		Long: `Set the AEAD safety limits that force a DEK to rotate before nonce reuse
becomes statistically possible.

At least one of --max-operations / --max-bytes must be supplied.

Examples:
  quicbaas dek set-safety /tenants/acme --max-operations 4294967296
  quicbaas dek set-safety /tenants/acme --max-bytes 1099511627776 --warning-percent 80`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKSetSafety,
	}
	cmd.Flags().Uint64Var(&dekMaxOperations, "max-operations", 0, "Maximum encrypt operations")
	cmd.Flags().Uint64Var(&dekMaxBytes, "max-bytes", 0, "Maximum bytes encrypted")
	cmd.Flags().IntVar(&dekWarningPercent, "warning-percent", 0, "Percent of the limit that triggers a warning")
	return cmd
}

func runDEKSetSafety(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("set-safety", args)
	if err != nil {
		return formatError(err)
	}
	if dekMaxOperations == 0 && dekMaxBytes == 0 {
		return formatError(&DEKFlagInvalidError{
			Verb:   "set-safety",
			Flag:   "max-operations/--max-bytes",
			Reason: "at least one limit must be supplied",
		})
	}
	if dekWarningPercent < 0 || dekWarningPercent > 100 {
		return formatError(&DEKFlagInvalidError{Verb: "set-safety", Flag: "warning-percent", Reason: "must be between 0 and 100"})
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.SetDEKSafety(ctx, &dekclient.SetDEKSafetyRequest{
		Path:           path,
		MaxOperations:  dekMaxOperations,
		MaxBytes:       dekMaxBytes,
		WarningPercent: dekWarningPercent,
	}); err != nil {
		return formatError(err)
	}
	return formatDEKAck(
		map[string]interface{}{
			"path":            path,
			"max_operations":  dekMaxOperations,
			"max_bytes":       dekMaxBytes,
			"warning_percent": dekWarningPercent,
		},
		fmt.Sprintf("safety limits updated for DEK %s\n", path),
		dekOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// clear-safety
// =============================================================================

func newDEKClearSafetyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clear-safety <path>",
		Short: "Remove custom AEAD safety limits from a DEK",
		Long: `Drop the DEK's caller-supplied AEAD safety limits, reverting it to the
server's defaults.

Examples:
  quicbaas dek clear-safety /tenants/acme`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKClearSafety,
	}
}

func runDEKClearSafety(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("clear-safety", args)
	if err != nil {
		return formatError(err)
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.ClearDEKSafety(ctx, path); err != nil {
		return formatError(err)
	}
	return formatDEKAck(
		map[string]interface{}{"path": path, "cleared": true},
		fmt.Sprintf("safety limits cleared for DEK %s\n", path),
		dekOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// reencrypt
// =============================================================================

func newDEKReencryptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reencrypt <path>",
		Short: "Start background re-encryption to a new DEK version",
		Long: `Start a background job that re-encrypts data from one DEK version to
another. Track it with 'dek reencrypt-progress'.

Examples:
  quicbaas dek reencrypt /tenants/acme --from-version 1 --to-version 2
  quicbaas dek reencrypt /tenants/acme --from-version 1 --to-version 2 --batch-size 500 --force`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKReencrypt,
	}
	cmd.Flags().IntVar(&dekFromVersion, "from-version", 0, "DEK version to re-encrypt from (required)")
	cmd.Flags().IntVar(&dekToVersion, "to-version", 0, "DEK version to re-encrypt to (required)")
	cmd.Flags().IntVar(&dekBatchSize, "batch-size", 0, "Entities processed per batch (0 = server default)")
	cmd.Flags().BoolVar(&dekReencryptForce, "force", false, "Start even if a re-encryption is already in progress")
	return cmd
}

func runDEKReencrypt(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("reencrypt", args)
	if err != nil {
		return formatError(err)
	}
	if dekFromVersion <= 0 {
		return formatError(&DEKFlagInvalidError{Verb: "reencrypt", Flag: "from-version", Reason: "must be a positive DEK version"})
	}
	if dekToVersion <= 0 {
		return formatError(&DEKFlagInvalidError{Verb: "reencrypt", Flag: "to-version", Reason: "must be a positive DEK version"})
	}
	if dekFromVersion == dekToVersion {
		return formatError(&DEKFlagInvalidError{Verb: "reencrypt", Flag: "to-version", Reason: "must differ from --from-version"})
	}
	if dekBatchSize < 0 {
		return formatError(&DEKFlagInvalidError{Verb: "reencrypt", Flag: "batch-size", Reason: "must not be negative"})
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.StartReencryption(ctx, &dekclient.StartReencryptionRequest{
		Path:        path,
		FromVersion: dekFromVersion,
		ToVersion:   dekToVersion,
		BatchSize:   dekBatchSize,
		Force:       dekReencryptForce,
	})
	if err != nil {
		return formatError(err)
	}
	if resp == nil {
		resp = &dekclient.StartReencryptionResponse{Path: path}
	}
	return formatDEKAck(
		map[string]interface{}{
			"path":            resp.Path,
			"operation_id":    resp.OperationID,
			"started":         resp.Started,
			"estimated_items": resp.EstimatedItems,
		},
		fmt.Sprintf("re-encryption %s started for DEK %s (%d estimated items)\n",
			resp.OperationID, resp.Path, resp.EstimatedItems),
		dekOutputFmt, cmd.OutOrStdout(),
	)
}

// =============================================================================
// reencrypt-progress
// =============================================================================

func newDEKReencryptProgressCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reencrypt-progress <path>",
		Short: "Show re-encryption progress",
		Long: `Report the progress of a running or completed re-encryption for a DEK.

Examples:
  quicbaas dek reencrypt-progress /tenants/acme
  quicbaas dek reencrypt-progress /tenants/acme --operation-id op_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runDEKReencryptProgress,
	}
	cmd.Flags().StringVar(&dekOperationID, "operation-id", "", "Report on this specific re-encryption operation")
	return cmd
}

func runDEKReencryptProgress(cmd *cobra.Command, args []string) error {
	path, err := dekPathArg("reencrypt-progress", args)
	if err != nil {
		return formatError(err)
	}

	client, err := sdkDEKClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GetReencryptionProgress(ctx, &dekclient.GetReencryptionProgressRequest{
		Path:        path,
		OperationID: dekOperationID,
	})
	if err != nil {
		return formatError(err)
	}
	return formatDEKReencryptionProgress(resp, dekOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Argument helpers
// =============================================================================

// dekPathArg validates and normalises the single positional path argument
// every path-scoped dek verb takes.
func dekPathArg(verb string, args []string) (string, error) {
	if len(args) == 0 {
		return "", &DEKArgRequiredError{Verb: verb, Arg: "path"}
	}
	path := strings.TrimSpace(args[0])
	if path == "" {
		return "", &DEKArgRequiredError{Verb: verb, Arg: "path"}
	}
	return path, nil
}

// =============================================================================
// Formatting
// =============================================================================

// formatDEKDetail renders a single DEK's metadata.
func formatDEKDetail(info *dekclient.DEKInfo, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if info == nil {
		fmt.Fprintln(w, "(no DEK)")
		return nil
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(info)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(info)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		writeDEKInfoRows(tw, info)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s v%d (%s, %s)\n", info.Path, info.Version, info.Algorithm, info.Status)
		return nil
	}
}

// writeDEKInfoRows emits the shared key/value rows for a DEK.
func writeDEKInfoRows(tw io.Writer, info *dekclient.DEKInfo) {
	fmt.Fprintf(tw, "Path:\t%s\n", info.Path)
	fmt.Fprintf(tw, "Algorithm:\t%s\n", info.Algorithm)
	fmt.Fprintf(tw, "Version:\t%d\n", info.Version)
	fmt.Fprintf(tw, "Status:\t%s\n", info.Status)
	fmt.Fprintf(tw, "Default:\t%t\n", info.IsDefault)
	if info.BackendType != "" {
		fmt.Fprintf(tw, "Backend:\t%s\n", info.BackendType)
	}
	if info.Description != "" {
		fmt.Fprintf(tw, "Description:\t%s\n", info.Description)
	}
	if !info.CreatedAt.IsZero() {
		fmt.Fprintf(tw, "Created:\t%s\n", info.CreatedAt.UTC().Format(time.RFC3339))
	}
	if !info.RotatedAt.IsZero() {
		fmt.Fprintf(tw, "Rotated:\t%s\n", info.RotatedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(tw, "Usage Count:\t%d\n", info.UsageCount)
	fmt.Fprintf(tw, "Bytes Used:\t%d\n", info.BytesUsed)
	if info.MaxUsageCount > 0 {
		fmt.Fprintf(tw, "Max Usage Count:\t%d\n", info.MaxUsageCount)
	}
	if info.MaxBytesUsed > 0 {
		fmt.Fprintf(tw, "Max Bytes:\t%d\n", info.MaxBytesUsed)
	}
	if info.WarningPercent > 0 {
		fmt.Fprintf(tw, "Warning Percent:\t%d\n", info.WarningPercent)
	}
}

// formatDEKList renders a DEK listing.
func formatDEKList(resp *dekclient.ListDEKsResponse, format string, w io.Writer) error {
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
		if resp == nil || len(resp.DEKs) == 0 {
			fmt.Fprintln(w, "No DEKs found")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PATH\tALGORITHM\tVERSION\tSTATUS\tDEFAULT\tUSAGE")
		for i := range resp.DEKs {
			d := &resp.DEKs[i]
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%t\t%d\n",
				d.Path, d.Algorithm, d.Version, d.Status, d.IsDefault, d.UsageCount)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(w, "\n(showing %d of %d total)\n", len(resp.DEKs), resp.TotalCount)
		return nil

	default:
		if resp == nil {
			return nil
		}
		for i := range resp.DEKs {
			fmt.Fprintln(w, resp.DEKs[i].Path)
		}
		return nil
	}
}

// formatDEKStatus renders a detailed DEK status response.
func formatDEKStatus(resp *dekclient.DEKStatusResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if resp == nil {
		fmt.Fprintln(w, "(no status)")
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
		if resp.DEK != nil {
			writeDEKInfoRows(tw, resp.DEK)
		}
		if p := resp.ReencryptionProgress; p != nil {
			fmt.Fprintf(tw, "\nRe-encryption In Progress:\t%t\n", p.InProgress)
			fmt.Fprintf(tw, "Processed / Total:\t%d / %d\n", p.ProcessedKeys, p.TotalKeys)
			fmt.Fprintf(tw, "Failed:\t%d\n", p.FailedKeys)
			fmt.Fprintf(tw, "Percent Complete:\t%.2f\n", p.PercentComplete)
			if !p.StartedAt.IsZero() {
				fmt.Fprintf(tw, "Started:\t%s\n", p.StartedAt.UTC().Format(time.RFC3339))
			}
			if p.ErrorMessage != "" {
				fmt.Fprintf(tw, "Error:\t%s\n", p.ErrorMessage)
			}
		}
		return tw.Flush()

	default:
		if resp.DEK != nil {
			fmt.Fprintf(w, "%s v%d (%s)\n", resp.DEK.Path, resp.DEK.Version, resp.DEK.Status)
		}
		return nil
	}
}

// formatDEKReencryptionProgress renders a re-encryption progress report.
func formatDEKReencryptionProgress(resp *dekclient.ReencryptionProgressResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if resp == nil {
		fmt.Fprintln(w, "(no progress)")
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
		fmt.Fprintf(tw, "Path:\t%s\n", resp.Path)
		fmt.Fprintf(tw, "Operation ID:\t%s\n", resp.OperationID)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Status)
		if p := resp.Progress; p != nil {
			fmt.Fprintf(tw, "In Progress:\t%t\n", p.InProgress)
			fmt.Fprintf(tw, "Processed / Total:\t%d / %d\n", p.ProcessedKeys, p.TotalKeys)
			fmt.Fprintf(tw, "Failed:\t%d\n", p.FailedKeys)
			fmt.Fprintf(tw, "Percent Complete:\t%.2f\n", p.PercentComplete)
			if !p.StartedAt.IsZero() {
				fmt.Fprintf(tw, "Started:\t%s\n", p.StartedAt.UTC().Format(time.RFC3339))
			}
		}
		if resp.Error != "" {
			fmt.Fprintf(tw, "Error:\t%s\n", resp.Error)
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s %s\n", resp.Path, resp.Status)
		return nil
	}
}

// formatDEKAck renders the acknowledgement returned by verbs with no body.
func formatDEKAck(payload map[string]interface{}, humanLine, format string, w io.Writer) error {
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
