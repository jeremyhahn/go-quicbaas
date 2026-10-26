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
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

const fleetCSRTestTenant = "tenant-a"

// fleetCSRTestServer returns a server with the CA and record store wired,
// and the record store, so a test can check what was recorded.
func fleetCSRTestServer(t *testing.T) (*Server, *service.KVFleetNodeCertRecords) {
	t.Helper()
	s := &Server{config: &Config{SPIFFETrustDomain: "example.test"}}
	s.SetCertificateAuthority(testca.New(t, "rest-fleet-csr-ca"))
	records := fleetTestCertRecordStore(t)
	s.SetFleetNodeCertRecords(records)
	return s, records
}

// postFleetCSR drives HandleSignFleetNodeCSR as a caller bound to
// fleetCSRTestTenant.
func postFleetCSR(t *testing.T, s *Server, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/fleet/certificates", bytes.NewReader(payload))
	r = r.WithContext(tenantctx.WithTenant(r.Context(), fleetCSRTestTenant))
	w := httptest.NewRecorder()
	s.HandleSignFleetNodeCSR(w, r)
	return w
}

// fleetNodeTestKey builds the key and CSR a workstation deploy sends.
func fleetNodeTestKey(t *testing.T) *service.FleetNodeKey {
	t.Helper()
	key, err := service.NewFleetNodeKey(&service.FleetNodeCertificateRequest{
		TenantID: fleetCSRTestTenant,
		Hostname: "agent01.example.com",
	})
	require.NoError(t, err)
	return key
}

// TestHandleSignFleetNodeCSR_IssuesAnEnrollableAgentCertificate proves the
// workstation deploy's issuance route signs the CSR as an agent of the
// caller's tenant and records it, so the daemon holding it can enroll.
func TestHandleSignFleetNodeCSR_IssuesAnEnrollableAgentCertificate(t *testing.T) {
	s, records := fleetCSRTestServer(t)
	key := fleetNodeTestKey(t)

	w := postFleetCSR(t, s, &fleetCertificateRequest{CSRPEM: key.CSRPEM, ValidityDays: 1})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp fleetCertificateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.CAPEM)
	block, _ := pem.Decode(resp.CertificatePEM)
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.Equal(t, key.CommonName, leaf.Subject.CommonName)
	tenant, err := fleetNodeCertTenant(leaf, "example.test")
	require.NoError(t, err, "enrolment must accept the certificate as an agent")
	assert.Equal(t, fleetCSRTestTenant, tenant)
	assert.NoError(t, records.VerifyFleetNodeCertificate(t.Context(), fleetCSRTestTenant, leaf))
}

// TestHandleSignFleetNodeCSR_RefusesBadRequests proves a foreign tenant, a
// bad CSR and a server with no CA are refused without a certificate.
func TestHandleSignFleetNodeCSR_RefusesBadRequests(t *testing.T) {
	s, _ := fleetCSRTestServer(t)
	key := fleetNodeTestKey(t)
	notACSR := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1}})

	cases := map[string]struct {
		server *Server
		body   any
		want   int
	}{
		"foreign tenant": {s, &fleetCertificateRequest{TenantID: "tenant-b", CSRPEM: key.CSRPEM}, http.StatusForbidden},
		"no csr":         {s, &fleetCertificateRequest{}, http.StatusBadRequest},
		"not a csr":      {s, &fleetCertificateRequest{CSRPEM: notACSR}, http.StatusBadRequest},
		"invalid json":   {s, "not an object", http.StatusBadRequest},
		"no ca":          {&Server{config: &Config{SPIFFETrustDomain: "example.test"}}, &fleetCertificateRequest{CSRPEM: key.CSRPEM}, http.StatusServiceUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := postFleetCSR(t, tc.server, tc.body)
			assert.Equal(t, tc.want, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), "certificate_pem")
		})
	}
}
