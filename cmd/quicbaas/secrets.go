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

// Package main provides Secrets Management CLI commands for QuicBaaS.
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

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	clisecrets "github.com/jeremyhahn/go-quicbaas/pkg/cli/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

var (
	secretsData        []string
	secretsTTL         time.Duration
	secretsVersion     uint64
	secretsPrefix      string
	secretsLimit       int
	secretsOutputFmt   string
	secretsPermanent   bool
	secretsAllVersions bool
	secretsIncludeData bool
	secretsWatchTTL    time.Duration

	// Fetch command flags.
	fetchOutputDir   string
	fetchFormat      string
	fetchCreateDirs  bool
	fetchFileMode    uint32
	fetchDirMode     uint32
	fetchFlattenKeys bool
	fetchStripPrefix bool
	fetchKeys        []string

	// TLS flags for secrets commands (mTLS authentication).
	secretsTLSEnabled bool
	secretsTLSCert    string
	secretsTLSKey     string
	secretsTLSCA      string

	// Authentication mode (token, k8s).
	secretsAuthMode string
)

// newSecretsCommand creates the secrets command group.
func newSecretsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Secrets management commands",
		Long: `Secrets commands for storing and managing sensitive data.

These commands allow you to store, retrieve, and manage versioned secrets
with automatic encryption and optional TTL.`,
	}

	cmd.PersistentFlags().StringVar(&secretsOutputFmt, "output", "table", "Output format (json, yaml, table)")

	// TLS flags for mTLS authentication
	cmd.PersistentFlags().BoolVar(&secretsTLSEnabled, "tls", false, "Enable TLS")
	cmd.PersistentFlags().StringVar(&secretsTLSCert, "cert", "", "TLS client certificate file")
	cmd.PersistentFlags().StringVar(&secretsTLSKey, "key", "", "TLS client key file")
	cmd.PersistentFlags().StringVar(&secretsTLSCA, "ca", "", "TLS CA certificate file")

	// Authentication mode
	cmd.PersistentFlags().StringVar(&secretsAuthMode, "auth", "token", "Authentication mode (token, k8s)")

	cmd.AddCommand(newSecretsPutCommand())
	cmd.AddCommand(newSecretsGetCommand())
	cmd.AddCommand(newSecretsGetPrefixCommand())
	cmd.AddCommand(newSecretsListCommand())
	cmd.AddCommand(newSecretsDeleteCommand())
	cmd.AddCommand(newSecretsDestroyCommand())
	cmd.AddCommand(newSecretsVersionsCommand())
	cmd.AddCommand(newSecretsRollbackCommand())
	cmd.AddCommand(newSecretsRotateCommand())
	cmd.AddCommand(newSecretsEnableCommand())
	cmd.AddCommand(newSecretsDisableCommand())
	cmd.AddCommand(newSecretsWatchCommand())
	cmd.AddCommand(newSecretsFetchCommand())

	return cmd
}

// newSecretsPutCommand creates the secrets put command.
func newSecretsPutCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "put <path>",
		Short: "Store a secret",
		Long: `Store a secret at the specified path with key-value data.

Secrets are automatically encrypted and versioned.

Examples:
  # Store a single key-value pair
  quicbaas secrets put database/prod --data password=secret123

  # Store multiple key-value pairs
  quicbaas secrets put app/config --data username=admin --data password=secret --data host=localhost

  # Store with TTL
  quicbaas secrets put temp/token --data token=abc123 --ttl 1h`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsPutCommand,
	}

	cmd.Flags().StringArrayVar(&secretsData, "data", nil, "Secret data as KEY=VALUE pairs (can be specified multiple times)")
	cmd.Flags().DurationVar(&secretsTTL, "ttl", 0, "Time-to-live for the secret")

	cmd.MarkFlagRequired("data")

	return cmd
}

