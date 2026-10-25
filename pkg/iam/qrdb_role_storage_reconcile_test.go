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

package iam_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"slices"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// These tests run the built-in role reconcile on a real in-process qrdb node
// (the consensus-arbitration carve-out, pkg/testutil/qrdbnode): what is under
// test is who go-qrdb lets converge a tenant's built-in roles, and a double
// would only agree with the test.
//
// The actor is the tenant's Security Officer (CA ruling 62(c), amended to
// (a)). go-qrdb grants roles:create, which the reconcile verb requires, to
// the SO role alone, so the tenant admin is refused.
//
// A tenant SO is an ordinary account of the tenant holding its "so" role.
// The tenant admin creates the account, has the tenant CA issue it a
// certificate, and only then assigns "so": go-qrdb refuses to issue a
// certificate to an account that already holds the role.

func TestMain(m *testing.M) {
	code := m.Run()
	qrdbnode.CloseShared()
	os.Exit(code)
}

// roleClients resolves the node's own embedded RoleClient, so the reconcile
// is reached through iam.QRDBRoleStorage exactly as the server reaches it.
type roleClients struct {
	roles qrdbtransport.RoleClient
}

func (r roleClients) RoleClient(context.Context) (qrdbtransport.RoleClient, error) {
	return r.roles, nil
}

// tenantActors are the authenticated contexts of one onboarded tenant.
type tenantActors struct {
	tenantID string
	admin    context.Context
	so       context.Context
}

// reconcileFixture is one tenant on the tenant-hosting node, plus the role
// storage under test.
type reconcileFixture struct {
	node    *qrdbnode.Node
	storage *iam.QRDBRoleStorage
	actors  tenantActors
}

func newReconcileFixture(t *testing.T, tenantID string) reconcileFixture {
	t.Helper()
	n := qrdbnode.TenantHost(t)
	return reconcileFixture{
		node:    n,
		storage: iam.NewQRDBRoleStorage(roleClients{roles: n.Node.Client().Roles()}),
		actors:  onboardTenant(t, n, tenantID),
	}
}

// onboardTenant creates tenantID with a certificate-enrolled admin, then has
// that admin seat the tenant's SO, and returns both identities.
func onboardTenant(t *testing.T, n *qrdbnode.Node, tenantID string) tenantActors {
	t.Helper()
	ctx := context.Background()
	client := n.Node.Client()

	svcCtx, err := n.Node.ServiceContext(ctx)
	require.NoError(t, err)
	adminName := tenantID + "-admin"
	resp, err := client.Tenants().CreateTenant(svcCtx, &qrdbtransport.CreateTenantRequest{
		ID:            tenantID,
		Name:          tenantID,
		IsolationType: qrdbtransport.TenantIsolationPrefix,
		AdminUsername: adminName,
		AdminCSRPEM:   newCSR(t, adminName),
	})
	require.NoError(t, err, "CreateTenant")
	require.NotNil(t, resp.Admin, "CreateTenant returned no admin certificate")
	admin := certContext(t, n, resp.Admin.CertPEM)

	soName := tenantID + "-so"
	so, err := client.Users().CreateUser(admin, &qrdbtransport.CreateUserRequest{
		Username:             soName,
		WithoutEmailLoginKey: true,
		TenantID:             tenantID,
	})
	require.NoError(t, err, "CreateUser for the SO")
	cert, err := client.Certs().IssueCert(admin, &qrdbtransport.IssueCertRequest{
		Username: soName,
		CSRPEM:   newCSR(t, soName),
	})
	require.NoError(t, err, "IssueCert for the SO")
	require.NoError(t, client.Roles().AssignRole(admin, so.ID, roleID(t, n, admin, tenantID, qrdbsdk.PermissionRoleSO)),
		"AssignRole so")

	return tenantActors{tenantID: tenantID, admin: admin, so: certContext(t, n, cert.CertPEM)}
}

// newCSR returns a PEM certificate request for a fresh P-256 key.
func newCSR(t *testing.T, commonName string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: commonName}}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

// certContext authenticates certPEM through the node's mTLS authentication,
// as a TLS listener would, and returns the authenticated context.
func certContext(t *testing.T, n *qrdbnode.Node, certPEM string) context.Context {
	t.Helper()
	block, _ := pem.Decode([]byte(certPEM))
	require.NotNil(t, block, "certificate is not PEM")
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	ctx, _, err := n.Node.AuthMiddleware().Authenticate(qrdbsdk.WithTLSState(context.Background(),
		&tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}))
	require.NoError(t, err, "Authenticate")
	return ctx
}

// roleID returns the identifier of tenantID's built-in role name.
func roleID(t *testing.T, n *qrdbnode.Node, ctx context.Context, tenantID, name string) string {
	t.Helper()
	return storedRole(t, n, ctx, tenantID, name).ID
}

