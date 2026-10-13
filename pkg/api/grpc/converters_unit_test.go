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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// toProtoError Tests (helpers.go)
// =============================================================================

func TestToProtoError_Coverage(t *testing.T) {
	t.Run("nil_error", func(t *testing.T) {
		result := toProtoError(nil)
		assert.Nil(t, result)
	})

	t.Run("already_grpc_error", func(t *testing.T) {
		err := status.Error(codes.NotFound, "not found")
		result := toProtoError(err)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("qb_error", func(t *testing.T) {
		err := &errors.QBError{
			Code: errors.ErrForbidden,
			Op:   "test",
			Err:  stderrors.New("permission denied"),
		}
		result := toProtoError(err)
		assert.Equal(t, codes.PermissionDenied, status.Code(result))
	})

	t.Run("plain_error_wraps_as_internal", func(t *testing.T) {
		result := toProtoError(stderrors.New("unknown"))
		assert.Equal(t, codes.Internal, status.Code(result))
	})
}

// =============================================================================
// mapErrorCode Tests (helpers.go)
// =============================================================================

// =============================================================================
// Validation Function Tests (helpers.go) - only functions not already tested
// =============================================================================

func TestValidateListRequest_Coverage(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		err := validateListRequest(nil)
		require.Error(t, err)
	})

	t.Run("empty_entity_type", func(t *testing.T) {
		err := validateListRequest(&pb.ListRequest{EntityType: ""})
		require.Error(t, err)
	})

	t.Run("valid", func(t *testing.T) {
		err := validateListRequest(&pb.ListRequest{EntityType: "user"})
		assert.NoError(t, err)
	})
}

func TestValidateDeleteRequest_Coverage(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		err := validateDeleteRequest(nil)
		require.Error(t, err)
	})

	t.Run("empty_entity_type", func(t *testing.T) {
		err := validateDeleteRequest(&pb.DeleteRequest{EntityType: "", Id: 1})
		require.Error(t, err)
	})

	t.Run("zero_id", func(t *testing.T) {
		err := validateDeleteRequest(&pb.DeleteRequest{EntityType: "user", Id: 0})
		require.Error(t, err)
	})

	t.Run("valid", func(t *testing.T) {
		err := validateDeleteRequest(&pb.DeleteRequest{EntityType: "user", Id: 1})
		assert.NoError(t, err)
	})
}

func TestValidateQueryRequest_Coverage(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		err := validateQueryRequest(nil)
		require.Error(t, err)
	})

	t.Run("empty_entity_type", func(t *testing.T) {
		err := validateQueryRequest(&pb.QueryRequest{EntityType: ""})
		require.Error(t, err)
	})
}

func TestValidateTransactionRequest_Coverage(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		err := validateTransactionRequest(nil)
		require.Error(t, err)
	})

	t.Run("empty_conditions", func(t *testing.T) {
		err := validateTransactionRequest(&pb.TransactionRequest{
			Compare: nil,
		})
		require.Error(t, err)
	})
}
