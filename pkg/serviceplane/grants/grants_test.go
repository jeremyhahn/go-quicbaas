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

package grants

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"sync"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry/registrytest"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// roles records role bindings: tenant|principal -> role.
type roles struct {
	mu    sync.Mutex
	bound map[string]string
	err   error
}

func (r *roles) AssignRole(_ context.Context, tenant, principal, role string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.bound[tenant+"|"+principal+"|"+role] = role
	return nil
}

func (r *roles) RevokeRole(_ context.Context, tenant, principal, role string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bound, tenant+"|"+principal+"|"+role)
	return nil
}

type env struct {
	ca      xkmssdk.TrustedCA
	secrets *secrets.Manager
	reg     *registry.Registry
	roles   *roles
	grants  []serviceplane.Grant
}

func newEnv(t *testing.T) *env {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	require.NoError(t, err)
	e := &env{
		ca:      xkmsca.New(t, "serviceplane grants "+t.Name()),
		secrets: mgr,
		reg:     registry.New(registrytest.NewMemoryBackend()),
		roles:   &roles{bound: map[string]string{}},
	}
	e.grants, err = All(Config{CA: e.ca, Secrets: e.secrets, Registry: e.reg, Roles: e.roles, Domain: "svc.example"})
	require.NoError(t, err)
	return e
}

func (e *env) grant(k serviceplane.GrantKind) serviceplane.Grant {
	for _, g := range e.grants {
		if g.Kind() == k {
			return g
		}
	}
	return nil
}

func (e *env) kept(t *testing.T, tenant, path string) *x509.Certificate {
	t.Helper()
	data, _, err := e.secrets.Get(tenantctx.WithTenant(context.Background(), tenant), path)
	require.NoError(t, err)
	require.NotEmpty(t, data["private_key"])
	block, _ := pem.Decode(data["certificate"])
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert
}

func (e *env) revoked(t *testing.T, cert *x509.Certificate) bool {
	t.Helper()
	revoked, err := e.ca.IsRevoked(cert.SerialNumber)
	require.NoError(t, err)
	return revoked
}

func TestIdentity_IssuesAnX509SVIDAndRevokesIt(t *testing.T) {
	e := newEnv(t)
	m := serviceplanetest.Manifest("acme", "web")
	g := e.grant(serviceplane.GrantIdentity)
	ctx := context.Background()

	ref, err := g.Grant(ctx, m, "")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(ref, "spiffe://acme/svc/web#"), ref)

	cert := e.kept(t, "acme", "services/web/svid")
	require.Len(t, cert.URIs, 1)
	assert.Equal(t, "spiffe://acme/svc/web", cert.URIs[0].String(), "the SVID carries the one SPIFFE ID")
	assert.Empty(t, cert.DNSNames)
	assert.Contains(t, cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
	assert.Contains(t, cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
	assert.Equal(t, cert.SerialNumber.Text(16), strings.SplitN(ref, "#", 2)[1])

	again, err := g.Grant(ctx, m, ref)
	require.NoError(t, err)
	assert.Equal(t, ref, again, "a redeploy keeps the identity")

	require.NoError(t, g.Revoke(ctx, m, ref))
	assert.True(t, e.revoked(t, cert))
	_, _, err = e.secrets.Get(tenantctx.WithTenant(ctx, "acme"), "services/web/svid")
	assert.Error(t, err, "the SVID and its key are dropped")

	var bad *RefError
	assert.ErrorAs(t, g.Revoke(ctx, m, "no-serial"), &bad)
}

func TestCertificate_IssuesTheRegisteredNamesLeaf(t *testing.T) {
	e := newEnv(t)
	m := serviceplanetest.Manifest("acme", "web")
	g := e.grant(serviceplane.GrantCertificate)
	ctx := context.Background()

	ref, err := g.Grant(ctx, m, "")
	require.NoError(t, err)
	cert := e.kept(t, "acme", "services/web/tls")
	assert.Equal(t, []string{"web.acme.svc.example"}, cert.DNSNames)
	assert.Equal(t, cert.SerialNumber.Text(16), ref)

	require.NoError(t, g.Revoke(ctx, m, ref))
	assert.True(t, e.revoked(t, cert))

	_, err = (&Certificate{CA: e.ca, Secrets: e.secrets}).Grant(ctx, m, "")
	var cfg *ConfigError
	assert.ErrorAs(t, err, &cfg)
}

func TestName_RegistersInTheTenantsRegistry(t *testing.T) {
	e := newEnv(t)
	m := serviceplanetest.Manifest("acme", "web")
	g := e.grant(serviceplane.GrantName)
	ctx := context.Background()
	acme := tenantctx.WithTenant(ctx, "acme")

	ref, err := g.Grant(ctx, m, "")
	require.NoError(t, err)
	got, _, err := e.reg.Resolve(acme, "web")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, strings.HasPrefix(got[0].Address, "web.acme.svc.example"), got[0].Address)
	assert.Equal(t, "spiffe://acme/svc/web", got[0].Metadata["spiffe_id"])

	other, _, err := e.reg.Resolve(tenantctx.WithTenant(ctx, "globex"), "web")
	require.NoError(t, err)
	assert.Empty(t, other, "another tenant does not see the name")

	require.NoError(t, g.Revoke(ctx, m, ref))
	got, _, err = e.reg.Resolve(acme, "web")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NoError(t, g.Revoke(ctx, m, ref), "a name already gone is revoked")
}

