//go:build rest && mcp

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

package apimodule

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	sdkembedded "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
	sdkgrpc "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
	sdkmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
	sdkrest "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
	sdkunix "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/unix"
)

// =============================================================================
// Cross-transport Project parity (go-quicbaas#1261)
//
// The Go SDK Project client answered differently per transport: REST and QUIC
// decoded the server's {project, timestamp} envelope as a bare record and
// returned an empty ProjectInfo, gRPC and embedded dropped the managed
// workspace and provisioning state, and neither REST nor MCP could look a
// project up by name. This drives the SDK client over every transport against
// ONE real project service and asserts each answers create, get, get-by-name,
// update and list with the same populated ProjectInfo, and reports a deleted
// project gone alike.
//
// QUIC is the REST transport over HTTP/3 (sdk/go/transport/quic embeds the
// REST transport and the server mounts the same router), so the REST row
// covers its wire format.
// =============================================================================

const parityProjectTenant = "tenant-parity"

// parityRoles is the role matrix the tests grant and expect back.
var parityRoles = map[string]project.ProjectRole{"node-1": project.ProjectRoleLead}

// projectParityService is the one real project service every transport in
// this test reaches, with managed workspaces enabled under a temp root.
func projectParityService(t *testing.T) service.ProjectService {
	t.Helper()

	store := orchestration.NewInMemoryProjectStore()
	svc := service.NewProjectService(store)
	workspaces, err := orchestration.NewWorkspaceManager(t.TempDir())
	require.NoError(t, err)
	provisioner, err := service.NewProjectWorkspaceProvisioner(service.ProjectWorkspaceProvisionerConfig{
		Workspaces: workspaces,
		Store:      store,
	})
	require.NoError(t, err)

	wirer, ok := svc.(interface {
		SetWorkspaceProvisioner(*service.ProjectWorkspaceProvisioner)
	})
	require.True(t, ok, "the project service no longer accepts a workspace provisioner")
	wirer.SetWorkspaceProvisioner(provisioner)
	return svc
}

// parityTLS issues a self-signed localhost certificate and returns the server
// and client TLS configurations that trust it.
func parityTLS(t *testing.T) (server, client *tls.Config) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	server = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}},
		MinVersion:   tls.VersionTLS13,
	}
	client = &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	return server, client
}

// serveParityGRPC serves the gRPC services register registers on lis until
// the test ends, every call bound to parityProjectTenant.
func serveParityGRPC(t *testing.T, register func(grpclib.ServiceRegistrar), serverTLS *tls.Config, lis net.Listener) {
	t.Helper()

	srv := grpclib.NewServer(grpclib.Creds(credentials.NewTLS(serverTLS)),
		grpclib.UnaryInterceptor(parityTenantInterceptor))
	register(srv)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		<-served
	})
}

// parityTenantInterceptor binds every gRPC call to parityProjectTenant, as
// the authentication interceptor binds a caller to its own tenant.
func parityTenantInterceptor(ctx context.Context, req any, _ *grpclib.UnaryServerInfo, handler grpclib.UnaryHandler) (any, error) {
	return handler(tenantctx.WithTenant(ctx, parityProjectTenant), req)
}

// parityTenantHandler binds every REST call to parityProjectTenant, as the
// authentication middleware binds a caller to its own tenant.
func parityTenantHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(tenantctx.WithTenant(r.Context(), parityProjectTenant)))
	})
}

// projectParityTransports builds one SDK transport per reachable transport,
// each served by the real server-side Project adapter over svc.
func projectParityTransports(t *testing.T, svc service.ProjectService) map[string]transport.Transport {
	t.Helper()

	facade := mocks.NewMockQuicBaaS()
	facade.ProjectServiceFunc = func() service.ProjectService { return svc }
	return parityTransports(t, facade, func(srv grpclib.ServiceRegistrar) {
		pb.RegisterProjectServiceServer(srv, grpcapi.NewProjectServer(svc, testLogger(t)))
	})
}

