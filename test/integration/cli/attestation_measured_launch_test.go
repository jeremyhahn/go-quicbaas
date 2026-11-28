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

package cli

import (
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
)

// verifierSuffix names one transport's run uniquely.
func verifierSuffix(runner *TestRunner) string {
	return strings.ToLower(string(runner.transport)) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// writeTempFile writes data to a new file under dir and returns its path.
func writeTempFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// TestAttestationVerifierLaunchSignerEndorsements drives the measured-launch
// trust anchors through the CLI on every transport: an image-signer and a
// manifest-signer CA are endorsed and listed by kind, while a leaf (not a CA)
// and an unknown kind are refused.
func TestAttestationVerifierLaunchSignerEndorsements(t *testing.T) {
	RunForAllTransports(t, "AttestationVerifierLaunchSignerEndorsements", func(t *testing.T, runner *TestRunner) {
		dir := t.TempDir()
		suffix := verifierSuffix(runner)
		imgName, mfsName := "img-"+suffix, "mfs-"+suffix

		imgCA, err := attestationtest.NewAuthority("integration image-signer CA")
		require.NoError(t, err)
		mfsCA, err := attestationtest.NewAuthority("integration manifest-signer CA")
		require.NoError(t, err)
		imgCAFile := writeTempFile(t, dir, "img-ca.pem", imgCA.CertificatePEM())
		mfsCAFile := writeTempFile(t, dir, "mfs-ca.pem", mfsCA.CertificatePEM())

		_, err = runVerifier(t, runner, "endorsement put image-signer", "attestation", "endorsement", "put", imgName, "--kind", "image-signer", "--certificate", imgCAFile)
		require.NoError(t, err, "[%s]", runner.transport)
		_, err = runVerifier(t, runner, "endorsement put manifest-signer", "attestation", "endorsement", "put", mfsName, "--kind", "manifest-signer", "--certificate", mfsCAFile)
		require.NoError(t, err, "[%s]", runner.transport)

		out, err := runVerifier(t, runner, "endorsement list image-signer", "attestation", "endorsement", "list", "--kind", "image-signer", "--output", "json")
		require.NoError(t, err, "[%s]", runner.transport)
		assert.Contains(t, out, imgName, "[%s]", runner.transport)
		assert.NotContains(t, out, mfsName, "[%s] the kind filter must exclude manifest signers", runner.transport)

		// A launch-signer anchor must be a CA: a leaf is refused.
		leaf, _, err := attestationtest.SignerUnder(imgCA)
		require.NoError(t, err)
		leafFile := writeTempFile(t, dir, "leaf.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw}))
		_, err = runVerifier(t, runner, "endorsement put image-signer (leaf)", "attestation", "endorsement", "put", "leaf-"+suffix, "--kind", "image-signer", "--certificate", leafFile)
		assert.Error(t, err, "[%s] a leaf certificate must not anchor image signers", runner.transport)

		// An unknown kind is refused.
		_, err = runVerifier(t, runner, "endorsement put (unknown kind)", "attestation", "endorsement", "put", "bad-"+suffix, "--kind", "not-a-kind", "--certificate", imgCAFile)
		assert.Error(t, err, "[%s] an unknown endorsement kind must be refused", runner.transport)

		_, err = runVerifier(t, runner, "endorsement delete", "attestation", "endorsement", "delete", imgName)
		require.NoError(t, err)
		_, err = runVerifier(t, runner, "endorsement delete", "attestation", "endorsement", "delete", mfsName)
		require.NoError(t, err)
	})
}

// signGolden signs the fixtures' PCRs as go-trusted-platform golden values.
func signGolden(t *testing.T, id string, pcrs map[uint32][]byte, signerCA *attestationtest.Authority) []byte {
	t.Helper()
	cert, key, err := attestationtest.SignerUnder(signerCA)
	require.NoError(t, err)
	indices := make([]uint32, 0, len(pcrs))
	for idx := range pcrs {
		indices = append(indices, idx)
	}
	slices.Sort(indices)
	gv := &service.GoldenValues{ID: id, Vendor: "integration"}
	for _, idx := range indices {
		gv.PCRs = append(gv.PCRs, service.GoldenPCR{Index: idx, Hash: crypto.SHA256, Value: pcrs[idx]})
	}
	corim, err := service.SignGoldenValues(gv, key, []*x509.Certificate{cert})
	require.NoError(t, err)
	return corim
}

// TestAttestationVerifierGoldenValuesImport drives the golden-values import
// through the CLI on every transport: go-trusted-platform golden values signed
// as a CoRIM are stored under a reference-signer endorsement and appraised
// through AppraiseQuote. A genuine quote verifies against the signed baseline,
// a divergent PCR does not, and golden values signed under an unendorsed CA
// are refused.
func TestAttestationVerifierGoldenValuesImport(t *testing.T) {
	RunForAllTransports(t, "AttestationVerifierGoldenValuesImport", func(t *testing.T, runner *TestRunner) {
		f := writeVerifierFixtures(t)
		dir := t.TempDir()
		suffix := verifierSuffix(runner)
		akName, rvName, golden := "ak-"+suffix, "gv-signer-"+suffix, "golden-"+suffix

		rvCA, err := attestationtest.NewAuthority("integration golden-values CA")
		require.NoError(t, err)
		rvCAFile := writeTempFile(t, dir, "gv-ca.pem", rvCA.CertificatePEM())
		corimFile := writeTempFile(t, dir, "golden.corim", signGolden(t, golden, f.pcrs, rvCA))

		_, err = runVerifier(t, runner, "endorsement put ak-root", "attestation", "endorsement", "put", akName, "--kind", "ak-root", "--certificate", f.akCA)
		require.NoError(t, err, "[%s]", runner.transport)
		_, err = runVerifier(t, runner, "endorsement put reference-signer", "attestation", "endorsement", "put", rvName, "--kind", "reference-signer", "--certificate", rvCAFile)
		require.NoError(t, err, "[%s]", runner.transport)

		_, err = runVerifier(t, runner, "reference-values put (golden)", "attestation", "reference-values", "put", golden, "--corim", corimFile, "--output", "json")
		require.NoError(t, err, "[%s]", runner.transport)

		out, err := runVerifier(t, runner, "appraise genuine", f.appraiseArgs(golden, "")...)
		require.NoError(t, err, "[%s]", runner.transport)
		var genuine verifierResult
		require.NoError(t, json.Unmarshal([]byte(out), &genuine), "[%s] %s", runner.transport, out)
		assert.True(t, genuine.Verified, "[%s] genuine evidence against golden values: %s", runner.transport, out)
		assert.Equal(t, "signed", genuine.BaselineSource)

		out, err = runVerifier(t, runner, "appraise divergent", f.appraiseArgs(golden, f.tamperedPCR7)...)
		require.NoError(t, err, "[%s]", runner.transport)
		var divergent verifierResult
		require.NoError(t, json.Unmarshal([]byte(out), &divergent), "[%s] %s", runner.transport, out)
		assert.False(t, divergent.Verified, "[%s] a divergent PCR must not verify against golden values", runner.transport)

		// Golden values signed under a CA nobody endorsed are refused.
		rogueCA, err := attestationtest.NewAuthority("integration unendorsed CA")
		require.NoError(t, err)
		rogue := "golden-rogue-" + suffix
		rogueFile := writeTempFile(t, dir, "rogue.corim", signGolden(t, rogue, f.pcrs, rogueCA))
		_, err = runVerifier(t, runner, "reference-values put (unendorsed)", "attestation", "reference-values", "put", rogue, "--corim", rogueFile)
		assert.Error(t, err, "[%s] golden values under an unendorsed signer must be refused", runner.transport)

		_, err = runVerifier(t, runner, "reference-values delete", "attestation", "reference-values", "delete", golden)
		require.NoError(t, err)
		_, err = runVerifier(t, runner, "endorsement delete", "attestation", "endorsement", "delete", akName)
		require.NoError(t, err)
		_, err = runVerifier(t, runner, "endorsement delete", "attestation", "endorsement", "delete", rvName)
		require.NoError(t, err)
	})
}
