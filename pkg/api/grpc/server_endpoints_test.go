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
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// newTestServer constructs a Server with a mock service suitable for unit tests.
func newTestServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	s, err := NewServer(mockSvc, log, 1, "test")
	require.NoError(t, err)
	s.initMetrics()
	s.SetShardRouter(routing.New())
	return s, mockSvc
}

func TestPut_Success(t *testing.T) {
	s, mockSvc := newTestServer(t)

	mockSvc.On("Put", mock.Anything, mock.MatchedBy(func(req *service.PutRequest) bool {
		return req.EntityType == "user" && req.ID == 42
	})).Return(&service.PutResponse{
		ID:        42,
		Version:   1,
		Timestamp: time.Now().UnixNano(),
		LeaderID:  1,
	}, nil).Once()

	resp, err := s.Put(context.Background(), &pb.PutRequest{
		EntityType:  "user",
		Id:          42,
		Data:        []byte(`{"name":"alice"}`),
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED,
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(42), resp.Id)
	mockSvc.AssertExpectations(t)
}

func TestGet_NotFound(t *testing.T) {
	s, mockSvc := newTestServer(t)

	// Return not found response
	mockSvc.On("Get", mock.Anything, mock.Anything).
		Return(&service.GetResponse{
			ID:    123,
			Found: false,
		}, nil).Once()

	_, err := s.Get(context.Background(), &pb.GetRequest{EntityType: "user", Id: 123})
	assert.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestGet_Success_WithWrappedData(t *testing.T) {
	s, mockSvc := newTestServer(t)

	rawValue := []byte("hello")
	mockSvc.On("Get", mock.Anything, mock.Anything).
		Return(&service.GetResponse{
			ID:    123,
			Data:  rawValue,
			Found: true,
		}, nil).Once()

	resp, err := s.Get(context.Background(), &pb.GetRequest{EntityType: "user", Id: 123})
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), resp.Data)
	mockSvc.AssertExpectations(t)
}

