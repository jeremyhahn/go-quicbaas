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
	"context"
	"errors"
	"os"
	"testing"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// fakeAgentCertIssuer is a test double for service.AgentCertIssuer. It records
// the request it received and replays a scripted certificate (with a real
// mlocked key buffer so the resolver's Bytes()/Destroy() lifecycle is
// exercised) or a scripted error.
type fakeAgentCertIssuer struct {
	gotReq  *service.AgentCertificateRequest
	certPEM []byte
	caPEM   []byte
	keyPEM  []byte
	err     error
}

func (f *fakeAgentCertIssuer) IssueAgentCertificate(_ context.Context, req *service.AgentCertificateRequest) (*service.IssuedAgentCertificate, error) {
	f.gotReq = req
	if f.err != nil {
		return nil, f.err
	}
	// NewBufferFromBytes wipes its source slice, so copy first.
	src := make([]byte, len(f.keyPEM))
	copy(src, f.keyPEM)
	buf, err := secure.NewBufferFromBytes(src)
	if err != nil {
		return nil, err
	}
	return &service.IssuedAgentCertificate{
		CertPEM:    append([]byte(nil), f.certPEM...),
		PrivateKey: buf,
		CAPEM:      append([]byte(nil), f.caPEM...),
	}, nil
}

func newTestIssuer() *fakeAgentCertIssuer {
	return &fakeAgentCertIssuer{
		certPEM: []byte("-----BEGIN CERTIFICATE-----\nLEAF\n-----END CERTIFICATE-----\n"),
		caPEM:   []byte("-----BEGIN CERTIFICATE-----\nCA\n-----END CERTIFICATE-----\n"),
		keyPEM:  []byte("-----BEGIN PRIVATE KEY-----\nKEY\n-----END PRIVATE KEY-----\n"),
	}
}

// TestAgentMCPPresetResolver_Resolve verifies the happy path: a fresh cert is
// minted with the expected subject, three files are written with correct modes
// (key strictly 0600), the env points at them, and cleanup removes the dir.
func TestAgentMCPPresetResolver_Resolve(t *testing.T) {
	issuer := newTestIssuer()
	r, err := NewAgentMCPPresetResolver(issuer, "https://127.0.0.1:11443/mcp", "host-1", "system", makeNoopLogger())
	if err != nil {
		t.Fatalf("NewAgentMCPPresetResolver: %v", err)
	}

	url, env, cleanup, err := r.Resolve(context.Background(), "ses-xyz", "tenant-7")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cleanup == nil {
		t.Fatal("Resolve returned nil cleanup")
	}

	if url != "https://127.0.0.1:11443/mcp" {
		t.Errorf("url = %q, want the configured endpoint", url)
	}

	// Subject must carry tenant in O= and the agent OU sentinel so the MCP
	// authorizer recognizes it.
	if issuer.gotReq.Subject.Organization != "tenant-7" {
		t.Errorf("cert O= = %q, want tenant-7", issuer.gotReq.Subject.Organization)
	}
	if issuer.gotReq.Subject.OrganizationalUnit != service.DefaultAgentCertOU {
		t.Errorf("cert OU = %q, want %q", issuer.gotReq.Subject.OrganizationalUnit, service.DefaultAgentCertOU)
	}
	if issuer.gotReq.Subject.CommonName != "agent-ses-xyz" {
		t.Errorf("cert CN = %q, want agent-ses-xyz", issuer.gotReq.Subject.CommonName)
	}
	if issuer.gotReq.ValidityDays != ephemeralAgentCertValidityDays {
		t.Errorf("validity = %d, want %d", issuer.gotReq.ValidityDays, ephemeralAgentCertValidityDays)
	}

	certPath := env[mcpCertEnvVar]
	keyPath := env[mcpKeyEnvVar]
	caPath := env[mcpCAEnvVar]
	if certPath == "" || keyPath == "" || caPath == "" {
		t.Fatalf("env missing cert/key/ca paths: %+v", env)
	}

	assertFileMode(t, certPath, 0644)
	assertFileMode(t, keyPath, 0600)
	assertFileMode(t, caPath, 0644)

	// File contents must match the issued material.
	if got, _ := os.ReadFile(keyPath); string(got) != string(issuer.keyPEM) {
		t.Errorf("key file contents mismatch: %q", string(got))
	}
	if got, _ := os.ReadFile(certPath); string(got) != string(issuer.certPEM) {
		t.Errorf("cert file contents mismatch: %q", string(got))
	}

	// Cleanup removes the directory.
	dir := dirOf(keyPath)
	cleanup()
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("cleanup did not remove dir %s (stat err: %v)", dir, statErr)
	}
}

