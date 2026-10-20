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

package objecttransfer

import (
	"context"
	stderrors "errors"
	"io"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Mock Transport
// =============================================================================

type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} { return nil }
func (m *mockTransport) Close() error      { return nil }
func (m *mockTransport) Healthy() bool     { return true }

// =============================================================================
// Mock Stream
// =============================================================================

type mockStream struct {
	recvFn      func() (interface{}, error)
	sendFn      func(msg interface{}) error
	closeFn     func() error
	closeSendFn func() error
}

func (m *mockStream) Recv() (interface{}, error) {
	if m.recvFn != nil {
		return m.recvFn()
	}
	return nil, nil
}

func (m *mockStream) Send(msg interface{}) error {
	if m.sendFn != nil {
		return m.sendFn(msg)
	}
	return nil
}

func (m *mockStream) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func (m *mockStream) CloseSend() error {
	if m.closeSendFn != nil {
		return m.closeSendFn()
	}
	return nil
}

// =============================================================================
// TestNewClient
// =============================================================================

func TestNewClient(t *testing.T) {
	t.Run("valid transport returns non-nil client", func(t *testing.T) {
		c := NewClient(&mockTransport{})
		if c == nil {
			t.Fatal("expected non-nil client when transport is provided")
		}
	})

	t.Run("nil transport returns nil client", func(t *testing.T) {
		c := NewClient(nil)
		if c != nil {
			t.Fatal("expected nil client when transport is nil")
		}
	})
}

// =============================================================================
// TestClient_FetchObject_Validation
// =============================================================================

func TestClient_FetchObject_Validation(t *testing.T) {
	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &FetchObjectResponse{
				ObjectRef: req.(*FetchObjectRequest).ObjectRef,
				Found:     true,
				Data:      []byte("data"),
			}, nil
		},
	})

	tests := []struct {
		name        string
		req         *FetchObjectRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request",
			req:         nil,
			wantErr:     true,
			errContains: "request is required",
		},
		{
			name: "zero shard ID",
			req: &FetchObjectRequest{
				ShardID:   0,
				ObjectRef: "bucket:key",
			},
			wantErr:     true,
			errContains: "shard ID is required",
		},
		{
			name: "empty object ref",
			req: &FetchObjectRequest{
				ShardID:   1,
				ObjectRef: "",
			},
			wantErr:     true,
			errContains: "object reference is required",
		},
		{
			name: "valid request",
			req: &FetchObjectRequest{
				ShardID:   1,
				ObjectRef: "bucket:key",
			},
			wantErr: false,
		},
		{
			name: "valid request with optional checksum",
			req: &FetchObjectRequest{
				ShardID:   1,
				ObjectRef: "bucket:key",
				Checksum:  []byte("abc123"),
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := c.FetchObject(context.Background(), tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if !errors.IsInvalidInput(err) {
					t.Fatalf("expected InvalidInput error, got: %v", err)
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error to contain %q, got: %v", tc.errContains, err)
				}
				if resp != nil {
					t.Fatal("expected nil response on validation error")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Fatal("expected non-nil response")
				}
			}
		})
	}
}

// =============================================================================
// TestClient_FetchObject_TransportError
// =============================================================================

func TestClient_FetchObject_TransportError(t *testing.T) {
	transportErr := errors.NewUnavailable("transport", "connection refused")
	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, transportErr
		},
	})

	resp, err := c.FetchObject(context.Background(), &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "bucket:key",
	})

	if err == nil {
		t.Fatal("expected error from transport")
	}
	if !stderrors.Is(err, transportErr) {
		t.Fatalf("expected transport error to be propagated, got: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response on transport error")
	}
}

// =============================================================================
// TestClient_FetchObject_NilResponse
// =============================================================================

func TestClient_FetchObject_NilResponse(t *testing.T) {
	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	})

	resp, err := c.FetchObject(context.Background(), &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "bucket:key",
	})

	if err != nil {
		t.Fatalf("expected no error for nil response, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response even when transport returns nil")
	}
	if resp.Found {
		t.Fatal("expected Found to be false for nil transport response")
	}
	if resp.ObjectRef != "bucket:key" {
		t.Fatalf("expected ObjectRef to be %q, got %q", "bucket:key", resp.ObjectRef)
	}
}

// =============================================================================
// TestClient_FetchObject_InvalidResponseType
// =============================================================================

