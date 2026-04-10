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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestParameterStoreServer_ServiceErrors covers the service-level error return
// paths in SetParameter, DeleteParameter, GetParameters, SetParameters,
// DeleteParameters, ListParameters, and SearchByTags.
func TestParameterStoreServer_ServiceErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("SetParameter_ServiceError", func(t *testing.T) {
		mock := &mockParameterStoreService{
			setParameterFunc: func(_ context.Context, _ *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
				return nil, errors.New(errors.ErrInternal, "SetParameter", "write failed")
			},
		}
		srv := newTestParameterStoreServer(t, mock)

		_, err := srv.SetParameter(ctx, &pb.SetParameterRequest{
			Path:  "/app/config",
			Value: []byte("value"),
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("DeleteParameter_ServiceError", func(t *testing.T) {
		mock := &mockParameterStoreService{
			deleteParameterFunc: func(_ context.Context, _ *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "DeleteParameter", "not found")
			},
		}
		srv := newTestParameterStoreServer(t, mock)

		_, err := srv.DeleteParameter(ctx, &pb.DeleteParameterRequest{
			Path: "/app/missing",
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("GetParameters_ServiceError", func(t *testing.T) {
		mock := &mockParameterStoreService{
			getParametersFunc: func(_ context.Context, _ *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
				return nil, errors.New(errors.ErrInternal, "GetParameters", "batch read failed")
			},
		}
		srv := newTestParameterStoreServer(t, mock)

		_, err := srv.GetParameters(ctx, &pb.GetParametersRequest{
			Paths: []string{"/app/a", "/app/b"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("SetParameters_ServiceError", func(t *testing.T) {
		mock := &mockParameterStoreService{
			setParametersFunc: func(_ context.Context, _ *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
				return nil, errors.New(errors.ErrInternal, "SetParameters", "batch write failed")
			},
		}
		srv := newTestParameterStoreServer(t, mock)

		_, err := srv.SetParameters(ctx, &pb.SetParametersRequest{
			Parameters: []*pb.SetParameterItem{
				{Path: "/app/a", Value: []byte("v1")},
			},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("DeleteParameters_ServiceError", func(t *testing.T) {
		mock := &mockParameterStoreService{
			deleteParametersFunc: func(_ context.Context, _ *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
				return nil, errors.New(errors.ErrInternal, "DeleteParameters", "batch delete failed")
			},
		}
		srv := newTestParameterStoreServer(t, mock)

		_, err := srv.DeleteParameters(ctx, &pb.DeleteParametersRequest{
			Paths: []string{"/app/a"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("ListParameters_ServiceError", func(t *testing.T) {
		mock := &mockParameterStoreService{
			listParametersFunc: func(_ context.Context, _ *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
				return nil, errors.New(errors.ErrInternal, "ListParameters", "list failed")
			},
		}
		srv := newTestParameterStoreServer(t, mock)

		_, err := srv.ListParameters(ctx, &pb.ListParametersRequest{
			Prefix: "/app/",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("SearchByTags_ServiceError", func(t *testing.T) {
		mock := &mockParameterStoreService{
			searchByTagsFunc: func(_ context.Context, _ *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
				return nil, errors.New(errors.ErrInternal, "SearchByTags", "search failed")
			},
		}
		srv := newTestParameterStoreServer(t, mock)

		_, err := srv.SearchByTags(ctx, &pb.SearchByTagsRequest{
			Tags: map[string]string{"env": "prod"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}
