// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	certclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// =============================================================================
// Test fakes
// =============================================================================

// fakeCAClient is a stand-in for quicbaas.CertificateClient. Each
// method records its last call and returns the configured response.
type fakeCAClient struct {
	caInfoResp *certclient.CAInfoResponse
	caInfoErr  error
	bundleResp *certclient.GetCABundleResponse
	bundleErr  error
	listReq    *certclient.ListCertificatesRequest
	listResp   *certclient.ListCertificatesResponse
	listErr    error
	issueReq   *certclient.IssueCertificateRequest
	issueResp  *certclient.IssueCertificateResponse
	issueErr   error
	revokeReq  *certclient.RevokeCertificateRequest
	revokeResp *certclient.RevokeCertificateResponse
	revokeErr  error
}

func (f *fakeCAClient) IssueCertificate(_ context.Context, req *certclient.IssueCertificateRequest) (*certclient.IssueCertificateResponse, error) {
	f.issueReq = req
	return f.issueResp, f.issueErr
}

func (f *fakeCAClient) SignCSR(_ context.Context, _ *certclient.SignCSRRequest) (*certclient.SignCSRResponse, error) {
	return nil, stderrors.New("SignCSR not implemented in fake")
}

func (f *fakeCAClient) RevokeCertificate(_ context.Context, req *certclient.RevokeCertificateRequest) (*certclient.RevokeCertificateResponse, error) {
	f.revokeReq = req
	return f.revokeResp, f.revokeErr
}

func (f *fakeCAClient) VerifyCertificate(_ context.Context, _ *certclient.VerifyCertificateRequest) (*certclient.VerifyCertificateResponse, error) {
	return nil, stderrors.New("VerifyCertificate not implemented in fake")
}

func (f *fakeCAClient) GetCACertificate(_ context.Context, _ *certclient.GetCACertificateRequest) (*certclient.GetCACertificateResponse, error) {
	return nil, stderrors.New("GetCACertificate not implemented in fake")
}

func (f *fakeCAClient) GetCABundle(_ context.Context, _ *certclient.GetCABundleRequest) (*certclient.GetCABundleResponse, error) {
	return f.bundleResp, f.bundleErr
}

func (f *fakeCAClient) GetTLSConfig(_ context.Context, _ *certclient.GetTLSConfigRequest) (*certclient.GetTLSConfigResponse, error) {
	return nil, stderrors.New("GetTLSConfig not implemented in fake")
}

func (f *fakeCAClient) IsInitialized(_ context.Context) (bool, error) {
	return true, nil
}

func (f *fakeCAClient) GenerateCRL(_ context.Context, _ *certclient.GenerateCRLRequest) (*certclient.GenerateCRLResponse, error) {
	return nil, stderrors.New("GenerateCRL not implemented in fake")
}

func (f *fakeCAClient) IsRevoked(_ context.Context, _ *certclient.IsRevokedRequest) (*certclient.IsRevokedResponse, error) {
	return nil, stderrors.New("IsRevoked not implemented in fake")
}

func (f *fakeCAClient) CAInfo(_ context.Context, _ *certclient.CAInfoRequest) (*certclient.CAInfoResponse, error) {
	return f.caInfoResp, f.caInfoErr
}

func (f *fakeCAClient) ListCertificates(_ context.Context, req *certclient.ListCertificatesRequest) (*certclient.ListCertificatesResponse, error) {
	f.listReq = req
	return f.listResp, f.listErr
}

// withFakeCAFactory swaps caClientFactory with a fixture returning the
// supplied fakeCAClient, restoring the previous factory afterwards.
func withFakeCAFactory(t *testing.T, fc *fakeCAClient) func() {
	t.Helper()
	orig := caClientFactory
	caClientFactory = func() (quicbaas.CertificateClient, error) {
		return fc, nil
	}
	// Ensure timeout is set so context.WithTimeout doesn't panic.
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return func() { caClientFactory = orig }
}

