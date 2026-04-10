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

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// dummyStream implements grpc.ServerStream for tests.
type dummyStream struct{ ctx context.Context }

func (d *dummyStream) SetHeader(metadata.MD) error  { return nil }
func (d *dummyStream) SendHeader(metadata.MD) error { return nil }
func (d *dummyStream) SetTrailer(metadata.MD)       {}
func (d *dummyStream) Context() context.Context     { return d.ctx }
func (d *dummyStream) SendMsg(any) error            { return nil }
func (d *dummyStream) RecvMsg(any) error            { return nil }

func TestStreamInterceptorChain_Order(t *testing.T) {
	var order []string
	i1 := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		order = append(order, "i1")
		return handler(srv, ss)
	}
	i2 := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		order = append(order, "i2")
		return handler(srv, ss)
	}
	chain := StreamInterceptorChain(i1, i2)
	err := chain(nil, &dummyStream{ctx: context.Background()}, &grpc.StreamServerInfo{FullMethod: "/test"}, func(interface{}, grpc.ServerStream) error {
		order = append(order, "handler")
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"i1", "i2", "handler"}, order)
}

func TestStreamInterceptors_Basics(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	stream := &dummyStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Stream"}
	handler := func(s interface{}, ss grpc.ServerStream) error { return nil }

	// Logging
	err := StreamLoggingInterceptor(log)(nil, stream, info, handler)
	assert.NoError(t, err)
	// Metrics
	err = StreamMetricsInterceptor()(nil, stream, info, handler)
	assert.NoError(t, err)
	// Error handler should translate error
	errBoom := errors.New("boom")
	e := StreamErrorHandlerInterceptor()(nil, stream, info, func(s interface{}, ss grpc.ServerStream) error { return errBoom })
	_, ok := status.FromError(e)
	assert.True(t, ok)
	// FIPS disabled - should pass without TLS
	err = StreamFIPSInterceptor(false, log)(nil, stream, info, handler)
	assert.NoError(t, err)
	// FIPS enabled without TLS - should fail
	err = StreamFIPSInterceptor(true, log)(nil, stream, info, handler)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestStreamFIPSInterceptor_WithFIPSCompliantTLS(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	// Create a FIPS-compliant TLS context
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			Version:     tls.VersionTLS13,
			CipherSuite: tls.TLS_AES_256_GCM_SHA384,
		},
	}
	p := &peer.Peer{AuthInfo: tlsInfo}
	ctx := peer.NewContext(context.Background(), p)

	stream := &dummyStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Stream"}
	handler := func(s interface{}, ss grpc.ServerStream) error { return nil }

	// FIPS enabled with compliant TLS - should pass
	err := StreamFIPSInterceptor(true, log)(nil, stream, info, handler)
	assert.NoError(t, err)
}

func TestStreamFIPSInterceptor_WithNonFIPSCipher(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	// Create a TLS context with non-FIPS cipher
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			Version:     tls.VersionTLS13,
			CipherSuite: tls.TLS_CHACHA20_POLY1305_SHA256, // Not FIPS-approved
		},
	}
	p := &peer.Peer{AuthInfo: tlsInfo}
	ctx := peer.NewContext(context.Background(), p)

	stream := &dummyStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Stream"}
	handler := func(s interface{}, ss grpc.ServerStream) error { return nil }

	// FIPS enabled with non-compliant cipher - should fail
	err := StreamFIPSInterceptor(true, log)(nil, stream, info, handler)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "non-FIPS cipher suite")
}

func TestStreamFIPSInterceptor_WithOldTLSVersion(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	// Create a TLS context with old TLS version
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			Version:     tls.VersionTLS12,
			CipherSuite: tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
	}
	p := &peer.Peer{AuthInfo: tlsInfo}
	ctx := peer.NewContext(context.Background(), p)

	stream := &dummyStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Stream"}
	handler := func(s interface{}, ss grpc.ServerStream) error { return nil }

	// FIPS enabled with old TLS version - should fail
	err := StreamFIPSInterceptor(true, log)(nil, stream, info, handler)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "insufficient TLS version")
}

func TestWrapServerStream_Context(t *testing.T) {
	base := &dummyStream{ctx: context.Background()}
	newCtx := context.WithValue(context.Background(), struct{}{}, 1)
	wrapped := wrapServerStream(base, newCtx)
	assert.Equal(t, newCtx, wrapped.Context())
}