func TestRBAC_BindsTheBuiltInRoleToTheIdentity(t *testing.T) {
	e := newEnv(t)
	m := serviceplanetest.Manifest("acme", "web")
	g := e.grant(serviceplane.GrantRBAC)
	ctx := context.Background()

	ref, err := g.Grant(ctx, m, "")
	require.NoError(t, err)
	assert.Equal(t, m.Role+"@spiffe://acme/svc/web", ref)
	assert.Contains(t, e.roles.bound, "acme|spiffe://acme/svc/web|"+m.Role)

	moved := *m
	moved.Role = "auditor"
	ref2, err := g.Grant(ctx, &moved, ref)
	require.NoError(t, err)
	assert.Equal(t, "auditor@spiffe://acme/svc/web", ref2)
	assert.Contains(t, e.roles.bound, "acme|spiffe://acme/svc/web|auditor")
	assert.NotContains(t, e.roles.bound, "acme|spiffe://acme/svc/web|"+m.Role, "a role change unbinds the prior role")

	require.NoError(t, g.Revoke(ctx, &moved, ref2))
	assert.Empty(t, e.roles.bound)
}

func TestSecrets_BindsTheNamespaceToTheIdentity(t *testing.T) {
	e := newEnv(t)
	m := serviceplanetest.Manifest("acme", "web")
	g := e.grant(serviceplane.GrantSecrets)
	ctx := context.Background()
	acme := tenantctx.WithTenant(ctx, "acme")

	ref, err := g.Grant(ctx, m, "")
	require.NoError(t, err)
	assert.Equal(t, "services/web/", ref)
	data, meta, err := e.secrets.Get(acme, "services/web/binding")
	require.NoError(t, err)
	assert.Equal(t, "spiffe://acme/svc/web", string(data["spiffe_id"]))
	assert.Equal(t, "spiffe://acme/svc/web", meta["bound_to"])

	require.NoError(t, g.Revoke(ctx, m, ref))
	_, _, err = e.secrets.Get(acme, "services/web/binding")
	assert.Error(t, err)
}