// TestAgentMCPPresetResolver_DefaultTenant uses the configured default tenant
// when the spawn supplies no tenant.
func TestAgentMCPPresetResolver_DefaultTenant(t *testing.T) {
	issuer := newTestIssuer()
	r, err := NewAgentMCPPresetResolver(issuer, "https://h:1/mcp", "host-1", "system", makeNoopLogger())
	if err != nil {
		t.Fatalf("NewAgentMCPPresetResolver: %v", err)
	}
	_, _, cleanup, err := r.Resolve(context.Background(), "ses-1", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer cleanup()
	if issuer.gotReq.Subject.Organization != "system" {
		t.Errorf("O= = %q, want default tenant 'system'", issuer.gotReq.Subject.Organization)
	}
}

// TestAgentMCPPresetResolver_IssueError surfaces an MCPPresetResolveError when
// the issuer fails, and writes no files.
func TestAgentMCPPresetResolver_IssueError(t *testing.T) {
	issuer := newTestIssuer()
	issuer.err = errors.New("ca sealed")
	r, _ := NewAgentMCPPresetResolver(issuer, "https://h:1/mcp", "host-1", "system", makeNoopLogger())

	url, env, cleanup, err := r.Resolve(context.Background(), "ses-1", "tenant-1")
	if err == nil {
		t.Fatal("Resolve: expected error, got nil")
	}
	var resolveErr *MCPPresetResolveError
	if !errors.As(err, &resolveErr) {
		t.Fatalf("error type = %T, want *MCPPresetResolveError", err)
	}
	if !errors.Is(err, issuer.err) {
		t.Error("MCPPresetResolveError does not unwrap to the issuer cause")
	}
	if url != "" || env != nil {
		t.Errorf("on error want zero values, got url=%q env=%v", url, env)
	}
	if cleanup != nil {
		t.Error("on error want nil cleanup, got non-nil")
	}
}

// TestAgentMCPPresetResolver_NoTenant denies issuance when neither the spawn
// nor the resolver default supplies a tenant.
func TestAgentMCPPresetResolver_NoTenant(t *testing.T) {
	issuer := newTestIssuer()
	r, _ := NewAgentMCPPresetResolver(issuer, "https://h:1/mcp", "host-1", "", makeNoopLogger())

	_, _, _, err := r.Resolve(context.Background(), "ses-1", "")
	if !errors.Is(err, ErrMCPPresetResolverNoTenant) {
		t.Fatalf("error = %v, want ErrMCPPresetResolverNoTenant", err)
	}
}

// TestNewAgentMCPPresetResolver_Validation covers the constructor guards.
func TestNewAgentMCPPresetResolver_Validation(t *testing.T) {
	if _, err := NewAgentMCPPresetResolver(nil, "https://h:1/mcp", "host", "system", makeNoopLogger()); !errors.Is(err, ErrMCPPresetResolverNilIssuer) {
		t.Errorf("nil issuer: err = %v, want ErrMCPPresetResolverNilIssuer", err)
	}
	if _, err := NewAgentMCPPresetResolver(newTestIssuer(), "", "host", "system", makeNoopLogger()); !errors.Is(err, ErrMCPPresetResolverNoEndpoint) {
		t.Errorf("empty endpoint: err = %v, want ErrMCPPresetResolverNoEndpoint", err)
	}
	if _, err := NewAgentMCPPresetResolver(newTestIssuer(), "https://h:1/mcp", "", "system", makeNoopLogger()); !errors.Is(err, ErrMCPPresetResolverNoHostname) {
		t.Errorf("empty hostname: err = %v, want ErrMCPPresetResolverNoHostname", err)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != want {
		t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
