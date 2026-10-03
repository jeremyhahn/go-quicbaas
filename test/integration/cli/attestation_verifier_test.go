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

//go:build integration

// Package cli provides E2E integration tests for the `quicbaas attestation`
// command tree — the attestation Verifier — on every transport against a live
// server, with real evidence: an AK certificate under an endorsed CA, a
// COSE-signed CoRIM, and a TPMS_ATTEST quote signed by the AK.
package cli

import (
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
)

// verifierFixtures are the evidence files one transport's run uses.
type verifierFixtures struct {
	akCA, rvCA, corim, akCert, quote, signature string
	nonce                                       string
	pcrs                                        map[uint32][]byte
	tamperedPCR7                                string
}

func writeVerifierFixtures(t *testing.T) verifierFixtures {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, data, 0o600))
		return path
	}
	akCA, err := attestationtest.NewAuthority("integration AK CA")
	require.NoError(t, err)
	rvCA, err := attestationtest.NewAuthority("integration RV CA")
	require.NoError(t, err)
	signerCert, signerKey, err := attestationtest.SignerUnder(rvCA)
	require.NoError(t, err)
	ak, err := attestationtest.IssueAttestationKey(akCA, true)
	require.NoError(t, err)
	pcrs := attestationtest.PCRValues(0, 4, 7)
	corim, err := attestationtest.SignCoRIM(pcrs, signerKey, []*x509.Certificate{signerCert})
	require.NoError(t, err)
	nonce := []byte("integration-nonce")
	quote, err := attestationtest.SignQuote(ak, pcrs, nonce)
	require.NoError(t, err)
	return verifierFixtures{
		akCA: write("ak-ca.pem", akCA.CertificatePEM()), rvCA: write("rv-ca.pem", rvCA.CertificatePEM()),
		corim: write("golden.corim", corim), akCert: write("ak.pem", ak.CertificatePEM()),
		quote: write("quote.bin", quote.Attest), signature: write("quote.sig", quote.Signature),
		nonce: hex.EncodeToString(nonce), pcrs: pcrs,
		tamperedPCR7: hex.EncodeToString(attestationtest.PCRValues(9)[9]),
	}
}

// appraiseArgs builds an appraise-quote invocation; pcr7 overrides PCR 7.
func (f verifierFixtures) appraiseArgs(refValues, pcr7 string) []string {
	args := []string{"attestation", "appraise-quote", "--output", "json",
		"--ak-cert", f.akCert, "--quote", f.quote, "--signature", f.signature,
		"--nonce", f.nonce, "--reference-values", refValues}
	for idx, v := range f.pcrs {
		digest := hex.EncodeToString(v)
		if idx == 7 && pcr7 != "" {
			digest = pcr7
		}
		args = append(args, "--pcr", strconv.FormatUint(uint64(idx), 10)+"="+digest)
	}
	return args
}

type verifierResult struct {
	Verified             bool   `json:"verified"`
	CryptographicFailure bool   `json:"cryptographic_failure"`
	BaselineSource       string `json:"baseline_source"`
	Checks               []struct {
		Name    string `json:"name"`
		Outcome string `json:"outcome"`
	} `json:"checks"`
}

func runVerifier(t *testing.T, runner *TestRunner, name string, args ...string) (string, error) {
	t.Helper()
	stdout, stderr, err := runner.Run(CommandDef{Name: name, Args: args, Timeout: 20 * time.Second})
	if err != nil {
		t.Logf("[%s] %s: stderr=%s", runner.transport, name, stderr)
	}
	return stdout, err
}

// TestAttestationVerifierLifecycle drives endorsements, signed reference
// values and appraisal through the CLI on every transport: a genuine quote is
// verified against the signed baseline, a divergent PCR is a cryptographic
// failure, and unendorsed reference values are refused (no TOFU).
func TestAttestationVerifierLifecycle(t *testing.T) {
	RunForAllTransports(t, "AttestationVerifierLifecycle", func(t *testing.T, runner *TestRunner) {
		f := writeVerifierFixtures(t)
		suffix := strings.ToLower(string(runner.transport)) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		akName, rvName, golden := "ak-"+suffix, "rv-"+suffix, "golden-"+suffix

		// No TOFU: before any reference-signer is endorsed, the CoRIM is refused.
		_, err := runVerifier(t, runner, "reference-values put (unendorsed)", "attestation", "reference-values", "put", golden, "--corim", f.corim)
		assert.Error(t, err, "[%s] unendorsed reference values must be refused", runner.transport)

		_, err = runVerifier(t, runner, "endorsement put ak-root", "attestation", "endorsement", "put", akName, "--kind", "ak-root", "--certificate", f.akCA)
		require.NoError(t, err, "[%s]", runner.transport)
		_, err = runVerifier(t, runner, "endorsement put reference-signer", "attestation", "endorsement", "put", rvName, "--kind", "reference-signer", "--certificate", f.rvCA)
		require.NoError(t, err, "[%s]", runner.transport)

		out, err := runVerifier(t, runner, "endorsement list", "attestation", "endorsement", "list", "--output", "json")
		require.NoError(t, err)
		assert.Contains(t, out, akName)
		assert.Contains(t, out, rvName)

		out, err = runVerifier(t, runner, "reference-values put", "attestation", "reference-values", "put", golden, "--corim", f.corim, "--output", "json")
		require.NoError(t, err, "[%s]", runner.transport)
		assert.Contains(t, out, `"pcr_indices"`)

		out, err = runVerifier(t, runner, "appraise genuine", f.appraiseArgs(golden, "")...)
		require.NoError(t, err, "[%s]", runner.transport)
		var genuine verifierResult
		require.NoError(t, json.Unmarshal([]byte(out), &genuine), "[%s] %s", runner.transport, out)
		assert.True(t, genuine.Verified, "[%s] genuine evidence: %s", runner.transport, out)
		assert.Equal(t, "signed", genuine.BaselineSource)

		out, err = runVerifier(t, runner, "appraise divergent", f.appraiseArgs(golden, f.tamperedPCR7)...)
		require.NoError(t, err, "[%s]", runner.transport)
		var divergent verifierResult
		require.NoError(t, json.Unmarshal([]byte(out), &divergent))
		assert.False(t, divergent.Verified, "[%s] a divergent PCR must not verify", runner.transport)
		assert.True(t, divergent.CryptographicFailure, "[%s] a PCR whose quote the AK signed but whose value diverges is a forgery: %s", runner.transport, out)

		_, err = runVerifier(t, runner, "reference-values delete", "attestation", "reference-values", "delete", golden)
		require.NoError(t, err)
		_, err = runVerifier(t, runner, "endorsement delete", "attestation", "endorsement", "delete", akName)
		require.NoError(t, err)
		_, err = runVerifier(t, runner, "endorsement delete", "attestation", "endorsement", "delete", rvName)
		require.NoError(t, err)
	})
}
