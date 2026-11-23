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
)

func TestDynamicSecretCredentials_SealEnabledNodeRegistersAndResolves(t *testing.T) {
	ctx := context.Background()
	srv := buildSealEnabledNodeServer(t)
	gens := credentials.NewGeneratorRegistry()
	require.NoError(t, srv.registerDynamicSecretGenerators(gens))
	require.Equal(t, []string{credstore.GeneratorType}, gens.List())

	gen, err := gens.Get(credstore.GeneratorType)
	require.NoError(t, err)
	cred, err := gen.Generate(ctx, dscRole())
	require.NoError(t, err)
	ref := cred.Data["reference"]

	registry := srv.buildSecretRegistry()
	require.True(t, dscResolvesTo(t, func() (*qrdbsecret.SecretValue, error) { return registry.Resolve(ctx, ref) }, cred.Data["value"]),
		"credential %s does not resolve through the server's secret registry", cred.CredentialID)

	require.NoError(t, gen.Revoke(ctx, cred))
	_, err = registry.Resolve(ctx, ref)
	require.Error(t, err, "credential %s still resolves after revoke", cred.CredentialID)
	require.NoError(t, gen.Revoke(ctx, cred), "a second revoke of credential %s must succeed", cred.CredentialID)
}

func TestDynamicSecretCredentials_SealDisabledRegistersNothing(t *testing.T) {
	srv := buildSealDisabledNodeServer(t)
	gens := credentials.NewGeneratorRegistry()
	require.NoError(t, srv.registerDynamicSecretGenerators(gens))
	require.Empty(t, gens.List())
}
