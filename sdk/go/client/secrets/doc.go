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

// Package secrets provides the secret storage client for the QuicBaaS SDK.
//
// The secrets client provides a Vault-compatible interface for storing
// and retrieving sensitive data with versioning and metadata support.
//
// # Storing Secrets
//
//	client := secrets.NewClient(transport)
//
//	// Store a secret
//	resp, err := client.Store(ctx, &secrets.StoreSecretRequest{
//	    Path: "database/production",
//	    Data: map[string]interface{}{
//	        "username": "admin",
//	        "password": "s3cr3t",
//	    },
//	    Metadata: map[string]string{
//	        "owner": "dba-team",
//	    },
//	})
//
// # Retrieving Secrets
//
//	// Get the latest version
//	secret, err := client.Get(ctx, "database/production")
//
//	// Get a specific version
//	secret, err := client.Get(ctx, "database/production",
//	    secrets.WithVersion(2))
//
// # Secret Versioning
//
// Secrets are versioned automatically. Each Store operation creates a new
// version, preserving the history of changes.
//
// # Listing Secrets
//
//	// List secrets under a path prefix
//	resp, err := client.List(ctx, "database/")
//	for _, path := range resp.Paths {
//	    fmt.Println(path)
//	}
package secrets
