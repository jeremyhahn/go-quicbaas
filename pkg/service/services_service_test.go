// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	stderrors "errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

type planeGuard struct {
	mu sync.Mutex
	kv *kvstore.Store
}

func (g *planeGuard) Create(ctx context.Context, key string, value []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ok, err := g.kv.Exists(ctx, key); err != nil || ok {
		return false, err
	}
	return true, g.kv.Put(ctx, key, value)
}

func (g *planeGuard) Swap(ctx context.Context, key string, held, next []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.kv.CompareAndSwap(ctx, key, held, next)
}

type planeLauncher struct{ err error }

func (l *planeLauncher) Launch(context.Context, *serviceplane.Service) error { return l.err }
func (l *planeLauncher) Halt(context.Context, *serviceplane.Service) error   { return nil }

type planeGrant struct{ kind serviceplane.GrantKind }

func (g planeGrant) Kind() serviceplane.GrantKind { return g.kind }
func (g planeGrant) Grant(_ context.Context, m *serviceplane.Manifest, _ string) (string, error) {
	return string(g.kind) + ":" + m.Tenant + "/" + m.Name, nil
}
func (g planeGrant) Revoke(context.Context, *serviceplane.Manifest, string) error { return nil }

type servicesHarness struct {
	svc      ServicesService
	launcher *planeLauncher
	key      ed25519.PrivateKey
}

