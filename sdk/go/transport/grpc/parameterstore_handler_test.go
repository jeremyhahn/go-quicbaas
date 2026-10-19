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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/parameterstore"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// paramStoreNanos is the instant the fake server stamps on every response.
const paramStoreNanos int64 = 1_700_000_000_123_456_789

var paramStoreTime = time.Unix(0, paramStoreNanos)

// fakeParameterStoreServer answers every ParameterStore RPC with a fixed
// response, or with code when code is set, and records the last request of
// each RPC so a test can see every field arrived.
type fakeParameterStoreServer struct {
	pb.UnimplementedParameterStoreServer
	code       codes.Code
	get        *pb.GetParameterRequest
	set        *pb.SetParameterRequest
	del        *pb.DeleteParameterRequest
	getBatch   *pb.GetParametersRequest
	setBatch   *pb.SetParametersRequest
	delBatch   *pb.DeleteParametersRequest
	list       *pb.ListParametersRequest
	searchTags *pb.SearchByTagsRequest
}

func (s *fakeParameterStoreServer) refusal() error {
	if s.code == codes.OK {
		return nil
	}
	return status.Error(s.code, "refused")
}

// wireParameter is the parameter the fake server returns from every read.
func wireParameter(path string) *pb.Parameter {
	return &pb.Parameter{
		Path: path, Value: []byte("v"), Type: pb.ParameterType_PARAMETER_TYPE_JSON, Description: "d",
		Tags: map[string]string{"env": "prod"}, Encrypted: true, KmsKeyId: "k1", ExpiresAt: paramStoreNanos,
		AuditEnabled: true, CreatedAt: paramStoreNanos, UpdatedAt: paramStoreNanos, CreatedBy: "alice", UpdatedBy: "bob",
	}
}

func (s *fakeParameterStoreServer) GetParameter(_ context.Context, req *pb.GetParameterRequest) (*pb.GetParameterResponse, error) {
	s.get = req
	if err := s.refusal(); err != nil {
		return nil, err
	}
	return &pb.GetParameterResponse{Parameter: wireParameter(req.GetPath()), Timestamp: paramStoreNanos}, nil
}

func (s *fakeParameterStoreServer) SetParameter(_ context.Context, req *pb.SetParameterRequest) (*pb.SetParameterResponse, error) {
	s.set = req
	if err := s.refusal(); err != nil {
		return nil, err
	}
	return &pb.SetParameterResponse{Path: req.GetPath(), Created: true, Timestamp: paramStoreNanos}, nil
}

func (s *fakeParameterStoreServer) DeleteParameter(_ context.Context, req *pb.DeleteParameterRequest) (*pb.DeleteParameterResponse, error) {
	s.del = req
	if err := s.refusal(); err != nil {
		return nil, err
	}
	return &pb.DeleteParameterResponse{Path: req.GetPath(), Deleted: true, Timestamp: paramStoreNanos}, nil
}

func (s *fakeParameterStoreServer) GetParameters(_ context.Context, req *pb.GetParametersRequest) (*pb.GetParametersResponse, error) {
	s.getBatch = req
	if err := s.refusal(); err != nil {
		return nil, err
	}
	return &pb.GetParametersResponse{
		Parameters: []*pb.Parameter{wireParameter("/a")}, Errors: map[string]string{"/b": "not found"}, Timestamp: paramStoreNanos,
	}, nil
}

func (s *fakeParameterStoreServer) SetParameters(_ context.Context, req *pb.SetParametersRequest) (*pb.SetParametersResponse, error) {
	s.setBatch = req
	if err := s.refusal(); err != nil {
		return nil, err
	}
	return &pb.SetParametersResponse{
		Results: []*pb.SetParameterResult{
			{Path: "/a", Created: true, Success: true},
			{Path: "/b", Error: "denied"},
		},
		Timestamp: paramStoreNanos,
	}, nil
}