func TestClient_FetchObject_InvalidResponseType(t *testing.T) {
	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "not-a-fetch-response", nil
		},
	})

	resp, err := c.FetchObject(context.Background(), &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "bucket:key",
	})

	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Fatalf("expected Internal error code, got: %v", errors.Code(err))
	}
	if !strings.Contains(err.Error(), "invalid response type") {
		t.Fatalf("expected error message to contain 'invalid response type', got: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response on type assertion failure")
	}
}

// =============================================================================
// TestClient_FetchObject_Success
// =============================================================================

func TestClient_FetchObject_Success(t *testing.T) {
	expectedData := []byte("the-object-data")
	expectedChecksum := []byte("sha256-hash")

	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if service != serviceName {
				t.Fatalf("expected service %q, got %q", serviceName, service)
			}
			if method != "FetchObject" {
				t.Fatalf("expected method %q, got %q", "FetchObject", method)
			}
			fetchReq, ok := req.(*FetchObjectRequest)
			if !ok {
				t.Fatalf("expected *FetchObjectRequest, got %T", req)
			}
			return &FetchObjectResponse{
				ObjectRef:       fetchReq.ObjectRef,
				Data:            expectedData,
				Checksum:        expectedChecksum,
				Size:            int64(len(expectedData)),
				Compressed:      true,
				CompressionAlgo: CompressionZstd,
				Found:           true,
			}, nil
		},
	})

	resp, err := c.FetchObject(context.Background(), &FetchObjectRequest{
		ShardID:   42,
		ObjectRef: "mybucket:mykey",
		Checksum:  []byte("expected-checksum"),
		RequestID: "req-001",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.ObjectRef != "mybucket:mykey" {
		t.Fatalf("expected ObjectRef %q, got %q", "mybucket:mykey", resp.ObjectRef)
	}
	if string(resp.Data) != string(expectedData) {
		t.Fatalf("expected Data %q, got %q", expectedData, resp.Data)
	}
	if string(resp.Checksum) != string(expectedChecksum) {
		t.Fatalf("expected Checksum %q, got %q", expectedChecksum, resp.Checksum)
	}
	if resp.Size != int64(len(expectedData)) {
		t.Fatalf("expected Size %d, got %d", len(expectedData), resp.Size)
	}
	if !resp.Compressed {
		t.Fatal("expected Compressed to be true")
	}
	if resp.CompressionAlgo != CompressionZstd {
		t.Fatalf("expected CompressionAlgo %q, got %q", CompressionZstd, resp.CompressionAlgo)
	}
	if !resp.Found {
		t.Fatal("expected Found to be true")
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_Validation
// =============================================================================

func TestClient_StreamFetchObjects_Validation(t *testing.T) {
	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					return nil, io.EOF
				},
			}, nil
		},
	})

	tests := []struct {
		name        string
		req         *StreamFetchObjectsRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request",
			req:         nil,
			wantErr:     true,
			errContains: "request is required",
		},
		{
			name: "zero shard ID",
			req: &StreamFetchObjectsRequest{
				ShardID: 0,
				Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
			},
			wantErr:     true,
			errContains: "shard ID is required",
		},
		{
			name: "empty items",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items:   []ObjectFetchItem{},
			},
			wantErr:     true,
			errContains: "at least one item is required",
		},
		{
			name: "item with empty object ref at index 0",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: []ObjectFetchItem{
					{ObjectRef: ""},
				},
			},
			wantErr:     true,
			errContains: "object reference is required for item at index 0",
		},
		{
			name: "item with empty object ref at index 2",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: []ObjectFetchItem{
					{ObjectRef: "bucket:key1"},
					{ObjectRef: "bucket:key2"},
					{ObjectRef: ""},
				},
			},
			wantErr:     true,
			errContains: "object reference is required for item at index 2",
		},
		{
			name: "valid request",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: []ObjectFetchItem{
					{ObjectRef: "bucket:key1"},
					{ObjectRef: "bucket:key2"},
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := c.StreamFetchObjects(context.Background(), tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if !errors.IsInvalidInput(err) {
					t.Fatalf("expected InvalidInput error, got: %v", err)
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error to contain %q, got: %v", tc.errContains, err)
				}
				if ch != nil {
					t.Fatal("expected nil channel on validation error")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ch == nil {
					t.Fatal("expected non-nil channel")
				}
				// Drain channel to avoid goroutine leak.
				for range ch {
				}
			}
		})
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_TransportStreamError
// =============================================================================

func TestClient_StreamFetchObjects_TransportStreamError(t *testing.T) {
	transportErr := errors.NewUnavailable("transport", "stream creation failed")
	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return nil, transportErr
		},
	})

	ch, err := c.StreamFetchObjects(context.Background(), &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
	})

	if err == nil {
		t.Fatal("expected error from transport stream creation")
	}
	if errors.Code(err) != errors.CodeConnection {
		t.Fatalf("expected Connection error code (wrapped), got: %v", errors.Code(err))
	}
	if !strings.Contains(err.Error(), "failed to create stream") {
		t.Fatalf("expected error message to contain 'failed to create stream', got: %v", err)
	}
	if ch != nil {
		t.Fatal("expected nil channel on stream creation error")
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_Success
// =============================================================================

func TestClient_StreamFetchObjects_Success(t *testing.T) {
	callCount := 0
	responses := []*FetchObjectResponse{
		{
			ObjectRef: "bucket:key1",
			Data:      []byte("data1"),
			Found:     true,
		},
		{
			ObjectRef: "bucket:key2",
			Data:      []byte("data2"),
			Found:     true,
		},
	}

	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					if callCount < len(responses) {
						resp := responses[callCount]
						callCount++
						return resp, nil
					}
					return nil, io.EOF
				},
			}, nil
		},
	})

	ch, err := c.StreamFetchObjects(context.Background(), &StreamFetchObjectsRequest{
		ShardID: 1,
		Items: []ObjectFetchItem{
			{ObjectRef: "bucket:key1"},
			{ObjectRef: "bucket:key2"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	received := make([]*FetchObjectResponse, 0, 2)
	for resp := range ch {
		received = append(received, resp)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(received))
	}
	if received[0].ObjectRef != "bucket:key1" {
		t.Fatalf("expected first response ObjectRef %q, got %q", "bucket:key1", received[0].ObjectRef)
	}
	if string(received[0].Data) != "data1" {
		t.Fatalf("expected first response Data %q, got %q", "data1", received[0].Data)
	}
	if !received[0].Found {
		t.Fatal("expected first response Found to be true")
	}
	if received[1].ObjectRef != "bucket:key2" {
		t.Fatalf("expected second response ObjectRef %q, got %q", "bucket:key2", received[1].ObjectRef)
	}
	if string(received[1].Data) != "data2" {
		t.Fatalf("expected second response Data %q, got %q", "data2", received[1].Data)
	}
	if !received[1].Found {
		t.Fatal("expected second response Found to be true")
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_ContextCancellation
// =============================================================================

func TestClient_StreamFetchObjects_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	recvStarted := make(chan struct{})
	releaseRecv := make(chan struct{})

	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			var sent bool
			return &mockStream{
				recvFn: func() (interface{}, error) {
					if sent {
						return nil, io.EOF
					}
					sent = true
					close(recvStarted)
					<-releaseRecv
					return &FetchObjectResponse{Found: true}, nil
				},
			}, nil
		},
	})

	ch, err := c.StreamFetchObjects(ctx, &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	<-recvStarted
	cancel()
	close(releaseRecv)

	var gotCancelErr bool
	for resp := range ch {
		if resp.ErrorMessage != "" && strings.Contains(resp.ErrorMessage, "context canceled") {
			gotCancelErr = true
		}
	}

	if !gotCancelErr {
		t.Fatal("expected a response with context cancellation error message")
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_InvalidResponseType
// =============================================================================

func TestClient_StreamFetchObjects_InvalidResponseType(t *testing.T) {
	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			callCount := 0
			return &mockStream{
				recvFn: func() (interface{}, error) {
					callCount++
					if callCount == 1 {
						// Return wrong type.
						return "not-a-fetch-response", nil
					}
					// End the stream.
					return nil, io.EOF
				},
			}, nil
		},
	})

	ch, err := c.StreamFetchObjects(context.Background(), &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotTypeErr bool
	for resp := range ch {
		if resp.ErrorMessage != "" && strings.Contains(resp.ErrorMessage, "invalid response type") {
			gotTypeErr = true
		}
	}

	if !gotTypeErr {
		t.Fatal("expected a response with 'invalid response type' error message")
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_StreamRecvError
// =============================================================================

func TestClient_StreamFetchObjects_StreamRecvError(t *testing.T) {
	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					return nil, stderrors.New("network failure")
				},
			}, nil
		},
	})

	ch, err := c.StreamFetchObjects(context.Background(), &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotErr bool
	for resp := range ch {
		if resp.ErrorMessage != "" && strings.Contains(resp.ErrorMessage, "network failure") {
			gotErr = true
		}
	}

	if !gotErr {
		t.Fatal("expected a response with 'network failure' error message")
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_NilRecvResponse
// =============================================================================

func TestClient_StreamFetchObjects_NilRecvResponse(t *testing.T) {
	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					// Return nil response with no error, indicating stream ended.
					return nil, nil
				},
			}, nil
		},
	})

	ch, err := c.StreamFetchObjects(context.Background(), &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Channel should close with no responses.
	received := make([]*FetchObjectResponse, 0)
	for resp := range ch {
		received = append(received, resp)
	}

	if len(received) != 0 {
		t.Fatalf("expected 0 responses for nil recv, got %d", len(received))
	}
}

