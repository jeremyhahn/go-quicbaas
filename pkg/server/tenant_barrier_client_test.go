//go:build !integration

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
	"errors"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// hasErrorCode reports whether err is a QBError with code.
func hasErrorCode(err error, code pkgerrors.ErrorCode) bool {
	var qe *pkgerrors.QBError
	return errors.As(err, &qe) && qe.Code == code
}

// The tenant SDK client belongs to one node service graph: when the graph
// behind the server changes the client is built again over the new one, and
// the replaced client stays open until shutdown closes it.
func TestQRDBTenantClient_IsRebuiltWhenTheServiceGraphChanges(t *testing.T) {
	first := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = first.Close() })
	second := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = second.Close() })

	s := &Server{logger: buildTestLogger(t), qrdbNode: first}
	t.Cleanup(func() { _ = s.closeTenantBarrierClients() })

	before := s.qrdbTenantClient()
	require.NotNil(t, before)
	require.Same(t, before, s.qrdbTenantClient(), "one graph, one client")

	s.setQRDBNode(second)
	after := s.qrdbTenantClient()
	require.NotNil(t, after)
	require.NotSame(t, before, after, "a new service graph must get a new client")
	require.Same(t, after, s.qrdbTenantClient())
	require.Same(t, after, s.tenantSDKClient)
	require.Len(t, s.tenantSDKRetired, 1)
	require.Same(t, before, s.tenantSDKRetired[0])

	require.NoError(t, s.closeTenantBarrierClients())
	require.Nil(t, s.tenantSDKClient)
	require.Empty(t, s.tenantSDKRetired)
}

// Tenant unseals arrive on any goroutine: concurrent callers share one
// client, with no data race on the cache.
func TestQRDBTenantClient_ConcurrentCallersShareOneClient(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })
	s := &Server{logger: buildTestLogger(t), qrdbNode: node}
	t.Cleanup(func() { _ = s.closeTenantBarrierClients() })

	const callers = 16
	clients := make([]*qrdbsdk.Client, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			clients[i] = s.qrdbTenantClient()
		}(i)
	}
	wg.Wait()

	require.NotNil(t, clients[0])
	for i := 1; i < callers; i++ {
		require.Same(t, clients[0], clients[i])
	}
	require.Empty(t, s.tenantSDKRetired)
}

// Once shutdown has begun no client is built or returned: the factory the
// node's transports call has no facade, and a facade a transport took at
// boot answers unavailable.
func TestQRDBTenantClient_NoneOnceShutdownHasBegun(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })
	s := &Server{logger: buildTestLogger(t), qrdbNode: node}
	t.Cleanup(func() { _ = s.closeTenantBarrierClients() })

	facade := s.buildTenantBarrierService()
	require.NotNil(t, facade)
	require.NotNil(t, s.currentTenantClient())

	s.stopTenantBarrierClient()
	require.Nil(t, s.qrdbTenantClient())
	require.Nil(t, s.currentTenantClient())
	require.Nil(t, s.buildTenantBarrierService())
	require.NotNil(t, s.tenantSDKClient, "the built client stays open until the node has stopped")

	_, err := facade.TenantBarrierStatus(tenantAdminCtx("acme"), "acme")
	require.True(t, hasErrorCode(err, pkgerrors.ErrUnavailable), "expected ErrUnavailable, got %v", err)
	err = facade.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key"))
	require.True(t, hasErrorCode(err, pkgerrors.ErrUnavailable), "expected ErrUnavailable, got %v", err)

	require.NoError(t, s.closeTenantBarrierClients())
	require.Nil(t, s.tenantSDKClient)
	require.Nil(t, s.qrdbTenantClient())
}
