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
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/parameterstore"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The parameterstore client sends plain Go structs under the SDK service name
// "quicbaas.ParameterStore", which the server does not serve: it serves
// quicbaas.parameterstore.v1.ParameterStore. This handler converts each verb
// to its generated RPC so no ParameterStore call falls through to the untyped
// conn.Invoke path.

// isParameterStoreService reports whether the service name is the
// ParameterStore service.
func isParameterStoreService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "ParameterStore"
}

// parameterStoreHandlerFunc serves one ParameterStore method.
type parameterStoreHandlerFunc func(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error)

var parameterStoreMethodMap = map[string]parameterStoreHandlerFunc{
	"GetParameter":     handleParameterStoreGetParameter,
	"SetParameter":     handleParameterStoreSetParameter,
	"DeleteParameter":  handleParameterStoreDeleteParameter,
	"GetParameters":    handleParameterStoreGetParameters,
	"SetParameters":    handleParameterStoreSetParameters,
	"DeleteParameters": handleParameterStoreDeleteParameters,
	"ListParameters":   handleParameterStoreListParameters,
	"SearchByTags":     handleParameterStoreSearchByTags,
}

// handleParameterStoreRequest routes one ParameterStore method over conn.
func handleParameterStoreRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	handler, ok := parameterStoreMethodMap[method]
	if !ok {
		return nil, errors.NewInvalidInput("grpc.parameterstore."+method, "unknown parameter store method")
	}
	return handler(ctx, pb.NewParameterStoreClient(conn), req)
}

// parameterTypeToProto maps the SDK value type to the wire enum. An empty or
// unknown type is sent as STRING, the default the server applies.
var parameterTypeToProto = map[string]pb.ParameterType{
	"string": pb.ParameterType_PARAMETER_TYPE_STRING,
	"number": pb.ParameterType_PARAMETER_TYPE_NUMBER,
	"json":   pb.ParameterType_PARAMETER_TYPE_JSON,
	"binary": pb.ParameterType_PARAMETER_TYPE_BINARY,
}

// parameterTypeFromProto maps the wire enum to the SDK value type. An
// unspecified type reads as "string", as it does on the server.
var parameterTypeFromProto = map[pb.ParameterType]string{
	pb.ParameterType_PARAMETER_TYPE_STRING: "string",
	pb.ParameterType_PARAMETER_TYPE_NUMBER: "number",
	pb.ParameterType_PARAMETER_TYPE_JSON:   "json",
	pb.ParameterType_PARAMETER_TYPE_BINARY: "binary",
}

func parameterTypeToWire(t string) pb.ParameterType {
	if pt, ok := parameterTypeToProto[t]; ok {
		return pt
	}
	return pb.ParameterType_PARAMETER_TYPE_STRING
}

func parameterTypeFromWire(t pb.ParameterType) string {
	if s, ok := parameterTypeFromProto[t]; ok {
		return s
	}
	return "string"
}

