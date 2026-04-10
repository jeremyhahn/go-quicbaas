//go:build quic

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

package quic

import (
	"context"
	"crypto/tls"
	"io"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Integration Tests for handleConnection and handleStream
// =============================================================================

// TestServer_HandleConnection_Success tests the full request/response flow
// through handleConnection and handleStream.
func TestServer_HandleConnection_Success(t *testing.T) {
	// Setup server
	svc := &mockService{
		putFn: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			return &service.PutResponse{ID: req.ID, Version: 1}, nil
		},
	}

	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// Get the actual listening address
	actualAddr := server.listener.Addr().String()

	// Setup client
	clientTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-quicbaas"},
	}

	// Connect to server
	conn, err := quicgo.DialAddr(context.Background(), actualAddr, clientTLSConfig, nil)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer conn.CloseWithError(0, "test complete")

	// Open stream
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	defer stream.Close()

	// Create properly formatted request
	putReq := &service.PutRequest{
		EntityType: "test",
		ID:         1,
		Data:       []byte("test-data"),
	}
	payload, err := EncodePayload(putReq)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpPut,
		Payload: payload,
	}

	if err := WriteRequest(stream, req); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	// Read response
	resp, err := ReadResponse(stream)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if resp.Status != StatusOK {
		t.Errorf("expected status OK, got %d, payload: %s", resp.Status, string(resp.Payload))
	}

	// Verify metrics
	metrics := server.GetMetrics()
	totalRequests := metrics["total_requests"].(uint64)
	if totalRequests != 1 {
		t.Errorf("expected 1 request, got %d", totalRequests)
	}
}

// TestServer_HandleConnection_MultipleStreams tests concurrent streams on a single connection.
func TestServer_HandleConnection_MultipleStreams(t *testing.T) {
	// Setup server
	svc := &mockService{
		getFn: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			return &service.GetResponse{
				ID:      req.ID,
				Data:    []byte("test-value"),
				Version: 1,
				Found:   true,
			}, nil
		},
	}

	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	config.MaxStreams = 10
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	// Get actual address
	actualAddr := server.listener.Addr().String()

	// Setup client
	clientTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-quicbaas"},
	}

	conn, err := quicgo.DialAddr(context.Background(), actualAddr, clientTLSConfig, nil)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer conn.CloseWithError(0, "test complete")

	// Open multiple streams concurrently
	const numStreams = 5
	done := make(chan error, numStreams)

	for i := 0; i < numStreams; i++ {
		go func(idx int) {
			stream, err := conn.OpenStreamSync(context.Background())
			if err != nil {
				done <- err
				return
			}
			defer stream.Close()

			getReq := &service.GetRequest{
				EntityType: "test",
				ID:         uint64(idx),
			}
			payload, err := EncodePayload(getReq)
			if err != nil {
				done <- err
				return
			}

			req := &Request{
				Opcode:  OpGet,
				Payload: payload,
			}

			if err := WriteRequest(stream, req); err != nil {
				done <- err
				return
			}

			resp, err := ReadResponse(stream)
			if err != nil {
				done <- err
				return
			}

			if resp.Status != StatusOK {
				done <- err
				return
			}

			done <- nil
		}(i)
	}

	// Wait for all streams
	for i := 0; i < numStreams; i++ {
		if err := <-done; err != nil {
			t.Errorf("stream %d failed: %v", i, err)
		}
	}

	// Verify metrics
	metrics := server.GetMetrics()
	totalRequests := metrics["total_requests"].(uint64)
	if totalRequests != numStreams {
		t.Errorf("expected %d requests, got %d", numStreams, totalRequests)
	}
}

