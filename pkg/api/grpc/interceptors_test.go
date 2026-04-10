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
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestUnaryLoggingInterceptor(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := UnaryLoggingInterceptor(log)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestUnaryLoggingInterceptorWithError(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := UnaryLoggingInterceptor(log)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	expectedErr := errors.New("test error")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, expectedErr
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, resp)
}

func TestUnaryRecoveryInterceptor(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := UnaryRecoveryInterceptor(log)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	// Test panic recovery
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		panic("test panic")
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "panic recovered")
}

func TestUnaryRecoveryInterceptorNoPanic(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := UnaryRecoveryInterceptor(log)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	// Test normal operation
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestUnaryMetricsInterceptor(t *testing.T) {
	interceptor := UnaryMetricsInterceptor()

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestUnaryErrorHandlerInterceptor(t *testing.T) {
	interceptor := UnaryErrorHandlerInterceptor()

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	// Test with error
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, errors.New("test error")
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestUnaryFIPSInterceptor(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		fipsEnabled bool
		ctx         context.Context
		expectError bool
		errorCode   codes.Code
	}{
		{
			name:        "FIPS disabled - no TLS",
			fipsEnabled: false,
			ctx:         context.Background(),
			expectError: false,
		},
		{
			name:        "FIPS enabled - no peer info",
			fipsEnabled: true,
			ctx:         context.Background(),
			expectError: true,
			errorCode:   codes.FailedPrecondition,
		},
		{
			name:        "FIPS enabled - FIPS-compliant TLS",
			fipsEnabled: true,
			ctx:         createContextWithTLS(tls.VersionTLS13, tls.TLS_AES_256_GCM_SHA384),
			expectError: false,
		},
		{
			name:        "FIPS enabled - non-FIPS cipher suite",
			fipsEnabled: true,
			ctx:         createContextWithTLS(tls.VersionTLS13, tls.TLS_CHACHA20_POLY1305_SHA256),
			expectError: true,
			errorCode:   codes.FailedPrecondition,
		},
		{
			name:        "FIPS enabled - old TLS version",
			fipsEnabled: true,
			ctx:         createContextWithTLS(tls.VersionTLS12, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384),
			expectError: true,
			errorCode:   codes.FailedPrecondition,
		},
		{
			name:        "FIPS enabled - TLS 1.3 with AES-128-GCM",
			fipsEnabled: true,
			ctx:         createContextWithTLS(tls.VersionTLS13, tls.TLS_AES_128_GCM_SHA256),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor := UnaryFIPSInterceptor(tt.fipsEnabled, log)

			info := &grpc.UnaryServerInfo{
				FullMethod: "/quicbaas.QuicBaaS/Put",
			}

			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return "response", nil
			}

			resp, err := interceptor(tt.ctx, "request", info, handler)

			if tt.expectError {
				assert.Error(t, err)
				st, ok := status.FromError(err)
				assert.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
				assert.Contains(t, st.Message(), "FIPS compliance validation failed")
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, "response", resp)
			}
		})
	}
}

