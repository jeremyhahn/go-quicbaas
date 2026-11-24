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

//go:build integration

// Package services checks the service plane as the production server
// composes it: a node started with server.WithServicePlane keeps each
// tenant's signed services in its platform shard, and the seven lifecycle
// verbs reach that plane through the SDK. The per-transport cases run in
// sdk/go/transport/*; this package keeps what those harnesses cannot show:
// that server Start wires the plane over the node's own qrdb store.
package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/grants"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// startNode starts a real embedded node with the stub plane runtime and
// returns an embedded SDK client bound to it.
func startNode(t *testing.T, rt *serviceplanetest.Runtime, withPlane bool) quicbaas.Client {
	t.Helper()
	var plane *service.ServicePlaneRuntime
	if withPlane {
		plane = &service.ServicePlaneRuntime{
			Launcher: rt.Launcher, Grants: rt.Grants, Verifier: rt.Verifier,
		}
	}
	client, _ := startNodeWith(t, plane)
	return client
}

// startNodeWith starts an embedded node with the given service plane
// runtime (nil: no plane) and returns an SDK client bound to it and the
// node's service.
func startNodeWith(t *testing.T, rt *service.ServicePlaneRuntime) (quicbaas.Client, service.QuicBaaS) {
	t.Helper()
	dataDir := t.TempDir()
	cfg := config.EmbeddedConfig()
	cfg.IAM.SPIFFETrustDomain = helpers.SPIFFETrustDomain
	cfg.Storage.DataDir = filepath.Join(dataDir, "data")
	cfg.Storage.WALDir = filepath.Join(dataDir, "wal")
	cfg.Raft.NodeID = 1
	cfg.Logging.Level = "error"
	cfg.Server.Embedded = true

	var opts []server.ServerOption
	if rt != nil {
		opts = append(opts, server.WithServicePlane(*rt))
	}
	srv, err := server.NewServer(cfg, opts...)
	require.NoError(t, err)
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		assert.NoError(t, srv.Shutdown(ctx))
	})
	svc := srv.Service()
	require.NotNil(t, svc, "a started server must expose its service")
	client, err := quicbaas.NewClient("embedded://local", quicbaas.WithEmbedded(svc))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	return client, svc
}

func signed(t *testing.T, rt *serviceplanetest.Runtime, tenant, name string) *quicbaas.SignedServiceManifest {
	sm := rt.Sign(t, tenant, name)
	return &quicbaas.SignedServiceManifest{Payload: sm.Payload, KeyID: sm.KeyID, Signature: sm.Signature}
}

func TestServices_TheSevenVerbsOnARealNode(t *testing.T) {
	rt := serviceplanetest.NewRuntime(t)
	plane := startNode(t, rt, true).Services()
	acme := tenantctx.WithTenant(context.Background(), "acme")

	rec, err := plane.Deploy(acme, signed(t, rt, "acme", "web"))
	require.NoError(t, err)
	assert.Equal(t, "running", rec.State)
	for _, kind := range []string{"identity", "certificate", "name", "rbac", "secrets"} {
		assert.Equal(t, kind+":acme/web", rec.Grants[kind], "deploy grants the %s", kind)
	}

	for _, step := range []struct {
		verb  func(context.Context, string) (*quicbaas.ServiceRecord, error)
		state string
	}{{plane.Stop, "stopped"}, {plane.Start, "running"}, {plane.Restart, "running"}, {plane.Get, "running"}} {
		rec, err = step.verb(acme, "web")
		require.NoError(t, err)
		assert.Equal(t, step.state, rec.State)
	}
	rec, err = plane.Redeploy(acme, signed(t, rt, "acme", "web"))
	require.NoError(t, err)
	assert.EqualValues(t, 2, rec.Revision)

	globex := tenantctx.WithTenant(context.Background(), "globex")
	_, err = plane.Get(globex, "web")
	assert.Error(t, err, "another tenant cannot read the service")
	_, err = plane.Deploy(globex, signed(t, rt, "acme", "api"))
	assert.Error(t, err, "a tenant cannot deploy another tenant's manifest")

	list, err := plane.List(acme)
	require.NoError(t, err)
	require.Len(t, list.Services, 1)
	require.NoError(t, plane.Undeploy(acme, "web"))
	_, err = plane.Get(acme, "web")
	assert.Error(t, err)
}

