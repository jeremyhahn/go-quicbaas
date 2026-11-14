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

// The Query service handler maps the SDK query client's Execute verb onto the
// served QuicBaaS Query stream RPC (go-quicbaas#1612). The RPC matches one
// exact index value and carries no limit or cursor, so a request that needs
// any of those is refused with a typed unimplemented error.

package grpc

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	queryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const queryServiceName = "quicbaas.Query"

// QueryUnsupportedError reports a query request field the served Query RPC
// cannot carry. It unwraps to an SDK unimplemented error.
type QueryUnsupportedError struct {
	Field string
}

// Error implements error.
func (e *QueryUnsupportedError) Error() string {
	return "grpc.query.Execute: the served Query RPC cannot carry " + e.Field
}

// Unwrap returns the SDK unimplemented error the field maps to.
func (e *QueryUnsupportedError) Unwrap() error {
	return errors.NewUnimplemented("grpc.query.Execute", "the served Query RPC cannot carry "+e.Field)
}

// isQueryService reports whether service is the SDK Query service.
func isQueryService(service string) bool {
	return service == queryServiceName
}

// queryHandler serves quicbaas.Query requests over the QuicBaaS gRPC service.
type queryHandler struct {
	client pb.QuicBaaSClient
}

func newQueryHandler(conn *grpc.ClientConn) *queryHandler {
	return &queryHandler{client: pb.NewQuicBaaSClient(conn)}
}

func (h *queryHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	if method != "Execute" {
		return nil, errors.NewUnimplemented("grpc.query.handleRequest", "the gRPC transport serves no quicbaas.Query method "+method)
	}
	r, ok := req.(*queryclient.QueryRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.query.Execute", "invalid request type, expected *query.QueryRequest")
	}
	return h.execute(ctx, r)
}

func (h *queryHandler) execute(ctx context.Context, r *queryclient.QueryRequest) (*queryclient.QueryResponse, error) {
	pbReq, err := queryRequestToProto(r)
	if err != nil {
		return nil, err
	}
	stream, err := h.client.Query(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "grpc.query.Execute")
	}
	resp := &queryclient.QueryResponse{Results: []queryclient.QueryResult{}}
	for {
		batch, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, convertGRPCError(err, "grpc.query.Execute")
		}
		for _, e := range batch.Entities {
			data := e.Data
			if data == nil {
				data = []byte{}
			}
			resp.Results = append(resp.Results, queryclient.QueryResult{
				Key:   strconv.FormatUint(e.Id, 10),
				Value: data,
			})
		}
		resp.NextCursor = batch.NextPageToken
	}
	resp.HasMore = resp.NextCursor != ""
	return resp, nil
}

// queryRequestToProto converts an SDK query request into the served Query
// RPC's request, refusing what the RPC cannot carry.
func queryRequestToProto(r *queryclient.QueryRequest) (*pb.QueryRequest, error) {
	if r.Operator != queryclient.OpEqual {
		return nil, &QueryUnsupportedError{Field: "operator " + r.Operator.String()}
	}
	if r.Limit != 0 {
		return nil, &QueryUnsupportedError{Field: "limit"}
	}
	if r.Cursor != "" {
		return nil, &QueryUnsupportedError{Field: "cursor"}
	}
	if r.Value == nil {
		return nil, errors.NewInvalidInput("grpc.query.Execute", "value is required")
	}
	return &pb.QueryRequest{
		EntityType: r.EntityType,
		IndexName:  r.Index,
		Value:      queryValueBytes(r.Value),
	}, nil
}

// queryValueBytes renders an index value as the bytes the RPC matches.
func queryValueBytes(v interface{}) []byte {
	switch val := v.(type) {
	case string:
		return []byte(val)
	case []byte:
		return val
	default:
		return []byte(fmt.Sprint(val))
	}
}
