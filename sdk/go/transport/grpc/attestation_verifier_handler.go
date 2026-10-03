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

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// verifierHandler serves one Verifier verb over the generated
// AttestationService client.
type verifierHandler func(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error)

// attestationVerifierHandlers dispatches the Verifier verbs.
var attestationVerifierHandlers = map[string]verifierHandler{
	"PutEndorsement":        verifierPutEndorsement,
	"ListEndorsements":      verifierListEndorsements,
	"DeleteEndorsement":     verifierDeleteEndorsement,
	"PutReferenceValues":    verifierPutReferenceValues,
	"GetReferenceValues":    verifierGetReferenceValues,
	"ListReferenceValues":   verifierListReferenceValues,
	"DeleteReferenceValues": verifierDeleteReferenceValues,
	"AppraiseQuote":         verifierAppraiseQuote,
}

func invalidVerifierRequest(method, want string) error {
	return errors.NewInvalidInput("attestationHandler."+method, "invalid request type, expected "+want)
}

func verifierPutEndorsement(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.PutEndorsementRequest)
	if !ok || r == nil {
		return nil, invalidVerifierRequest("PutEndorsement", "*attestation.PutEndorsementRequest")
	}
	e, err := client.PutEndorsement(ctx, &pb.PutEndorsementRequest{Name: r.Name, Kind: r.Kind, CertificatePem: r.CertificatePEM, PublicKeyPem: r.PublicKeyPEM})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.PutEndorsement")
	}
	return endorsementFromProto(e), nil
}

func verifierListEndorsements(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.ListEndorsementsRequest)
	if !ok || r == nil {
		return nil, invalidVerifierRequest("ListEndorsements", "*attestation.ListEndorsementsRequest")
	}
	resp, err := client.ListEndorsements(ctx, &pb.ListEndorsementsRequest{Kind: r.Kind})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.ListEndorsements")
	}
	out := &attestationclient.ListEndorsementsResponse{Endorsements: make([]*attestationclient.Endorsement, 0, len(resp.Endorsements))}
	for _, e := range resp.Endorsements {
		out.Endorsements = append(out.Endorsements, endorsementFromProto(e))
	}
	return out, nil
}

func verifierDeleteEndorsement(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, invalidVerifierRequest("DeleteEndorsement", "*attestation.NameRequest")
	}
	if _, err := client.DeleteEndorsement(ctx, &pb.DeleteEndorsementRequest{Name: r.Name}); err != nil {
		return nil, convertGRPCError(err, "attestationHandler.DeleteEndorsement")
	}
	return nil, nil
}

func verifierPutReferenceValues(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.PutReferenceValuesRequest)
	if !ok || r == nil {
		return nil, invalidVerifierRequest("PutReferenceValues", "*attestation.PutReferenceValuesRequest")
	}
	rv, err := client.PutReferenceValues(ctx, &pb.PutReferenceValuesRequest{Name: r.Name, SignedCorim: r.SignedCoRIM})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.PutReferenceValues")
	}
	return referenceValuesFromProto(rv), nil
}

func verifierGetReferenceValues(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, invalidVerifierRequest("GetReferenceValues", "*attestation.NameRequest")
	}
	rv, err := client.GetReferenceValues(ctx, &pb.GetReferenceValuesRequest{Name: r.Name})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.GetReferenceValues")
	}
	return referenceValuesFromProto(rv), nil
}

func verifierListReferenceValues(ctx context.Context, client pb.AttestationServiceClient, _ interface{}) (interface{}, error) {
	resp, err := client.ListReferenceValues(ctx, &pb.ListReferenceValuesRequest{})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.ListReferenceValues")
	}
	out := &attestationclient.ListReferenceValuesResponse{ReferenceValues: make([]*attestationclient.ReferenceValues, 0, len(resp.ReferenceValues))}
	for _, rv := range resp.ReferenceValues {
		out.ReferenceValues = append(out.ReferenceValues, referenceValuesFromProto(rv))
	}
	return out, nil
}

func verifierDeleteReferenceValues(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, invalidVerifierRequest("DeleteReferenceValues", "*attestation.NameRequest")
	}
	if _, err := client.DeleteReferenceValues(ctx, &pb.DeleteReferenceValuesRequest{Name: r.Name}); err != nil {
		return nil, convertGRPCError(err, "attestationHandler.DeleteReferenceValues")
	}
	return nil, nil
}

func verifierAppraiseQuote(ctx context.Context, client pb.AttestationServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.AppraiseQuoteRequest)
	if !ok || r == nil {
		return nil, invalidVerifierRequest("AppraiseQuote", "*attestation.AppraiseQuoteRequest")
	}
	result, err := client.AppraiseQuote(ctx, &pb.AppraiseQuoteRequest{
		AkCertificatePem: r.AKCertificatePEM,
		Quote:            r.Quote,
		Signature:        r.Signature,
		Nonce:            r.Nonce,
		Pcrs:             r.PCRs,
		EventLog:         r.EventLog,
		ReferenceValues:  r.ReferenceValues,
	})
	if err != nil {
		return nil, convertGRPCError(err, "attestationHandler.AppraiseQuote")
	}
	out := &attestationclient.AttestationResult{
		Verified:             result.Verified,
		CryptographicFailure: result.CryptographicFailure,
		Checks:               make([]attestationclient.AppraisalCheck, 0, len(result.Checks)),
		ReferenceValues:      result.ReferenceValues,
		ManifestID:           result.ManifestId,
		Signer:               result.Signer,
		BaselineSource:       result.BaselineSource,
		AppraisedAt:          rfc3339(result.AppraisedAt),
	}
	for _, check := range result.Checks {
		out.Checks = append(out.Checks, attestationclient.AppraisalCheck{Name: check.Name, Outcome: check.Outcome, Reason: check.Reason})
	}
	return out, nil
}

func endorsementFromProto(e *pb.Endorsement) *attestationclient.Endorsement {
	return &attestationclient.Endorsement{
		Name:           e.Name,
		Kind:           e.Kind,
		CertificatePEM: e.CertificatePem,
		PublicKeyPEM:   e.PublicKeyPem,
		CreatedAt:      rfc3339(e.CreatedAt),
	}
}

func referenceValuesFromProto(rv *pb.ReferenceValues) *attestationclient.ReferenceValues {
	return &attestationclient.ReferenceValues{
		Name:        rv.Name,
		ManifestID:  rv.ManifestId,
		Profile:     rv.Profile,
		Signer:      rv.Signer,
		NotBefore:   rfc3339(rv.NotBefore),
		NotAfter:    rfc3339(rv.NotAfter),
		PCRIndices:  rv.PcrIndices,
		StoredAt:    rfc3339(rv.StoredAt),
		SignedCoRIM: rv.SignedCorim,
	}
}

// rfc3339 renders a proto timestamp in the form the JSON transports carry,
// leaving an unset one empty.
func rfc3339(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339Nano)
}
