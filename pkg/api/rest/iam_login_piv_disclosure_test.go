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
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// What a failed attestation tells an UNAUTHENTICATED caller.
//
// The PIV login endpoint runs a verifier whose errors describe the SERVER:
// construction failures carry the configured attestation_ca_file path and the
// parser's complaint about it, and chain failures carry the subjects and
// issuers in the trust store. Returning them verbatim let anyone able to
// complete a TLS handshake enumerate deployment layout and trust-store
// contents, one self-minted certificate at a time.
//
// These are property tests, not string matches. Asserting the exact generic
// sentence would pass trivially and would catch nothing the day a new error
// type starts reaching this path — the property that matters is that NO
// filesystem path and NO certificate subject appears in the body, whatever the
// error turns out to be.
// =============================================================================

// assertNoServerDetail fails when a response body carries anything that
// describes the server's configuration or trust store.
func assertNoServerDetail(t *testing.T, body string, secrets map[string]string) {
	t.Helper()
	for what, needle := range secrets {
		if needle == "" {
			continue
		}
		if strings.Contains(body, needle) {
			t.Errorf("the response discloses %s (%q):\n%s", what, needle, body)
		}
	}
	// Path separators and PEM/DER file extensions are structural giveaways
	// independent of the specific fixture values above.
	for _, marker := range []string{".pem", ".crt", ".der", "/tmp/", "CN=", "O="} {
		if strings.Contains(body, marker) {
			t.Errorf("the response contains %q, which discloses server detail:\n%s", marker, body)
		}
	}
}

// auditDetail returns the concatenated failure text the audit sink recorded.
func auditDetail(audit *captureAuditLogger) string {
	var b strings.Builder
	for _, event := range audit.events {
		for _, v := range event.Details {
			if s, ok := v.(string); ok {
				b.WriteString(s)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// TestPIVLogin_ConstructionFailureDisclosesNothing covers the branch where the
// verifier could not be built at all — a misconfigured attestation_ca_file.
// That error names the operator's path and the parser's complaint, and it is
// produced before any certificate is examined, so an attacker reaches it with
// any well-formed request.
func TestPIVLogin_ConstructionFailureDisclosesNothing(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	handler, audit, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()

	// A bundle that exists but cannot serve as trust anchors: the refusal
	// carries both the path and the offending subject.
	badBundle := filepath.Join(t.TempDir(), "not-a-ca-bundle.pem")
	require.NoError(t, os.WriteFile(badBundle, pivLeafPEM(t, leaf), 0o600))
	handler.server.config.PIVClientCert.AttestationCAFile = badBundle

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	body := w.Body.String()
	assertNoServerDetail(t, body, map[string]string{
		"the configured trust-bundle path": badBundle,
		"the temp directory":               filepath.Dir(badBundle),
		"the certificate subject":          leaf.Subject.String(),
	})

	// The client still learns WHICH failure class this is, so existing
	// branching keeps working.
	assert.Contains(t, body, ErrPIVCertIssuerRejected,
		"the error code must survive so a client can still branch on it")

	// And the operator still gets everything, in the sink that is theirs.
	detail := auditDetail(audit)
	assert.Contains(t, detail, badBundle,
		"the audit record lost the path the operator has to fix")
}

// TestPIVLogin_VerificationFailureDisclosesNothing covers the other branch: a
// chain that genuinely did not verify. Those errors carry certificate subjects
// and issuers — partly the requester's own, but also the trust store's.
func TestPIVLogin_VerificationFailureDisclosesNothing(t *testing.T) {
	ca, _ := pivLoginTestCA(t, "PIV Root CA")
	impostorCA, impostorKey := pivLoginTestCA(t, "Impostor Root")
	leaf := pivSpiffeLeaf(t, impostorCA, impostorKey, "tenant-piv")

	// An attestation certificate IS presented, so verification reaches genuine
	// PATH BUILDING and fails there. That is the branch whose error carries the
	// subject and issuer; without a presented attestation the handler stops
	// earlier with a message that names neither, and this test would pass
	// without ever exercising the disclosure it exists to prevent.
	forged := pivSlotAttestationFor(t, leaf, impostorCA, impostorKey)

	handler, audit, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
	defer cleanup()
	trustPIVAttestationCA(t, handler, ca)

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithChain(leaf, forged))

	body := w.Body.String()
	assertNoServerDetail(t, body, map[string]string{
		"the presented certificate subject":   leaf.Subject.String(),
		"the presented certificate issuer":    leaf.Issuer.String(),
		"the attestation certificate subject": forged.Subject.String(),
		"a trusted root subject":              ca.Subject.String(),
	})
	assert.Contains(t, body, ErrPIVCertIssuerRejected)

	detail := auditDetail(audit)
	assert.NotEmpty(t, detail, "the failure reached no server-side sink at all")
}

// TestPIVLogin_BothFailureBranchesLookIdenticalOnTheWire is the probing
// control. If a caller can tell "your certificate was refused" from "the
// server's trust bundle is broken", the endpoint is an oracle for
// configuration state even without leaking the strings themselves.
func TestPIVLogin_BothFailureBranchesLookIdenticalOnTheWire(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")

	misconfigured := func() string {
		leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
		handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
		defer cleanup()
		bad := filepath.Join(t.TempDir(), "bundle.pem")
		require.NoError(t, os.WriteFile(bad, pivLeafPEM(t, leaf), 0o600))
		handler.server.config.PIVClientCert.AttestationCAFile = bad
		w := httptest.NewRecorder()
		handler.PIVLogin(w, pivRequestWithSpiffe(leaf))
		return w.Body.String()
	}()

	refused := func() string {
		impostorCA, impostorKey := pivLoginTestCA(t, "Impostor Root")
		leaf := pivSpiffeLeaf(t, impostorCA, impostorKey, "tenant-piv")
		handler, _, cleanup := setupPIVFullHandler(t, leaf, "tenant-piv", true)
		defer cleanup()
		trustPIVAttestationCA(t, handler, ca)
		w := httptest.NewRecorder()
		handler.PIVLogin(w, pivRequestWithSpiffe(leaf))
		return w.Body.String()
	}()

	assert.Equal(t, misconfigured, refused,
		"a caller can distinguish a misconfigured server from a refused credential, "+
			"which makes the login endpoint an oracle for configuration state")
}

// pivLeafPEM encodes a certificate as PEM. An end-entity certificate written
// where a CA bundle belongs is the operator mistake these tests reproduce.
func pivLeafPEM(t *testing.T, cert *x509.Certificate) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// pivErrorCode extracts the stable error code from an IAM error response, so a
// test can assert the code survived sanitisation without matching on the
// free-text message.
func pivErrorCode(t *testing.T, body string) string {
	t.Helper()
	var resp struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	return resp.Error
}
