// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package quicbaas

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/spiffe"
)

// TestWithSPIFFETrustDomain_SetsTheEmbeddedNodesTrustDomain: the SPIFFE
// trust domain of an embedded node is settable through the SDK, and the
// node reports the domain it was given.
func TestWithSPIFFETrustDomain_SetsTheEmbeddedNodesTrustDomain(t *testing.T) {
	cfg := EmbeddedServerConfig()
	srv, err := NewServer(cfg, WithSPIFFETrustDomain("edge.example.test"))
	if err != nil {
		t.Fatalf("NewServer with WithSPIFFETrustDomain: %v", err)
	}
	if got := srv.SPIFFETrustDomain(); got != "edge.example.test" {
		t.Fatalf("SPIFFETrustDomain() = %q, want edge.example.test", got)
	}
	if cfg.IAM.SPIFFETrustDomain != "" {
		t.Fatalf("the option must not write through to the caller's config, got %q", cfg.IAM.SPIFFETrustDomain)
	}
}

// TestWithSPIFFETrustDomain_RefusesAnInvalidDomain: an invalid trust domain
// fails construction with go-xkms's SPIFFE validation error.
func TestWithSPIFFETrustDomain_RefusesAnInvalidDomain(t *testing.T) {
	for _, td := range []string{"not a domain/", "Edge.Example.Test"} {
		_, err := NewServer(EmbeddedServerConfig(), WithSPIFFETrustDomain(td))
		if err == nil {
			t.Fatalf("NewServer accepted the invalid SPIFFE trust domain %q", td)
		}
		if !errors.Is(err, spiffe.ErrTrustDomainCharacters) {
			t.Fatalf("NewServer(%q) error = %v, want spiffe.ErrTrustDomainCharacters", td, err)
		}
	}
}