// TestServices_DeployGrantsAllFiveOnARealNode starts a node with no grants,
// so the node builds the five itself over its own CA, secrets manager,
// platform registry and IAM: deploy grants each for real, the name resolves
// through the SDK registry, and undeploy revokes them.
// startRealGrantsNode starts a node whose service plane composes the
// node's own five grants (no stub grants), under the domain
// "svc.example.test", with the tenant "acme" created so the rbac grant
// can bind the manifest's built-in role, which the node seeds with the
// tenant.
func startRealGrantsNode(t *testing.T) (quicbaas.Client, service.QuicBaaS, *serviceplanetest.Runtime) {
	t.Helper()
	rt := serviceplanetest.NewRuntime(t)
	client, svc := startNodeWith(t, &service.ServicePlaneRuntime{
		Launcher: rt.Launcher, Verifier: rt.Verifier, Domain: "svc.example.test",
	})
	_, err := nodeIAM(t, svc).CreateTenant(context.Background(), &service.CreateTenantRequest{ID: "acme", Name: "acme"})
	require.NoError(t, err, "create tenant acme")
	return client, svc, rt
}

// nodeIAM answers the node's IAM service.
func nodeIAM(t *testing.T, svc service.QuicBaaS) service.IAMService {
	t.Helper()
	accessor, ok := svc.(interface{ IAM() service.IAMService })
	require.True(t, ok, "the node's service exposes IAM()")
	return accessor.IAM()
}

// asPrincipal answers a context authenticated as the principal id in
// the tenant "acme", the way the node's transports hand a request to
// its services.
func asPrincipal(id string) context.Context {
	return auth.SubjectIntoContext(tenantctx.WithTenant(context.Background(), "acme"),
		interfaces.Subject{ID: id, Authenticated: true})
}

// readLeaf reads the credential at path through the node's secrets API
// in ctx, checks it loads as a key pair, and answers its leaf.
func readLeaf(t *testing.T, svc service.QuicBaaS, ctx context.Context, path string) *x509.Certificate {
	t.Helper()
	got, err := svc.Secrets().GetSecret(ctx, &service.GetSecretRequest{SecretID: path})
	require.NoError(t, err, "read %s", path)
	require.NotNil(t, got)
	pair, err := tls.X509KeyPair(got.Data, got.Data)
	require.NoError(t, err, "%s loads as a key pair", path)
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	require.NoError(t, err)
	return leaf
}

// workloadUser answers the tenant acme's IAM principal named id, or nil.
func workloadUser(t *testing.T, svc service.QuicBaaS, id string) *iam.User {
	t.Helper()
	users, err := nodeIAM(t, svc).ListUsers(context.Background(), &service.IAMListUsersRequest{TenantID: "acme"})
	require.NoError(t, err)
	var found *iam.User
	for _, u := range users.Users {
		if u.Username == id {
			require.Nil(t, found, "one principal is named %s", id)
			found = u
		}
	}
	return found
}