func (s *fakeParameterStoreServer) DeleteParameters(_ context.Context, req *pb.DeleteParametersRequest) (*pb.DeleteParametersResponse, error) {
	s.delBatch = req
	if err := s.refusal(); err != nil {
		return nil, err
	}
	return &pb.DeleteParametersResponse{
		Results: []*pb.DeleteParameterResult{
			{Path: "/a", Deleted: true},
			{Path: "/b", Error: "not found"},
		},
		DeletedCount: 1,
		Timestamp:    paramStoreNanos,
	}, nil
}

func (s *fakeParameterStoreServer) ListParameters(_ context.Context, req *pb.ListParametersRequest) (*pb.ListParametersResponse, error) {
	s.list = req
	if err := s.refusal(); err != nil {
		return nil, err
	}
	return &pb.ListParametersResponse{
		Parameters: []*pb.Parameter{wireParameter("/app/a")}, NextCursor: "c2", HasMore: true, TotalCount: 7, Timestamp: paramStoreNanos,
	}, nil
}

func (s *fakeParameterStoreServer) SearchByTags(_ context.Context, req *pb.SearchByTagsRequest) (*pb.SearchByTagsResponse, error) {
	s.searchTags = req
	if err := s.refusal(); err != nil {
		return nil, err
	}
	return &pb.SearchByTagsResponse{
		Parameters: []*pb.Parameter{{Path: "/app/bare"}}, NextCursor: "c3", HasMore: true, Timestamp: paramStoreNanos,
	}, nil
}

// newParameterStoreClient drives the real SDK parameter store client over the
// gRPC transport against fake.
func newParameterStoreClient(t *testing.T, fake *fakeParameterStoreServer) parameterstore.Client {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterParameterStoreServer(s, fake) })
	tr := &Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: srv.dial(t)}
	return parameterstore.NewClient(tr)
}

// wantParameter is wireParameter as the SDK client must see it.
func wantParameter(path string) *parameterstore.ParameterValue {
	expires := paramStoreTime
	return &parameterstore.ParameterValue{
		Path: path, Value: []byte("v"), Type: "json", Description: "d",
		Tags: map[string]string{"env": "prod"}, IsEncrypted: true, KMSKeyID: "k1", ExpiresAt: &expires,
		AuditEnabled: true, CreatedAt: paramStoreTime, UpdatedAt: paramStoreTime, CreatedBy: "alice", UpdatedBy: "bob",
	}
}

func TestParameterStoreHandler_GetParameter(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	resp, err := newParameterStoreClient(t, fake).GetParameter(context.Background(),
		&parameterstore.GetParameterRequest{Path: "/app/host", WithDecryption: true})
	require.NoError(t, err)
	assert.Equal(t, "/app/host", fake.get.GetPath())
	assert.True(t, fake.get.GetWithDecryption())
	assert.Equal(t, &parameterstore.GetParameterResponse{Parameter: wantParameter("/app/host"), Timestamp: paramStoreTime}, resp)
}

func TestParameterStoreHandler_SetParameter(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	resp, err := newParameterStoreClient(t, fake).SetParameter(context.Background(), &parameterstore.SetParameterRequest{
		Path: "/app/host", Value: []byte("db"), Type: "number", Description: "d", KMSKeyID: "k1",
		Tags: map[string]string{"env": "prod"}, TTLSeconds: 60, AuditEnabled: true, Overwrite: true,
	})
	require.NoError(t, err)
	want := &pb.SetParameterRequest{
		Path: "/app/host", Value: []byte("db"), Type: pb.ParameterType_PARAMETER_TYPE_NUMBER, Description: "d", KmsKeyId: "k1",
		Tags: map[string]string{"env": "prod"}, TtlSeconds: 60, AuditEnabled: true, Overwrite: true,
	}
	assert.Equal(t, want.String(), fake.set.String())
	assert.Equal(t, &parameterstore.SetParameterResponse{Path: "/app/host", Created: true, Timestamp: paramStoreTime}, resp)
}