// runSecretsPutCommand executes the secrets put command.
func runSecretsPutCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	dataMap := make(map[string]string)
	for _, kv := range secretsData {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return errors.Newf(errors.ErrInvalidInput, "secrets.put", "invalid data format: %s (expected KEY=VALUE)", kv)
		}
		dataMap[parts[0]] = parts[1]
	}

	dataBytes, err := json.Marshal(dataMap)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "secrets.put", "failed to marshal secret data")
	}

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.StoreSecretRequest{
		SecretID: secretPath,
		Name:     secretPath,
		Data:     dataBytes,
	}

	if secretsTTL > 0 {
		expiresAt := time.Now().Add(secretsTTL)
		req.ExpiresAt = &expiresAt
	}

	resp, err := executeSecretsStore(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsStoreResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsGetCommand creates the secrets get command.
func newSecretsGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <path>",
		Short: "Retrieve a secret",
		Long: `Retrieve a secret from the specified path.

Examples:
  # Get latest version
  quicbaas secrets get database/prod

  # Get specific version
  quicbaas secrets get database/prod --version 2

  # Get as JSON
  quicbaas secrets get database/prod --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsGetCommand,
	}

	cmd.Flags().Uint64Var(&secretsVersion, "version", 0, "Secret version (0 for latest)")

	return cmd
}

// runSecretsGetCommand executes the secrets get command.
func runSecretsGetCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.GetSecretRequest{
		SecretID: secretPath,
		Version:  secretsVersion,
	}

	resp, err := executeSecretsGet(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsGetResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsListCommand creates the secrets list command.
func newSecretsListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List secrets",
		Long: `List all secrets with optional prefix filtering.

Examples:
  # List all secrets
  quicbaas secrets list

  # List with prefix filter
  quicbaas secrets list --prefix database/

  # List with limit
  quicbaas secrets list --limit 50`,
		RunE: runSecretsListCommand,
	}

	cmd.Flags().StringVar(&secretsPrefix, "prefix", "", "Filter by path prefix")
	cmd.Flags().IntVar(&secretsLimit, "limit", 100, "Maximum number of secrets to return")

	return cmd
}

// runSecretsListCommand executes the secrets list command.
func runSecretsListCommand(cmd *cobra.Command, args []string) error {
	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	filter := make(map[string]string)
	if secretsPrefix != "" {
		filter["prefix"] = secretsPrefix
	}

	req := &service.ListSecretsRequest{
		Limit:  secretsLimit,
		Filter: filter,
	}

	resp, err := executeSecretsList(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsListResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsDeleteCommand creates the secrets delete command.
func newSecretsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <path>",
		Short: "Soft-delete a secret",
		Long: `Soft-delete a secret (can be recovered).

Examples:
  # Soft-delete a secret
  quicbaas secrets delete database/prod

  # Permanently delete
  quicbaas secrets delete database/prod --permanent`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsDeleteCommand,
	}

	cmd.Flags().BoolVar(&secretsPermanent, "permanent", false, "Permanently delete (cannot be recovered)")
	cmd.Flags().BoolVar(&secretsAllVersions, "all-versions", false, "Delete all versions of the secret")

	return cmd
}

// runSecretsDeleteCommand executes the secrets delete command.
func runSecretsDeleteCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.DeleteSecretRequest{
		SecretID:          secretPath,
		PermanentlyDelete: secretsPermanent,
		AllVersions:       secretsAllVersions,
	}

	resp, err := executeSecretsDelete(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsDeleteResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsDestroyCommand creates the secrets destroy command.
func newSecretsDestroyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy <path>",
		Short: "Permanently destroy a secret",
		Long: `Permanently destroy a secret and all its versions.

This operation is irreversible.

Examples:
  # Permanently destroy a secret
  quicbaas secrets destroy database/old`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsDestroyCommand,
	}

	return cmd
}

// runSecretsDestroyCommand executes the secrets destroy command.
func runSecretsDestroyCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.DeleteSecretRequest{
		SecretID:          secretPath,
		PermanentlyDelete: true,
	}

	resp, err := executeSecretsDelete(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsDeleteResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsVersionsCommand creates the secrets versions command.
func newSecretsVersionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions <path>",
		Short: "List secret versions",
		Long: `List all versions of a secret.

Examples:
  # List all versions
  quicbaas secrets versions database/prod

  # List as JSON
  quicbaas secrets versions database/prod --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsVersionsCommand,
	}

	return cmd
}