// roleIDs answers the IDs of acme's roles named name.
func roleIDs(t *testing.T, svc service.QuicBaaS, name string) []string {
	t.Helper()
	roles, err := nodeIAM(t, svc).ListRoles(context.Background(), &service.IAMListRolesRequest{TenantID: "acme"})
	require.NoError(t, err)
	var ids []string
	for _, r := range roles.Roles {
		if r.Name == name {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func TestServices_DeployGrantsAllFiveOnARealNode(t *testing.T) {
	client, _, rt := startRealGrantsNode(t)
	plane := client.Services()
	acme := tenantctx.WithTenant(context.Background(), "acme")

	rec, err := plane.Deploy(acme, signed(t, rt, "acme", "web"))
	require.NoError(t, err)
	assert.Equal(t, "running", rec.State)
	kinds := []string{"identity", "certificate", "name", "rbac", "secrets"}
	require.Len(t, rec.Grants, len(kinds), "deploy grants exactly the five kinds: %v", rec.Grants)
	for _, kind := range kinds {
		ref := rec.Grants[kind]
		assert.NotEmpty(t, ref, "deploy grants the %s", kind)
		assert.NotEqual(t, kind+":acme/web", ref, "the %s grant is the node's, not a stub", kind)
	}
	id := grants.SPIFFEID(helpers.SPIFFETrustDomain, &serviceplane.Manifest{Tenant: "acme", Name: "web"})
	assert.True(t, strings.HasPrefix(rec.Grants["identity"], id+"#"),
		"the identity grant is the workload's SVID: %q", rec.Grants["identity"])
	assert.True(t, strings.HasSuffix(rec.Grants["rbac"], "@"+id),
		"the rbac grant binds the workload principal: %q", rec.Grants["rbac"])
	assert.Contains(t, rec.Grants["name"], "@web.acme.svc.example.test",
		"the name grant registers under the runtime's domain")

	resolved, err := client.Registry().Resolve(acme, "web")
	require.NoError(t, err)
	require.NotEmpty(t, resolved.Instances, "the deployed service's name resolves")

	require.NoError(t, plane.Undeploy(acme, "web"))
	resolved, err = client.Registry().Resolve(acme, "web")
	if err == nil {
		assert.Empty(t, resolved.Instances, "undeploy withdraws the name")
	}
}

func TestServices_ResumeAfterAFailedLaunch(t *testing.T) {
	rt := serviceplanetest.NewRuntime(t)
	plane := startNode(t, rt, true).Services()
	acme := tenantctx.WithTenant(context.Background(), "acme")

	rt.Launcher.Fail(stderrors.New("image pull failed"))
	_, err := plane.Deploy(acme, signed(t, rt, "acme", "web"))
	require.Error(t, err)
	rec, err := plane.Get(acme, "web")
	require.NoError(t, err, "a failed deploy is recorded so it can be resumed")
	assert.NotEqual(t, "running", rec.State)

	rt.Launcher.Fail(nil)
	rec, err = plane.Resume(acme, "web")
	require.NoError(t, err)
	assert.Equal(t, "running", rec.State)
}

func TestServices_ANodeWithoutAPlaneRefuses(t *testing.T) {
	rt := serviceplanetest.NewRuntime(t)
	plane := startNode(t, rt, false).Services()
	_, err := plane.Deploy(tenantctx.WithTenant(context.Background(), "acme"), signed(t, rt, "acme", "web"))
	require.Error(t, err, "a node started without WithServicePlane serves no services")
}

// The node's five grants compose into a workload that can authenticate:
// the bound workload reads its SVID and TLS credentials through the
// node's secrets API (another principal cannot), and the IAM principal
// named by its SPIFFE ID holds the manifest's role under exactly the
// SPKI binding of that SVID, the key the mTLS login resolves it by.
func TestServices_TheDeployedWorkloadReadsItsCredentialsAndHoldsItsRole(t *testing.T) {
	client, svc, rt := startRealGrantsNode(t)
	plane := client.Services()
	acme := tenantctx.WithTenant(context.Background(), "acme")

	rec, err := plane.Deploy(acme, signed(t, rt, "acme", "web"))
	require.NoError(t, err)
	require.Equal(t, "running", rec.State)
	t.Cleanup(func() { _ = plane.Undeploy(acme, "web") })

	id := grants.SPIFFEID(helpers.SPIFFETrustDomain, &serviceplane.Manifest{Tenant: "acme", Name: "web"})
	asWorkload := asPrincipal(id)

	svid := readLeaf(t, svc, asWorkload, "services/web/svid")
	require.Len(t, svid.URIs, 1, "the SVID names one SPIFFE ID")
	assert.Equal(t, id, svid.URIs[0].String(), "the SVID's URI SAN is the workload's SPIFFE ID")
	_, svidSerial, ok := strings.Cut(rec.Grants["identity"], "#")
	require.True(t, ok, "the identity grant carries the SVID serial: %q", rec.Grants["identity"])
	assert.Equal(t, svidSerial, svid.SerialNumber.Text(16), "the stored SVID is the granted one")

	leaf := readLeaf(t, svc, asWorkload, "services/web/tls")
	assert.Equal(t, rec.Grants["certificate"], leaf.SerialNumber.Text(16), "the stored TLS leaf is the granted one")
	assert.Contains(t, leaf.DNSNames, "web.acme.svc.example.test")

	for _, path := range []string{"services/web/svid", "services/web/tls"} {
		_, err := svc.Secrets().GetSecret(asPrincipal("mallory"), &service.GetSecretRequest{SecretID: path})
		assert.Error(t, err, "another principal cannot read %s", path)
	}

	role, _, ok := strings.Cut(rec.Grants["rbac"], "@")
	require.True(t, ok, "the rbac grant names its role: %q", rec.Grants["rbac"])
	u := workloadUser(t, svc, id)
	require.NotNil(t, u, "the workload principal %s exists", id)
	held := map[string]bool{}
	for _, r := range u.Roles {
		held[r] = true
	}
	holds := held[role]
	for _, rid := range roleIDs(t, svc, role) {
		holds = holds || held[rid]
	}
	assert.True(t, holds, "the workload principal holds the role %q: %v", role, u.Roles)
	require.Len(t, u.CertBindings, 1, "the workload principal has one certificate binding")
	binding := u.CertBindings[0]
	assert.Equal(t, qrdbsdk.CertBindingSPKIThumbprint, binding.Kind)
	assert.Equal(t, qrdbsdk.ComputeSPKIThumbprint(svid), binding.Thumbprint,
		"the principal is bound to the SVID's key")
	assert.True(t, binding.NotAfter.Equal(svid.NotAfter), "the binding expires with the SVID")
}

// Undeploy withdraws each of the node's real grants: both certificates
// are revoked at the CA, the credentials are destroyed, the principal
// holds no role, and the name no longer resolves.
func TestServices_UndeployWithdrawsEveryRealGrant(t *testing.T) {
	client, svc, rt := startRealGrantsNode(t)
	plane := client.Services()
	acme := tenantctx.WithTenant(context.Background(), "acme")

	rec, err := plane.Deploy(acme, signed(t, rt, "acme", "web"))
	require.NoError(t, err)
	require.Equal(t, "running", rec.State)
	id := grants.SPIFFEID(helpers.SPIFFETrustDomain, &serviceplane.Manifest{Tenant: "acme", Name: "web"})
	asWorkload := asPrincipal(id)
	svid := readLeaf(t, svc, asWorkload, "services/web/svid")
	leaf := readLeaf(t, svc, asWorkload, "services/web/tls")

	require.NoError(t, plane.Undeploy(acme, "web"))
	_, err = plane.Get(acme, "web")
	assert.Error(t, err, "an undeployed service is gone")

	for kind, cert := range map[string]*x509.Certificate{"SVID": svid, "TLS leaf": leaf} {
		got, err := svc.Certificate().IsRevoked(acme, &service.IsRevokedRequest{SerialNumber: cert.SerialNumber.Text(16)})
		require.NoError(t, err, "the CA answers for the %s", kind)
		assert.True(t, got.Revoked, "undeploy revokes the %s", kind)
	}

	for _, path := range []string{"services/web/svid", "services/web/tls"} {
		_, err := svc.Secrets().GetSecret(asWorkload, &service.GetSecretRequest{SecretID: path})
		assert.Error(t, err, "undeploy destroys %s", path)
	}

	if u := workloadUser(t, svc, id); u != nil {
		assert.Empty(t, u.Roles, "the workload principal holds no role after undeploy")
	}

	resolved, err := client.Registry().Resolve(acme, "web")
	if err == nil {
		assert.Empty(t, resolved.Instances, "undeploy withdraws the name")
	}

	assert.NotPanics(t, func() {
		assert.Error(t, plane.Undeploy(acme, "web"), "nothing is left to undeploy")
	})
}
