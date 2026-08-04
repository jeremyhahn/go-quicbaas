// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
)

// grpcCAOnlyTLSOption returns a grpc.ClientOption that configures server-only
// TLS verification using the given CA certificate file. grpc.WithTLS requires
// both certFile and keyFile, so it cannot be used for the CA-only case;
// callers must build the TLS config manually and pass it via grpc.WithTLSConfig.
// QuicBaaS bans insecure TLS — a valid CA is required.
func grpcCAOnlyTLSOption(caFile string) (grpc.ClientOption, error) {
	if caFile == "" {
		return nil, &MessageError{Message: "grpcCAOnlyTLSOption: CA file path is required"}
	}
	caPEM, err := os.ReadFile(caFile) //nolint:gosec // caFile comes from operator config
	if err != nil {
		return nil, &OpError{Op: fmt.Sprintf("read CA cert %s", caFile), Cause: err}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, &MessageError{Message: fmt.Sprintf("no PEM certs in CA bundle %s", caFile)}
	}
	return grpc.WithTLSConfig(&tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS13,
	}), nil
}
