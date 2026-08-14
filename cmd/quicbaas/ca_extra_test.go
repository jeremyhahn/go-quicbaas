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

// Package main: extended coverage for ca.go — nil-response formatters,
// JSON revoke, truncate edge cases, and write-perm error branches not
// already exercised by ca_test.go.
package main

import (
	"bytes"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	certclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// =============================================================================
// writeCAInfo / writeCertificateList / writeIssuedCertificate: nil branches
// =============================================================================

// TestWriteCAInfo_NilResponse prints a friendly marker when the SDK
// returns a nil response (defensive branch in writeCAInfo).
func TestWriteCAInfo_NilResponse(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeCAInfo(&buf, nil, "table"))
	assert.Contains(t, buf.String(), "(no CA info)")
}

// TestWriteCAInfo_JSONNoKeyBits exercises the "KeyBits == 0" branch.
func TestWriteCAInfo_JSONNoKeyBits(t *testing.T) {
	var buf bytes.Buffer
	resp := &certclient.CAInfoResponse{
		CommonName:         "Root",
		KeyType:            "Ed25519",
		KeyBits:            0,
		SignatureAlgorithm: "Ed25519",
	}
	require.NoError(t, writeCAInfo(&buf, resp, "table"))
	got := buf.String()
	assert.Contains(t, got, "Ed25519")
	// When KeyBits == 0, the "(N bits)" suffix is omitted.
	assert.NotContains(t, got, "(0 bits)")
}

// TestWriteCertificateList_NilResponse prints a friendly marker.
func TestWriteCertificateList_NilResponse(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeCertificateList(&buf, nil, "table"))
	assert.Contains(t, buf.String(), "(no certificates)")
}

// TestWriteIssuedCertificate_NilResponse prints a friendly marker.
func TestWriteIssuedCertificate_NilResponse(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeIssuedCertificate(&buf, nil, "table"))
	assert.Contains(t, buf.String(), "(no response)")
}

// TestWriteIssuedCertificate_AppendsNewlines confirms the trailing
// newline is added when the PEM body does not already end with \n.
func TestWriteIssuedCertificate_AppendsNewlines(t *testing.T) {
	var buf bytes.Buffer
	resp := &certclient.IssueCertificateResponse{
		CertificatePEM: "-----BEGIN CERTIFICATE-----\nleaf\n-----END CERTIFICATE-----",
		ChainPEM:       "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
		PrivateKeyPEM:  "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----",
		SerialNumber:   "01",
		NotBefore:      time.Now().UTC(),
		NotAfter:       time.Now().UTC().Add(time.Hour),
	}
	require.NoError(t, writeIssuedCertificate(&buf, resp, "table"))
	got := buf.String()
	assert.Contains(t, got, "Certificate PEM:")
	assert.Contains(t, got, "Chain PEM:")
	assert.Contains(t, got, "Private Key PEM:")
	// Each section's PEM body should be followed by a newline.
	assert.True(t, strings.HasSuffix(got, "\n"))
}

// TestWriteIssuedCertificate_JSON exercises the JSON branch.
func TestWriteIssuedCertificate_JSON(t *testing.T) {
	var buf bytes.Buffer
	resp := &certclient.IssueCertificateResponse{SerialNumber: "ff"}
	require.NoError(t, writeIssuedCertificate(&buf, resp, "json"))
	assert.Contains(t, buf.String(), `"serial_number": "ff"`)
}

// TestWriteCertificateList_JSON exercises the JSON branch directly.
func TestWriteCertificateList_JSON(t *testing.T) {
	var buf bytes.Buffer
	resp := &certclient.ListCertificatesResponse{
		Certificates: []certclient.CertificateSummary{{CommonName: "x"}},
		TotalCount:   1,
	}
	require.NoError(t, writeCertificateList(&buf, resp, "json"))
	assert.Contains(t, buf.String(), `"common_name": "x"`)
}

// =============================================================================
// truncate: rune-boundary edge cases
// =============================================================================

// TestTruncate_MultibyteRuneBoundary verifies truncate operates on runes,
// not bytes — important for non-ASCII names in tables.
func TestTruncate_MultibyteRuneBoundary(t *testing.T) {
	// 7 runes of 3 bytes each = 21 bytes; max = 4 runes -> first 3 + ellipsis.
	in := "你好世界测试串"
	got := truncate(in, 4)
	runes := []rune(got)
	require.Equal(t, 4, len(runes))
	assert.Equal(t, '…', runes[len(runes)-1])
}

// TestTruncate_MaxZeroOrNegative returns the input unchanged when max <= 1.
func TestTruncate_MaxZeroOrNegative(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 0))
	assert.Equal(t, "hello", truncate("hello", -1))
}

// =============================================================================
// revoke: JSON output and missing-arg branches
// =============================================================================

