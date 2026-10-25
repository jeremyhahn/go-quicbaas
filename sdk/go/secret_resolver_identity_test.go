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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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
	byTenant    map[string]map[string]string
	calls       int
	ensureCalls int
}

func (s *tenantSecretsService) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	s.calls++
	value, ok := s.byTenant[boundTenant(ctx)][req.SecretID]
	if !ok {
		return nil, &credentialNotVisibleError{name: req.SecretID}
	}
	return &service.GetSecretResponse{SecretID: req.SecretID, Data: []byte(value)}, nil
}

// EnsureSecret returns the tenant's secret at req.SecretID, creating it with
// a fixed value on first use.
func (s *tenantSecretsService) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	s.ensureCalls++
	tenant := boundTenant(ctx)
	if s.byTenant[tenant] == nil {
		s.byTenant[tenant] = map[string]string{}
	}
	value, ok := s.byTenant[tenant][req.SecretID]
	if !ok {
		value = "generated-" + req.SecretID
		s.byTenant[tenant][req.SecretID] = value
	}
	return &service.EnsureSecretResponse{SecretID: req.SecretID, Data: []byte(value), Generated: !ok}, nil
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

// permCredentialRetrieve is the grant qrdb declares on CredentialRetrieve.
const permCredentialRetrieve = "credentials:retrieve"

// permSecretsRead and permSecretsWrite are the grants go-qrdb declares for
// tenant secret material (pkg/permission/broker_scope.go).
const (
	permSecretsRead  = "secrets:read"
	permSecretsWrite = "secrets:write"
)

// tenantGrant binds a delegation to tenant holding exactly grants, enforced
// by the tenant-hosting node's own AuthMiddleware.
func tenantGrant(t *testing.T, tenant string, grants ...string) *grantBinder {
	t.Helper()
	return &grantBinder{tenant: tenant, grants: grants, mw: nodeAuthMiddleware(t)}
}

// refusedGrant is a revoked delegation: the binder refuses every call.
func refusedGrant(t *testing.T) *grantBinder {
	t.Helper()
	return &grantBinder{refuse: &bindRefusedError{}, mw: nodeAuthMiddleware(t)}
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
	c := newDelegatedSecretClient(t, creds, refusedGrant(t))

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
	c := newDelegatedSecretClient(t, creds, tenantGrant(t, "tenant-a", permCredentialRetrieve))

	for _, uri := range []string{"credential://b-key", "credential://system-key"} {
		if got, err := resolveSecret(t, c, uri); err == nil {
			t.Fatalf("Resolve(%s) for tenant-a = %q, want an error", uri, got)
		}
	}
}

// A delegation holding credentials:retrieve resolves a credential through
// the node's AuthMiddleware: the resolver reads through the authorized qrdb
// client. qrdb's named-credential store is system-scoped (credentials:* are
// ScopeCross), so only a system-tenant delegation can hold the grant.
func TestSecretResolver_DelegationWithRetrieveGrantResolves(t *testing.T) {
	creds := newTenantCredentialService()
	c := newDelegatedSecretClient(t, creds, tenantGrant(t, "", permCredentialRetrieve))

	got, err := resolveSecret(t, c, "credential://system-key")
	if err != nil {
		t.Fatalf("Resolve(credential://system-key): %v", err)
	}
	if got != "system-value" {
		t.Fatalf("Resolve(credential://system-key) = %q, want system-value", got)
	}
}

// A delegation without the credentials:retrieve grant is refused by the
// node's middleware before the credential servicer is reached.
func TestSecretResolver_DelegationWithoutRetrieveGrantCannotResolve(t *testing.T) {
	creds := newTenantCredentialService()
	c := newDelegatedSecretClient(t, creds, tenantGrant(t, "", "credentials:list"))

	if got, err := resolveSecret(t, c, "credential://system-key"); err == nil {
		t.Fatalf("Resolve(credential://system-key) without credentials:retrieve = %q, want an error", got)
	}
	if creds.calls != 0 {
		t.Fatalf("credential servicer reached %d times without the grant, want 0", creds.calls)
	}
}

// A tenant delegation cannot read credentials even with the grant: qrdb
// confines credentials:* to the system tenant, and the middleware the
// delegation runs under enforces that before the servicer is reached.
func TestSecretResolver_TenantDelegationCannotReadCredentials(t *testing.T) {
	creds := newTenantCredentialService()
	c := newDelegatedSecretClient(t, creds, tenantGrant(t, "tenant-a", permCredentialRetrieve))

	if got, err := resolveSecret(t, c, "credential://a-key"); err == nil {
		t.Fatalf("Resolve(credential://a-key) for tenant-a = %q, want an error", got)
	}
	if creds.calls != 0 {
		t.Fatalf("credential servicer reached %d times for a tenant delegation, want 0", creds.calls)
	}
}

