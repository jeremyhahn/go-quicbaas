//go:build !integration

package server_test

import (
	"context"
	"crypto/x509"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaassdk "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// delegationSecrets is the secrets backend under the kms:// resolver: it
// answers the delegating tenant's one secret, generates any secret an ensure
// names, and counts both.
type delegationSecrets struct {
	service.SecretsService
	calls       int
	ensureCalls int
}

func (d *delegationSecrets) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	d.calls++
	tenantID, _ := qrdbsdk.TenantFromContext(ctx)
	if tenantID != server.DelegatingTenantID || req.SecretID != "app/key" {
		return nil, &delegationSecretNotFoundError{tenantID: tenantID, secretID: req.SecretID}
	}
	return &service.GetSecretResponse{SecretID: req.SecretID, Data: []byte("tenant-value")}, nil
}

func (d *delegationSecrets) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	d.ensureCalls++
	tenantID, _ := qrdbsdk.TenantFromContext(ctx)
	if tenantID != server.DelegatingTenantID {
		return nil, &delegationSecretNotFoundError{tenantID: tenantID, secretID: req.SecretID}
	}
	return &service.EnsureSecretResponse{SecretID: req.SecretID, Data: []byte("generated-" + req.SecretID), Generated: true}, nil
}

func (d *delegationSecrets) ListSecrets(ctx context.Context, _ *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	d.calls++
	tenantID, _ := qrdbsdk.TenantFromContext(ctx)
	if tenantID != server.DelegatingTenantID {
		return nil, &delegationSecretNotFoundError{tenantID: tenantID}
	}
	return &service.ListSecretsResponse{Secrets: []*service.Secret{{SecretID: "app/key"}}, TotalCount: 1}, nil
}

// delegationSecretNotFoundError is the backend's answer for any secret other
// than the delegating tenant's one.
type delegationSecretNotFoundError struct{ tenantID, secretID string }

func (e *delegationSecretNotFoundError) Error() string {
	return "no secret " + e.secretID + " in tenant " + e.tenantID
}

// delegationFacade carries the secrets backend into the embedded SDK client.
type delegationFacade struct {
	service.QuicBaaS
	secrets *delegationSecrets
}

func (f *delegationFacade) Secrets() service.SecretsService { return f.secrets }

// newDelegatedSDKClient is the embedded SDK client a consumer builds for a
// delegation: the server is its binder, so every call runs under the node's
// real DelegatedContext and AuthMiddleware.
func newDelegatedSDKClient(t *testing.T, s *server.Server, leaf *x509.Certificate, secrets *delegationSecrets) quicbaassdk.Client {
	t.Helper()
	node := s.QRDBNodeForTest()
	c, err := quicbaassdk.NewClient("embedded://local",
		quicbaassdk.WithEmbedded(&delegationFacade{secrets: secrets}),
		quicbaassdk.WithEmbeddedQRDB(quicbaassdk.QRDBEmbeddedServicers{KV: node.KVService(), Admin: node.AdminService()}),
		quicbaassdk.WithEmbeddedIdentity(quicbaassdk.DelegatedIdentity(s, []*x509.Certificate{leaf})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func resolveDelegatedKMS(t *testing.T, c quicbaassdk.Client, uri string) (string, error) {
	t.Helper()
	resolver := c.Security().SecretResolver()
	require.NotNil(t, resolver)
	value, err := resolver.Resolve(context.Background(), uri)
	if err != nil {
		return "", err
	}
	defer value.Clear()
	var got string
	err = value.WithBytes(context.Background(), func(b []byte) error {
		got = string(b)
		return nil
	})
	require.NoError(t, err)
	return got, nil
}

func TestDelegatedKMS_SecretsReadDelegationResolves(t *testing.T) {
	s := server.NewDelegatingServer(t)
	leaf := server.IssueTestDelegation(t, s, "secrets:read")
	secrets := &delegationSecrets{}
	c := newDelegatedSDKClient(t, s, leaf, secrets)

	got, err := resolveDelegatedKMS(t, c, "kms://app/key")
	require.NoError(t, err)
	require.Equal(t, "tenant-value", got)
}

func TestDelegatedKMS_DelegationWithoutSecretsReadIsRefused(t *testing.T) {
	s := server.NewDelegatingServer(t)
	leaf := server.IssueTestDelegation(t, s, "kv:get")
	secrets := &delegationSecrets{}
	c := newDelegatedSDKClient(t, s, leaf, secrets)

	_, err := resolveDelegatedKMS(t, c, "kms://app/key")
	require.Error(t, err, "a delegation without secrets:read resolved kms://")
	require.Zero(t, secrets.calls, "the secrets backend was reached without secrets:read")
}

func TestDelegatedKMS_SecretsWriteDelegationEnsures(t *testing.T) {
	s := server.NewDelegatingServer(t)
	leaf := server.IssueTestDelegation(t, s, "secrets:read", "secrets:write")
	secrets := &delegationSecrets{}
	c := newDelegatedSDKClient(t, s, leaf, secrets)

	got, err := resolveDelegatedKMS(t, c, "kms://app/new?ensure=true")
	require.NoError(t, err)
	require.Equal(t, "generated-app/new", got)
}

func TestDelegatedKMS_EnsureWithoutSecretsWriteIsRefused(t *testing.T) {
	s := server.NewDelegatingServer(t)
	leaf := server.IssueTestDelegation(t, s, "secrets:read")
	secrets := &delegationSecrets{}
	c := newDelegatedSDKClient(t, s, leaf, secrets)

	_, err := resolveDelegatedKMS(t, c, "kms://app/new?ensure=true")
	require.Error(t, err, "a delegation without secrets:write ensured kms://")
	require.Zero(t, secrets.ensureCalls, "the secrets backend was reached without secrets:write")
}

// A delegated Security call reaches its backend under the node's real
// DelegatedContext: the principal's grant ceiling covers every permission the
// embedded Security operation map declares, so a delegation granting
// secrets:list holds it after go-qrdb intersects scope with the grants.
func TestDelegatedSecurity_SecretsListDelegationIsServed(t *testing.T) {
	s := server.NewDelegatingServer(t)
	leaf := server.IssueTestDelegation(t, s, "secrets:list")
	secrets := &delegationSecrets{}
	c := newDelegatedSDKClient(t, s, leaf, secrets)

	got, err := c.Security().Secrets().List(context.Background(), "app/")
	require.NoError(t, err)
	require.Equal(t, 1, secrets.calls)
	require.Len(t, got.Paths, 1)
}

// The intersection stays: a delegation that does not grant secrets:list is
// refused before the backend, whatever the principal's ceiling holds.
func TestDelegatedSecurity_SecretsListWithoutScopeIsRefused(t *testing.T) {
	s := server.NewDelegatingServer(t)
	leaf := server.IssueTestDelegation(t, s, "secrets:read")
	secrets := &delegationSecrets{}
	c := newDelegatedSDKClient(t, s, leaf, secrets)

	_, err := c.Security().Secrets().List(context.Background(), "app/")
	require.Error(t, err, "a delegation without secrets:list listed secrets")
	require.Zero(t, secrets.calls, "the secrets backend was reached without secrets:list")
}
