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

package grpc

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	servicesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
)

// startServicesServer serves the real ServicesService over a real plane on an
// in-memory listener, every call as tenant acme.
func startServicesServer(t *testing.T) (*grpc.ClientConn, *serviceplanetest.Runtime) {
	t.Helper()
	rt := serviceplanetest.NewRuntime(t)
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		return h(tenantctx.WithTenant(ctx, "acme"), req)
	}))
	pb.RegisterServicesServiceServer(server, grpcapi.NewServicesServer(service.NewServicesService(rt.Resolver(t))))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, rt
}

func signedRequest(t *testing.T, rt *serviceplanetest.Runtime, tenant, name string) *servicesclient.DeployRequest {
	sm := rt.Sign(t, tenant, name)
	return &servicesclient.DeployRequest{Manifest: &servicesclient.SignedManifest{Payload: sm.Payload, KeyID: sm.KeyID, Signature: sm.Signature}}
}

func callServices(t *testing.T, conn *grpc.ClientConn, method string, req interface{}) (*servicesclient.Record, error) {
	t.Helper()
	out, err := handleServicesRequest(context.Background(), conn, method, req)
	if err != nil {
		return nil, err
	}
	return out.(*servicesclient.Record), nil
}

func TestGRPCServices_TheSevenVerbs(t *testing.T) {
	conn, rt := startServicesServer(t)
	rec, err := callServices(t, conn, "Deploy", signedRequest(t, rt, "acme", "web"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ Name string }
	if err := json.Unmarshal(rec.Manifest, &m); err != nil || m.Name != "web" {
		t.Fatalf("manifest %s, %v", rec.Manifest, err)
	}
	if rec.State != "running" || rec.Grants["certificate"] != "certificate:acme/web" || rec.UpdatedAt.IsZero() {
		t.Fatalf("deployed = %+v", rec)
	}
	name := &servicesclient.NameRequest{Name: "web"}
	for _, step := range []struct{ method, state string }{
		{"Stop", "stopped"}, {"Start", "running"}, {"Restart", "running"}, {"Get", "running"},
	} {
		if rec, err = callServices(t, conn, step.method, name); err != nil || rec.State != step.state {
			t.Fatalf("%s -> %+v, %v", step.method, rec, err)
		}
	}
	if rec, err = callServices(t, conn, "Redeploy", signedRequest(t, rt, "acme", "web")); err != nil || rec.Revision != 2 {
		t.Fatalf("redeploy = %+v, %v", rec, err)
	}
	list, err := handleServicesRequest(context.Background(), conn, "List", &servicesclient.ListRequest{})
	if err != nil || len(list.(*servicesclient.ListResponse).Services) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := handleServicesRequest(context.Background(), conn, "Undeploy", name); err != nil {
		t.Fatal(err)
	}
	if _, err := callServices(t, conn, "Get", name); err == nil {
		t.Fatal("get after undeploy succeeded")
	}
}

func TestGRPCServices_ResumeAndRefusals(t *testing.T) {
	conn, rt := startServicesServer(t)
	rt.Launcher.Fail(stderrors.New("image pull failed"))
	if _, err := callServices(t, conn, "Deploy", signedRequest(t, rt, "acme", "web")); err == nil {
		t.Fatal("a failed launch deployed")
	}
	rt.Launcher.Fail(nil)
	if rec, err := callServices(t, conn, "Resume", &servicesclient.NameRequest{Name: "web"}); err != nil || rec.State != "running" {
		t.Fatalf("resume = %+v, %v", rec, err)
	}
	if _, err := callServices(t, conn, "Deploy", signedRequest(t, rt, "other", "api")); err == nil {
		t.Fatal("deployed another tenant's manifest")
	}
	if _, err := callServices(t, conn, "Deploy", &servicesclient.NameRequest{}); err == nil {
		t.Fatal("a name request deployed")
	}
	if _, err := handleServicesRequest(context.Background(), conn, "Watch", nil); err == nil {
		t.Fatal("an unknown method was served")
	}
	if !isServicesService(servicesclient.ServiceName) || isServicesService("quicbaas.Registry") {
		t.Fatal("isServicesService")
	}
}
