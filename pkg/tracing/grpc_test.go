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

package tracing

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryServerInterceptor(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		method      string
		handlerErr  error
		wantSpans   int
		checkStatus bool
	}{
		{
			name:       "successful request",
			enabled:    true,
			method:     "/quicbaas.QuicBaaS/Get",
			handlerErr: nil,
			wantSpans:  1,
		},
		{
			name:        "failed request",
			enabled:     true,
			method:      "/quicbaas.QuicBaaS/Put",
			handlerErr:  errors.New("internal error"),
			wantSpans:   1,
			checkStatus: true,
		},
		{
			name:       "tracing disabled",
			enabled:    false,
			method:     "/quicbaas.QuicBaaS/Get",
			handlerErr: nil,
			wantSpans:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			interceptor := tracer.UnaryServerInterceptor()

			// Mock handler
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return "response", tt.handlerErr
			}

			// Mock unary server info
			info := &grpc.UnaryServerInfo{
				FullMethod: tt.method,
			}

			// Create context with metadata
			ctx := metadata.NewIncomingContext(context.Background(), metadata.New(nil))

			// Call interceptor
			resp, err := interceptor(ctx, "request", info, handler)

			// Verify response
			if tt.handlerErr != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.handlerErr, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, "response", resp)
			}

			// Verify spans
			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, tt.method, span.Name())

				// Check attributes
				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}
				assert.True(t, hasAttr("rpc.system"))
				assert.True(t, hasAttr("rpc.method"))
			}
		})
	}
}

func TestStreamServerInterceptor(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	interceptor := tracer.StreamServerInterceptor()

	// Mock stream server
	ss := &mockServerStream{
		ctx: metadata.NewIncomingContext(context.Background(), metadata.New(nil)),
	}

	// Mock stream handler
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		// Verify wrapped stream has traced context
		assert.NotNil(t, stream.Context())
		return nil
	}

	// Mock stream server info
	info := &grpc.StreamServerInfo{
		FullMethod:     "/quicbaas.QuicBaaS/Watch",
		IsClientStream: false,
		IsServerStream: true,
	}

	// Call interceptor
	err := interceptor(nil, ss, info, handler)
	assert.NoError(t, err)

	// Verify span
	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, info.FullMethod, span.Name())

	// Check stream attributes
	attrs := span.Attributes()
	hasAttr := func(key, expected string) bool {
		for _, attr := range attrs {
			if string(attr.Key) == key {
				return true
			}
		}
		return false
	}
	assert.True(t, hasAttr("rpc.grpc.is_server_stream", ""))
}

func TestUnaryClientInterceptor(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-client",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	interceptor := tracer.UnaryClientInterceptor()

	// Mock invoker
	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		// Verify outgoing metadata has trace context
		md, ok := metadata.FromOutgoingContext(ctx)
		assert.True(t, ok)
		assert.NotNil(t, md)
		return nil
	}

	method := "/quicbaas.QuicBaaS/Get"
	ctx := context.Background()

	// Call interceptor
	err := interceptor(ctx, method, nil, nil, nil, invoker)
	assert.NoError(t, err)

	// Verify span
	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, method, span.Name())
}

func TestMetadataCarrier(t *testing.T) {
	md := metadata.New(nil)
	carrier := &metadataCarrier{md: md}

	// Test Set
	carrier.Set("traceparent", "00-trace-id-span-id-01")
	carrier.Set("tracestate", "key=value")

	// Test Get
	assert.Equal(t, "00-trace-id-span-id-01", carrier.Get("traceparent"))
	assert.Equal(t, "key=value", carrier.Get("tracestate"))

	// Test Keys
	keys := carrier.Keys()
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "traceparent")
	assert.Contains(t, keys, "tracestate")

	// Test Get non-existent key
	assert.Equal(t, "", carrier.Get("non-existent"))
}

func TestTraceContextPropagation(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	// Create parent span
	ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")

	// Inject trace context
	ctx = tracer.injectTraceContext(ctx)

	// Extract trace context
	ctx = tracer.extractTraceContext(ctx)

	// Create child span
	_, childSpan := tracer.StartSpan(ctx, "child")

	childSpan.End()
	parentSpan.End()

	// Verify parent-child relationship
	spans := sr.Ended()
	require.Len(t, spans, 2)

	// Both spans should have the same trace ID
	assert.Equal(t, spans[0].SpanContext().TraceID(), spans[1].SpanContext().TraceID())
}

