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
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
)

// getSecretBytes reads the whole payload of the secret at path through the
// ordinary Security().Secrets() accessor, returning a copy of it.
func getSecretBytes(t *testing.T, c Client, path string) (string, error) {
	t.Helper()
	var got string
	err := c.Security().Secrets().GetWithBytes(context.Background(), path, "", func(payload []byte) error {
		got = string(payload)
		return nil
	})
	return got, err
}

// A delegation without secrets:read is refused on the ordinary Secrets
// accessor before the secrets service is reached, so no plaintext leaves it
// (Codex review 7361 P1 1 on #1841).
func TestEmbeddedTransport_DelegationWithoutSecretsReadCannotGetSecret(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms, tenantGrant(t, "tenant-a", permCredentialRetrieve))

	got, err := getSecretBytes(t, c, "a/key")
	if err == nil {
		t.Fatalf("GetWithBytes(a/key) without secrets:read = %q, want a refusal", got)
	}
	if got != "" {
		t.Fatalf("GetWithBytes(a/key) without secrets:read returned plaintext %q", got)
	}
	if kms.calls != 0 {
		t.Fatalf("secrets service reached %d times without secrets:read, want 0", kms.calls)
	}
}

// A delegation holding secrets:read reads its own tenant's secret.
func TestEmbeddedTransport_DelegationWithSecretsReadGetsSecret(t *testing.T) {
	kms := newTenantSecretsService()
	c := newDelegatedSecretClientWithSecrets(t, newTenantCredentialService(), kms, tenantGrant(t, "tenant-a", permSecretsRead))

	got, err := getSecretBytes(t, c, "a/key")
	if err != nil {
		t.Fatalf("GetWithBytes(a/key) with secrets:read: %v", err)
	}
	if got != "a-secret" {
		t.Fatalf("GetWithBytes(a/key) = %q, want \"a-secret\"", got)
	}
}

// unreachableService is a service facade no refused call may reach: every
// method is the nil embedded interface's.
type unreachableService struct {
	service.QuicBaaS
}

// newDelegatedTransport returns an embedded transport wired as
// createEmbeddedTransport wires it, for a delegation bound through binder.
func newDelegatedTransport(t *testing.T, binder EmbeddedIdentityBinder) *embedded.Transport {
	t.Helper()
	id := DelegatedIdentity(binder, []*x509.Certificate{{}})
	tr, err := embedded.New(unreachableService{},
		embedded.WithContextBinder(id.bind),
		embedded.WithOperationAuthorizer(id.authorizeOperation))
	if err != nil {
		t.Fatalf("embedded.New: %v", err)
	}
	return tr
}

// Each verb family refuses a delegation that lacks the operation's declared
// permission before dispatch. The delegation holds secrets:read only, so it
// is a real but restricted grant set.
func TestEmbeddedTransport_DelegationRefusedPerVerbFamily(t *testing.T) {
	tr := newDelegatedTransport(t, tenantGrant(t, "tenant-a", permSecretsRead))
	cases := []struct{ svc, method string }{
		{"quicbaas.Secrets", "Store"},
		{"quicbaas.KV", "Get"},
		{"quicbaas.ParameterStore", "GetParameter"},
		{"quicbaas.KMS", "GenerateKey"},
		{"quicbaas.Crypto", "Encrypt"},
		{"quicbaas.Certificate", "IssueCertificate"},
		{"quicbaas.SSH", "GenerateKey"},
		{"quicbaas.DynamicSecrets", "CreateRole"},
		{"quicbaas.ObjectStorage", "PutObject"},
		{"quicbaas.Messaging", "CreateChannel"},
		{"quicbaas.Orchestrator", "Inference"},
		{"quicbaas.Attestation", "Verify"},
		{"quicbaas.IAM.User", "Create"},
		{"quicbaas.IAM.Tenant", "Create"},
	}
	for _, tc := range cases {
		t.Run(tc.svc+"/"+tc.method, func(t *testing.T) {
			resp, err := tr.Request(context.Background(), tc.svc, tc.method, nil)
			if err == nil {
				t.Fatalf("Request = %v, want a refusal", resp)
			}
			var undeclared *DelegatedOperationUndeclaredError
			if stderrors.As(err, &undeclared) {
				t.Fatalf("Request refused as undeclared, want a grant refusal: %v", err)
			}
		})
	}
}

// A stream operation is refused the same way before its handler starts.
func TestEmbeddedTransport_DelegationRefusedOnStream(t *testing.T) {
	tr := newDelegatedTransport(t, tenantGrant(t, "tenant-a", permSecretsRead))
	s, err := tr.RequestStream(context.Background(), "quicbaas.KV", "Watch", nil)
	if err == nil {
		t.Fatalf("RequestStream(KV/Watch) = %v, want a refusal", s)
	}
}

// An operation that declares no permission is refused to every delegation,
// so no grant set widens a delegation to it.
func TestEmbeddedTransport_DelegationRefusedUndeclaredOperation(t *testing.T) {
	tr := newDelegatedTransport(t, tenantGrant(t, "tenant-a", permSecretsRead, permSecretsWrite))
	_, err := tr.Request(context.Background(), "quicbaas.Auth", "Login", nil)
	var undeclared *DelegatedOperationUndeclaredError
	if !stderrors.As(err, &undeclared) {
		t.Fatalf("Request(Auth/Login) = %v, want DelegatedOperationUndeclaredError", err)
	}
	if undeclared.Operation != "quicbaas.Auth/Login" {
		t.Errorf("refused operation = %q, want quicbaas.Auth/Login", undeclared.Operation)
	}
}

// A delegation whose binding fails is refused before authorization.
func TestEmbeddedTransport_DelegationBindRefused(t *testing.T) {
	tr := newDelegatedTransport(t, refusedGrant(t))
	_, err := tr.Request(context.Background(), "quicbaas.Secrets", "Get", nil)
	var refused *bindRefusedError
	if !stderrors.As(err, &refused) {
		t.Fatalf("Request = %v, want bindRefusedError", err)
	}
}

// The service principal is not narrowed: it is admitted to declared and
// undeclared operations alike.
func TestEmbeddedIdentity_ServicePrincipalAuthorizesEveryOperation(t *testing.T) {
	id := ServicePrincipalIdentity(tenantGrant(t, "tenant-a"))
	if err := id.authorizeOperation(context.Background(), "quicbaas.Auth/Login", nil); err != nil {
		t.Errorf("undeclared operation: %v", err)
	}
	if err := id.authorizeOperation(context.Background(), "quicbaas.Secrets/Get", []string{"secrets:read"}); err != nil {
		t.Errorf("declared operation: %v", err)
	}
}

// A delegation whose binder supplies no AuthMiddleware cannot be admitted to
// a declared operation.
func TestEmbeddedIdentity_DelegationWithoutMiddlewareRefused(t *testing.T) {
	id := DelegatedIdentity(&recordingBinder{}, []*x509.Certificate{{}})
	err := id.authorizeOperation(context.Background(), "quicbaas.Secrets/Get", []string{"secrets:read"})
	var noAuthz *DelegationWithoutAuthorizationError
	if !stderrors.As(err, &noAuthz) {
		t.Fatalf("authorizeOperation = %v, want DelegationWithoutAuthorizationError", err)
	}
}
