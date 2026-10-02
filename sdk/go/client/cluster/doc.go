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

// Package cluster provides the cluster management client for the QuicBaaS SDK.
//
// The cluster client supports operations for managing the QuicBaaS cluster
// including status checks, node management, and cluster configuration.
//
// # Basic Usage
//
//	client := cluster.NewClient(transport)
//
//	// Get cluster status
//	status, err := client.Status(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Cluster ID: %s, Leader: %s\n", status.ClusterID, status.LeaderID)
//
//	// List nodes
//	nodes, err := client.Nodes(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, node := range nodes.Nodes {
//	    fmt.Printf("Node: %s (%s)\n", node.Name, node.Address)
//	}
//
// # Node Management
//
//	// Add a node
//	err := client.AddNode(ctx, &cluster.AddNodeRequest{
//	    ShardID:  1,
//	    NodeID:   4,
//	    Address:  "node-4.example.com:63001",
//	    Identity: "spki:<sha256-hex>", // from `qrdbd node-identity --cert`
//	})
//
//	// Remove a node
//	err := client.RemoveNode(ctx, &cluster.RemoveNodeRequest{
//	    NodeID: "node-id-to-remove",
//	})
package cluster