func TestSecretResolver_RevokedDelegationCannotResolveKMS(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms, refusedGrant(t))

	if got, err := resolveSecret(t, c, "kms://system/key"); err == nil {
		t.Fatalf("Resolve(kms://system/key) under a revoked delegation = %q, want an error", got)
	}
	if kms.calls != 0 {
		t.Fatalf("secrets service reached %d times under a revoked delegation, want 0", kms.calls)
	}
}

func TestSecretResolver_DelegationResolvesOnlyItsOwnKMSSecret(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms, tenantGrant(t, "tenant-a", permSecretsRead))

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

// A delegation without secrets:read must not read a kms:// secret: the
// secrets backend runs as the service principal, so the delegation's grants
// are enforced before the call is dispatched.
func TestSecretResolver_DelegationWithoutSecretsReadCannotResolveKMS(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms, tenantGrant(t, "tenant-a", permCredentialRetrieve))

	if got, err := resolveSecret(t, c, "kms://a/key"); err == nil {
		t.Fatalf("Resolve(kms://a/key) without secrets:read = %q, want an error", got)
	}
	if kms.calls != 0 {
		t.Fatalf("secrets service reached %d times without secrets:read, want 0", kms.calls)
	}
}

// ?ensure=true may create the secret, so it needs secrets:write as well as
// secrets:read.
func TestSecretResolver_DelegationWithoutSecretsWriteCannotEnsureKMS(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms, tenantGrant(t, "tenant-a", permSecretsRead))

	if got, err := resolveSecret(t, c, "kms://a/new?ensure=true"); err == nil {
		t.Fatalf("Resolve(kms://a/new?ensure=true) without secrets:write = %q, want an error", got)
	}
	if kms.ensureCalls != 0 {
		t.Fatalf("secrets service ensured %d times without secrets:write, want 0", kms.ensureCalls)
	}
	if _, ok := kms.byTenant["tenant-a"]["a/new"]; ok {
		t.Fatal("a/new was created without secrets:write")
	}
}

func TestSecretResolver_DelegationWithSecretsGrantsResolvesAndEnsures(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms,
		tenantGrant(t, "tenant-a", permSecretsRead, permSecretsWrite))

	got, err := resolveSecret(t, c, "kms://a/key")
	if err != nil {
		t.Fatalf("Resolve(kms://a/key): %v", err)
	}
	if got != "a-secret" {
		t.Fatalf("Resolve(kms://a/key) = %q, want a-secret", got)
	}
	got, err = resolveSecret(t, c, "kms://a/new?ensure=true")
	if err != nil {
		t.Fatalf("Resolve(kms://a/new?ensure=true): %v", err)
	}
	if got != "generated-a/new" {
		t.Fatalf("Resolve(kms://a/new?ensure=true) = %q, want generated-a/new", got)
	}
}

// The resolver authorizes against go-qrdb's own secrets permissions; a name
// qrdb does not register would refuse every delegation.
func TestSecretResolver_SecretsPermissionsAreQRDBRegistered(t *testing.T) {
	for _, perm := range []qrdbsdk.Permission{permissionSecretsRead, permissionSecretsWrite} {
		if !qrdbsdk.IsPermissionRegistered(perm) {
			t.Errorf("%s is not a go-qrdb registered permission", perm)
		}
	}
	if qrdbsdk.IsPermissionRegistered(qrdbsdk.Permission("secrets:unregistered")) {
		t.Error("an unregistered secrets permission reports registered")
	}
}

// A service-principal client authorizes nothing in the resolver: its calls
// are not delegated.
func TestEmbeddedIdentity_AuthorizeServicePrincipalIsNoop(t *testing.T) {
	id := ServicePrincipalIdentity(tenantGrant(t, ""))
	if err := id.authorize(context.Background(), permissionSecretsWrite); err != nil {
		t.Fatalf("authorize as the service principal: %v", err)
	}
}

// A revoked delegation fails at binding, before any permission is checked.
func TestEmbeddedIdentity_AuthorizeRevokedDelegationFails(t *testing.T) {
	id := DelegatedIdentity(refusedGrant(t), []*x509.Certificate{{}})
	if err := id.authorize(context.Background(), permissionSecretsRead); err == nil {
		t.Fatal("authorize under a revoked delegation succeeded, want an error")
	}
}
