// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package registry is the QuicBaaS service registry client: lease-based
// service-instance endpoints, served on every transport (gRPC, Unix, REST,
// QUIC, MCP and the in-process embedded transport).
//
// An instance registers under a lease and stays listed while its owner renews
// the lease. When the owner stops renewing — or its host dies — the lease
// expires, the entry disappears, and every watcher sees a DELETE. Every call
// is scoped to the caller's authenticated tenant; one tenant can neither see
// nor renew nor revoke another's registrations.
//
// # Registering
//
//	reg := client.Registry()
//	lease, err := reg.Register(ctx, &registry.RegisterRequest{
//	    Instance:   registry.ServiceInstance{Service: "api", ID: "node-a", Address: "10.0.4.7:8443"},
//	    TTLSeconds: 15,
//	})
//	// renew well inside the TTL, e.g. every 5 seconds
//	_, err = reg.Renew(ctx, lease.LeaseID)
//
// # Resolving and watching
//
// Resolve returns the instances and the store revision they were read at;
// Watch from that revision plus one delivers every later change exactly once:
//
//	snap, err := reg.Resolve(ctx, "api")
//	events, err := reg.Watch(ctx, &registry.WatchRequest{Service: "api", FromRevision: snap.Revision + 1})
//	for ev := range events {
//	    if ev.Err != nil { /* resolve again (compaction) or reconnect */ }
//	}
//
// A dropped connection is resumed transparently from the last delivered
// revision. A start revision older than the store's retained history ends the
// watch with *WatchCompactedError: resolve again and watch from its revision.
package registry
