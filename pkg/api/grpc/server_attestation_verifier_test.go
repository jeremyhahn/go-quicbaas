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
	"crypto/x509"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestServer_AttestationVerifierEndToEnd drives every Verifier RPC through
// the gRPC handlers into the real service with real evidence.
func TestServer_AttestationVerifierEndToEnd(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	store := attestationtest.NewMemoryStore()
	mockSvc.AttestationSvc = service.NewAttestationService(nil, nil, service.WithAttestationStore(store))
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	akCA, err := attestationtest.NewAuthority("AK CA")
	require.NoError(t, err)
	rvCA, err := attestationtest.NewAuthority("RV CA")
	require.NoError(t, err)
	signerCert, signerKey, err := attestationtest.SignerUnder(rvCA)
	require.NoError(t, err)
	ak, err := attestationtest.IssueAttestationKey(akCA, true)
	require.NoError(t, err)
	pcrs := attestationtest.PCRValues(0, 7)
	corim, err := attestationtest.SignCoRIM(pcrs, signerKey, []*x509.Certificate{signerCert})
	require.NoError(t, err)

	e, err := srv.PutEndorsement(ctx, &pb.PutEndorsementRequest{Name: "ak-ca", Kind: "ak-root", CertificatePem: akCA.CertificatePEM()})
	require.NoError(t, err)
	assert.Equal(t, "ak-root", e.Kind)
	assert.NotNil(t, e.CreatedAt)
	_, err = srv.PutEndorsement(ctx, &pb.PutEndorsementRequest{Name: "rv-ca", Kind: "reference-signer", CertificatePem: rvCA.CertificatePEM()})
	require.NoError(t, err)

	listed, err := srv.ListEndorsements(ctx, &pb.ListEndorsementsRequest{Kind: "reference-signer"})
	require.NoError(t, err)
	require.Len(t, listed.Endorsements, 1)
	assert.Equal(t, "rv-ca", listed.Endorsements[0].Name)

	rv, err := srv.PutReferenceValues(ctx, &pb.PutReferenceValuesRequest{Name: "golden", SignedCorim: corim})
	require.NoError(t, err)
	assert.Equal(t, []uint32{0, 7}, rv.PcrIndices)
	got, err := srv.GetReferenceValues(ctx, &pb.GetReferenceValuesRequest{Name: "golden"})
	require.NoError(t, err)
	assert.Equal(t, corim, got.SignedCorim)
	all, err := srv.ListReferenceValues(ctx, &pb.ListReferenceValuesRequest{})
	require.NoError(t, err)
	require.Len(t, all.ReferenceValues, 1)
	assert.Empty(t, all.ReferenceValues[0].SignedCorim)

	quote, err := attestationtest.SignQuote(ak, pcrs, []byte("n"))
	require.NoError(t, err)
	result, err := srv.AppraiseQuote(ctx, &pb.AppraiseQuoteRequest{
		AkCertificatePem: ak.CertificatePEM(), Quote: quote.Attest, Signature: quote.Signature,
		Nonce: []byte("n"), Pcrs: pcrs, ReferenceValues: "golden",
	})
	require.NoError(t, err)
	assert.True(t, result.Verified)
	assert.Equal(t, "signed", result.BaselineSource)
	require.Len(t, result.Checks, 4)
	assert.Equal(t, "pass", result.Checks[0].Outcome)
	assert.NotNil(t, result.AppraisedAt)

	_, err = srv.DeleteReferenceValues(ctx, &pb.DeleteReferenceValuesRequest{Name: "golden"})
	require.NoError(t, err)
	_, err = srv.DeleteReferenceValues(ctx, &pb.DeleteReferenceValuesRequest{Name: "golden"})
	assert.Equal(t, codes.NotFound, status.Code(err))
	_, err = srv.DeleteEndorsement(ctx, &pb.DeleteEndorsementRequest{Name: "ak-ca"})
	require.NoError(t, err)
}

func TestServer_AttestationVerifierErrors(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	// A nil request is refused before the service is consulted.
	for name, call := range map[string]func() error{
		"put endorsement":  func() error { _, err := srv.PutEndorsement(ctx, nil); return err },
		"list endorsement": func() error { _, err := srv.ListEndorsements(ctx, nil); return err },
		"del endorsement":  func() error { _, err := srv.DeleteEndorsement(ctx, nil); return err },
		"put refvals":      func() error { _, err := srv.PutReferenceValues(ctx, nil); return err },
		"get refvals":      func() error { _, err := srv.GetReferenceValues(ctx, nil); return err },
		"list refvals":     func() error { _, err := srv.ListReferenceValues(ctx, nil); return err },
		"del refvals":      func() error { _, err := srv.DeleteReferenceValues(ctx, nil); return err },
		"appraise":         func() error { _, err := srv.AppraiseQuote(ctx, nil); return err },
	} {
		assert.Equal(t, codes.InvalidArgument, status.Code(call()), name)
	}

	// No attestation service at all.
	mockSvc.AttestationSvc = nil
	_, err := srv.ListReferenceValues(ctx, &pb.ListReferenceValuesRequest{})
	assert.Equal(t, codes.Unimplemented, status.Code(err))

	// Service errors keep their codes: invalid input, not found, no store.
	store := attestationtest.NewMemoryStore()
	mockSvc.AttestationSvc = service.NewAttestationService(nil, nil, service.WithAttestationStore(store))
	_, err = srv.PutEndorsement(ctx, &pb.PutEndorsementRequest{Name: "x", Kind: "nope"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = srv.GetReferenceValues(ctx, &pb.GetReferenceValuesRequest{Name: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))
	_, err = srv.DeleteEndorsement(ctx, &pb.DeleteEndorsementRequest{Name: "missing"})
	assert.Equal(t, codes.NotFound, status.Code(err))
	_, err = srv.PutReferenceValues(ctx, &pb.PutReferenceValuesRequest{Name: "x", SignedCorim: []byte{1}})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	store.Fail = stderrors.New("down")
	for name, call := range map[string]func() error{
		"list endorsements": func() error { _, err := srv.ListEndorsements(ctx, &pb.ListEndorsementsRequest{}); return err },
		"list refvals":      func() error { _, err := srv.ListReferenceValues(ctx, &pb.ListReferenceValuesRequest{}); return err },
		"appraise": func() error {
			_, err := srv.AppraiseQuote(ctx, &pb.AppraiseQuoteRequest{AkCertificatePem: []byte("x")})
			return err
		},
	} {
		assert.Equal(t, codes.Unavailable, status.Code(call()), name)
	}
}
