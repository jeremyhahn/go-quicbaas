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

// fleet_deploy_sdk_issuer_csr_test.go pins that the workstation `quicbaas fleet
// deploy` issuer refuses to issue. Its only signing route, POST
// /api/v1/xkms/ca/sign, takes a request with no SPIFFE kind field, so the
// certificate it returns is not an agent's and fleet enrolment refuses it. The
// issuer must refuse before it generates a key or contacts the server.

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// assertDeployIssuerRefusesTheKind runs req through the workstation issuer
// against a server that counts sign requests, and checks the issuer refused
// with *FleetNodeCertKindUnavailableError without one.
func assertDeployIssuerRefusesTheKind(t *testing.T, req *pkgservice.FleetNodeCertificateRequest) {
	t.Helper()
	var signCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/xkms/ca/sign", func(w http.ResponseWriter, _ *http.Request) {
		signCalls.Add(1)
		http.Error(w, "unexpected sign request", http.StatusTeapot)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	issuer := &sdkFleetNodeCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	out, err := issuer.IssueFleetNodeCertificate(context.Background(), req)
	var kindErr *pkgservice.FleetNodeCertKindUnavailableError
	require.ErrorAs(t, err, &kindErr)
	assert.Equal(t, req.TenantID, kindErr.TenantID)
	assert.Nil(t, out, "no material may be returned with the refusal")
	assert.Zero(t, signCalls.Load(), "the refusal must precede the sign request")
}

// A fully specified request is refused before any CSR is posted: the sign
// route cannot request the agent SPIFFE kind, so the certificate it would
// return cannot enroll.
func TestSDKAgentCertIssuer_PostsASignableCSRAndKeepsTheMatchingKey(t *testing.T) {
	assertDeployIssuerRefusesTheKind(t, &pkgservice.FleetNodeCertificateRequest{
		TenantID:          "automatethethings",
		Hostname:          "agent01.example.com",
		ExtraDNSNames:     []string{"alt.example.com"},
		ValidityDays:      30,
		SPIFFETrustDomain: "quicbaas.example.test",
	})
}

// A CA that would refuse the CSR is never reached: the issuer refuses first,
// with the kind error rather than the CA's.
func TestSDKAgentCertIssuer_ReportsARefusedCSR(t *testing.T) {
	assertDeployIssuerRefusesTheKind(t, &pkgservice.FleetNodeCertificateRequest{
		TenantID: "automatethethings",
		Hostname: "agent01.example.com",
	})
}
