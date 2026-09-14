//go:build quic && rest

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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"

	"github.com/quic-go/quic-go/http3"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// alpnHTTP3 is the ALPN protocol identifier for HTTP/3 (RFC 9114 section 3.1).
const alpnHTTP3 = "h3"

// QUICSecretsClient implements the SecretsClient interface over HTTP/3. It
// delegates every method to pkg/api/rest.RESTClient — the same REST-dialect
// client the "rest" CLI transport uses — so request/response encoding, error
// decoding, and endpoint mapping are shared verbatim. Only the http.Client's
// RoundTripper differs: HTTP/3 over QUIC instead of HTTP/1.1 or HTTP/2.
type QUICSecretsClient struct {
	client *rest.RESTClient
}

// StoreSecret stores a new secret.
func (c *QUICSecretsClient) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return c.client.StoreSecret(ctx, req)
}

// GetSecret retrieves a secret by ID.
func (c *QUICSecretsClient) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return c.client.GetSecret(ctx, req)
}

// ListSecrets returns all accessible secrets.
func (c *QUICSecretsClient) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return c.client.ListSecrets(ctx, req)
}

// DeleteSecret deletes a secret.
func (c *QUICSecretsClient) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return c.client.DeleteSecret(ctx, req)
}

// ListSecretVersions returns all versions of a secret.
func (c *QUICSecretsClient) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return c.client.ListSecretVersions(ctx, req)
}

// RollbackSecret rolls back to a previous version.
func (c *QUICSecretsClient) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return c.client.RollbackSecret(ctx, req)
}

// RotateSecret rotates a secret to a new value.
func (c *QUICSecretsClient) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return c.client.RotateSecret(ctx, req)
}

// UpdateSecretStatus updates the status of a secret (enable/disable).
func (c *QUICSecretsClient) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return c.client.UpdateSecretStatus(ctx, req)
}

// GetSecretsByPrefix retrieves all secrets matching a path prefix.
func (c *QUICSecretsClient) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	return c.client.GetSecretsByPrefix(ctx, req)
}

// WatchSecretsByPrefix watches for secret changes under a prefix. The
// underlying RESTClient serves this over Server-Sent Events, which HTTP/3
// streams identically to HTTP/1.1.
func (c *QUICSecretsClient) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	return c.client.WatchSecretsByPrefix(ctx, req)
}

// Close closes the underlying HTTP/3 client connection.
func (c *QUICSecretsClient) Close() error {
	return c.client.Close()
}

// newQUICRESTClient builds a pkg/api/rest.RESTClient whose HTTP transport
// rides HTTP/3 over QUIC (quic-go/http3) instead of HTTP/1.1 or HTTP/2. This
// is the CLI-layer mirror of sdk/go/transport/quic: rather than maintaining a
// separate opcode wire protocol, every QUIC-transport CLI client shares this
// one REST-dialect request/response encoder, changing only the RoundTripper.
func newQUICRESTClient(component string) (*rest.RESTClient, error) {
	level := logLevel
	if level == "" {
		level = "info"
	}
	log, err := logger.New(logger.LogConfig{
		Level:     level,
		Format:    "json",
		Output:    "stderr",
		Component: component,
	})
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrConfiguration, "newQUICRESTClient",
			"failed to create logger")
	}

	// QUIC always uses TLS — QuicBaaS bans insecure TLS so the operator must
	// supply a CA via --tls-ca (or set --tls-cert/--tls-key for mTLS).
	tlsCfg, err := buildTLSConfig()
	if err != nil {
		return nil, err
	}
	if tlsCfg == nil {
		return nil, errors.Newf(errors.ErrConfiguration, "newQUICRESTClient",
			"TLS configuration is required for QUIC connections (use --tls-ca)")
	}
	tlsCfg.MinVersion = tls.VersionTLS13
	if len(tlsCfg.NextProtos) == 0 {
		tlsCfg.NextProtos = []string{alpnHTTP3}
	}

	httpClient := &http.Client{
		Transport: &http3.Transport{TLSClientConfig: tlsCfg},
		Timeout:   globalTimeout,
	}

	restClient, err := rest.NewRESTClient(quicBaseURL(address), log, rest.WithHTTPClient(httpClient))
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrUnavailable, "newQUICRESTClient",
			"failed to create QUIC client for server %s", address)
	}

	return restClient, nil
}

// quicBaseURL builds an https:// base URL from a bare host:port address.
// QUIC mandates TLS at the protocol level, so the scheme is always https.
func quicBaseURL(addr string) string {
	if strings.HasPrefix(addr, "https://") {
		return addr
	}
	if strings.HasPrefix(addr, "http://") {
		return "https://" + strings.TrimPrefix(addr, "http://")
	}
	return fmt.Sprintf("https://%s", addr)
}

// createQUICSecretsClient creates a QUIC (HTTP/3) client for secrets operations.
func createQUICSecretsClient() (SecretsClient, error) {
	client, err := newQUICRESTClient("quic-secrets-client")
	if err != nil {
		return nil, err
	}
	return &QUICSecretsClient{client: client}, nil
}

// Compile-time interface check.
var _ SecretsClient = (*QUICSecretsClient)(nil)