// withCAFactoryErr swaps caClientFactory with one that fails to
// construct the client. Used to test factory-error propagation.
func withCAFactoryErr(t *testing.T, err error) func() {
	t.Helper()
	orig := caClientFactory
	caClientFactory = func() (quicbaas.CertificateClient, error) {
		return nil, err
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return func() { caClientFactory = orig }
}

// =============================================================================
// info
// =============================================================================

func TestCAInfo_Success(t *testing.T) {
	fc := &fakeCAClient{
		caInfoResp: &certclient.CAInfoResponse{
			CommonName:         "Test Root CA",
			Subject:            "CN=Test Root CA,O=Acme",
			Issuer:             "CN=Test Root CA,O=Acme",
			SerialNumber:       "deadbeef",
			NotBefore:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			NotAfter:           time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
			IsCA:               true,
			KeyType:            "ECDSA",
			KeyBits:            256,
			SignatureAlgorithm: "ECDSA-SHA256",
			CertificatePEM:     "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n",
			CABundlePEM:        "-----BEGIN CERTIFICATE-----\nbundle\n-----END CERTIFICATE-----\n",
		},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	var buf bytes.Buffer
	cmd := newCAInfoCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := runCAInfo(cmd, nil); err != nil {
		t.Fatalf("runCAInfo: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Test Root CA", "deadbeef", "ECDSA", "256 bits", "ECDSA-SHA256"} {
		if !strings.Contains(out, want) {
			t.Errorf("info output missing %q\ngot: %s", want, out)
		}
	}
}

func TestCAInfo_JSON(t *testing.T) {
	fc := &fakeCAClient{
		caInfoResp: &certclient.CAInfoResponse{
			CommonName: "Test Root CA",
			KeyType:    "RSA",
			KeyBits:    2048,
		},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "json"
	var buf bytes.Buffer
	cmd := newCAInfoCmd()
	cmd.SetOut(&buf)
	if err := runCAInfo(cmd, nil); err != nil {
		t.Fatalf("runCAInfo json: %v", err)
	}
	if !strings.Contains(buf.String(), `"common_name": "Test Root CA"`) {
		t.Errorf("json output missing common_name field\n%s", buf.String())
	}
}

func TestCAInfo_ServerError(t *testing.T) {
	want := stderrors.New("server boom")
	fc := &fakeCAClient{caInfoErr: want}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	cmd := newCAInfoCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := runCAInfo(cmd, nil)
	if err == nil {
		t.Fatal("runCAInfo with server error: want error, got nil")
	}
	if !stderrors.Is(err, want) {
		t.Errorf("runCAInfo error chain: want %v, got %v", want, err)
	}
}

func TestCAInfo_FactoryError(t *testing.T) {
	want := stderrors.New("factory boom")
	defer withCAFactoryErr(t, want)()

	caOutputFmt = "table"
	cmd := newCAInfoCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runCAInfo(cmd, nil); err == nil || !stranges(err, want) {
		t.Fatalf("want factory error, got %v", err)
	}
}

// =============================================================================
// bundle
// =============================================================================

func TestCABundle_Success(t *testing.T) {
	fc := &fakeCAClient{
		bundleResp: &certclient.GetCABundleResponse{BundlePEM: "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----\n"},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	var buf bytes.Buffer
	cmd := newCABundleCmd()
	cmd.SetOut(&buf)
	if err := runCABundle(cmd, nil); err != nil {
		t.Fatalf("runCABundle: %v", err)
	}
	if !strings.Contains(buf.String(), "BEGIN CERTIFICATE") {
		t.Errorf("expected PEM in output, got: %s", buf.String())
	}
}

func TestCABundle_ServerError(t *testing.T) {
	want := stderrors.New("server boom")
	fc := &fakeCAClient{bundleErr: want}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	cmd := newCABundleCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runCABundle(cmd, nil); err == nil || !stderrors.Is(err, want) {
		t.Fatalf("want server error, got %v", err)
	}
}

// =============================================================================
// list
// =============================================================================

func TestCAList_Success(t *testing.T) {
	now := time.Now().UTC()
	fc := &fakeCAClient{
		listResp: &certclient.ListCertificatesResponse{
			Certificates: []certclient.CertificateSummary{
				{CommonName: "api.example.com", SerialNumber: "ab12", NotBefore: now, NotAfter: now.Add(time.Hour)},
				{CommonName: "client-1", SerialNumber: "cd34", NotBefore: now, NotAfter: now.Add(2 * time.Hour)},
			},
			TotalCount: 2,
		},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	var buf bytes.Buffer
	cmd := newCAListCmd()
	// cobra's StringVar default-initialises the package var; set AFTER
	// constructing the command so our value isn't overwritten.
	caListCommonNameFilter = "api"
	caListLimit = 0
	defer func() { caListCommonNameFilter = ""; caListLimit = 0 }()
	cmd.SetOut(&buf)
	if err := runCAList(cmd, nil); err != nil {
		t.Fatalf("runCAList: %v", err)
	}
	if fc.listReq == nil || fc.listReq.CommonNameFilter != "api" {
		t.Errorf("expected listReq.CommonNameFilter='api', got %#v", fc.listReq)
	}
	for _, want := range []string{"api.example.com", "client-1", "showing 2 of 2"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("list output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestCAList_NegativeLimit(t *testing.T) {
	fc := &fakeCAClient{}
	defer withFakeCAFactory(t, fc)()

	cmd := newCAListCmd()
	caListLimit = -1
	defer func() { caListLimit = 0 }()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAList(cmd, nil)
	if err == nil {
		t.Fatal("expected error on negative limit, got nil")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("expected 'limit' in error, got %v", err)
	}
}

func TestCAList_EmptyResult(t *testing.T) {
	fc := &fakeCAClient{
		listResp: &certclient.ListCertificatesResponse{Certificates: nil, TotalCount: 42},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	var buf bytes.Buffer
	cmd := newCAListCmd()
	cmd.SetOut(&buf)
	if err := runCAList(cmd, nil); err != nil {
		t.Fatalf("runCAList: %v", err)
	}
	if !strings.Contains(buf.String(), "(no matching certificates; total in store: 42)") {
		t.Errorf("missing empty-result line:\n%s", buf.String())
	}
}

func TestCAList_JSON(t *testing.T) {
	fc := &fakeCAClient{
		listResp: &certclient.ListCertificatesResponse{
			Certificates: []certclient.CertificateSummary{{CommonName: "alpha"}},
			TotalCount:   1,
		},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "json"
	var buf bytes.Buffer
	cmd := newCAListCmd()
	cmd.SetOut(&buf)
	if err := runCAList(cmd, nil); err != nil {
		t.Fatalf("runCAList: %v", err)
	}
	if !strings.Contains(buf.String(), `"common_name": "alpha"`) {
		t.Errorf("expected json output, got:\n%s", buf.String())
	}
}

// =============================================================================
// issue
// =============================================================================

func TestCAIssue_MissingCommonName(t *testing.T) {
	fc := &fakeCAClient{}
	defer withFakeCAFactory(t, fc)()

	caIssueCommonName = ""
	cmd := newCAIssueCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAIssue(cmd, nil)
	if err == nil {
		t.Fatal("expected error on empty common name")
	}
	if !strings.Contains(err.Error(), "common-name") {
		t.Errorf("expected common-name error, got %v", err)
	}
}

func TestCAIssue_Success(t *testing.T) {
	fc := &fakeCAClient{
		issueResp: &certclient.IssueCertificateResponse{
			CertificatePEM: "-----BEGIN CERTIFICATE-----\nleaf\n-----END CERTIFICATE-----\n",
			ChainPEM:       "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----\n",
			PrivateKeyPEM:  "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n",
			SerialNumber:   "feed",
			NotBefore:      time.Now().UTC(),
			NotAfter:       time.Now().UTC().Add(24 * time.Hour),
		},
	}
	defer withFakeCAFactory(t, fc)()

	cmd := newCAIssueCmd()
	caIssueCommonName = "api.example.com"
	caIssueOrganization = "Acme"
	caIssueDNSNames = []string{"api.example.com"}
	caIssueIPAddresses = []string{"127.0.0.1"}
	caIssueValidityDays = 30
	caIssueUsage = "server"
	caIssueKeyAlgorithm = "ecdsa"
	caIssueKeySize = 256
	caOutputFmt = "table"
	defer func() {
		caIssueCommonName = ""
		caIssueOrganization = ""
		caIssueDNSNames = nil
		caIssueIPAddresses = nil
		caIssueValidityDays = 0
		caIssueUsage = ""
		caIssueKeyAlgorithm = ""
		caIssueKeySize = 0
	}()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := runCAIssue(cmd, nil); err != nil {
		t.Fatalf("runCAIssue: %v", err)
	}
	if fc.issueReq == nil || fc.issueReq.CommonName != "api.example.com" {
		t.Fatalf("issueReq not set correctly: %#v", fc.issueReq)
	}
	if fc.issueReq.KeyAlgorithm != "ecdsa" || fc.issueReq.KeySize != 256 {
		t.Errorf("issueReq did not carry key algo/size: %#v", fc.issueReq)
	}
	if !strings.Contains(buf.String(), "feed") {
		t.Errorf("expected serial in output, got:\n%s", buf.String())
	}
}

// =============================================================================
// revoke
// =============================================================================

func TestCARevoke_MissingArg(t *testing.T) {
	fc := &fakeCAClient{}
	defer withFakeCAFactory(t, fc)()

	cmd := newCARevokeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCARevoke(cmd, []string{"   "})
	if err == nil {
		t.Fatal("expected error on whitespace-only serial")
	}
}

func TestCARevoke_Success(t *testing.T) {
	fc := &fakeCAClient{
		revokeResp: &certclient.RevokeCertificateResponse{Success: true},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	var buf bytes.Buffer
	cmd := newCARevokeCmd()
	cmd.SetOut(&buf)
	if err := runCARevoke(cmd, []string{"abcd1234"}); err != nil {
		t.Fatalf("runCARevoke: %v", err)
	}
	if fc.revokeReq == nil || fc.revokeReq.SerialNumber != "abcd1234" {
		t.Errorf("revokeReq not propagated: %#v", fc.revokeReq)
	}
	if !strings.Contains(buf.String(), "abcd1234 revoked") {
		t.Errorf("expected confirmation, got %s", buf.String())
	}
}

func TestCARevoke_ServerReportsUnsuccessful(t *testing.T) {
	fc := &fakeCAClient{
		revokeResp: &certclient.RevokeCertificateResponse{Success: false},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	var buf bytes.Buffer
	cmd := newCARevokeCmd()
	cmd.SetOut(&buf)
	if err := runCARevoke(cmd, []string{"ff"}); err != nil {
		t.Fatalf("runCARevoke: %v", err)
	}
	if !strings.Contains(buf.String(), "unsuccessful") {
		t.Errorf("expected unsuccessful message, got %s", buf.String())
	}
}

// =============================================================================
// export-bundle
// =============================================================================

func TestCAExportBundle_WritesFile(t *testing.T) {
	bundlePEM := "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----\n"
	fc := &fakeCAClient{
		bundleResp: &certclient.GetCABundleResponse{BundlePEM: bundlePEM},
	}
	defer withFakeCAFactory(t, fc)()

	dir := t.TempDir()
	out := filepath.Join(dir, "ca-bundle.pem")
	caOutputFmt = "table"

	cmd := newCAExportBundleCmd()
	cmd.SetOut(&bytes.Buffer{})
	if err := runCAExportBundle(cmd, []string{out}); err != nil {
		t.Fatalf("runCAExportBundle: %v", err)
	}
	got, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("reading written file: %v", rerr)
	}
	if string(got) != bundlePEM {
		t.Errorf("written file mismatch:\nwant: %s\ngot:  %s", bundlePEM, got)
	}
}

func TestCAExportBundle_Stdout(t *testing.T) {
	bundlePEM := "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----\n"
	fc := &fakeCAClient{
		bundleResp: &certclient.GetCABundleResponse{BundlePEM: bundlePEM},
	}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	var buf bytes.Buffer
	cmd := newCAExportBundleCmd()
	cmd.SetOut(&buf)
	if err := runCAExportBundle(cmd, []string{"-"}); err != nil {
		t.Fatalf("runCAExportBundle: %v", err)
	}
	if !strings.Contains(buf.String(), "BEGIN CERTIFICATE") {
		t.Errorf("expected PEM in stdout, got %s", buf.String())
	}
}

func TestCAExportBundle_ServerError(t *testing.T) {
	want := stderrors.New("server boom")
	fc := &fakeCAClient{bundleErr: want}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	dir := t.TempDir()
	cmd := newCAExportBundleCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAExportBundle(cmd, []string{filepath.Join(dir, "x.pem")})
	if err == nil || !stderrors.Is(err, want) {
		t.Fatalf("expected server error, got %v", err)
	}
}

func TestCAExportBundle_WriteFails(t *testing.T) {
	bundlePEM := "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----\n"
	fc := &fakeCAClient{bundleResp: &certclient.GetCABundleResponse{BundlePEM: bundlePEM}}
	defer withFakeCAFactory(t, fc)()

	caOutputFmt = "table"
	// Use a path under a non-existent directory to force WriteFile to fail.
	bad := filepath.Join(t.TempDir(), "does-not-exist", "x.pem")
	cmd := newCAExportBundleCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runCAExportBundle(cmd, []string{bad})
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
	var writeErr *CAExportWriteError
	if !stderrors.As(err, &writeErr) {
		t.Fatalf("expected *CAExportWriteError in chain, got %T: %v", err, err)
	}
}

// =============================================================================
// command tree wiring
// =============================================================================

func TestNewCACmd_HasAllVerbs(t *testing.T) {
	root := newCACmd()
	want := map[string]bool{
		"info":          true,
		"bundle":        true,
		"list":          true,
		"issue":         true,
		"revoke":        true,
		"export-bundle": true,
	}
	for _, c := range root.Commands() {
		verb := strings.Fields(c.Use)[0]
		if !want[verb] {
			t.Errorf("unexpected ca subcommand: %s", verb)
		}
		delete(want, verb)
	}
	for v := range want {
		t.Errorf("missing ca subcommand: %s", v)
	}
}

func TestNewCanonicalCACmd_DelegatesToNewCACmd(t *testing.T) {
	got := newCanonicalCACmd()
	if got == nil {
		t.Fatal("newCanonicalCACmd returned nil")
	}
	if got.Use != "ca" {
		t.Errorf("Use=%q, want 'ca'", got.Use)
	}
	if len(got.Commands()) < 6 {
		t.Errorf("expected at least 6 subcommands, got %d", len(got.Commands()))
	}
}

func TestCAArgRequiredError(t *testing.T) {
	e := &CAArgRequiredError{Verb: "revoke", Arg: "serial"}
	if !strings.Contains(e.Error(), "revoke") || !strings.Contains(e.Error(), "serial") {
		t.Errorf("error message missing parts: %s", e.Error())
	}
}

func TestCAFlagRequiredError(t *testing.T) {
	e := &CAFlagRequiredError{Verb: "issue", Flag: "common-name"}
	if !strings.Contains(e.Error(), "issue") || !strings.Contains(e.Error(), "common-name") {
		t.Errorf("error message missing parts: %s", e.Error())
	}
}

func TestCAExportWriteError(t *testing.T) {
	underlying := stderrors.New("disk full")
	e := &CAExportWriteError{Path: "/tmp/x.pem", Err: underlying}
	if !strings.Contains(e.Error(), "/tmp/x.pem") || !strings.Contains(e.Error(), "disk full") {
		t.Errorf("error message missing parts: %s", e.Error())
	}
	if !stderrors.Is(e, underlying) {
		t.Errorf("Unwrap chain broken")
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactlyTen", 10, "exactlyTen"},
		{"this is longer than ten", 10, "this is l…"},
		{"x", 1, "x"},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.max); got != c.want {
			t.Errorf("truncate(%q,%d)=%q, want %q", c.in, c.max, got, c.want)
		}
	}
}

// =============================================================================
// Helpers
// =============================================================================

// stranges (string-ranges) returns true when err's message ranges over
// want.Error(); used as a fallback when wrapping uses string-based
// formatters that defeat errors.Is.
func stranges(err, want error) bool {
	if err == nil || want == nil {
		return false
	}
	if stderrors.Is(err, want) {
		return true
	}
	return strings.Contains(err.Error(), want.Error())
}

// Confirm bigint helpers continue to compile (so the test file is not
// flagged for unused imports).
var _ = big.NewInt(0)
