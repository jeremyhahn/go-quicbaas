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

//go:build integration

package cluster

import (
	"crypto/tls"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// requireTLSConfig loads the integration TLS config for tests that use
// gRPC with TLS. If the CA certificate is not available (e.g. when the
// cluster stack runs plain HTTP without mounted certs), the test is
// skipped rather than failed.
func requireTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	cfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		t.Skipf("skipping TLS-gRPC test: integration CA unavailable (%v); "+
			"cluster may be running plain HTTP", err)
	}
	return cfg
}
