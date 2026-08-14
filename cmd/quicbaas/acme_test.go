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

// Package main: command-handler coverage for acme.go.
package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// Mocks
// =============================================================================

// mockACMEServerClient implements quicbaas.ACMEServerClient.
type mockACMEServerClient struct {
	registeredName string
	registeredFn   quicbaas.ACMEChallengeVerifierFunc
	registerErr    error

	issueCSR       []byte
	issueType      string
	issueValue     string
	issueResp      *quicbaas.ACMEIssueResponse
	issueErr       error
	issueCallCount int
}

func (m *mockACMEServerClient) RegisterChallenge(_ context.Context, name string, verifier quicbaas.ACMEChallengeVerifierFunc) error {
	m.registeredName = name
	m.registeredFn = verifier
	return m.registerErr
}

func (m *mockACMEServerClient) Issue(_ context.Context, csrPEM []byte, challengeType, challengeValue string) (*quicbaas.ACMEIssueResponse, error) {
	m.issueCallCount++
	m.issueCSR, m.issueType, m.issueValue = csrPEM, challengeType, challengeValue
	if m.issueErr != nil {
		return nil, m.issueErr
	}
	if m.issueResp != nil {
		return m.issueResp, nil
	}
	return &quicbaas.ACMEIssueResponse{
		CertificatePEM: []byte("-----BEGIN CERTIFICATE-----\nLEAF\n-----END CERTIFICATE-----\n"),
		ChainPEM:       []byte("-----BEGIN CERTIFICATE-----\nCHAIN\n-----END CERTIFICATE-----\n"),
		SerialNumber:   "7a4f3c1d",
		NotBefore:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:       time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}, nil
}

// mockACMEProtocolClient implements quicbaas.ACMEClientAPI.
type mockACMEProtocolClient struct {
	orderIDs      []quicbaas.ACMEOrderIdentifier
	orderType     string
	orderErr      error
	finalizeURL   string
	finalizeCSR   []byte
	finalizeErr   error
	renewCertURL  string
	renewErr      error
	certPEMResult []byte
}

func (m *mockACMEProtocolClient) NewOrder(_ context.Context, identifiers []quicbaas.ACMEOrderIdentifier, challengeType string) (*quicbaas.ACMEOrder, error) {
	m.orderIDs, m.orderType = identifiers, challengeType
	if m.orderErr != nil {
		return nil, m.orderErr
	}
	return &quicbaas.ACMEOrder{
		ID:          42,
		Status:      quicbaas.ACMEStatusPending,
		Identifiers: identifiers,
		Finalize:    "/acme/order/42/finalize",
		URL:         "/acme/order/42",
	}, nil
}

func (m *mockACMEProtocolClient) FinalizeOrder(_ context.Context, orderURL string, csrPEM []byte) ([]byte, error) {
	m.finalizeURL, m.finalizeCSR = orderURL, csrPEM
	if m.finalizeErr != nil {
		return nil, m.finalizeErr
	}
	return m.result(), nil
}

func (m *mockACMEProtocolClient) Renew(_ context.Context, certURL string) ([]byte, error) {
	m.renewCertURL = certURL
	if m.renewErr != nil {
		return nil, m.renewErr
	}
	return m.result(), nil
}

func (m *mockACMEProtocolClient) result() []byte {
	if m.certPEMResult != nil {
		return m.certPEMResult
	}
	return []byte("-----BEGIN CERTIFICATE-----\nLEAF\n-----END CERTIFICATE-----\n")
}

// =============================================================================
// Helpers
// =============================================================================