// runSecretsVersionsCommand executes the secrets versions command.
func runSecretsVersionsCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.ListSecretVersionsRequest{
		SecretID: secretPath,
		Limit:    100,
	}

	resp, err := executeSecretsListVersions(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsVersionsResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsRollbackCommand creates the secrets rollback command.
func newSecretsRollbackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback <path>",
		Short: "Rollback to a previous version",
		Long: `Rollback a secret to a previous version.

Examples:
  # Rollback to version 2
  quicbaas secrets rollback database/prod --version 2`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsRollbackCommand,
	}

	cmd.Flags().Uint64Var(&secretsVersion, "version", 0, "Target version (required)")

	cmd.MarkFlagRequired("version")

	return cmd
}

// runSecretsRollbackCommand executes the secrets rollback command.
func runSecretsRollbackCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	if secretsVersion == 0 {
		return errors.Newf(errors.ErrInvalidInput, "secrets.rollback", "version must be greater than 0")
	}

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.RollbackSecretRequest{
		SecretID:      secretPath,
		TargetVersion: secretsVersion,
	}

	resp, err := executeSecretsRollback(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsRollbackResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsRotateCommand creates the secrets rotate command.
func newSecretsRotateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate <path>",
		Short: "Rotate a secret with new data",
		Long: `Rotate a secret by creating a new version with updated data.

This creates a new version of the secret with the provided data.
The previous versions are preserved and can be accessed by version number.

Examples:
  # Rotate with new password
  quicbaas secrets rotate database/prod --data password=newpassword123

  # Rotate with multiple key-value pairs
  quicbaas secrets rotate app/config --data username=newadmin --data password=newsecret`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsRotateCommand,
	}

	cmd.Flags().StringArrayVar(&secretsData, "data", nil, "Secret data as KEY=VALUE pairs (can be specified multiple times)")
	cmd.MarkFlagRequired("data")

	return cmd
}

// runSecretsRotateCommand executes the secrets rotate command.
func runSecretsRotateCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	dataMap := make(map[string]string)
	for _, kv := range secretsData {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return errors.Newf(errors.ErrInvalidInput, "secrets.rotate", "invalid data format: %s (expected KEY=VALUE)", kv)
		}
		dataMap[parts[0]] = parts[1]
	}

	dataBytes, err := json.Marshal(dataMap)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "secrets.rotate", "failed to marshal secret data")
	}

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.RotateSecretRequest{
		SecretID: secretPath,
		NewData:  dataBytes,
	}

	resp, err := executeSecretsRotate(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsRotateResponse(resp, secretsOutputFmt, os.Stdout)
}

func executeSecretsRotate(ctx context.Context, client SecretsClient, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.rotate", "invalid client type")
	}
	return client.RotateSecret(ctx, req)
}

func formatSecretsRotateResponse(resp *service.RotateSecretResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Secret ID:\t%s\n", resp.SecretID)
		fmt.Fprintf(tw, "New Version:\t%d\n", resp.NewVersion)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Secret rotated: %s (new version %d)\n", resp.SecretID, resp.NewVersion)
		return nil
	}
}

// Secrets service execution functions

func executeSecretsStore(ctx context.Context, client SecretsClient, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.store", "invalid client type")
	}
	return client.StoreSecret(ctx, req)
}

func executeSecretsGet(ctx context.Context, client SecretsClient, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.get", "invalid client type")
	}
	return client.GetSecret(ctx, req)
}

func executeSecretsList(ctx context.Context, client SecretsClient, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.list", "invalid client type")
	}
	return client.ListSecrets(ctx, req)
}

func executeSecretsDelete(ctx context.Context, client SecretsClient, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.delete", "invalid client type")
	}
	return client.DeleteSecret(ctx, req)
}

func executeSecretsListVersions(ctx context.Context, client SecretsClient, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.versions", "invalid client type")
	}
	return client.ListSecretVersions(ctx, req)
}

func executeSecretsRollback(ctx context.Context, client SecretsClient, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.rollback", "invalid client type")
	}
	return client.RollbackSecret(ctx, req)
}

// Formatting functions

func formatSecretsStoreResponse(resp *service.StoreSecretResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Secret ID:\t%s\n", resp.SecretID)
		fmt.Fprintf(tw, "Version:\t%d\n", resp.Version)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Secret stored: %s (version %d)\n", resp.SecretID, resp.Version)
		return nil
	}
}

