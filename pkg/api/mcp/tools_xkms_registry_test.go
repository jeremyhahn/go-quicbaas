//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/xkmsca"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	"github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// newCARegistry returns a registry wired to its own CA servicer, and the
// certificate result that registry's xkms.ca.getCert call must return.
func newCARegistry(t *testing.T, ctx context.Context, caName string) (*ToolRegistry, interface{}) {
	t.Helper()
	servicer := xkmsca.New(testca.New(t, caName), "example.test")
	registry := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	registry.SetCAService(servicer)
	want, err := servicer.GetCACertificate(ctx, &transport.GetCACertificateRequest{})
	require.NoError(t, err)
	return registry, mustMarshalResult(t, want)
}

// TestExecuteXKMSTool_EachRegistryDispatchesToItsOwnServices verifies that
// an xkms tool call reaches the services of the registry it is made on, not
// those of whichever registry dispatched an xkms tool first.
func TestExecuteXKMSTool_EachRegistryDispatchesToItsOwnServices(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	registryA, wantA := newCARegistry(t, ctx, "mcp-ca-a")
	registryB, wantB := newCARegistry(t, ctx, "mcp-ca-b")

	gotA, err := registryA.executeXKMSTool(ctx, "xkms.ca.getCert", map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, wantA, gotA)

	gotB, err := registryB.executeXKMSTool(ctx, "xkms.ca.getCert", map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, wantB, gotB)
	assert.NotEqual(t, gotA, gotB, "registry B must not answer with registry A's CA")

	// A registry with no CA service wired stays unavailable, even though
	// other registries have already dispatched xkms tools.
	registryC := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	_, err = registryC.executeXKMSTool(ctx, "xkms.ca.getCert", map[string]interface{}{})
	require.Error(t, err)
	var unavailable *api.ServiceUnavailableError
	assert.True(t, errors.As(err, &unavailable), "want service-unavailable, got %v", err)
}

// TestExecuteXKMSTool_ConcurrentRegistriesDispatchToTheirOwnServices verifies
// that registries used concurrently, none of them used before, each build
// their own dispatch table and reach their own services.
func TestExecuteXKMSTool_ConcurrentRegistriesDispatchToTheirOwnServices(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	registryA, wantA := newCARegistry(t, ctx, "mcp-ca-a")
	registryB, wantB := newCARegistry(t, ctx, "mcp-ca-b")

	const perRegistry = 8
	registries := make([]*ToolRegistry, 0, 2*perRegistry)
	wants := make([]interface{}, 0, 2*perRegistry)
	for i := 0; i < perRegistry; i++ {
		registries = append(registries, registryA, registryB)
		wants = append(wants, wantA, wantB)
	}

	results := make([]interface{}, len(registries))
	errs := make([]error, len(registries))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, registry := range registries {
		wg.Add(1)
		go func(i int, registry *ToolRegistry) {
			defer wg.Done()
			<-start
			results[i], errs[i] = registry.executeXKMSTool(ctx, "xkms.ca.getCert", map[string]interface{}{})
		}(i, registry)
	}
	close(start)
	wg.Wait()

	for i := range registries {
		require.NoError(t, errs[i], "goroutine %d", i)
		assert.Equal(t, wants[i], results[i], "goroutine %d got another registry's CA", i)
	}
}