func TestGRPCInterceptor_ErrorHandling(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	interceptor := tracer.UnaryServerInterceptor()

	testErr := status.Error(codes.NotFound, "key not found")

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, testErr
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Get",
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(nil))

	_, err := interceptor(ctx, "request", info, handler)
	assert.Error(t, err)
	assert.Equal(t, testErr, err)

	// Verify span recorded error
	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	// Check for error events
	events := span.Events()
	hasErrorEvent := false
	for _, event := range events {
		if event.Name == "exception" {
			hasErrorEvent = true
			break
		}
	}
	assert.True(t, hasErrorEvent)
}

// Mock server stream for testing.
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (mss *mockServerStream) Context() context.Context {
	return mss.ctx
}

func (mss *mockServerStream) SendMsg(m interface{}) error {
	return nil
}

func (mss *mockServerStream) RecvMsg(m interface{}) error {
	return nil
}

func BenchmarkUnaryServerInterceptor(b *testing.B) {
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.NeverSample()),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "bench-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	interceptor := tracer.UnaryServerInterceptor()

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Get",
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(nil))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = interceptor(ctx, "request", info, handler)
	}
}

// =============================================================================
// Consolidated from grpc_comprehensive_test.go
// =============================================================================

// TestStreamClientInterceptor tests the stream client interceptor.
func TestStreamClientInterceptor(t *testing.T) {
	tests := []struct {
		name          string
		enabled       bool
		streamerErr   error
		wantSpanCount int
	}{
		{
			name:          "successful stream",
			enabled:       true,
			streamerErr:   nil,
			wantSpanCount: 1,
		},
		{
			name:          "streamer error",
			enabled:       true,
			streamerErr:   errors.New("stream failed"),
			wantSpanCount: 1,
		},
		{
			name:          "tracing disabled",
			enabled:       false,
			streamerErr:   nil,
			wantSpanCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			interceptor := tracer.StreamClientInterceptor()

			desc := &grpc.StreamDesc{
				ClientStreams: true,
				ServerStreams: true,
			}

			streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				if tt.streamerErr != nil {
					return nil, tt.streamerErr
				}
				return &mockClientStream{}, nil
			}

			stream, err := interceptor(context.Background(), desc, nil, "/test.Service/StreamMethod", streamer)

			if tt.streamerErr != nil {
				assert.Error(t, err)
				assert.Nil(t, stream)
			} else {
				assert.NoError(t, err)
				if tt.enabled {
					require.NotNil(t, stream)
					// Close the stream to end the span
					err := stream.CloseSend()
					assert.NoError(t, err)
				}
			}

			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpanCount)
		})
	}
}

