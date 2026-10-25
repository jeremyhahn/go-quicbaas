//go:build mcp

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

package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"regexp"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

var mcpToolNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func newModuleSupportMCPServer(t *testing.T) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(testutil.NewMockQuicBaaS(), DefaultConfig(), log)
}

// TestModuleTools_SurfaceGuard proves the enumeration mirrors the served
// registry exactly, minus qrdbNativeDuplicateTools and the "xkms." family
// (P6.1/item-C: both are deliberately excluded from the module surface --
// go-qrdb and go-xkms each register the identical capability natively when
// composed alongside this module, see ModuleTools's doc comment): every
// enumerated tool is still in the registry, every sanitized name is in the
// MCP charset, sanitized names are unique, and every spec is classified
// consistently with the canonical qrdb permission tables.
func TestModuleTools_SurfaceGuard(t *testing.T) {
	srv := newModuleSupportMCPServer(t)
	registry := srv.tools.ListTools()
	specs := ModuleTools()

	excluded := 0
	for _, def := range registry {
		if _, ok := qrdbNativeDuplicateTools[def.Name]; ok {
			excluded++
			continue
		}
		if strings.HasPrefix(def.Name, "xkms.") {
			excluded++
		}
	}
	if want := len(registry) - excluded; len(specs) != want {
		t.Errorf("enumeration size %d != registry size %d minus %d excluded (%d)", len(specs), len(registry), excluded, want)
	}

	registered := make(map[string]ToolDefinition, len(registry))
	for _, def := range registry {
		registered[def.Name] = def
	}

	seenTool := map[string]string{}
	for _, spec := range specs {
		def, ok := registered[spec.Name]
		if !ok {
			t.Errorf("enumerated tool %q not in the served registry", spec.Name)
			continue
		}
		if !mcpToolNameRE.MatchString(spec.Tool) {
			t.Errorf("tool name %q sanitized to invalid MCP name %q", spec.Name, spec.Tool)
		}
		if prev, dup := seenTool[spec.Tool]; dup {
			t.Errorf("sanitized tool name collision: %q and %q both map to %q", prev, spec.Name, spec.Tool)
		}
		seenTool[spec.Tool] = spec.Name

		if spec.Description != def.Description {
			t.Errorf("tool %q description drifted from the registry", spec.Name)
		}

		// Classification agreement with the canonical tables.
		if spec.Public != IsPublicTool(spec.Name) {
			t.Errorf("tool %q public flag disagrees with IsPublicTool", spec.Name)
		}
		resource, action, mapped := getToolQRDBPermission(spec.Name)
		switch {
		case spec.Public:
			if spec.Resource != "" || spec.Action != "" {
				t.Errorf("public tool %q must carry no permission", spec.Name)
			}
		case mapped:
			if spec.Resource != resource || spec.Action != action {
				t.Errorf("tool %q permission %s:%s disagrees with table %s:%s",
					spec.Name, spec.Resource, spec.Action, resource, action)
			}
		default:
			if spec.Resource != "" || spec.Action != "" {
				t.Errorf("unmapped tool %q must carry an empty permission (fail-closed)", spec.Name)
			}
		}
	}
}

// TestModuleTools_ClassCounts pins the mapped / public / unmapped partition
// sizes so a tool silently falling out of the qrdb permission tables (and
// therefore off the unified server, fail-closed) is an explicit diff.
func TestModuleTools_ClassCounts(t *testing.T) {
	var public, mapped, unmapped int
	for _, spec := range ModuleTools() {
		switch {
		case spec.Public:
			public++
		case spec.Resource != "":
			mapped++
		default:
			unmapped++
		}
	}
	total := public + mapped + unmapped
	if total == 0 {
		t.Fatal("no tools enumerated")
	}
	t.Logf("tools: total=%d mapped=%d public=%d unmapped=%d", total, mapped, public, unmapped)
	if public == 0 {
		t.Error("expected at least one public tool (health)")
	}
	if mapped == 0 {
		t.Error("expected mapped tools")
	}
}

