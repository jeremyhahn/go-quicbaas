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
	stderrors "errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func requireBindingForbidden(t *testing.T, err error, msg ...any) {
	t.Helper()
	require.Error(t, err, msg...)
	var qe *errors.QBError
	require.True(t, stderrors.As(err, &qe), "want a QBError, got %v", err)
	require.Equal(t, errors.ErrForbidden, qe.Code, "%v", err)
}

// TestSecretsService_ServiceBindingGuardsTheServicesCredentials pins the
// secrets grant's binding: a service's services/<name>/ secrets are served
// to the workload its binding names and to in-process system calls, never to
// another principal of the tenant, and the plane's own keys are never
// writable by the workload.
func TestSecretsService_ServiceBindingGuardsTheServicesCredentials(t *testing.T) {
	t.Parallel()

	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	require.NoError(t, err)
	svc := newSecretsService(mgr, newMockLogger())

	base := tenantctx.WithSystem(context.Background())
	tenantID, err := secretsScopedTenant(base, "", "test")
	require.NoError(t, err)
	const web = "spiffe://example.org/tenant/acme/svc/web"
	seed := func(id string, meta map[string]string) {
		require.NoError(t, mgr.Store(base, secretsTenantScopedID(tenantID, id), map[string][]byte{"data": []byte("k-" + id)}, meta))
	}
	seed("services/web/binding", map[string]string{serviceBindingMeta: web})
	seed("services/web/svid", nil)
	seed("services/db/svid", nil) // no binding: the plane's alone
	seed("app/config", nil)

	as := func(id string) context.Context {
		return auth.SubjectIntoContext(base, interfaces.Subject{ID: id, Authenticated: true})
	}

	t.Run("the bound workload reads its own credentials", func(t *testing.T) {
		got, err := svc.GetSecret(as(web), &GetSecretRequest{SecretID: "services/web/svid"})
		require.NoError(t, err)
		require.NotNil(t, got)
	})

	t.Run("another principal of the tenant is refused them", func(t *testing.T) {
		_, err := svc.GetSecret(as("mallory"), &GetSecretRequest{SecretID: "services/web/svid"})
		requireBindingForbidden(t, err)
		_, err = svc.GetSecret(as("spiffe://example.org/tenant/acme/svc/db"), &GetSecretRequest{SecretID: "services/web/svid"})
		requireBindingForbidden(t, err)
		_, err = svc.GetSecret(as(web), &GetSecretRequest{SecretID: "services/db/svid"})
		requireBindingForbidden(t, err, "a prefix with no binding is the plane's alone")
	})

	t.Run("a prefix listing hides another service's credentials", func(t *testing.T) {
		for _, prefix := range []string{"", "services/", "services/web/"} {
			got, err := svc.GetSecretsByPrefix(as("mallory"), &GetSecretsByPrefixRequest{Prefix: prefix, IncludeData: true})
			if err != nil {
				continue
			}
			for path := range got.Secrets {
				require.False(t, strings.Contains(path, "services/"), "prefix %q listed %s", prefix, path)
			}
		}
		got, err := svc.GetSecretsByPrefix(as(web), &GetSecretsByPrefixRequest{Prefix: "services/", IncludeData: true})
		require.NoError(t, err)
		for path := range got.Secrets {
			require.True(t, strings.Contains(path, "services/web/"), "the workload listed %s", path)
		}
		require.NotEmpty(t, got.Secrets)
	})

	t.Run("the workload never writes the plane's keys", func(t *testing.T) {
		_, err := svc.StoreSecret(as(web), &StoreSecretRequest{SecretID: "services/web/svid", Name: "svid", Data: []byte("x")})
		requireBindingForbidden(t, err)
		_, err = svc.StoreSecret(as(web), &StoreSecretRequest{SecretID: "services/web/binding", Name: "binding", Data: []byte("x")})
		requireBindingForbidden(t, err)
		_, err = svc.DeleteSecret(as("mallory"), &DeleteSecretRequest{SecretID: "services/web/svid"})
		requireBindingForbidden(t, err)
		_, err = svc.StoreSecret(as(web), &StoreSecretRequest{SecretID: "services/web/app-token", Name: "app-token", Data: []byte("x")})
		require.NoError(t, err, "the workload keeps its own secrets under its prefix")
	})

	t.Run("in-process system calls pass", func(t *testing.T) {
		_, err := svc.GetSecret(auth.NewSystemContext(base), &GetSecretRequest{SecretID: "services/db/svid"})
		require.NoError(t, err)
	})

	t.Run("other secrets are untouched", func(t *testing.T) {
		_, err := svc.GetSecret(as("mallory"), &GetSecretRequest{SecretID: "app/config"})
		require.NoError(t, err)
	})
}
