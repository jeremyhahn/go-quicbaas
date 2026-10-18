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

package unix

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	serveriam "github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestTransport_TenantDisplayNameRoundTripOverUnixSocket updates a tenant's
// display name over a real Unix socket to the real gRPC IAM server and reads
// it back: it is stored in settings, and Name and other settings stay (#1518).
func TestTransport_TenantDisplayNameRoundTripOverUnixSocket(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	mem := serveriam.NewMemoryStorage()
	tenants := serveriam.NewDefaultTenantManager(mem)
	if _, err := tenants.CreateTenant(tenantctx.WithTenant(context.Background(), "acme"), serveriam.CreateTenantOptions{
		ID:       "acme",
		Name:     "Acme",
		Settings: &serveriam.TenantSettings{MaxUsers: 42},
	}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	store := serveriam.NewIdentityStore(serveriam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem, Tenants: tenants})
	adapter := grpcapi.NewIAMServiceAdapter(tenants, store, nil, nil, nil, nil)

	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	bindTenant := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(tenantctx.WithTenant(ctx, "acme"), req)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)), grpc.UnaryInterceptor(bindTenant))
	iampb.RegisterIAMServiceServer(server, grpcapi.NewIAMServer(adapter, log))
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()

	tr, err := New(&transport.Config{Address: socket, Timeout: 5 * time.Second, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := tr.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

	client := iam.NewTenantClient(tr)
	ctx := context.Background()
	if _, err := client.Update(ctx, "acme", &iam.UpdateTenantRequest{DisplayName: "Acme Corp"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := client.Get(ctx, "acme")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != "Acme Corp" {
		t.Errorf("DisplayName = %q, want Acme Corp", got.DisplayName)
	}
	if got.Name != "Acme" {
		t.Errorf("Name = %q, want Acme unchanged", got.Name)
	}
	stored, err := tenants.GetTenant(tenantctx.WithTenant(ctx, "acme"), "acme")
	if err != nil {
		t.Fatalf("GetTenant: %v", err)
	}
	if stored.Settings.DisplayName != "Acme Corp" || stored.Settings.MaxUsers != 42 {
		t.Errorf("stored settings = %+v, want display name set and MaxUsers kept", stored.Settings)
	}

	if _, err := client.Update(ctx, "ghost", &iam.UpdateTenantRequest{DisplayName: "Ghost"}); err == nil {
		t.Error("Update of an unknown tenant must fail")
	}
}
