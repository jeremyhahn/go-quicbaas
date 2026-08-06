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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
)

var (
	// Data command flags.
	timeout      time.Duration
	outputFormat string
	consistency  string

	// TLS flags.
	tlsEnabled bool
	tlsCert    string
	tlsKey     string
	tlsCA      string

	// Connection flags.
	maxRetries   int
	retryBackoff time.Duration

	// Put command flags.
	jsonInput bool

	// List command flags.
	listLimit  int
	listCursor string

	// Query command flags.
	queryIndex string
	queryValue string
	queryLimit int

	// Delete command flags.
	deleteForce bool
)

// newDataCommands creates the data manipulation command group.
func newDataCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data",
		Short: "Data manipulation commands (put, get, delete, list, query)",
		Long: `Data manipulation commands for interacting with QuicBaaS entities.

These commands allow you to perform CRUD operations on entities stored in QuicBaaS.
All commands connect to a running QuicBaaS server using the configured transport.`,
	}

	// Add persistent flags for all data commands
	// Note: --address and --transport are global flags defined in main.go
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout")
	cmd.PersistentFlags().StringVar(&outputFormat, "output", "table", "Output format (json, yaml, table, raw)")
	cmd.PersistentFlags().StringVar(&consistency, "consistency", "linearizable", "Consistency level (strong, linearizable, eventual, local)")

	// TLS flags are registered as root-level persistent flags in main.go

	// Connection flags
	cmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retry attempts")
	cmd.PersistentFlags().DurationVar(&retryBackoff, "retry-backoff", 100*time.Millisecond, "Initial retry backoff duration")

	// Add subcommands
	cmd.AddCommand(newPutCommand())
	cmd.AddCommand(newGetCommand())
	cmd.AddCommand(newDeleteCommand())
	cmd.AddCommand(newListCommand())
	cmd.AddCommand(newQueryCommand())

	return cmd
}

// newPutCommand creates the put command.
func newPutCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "put <type> <key> <value>",
		Short: "Store a key-value pair",
		Long: `Store a key-value pair in QuicBaaS.

The value can be provided as a string argument or as JSON using the --json flag.

Examples:
  # Store a simple string value
  quicbaas data put user john '{"name":"John Doe","email":"john@example.com"}'

  # Store JSON data
  quicbaas data put user john --json < user.json

  # Store with strong consistency
  quicbaas data put user john '{"name":"John"}' --consistency strong

  # Store with TLS
  quicbaas data put user john '{"name":"John"}' --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt`,
		Args: cobra.RangeArgs(2, 3),
		RunE: runPutCommand,
	}

	cmd.Flags().BoolVar(&jsonInput, "json", false, "Parse value as JSON")

	return cmd
}

// runPutCommand executes the put command.
func runPutCommand(cmd *cobra.Command, args []string) error {
	entityType := args[0]
	key := args[1]

	var value []byte

	// Get value from args or stdin
	if len(args) == 3 {
		value = []byte(args[2])
	} else {
		// Read from stdin
		scanner := bufio.NewScanner(os.Stdin)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "put", "failed to read stdin")
		}
		value = []byte(strings.Join(lines, "\n"))
	}

	// Validate JSON if flag is set
	if jsonInput {
		var jsonData interface{}
		if err := json.Unmarshal(value, &jsonData); err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "put", "invalid JSON")
		}
	}

	// Create SDK client
	client, err := sdkClientFactory()
	if err != nil {
		return err
	}
	defer func() { _ = closeSDKClient() }()

	// Parse consistency level
	consistencyLevel, err := parseServiceConsistency(consistency)
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute put request using SDK
	req := &service.PutRequest{
		EntityType:  entityType,
		ID:          ParseKeyToID(key),
		Data:        value,
		Consistency: consistencyLevel,
	}

	resp, err := client.KV().Put(ctx, req)
	if err != nil {
		return formatError(err)
	}

	// Format and display response using CLI formatter
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	// Convert SDK response to CLI response for formatting
	cliResp := &cli.PutResponse{
		Key:       key,
		Success:   true,
		Timestamp: time.Unix(0, resp.Timestamp),
	}

	formatter := cli.NewFormatter(true)
	return formatter.FormatPutResponse(cliResp, format, os.Stdout)
}

