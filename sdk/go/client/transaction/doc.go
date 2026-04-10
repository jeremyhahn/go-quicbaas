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

// Package transaction provides the compare-and-swap transaction client for the QuicBaaS SDK.
//
// The transaction client supports compare-and-swap (CAS) transactions
// with conditional execution based on key conditions.
//
// # Basic Usage
//
//	client := transaction.NewClient(transport)
//
//	// Compare-and-swap: update only if key exists with expected value
//	resp, err := client.Execute(ctx, &transaction.TxnRequest{
//	    Compare: []transaction.Compare{
//	        {Key: "users/1", Target: transaction.CompareValue, Result: transaction.CompareEqual, Value: []byte("Alice")},
//	    },
//	    Success: []transaction.TxnOp{
//	        {Type: transaction.TxnOpPut, EntityType: "users", Key: "users/1", Value: []byte("Alice Updated")},
//	    },
//	    Failure: []transaction.TxnOp{
//	        {Type: transaction.TxnOpGet, EntityType: "users", Key: "users/1"},
//	    },
//	})
//
// # Compare Targets
//
// Available comparison targets:
//   - CompareVersion: Compare key version (0 means key doesn't exist)
//   - CompareValue: Compare key value
//   - CompareModRevision: Compare modification revision
//   - CompareCreateRevision: Compare creation revision
//
// # Compare Results
//
// Available comparison operators:
//   - CompareEqual: ==
//   - CompareNotEqual: !=
//   - CompareLess: <
//   - CompareGreater: >
//
// # Transaction Operations
//
// Available operation types:
//   - TxnOpPut: Create or update a key
//   - TxnOpGet: Retrieve a key value
//   - TxnOpDelete: Delete a key
package transaction