func TestParameterStoreHandler_DeleteParameter(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	resp, err := newParameterStoreClient(t, fake).DeleteParameter(context.Background(),
		&parameterstore.DeleteParameterRequest{Path: "/app/host"})
	require.NoError(t, err)
	assert.Equal(t, "/app/host", fake.del.GetPath())
	assert.Equal(t, &parameterstore.DeleteParameterResponse{Path: "/app/host", Deleted: true, Timestamp: paramStoreTime}, resp)
}

func TestParameterStoreHandler_GetParameters(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	resp, err := newParameterStoreClient(t, fake).GetParameters(context.Background(),
		&parameterstore.GetParametersRequest{Paths: []string{"/a", "/b"}, WithDecryption: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"/a", "/b"}, fake.getBatch.GetPaths())
	assert.True(t, fake.getBatch.GetWithDecryption())
	assert.Equal(t, &parameterstore.GetParametersResponse{
		Parameters: []*parameterstore.ParameterValue{wantParameter("/a")},
		Errors:     map[string]string{"/b": "not found"},
		Timestamp:  paramStoreTime,
	}, resp)
}

func TestParameterStoreHandler_SetParameters(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	resp, err := newParameterStoreClient(t, fake).SetParameters(context.Background(), &parameterstore.SetParametersRequest{
		Parameters: []*parameterstore.SetParameterRequest{
			{
				Path: "/a", Value: []byte("1"), Type: "binary", Description: "d", KMSKeyID: "k1",
				Tags: map[string]string{"env": "prod"}, TTLSeconds: 30, AuditEnabled: true, Overwrite: true,
			},
			{Path: "/b", Value: []byte("2")},
		},
	})
	require.NoError(t, err)
	want := &pb.SetParametersRequest{Parameters: []*pb.SetParameterItem{
		{
			Path: "/a", Value: []byte("1"), Type: pb.ParameterType_PARAMETER_TYPE_BINARY, Description: "d", KmsKeyId: "k1",
			Tags: map[string]string{"env": "prod"}, TtlSeconds: 30, AuditEnabled: true, Overwrite: true,
		},
		{Path: "/b", Value: []byte("2"), Type: pb.ParameterType_PARAMETER_TYPE_STRING},
	}}
	assert.Equal(t, want.String(), fake.setBatch.String())
	assert.Equal(t, &parameterstore.SetParametersResponse{
		Results: []*parameterstore.SetParameterResult{
			{Path: "/a", Created: true, Success: true},
			{Path: "/b", Error: "denied"},
		},
		Timestamp: paramStoreTime,
	}, resp)
}