// =============================================================================
// TestIsStreamEnded
// =============================================================================

func TestIsStreamEnded(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "bare io.EOF",
			err:      io.EOF,
			expected: true,
		},
		{
			name:     "wrapped io.EOF",
			err:      errors.Wrap(io.EOF, errors.CodeConnection, "test.Recv", "stream read"),
			expected: true,
		},
		{
			name:     "io.ErrClosedPipe",
			err:      io.ErrClosedPipe,
			expected: true,
		},
		{
			name:     "wrapped io.ErrClosedPipe",
			err:      errors.Wrap(io.ErrClosedPipe, errors.CodeConnection, "test.Recv", "stream read"),
			expected: true,
		},
		{
			name:     "unrelated error whose text is EOF",
			err:      eofTextError{},
			expected: false,
		},
		{
			name:     "unrelated error whose text is the closed pipe message",
			err:      stderrors.New("io: read/write on closed pipe"),
			expected: false,
		},
		{
			name:     "other error",
			err:      stderrors.New("some other error"),
			expected: false,
		},
		{
			name:     "empty error message",
			err:      stderrors.New(""),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isStreamEnded(tc.err)
			if result != tc.expected {
				t.Fatalf("isStreamEnded(%v) = %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}

// eofTextError is an unrelated error whose text happens to be "EOF". It must
// not be mistaken for the end of a stream.
type eofTextError struct{}

func (eofTextError) Error() string { return "EOF" }

// drainStream starts StreamFetchObjects over a stream that returns recvErr
// on its first Recv and closeErr from Close, and returns every response
// sent plus the number of Close calls.
func drainStream(t *testing.T, recvErr, closeErr error) ([]*FetchObjectResponse, int) {
	t.Helper()
	closeCalls := 0
	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					return nil, recvErr
				},
				closeFn: func() error {
					closeCalls++
					return closeErr
				},
			}, nil
		},
	})

	ch, err := c.StreamFetchObjects(context.Background(), &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got []*FetchObjectResponse
	for resp := range ch {
		got = append(got, resp)
	}
	return got, closeCalls
}

