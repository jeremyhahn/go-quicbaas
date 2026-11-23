//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The module resolvers (ModuleRESTServer, ModuleMCPServer) back the unified
// API server's module surfaces. They build nothing before IAM is attached,
// never cache a failed build, and hand every concurrent first caller the one
// server a single build produced.

// countREST replaces the newREST seam with one that counts its calls and
// fails while failFirst calls remain, then builds the real server.
func countREST(t *testing.T, failFirst int32) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	seamMu.Lock()
	newREST = func(svc service.QuicBaaS, cfg *restapi.Config, log *logger.Logger) (*restapi.Server, error) {
		if calls.Add(1) <= failFirst {
			return nil, errModuleBuildBoom
		}
		return restapi.NewServer(svc, cfg, log)
	}
	seamMu.Unlock()
	return &calls
}

// countMCP replaces the newMCP seam with one that counts its calls.
func countMCP(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	seamMu.Lock()
	newMCP = func(svc service.QuicBaaS, cfg *mcpapi.Config, log *logger.Logger) *mcpapi.Server {
		calls.Add(1)
		return mcpapi.NewServer(svc, cfg, log)
	}
	seamMu.Unlock()
	return &calls
}

var errModuleBuildBoom = errors.New("module build boom")

// moduleResolverServer returns a composed server whose legacy REST listener
// is not running, so the module resolvers build their own servers.
func moduleResolverServer(t *testing.T) *Server {
	t.Helper()
	saveAndRestoreSeams(t)
	srv := createFTServerWithService(t)
	srv.setRESTServer(nil)
	srv.iamReady.Store(false)
	return srv
}

func TestModuleRESTServer_BuildsNothingBeforeIAMIsAttached(t *testing.T) {
	srv := moduleResolverServer(t)
	calls := countREST(t, 0)

	require.Nil(t, srv.ModuleRESTServer(), "no module REST server before IAM is attached")
	require.Zero(t, calls.Load(), "nothing is constructed before IAM is attached")
	require.Nil(t, srv.moduleREST, "nothing is cached before IAM is attached")

	srv.iamReady.Store(true)
	built := srv.ModuleRESTServer()
	require.NotNil(t, built, "the first call after IAM is attached builds the server")
	assert.Equal(t, int32(1), calls.Load())
}

func TestModuleRESTServer_RecoversFromAFailedBuild(t *testing.T) {
	srv := moduleResolverServer(t)
	srv.iamReady.Store(true)
	calls := countREST(t, 1)

	require.Nil(t, srv.ModuleRESTServer(), "a failed build serves no server")
	require.Nil(t, srv.moduleREST, "a failed build is not cached")

	built := srv.ModuleRESTServer()
	require.NotNil(t, built, "the next call builds again and succeeds")
	assert.Same(t, built, srv.ModuleRESTServer(), "a successful build is cached")
	assert.Equal(t, int32(2), calls.Load(), "one failed build, one successful build, then the cache")
}

func TestModuleRESTServer_ConcurrentFirstCallsShareOneServer(t *testing.T) {
	srv := moduleResolverServer(t)
	srv.iamReady.Store(true)
	calls := countREST(t, 0)

	got := resolveConcurrently(16, func() any { return srv.ModuleRESTServer() })
	require.NotNil(t, got[0])
	for _, g := range got {
		assert.Same(t, got[0], g, "every concurrent first caller gets the same server")
	}
	assert.Equal(t, int32(1), calls.Load(), "concurrent first calls construct once")
}

func TestModuleMCPServer_BuildsNothingBeforeIAMIsAttached(t *testing.T) {
	srv := moduleResolverServer(t)
	calls := countMCP(t)

	require.Nil(t, srv.ModuleMCPServer(), "no module MCP server before IAM is attached")
	require.Zero(t, calls.Load(), "nothing is constructed before IAM is attached")
	require.Nil(t, srv.moduleMCP, "nothing is cached before IAM is attached")

	srv.iamReady.Store(true)
	require.NotNil(t, srv.ModuleMCPServer(), "the first call after IAM is attached builds the server")
	assert.Equal(t, int32(1), calls.Load())
}

func TestModuleMCPServer_ConcurrentFirstCallsShareOneServer(t *testing.T) {
	srv := moduleResolverServer(t)
	srv.iamReady.Store(true)
	calls := countMCP(t)

	got := resolveConcurrently(16, func() any { return srv.ModuleMCPServer() })
	require.NotNil(t, got[0])
	for _, g := range got {
		assert.Same(t, got[0], g, "every concurrent first caller gets the same server")
	}
	assert.Equal(t, int32(1), calls.Load(), "concurrent first calls construct once")
}

// resolveConcurrently runs resolve from n goroutines released together and
// returns what each got.
func resolveConcurrently(n int, resolve func() any) []any {
	got := make([]any, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i] = resolve()
		}(i)
	}
	close(start)
	wg.Wait()
	return got
}
