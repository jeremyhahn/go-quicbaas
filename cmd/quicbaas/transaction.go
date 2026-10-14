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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// defaultTransactionRESTAddress is the --rest default: the daemon's
// defaults-layer REST port on the local host. The daemon serves REST over
// TLS only, so the scheme is https.
var defaultTransactionRESTAddress = "https://localhost:" + strconv.Itoa(config.DefaultRESTPort)

var (
	// Transaction builder state (persists across subcommands).
	txnCompare []rest.CompareCondition
	txnSuccess []rest.TransactionOp
	txnFailure []rest.TransactionOp

	// Transaction flags.
	restAddress string
	restTLS     bool
)

// newTransactionCommand creates the transaction command group.
func newTransactionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "txn",
		Short: "Transaction commands (compare-and-swap operations)",
		Long: `Transaction commands for performing atomic compare-and-swap operations.

Transactions in QuicBaaS allow you to:
  1. Specify compare conditions (predicates)
  2. Define success operations (executed if all comparisons succeed)
  3. Define failure operations (executed if any comparison fails)
  4. Execute the transaction atomically

Examples:
  # Check if user exists, update if so, create if not
  quicbaas txn compare user 123 exists equal
  quicbaas txn success put user 123 '{"name":"John Updated"}'
  quicbaas txn failure put user 123 '{"name":"John New"}'
  quicbaas txn execute

  # Conditional update based on version
  quicbaas txn compare user 123 version equal 1
  quicbaas txn success put user 123 '{"name":"Updated"}'
  quicbaas txn execute

  # Read-modify-write pattern
  quicbaas txn success get user 123
  quicbaas txn success put user 123 '{"name":"Modified","updated_at":"2025-10-12"}'
  quicbaas txn execute`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// This hook replaces the root's, so run the root initialisation
			// that resolves --rest from flag, env and config file.
			if err := initCLI(cmd); err != nil {
				return err
			}
			// Reset transaction state on each top-level invocation
			if cmd.Parent() == nil || cmd.Parent().Name() == "quicbaas" {
				resetTransaction()
			}
			return nil
		},
	}

	// Add persistent flags
	cmd.PersistentFlags().StringVar(&restAddress, "rest", defaultTransactionRESTAddress, "REST API address (URL or host:port)")
	cmd.PersistentFlags().BoolVar(&restTLS, "tls", false, "Use HTTPS")

	// Add subcommands
	cmd.AddCommand(newTxnCompareCommand())
	cmd.AddCommand(newTxnSuccessCommand())
	cmd.AddCommand(newTxnFailureCommand())
	cmd.AddCommand(newTxnExecuteCommand())
	cmd.AddCommand(newTxnResetCommand())
	cmd.AddCommand(newTxnShowCommand())

	return cmd
}

// resetTransaction clears all transaction state.
func resetTransaction() {
	txnCompare = nil
	txnSuccess = nil
	txnFailure = nil
}

// newTxnCompareCommand creates the compare condition command.
func newTxnCompareCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compare <entity-type> <id> <target> <operator> [value]",
		Short: "Add a comparison condition to the transaction",
		Long: `Add a comparison condition to the transaction.

Targets:
  version - Compare entity version number
  value   - Compare entity value (requires JSON value argument)
  exists  - Check if entity exists

Operators:
  equal, not_equal, greater, less, greater_equal, less_equal

Examples:
  # Check if user 123 exists
  quicbaas txn compare user 123 exists equal

  # Check if user 123 does not exist
  quicbaas txn compare user 123 exists not_equal

  # Check if user 123 version equals 1
  quicbaas txn compare user 123 version equal 1

  # Check if user 123 value matches JSON
  quicbaas txn compare user 123 value equal '{"name":"John"}'`,
		Args: cobra.RangeArgs(4, 5),
		RunE: runTxnCompareCommand,
	}

	return cmd
}

