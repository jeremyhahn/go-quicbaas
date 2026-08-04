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

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	xkmssecure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// sdkAgentCertIssuer satisfies service.AgentCertIssuer by composing
// the go-xkms CA REST surface that quicbaasd now mounts at
// /api/v1/xkms/ca/*. The issuer generates the agent's keypair locally
// (so the private key never leaves this process), builds a PKCS#10
// CSR, and POSTs it to /api/v1/xkms/ca/sign-csr — where the
// production-tested go-xkms HandleSignCSR handler verifies and signs
// it. The response contains only public material (cert + chain); the
// private key bytes are mlocked in a *secure.Buffer before the raw
// PEM leaves our stack frame.
type sdkAgentCertIssuer struct {
	baseURL    string
	httpClient *http.Client
}

// ErrInvalidCABundle is returned when --tls-ca contents cannot be
// parsed as PEM-encoded certificates.
var ErrInvalidCABundle = errors.New("agent deploy: invalid CA bundle in --tls-ca")

// ErrEmptyCABundle is returned when the server's CA bundle endpoint
// responded successfully but produced an empty PEM payload.
var ErrEmptyCABundle = errors.New("agent deploy: CA bundle is empty")

// newSDKAgentCertIssuer derives the HTTP transport from the top-level
// CLI flags. It reuses --address, --tls, --tls-ca, --tls-cert,
// --tls-key, --tls-skip-verify exactly like the SDK client does.
func newSDKAgentCertIssuer() (*sdkAgentCertIssuer, error) {
	addr, err := resolveServerBaseURL()
	if err != nil {
		return nil, err
	}
	tlsCfg, err := buildIssuerTLSConfig()
	if err != nil {
		return nil, err
	}
	return &sdkAgentCertIssuer{
		baseURL: addr,
		httpClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

func resolveServerBaseURL() (string, error) {
	addr := address
	if addr == "" {
		return "", errors.New("agent deploy: --address is required to reach the QuicBaaS CA")
	}
	if len(addr) > 0 && addr[0] == '/' {
		return "", errors.New("agent deploy: Unix socket address not supported for REST CA calls — use https://host:port")
	}
	return addr, nil
}

// buildIssuerTLSConfig mirrors the SDK client's TLS resolution rules so the
// CA-issuer HTTP client respects the same --tls* flag surface. It auto-
// discovers the daemon's CA chain from --data-dir/certs/ca-chain.crt when
// no explicit --tls-ca is given. QuicBaaS bans insecure TLS — a CA must be
// resolvable (explicit or auto-discovered) when TLS is requested.
func buildIssuerTLSConfig() (*tls.Config, error) {
	caFile := tlsCA
	if caFile == "" && dataDir != "" {
		autoCA := filepath.Join(dataDir, "certs", "ca-chain.crt")
		if _, statErr := os.Stat(autoCA); statErr == nil {
			caFile = autoCA
		}
	}

	// No TLS-related flag given and no auto-discovered CA: leave the HTTP
	// transport at its zero value (system roots).
	if !tlsEnabled && caFile == "" && tlsCert == "" {
		return nil, nil
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS13}
	if caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, &OpError{Op: fmt.Sprintf("read CA cert %s", caFile), Cause: err}
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, ErrInvalidCABundle
		}
		cfg.RootCAs = pool
	}
	if tlsCert != "" && tlsKey != "" {
		cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
		if err != nil {
			return nil, &OpError{Op: "load --tls-cert/--tls-key", Cause: err}
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// xkmsSignCSRRequest mirrors go-xkms pkg/api/transport.SignCSRRequest.
type xkmsSignCSRRequest struct {
	CSRPEM       []byte `json:"csr_pem"`
	Profile      string `json:"profile,omitempty"`
	ValidityDays int    `json:"validity_days,omitempty"`
	TenantID     string `json:"tenant_id,omitempty"`
}

// xkmsSignCSRResponse mirrors go-xkms pkg/api/transport.SignCSRResponse.
type xkmsSignCSRResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	ChainPEM       []byte `json:"chain_pem"`
	SerialNumber   string `json:"serial_number"`
}

// xkmsCABundleResponse mirrors go-xkms pkg/api/transport.GetCABundleResponse.
type xkmsCABundleResponse struct {
	BundlePEM []byte `json:"bundle_pem"`
}

// IssueAgentCertificate generates an ECDSA-P256 keypair locally, builds
// a CSR with the agent's subject and SANs, and POSTs it to the CA for
// signing. The server sees only the public key; the private key is
// mlocked throughout.
func (i *sdkAgentCertIssuer) IssueAgentCertificate(ctx context.Context, req *service.AgentCertificateRequest) (*service.IssuedAgentCertificate, error) {
	if req == nil {
		return nil, service.ErrAgentCertRequestNil
	}
	if req.TenantID == "" {
		return nil, &service.AgentCertRequestFieldError{Field: "tenant_id"}
	}
	if req.Hostname == "" {
		return nil, &service.AgentCertRequestFieldError{Field: "hostname"}
	}

	subject := req.Subject
	cn := subject.CommonName
	if cn == "" {
		cn = "agent-" + req.Hostname
	}
	org := subject.Organization
	if org == "" {
		org = req.TenantID
	}
	ou := subject.OrganizationalUnit
	if ou == "" {
		ou = "agents"
	}
	ttl := req.ValidityDays
	if ttl <= 0 {
		ttl = 365
	}

	dnsNames := make([]string, 0, 1+len(req.ExtraDNSNames))
	dnsNames = append(dnsNames, req.Hostname)
	dnsNames = append(dnsNames, req.ExtraDNSNames...)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, &OpError{Op: "agent deploy: generate keypair", Cause: err}
	}

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:         cn,
			Organization:       []string{org},
			OrganizationalUnit: []string{ou},
		},
		DNSNames:           dnsNames,
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, priv)
	if err != nil {
		return nil, &OpError{Op: "agent deploy: build CSR", Cause: err}
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	signedCert, chainPEM, err := i.signCSR(ctx, &xkmsSignCSRRequest{
		CSRPEM:       csrPEM,
		Profile:      "client",
		ValidityDays: ttl,
		TenantID:     req.TenantID,
	})
	if err != nil {
		return nil, err
	}

	caPEM := chainPEM
	if len(caPEM) == 0 {
		caPEM, err = i.fetchCABundle(ctx)
		if err != nil {
			return nil, &OpError{Op: "agent deploy: fetch CA bundle", Cause: err}
		}
	}

	privKeyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, &OpError{Op: "agent deploy: marshal private key", Cause: err}
	}
	privKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privKeyDER})

	keyBuf, err := xkmssecure.NewBufferFromBytes(privKeyPEM)
	if err != nil {
		return nil, &OpError{Op: "agent deploy: secure private key", Cause: err}
	}
	// Wipe the heap copy so plaintext key bytes are not reachable.
	for j := range privKeyPEM {
		privKeyPEM[j] = 0
	}

	parsed, err := x509.ParseCertificate(decodeFirstPEMBlock(signedCert))
	if err != nil {
		keyBuf.Destroy()
		return nil, &OpError{Op: "agent deploy: parse issued certificate", Cause: err}
	}

	return &service.IssuedAgentCertificate{
		CertPEM:    append([]byte(nil), signedCert...),
		PrivateKey: keyBuf,
		CAPEM:      append([]byte(nil), caPEM...),
		ParsedCert: parsed,
	}, nil
}