// withMockACME installs both ACME factories and restores prior state.
func withMockACME(t *testing.T, server *mockACMEServerClient, protocol *mockACMEProtocolClient) {
	t.Helper()
	origServer := acmeServerClientFactory
	origProtocol := acmeProtocolClientFactory
	origTimeout := globalTimeout
	origOutput := acmeOutputFmt

	acmeServerClientFactory = func() (quicbaas.ACMEServerClient, error) { return server, nil }
	acmeProtocolClientFactory = func() (quicbaas.ACMEClientAPI, error) { return protocol, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	acmeOutputFmt = "table"

	t.Cleanup(func() {
		acmeServerClientFactory = origServer
		acmeProtocolClientFactory = origProtocol
		globalTimeout = origTimeout
		acmeOutputFmt = origOutput
		resetACMEFlags()
	})
}

// withFailingACMEFactories installs factories that always fail.
func withFailingACMEFactories(t *testing.T, err error) {
	t.Helper()
	origServer := acmeServerClientFactory
	origProtocol := acmeProtocolClientFactory
	acmeServerClientFactory = func() (quicbaas.ACMEServerClient, error) { return nil, err }
	acmeProtocolClientFactory = func() (quicbaas.ACMEClientAPI, error) { return nil, err }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	t.Cleanup(func() {
		acmeServerClientFactory = origServer
		acmeProtocolClientFactory = origProtocol
		resetACMEFlags()
	})
}

// resetACMEFlags clears every package-level acme flag.
func resetACMEFlags() {
	acmeCSRFile = ""
	acmeChallengeType = ""
	acmeChallengeValue = ""
	acmeOrderURL = ""
	acmeIdentifiers = nil
	acmeCertOut = ""
}

const testCSRPEM = "-----BEGIN CERTIFICATE REQUEST-----\nTESTCSR\n-----END CERTIFICATE REQUEST-----\n"

// csrFile writes a CSR PEM to a temp file and returns its path.
func csrFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "req.csr")
	require.NoError(t, os.WriteFile(path, []byte(testCSRPEM), 0o600))
	return path
}

// =============================================================================
// Command construction
// =============================================================================

func TestNewACMECommand_VerbTree(t *testing.T) {
	cmd := newACMECommand()
	require.Equal(t, "acme", cmd.Use)

	got := make(map[string]bool)
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}
	for _, verb := range []string{"register-challenge", "issue", "new-order", "finalize-order", "renew"} {
		assert.True(t, got[verb], "acme is missing the %q verb", verb)
	}
	assert.Len(t, cmd.Commands(), 5)

	require.NotNil(t, cmd.PersistentFlags().Lookup("output"))
	assert.Contains(t, cmd.Long, "TRANSPORT EXCEPTION",
		"the help text must document the REST/embedded-only design exception")
	assert.Contains(t, cmd.Long, "HTTP-native")
}

func TestACMEChallengeNames_AreSortedAndComplete(t *testing.T) {
	names := acmeChallengeNames()
	require.Len(t, names, 4)
	assert.Equal(t, []string{"dns-01", "endorse-01", "enroll-01", "http-01"}, names)
	for _, name := range names {
		assert.NotNil(t, acmeVerifiers[name], "verifier for %q must be non-nil", name)
	}
}

// =============================================================================
// register-challenge
// =============================================================================

func TestRunACMERegisterChallenge_Success(t *testing.T) {
	server := &mockACMEServerClient{}
	withMockACME(t, server, &mockACMEProtocolClient{})

	var buf bytes.Buffer
	require.NoError(t, runACMERegisterChallenge(captureCmd(&buf), []string{"http-01"}))

	assert.Equal(t, "http-01", server.registeredName)
	assert.NotNil(t, server.registeredFn, "the SDK requires a non-nil verifier")
	assert.Contains(t, buf.String(), "challenge http-01 registered")
}

func TestRunACMERegisterChallenge_UnknownChallenge(t *testing.T) {
	withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})

	err := runACMERegisterChallenge(captureCmd(&bytes.Buffer{}), []string{"tls-alpn-01"})
	require.Error(t, err)
	var typed *ACMEUnknownChallengeError
	require.True(t, stderrors.As(err, &typed))
	assert.Contains(t, err.Error(), "http-01")
}

func TestRunACMERegisterChallenge_BlankArg(t *testing.T) {
	withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})

	err := runACMERegisterChallenge(captureCmd(&bytes.Buffer{}), []string{"   "})
	require.Error(t, err)
	var typed *ACMEArgRequiredError
	require.True(t, stderrors.As(err, &typed))
}

