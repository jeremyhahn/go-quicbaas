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

package main

import (
	"bytes"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// withEmbeddedAttestation points the CLI at the real SDK client over the
// embedded transport and the real Verifier.
func withEmbeddedAttestation(t *testing.T) *attestationtest.MemoryStore {
	t.Helper()
	store := attestationtest.NewMemoryStore()
	client, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(&testutil.MockQuicBaaS{
		AttestationSvc: service.NewAttestationService(nil, nil, service.WithAttestationStore(store)),
	}))
	if err != nil {
		t.Fatalf("embedded client: %v", err)
	}
	origFactory, origTimeout, origOutput := attestationClientFactory, globalTimeout, attestationOutputFmt
	attestationClientFactory = func() (quicbaas.AttestationService, error) { return client.Security().Attestation(), nil }
	globalTimeout = 10 * time.Second
	t.Cleanup(func() {
		attestationClientFactory, globalTimeout, attestationOutputFmt = origFactory, origTimeout, origOutput
		attestationKind, attestationCertFile, attestationKeyFile, attestationCoRIMFile = "", "", "", ""
		attestationAKCertFile, attestationQuoteFile, attestationSigFile, attestationNonceHex = "", "", "", ""
		attestationEventLog, attestationRefValues, attestationPCRs, attestationCoRIMOutput = "", "", nil, ""
		if err := client.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return store
}

func runAttestationCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newAttestationCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeFixture(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAttestationCLI_EndToEnd(t *testing.T) {
	withEmbeddedAttestation(t)
	dir := t.TempDir()

	akCA, err := attestationtest.NewAuthority("AK CA")
	if err != nil {
		t.Fatal(err)
	}
	rvCA, err := attestationtest.NewAuthority("RV CA")
	if err != nil {
		t.Fatal(err)
	}
	signerCert, signerKey, err := attestationtest.SignerUnder(rvCA)
	if err != nil {
		t.Fatal(err)
	}
	ak, err := attestationtest.IssueAttestationKey(akCA, true)
	if err != nil {
		t.Fatal(err)
	}
	pcrs := attestationtest.PCRValues(0, 7)
	corim, err := attestationtest.SignCoRIM(pcrs, signerKey, []*x509.Certificate{signerCert})
	if err != nil {
		t.Fatal(err)
	}
	quote, err := attestationtest.SignQuote(ak, pcrs, []byte("nonce"))
	if err != nil {
		t.Fatal(err)
	}

	if out, err := runAttestationCLI(t, "endorsement", "put", "ak-ca", "--kind", "ak-root", "--certificate", writeFixture(t, dir, "ak-ca.pem", akCA.CertificatePEM())); err != nil || !strings.Contains(out, "ak-root") {
		t.Fatalf("endorsement put: %q, %v", out, err)
	}
	if _, err := runAttestationCLI(t, "endorsement", "put", "rv-ca", "--kind", "reference-signer", "--certificate", writeFixture(t, dir, "rv-ca.pem", rvCA.CertificatePEM())); err != nil {
		t.Fatalf("endorsement put: %v", err)
	}
	if out, err := runAttestationCLI(t, "endorsement", "list", "--output", "json"); err != nil || !strings.Contains(out, "rv-ca") {
		t.Fatalf("endorsement list: %q, %v", out, err)
	}
	if out, err := runAttestationCLI(t, "reference-values", "put", "golden", "--corim", writeFixture(t, dir, "golden.corim", corim)); err != nil || !strings.Contains(out, "golden") {
		t.Fatalf("reference-values put: %q, %v", out, err)
	}
	corimOut := filepath.Join(dir, "out.corim")
	if out, err := runAttestationCLI(t, "reference-values", "get", "golden", "--corim-out", corimOut, "--output", "yaml"); err != nil || !strings.Contains(out, "golden") {
		t.Fatalf("reference-values get: %q, %v", out, err)
	}
	if written, err := os.ReadFile(corimOut); err != nil || !bytes.Equal(written, corim) {
		t.Fatalf("--corim-out wrote %d bytes (%v), want the signed CoRIM", len(written), err)
	}
	if out, err := runAttestationCLI(t, "reference-values", "list"); err != nil || !strings.Contains(out, "golden") {
		t.Fatalf("reference-values list: %q, %v", out, err)
	}

	args := []string{"appraise-quote", "--output", "json",
		"--ak-cert", writeFixture(t, dir, "ak.pem", ak.CertificatePEM()),
		"--quote", writeFixture(t, dir, "quote.bin", quote.Attest),
		"--signature", writeFixture(t, dir, "quote.sig", quote.Signature),
		"--nonce", hex.EncodeToString([]byte("nonce")),
		"--reference-values", "golden"}
	for idx, v := range pcrs {
		args = append(args, "--pcr", strconv.FormatUint(uint64(idx), 10)+"="+hex.EncodeToString(v))
	}
	out, err := runAttestationCLI(t, args...)
	if err != nil {
		t.Fatalf("appraise-quote: %q, %v", out, err)
	}
	var result quicbaas.AttestationResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || !result.Verified || result.BaselineSource != "signed" {
		t.Fatalf("appraise-quote result %q (%v)", out, err)
	}

	if out, err := runAttestationCLI(t, "reference-values", "delete", "golden"); err != nil || !strings.Contains(out, "Deleted") {
		t.Fatalf("reference-values delete: %q, %v", out, err)
	}
	if out, err := runAttestationCLI(t, "endorsement", "delete", "ak-ca"); err != nil || !strings.Contains(out, "Deleted") {
		t.Fatalf("endorsement delete: %q, %v", out, err)
	}
}

func TestAttestationCLI_Errors(t *testing.T) {
	store := withEmbeddedAttestation(t)
	dir := t.TempDir()

	for name, args := range map[string][]string{
		"missing corim file":   {"reference-values", "put", "x"},
		"unreadable corim":     {"reference-values", "put", "x", "--corim", filepath.Join(dir, "nope")},
		"bad kind":             {"endorsement", "put", "x", "--kind", "nope", "--certificate", writeFixture(t, dir, "c.pem", []byte("junk"))},
		"unreadable cert":      {"endorsement", "put", "x", "--kind", "ak-root", "--certificate", filepath.Join(dir, "nope")},
		"unreadable key":       {"endorsement", "put", "x", "--kind", "reference-signer", "--public-key", filepath.Join(dir, "nope")},
		"missing ak cert":      {"appraise-quote", "--reference-values", "g"},
		"bad nonce":            appraiseArgs(t, dir, "--nonce", "zz"),
		"bad pcr":              appraiseArgs(t, dir, "--pcr", "seven=00"),
		"bad pcr digest":       appraiseArgs(t, dir, "--pcr", "7=zz"),
		"pcr without digest":   appraiseArgs(t, dir, "--pcr", "7"),
		"unreadable event log": appraiseArgs(t, dir, "--event-log", filepath.Join(dir, "nope")),
		"unknown output":       {"endorsement", "list", "--output", "xml"},
		"missing refvals":      {"reference-values", "get", "nope"},
		"delete missing":       {"endorsement", "delete", "nope"},
	} {
		if _, err := runAttestationCLI(t, args...); err == nil {
			t.Fatalf("%s: no error", name)
		}
	}

	store.Fail = errors.New("down")
	for _, args := range [][]string{{"endorsement", "list"}, {"reference-values", "list"}, {"reference-values", "delete", "g"}} {
		if _, err := runAttestationCLI(t, args...); err == nil {
			t.Fatalf("%v did not report the store failure", args)
		}
	}

	attestationClientFactory = func() (quicbaas.AttestationService, error) { return nil, errors.New("no client") }
	if _, err := runAttestationCLI(t, "endorsement", "list"); err == nil {
		t.Fatal("a factory failure must fail the command")
	}
	for _, e := range []error{&InvalidPCRFlagError{Value: "x"}, &AttestationFileError{Flag: "f", Cause: os.ErrNotExist}} {
		if e.Error() == "" {
			t.Fatal("empty error message")
		}
	}
	if !errors.Is(&AttestationFileError{Flag: "f", Cause: os.ErrNotExist}, os.ErrNotExist) {
		t.Fatal("AttestationFileError does not unwrap")
	}
}

// appraiseArgs builds an appraise-quote invocation with readable evidence
// files plus the given extra flags.
func appraiseArgs(t *testing.T, dir string, extra ...string) []string {
	t.Helper()
	file := writeFixture(t, dir, "evidence", []byte("x"))
	return append([]string{"appraise-quote", "--ak-cert", file, "--quote", file, "--signature", file, "--reference-values", "g"}, extra...)
}