func TestParameterStoreHandler_DeleteParameters(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	resp, err := newParameterStoreClient(t, fake).DeleteParameters(context.Background(),
		&parameterstore.DeleteParametersRequest{Paths: []string{"/a", "/b"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"/a", "/b"}, fake.delBatch.GetPaths())
	assert.Equal(t, &parameterstore.DeleteParametersResponse{
		Results: []*parameterstore.DeleteParameterResult{
			{Path: "/a", Deleted: true},
			{Path: "/b", Error: "not found"},
		},
		DeletedCount: 1,
		Timestamp:    paramStoreTime,
	}, resp)
}

func TestParameterStoreHandler_ListParameters(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	resp, err := newParameterStoreClient(t, fake).ListParameters(context.Background(), &parameterstore.ListParametersRequest{
		Prefix: "/app/", Recursive: true, WithDecryption: true, WithValues: true, Limit: 25, Cursor: "c1",
	})
	require.NoError(t, err)
	want := &pb.ListParametersRequest{Prefix: "/app/", Recursive: true, WithDecryption: true, WithValues: true, Limit: 25, Cursor: "c1"}
	assert.Equal(t, want.String(), fake.list.String())
	assert.Equal(t, &parameterstore.ListParametersResponse{
		Parameters: []*parameterstore.ParameterValue{wantParameter("/app/a")},
		NextCursor: "c2", HasMore: true, TotalCount: 7, Timestamp: paramStoreTime,
	}, resp)
}

// TestParameterStoreHandler_SearchByTags also pins the decoding of a bare
// parameter: an unspecified type reads as "string", and a zero expiry or
// creation time reads as no expiry and the zero time.
func TestParameterStoreHandler_SearchByTags(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	resp, err := newParameterStoreClient(t, fake).SearchByTags(context.Background(), &parameterstore.SearchByTagsRequest{
		Tags: map[string]string{"env": "prod"}, Prefix: "/app/", WithValues: true, WithDecryption: true, Limit: 10, Cursor: "c1",
	})
	require.NoError(t, err)
	want := &pb.SearchByTagsRequest{
		Tags: map[string]string{"env": "prod"}, Prefix: "/app/", WithValues: true, WithDecryption: true, Limit: 10, Cursor: "c1",
	}
	assert.Equal(t, want.String(), fake.searchTags.String())
	assert.Equal(t, &parameterstore.SearchByTagsResponse{
		Parameters: []*parameterstore.ParameterValue{{Path: "/app/bare", Type: "string"}},
		NextCursor: "c3", HasMore: true, Timestamp: paramStoreTime,
	}, resp)
}

// parameterStoreVerbCalls calls every parameterstore.Client verb with a valid
// request, so a test can drive each one against a refusing server.
func parameterStoreVerbCalls(client parameterstore.Client) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"GetParameter": func(ctx context.Context) error {
			_, err := client.GetParameter(ctx, &parameterstore.GetParameterRequest{Path: "/a"})
			return err
		},
		"SetParameter": func(ctx context.Context) error {
			_, err := client.SetParameter(ctx, &parameterstore.SetParameterRequest{Path: "/a", Value: []byte("v")})
			return err
		},
		"DeleteParameter": func(ctx context.Context) error {
			_, err := client.DeleteParameter(ctx, &parameterstore.DeleteParameterRequest{Path: "/a"})
			return err
		},
		"GetParameters": func(ctx context.Context) error {
			_, err := client.GetParameters(ctx, &parameterstore.GetParametersRequest{Paths: []string{"/a"}})
			return err
		},
		"SetParameters": func(ctx context.Context) error {
			_, err := client.SetParameters(ctx, &parameterstore.SetParametersRequest{
				Parameters: []*parameterstore.SetParameterRequest{{Path: "/a", Value: []byte("v")}},
			})
			return err
		},
		"DeleteParameters": func(ctx context.Context) error {
			_, err := client.DeleteParameters(ctx, &parameterstore.DeleteParametersRequest{Paths: []string{"/a"}})
			return err
		},
		"ListParameters": func(ctx context.Context) error {
			_, err := client.ListParameters(ctx, &parameterstore.ListParametersRequest{Prefix: "/"})
			return err
		},
		"SearchByTags": func(ctx context.Context) error {
			_, err := client.SearchByTags(ctx, &parameterstore.SearchByTagsRequest{Tags: map[string]string{"k": "v"}})
			return err
		},
	}
}

// TestParameterStoreHandler_MapsServerRefusals checks that each verb maps a
// server status to the typed SDK error every other handler returns.
func TestParameterStoreHandler_MapsServerRefusals(t *testing.T) {
	codesToPredicates := map[codes.Code]func(error) bool{
		codes.InvalidArgument:  errors.IsInvalidInput,
		codes.NotFound:         errors.IsNotFound,
		codes.AlreadyExists:    errors.IsAlreadyExists,
		codes.PermissionDenied: errors.IsForbidden,
		codes.Unavailable:      errors.IsUnavailable,
	}
	for code, is := range codesToPredicates {
		calls := parameterStoreVerbCalls(newParameterStoreClient(t, &fakeParameterStoreServer{code: code}))
		for verb, call := range calls {
			t.Run(verb+"/"+code.String(), func(t *testing.T) {
				err := call(context.Background())
				assert.True(t, is(err), "%s answering %s gave %v", verb, code, err)
			})
		}
	}
}

