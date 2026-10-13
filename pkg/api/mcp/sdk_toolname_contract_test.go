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
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdkmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// sdkClientRoot is the directory holding every SDK service client. Every call
// those clients make to transport.Request(ctx, service, method, req) becomes an
// MCP tool invocation when the caller selects the MCP transport.
const sdkClientRoot = "../../../sdk/go/client"

// sdkCall is one (service, method) pair the SDK can send over a transport.
type sdkCall struct {
	Service string
	Method  string
}

func (c sdkCall) String() string { return c.Service + "." + c.Method }

// Gap classifications for sdkCallsWithoutMCPTool. Every entry in the ledger
// carries one of these so the reason a verb is unreachable over MCP is never
// implicit.
const (
	// gapNoTool: the server registers nothing for this verb. Reaching it over
	// MCP requires registering a tool, not renaming anything.
	gapNoTool = "no MCP tool is registered for this verb"

	// gapNameDrift: the server DOES implement the verb, under a name the SDK
	// mapper cannot produce. This is exactly the DEK defect
	// (quicbaas_dek_create_dek vs quicbaas_dek_create) and every entry is a
	// live product gap. Repairing one is a map entry in
	// sdkmcp.ToolName PLUS a check that the tool's argument schema matches the
	// SDK request type - a rename alone can silently drop a field (the SDK's
	// GenerateDataKey sends key_size, quicbaas_crypto_datakey takes algorithm),
	// which turns a loud "unknown tool" into a quiet wrong answer.
	gapNameDrift = "name drift, server registers: "

	// gapAmbiguous: a registered tool plausibly implements the verb, but the
	// pairing crosses a service boundary or is not a clean 1:1, so wiring it is
	// a product decision rather than a mechanical fix.
	gapAmbiguous = "candidate tool needs a product decision: "

	// gapRefusedOverMCP: the SDK's MCP transport refuses the verb before any
	// I/O, by design rather than as debt. Secrets.GetData backs
	// GetWithBytes (go-quicbaas#954): MCP tool results are text strings, so a
	// secret fetched over MCP would already be an unwipeable Go string, and
	// registering a tool would defeat the protected read it exists for.
	gapRefusedOverMCP = "refused by the SDK MCP transport: MCP results are text strings, so protected-memory reads fail closed"
)

