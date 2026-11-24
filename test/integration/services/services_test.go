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
	stderrors "errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
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
func TestServices_DeployGrantsAllFiveOnARealNode(t *testing.T) {
	rt := serviceplanetest.NewRuntime(t)
	client, svc := startNodeWith(t, &service.ServicePlaneRuntime{
		Launcher: rt.Launcher, Verifier: rt.Verifier, Domain: "svc.example.test",
	})
	// The rbac grant binds the manifest's built-in role, which the node
	// seeds when it creates the tenant.
	accessor, ok := svc.(interface{ IAM() service.IAMService })
	require.True(t, ok, "the node's service exposes IAM()")
	_, err := accessor.IAM().CreateTenant(context.Background(), &service.CreateTenantRequest{ID: "acme", Name: "acme"})
	require.NoError(t, err, "create tenant acme")
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
