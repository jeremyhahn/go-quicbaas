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

// Package health provides the health check client for the QuicBaaS SDK.
//
// The health client supports standard health check operations including
// Ping, Liveness, and Readiness probes for Kubernetes and monitoring systems.
//
// # Basic Usage
//
//	client := health.NewClient(transport)
//
//	// Simple ping test
//	if err := client.Ping(ctx); err != nil {
//	    log.Fatal("server unreachable:", err)
//	}
//
//	// Liveness probe (is the server running?)
//	resp, err := client.Liveness(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Liveness: %v\n", resp.Healthy)
//
//	// Readiness probe (is the server ready to serve traffic?)
//	resp, err := client.Readiness(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Readiness: %v\n", resp.Healthy)
//
// # Health Response
//
// Health responses include:
//   - Healthy: boolean indicating overall health status
//   - Status: human-readable status message
//   - Details: map of component-specific health information
package health