func formatSecretsGetResponse(resp *service.GetSecretResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	var dataMap map[string]interface{}
	if err := json.Unmarshal(resp.Data, &dataMap); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "secrets.get", "failed to unmarshal secret data")
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"secret_id": resp.SecretID,
			"version":   resp.Version,
			"data":      dataMap,
			"created":   resp.CreatedAt.Format(time.RFC3339),
		}
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Secret ID:\t%s\n", resp.SecretID)
		fmt.Fprintf(tw, "Version:\t%d\n", resp.Version)
		fmt.Fprintf(tw, "Created:\t%s\n", resp.CreatedAt.Format(time.RFC3339))
		fmt.Fprintf(tw, "\nData:\n")
		for k, v := range dataMap {
			fmt.Fprintf(tw, "  %s:\t%v\n", k, v)
		}
		return tw.Flush()

	default:
		for k, v := range dataMap {
			fmt.Fprintf(w, "%s=%v\n", k, v)
		}
		return nil
	}
}

func formatSecretsListResponse(resp *service.ListSecretsResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "SECRET ID\tVERSION\tCREATED\tUPDATED\n")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			strings.Repeat("-", 40),
			strings.Repeat("-", 7),
			strings.Repeat("-", 20),
			strings.Repeat("-", 20))
		for _, secret := range resp.Secrets {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n",
				secret.SecretID,
				secret.CurrentVersion,
				secret.CreatedAt.Format(time.RFC3339),
				secret.UpdatedAt.Format(time.RFC3339))
		}
		fmt.Fprintf(tw, "\nTotal: %d secrets\n", len(resp.Secrets))
		return tw.Flush()

	default:
		for _, secret := range resp.Secrets {
			fmt.Fprintf(w, "%s (v%d)\n", secret.SecretID, secret.CurrentVersion)
		}
		fmt.Fprintf(w, "\nTotal: %d secrets\n", len(resp.Secrets))
		return nil
	}
}

func formatSecretsDeleteResponse(resp *service.DeleteSecretResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Secret ID:\t%s\n", resp.SecretID)
		fmt.Fprintf(tw, "Deleted:\t%v\n", resp.Deleted)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		return tw.Flush()

	default:
		if resp.Deleted {
			fmt.Fprintf(w, "Secret deleted: %s\n", resp.SecretID)
		} else {
			fmt.Fprintf(w, "Failed to delete secret: %s\n", resp.SecretID)
		}
		return nil
	}
}

func formatSecretsVersionsResponse(resp *service.ListSecretVersionsResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "VERSION\tCREATED\tCREATED BY\tDELETED\n")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			strings.Repeat("-", 7),
			strings.Repeat("-", 20),
			strings.Repeat("-", 20),
			strings.Repeat("-", 7))
		for _, version := range resp.Versions {
			deleted := "no"
			if version.DeletedAt != nil {
				deleted = "yes"
			}
			createdBy := version.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n",
				version.Version,
				version.CreatedAt.Format(time.RFC3339),
				createdBy,
				deleted)
		}
		fmt.Fprintf(tw, "\nTotal: %d versions\n", len(resp.Versions))
		return tw.Flush()

	default:
		for _, version := range resp.Versions {
			fmt.Fprintf(w, "v%d - %s\n", version.Version, version.CreatedAt.Format(time.RFC3339))
		}
		fmt.Fprintf(w, "\nTotal: %d versions\n", len(resp.Versions))
		return nil
	}
}

func formatSecretsRollbackResponse(resp *service.RollbackSecretResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Secret ID:\t%s\n", resp.SecretID)
		fmt.Fprintf(tw, "New Version:\t%d\n", resp.NewVersion)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Secret rolled back: %s (new version %d)\n", resp.SecretID, resp.NewVersion)
		return nil
	}
}

// newSecretsEnableCommand creates the secrets enable command.
func newSecretsEnableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable <path>",
		Short: "Enable a secret",
		Long: `Enable a previously disabled secret.

This makes the secret accessible for read operations again.

Examples:
  quicbaas secrets enable database/prod`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsEnableCommand,
	}

	return cmd
}

