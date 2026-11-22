// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsealedwindow "github.com/jeremyhahn/go-qrdb/pkg/api/sealedwindow"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// toggleSealed is a live barrier probe a test unseals.
type toggleSealed struct{ sealed atomic.Bool }

func (p *toggleSealed) IsSealed() bool { return p.sealed.Load() }

// sealedGRPCMethods picks an allowlisted unseal method and an allowlisted
// non-transition method.
func sealedGRPCMethods(t *testing.T) (unseal, other string) {
	t.Helper()
	transition := sealedwindow.GRPCFullMethodTransition()
	for m := range transition {
		if strings.HasSuffix(m, "Unseal") {
			unseal = m
			break
		}
	}
	for _, m := range sealedwindow.GRPCFullMethodAllowlist() {
		if _, ok := transition[m]; !ok {
			other = m
			break
		}
	}
	require.NotEmpty(t, unseal)
	require.NotEmpty(t, other)
	return unseal, other
}

func callUnary(icpt ggrpc.UnaryServerInterceptor, method string, h ggrpc.UnaryHandler) error {
	_, err := icpt(context.Background(), nil, &ggrpc.UnaryServerInfo{FullMethod: method}, h)
	return err
}

// One listener probe serves the sealed gRPC listener and the mounted qrdb
// REST handler. A gRPC unseal waits for a request in flight on the other
// gRPC verb, and once it has unsealed the barrier, the anonymous REST surface
// refuses as closed rather than serving against the unsealed barrier.
func TestSealedWindowAdmission_ConcurrentUnsealAcrossTransports(t *testing.T) {
	barrier := &toggleSealed{}
	barrier.sealed.Store(true)
	listener := qrdbsdk.NewSealedWindowListenerProbe(barrier, nil)
	icpt := sealedAdmissionUnaryInterceptor(listener)
	unsealMethod, otherMethod := sealedGRPCMethods(t)

	inFlight := make(chan struct{})
	finish := make(chan struct{})
	otherDone := make(chan error, 1)
	go func() {
		otherDone <- callUnary(icpt, otherMethod, func(context.Context, any) (any, error) {
			close(inFlight)
			<-finish
			return nil, nil
		})
	}()
	<-inFlight

	var unsealed atomic.Bool
	unsealDone := make(chan error, 1)
	go func() {
		unsealDone <- callUnary(icpt, unsealMethod, func(context.Context, any) (any, error) {
			barrier.sealed.Store(false)
			unsealed.Store(true)
			return nil, nil
		})
	}()
	time.Sleep(150 * time.Millisecond)
	assert.False(t, unsealed.Load(), "unseal ran while an admitted request was in flight")

	close(finish)
	require.NoError(t, <-otherDone)
	require.NoError(t, <-unsealDone)
	require.True(t, unsealed.Load())

	// The barrier is unsealed: the shared probe closes every transport.
	err := callUnary(icpt, otherMethod, func(context.Context, any) (any, error) {
		t.Fatal("served a gRPC request against an unsealed barrier")
		return nil, nil
	})
	assert.Equal(t, codes.Unavailable, status.Code(err))

	handler := qrdbsdk.NewSealedWindowHandler(nil, nil, nil, listener)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/barrier/officers", strings.NewReader("{}")))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "body %q", rec.Body.String())
	assert.Contains(t, rec.Body.String(), qrdbsealedwindow.ClosedMessage)
}

// A server with no sealed-window listener refuses every sealed request.
func TestSealedWindowListener_FailsClosedWithoutAWindow(t *testing.T) {
	s := &Server{}
	listener := s.sealedListener()
	assert.False(t, listener.IsSealed() && listener.EnrollmentOpen(context.Background()))
	err := callUnary(sealedAdmissionUnaryInterceptor(listener), "/qrdb.v1.QRDB/Health",
		func(context.Context, any) (any, error) {
			t.Fatal("served without a sealed-window listener")
			return nil, nil
		})
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestSealedWindowAdmission_StreamRefusesOnceUnsealed(t *testing.T) {
	barrier := &toggleSealed{}
	listener := qrdbsdk.NewSealedWindowListenerProbe(barrier, nil)
	err := sealedAdmissionStreamInterceptor(listener)(nil, nil,
		&ggrpc.StreamServerInfo{FullMethod: "/qrdb.v1.QRDB/Health"},
		func(any, ggrpc.ServerStream) error {
			t.Fatal("served a stream against an unsealed barrier")
			return nil
		})
	assert.Equal(t, codes.Unavailable, status.Code(err))
}