// parityTransports builds one SDK transport per reachable transport over
// facade: the production REST router, the gRPC services registerGRPC
// registers (over TCP and over a unix socket), the production MCP module
// registration and the embedded transport.
func parityTransports(t *testing.T, facade *mocks.MockQuicBaaS, registerGRPC func(grpclib.ServiceRegistrar)) map[string]transport.Transport {
	t.Helper()

	log := testLogger(t)
	transports := map[string]transport.Transport{}
	add := func(name string, tr transport.Transport, err error) {
		t.Helper()
		require.NoError(t, err, "%s transport", name)
		t.Cleanup(func() { assert.NoError(t, tr.Close(), "%s close", name) })
		transports[name] = tr
	}

	// REST (and the QUIC wire, which is the same router over HTTP/3).
	restSrv, err := restapi.NewServer(facade, restapi.DefaultConfig(), log)
	require.NoError(t, err)
	restHTTP := httptest.NewServer(parityTenantHandler(restSrv.GetRouter()))
	t.Cleanup(restHTTP.Close)
	restTr, err := sdkrest.New(&transport.Config{Address: restHTTP.URL, Timeout: 5 * time.Second})
	add("rest", restTr, err)

	// gRPC over TCP, and unix: the same gRPC wire over a UDS.
	serverTLS, clientTLS := parityTLS(t)
	tcpLis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveParityGRPC(t, registerGRPC, serverTLS, tcpLis)
	grpcTr, err := sdkgrpc.New(&transport.Config{
		Address: tcpLis.Addr().String(), Timeout: 5 * time.Second, TLSConfig: clientTLS,
	})
	add("grpc", grpcTr, err)

	// A short socket directory: t.TempDir() embeds the test name and can
	// exceed the 107-byte sun_path limit.
	sockDir, err := os.MkdirTemp("", "pp")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(sockDir)) })
	sockPath := filepath.Join(sockDir, "p.sock")
	unixLis, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	serveParityGRPC(t, registerGRPC, serverTLS, unixLis)
	unixTr, err := sdkunix.New(&transport.Config{
		Address: sockPath, Timeout: 5 * time.Second, TLSConfig: clientTLS,
	})
	add("unix", unixTr, err)

	// MCP: the production module registration over a real streamable-HTTP
	// MCP server.
	mcpBackend := mcpapi.NewServer(facade, nil, log)
	module, err := NewModule(func() service.QuicBaaS { return facade }, log,
		WithMCPServer(func() *mcpapi.Server { return mcpBackend }))
	require.NoError(t, err)
	mcpSrv := mcpserver.NewMCPServer("quicbaas-parity", "1.0.0")
	module.RegisterMCP(mcpSrv)
	mcpHTTP := mcpserver.NewTestStreamableHTTPServer(mcpSrv)
	t.Cleanup(mcpHTTP.Close)
	mcpTr, err := sdkmcp.New(&transport.Config{Address: mcpHTTP.URL, Timeout: 5 * time.Second})
	add("mcp", mcpTr, err)

	embeddedTr, err := sdkembedded.New(facade)
	add("embedded", embeddedTr, err)

	return transports
}

// assertPopulatedProject asserts info carries every field the caller set, plus
// the server-assigned identity and provisioning state. roles is nil until an
// update grants one: the SDK create request carries no role matrix.
func assertPopulatedProject(t *testing.T, info *project.ProjectInfo, name, description string, roles map[string]project.ProjectRole) {
	t.Helper()

	require.NotNil(t, info)
	assert.NotEmpty(t, info.ID, "ID")
	assert.Equal(t, parityProjectTenant, info.TenantID, "TenantID")
	assert.Equal(t, name, info.Name, "Name")
	assert.Equal(t, description, info.Description, "Description")
	assert.NotEmpty(t, info.Workspace.Path, "Workspace.Path")
	assert.True(t, info.Provisioning.Managed, "Provisioning.Managed")
	assert.Equal(t, project.ProvisioningStateReady, info.Provisioning.State, "Provisioning.State")
	assert.False(t, info.Provisioning.UpdatedAt.IsZero(), "Provisioning.UpdatedAt")
	assert.Equal(t, roles, info.Permissions.FleetNodeRoles, "Permissions.FleetNodeRoles")
	assert.Equal(t, map[string]string{"team": "core"}, info.Metadata, "Metadata")
	assert.False(t, info.CreatedAt.IsZero(), "CreatedAt")
	assert.False(t, info.UpdatedAt.IsZero(), "UpdatedAt")
}

// TestProjectTransportParity_CreateGetGetByNameUpdate runs the four verbs on
// every transport and asserts each returns a populated ProjectInfo.
func TestProjectTransportParity_CreateGetGetByNameUpdate(t *testing.T) {
	svc := projectParityService(t)
	ctx := context.Background()

	for name, tr := range projectParityTransports(t, svc) {
		t.Run(name, func(t *testing.T) {
			client := project.NewClient(tr)
			projectName := "parity-" + name

			created, err := client.Create(ctx, &project.CreateProjectRequest{
				TenantID:         parityProjectTenant,
				Name:             projectName,
				Description:      "created over " + name,
				WorkspaceManaged: true,
				Metadata:         map[string]string{"team": "core"},
			})
			require.NoError(t, err, "Create")
			assertPopulatedProject(t, created, projectName, "created over "+name, nil)

			got, err := client.Get(ctx, parityProjectTenant, created.ID)
			require.NoError(t, err, "Get")
			assertPopulatedProject(t, got, projectName, "created over "+name, nil)
			assert.Equal(t, created.ID, got.ID)

			byName, err := client.GetByName(ctx, parityProjectTenant, projectName)
			require.NoError(t, err, "GetByName")
			assertPopulatedProject(t, byName, projectName, "created over "+name, nil)
			assert.Equal(t, created.ID, byName.ID)

			updated, err := client.Update(ctx, &project.UpdateProjectRequest{
				TenantID:       parityProjectTenant,
				ProjectID:      created.ID,
				Description:    "updated over " + name,
				FleetNodeRoles: parityRoles,
			})
			require.NoError(t, err, "Update")
			assertPopulatedProject(t, updated, projectName, "updated over "+name, parityRoles)
			assert.Equal(t, created.ID, updated.ID)
		})
	}
}

