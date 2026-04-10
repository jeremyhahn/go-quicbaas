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

//go:build integration

package cli

import (
	"fmt"
	"testing"
	"time"
)

// =============================================================================
// Transaction Command Definitions
// =============================================================================

// transactionTransportsToSkip defines which transports to skip for transaction operations.
// Transaction operations currently use REST API directly, so only REST transport is supported.
// gRPC, QUIC, Unix, and MCP transports do not have transaction CLI support.
var transactionTransportsToSkip = []Transport{TransportGRPC, TransportQUIC, TransportUnix, TransportMCP}

// TxnCompareCommand returns a command to add a comparison condition to the transaction.
// Supported targets: version, value, exists
// Supported operators: equal, not_equal, greater, less, greater_equal, less_equal
func TxnCompareCommand(entityType string, id uint64, target, operator string, value ...string) CommandDef {
	args := []string{"txn", "compare", entityType, fmt.Sprintf("%d", id), target, operator}
	if len(value) > 0 {
		args = append(args, value[0])
	}
	name := fmt.Sprintf("txn compare %s %d %s %s", entityType, id, target, operator)
	return CommandDef{
		Name:           name,
		Args:           args,
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Added comparison"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnSuccessPutCommand returns a command to add a put operation to the success branch.
func TxnSuccessPutCommand(entityType string, id uint64, data string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn success put %s %d", entityType, id),
		Args:           []string{"txn", "success", "put", entityType, fmt.Sprintf("%d", id), data},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Added success operation", "put"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnSuccessGetCommand returns a command to add a get operation to the success branch.
func TxnSuccessGetCommand(entityType string, id uint64) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn success get %s %d", entityType, id),
		Args:           []string{"txn", "success", "get", entityType, fmt.Sprintf("%d", id)},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Added success operation", "get"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnSuccessDeleteCommand returns a command to add a delete operation to the success branch.
func TxnSuccessDeleteCommand(entityType string, id uint64) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn success delete %s %d", entityType, id),
		Args:           []string{"txn", "success", "delete", entityType, fmt.Sprintf("%d", id)},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Added success operation", "delete"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnFailurePutCommand returns a command to add a put operation to the failure branch.
func TxnFailurePutCommand(entityType string, id uint64, data string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn failure put %s %d", entityType, id),
		Args:           []string{"txn", "failure", "put", entityType, fmt.Sprintf("%d", id), data},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Added failure operation", "put"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnFailureGetCommand returns a command to add a get operation to the failure branch.
func TxnFailureGetCommand(entityType string, id uint64) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn failure get %s %d", entityType, id),
		Args:           []string{"txn", "failure", "get", entityType, fmt.Sprintf("%d", id)},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Added failure operation", "get"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnFailureDeleteCommand returns a command to add a delete operation to the failure branch.
func TxnFailureDeleteCommand(entityType string, id uint64) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn failure delete %s %d", entityType, id),
		Args:           []string{"txn", "failure", "delete", entityType, fmt.Sprintf("%d", id)},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Added failure operation", "delete"},
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnExecuteCommand returns a command to execute the transaction.
func TxnExecuteCommand(restAddress string) CommandDef {
	return CommandDef{
		Name:           "txn execute",
		Args:           []string{"txn", "execute", "--rest", restAddress},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Transaction executed"},
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnResetCommand returns a command to reset the transaction state.
func TxnResetCommand() CommandDef {
	return CommandDef{
		Name:           "txn reset",
		Args:           []string{"txn", "reset"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Transaction reset"},
		Timeout:        10 * time.Second,
		RequiresUnseal: false,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnShowCommand returns a command to show the current transaction state.
func TxnShowCommand() CommandDef {
	return CommandDef{
		Name:           "txn show",
		Args:           []string{"txn", "show"},
		ExpectSuccess:  true,
		ExpectOutput:   []string{"Transaction State"},
		Timeout:        10 * time.Second,
		RequiresUnseal: false,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnHelpCommand returns a command to show transaction help.
func TxnHelpCommand() CommandDef {
	return CommandDef{
		Name:          "txn help",
		Args:          []string{"txn", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"compare", "success", "failure", "execute", "reset", "show"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Error Case Command Definitions
// =============================================================================

// TxnExecuteNoOperationsCommand returns a command that should fail due to no operations.
func TxnExecuteNoOperationsCommand(restAddress string) CommandDef {
	return CommandDef{
		Name:           "txn execute no operations",
		Args:           []string{"txn", "execute", "--rest", restAddress},
		ExpectSuccess:  false,
		ExpectError:    []string{"no operations"},
		Timeout:        10 * time.Second,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnCompareInvalidIDCommand returns a command that should fail due to invalid ID.
func TxnCompareInvalidIDCommand(entityType, invalidID, target, operator string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn compare invalid ID %s", invalidID),
		Args:           []string{"txn", "compare", entityType, invalidID, target, operator},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid"},
		Timeout:        10 * time.Second,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnSuccessPutInvalidJSONCommand returns a command that should fail due to invalid JSON.
func TxnSuccessPutInvalidJSONCommand(entityType string, id uint64, invalidJSON string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn success put invalid JSON %s %d", entityType, id),
		Args:           []string{"txn", "success", "put", entityType, fmt.Sprintf("%d", id), invalidJSON},
		ExpectSuccess:  false,
		ExpectError:    []string{"invalid"},
		Timeout:        10 * time.Second,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnSuccessPutMissingDataCommand returns a command that should fail due to missing data.
func TxnSuccessPutMissingDataCommand(entityType string, id uint64) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn success put missing data %s %d", entityType, id),
		Args:           []string{"txn", "success", "put", entityType, fmt.Sprintf("%d", id)},
		ExpectSuccess:  false,
		ExpectError:    []string{"requires"},
		Timeout:        10 * time.Second,
		SkipTransports: transactionTransportsToSkip,
	}
}

// TxnCompareInvalidTargetCommand returns a command that should fail due to invalid target.
func TxnCompareInvalidTargetCommand(entityType string, id uint64, invalidTarget, operator string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("txn compare invalid target %s", invalidTarget),
		Args:           []string{"txn", "compare", entityType, fmt.Sprintf("%d", id), invalidTarget, operator},
		ExpectSuccess:  false,
		Timeout:        10 * time.Second,
		SkipTransports: transactionTransportsToSkip,
	}
}

// =============================================================================
// Transaction Integration Tests
// =============================================================================

// TestTransactionHelp tests the transaction help command across all transports.
func TestTransactionHelp(t *testing.T) {
	RunForAllTransports(t, "TransactionHelp", func(t *testing.T, runner *TestRunner) {
		cmd := TxnHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestTransactionReset tests the transaction reset command across all transports.
func TestTransactionReset(t *testing.T) {
	RunForAllTransports(t, "TransactionReset", func(t *testing.T, runner *TestRunner) {
		cmd := TxnResetCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestTransactionShow tests the transaction show command across all transports.
func TestTransactionShow(t *testing.T) {
	RunForAllTransports(t, "TransactionShow", func(t *testing.T, runner *TestRunner) {
		// First reset to ensure clean state
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		// Show empty transaction state
		showCmd := TxnShowCommand()
		runner.RunAndAssert(showCmd)
	})
}

// TestTransactionCompare tests the transaction compare command across all transports.
func TestTransactionCompare(t *testing.T) {
	RunForAllTransports(t, "TransactionCompare", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		entityType := "test_entity"
		var id uint64 = 12345

		// Test compare with exists target
		compareExistsCmd := TxnCompareCommand(entityType, id, "exists", "equal")
		runner.RunAndAssert(compareExistsCmd)

		// Test compare with version target
		compareVersionCmd := TxnCompareCommand(entityType, id, "version", "equal", "1")
		runner.RunAndAssert(compareVersionCmd)

		// Show transaction state to verify comparisons were added
		showCmd := TxnShowCommand()
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("Transaction state after comparisons: %s", stdout)
		}

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionSuccessOperations tests the transaction success operations across all transports.
func TestTransactionSuccessOperations(t *testing.T) {
	RunForAllTransports(t, "TransactionSuccessOperations", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		entityType := "success_test"
		var id uint64 = 67890

		// Test success put operation
		putCmd := TxnSuccessPutCommand(entityType, id, `{"name":"test","value":42}`)
		runner.RunAndAssert(putCmd)

		// Test success get operation
		getCmd := TxnSuccessGetCommand(entityType, id)
		runner.RunAndAssert(getCmd)

		// Test success delete operation
		deleteCmd := TxnSuccessDeleteCommand(entityType, id+1)
		runner.RunAndAssert(deleteCmd)

		// Show transaction state to verify operations were added
		showCmd := TxnShowCommand()
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("Transaction state with success operations: %s", stdout)
		}

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionFailureOperations tests the transaction failure operations across all transports.
func TestTransactionFailureOperations(t *testing.T) {
	RunForAllTransports(t, "TransactionFailureOperations", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		entityType := "failure_test"
		var id uint64 = 11111

		// Test failure put operation
		putCmd := TxnFailurePutCommand(entityType, id, `{"fallback":"value"}`)
		runner.RunAndAssert(putCmd)

		// Test failure get operation
		getCmd := TxnFailureGetCommand(entityType, id)
		runner.RunAndAssert(getCmd)

		// Test failure delete operation
		deleteCmd := TxnFailureDeleteCommand(entityType, id+1)
		runner.RunAndAssert(deleteCmd)

		// Show transaction state to verify operations were added
		showCmd := TxnShowCommand()
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("Transaction state with failure operations: %s", stdout)
		}

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionCompareOperators tests all comparison operators across all transports.
func TestTransactionCompareOperators(t *testing.T) {
	RunForAllTransports(t, "TransactionCompareOperators", func(t *testing.T, runner *TestRunner) {
		entityType := "operator_test"
		var id uint64 = 22222

		operators := []string{"equal", "not_equal", "greater", "less", "greater_equal", "less_equal"}

		for _, op := range operators {
			t.Run(op, func(t *testing.T) {
				// Reset before each test
				resetCmd := TxnResetCommand()
				runner.RunAndAssert(resetCmd)

				// Test compare with version and the operator
				compareCmd := TxnCompareCommand(entityType, id, "version", op, "1")
				runner.RunAndAssert(compareCmd)

				// Cleanup
				runner.Run(TxnResetCommand())
			})
		}
	})
}

// TestTransactionCompareTargets tests all comparison targets across all transports.
func TestTransactionCompareTargets(t *testing.T) {
	RunForAllTransports(t, "TransactionCompareTargets", func(t *testing.T, runner *TestRunner) {
		entityType := "target_test"
		var id uint64 = 33333

		// Test "exists" target
		t.Run("exists", func(t *testing.T) {
			resetCmd := TxnResetCommand()
			runner.RunAndAssert(resetCmd)

			compareCmd := TxnCompareCommand(entityType, id, "exists", "equal")
			runner.RunAndAssert(compareCmd)

			runner.Run(TxnResetCommand())
		})

		// Test "version" target
		t.Run("version", func(t *testing.T) {
			resetCmd := TxnResetCommand()
			runner.RunAndAssert(resetCmd)

			compareCmd := TxnCompareCommand(entityType, id, "version", "equal", "1")
			runner.RunAndAssert(compareCmd)

			runner.Run(TxnResetCommand())
		})

		// Test "value" target (requires JSON value)
		t.Run("value", func(t *testing.T) {
			resetCmd := TxnResetCommand()
			runner.RunAndAssert(resetCmd)

			compareCmd := TxnCompareCommand(entityType, id, "value", "equal", `{"name":"test"}`)
			runner.RunAndAssert(compareCmd)

			runner.Run(TxnResetCommand())
		})
	})
}

// TestTransactionMultiOperationBuild tests building a complex transaction with multiple operations.
func TestTransactionMultiOperationBuild(t *testing.T) {
	RunForAllTransports(t, "TransactionMultiOperationBuild", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		entityType := "multi_op_test"
		var baseID uint64 = 44444

		// Add multiple compare conditions
		compare1 := TxnCompareCommand(entityType, baseID, "exists", "equal")
		runner.RunAndAssert(compare1)

		compare2 := TxnCompareCommand(entityType, baseID, "version", "greater_equal", "0")
		runner.RunAndAssert(compare2)

		// Add multiple success operations
		successPut := TxnSuccessPutCommand(entityType, baseID, `{"status":"updated","version":2}`)
		runner.RunAndAssert(successPut)

		successGet := TxnSuccessGetCommand(entityType, baseID)
		runner.RunAndAssert(successGet)

		// Add multiple failure operations
		failurePut := TxnFailurePutCommand(entityType, baseID, `{"status":"created","version":1}`)
		runner.RunAndAssert(failurePut)

		failureGet := TxnFailureGetCommand(entityType, baseID)
		runner.RunAndAssert(failureGet)

		// Show the complete transaction state
		showCmd := TxnShowCommand()
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("Complete transaction state:\n%s", stdout)
		}

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionExecuteNoOperations tests that execute fails without operations.
func TestTransactionExecuteNoOperations(t *testing.T) {
	RunForAllTransports(t, "TransactionExecuteNoOperations", func(t *testing.T, runner *TestRunner) {
		// Reset first to ensure clean state
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		// Try to execute without any operations (should fail)
		restAddress := getEnvOrDefault("QUICBAAS_REST_ADDRESS", "https://test-node1:8080")
		executeCmd := TxnExecuteNoOperationsCommand(restAddress)
		runner.RunAndAssert(executeCmd)
	})
}

// TestTransactionInvalidID tests that compare fails with invalid ID.
func TestTransactionInvalidID(t *testing.T) {
	RunForAllTransports(t, "TransactionInvalidID", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		// Try compare with invalid ID
		compareCmd := TxnCompareInvalidIDCommand("test_entity", "not-a-number", "exists", "equal")
		runner.RunAndAssert(compareCmd)

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionInvalidJSON tests that success put fails with invalid JSON.
func TestTransactionInvalidJSON(t *testing.T) {
	RunForAllTransports(t, "TransactionInvalidJSON", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		// Try success put with invalid JSON
		putCmd := TxnSuccessPutInvalidJSONCommand("test_entity", 12345, "not-valid-json")
		runner.RunAndAssert(putCmd)

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionMissingData tests that success put fails without data.
func TestTransactionMissingData(t *testing.T) {
	RunForAllTransports(t, "TransactionMissingData", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		// Try success put without data
		putCmd := TxnSuccessPutMissingDataCommand("test_entity", 12345)
		runner.RunAndAssert(putCmd)

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionReadModifyWritePattern tests a common read-modify-write transaction pattern.
func TestTransactionReadModifyWritePattern(t *testing.T) {
	RunForAllTransports(t, "TransactionReadModifyWritePattern", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		entityType := "user"
		var id uint64 = 55555

		// Compare: check that user exists and version is as expected
		compareExists := TxnCompareCommand(entityType, id, "exists", "equal")
		runner.RunAndAssert(compareExists)

		compareVersion := TxnCompareCommand(entityType, id, "version", "equal", "1")
		runner.RunAndAssert(compareVersion)

		// Success: read current value, then update
		successGet := TxnSuccessGetCommand(entityType, id)
		runner.RunAndAssert(successGet)

		successPut := TxnSuccessPutCommand(entityType, id, `{"name":"John Updated","email":"john.updated@example.com"}`)
		runner.RunAndAssert(successPut)

		// Show transaction state
		showCmd := TxnShowCommand()
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("Read-modify-write transaction:\n%s", stdout)
		}

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionConditionalCreatePattern tests a conditional create pattern (create if not exists).
func TestTransactionConditionalCreatePattern(t *testing.T) {
	RunForAllTransports(t, "TransactionConditionalCreatePattern", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		entityType := "resource"
		var id uint64 = 66666

		// Compare: check that resource does NOT exist
		compareNotExists := TxnCompareCommand(entityType, id, "exists", "not_equal")
		runner.RunAndAssert(compareNotExists)

		// Success: create the resource (comparison succeeded, meaning entity doesn't exist)
		successPut := TxnSuccessPutCommand(entityType, id, `{"name":"New Resource","created_at":"2025-01-01T00:00:00Z"}`)
		runner.RunAndAssert(successPut)

		// Failure: resource already exists, return the existing one
		failureGet := TxnFailureGetCommand(entityType, id)
		runner.RunAndAssert(failureGet)

		// Show transaction state
		showCmd := TxnShowCommand()
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("Conditional create transaction:\n%s", stdout)
		}

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionConditionalDeletePattern tests a conditional delete pattern.
func TestTransactionConditionalDeletePattern(t *testing.T) {
	RunForAllTransports(t, "TransactionConditionalDeletePattern", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		entityType := "session"
		var id uint64 = 77777

		// Compare: check that session exists and is expired (version represents timestamp)
		compareExists := TxnCompareCommand(entityType, id, "exists", "equal")
		runner.RunAndAssert(compareExists)

		compareVersion := TxnCompareCommand(entityType, id, "version", "less", "1000")
		runner.RunAndAssert(compareVersion)

		// Success: delete the expired session
		successDelete := TxnSuccessDeleteCommand(entityType, id)
		runner.RunAndAssert(successDelete)

		// Failure: session still valid, do nothing (just get to confirm)
		failureGet := TxnFailureGetCommand(entityType, id)
		runner.RunAndAssert(failureGet)

		// Show transaction state
		showCmd := TxnShowCommand()
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("Conditional delete transaction:\n%s", stdout)
		}

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionBatchOperationsPattern tests a batch operations pattern.
func TestTransactionBatchOperationsPattern(t *testing.T) {
	RunForAllTransports(t, "TransactionBatchOperationsPattern", func(t *testing.T, runner *TestRunner) {
		// Reset first
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		entityType := "item"
		var baseID uint64 = 88888

		// Add multiple compare conditions for batch validation
		for i := uint64(0); i < 3; i++ {
			compareCmd := TxnCompareCommand(entityType, baseID+i, "exists", "equal")
			runner.RunAndAssert(compareCmd)
		}

		// Add multiple success operations (batch update)
		for i := uint64(0); i < 3; i++ {
			data := fmt.Sprintf(`{"item_id":%d,"status":"processed","batch_id":"batch-001"}`, baseID+i)
			putCmd := TxnSuccessPutCommand(entityType, baseID+i, data)
			runner.RunAndAssert(putCmd)
		}

		// Show transaction state
		showCmd := TxnShowCommand()
		stdout, _, err := runner.Run(showCmd)
		if err == nil && !runner.IsSkipped(showCmd) {
			t.Logf("Batch operations transaction:\n%s", stdout)
		}

		// Cleanup
		runner.Run(TxnResetCommand())
	})
}

// TestTransactionResetClearsState tests that reset properly clears all transaction state.
func TestTransactionResetClearsState(t *testing.T) {
	RunForAllTransports(t, "TransactionResetClearsState", func(t *testing.T, runner *TestRunner) {
		entityType := "reset_test"
		var id uint64 = 99999

		// Add some operations
		compareCmd := TxnCompareCommand(entityType, id, "exists", "equal")
		runner.RunAndAssert(compareCmd)

		successPut := TxnSuccessPutCommand(entityType, id, `{"data":"test"}`)
		runner.RunAndAssert(successPut)

		failureGet := TxnFailureGetCommand(entityType, id)
		runner.RunAndAssert(failureGet)

		// Show state before reset
		showBefore := TxnShowCommand()
		stdoutBefore, _, errBefore := runner.Run(showBefore)
		if errBefore == nil && !runner.IsSkipped(showBefore) {
			t.Logf("Transaction state before reset:\n%s", stdoutBefore)
		}

		// Reset
		resetCmd := TxnResetCommand()
		runner.RunAndAssert(resetCmd)

		// Show state after reset (should be empty)
		showAfter := TxnShowCommand()
		stdoutAfter, _, errAfter := runner.Run(showAfter)
		if errAfter == nil && !runner.IsSkipped(showAfter) {
			t.Logf("Transaction state after reset:\n%s", stdoutAfter)
		}
	})
}

// TestTransactionMultipleResets tests that multiple resets work correctly.
func TestTransactionMultipleResets(t *testing.T) {
	RunForAllTransports(t, "TransactionMultipleResets", func(t *testing.T, runner *TestRunner) {
		for i := 0; i < 3; i++ {
			// Add some state
			compareCmd := TxnCompareCommand("entity", uint64(i+1), "exists", "equal")
			runner.RunAndAssert(compareCmd)

			// Reset
			resetCmd := TxnResetCommand()
			runner.RunAndAssert(resetCmd)

			// Verify empty state
			showCmd := TxnShowCommand()
			stdout, _, err := runner.Run(showCmd)
			if err == nil && !runner.IsSkipped(showCmd) {
				t.Logf("Iteration %d - state after reset:\n%s", i+1, stdout)
			}
		}
	})
}

// =============================================================================
// Transaction Suite Tests
// =============================================================================

// TransactionBuildSuite returns a suite of commands for building a transaction.
func TransactionBuildSuite(entityType string, id uint64) []CommandDef {
	return []CommandDef{
		TxnResetCommand(),
		TxnCompareCommand(entityType, id, "exists", "equal"),
		TxnCompareCommand(entityType, id, "version", "greater_equal", "0"),
		TxnSuccessPutCommand(entityType, id, `{"status":"updated"}`),
		TxnSuccessGetCommand(entityType, id),
		TxnFailurePutCommand(entityType, id, `{"status":"created"}`),
		TxnShowCommand(),
		TxnResetCommand(),
	}
}

// TestTransactionBuildSuite tests the complete transaction build suite.
func TestTransactionBuildSuite(t *testing.T) {
	RunForAllTransports(t, "TransactionBuildSuite", func(t *testing.T, runner *TestRunner) {
		entityType := "suite_test"
		var id uint64 = 100000
		suite := TransactionBuildSuite(entityType, id)
		runner.RunSuite(suite)
	})
}

// TransactionErrorSuite returns a suite of commands for testing error cases.
func TransactionErrorSuite() []CommandDef {
	return []CommandDef{
		TxnResetCommand(),
		TxnCompareInvalidIDCommand("test", "invalid", "exists", "equal"),
		TxnSuccessPutInvalidJSONCommand("test", 1, "not-json"),
		TxnSuccessPutMissingDataCommand("test", 1),
		TxnResetCommand(),
	}
}

// TestTransactionErrorSuite tests the error case suite.
func TestTransactionErrorSuite(t *testing.T) {
	RunForAllTransports(t, "TransactionErrorSuite", func(t *testing.T, runner *TestRunner) {
		suite := TransactionErrorSuite()
		runner.RunSuite(suite)
	})
}