func TestClient_StreamFetchObjects_WrappedEOFEndsCleanly(t *testing.T) {
	recvErr := errors.Wrap(io.EOF, errors.CodeConnection, "test.Recv", "stream read")
	got, closeCalls := drainStream(t, recvErr, nil)
	if len(got) != 0 {
		t.Fatalf("got %d responses, want 0 (wrapped io.EOF is a clean end): %+v", len(got), got[0])
	}
	if closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", closeCalls)
	}
}

func TestClient_StreamFetchObjects_ClosedPipeEndsCleanly(t *testing.T) {
	got, closeCalls := drainStream(t, io.ErrClosedPipe, nil)
	if len(got) != 0 {
		t.Fatalf("got %d responses, want 0 (io.ErrClosedPipe is a clean end)", len(got))
	}
	if closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", closeCalls)
	}
}

func TestClient_StreamFetchObjects_EOFTextErrorIsReported(t *testing.T) {
	got, closeCalls := drainStream(t, eofTextError{}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1 error response", len(got))
	}
	if got[0].Found || got[0].ErrorMessage != "EOF" {
		t.Fatalf("got %+v, want a not-found response carrying the EOF-text error", got[0])
	}
	if closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", closeCalls)
	}
}

func TestClient_StreamFetchObjects_EarlierErrorKeptOverCloseError(t *testing.T) {
	got, closeCalls := drainStream(t, stderrors.New("network failure"), stderrors.New("close error"))
	if len(got) != 1 {
		t.Fatalf("got %d responses, want only the earlier error", len(got))
	}
	if !strings.Contains(got[0].ErrorMessage, "network failure") {
		t.Fatalf("got ErrorMessage %q, want the earlier network failure", got[0].ErrorMessage)
	}
	if closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", closeCalls)
	}
}