func TestAll_RequiresEveryDependency(t *testing.T) {
	e := newEnv(t)
	full := Config{CA: e.ca, Secrets: e.secrets, Registry: e.reg, Roles: e.roles, Domain: "d"}
	for name, c := range map[string]Config{
		"ca":       {Secrets: full.Secrets, Registry: full.Registry, Roles: full.Roles, Domain: "d"},
		"secrets":  {CA: full.CA, Registry: full.Registry, Roles: full.Roles, Domain: "d"},
		"registry": {CA: full.CA, Secrets: full.Secrets, Roles: full.Roles, Domain: "d"},
		"roles":    {CA: full.CA, Secrets: full.Secrets, Registry: full.Registry, Domain: "d"},
		"domain":   {CA: full.CA, Secrets: full.Secrets, Registry: full.Registry, Roles: full.Roles},
	} {
		_, err := All(c)
		var cfg *ConfigError
		assert.ErrorAs(t, err, &cfg, "without %s", name)
	}
	gs, err := All(full)
	require.NoError(t, err)
	for i, g := range gs {
		assert.Equal(t, serviceplane.GrantKinds[i], g.Kind(), "deploy order")
	}
}

// TestPlane_DeployGrantsAndUndeployRevokesThemAll drives the real plane over
// the five real grants.
func TestPlane_DeployGrantsAndUndeployRevokesThemAll(t *testing.T) {
	e := newEnv(t)
	rt := serviceplanetest.NewRuntime(t)
	rt.Grants = e.grants
	plane := rt.Plane(t)
	ctx := context.Background()
	acme := tenantctx.WithTenant(ctx, "acme")

	svc, err := plane.Deploy(ctx, "acme", rt.Sign(t, "acme", "web"))
	require.NoError(t, err)
	require.Len(t, svc.Grants, len(serviceplane.GrantKinds))
	svid := e.kept(t, "acme", "services/web/svid")
	leaf := e.kept(t, "acme", "services/web/tls")
	names, _, err := e.reg.Resolve(acme, "web")
	require.NoError(t, err)
	require.Len(t, names, 1)
	require.Len(t, e.roles.bound, 1)

	again, err := plane.Redeploy(ctx, "acme", rt.Sign(t, "acme", "web"))
	require.NoError(t, err)
	assert.Equal(t, svc.Grants, again.Grants, "a redeploy keeps every grant")
	assert.False(t, e.revoked(t, svid))

	require.NoError(t, plane.Undeploy(ctx, "acme", "web"))
	assert.True(t, e.revoked(t, svid), "undeploy revokes the SVID")
	assert.True(t, e.revoked(t, leaf), "undeploy revokes the certificate")
	names, _, err = e.reg.Resolve(acme, "web")
	require.NoError(t, err)
	assert.Empty(t, names, "undeploy deregisters the name")
	assert.Empty(t, e.roles.bound, "undeploy unbinds the role")
	_, _, err = e.secrets.Get(acme, "services/web/binding")
	assert.Error(t, err, "undeploy unbinds the secrets")
}

// TestPlane_AFailedGrantRevokesTheOnesMade checks a refused role binding
// leaves no SVID, certificate or name behind.
func TestPlane_AFailedGrantRevokesTheOnesMade(t *testing.T) {
	e := newEnv(t)
	e.roles.err = errors.New("role not found")
	rt := serviceplanetest.NewRuntime(t)
	rt.Grants = e.grants
	plane := rt.Plane(t)
	ctx := context.Background()
	acme := tenantctx.WithTenant(ctx, "acme")

	_, err := plane.Deploy(ctx, "acme", rt.Sign(t, "acme", "web"))
	require.Error(t, err)
	for _, path := range []string{"services/web/svid", "services/web/tls"} {
		_, _, gerr := e.secrets.Get(acme, path)
		assert.Error(t, gerr, "%s is dropped", path)
	}
	names, _, err := e.reg.Resolve(acme, "web")
	require.NoError(t, err)
	assert.Empty(t, names)
}

var _ CA = xkmssdk.TrustedCA(nil)

func TestWithdraw_RefusesAMalformedSerial(t *testing.T) {
	e := newEnv(t)
	var bad *RefError
	assert.ErrorAs(t, withdraw(context.Background(), e.ca, e.secrets, "p", serviceplane.GrantCertificate, "zz"), &bad)
}
