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

// mtls_posture_test.go covers optionalMTLSClientAuth: the Security.RequireMTLS
// flag (SC-7) flips the opportunistic-mTLS path from VerifyClientCertIfGiven to
// RequireAndVerifyClientCert, while the default false preserves current behavior.

package server

import (
	"crypto/tls"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/stretchr/testify/assert"
)

// TestOptionalMTLSClientAuthDefault verifies the default posture is the
// historical opportunistic mTLS (VerifyClientCertIfGiven).
func TestOptionalMTLSClientAuthDefault(t *testing.T) {
	s := &Server{config: &config.Config{}}
	assert.Equal(t, tls.VerifyClientCertIfGiven, s.optionalMTLSClientAuth())
}

// TestOptionalMTLSClientAuthRequire verifies RequireMTLS=true flips the mode to
// the strict RequireAndVerifyClientCert posture.
func TestOptionalMTLSClientAuthRequire(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.RequireMTLS = true
	s := &Server{config: cfg}
	assert.Equal(t, tls.RequireAndVerifyClientCert, s.optionalMTLSClientAuth())
}