// runTxnCompareCommand executes the compare command.
func runTxnCompareCommand(cmd *cobra.Command, args []string) error {
	entityType := args[0]
	idStr := args[1]
	target := args[2]
	operator := args[3]

	// Parse ID
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "txn compare", "invalid ID: %s", idStr)
	}

	// Create compare condition
	cmp := rest.CompareCondition{
		EntityType: entityType,
		ID:         id,
		Target:     rest.CompareTarget(target),
		Operator:   rest.CompareOperator(operator),
	}

	// Parse value or version if provided
	if len(args) == 5 {
		valueStr := args[4]
		switch cmp.Target {
		case rest.CompareTargetVersion:
			version, err := strconv.ParseInt(valueStr, 10, 64)
			if err != nil {
				return errors.Wrapf(err, errors.ErrInvalidInput, "txn compare", "invalid version: %s", valueStr)
			}
			cmp.Version = version

		case rest.CompareTargetValue:
			// Validate JSON
			var jsonData interface{}
			if err := json.Unmarshal([]byte(valueStr), &jsonData); err != nil {
				return errors.Wrapf(err, errors.ErrInvalidInput, "txn compare", "invalid JSON value")
			}
			cmp.Value = json.RawMessage(valueStr)

		default:
			return errors.Newf(errors.ErrInvalidInput, "txn compare", "value not allowed for target: %s", target)
		}
	}

	// Add to transaction
	txnCompare = append(txnCompare, cmp)

	fmt.Printf("Added comparison: %s %d %s %s", entityType, id, target, operator)
	if len(args) == 5 {
		if cmp.Target == rest.CompareTargetVersion {
			fmt.Printf(" %d", cmp.Version)
		} else {
			fmt.Printf(" <json>")
		}
	}
	fmt.Println()

	return nil
}

// newTxnSuccessCommand creates the success operation command.
func newTxnSuccessCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "success <operation> <entity-type> <id> [data]",
		Short: "Add a success operation to the transaction",
		Long: `Add an operation to execute if all comparisons succeed.

Operations:
  put    - Create or update entity (requires JSON data argument)
  delete - Delete entity
  get    - Retrieve entity

Examples:
  # Add a put operation
  quicbaas txn success put user 123 '{"name":"John Updated"}'

  # Add a delete operation
  quicbaas txn success delete user 123

  # Add a get operation
  quicbaas txn success get user 123`,
		Args: cobra.RangeArgs(3, 4),
		RunE: runTxnSuccessCommand,
	}

	return cmd
}

// runTxnSuccessCommand executes the success command.
func runTxnSuccessCommand(cmd *cobra.Command, args []string) error {
	operation := args[0]
	entityType := args[1]
	idStr := args[2]

	// Parse ID
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "txn success", "invalid ID: %s", idStr)
	}

	// Create operation
	op := rest.TransactionOp{
		Type:       rest.TransactionOpType(operation),
		EntityType: entityType,
		ID:         id,
	}

	// Parse data for put operations
	if len(args) == 4 {
		data := args[3]
		if op.Type != rest.TxnOpPut {
			return errors.Newf(errors.ErrInvalidInput, "txn success", "data only allowed for put operations")
		}

		// Validate JSON
		var jsonData interface{}
		if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "txn success", "invalid JSON data")
		}
		op.Data = json.RawMessage(data)
	} else if op.Type == rest.TxnOpPut {
		return errors.Newf(errors.ErrInvalidInput, "txn success", "put operation requires data argument")
	}

	// Add to transaction
	txnSuccess = append(txnSuccess, op)

	fmt.Printf("Added success operation: %s %s %d", operation, entityType, id)
	if op.Type == rest.TxnOpPut {
		fmt.Print(" <json>")
	}
	fmt.Println()

	return nil
}

