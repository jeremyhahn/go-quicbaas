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

package grpc

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

var (
	// Package-level metrics for gRPC interceptors.
	grpcMetricsOnce     sync.Once
	grpcRequestsTotal   *metrics.CounterVec
	grpcRequestDuration *metrics.HistogramVec
	grpcStreamsTotal    *metrics.CounterVec
	grpcStreamDuration  *metrics.HistogramVec
	grpcActiveStreams   *metrics.Gauge
)

func initGRPCMetrics() {
	grpcMetricsOnce.Do(func() {
		grpcRequestsTotal = metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc_interceptor",
			Name:      "requests_total",
			Help:      "Total gRPC requests by method and status",
		}, []string{"method", "status"})

		grpcRequestDuration = metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc_interceptor",
			Name:      "request_duration_seconds",
			Help:      "gRPC request duration in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"method"})

		grpcStreamsTotal = metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc_interceptor",
			Name:      "streams_total",
			Help:      "Total gRPC streams by method and status",
		}, []string{"method", "status"})

		grpcStreamDuration = metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc_interceptor",
			Name:      "stream_duration_seconds",
			Help:      "gRPC stream duration in seconds",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"method"})

		grpcActiveStreams = metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc_interceptor",
			Name:      "active_streams",
			Help:      "Number of active gRPC streams",
		})
	})
}

// InterceptorChain combines multiple unary interceptors into a single chain.
func InterceptorChain(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Build chain in reverse order
		chain := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor := interceptors[i]
			next := chain
			chain = func(currentCtx context.Context, currentReq interface{}) (interface{}, error) {
				return interceptor(currentCtx, currentReq, info, next)
			}
		}
		return chain(ctx, req)
	}
}

// StreamInterceptorChain combines multiple stream interceptors into a single chain.
func StreamInterceptorChain(interceptors ...grpc.StreamServerInterceptor) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Build chain in reverse order
		chain := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor := interceptors[i]
			next := chain
			chain = func(currentSrv interface{}, currentStream grpc.ServerStream) error {
				return interceptor(currentSrv, currentStream, info, next)
			}
		}
		return chain(srv, ss)
	}
}

// UnaryLoggingInterceptor logs all unary RPC requests and responses.
func UnaryLoggingInterceptor(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		// Extract metadata
		md, _ := metadata.FromIncomingContext(ctx)

		// Log request
		log.WithFields(map[string]interface{}{
			"method":   info.FullMethod,
			"metadata": md,
		}).Debug("Unary RPC request")

		// Call handler
		resp, err := handler(ctx, req)

		// Log response
		duration := time.Since(start)
		fields := map[string]interface{}{
			"method":   info.FullMethod,
			"duration": duration.String(),
		}

		if err != nil {
			fields["error"] = err.Error()
			log.WithFields(fields).Warn("Unary RPC failed")
		} else {
			log.WithFields(fields).Debug("Unary RPC succeeded")
		}

		return resp, err
	}
}

// StreamLoggingInterceptor logs all streaming RPC requests.
func StreamLoggingInterceptor(log *logger.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		// Extract metadata
		md, _ := metadata.FromIncomingContext(ss.Context())

		// Log stream start
		log.WithFields(map[string]interface{}{
			"method":           info.FullMethod,
			"is_client_stream": info.IsClientStream,
			"is_server_stream": info.IsServerStream,
			"metadata":         md,
		}).Debug("Stream RPC started")

		// Call handler
		err := handler(srv, ss)

		// Log stream end
		duration := time.Since(start)
		fields := map[string]interface{}{
			"method":   info.FullMethod,
			"duration": duration.String(),
		}

		if err != nil {
			fields["error"] = err.Error()
			log.WithFields(fields).Warn("Stream RPC failed")
		} else {
			log.WithFields(fields).Debug("Stream RPC completed")
		}

		return err
	}
}

// UnaryMetricsInterceptor tracks Prometheus metrics for unary RPCs.
func UnaryMetricsInterceptor() grpc.UnaryServerInterceptor {
	// Initialize metrics once
	initGRPCMetrics()

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		// Call handler
		resp, err := handler(ctx, req)

		// Record metrics
		duration := time.Since(start).Seconds()
		method := info.FullMethod
		statusCode := "success"
		if err != nil {
			if st, ok := status.FromError(err); ok {
				statusCode = st.Code().String()
			} else {
				statusCode = "error"
			}
		}

		grpcRequestsTotal.With(method, statusCode).Inc()
		grpcRequestDuration.With(method).Observe(duration)

		return resp, err
	}
}