// sdkCallsWithoutMCPTool is the ledger of SDK verbs that cannot currently be
// reached over MCP. It exists so the contract test can pass on a known baseline
// while still failing on anything NEW, and so the backlog is reviewable in one
// place instead of surfacing as "unknown tool" in production.
//
// The locked architecture requires every verb on every transport, so entries
// here are debt, not design. Removing one means either registering the tool or
// fixing sdkmcp.ToolName.
var sdkCallsWithoutMCPTool = map[sdkCall]string{
	// --- Auth ------------------------------------------------------------
	{"quicbaas.Auth", "GetJWKS"}:                 gapNoTool,
	{"quicbaas.Auth", "GetOpenIDConfiguration"}:  gapNoTool,
	{"quicbaas.Auth", "IntrospectToken"}:         gapNoTool,
	{"quicbaas.Auth", "Login"}:                   gapNoTool,
	{"quicbaas.Auth", "RefreshToken"}:            gapNoTool,
	{"quicbaas.Auth", "ValidateToken"}:           gapNoTool,
	{"quicbaas.Auth", "BeginFederatedLogin"}:     gapAmbiguous + "quicbaas_iam_federation_beginlogin",
	{"quicbaas.Auth", "HandleFederatedCallback"}: gapAmbiguous + "quicbaas_iam_federation_handlecallback",
	{"quicbaas.Auth", "ListProviders"}:           gapAmbiguous + "quicbaas_iam_federation_listproviders",
	{"quicbaas.Auth", "GetProvider"}:             gapAmbiguous + "quicbaas_iam_federation_getprovider",
	{"quicbaas.Auth", "GetSession"}:              gapAmbiguous + "quicbaas_iam_session_get",
	{"quicbaas.Auth", "ListSessions"}:            gapAmbiguous + "quicbaas_iam_session_list",
	{"quicbaas.Auth", "RevokeSession"}:           gapAmbiguous + "quicbaas_iam_session_revoke",
	{"quicbaas.Auth", "RevokeAllSessions"}:       gapAmbiguous + "quicbaas_iam_session_revoke_all",

	// --- Batch / Query / Transaction --------------------------------------
	{"quicbaas.Batch", "Execute"}:       gapNoTool,
	{"quicbaas.Query", "Execute"}:       gapNameDrift + "quicbaas_query",
	{"quicbaas.Transaction", "Execute"}: gapNameDrift + "quicbaas_transaction",

	// --- Cache -------------------------------------------------------------
	{"quicbaas.Cache", "ClearCache"}:        gapNameDrift + "quicbaas_cache_clear",
	{"quicbaas.Cache", "GetCacheConfig"}:    gapNameDrift + "quicbaas_cache_config_get",
	{"quicbaas.Cache", "GetCacheStats"}:     gapNameDrift + "quicbaas_cache_stats",
	{"quicbaas.Cache", "UpdateCacheConfig"}: gapNameDrift + "quicbaas_cache_config_update",

	// --- Certificate -------------------------------------------------------
	{"quicbaas.Certificate", "CAInfo"}:           gapNoTool,
	{"quicbaas.Certificate", "IsInitialized"}:    gapNoTool,
	{"quicbaas.Certificate", "IsRevoked"}:        gapNoTool,
	{"quicbaas.Certificate", "ListCertificates"}: gapNoTool,
	{"quicbaas.Certificate", "GenerateCRL"}:      gapAmbiguous + "xkms.ca.generateCRL",
	{"quicbaas.Certificate", "GetCABundle"}:      gapAmbiguous + "quicbaas_certificate_chain",

	// --- Cluster -----------------------------------------------------------
	{"quicbaas.Cluster", "AddNode"}:    gapNameDrift + "quicbaas_add_node",
	{"quicbaas.Cluster", "RemoveNode"}: gapNameDrift + "quicbaas_remove_node",
	{"quicbaas.Cluster", "Nodes"}:      gapNoTool,

	// --- Crypto ------------------------------------------------------------
	{"quicbaas.Crypto", "GenerateDataKey"}:        gapNameDrift + "quicbaas_crypto_datakey",
	{"quicbaas.Crypto", "GenerateWrappedDataKey"}: gapNameDrift + "quicbaas_crypto_datakey_wrapped",
	{"quicbaas.Crypto", "GenerateHMAC"}:           gapNameDrift + "quicbaas_crypto_hmac",
	{"quicbaas.Crypto", "GenerateRandom"}:         gapNameDrift + "quicbaas_crypto_random",

	// --- Health ------------------------------------------------------------
	{"quicbaas.Health", "Ping"}: gapAmbiguous + "quicbaas_liveness",

	// --- IAM ---------------------------------------------------------------
	// Remote audit emission (P5.3 of the unified login-plane plan) shipped
	// on REST/gRPC only; no MCP tool was registered for it.
	{"quicbaas.IAM.Audit", "Record"}:      gapNoTool,
	{"quicbaas.IAM.Audit", "RecordAsync"}: gapNoTool,

	// End-user invite activation is served over REST, gRPC, Unix and the
	// embedded transport. These verbs are pre-auth, so MCP invite tools wait
	// on the owner's publicTools decision.
	{"quicbaas.IAM.Invite", "ActivateFIDO2Begin"}:  gapNoTool,
	{"quicbaas.IAM.Invite", "ActivateFIDO2Finish"}: gapNoTool,
	{"quicbaas.IAM.Invite", "ActivatePIV"}:         gapNoTool,
	{"quicbaas.IAM.Invite", "GetInvite"}:           gapNoTool,

	{"quicbaas.IAM.Policy", "GetEffectivePermissions"}: gapNoTool,
	{"quicbaas.IAM.Policy", "Validate"}:                gapAmbiguous + "quicbaas_iam_policy_test",

	{"quicbaas.IAM.User", "Approve"}:   gapNoTool,
	{"quicbaas.IAM.User", "Reject"}:    gapNoTool,
	{"quicbaas.IAM.User", "Suspend"}:   gapNoTool,
	{"quicbaas.IAM.User", "Unsuspend"}: gapNoTool,

	// --- KMS ---------------------------------------------------------------
	{"quicbaas.KMS", "ListKeyVersions"}: gapNameDrift + "quicbaas_kms_key_versions",
	{"quicbaas.KMS", "UpdateKeyStatus"}: gapNameDrift + "quicbaas_kms_key_status_update",

	// --- Messaging ---------------------------------------------------------
	{"quicbaas.Messaging", "WatchChannel"}:    gapNoTool,
	{"quicbaas.Messaging", "GetChannelStats"}: gapAmbiguous + "quicbaas_msg_stats",

	// --- Object storage ----------------------------------------------------
	// The whole admin surface is registered under quicbaas_object_admin_*; the
	// SDK collapses ObjectStorageAdmin to the quicbaas_object_ prefix.
	{"quicbaas.ObjectStorageAdmin", "CompactBackend"}:               gapNameDrift + "quicbaas_object_admin_compact_backend",
	{"quicbaas.ObjectStorageAdmin", "CreateBackend"}:                gapNameDrift + "quicbaas_object_admin_create_backend",
	{"quicbaas.ObjectStorageAdmin", "DeleteBackend"}:                gapNameDrift + "quicbaas_object_admin_delete_backend",
	{"quicbaas.ObjectStorageAdmin", "UpdateBackend"}:                gapNameDrift + "quicbaas_object_admin_update_backend",
	{"quicbaas.ObjectStorageAdmin", "SetBackendStatus"}:             gapNameDrift + "quicbaas_object_admin_set_backend_status",
	{"quicbaas.ObjectStorageAdmin", "GetStorageStats"}:              gapNameDrift + "quicbaas_object_admin_get_storage_stats",
	{"quicbaas.ObjectStorageAdmin", "RepairObject"}:                 gapNameDrift + "quicbaas_object_admin_repair_object",
	{"quicbaas.ObjectStorageAdmin", "RunGarbageCollection"}:         gapNameDrift + "quicbaas_object_admin_run_gc",
	{"quicbaas.ObjectStorageAdmin", "GetGarbageCollectionProgress"}: gapNameDrift + "quicbaas_object_admin_get_gc_progress",
	{"quicbaas.ObjectTransfer", "FetchObject"}:                      gapNameDrift + "quicbaas_object_transfer_fetch",

	// --- Ollama ------------------------------------------------------------
	{"quicbaas.Ollama", "ListModels"}:     gapNameDrift + "quicbaas_ollama_model_list",
	{"quicbaas.Ollama", "GetModelInfo"}:   gapNameDrift + "quicbaas_ollama_model_info",
	{"quicbaas.Ollama", "IsAvailable"}:    gapNameDrift + "quicbaas_ollama_health",
	{"quicbaas.Ollama", "EmbedWithModel"}: gapAmbiguous + "quicbaas_ollama_embed",
	// BaseURL/DefaultModel are local client accessors with no server verb.
	{"quicbaas.Ollama", "BaseURL"}:      gapNoTool,
	{"quicbaas.Ollama", "DefaultModel"}: gapNoTool,

	// --- Operations --------------------------------------------------------
	{"quicbaas.Operations", "MigrateData"}:          gapNameDrift + "quicbaas_migrate_data",
	{"quicbaas.Operations", "GetMigrationProgress"}: gapNameDrift + "quicbaas_migration_progress",
	{"quicbaas.Operations", "ReplaceNode"}:          gapNameDrift + "quicbaas_replace_node",
	{"quicbaas.Operations", "UpgradeNode"}:          gapNameDrift + "quicbaas_upgrade_node",
	{"quicbaas.Operations", "GetUpgradeStatus"}:     gapNoTool,

	// --- Orchestrator ------------------------------------------------------
	// Every fleet verb is registered under quicbaas_fleet_*; the SDK derives
	// quicbaas_orchestrator_* for all of them.
	{"quicbaas.Orchestrator", "RegisterFleetNode"}:   gapNameDrift + "quicbaas_fleet_register",
	{"quicbaas.Orchestrator", "RemoveFleetNode"}:     gapNameDrift + "quicbaas_fleet_remove",
	{"quicbaas.Orchestrator", "FleetNodeHeartbeat"}:  gapNameDrift + "quicbaas_fleet_heartbeat",
	{"quicbaas.Orchestrator", "DisableFleetNode"}:    gapNameDrift + "quicbaas_fleet_disable",
	{"quicbaas.Orchestrator", "EnableFleetNode"}:     gapNameDrift + "quicbaas_fleet_enable",
	{"quicbaas.Orchestrator", "CreateTask"}:          gapNameDrift + "quicbaas_orchestrator_task_create",
	{"quicbaas.Orchestrator", "DecomposeTask"}:       gapNameDrift + "quicbaas_orchestrator_decompose",
	{"quicbaas.Orchestrator", "ListBackendModels"}:   gapNameDrift + "quicbaas_orchestrator_list_backends",
	{"quicbaas.Orchestrator", "GetTaskArtifact"}:     gapNameDrift + "quicbaas_task_get_artifact",
	{"quicbaas.Orchestrator", "GetFleetNode"}:        gapAmbiguous + "quicbaas_fleet_status",
	{"quicbaas.Orchestrator", "ClaimTask"}:           gapNoTool,
	{"quicbaas.Orchestrator", "CompleteTask"}:        gapNoTool,
	{"quicbaas.Orchestrator", "GetTask"}:             gapNoTool,
	{"quicbaas.Orchestrator", "ListTasks"}:           gapNoTool,
	{"quicbaas.Orchestrator", "SubmitTaskArtifacts"}: gapNoTool,
	{"quicbaas.Orchestrator", "SpawnChildJob"}:       gapNoTool,
	{"quicbaas.Orchestrator", "CreateWorkflow"}:      gapNoTool,
	{"quicbaas.Orchestrator", "UpdateWorkflow"}:      gapNoTool,
	{"quicbaas.Orchestrator", "DeleteWorkflow"}:      gapNoTool,
	{"quicbaas.Orchestrator", "GetWorkflow"}:         gapNoTool,

	// --- Parameter store ---------------------------------------------------
	// Registered under quicbaas_parameter_*; the SDK derives
	// quicbaas_parameterstore_*, and the plural verbs map to batch_* tools.
	{"quicbaas.ParameterStore", "GetParameter"}:     gapNameDrift + "quicbaas_parameter_get",
	{"quicbaas.ParameterStore", "SetParameter"}:     gapNameDrift + "quicbaas_parameter_set",
	{"quicbaas.ParameterStore", "DeleteParameter"}:  gapNameDrift + "quicbaas_parameter_delete",
	{"quicbaas.ParameterStore", "ListParameters"}:   gapNameDrift + "quicbaas_parameter_list",
	{"quicbaas.ParameterStore", "SearchByTags"}:     gapNameDrift + "quicbaas_parameter_search",
	{"quicbaas.ParameterStore", "GetParameters"}:    gapNameDrift + "quicbaas_parameter_batch_get",
	{"quicbaas.ParameterStore", "SetParameters"}:    gapNameDrift + "quicbaas_parameter_batch_set",
	{"quicbaas.ParameterStore", "DeleteParameters"}: gapNameDrift + "quicbaas_parameter_batch_delete",

	// --- Seal --------------------------------------------------------------
	// The Seal case derives quicbaas_<lowercase method>, which is right for
	// Seal/Unseal and wrong for everything else.
	{"quicbaas.Seal", "Init"}:   gapNameDrift + "quicbaas_seal_init",
	{"quicbaas.Seal", "Status"}: gapNameDrift + "quicbaas_seal_status",

	// --- Secrets -----------------------------------------------------------
	{"quicbaas.Secrets", "ListVersions"}: gapNameDrift + "quicbaas_secrets_versions",
	{"quicbaas.Secrets", "GetVersion"}:   gapNoTool,
	{"quicbaas.Secrets", "GetData"}:      gapRefusedOverMCP,

	// --- Vector ------------------------------------------------------------
	{"quicbaas.Vector", "Compact"}:               gapNameDrift + "quicbaas_vector_admin_compact_namespace",
	{"quicbaas.Vector", "Export"}:                gapNameDrift + "quicbaas_vector_admin_export_vectors",
	{"quicbaas.Vector", "Import"}:                gapNameDrift + "quicbaas_vector_admin_import_vectors",
	{"quicbaas.Vector", "GetCompactionProgress"}: gapNameDrift + "quicbaas_vector_admin_get_compaction_progress",
	{"quicbaas.Vector", "HybridSearch"}:          gapNoTool,
	{"quicbaas.Vector", "SearchByMetadata"}:      gapNoTool,
}