// runSecretsEnableCommand executes the secrets enable command.
func runSecretsEnableCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.UpdateSecretStatusRequest{
		SecretID: secretPath,
		Status:   service.SecretStatusEnabled,
	}

	resp, err := client.UpdateSecretStatus(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsStatusResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsDisableCommand creates the secrets disable command.
func newSecretsDisableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable <path>",
		Short: "Disable a secret",
		Long: `Disable a secret to prevent read operations.

The secret is not deleted and can be re-enabled later.

Examples:
  quicbaas secrets disable database/prod`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsDisableCommand,
	}

	return cmd
}

// runSecretsDisableCommand executes the secrets disable command.
func runSecretsDisableCommand(cmd *cobra.Command, args []string) error {
	secretPath := args[0]

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.UpdateSecretStatusRequest{
		SecretID: secretPath,
		Status:   service.SecretStatusDisabled,
	}

	resp, err := client.UpdateSecretStatus(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsStatusResponse(resp, secretsOutputFmt, os.Stdout)
}

func formatSecretsStatusResponse(resp *service.UpdateSecretStatusResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Secret ID:\t%s\n", resp.SecretID)
		fmt.Fprintf(tw, "Old Status:\t%s\n", resp.OldStatus)
		fmt.Fprintf(tw, "New Status:\t%s\n", resp.NewStatus)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Secret status updated: %s (%s -> %s)\n", resp.SecretID, resp.OldStatus, resp.NewStatus)
		return nil
	}
}

// newSecretsGetPrefixCommand creates the secrets get-prefix command.
func newSecretsGetPrefixCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-prefix <prefix>",
		Short: "Get all secrets by path prefix",
		Long: `Retrieve all secrets matching a path prefix.

This is useful for bulk retrieval of related secrets, such as all
secrets for a specific application or environment.

Examples:
  # Get all secrets under apps/myapp/
  quicbaas secrets get-prefix apps/myapp/

  # Get with data included
  quicbaas secrets get-prefix apps/myapp/ --include-data

  # Get with limit
  quicbaas secrets get-prefix apps/myapp/ --limit 50

  # Get as JSON
  quicbaas secrets get-prefix apps/myapp/ --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsGetPrefixCommand,
	}

	cmd.Flags().BoolVar(&secretsIncludeData, "include-data", true, "Include secret data in response")
	cmd.Flags().IntVar(&secretsLimit, "limit", 100, "Maximum number of secrets to return")

	return cmd
}

// runSecretsGetPrefixCommand executes the secrets get-prefix command.
func runSecretsGetPrefixCommand(cmd *cobra.Command, args []string) error {
	prefix := args[0]

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &service.GetSecretsByPrefixRequest{
		Prefix:      prefix,
		IncludeData: secretsIncludeData,
		Limit:       secretsLimit,
	}

	resp, err := executeSecretsGetByPrefix(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	return formatSecretsGetPrefixResponse(resp, secretsOutputFmt, os.Stdout)
}

// newSecretsWatchCommand creates the secrets watch command.
func newSecretsWatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch <prefix>",
		Short: "Watch for secret changes",
		Long: `Watch for real-time changes to secrets matching a path prefix.

This command streams changes (creates, updates, deletes) for secrets
matching the specified prefix. Use Ctrl+C to stop watching.

