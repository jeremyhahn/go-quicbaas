package grpc

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// conditionalPutServer holds one in-memory value per entity so a test can
// drive a conflict through the real gRPC client and server stubs.
type conditionalPutServer struct {
	pb.UnimplementedQuicBaaSServer
	mu     sync.Mutex
	stored map[uint64][]byte
}

func (s *conditionalPutServer) CompareAndSwap(_ context.Context, req *pb.CompareAndSwapRequest) (*pb.ConditionalPutResponse, error) {
	if req.GetEntityType() != "locks" {
		return nil, status.Error(codes.InvalidArgument, "unknown entity type")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.stored[req.GetId()]
	if !ok || !bytes.Equal(held, req.GetExpect()) {
		return &pb.ConditionalPutResponse{Applied: false}, nil
	}
	s.stored[req.GetId()] = req.GetSet()
	return &pb.ConditionalPutResponse{Applied: true}, nil
}

func (s *conditionalPutServer) PutIfAbsent(_ context.Context, req *pb.PutIfAbsentRequest) (*pb.ConditionalPutResponse, error) {
	if req.GetEntityType() != "locks" {
		return nil, status.Error(codes.InvalidArgument, "unknown entity type")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.stored[req.GetId()]; ok {
		return &pb.ConditionalPutResponse{Applied: false}, nil
	}
	s.stored[req.GetId()] = req.GetData()
	return &pb.ConditionalPutResponse{Applied: true}, nil
}

// newConditionalPutHandler serves a conditionalPutServer over bufconn and
// returns a kvHandler dialled to it.
func newConditionalPutHandler(t *testing.T, stored map[uint64][]byte) *kvHandler {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterQuicBaaSServer(srv, &conditionalPutServer{stored: stored})
	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		<-served
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return newKVHandler(conn)
}

func appliedOf(t *testing.T, resp interface{}) bool {
	t.Helper()
	condResp, ok := resp.(*service.ConditionalPutResponse)
	require.True(t, ok, "response type %T", resp)
	return condResp.Applied
}

func TestKVHandler_CompareAndSwap(t *testing.T) {
	handler := newConditionalPutHandler(t, map[uint64][]byte{7: []byte("v1")})
	ctx := context.Background()

	resp, err := handler.handleRequest(ctx, kvServicePrefix, "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "locks", ID: 7, Expect: []byte("v1"), Set: []byte("v2")})
	require.NoError(t, err)
	assert.True(t, appliedOf(t, resp), "matching expect must apply")

	// The value is now v2, so the same expect is stale.
	resp, err = handler.handleRequest(ctx, kvServicePrefix, "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "locks", ID: 7, Expect: []byte("v1"), Set: []byte("v3")})
	require.NoError(t, err)
	assert.False(t, appliedOf(t, resp), "stale expect must not apply")
}

func TestKVHandler_CompareAndSwap_Errors(t *testing.T) {
	handler := newConditionalPutHandler(t, map[uint64][]byte{})
	ctx := context.Background()

	_, err := handler.handleRequest(ctx, kvServicePrefix, "CompareAndSwap", &service.PutRequest{})
	assert.True(t, errors.IsInvalidInput(err), "wrong request type: %v", err)

	_, err = handler.handleRequest(ctx, kvServicePrefix, "CompareAndSwap",
		&service.CompareAndSwapRequest{EntityType: "other", ID: 1})
	assert.Error(t, err, "a server error must surface")
}

func TestKVHandler_PutIfAbsent(t *testing.T) {
	handler := newConditionalPutHandler(t, map[uint64][]byte{})
	ctx := context.Background()

	resp, err := handler.handleRequest(ctx, kvServicePrefix, "PutIfAbsent",
		&service.PutIfAbsentRequest{EntityType: "locks", ID: 7, Data: []byte("owner-a")})
	require.NoError(t, err)
	assert.True(t, appliedOf(t, resp), "absent key must apply")

	resp, err = handler.handleRequest(ctx, kvServicePrefix, "PutIfAbsent",
		&service.PutIfAbsentRequest{EntityType: "locks", ID: 7, Data: []byte("owner-b")})
	require.NoError(t, err)
	assert.False(t, appliedOf(t, resp), "present key must not apply")
}

func TestKVHandler_PutIfAbsent_Errors(t *testing.T) {
	handler := newConditionalPutHandler(t, map[uint64][]byte{})
	ctx := context.Background()

	_, err := handler.handleRequest(ctx, kvServicePrefix, "PutIfAbsent", (*service.PutIfAbsentRequest)(nil))
	assert.True(t, errors.IsInvalidInput(err), "nil request: %v", err)

	_, err = handler.handleRequest(ctx, kvServicePrefix, "PutIfAbsent",
		&service.PutIfAbsentRequest{EntityType: "other", ID: 1})
	assert.Error(t, err, "a server error must surface")
}
