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

// Package devcluster provides local multi-node cluster management for QuicBaaS.
//
// DevCluster mode is designed for development and testing, allowing developers
// to run a multi-node Raft cluster on a single machine. Each node runs in-process
// with auto-incremented ports to avoid conflicts.
//
// Usage:
//
//	cfg := &ManagerConfig{
//	    DevCluster: config.DefaultDevClusterConfig(),
//	    BaseConfig: myBaseConfig,
//	    Logger:     logger,
//	}
//	mgr, err := NewManager(cfg)
//	if err != nil {
//	    return err
//	}
//	if err := mgr.Start(ctx); err != nil {
//	    return err
//	}
//	defer mgr.Stop(ctx)
//
// The manager generates unique configurations for each node, including:
//   - Separate data directories (./dev-data/node-1, node-2, etc.)
//   - Auto-incremented ports (gRPC: 9090, 9091, 9092, etc.)
//   - Automatic Raft cluster formation with proper initial members
//
// Note: DevCluster is for development only. Use cluster mode for production.
package devcluster