// newGetCommand creates the get command.
func newGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <type> <key>",
		Short: "Retrieve a value by key",
		Long: `Retrieve a value by key from QuicBaaS.

Examples:
  # Get a value as table format (default)
  quicbaas data get user john

  # Get a value as JSON
  quicbaas data get user john --output json

  # Get raw value (useful for piping)
  quicbaas data get user john --output raw > user.json

  # Get with strong consistency
  quicbaas data get user john --consistency strong

  # Get with TLS
  quicbaas data get user john --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt`,
		Args: cobra.ExactArgs(2),
		RunE: runGetCommand,
	}

	return cmd
}

// runGetCommand executes the get command.
func runGetCommand(cmd *cobra.Command, args []string) error {
	entityType := args[0]
	key := args[1]

	// Create SDK client
	client, err := sdkClientFactory()
	if err != nil {
		return err
	}
	defer func() { _ = closeSDKClient() }()

	// Parse consistency level
	consistencyLevel, err := parseServiceConsistency(consistency)
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute get request using SDK
	req := &service.GetRequest{
		EntityType:  entityType,
		ID:          ParseKeyToID(key),
		Consistency: consistencyLevel,
	}

	resp, err := client.KV().Get(ctx, req)
	if err != nil {
		return formatError(err)
	}

	// Format and display response using CLI formatter
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	// Convert SDK response to CLI response for formatting
	cliResp := &cli.GetResponse{
		Key:       key,
		Value:     resp.Data,
		Found:     resp.Found,
		Timestamp: time.Unix(0, resp.Timestamp),
	}

	formatter := cli.NewFormatter(true)
	return formatter.FormatGetResponse(cliResp, format, os.Stdout)
}

// newDeleteCommand creates the delete command.
func newDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <type> <key>",
		Short: "Delete a key-value pair",
		Long: `Delete a key-value pair from QuicBaaS.

By default, this command will prompt for confirmation before deleting.
Use --yes to skip the confirmation prompt.

Examples:
  # Delete with confirmation
  quicbaas data delete user john

  # Delete without confirmation
  quicbaas data delete user john --yes

  # Delete with strong consistency
  quicbaas data delete user john --consistency strong --yes

  # Delete with TLS
  quicbaas data delete user john --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt --yes`,
		Args: cobra.ExactArgs(2),
		RunE: runDeleteCommand,
	}

	cmd.Flags().BoolVarP(&deleteForce, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// runDeleteCommand executes the delete command.
func runDeleteCommand(cmd *cobra.Command, args []string) error {
	entityType := args[0]
	key := args[1]

	// Confirmation prompt unless --yes is specified
	if !deleteForce {
		cli.PrintWarning(os.Stdout, fmt.Sprintf("About to delete key '%s' from entity type '%s'", key, entityType))
		reader := bufio.NewReader(os.Stdin)
		cli.PrintInfo(os.Stdout, "Are you sure you want to continue? (y/N): ")
		response, err := reader.ReadString('\n')
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "delete", "failed to read confirmation")
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			cli.PrintInfo(os.Stdout, "Delete cancelled")
			return nil
		}
	}

	// Create SDK client
	client, err := sdkClientFactory()
	if err != nil {
		return err
	}
	defer func() { _ = closeSDKClient() }()

	// Parse consistency level
	consistencyLevel, err := parseServiceConsistency(consistency)
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute delete request using SDK
	req := &service.DeleteRequest{
		EntityType:  entityType,
		ID:          ParseKeyToID(key),
		Consistency: consistencyLevel,
	}

	resp, err := client.KV().Delete(ctx, req)
	if err != nil {
		return formatError(err)
	}

	// Format and display response using CLI formatter
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	// Convert SDK response to CLI response for formatting
	cliResp := &cli.DeleteResponse{
		Key:       key,
		Deleted:   resp.Deleted,
		Timestamp: time.Unix(0, resp.Timestamp),
	}

	formatter := cli.NewFormatter(true)
	return formatter.FormatDeleteResponse(cliResp, format, os.Stdout)
}