// StreamMetricsInterceptor tracks Prometheus metrics for streaming RPCs.
func StreamMetricsInterceptor() grpc.StreamServerInterceptor {
	// Initialize metrics once
	initGRPCMetrics()

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		grpcActiveStreams.Inc()
		defer grpcActiveStreams.Dec()

		// Call handler
		err := handler(srv, ss)

		// Record metrics
		duration := time.Since(start).Seconds()
		method := info.FullMethod
		statusCode := "success"
		if err != nil {
			if st, ok := status.FromError(err); ok {
				statusCode = st.Code().String()
			} else {
				statusCode = "error"
			}
		}

		grpcStreamsTotal.With(method, statusCode).Inc()
		grpcStreamDuration.With(method).Observe(duration)

		return err
	}
}

// UnaryRecoveryInterceptor recovers from panics in unary RPCs.
func UnaryRecoveryInterceptor(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				// Log the panic with stack trace
				log.WithFields(map[string]interface{}{
					"method": info.FullMethod,
					"panic":  r,
					"stack":  string(debug.Stack()),
				}).Error("Panic recovered in unary RPC")

				// Return internal error to client
				err = status.Errorf(codes.Internal, "internal server error: panic recovered")
			}
		}()

		return handler(ctx, req)
	}
}

// StreamRecoveryInterceptor recovers from panics in streaming RPCs.
func StreamRecoveryInterceptor(log *logger.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				// Log the panic with stack trace
				log.WithFields(map[string]interface{}{
					"method": info.FullMethod,
					"panic":  r,
					"stack":  string(debug.Stack()),
				}).Error("Panic recovered in stream RPC")

				// Return internal error to client
				err = status.Errorf(codes.Internal, "internal server error: panic recovered")
			}
		}()

		return handler(srv, ss)
	}
}

// UnaryErrorHandlerInterceptor standardizes error responses.
func UnaryErrorHandlerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		// Convert error to proper gRPC status
		if err != nil {
			err = toProtoError(err)
		}

		return resp, err
	}
}

// StreamErrorHandlerInterceptor standardizes error responses for streams.
func StreamErrorHandlerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		err := handler(srv, ss)
		// Convert error to proper gRPC status
		if err != nil {
			err = toProtoError(err)
		}

		return err
	}
}

// UnaryRequestIDInterceptor adds a request ID to the context.
func UnaryRequestIDInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Extract or generate request ID
		md, ok := metadata.FromIncomingContext(ctx)
		var requestID string
		if ok {
			if ids := md.Get("x-request-id"); len(ids) > 0 {
				requestID = ids[0]
			}
		}

		if requestID == "" {
			requestID = generateRequestID()
		}

		// Add to outgoing metadata
		ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", requestID)

		return handler(ctx, req)
	}
}

// generateRequestID generates a simple request ID.
func generateRequestID() string {
	// Simple timestamp-based ID for now
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// UnaryTimeoutInterceptor enforces a default timeout if none is set.
func UnaryTimeoutInterceptor(defaultTimeout time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Check if context already has a deadline
		if _, ok := ctx.Deadline(); !ok {
			// No deadline set, apply default timeout
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
			defer cancel()
		}

		return handler(ctx, req)
	}
}

// StreamTimeoutInterceptor enforces a maximum stream lifetime if no deadline is set.
// If the stream's context already has a shorter deadline, it is preserved. Otherwise,
// the maxDuration is applied as the stream deadline to prevent streams from hanging
// indefinitely.
func StreamTimeoutInterceptor(maxDuration time.Duration) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()

		existingDeadline, hasDeadline := ctx.Deadline()
		if !hasDeadline {
			// No deadline set, apply max stream duration
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, maxDuration)
			defer cancel()
			return handler(srv, wrapServerStream(ss, ctx))
		}

		// Existing deadline is set; only override if maxDuration is shorter
		maxDeadline := time.Now().Add(maxDuration)
		if maxDeadline.Before(existingDeadline) {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, maxDeadline)
			defer cancel()
			return handler(srv, wrapServerStream(ss, ctx))
		}

		// Existing deadline is already shorter, preserve it
		return handler(srv, ss)
	}
}

// wrappedServerStream wraps grpc.ServerStream to allow context modification.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

// wrapServerStream creates a wrapped server stream with modified context.
func wrapServerStream(ss grpc.ServerStream, ctx context.Context) grpc.ServerStream {
	return &wrappedServerStream{
		ServerStream: ss,
		ctx:          ctx,
	}
}