Examples:
  # Watch all secrets under apps/myapp/
  quicbaas secrets watch apps/myapp/

  # Watch with JSON output (for scripting)
  quicbaas secrets watch apps/myapp/ --output json

  # Watch with timeout (for health checks)
  quicbaas secrets watch apps/myapp/ --timeout 60s`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsWatchCommand,
	}

	cmd.Flags().DurationVar(&secretsWatchTTL, "timeout", 0, "Watch timeout (e.g., 60s, 5m). Default: 0 (indefinite until Ctrl+C)")

	return cmd
}

// runSecretsWatchCommand executes the secrets watch command.
func runSecretsWatchCommand(cmd *cobra.Command, args []string) error {
	prefix := args[0]

	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx := context.Background()
	var cancel context.CancelFunc

	if secretsWatchTTL > 0 {
		ctx, cancel = context.WithTimeout(ctx, secretsWatchTTL)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	req := &service.WatchSecretsRequest{
		Prefix: prefix,
	}

	changes, err := executeSecretsWatch(ctx, client, req)
	if err != nil {
		return formatError(err)
	}

	fmt.Fprintf(os.Stdout, "Watching for changes on prefix: %s\n", prefix)
	if secretsWatchTTL > 0 {
		fmt.Fprintf(os.Stdout, "Timeout: %s\n", secretsWatchTTL)
	} else {
		fmt.Fprintf(os.Stdout, "Press Ctrl+C to stop\n")
	}
	fmt.Fprintln(os.Stdout)

	for {
		select {
		case change, ok := <-changes:
			if !ok {
				fmt.Fprintln(os.Stdout, "Watch stream closed")
				return nil
			}
			if err := formatSecretsWatchChange(change, secretsOutputFmt, os.Stdout); err != nil {
				return err
			}
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Fprintln(os.Stdout, "Watch timeout reached")
			}
			return nil
		}
	}
}

// Execution functions for prefix operations

func executeSecretsGetByPrefix(ctx context.Context, client SecretsClient, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.get-prefix", "invalid client type")
	}
	return client.GetSecretsByPrefix(ctx, req)
}

func executeSecretsWatch(ctx context.Context, client SecretsClient, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if client == nil {
		return nil, errors.Newf(errors.ErrInvalidInput, "secrets.watch", "invalid client type")
	}
	return client.WatchSecretsByPrefix(ctx, req)
}

// Formatting functions for prefix operations

func formatSecretsGetPrefixResponse(resp *service.GetSecretsByPrefixResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"secrets":     resp.Secrets,
			"metadata":    resp.Metadata,
			"version":     resp.Version,
			"next_cursor": resp.NextCursor,
			"has_more":    resp.HasMore,
			"total_count": resp.TotalCount,
			"timestamp":   resp.Timestamp.Format(time.RFC3339),
		}
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "PATH\tKEYS\n")
		fmt.Fprintf(tw, "%s\t%s\n",
			strings.Repeat("-", 40),
			strings.Repeat("-", 30))
		for path, data := range resp.Secrets {
			keys := make([]string, 0, len(data))
			for k := range data {
				keys = append(keys, k)
			}
			fmt.Fprintf(tw, "%s\t%s\n", path, strings.Join(keys, ", "))
		}
		fmt.Fprintf(tw, "\nTotal: %d secrets\n", resp.TotalCount)
		if resp.HasMore {
			fmt.Fprintf(tw, "More results available (use --cursor)\n")
		}
		return tw.Flush()

	default:
		for path := range resp.Secrets {
			fmt.Fprintf(w, "%s\n", path)
		}
		fmt.Fprintf(w, "\nTotal: %d secrets\n", resp.TotalCount)
		return nil
	}
}

func formatSecretsWatchChange(change *service.SecretChange, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"path":        change.Path,
			"change_type": string(change.ChangeType),
			"version":     change.Version,
			"timestamp":   change.Timestamp.Format(time.RFC3339),
		}
		if len(change.Data) > 0 {
			output["data"] = change.Data
		}
		if len(change.Metadata) > 0 {
			output["metadata"] = change.Metadata
		}
		encoder := json.NewEncoder(w)
		return encoder.Encode(output)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "[%s] %s\t%s\t(v%d)\n",
			change.Timestamp.Format("15:04:05"),
			strings.ToUpper(string(change.ChangeType)),
			change.Path,
			change.Version)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "[%s] %s %s (v%d)\n",
			change.Timestamp.Format("15:04:05"),
			strings.ToUpper(string(change.ChangeType)),
			change.Path,
			change.Version)
		return nil
	}
}

// newSecretsFetchCommand creates the secrets fetch command.
func newSecretsFetchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch <prefix>",
		Short: "Fetch secrets and write to filesystem",
		Long: `Fetch secrets matching a path prefix and write them to the local filesystem.

This command is designed for use in init containers and sidecar patterns
to make secrets available to applications as files.

Output formats:
  file  - One file per secret key (default)
  env   - Single .env file with all secrets
  json  - Single JSON file with all secrets

