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

// Package clusterdek provides a client for cluster-wide Data Encryption Key (DEK) management.
//
// The cluster DEK is distributed via Raft consensus and used for encrypting data at rest
// across all nodes in the cluster. This package provides operations for:
//
//   - Initializing the cluster DEK
//   - Checking cluster DEK status
//   - Rotating the cluster DEK
//   - Syncing DEK to nodes
//   - Managing re-encryption operations
//   - DEK recovery procedures
//
// Key Hierarchy Overview:
//
//  1. Master Key (per node, from seal)
//     - Derived from Shamir shares during unseal process
//     - Held in memory only, never persisted in plaintext
//     - Used to wrap the node's copy of the cluster DEK
//
//  2. Cluster DEK (shared, distributed via Raft)
//     - Single cluster-wide encryption key for all data
//     - Distributed to all nodes via Raft consensus
//     - Each node stores it encrypted (wrapped) by their master key
//
//  3. Data Keys (per entity, wrapped by cluster DEK)
//     - Generated per-entity for granular encryption
//     - Wrapped by cluster DEK before storage
//
// Example usage:
//
//	client := clusterdek.NewClient(transport)
//
//	// Initialize cluster DEK
//	resp, err := client.InitializeClusterDEK(ctx, &clusterdek.InitClusterDEKRequest{
//	    Algorithm: "AES-256-GCM",
//	    KeySize:   256,
//	})
//
//	// Check status
//	status, err := client.GetClusterDEKStatus(ctx)
//
//	// Rotate DEK
//	rotateResp, err := client.RotateClusterDEK(ctx, &clusterdek.RotateClusterDEKRequest{
//	    Reason:                "Scheduled rotation",
//	    AutoStartReencryption: true,
//	})
package clusterdek
