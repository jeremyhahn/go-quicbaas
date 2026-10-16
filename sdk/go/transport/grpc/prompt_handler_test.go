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
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// fakePromptServer records the address of each call and answers fixed
// messages, or fails every call with code when it is set.
type fakePromptServer struct {
	pb.UnimplementedPromptServiceServer
	code     codes.Code
	address  *pb.PromptAddress
	list     *pb.ListPromptsRequest
	put      *pb.PutPromptRequest
	rollback *pb.RollbackPromptRequest
	reset    *pb.ResetPromptRequest
	version  int64
}

const promptUnix = 1_700_000_000

func (f *fakePromptServer) fail() error {
	if f.code != codes.OK {
		return status.Error(f.code, "refused")
	}
	return nil
}

func (f *fakePromptServer) record() *pb.PromptRecord {
	return &pb.PromptRecord{
		TenantId: f.address.GetTenantId(), Set: f.address.GetSet(), Name: f.address.GetName(), Scope: "tenant",
		Version: 2, ContentHash: "h2", Content: "body", UpdatedAtUnix: promptUnix,
	}
}

func (f *fakePromptServer) ListPrompts(_ context.Context, req *pb.ListPromptsRequest) (*pb.ListPromptsResponse, error) {
	f.list = req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.ListPromptsResponse{Set: req.Set, Scope: "tenant", Prompts: []*pb.PromptListEntry{{
		Name: "n", Description: "d", Overridden: true, Version: 2,
		Effective: &pb.PromptVersionRef{Set: req.Set, Name: "n", Scope: "tenant", ScopeId: "t1", Version: 2, Hash: "h2"},
	}}}, nil
}

func (f *fakePromptServer) GetPrompt(_ context.Context, req *pb.GetPromptRequest) (*pb.PromptRecord, error) {
	f.address, f.version = req.Address, req.Version
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.record(), nil
}

func (f *fakePromptServer) GetEffectivePrompt(_ context.Context, req *pb.PromptAddress) (*pb.EffectivePrompt, error) {
	f.address = req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.EffectivePrompt{
		Set: req.Set, Name: req.Name, Scope: "tenant", Content: "body",
		Origin: &pb.PromptVersionRef{Scope: "tenant", Version: 2},
		Chain:  []*pb.PromptLayer{{Scope: "tenant", ScopeId: "t1", Present: true, Version: 2, Hash: "h2", Answered: true}, {Scope: "system"}},
	}, nil
}

func (f *fakePromptServer) DiffPrompt(_ context.Context, req *pb.PromptAddress) (*pb.PromptDiff, error) {
	f.address = req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.PromptDiff{
		Set: req.Set, Name: req.Name, Scope: "tenant",
		Origin: &pb.PromptVersionRef{Scope: "tenant"}, ParentOrigin: &pb.PromptVersionRef{Scope: "embedded"},
		Lines: []*pb.PromptDiffLine{{Op: "equal", Text: "a"}, {Op: "insert", Text: "b"}},
	}, nil
}

func (f *fakePromptServer) GetPromptHistory(_ context.Context, req *pb.PromptAddress) (*pb.PromptHistory, error) {
	f.address = req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &pb.PromptHistory{Set: req.Set, Name: req.Name, Scope: "tenant", Revisions: []*pb.PromptRevisionSummary{
		{Version: 1, ContentHash: "h1", Author: "alice", Note: "why", CreatedAtUnix: promptUnix},
		{Version: 2, FollowParent: true},
	}}, nil
}

func (f *fakePromptServer) PutPrompt(_ context.Context, req *pb.PutPromptRequest) (*pb.PromptRecord, error) {
	f.address, f.put = req.Address, req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.record(), nil
}

func (f *fakePromptServer) RollbackPrompt(_ context.Context, req *pb.RollbackPromptRequest) (*pb.PromptRecord, error) {
	f.address, f.rollback = req.Address, req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.record(), nil
}

func (f *fakePromptServer) ResetPrompt(_ context.Context, req *pb.ResetPromptRequest) (*pb.PromptRecord, error) {
	f.address, f.reset = req.Address, req
	if err := f.fail(); err != nil {
		return nil, err
	}
	return f.record(), nil
}

func newPromptConn(t *testing.T, fake *fakePromptServer) *grpc.ClientConn {
	t.Helper()
	server := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterPromptServiceServer(s, fake) })
	return server.dial(t)
}