Examples:
  # Fetch secrets as individual files
  quicbaas secrets fetch apps/myapp/ --output-dir /secrets

  # Fetch as .env file
  quicbaas secrets fetch apps/myapp/ --output-dir /secrets --format env

  # Fetch as JSON
  quicbaas secrets fetch apps/myapp/ --output-dir /secrets --format json

  # Fetch specific keys only
  quicbaas secrets fetch apps/myapp/ --output-dir /secrets --keys username,password

  # Flatten directory structure
  quicbaas secrets fetch apps/myapp/ --output-dir /secrets --flatten

Docker/Kubernetes usage:
  # As init container
  initContainers:
    - name: fetch-secrets
      image: quicbaas/quicbaas:latest
      command: ["quicbaas", "secrets", "fetch", "apps/myapp/", "--output-dir", "/secrets"]
      volumeMounts:
        - name: secrets
          mountPath: /secrets`,
		Args: cobra.ExactArgs(1),
		RunE: runSecretsFetchCommand,
	}

	cmd.Flags().StringVarP(&fetchOutputDir, "output-dir", "o", "/secrets", "Output directory for secrets")
	cmd.Flags().StringVarP(&fetchFormat, "format", "f", "file", "Output format (file, env, json)")
	cmd.Flags().BoolVar(&fetchCreateDirs, "create-dirs", true, "Create output directory if it doesn't exist")
	cmd.Flags().Uint32Var(&fetchFileMode, "file-mode", 0o600, "File permission mode (default: 0600)")
	cmd.Flags().Uint32Var(&fetchDirMode, "dir-mode", 0o700, "Directory permission mode (default: 0700)")
	cmd.Flags().BoolVar(&fetchFlattenKeys, "flatten", false, "Flatten keys to output directory root")
	cmd.Flags().BoolVar(&fetchStripPrefix, "strip-prefix", true, "Strip prefix from secret paths")
	cmd.Flags().StringSliceVar(&fetchKeys, "keys", nil, "Only fetch specific keys (comma-separated)")

	return cmd
}

// secretsClientAdapter adapts SecretsClient to clisecrets.SecretsProvider.
type secretsClientAdapter struct {
	client SecretsClient
	limit  int
}

// GetSecretsByPrefix implements clisecrets.SecretsProvider.
func (a *secretsClientAdapter) GetSecretsByPrefix(ctx context.Context, prefix string) ([]clisecrets.SecretData, error) {
	req := &service.GetSecretsByPrefixRequest{
		Prefix:      prefix,
		IncludeData: true,
		Limit:       a.limit,
	}

	resp, err := a.client.GetSecretsByPrefix(ctx, req)
	if err != nil {
		return nil, err
	}

	secrets := make([]clisecrets.SecretData, 0, len(resp.Secrets))
	for path, data := range resp.Secrets {
		// Convert []byte values to strings
		stringData := make(map[string]string, len(data))
		for k, v := range data {
			stringData[k] = string(v)
		}
		secrets = append(secrets, clisecrets.SecretData{
			Path: path,
			Data: stringData,
		})
	}

	return secrets, nil
}

// runSecretsFetchCommand executes the secrets fetch command.
func runSecretsFetchCommand(cmd *cobra.Command, args []string) error {
	prefix := args[0]

	// Parse output format
	outputFormat, err := clisecrets.ParseOutputFormat(fetchFormat)
	if err != nil {
		return err
	}

	// Create QuicBaaS client
	client, err := secretsClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	// Create adapter for SecretsProvider interface
	adapter := &secretsClientAdapter{
		client: client,
		limit:  1000, // High limit for fetch operations
	}

	// Build fetch config
	config := clisecrets.FetchConfig{
		Prefix:      prefix,
		OutputDir:   fetchOutputDir,
		Format:      outputFormat,
		CreateDirs:  fetchCreateDirs,
		FileMode:    os.FileMode(fetchFileMode),
		DirMode:     os.FileMode(fetchDirMode),
		FlattenKeys: fetchFlattenKeys,
		StripPrefix: fetchStripPrefix,
		Keys:        fetchKeys,
	}

	// Create fetcher
	fetcher, err := clisecrets.NewFetcher(adapter, config)
	if err != nil {
		return err
	}

	// Execute fetch with timeout
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	result, err := fetcher.Fetch(ctx)
	if err != nil {
		return formatError(err)
	}

	// Display result
	return formatFetchResult(result, secretsOutputFmt, os.Stdout)
}

// formatFetchResult formats the fetch result for display.
func formatFetchResult(result *clisecrets.FetchResult, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Secrets fetched:\t%d\n", result.SecretsCount)
		fmt.Fprintf(tw, "Keys written:\t%d\n", result.KeysCount)
		fmt.Fprintf(tw, "Files written:\t%d\n", len(result.FilesWritten))
		if len(result.FilesWritten) > 0 {
			fmt.Fprintf(tw, "\nFiles:\n")
			for _, f := range result.FilesWritten {
				fmt.Fprintf(tw, "  %s\n", f)
			}
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Fetched %d secrets (%d keys) to %d files\n",
			result.SecretsCount, result.KeysCount, len(result.FilesWritten))
		return nil
	}
}

// Kubernetes service account token paths.
const (
	k8sServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	k8sServiceAccountCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// createSecretsGRPCClient creates a gRPC client for secrets operations with mTLS and K8s auth support.
func createSecretsGRPCClient() (*grpc.Client, error) {
	// Create logger for client
	logConfig := logger.LogConfig{
		Level:  "warn",
		Format: "json",
		Output: "stderr",
	}

	log, err := logger.New(logConfig)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrConfiguration,
			"createSecretsGRPCClient",
			"failed to create logger",
		)
	}

	// Build client options
	opts := []grpc.ClientOption{
		grpc.WithLogger(log),
		grpc.WithTimeout(globalTimeout),
	}

	// Handle authentication mode
	switch secretsAuthMode {
	case "k8s":
		// Kubernetes service account authentication
		token, err := getK8sServiceAccountToken()
		if err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrConfiguration,
				"createSecretsGRPCClient",
				"failed to read Kubernetes service account token",
			)
		}
		// Set token as metadata for authentication
		opts = append(opts, grpc.WithMetadata("authorization", "Bearer "+token))

		// Use K8s CA if TLS is enabled and no custom CA is provided
		if secretsTLSEnabled && secretsTLSCA == "" {
			if _, err := os.Stat(k8sServiceAccountCAPath); err == nil {
				secretsTLSCA = k8sServiceAccountCAPath
			}
		}
	case "token", "":
		// Token-based authentication (default)
		// Token is read from QUICBAAS_TOKEN environment variable by the client
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createSecretsGRPCClient",
			"unsupported auth mode: %s (supported: token, k8s)", secretsAuthMode,
		)
	}

	// Add TLS configuration
	switch {
	case secretsTLSEnabled && secretsTLSCert != "" && secretsTLSKey != "":
		// mTLS: client certificate and key provided
		opts = append(opts, grpc.WithTLS(secretsTLSCert, secretsTLSKey, secretsTLSCA))
	case secretsTLSEnabled && secretsTLSCA != "":
		// Server-side TLS only: just verify server with CA
		opt, err := grpcCAOnlyTLSOption(secretsTLSCA)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrConfiguration,
				"createSecretsGRPCClient", "failed to load --tls-ca")
		}
		opts = append(opts, opt)
	default:
		// QuicBaaS bans insecure TLS — explicit CA trust required.
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createSecretsGRPCClient",
			"TLS is required for gRPC connections (use --tls-cert, --tls-key, and --tls-ca)",
		)
	}

	client, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"createSecretsGRPCClient",
			"failed to connect to server at %s", address,
		)
	}

	return client, nil
}

// getK8sServiceAccountToken reads the Kubernetes service account token from the standard location.
func getK8sServiceAccountToken() (string, error) {
	tokenBytes, err := os.ReadFile(k8sServiceAccountTokenPath)
	if err != nil {
		return "", errors.Wrapf(
			err,
			errors.ErrNotFound,
			"getK8sServiceAccountToken",
			"failed to read token from %s", k8sServiceAccountTokenPath,
		)
	}
	return strings.TrimSpace(string(tokenBytes)), nil
}
