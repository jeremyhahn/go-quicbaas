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

package service

import (
	"encoding/pem"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// TestAgentCertificateRequest_Validate_Branches exercises every branch of
// AgentCertificateRequest.Validate, including the nil-receiver and
// missing-hostname paths that the happy-path tests do not reach.
func TestAgentCertificateRequest_Validate_Branches(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var req *AgentCertificateRequest
		if err := req.Validate(); !errors.Is(err, ErrAgentCertRequestNil) {
			t.Fatalf("expected ErrAgentCertRequestNil, got %v", err)
		}
	})

	t.Run("missing tenant id", func(t *testing.T) {
		req := &AgentCertificateRequest{Hostname: "h"}
		var fieldErr *AgentCertRequestFieldError
		if err := req.Validate(); !errors.As(err, &fieldErr) || fieldErr.Field != "tenant_id" {
			t.Fatalf("expected tenant_id field error, got %v", err)
		}
	})

	t.Run("missing hostname", func(t *testing.T) {
		req := &AgentCertificateRequest{TenantID: "t", Hostname: "   "}
		var fieldErr *AgentCertRequestFieldError
		if err := req.Validate(); !errors.As(err, &fieldErr) || fieldErr.Field != "hostname" {
			t.Fatalf("expected hostname field error, got %v", err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		req := &AgentCertificateRequest{TenantID: "t", Hostname: "h"}
		if err := req.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

// TestParseLeafCertPEM_Branches covers the empty-PEM and wrong-type branches
// of parseLeafCertPEM in addition to the happy path.
func TestParseLeafCertPEM_Branches(t *testing.T) {
	t.Run("no pem block", func(t *testing.T) {
		if _, err := parseLeafCertPEM([]byte("not pem")); err == nil {
			t.Fatal("expected error for non-PEM input")
		}
	})

	t.Run("wrong pem type", func(t *testing.T) {
		blk := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("x")})
		if _, err := parseLeafCertPEM(blk); err == nil {
			t.Fatal("expected error for wrong PEM type")
		}
	})

	t.Run("valid certificate", func(t *testing.T) {
		// A real CA-issued leaf rather than a hand-rolled self-signed one: the
		// function under test parses what the CA actually emits.
		certPEM := issueLeafPEM(t, xkmsca.NewCAService(t, "QuicBaaS Parse Leaf CA"))
		cert, err := parseLeafCertPEM(certPEM)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cert == nil {
			t.Fatal("expected parsed certificate")
		}
	})
}

// TestUnexpectedPEMTypeError_Message covers the typed error's message, which is
// what an operator reads when a CA returns the wrong PEM block.
func TestUnexpectedPEMTypeError_Message(t *testing.T) {
	t.Run("names the offending type", func(t *testing.T) {
		err := &UnexpectedPEMTypeError{PEMType: "EC PRIVATE KEY"}
		if got := err.Error(); got != `unexpected PEM type "EC PRIVATE KEY"` {
			t.Fatalf("Error() = %q", got)
		}
	})

	t.Run("survives an empty type", func(t *testing.T) {
		err := &UnexpectedPEMTypeError{}
		if got := err.Error(); got != `unexpected PEM type ""` {
			t.Fatalf("Error() = %q", got)
		}
	})
}
