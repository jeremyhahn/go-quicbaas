//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildTenantBarrierService must compose the facade over an embedded
// transport that carries the node's TenantServicer: a tenant list through
// the cached SDK client must reach the real servicer instead of failing
// with the qrdb embedded transport's "TenantServicer must not be nil"
// NilServiceError (the wave-2 tenant-barrier regression).
func TestBuildTenantBarrierService_WiresNodeTenantServicer(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:   buildTestLogger(t),
		qrdbNode: node,
	}

	svc := s.buildTenantBarrierService()
	require.NotNil(t, svc, "facade must be non-nil when the node's tenant service is live")
	require.NotNil(t, s.tenantSDKClient, "tenant SDK client must be cached on first build")
	t.Cleanup(func() { _ = s.tenantSDKClient.Close() })

	// Prove the tenant servicer is actually registered on the embedded
	// transport: ListTenants must dispatch (any outcome except the nil
	// servicer error is acceptable for a fresh node).
	resp, err := s.tenantSDKClient.Tenants().ListTenants(context.Background())
	if err != nil {
		require.NotContains(t, err.Error(), "TenantServicer must not be nil",
			"tenant servicer must be registered on the embedded transport")
	} else {
		require.NotNil(t, resp)
	}
}

// buildTenantBarrierService must cache the SDK client: repeated builds
// (one per transport bring-up) must reuse one in-process client.
func TestBuildTenantBarrierService_CachesClient(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{
		logger:   buildTestLogger(t),
		qrdbNode: node,
	}

	first := s.buildTenantBarrierService()
	require.NotNil(t, first)
	cached := s.tenantSDKClient
	require.NotNil(t, cached)
	t.Cleanup(func() { _ = cached.Close() })

	second := s.buildTenantBarrierService()
	require.NotNil(t, second)
	require.Same(t, cached, s.tenantSDKClient,
		"second build must reuse the cached SDK client")
}

// buildTenantBarrierService must return nil (not panic) when no qrdb node
// is wired — the typical pre-wire / sealed-window state where the
// per-tenant barrier surface is intentionally unreachable.
func TestBuildTenantBarrierService_NilNode(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	require.Nil(t, s.buildTenantBarrierService())
	require.Nil(t, s.tenantSDKClient)
}

// newTenantBarrierClient must reject a nil tenant servicer with the typed
// TenantBarrierWiringError instead of composing a transport that fails at
// call time.
func TestNewTenantBarrierClient_NilTenantServicer(t *testing.T) {
	client, err := newTenantBarrierClient(nil, nil, nil)
	require.Nil(t, client)
	var wireErr *TenantBarrierWiringError
	require.Truef(t, errors.As(err, &wireErr),
		"expected TenantBarrierWiringError, got %T: %v", err, err)
	require.Equal(t, "tenant servicer", wireErr.Stage)
}

// newTenantBarrierClient must surface the qrdb embedded transport's typed
// construction error when the required KV/Admin servicers are absent.
func TestNewTenantBarrierClient_NilKVAdmin(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })
	tenantSvc := node.Services().Tenant
	require.NotNil(t, tenantSvc)

	client, err := newTenantBarrierClient(nil, nil, tenantSvc)
	require.Nil(t, client)
	var wireErr *TenantBarrierWiringError
	require.Truef(t, errors.As(err, &wireErr),
		"expected TenantBarrierWiringError, got %T: %v", err, err)
	require.Equal(t, "embedded transport", wireErr.Stage)
}