// newTxnFailureCommand creates the failure operation command.
func newTxnFailureCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "failure <operation> <entity-type> <id> [data]",
		Short: "Add a failure operation to the transaction",
		Long: `Add an operation to execute if any comparison fails.

Operations:
  put    - Create or update entity (requires JSON data argument)
  delete - Delete entity
  get    - Retrieve entity

Examples:
  # Add a put operation
  quicbaas txn failure put user 123 '{"name":"John New"}'

  # Add a delete operation
  quicbaas txn failure delete user 123

  # Add a get operation
  quicbaas txn failure get user 123`,
		Args: cobra.RangeArgs(3, 4),
		RunE: runTxnFailureCommand,
	}

	return cmd
}

// runTxnFailureCommand executes the failure command.
func runTxnFailureCommand(cmd *cobra.Command, args []string) error {
	operation := args[0]
	entityType := args[1]
	idStr := args[2]

	// Parse ID
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "txn failure", "invalid ID: %s", idStr)
	}

	// Create operation
	op := rest.TransactionOp{
		Type:       rest.TransactionOpType(operation),
		EntityType: entityType,
		ID:         id,
	}

	// Parse data for put operations
	if len(args) == 4 {
		data := args[3]
		if op.Type != rest.TxnOpPut {
			return errors.Newf(errors.ErrInvalidInput, "txn failure", "data only allowed for put operations")
		}

		// Validate JSON
		var jsonData interface{}
		if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "txn failure", "invalid JSON data")
		}
		op.Data = json.RawMessage(data)
	} else if op.Type == rest.TxnOpPut {
		return errors.Newf(errors.ErrInvalidInput, "txn failure", "put operation requires data argument")
	}

	// Add to transaction
	txnFailure = append(txnFailure, op)

	fmt.Printf("Added failure operation: %s %s %d", operation, entityType, id)
	if op.Type == rest.TxnOpPut {
		fmt.Print(" <json>")
	}
	fmt.Println()

	return nil
}

// newTxnExecuteCommand creates the execute command.
func newTxnExecuteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute",
		Short: "Execute the transaction",
		Long: `Execute the transaction with all configured comparisons and operations.

The transaction will:
  1. Evaluate all comparison conditions
  2. If all comparisons succeed, execute success operations
  3. If any comparison fails, execute failure operations
  4. Return the results

After execution, the transaction state is automatically reset.

Examples:
  # Execute the configured transaction
  quicbaas txn execute

  # Execute with JSON output
  quicbaas txn execute --output json`,
		Args: cobra.NoArgs,
		RunE: runTxnExecuteCommand,
	}

	return cmd
}

