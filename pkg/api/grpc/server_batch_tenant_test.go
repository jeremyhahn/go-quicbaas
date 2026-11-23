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
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// batchCallResult is what one batch RPC answered.
type batchCallResult struct {
	respNil bool
	err     error
}

// callBatch makes the service's BatchPut or BatchDelete fail with svcErr and
// drives the matching RPC through the gRPC server.
func callBatch(t *testing.T, op string, svcErr error) batchCallResult {
	t.Helper()
	h := newMockTestHelper(t)
	switch op {
	case "BatchPut":
		h.mock.BatchPutFunc = func(context.Context, *service.BatchPutRequest) (*service.BatchPutResponse, error) {
			return nil, svcErr
		}
		resp, err := h.server.BatchPut(context.Background(), &pb.BatchPutRequest{
			Requests: []*pb.PutRequest{{EntityType: "user", Id: 1, Data: []byte("{}")}},
		})
		return batchCallResult{respNil: resp == nil, err: err}
	case "BatchDelete":
		h.mock.BatchDeleteFunc = func(context.Context, *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
			return nil, svcErr
		}
		resp, err := h.server.BatchDelete(context.Background(), &pb.BatchDeleteRequest{
			Requests: []*pb.DeleteRequest{{EntityType: "user", Id: 1}},
		})
		return batchCallResult{respNil: resp == nil, err: err}
	}
	t.Fatalf("unknown batch op %q", op)
	return batchCallResult{}
}

func TestServerBatch_NoTenantIsPermissionDenied(t *testing.T) {
	// The service returns BuildKey's tenant refusal unchanged.
	_, tenantErr := tenantctx.FromContext(context.Background(), "keyspace.BuildKey")
	require.Error(t, tenantErr)

	for _, op := range []string{"BatchPut", "BatchDelete"} {
		t.Run(op, func(t *testing.T) {
			got := callBatch(t, op, tenantErr)
			require.True(t, got.respNil)
			require.Error(t, got.err)
			require.Equal(t, codes.PermissionDenied, status.Code(got.err), "error: %v", got.err)
		})
	}
}

func TestServerBatch_InvalidEntityTypeStaysInvalidArgument(t *testing.T) {
	for _, op := range []string{"BatchPut", "BatchDelete"} {
		t.Run(op, func(t *testing.T) {
			got := callBatch(t, op, serviceErr(errors.ErrInvalidInput, op))
			require.True(t, got.respNil)
			require.Error(t, got.err)
			require.Equal(t, codes.InvalidArgument, status.Code(got.err), "error: %v", got.err)
		})
	}
}
