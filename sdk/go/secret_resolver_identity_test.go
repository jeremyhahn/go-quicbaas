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

package quicbaas

import (
	"context"
	"crypto/x509"
	"testing"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	qrdbsecure "github.com/jeremyhahn/go-qrdb/sdk/go/secure"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// credentialNotVisibleError is what tenantCredentialService returns for a
// name the bound tenant does not own.
type credentialNotVisibleError struct{ name string }

func (e *credentialNotVisibleError) Error() string {
	return "credential " + e.name + " is not visible to the caller"
}

// tenantCredentialService stands in for qrdb's credential servicer: it
// serves a credential only from the namespace of the tenant bound on the
// call's context. An unbound context reads the system namespace, which is
// what qrdb's resolver falls back to when no tenant is bound.
type tenantCredentialService struct {
	stubCredentialService
	byTenant map[string]map[string]string
	calls    int
}

func (s *tenantCredentialService) CredentialRetrieveBuffer(ctx context.Context, name string) (*qrdbsecure.Buffer, error) {
	s.calls++
	value, ok := s.byTenant[boundTenant(ctx)][name]
	if !ok {
		return nil, &credentialNotVisibleError{name: name}
	}
	return qrdbsecure.NewBufferFromBytes([]byte(value))
}

// registryService is an embedded QuicBaaS service that exposes the server's
// raw secret registry, as the real server does.
type registryService struct {
	service.QuicBaaS
	registry *qrdbsecret.ResolverRegistry
	secrets  *tenantSecretsService
}

func (s *registryService) SecretRegistry() *qrdbsecret.ResolverRegistry { return s.registry }

func (s *registryService) Secrets() service.SecretsService { return s.secrets }

// tenantSecretsService stands in for the server's secrets service: it
// serves a secret only from the namespace of the tenant bound on the call's
// context, and counts the calls that reach it.
type tenantSecretsService struct {
	service.SecretsService
	byTenant map[string]map[string]string
	calls    int
}

func (s *tenantSecretsService) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	s.calls++
	value, ok := s.byTenant[boundTenant(ctx)][req.SecretID]
	if !ok {
		return nil, &credentialNotVisibleError{name: req.SecretID}
	}
	return &service.GetSecretResponse{SecretID: req.SecretID, Data: []byte(value)}, nil
}

func newTenantSecretsService() *tenantSecretsService {
	return &tenantSecretsService{byTenant: map[string]map[string]string{
		"":         {"system/key": "system-secret"},
		"tenant-a": {"a/key": "a-secret"},
	}}
}

func newTenantCredentialService() *tenantCredentialService {
	return &tenantCredentialService{byTenant: map[string]map[string]string{
		"":         {"system-key": "system-value"},
		"tenant-a": {"a-key": "a-value"},
		"tenant-b": {"b-key": "b-value"},
	}}
}

// newDelegatedSecretClient builds an embedded client through NewClient, with
// the server's raw registry on offer, acting under a delegation bound by
// binder.
func newDelegatedSecretClient(t *testing.T, creds *tenantCredentialService, binder EmbeddedIdentityBinder) Client {
	t.Helper()
	return newDelegatedSecretClientWithSecrets(t, creds, newTenantSecretsService(), binder)
}

func newDelegatedSecretClientWithSecrets(t *testing.T, creds *tenantCredentialService, kms *tenantSecretsService, binder EmbeddedIdentityBinder) Client {
	t.Helper()
	raw := qrdbsecret.NewResolverRegistry()
	raw.Register(qrdbsecret.CredentialResolverFromServicer(creds))
	c, err := NewClient("embedded://local",
		WithEmbedded(&registryService{registry: raw, secrets: kms}),
		WithEmbeddedQRDB(QRDBEmbeddedServicers{
			KV:         &tenantRecordingKV{memKV: newMemKV()},
			Admin:      stubAdminService{},
			Credential: creds,
		}),
		WithEmbeddedIdentity(DelegatedIdentity(binder, []*x509.Certificate{{}})))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func resolveSecret(t *testing.T, c Client, uri string) (string, error) {
	t.Helper()
	resolver := c.Security().SecretResolver()
	if resolver == nil {
		t.Fatal("Security().SecretResolver() is nil")
	}
	value, err := resolver.Resolve(context.Background(), uri)
	if err != nil {
		return "", err
	}
	defer value.Clear()
	var got string
	if err := value.WithBytes(context.Background(), func(b []byte) error {
		got = string(b)
		return nil
	}); err != nil {
		t.Fatalf("WithBytes: %v", err)
	}
	return got, nil
}

func TestSecretResolver_RevokedDelegationCannotResolve(t *testing.T) {
	creds := newTenantCredentialService()
	c := newDelegatedSecretClient(t, creds, &tenantBinder{refuse: &bindRefusedError{}})

	for _, uri := range []string{"credential://system-key", "credential://a-key"} {
		if got, err := resolveSecret(t, c, uri); err == nil {
			t.Fatalf("Resolve(%s) under a revoked delegation = %q, want an error", uri, got)
		}
	}
	if creds.calls != 0 {
		t.Fatalf("credential servicer reached %d times under a revoked delegation, want 0", creds.calls)
	}
}

func TestSecretResolver_DelegationReadsOnlyItsOwnTenant(t *testing.T) {
	creds := newTenantCredentialService()
	c := newDelegatedSecretClient(t, creds, &tenantBinder{tenant: "tenant-a"})

	for _, uri := range []string{"credential://b-key", "credential://system-key"} {
		if got, err := resolveSecret(t, c, uri); err == nil {
			t.Fatalf("Resolve(%s) for tenant-a = %q, want an error", uri, got)
		}
	}
}

func TestSecretResolver_DelegationResolvesItsOwnCredential(t *testing.T) {
	creds := newTenantCredentialService()
	c := newDelegatedSecretClient(t, creds, &tenantBinder{tenant: "tenant-a"})

	got, err := resolveSecret(t, c, "credential://a-key")
	if err != nil {
		t.Fatalf("Resolve(credential://a-key): %v", err)
	}
	if got != "a-value" {
		t.Fatalf("Resolve(credential://a-key) = %q, want a-value", got)
	}
}

func TestSecretResolver_RevokedDelegationCannotResolveKMS(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms, &tenantBinder{refuse: &bindRefusedError{}})

	if got, err := resolveSecret(t, c, "kms://system/key"); err == nil {
		t.Fatalf("Resolve(kms://system/key) under a revoked delegation = %q, want an error", got)
	}
	if kms.calls != 0 {
		t.Fatalf("secrets service reached %d times under a revoked delegation, want 0", kms.calls)
	}
}

func TestSecretResolver_DelegationResolvesOnlyItsOwnKMSSecret(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms, &tenantBinder{tenant: "tenant-a"})

	if got, err := resolveSecret(t, c, "kms://system/key"); err == nil {
		t.Fatalf("Resolve(kms://system/key) for tenant-a = %q, want an error", got)
	}
	got, err := resolveSecret(t, c, "kms://a/key")
	if err != nil {
		t.Fatalf("Resolve(kms://a/key): %v", err)
	}
	if got != "a-secret" {
		t.Fatalf("Resolve(kms://a/key) = %q, want a-secret", got)
	}
}
