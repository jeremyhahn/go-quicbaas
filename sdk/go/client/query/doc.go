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

// Package query provides the secondary index query client for the QuicBaaS SDK.
//
// The query client supports queries using secondary indexes for efficient
// data retrieval beyond simple key lookups.
//
// # Basic Usage
//
//	client := query.NewClient(transport)
//
//	// Query by secondary index
//	resp, err := client.Execute(ctx, &query.QueryRequest{
//	    EntityType: "users",
//	    Index:      "email",
//	    Value:      "alice@example.com",
//	    Operator:   query.OpEqual,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, result := range resp.Results {
//	    fmt.Printf("Found: %s = %s\n", result.Key, string(result.Value))
//	}
//
// # Query Operators
//
// Available query operators:
//   - OpEqual: Exact match (==)
//   - OpNotEqual: Not equal (!=)
//   - OpLessThan: Less than (<)
//   - OpLessThanOrEqual: Less than or equal (<=)
//   - OpGreaterThan: Greater than (>)
//   - OpGreaterThanOrEqual: Greater than or equal (>=)
//   - OpPrefix: Prefix match (starts with)
//   - OpRange: Range query (between two values)
//
// # Pagination
//
// Results can be paginated using Limit and Cursor:
//
//	resp, err := client.Execute(ctx, &query.QueryRequest{
//	    EntityType: "users",
//	    Index:      "created_at",
//	    Operator:   query.OpGreaterThan,
//	    Value:      "2024-01-01",
//	    Limit:      100,
//	})
//	// Use resp.NextCursor for subsequent pages
package query