// TestServer_HandleStream_InvalidRequest tests error handling in handleStream.
func TestServer_HandleStream_InvalidRequest(t *testing.T) {
	// Setup server
	svc := &mockService{}

	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	actualAddr := server.listener.Addr().String()

	// Setup client
	clientTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-quicbaas"},
	}

	conn, err := quicgo.DialAddr(context.Background(), actualAddr, clientTLSConfig, nil)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer conn.CloseWithError(0, "test complete")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}
	defer stream.Close()

	// Send malformed data
	_, err = stream.Write([]byte("invalid data"))
	if err != nil {
		t.Fatalf("failed to write invalid data: %v", err)
	}
	stream.Close()

	// Give server time to process
	time.Sleep(100 * time.Millisecond)

	// Verify error was tracked
	metrics := server.GetMetrics()
	totalErrors := metrics["total_errors"].(uint64)
	if totalErrors == 0 {
		t.Error("expected at least 1 error to be tracked")
	}
}

// TestServer_AcceptConnections_MaxStreamsLimit tests stream limit enforcement.
func TestServer_AcceptConnections_MaxStreamsLimit(t *testing.T) {
	// Setup server with low stream limit
	svc := &mockService{
		getFn: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			// Add delay to keep streams active
			time.Sleep(200 * time.Millisecond)
			return &service.GetResponse{
				ID:      req.ID,
				Data:    []byte("test-value"),
				Version: 1,
				Found:   true,
			}, nil
		},
	}

	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	config.MaxStreams = 2
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	actualAddr := server.listener.Addr().String()

	clientTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-quicbaas"},
	}

	conn, err := quicgo.DialAddr(context.Background(), actualAddr, clientTLSConfig, nil)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer conn.CloseWithError(0, "test complete")

	// Try to open more streams than the limit
	// Some should be rejected
	const attemptStreams = 5
	successCount := 0
	done := make(chan bool, attemptStreams)

	for i := 0; i < attemptStreams; i++ {
		go func() {
			stream, err := conn.OpenStreamSync(context.Background())
			if err != nil {
				done <- false
				return
			}
			defer stream.Close()

			getReq := &service.GetRequest{
				EntityType: "test",
				ID:         1,
			}
			payload, err := EncodePayload(getReq)
			if err != nil {
				done <- false
				return
			}

			req := &Request{
				Opcode:  OpGet,
				Payload: payload,
			}

			if err := WriteRequest(stream, req); err != nil {
				done <- false
				return
			}

			// Try to read response
			_, err = ReadResponse(stream)
			done <- err == nil
		}()
	}

	// Count successful requests
	for i := 0; i < attemptStreams; i++ {
		if <-done {
			successCount++
		}
	}

	// At least some requests should succeed
	if successCount == 0 {
		t.Error("expected at least some requests to succeed")
	}

	// Should have processed some requests
	metrics := server.GetMetrics()
	totalRequests := metrics["total_requests"].(uint64)
	if totalRequests == 0 {
		t.Error("expected some requests to be processed")
	}
}

// TestServer_HandleConnection_ContextCancellation tests proper cleanup on context cancellation.
func TestServer_HandleConnection_ContextCancellation(t *testing.T) {
	// Setup server
	svc := &mockService{}

	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	actualAddr := server.listener.Addr().String()

	clientTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-quicbaas"},
	}

	conn, err := quicgo.DialAddr(context.Background(), actualAddr, clientTLSConfig, nil)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}

	// Open a stream but don't send anything
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// Stop the server - this should cancel the context
	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}

	// Try to read from stream - should fail
	_, err = io.ReadAll(stream)
	if err == nil {
		t.Error("expected error reading from stream after server stop")
	}

	// Close connection
	conn.CloseWithError(0, "test complete")
}

// TestServer_AcceptConnections_ErrorHandling tests error handling in acceptConnections.
func TestServer_AcceptConnections_ErrorHandling(t *testing.T) {
	// This test verifies that acceptConnections handles errors gracefully
	svc := &mockService{}

	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Immediately stop the server
	// This tests the path where acceptConnections exits cleanly
	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}

	// Verify server is not running
	if server.IsRunning() {
		t.Error("expected server to not be running")
	}
}

