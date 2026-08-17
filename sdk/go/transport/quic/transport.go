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

// Package quic provides an HTTP/3 over QUIC transport for the QuicBaaS SDK.
//
// This transport speaks the exact same REST dialect as sdk/go/transport/rest
// (identical endpoint mapping, JSON request/response encoding, and error
// decoding): the only difference is the underlying network protocol. It is
// implemented by constructing an *http.Client whose RoundTripper is
// quic-go's http3.Transport, and handing that client to the REST transport
// via rest.WithHTTPClient. This means the QUIC transport shares 100% of its
// request-building and response-decoding logic with the REST transport
// rather than duplicating it — there is no separate wire protocol, opcode
// set, or handler tree to maintain here.
package quic

import (
	"crypto/tls"
	"net/http"

	"github.com/quic-go/quic-go/http3"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// alpnHTTP3 is the ALPN protocol identifier for HTTP/3 (RFC 9114 section 3.1).
const alpnHTTP3 = "h3"

// options holds QUIC-transport-specific construction options. It is
// deliberately narrow: every other setting (timeout, TLS, retries, auth
// token) already travels on transport.Config and is honored identically to
// the REST transport, since New ultimately delegates to rest.New.
type options struct {
	authToken string
}

// Option configures the QUIC transport.
type Option func(*options)

// WithAuthToken sets a Bearer token attached to every request, overriding
// transport.Config.AuthToken when both are set.
func WithAuthToken(token string) Option {
	return func(o *options) {
		o.authToken = token
	}
}

// Transport wraps *rest.Transport so that Close also tears down the
// underlying HTTP/3 QUIC connections. rest.Transport.Close only knows how to
// release an *http.Transport's idle connections; it does not recognize
// http3.Transport, so without this wrapper every QUIC connection this
// transport opened would leak past Close.
type Transport struct {
	*rest.Transport
	h3 *http3.Transport
}

// Close closes the HTTP/3 QUIC connections this transport opened, then
// delegates to the embedded REST transport's Close.
func (t *Transport) Close() error {
	h3Err := t.h3.Close()
	restErr := t.Transport.Close()
	if h3Err != nil {
		return h3Err
	}
	return restErr
}

// New creates a new HTTP/3-over-QUIC transport. The returned *Transport
// dispatches Request/RequestStream calls through the same code path as
// sdk/go/transport/rest.Transport (via the embedded *rest.Transport); only
// the underlying http.Client differs. *Transport satisfies transport.Transport,
// so callers that only need the interface can assign the result directly.
func New(cfg *transport.Config, opts ...Option) (*Transport, error) {
	if cfg == nil {
		return nil, errors.NewInvalidInput("quic.New", "config is required")
	}
	if cfg.Address == "" {
		return nil, errors.NewInvalidInput("quic.New", "address is required")
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	tlsConfig := buildTLSConfig(cfg)

	h3Transport := &http3.Transport{
		TLSClientConfig: tlsConfig,
	}

	httpClient := &http.Client{
		Transport: h3Transport,
	}
	if cfg.Timeout > 0 {
		httpClient.Timeout = cfg.Timeout
	}

	restOpts := []rest.Option{rest.WithHTTPClient(httpClient)}

	authToken := o.authToken
	if authToken == "" {
		authToken = cfg.AuthToken
	}
	if authToken != "" {
		restOpts = append(restOpts, rest.WithAuthToken(authToken))
	}

	restTransport, err := rest.New(cfg, restOpts...)
	if err != nil {
		return nil, err
	}

	return &Transport{Transport: restTransport, h3: h3Transport}, nil
}

// buildTLSConfig constructs the client TLS configuration for the HTTP/3 over
// QUIC transport. A caller-supplied cfg.TLSConfig is cloned and takes
// precedence, so mTLS client certificates (including hardware-backed
// crypto.Signer certificates surfaced through Certificates or
// GetClientCertificate) are preserved.
//
// QUIC mandates TLS 1.3 (RFC 9001 section 4.2): older versions cannot be
// negotiated over QUIC at all. A caller-supplied MinVersion below TLS 1.3 is
// therefore raised, never lowered.
func buildTLSConfig(cfg *transport.Config) *tls.Config {
	if cfg.TLSConfig != nil {
		tlsConfig := cfg.TLSConfig.Clone()
		if tlsConfig.MinVersion < tls.VersionTLS13 {
			tlsConfig.MinVersion = tls.VersionTLS13
		}
		if len(tlsConfig.NextProtos) == 0 {
			tlsConfig.NextProtos = []string{alpnHTTP3}
		}
		return tlsConfig
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{alpnHTTP3},
	}
}

// Factory implements transport.Factory for QUIC.
type Factory struct{}

// Create creates a new QUIC transport.
func (f *Factory) Create(cfg *transport.Config) (transport.Transport, error) {
	return New(cfg)
}
