package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestBatchPut_KeepsItemShard(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)
	mockSvc.On("BatchPut", mock.Anything, mock.MatchedBy(func(r *service.BatchPutRequest) bool {
		return len(r.Requests) == 2 && r.Requests[0].ShardID == 2 && r.Requests[1].ShardID == 0
	})).Return(&service.BatchPutResponse{Responses: []*service.PutResponse{{ID: 1}, {ID: 2}}}, nil)

	_, err := srv.BatchPut(context.Background(), &pb.BatchPutRequest{Requests: []*pb.PutRequest{
		{EntityType: "users", Id: 1, Data: []byte("a"), ShardId: 2},
		{EntityType: "users", Id: 2, Data: []byte("b"), ShardId: 0},
	}})
	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

func TestBatchGet_KeepsItemShard(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)
	mockSvc.On("BatchGet", mock.Anything, mock.MatchedBy(func(r *service.BatchGetRequest) bool {
		return len(r.Requests) == 2 && r.Requests[0].ShardID == 2 && r.Requests[1].ShardID == 0
	})).Return(&service.BatchGetResponse{Responses: []*service.GetResponse{{ID: 1, Found: true}, {ID: 2}}}, nil)

	_, err := srv.BatchGet(context.Background(), &pb.BatchGetRequest{Requests: []*pb.GetRequest{
		{EntityType: "users", Id: 1, ShardId: 2},
		{EntityType: "users", Id: 2, ShardId: 0},
	}})
	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

func TestBatchDelete_KeepsItemShard(t *testing.T) {
	srv, mockSvc := newMockTestServer(t)
	mockSvc.On("BatchDelete", mock.Anything, mock.MatchedBy(func(r *service.BatchDeleteRequest) bool {
		return len(r.Requests) == 2 && r.Requests[0].ShardID == 2 && r.Requests[1].ShardID == 0
	})).Return(&service.BatchDeleteResponse{Responses: []*service.DeleteResponse{{Deleted: true}, {Deleted: true}}}, nil)

	_, err := srv.BatchDelete(context.Background(), &pb.BatchDeleteRequest{Requests: []*pb.DeleteRequest{
		{EntityType: "users", Id: 1, ShardId: 2},
		{EntityType: "users", Id: 2, ShardId: 0},
	}})
	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}