// TestServer_HandleConnection_MultipleConnections tests handling multiple concurrent connections.
func TestServer_HandleConnection_MultipleConnections(t *testing.T) {
	// Setup server
	svc := &mockService{
		getFn: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			return &service.GetResponse{
				ID:      req.ID,
				Data:    []byte("test-value"),
				Version: 1,
				Found:   true,
			}, nil
		},
	}

	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	actualAddr := server.listener.Addr().String()

	clientTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-quicbaas"},
	}

	// Create multiple connections
	const numConns = 3
	done := make(chan error, numConns)

	for i := 0; i < numConns; i++ {
		go func(idx int) {
			conn, err := quicgo.DialAddr(context.Background(), actualAddr, clientTLSConfig, nil)
			if err != nil {
				done <- err
				return
			}
			defer conn.CloseWithError(0, "test complete")

			stream, err := conn.OpenStreamSync(context.Background())
			if err != nil {
				done <- err
				return
			}
			defer stream.Close()

			getReq := &service.GetRequest{
				EntityType: "test",
				ID:         uint64(idx),
			}
			payload, err := EncodePayload(getReq)
			if err != nil {
				done <- err
				return
			}

			req := &Request{
				Opcode:  OpGet,
				Payload: payload,
			}

			if err := WriteRequest(stream, req); err != nil {
				done <- err
				return
			}

			resp, err := ReadResponse(stream)
			if err != nil {
				done <- err
				return
			}

			if resp.Status != StatusOK {
				done <- err
				return
			}

			done <- nil
		}(i)
	}

	// Wait for all connections
	for i := 0; i < numConns; i++ {
		if err := <-done; err != nil {
			t.Errorf("connection %d failed: %v", i, err)
		}
	}

	// Verify metrics
	metrics := server.GetMetrics()
	totalRequests := metrics["total_requests"].(uint64)
	if totalRequests != numConns {
		t.Errorf("expected %d requests, got %d", numConns, totalRequests)
	}
}

// TestServer_HandleStream_AllOpcodes tests handling different request opcodes.
func TestServer_HandleStream_AllOpcodes(t *testing.T) {
	// Setup server with handlers for multiple opcodes
	svc := &mockService{
		putFn: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			return &service.PutResponse{ID: req.ID, Version: 1}, nil
		},
		getFn: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			return &service.GetResponse{ID: req.ID, Data: []byte("value"), Version: 1, Found: true}, nil
		},
		deleteFn: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			return &service.DeleteResponse{Deleted: true}, nil
		},
	}

	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	config.ListenAddr = "127.0.0.1:0"
	log := createServerTestLogger()

	server, err := NewServer(svc, config, log)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Stop()

	actualAddr := server.listener.Addr().String()

	clientTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-quicbaas"},
	}

	tests := []struct {
		name    string
		opcode  byte
		request interface{}
	}{
		{
			"Put",
			OpPut,
			&service.PutRequest{EntityType: "test", ID: 1, Data: []byte("value")},
		},
		{
			"Get",
			OpGet,
			&service.GetRequest{EntityType: "test", ID: 1},
		},
		{
			"Delete",
			OpDelete,
			&service.DeleteRequest{EntityType: "test", ID: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := quicgo.DialAddr(context.Background(), actualAddr, clientTLSConfig, nil)
			if err != nil {
				t.Fatalf("failed to dial: %v", err)
			}
			defer conn.CloseWithError(0, "test complete")

			stream, err := conn.OpenStreamSync(context.Background())
			if err != nil {
				t.Fatalf("failed to open stream: %v", err)
			}
			defer stream.Close()

			payload, err := EncodePayload(tt.request)
			if err != nil {
				t.Fatalf("failed to encode payload: %v", err)
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			if err := WriteRequest(stream, req); err != nil {
				t.Fatalf("failed to write request: %v", err)
			}

			resp, err := ReadResponse(stream)
			if err != nil {
				t.Fatalf("failed to read response: %v", err)
			}

			if resp.Status != StatusOK {
				t.Errorf("expected status OK, got %d", resp.Status)
			}
		})
	}

	// Verify all requests were processed
	metrics := server.GetMetrics()
	totalRequests := metrics["total_requests"].(uint64)
	if totalRequests != uint64(len(tests)) {
		t.Errorf("expected %d requests, got %d", len(tests), totalRequests)
	}
}
