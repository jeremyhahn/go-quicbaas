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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor returns a gRPC unary server interceptor that traces requests.
func (t *Tracer) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Skip if tracing is disabled
		if !t.IsEnabled() {
			return handler(ctx, req)
		}

		// Extract trace context from incoming metadata
		ctx = t.extractTraceContext(ctx)

		// Start span
		spanCtx, span := t.tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.service", info.FullMethod),
				attribute.String("rpc.method", info.FullMethod),
				AttrNetworkProtocol.String("grpc"),
			),
		)
		defer span.End()

		// Call handler
		resp, err := handler(spanCtx, req)

		// Record error if present
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			// Add gRPC status code
			if st, ok := status.FromError(err); ok {
				span.SetAttributes(
					attribute.String("rpc.grpc.status_code", st.Code().String()),
				)
			}
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return resp, err
	}
}

// StreamServerInterceptor returns a gRPC stream server interceptor that traces requests.
func (t *Tracer) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Skip if tracing is disabled
		if !t.IsEnabled() {
			return handler(srv, ss)
		}

		// Extract trace context from incoming metadata
		ctx := t.extractTraceContext(ss.Context())

		// Start span
		spanCtx, span := t.tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.service", info.FullMethod),
				attribute.String("rpc.method", info.FullMethod),
				attribute.Bool("rpc.grpc.is_client_stream", info.IsClientStream),
				attribute.Bool("rpc.grpc.is_server_stream", info.IsServerStream),
				AttrNetworkProtocol.String("grpc"),
			),
		)
		defer span.End()

		// Wrap server stream with traced context
		wrappedStream := &tracedServerStream{
			ServerStream: ss,
			ctx:          spanCtx,
		}

		// Call handler
		err := handler(srv, wrappedStream)

		// Record error if present
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			// Add gRPC status code
			if st, ok := status.FromError(err); ok {
				span.SetAttributes(
					attribute.String("rpc.grpc.status_code", st.Code().String()),
				)
			}
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return err
	}
}

// UnaryClientInterceptor returns a gRPC unary client interceptor that traces requests.
func (t *Tracer) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// Skip if tracing is disabled
		if !t.IsEnabled() {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		// Start span
		spanCtx, span := t.tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.service", method),
				attribute.String("rpc.method", method),
				AttrNetworkProtocol.String("grpc"),
			),
		)
		defer span.End()

		// Inject trace context into outgoing metadata
		spanCtx = t.injectTraceContext(spanCtx)

		// Call invoker
		err := invoker(spanCtx, method, req, reply, cc, opts...)

		// Record error if present
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			// Add gRPC status code
			if st, ok := status.FromError(err); ok {
				span.SetAttributes(
					attribute.String("rpc.grpc.status_code", st.Code().String()),
				)
			}
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return err
	}
}

// StreamClientInterceptor returns a gRPC stream client interceptor that traces requests.
func (t *Tracer) StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		// Skip if tracing is disabled
		if !t.IsEnabled() {
			return streamer(ctx, desc, cc, method, opts...)
		}

		// Start span
		spanCtx, span := t.tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.service", method),
				attribute.String("rpc.method", method),
				attribute.Bool("rpc.grpc.is_client_stream", desc.ClientStreams),
				attribute.Bool("rpc.grpc.is_server_stream", desc.ServerStreams),
				AttrNetworkProtocol.String("grpc"),
			),
		)

		// Inject trace context into outgoing metadata
		spanCtx = t.injectTraceContext(spanCtx)

		// Call streamer
		clientStream, err := streamer(spanCtx, desc, cc, method, opts...)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
			return nil, err
		}

		// Wrap client stream
		return &tracedClientStream{
			ClientStream: clientStream,
			span:         span,
		}, nil
	}
}

// extractTraceContext extracts trace context from incoming gRPC metadata.
func (t *Tracer) extractTraceContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}

	// Create carrier from metadata
	carrier := &metadataCarrier{md: md}

	// Extract trace context using global propagator
	return trace.ContextWithRemoteSpanContext(
		ctx,
		trace.SpanContextFromContext(
			propagation.TraceContext{}.Extract(ctx, carrier),
		),
	)
}

// injectTraceContext injects trace context into outgoing gRPC metadata.
func (t *Tracer) injectTraceContext(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	}

	// Create carrier from metadata
	carrier := &metadataCarrier{md: md}

	// Inject trace context using global propagator
	propagation.TraceContext{}.Inject(ctx, carrier)

	return metadata.NewOutgoingContext(ctx, carrier.md)
}

// metadataCarrier implements propagation.TextMapCarrier for gRPC metadata.
type metadataCarrier struct {
	md metadata.MD
}

// Get returns the value associated with the passed key.
func (mc *metadataCarrier) Get(key string) string {
	values := mc.md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Set stores the key-value pair.
func (mc *metadataCarrier) Set(key, value string) {
	mc.md.Set(key, value)
}

// Keys returns all keys in the carrier.
func (mc *metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(mc.md))
	for key := range mc.md {
		keys = append(keys, key)
	}
	return keys
}

// tracedServerStream wraps grpc.ServerStream with traced context.
type tracedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the traced context.
func (tss *tracedServerStream) Context() context.Context {
	return tss.ctx
}

// tracedClientStream wraps grpc.ClientStream with tracing.
type tracedClientStream struct {
	grpc.ClientStream
	span trace.Span
}

// RecvMsg traces message receive.
func (tcs *tracedClientStream) RecvMsg(m interface{}) error {
	err := tcs.ClientStream.RecvMsg(m)
	if err != nil {
		tcs.span.RecordError(err)
		tcs.span.SetStatus(codes.Error, err.Error())
		tcs.span.End()
	}
	return err
}

// SendMsg traces message send.
func (tcs *tracedClientStream) SendMsg(m interface{}) error {
	err := tcs.ClientStream.SendMsg(m)
	if err != nil {
		tcs.span.RecordError(err)
	}
	return err
}

// CloseSend traces stream close.
func (tcs *tracedClientStream) CloseSend() error {
	err := tcs.ClientStream.CloseSend()
	if err == nil {
		tcs.span.SetStatus(codes.Ok, "")
	}
	tcs.span.End()
	return err
}