// storedRole reads tenantID's role name as the node stores it.
func storedRole(t *testing.T, n *qrdbnode.Node, ctx context.Context, tenantID, name string) *qrdbtransport.Role {
	t.Helper()
	list, err := n.Node.Client().Roles().ListRoles(ctx, &qrdbtransport.ListRolesRequest{TenantID: tenantID})
	require.NoError(t, err, "ListRoles")
	for _, r := range list.Roles {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("tenant %s has no %q role", tenantID, name)
	return nil
}

// driftAuditor makes this build grant the auditor role a permission the
// tenant's stored auditor role does not carry: the drift a new build
// introduces when it adds a default grant after a tenant was seeded.
func driftAuditor(t *testing.T, perm qrdbsdk.Permission) {
	t.Helper()
	qrdbsdk.RegisterPermissions(perm)
	qrdbsdk.RegisterPermissionRoleGrant(qrdbsdk.PermissionRoleAuditor, perm)
	require.Contains(t, qrdbsdk.PermissionsForRole(qrdbsdk.PermissionRoleAuditor), perm)
}

// requireForbidden asserts a refused reconcile reaches the transports as a
// refusal, not as a server fault.
func requireForbidden(t *testing.T, err error) {
	t.Helper()
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	require.Equal(t, dberrors.ErrForbidden, qbErr.Code, "a refused reconcile is reported as %v", qbErr.Code)
}

func (f reconcileFixture) auditorHolds(t *testing.T, perm qrdbsdk.Permission) bool {
	t.Helper()
	role := storedRole(t, f.node, f.actors.so, f.actors.tenantID, qrdbsdk.PermissionRoleAuditor)
	return slices.Contains(role.Permissions, string(perm))
}

func TestQRDBRoleStorageReconcile_SORepairsDrift(t *testing.T) {
	f := newReconcileFixture(t, "recon-drift")
	const perm qrdbsdk.Permission = "reconciletest:drift"
	driftAuditor(t, perm)
	require.False(t, f.auditorHolds(t, perm), "the seeded auditor role already carries the new grant")

	report, err := f.storage.ReconcileBuiltInRoles(f.actors.so, f.actors.tenantID)
	require.NoError(t, err, "the tenant SO's reconcile was refused")

	require.Equal(t, f.actors.tenantID, report.TenantID)
	auditorID := roleID(t, f.node, f.actors.so, f.actors.tenantID, qrdbsdk.PermissionRoleAuditor)
	idx := slices.IndexFunc(report.Roles, func(r iam.RoleReconcileResult) bool { return r.RoleID == auditorID })
	require.GreaterOrEqual(t, idx, 0, "the report does not name the drifted auditor role: %+v", report.Roles)
	require.Contains(t, report.Roles[idx].Added, string(perm))
	require.False(t, report.Roles[idx].Created)

	require.True(t, f.auditorHolds(t, perm), "the stored auditor role still lacks the build's grant")
	stored := storedRole(t, f.node, f.actors.so, f.actors.tenantID, qrdbsdk.PermissionRoleAuditor)
	require.ElementsMatch(t, permissionStrings(qrdbsdk.PermissionsForRole(qrdbsdk.PermissionRoleAuditor)), stored.Permissions,
		"the stored auditor role does not match go-qrdb's registry after the reconcile")
}

func TestQRDBRoleStorageReconcile_NoDriftWritesNothing(t *testing.T) {
	f := newReconcileFixture(t, "recon-clean")
	// The first pass converges any drift an earlier test's registration left
	// for this tenant; the second is the one under test.
	_, err := f.storage.ReconcileBuiltInRoles(f.actors.so, f.actors.tenantID)
	require.NoError(t, err)
	before := storedRole(t, f.node, f.actors.so, f.actors.tenantID, qrdbsdk.PermissionRoleAuditor)

	report, err := f.storage.ReconcileBuiltInRoles(f.actors.so, f.actors.tenantID)
	require.NoError(t, err)
	require.Equal(t, f.actors.tenantID, report.TenantID)
	require.Empty(t, report.Roles, "a tenant that matches the build reported changes")

	after := storedRole(t, f.node, f.actors.so, f.actors.tenantID, qrdbsdk.PermissionRoleAuditor)
	require.ElementsMatch(t, before.Permissions, after.Permissions)
}

func TestQRDBRoleStorageReconcile_TenantAdminIsRefused(t *testing.T) {
	f := newReconcileFixture(t, "recon-admin")
	const perm qrdbsdk.Permission = "reconciletest:admin"
	driftAuditor(t, perm)

	_, err := f.storage.ReconcileBuiltInRoles(f.actors.admin, f.actors.tenantID)
	var denied *qrdbsdk.PermissionDeniedError
	require.ErrorAs(t, err, &denied, "the tenant admin reconciled; roles:create is SO-exclusive")
	requireForbidden(t, err)

	require.False(t, f.auditorHolds(t, perm), "a refused reconcile changed the stored role")
}

func TestQRDBRoleStorageReconcile_SOCannotReconcileAnotherTenant(t *testing.T) {
	home := newReconcileFixture(t, "recon-home")
	other := onboardTenant(t, home.node, "recon-other")
	const perm qrdbsdk.Permission = "reconciletest:scoped"
	driftAuditor(t, perm)

	// go-qrdb refuses a tenant-scoped caller naming another tenant before any
	// permission is resolved, with its typed cross-tenant denial.
	_, err := home.storage.ReconcileBuiltInRoles(home.actors.so, other.tenantID)
	var denied *qrdbsdk.CrossTenantDeniedError
	require.ErrorAs(t, err, &denied, "one tenant's SO reconciled another tenant's roles")
	requireForbidden(t, err)

	otherFixture := reconcileFixture{node: home.node, storage: home.storage, actors: other}
	require.False(t, otherFixture.auditorHolds(t, perm), "a refused cross-tenant reconcile changed the stored role")
}

func permissionStrings(perms []qrdbsdk.Permission) []string {
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = string(p)
	}
	return out
}