// TestSDKToolNameContract is the build-time guard for the SDK -> MCP naming
// contract. sdk/go/transport/mcp derives a tool name from the gRPC-style
// service/method pair; the server registers tool names independently. When the
// two drift the verb is simply unreachable over MCP and the only symptom is an
// "unknown tool" error at runtime, on a transport that may have no test
// coverage (this is exactly how every DEK verb shipped broken: the SDK emitted
// quicbaas_dek_create_dek while the server registered quicbaas_dek_create).
//
// The test discovers the SDK's service/method pairs from source rather than
// from a hand-maintained table, so a new SDK verb is covered the moment it is
// written.
func TestSDKToolNameContract(t *testing.T) {
	registered := make(map[string]struct{})
	for _, name := range registeredToolNames(t) {
		registered[name] = struct{}{}
	}

	calls := discoverSDKCalls(t)
	require.NotEmpty(t, calls, "no SDK transport calls discovered - the AST scan is broken, not the SDK")

	var missing []string
	for _, call := range calls {
		toolName := sdkmcp.ToolName(call.Service, call.Method)
		if _, ok := registered[toolName]; ok {
			continue
		}
		if _, exempt := sdkCallsWithoutMCPTool[call]; exempt {
			continue
		}
		// Verbs whose tool exists only in some builds. The ledger above is
		// unconditional because it records product DEBT, which does not
		// depend on how the binary was compiled; this one records ABSENCE BY
		// CONFIGURATION, which does. See featureGatedSDKCalls.
		if _, gated := featureGatedSDKCalls[call]; gated {
			continue
		}
		missing = append(missing, fmt.Sprintf("%s -> %q", call, toolName))
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d SDK verb(s) map to MCP tools the server does not register.\n"+
			"Either fix sdkmcp.ToolName, register the tool, or add the pair to "+
			"sdkCallsWithoutMCPTool with a reason:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestSDKToolNameContract_NoStaleExemptions keeps the exemption list honest:
// an entry must name a real SDK call that really has no registered tool.
func TestSDKToolNameContract_NoStaleExemptions(t *testing.T) {
	registered := make(map[string]struct{})
	for _, name := range registeredToolNames(t) {
		registered[name] = struct{}{}
	}

	known := make(map[sdkCall]struct{})
	for _, call := range discoverSDKCalls(t) {
		known[call] = struct{}{}
	}

	// The feature-gated map is held to the SAME standard, and by the same
	// loop: an exemption that outlives its reason is how a ledger stops being
	// evidence and starts being noise. If a no-rag build ever registers the
	// RAG tools, or the SDK drops the verb, this fails instead of quietly
	// excusing something that no longer needs excusing.
	ledgers := []map[sdkCall]string{sdkCallsWithoutMCPTool, featureGatedSDKCalls}
	for _, ledger := range ledgers {
		for call, reason := range ledger {
			if reason == "" {
				t.Errorf("exemption %s has no reason", call)
			}
			if _, ok := known[call]; !ok {
				t.Errorf("stale exemption %s: no such SDK transport call - remove it", call)
				continue
			}
			if _, ok := registered[sdkmcp.ToolName(call.Service, call.Method)]; ok {
				t.Errorf("unnecessary exemption %s: tool %q is registered - remove it",
					call, sdkmcp.ToolName(call.Service, call.Method))
			}
		}
	}
}

// TestSDKToolNameContract_DEKSurface pins the DEK surface explicitly. The
// generic contract test above would catch a regression here too, but DEK is the
// surface that actually shipped broken, and its method names embed the resource
// noun ("CreateDEK"), so the generic quicbaas_<service>_<method> derivation
// doubles it. Pinning the exact names documents the intent.
func TestSDKToolNameContract_DEKSurface(t *testing.T) {
	want := map[string]string{
		"CreateDEK":               "quicbaas_dek_create",
		"GetDEK":                  "quicbaas_dek_get",
		"ListDEKs":                "quicbaas_dek_list",
		"RotateDEK":               "quicbaas_dek_rotate",
		"DeleteDEK":               "quicbaas_dek_delete",
		"GetDEKStatus":            "quicbaas_dek_status",
		"SetDefaultDEK":           "quicbaas_dek_set_default",
		"SetDEKSafety":            "quicbaas_dek_set_safety",
		"ClearDEKSafety":          "quicbaas_dek_clear_safety",
		"StartReencryption":       "quicbaas_dek_start_reencryption",
		"GetReencryptionProgress": "quicbaas_dek_get_reencryption_progress",
	}

	registered := make(map[string]struct{})
	for _, name := range registeredToolNames(t) {
		registered[name] = struct{}{}
	}

	for method, toolName := range want {
		got := sdkmcp.ToolName("quicbaas.DEK", method)
		if got != toolName {
			t.Errorf("ToolName(quicbaas.DEK, %s) = %q, want %q", method, got, toolName)
		}
		if _, ok := registered[toolName]; !ok {
			t.Errorf("tool %q is not registered by the MCP server", toolName)
		}
	}
}

// discoverSDKCalls parses every SDK service client and returns the distinct
// (service, method) pairs handed to transport.Request. RequestStream is
// excluded: the MCP transport reports Unimplemented for streaming by design.
func discoverSDKCalls(t *testing.T) []sdkCall {
	t.Helper()

	entries, err := os.ReadDir(sdkClientRoot)
	require.NoError(t, err, "read SDK client root")

	seen := make(map[sdkCall]struct{})
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, call := range scanSDKClientPackage(t, filepath.Join(sdkClientRoot, entry.Name())) {
			seen[call] = struct{}{}
		}
	}

	calls := make([]sdkCall, 0, len(seen))
	for call := range seen {
		calls = append(calls, call)
	}
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].Service != calls[j].Service {
			return calls[i].Service < calls[j].Service
		}
		return calls[i].Method < calls[j].Method
	})
	return calls
}