// =============================================================================
// TestIndexToString
// =============================================================================

func TestIndexToString(t *testing.T) {
	tests := []struct {
		name     string
		index    int
		expected string
	}{
		{name: "zero", index: 0, expected: "0"},
		{name: "single digit 1", index: 1, expected: "1"},
		{name: "single digit 5", index: 5, expected: "5"},
		{name: "single digit 9", index: 9, expected: "9"},
		{name: "double digit 10", index: 10, expected: "10"},
		{name: "double digit 42", index: 42, expected: "42"},
		{name: "double digit 99", index: 99, expected: "99"},
		{name: "triple digit 100", index: 100, expected: "100"},
		{name: "triple digit 255", index: 255, expected: "255"},
		{name: "quad digit 1000", index: 1000, expected: "1000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := indexToString(tc.index)
			if result != tc.expected {
				t.Fatalf("indexToString(%d) = %q, want %q", tc.index, result, tc.expected)
			}
		})
	}
}

// =============================================================================
// TestValidateFetchObjectRequest
// =============================================================================

func TestValidateFetchObjectRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *FetchObjectRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request",
			req:         nil,
			wantErr:     true,
			errContains: "request is required",
		},
		{
			name: "zero shard ID",
			req: &FetchObjectRequest{
				ShardID:   0,
				ObjectRef: "bucket:key",
			},
			wantErr:     true,
			errContains: "shard ID is required",
		},
		{
			name: "empty object ref",
			req: &FetchObjectRequest{
				ShardID:   1,
				ObjectRef: "",
			},
			wantErr:     true,
			errContains: "object reference is required",
		},
		{
			name: "valid request",
			req: &FetchObjectRequest{
				ShardID:   1,
				ObjectRef: "bucket:key",
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFetchObjectRequest(tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if !errors.IsInvalidInput(err) {
					t.Fatalf("expected InvalidInput error, got: %v", err)
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error to contain %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// TestValidateStreamFetchObjectsRequest
// =============================================================================

func TestValidateStreamFetchObjectsRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *StreamFetchObjectsRequest
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil request",
			req:         nil,
			wantErr:     true,
			errContains: "request is required",
		},
		{
			name: "zero shard ID",
			req: &StreamFetchObjectsRequest{
				ShardID: 0,
				Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
			},
			wantErr:     true,
			errContains: "shard ID is required",
		},
		{
			name: "empty items",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items:   []ObjectFetchItem{},
			},
			wantErr:     true,
			errContains: "at least one item is required",
		},
		{
			name: "nil items slice",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items:   nil,
			},
			wantErr:     true,
			errContains: "at least one item is required",
		},
		{
			name: "empty object ref in first item",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: []ObjectFetchItem{
					{ObjectRef: ""},
				},
			},
			wantErr:     true,
			errContains: "object reference is required for item at index 0",
		},
		{
			name: "empty object ref in middle item",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: []ObjectFetchItem{
					{ObjectRef: "bucket:key1"},
					{ObjectRef: ""},
					{ObjectRef: "bucket:key3"},
				},
			},
			wantErr:     true,
			errContains: "object reference is required for item at index 1",
		},
		{
			name: "empty object ref at index 10",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: func() []ObjectFetchItem {
					items := make([]ObjectFetchItem, 11)
					for i := 0; i < 10; i++ {
						items[i] = ObjectFetchItem{ObjectRef: "bucket:key"}
					}
					items[10] = ObjectFetchItem{ObjectRef: ""}
					return items
				}(),
			},
			wantErr:     true,
			errContains: "object reference is required for item at index 10",
		},
		{
			name: "valid request with single item",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: []ObjectFetchItem{
					{ObjectRef: "bucket:key"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with multiple items",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: []ObjectFetchItem{
					{ObjectRef: "bucket:key1"},
					{ObjectRef: "bucket:key2"},
					{ObjectRef: "bucket:key3"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with checksums",
			req: &StreamFetchObjectsRequest{
				ShardID: 1,
				Items: []ObjectFetchItem{
					{ObjectRef: "bucket:key1", Checksum: []byte("abc")},
					{ObjectRef: "bucket:key2", Checksum: []byte("def")},
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStreamFetchObjectsRequest(tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if !errors.IsInvalidInput(err) {
					t.Fatalf("expected InvalidInput error, got: %v", err)
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error to contain %q, got: %v", tc.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// =============================================================================
// TestClient_FetchObject_ServiceAndMethodPropagation
// =============================================================================

func TestClient_FetchObject_ServiceAndMethodPropagation(t *testing.T) {
	var capturedService, capturedMethod string
	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedService = service
			capturedMethod = method
			return &FetchObjectResponse{Found: true}, nil
		},
	})

	_, err := c.FetchObject(context.Background(), &FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "bucket:key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedService != "quicbaas.ObjectTransfer" {
		t.Fatalf("expected service %q, got %q", "quicbaas.ObjectTransfer", capturedService)
	}
	if capturedMethod != "FetchObject" {
		t.Fatalf("expected method %q, got %q", "FetchObject", capturedMethod)
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_ServiceAndMethodPropagation
// =============================================================================

func TestClient_StreamFetchObjects_ServiceAndMethodPropagation(t *testing.T) {
	var capturedService, capturedMethod string
	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			capturedService = service
			capturedMethod = method
			return &mockStream{
				recvFn: func() (interface{}, error) {
					return nil, io.EOF
				},
			}, nil
		},
	})

	ch, err := c.StreamFetchObjects(context.Background(), &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "bucket:key"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain channel to allow goroutine to complete.
	for range ch {
	}

	if capturedService != "quicbaas.ObjectTransfer" {
		t.Fatalf("expected service %q, got %q", "quicbaas.ObjectTransfer", capturedService)
	}
	if capturedMethod != "StreamFetchObjects" {
		t.Fatalf("expected method %q, got %q", "StreamFetchObjects", capturedMethod)
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_ChannelBufferSize
// =============================================================================

func TestClient_StreamFetchObjects_ChannelBufferSize(t *testing.T) {
	// Verify the channel has buffer capacity equal to len(req.Items)
	// by sending all items without a receiver and confirming no deadlock.
	itemCount := 3
	callCount := 0
	responses := make([]*FetchObjectResponse, itemCount)
	for i := 0; i < itemCount; i++ {
		responses[i] = &FetchObjectResponse{
			ObjectRef: "bucket:key",
			Found:     true,
		}
	}

	c := NewClient(&mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					if callCount < len(responses) {
						resp := responses[callCount]
						callCount++
						return resp, nil
					}
					return nil, io.EOF
				},
			}, nil
		},
	})

	items := make([]ObjectFetchItem, itemCount)
	for i := 0; i < itemCount; i++ {
		items[i] = ObjectFetchItem{ObjectRef: "bucket:key"}
	}

	ch, err := c.StreamFetchObjects(context.Background(), &StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   items,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Drain all responses; the goroutine should complete without deadlock
	// because the channel is buffered to len(items).
	received := 0
	for range ch {
		received++
	}

	if received != itemCount {
		t.Fatalf("expected %d responses, got %d", itemCount, received)
	}
}

// =============================================================================
// TestClient_StreamFetchObjects_StreamCloseError
// =============================================================================

func TestClient_StreamFetchObjects_StreamCloseError(t *testing.T) {
	// A stream that ended cleanly but failed to close reports the close
	// error as one final response before the channel closes.
	got, closeCalls := drainStream(t, io.EOF, stderrors.New("close error"))
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1 close-error response", len(got))
	}
	if got[0].Found || !strings.Contains(got[0].ErrorMessage, "close error") {
		t.Fatalf("got %+v, want a not-found response carrying the close error", got[0])
	}
	if closeCalls != 1 {
		t.Fatalf("Close called %d times, want 1", closeCalls)
	}
}
