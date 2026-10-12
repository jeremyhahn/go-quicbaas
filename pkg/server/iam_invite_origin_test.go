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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestInviteOriginPrefersConfiguredOrigin proves gRPC invite links use the
// operator's first WebAuthn origin, the one the activation page is served on.
func TestInviteOriginPrefersConfiguredOrigin(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = "node1.internal.test"
	cfg.IAM.WebAuthn.RPOrigins = []string{"https://id.acme.test", "https://alt.acme.test"}
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	assert.Equal(t, "https://id.acme.test", s.inviteOrigin())
}

// TestInviteOriginDerivesFromIssuer proves an unconfigured node builds links
// under its issuer origin.
func TestInviteOriginDerivesFromIssuer(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.TLSEnabled = true
	cfg.Server.Host = "id.acme.test"
	cfg.Server.REST.Port = 9443
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	assert.Equal(t, "https://id.acme.test:9443", s.inviteOrigin())
}

// TestInviteOriginEmptyForIPLiteralIssuer proves a node addressed by IP gets
// no origin rather than a link the browser will not honour.
func TestInviteOriginEmptyForIPLiteralIssuer(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = "10.0.0.5"
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	assert.Empty(t, s.inviteOrigin())
}
