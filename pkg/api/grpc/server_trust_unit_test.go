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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubTrustService is a minimal service.TrustService for the gRPC server
// unit tests.
type stubTrustService struct {
	crossErr    error
	importErr   error
	rootPEMs    [][]byte
	importedQt  int
	publishErr  error
	publishedQt int
}

func (s *stubTrustService) CrossSignedRootPool(ctx context.Context, _ *service.CrossSignedRootPoolRequest) (*service.CrossSignedRootPoolResponse, error) {
	if s.crossErr != nil {
		return nil, s.crossErr
	}
	return &service.CrossSignedRootPoolResponse{RootPEMs: s.rootPEMs}, nil
}

func (s *stubTrustService) AutoImportAIA(ctx context.Context, req *service.AutoImportAIARequest) (*service.AutoImportAIAResponse, error) {
	if s.importErr != nil {
		return nil, s.importErr
	}
	return &service.AutoImportAIAResponse{Imported: s.importedQt}, nil
}

func (s *stubTrustService) PublishDANE(ctx context.Context, req *service.PublishDANERequest) (*service.PublishDANEResponse, error) {
	if s.publishErr != nil {
		return nil, s.publishErr
	}
	return &service.PublishDANEResponse{Published: s.publishedQt}, nil
}

func TestServer_CrossSignedRootPool(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = &stubTrustService{rootPEMs: [][]byte{[]byte("root-a"), []byte("root-b")}}

		resp, err := srv.CrossSignedRootPool(context.Background(), &pb.CrossSignedRootPoolRequest{})
		require.NoError(t, err)
		require.Len(t, resp.RootPems, 2)
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = nil
		_, err := srv.CrossSignedRootPool(context.Background(), &pb.CrossSignedRootPoolRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = &stubTrustService{crossErr: stderrors.New("boom")}
		_, err := srv.CrossSignedRootPool(context.Background(), &pb.CrossSignedRootPoolRequest{})
		require.Error(t, err)
	})
}

func TestServer_AutoImportAIA(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = &stubTrustService{importedQt: 2}

		resp, err := srv.AutoImportAIA(context.Background(), &pb.AutoImportAIARequest{
			ChainDer: [][]byte{{0x30, 0x82}, {0x30, 0x82}},
		})
		require.NoError(t, err)
		assert.Equal(t, int32(2), resp.Imported)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.AutoImportAIA(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_chain", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.AutoImportAIA(context.Background(), &pb.AutoImportAIARequest{})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = nil
		_, err := srv.AutoImportAIA(context.Background(), &pb.AutoImportAIARequest{
			ChainDer: [][]byte{{0x30}},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = &stubTrustService{importErr: stderrors.New("import boom")}
		_, err := srv.AutoImportAIA(context.Background(), &pb.AutoImportAIARequest{
			ChainDer: [][]byte{{0x30}},
		})
		require.Error(t, err)
	})
}

func TestServer_PublishDANE(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = &stubTrustService{publishedQt: 4}

		resp, err := srv.PublishDANE(context.Background(), &pb.PublishDANERequest{
			Name: "kms.example.com", Port: 443, CertDer: []byte{0x30, 0x82},
		})
		require.NoError(t, err)
		assert.Equal(t, int32(4), resp.Published)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.PublishDANE(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = nil
		_, err := srv.PublishDANE(context.Background(), &pb.PublishDANERequest{
			Name: "kms.example.com", Port: 443, CertDer: []byte{0x30},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.TrustSvc = &stubTrustService{publishErr: stderrors.New("boom")}
		_, err := srv.PublishDANE(context.Background(), &pb.PublishDANERequest{
			Name: "kms.example.com", Port: 443, CertDer: []byte{0x30},
		})
		require.Error(t, err)
	})
}
