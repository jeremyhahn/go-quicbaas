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

// Package mock provides mock implementations for testing the QuicBaaS SDK.
//
// This package contains mock implementations of the transport and server
// interfaces that allow for comprehensive testing without requiring
// actual network connections or server instances.
//
// # Mock Transport
//
// The Transport type implements transport.Transport and provides:
//   - Configurable responses for specific service/method combinations
//   - Request capture for verification in tests
//   - Stream support with configurable message sequences
//   - Health and connection state simulation
//
// Example usage:
//
//	transport := mock.NewTransport()
//
//	// Configure a response
//	transport.SetResponse("kv", "Get", map[string]string{"value": "test"}, nil)
//
//	// Make a request
//	resp, err := transport.Request(ctx, "kv", "Get", map[string]string{"key": "mykey"})
//
//	// Verify requests were made
//	requests := transport.Requests()
//	if requests[0].Method != "Get" {
//	    t.Error("expected Get method")
//	}
//
// # Mock Stream
//
// The Stream type implements transport.Stream and provides:
//   - Pre-configured message sequences for Recv
//   - Message capture for Send operations
//   - Configurable errors for all operations
//
// Example usage:
//
//	transport := mock.NewTransport()
//	transport.SetStreamConfig("watch", "Watch", mock.StreamConfig{
//	    Messages: []interface{}{"event1", "event2"},
//	})
//
//	stream, _ := transport.RequestStream(ctx, "watch", "Watch", nil)
//	msg, _ := stream.Recv() // Returns "event1"
//
// # Mock Server
//
// The Server type provides a mock server for integration testing:
//   - Configurable handlers for different services
//   - Request capture for verification
//   - Start/Stop lifecycle management
//
// Example usage:
//
//	server := mock.NewServer(mock.DefaultServerConfig())
//	server.RegisterHandler("kv", func(ctx context.Context, method string, req interface{}) (interface{}, error) {
//	    return map[string]string{"value": "response"}, nil
//	})
//
//	err := server.Start()
//	defer server.Stop()
//
//	// Use server.Address() to connect clients
package mock
