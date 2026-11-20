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

package grpc

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	queryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeQueryServer serves the QuicBaaS Query stream from fixed batches and
// records the request it received.
type fakeQueryServer struct {
	pb.UnimplementedQuicBaaSServer
	batches  []*pb.QueryResponse
	err      error
	received *pb.QueryRequest
}

func (f *fakeQueryServer) Query(req *pb.QueryRequest, stream pb.QuicBaaS_QueryServer) error {
	f.received = req
	if f.err != nil {
		return f.err
	}
	for _, b := range f.batches {
		if err := stream.Send(b); err != nil {
			return err
		}
	}
	return nil
}

func newQueryTransportClient(t *testing.T, srv *fakeQueryServer) queryclient.Client {
	t.Helper()
	hs := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterQuicBaaSServer(s, srv)
	})
	return queryclient.NewClient(&Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: hs.dial(t)})
}

func TestGRPCQuery_ExecuteCollectsTheQueryStream(t *testing.T) {
	srv := &fakeQueryServer{batches: []*pb.QueryResponse{
		{Entities: []*pb.Entity{{Id: 7, Data: []byte(`{"n":1}`)}}},
		{Entities: []*pb.Entity{{Id: 9}}, NextPageToken: "next"},
	}}
	c := newQueryTransportClient(t, srv)

	resp, err := c.Execute(context.Background(), &queryclient.QueryRequest{
		EntityType: "user", Index: "email", Value: "a@example.com",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if srv.received == nil {
		t.Fatal("the server received no request")
	}
	if srv.received.EntityType != "user" || srv.received.IndexName != "email" || string(srv.received.Value) != "a@example.com" {
		t.Errorf("server received %q/%q/%q", srv.received.EntityType, srv.received.IndexName, srv.received.Value)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(resp.Results))
	}
	if resp.Results[0].Key != "7" || string(resp.Results[0].Value) != `{"n":1}` {
		t.Errorf("result 0 = %+v", resp.Results[0])
	}
	if resp.Results[1].Key != "9" || resp.Results[1].Value == nil || len(resp.Results[1].Value) != 0 {
		t.Errorf("result 1 = %+v, want key 9 and a non-nil empty value", resp.Results[1])
	}
	if resp.NextCursor != "next" || !resp.HasMore {
		t.Errorf("NextCursor=%q HasMore=%v, want next/true", resp.NextCursor, resp.HasMore)
	}
}

func TestGRPCQuery_ExecuteRendersNonStringValues(t *testing.T) {
	for _, tc := range []struct {
		value interface{}
		want  string
	}{
		{42, "42"},
		{true, "true"},
		{[]byte("raw"), "raw"},
	} {
		srv := &fakeQueryServer{}
		c := newQueryTransportClient(t, srv)
		if _, err := c.Execute(context.Background(), &queryclient.QueryRequest{
			EntityType: "user", Index: "age", Value: tc.value,
		}); err != nil {
			t.Fatalf("Execute(%v): %v", tc.value, err)
		}
		if srv.received == nil || string(srv.received.Value) != tc.want {
			t.Errorf("value %v sent as %v, want %q", tc.value, srv.received, tc.want)
		}
	}
}

func TestGRPCQuery_ExecuteRefusesWhatTheQueryRPCCannotCarry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		req   queryclient.QueryRequest
		field string
	}{
		{"range", queryclient.QueryRequest{Operator: queryclient.OpRange}, "operator RANGE"},
		{"prefix", queryclient.QueryRequest{Operator: queryclient.OpPrefix}, "operator PREFIX"},
		{"limit", queryclient.QueryRequest{Limit: 10}, "limit"},
		{"cursor", queryclient.QueryRequest{Cursor: "c"}, "cursor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := &fakeQueryServer{}
			c := newQueryTransportClient(t, srv)
			req := tc.req
			req.EntityType = "user"
			req.Index = "email"
			req.Value = "a@example.com"
			_, err := c.Execute(context.Background(), &req)
			var unsupported *QueryUnsupportedError
			if !stderrors.As(err, &unsupported) {
				t.Fatalf("err = %v, want *QueryUnsupportedError", err)
			}
			if unsupported.Field != tc.field {
				t.Errorf("Field = %q, want %q", unsupported.Field, tc.field)
			}
			if !sdkerrors.IsUnimplemented(err) {
				t.Errorf("IsUnimplemented(%v) = false", err)
			}
			if srv.received != nil {
				t.Errorf("the server received %v, want no request", srv.received)
			}
		})
	}
}

func TestGRPCQuery_ExecuteReturnsTheServerError(t *testing.T) {
	srv := &fakeQueryServer{err: status.Error(codes.NotFound, "no such index")}
	c := newQueryTransportClient(t, srv)
	_, err := c.Execute(context.Background(), &queryclient.QueryRequest{
		EntityType: "user", Index: "email", Value: "a@example.com",
	})
	if !sdkerrors.IsNotFound(err) {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestGRPCQuery_HandlerRefusesBadInput(t *testing.T) {
	h := newQueryHandler(nil)
	ctx := context.Background()
	if _, err := h.handleRequest(ctx, "Search", &queryclient.QueryRequest{}); !sdkerrors.IsUnimplemented(err) {
		t.Errorf("Search: err = %v, want unimplemented", err)
	}
	if _, err := h.handleRequest(ctx, "Execute", "wrong"); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("wrong type: err = %v, want invalid input", err)
	}
	if _, err := h.handleRequest(ctx, "Execute", &queryclient.QueryRequest{EntityType: "user", Index: "email"}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("no value: err = %v, want invalid input", err)
	}
	if !isQueryService("quicbaas.Query") {
		t.Error(`isQueryService("quicbaas.Query") = false`)
	}
	if isQueryService("quicbaas.KV") {
		t.Error(`isQueryService("quicbaas.KV") = true`)
	}
}
