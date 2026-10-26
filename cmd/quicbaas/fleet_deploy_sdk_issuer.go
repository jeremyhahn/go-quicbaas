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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// sdkFleetNodeCertIssuer satisfies service.FleetNodeCertIssuer for a
// workstation deploy. Every cryptographic step is go-xkms's: the key pair
// and PKCS#10 CSR come from service.NewFleetNodeKey, and quicbaasd signs
// the CSR at POST /api/v1/fleet/certificates as an agent of the caller's
// tenant and records it, so the deployed daemon can enroll.
//
// The key is created HERE and not asked of the server, deliberately:
// it is the deployed agent's own credential, the CA sees only the CSR,
// and the private key never crosses the network. The response contains
// only public material (cert + CA bundle); the private key bytes are
// mlocked in a *secure.Buffer before the raw PEM leaves our stack
// frame.
type sdkFleetNodeCertIssuer struct {
	baseURL    string
	httpClient *http.Client
}

// ErrInvalidCABundle is returned when --tls-ca contents cannot be
// parsed as PEM-encoded certificates.
var ErrInvalidCABundle = errors.New("agent deploy: invalid CA bundle in --tls-ca")

// ErrEmptyCABundle is returned when the server's CA bundle endpoint
// responded successfully but produced an empty PEM payload.
var ErrEmptyCABundle = errors.New("agent deploy: CA bundle is empty")

// newSDKFleetNodeCertIssuer derives the HTTP transport from the top-level
// CLI flags. It reuses --address, --tls, --tls-ca, --tls-cert and
// --tls-key exactly like the SDK client does.
func newSDKFleetNodeCertIssuer() (*sdkFleetNodeCertIssuer, error) {
	addr, err := resolveServerBaseURL()
	if err != nil {
		return nil, err
	}
	tlsCfg, err := buildIssuerTLSConfig()
	if err != nil {
		return nil, err
	}
	return &sdkFleetNodeCertIssuer{
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

// fleetCertificateRequest is the body of POST /api/v1/fleet/certificates.
type fleetCertificateRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	CSRPEM       []byte `json:"csr_pem"`
	ValidityDays int    `json:"validity_days,omitempty"`
}

// fleetCertificateResponse is the signed agent certificate and the CA
// bundle the daemon trusts.
type fleetCertificateResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	CAPEM          []byte `json:"ca_pem"`
}

// xkmsCABundleResponse mirrors go-xkms pkg/api/transport.GetCABundleResponse.
type xkmsCABundleResponse struct {
	BundlePEM []byte `json:"bundle_pem"`
}

// IssueFleetNodeCertificate builds the agent key pair and CSR here with
// service.NewFleetNodeKey and POSTs only the CSR to the fleet signing route,
// which signs it as an agent of the caller's tenant and records it for
// enrolment. The server sees only the public key; the private key is
// mlocked once encoded.
func (i *sdkFleetNodeCertIssuer) IssueFleetNodeCertificate(ctx context.Context, req *service.FleetNodeCertificateRequest) (*service.IssuedFleetNodeCertificate, error) {
	key, err := service.NewFleetNodeKey(req)
	if err != nil {
		return nil, err
	}
	signed, err := i.signFleetCSR(ctx, &fleetCertificateRequest{
		TenantID:     req.TenantID,
		CSRPEM:       key.CSRPEM,
		ValidityDays: req.ValidityDays,
	})
	if err != nil {
		return nil, err
	}
	der := decodeFirstPEMBlock(signed.CertificatePEM)
	if der == nil {
		return nil, &MessageError{Message: "agent deploy: fleet certificate response carries no certificate"}
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, &OpError{Op: "agent deploy: parse fleet certificate", Cause: err}
	}
	if len(signed.CAPEM) == 0 {
		return nil, ErrEmptyCABundle
	}
	keyBuf, err := key.PrivateKeyPEM()
	if err != nil {
		return nil, err
	}
	return &service.IssuedFleetNodeCertificate{
		CertPEM:    signed.CertificatePEM,
		PrivateKey: keyBuf,
		CAPEM:      signed.CAPEM,
		ParsedCert: leaf,
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

// signFleetCSR POSTs the CSR to /api/v1/fleet/certificates and returns the
// signed agent certificate with the CA bundle.
func (i *sdkFleetNodeCertIssuer) signFleetCSR(ctx context.Context, req *fleetCertificateRequest) (*fleetCertificateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, &OpError{Op: "agent deploy: marshal fleet csr", Cause: err}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, i.baseURL+"/api/v1/fleet/certificates", bytes.NewReader(body))
	if err != nil {
		return nil, &OpError{Op: "agent deploy: build fleet csr request", Cause: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := i.httpClient.Do(httpReq)
	if err != nil {
		return nil, &OpError{Op: "agent deploy: fleet csr call", Cause: err}
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &OpError{Op: "agent deploy: read fleet csr response", Cause: err}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &MessageError{Message: fmt.Sprintf("agent deploy: fleet csr HTTP %d: %s", resp.StatusCode, string(respBody))}
	}
	var out fleetCertificateResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, &OpError{Op: "agent deploy: decode fleet csr response", Cause: err}
	}
	return &out, nil
}

// FleetCABundle implements service.FleetCABundleSource: the CA bundle the
// deploy installs as server-ca.crt, read without issuing anything so
// preflight can verify the control plane against it.
func (i *sdkFleetNodeCertIssuer) FleetCABundle(ctx context.Context, _ string) ([]byte, error) {
	return i.fetchCABundle(ctx)
}

var _ service.FleetCABundleSource = (*sdkFleetNodeCertIssuer)(nil)

// fetchCABundle retrieves the CA bundle from /api/v1/xkms/ca/bundle for
// FleetCABundle.
func (i *sdkFleetNodeCertIssuer) fetchCABundle(ctx context.Context) ([]byte, error) {
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
