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

// Package operations provides the Operations client for the QuicBaaS SDK.
//
// The Operations client provides access to operational management functionality
// including data migration, node replacement, and rolling upgrades. These
// operations are typically long-running and support progress tracking.
//
// # Migration Operations
//
// Data migration allows moving data between shards:
//
//	client := operations.NewClient(transport)
//
//	// Start a migration
//	resp, err := client.MigrateData(ctx, &operations.MigrateDataRequest{
//	    SourceShard: 1,
//	    TargetShard: 2,
//	    EntityTypes: []string{"users", "orders"},
//	    Concurrency: 4,
//	})
//
//	// Track progress
//	progress, err := client.GetMigrationProgress(ctx, &operations.GetMigrationProgressRequest{
//	    MigrationID: resp.MigrationID,
//	})
//
// # Node Operations
//
// Replace a failed node with a new one:
//
//	resp, err := client.ReplaceNode(ctx, &operations.ReplaceNodeRequest{
//	    ShardID:    1,
//	    OldNodeID:  3,
//	    NewNodeID:  5,
//	    NewAddress: "node5.cluster.local:5001",
//	})
//
// # Upgrade Operations
//
// Perform rolling upgrades:
//
//	// Start upgrade
//	resp, err := client.UpgradeNode(ctx, &operations.UpgradeNodeRequest{
//	    NodeID:        1,
//	    TargetVersion: "2.0.0",
//	})
//
//	// Check status
//	status, err := client.GetUpgradeStatus(ctx, &operations.GetUpgradeStatusRequest{
//	    JobID: resp.JobID,
//	})
//
// All operations support context cancellation and timeouts for long-running
// requests.
package operations