func TestParameterStoreHandler_RefusesUnknownMethodsAndRequestTypes(t *testing.T) {
	conn := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterParameterStoreServer(s, &fakeParameterStoreServer{})
	}).dial(t)
	ctx := context.Background()

	_, err := handleParameterStoreRequest(ctx, conn, "PutParameter", &parameterstore.GetParameterRequest{Path: "/a"})
	assert.True(t, errors.IsInvalidInput(err), "unknown method: %v", err)

	// The routed transport must refuse an unknown verb too, rather than fall
	// through to the untyped conn.Invoke path under the SDK service name.
	tr := &Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: conn}
	_, err = tr.Request(ctx, "quicbaas.ParameterStore", "PutParameter", &parameterstore.GetParameterRequest{Path: "/a"})
	assert.True(t, errors.IsInvalidInput(err), "routed unknown method: %v", err)

	for verb := range parameterStoreMethodMap {
		_, err := handleParameterStoreRequest(ctx, conn, verb, "wrong")
		assert.True(t, errors.IsInvalidInput(err), "%s with a wrong request type: %v", verb, err)
	}

	assert.True(t, isParameterStoreService("quicbaas.ParameterStore"))
	assert.True(t, isParameterStoreService("quicbaas.parameterstore.v1.ParameterStore"))
	assert.False(t, isParameterStoreService("quicbaas.Prompts"))
}

// TestParameterStoreHandler_RefusesWhatTheWireCannotCarry checks a page limit
// outside int32 and a nil batch item are refused before any RPC is sent.
func TestParameterStoreHandler_RefusesWhatTheWireCannotCarry(t *testing.T) {
	fake := &fakeParameterStoreServer{}
	client := newParameterStoreClient(t, fake)
	ctx := context.Background()

	for _, limit := range []int{-1, maxParameterLimit + 1} {
		_, err := client.ListParameters(ctx, &parameterstore.ListParametersRequest{Limit: limit})
		assert.True(t, errors.IsInvalidInput(err), "list limit %d: %v", limit, err)
		_, err = client.SearchByTags(ctx, &parameterstore.SearchByTagsRequest{Tags: map[string]string{"k": "v"}, Limit: limit})
		assert.True(t, errors.IsInvalidInput(err), "search limit %d: %v", limit, err)
	}
	assert.Nil(t, fake.list)
	assert.Nil(t, fake.searchTags)

	_, err := handleParameterStoreSetParameters(ctx, nil, &parameterstore.SetParametersRequest{
		Parameters: []*parameterstore.SetParameterRequest{nil},
	})
	assert.True(t, errors.IsInvalidInput(err), "nil batch item: %v", err)
}

// TestParameterStoreHandler_CarriesEveryVerb fails when a parameterstore.Client
// verb has no gRPC route, or a route names a verb the client does not declare.
func TestParameterStoreHandler_CarriesEveryVerb(t *testing.T) {
	iface := reflect.TypeOf((*parameterstore.Client)(nil)).Elem()
	declared := make(map[string]bool, iface.NumMethod())
	calls := parameterStoreVerbCalls(nil)
	for i := 0; i < iface.NumMethod(); i++ {
		verb := iface.Method(i).Name
		declared[verb] = true
		_, routed := parameterStoreMethodMap[verb]
		assert.True(t, routed, "the gRPC transport does not route parameterstore.%s", verb)
		_, tested := calls[verb]
		assert.True(t, tested, "parameterstore.%s is missing from parameterStoreVerbCalls", verb)
	}
	for method := range parameterStoreMethodMap {
		assert.True(t, declared[method], "the gRPC transport routes %q, which the SDK interface does not declare", method)
	}
}
