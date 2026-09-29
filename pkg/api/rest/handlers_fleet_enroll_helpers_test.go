//go:build rest

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

package rest

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestSanitizeAppName(t *testing.T) {
	// Delegates to iam.SanitizeAppName; assert it lowercases and replaces
	// disallowed characters with a deterministic, non-empty result.
	got := sanitizeAppName("Agent Host.01")
	assert.NotEmpty(t, got)
	assert.NotContains(t, got, " ")
}

func TestBackendsFromRegisterMetadata(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]string
		want []string
	}{
		{"nil_map", nil, nil},
		{"missing_key", map[string]string{"other": "x"}, nil},
		{"empty_value", map[string]string{"backends": ""}, nil},
		{"single", map[string]string{"backends": "kv"}, []string{"kv"}},
		{"multi_trimmed", map[string]string{"backends": " kv , blob ,"}, []string{"kv", "blob"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, backendsFromRegisterMetadata(tt.meta))
		})
	}
}

func TestMergeRegisterMetadata(t *testing.T) {
	req := &fleetEnrollRequest{
		SandboxPolicy: "strict",
		Hostname:      "agent01",
		Metadata:      map[string]string{"region": "us", "hostname": "should-not-override"},
	}
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(255),
		Subject:      pkix.Name{CommonName: "agent-10.0.0.1"},
	}
	out := mergeRegisterMetadata(req, cert)

	assert.Equal(t, "strict", out["sandbox_policy"])
	assert.Equal(t, "agent01", out["hostname"], "request metadata must not override reserved key")
	assert.Equal(t, "ff", out["cert_serial"])
	assert.Equal(t, "agent-10.0.0.1", out["cert_cn"])
	assert.Equal(t, "us", out["region"])
}

func TestMergeRegisterMetadata_NilCert(t *testing.T) {
	req := &fleetEnrollRequest{SandboxPolicy: "none", Hostname: "h"}
	out := mergeRegisterMetadata(req, nil)
	assert.Equal(t, "none", out["sandbox_policy"])
	_, hasSerial := out["cert_serial"]
	assert.False(t, hasSerial)
}

func TestExtractCertTenant(t *testing.T) {
	assert.Equal(t, "", extractCertTenant(nil))
	assert.Equal(t, "", extractCertTenant(&x509.Certificate{}))
	cert := &x509.Certificate{Subject: pkix.Name{Organization: []string{" acme "}}}
	assert.Equal(t, "acme", extractCertTenant(cert))
}

func TestRespondAgentRegisterError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/fleet/enroll", nil)
	respondFleetEnrollError(w, r, 401, "invalid_client", "missing peer cert")

	assert.Equal(t, 401, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Contains(t, w.Body.String(), "invalid_client")
	assert.Contains(t, w.Body.String(), "missing peer cert")
}

func TestDialableAgentHost(t *testing.T) {
	certCN := func(cn string, dns ...string) *x509.Certificate {
		return &x509.Certificate{Subject: pkix.Name{CommonName: cn}, DNSNames: dns}
	}
	tests := []struct {
		name     string
		bindAddr string
		hostname string
		peer     *x509.Certificate
		want     string
	}{
		{"empty", "", "h", nil, ""},
		{"unparseable", "no-port", "h", nil, "no-port"},
		{"concrete_host", "10.0.0.5:9091", "h", nil, "10.0.0.5:9091"},
		{"wildcard_cert_cn", "0.0.0.0:9091", "h", certCN("agent-10.0.0.9"), "10.0.0.9:9091"},
		{"wildcard_cert_dns", "0.0.0.0:9091", "h", certCN("plain", "dns.example"), "dns.example:9091"},
		{"wildcard_hostname_fallback", "0.0.0.0:9091", "host01", nil, "host01:9091"},
		{"wildcard_no_hint", "0.0.0.0:9091", "", nil, "0.0.0.0:9091"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, dialableFleetNodeHost(tt.bindAddr, tt.hostname, tt.peer))
		})
	}
}

func TestIsAgentAlreadyExistsError(t *testing.T) {
	assert.False(t, isFleetNodeAlreadyExistsError(nil))
	assert.False(t, isFleetNodeAlreadyExistsError(errors.New("other")))
	assert.True(t, isFleetNodeAlreadyExistsError(orchestration.ErrFleetNodeAlreadyExists))
	assert.True(t, isFleetNodeAlreadyExistsError(&orchestration.FleetNodeAlreadyExistsError{NodeID: "a1"}))
}