// TestCARevoke_JSON exercises the JSON output branch of runCARevoke.
func TestCARevoke_JSON(t *testing.T) {
	fc := &fakeCAClient{
		revokeResp: &certclient.RevokeCertificateResponse{Success: true},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "json"
	defer func() { caOutputFmt = "table" }()
	var buf bytes.Buffer
	cmd := newCARevokeCmd()
	cmd.SetOut(&buf)
	require.NoError(t, runCARevoke(cmd, []string{"abcd"}))
	assert.Contains(t, buf.String(), `"success": true`)
}

// TestCARevoke_FactoryError surfaces factory-level errors.
func TestCARevoke_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	defer withCAFactoryErr(t, want)()

	cmd := newCARevokeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCARevoke(cmd, []string{"ff"})
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestCARevoke_TransportError surfaces SDK-level errors.
func TestCARevoke_TransportError(t *testing.T) {
	want := stderrors.New("server boom")
	fc := &fakeCAClient{revokeErr: want}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	cmd := newCARevokeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCARevoke(cmd, []string{"ff"})
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// =============================================================================
// issue: factory + transport errors. Note: pflag's StringVar resets the
// package var to the registered default when the cobra cmd is created,
// so the var MUST be set AFTER constructing the command.
// =============================================================================

// TestCAIssue_FactoryError surfaces factory failures.
func TestCAIssue_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	defer withCAFactoryErr(t, want)()

	cmd := newCAIssueCmd()
	caIssueCommonName = "alpha"
	defer func() { caIssueCommonName = "" }()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAIssue(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestCAIssue_TransportError surfaces SDK errors.
func TestCAIssue_TransportError(t *testing.T) {
	want := stderrors.New("issue boom")
	fc := &fakeCAClient{issueErr: want}
	defer withFakeCAFactory(t, fc)()

	cmd := newCAIssueCmd()
	caIssueCommonName = "alpha"
	defer func() { caIssueCommonName = "" }()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAIssue(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestCAIssue_JSON exercises the JSON output branch.
func TestCAIssue_JSON(t *testing.T) {
	fc := &fakeCAClient{
		issueResp: &certclient.IssueCertificateResponse{
			SerialNumber: "feed",
			NotBefore:    time.Now().UTC(),
			NotAfter:     time.Now().UTC().Add(time.Hour),
		},
	}
	defer withFakeCAFactory(t, fc)()

	var buf bytes.Buffer
	cmd := newCAIssueCmd()
	caIssueCommonName = "alpha"
	caOutputFmt = "json"
	defer func() {
		caIssueCommonName = ""
		caOutputFmt = "table"
	}()
	cmd.SetOut(&buf)
	require.NoError(t, runCAIssue(cmd, nil))
	assert.Contains(t, buf.String(), `"serial_number": "feed"`)
}

// TestCAIssue_WhitespaceCommonName rejects whitespace-only names.
func TestCAIssue_WhitespaceCommonName(t *testing.T) {
	fc := &fakeCAClient{}
	defer withFakeCAFactory(t, fc)()

	cmd := newCAIssueCmd()
	caIssueCommonName = "   "
	defer func() { caIssueCommonName = "" }()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAIssue(cmd, nil)
	require.Error(t, err)
	var typed *CAFlagRequiredError
	require.ErrorAs(t, err, &typed)
}

// =============================================================================
// bundle: factory error + JSON branch
// =============================================================================

// TestCABundle_FactoryError surfaces factory failures.
func TestCABundle_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	defer withCAFactoryErr(t, want)()

	cmd := newCABundleCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCABundle(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestCABundle_JSON exercises the JSON output branch.
func TestCABundle_JSON(t *testing.T) {
	fc := &fakeCAClient{
		bundleResp: &certclient.GetCABundleResponse{BundlePEM: "x"},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "json"
	defer func() { caOutputFmt = "table" }()
	var buf bytes.Buffer
	cmd := newCABundleCmd()
	cmd.SetOut(&buf)
	require.NoError(t, runCABundle(cmd, nil))
	assert.Contains(t, buf.String(), `"bundle_pem"`)
}

// =============================================================================
// list: factory + transport error
// =============================================================================

// TestCAList_FactoryError surfaces factory failures.
func TestCAList_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	defer withCAFactoryErr(t, want)()

	cmd := newCAListCmd()
	caListLimit = 0
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAList(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestCAList_TransportError surfaces SDK errors.
func TestCAList_TransportError(t *testing.T) {
	want := stderrors.New("list boom")
	fc := &fakeCAClient{listErr: want}
	defer withFakeCAFactory(t, fc)()

	cmd := newCAListCmd()
	caListLimit = 0
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAList(cmd, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// =============================================================================
// export-bundle: missing path + stdout newline branch
// =============================================================================

// TestCAExportBundle_StdoutAppendsNewline ensures the stdout branch
// appends a trailing newline when the bundle lacks one.
func TestCAExportBundle_StdoutAppendsNewline(t *testing.T) {
	bundlePEM := "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----"
	fc := &fakeCAClient{
		bundleResp: &certclient.GetCABundleResponse{BundlePEM: bundlePEM},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	var buf bytes.Buffer
	cmd := newCAExportBundleCmd()
	cmd.SetOut(&buf)
	require.NoError(t, runCAExportBundle(cmd, []string{"-"}))
	got := buf.String()
	assert.True(t, strings.HasSuffix(got, "\n"))
}

// TestCAExportBundle_WhitespaceOnlyPath returns the typed missing-arg
// error rather than attempting to open a "" path.
func TestCAExportBundle_WhitespaceOnlyPath(t *testing.T) {
	fc := &fakeCAClient{}
	defer withFakeCAFactory(t, fc)()

	cmd := newCAExportBundleCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAExportBundle(cmd, []string{"   "})
	require.Error(t, err)
	var typed *CAArgRequiredError
	require.ErrorAs(t, err, &typed)
}

// TestCAExportBundle_FactoryError surfaces factory failures.
func TestCAExportBundle_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	defer withCAFactoryErr(t, want)()

	cmd := newCAExportBundleCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAExportBundle(cmd, []string{"/tmp/x.pem"})
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// =============================================================================
// CAExportWriteError edge cases
// =============================================================================

// TestCAExportWriteError_UnwrapNilCause exercises the Unwrap path when
// the underlying cause is nil. A real caller never constructs it that
// way — this pins the Unwrap contract for completeness.
func TestCAExportWriteError_UnwrapNilCause(t *testing.T) {
	e := &CAExportWriteError{Path: "/tmp/x", Err: nil}
	assert.Nil(t, e.Unwrap())
}
