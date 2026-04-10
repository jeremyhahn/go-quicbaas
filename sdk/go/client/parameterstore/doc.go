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

// Package parameterstore provides the ParameterStore client for the QuicBaaS SDK.
//
// The ParameterStore client supports hierarchical parameter management with
// encryption, TTL, tagging, and batch operations.
//
// # Basic Usage
//
//	client := parameterstore.NewClient(transport)
//
//	// Store a parameter
//	resp, err := client.SetParameter(ctx, &parameterstore.SetParameterRequest{
//	    Path:        "/app/database/host",
//	    Value:       []byte("localhost:5432"),
//	    Description: "Database connection host",
//	    Tags:        map[string]string{"env": "production"},
//	})
//
//	// Retrieve a parameter
//	resp, err := client.GetParameter(ctx, &parameterstore.GetParameterRequest{
//	    Path:           "/app/database/host",
//	    WithDecryption: true,
//	})
//
// # Batch Operations
//
// The client supports batch operations for efficient bulk access:
//
//	// Get multiple parameters at once
//	resp, err := client.GetParameters(ctx, &parameterstore.GetParametersRequest{
//	    Paths:          []string{"/app/db/host", "/app/db/port", "/app/db/user"},
//	    WithDecryption: true,
//	})
//
//	// Set multiple parameters atomically
//	resp, err := client.SetParameters(ctx, &parameterstore.SetParametersRequest{
//	    Parameters: []*parameterstore.SetParameterRequest{
//	        {Path: "/app/config/a", Value: []byte("value-a")},
//	        {Path: "/app/config/b", Value: []byte("value-b")},
//	    },
//	})
//
// # Listing and Searching
//
// Parameters can be listed by prefix or searched by tags:
//
//	// List parameters under a prefix
//	resp, err := client.ListParameters(ctx, &parameterstore.ListParametersRequest{
//	    Prefix:     "/app/database/",
//	    Recursive:  true,
//	    WithValues: true,
//	    Limit:      100,
//	})
//
//	// Search by tags
//	resp, err := client.SearchByTags(ctx, &parameterstore.SearchByTagsRequest{
//	    Tags:       map[string]string{"env": "production", "team": "platform"},
//	    WithValues: true,
//	})
//
// # Encryption
//
// Parameters can be encrypted using KMS:
//
//	resp, err := client.SetParameter(ctx, &parameterstore.SetParameterRequest{
//	    Path:     "/app/secrets/api-key",
//	    Value:    []byte("secret-api-key-value"),
//	    KMSKeyID: "alias/my-encryption-key",
//	})
//
// # TTL Support
//
// Parameters can have an expiration time:
//
//	resp, err := client.SetParameter(ctx, &parameterstore.SetParameterRequest{
//	    Path:       "/app/tokens/session",
//	    Value:      []byte("temp-token-value"),
//	    TTLSeconds: 3600, // Expires in 1 hour
//	})
package parameterstore
