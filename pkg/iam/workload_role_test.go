// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package iam

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

const workloadTestPrincipal = "spiffe://example.org/tenant/t1/svc/web"

// workloadTestSVID is a self-signed SVID whose URI SAN is uri.
func workloadTestSVID(t *testing.T, uri string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: uri},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour).Truncate(time.Second),
		URIs:         []*url.URL{u},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// wantSVIDBinding fails unless u carries exactly svid's SPKI binding.
func wantSVIDBinding(t *testing.T, u *User, svid *x509.Certificate) {
	t.Helper()
	if len(u.CertBindings) != 1 {
		t.Fatalf("cert bindings = %v, want one", u.CertBindings)
	}
	got := u.CertBindings[0]
	if got.Kind != qrdbsdk.CertBindingSPKIThumbprint || got.Thumbprint != qrdbsdk.ComputeSPKIThumbprint(svid) || !got.NotAfter.Equal(svid.NotAfter) {
		t.Fatalf("cert binding = %+v, want the SVID's SPKI binding", got)
	}
}

func TestWorkloadRoleBinder_AssignWithTheRealMemoryStorage(t *testing.T) {
	ctx := context.Background()
	roles := newMockRoleStorage()
	if err := roles.CreateRole(ctx, &Role{ID: "role-viewer", TenantID: "t1", Name: "viewer"}); err != nil {
		t.Fatal(err)
	}
	users := NewMemoryStorage()
	b := &WorkloadRoleBinder{Store: NewIdentityStore(IdentityStoreOptions{UserStorage: users, RoleStorage: roles})}
	svid := workloadTestSVID(t, workloadTestPrincipal)
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer", svid); err != nil {
		t.Fatalf("assign over the real storage: %v", err)
	}
	u, err := users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Roles) != 1 || u.Roles[0] != "role-viewer" {
		t.Fatalf("roles = %v, want [role-viewer]", u.Roles)
	}
	wantSVIDBinding(t, u, svid)

	// A reissued SVID replaces the binding.
	next := workloadTestSVID(t, workloadTestPrincipal)
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer", next); err != nil {
		t.Fatal(err)
	}
	u, err = users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	wantSVIDBinding(t, u, next)
	if len(u.Roles) != 1 {
		t.Fatalf("roles = %v, want one", u.Roles)
	}
}

func TestWorkloadRoleBinder_RefusesAnSVIDThatIsNotThePrincipals(t *testing.T) {
	b, users := newWorkloadBinderStore(t)
	ctx := context.Background()
	var se *WorkloadSVIDError
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer", nil); !errors.As(err, &se) {
		t.Fatalf("nil SVID: err = %v, want WorkloadSVIDError", err)
	}
	other := workloadTestSVID(t, "spiffe://example.org/tenant/t1/svc/other")
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer", other); !errors.As(err, &se) {
		t.Fatalf("another principal's SVID: err = %v, want WorkloadSVIDError", err)
	}
	if _, err := users.GetUserByUsername(ctx, "t1", workloadTestPrincipal); err == nil {
		t.Fatal("a refused SVID created the principal")
	}
}

func newWorkloadBinderStore(t *testing.T) (*WorkloadRoleBinder, *mockUserStorage) {
	t.Helper()
	users := newMockUserStorage()
	roles := newMockRoleStorage()
	for _, r := range []*Role{
		{ID: "role-viewer", TenantID: "t1", Name: "viewer"},
		{ID: "role-operator", TenantID: "t1", Name: "operator"},
	} {
		if err := roles.CreateRole(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	store := NewIdentityStore(IdentityStoreOptions{UserStorage: users, RoleStorage: roles})
	return &WorkloadRoleBinder{Store: store}, users
}

func TestWorkloadRoleBinder_AssignCreatesMachinePrincipal(t *testing.T) {
	b, users := newWorkloadBinderStore(t)
	ctx := context.Background()
	svid := workloadTestSVID(t, workloadTestPrincipal)
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer", svid); err != nil {
		t.Fatal(err)
	}
	u, err := users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Roles) != 1 || u.Roles[0] != "role-viewer" {
		t.Fatalf("roles = %v, want [role-viewer]", u.Roles)
	}
	if len(u.AuthMethods) != 1 || u.AuthMethods[0] != AuthMethodMTLS {
		t.Fatalf("auth methods = %v, want [mtls]", u.AuthMethods)
	}
	if u.Email != "" || u.Lifecycle != LifecycleActive {
		t.Fatalf("principal = email %q lifecycle %q", u.Email, u.Lifecycle)
	}
	wantSVIDBinding(t, u, svid)
	// A second grant of the same role is a no-op; another role is added.
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer", svid); err != nil {
		t.Fatal(err)
	}
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "operator", svid); err != nil {
		t.Fatal(err)
	}
	u, _ = users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if len(u.Roles) != 2 {
		t.Fatalf("roles = %v, want viewer and operator", u.Roles)
	}
}

func TestWorkloadRoleBinder_Revoke(t *testing.T) {
	b, users := newWorkloadBinderStore(t)
	ctx := context.Background()
	svid := workloadTestSVID(t, workloadTestPrincipal)
	// Revoking from a principal that never existed is already done.
	if err := b.RevokeRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatal(err)
	}
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer", svid); err != nil {
		t.Fatal(err)
	}
	if err := b.RevokeRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatal(err)
	}
	u, err := users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Roles) != 0 {
		t.Fatalf("roles = %v, want none", u.Roles)
	}
	if err := b.RevokeRole(ctx, "t1", workloadTestPrincipal, "viewer"); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
}

func TestWorkloadRoleBinder_Refusals(t *testing.T) {
	b, users := newWorkloadBinderStore(t)
	ctx := context.Background()
	svid := workloadTestSVID(t, workloadTestPrincipal)

	var pe *WorkloadPrincipalError
	if err := b.AssignRole(ctx, "t1", "alice@example.org", "viewer", svid); !errors.As(err, &pe) {
		t.Fatalf("non-SPIFFE principal: err = %v, want WorkloadPrincipalError", err)
	}
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "no-such-role", svid); err == nil {
		t.Fatal("unknown role: want an error")
	}
	if err := b.AssignRole(ctx, "t2", workloadTestPrincipal, "viewer", svid); err == nil {
		t.Fatal("role of another tenant: want an error")
	}

	// A human account that shares the name is never widened.
	human := &User{TenantID: "t1", Username: workloadTestPrincipal, AuthMethods: []string{"webauthn"}, Lifecycle: LifecycleActive}
	if err := users.CreateUser(ctx, human); err != nil {
		t.Fatal(err)
	}
	if err := b.AssignRole(ctx, "t1", workloadTestPrincipal, "viewer", svid); err == nil {
		t.Fatal("human account: want a refusal")
	}
	u, _ := users.GetUserByUsername(ctx, "t1", workloadTestPrincipal)
	if len(u.Roles) != 0 {
		t.Fatalf("human account roles = %v, want none", u.Roles)
	}
}
