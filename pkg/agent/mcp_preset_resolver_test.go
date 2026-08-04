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

package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestFileMCPPresetResolver_Resolve verifies the resolver returns the
// configured endpoint URL and an env map pointing at the on-disk cert/key/ca
// paths, with no cleanup (the long-lived material must never be deleted).
func TestFileMCPPresetResolver_Resolve(t *testing.T) {
	const (
		endpoint = "https://cp.example.com:11443/mcp"
		certFile = "/var/lib/quicbaas-agent/agent.crt"
		keyFile  = "/var/lib/quicbaas-agent/agent.key"
		caFile   = "/var/lib/quicbaas-agent/server-ca.crt"
	)

	r, err := NewFileMCPPresetResolver(endpoint, certFile, keyFile, caFile)
	if err != nil {
		t.Fatalf("NewFileMCPPresetResolver: unexpected error: %v", err)
	}

	url, env, cleanup, err := r.Resolve(context.Background(), "session-123", "acme")
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if url != endpoint {
		t.Errorf("url = %q, want %q", url, endpoint)
	}
	if cleanup != nil {
		t.Error("cleanup must be nil: on-disk cert material is long-lived and shared")
	}
	if got := env[orchestration.MCPCertEnvVar]; got != certFile {
		t.Errorf("%s = %q, want %q", orchestration.MCPCertEnvVar, got, certFile)
	}
	if got := env[orchestration.MCPKeyEnvVar]; got != keyFile {
		t.Errorf("%s = %q, want %q", orchestration.MCPKeyEnvVar, got, keyFile)
	}
	if got := env[orchestration.MCPCAEnvVar]; got != caFile {
		t.Errorf("%s = %q, want %q", orchestration.MCPCAEnvVar, got, caFile)
	}
	if len(env) != 3 {
		t.Errorf("env has %d entries, want exactly 3", len(env))
	}
}

// TestNewFileMCPPresetResolver_Validation verifies that each required field is
// rejected with its typed error, so the daemon never advertises an unusable
// preset.
func TestNewFileMCPPresetResolver_Validation(t *testing.T) {
	tests := []struct {
		name                       string
		endpoint, cert, key, caCrt string
		wantErr                    error
	}{
		{"empty endpoint", "", "c", "k", "ca", ErrFileMCPResolverNoEndpoint},
		{"blank endpoint", "   ", "c", "k", "ca", ErrFileMCPResolverNoEndpoint},
		{"empty cert", "https://h/mcp", "", "k", "ca", ErrFileMCPResolverNoCert},
		{"empty key", "https://h/mcp", "c", "", "ca", ErrFileMCPResolverNoKey},
		{"empty ca", "https://h/mcp", "c", "k", "", ErrFileMCPResolverNoCA},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewFileMCPPresetResolver(tc.endpoint, tc.cert, tc.key, tc.caCrt)
			if r != nil {
				t.Errorf("resolver = %v, want nil on error", r)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestFileMCPPresetResolver_ImplementsInterface is a compile-and-runtime check
// that the resolver satisfies the orchestration seam.
func TestFileMCPPresetResolver_ImplementsInterface(t *testing.T) {
	var _ orchestration.MCPPresetResolver = (*FileMCPPresetResolver)(nil)
	r, err := NewFileMCPPresetResolver("https://h:1/mcp", "c", "k", "ca")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var iface orchestration.MCPPresetResolver = r
	if iface == nil {
		t.Fatal("resolver does not satisfy MCPPresetResolver")
	}
}
