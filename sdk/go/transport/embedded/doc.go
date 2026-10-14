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

// Package embedded provides an in-process transport implementation for the QuicBaaS SDK.
//
// The embedded transport allows SDK clients to directly call the service layer without
// any network overhead. This is useful for:
//   - Testing: Run integration tests without starting a server
//   - Embedded deployments: Include QuicBaaS as a library in your application
//   - Performance: Eliminate serialization and network latency for co-located clients
//
// # Usage
//
// Create an embedded transport by providing a service.QuicBaaS instance:
//
//	svc, err := service.NewServiceBuilder().
//	    WithRaftNode(raftNode).
//	    WithStorage(storage).
//	    Build()
//	if err != nil {
//	    return err
//	}
//
//	transport := embedded.New(svc)
//
//	// Use with SDK client
//	client, err := quicbaas.NewClient(
//	    quicbaas.WithTransport(transport),
//	)
//
// # Method Dispatch
//
// The transport uses O(1) map-based dispatch to route requests to the appropriate
// service methods. The method path format is "service/method", for example:
//   - "quicbaas.KV/Put" -> calls service.Put()
//   - "quicbaas.KV/Get" -> calls service.Get()
//   - "quicbaas.Health/Check" -> calls service.Health()
//   - "quicbaas.KMS/GenerateKey" -> calls service.KMS().GenerateKey()
//
// # Streaming Operations
//
// Streaming operations (Watch, List, Query) are supported through the stream
// wrapper that bridges between Go channels and the transport.Stream interface.
//
// # Authorization
//
// The embedded transport performs no authorization and is not a trust
// boundary. Linking the SDK into a process and constructing the transport over
// a service.QuicBaaS is the grant: code in that process already holds the
// service and could call it directly. Network transports, gRPC included,
// remain authorization boundaries (go-quicbaas#329, go-quicbaas#301).
package embedded
