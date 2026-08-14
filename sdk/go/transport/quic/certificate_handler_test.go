// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package quic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestIsCertificateService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.Certificate", true},
		{"Certificate", true},
		{"quicbaas.KMS", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isCertificateService(tt.svc), tt.svc)
	}
}

func TestCertificateMethodToOpcode(t *testing.T) {
	cases := []struct {
		method string
		want   byte
	}{
		{"IssueCertificate", OpCertIssue},
		{"SignCSR", OpCertSignCSR},
		{"RevokeCertificate", OpCertRevoke},
		{"VerifyCertificate", OpCertVerify},
		{"GetCACertificate", OpCertGetCA},
		{"GetTLSConfig", OpCertGetTLSConfig},
	}
	for _, c := range cases {
		op, ok := MethodToOpcode("quicbaas.Certificate", c.method)
		require.True(t, ok, "method %s should resolve", c.method)
		assert.Equal(t, c.want, op, c.method)
	}
	// Certificate opcodes must match the server (0x84-0x89).
	assert.Equal(t, byte(0x84), OpCertIssue)
	assert.Equal(t, byte(0x89), OpCertGetTLSConfig)

	// Methods with no QUIC opcode must surface as unknown, not be misrouted.
	for _, m := range []string{"GetCABundle", "IsInitialized", "GenerateCRL", "IsRevoked", "CAInfo", "ListCertificates"} {
		_, ok := MethodToOpcode("quicbaas.Certificate", m)
		assert.False(t, ok, "method %s should not resolve to a QUIC opcode", m)
	}
}

// TestDecodeCertificateResponse_Issue proves the server's IssueCertificateResponse
// (emitted via EncodePayload) round-trips into the SDK
// *certificate.IssueCertificateResponse the client asserts on. The server and
// SDK types share identical snake_case JSON tags, so every field is preserved.
func TestDecodeCertificateResponse_Issue(t *testing.T) {
	nb := time.Unix(1700000000, 0).UTC()
	na := time.Unix(1700086400, 0).UTC()
	want := &service.IssueCertificateResponse{
		CertificatePEM:   "-----CERT-----",
		PrivateKeyPEM:    "-----KEY-----",
		CACertificatePEM: "-----CA-----",
		ChainPEM:         "-----CHAIN-----",
		SerialNumber:     "0a1b2c",
		NotBefore:        nb,
		NotAfter:         na,
	}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeCertificateResponse("IssueCertificate", payload)
	require.NoError(t, err)
	resp, ok := got.(*certificate.IssueCertificateResponse)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, "-----CERT-----", resp.CertificatePEM)
	assert.Equal(t, "-----KEY-----", resp.PrivateKeyPEM)
	assert.Equal(t, "0a1b2c", resp.SerialNumber)
	assert.Equal(t, nb, resp.NotBefore)
	assert.Equal(t, na, resp.NotAfter)
}

func TestDecodeCertificateResponse_AllTypedMethods(t *testing.T) {
	sign := &service.SignCSRResponse{CertificatePEM: "c", SerialNumber: "s1"}
	revoke := &service.RevokeCertificateResponse{Success: true}
	verify := &service.VerifyCertificateResponse{Valid: true, ErrorMessage: ""}
	ca := &service.GetCACertificateResponse{CACertificatePEM: "ca", CABundlePEM: "bundle", Identity: "root"}

	cases := []struct {
		method string
		in     interface{}
		assert func(t *testing.T, got interface{})
	}{
		{"SignCSR", sign, func(t *testing.T, got interface{}) {
			r, ok := got.(*certificate.SignCSRResponse)
			require.True(t, ok, "got %T", got)
			assert.Equal(t, "s1", r.SerialNumber)
		}},
		{"RevokeCertificate", revoke, func(t *testing.T, got interface{}) {
			r, ok := got.(*certificate.RevokeCertificateResponse)
			require.True(t, ok, "got %T", got)
			assert.True(t, r.Success)
		}},
		{"VerifyCertificate", verify, func(t *testing.T, got interface{}) {
			r, ok := got.(*certificate.VerifyCertificateResponse)
			require.True(t, ok, "got %T", got)
			assert.True(t, r.Valid)
		}},
		{"GetCACertificate", ca, func(t *testing.T, got interface{}) {
			r, ok := got.(*certificate.GetCACertificateResponse)
			require.True(t, ok, "got %T", got)
			assert.Equal(t, "ca", r.CACertificatePEM)
			assert.Equal(t, "root", r.Identity)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			payload, err := EncodePayload(tc.in)
			require.NoError(t, err)
			got, err := decodeCertificateResponse(tc.method, payload)
			require.NoError(t, err)
			tc.assert(t, got)
		})
	}
}

// TestDecodeCertificateResponse_GetTLSConfig proves the server's {success,message}
// acknowledgement decodes into an empty SDK GetTLSConfigResponse (the live
// *tls.Config is intentionally not transported over the wire).
func TestDecodeCertificateResponse_GetTLSConfig(t *testing.T) {
	ack := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{Success: true, Message: "ok"}
	payload, err := EncodePayload(ack)
	require.NoError(t, err)

	got, err := decodeCertificateResponse("GetTLSConfig", payload)
	require.NoError(t, err)
	resp, ok := got.(*certificate.GetTLSConfigResponse)
	require.True(t, ok, "got %T", got)
	assert.Nil(t, resp.Config)
}

func TestDecodeCertificateResponse_EmptyPayload(t *testing.T) {
	got, err := decodeCertificateResponse("IssueCertificate", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeCertificateResponse_UnknownMethod(t *testing.T) {
	_, err := decodeCertificateResponse("Bogus", []byte(`{"foo":"bar"}`))
	require.Error(t, err)
}

func TestDecodeCertificateResponse_InvalidJSON(t *testing.T) {
	_, err := decodeCertificateResponse("IssueCertificate", []byte(`{not json`))
	require.Error(t, err)
}

func TestDecodeCertificateResponse_AllInvalidJSON(t *testing.T) {
	for _, m := range []string{
		"SignCSR", "RevokeCertificate", "VerifyCertificate",
		"GetCACertificate", "GetTLSConfig",
	} {
		_, err := decodeCertificateResponse(m, []byte(`{bad`))
		require.Error(t, err, m)
	}
}
