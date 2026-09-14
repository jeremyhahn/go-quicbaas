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

// agent_deploy_sdk_issuer_csr_test.go is the acceptance test for
// go-quicbaas#910 on the `quicbaas agent deploy` path: after moving key
// creation to pkg/xkms and CSR construction to the go-xkms CSR builder, the CSR
// this CLI posts must still be a well-formed PKCS#10 carrying the agent's
// subject and SANs, and the key it keeps must still be the one the CA certifies.
//
// It signs the captured CSR with a real go-xkms CA rather than replaying a canned
// certificate, so the test fails if the CSR stops being signable or stops
// matching the key the issuer hands back — neither of which a fixture PEM can
// detect.

package main

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// The CSR the CLI posts is a valid PKCS#10 for the agent's identity, the CA can
// sign it, and the private key the CLI kept is the one in the resulting
// certificate.
func TestSDKAgentCertIssuer_PostsASignableCSRAndKeepsTheMatchingKey(t *testing.T) {
	ca := xkmsca.New(t, "QuicBaaS #910 Agent Deploy Root CA")

	var gotCSR []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/xkms/ca/sign", func(w http.ResponseWriter, r *http.Request) {
		var req xkmsSignCSRRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotCSR = req.CSRPEM

		cert, signErr := ca.SignCSR(r.Context(), req.CSRPEM, &xkmssdk.TrustedCASignOptions{
			ValidityDays: req.ValidityDays,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		})
		if signErr != nil {
			http.Error(w, signErr.Error(), http.StatusUnprocessableEntity)
			return
		}
		bundle, _ := ca.CABundle()
		_ = json.NewEncoder(w).Encode(&xkmsSignCSRResponse{
			CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
			ChainPEM:       bundle,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	issuer := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	out, err := issuer.IssueAgentCertificate(context.Background(), &pkgservice.AgentCertificateRequest{
		TenantID:      "automatethethings",
		Hostname:      "agent01.example.com",
		ExtraDNSNames: []string{"alt.example.com"},
		ValidityDays:  30,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	defer out.PrivateKey.Destroy()

	// 1. The CSR is a real PKCS#10 that verifies under its own public key.
	require.NotEmpty(t, gotCSR, "the CLI posted no CSR")
	csrBlock, _ := pem.Decode(gotCSR)
	require.NotNil(t, csrBlock, "the posted CSR is not PEM")
	require.Equal(t, "CERTIFICATE REQUEST", csrBlock.Type)
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	require.NoError(t, err)
	require.NoError(t, csr.CheckSignature(),
		"the posted CSR is not signed by the key it names")

	// 2. It carries the identity the deploy asked for.
	assert.Equal(t, "agent-agent01.example.com", csr.Subject.CommonName)
	require.NotEmpty(t, csr.Subject.Organization)
	assert.Equal(t, "automatethethings", csr.Subject.Organization[0])
	require.NotEmpty(t, csr.Subject.OrganizationalUnit)
	assert.Equal(t, "agents", csr.Subject.OrganizationalUnit[0],
		"the control plane's register handler rejects any other OU")
	assert.ElementsMatch(t, []string{"agent01.example.com", "alt.example.com"}, csr.DNSNames)

	// 3. The certificate the CA returned verifies against the bundle shipped
	//    with it, for the usage an agent identity is issued for.
	leaf := parseAgentDeployLeaf(t, out.CertPEM)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(out.CAPEM), "no usable CA bundle returned")
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	require.NoError(t, err, "the deployed agent certificate does not verify against its own CA bundle")

	// 4. The key the CLI kept is the certificate's. This is the half the deploy
	//    copies to the target host; a mismatch fails at the agent's first
	//    handshake, nowhere near here.
	assertDeployKeyMatchesCertificate(t, out.PrivateKey.Bytes(), leaf)

	// And it is the key that signed the CSR, so nothing was swapped in between.
	csrPub, err := x509.MarshalPKIXPublicKey(csr.PublicKey)
	require.NoError(t, err)
	leafPub, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	require.NoError(t, err)
	require.Equal(t, csrPub, leafPub, "the certified key is not the key that requested certification")
}

// A CA that refuses the CSR surfaces as an error with nothing returned: the
// deploy must not proceed with a certificate it does not have, and must not
// leave a key buffer behind for the caller to destroy.
func TestSDKAgentCertIssuer_ReportsARefusedCSR(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/xkms/ca/sign", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "profile not permitted", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	issuer := &sdkAgentCertIssuer{baseURL: srv.URL, httpClient: srv.Client()}
	out, err := issuer.IssueAgentCertificate(context.Background(), &pkgservice.AgentCertificateRequest{
		TenantID: "automatethethings",
		Hostname: "agent01.example.com",
	})
	require.Error(t, err)
	assert.Nil(t, out, "no material may be returned when the CA refuses the CSR")
}

// =============================================================================
// helpers
// =============================================================================

// parseAgentDeployLeaf decodes the issued leaf certificate.
func parseAgentDeployLeaf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block, "issued certificate is not PEM")
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return leaf
}

// assertDeployKeyMatchesCertificate proves the mlocked key is the leaf's.
func assertDeployKeyMatchesCertificate(t *testing.T, keyPEM []byte, leaf *x509.Certificate) {
	t.Helper()
	require.NotEmpty(t, keyPEM, "no private key material for the deployed agent")
	key, err := xkmssdk.DecodePrivateKeyPEM(keyPEM, nil)
	require.NoError(t, err, "the agent private key must be decodable on the target host")
	signer, ok := key.(crypto.Signer)
	require.True(t, ok, "the agent key must be a crypto.Signer, got %T", key)

	got, err := x509.MarshalPKIXPublicKey(signer.Public())
	require.NoError(t, err)
	want, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	require.NoError(t, err)
	require.Equal(t, want, got, "the agent private key does not match its certificate")
}
