//go:build rest

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

package rest

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// verifierRouter mounts the Verifier routes over a real attestation service
// and in-memory store.
func verifierRouter(t *testing.T, svc service.AttestationService) http.Handler {
	t.Helper()
	server, mockSvc, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mockSvc.AttestationSvc = svc
	r := chi.NewRouter()
	r.Put("/api/v1/attestation/endorsements/{name}", server.HandleAttestationPutEndorsement)
	r.Get("/api/v1/attestation/endorsements", server.HandleAttestationListEndorsements)
	r.Delete("/api/v1/attestation/endorsements/{name}", server.HandleAttestationDeleteEndorsement)
	r.Put("/api/v1/attestation/reference-values/{name}", server.HandleAttestationPutReferenceValues)
	r.Get("/api/v1/attestation/reference-values/{name}", server.HandleAttestationGetReferenceValues)
	r.Get("/api/v1/attestation/reference-values", server.HandleAttestationListReferenceValues)
	r.Delete("/api/v1/attestation/reference-values/{name}", server.HandleAttestationDeleteReferenceValues)
	r.Post("/api/v1/attestation/appraise-quote", server.HandleAttestationAppraiseQuote)
	return r
}

func serveJSON(t *testing.T, h http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if raw, ok := body.(string); ok {
		buf.WriteString(raw)
	} else if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	// The tenant go-qrdb's AuthMiddleware binds for an authenticated caller.
	req := httptest.NewRequest(method, path, &buf)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "acme"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAttestationVerifierREST_EndToEnd(t *testing.T) {
	store := attestationtest.NewMemoryStore()
	h := verifierRouter(t, service.NewAttestationService(nil, nil, service.WithAttestationStore(store)))

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

	w := serveJSON(t, h, http.MethodPut, "/api/v1/attestation/endorsements/ak-ca", map[string]interface{}{"kind": "ak-root", "certificate_pem": akCA.CertificatePEM()})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = serveJSON(t, h, http.MethodPut, "/api/v1/attestation/endorsements/rv-ca", map[string]interface{}{"kind": "reference-signer", "certificate_pem": rvCA.CertificatePEM()})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = serveJSON(t, h, http.MethodGet, "/api/v1/attestation/endorsements?kind=ak-root", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var listed service.ListEndorsementsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Endorsements, 1)
	assert.Equal(t, "ak-ca", listed.Endorsements[0].Name)

	w = serveJSON(t, h, http.MethodPut, "/api/v1/attestation/reference-values/golden", map[string]interface{}{"signed_corim": corim})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = serveJSON(t, h, http.MethodGet, "/api/v1/attestation/reference-values/golden", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var rv service.ReferenceValues
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rv))
	assert.Equal(t, corim, rv.SignedCoRIM)
	assert.Equal(t, []uint32{0, 7}, rv.PCRIndices)
	w = serveJSON(t, h, http.MethodGet, "/api/v1/attestation/reference-values", nil)
	require.Equal(t, http.StatusOK, w.Code)

	quote, err := attestationtest.SignQuote(ak, pcrs, []byte("n"))
	require.NoError(t, err)
	w = serveJSON(t, h, http.MethodPost, "/api/v1/attestation/appraise-quote", &service.AppraiseQuoteRequest{
		AKCertificatePEM: ak.CertificatePEM(), Quote: quote.Attest, Signature: quote.Signature,
		Nonce: []byte("n"), PCRs: pcrs, ReferenceValues: "golden",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result service.AttestationResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result.Verified)
	assert.Equal(t, service.BaselineSourceSigned, result.BaselineSource)

	assert.Equal(t, http.StatusNoContent, serveJSON(t, h, http.MethodDelete, "/api/v1/attestation/reference-values/golden", nil).Code)
	assert.Equal(t, http.StatusNotFound, serveJSON(t, h, http.MethodDelete, "/api/v1/attestation/reference-values/golden", nil).Code)
	assert.Equal(t, http.StatusNoContent, serveJSON(t, h, http.MethodDelete, "/api/v1/attestation/endorsements/ak-ca", nil).Code)
}

func TestAttestationVerifierREST_Errors(t *testing.T) {
	store := attestationtest.NewMemoryStore()
	h := verifierRouter(t, service.NewAttestationService(nil, nil, service.WithAttestationStore(store)))

	for _, c := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/attestation/endorsements/x"},
		{http.MethodPut, "/api/v1/attestation/reference-values/x"},
		{http.MethodPost, "/api/v1/attestation/appraise-quote"},
	} {
		assert.Equal(t, http.StatusBadRequest, serveJSON(t, h, c.method, c.path, "{not json").Code, c.path)
	}
	assert.Equal(t, http.StatusBadRequest, serveJSON(t, h, http.MethodPut, "/api/v1/attestation/endorsements/x", map[string]string{"kind": "nope"}).Code)
	assert.Equal(t, http.StatusBadRequest, serveJSON(t, h, http.MethodPut, "/api/v1/attestation/reference-values/x", map[string][]byte{"signed_corim": {1}}).Code,
		"unendorsed reference values are refused")
	assert.Equal(t, http.StatusNotFound, serveJSON(t, h, http.MethodGet, "/api/v1/attestation/reference-values/missing", nil).Code)
	assert.Equal(t, http.StatusNotFound, serveJSON(t, h, http.MethodDelete, "/api/v1/attestation/endorsements/missing", nil).Code)

	store.Fail = stderrors.New("down")
	assert.Equal(t, http.StatusServiceUnavailable, serveJSON(t, h, http.MethodGet, "/api/v1/attestation/endorsements", nil).Code)
	assert.Equal(t, http.StatusServiceUnavailable, serveJSON(t, h, http.MethodGet, "/api/v1/attestation/reference-values", nil).Code)
	assert.Equal(t, http.StatusServiceUnavailable, serveJSON(t, h, http.MethodPost, "/api/v1/attestation/appraise-quote", &service.AppraiseQuoteRequest{AKCertificatePEM: []byte("x")}).Code)

	none := verifierRouter(t, nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/attestation/endorsements/x"},
		{http.MethodGet, "/api/v1/attestation/endorsements"},
		{http.MethodDelete, "/api/v1/attestation/endorsements/x"},
		{http.MethodPut, "/api/v1/attestation/reference-values/x"},
		{http.MethodGet, "/api/v1/attestation/reference-values/x"},
		{http.MethodGet, "/api/v1/attestation/reference-values"},
		{http.MethodDelete, "/api/v1/attestation/reference-values/x"},
		{http.MethodPost, "/api/v1/attestation/appraise-quote"},
	} {
		assert.Equal(t, http.StatusServiceUnavailable, serveJSON(t, none, c.method, c.path, "{}").Code, c.path)
	}
}
