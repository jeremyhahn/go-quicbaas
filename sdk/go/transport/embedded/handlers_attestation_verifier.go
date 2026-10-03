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

package embedded

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
)

// The attestation Verifier on the in-process embedded transport: the handlers
// call the same service.AttestationService every wire transport serves and
// convert to the leaf client/attestation types.

const attestationService = "quicbaas.Attestation"

// attestationOf returns the attestation service or the not-configured error.
func attestationOf(svc service.QuicBaaS) (service.AttestationService, error) {
	a := svc.Attestation()
	if a == nil {
		return nil, NewServiceNotConfiguredError("Attestation")
	}
	return a, nil
}

func handleAttestationPutEndorsement(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	a, err := attestationOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*attestationclient.PutEndorsementRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(attestationService, "PutEndorsement", "*attestation.PutEndorsementRequest", typeNameOf(req))
	}
	e, err := a.PutEndorsement(ctx, &service.PutEndorsementRequest{
		Name: r.Name, Kind: service.EndorsementKind(r.Kind), CertificatePEM: r.CertificatePEM, PublicKeyPEM: r.PublicKeyPEM,
	})
	if err != nil {
		return nil, err
	}
	return sdkEndorsement(e), nil
}

func handleAttestationListEndorsements(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	a, err := attestationOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*attestationclient.ListEndorsementsRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(attestationService, "ListEndorsements", "*attestation.ListEndorsementsRequest", typeNameOf(req))
	}
	resp, err := a.ListEndorsements(ctx, &service.ListEndorsementsRequest{Kind: service.EndorsementKind(r.Kind)})
	if err != nil {
		return nil, err
	}
	out := &attestationclient.ListEndorsementsResponse{Endorsements: make([]*attestationclient.Endorsement, 0, len(resp.Endorsements))}
	for _, e := range resp.Endorsements {
		out.Endorsements = append(out.Endorsements, sdkEndorsement(e))
	}
	return out, nil
}

func handleAttestationDeleteEndorsement(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	a, err := attestationOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(attestationService, "DeleteEndorsement", "*attestation.NameRequest", typeNameOf(req))
	}
	return nil, a.DeleteEndorsement(ctx, &service.DeleteEndorsementRequest{Name: r.Name})
}

func handleAttestationPutReferenceValues(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	a, err := attestationOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*attestationclient.PutReferenceValuesRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(attestationService, "PutReferenceValues", "*attestation.PutReferenceValuesRequest", typeNameOf(req))
	}
	rv, err := a.PutReferenceValues(ctx, &service.PutReferenceValuesRequest{Name: r.Name, SignedCoRIM: r.SignedCoRIM})
	if err != nil {
		return nil, err
	}
	return sdkReferenceValues(rv), nil
}

func handleAttestationGetReferenceValues(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	a, err := attestationOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(attestationService, "GetReferenceValues", "*attestation.NameRequest", typeNameOf(req))
	}
	rv, err := a.GetReferenceValues(ctx, &service.GetReferenceValuesRequest{Name: r.Name})
	if err != nil {
		return nil, err
	}
	return sdkReferenceValues(rv), nil
}

func handleAttestationListReferenceValues(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	a, err := attestationOf(svc)
	if err != nil {
		return nil, err
	}
	resp, err := a.ListReferenceValues(ctx)
	if err != nil {
		return nil, err
	}
	out := &attestationclient.ListReferenceValuesResponse{ReferenceValues: make([]*attestationclient.ReferenceValues, 0, len(resp.ReferenceValues))}
	for _, rv := range resp.ReferenceValues {
		out.ReferenceValues = append(out.ReferenceValues, sdkReferenceValues(rv))
	}
	return out, nil
}

func handleAttestationDeleteReferenceValues(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	a, err := attestationOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(attestationService, "DeleteReferenceValues", "*attestation.NameRequest", typeNameOf(req))
	}
	return nil, a.DeleteReferenceValues(ctx, &service.DeleteReferenceValuesRequest{Name: r.Name})
}

func handleAttestationAppraiseQuote(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	a, err := attestationOf(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*attestationclient.AppraiseQuoteRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(attestationService, "AppraiseQuote", "*attestation.AppraiseQuoteRequest", typeNameOf(req))
	}
	result, err := a.AppraiseQuote(ctx, &service.AppraiseQuoteRequest{
		AKCertificatePEM: r.AKCertificatePEM, Quote: r.Quote, Signature: r.Signature, Nonce: r.Nonce,
		PCRs: r.PCRs, EventLog: r.EventLog, ReferenceValues: r.ReferenceValues,
	})
	if err != nil {
		return nil, err
	}
	out := &attestationclient.AttestationResult{
		Verified:             result.Verified,
		CryptographicFailure: result.CryptographicFailure,
		Checks:               make([]attestationclient.AppraisalCheck, 0, len(result.Checks)),
		ReferenceValues:      result.ReferenceValues,
		ManifestID:           result.ManifestID,
		Signer:               result.Signer,
		BaselineSource:       result.BaselineSource,
		AppraisedAt:          rfc3339(result.AppraisedAt),
	}
	for _, check := range result.Checks {
		out.Checks = append(out.Checks, attestationclient.AppraisalCheck{Name: check.Name, Outcome: string(check.Outcome), Reason: check.Reason})
	}
	return out, nil
}

func sdkEndorsement(e *service.Endorsement) *attestationclient.Endorsement {
	return &attestationclient.Endorsement{
		Name: e.Name, Kind: string(e.Kind), CertificatePEM: e.CertificatePEM, PublicKeyPEM: e.PublicKeyPEM, CreatedAt: rfc3339(e.CreatedAt),
	}
}

func sdkReferenceValues(rv *service.ReferenceValues) *attestationclient.ReferenceValues {
	return &attestationclient.ReferenceValues{
		Name: rv.Name, ManifestID: rv.ManifestID, Profile: rv.Profile, Signer: rv.Signer,
		NotBefore: rfc3339(rv.NotBefore), NotAfter: rfc3339(rv.NotAfter), PCRIndices: rv.PCRIndices,
		StoredAt: rfc3339(rv.StoredAt), SignedCoRIM: rv.SignedCoRIM,
	}
}

// rfc3339 renders a time as the JSON transports carry it; a zero time is empty.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