// parameterTime converts Unix nanoseconds to a time, reading 0 as the zero
// time.
func parameterTime(nanos int64) time.Time {
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

func parameterFromProto(p *pb.Parameter) *parameterstore.ParameterValue {
	if p == nil {
		return nil
	}
	out := &parameterstore.ParameterValue{
		Path: p.GetPath(), Value: p.GetValue(), Type: parameterTypeFromWire(p.GetType()), Description: p.GetDescription(),
		Tags: p.GetTags(), IsEncrypted: p.GetEncrypted(), KMSKeyID: p.GetKmsKeyId(), AuditEnabled: p.GetAuditEnabled(),
		CreatedAt: parameterTime(p.GetCreatedAt()), UpdatedAt: parameterTime(p.GetUpdatedAt()),
		CreatedBy: p.GetCreatedBy(), UpdatedBy: p.GetUpdatedBy(),
	}
	if p.GetExpiresAt() != 0 {
		expires := time.Unix(0, p.GetExpiresAt())
		out.ExpiresAt = &expires
	}
	return out
}

func parametersFromProto(params []*pb.Parameter) []*parameterstore.ParameterValue {
	out := make([]*parameterstore.ParameterValue, 0, len(params))
	for _, p := range params {
		out = append(out, parameterFromProto(p))
	}
	return out
}

func handleParameterStoreGetParameter(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error) {
	const op = "grpc.parameterstore.GetParameter"
	r, err := promptRequest[parameterstore.GetParameterRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetParameter(ctx, &pb.GetParameterRequest{Path: r.Path, WithDecryption: r.WithDecryption})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &parameterstore.GetParameterResponse{
		Parameter: parameterFromProto(resp.GetParameter()), Timestamp: parameterTime(resp.GetTimestamp()),
	}, nil
}

func handleParameterStoreSetParameter(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error) {
	const op = "grpc.parameterstore.SetParameter"
	r, err := promptRequest[parameterstore.SetParameterRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.SetParameter(ctx, &pb.SetParameterRequest{
		Path: r.Path, Value: r.Value, Type: parameterTypeToWire(r.Type), Description: r.Description, KmsKeyId: r.KMSKeyID,
		Tags: r.Tags, TtlSeconds: r.TTLSeconds, AuditEnabled: r.AuditEnabled, Overwrite: r.Overwrite,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &parameterstore.SetParameterResponse{
		Path: resp.GetPath(), Created: resp.GetCreated(), Timestamp: parameterTime(resp.GetTimestamp()),
	}, nil
}

func handleParameterStoreDeleteParameter(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error) {
	const op = "grpc.parameterstore.DeleteParameter"
	r, err := promptRequest[parameterstore.DeleteParameterRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.DeleteParameter(ctx, &pb.DeleteParameterRequest{Path: r.Path})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &parameterstore.DeleteParameterResponse{
		Path: resp.GetPath(), Deleted: resp.GetDeleted(), Timestamp: parameterTime(resp.GetTimestamp()),
	}, nil
}

func handleParameterStoreGetParameters(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error) {
	const op = "grpc.parameterstore.GetParameters"
	r, err := promptRequest[parameterstore.GetParametersRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetParameters(ctx, &pb.GetParametersRequest{Paths: r.Paths, WithDecryption: r.WithDecryption})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &parameterstore.GetParametersResponse{
		Parameters: parametersFromProto(resp.GetParameters()), Errors: resp.GetErrors(), Timestamp: parameterTime(resp.GetTimestamp()),
	}, nil
}

func handleParameterStoreSetParameters(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error) {
	const op = "grpc.parameterstore.SetParameters"
	r, err := promptRequest[parameterstore.SetParametersRequest](op, req)
	if err != nil {
		return nil, err
	}
	items := make([]*pb.SetParameterItem, 0, len(r.Parameters))
	for _, p := range r.Parameters {
		if p == nil {
			return nil, errors.NewInvalidInput(op, "parameter is required")
		}
		items = append(items, &pb.SetParameterItem{
			Path: p.Path, Value: p.Value, Type: parameterTypeToWire(p.Type), Description: p.Description, KmsKeyId: p.KMSKeyID,
			Tags: p.Tags, TtlSeconds: p.TTLSeconds, AuditEnabled: p.AuditEnabled, Overwrite: p.Overwrite,
		})
	}
	resp, err := client.SetParameters(ctx, &pb.SetParametersRequest{Parameters: items})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	results := make([]*parameterstore.SetParameterResult, 0, len(resp.GetResults()))
	for _, res := range resp.GetResults() {
		results = append(results, &parameterstore.SetParameterResult{
			Path: res.GetPath(), Created: res.GetCreated(), Success: res.GetSuccess(), Error: res.GetError(),
		})
	}
	return &parameterstore.SetParametersResponse{Results: results, Timestamp: parameterTime(resp.GetTimestamp())}, nil
}

func handleParameterStoreDeleteParameters(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error) {
	const op = "grpc.parameterstore.DeleteParameters"
	r, err := promptRequest[parameterstore.DeleteParametersRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.DeleteParameters(ctx, &pb.DeleteParametersRequest{Paths: r.Paths})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	results := make([]*parameterstore.DeleteParameterResult, 0, len(resp.GetResults()))
	for _, res := range resp.GetResults() {
		results = append(results, &parameterstore.DeleteParameterResult{
			Path: res.GetPath(), Deleted: res.GetDeleted(), Error: res.GetError(),
		})
	}
	return &parameterstore.DeleteParametersResponse{
		Results: results, DeletedCount: int(resp.GetDeletedCount()), Timestamp: parameterTime(resp.GetTimestamp()),
	}, nil
}

func handleParameterStoreListParameters(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error) {
	const op = "grpc.parameterstore.ListParameters"
	r, err := promptRequest[parameterstore.ListParametersRequest](op, req)
	if err != nil {
		return nil, err
	}
	limit, err := parameterLimit(op, r.Limit)
	if err != nil {
		return nil, err
	}
	resp, err := client.ListParameters(ctx, &pb.ListParametersRequest{
		Prefix: r.Prefix, Recursive: r.Recursive, WithDecryption: r.WithDecryption, WithValues: r.WithValues,
		Limit: limit, Cursor: r.Cursor,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &parameterstore.ListParametersResponse{
		Parameters: parametersFromProto(resp.GetParameters()), NextCursor: resp.GetNextCursor(), HasMore: resp.GetHasMore(),
		TotalCount: resp.GetTotalCount(), Timestamp: parameterTime(resp.GetTimestamp()),
	}, nil
}

func handleParameterStoreSearchByTags(ctx context.Context, client pb.ParameterStoreClient, req interface{}) (interface{}, error) {
	const op = "grpc.parameterstore.SearchByTags"
	r, err := promptRequest[parameterstore.SearchByTagsRequest](op, req)
	if err != nil {
		return nil, err
	}
	limit, err := parameterLimit(op, r.Limit)
	if err != nil {
		return nil, err
	}
	resp, err := client.SearchByTags(ctx, &pb.SearchByTagsRequest{
		Tags: r.Tags, Prefix: r.Prefix, WithValues: r.WithValues, WithDecryption: r.WithDecryption,
		Limit: limit, Cursor: r.Cursor,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &parameterstore.SearchByTagsResponse{
		Parameters: parametersFromProto(resp.GetParameters()), NextCursor: resp.GetNextCursor(), HasMore: resp.GetHasMore(),
		Timestamp: parameterTime(resp.GetTimestamp()),
	}, nil
}

// parameterLimit narrows the SDK page limit to the wire's int32, refusing a
// limit the wire cannot carry rather than truncating it.
func parameterLimit(op string, limit int) (int32, error) {
	if limit < 0 || limit > maxParameterLimit {
		return 0, errors.NewInvalidInput(op, "limit is out of range")
	}
	return int32(limit), nil
}

// maxParameterLimit is the largest page limit the int32 wire field carries.
const maxParameterLimit = 1<<31 - 1
