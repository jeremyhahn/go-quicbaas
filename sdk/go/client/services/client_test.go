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

package services

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeTransport records each call and answers with reply.
type fakeTransport struct {
	calls []string
	reqs  []interface{}
	reply interface{}
	err   error
}

func (f *fakeTransport) Request(_ context.Context, service, method string, req interface{}) (interface{}, error) {
	f.calls = append(f.calls, service+"/"+method)
	f.reqs = append(f.reqs, req)
	return f.reply, f.err
}

func (f *fakeTransport) RequestStream(context.Context, string, string, interface{}) (transport.Stream, error) {
	return nil, nil
}
func (f *fakeTransport) Conn() interface{} { return nil }
func (f *fakeTransport) Close() error      { return nil }
func (f *fakeTransport) Healthy() bool     { return true }

var signed = &SignedManifest{Payload: []byte("p"), KeyID: "k", Signature: []byte("s")}

func TestClient_VerbsNameTheirMethodsAndRequests(t *testing.T) {
	ctx := context.Background()
	ft := &fakeTransport{reply: &Record{Name: "web", State: "running"}}
	c := NewClient(ft)
	if rec, err := c.Deploy(ctx, signed); err != nil || rec.Name != "web" {
		t.Fatalf("deploy = %+v, %v", rec, err)
	}
	if _, err := c.Redeploy(ctx, signed); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []func(context.Context, string) (*Record, error){c.Start, c.Stop, c.Resume, c.Restart, c.Get} {
		if _, err := verb(ctx, "web"); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Undeploy(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	want := []string{"Deploy", "Redeploy", "Start", "Stop", "Resume", "Restart", "Get", "Undeploy"}
	for i, m := range want {
		if ft.calls[i] != ServiceName+"/"+m {
			t.Fatalf("call %d = %s, want %s", i, ft.calls[i], m)
		}
	}
	if r := ft.reqs[0].(*DeployRequest); r.Manifest != signed {
		t.Fatalf("deploy request = %+v", r)
	}
	if r := ft.reqs[2].(*NameRequest); r.Name != "web" {
		t.Fatalf("start request = %+v", r)
	}

	ft.reply = &ListResponse{Services: []Record{{Name: "web"}}}
	if list, err := c.List(ctx); err != nil || len(list.Services) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestClient_RefusesBeforeTheWireAndChecksReplies(t *testing.T) {
	ctx := context.Background()
	ft := &fakeTransport{reply: &Record{}}
	c := NewClient(ft)
	for _, bad := range []*SignedManifest{nil, {KeyID: "k", Signature: []byte("s")}, {Payload: []byte("p"), Signature: []byte("s")}, {Payload: []byte("p"), KeyID: "k"}} {
		if _, err := c.Deploy(ctx, bad); !errors.IsInvalidInput(err) {
			t.Fatalf("deploy %+v = %v", bad, err)
		}
	}
	if _, err := c.Start(ctx, ""); !errors.IsInvalidInput(err) {
		t.Fatalf("start without a name = %v", err)
	}
	if err := c.Undeploy(ctx, ""); !errors.IsInvalidInput(err) {
		t.Fatalf("undeploy without a name = %v", err)
	}
	if len(ft.calls) != 0 {
		t.Fatalf("refused calls reached the transport: %v", ft.calls)
	}
	ft.reply = "wrong"
	if _, err := c.Get(ctx, "web"); errors.Code(err) != errors.CodeInternal {
		t.Fatalf("wrong reply = %v", err)
	}
	if _, err := c.List(ctx); errors.Code(err) != errors.CodeInternal {
		t.Fatalf("wrong list reply = %v", err)
	}
	ft.err = errors.NewNotFound("x", "gone")
	if _, err := c.Get(ctx, "web"); !errors.IsNotFound(err) {
		t.Fatalf("transport error = %v", err)
	}
}