func newServicesHarness(t *testing.T) *servicesHarness {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kv := kvstore.NewMemory()
	store, err := serviceplane.NewStore(&planeGuard{kv: kv}, kv)
	if err != nil {
		t.Fatal(err)
	}
	h := &servicesHarness{launcher: &planeLauncher{}, key: key}
	var grants []serviceplane.Grant
	for _, k := range serviceplane.GrantKinds {
		grants = append(grants, planeGrant{kind: k})
	}
	plane, err := serviceplane.NewPlane(serviceplane.Config{
		Store: store, Verifier: serviceplane.Keyring{"k1": pub}, Launcher: h.launcher, Grants: grants,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = NewServicesService(func(context.Context) (*serviceplane.Plane, error) { return plane, nil })
	return h
}

func (h *servicesHarness) signed(t *testing.T, tenant, name string) *DeployServiceRequest {
	t.Helper()
	sm, err := serviceplane.Sign(&serviceplane.Manifest{
		Tenant: tenant,
		Name:   name,
		Image: serviceplane.Image{
			Reference:  "registry.acme.internal/" + name,
			Digest:     "sha256:" + strings.Repeat("ab", 32),
			Provenance: "slsa://registry.acme.internal/" + name,
		},
		Policy:    serviceplane.PolicyRefs{Sandbox: "default", Mount: "ro-root", Network: "egress-none"},
		Resources: serviceplane.Resources{MilliCPU: 250, MemoryBytes: 64 << 20},
		Role:      "operator",
	}, "k1", h.key)
	if err != nil {
		t.Fatal(err)
	}
	return &DeployServiceRequest{Manifest: sm}
}

func in(tenant string) context.Context {
	return tenantctx.WithTenant(context.Background(), tenant)
}

func wantCode(t *testing.T, err error, code errors.ErrorCode) {
	t.Helper()
	if got := errors.Code(err); got != code {
		t.Fatalf("error %v has code %v, want %v", err, got, code)
	}
}

func TestServicesService_TheSevenVerbsInTheCallersTenant(t *testing.T) {
	h := newServicesHarness(t)
	ctx := in("acme")
	rec, err := h.svc.Deploy(ctx, h.signed(t, "acme", "web"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != "running" || rec.KeyID != "k1" || rec.Manifest.Image.Reference != "registry.acme.internal/web" ||
		rec.Grants["identity"] != "identity:acme/web" || len(rec.Grants) != 5 {
		t.Fatalf("deployed = %+v", rec)
	}
	name := &ServiceNameRequest{Name: "web"}
	for _, step := range []struct {
		verb  func(context.Context, *ServiceNameRequest) (*ServiceRecord, error)
		state string
	}{{h.svc.Stop, "stopped"}, {h.svc.Start, "running"}, {h.svc.Restart, "running"}} {
		if rec, err = step.verb(ctx, name); err != nil || rec.State != step.state {
			t.Fatalf("verb -> %+v, %v, want %s", rec, err, step.state)
		}
	}
	if rec, err = h.svc.Redeploy(ctx, h.signed(t, "acme", "web")); err != nil || rec.Revision != 2 {
		t.Fatalf("redeploy = %+v, %v", rec, err)
	}
	list, err := h.svc.List(ctx)
	if err != nil || len(list.Services) != 1 || list.Services[0].Restarts != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := h.svc.Undeploy(ctx, name); err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.Get(ctx, name)
	wantCode(t, err, errors.ErrNotFound)
}

func TestServicesService_ResumeAfterAFailedLaunch(t *testing.T) {
	h := newServicesHarness(t)
	ctx := in("acme")
	h.launcher.err = stderrors.New("image pull failed")
	_, err := h.svc.Deploy(ctx, h.signed(t, "acme", "web"))
	wantCode(t, err, errors.ErrUnavailable)
	h.launcher.err = nil
	rec, err := h.svc.Resume(ctx, &ServiceNameRequest{Name: "web"})
	if err != nil || rec.State != "running" {
		t.Fatalf("resume = %+v, %v", rec, err)
	}
}

func TestServicesService_RefusesAnotherTenant(t *testing.T) {
	h := newServicesHarness(t)
	if _, err := h.svc.Deploy(in("acme"), h.signed(t, "acme", "web")); err != nil {
		t.Fatal(err)
	}
	_, err := h.svc.Deploy(in("acme"), h.signed(t, "other", "api"))
	wantCode(t, err, errors.ErrForbidden)
	_, err = h.svc.Get(in("other"), &ServiceNameRequest{Name: "web"})
	wantCode(t, err, errors.ErrNotFound)
	_, err = h.svc.List(tenantctx.WithSystem(context.Background()))
	wantCode(t, err, errors.ErrForbidden)
	_, err = h.svc.List(context.Background())
	if err == nil {
		t.Fatal("a caller with no tenant listed services")
	}
}

func TestServicesService_CodesThePlanesRefusals(t *testing.T) {
	h := newServicesHarness(t)
	ctx := in("acme")
	req := h.signed(t, "acme", "web")
	if _, err := h.svc.Deploy(ctx, req); err != nil {
		t.Fatal(err)
	}
	_, err := h.svc.Deploy(ctx, req)
	wantCode(t, err, errors.ErrAlreadyExists)
	_, err = h.svc.Start(ctx, &ServiceNameRequest{Name: "web"})
	wantCode(t, err, errors.ErrPreconditionFailed)
	tampered := *req.Manifest
	tampered.Signature = append([]byte{}, req.Manifest.Signature...)
	tampered.Signature[0] ^= 0xff
	_, err = h.svc.Redeploy(ctx, &DeployServiceRequest{Manifest: &tampered})
	wantCode(t, err, errors.ErrForbidden)
	_, err = h.svc.Deploy(ctx, &DeployServiceRequest{})
	wantCode(t, err, errors.ErrInvalidInput)
	_, err = h.svc.Stop(ctx, nil)
	wantCode(t, err, errors.ErrInvalidInput)
}

func TestServicesService_NotConfigured(t *testing.T) {
	svc := NewServicesService(nil)
	_, err := svc.List(in("acme"))
	wantCode(t, err, errors.ErrPreconditionFailed)
	if !stderrors.Is(err, ErrServicePlaneNotConfigured) {
		t.Fatalf("error = %v", err)
	}
	failing := NewServicesService(func(context.Context) (*serviceplane.Plane, error) {
		return nil, stderrors.New("platform shard down")
	})
	_, err = failing.Get(in("acme"), &ServiceNameRequest{Name: "web"})
	wantCode(t, err, errors.ErrUnavailable)
}
