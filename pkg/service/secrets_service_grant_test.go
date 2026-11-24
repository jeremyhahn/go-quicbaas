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

package service

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/grants"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// The SVID the identity grant keeps is served by the secrets API to the
// workload the binding names, and to no other principal of the tenant.
func TestSecretsService_TheBoundWorkloadReadsItsGrantedSVID(t *testing.T) {
	t.Parallel()

	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	require.NoError(t, err)
	ca := xkmsca.New(t, "secrets service grant "+t.Name())
	m := serviceplanetest.Manifest("acme", "web")
	ctx := context.Background()
	_, err = (&grants.Identity{CA: ca, Secrets: mgr, TrustDomain: "quicbaas.test"}).Grant(ctx, m, "")
	require.NoError(t, err)
	_, err = (&grants.Secrets{Secrets: mgr, TrustDomain: "quicbaas.test"}).Grant(ctx, m, "")
	require.NoError(t, err)

	svc := newSecretsService(mgr, newMockLogger())
	as := func(id string) context.Context {
		base := tenantctx.WithTenant(context.Background(), "acme")
		return auth.SubjectIntoContext(base, interfaces.Subject{ID: id, Authenticated: true})
	}

	got, err := svc.GetSecret(as(grants.SPIFFEID("quicbaas.test", m)), &GetSecretRequest{SecretID: "services/web/svid"})
	require.NoError(t, err)
	require.NotNil(t, got)
	_, err = tls.X509KeyPair(got.Data, got.Data)
	require.NoError(t, err, "the served SVID loads as a key pair")

	_, err = svc.GetSecret(as("mallory"), &GetSecretRequest{SecretID: "services/web/svid"})
	requireBindingForbidden(t, err)
}
