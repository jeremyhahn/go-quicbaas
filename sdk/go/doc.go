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

// Package quicbaas provides the official Go SDK for QuicBaaS.
//
// The SDK supports multiple transport protocols (gRPC, REST, QUIC) and provides
// both a unified client interface and standalone domain-specific clients.
//
// # Quick Start
//
// Create a unified client to access all QuicBaaS services:
//
//	client, err := quicbaas.NewClient("/var/run/quicbaas.sock")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	// Use KV operations
//	resp, err := client.KV().Put(ctx, &kv.PutRequest{
//	    EntityType: "users",
//	    Key:        "user-123",
//	    Value:      []byte(`{"name": "Alice"}`),
//	})
//
// # Connection Options
//
// The SDK supports various connection methods:
//
//	// Unix socket (recommended for local)
//	client, _ := quicbaas.NewClient("/var/run/quicbaas.sock")
//
//	// TCP with mTLS
//	client, _ := quicbaas.NewClient("localhost:50051",
//	    quicbaas.WithTLS(quicbaas.TLSConfig{
//	        CertFile: "client.crt",
//	        KeyFile:  "client.key",
//	        CAFile:   "ca.crt",
//	    }),
//	)
//
//	// REST transport
//	client, _ := quicbaas.NewClient("http://localhost:8080", quicbaas.WithREST())
//
//	// QUIC transport
//	client, _ := quicbaas.NewClient("localhost:50052", quicbaas.WithQUIC())
//
// # Watch Support
//
// The KV client supports watches for change notifications:
//
//	watcher, err := client.KV().WatchPrefix(ctx, "users/", kv.WithPrevKV())
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer watcher.Close()
//
//	for event := range watcher.Events() {
//	    log.Printf("Event: %v %s", event.Type, event.Key)
//	}
//
// # Authentication
//
// The SDK uses mTLS for authentication. API keys are not supported.
//
// # Mounting a host ServiceModule
//
// A host that embeds a QuicBaaS node can mount its own capability plane on
// that node, served over gRPC, unix, REST and MCP under the node's single
// authorizer alongside the "quicbaas" and "xkms" modules:
//
//	type deviceModule struct{ quicbaas.BaseHostModule }
//
//	func (m *deviceModule) Name() string { return "trusted-platform" }
//
//	func (m *deviceModule) RegisterHTTP(r *quicbaas.ModuleHTTPRegistrar) {
//	    r.Handle(http.MethodGet, "/api/v1/devices",
//	        quicbaas.Permission("device:read"), m.list)
//	}
//
//	srv, err := quicbaas.NewServer(cfg, quicbaas.WithModule(&deviceModule{}))
//
// WithModule is the canonical shape — the SDK exposes no qrdb server
// constructor, because composing the node is go-quicbaas's job. Modules do
// NOT reach QUIC or the embedded in-process transport; see WithModule and
// docs/go-sdk/api.md for both gaps and the failure modes.
//
// # Embedded server lifecycle
//
// NewServer returns a server whose Start does NOT unwind on failure: Start
// returns at the first component that fails and leaves everything started
// before it running — the embedded qrdb node, an open audit log, background
// pollers and cleanup goroutines.
//
// A non-nil error from Start MUST therefore be followed by Shutdown. Shutdown
// is safe on a server that never started, on one that started fully, and on
// one that stopped anywhere in between, and it is idempotent, so the deferred
// call belongs immediately after NewServer:
//
//	srv, err := quicbaas.NewServer(cfg)
//	if err != nil {
//	    return err
//	}
//	defer srv.Shutdown(context.Background()) // also releases a PARTIAL start
//
//	if err := srv.Start(); err != nil {
//	    return err
//	}
//
// The daemon does not need this — it logs the error and exits, and process
// teardown collects everything. An embedding application has no such
// backstop.
//
// # Services
//
// Available services through the unified client:
//   - KV: Key-value storage with Watch support
//   - Batch: Atomic batch operations
//   - Transaction: Compare-and-swap transactions
//   - Query: Secondary index queries
//   - Cluster: Cluster status and management
//   - Health: Health checks and probes
//   - KMS: Key management service
//   - Secrets: Secret storage with versioning
//   - Crypto: Encryption/decryption as a service
//   - Vector: Vector similarity search
//   - TimeSeries: Time-series data storage
//   - Backup: Backup and restore
package quicbaas
