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
	"errors"
	"regexp"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
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
// composed alongside this module, see ModuleTools's doc comment), except
// moduleServedXKMSTools, which the module lists because go-xkms's module
// serves no FIDO2 ceremony tool: every
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
		if _, served := moduleServedXKMSTools[def.Name]; !served && strings.HasPrefix(def.Name, "xkms.") {
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

// TestModuleTools_EntityKVToolsOnExistingKVPermissions proves a full node
// lists quicbaas's entity KV tools under their quicbaas_kv_* names (the
// qrdb_* names belong to go-qrdb's own key/value tools there), each on the
// kv:* permission its qrdb_* twin already carries: no new permission, no
// role widened. The entity list returns values, so it is on kv:range, the
// value-scan permission, not on the keys-only kv:list.
func TestModuleTools_EntityKVToolsOnExistingKVPermissions(t *testing.T) {
	specs := make(map[string]ToolSpec)
	for _, spec := range ModuleTools() {
		specs[spec.Name] = spec
	}
	for _, verb := range []string{"put", "get", "delete", "list"} {
		name := "quicbaas_kv_" + verb
		spec, ok := specs[name]
		if !ok {
			t.Errorf("%s is not listed on the node", name)
			continue
		}
		if spec.Public {
			t.Errorf("%s must not be public", name)
		}
		wantAction := verb
		if verb == "list" {
			// The entity list answers values: it needs the value-scan
			// permission, never go-qrdb's keys-only kv:list.
			wantAction = "range"
		}
		if spec.Resource != "kv" || spec.Action != wantAction {
			t.Errorf("%s permission = %s:%s, want kv:%s", name, spec.Resource, spec.Action, wantAction)
		}
		if verb == "list" {
			if _, listed := specs["qrdb_"+verb]; listed {
				t.Errorf("qrdb_%s must stay off the node (go-qrdb owns it)", verb)
			}
			continue
		}
		twinResource, twinAction, mapped := getToolQRDBPermission("qrdb_" + verb)
		if !mapped || twinResource != spec.Resource || twinAction != spec.Action {
			t.Errorf("%s permission %s:%s is not its qrdb_%s twin's %s:%s",
				name, spec.Resource, spec.Action, verb, twinResource, twinAction)
		}
		if _, listed := specs["qrdb_"+verb]; listed {
			t.Errorf("qrdb_%s must stay off the node (go-qrdb owns it)", verb)
		}
	}
}

// TestInvokeTool_EntityKVPutReachesEntityHandler proves quicbaas_kv_put runs
// quicbaas's entity put handler: a call with undecodable data fails in the entity
// handler's own base64 decoding, not with an unknown-tool error.
func TestInvokeTool_EntityKVPutReachesEntityHandler(t *testing.T) {
	srv := newModuleSupportMCPServer(t)
	_, err := srv.InvokeTool(context.Background(), "quicbaas_kv_put", map[string]interface{}{
		"entityType": "users", "id": float64(1), "data": "not base64!",
	})
	var invalid *UserToolInvalidArgumentError
	if !errors.As(err, &invalid) || invalid.Arg != "data" {
		t.Fatalf("expected the entity handler's *UserToolInvalidArgumentError on data, got %T: %v", err, err)
	}
}

// TestModuleTools_FIDO2AddCredentialOnItsRESTGrant proves the module lists
// exactly the passkey add-credential pair of the "xkms." family, under its
// sanitized names and on user_authfactors:add, the grant its REST and gRPC
// twins take, and leaves the other FIDO2 ceremony tools off.
func TestModuleTools_FIDO2AddCredentialOnItsRESTGrant(t *testing.T) {
	byName := map[string]ToolSpec{}
	for _, spec := range ModuleTools() {
		byName[spec.Name] = spec
	}

	want := map[string]string{
		"xkms.fido2.beginAddCredential": "xkms_fido2_beginAddCredential",
		"xkms.fido2.finishRegistration": "xkms_fido2_finishRegistration",
	}
	for name, tool := range want {
		spec, ok := byName[name]
		if !ok {
			t.Errorf("%s is not listed", name)
			continue
		}
		if spec.Tool != tool {
			t.Errorf("%s registers as %q, want %q", name, spec.Tool, tool)
		}
		if spec.Public {
			t.Errorf("%s must not be public", name)
		}
		if spec.Resource != "user_authfactors" || spec.Action != "add" {
			t.Errorf("%s permission %s:%s, want user_authfactors:add", name, spec.Resource, spec.Action)
		}
	}
	for name := range byName {
		if _, ok := want[name]; !ok && strings.HasPrefix(name, "xkms.") {
			t.Errorf("xkms tool %q is listed; only the add-credential pair may be", name)
		}
	}
	for _, name := range []string{
		"xkms.fido2.beginRegistration",
		"xkms.fido2.beginAuthentication",
		"xkms.fido2.finishAuthentication",
	} {
		if _, ok := byName[name]; ok {
			t.Errorf("%s must stay off the module", name)
		}
	}
}

// TestInvokeTool_FIDO2AddCredentialActsInCallerTenant proves a module call
// of the add-credential pair runs in the caller's bound tenant, and that a
// call naming another tenant, or from a caller bound to no tenant, is refused
// before the service is reached.
func TestInvokeTool_FIDO2AddCredentialActsInCallerTenant(t *testing.T) {
	caller := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "admin", TenantID: "tenant-a"})
	setup := func(t *testing.T) (*Server, *fido2FakeService) {
		t.Helper()
		fake := &fido2FakeService{}
		srv := newModuleSupportMCPServer(t)
		srv.tools = newFIDO2Registry(t, fake)
		// The xkms dispatch table is package-global and bound to the
		// registry that first dispatches; rebind it to this one.
		xkmsHandlerMap = nil
		t.Cleanup(func() { xkmsHandlerMap = nil })
		return srv, fake
	}
	finishArgs := func(extra map[string]interface{}) map[string]interface{} {
		args := map[string]interface{}{"session_id": "sess-1", "attestation_json": "{}"}
		for k, v := range extra {
			args[k] = v
		}
		return args
	}

	t.Run("begin_add_credential", func(t *testing.T) {
		srv, fake := setup(t)
		if _, err := srv.InvokeTool(caller, "xkms.fido2.beginAddCredential", nil); err != nil {
			t.Fatalf("beginAddCredential: %v", err)
		}
		if fake.lastTenant != "tenant-a" {
			t.Errorf("tenant %q, want tenant-a", fake.lastTenant)
		}
	})

	t.Run("finish_unnamed", func(t *testing.T) {
		srv, fake := setup(t)
		if _, err := srv.InvokeTool(caller, "xkms.fido2.finishRegistration", finishArgs(nil)); err != nil {
			t.Fatalf("finishRegistration: %v", err)
		}
		if fake.lastTenant != "tenant-a" {
			t.Errorf("tenant %q, want tenant-a", fake.lastTenant)
		}
		if fake.lastFinishReg == nil || fake.lastFinishReg.SessionID != "sess-1" {
			t.Errorf("finish request %+v, want session sess-1", fake.lastFinishReg)
		}
	})

	t.Run("finish_own_tenant", func(t *testing.T) {
		srv, fake := setup(t)
		args := finishArgs(map[string]interface{}{"tenant_id": "tenant-a"})
		if _, err := srv.InvokeTool(caller, "xkms.fido2.finishRegistration", args); err != nil {
			t.Fatalf("finishRegistration: %v", err)
		}
		if fake.lastTenant != "tenant-a" {
			t.Errorf("tenant %q, want tenant-a", fake.lastTenant)
		}
	})

	t.Run("finish_other_tenant", func(t *testing.T) {
		srv, fake := setup(t)
		args := finishArgs(map[string]interface{}{"tenant_id": "tenant-b"})
		_, err := srv.InvokeTool(caller, "xkms.fido2.finishRegistration", args)
		var tenantErr *ModuleToolTenantError
		if !errors.As(err, &tenantErr) || tenantErr.Named != "tenant-b" {
			t.Fatalf("expected *ModuleToolTenantError naming tenant-b, got %T: %v", err, err)
		}
		if fake.lastFinishReg != nil {
			t.Errorf("the service was reached: %+v", fake.lastFinishReg)
		}
	})

	t.Run("unbound_caller", func(t *testing.T) {
		srv, fake := setup(t)
		_, err := srv.InvokeTool(context.Background(), "xkms.fido2.beginAddCredential", nil)
		var tenantErr *ModuleToolTenantError
		if !errors.As(err, &tenantErr) || tenantErr.Named != "" {
			t.Fatalf("expected *ModuleToolTenantError naming no tenant, got %T: %v", err, err)
		}
		if fake.lastTenant != "" {
			t.Errorf("the service was reached in tenant %q", fake.lastTenant)
		}
	})
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
