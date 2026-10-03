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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubAttestationService is a minimal service.AttestationService for the gRPC
// server unit tests. It records its input so the proto conversion can be
// asserted in both directions.
type stubAttestationService struct {
	// The embedded interface supplies the Verifier verbs this stub does not
	// exercise; they are tested against the real service elsewhere.
	service.AttestationService

	verifyErr      error
	verifyChainErr error
	attestErr      error

	lastVerify      *service.VerifyAttestationRequest
	lastVerifyChain *service.VerifyAttestationChainRequest
	lastAttest      *service.AttestContainerRequest
}

func (s *stubAttestationService) Verify(_ context.Context, req *service.VerifyAttestationRequest) (*service.VerifyAttestationResponse, error) {
	s.lastVerify = req
	if s.verifyErr != nil {
		return nil, s.verifyErr
	}
	return &service.VerifyAttestationResponse{
		Valid:          true,
		Format:         req.Format,
		Backend:        req.Backend,
		ChainValid:     true,
		SignatureValid: true,
		FreshnessValid: true,
	}, nil
}

func (s *stubAttestationService) VerifyChain(_ context.Context, req *service.VerifyAttestationChainRequest) (*service.VerifyAttestationChainResponse, error) {
	s.lastVerifyChain = req
	if s.verifyChainErr != nil {
		return nil, s.verifyChainErr
	}
	return &service.VerifyAttestationChainResponse{Valid: true}, nil
}

func (s *stubAttestationService) AttestContainer(_ context.Context, req *service.AttestContainerRequest) (*service.AttestContainerResponse, error) {
	s.lastAttest = req
	if s.attestErr != nil {
		return nil, s.attestErr
	}
	return &service.AttestContainerResponse{
		Attested:        true,
		DeviceID:        req.DeviceID,
		ContainerID:     req.ContainerID,
		Image:           req.Image,
		ImageDigest:     req.ImageDigest,
		ConfigDigest:    "sha256:cfg",
		TrustLabel:      req.TrustLabel,
		TrustLevel:      req.TrustLevel,
		Measured:        true,
		MeasurementPCR:  16,
		MeasurementBank: "sha256",
		Quoted:          []byte("tpms-attest"),
		Signature:       []byte("sig"),
		Nonce:           []byte("nonce"),
		PCRs:            map[uint32][]byte{7: {0x01}},
		AttestedAt:      time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}, nil
}

func TestServer_AttestationVerify(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubAttestationService{}
		mockSvc.AttestationSvc = stub

		resp, err := srv.Verify(context.Background(), &pb.VerifyAttestationRequest{
			Format:          "tpm2",
			Backend:         "tpm2",
			ChainPem:        [][]byte{[]byte("cert")},
			AttestedKeyDer:  []byte("key"),
			AttestationData: []byte("data"),
			Signature:       []byte("sig"),
			Pcrs:            map[uint32][]byte{7: {0x01}},
			Options: &pb.AttestationVerifyOptions{
				CheckFreshness:         true,
				FreshnessWindowSeconds: 120,
				AllowSelfSigned:        true,
			},
		})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChainValid)
		assert.True(t, resp.SignatureValid)
		assert.Equal(t, "tpm2", resp.Format)
		require.NotNil(t, resp.Timestamp)

		require.NotNil(t, stub.lastVerify)
		assert.Equal(t, []byte("data"), stub.lastVerify.AttestationData)
		assert.Len(t, stub.lastVerify.PCRs, 1)
		require.NotNil(t, stub.lastVerify.Options)
		assert.Equal(t, int64(120), stub.lastVerify.Options.FreshnessWindowSeconds)
		assert.True(t, stub.lastVerify.Options.AllowSelfSigned)
	})

	// A nil options message must stay nil so the service applies the xkms
	// secure defaults rather than an all-false struct that would silently
	// disable the freshness check.
	t.Run("nil_options_stay_nil", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubAttestationService{}
		mockSvc.AttestationSvc = stub

		_, err := srv.Verify(context.Background(), &pb.VerifyAttestationRequest{
			Format:   "tpm2",
			ChainPem: [][]byte{[]byte("cert")},
		})
		require.NoError(t, err)
		require.NotNil(t, stub.lastVerify)
		assert.Nil(t, stub.lastVerify.Options)
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = &stubAttestationService{}

		_, err := srv.Verify(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = nil

		_, err := srv.Verify(context.Background(), &pb.VerifyAttestationRequest{Format: "tpm2"})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = &stubAttestationService{verifyErr: stderrors.New("boom")}

		_, err := srv.Verify(context.Background(), &pb.VerifyAttestationRequest{Format: "tpm2"})
		require.Error(t, err)
	})
}

