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

// stubFIDO2Service is a minimal service.FIDO2Service for the gRPC server
// unit tests. It records the tenant each verb received so the tests can pin
// the proto tenant_id -> service tenant plumbing.
type stubFIDO2Service struct {
	err        error
	lastTenant string
}

func (s *stubFIDO2Service) BeginRegistration(_ context.Context, tenantID string, req *xkmstransport.BeginRegistrationRequest) (*xkmstransport.BeginRegistrationResponse, error) {
	s.lastTenant = tenantID
	if s.err != nil {
		return nil, s.err
	}
	return &xkmstransport.BeginRegistrationResponse{SessionID: "sess-1", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (s *stubFIDO2Service) FinishRegistration(_ context.Context, tenantID string, req *xkmstransport.FinishRegistrationRequest) (*xkmstransport.FinishRegistrationResponse, error) {
	s.lastTenant = tenantID
	if s.err != nil {
		return nil, s.err
	}
	return &xkmstransport.FinishRegistrationResponse{Success: true, CredentialID: "cred-1", Token: "tok-1"}, nil
}

func (s *stubFIDO2Service) BeginAuthentication(_ context.Context, tenantID string, req *xkmstransport.BeginAuthenticationRequest) (*xkmstransport.BeginAuthenticationResponse, error) {
	s.lastTenant = tenantID
	if s.err != nil {
		return nil, s.err
	}
	return &xkmstransport.BeginAuthenticationResponse{SessionID: "sess-2", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (s *stubFIDO2Service) FinishAuthentication(_ context.Context, tenantID string, req *xkmstransport.FinishAuthenticationRequest) (*xkmstransport.FinishAuthenticationResponse, error) {
	s.lastTenant = tenantID
	if s.err != nil {
		return nil, s.err
	}
	return &xkmstransport.FinishAuthenticationResponse{Success: true, Token: "tok-2", Username: "alice"}, nil
}

func TestServer_FIDO2BeginRegistration(t *testing.T) {
	t.Run("success_plumbs_tenant_and_fields", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubFIDO2Service{}
		mockSvc.FIDO2Svc = stub

		resp, err := srv.BeginRegistration(context.Background(), &pb.FIDO2BeginRegistrationRequest{
			TenantId:    "tenant-a",
			Username:    "alice@example.com",
			DisplayName: "Alice",
		})
		require.NoError(t, err)
		assert.Equal(t, "sess-1", resp.SessionId)
		assert.Equal(t, `{"publicKey":{}}`, resp.OptionsJson)
		assert.Equal(t, "tenant-a", stub.lastTenant)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.BeginRegistration(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.FIDO2Svc = &stubFIDO2Service{err: stderrors.New("boom")}
		_, err := srv.BeginRegistration(context.Background(), &pb.FIDO2BeginRegistrationRequest{TenantId: "t", Username: "u"})
		require.Error(t, err)
	})
}

func TestServer_FIDO2FinishRegistration(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubFIDO2Service{}
		mockSvc.FIDO2Svc = stub

		resp, err := srv.FinishRegistration(context.Background(), &pb.FIDO2FinishRegistrationRequest{
			TenantId:        "tenant-a",
			SessionId:       "sess-1",
			AttestationJson: `{"id":"cred-1"}`,
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, "cred-1", resp.CredentialId)
		assert.Equal(t, "tok-1", resp.Token)
		assert.Equal(t, "tenant-a", stub.lastTenant)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.FinishRegistration(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.FIDO2Svc = &stubFIDO2Service{err: stderrors.New("boom")}
		_, err := srv.FinishRegistration(context.Background(), &pb.FIDO2FinishRegistrationRequest{TenantId: "t", SessionId: "s"})
		require.Error(t, err)
	})
}

func TestServer_FIDO2BeginAuthentication(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubFIDO2Service{}
		mockSvc.FIDO2Svc = stub

		resp, err := srv.BeginAuthentication(context.Background(), &pb.FIDO2BeginAuthenticationRequest{
			TenantId: "tenant-b",
			Username: "alice@example.com",
		})
		require.NoError(t, err)
		assert.Equal(t, "sess-2", resp.SessionId)
		assert.Equal(t, "tenant-b", stub.lastTenant)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.BeginAuthentication(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.FIDO2Svc = &stubFIDO2Service{err: stderrors.New("boom")}
		_, err := srv.BeginAuthentication(context.Background(), &pb.FIDO2BeginAuthenticationRequest{TenantId: "t", Username: "u"})
		require.Error(t, err)
	})
}

func TestServer_FIDO2FinishAuthentication(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubFIDO2Service{}
		mockSvc.FIDO2Svc = stub

		resp, err := srv.FinishAuthentication(context.Background(), &pb.FIDO2FinishAuthenticationRequest{
			TenantId:      "tenant-b",
			SessionId:     "sess-2",
			Username:      "alice@example.com",
			AssertionJson: `{"id":"cred-1"}`,
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, "tok-2", resp.Token)
		assert.Equal(t, "alice", resp.Username)
		assert.Equal(t, "tenant-b", stub.lastTenant)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.FinishAuthentication(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.FIDO2Svc = &stubFIDO2Service{err: stderrors.New("boom")}
		_, err := srv.FinishAuthentication(context.Background(), &pb.FIDO2FinishAuthenticationRequest{TenantId: "t", SessionId: "s"})
		require.Error(t, err)
	})
}

// TestServer_FIDO2ServiceUnavailable pins the Unimplemented mapping when the
// facade has no FIDO2 sub-service (nil mock default).
func TestServer_FIDO2ServiceUnavailable(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.FIDO2Svc = nil

	_, err := srv.BeginRegistration(context.Background(), &pb.FIDO2BeginRegistrationRequest{TenantId: "t", Username: "u"})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}