// TestProjectTransportParity_EveryTransportRendersOneRecordAlike reads one
// project through every transport and requires the same ProjectInfo.
func TestProjectTransportParity_EveryTransportRendersOneRecordAlike(t *testing.T) {
	svc := projectParityService(t)
	ctx := context.Background()
	resp := createReferenceProject(t, svc)

	var want *project.ProjectInfo
	var wantFrom string
	for name, tr := range projectParityTransports(t, svc) {
		got, err := project.NewClient(tr).Get(ctx, parityProjectTenant, resp.Project.ID)
		require.NoError(t, err, "%s Get", name)
		assertPopulatedProject(t, got, "reference", "one record, every transport", parityRoles)
		got = normalizeProject(got)
		if want == nil {
			want, wantFrom = got, name
			continue
		}
		assert.Equal(t, want, got, "%s renders the record differently from %s", name, wantFrom)
	}
}

// createReferenceProject creates, straight through the service, the one
// project the rendering tests read back over every transport.
func createReferenceProject(t *testing.T, svc service.ProjectService) *service.CreateProjectResponse {
	t.Helper()

	resp, err := svc.CreateProject(context.Background(), &service.CreateProjectRequest{
		TenantID:         parityProjectTenant,
		Name:             "reference",
		Description:      "one record, every transport",
		WorkspaceManaged: true,
		FleetNodeRoles:   map[string]orchestration.ProjectRole{"node-1": orchestration.ProjectRoleLead},
		Metadata:         map[string]string{"team": "core"},
	})
	require.NoError(t, err)
	return resp
}

// normalizeProject drops the precision gRPC does not carry: it sends times
// as unix seconds, so times are compared at that precision.
func normalizeProject(info *project.ProjectInfo) *project.ProjectInfo {
	info.CreatedAt = info.CreatedAt.Truncate(time.Second).UTC()
	info.UpdatedAt = info.UpdatedAt.Truncate(time.Second).UTC()
	info.Provisioning.UpdatedAt = info.Provisioning.UpdatedAt.Truncate(time.Second).UTC()
	return info
}

// TestProjectTransportParity_EveryTransportListsOneRecordAlike lists a tenant
// holding one project through every transport and requires the same page.
func TestProjectTransportParity_EveryTransportListsOneRecordAlike(t *testing.T) {
	svc := projectParityService(t)
	ctx := context.Background()
	resp := createReferenceProject(t, svc)

	var want *project.ListProjectsResponse
	var wantFrom string
	for name, tr := range projectParityTransports(t, svc) {
		got, err := project.NewClient(tr).List(ctx, &project.ListProjectsRequest{TenantID: parityProjectTenant})
		require.NoError(t, err, "%s List", name)
		require.Len(t, got.Projects, 1, "%s List", name)
		assert.Equal(t, resp.Project.ID, got.Projects[0].ID, "%s List", name)
		assertPopulatedProject(t, got.Projects[0], "reference", "one record, every transport", parityRoles)
		normalizeProject(got.Projects[0])
		if want == nil {
			want, wantFrom = got, name
			continue
		}
		assert.Equal(t, want, got, "%s lists the tenant differently from %s", name, wantFrom)
	}
}

// TestProjectTransportParity_Delete deletes a project over every transport
// and requires each to report it gone the same way: a later Get, and a
// second Delete, answer not-found.
func TestProjectTransportParity_Delete(t *testing.T) {
	svc := projectParityService(t)
	ctx := context.Background()

	for name, tr := range projectParityTransports(t, svc) {
		t.Run(name, func(t *testing.T) {
			client := project.NewClient(tr)
			created, err := client.Create(ctx, &project.CreateProjectRequest{
				TenantID: parityProjectTenant, Name: "delete-" + name, Metadata: map[string]string{"team": "core"},
			})
			require.NoError(t, err, "Create")

			require.NoError(t, client.Delete(ctx, parityProjectTenant, created.ID), "Delete")

			_, err = client.Get(ctx, parityProjectTenant, created.ID)
			assert.True(t, sdkerrors.IsNotFound(err), "Get after Delete: %v", err)
			err = client.Delete(ctx, parityProjectTenant, created.ID)
			assert.True(t, sdkerrors.IsNotFound(err), "second Delete: %v", err)
		})
	}
}
