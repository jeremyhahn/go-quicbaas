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

// Package batch provides the atomic batch operations client for the QuicBaaS SDK.
//
// The batch client supports executing multiple operations atomically as a single
// unit. All operations in a batch either succeed together or fail together.
//
// # Basic Usage
//
//	client := batch.NewClient(transport)
//
//	// Execute multiple operations atomically
//	resp, err := client.Execute(ctx, &batch.BatchRequest{
//	    Operations: []batch.BatchOperation{
//	        {Type: batch.OpPut, EntityType: "users", Key: "user-1", Value: []byte(`{"name":"Alice"}`)},
//	        {Type: batch.OpPut, EntityType: "users", Key: "user-2", Value: []byte(`{"name":"Bob"}`)},
//	        {Type: batch.OpDelete, EntityType: "users", Key: "user-old"},
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Batch completed at revision %d\n", resp.Revision)
//
// # Operation Types
//
// Available batch operation types:
//   - OpPut: Create or update a key-value pair
//   - OpDelete: Delete a key
//
// # Results
//
// The BatchResponse contains results for each operation in the batch:
//
//	for i, result := range resp.Results {
//	    if result.Success {
//	        fmt.Printf("Operation %d succeeded\n", i)
//	    } else {
//	        fmt.Printf("Operation %d failed: %s\n", i, result.Error)
//	    }
//	}
package batch