func TestPromptHandler_CarriesEveryVerbOverTheWire(t *testing.T) {
	fake := &fakePromptServer{}
	conn := newPromptConn(t, fake)
	ctx := context.Background()
	addr := prompt.Address{TenantID: "t1", Scope: "project", ProjectID: "p1", Set: "s", Name: "n"}
	wantAddr := &pb.PromptAddress{TenantId: "t1", Scope: "project", ProjectId: "p1", Set: "s", Name: "n"}
	sameAddr := func(verb string) {
		t.Helper()
		if fake.address.GetTenantId() != wantAddr.TenantId || fake.address.GetScope() != wantAddr.Scope ||
			fake.address.GetProjectId() != wantAddr.ProjectId || fake.address.GetSet() != wantAddr.Set || fake.address.GetName() != wantAddr.Name {
			t.Fatalf("%s sent address %+v", verb, fake.address)
		}
	}

	resp, err := handlePromptRequest(ctx, conn, "List", &prompt.ListRequest{TenantID: "t1", Scope: "project", ProjectID: "p1", Set: "s"})
	if err != nil {
		t.Fatal(err)
	}
	list := resp.(*prompt.ListResponse)
	if fake.list.GetProjectId() != "p1" || len(list.Prompts) != 1 || list.Prompts[0].Effective.ScopeID != "t1" || !list.Prompts[0].Overridden {
		t.Fatalf("list = %+v (sent %+v)", list, fake.list)
	}

	resp, err = handlePromptRequest(ctx, conn, "Get", &prompt.GetRequest{Address: addr, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	sameAddr("Get")
	record := resp.(*prompt.Record)
	if fake.version != 1 || record.Version != 2 || record.ContentHash != "h2" || record.UpdatedAt.Unix() != promptUnix {
		t.Fatalf("record = %+v", record)
	}

	resp, err = handlePromptRequest(ctx, conn, "Effective", &addr)
	if err != nil {
		t.Fatal(err)
	}
	sameAddr("Effective")
	effective := resp.(*prompt.EffectivePrompt)
	if effective.Content != "body" || len(effective.Chain) != 2 || !effective.Chain[0].Answered || effective.Chain[0].Hash != "h2" {
		t.Fatalf("effective = %+v", effective)
	}

	resp, err = handlePromptRequest(ctx, conn, "Diff", &addr)
	if err != nil {
		t.Fatal(err)
	}
	diff := resp.(*prompt.Diff)
	if diff.ParentOrigin.Scope != "embedded" || len(diff.Lines) != 2 || diff.Lines[1].Op != prompt.DiffInsert {
		t.Fatalf("diff = %+v", diff)
	}

	resp, err = handlePromptRequest(ctx, conn, "History", &addr)
	if err != nil {
		t.Fatal(err)
	}
	history := resp.(*prompt.History)
	if len(history.Revisions) != 2 || history.Revisions[0].Author != "alice" || history.Revisions[0].CreatedAt.Unix() != promptUnix ||
		!history.Revisions[1].CreatedAt.IsZero() || !history.Revisions[1].FollowParent {
		t.Fatalf("history = %+v", history)
	}

	if _, err := handlePromptRequest(ctx, conn, "Put", &prompt.PutRequest{Address: addr, Content: "body", Note: "why", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	sameAddr("Put")
	if fake.put.Content != "body" || fake.put.Note != "why" || fake.put.ExpectedVersion != 1 {
		t.Fatalf("put sent %+v", fake.put)
	}

	if _, err := handlePromptRequest(ctx, conn, "Rollback", &prompt.RollbackRequest{Address: addr, ToVersion: 1, Note: "why", ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if fake.rollback.ToVersion != 1 || fake.rollback.ExpectedVersion != 2 || fake.rollback.Note != "why" {
		t.Fatalf("rollback sent %+v", fake.rollback)
	}

	if _, err := handlePromptRequest(ctx, conn, "Reset", &prompt.ResetRequest{Address: addr, Note: "why", ExpectedVersion: 3}); err != nil {
		t.Fatal(err)
	}
	sameAddr("Reset")
	if fake.reset.ExpectedVersion != 3 || fake.reset.Note != "why" {
		t.Fatalf("reset sent %+v", fake.reset)
	}
}

func TestPromptHandler_MapsServerRefusals(t *testing.T) {
	codesToPredicates := map[codes.Code]func(error) bool{
		codes.InvalidArgument:  errors.IsInvalidInput,
		codes.NotFound:         errors.IsNotFound,
		codes.Aborted:          errors.IsConflict,
		codes.PermissionDenied: errors.IsForbidden,
		codes.Unavailable:      errors.IsUnavailable,
	}
	addr := prompt.Address{Set: "s", Name: "n"}
	requests := map[string]interface{}{
		"List": &prompt.ListRequest{Set: "s"}, "Get": &prompt.GetRequest{Address: addr},
		"Effective": &addr, "Diff": &addr, "History": &addr,
		"Put":      &prompt.PutRequest{Address: addr, Content: "b", Note: "n"},
		"Rollback": &prompt.RollbackRequest{Address: addr, ToVersion: 1, ExpectedVersion: 1},
		"Reset":    &prompt.ResetRequest{Address: addr, ExpectedVersion: 1},
	}
	for code, is := range codesToPredicates {
		conn := newPromptConn(t, &fakePromptServer{code: code})
		for verb, req := range requests {
			if _, err := handlePromptRequest(context.Background(), conn, verb, req); !is(err) {
				t.Errorf("%s answering %s gave %v", verb, code, err)
			}
		}
	}
}

func TestPromptHandler_RefusesUnknownMethodsAndRequestTypes(t *testing.T) {
	conn := newPromptConn(t, &fakePromptServer{})
	ctx := context.Background()
	if _, err := handlePromptRequest(ctx, conn, "Delete", &prompt.Address{}); !errors.IsInvalidInput(err) {
		t.Fatalf("unknown method: %v", err)
	}
	for verb := range promptMethodMap {
		if _, err := handlePromptRequest(ctx, conn, verb, "wrong"); !errors.IsInvalidInput(err) {
			t.Fatalf("%s with a wrong request type: %v", verb, err)
		}
	}
	if !isPromptsService("quicbaas.Prompts") || isPromptsService("quicbaas.SDLC") {
		t.Fatal("service detection")
	}
}

// TestPromptHandler_CarriesEveryVerb, in both directions, against the SDK
// interface read by reflection.
func TestPromptHandler_CarriesEveryVerb(t *testing.T) {
	iface := reflect.TypeOf((*prompt.Client)(nil)).Elem()
	declared := make(map[string]bool, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verb := iface.Method(i).Name
		declared[verb] = true
		if _, ok := promptMethodMap[verb]; !ok {
			t.Errorf("the gRPC transport does not route prompt.%s", verb)
		}
	}
	for method := range promptMethodMap {
		if !declared[method] {
			t.Errorf("the gRPC transport routes %q, which the SDK interface does not declare", method)
		}
	}
	if !(&Transport{}).SupportsPrompts() {
		t.Fatal("the gRPC transport must declare the Prompts service")
	}
}
