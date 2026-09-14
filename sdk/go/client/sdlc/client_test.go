// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package sdlc

import (
	"context"
	"errors"
	"testing"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeTransport records the last request and answers what it was given.
type fakeTransport struct {
	capable bool
	service string
	method  string
	req     interface{}
	resp    interface{}
	err     error
}

func (f *fakeTransport) Request(_ context.Context, service, method string, req interface{}) (interface{}, error) {
	f.service, f.method, f.req = service, method, req
	return f.resp, f.err
}

func (f *fakeTransport) RequestStream(context.Context, string, string, interface{}) (transport.Stream, error) {
	return nil, nil
}
func (f *fakeTransport) Conn() interface{}  { return nil }
func (f *fakeTransport) Close() error       { return nil }
func (f *fakeTransport) Healthy() bool      { return true }
func (f *fakeTransport) SupportsSDLC() bool { return f.capable }

func TestClient_RoutesEveryVerbUnderTheService(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)

	tr.resp = &ProjectBinding{ProjectID: "p1"}
	bound, err := c.Bind(ctx, &BindRequest{TenantID: "t", ProjectID: "p1", Binding: ProjectBinding{Forge: ForgeConfig{Type: "gitea"}}})
	if err != nil || bound.ProjectID != "p1" || tr.service != serviceName || tr.method != "Bind" {
		t.Fatalf("Bind: %+v %v (%s/%s)", bound, err, tr.service, tr.method)
	}
	if got := tr.req.(*BindRequest); got.Binding.ProjectID != "p1" {
		t.Fatal("the binding's project id must be filled from the request")
	}

	got, err := c.Binding(ctx, "t", "p1")
	if err != nil || got.ProjectID != "p1" || tr.method != "Binding" {
		t.Fatalf("Binding: %+v %v", got, err)
	}
	if r := tr.req.(*GetBindingRequest); r.TenantID != "t" || r.ProjectID != "p1" {
		t.Fatalf("GetBindingRequest = %+v", r)
	}

	tr.resp = &UnbindResponse{Unbound: true}
	if err := c.Unbind(ctx, "t", "p1"); err != nil || tr.method != "Unbind" {
		t.Fatalf("Unbind: %v", err)
	}

	tr.resp = &Delivery{TaskID: "task-1"}
	d, err := c.Delivery(ctx, "t", "task-1")
	if err != nil || d.TaskID != "task-1" || tr.method != "Delivery" {
		t.Fatalf("Delivery: %+v %v", d, err)
	}
	if r := tr.req.(*GetDeliveryRequest); r.TaskID != "task-1" {
		t.Fatalf("GetDeliveryRequest = %+v", r)
	}

	// Forges is build capability, not tenant data: the request carries no
	// field and the verb takes no tenant argument (go-quicbaas#606).
	tr.resp = &ListForgesResponse{Forges: []string{"gitea"}, Count: 1}
	forges, err := c.ListForges(ctx)
	if err != nil || tr.method != "ListForges" || tr.service != serviceName {
		t.Fatalf("ListForges: %v (%s/%s)", err, tr.service, tr.method)
	}
	if len(forges.Forges) != 1 || forges.Forges[0] != "gitea" || forges.Count != 1 {
		t.Fatalf("ListForgesResponse = %+v", forges)
	}
	if _, ok := tr.req.(*ListForgesRequest); !ok {
		t.Fatalf("ListForges sent %T, want *ListForgesRequest", tr.req)
	}
}

func TestClient_RefusesBadInputAndBadAnswers(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)
	if _, err := c.Bind(ctx, nil); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("nil bind: %v", err)
	}
	if _, err := c.Bind(ctx, &BindRequest{ProjectID: "p1", Binding: ProjectBinding{ProjectID: "other"}}); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("mismatched ids: %v", err)
	}
	if _, err := c.Binding(ctx, "t", ""); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("empty project: %v", err)
	}
	if err := c.Unbind(ctx, "t", ""); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("empty unbind: %v", err)
	}
	if _, err := c.Delivery(ctx, "t", ""); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("empty task: %v", err)
	}
	var nilCtx context.Context
	if _, err := c.Binding(nilCtx, "t", "p1"); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("nil ctx: %v", err)
	}
	if _, err := c.ListForges(nilCtx); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("nil ctx on forges: %v", err)
	}
	tr.resp = "not a binding"
	if _, err := c.Binding(ctx, "t", "p1"); err == nil {
		t.Fatal("wrong response type accepted")
	}
	if _, err := c.ListForges(ctx); err == nil {
		t.Fatal("wrong forges type accepted")
	}
	if _, err := c.Delivery(ctx, "t", "task"); err == nil {
		t.Fatal("wrong delivery type accepted")
	}
	tr.resp = nil
	tr.err = errors.New("boom")
	if _, err := c.Binding(ctx, "t", "p1"); err == nil {
		t.Fatal("transport error swallowed")
	}
	if err := c.Unbind(ctx, "t", "p1"); err == nil {
		t.Fatal("transport error swallowed on unbind")
	}
	if NewClient(nil) != nil {
		t.Fatal("nil transport must yield a nil client")
	}
}

func TestClient_FailsClosedOnATransportWithoutTheService(t *testing.T) {
	c := NewClient(&fakeTransport{capable: false})
	_, err := c.Binding(context.Background(), "t", "p1")
	var unsupported *sdkerrors.SDLCTransportUnsupportedError
	if !errors.As(err, &unsupported) || !sdkerrors.IsUnimplemented(err) {
		t.Fatalf("declared-false transport: %v", err)
	}
	if _, err := c.Bind(context.Background(), &BindRequest{ProjectID: "p"}); !errors.As(err, &unsupported) {
		t.Fatal("bind on an incapable transport")
	}
	if err := c.Unbind(context.Background(), "t", "p"); !errors.As(err, &unsupported) {
		t.Fatal("unbind on an incapable transport")
	}
	if _, err := c.Delivery(context.Background(), "t", "x"); !errors.As(err, &unsupported) {
		t.Fatal("delivery on an incapable transport")
	}
	if _, err := c.ListForges(context.Background()); !errors.As(err, &unsupported) {
		t.Fatal("forges on an incapable transport")
	}
}