// newListCommand creates the list command.
func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <type>",
		Short: "List all keys of a type",
		Long: `List all keys of a specific entity type.

Examples:
  # List all users
  quicbaas data list user

  # List with limit
  quicbaas data list user --limit 10

  # List with pagination cursor
  quicbaas data list user --cursor "next_page_token"

  # List as JSON
  quicbaas data list user --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runListCommand,
	}

	cmd.Flags().IntVar(&listLimit, "limit", 100, "Maximum number of keys to return")
	cmd.Flags().StringVar(&listCursor, "cursor", "", "Pagination cursor for next page")

	return cmd
}

// runListCommand executes the list command.
func runListCommand(cmd *cobra.Command, args []string) error {
	entityType := args[0]

	// Create SDK client
	client, err := sdkClientFactory()
	if err != nil {
		return err
	}
	defer func() { _ = closeSDKClient() }()

	// Parse consistency level
	consistencyLevel, err := parseServiceConsistency(consistency)
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute list request using SDK
	req := &service.ListRequest{
		EntityType:  entityType,
		Consistency: consistencyLevel,
		PageSize:    uint64(listLimit),
		PageToken:   listCursor,
	}

	resp, err := client.KV().List(ctx, req)
	if err != nil {
		return formatError(err)
	}

	// Format and display response using CLI formatter
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	// Convert SDK response to CLI response for formatting
	items := make([]cli.ListItem, len(resp.Entities))
	for i, entity := range resp.Entities {
		items[i] = cli.ListItem{
			Key:       FormatIDToKey(entity.ID),
			Value:     entity.Data,
			Timestamp: time.Unix(0, entity.Timestamp),
		}
	}

	cliResp := &cli.ListResponse{
		Items:      items,
		NextCursor: resp.NextPageToken,
		Total:      len(items),
	}

	formatter := cli.NewFormatter(true)
	return formatter.FormatListResponse(cliResp, format, os.Stdout)
}

// newQueryCommand creates the query command.
func newQueryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query <type>",
		Short: "Query entities by index",
		Long: `Query entities by a secondary index.

Examples:
  # Query users by email index
  quicbaas data query user --index email --value "john@example.com"

  # Query with limit
  quicbaas data query user --index status --value active --limit 50

  # Query as JSON
  quicbaas data query user --index email --value "john@example.com" --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runQueryCommand,
	}

	cmd.Flags().StringVar(&queryIndex, "index", "", "Index field to query")
	cmd.Flags().StringVar(&queryValue, "value", "", "Value to search for")
	cmd.Flags().IntVar(&queryLimit, "limit", 100, "Maximum number of results")

	return cmd
}

// runQueryCommand executes the query command.
func runQueryCommand(cmd *cobra.Command, args []string) error {
	entityType := args[0]

	if queryIndex == "" {
		return errors.New(errors.ErrInvalidInput, "query", "index field is required (use --index)")
	}
	if queryValue == "" {
		return errors.New(errors.ErrInvalidInput, "query", "query value is required (use --value)")
	}

	// Create SDK client
	client, err := sdkClientFactory()
	if err != nil {
		return err
	}
	defer func() { _ = closeSDKClient() }()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute query request using SDK query client
	req := &query.QueryRequest{
		EntityType: entityType,
		Index:      queryIndex,
		Value:      queryValue,
		Operator:   query.OpEqual,
		Limit:      queryLimit,
	}

	resp, err := client.Query().Execute(ctx, req)
	if err != nil {
		return formatError(err)
	}

	// Format and display response using CLI formatter
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	// Convert SDK response to CLI response for formatting
	items := make([]cli.QueryItem, len(resp.Results))
	for i, result := range resp.Results {
		items[i] = cli.QueryItem{
			Key:   result.Key,
			Value: result.Value,
		}
	}

	cliResp := &cli.QueryResponse{
		Items: items,
		Total: len(items),
	}

	formatter := cli.NewFormatter(true)
	return formatter.FormatQueryResponse(cliResp, format, os.Stdout)
}

// parseServiceConsistency parses a consistency level string to service type.
func parseServiceConsistency(s string) (service.ConsistencyLevel, error) {
	switch strings.ToLower(s) {
	case "strong":
		return service.ConsistencyLevelStrong, nil
	case "linearizable", "":
		return service.ConsistencyLevelLinearizable, nil // Default
	case "eventual", "stale", "local":
		return service.ConsistencyLevelEventual, nil
	default:
		return service.ConsistencyLevelLinearizable, errors.Newf(
			errors.ErrInvalidInput,
			"parseConsistency",
			"unknown consistency level: %s (valid: strong, linearizable, eventual, local)", s,
		)
	}
}

// formatError formats an error for CLI output. A nil error passes through
// untouched — success paths commonly `return formatError(client.X(...))`,
// and printing a red "Error: <nil>" banner on success is wrong.
func formatError(err error) error {
	if err == nil {
		return nil
	}
	formatter := cli.NewFormatter(true)
	formatter.FormatError(err, os.Stderr)
	return err
}
