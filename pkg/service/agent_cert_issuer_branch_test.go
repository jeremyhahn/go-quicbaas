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
	"context"
	"encoding/pem"
	"errors"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
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
		certPEM, _, _ := newTestCertPEMs(t)
		cert, err := parseLeafCertPEM(certPEM)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cert == nil {
			t.Fatal("expected parsed certificate")
		}
	})
}

// TestSDKAgentCertIssuer_NilResponse exercises the resp == nil branch where
// the CA returns neither an error nor a response.
func TestSDKAgentCertIssuer_NilResponse(t *testing.T) {
	issuer, _ := NewSDKAgentCertIssuer(&stubCAService{issueResp: nil})
	_, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
		TenantID: "t", Hostname: "h",
	})
	var issErr *AgentCertIssueError
	if !errors.As(err, &issErr) {
		t.Fatalf("expected AgentCertIssueError, got %v", err)
	}
}

// TestSDKAgentCertIssuer_EmptyPrivateKey exercises the branch that rejects a
// response carrying a certificate but no exportable private key.
func TestSDKAgentCertIssuer_EmptyPrivateKey(t *testing.T) {
	certPEM, _, _ := newTestCertPEMs(t)
	stub := &stubCAService{
		issueResp: &xkmstransport.IssueCertificateResponse{
			CertificatePEM: certPEM,
		},
	}
	issuer, _ := NewSDKAgentCertIssuer(stub)
	_, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
		TenantID: "t", Hostname: "h",
	})
	if err == nil {
		t.Fatal("expected error for missing private key")
	}
}

// TestSDKAgentCertIssuer_EmptyBundleAndChain forces both the GetCABundle
// failure and an empty ChainPEM fallback so the final "ca bundle is empty"
// branch is exercised (with the secure buffer destroyed on that path).
func TestSDKAgentCertIssuer_EmptyBundleAndChain(t *testing.T) {
	certPEM, keyPEM, _ := newTestCertPEMs(t)
	stub := &stubCAService{
		issueResp: &xkmstransport.IssueCertificateResponse{
			CertificatePEM: certPEM,
			PrivateKeyPEM:  keyPEM,
			// ChainPEM intentionally empty.
		},
		bundleErr: errors.New("no bundle"),
	}
	issuer, _ := NewSDKAgentCertIssuer(stub)
	_, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
		TenantID: "t", Hostname: "h",
	})
	var issErr *AgentCertIssueError
	if !errors.As(err, &issErr) || issErr.Op != "resolve ca bundle" {
		t.Fatalf("expected resolve ca bundle error, got %v", err)
	}
}

// TestSDKAgentCertIssuer_ParseError forces parseLeafCertPEM to fail on the
// issued certificate so the parse-error branch (and buffer destroy) runs.
func TestSDKAgentCertIssuer_ParseError(t *testing.T) {
	_, keyPEM, bundlePEM := newTestCertPEMs(t)
	// Hand back a non-certificate PEM as the "certificate" so parsing fails
	// after the key has already been secured.
	badCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not-a-cert")})
	stub := &stubCAService{
		issueResp: &xkmstransport.IssueCertificateResponse{
			CertificatePEM: badCert,
			PrivateKeyPEM:  keyPEM,
			ChainPEM:       bundlePEM,
		},
		bundle: bundlePEM,
	}
	issuer, _ := NewSDKAgentCertIssuer(stub)
	_, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
		TenantID: "t", Hostname: "h",
	})
	var issErr *AgentCertIssueError
	if !errors.As(err, &issErr) || issErr.Op != "parse issued certificate" {
		t.Fatalf("expected parse issued certificate error, got %v", err)
	}
}

// TestSDKAgentCertIssuer_ExtraDNSNames covers the SAN-building loop including
// the skip cases (empty / duplicate-of-hostname) and a real extra name.
func TestSDKAgentCertIssuer_ExtraDNSNames(t *testing.T) {
	certPEM, keyPEM, bundlePEM := newTestCertPEMs(t)
	stub := &stubCAService{
		issueResp: &xkmstransport.IssueCertificateResponse{
			CertificatePEM: certPEM,
			PrivateKeyPEM:  keyPEM,
			ChainPEM:       bundlePEM,
		},
		bundle: bundlePEM,
	}
	issuer, _ := NewSDKAgentCertIssuer(stub)
	result, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
		TenantID:      "t",
		Hostname:      "h",
		ExtraDNSNames: []string{"alias.lan", "", "h"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer result.PrivateKey.Destroy()
	if len(result.CertPEM) == 0 {
		t.Fatal("expected cert pem")
	}
}
