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

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// stubPIVService is a minimal xkmstransport.PIVService for the gRPC server
// unit tests. It records the last request per verb so the tests can pin the
// pb -> wire-struct conversion, and distinguishes Get/Export and
// Store/Import so verb routing is provable.
type stubPIVService struct {
	err error

	lastList   *xkmstransport.ListPIVSlotsRequest
	lastGet    *xkmstransport.GetPIVCertificateRequest
	lastStore  *xkmstransport.StorePIVCertificateRequest
	lastDelete *xkmstransport.DeletePIVCertificateRequest
	lastGenKey *xkmstransport.GeneratePIVKeyRequest
	lastGenCSR *xkmstransport.GeneratePIVCSRRequest

	exported bool
	imported bool
}

func (s *stubPIVService) ListPIVSlots(_ context.Context, req *xkmstransport.ListPIVSlotsRequest) (*xkmstransport.ListPIVSlotsResponse, error) {
	s.lastList = req
	if s.err != nil {
		return nil, s.err
	}
	return &xkmstransport.ListPIVSlotsResponse{
		Slots: []xkmstransport.PIVSlotStatus{{Slot: "9a", Name: "PIV Authentication", HasCert: true, Backend: req.Backend}},
	}, nil
}

func (s *stubPIVService) GetPIVCertificate(_ context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	s.lastGet = req
	if s.err != nil {
		return nil, s.err
	}
	return &xkmstransport.GetPIVCertificateResponse{Slot: req.Slot, Certificate: []byte("cert-pem"), Format: req.Format}, nil
}

func (s *stubPIVService) StorePIVCertificate(_ context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	s.lastStore = req
	return s.err
}

func (s *stubPIVService) DeletePIVCertificate(_ context.Context, req *xkmstransport.DeletePIVCertificateRequest) error {
	s.lastDelete = req
	return s.err
}

func (s *stubPIVService) GeneratePIVKey(_ context.Context, req *xkmstransport.GeneratePIVKeyRequest) (*xkmstransport.GeneratePIVKeyResponse, error) {
	s.lastGenKey = req
	if s.err != nil {
		return nil, s.err
	}
	return &xkmstransport.GeneratePIVKeyResponse{Slot: req.Slot, Certificate: []byte("cert"), PublicKey: []byte("pub")}, nil
}

func (s *stubPIVService) ImportPIVCertificate(_ context.Context, req *xkmstransport.StorePIVCertificateRequest) error {
	s.imported = true
	s.lastStore = req
	return s.err
}

func (s *stubPIVService) ExportPIVCertificate(_ context.Context, req *xkmstransport.GetPIVCertificateRequest) (*xkmstransport.GetPIVCertificateResponse, error) {
	s.exported = true
	return s.GetPIVCertificate(context.Background(), req)
}

func (s *stubPIVService) GeneratePIVCSR(_ context.Context, req *xkmstransport.GeneratePIVCSRRequest) (*xkmstransport.GeneratePIVCSRResponse, error) {
	s.lastGenCSR = req
	if s.err != nil {
		return nil, s.err
	}
	return &xkmstransport.GeneratePIVCSRResponse{Slot: req.Slot, CSR: []byte("csr-pem")}, nil
}

func TestServer_ListPIVSlots(t *testing.T) {
	t.Run("success_plumbs_backend_and_pagination", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubPIVService{}
		mockSvc.PIVSvc = stub

		resp, err := srv.ListPIVSlots(context.Background(), &pb.PIVListSlotsRequest{
			Backend:  "file",
			TenantId: "tenant-a",
			Page:     &pb.PIVPageRequest{Page: 2, PageSize: 10},
		})
		require.NoError(t, err)
		require.Len(t, resp.Slots, 1)
		assert.Equal(t, "9a", resp.Slots[0].Slot)
		assert.True(t, resp.Slots[0].HasCert)
		require.NotNil(t, stub.lastList)
		assert.Equal(t, "file", stub.lastList.Backend)
		assert.Equal(t, "tenant-a", stub.lastList.TenantID)
		assert.Equal(t, 2, stub.lastList.Page)
		assert.Equal(t, 10, stub.lastList.PageSize)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.ListPIVSlots(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.PIVSvc = &stubPIVService{err: stderrors.New("boom")}
		_, err := srv.ListPIVSlots(context.Background(), &pb.PIVListSlotsRequest{Backend: "file"})
		require.Error(t, err)
	})
}

func TestServer_GetAndExportPIVCertificate(t *testing.T) {
	t.Run("get_success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubPIVService{}
		mockSvc.PIVSvc = stub

		resp, err := srv.GetPIVCertificate(context.Background(), &pb.PIVGetCertificateRequest{
			Backend: "file", Slot: "9a", Format: "pem",
		})
		require.NoError(t, err)
		assert.Equal(t, "9a", resp.Slot)
		assert.Equal(t, []byte("cert-pem"), resp.Certificate)
		assert.False(t, stub.exported, "GetPIVCertificate must not route to Export")
	})

	t.Run("export_routes_to_export_verb", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubPIVService{}
		mockSvc.PIVSvc = stub

		_, err := srv.ExportPIVCertificate(context.Background(), &pb.PIVGetCertificateRequest{
			Backend: "file", Slot: "9a", Format: "pem",
		})
		require.NoError(t, err)
		assert.True(t, stub.exported)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GetPIVCertificate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.PIVSvc = &stubPIVService{err: stderrors.New("boom")}
		_, err := srv.GetPIVCertificate(context.Background(), &pb.PIVGetCertificateRequest{Backend: "file", Slot: "9a"})
		require.Error(t, err)
	})
}