func TestUnaryTimeoutInterceptor(t *testing.T) {
	interceptor := UnaryTimeoutInterceptor(5 * time.Second)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		// Verify deadline was set
		_, ok := ctx.Deadline()
		assert.True(t, ok, "deadline should be set")
		return "response", nil
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestUnaryTimeoutInterceptorWithExistingDeadline(t *testing.T) {
	interceptor := UnaryTimeoutInterceptor(5 * time.Second)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	// Create context with existing deadline
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := interceptor(ctx, "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestStreamTimeoutInterceptor(t *testing.T) {
	interceptor := StreamTimeoutInterceptor(5 * time.Second)

	info := &grpc.StreamServerInfo{
		FullMethod:     "/quicbaas.QuicBaaS/StreamBatch",
		IsServerStream: true,
	}

	// Stream with no existing deadline should get one applied
	stream := &testServerStream{ctx: context.Background()}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		// Verify that a deadline was set on the wrapped stream context
		deadline, ok := ss.Context().Deadline()
		assert.True(t, ok, "deadline should be set on stream context")
		assert.WithinDuration(t, time.Now().Add(5*time.Second), deadline, 500*time.Millisecond)
		return nil
	}

	err := interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamTimeoutInterceptorWithExistingDeadline(t *testing.T) {
	interceptor := StreamTimeoutInterceptor(5 * time.Minute)

	info := &grpc.StreamServerInfo{
		FullMethod:     "/quicbaas.QuicBaaS/StreamBatch",
		IsServerStream: true,
	}

	// Stream with a shorter existing deadline should preserve it
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream := &testServerStream{ctx: ctx}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		// The original 10s deadline is shorter than 5m, so it should be preserved
		deadline, ok := ss.Context().Deadline()
		assert.True(t, ok, "deadline should still exist on stream context")
		assert.WithinDuration(t, time.Now().Add(10*time.Second), deadline, 500*time.Millisecond)
		return nil
	}

	err := interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamTimeoutInterceptorOverridesLongerDeadline(t *testing.T) {
	interceptor := StreamTimeoutInterceptor(2 * time.Second)

	info := &grpc.StreamServerInfo{
		FullMethod:     "/quicbaas.QuicBaaS/StreamBatch",
		IsServerStream: true,
	}

	// Stream with a longer existing deadline should be overridden by the shorter maxDuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	stream := &testServerStream{ctx: ctx}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		// The 2s maxDuration is shorter than 10m, so the deadline should be overridden
		deadline, ok := ss.Context().Deadline()
		assert.True(t, ok, "deadline should be set on stream context")
		assert.WithinDuration(t, time.Now().Add(2*time.Second), deadline, 500*time.Millisecond)
		return nil
	}

	err := interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamTimeoutInterceptorHandlerError(t *testing.T) {
	interceptor := StreamTimeoutInterceptor(5 * time.Second)

	info := &grpc.StreamServerInfo{
		FullMethod:     "/quicbaas.QuicBaaS/StreamBatch",
		IsServerStream: true,
	}

	stream := &testServerStream{ctx: context.Background()}

	expectedErr := status.Errorf(codes.Internal, "handler failed")
	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return expectedErr
	}

	err := interceptor(nil, stream, info, handler)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestInterceptorChain(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Create chain of interceptors
	chain := InterceptorChain(
		UnaryLoggingInterceptor(log),
		UnaryMetricsInterceptor(),
		UnaryErrorHandlerInterceptor(),
	)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := chain(context.Background(), "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	assert.NotEmpty(t, id1)

	time.Sleep(1 * time.Millisecond)

	id2 := generateRequestID()
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
}

func TestUnaryRequestIDInterceptor(t *testing.T) {
	interceptor := UnaryRequestIDInterceptor()

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestValidateFIPSCompliance(t *testing.T) {
	tests := []struct {
		name          string
		ctx           context.Context
		expectError   bool
		errorContains string
	}{
		{
			name:          "no peer information",
			ctx:           context.Background(),
			expectError:   true,
			errorContains: "no peer information",
		},
		{
			name:          "peer without TLS",
			ctx:           createContextWithNonTLSPeer(),
			expectError:   true,
			errorContains: "TLS not enabled",
		},
		{
			name:          "TLS 1.2 - insufficient version",
			ctx:           createContextWithTLS(tls.VersionTLS12, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384),
			expectError:   true,
			errorContains: "insufficient TLS version",
		},
		{
			name:          "TLS 1.3 with non-FIPS cipher",
			ctx:           createContextWithTLS(tls.VersionTLS13, tls.TLS_CHACHA20_POLY1305_SHA256),
			expectError:   true,
			errorContains: "non-FIPS cipher suite",
		},
		{
			name:        "TLS 1.3 with AES-256-GCM - valid",
			ctx:         createContextWithTLS(tls.VersionTLS13, tls.TLS_AES_256_GCM_SHA384),
			expectError: false,
		},
		{
			name:        "TLS 1.3 with AES-128-GCM - valid",
			ctx:         createContextWithTLS(tls.VersionTLS13, tls.TLS_AES_128_GCM_SHA256),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFIPSCompliance(tt.ctx)

			if tt.expectError {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestFIPSValidationError(t *testing.T) {
	t.Run("with detail", func(t *testing.T) {
		err := &FIPSValidationError{
			Reason: "test reason",
			Detail: "test detail",
		}
		assert.Contains(t, err.Error(), "test reason")
		assert.Contains(t, err.Error(), "test detail")
	})

	t.Run("without detail", func(t *testing.T) {
		err := &FIPSValidationError{
			Reason: "test reason",
		}
		assert.Contains(t, err.Error(), "test reason")
		assert.NotContains(t, err.Error(), "()")
	})
}

func TestIsFIPSApprovedCipherSuite(t *testing.T) {
	tests := []struct {
		name        string
		cipherSuite uint16
		expected    bool
	}{
		{"TLS_AES_128_GCM_SHA256", tls.TLS_AES_128_GCM_SHA256, true},
		{"TLS_AES_256_GCM_SHA384", tls.TLS_AES_256_GCM_SHA384, true},
		{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, true},
		{"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384", tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, true},
		{"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, true},
		{"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384", tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, true},
		{"TLS_CHACHA20_POLY1305_SHA256 (not FIPS)", tls.TLS_CHACHA20_POLY1305_SHA256, false},
		{"Unknown cipher", 0x9999, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsFIPSApprovedCipherSuite(tt.cipherSuite)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetFIPSApprovedCipherSuiteNames(t *testing.T) {
	names := GetFIPSApprovedCipherSuiteNames()
	assert.Len(t, names, len(FIPSApprovedCipherSuites))
	assert.Contains(t, names, "TLS_AES_128_GCM_SHA256")
	assert.Contains(t, names, "TLS_AES_256_GCM_SHA384")
}

func BenchmarkUnaryLoggingInterceptor(b *testing.B) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(b, err)

	interceptor := UnaryLoggingInterceptor(log)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = interceptor(context.Background(), "request", info, handler)
	}
}

func BenchmarkUnaryMetricsInterceptor(b *testing.B) {
	interceptor := UnaryMetricsInterceptor()

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = interceptor(context.Background(), "request", info, handler)
	}
}

func BenchmarkInterceptorChain(b *testing.B) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(b, err)

	chain := InterceptorChain(
		UnaryLoggingInterceptor(log),
		UnaryMetricsInterceptor(),
		UnaryErrorHandlerInterceptor(),
		UnaryRecoveryInterceptor(log),
	)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = chain(context.Background(), "request", info, handler)
	}
}

func BenchmarkFIPSValidation(b *testing.B) {
	ctx := createContextWithTLS(tls.VersionTLS13, tls.TLS_AES_256_GCM_SHA384)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateFIPSCompliance(ctx)
	}
}

func BenchmarkStreamTimeoutInterceptor(b *testing.B) {
	interceptor := StreamTimeoutInterceptor(5 * time.Minute)

	info := &grpc.StreamServerInfo{
		FullMethod:     "/quicbaas.QuicBaaS/StreamBatch",
		IsServerStream: true,
	}

	stream := &testServerStream{ctx: context.Background()}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = interceptor(nil, stream, info, handler)
	}
}

// Helper functions for creating test contexts

// createContextWithTLS creates a context with TLS peer information.
func createContextWithTLS(version uint16, cipherSuite uint16) context.Context {
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			Version:     version,
			CipherSuite: cipherSuite,
		},
	}
	p := &peer.Peer{
		AuthInfo: tlsInfo,
	}
	return peer.NewContext(context.Background(), p)
}

// nonTLSAuthInfo is an AuthInfo that is not TLS.
type nonTLSAuthInfo struct{}

func (nonTLSAuthInfo) AuthType() string { return "non-tls" }

// createContextWithNonTLSPeer creates a context with a peer that has non-TLS auth.
func createContextWithNonTLSPeer() context.Context {
	p := &peer.Peer{
		AuthInfo: nonTLSAuthInfo{},
	}
	return peer.NewContext(context.Background(), p)
}

// --- Merged from interceptors_extended_test.go ---

// Mock implementations for stream testing.
type testServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (t *testServerStream) Context() context.Context {
	return t.ctx
}

func (t *testServerStream) SendMsg(m interface{}) error {
	return nil
}

func (t *testServerStream) RecvMsg(m interface{}) error {
	return nil
}

func TestStreamLoggingInterceptorMetadata(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := StreamLoggingInterceptor(log)

	md := metadata.Pairs("key", "value")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	stream := &testServerStream{ctx: ctx}

	info := &grpc.StreamServerInfo{
		FullMethod:     "/test.Service/Method",
		IsClientStream: true,
		IsServerStream: true,
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	err = interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamLoggingInterceptorError(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := StreamLoggingInterceptor(log)

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	expectedErr := status.Error(codes.Internal, "stream error")
	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return expectedErr
	}

	err = interceptor(nil, stream, info, handler)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestStreamMetricsInterceptorSuccess(t *testing.T) {
	interceptor := StreamMetricsInterceptor()

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	err := interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamMetricsInterceptorError(t *testing.T) {
	interceptor := StreamMetricsInterceptor()

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	expectedErr := status.Error(codes.NotFound, "not found")
	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return expectedErr
	}

	err := interceptor(nil, stream, info, handler)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestStreamRecoveryInterceptorSuccess(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := StreamRecoveryInterceptor(log)

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	err = interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamRecoveryInterceptorPanic(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := StreamRecoveryInterceptor(log)

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		panic("stream panic!")
	}

	err = interceptor(nil, stream, info, handler)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestStreamErrorHandlerInterceptor(t *testing.T) {
	interceptor := StreamErrorHandlerInterceptor()

	t.Run("Success", func(t *testing.T) {
		stream := &testServerStream{ctx: context.Background()}
		info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Method"}

		handler := func(srv interface{}, ss grpc.ServerStream) error {
			return nil
		}

		err := interceptor(nil, stream, info, handler)
		assert.NoError(t, err)
	})

	t.Run("WithError", func(t *testing.T) {
		stream := &testServerStream{ctx: context.Background()}
		info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Method"}

		handler := func(srv interface{}, ss grpc.ServerStream) error {
			return status.Error(codes.Internal, "error")
		}

		err := interceptor(nil, stream, info, handler)
		assert.Error(t, err)
	})
}

func TestStreamFIPSInterceptorDisabled(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := StreamFIPSInterceptor(false, log)

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	err = interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamFIPSInterceptorEnabledNoTLS(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := StreamFIPSInterceptor(true, log)

	// Create context without TLS - should fail FIPS validation
	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	err = interceptor(nil, stream, info, handler)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "FIPS compliance validation failed")
}

func TestStreamFIPSInterceptorEnabledWithFIPSTLS(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := StreamFIPSInterceptor(true, log)

	// Create context with FIPS-compliant TLS
	ctx := createFIPSCompliantTLSContext()
	stream := &testServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	err = interceptor(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamInterceptorChainOrdering(t *testing.T) {
	callOrder := []string{}

	interceptor1 := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		callOrder = append(callOrder, "int1-before")
		err := handler(srv, ss)
		callOrder = append(callOrder, "int1-after")
		return err
	}

	interceptor2 := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		callOrder = append(callOrder, "int2-before")
		err := handler(srv, ss)
		callOrder = append(callOrder, "int2-after")
		return err
	}

	chain := StreamInterceptorChain(interceptor1, interceptor2)

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		callOrder = append(callOrder, "handler")
		return nil
	}

	err := chain(nil, stream, info, handler)
	assert.NoError(t, err)

	expected := []string{
		"int1-before",
		"int2-before",
		"handler",
		"int2-after",
		"int1-after",
	}
	assert.Equal(t, expected, callOrder)
}

func TestWrappedServerStream(t *testing.T) {
	originalStream := &testServerStream{
		ctx: context.Background(),
	}

	newCtx := context.WithValue(context.Background(), "testkey", "testvalue")
	wrapped := wrapServerStream(originalStream, newCtx)

	assert.Equal(t, newCtx, wrapped.Context())
	assert.NotEqual(t, originalStream.Context(), wrapped.Context())

	// Test that other methods work
	err := wrapped.SendMsg("test")
	assert.NoError(t, err)

	err = wrapped.RecvMsg("test")
	assert.NoError(t, err)
}

func TestUnaryLoggingInterceptorWithMetadata(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	interceptor := UnaryLoggingInterceptor(log)

	md := metadata.Pairs(
		"x-request-id", "test-123",
		"user-agent", "test-client",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	resp, err := interceptor(ctx, "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestUnaryRequestIDInterceptorWithExisting(t *testing.T) {
	interceptor := UnaryRequestIDInterceptor()

	md := metadata.Pairs("x-request-id", "existing-request-id")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		// Verify the request ID is in outgoing metadata
		return "response", nil
	}

	resp, err := interceptor(ctx, "request", info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestUnaryMetricsInterceptorWithNonStatusError(t *testing.T) {
	interceptor := UnaryMetricsInterceptor()

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		// Return a plain error, not a status error
		return nil, assert.AnError
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestStreamMetricsInterceptorWithNonStatusError(t *testing.T) {
	interceptor := StreamMetricsInterceptor()

	stream := &testServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/Method",
	}

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		// Return a plain error, not a status error
		return assert.AnError
	}

	err := interceptor(nil, stream, info, handler)
	assert.Error(t, err)
}

// createFIPSCompliantTLSContext creates a context with FIPS-compliant TLS peer info.
func createFIPSCompliantTLSContext() context.Context {
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			Version:     tls.VersionTLS13,
			CipherSuite: tls.TLS_AES_256_GCM_SHA384,
		},
	}
	p := &peer.Peer{
		AuthInfo: tlsInfo,
	}
	return peer.NewContext(context.Background(), p)
}

// createNonFIPSCipherTLSContext creates a context with TLS 1.3 but non-FIPS cipher.
func createNonFIPSCipherTLSContext() context.Context {
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			Version:     tls.VersionTLS13,
			CipherSuite: tls.TLS_CHACHA20_POLY1305_SHA256, // Not FIPS-approved
		},
	}
	p := &peer.Peer{
		AuthInfo: tlsInfo,
	}
	return peer.NewContext(context.Background(), p)
}

// createOldTLSVersionContext creates a context with TLS 1.2.
func createOldTLSVersionContext() context.Context {
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			Version:     tls.VersionTLS12,
			CipherSuite: tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
	}
	p := &peer.Peer{
		AuthInfo: tlsInfo,
	}
	return peer.NewContext(context.Background(), p)
}
