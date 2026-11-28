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

//go:build !integration

package server

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials/credstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// dscTrackingSystemStore records whether the system credential store was
// reached.
type dscTrackingSystemStore struct {
	mu    sync.Mutex
	calls int
}

func (s *dscTrackingSystemStore) CredentialStore(context.Context, string, []byte) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return nil
}

func (s *dscTrackingSystemStore) CredentialDelete(context.Context, string) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return nil
}

func dscRole() *credentials.Role {
	return &credentials.Role{Name: "app-token", GeneratorType: credstore.GeneratorType, DefaultTTL: time.Hour, MaxTTL: time.Hour}
}

// dscResolvesTo reports whether ref resolves to want through resolve.
func dscResolvesTo(t *testing.T, resolve func() (*qrdbsecret.SecretValue, error), want string) bool {
	t.Helper()
	v, err := resolve()
	if err != nil {
		return false
	}
	defer v.Clear()
	return bytes.Equal(v.Bytes(), []byte(want))
}

func TestDynamicSecretCredentials_TenantRoundTrip(t *testing.T) {
	ctx := context.Background()
	reg := buildTestBarrierRegistry(t)

	tenantAStrategy, err := qrdbsdk.NewSoftwareStrategy([]byte("tenant-a-pass"))
	require.NoError(t, err)
	tb, err := reg.RegisterTenant(ctx, "tenant-a",
		qrdbsdk.NewMemoryBackend(),
		qrdbsdk.BarrierConfig{},
		tenantAStrategy,
	)
	require.NoError(t, err)
	tenantCreds, err := qrdbsdk.CredentialsFor([]byte("tenant-a-pass"))
	require.NoError(t, err)
	defer tenantCreds.Secret.Destroy()
	require.NoError(t, tb.Barrier().Initialize(ctx, tenantCreds))

	gen, err := credstore.NewGenerator(dynamicCredentialStores(reg, nil, tenantIDAdapter))
	require.NoError(t, err)
	resolver := qrdbsecret.NewCredentialResolver(
		qrdbsecret.NewTenantScopedCredentialRetrieve(reg, nil, tenantIDAdapter, slog.Default()))

	ctxA := tenantctx.WithTenant(ctx, "tenant-a")
	ctxB := tenantctx.WithTenant(ctx, "tenant-b")
	cred, err := gen.Generate(ctxA, dscRole())
	require.NoError(t, err)
	ref := cred.Data["reference"]
	value := cred.Data["value"]
	require.Equal(t, "credential://dynamic-"+cred.CredentialID, ref)

	require.True(t, dscResolvesTo(t, func() (*qrdbsecret.SecretValue, error) { return resolver.Resolve(ctxA, ref) }, value),
		"credential %s does not resolve under tenant-a", cred.CredentialID)
	_, err = resolver.Resolve(ctxB, ref)
	require.Error(t, err, "credential %s resolved under tenant-b", cred.CredentialID)

	require.NoError(t, gen.Revoke(ctxA, cred))
	_, err = resolver.Resolve(ctxA, ref)
	require.Error(t, err, "credential %s still resolves after revoke", cred.CredentialID)
	require.NoError(t, gen.Revoke(ctxA, cred), "a second revoke of credential %s must succeed", cred.CredentialID)
}

func TestDynamicSecretCredentials_TenantWithoutBarrierFailsClosed(t *testing.T) {
	reg := buildTestBarrierRegistry(t)
	system := &dscTrackingSystemStore{}
	gen, err := credstore.NewGenerator(dynamicCredentialStores(reg, system, tenantIDAdapter))
	require.NoError(t, err)

	_, err = gen.Generate(tenantctx.WithTenant(context.Background(), "tenant-unregistered"), dscRole())
	require.Error(t, err)
	system.mu.Lock()
	defer system.mu.Unlock()
	require.Zero(t, system.calls, "a tenant call fell back to the system store")
}

func TestDynamicSecretCredentials_NoSystemStoreIsAnError(t *testing.T) {
	storeFor := dynamicCredentialStores(nil, nil, tenantIDAdapter)
	_, err := storeFor(context.Background())
	require.ErrorIs(t, err, errNoSystemCredentialStore)
}