func TestACMEVerifyEnroll01_FailsClosedOnUnsupportedCA(t *testing.T) {
	// A CA handle that cannot verify TCG-CSR-IDEVID must be rejected rather
	// than silently skipped.
	err := acmeVerifyEnroll01(nil, struct{}{}, "example.com", "80", "token", "keyauth")
	require.Error(t, err)
	var typed *ACMEEnrollCAUnsupportedError
	require.True(t, stderrors.As(err, &typed))
}

// =============================================================================
// issue
// =============================================================================

func TestRunACMEIssue_FlagValidation(t *testing.T) {
	tests := []struct {
		name          string
		csr           string
		challengeType string
		wantFlag      string
	}{
		{name: "missing csr", challengeType: "http-01", wantFlag: "csr"},
		{name: "missing challenge type", csr: "/tmp/x.csr", wantFlag: "challenge-type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})
			acmeCSRFile, acmeChallengeType = tt.csr, tt.challengeType

			err := runACMEIssue(captureCmd(&bytes.Buffer{}), nil)
			require.Error(t, err)
			var typed *ACMEFlagRequiredError
			require.True(t, stderrors.As(err, &typed))
			assert.Equal(t, tt.wantFlag, typed.Flag)
		})
	}
}

func TestRunACMEIssue_UnreadableCSR(t *testing.T) {
	withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})
	acmeCSRFile = filepath.Join(t.TempDir(), "absent.csr")
	acmeChallengeType = "http-01"

	err := runACMEIssue(captureCmd(&bytes.Buffer{}), nil)
	require.Error(t, err)
	var typed *ACMEReadError
	require.True(t, stderrors.As(err, &typed))
}

func TestRunACMEIssue_Success(t *testing.T) {
	server := &mockACMEServerClient{}
	withMockACME(t, server, &mockACMEProtocolClient{})
	acmeCSRFile = csrFile(t)
	acmeChallengeType = "http-01"
	acmeChallengeValue = "api.example.com"

	var buf bytes.Buffer
	require.NoError(t, runACMEIssue(captureCmd(&buf), nil))

	assert.Equal(t, []byte(testCSRPEM), server.issueCSR)
	assert.Equal(t, "http-01", server.issueType)
	assert.Equal(t, "api.example.com", server.issueValue)
	assert.Contains(t, buf.String(), "7a4f3c1d")
	assert.Contains(t, buf.String(), "Certificate PEM:")
}

func TestRunACMEIssue_ReadsCSRFromStdin(t *testing.T) {
	server := &mockACMEServerClient{}
	withMockACME(t, server, &mockACMEProtocolClient{})
	acmeCSRFile = "-"
	acmeChallengeType = "dns-01"

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetIn(bytes.NewReader([]byte(testCSRPEM)))

	require.NoError(t, runACMEIssue(cmd, nil))
	assert.Equal(t, []byte(testCSRPEM), server.issueCSR)
}

func TestRunACMEIssue_WritesCertToFile(t *testing.T) {
	withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})
	acmeCSRFile = csrFile(t)
	acmeChallengeType = "http-01"
	acmeCertOut = filepath.Join(t.TempDir(), "issued.crt")

	require.NoError(t, runACMEIssue(captureCmd(&bytes.Buffer{}), nil))

	body, err := os.ReadFile(acmeCertOut)
	require.NoError(t, err)
	assert.Contains(t, string(body), "LEAF")
}

func TestRunACMEIssue_WriteErrorIsTyped(t *testing.T) {
	withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})
	acmeCSRFile = csrFile(t)
	acmeChallengeType = "http-01"
	acmeCertOut = t.TempDir() // a directory can never be overwritten as a file

	err := runACMEIssue(captureCmd(&bytes.Buffer{}), nil)
	require.Error(t, err)
	var typed *ACMEWriteError
	require.True(t, stderrors.As(err, &typed))
}

// =============================================================================
// new-order
// =============================================================================