// decodeFirstPEMBlock returns the DER bytes of the first PEM block in p,
// or nil when no block is present.
func decodeFirstPEMBlock(p []byte) []byte {
	block, _ := pem.Decode(p)
	if block == nil {
		return nil
	}
	return block.Bytes
}

// signCSR POSTs the CSR to /api/v1/xkms/ca/sign-csr and returns the signed
// leaf certificate PEM along with any chain PEM the CA included.
func (i *sdkAgentCertIssuer) signCSR(ctx context.Context, req *xkmsSignCSRRequest) ([]byte, []byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, &OpError{Op: "agent deploy: marshal sign-csr", Cause: err}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, i.baseURL+"/api/v1/xkms/ca/sign", bytes.NewReader(body))
	if err != nil {
		return nil, nil, &OpError{Op: "agent deploy: build sign-csr request", Cause: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := i.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, &OpError{Op: "agent deploy: sign-csr call", Cause: err}
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, &OpError{Op: "agent deploy: read sign-csr response", Cause: err}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, &MessageError{Message: fmt.Sprintf("agent deploy: sign-csr HTTP %d: %s", resp.StatusCode, string(respBody))}
	}
	var out xkmsSignCSRResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, nil, &OpError{Op: "agent deploy: decode sign-csr response", Cause: err}
	}
	return out.CertificatePEM, out.ChainPEM, nil
}

// fetchCABundle retrieves the CA bundle from /api/v1/xkms/ca/bundle. Used
// when the sign-csr response did not embed a chain PEM.
func (i *sdkAgentCertIssuer) fetchCABundle(ctx context.Context) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, i.baseURL+"/api/v1/xkms/ca/bundle", nil)
	if err != nil {
		return nil, &OpError{Op: "build ca-bundle request", Cause: err}
	}
	httpReq.Header.Set("Accept", "application/json")
	resp, err := i.httpClient.Do(httpReq)
	if err != nil {
		return nil, &OpError{Op: "ca-bundle call", Cause: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &OpError{Op: "read ca-bundle", Cause: err}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &MessageError{Message: fmt.Sprintf("ca-bundle HTTP %d: %s", resp.StatusCode, string(body))}
	}
	var out xkmsCABundleResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, &OpError{Op: "decode ca-bundle", Cause: err}
	}
	if len(out.BundlePEM) == 0 {
		return nil, ErrEmptyCABundle
	}
	return out.BundlePEM, nil
}