// scanSDKClientPackage extracts the transport.Request calls from one SDK client
// package.
func scanSDKClientPackage(t *testing.T, dir string) []sdkCall {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	require.NoError(t, err, "parse %s", dir)

	var calls []sdkCall
	for _, pkg := range pkgs {
		files := make([]*ast.File, 0, len(pkg.Files))
		for _, file := range pkg.Files {
			files = append(files, file)
		}
		scan := &sdkPackageScan{consts: stringConstants(files), files: files}
		calls = append(calls, scan.requestCalls()...)
	}
	return calls
}

// sdkPackageScan resolves the arguments of transport.Request within a single
// package.
type sdkPackageScan struct {
	consts map[string]string
	files  []*ast.File
}

// requestCalls returns every (service, method) pair the package sends.
func (s *sdkPackageScan) requestCalls() []sdkCall {
	var calls []sdkCall
	for _, file := range s.files {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}
			ast.Inspect(fn.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok || !isTransportRequest(call) {
					return true
				}
				for _, service := range s.resolveArg(fn, call.Args[1]) {
					for _, method := range s.resolveArg(fn, call.Args[2]) {
						calls = append(calls, sdkCall{Service: service, Method: method})
					}
				}
				return true
			})
			return true
		})
	}
	return calls
}

// resolveArg resolves an argument to its possible string values: a literal, a
// package-level string constant, or - when the value arrives as a parameter of
// a package-local helper - every literal its callers pass at that position.
func (s *sdkPackageScan) resolveArg(fn *ast.FuncDecl, arg ast.Expr) []string {
	if value, ok := s.staticString(arg); ok {
		return []string{value}
	}
	ident, ok := arg.(*ast.Ident)
	if !ok {
		return nil
	}
	index, ok := paramIndex(fn, ident.Name)
	if !ok {
		return nil
	}
	return s.callerLiterals(fn.Name.Name, index)
}