// TestToolName covers the sanitization rules.
func TestToolName(t *testing.T) {
	cases := map[string]string{
		"quicbaas_kms_generate_key": "quicbaas_kms_generate_key",
		"xkms.generateKey":          "xkms_generateKey",
		"xkms.ca.signCSR":           "xkms_ca_signCSR",
		"with-dash":                 "with-dash",
		"weird name!":               "weird_name_",
	}
	for in, want := range cases {
		if got := ToolName(in); got != want {
			t.Errorf("ToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestInvokeTool_DispatchesRegistryHandler proves InvokeTool reaches the
// same per-tool handler the bespoke tools/call path executes: the barrier
// status tool, invoked with no barrier service wired, answers with the
// dispatcher's own typed unavailable error — evidence the real handler ran.
func TestInvokeTool_DispatchesRegistryHandler(t *testing.T) {
	srv := newModuleSupportMCPServer(t)
	_, err := srv.InvokeTool(context.Background(), "qrdb_barrier_status", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected the barrier dispatcher's typed unavailable error")
	}
	var unavailable *BarrierServiceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("expected *BarrierServiceUnavailableError, got %T: %v", err, err)
	}
}

// TestInvokeTool_UnknownTool proves an unknown tool name returns an error,
// never a silent success.
func TestInvokeTool_UnknownTool(t *testing.T) {
	srv := newModuleSupportMCPServer(t)
	if _, err := srv.InvokeTool(context.Background(), "no_such_tool_exists", nil); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// moduleSealedProbe is a constant sealed-window probe for these tests.
type moduleSealedProbe struct{ sealed bool }

func (p moduleSealedProbe) IsSealed() bool { return p.sealed }

// TestInvokeTool_SealedWindowDenied proves the sealed-window gate runs
// inside InvokeTool: with a sealed probe installed, a non-allowlisted tool
// is rejected with the typed SealedWindowError.
func TestInvokeTool_SealedWindowDenied(t *testing.T) {
	srv := newModuleSupportMCPServer(t)
	srv.SetSealedWindowGate(moduleSealedProbe{sealed: true}, nil, map[string]struct{}{})
	_, err := srv.InvokeTool(context.Background(), "quicbaas_kms_list_keys", nil)
	if err == nil {
		t.Fatal("expected sealed-window denial")
	}
	if _, ok := err.(*SealedWindowError); !ok {
		t.Fatalf("expected *SealedWindowError, got %T: %v", err, err)
	}
}

// TestUpstreamToolContext_ProjectsQRDBIdentity proves a qrdb identity in the
// context is projected into the quicbaas Subject + tenant contract before
// dispatch, and that an identity-less context passes through unchanged.
func TestUpstreamToolContext_ProjectsQRDBIdentity(t *testing.T) {
	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{
		Subject:  "operator@example",
		TenantID: "tenant-a",
		Roles:    []string{"admin"},
	})

	projected := upstreamToolContext(ctx)
	subject, ok := auth.SubjectFromContext(projected)
	if !ok {
		t.Fatal("Subject not injected")
	}
	if subject.ID != "operator@example" || subject.TenantID != "tenant-a" || !subject.Authenticated {
		t.Fatalf("subject projection wrong: %+v", subject)
	}
	if len(subject.Roles) != 1 || subject.Roles[0] != "admin" {
		t.Fatalf("roles projection wrong: %v", subject.Roles)
	}
	// An unmarked identity is qrdb's own (mTLS): its subject is a username.
	if subject.IDKind != interfaces.SubjectIDUsername {
		t.Fatalf("IDKind = %v, want SubjectIDUsername", subject.IDKind)
	}

	plain := upstreamToolContext(context.Background())
	if _, ok := auth.SubjectFromContext(plain); ok {
		t.Fatal("identity-less context must not gain a Subject")
	}
}

// peerCertLoginClient is a qrdb LoginClient double that records the verified
// peer certificate the login facade forwards a FinishLogin with.
type peerCertLoginClient struct {
	qrdbtransport.LoginClient
	gotCert *x509.Certificate
}

func (c *peerCertLoginClient) FinishLogin(ctx context.Context, _ qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	c.gotCert = service.VerifiedPeerCertFromContext(ctx)
	return &qrdbtransport.FinishLoginResponse{Bearer: "tok"}, nil
}

// TestInvokeTool_LoginFinishBridgesVerifiedPeerCert proves the MCP module path
// hands the TLS leaf qrdb verified to the login facade, so an mtls
// quicbaas_login_finish completes, and that a call without one is refused with
// the facade's typed error rather than trusting the client-supplied key.
func TestInvokeTool_LoginFinishBridgesVerifiedPeerCert(t *testing.T) {
	args := map[string]interface{}{"session_id": "s1", "type": "mtls"}

	t.Run("verified_peer_cert", func(t *testing.T) {
		client := &peerCertLoginClient{}
		srv := newModuleSupportMCPServer(t)
		srv.tools = newLoginRegistry(t, service.NewLoginService(client))
		leaf := &x509.Certificate{Raw: []byte("leaf"), RawSubjectPublicKeyInfo: []byte("spki")}
		ctx := qrdbsdk.WithTLSState(context.Background(), &tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{leaf}},
		})

		if _, err := srv.InvokeTool(ctx, "quicbaas_login_finish", args); err != nil {
			t.Fatalf("InvokeTool: %v", err)
		}
		if client.gotCert != leaf {
			t.Fatalf("login facade saw cert %v, want the verified leaf", client.gotCert)
		}
	})

	t.Run("no_peer_cert", func(t *testing.T) {
		client := &peerCertLoginClient{}
		srv := newModuleSupportMCPServer(t)
		srv.tools = newLoginRegistry(t, service.NewLoginService(client))
		ctx := qrdbsdk.WithTLSState(context.Background(), &tls.ConnectionState{})

		_, err := srv.InvokeTool(ctx, "quicbaas_login_finish", args)
		var required *service.MTLSPeerCertRequiredError
		if !errors.As(err, &required) {
			t.Fatalf("expected *service.MTLSPeerCertRequiredError, got %T: %v", err, err)
		}
		if client.gotCert != nil {
			t.Fatal("a call without a verified peer cert must not reach the node")
		}
	})
}
