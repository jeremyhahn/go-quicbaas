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

//go:build !integration && raft

package server

import (
	"context"
	"testing"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials/credstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// A caller bound to the system tenant (every transport binds the caller's
// tenant, and the integration system admin is tenant "system") mints a
// dynamic credential and resolves its credential:// reference on a
// seal-enabled node.
func TestDynamicSecretCredentials_SystemTenantMintsAndResolves(t *testing.T) {
	srv := buildSealEnabledNodeServer(t)
	gens := credentials.NewGeneratorRegistry()
	require.NoError(t, srv.registerDynamicSecretGenerators(gens))

	gen, err := gens.Get(credstore.GeneratorType)
	require.NoError(t, err)

	ctx := tenantctx.WithSystem(context.Background())
	cred, err := gen.Generate(ctx, dscRole())
	require.NoError(t, err, "the system tenant cannot mint a dynamic credential")
	ref := cred.Data["reference"]

	registry := srv.buildSecretRegistry()
	require.True(t, dscResolvesTo(t, func() (*qrdbsecret.SecretValue, error) { return registry.Resolve(ctx, ref) }, cred.Data["value"]),
		"credential %s does not resolve for the system tenant", cred.CredentialID)

	require.NoError(t, gen.Revoke(ctx, cred))
	_, err = registry.Resolve(ctx, ref)
	require.Error(t, err, "credential %s still resolves for the system tenant after revoke", cred.CredentialID)
}

// The system tenant goes to the system credential store; a non-system tenant
// without a barrier still fails closed and never falls back to it.
func TestDynamicSecretCredentials_SystemTenantUsesSystemStore(t *testing.T) {
	reg := buildTestBarrierRegistry(t)
	system := &dscTrackingSystemStore{}
	gen, err := credstore.NewGenerator(dynamicCredentialStores(reg, system, credentialTenant))
	require.NoError(t, err)

	_, err = gen.Generate(tenantctx.WithSystem(context.Background()), dscRole())
	require.NoError(t, err, "the system tenant did not reach the system store")
	system.mu.Lock()
	require.Equal(t, 1, system.calls, "the system tenant did not write to the system store")
	system.mu.Unlock()

	_, err = gen.Generate(tenantctx.WithTenant(context.Background(), "tenant-unregistered"), dscRole())
	require.Error(t, err)
	system.mu.Lock()
	defer system.mu.Unlock()
	require.Equal(t, 1, system.calls, "a tenant call fell back to the system store")
}
