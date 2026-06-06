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

//go:build integration

package helpers

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// IntegrationCAPath is the in-container path to the integration cluster's CA
// certificate. The docker-compose mounts ./certs:/etc/quicbaas/certs:ro on
// every container, so this path is stable for any test process running inside
// the integration suite.
const IntegrationCAPath = "/etc/quicbaas/certs/ca.crt"

var (
	integrationCAOnce sync.Once
	integrationCAPool *x509.CertPool
	integrationCAErr  error
)

// IntegrationCertPool loads the integration cluster's CA chain once and
// returns a *x509.CertPool that trusts it. Subsequent calls reuse the cached
// pool. Returns an error if the CA file is missing or unparseable; callers
// must treat that as a fatal test-environment misconfiguration (QuicBaaS
// bans InsecureSkipVerify so falling back to no-trust is not an option).
func IntegrationCertPool() (*x509.CertPool, error) {
	integrationCAOnce.Do(func() {
		pem, err := os.ReadFile(IntegrationCAPath)
		if err != nil {
			integrationCAErr = fmt.Errorf("read integration CA %s: %w", IntegrationCAPath, err)
			return
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			integrationCAErr = fmt.Errorf("integration CA %s: no PEM certificates", IntegrationCAPath)
			return
		}
		integrationCAPool = pool
	})
	return integrationCAPool, integrationCAErr
}

// IntegrationTLSConfig returns a *tls.Config that trusts the integration
// cluster's CA. Use this anywhere an HTTP / gRPC / QUIC client needs to dial
// a node in the integration cluster. Never InsecureSkipVerify.
func IntegrationTLSConfig() (*tls.Config, error) {
	pool, err := IntegrationCertPool()
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS13,
	}, nil
}

// NewHTTPClient returns an HTTP client configured for integration tests with
// explicit CA trust. The CA is loaded from IntegrationCAPath; if it cannot
// be loaded the returned client returns errors on every request.
func NewHTTPClient(timeout time.Duration) *http.Client {
	tlsCfg, err := IntegrationTLSConfig()
	if err != nil {
		// Return a client that always fails — better than silently
		// falling back to insecure mode (which would violate the
		// project-wide no-insecure-TLS rule).
		return &http.Client{
			Timeout:   timeout,
			Transport: errorRoundTripper{err: fmt.Errorf("integration HTTP client: %w", err)},
		}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:     tlsCfg,
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

// errorRoundTripper returns the same error on every request. Used to surface
// CA-loading failures at request time rather than at client construction.
type errorRoundTripper struct{ err error }

func (e errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, e.err
}
