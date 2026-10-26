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

// fleet_deploy_sdk_issuer_csr_test.go pins that the workstation `quicbaas
// fleet deploy` issuer obtains an enrollable agent certificate: it builds the
// key and CSR locally, posts only the CSR to POST /api/v1/fleet/certificates,
// and keeps the key matching the certificate the server signed and recorded.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

const fleetCSRTestTrustDomain = "quicbaas.example.test"

// fleetSigningServer serves POST /api/v1/fleet/certificates the way
// quicbaasd does: it signs the posted CSR with a real go-xkms CA through the
// fleet issuer and records it. It returns the record store and a pointer to
// the last request body so a test can check what crossed the network.
func fleetSigningServer(t *testing.T) (*httptest.Server, *pkgservice.KVFleetNodeCertRecords, *[]byte) {
	t.Helper()
	records, err := pkgservice.NewKVFleetNodeCertRecords(qrdbsdk.NewMemoryKVStore())
	require.NoError(t, err)
	issuer, err := pkgservice.NewXKMSFleetNodeCertIssuer(xkmsca.New(t, "deploy-csr-test-ca"), records)
	require.NoError(t, err)

	var lastBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/fleet/certificates", func(w http.ResponseWriter, r *http.Request) {
		var req fleetCertificateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		lastBody, err = json.Marshal(&req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		signed, err := issuer.SignFleetNodeCSR(r.Context(), &pkgservice.FleetNodeCSRRequest{
			TenantID:          req.TenantID,
			CSRPEM:            req.CSRPEM,
			ValidityDays:      req.ValidityDays,
			SPIFFETrustDomain: fleetCSRTestTrustDomain,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(&fleetCertificateResponse{CertificatePEM: signed.CertPEM, CAPEM: signed.CAPEM}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, records, &lastBody
}

// A workstation deploy receives a certificate enrolment accepts: the agent
// SPIFFE SAN under the tenant, recorded as issued, and matching the key the
// deploy kept. Only the CSR crossed the network.
func TestSDKAgentCertIssuer_IssuesAnEnrollableAgentCertificate(t *testing.T) {
	srv, records, lastBody := fleetSigningServer(t)
	issuer := &sdkFleetNodeCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}

	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &pkgservice.FleetNodeCertificateRequest{
		TenantID:      "automatethethings",
		Hostname:      "agent01.example.com",
		ExtraDNSNames: []string{"alt.example.com"},
		ValidityDays:  30,
	})
	require.NoError(t, err)
	defer issued.PrivateKey.Destroy()

	leaf := issued.ParsedCert
	require.NotNil(t, leaf)
	require.NotEmpty(t, leaf.URIs, "certificate carries no SPIFFE SAN; enrolment would refuse it")
	assert.Equal(t, "spiffe://automatethethings/svc/agent-agent01.example.com", leaf.URIs[0].String())
	assert.ElementsMatch(t, []string{"agent01.example.com", "alt.example.com"}, leaf.DNSNames)
	assert.NoError(t, records.VerifyFleetNodeCertificate(context.Background(), "automatethethings", leaf),
		"the server must record the certificate for enrolment")
	assert.NotEmpty(t, issued.CAPEM)

	keyPEM, err := secure.CopyOut(issued.PrivateKey)
	require.NoError(t, err)
	block, _ := pem.Decode(keyPEM)
	require.NotNil(t, block, "private key is not PEM")
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	key, ok := parsed.(*ecdsa.PrivateKey)
	require.True(t, ok, "private key %T is not ECDSA", parsed)
	assert.True(t, key.PublicKey.Equal(leaf.PublicKey), "the kept key must match the certificate")
	assert.NotContains(t, string(*lastBody), "PRIVATE KEY", "the private key must never be posted")
}

// A server that refuses, answers garbage or omits the certificate or CA
// bundle yields an error and no material.
func TestSDKAgentCertIssuer_RefusesABadSigningResponse(t *testing.T) {
	cases := map[string]struct {
		handler http.HandlerFunc
		want    string
	}{
		"http error": {func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "denied", http.StatusForbidden)
		}, "HTTP 403"},
		"bad json": {func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("{not json"))
		}, "decode fleet csr response"},
		"no certificate": {func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(&fleetCertificateResponse{CAPEM: []byte("CA")})
		}, "carries no certificate"},
		"not a certificate": {func(w http.ResponseWriter, _ *http.Request) {
			junk := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1}})
			_ = json.NewEncoder(w).Encode(&fleetCertificateResponse{CertificatePEM: junk, CAPEM: []byte("CA")})
		}, "parse fleet certificate"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			issuer := &sdkFleetNodeCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}

			issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &pkgservice.FleetNodeCertificateRequest{
				TenantID: "automatethethings",
				Hostname: "agent01.example.com",
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Nil(t, issued)
		})
	}
}

// A signed certificate with no CA bundle is refused: the daemon could not
// verify the server it enrols with.
func TestSDKAgentCertIssuer_RefusesAResponseWithoutCABundle(t *testing.T) {
	srv, _, _ := fleetSigningServer(t)
	stripCA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := srv.Client().Post(srv.URL+r.URL.Path, "application/json", r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		var out fleetCertificateResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		out.CAPEM = nil
		_ = json.NewEncoder(w).Encode(&out)
	}))
	defer stripCA.Close()
	issuer := &sdkFleetNodeCertIssuer{baseURL: stripCA.URL, httpClient: stripCA.Client()}

	issued, err := issuer.IssueFleetNodeCertificate(context.Background(), &pkgservice.FleetNodeCertificateRequest{
		TenantID: "automatethethings",
		Hostname: "agent01.example.com",
	})
	assert.ErrorIs(t, err, ErrEmptyCABundle)
	assert.Nil(t, issued)
}
