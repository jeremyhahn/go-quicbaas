package grpc

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestServerCompareAndSwap(t *testing.T) {
	stored := []byte("v1")
	tests := []struct {
		name   string
		expect []byte
		want   bool
	}{
		{name: "matching expect is applied", expect: []byte("v1"), want: true},
		{name: "stale expect is not applied", expect: []byte("v0"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newMockTestHelper(t)
			var got *service.CompareAndSwapRequest
			h.mock.CompareAndSwapFunc = func(_ context.Context, req *service.CompareAndSwapRequest) (*service.ConditionalPutResponse, error) {
				got = req
				return &service.ConditionalPutResponse{Applied: bytes.Equal(req.Expect, stored)}, nil
			}

			resp, err := h.server.CompareAndSwap(context.Background(), &pb.CompareAndSwapRequest{
				EntityType: "locks", Id: 7, Expect: tt.expect, Set: []byte("v2"), ShardId: 3,
			})

			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.GetApplied())
			assert.Equal(t, &service.CompareAndSwapRequest{
				EntityType: "locks", ID: 7, Expect: tt.expect, Set: []byte("v2"), ShardID: 3,
			}, got)
		})
	}
}

func TestServerCompareAndSwap_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)
	h.mock.CompareAndSwapFunc = func(context.Context, *service.CompareAndSwapRequest) (*service.ConditionalPutResponse, error) {
		return nil, service.ErrRaft
	}

	resp, err := h.server.CompareAndSwap(context.Background(), &pb.CompareAndSwapRequest{EntityType: "locks", Id: 7})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerPutIfAbsent(t *testing.T) {
	tests := []struct {
		name    string
		present bool
		want    bool
	}{
		{name: "absent key is applied", present: false, want: true},
		{name: "present key is not applied", present: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newMockTestHelper(t)
			var got *service.PutIfAbsentRequest
			h.mock.PutIfAbsentFunc = func(_ context.Context, req *service.PutIfAbsentRequest) (*service.ConditionalPutResponse, error) {
				got = req
				return &service.ConditionalPutResponse{Applied: !tt.present}, nil
			}

			resp, err := h.server.PutIfAbsent(context.Background(), &pb.PutIfAbsentRequest{
				EntityType: "locks", Id: 7, Data: []byte("owner-a"),
			})

			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.GetApplied())
			assert.Equal(t, &service.PutIfAbsentRequest{EntityType: "locks", ID: 7, Data: []byte("owner-a")}, got)
		})
	}
}

func TestServerPutIfAbsent_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)
	h.mock.PutIfAbsentFunc = func(context.Context, *service.PutIfAbsentRequest) (*service.ConditionalPutResponse, error) {
		return nil, service.ErrRaft
	}

	resp, err := h.server.PutIfAbsent(context.Background(), &pb.PutIfAbsentRequest{EntityType: "locks", Id: 7})

	assert.Error(t, err)
	assert.Nil(t, resp)
}