func TestParseACMEIdentifiers(t *testing.T) {
	ids, err := parseACMEIdentifiers([]string{"dns:api.example.com", " ip : 10.0.0.1 "})
	require.NoError(t, err)
	require.Len(t, ids, 2)
	assert.Equal(t, "dns", ids[0].Type)
	assert.Equal(t, "api.example.com", ids[0].Value)
	assert.Equal(t, "ip", ids[1].Type)
	assert.Equal(t, "10.0.0.1", ids[1].Value)
}

func TestParseACMEIdentifiers_Invalid(t *testing.T) {
	for _, bad := range []string{"api.example.com", ":value", "dns:", ""} {
		t.Run(bad, func(t *testing.T) {
			_, err := parseACMEIdentifiers([]string{bad})
			require.Error(t, err)
			var typed *ACMEIdentifierFormatError
			require.True(t, stderrors.As(err, &typed))
		})
	}
}

func TestRunACMENewOrder_RequiresIdentifier(t *testing.T) {
	withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})

	err := runACMENewOrder(captureCmd(&bytes.Buffer{}), nil)
	require.Error(t, err)
	var typed *ACMEFlagRequiredError
	require.True(t, stderrors.As(err, &typed))
	assert.Equal(t, "identifier", typed.Flag)
}

func TestRunACMENewOrder_Success(t *testing.T) {
	protocol := &mockACMEProtocolClient{}
	withMockACME(t, &mockACMEServerClient{}, protocol)
	acmeIdentifiers = []string{"dns:a.example.com", "dns:b.example.com"}
	acmeChallengeType = "http-01"

	var buf bytes.Buffer
	require.NoError(t, runACMENewOrder(captureCmd(&buf), nil))

	require.Len(t, protocol.orderIDs, 2)
	assert.Equal(t, "http-01", protocol.orderType)
	assert.Contains(t, buf.String(), "/acme/order/42/finalize")
}

// =============================================================================
// finalize-order
// =============================================================================

func TestRunACMEFinalizeOrder_FlagValidation(t *testing.T) {
	tests := []struct {
		name     string
		orderURL string
		csr      string
		wantFlag string
	}{
		{name: "missing order url", csr: "/tmp/x.csr", wantFlag: "order-url"},
		{name: "missing csr", orderURL: "/acme/order/42", wantFlag: "csr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})
			acmeOrderURL, acmeCSRFile = tt.orderURL, tt.csr

			err := runACMEFinalizeOrder(captureCmd(&bytes.Buffer{}), nil)
			require.Error(t, err)
			var typed *ACMEFlagRequiredError
			require.True(t, stderrors.As(err, &typed))
			assert.Equal(t, tt.wantFlag, typed.Flag)
		})
	}
}

func TestRunACMEFinalizeOrder_Success(t *testing.T) {
	protocol := &mockACMEProtocolClient{}
	withMockACME(t, &mockACMEServerClient{}, protocol)
	acmeOrderURL = "/acme/order/42"
	acmeCSRFile = csrFile(t)

	var buf bytes.Buffer
	require.NoError(t, runACMEFinalizeOrder(captureCmd(&buf), nil))

	assert.Equal(t, "/acme/order/42", protocol.finalizeURL)
	assert.Equal(t, []byte(testCSRPEM), protocol.finalizeCSR)
	assert.Contains(t, buf.String(), "LEAF")
}

// =============================================================================
// renew
// =============================================================================

func TestRunACMERenew_Success(t *testing.T) {
	protocol := &mockACMEProtocolClient{}
	withMockACME(t, &mockACMEServerClient{}, protocol)

	var buf bytes.Buffer
	require.NoError(t, runACMERenew(captureCmd(&buf), []string{" 7a4f3c1d "}))

	assert.Equal(t, "7a4f3c1d", protocol.renewCertURL)
	assert.Contains(t, buf.String(), "LEAF")
}

func TestRunACMERenew_BlankArg(t *testing.T) {
	withMockACME(t, &mockACMEServerClient{}, &mockACMEProtocolClient{})

	err := runACMERenew(captureCmd(&bytes.Buffer{}), []string{"  "})
	require.Error(t, err)
	var typed *ACMEArgRequiredError
	require.True(t, stderrors.As(err, &typed))
}

