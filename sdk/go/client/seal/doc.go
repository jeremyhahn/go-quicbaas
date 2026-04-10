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

// Package seal provides the seal/unseal operations client for the QuicBaaS SDK.
//
// The seal client supports Vault-style seal/unseal operations using Shamir's
// Secret Sharing for secure key management.
//
// # Initialization
//
// Before using QuicBaaS, it must be initialized to generate the master key
// shares and root token:
//
//	client := seal.NewClient(transport)
//
//	// Initialize with 5 key shares, requiring 3 to unseal
//	resp, err := client.Init(ctx, &seal.InitRequest{
//	    SecretShares:    5,
//	    SecretThreshold: 3,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// IMPORTANT: Store resp.Keys securely, each with a different custodian
//	// Store resp.RootToken securely for initial admin operations
//
// # Unsealing
//
// After restart, QuicBaaS starts in a sealed state. Multiple unseal keys
// must be provided to reach the threshold:
//
//	// Check current status
//	status, err := client.Status(ctx)
//	if status.Sealed {
//	    // Provide unseal keys from different custodians
//	    resp, err := client.Unseal(ctx, &seal.UnsealRequest{Key: key1})
//	    resp, err = client.Unseal(ctx, &seal.UnsealRequest{Key: key2})
//	    resp, err = client.Unseal(ctx, &seal.UnsealRequest{Key: key3})
//	    // Server is now unsealed when threshold is reached
//	}
//
// # Sealing
//
// The server can be sealed at any time for emergency lockdown:
//
//	err := client.Seal(ctx)
//	// All operations will now fail until the server is unsealed
package seal