// TestTracedClientStream_RecvMsg tests RecvMsg functionality.
func TestTracedClientStream_RecvMsg(t *testing.T) {
	tests := []struct {
		name    string
		recvErr error
		wantErr bool
	}{
		{
			name:    "successful recv",
			recvErr: nil,
			wantErr: false,
		},
		{
			name:    "recv with error",
			recvErr: errors.New("recv failed"),
			wantErr: true,
		},
		{
			name:    "recv with EOF",
			recvErr: io.EOF,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			_, span := provider.Tracer("test").Start(context.Background(), "test-recv")

			mockStream := &mockClientStream{recvErr: tt.recvErr}
			traced := &tracedClientStream{
				ClientStream: mockStream,
				span:         span,
			}

			err := traced.RecvMsg(nil)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.recvErr, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestTracedClientStream_SendMsg tests SendMsg functionality.
func TestTracedClientStream_SendMsg(t *testing.T) {
	tests := []struct {
		name    string
		sendErr error
		wantErr bool
	}{
		{
			name:    "successful send",
			sendErr: nil,
			wantErr: false,
		},
		{
			name:    "send with error",
			sendErr: errors.New("send failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			_, span := provider.Tracer("test").Start(context.Background(), "test-send")

			mockStream := &mockClientStream{sendErr: tt.sendErr}
			traced := &tracedClientStream{
				ClientStream: mockStream,
				span:         span,
			}

			err := traced.SendMsg("message")
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.sendErr, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestTracedClientStream_CloseSend tests CloseSend functionality.
func TestTracedClientStream_CloseSend(t *testing.T) {
	tests := []struct {
		name     string
		closeErr error
		wantErr  bool
	}{
		{
			name:     "successful close",
			closeErr: nil,
			wantErr:  false,
		},
		{
			name:     "close with error",
			closeErr: errors.New("close failed"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			_, span := provider.Tracer("test").Start(context.Background(), "test-close")

			mockStream := &mockClientStream{closeErr: tt.closeErr}
			traced := &tracedClientStream{
				ClientStream: mockStream,
				span:         span,
			}

			err := traced.CloseSend()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.closeErr, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestStreamServerInterceptor_WithError tests stream server interceptor error handling.
func TestStreamServerInterceptor_WithError(t *testing.T) {
	tests := []struct {
		name       string
		handlerErr error
		statusErr  bool
	}{
		{
			name:       "handler error",
			handlerErr: errors.New("handler failed"),
			statusErr:  false,
		},
		{
			name:       "grpc status error",
			handlerErr: status.Error(codes.Internal, "internal error"),
			statusErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     true,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			interceptor := tracer.StreamServerInterceptor()

			ss := &mockServerStream{
				ctx: metadata.NewIncomingContext(context.Background(), metadata.New(nil)),
			}

			handler := func(srv interface{}, stream grpc.ServerStream) error {
				return tt.handlerErr
			}

			info := &grpc.StreamServerInfo{
				FullMethod:     "/test.Service/Stream",
				IsClientStream: true,
				IsServerStream: true,
			}

			err := interceptor(nil, ss, info, handler)
			assert.Error(t, err)
			assert.Equal(t, tt.handlerErr, err)

			spans := sr.Ended()
			require.Len(t, spans, 1)

			// Verify error was recorded
			events := spans[0].Events()
			hasError := false
			for _, event := range events {
				if event.Name == "exception" {
					hasError = true
					break
				}
			}
			assert.True(t, hasError)
		})
	}
}

// TestUnaryClientInterceptor_WithError tests client interceptor error handling.
func TestUnaryClientInterceptor_WithError(t *testing.T) {
	tests := []struct {
		name       string
		invokerErr error
		statusErr  bool
	}{
		{
			name:       "invoker error",
			invokerErr: errors.New("call failed"),
			statusErr:  false,
		},
		{
			name:       "grpc status error",
			invokerErr: status.Error(codes.Unavailable, "service unavailable"),
			statusErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     true,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			interceptor := tracer.UnaryClientInterceptor()

			invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				return tt.invokerErr
			}

			err := interceptor(context.Background(), "/test.Service/Method", nil, nil, nil, invoker)
			assert.Error(t, err)
			assert.Equal(t, tt.invokerErr, err)

			spans := sr.Ended()
			require.Len(t, spans, 1)
		})
	}
}

// TestTracedServerStream_Context tests the traced server stream context.
func TestTracedServerStream_Context(t *testing.T) {
	originalCtx := context.WithValue(context.Background(), "original", "value")
	tracedCtx := context.WithValue(context.Background(), "traced", "yes")

	mockStream := &mockServerStream{ctx: originalCtx}
	traced := &tracedServerStream{
		ServerStream: mockStream,
		ctx:          tracedCtx,
	}

	// Verify traced context is returned, not original
	assert.NotEqual(t, originalCtx, traced.Context())
	assert.Equal(t, tracedCtx, traced.Context())
	assert.Equal(t, "yes", traced.Context().Value("traced"))
}

// TestMetadataCarrier_EmptyMetadata tests metadata carrier with empty metadata.
func TestMetadataCarrier_EmptyMetadata(t *testing.T) {
	md := metadata.New(nil)
	carrier := &metadataCarrier{md: md}

	// Test Get on empty metadata
	assert.Equal(t, "", carrier.Get("nonexistent"))

	// Test Keys on empty metadata
	keys := carrier.Keys()
	assert.Empty(t, keys)

	// Test Set
	carrier.Set("key", "value")
	assert.Equal(t, "value", carrier.Get("key"))

	keys = carrier.Keys()
	assert.Len(t, keys, 1)
	assert.Contains(t, keys, "key")
}

// Mock client stream for testing.
type mockClientStream struct {
	grpc.ClientStream
	recvErr  error
	sendErr  error
	closeErr error
}

func (m *mockClientStream) RecvMsg(msg interface{}) error {
	return m.recvErr
}

func (m *mockClientStream) SendMsg(msg interface{}) error {
	return m.sendErr
}

func (m *mockClientStream) CloseSend() error {
	return m.closeErr
}