// =============================================================================
// Error propagation
// =============================================================================

func TestRunACME_FactoryErrorPropagates(t *testing.T) {
	sentinel := stderrors.New("no acme transport")
	withFailingACMEFactories(t, sentinel)
	csr := csrFile(t)

	acmeCSRFile, acmeChallengeType = csr, "http-01"
	require.ErrorIs(t, runACMEIssue(captureCmd(&bytes.Buffer{}), nil), sentinel)
	require.ErrorIs(t, runACMERegisterChallenge(captureCmd(&bytes.Buffer{}), []string{"http-01"}), sentinel)

	acmeIdentifiers = []string{"dns:api.example.com"}
	require.ErrorIs(t, runACMENewOrder(captureCmd(&bytes.Buffer{}), nil), sentinel)

	acmeOrderURL, acmeCSRFile = "/acme/order/1", csr
	require.ErrorIs(t, runACMEFinalizeOrder(captureCmd(&bytes.Buffer{}), nil), sentinel)
	require.ErrorIs(t, runACMERenew(captureCmd(&bytes.Buffer{}), []string{"7a"}), sentinel)
}

func TestRunACME_ServerErrorPropagates(t *testing.T) {
	sentinel := stderrors.New("acme boom")
	withMockACME(t,
		&mockACMEServerClient{issueErr: sentinel, registerErr: sentinel},
		&mockACMEProtocolClient{orderErr: sentinel, finalizeErr: sentinel, renewErr: sentinel},
	)
	acmeCSRFile, acmeChallengeType = csrFile(t), "http-01"

	require.ErrorIs(t, runACMEIssue(captureCmd(&bytes.Buffer{}), nil), sentinel)
	require.ErrorIs(t, runACMERegisterChallenge(captureCmd(&bytes.Buffer{}), []string{"http-01"}), sentinel)
	require.ErrorIs(t, runACMERenew(captureCmd(&bytes.Buffer{}), []string{"7a"}), sentinel)
}

// =============================================================================
// Formatting
// =============================================================================

func TestFormatACMEIssue_Formats(t *testing.T) {
	resp := &quicbaas.ACMEIssueResponse{
		CertificatePEM: []byte("LEAF"),
		ChainPEM:       []byte("CHAIN"),
		SerialNumber:   "abc",
		NotBefore:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:       time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, format := range []string{"table", "json", "yaml", ""} {
		t.Run("format="+format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatACMEIssue(resp, format, &buf))
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestFormatACMEIssue_NilAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatACMEIssue(nil, "table", &buf))
	assert.Contains(t, buf.String(), "(no response)")

	buf.Reset()
	require.Error(t, formatACMEIssue(&quicbaas.ACMEIssueResponse{}, "toml", &buf))
}

func TestFormatACMEOrder_NilAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatACMEOrder(nil, "table", &buf))
	assert.Contains(t, buf.String(), "(no order)")

	buf.Reset()
	require.Error(t, formatACMEOrder(&quicbaas.ACMEOrder{}, "toml", &buf))
}

func TestFormatACMECertificate_Formats(t *testing.T) {
	for _, format := range []string{"table", "json", "yaml", ""} {
		t.Run("format="+format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatACMECertificate([]byte("LEAF"), format, &buf))
			assert.Contains(t, buf.String(), "LEAF")
		})
	}

	var buf bytes.Buffer
	require.Error(t, formatACMECertificate([]byte("LEAF"), "toml", &buf))
}

func TestWriteTrailingNewline(t *testing.T) {
	var buf bytes.Buffer
	writeTrailingNewline(&buf, []byte("abc"))
	assert.Equal(t, "abc\n", buf.String())

	buf.Reset()
	writeTrailingNewline(&buf, []byte("abc\n"))
	assert.Equal(t, "abc\n", buf.String())

	buf.Reset()
	writeTrailingNewline(&buf, nil)
	assert.Equal(t, "\n", buf.String())
}
