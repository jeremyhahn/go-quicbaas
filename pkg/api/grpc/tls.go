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

package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	"google.golang.org/grpc/credentials"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// TLSConfig holds TLS configuration options.
type TLSConfig struct {
	// Enabled indicates whether TLS is enabled
	Enabled bool

	// CertFile is the path to the server certificate
	CertFile string

	// KeyFile is the path to the server private key
	KeyFile string

	// CAFile is the path to the CA certificate for client verification
	CAFile string

	// ClientAuth indicates whether client certificate authentication is required
	ClientAuth bool
}

// LoadServerTLSCredentials loads TLS credentials for the gRPC server.
func LoadServerTLSCredentials(config *TLSConfig) (credentials.TransportCredentials, error) {
	if config == nil || !config.Enabled {
		return nil, nil
	}

	if config.CertFile == "" || config.KeyFile == "" {
		return nil, api.NewConfigurationError("grpc.LoadServerTLSCredentials", "validate TLS config: cert and key files are required when TLS is enabled", nil)
	}

	// Load server certificate and key
	serverCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, api.NewConfigurationError("grpc.LoadServerTLSCredentials", "load server certificate", err)
	}

	// Create TLS config
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS13, // TLS 1.3 minimum
	}

	// Configure client authentication if required
	if config.ClientAuth {
		if config.CAFile == "" {
			return nil, api.NewConfigurationError("grpc.LoadServerTLSCredentials", "validate client auth config: CA file is required when client authentication is enabled", nil)
		}

		// Load CA certificate
		caCert, err := os.ReadFile(config.CAFile)
		if err != nil {
			return nil, api.NewConfigurationError("grpc.LoadServerTLSCredentials", "read CA certificate", err)
		}

		// Create CA certificate pool
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, api.NewConfigurationError("grpc.LoadServerTLSCredentials", "add CA certificate to pool", nil)
		}

		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return credentials.NewTLS(tlsConfig), nil
}

// ServerTLSCredentialsFromConfig creates gRPC transport credentials from an
// existing *tls.Config. This allows the server to use in-memory TLS
// configurations backed by OpaqueKeys instead of loading from files.
func ServerTLSCredentialsFromConfig(tlsConfig *tls.Config) credentials.TransportCredentials {
	return credentials.NewTLS(tlsConfig)
}

// LoadClientTLSCredentials loads TLS credentials for the gRPC client.
func LoadClientTLSCredentials(config *TLSConfig) (credentials.TransportCredentials, error) {
	if config == nil || !config.Enabled {
		return nil, nil
	}

	// Create TLS config
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	// Load CA certificate if provided
	if config.CAFile != "" {
		caCert, err := os.ReadFile(config.CAFile)
		if err != nil {
			return nil, api.NewConfigurationError("grpc.LoadClientTLSCredentials", "read CA certificate", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, api.NewConfigurationError("grpc.LoadClientTLSCredentials", "add CA certificate to pool", nil)
		}

		tlsConfig.RootCAs = certPool
	}

	// Load client certificate for mutual TLS if provided
	if config.CertFile != "" && config.KeyFile != "" {
		clientCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			return nil, api.NewConfigurationError("grpc.LoadClientTLSCredentials", "load client certificate", err)
		}

		tlsConfig.Certificates = []tls.Certificate{clientCert}
	}

	return credentials.NewTLS(tlsConfig), nil
}