func TestServer_AttestationVerifyChain(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubAttestationService{}
		mockSvc.AttestationSvc = stub

		resp, err := srv.VerifyChain(context.Background(), &pb.VerifyAttestationChainRequest{
			Format:   "tpm2",
			ChainPem: [][]byte{[]byte("leaf"), []byte("root")},
		})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
		require.NotNil(t, stub.lastVerifyChain)
		assert.Len(t, stub.lastVerifyChain.ChainPEM, 2)
	})

	t.Run("empty_chain", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = &stubAttestationService{}

		_, err := srv.VerifyChain(context.Background(), &pb.VerifyAttestationChainRequest{Format: "tpm2"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = &stubAttestationService{}

		_, err := srv.VerifyChain(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = nil

		_, err := srv.VerifyChain(context.Background(), &pb.VerifyAttestationChainRequest{
			ChainPem: [][]byte{[]byte("leaf")},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = &stubAttestationService{verifyChainErr: stderrors.New("boom")}

		_, err := srv.VerifyChain(context.Background(), &pb.VerifyAttestationChainRequest{
			ChainPem: [][]byte{[]byte("leaf")},
		})
		require.Error(t, err)
	})
}

func TestServer_AttestContainer(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		stub := &stubAttestationService{}
		mockSvc.AttestationSvc = stub

		resp, err := srv.AttestContainer(context.Background(), &pb.AttestContainerRequest{
			DeviceId:        "device-1",
			ContainerId:     "container-1",
			Image:           "alpine@sha256:abc",
			ImageDigest:     "sha256:abc",
			TrustLabel:      "prod",
			TrustLevel:      "measured",
			ExpectedPcrs:    map[uint32][]byte{7: {0x01}},
			MeasurementPcr:  16,
			MeasurementBank: "sha256",
		})
		require.NoError(t, err)
		assert.True(t, resp.Attested)
		assert.True(t, resp.Measured)
		assert.Equal(t, "sha256:cfg", resp.ConfigDigest)
		assert.Equal(t, int32(16), resp.MeasurementPcr)
		assert.Len(t, resp.Pcrs, 1)
		require.NotNil(t, resp.AttestedAt)
		assert.Equal(t, time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC), resp.AttestedAt.AsTime().UTC())

		require.NotNil(t, stub.lastAttest)
		assert.Equal(t, "device-1", stub.lastAttest.DeviceID)
		assert.Equal(t, 16, stub.lastAttest.MeasurementPCR)
		assert.Len(t, stub.lastAttest.ExpectedPCRs, 1)
	})

	t.Run("missing_image", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = &stubAttestationService{}

		_, err := srv.AttestContainer(context.Background(), &pb.AttestContainerRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("nil_request", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = &stubAttestationService{}

		_, err := srv.AttestContainer(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = nil

		_, err := srv.AttestContainer(context.Background(), &pb.AttestContainerRequest{
			Image: "alpine@sha256:abc",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	// With no attestor registered the real service reports "not configured"
	// rather than a generic failure, and the gRPC layer must carry that
	// distinction through instead of flattening it.
	t.Run("no_attestor_registered", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = service.NewAttestationService(nil, nil)

		_, err := srv.AttestContainer(context.Background(), &pb.AttestContainerRequest{
			Image: "alpine@sha256:abc",
		})
		require.Error(t, err)
		assert.NotEqual(t, codes.OK, status.Code(err))
	})

	t.Run("service_error", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.AttestationSvc = &stubAttestationService{attestErr: stderrors.New("boom")}

		_, err := srv.AttestContainer(context.Background(), &pb.AttestContainerRequest{
			Image: "alpine@sha256:abc",
		})
		require.Error(t, err)
	})
}

func TestAttestationOptionsFromProto(t *testing.T) {
	t.Run("nil_stays_nil", func(t *testing.T) {
		assert.Nil(t, attestationOptionsFromProto(nil))
	})

	t.Run("all_fields_pass_through", func(t *testing.T) {
		opts := attestationOptionsFromProto(&pb.AttestationVerifyOptions{
			TrustedRootsPem:        [][]byte{[]byte("root")},
			CheckFreshness:         true,
			ExpectedNonce:          []byte("nonce"),
			FreshnessWindowSeconds: 42,
			VerifyPcrs:             true,
			ExpectedPcrs:           map[uint32][]byte{7: {0x01}},
			AllowSelfSigned:        true,
		})
		require.NotNil(t, opts)
		assert.Len(t, opts.TrustedRootsPEM, 1)
		assert.True(t, opts.CheckFreshness)
		assert.Equal(t, []byte("nonce"), opts.ExpectedNonce)
		assert.Equal(t, int64(42), opts.FreshnessWindowSeconds)
		assert.True(t, opts.VerifyPCRs)
		assert.Len(t, opts.ExpectedPCRs, 1)
		assert.True(t, opts.AllowSelfSigned)
	})
}

// TestAttestationGRPCPermissions pins the authz vocabulary at the descriptor
// level. grpcMethodPermissions is default-deny, so an unmapped RPC is
// unreachable; a WRONGLY mapped one is worse, because it is reachable with the
// wrong permission. Both are covered here.
func TestAttestationGRPCPermissions(t *testing.T) {
	want := map[string][2]string{
		"/quicbaas.attestation.v1.AttestationService/Verify":          {"attestation", "verify"},
		"/quicbaas.attestation.v1.AttestationService/VerifyChain":     {"attestation", "verify"},
		"/quicbaas.attestation.v1.AttestationService/AttestContainer": {"attestation", "attest"},

		// The Verifier: managing what it trusts is provision (Admin);
		// reading those records and appraising against them is verify.
		"/quicbaas.attestation.v1.AttestationService/PutEndorsement":        {"attestation", "provision"},
		"/quicbaas.attestation.v1.AttestationService/ListEndorsements":      {"attestation", "verify"},
		"/quicbaas.attestation.v1.AttestationService/DeleteEndorsement":     {"attestation", "provision"},
		"/quicbaas.attestation.v1.AttestationService/PutReferenceValues":    {"attestation", "provision"},
		"/quicbaas.attestation.v1.AttestationService/GetReferenceValues":    {"attestation", "verify"},
		"/quicbaas.attestation.v1.AttestationService/ListReferenceValues":   {"attestation", "verify"},
		"/quicbaas.attestation.v1.AttestationService/DeleteReferenceValues": {"attestation", "provision"},
		"/quicbaas.attestation.v1.AttestationService/AppraiseQuote":         {"attestation", "verify"},
	}

	for method, expected := range want {
		resource, action, mapped := grpcResourceAndAction(method)
		require.True(t, mapped, "method %s is unmapped and would be default-denied", method)
		assert.Equal(t, expected[0], resource, method)
		assert.Equal(t, expected[1], action, method)
		assert.False(t, isPublicGRPCMethod(method), "attestation must never be a public method: %s", method)
	}

	// Every RPC on the descriptor must appear in the expectation table, so
	// adding a verb without an authz decision fails here as well as in the
	// exhaustive descriptor guard.
	for _, method := range pb.AttestationService_ServiceDesc.Methods {
		full := "/" + pb.AttestationService_ServiceDesc.ServiceName + "/" + method.MethodName
		_, ok := want[full]
		assert.True(t, ok, "RPC %s has no authz expectation", full)
	}
	assert.Len(t, want, len(pb.AttestationService_ServiceDesc.Methods))
}

// TestAttestationServiceIsRegisteredForAuthzGuard proves the descriptor is in
// brokerServiceDescs. Without it TestGRPCPermissions_EveryMethodMapped would
// never walk these RPCs, and an unmapped verb could ship silently.
func TestAttestationServiceIsRegisteredForAuthzGuard(t *testing.T) {
	var found bool
	for _, desc := range brokerServiceDescs {
		if desc.ServiceName == pb.AttestationService_ServiceDesc.ServiceName {
			found = true
			break
		}
	}
	assert.True(t, found, "AttestationService must be listed in brokerServiceDescs")
}