func TestServer_StoreAndImportPIVCertificate(t *testing.T) {
	t.Run("store_success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubPIVService{}
		mockSvc.PIVSvc = stub

		_, err := srv.StorePIVCertificate(context.Background(), &pb.PIVStoreCertificateRequest{
			Backend: "file", Slot: "9c", Certificate: []byte("cert"), Format: "pem",
		})
		require.NoError(t, err)
		require.NotNil(t, stub.lastStore)
		assert.Equal(t, "9c", stub.lastStore.Slot)
		assert.False(t, stub.imported, "StorePIVCertificate must not route to Import")
	})

	t.Run("import_routes_to_import_verb", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubPIVService{}
		mockSvc.PIVSvc = stub

		_, err := srv.ImportPIVCertificate(context.Background(), &pb.PIVStoreCertificateRequest{
			Backend: "file", Slot: "9c", Certificate: []byte("cert"), Format: "pem",
		})
		require.NoError(t, err)
		assert.True(t, stub.imported)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.StorePIVCertificate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.PIVSvc = &stubPIVService{err: stderrors.New("boom")}
		_, err := srv.StorePIVCertificate(context.Background(), &pb.PIVStoreCertificateRequest{Backend: "file", Slot: "9c"})
		require.Error(t, err)
	})
}

func TestServer_DeletePIVCertificate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubPIVService{}
		mockSvc.PIVSvc = stub

		_, err := srv.DeletePIVCertificate(context.Background(), &pb.PIVDeleteCertificateRequest{Backend: "file", Slot: "9a"})
		require.NoError(t, err)
		require.NotNil(t, stub.lastDelete)
		assert.Equal(t, "9a", stub.lastDelete.Slot)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.DeletePIVCertificate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.PIVSvc = &stubPIVService{err: stderrors.New("boom")}
		_, err := srv.DeletePIVCertificate(context.Background(), &pb.PIVDeleteCertificateRequest{Backend: "file", Slot: "9a"})
		require.Error(t, err)
	})
}

func TestServer_GeneratePIVKey(t *testing.T) {
	t.Run("success_plumbs_all_fields", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubPIVService{}
		mockSvc.PIVSvc = stub

		resp, err := srv.GeneratePIVKey(context.Background(), &pb.PIVGenerateKeyRequest{
			Backend:      "tpm2",
			Slot:         "9a",
			Algorithm:    "ecdsap256",
			Subject:      "CN=alice",
			OutputFormat: "csr",
			ValidityDays: 30,
		})
		require.NoError(t, err)
		assert.Equal(t, "9a", resp.Slot)
		require.NotNil(t, stub.lastGenKey)
		assert.Equal(t, "tpm2", stub.lastGenKey.Backend)
		assert.Equal(t, "ecdsap256", stub.lastGenKey.Algorithm)
		assert.Equal(t, "csr", stub.lastGenKey.OutputFormat)
		assert.Equal(t, 30, stub.lastGenKey.ValidityDays)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GeneratePIVKey(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.PIVSvc = &stubPIVService{err: stderrors.New("boom")}
		_, err := srv.GeneratePIVKey(context.Background(), &pb.PIVGenerateKeyRequest{Backend: "file", Slot: "9a"})
		require.Error(t, err)
	})
}

func TestServer_GeneratePIVCSR(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubPIVService{}
		mockSvc.PIVSvc = stub

		resp, err := srv.GeneratePIVCSR(context.Background(), &pb.PIVGenerateCSRRequest{
			Backend: "file", Slot: "9a", Subject: "CN=alice", Organization: "ACME",
		})
		require.NoError(t, err)
		assert.Equal(t, []byte("csr-pem"), resp.Csr)
		require.NotNil(t, stub.lastGenCSR)
		assert.Equal(t, "ACME", stub.lastGenCSR.Organization)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GeneratePIVCSR(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.PIVSvc = &stubPIVService{err: stderrors.New("boom")}
		_, err := srv.GeneratePIVCSR(context.Background(), &pb.PIVGenerateCSRRequest{Backend: "file", Slot: "9a"})
		require.Error(t, err)
	})
}

// TestServer_PIVServiceUnavailable pins the Unimplemented mapping when the
// facade has no PIV sub-service (nil mock default).
func TestServer_PIVServiceUnavailable(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.PIVSvc = nil

	_, err := srv.ListPIVSlots(context.Background(), &pb.PIVListSlotsRequest{Backend: "file"})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}
