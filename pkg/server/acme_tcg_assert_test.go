//go:build !integration && rest

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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// buildACMEService rejects a CA that cannot satisfy the TCG-extended surface.
// The plain fakeTrustedCA implements only the base TrustedCA interface, so the
// CACertificateAuthority (TCGCA) assertion fails and the builder returns a typed
// ACMECAAssertError. This exercises the TCG-assert branch without standing up a
// real TCG CA (which is integration territory).
func TestBuildACMEService_NonTCGCARejected(t *testing.T) {
	node := buildMemoryStandaloneNode(t)
	t.Cleanup(func() { _ = node.Close() })

	cfg := &config.Config{}
	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = 8443
	cfg.Server.Host = "localhost"

	s := &Server{
		logger:     buildTestLogger(t),
		config:     cfg,
		qrdbClient: newNodeBackedShardedClient(node),
		xkmsCA:     newFakeTrustedCA(t), // base TrustedCA only; not a TCGCA
	}

	adapter, err := newCAServiceAdapter(newFakeTrustedCA(t))
	if err != nil {
		t.Fatalf("newCAServiceAdapter: %v", err)
	}

	svc, err := s.buildACMEService(adapter)
	if svc != nil {
		t.Fatal("expected nil ACME servicer when the CA is not TCG-capable")
	}
	var assertErr *ACMECAAssertError
	if !errors.As(err, &assertErr) {
		t.Fatalf("want *ACMECAAssertError, got %T (%v)", err, err)
	}
}