// staticString resolves a string literal or a package-level string constant.
func (s *sdkPackageScan) staticString(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(v.Value)
		return value, err == nil
	case *ast.Ident:
		value, ok := s.consts[v.Name]
		return value, ok
	}
	return "", false
}

// callerLiterals collects the literals passed at argIndex by every call to
// funcName in the package.
func (s *sdkPackageScan) callerLiterals(funcName string, argIndex int) []string {
	var values []string
	for _, file := range s.files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || calleeName(call) != funcName || argIndex >= len(call.Args) {
				return true
			}
			if value, ok := s.staticString(call.Args[argIndex]); ok {
				values = append(values, value)
			}
			return true
		})
	}
	return values
}

// isTransportRequest reports whether the call is x.Request(ctx, service,
// method, req).
func isTransportRequest(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Request" && len(call.Args) >= 3
}

// calleeName returns the called function's identifier, ignoring the receiver.
func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// paramIndex returns the flat argument position of a named parameter.
func paramIndex(fn *ast.FuncDecl, name string) (int, bool) {
	index := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			index++
			continue
		}
		for _, ident := range field.Names {
			if ident.Name == name {
				return index, true
			}
			index++
		}
	}
	return 0, false
}

// stringConstants collects package-level string constants.
func stringConstants(files []*ast.File) map[string]string {
	consts := make(map[string]string)
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range value.Names {
					if i >= len(value.Values) {
						continue
					}
					lit, ok := value.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					unquoted, err := strconv.Unquote(lit.Value)
					if err == nil {
						consts[name.Name] = unquoted
					}
				}
			}
		}
	}
	return consts
}
