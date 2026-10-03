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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The attestation Verifier's verbs on the AttestationService: endorsements,
// signed reference values and quote appraisal. Each handler converts the
// proto messages and delegates to service.AttestationService.

// attestationVerifier returns the attestation service or Unimplemented.
func (s *Server) attestationVerifier() (service.AttestationService, error) {
	svc := s.service.Attestation()
	if svc == nil {
		return nil, status.Error(codes.Unimplemented, "attestation service not available")
	}
	return svc, nil
}

// PutEndorsement stores a Verifier trust anchor.
func (s *Server) PutEndorsement(ctx context.Context, req *pb.PutEndorsementRequest) (*pb.Endorsement, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	svc, err := s.attestationVerifier()
	if err != nil {
		return nil, err
	}
	e, err := svc.PutEndorsement(ctx, &service.PutEndorsementRequest{
		Name:           req.Name,
		Kind:           service.EndorsementKind(req.Kind),
		CertificatePEM: req.CertificatePem,
		PublicKeyPEM:   req.PublicKeyPem,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	return endorsementToProto(e), nil
}

// ListEndorsements lists the tenant's endorsements.
func (s *Server) ListEndorsements(ctx context.Context, req *pb.ListEndorsementsRequest) (*pb.ListEndorsementsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	svc, err := s.attestationVerifier()
	if err != nil {
		return nil, err
	}
	resp, err := svc.ListEndorsements(ctx, &service.ListEndorsementsRequest{Kind: service.EndorsementKind(req.Kind)})
	if err != nil {
		return nil, toProtoError(err)
	}
	out := &pb.ListEndorsementsResponse{Endorsements: make([]*pb.Endorsement, 0, len(resp.Endorsements))}
	for _, e := range resp.Endorsements {
		out.Endorsements = append(out.Endorsements, endorsementToProto(e))
	}
	return out, nil
}

// DeleteEndorsement removes an endorsement.
func (s *Server) DeleteEndorsement(ctx context.Context, req *pb.DeleteEndorsementRequest) (*pb.DeleteEndorsementResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	svc, err := s.attestationVerifier()
	if err != nil {
		return nil, err
	}
	if err := svc.DeleteEndorsement(ctx, &service.DeleteEndorsementRequest{Name: req.Name}); err != nil {
		return nil, toProtoError(err)
	}
	return &pb.DeleteEndorsementResponse{}, nil
}

// PutReferenceValues stores endorsed, signed reference values.
func (s *Server) PutReferenceValues(ctx context.Context, req *pb.PutReferenceValuesRequest) (*pb.ReferenceValues, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	svc, err := s.attestationVerifier()
	if err != nil {
		return nil, err
	}
	rv, err := svc.PutReferenceValues(ctx, &service.PutReferenceValuesRequest{Name: req.Name, SignedCoRIM: req.SignedCorim})
	if err != nil {
		return nil, toProtoError(err)
	}
	return referenceValuesToProto(rv), nil
}

// GetReferenceValues returns stored reference values.
func (s *Server) GetReferenceValues(ctx context.Context, req *pb.GetReferenceValuesRequest) (*pb.ReferenceValues, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	svc, err := s.attestationVerifier()
	if err != nil {
		return nil, err
	}
	rv, err := svc.GetReferenceValues(ctx, &service.GetReferenceValuesRequest{Name: req.Name})
	if err != nil {
		return nil, toProtoError(err)
	}
	return referenceValuesToProto(rv), nil
}

// ListReferenceValues lists stored reference values.
func (s *Server) ListReferenceValues(ctx context.Context, req *pb.ListReferenceValuesRequest) (*pb.ListReferenceValuesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	svc, err := s.attestationVerifier()
	if err != nil {
		return nil, err
	}
	resp, err := svc.ListReferenceValues(ctx)
	if err != nil {
		return nil, toProtoError(err)
	}
	out := &pb.ListReferenceValuesResponse{ReferenceValues: make([]*pb.ReferenceValues, 0, len(resp.ReferenceValues))}
	for _, rv := range resp.ReferenceValues {
		out.ReferenceValues = append(out.ReferenceValues, referenceValuesToProto(rv))
	}
	return out, nil
}

// DeleteReferenceValues removes stored reference values.
func (s *Server) DeleteReferenceValues(ctx context.Context, req *pb.DeleteReferenceValuesRequest) (*pb.DeleteReferenceValuesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	svc, err := s.attestationVerifier()
	if err != nil {
		return nil, err
	}
	if err := svc.DeleteReferenceValues(ctx, &service.DeleteReferenceValuesRequest{Name: req.Name}); err != nil {
		return nil, toProtoError(err)
	}
	return &pb.DeleteReferenceValuesResponse{}, nil
}

// AppraiseQuote appraises TPM quote evidence.
func (s *Server) AppraiseQuote(ctx context.Context, req *pb.AppraiseQuoteRequest) (*pb.AttestationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	svc, err := s.attestationVerifier()
	if err != nil {
		return nil, err
	}
	result, err := svc.AppraiseQuote(ctx, &service.AppraiseQuoteRequest{
		AKCertificatePEM: req.AkCertificatePem,
		Quote:            req.Quote,
		Signature:        req.Signature,
		Nonce:            req.Nonce,
		PCRs:             req.Pcrs,
		EventLog:         req.EventLog,
		ReferenceValues:  req.ReferenceValues,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	out := &pb.AttestationResult{
		Verified:             result.Verified,
		CryptographicFailure: result.CryptographicFailure,
		Checks:               make([]*pb.AppraisalCheck, 0, len(result.Checks)),
		ReferenceValues:      result.ReferenceValues,
		ManifestId:           result.ManifestID,
		Signer:               result.Signer,
		BaselineSource:       result.BaselineSource,
		AppraisedAt:          optionalTimestamp(result.AppraisedAt),
	}
	for _, check := range result.Checks {
		out.Checks = append(out.Checks, &pb.AppraisalCheck{Name: check.Name, Outcome: string(check.Outcome), Reason: check.Reason})
	}
	return out, nil
}

func endorsementToProto(e *service.Endorsement) *pb.Endorsement {
	return &pb.Endorsement{
		Name:           e.Name,
		Kind:           string(e.Kind),
		CertificatePem: e.CertificatePEM,
		PublicKeyPem:   e.PublicKeyPEM,
		CreatedAt:      optionalTimestamp(e.CreatedAt),
	}
}

func referenceValuesToProto(rv *service.ReferenceValues) *pb.ReferenceValues {
	return &pb.ReferenceValues{
		Name:        rv.Name,
		ManifestId:  rv.ManifestID,
		Profile:     rv.Profile,
		Signer:      rv.Signer,
		NotBefore:   optionalTimestamp(rv.NotBefore),
		NotAfter:    optionalTimestamp(rv.NotAfter),
		PcrIndices:  rv.PCRIndices,
		StoredAt:    optionalTimestamp(rv.StoredAt),
		SignedCorim: rv.SignedCoRIM,
	}
}

// optionalTimestamp leaves an unset time unset on the wire.
func optionalTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