// runTxnExecuteCommand executes the transaction.
func runTxnExecuteCommand(cmd *cobra.Command, args []string) error {
	// Validate transaction has operations
	if len(txnSuccess) == 0 && len(txnFailure) == 0 {
		return errors.Newf(errors.ErrInvalidInput, "txn execute", "transaction has no operations. Use 'quicbaas txn success' or 'quicbaas txn failure' to add operations.")
	}

	// Build transaction request
	txnReq := rest.TransactionRequest{
		Compare: txnCompare,
		Success: txnSuccess,
		Failure: txnFailure,
	}

	// Validate request
	if err := txnReq.Validate(); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "txn execute", "invalid transaction")
	}

	// Marshal request
	reqBody, err := json.Marshal(txnReq)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "txn execute", "failed to marshal request")
	}

	// Build REST URL
	scheme := "http"
	if restTLS {
		scheme = "https"
	}
	url := fmt.Sprintf("%s/v1/transaction", strings.TrimSuffix(restAddress, "/"))
	if !strings.HasPrefix(restAddress, "http://") && !strings.HasPrefix(restAddress, "https://") {
		url = fmt.Sprintf("%s://%s/v1/transaction", scheme, restAddress)
	}

	// Create HTTP client. QuicBaaS bans insecure TLS — when HTTPS is in use,
	// trust the CA via --tls-ca.
	client := &http.Client{
		Timeout: globalTimeout,
	}
	if tlsCfg, tlsErr := buildTLSConfig(); tlsErr != nil {
		return errors.Wrapf(tlsErr, errors.ErrConfiguration, "txn execute", "failed to build TLS config")
	} else if tlsCfg != nil {
		tlsCfg.MinVersion = tls.VersionTLS13
		client.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	}

	// Create request
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "txn execute", "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrapf(err, errors.ErrUnavailable, "txn execute", "failed to execute request")
	}
	defer resp.Body.Close()

	// Read response
	var txnResp rest.TransactionResponse
	if err := json.NewDecoder(resp.Body).Decode(&txnResp); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "txn execute", "failed to decode response")
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return errors.Newf(errors.ErrInternal, "txn execute", "transaction failed: HTTP %d - %s", resp.StatusCode, txnResp.Message)
	}

	// Display results
	fmt.Println("Transaction executed successfully")
	fmt.Printf("Succeeded: %v\n", txnResp.Succeeded)
	fmt.Printf("Timestamp: %s\n", txnResp.Timestamp.Format(time.RFC3339))
	fmt.Printf("Message: %s\n", txnResp.Message)
	fmt.Printf("\nOperations executed: %d\n", len(txnResp.Responses))

	for i, opResp := range txnResp.Responses {
		fmt.Printf("\n[%d] %s %s/%d\n", i+1, opResp.Type, opResp.EntityType, opResp.ID)
		fmt.Printf("    Success: %v\n", opResp.Success)
		if opResp.Error != "" {
			fmt.Printf("    Error: %s\n", opResp.Error)
		}
		if len(opResp.Data) > 0 {
			fmt.Printf("    Data: %s\n", string(opResp.Data))
		}
	}

	// Reset transaction state
	resetTransaction()

	return nil
}

// newTxnResetCommand creates the reset command.
func newTxnResetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset the transaction (clear all conditions and operations)",
		Long: `Reset the transaction, clearing all comparison conditions and operations.

This is useful if you want to start over without executing the current transaction.

Examples:
  # Reset the transaction
  quicbaas txn reset`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resetTransaction()
			fmt.Fprintln(cmd.OutOrStdout(), "Transaction reset")
			return nil
		},
	}

	return cmd
}

// newTxnShowCommand creates the show command.
func newTxnShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the current transaction state",
		Long: `Display the current transaction state, including all comparison conditions and operations.

Examples:
  # Show current transaction
  quicbaas txn show`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Transaction State:")
			fmt.Fprintf(out, "\nComparisons: %d\n", len(txnCompare))
			for i, cmp := range txnCompare {
				fmt.Fprintf(out, "  [%d] %s/%d %s %s", i+1, cmp.EntityType, cmp.ID, cmp.Target, cmp.Operator)
				if cmp.Target == rest.CompareTargetVersion {
					fmt.Fprintf(out, " %d", cmp.Version)
				} else if len(cmp.Value) > 0 {
					fmt.Fprint(out, " <json>")
				}
				fmt.Fprintln(out)
			}

			fmt.Fprintf(out, "\nSuccess Operations: %d\n", len(txnSuccess))
			for i, op := range txnSuccess {
				fmt.Fprintf(out, "  [%d] %s %s/%d", i+1, op.Type, op.EntityType, op.ID)
				if len(op.Data) > 0 {
					fmt.Fprint(out, " <json>")
				}
				fmt.Fprintln(out)
			}

			fmt.Fprintf(out, "\nFailure Operations: %d\n", len(txnFailure))
			for i, op := range txnFailure {
				fmt.Fprintf(out, "  [%d] %s %s/%d", i+1, op.Type, op.EntityType, op.ID)
				if len(op.Data) > 0 {
					fmt.Fprint(out, " <json>")
				}
				fmt.Fprintln(out)
			}

			return nil
		},
	}

	return cmd
}
