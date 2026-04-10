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

// Package objecttransfer provides the Object Transfer client for the QuicBaaS SDK.
//
// The Object Transfer client enables applications to fetch objects from remote
// shards for data transfer, replication, and recovery operations. It supports
// both single object fetch and streaming batch fetch operations.
//
// # Basic Usage
//
// Create a client using a transport connection:
//
//	client := objecttransfer.NewClient(transport)
//
//	// Fetch a single object
//	resp, err := client.FetchObject(ctx, &objecttransfer.FetchObjectRequest{
//	    ShardID:   1,
//	    ObjectRef: "bucket:key/path/to/object",
//	    Checksum:  expectedChecksum,
//	    RequestID: "request-123",
//	})
//	if resp.Found {
//	    // Use resp.Data
//	}
//
// # Streaming Batch Fetch
//
// For efficient batch retrieval of multiple objects:
//
//	items := []objecttransfer.ObjectFetchItem{
//	    {ObjectRef: "bucket:key1", Checksum: checksum1},
//	    {ObjectRef: "bucket:key2", Checksum: checksum2},
//	    {ObjectRef: "bucket:key3", Checksum: checksum3},
//	}
//
//	responseChan, err := client.StreamFetchObjects(ctx, &objecttransfer.StreamFetchObjectsRequest{
//	    ShardID: 1,
//	    Items:   items,
//	})
//	if err != nil {
//	    return err
//	}
//
//	for resp := range responseChan {
//	    if resp.Found {
//	        // Process resp.Data
//	    }
//	}
//
// # Use Cases
//
// The Object Transfer service is designed for:
//   - Data replication between nodes
//   - Shard migration during cluster rebalancing
//   - Recovery operations after node failures
//   - Backup and restore operations
//
// # Checksum Verification
//
// Each fetch request can include an expected checksum. If provided, the server
// will verify the object's checksum matches before returning the data:
//
//	resp, err := client.FetchObject(ctx, &objecttransfer.FetchObjectRequest{
//	    ShardID:   1,
//	    ObjectRef: "bucket:key",
//	    Checksum:  sha256Checksum, // Optional verification
//	})
package objecttransfer
