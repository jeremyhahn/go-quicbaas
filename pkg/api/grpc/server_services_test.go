// Copyright (c) 2026 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package grpc

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func servicesServer(t *testing.T) (*ServicesServer, *serviceplanetest.Runtime) {
	t.Helper()
	rt := serviceplanetest.NewRuntime(t)
	return NewServicesServer(service.NewServicesService(rt.Resolver(t))), rt
}

func signedProto(sm *serviceplane.SignedManifest) *pb.DeployServiceRequest {
	return &pb.DeployServiceRequest{Manifest: &pb.SignedServiceManifest{
		Payload: sm.Payload, KeyId: sm.KeyID, Signature: sm.Signature,
	}}
}

func wantGRPCCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if got := status.Code(err); got != code {
		t.Fatalf("error %v has code %v, want %v", err, got, code)
	}
}

func TestServicesServer_TheSevenVerbs(t *testing.T) {
	srv, rt := servicesServer(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	rec, err := srv.Deploy(ctx, signedProto(rt.Sign(t, "acme", "web")))
	if err != nil {
		t.Fatal(err)
	}
	var m serviceplane.Manifest
	if err := json.Unmarshal(rec.ManifestJson, &m); err != nil {
		t.Fatal(err)
	}
	if rec.State != "running" || rec.Tenant != "acme" || rec.KeyId != serviceplanetest.KeyID ||
		rec.Grants["name"] != "name:acme/web" || m.Name != "web" || rec.UpdatedAtUnixNano == 0 {
		t.Fatalf("deployed = %+v", rec)
	}
	name := &pb.ServiceNameRequest{Name: "web"}
	for _, step := range []struct {
		verb  func(context.Context, *pb.ServiceNameRequest) (*pb.ServiceRecord, error)
		state string
	}{{srv.Stop, "stopped"}, {srv.Start, "running"}, {srv.Restart, "running"}, {srv.Get, "running"}} {
		if rec, err = step.verb(ctx, name); err != nil || rec.State != step.state {
			t.Fatalf("verb -> %+v, %v, want %s", rec, err, step.state)
		}
	}
	if rec, err = srv.Redeploy(ctx, signedProto(rt.Sign(t, "acme", "web"))); err != nil || rec.Revision != 2 {
		t.Fatalf("redeploy = %+v, %v", rec, err)
	}
	list, err := srv.List(ctx, &pb.ListServicesRequest{})
	if err != nil || len(list.Services) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := srv.Undeploy(ctx, name); err != nil {
		t.Fatal(err)
	}
	_, err = srv.Get(ctx, name)
	wantGRPCCode(t, err, codes.NotFound)
}

func TestServicesServer_ResumeAfterAFailedLaunch(t *testing.T) {
	srv, rt := servicesServer(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	rt.Launcher.Fail(stderrors.New("image pull failed"))
	_, err := srv.Deploy(ctx, signedProto(rt.Sign(t, "acme", "web")))
	wantGRPCCode(t, err, codes.Unavailable)
	rt.Launcher.Fail(nil)
	rec, err := srv.Resume(ctx, &pb.ServiceNameRequest{Name: "web"})
	if err != nil || rec.State != "running" {
		t.Fatalf("resume = %+v, %v", rec, err)
	}
}

func TestServicesServer_CodesRefusals(t *testing.T) {
	srv, rt := servicesServer(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	req := signedProto(rt.Sign(t, "acme", "web"))
	if _, err := srv.Deploy(ctx, req); err != nil {
		t.Fatal(err)
	}
	_, err := srv.Deploy(ctx, req)
	wantGRPCCode(t, err, codes.AlreadyExists)
	_, err = srv.Start(ctx, &pb.ServiceNameRequest{Name: "web"})
	wantGRPCCode(t, err, codes.FailedPrecondition)
	_, err = srv.Deploy(ctx, signedProto(rt.Sign(t, "other", "api")))
	wantGRPCCode(t, err, codes.PermissionDenied)
	tampered := signedProto(rt.Sign(t, "acme", "api"))
	tampered.Manifest.Signature[0] ^= 0xff
	_, err = srv.Deploy(ctx, tampered)
	wantGRPCCode(t, err, codes.PermissionDenied)
	_, err = srv.Deploy(ctx, &pb.DeployServiceRequest{})
	wantGRPCCode(t, err, codes.InvalidArgument)
	_, err = srv.Stop(ctx, nil)
	wantGRPCCode(t, err, codes.InvalidArgument)
	_, err = srv.Get(tenantctx.WithTenant(context.Background(), "other"), &pb.ServiceNameRequest{Name: "web"})
	wantGRPCCode(t, err, codes.NotFound)
}

func TestServicesServer_NotConfigured(t *testing.T) {
	_, err := NewServicesServer(nil).List(context.Background(), &pb.ListServicesRequest{})
	wantGRPCCode(t, err, codes.Unavailable)
	_, err = NewServicesServer(service.NewServicesService(nil)).
		List(tenantctx.WithTenant(context.Background(), "acme"), &pb.ListServicesRequest{})
	wantGRPCCode(t, err, codes.FailedPrecondition)
}
