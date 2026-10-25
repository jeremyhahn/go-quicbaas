package iam

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// TestWebAuthnServiceFactory_ServiceFor_AuthorizedNode builds the factory over
// a real embedded qrdb node, whose transport authorizes every call. Building a
// tenant's service registers the credential DAO's indexes, which go-qrdb does
// with a context of its own: unless the store the factory hands go-xkms binds
// an identity to every call, the node refuses it as unauthenticated and no
// registration or login can start (Codex review 6709 on #1619).
func TestWebAuthnServiceFactory_ServiceFor_AuthorizedNode(t *testing.T) {
	n := qrdbnode.TenantHost(t)

	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                n.Node.Client(),
		Service:             n.Node,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}

	svc, err := f.ServiceFor(context.Background(), "tenant-authz")
	if err != nil {
		t.Fatalf("ServiceFor on an authorizing node: %v", err)
	}
	if svc == nil {
		t.Fatal("ServiceFor returned a nil service")
	}
}
