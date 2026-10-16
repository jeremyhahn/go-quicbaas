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

package prompt

import (
	"context"
	"encoding/json"
	stderrors "errors"
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
func (f *fakeTransport) Conn() interface{}     { return nil }
func (f *fakeTransport) Close() error          { return nil }
func (f *fakeTransport) Healthy() bool         { return true }
func (f *fakeTransport) SupportsPrompts() bool { return f.capable }

// plainTransport declares no service at all.
type plainTransport struct{ fakeTransport }

func (plainTransport) SupportsPrompts() {}

func charter() Address { return Address{Set: "fixture", Name: "charter"} }

func isInternal(err error) bool {
	var qb *sdkerrors.QBError
	return stderrors.As(err, &qb) && qb.Code == sdkerrors.CodeInternal
}

func TestNewClient_NilTransportIsNil(t *testing.T) {
	if NewClient(nil) != nil {
		t.Fatal("a nil transport must give a nil client")
	}
	if NewClient(&fakeTransport{}) == nil {
		t.Fatal("a transport must give a client")
	}
}

func TestClient_RoutesEveryVerbUnderTheService(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)
	addr := charter()

	expect := func(method string) {
		t.Helper()
		if tr.service != serviceName || tr.method != method {
			t.Fatalf("sent %s/%s, want %s/%s", tr.service, tr.method, serviceName, method)
		}
	}

	tr.resp = &ListResponse{Set: "fixture", Prompts: []ListEntry{{Name: "charter"}}}
	list, err := c.List(ctx, &ListRequest{Set: "fixture", Scope: "project", ProjectID: "p1"})
	if err != nil || len(list.Prompts) != 1 {
		t.Fatalf("List: %+v %v", list, err)
	}
	expect("List")
	if r := tr.req.(*ListRequest); r.ProjectID != "p1" || r.Scope != "project" {
		t.Fatalf("ListRequest = %+v", r)
	}

	tr.resp = &Record{Name: "charter", Version: 2}
	got, err := c.Get(ctx, &GetRequest{Address: addr, Version: 2})
	if err != nil || got.Version != 2 {
		t.Fatalf("Get: %+v %v", got, err)
	}
	expect("Get")

	tr.resp = &EffectivePrompt{Content: "body"}
	effective, err := c.Effective(ctx, &addr)
	if err != nil || effective.Content != "body" {
		t.Fatalf("Effective: %+v %v", effective, err)
	}
	expect("Effective")

	tr.resp = &Diff{Same: true}
	diff, err := c.Diff(ctx, &addr)
	if err != nil || !diff.Same {
		t.Fatalf("Diff: %+v %v", diff, err)
	}
	expect("Diff")

	tr.resp = &History{Revisions: []RevisionSummary{{Version: 1}}}
	history, err := c.History(ctx, &addr)
	if err != nil || len(history.Revisions) != 1 {
		t.Fatalf("History: %+v %v", history, err)
	}
	expect("History")

	tr.resp = &Record{Version: 1}
	if _, err := c.Put(ctx, &PutRequest{Address: addr, Content: "body", Note: "why"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	expect("Put")
	if r := tr.req.(*PutRequest); r.Content != "body" || r.Note != "why" {
		t.Fatalf("PutRequest = %+v", r)
	}

	if _, err := c.Rollback(ctx, &RollbackRequest{Address: addr, ToVersion: 1, ExpectedVersion: 2}); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	expect("Rollback")

	if _, err := c.Reset(ctx, &ResetRequest{Address: addr, ExpectedVersion: 3}); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	expect("Reset")
}

// TestRequests_EncodeAsTheToolArguments pins the JSON the requests carry:
// the address is flat, so it is the argument object the prompt_* tools take.
func TestRequests_EncodeAsTheToolArguments(t *testing.T) {
	raw, err := json.Marshal(&RollbackRequest{
		Address:   Address{TenantID: "t", Scope: "project", ProjectID: "p1", Set: "s", Name: "n"},
		ToVersion: 1, Note: "why", ExpectedVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]interface{}
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"tenant_id": "t", "scope": "project", "project_id": "p1", "set": "s", "name": "n",
		"to_version": float64(1), "note": "why", "expected_version": float64(2),
	}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for k, v := range want {
		if args[k] != v {
			t.Fatalf("%s = %v, want %v", k, args[k], v)
		}
	}
}

func TestClient_RefusesBadInput(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)
	addr := charter()
	noName := Address{Set: "fixture"}
	var nilCtx context.Context

	cases := map[string]error{}
	_, cases["nil ctx"] = c.List(nilCtx, &ListRequest{Set: "fixture"})
	_, cases["nil list"] = c.List(ctx, nil)
	_, cases["list without set"] = c.List(ctx, &ListRequest{})
	_, cases["nil get"] = c.Get(ctx, nil)
	_, cases["get without name"] = c.Get(ctx, &GetRequest{Address: noName})
	_, cases["get negative version"] = c.Get(ctx, &GetRequest{Address: addr, Version: -1})
	_, cases["nil effective"] = c.Effective(ctx, nil)
	_, cases["effective without set"] = c.Effective(ctx, &Address{Name: "charter"})
	_, cases["diff without name"] = c.Diff(ctx, &noName)
	_, cases["history without name"] = c.History(ctx, &noName)
	_, cases["nil put"] = c.Put(ctx, nil)
	_, cases["put without name"] = c.Put(ctx, &PutRequest{Address: noName, Content: "b", Note: "n"})
	_, cases["put without content"] = c.Put(ctx, &PutRequest{Address: addr, Note: "n"})
	_, cases["put without note"] = c.Put(ctx, &PutRequest{Address: addr, Content: "b"})
	_, cases["put negative version"] = c.Put(ctx, &PutRequest{Address: addr, Content: "b", Note: "n", ExpectedVersion: -1})
	_, cases["nil rollback"] = c.Rollback(ctx, nil)
	_, cases["rollback without name"] = c.Rollback(ctx, &RollbackRequest{Address: noName, ToVersion: 1, ExpectedVersion: 1})
	_, cases["rollback to 0"] = c.Rollback(ctx, &RollbackRequest{Address: addr, ExpectedVersion: 1})
	_, cases["rollback expecting 0"] = c.Rollback(ctx, &RollbackRequest{Address: addr, ToVersion: 1})
	_, cases["nil reset"] = c.Reset(ctx, nil)
	_, cases["reset without name"] = c.Reset(ctx, &ResetRequest{Address: noName, ExpectedVersion: 1})
	_, cases["reset expecting 0"] = c.Reset(ctx, &ResetRequest{Address: addr})

	for name, err := range cases {
		if !sdkerrors.IsInvalidInput(err) {
			t.Errorf("%s: %v, want InvalidInput", name, err)
		}
	}
	if tr.method != "" {
		t.Fatalf("a refused call reached the transport: %s", tr.method)
	}
}

func TestClient_RefusesBadAnswersAndPassesErrorsThrough(t *testing.T) {
	ctx := context.Background()
	addr := charter()

	wrong := &fakeTransport{capable: true, resp: &History{}}
	if _, err := NewClient(wrong).Get(ctx, &GetRequest{Address: addr}); !isInternal(err) {
		t.Fatalf("wrong answer type: %v", err)
	}
	empty := &fakeTransport{capable: true}
	if _, err := NewClient(empty).Effective(ctx, &addr); !isInternal(err) {
		t.Fatalf("no answer: %v", err)
	}

	backend := sdkerrors.NewNotFound("prompt.Get", "nothing is overridden")
	failing := &fakeTransport{capable: true, err: backend}
	if _, err := NewClient(failing).Get(ctx, &GetRequest{Address: addr}); !stderrors.Is(err, backend) {
		t.Fatalf("a transport error must pass through: %v", err)
	}
}

func TestClient_FailsClosedOnAnUnwiredTransport(t *testing.T) {
	ctx := context.Background()
	addr := charter()
	for name, tr := range map[string]transport.Transport{
		"declines":   &fakeTransport{capable: false},
		"undeclared": &plainTransport{},
	} {
		c := NewClient(tr)
		_, err := c.Effective(ctx, &addr)
		var refusal *sdkerrors.PromptTransportUnsupportedError
		if !stderrors.As(err, &refusal) || !sdkerrors.IsUnimplemented(err) {
			t.Fatalf("%s: %v, want the named refusal", name, err)
		}
		if refusal.Transport == "" {
			t.Fatalf("%s: the refusal must name the transport", name)
		}
	}
}