func TestDelete_Success(t *testing.T) {
	s, mockSvc := newTestServer(t)

	mockSvc.On("Delete", mock.Anything, mock.Anything).
		Return(&service.DeleteResponse{
			Deleted:   true,
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := s.Delete(context.Background(), &pb.DeleteRequest{EntityType: "user", Id: 7})
	require.NoError(t, err)
	assert.True(t, resp.Deleted)
	mockSvc.AssertExpectations(t)
}

// fakeListServer implements pb.QuicBaaS_ListServer for unit testing.
type fakeListServer struct {
	ctx  context.Context
	sent []*pb.ListResponse
}

func (f *fakeListServer) Send(r *pb.ListResponse) error {
	f.sent = append(f.sent, r)
	return nil
}

// ServerStream methods (no-ops for tests).
func (f *fakeListServer) SetHeader(metadata.MD) error  { return nil }
func (f *fakeListServer) SendHeader(metadata.MD) error { return nil }
func (f *fakeListServer) SetTrailer(metadata.MD)       {}
func (f *fakeListServer) Context() context.Context     { return f.ctx }
func (f *fakeListServer) SendMsg(any) error            { return nil }
func (f *fakeListServer) RecvMsg(any) error            { return io.EOF }

func TestList_Pagination(t *testing.T) {
	s, mockSvc := newTestServer(t)

	// Mock the List method to send entities via channel
	mockSvc.On("List", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			stream := args.Get(2).(chan<- *service.ListResponse)
			go func() {
				defer close(stream)
				stream <- &service.ListResponse{
					Entities: []*service.Entity{
						{EntityType: "user", ID: 1, Data: []byte(`{"name":"alice"}`)},
						{EntityType: "user", ID: 2, Data: []byte(`{"name":"bob"}`)},
					},
					NextPageToken: "page2",
					TotalCount:    2,
				}
			}()
		}).Return(nil).Once()

	fake := &fakeListServer{ctx: context.Background()}
	err := s.List(&pb.ListRequest{
		EntityType: "user",
		PageSize:   10,
	}, fake)

	require.NoError(t, err)
	assert.Len(t, fake.sent, 1)
	assert.Len(t, fake.sent[0].Entities, 2)
	mockSvc.AssertExpectations(t)
}

func TestBatchPut_AtomicSuccess(t *testing.T) {
	s, mockSvc := newTestServer(t)

	mockSvc.On("BatchPut", mock.Anything, mock.Anything).
		Return(&service.BatchPutResponse{
			Responses: []*service.PutResponse{
				{ID: 1, Version: 1, Timestamp: time.Now().UnixNano()},
				{ID: 2, Version: 1, Timestamp: time.Now().UnixNano()},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := s.BatchPut(context.Background(), &pb.BatchPutRequest{
		Requests: []*pb.PutRequest{
			{EntityType: "user", Id: 1, Data: []byte(`{"name":"a"}`)},
			{EntityType: "user", Id: 2, Data: []byte(`{"name":"b"}`)},
		},
	})

	require.NoError(t, err)
	assert.Len(t, resp.Responses, 2)
	mockSvc.AssertExpectations(t)
}

func TestBatchGet_MultipleKeys(t *testing.T) {
	s, mockSvc := newTestServer(t)

	mockSvc.On("BatchGet", mock.Anything, mock.Anything).
		Return(&service.BatchGetResponse{
			Responses: []*service.GetResponse{
				{ID: 1, Data: []byte(`{"name":"alice"}`), Found: true},
				{ID: 2, Data: []byte(`{"name":"bob"}`), Found: true},
			},
		}, nil).Once()

	resp, err := s.BatchGet(context.Background(), &pb.BatchGetRequest{
		Requests: []*pb.GetRequest{
			{EntityType: "user", Id: 1},
			{EntityType: "user", Id: 2},
		},
	})

	require.NoError(t, err)
	assert.Len(t, resp.Responses, 2)
	assert.True(t, resp.Responses[0].Found)
	assert.True(t, resp.Responses[1].Found)
	mockSvc.AssertExpectations(t)
}

func TestBatchDelete_Atomicity(t *testing.T) {
	s, mockSvc := newTestServer(t)

	mockSvc.On("BatchDelete", mock.Anything, mock.Anything).
		Return(&service.BatchDeleteResponse{
			Responses: []*service.DeleteResponse{
				{Deleted: true, Timestamp: time.Now().UnixNano()},
				{Deleted: true, Timestamp: time.Now().UnixNano()},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := s.BatchDelete(context.Background(), &pb.BatchDeleteRequest{
		Requests: []*pb.DeleteRequest{
			{EntityType: "user", Id: 1},
			{EntityType: "user", Id: 2},
		},
	})

	require.NoError(t, err)
	assert.Len(t, resp.Responses, 2)
	assert.True(t, resp.Responses[0].Deleted)
	assert.True(t, resp.Responses[1].Deleted)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_CompareAndSwap(t *testing.T) {
	s, mockSvc := newTestServer(t)

	mockSvc.On("Transaction", mock.Anything, mock.Anything).
		Return(&service.TransactionResponse{
			Succeeded: true,
			Responses: []*service.TransactionOpResponse{
				{
					OpType:     service.TransactionOpTypePut,
					EntityType: "user",
					ID:         42,
					Success:    true,
					Version:    2,
					Timestamp:  time.Now().UnixNano(),
				},
			},
			Timestamp: time.Now().UnixNano(),
			Message:   "Transaction succeeded",
			Revision:  1,
		}, nil).Once()

	resp, err := s.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         42,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    1,
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         42,
				Data:       []byte(`{"name":"updated"}`),
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Len(t, resp.Responses, 1)
	mockSvc.AssertExpectations(t)
}

func TestClusterStatus_AllNodes(t *testing.T) {
	s, mockSvc := newTestServer(t)

	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
		Return(&service.ClusterStatusResponse{
			ClusterID: "test-cluster",
			Nodes: []*service.NodeInfo{
				{NodeID: 1, IsLeader: true, Version: "1.0.0", HealthStatus: service.HealthStatusHealthy},
				{NodeID: 2, IsLeader: false, Version: "1.0.0", HealthStatus: service.HealthStatusHealthy},
			},
			Shards: []*service.ShardInfo{
				{ShardID: 1, LeaderID: 1, Replicas: []uint64{1, 2}, Status: service.HealthStatusHealthy},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := s.ClusterStatus(context.Background(), &pb.ClusterStatusRequest{
		IncludeMetrics: true,
	})

	require.NoError(t, err)
	assert.Equal(t, "test-cluster", resp.ClusterId)
	assert.Len(t, resp.Nodes, 2)
	assert.Len(t, resp.Shards, 1)
	mockSvc.AssertExpectations(t)
}

func TestHealth_ComponentDetails(t *testing.T) {
	s, mockSvc := newTestServer(t)

	mockSvc.On("Health", mock.Anything, mock.Anything).
		Return(&service.HealthResponse{
			Status:        service.HealthStatusHealthy,
			NodeID:        1,
			Version:       "1.0.0",
			UptimeSeconds: 100,
			Details: map[string]*service.ComponentHealth{
				"storage": {
					Status:    service.HealthStatusHealthy,
					Message:   "OK",
					LastCheck: time.Now().UnixNano(),
				},
				"raft": {
					Status:    service.HealthStatusHealthy,
					Message:   "OK",
					LastCheck: time.Now().UnixNano(),
				},
			},
		}, nil).Once()

	resp, err := s.Health(context.Background(), &pb.HealthRequest{
		IncludeDetails: true,
	})

	require.NoError(t, err)
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.Status)
	assert.Equal(t, uint64(1), resp.NodeId)
	assert.Len(t, resp.Details, 2)
	mockSvc.AssertExpectations(t)
}
