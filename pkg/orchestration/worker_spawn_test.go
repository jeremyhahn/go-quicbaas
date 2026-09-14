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

package orchestration

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestWorkerSpawnArgs_StatesTheGrantForClaudeCodeOnly covers both directions of
// the backend gate: the CLI that takes the flag gets it, and one that does not
// is spawned exactly as it was — a --permission-mode handed to a program with
// no such flag does not degrade the worker, it kills the exec.
func TestWorkerSpawnArgs_StatesTheGrantForClaudeCodeOnly(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"claude", "claude-code"} {
		args := WorkerSpawnArgs(backend, false)
		if !slices.Equal(args, []string{"--permission-mode", "acceptEdits"}) {
			t.Errorf("WorkerSpawnArgs(%q, host) = %v, want the acceptEdits grant", backend, args)
		}
		if slices.Contains(args, WorkerFullControlFlag) {
			t.Errorf("WorkerSpawnArgs(%q, host) granted Bash on the daemon; the ExecutionGuard is the authority for that", backend)
		}
	}
	for _, backend := range []string{"codex", "ollama", ""} {
		for _, sandboxed := range []bool{false, true} {
			if args := WorkerSpawnArgs(backend, sandboxed); args != nil {
				t.Errorf("WorkerSpawnArgs(%q, %v) = %v, want nil for a CLI that takes no such flag",
					backend, sandboxed, args)
			}
		}
	}
}

// TestWorkerSpawnArgs_ASandboxedSpawnTakesFullControl is go-quicbaas#927's
// container half. A containerized worker handed acceptEdits can edit files and
// cannot run one command — no build, no test, no diff — and in a
// non-interactive session nobody is there to approve the command it asks for.
// The measured result was an attempt that spent 13m46s producing prose and made
// zero tool calls.
func TestWorkerSpawnArgs_ASandboxedSpawnTakesFullControl(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"claude", "claude-code"} {
		args := WorkerSpawnArgs(backend, true)
		if !slices.Equal(args, []string{WorkerFullControlFlag}) {
			t.Errorf("WorkerSpawnArgs(%q, sandboxed) = %v, want the full-control grant", backend, args)
		}
		// The two grants are alternatives, never both: the CLI takes one
		// permission statement and a second would be an argument conflict.
		if slices.Contains(args, "--permission-mode") {
			t.Errorf("WorkerSpawnArgs(%q, sandboxed) = %v, want no --permission-mode beside the grant", backend, args)
		}
	}
}

// TestWorkerSpawnEnv_NamesTheEnginesConfigDir pins the positive half of the
// environment contract, and that it is stated only where it means something.
func TestWorkerSpawnEnv_NamesTheEnginesConfigDir(t *testing.T) {
	t.Parallel()
	env := WorkerSpawnEnv("claude", "/var/lib/quicbaas/agents/claude-config")
	if len(env) != 1 || env["CLAUDE_CONFIG_DIR"] != "/var/lib/quicbaas/agents/claude-config" {
		t.Fatalf("WorkerSpawnEnv = %v, want CLAUDE_CONFIG_DIR set to the engine's directory", env)
	}
	if env := WorkerSpawnEnv("claude", ""); env != nil {
		t.Errorf("WorkerSpawnEnv with no directory = %v, want nil", env)
	}
	if env := WorkerSpawnEnv("codex", "/var/lib/quicbaas/agents/claude-config"); env != nil {
		t.Errorf("WorkerSpawnEnv for codex = %v, want nil; CLAUDE_CONFIG_DIR means nothing to it", env)
	}
}

// TestWorkerSpawnAccountAuto_PooledBackendsOnly pins the pooled set to the
// backends whose credential the engine supplies from its own pool.
//
// "codex" IS POOLED, and this test previously asserted the opposite. Its
// credential is a ChatGPT subscription carried as a FILE, so a codex spawn that
// does not resolve an account gets no materialized home directory and dies
// before its first turn — which is what stopped every independent review on the
// burn-in node until 2026-09-13. A reviewer is codex precisely because
// ReviewPolicy.RequireIndependentReviewer refuses the worker's own family, so
// the reviewing path is the one that needs this most.
func TestWorkerSpawnAccountAuto_PooledBackendsOnly(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"claude", "claude-code", "codex", "opencode"} {
		if !WorkerSpawnAccountAuto(backend) {
			t.Errorf("WorkerSpawnAccountAuto(%q) = false, want a pooled account", backend)
		}
	}
	// ollama is local inference with no provider account to pool, and the
	// empty backend names nothing at all.
	for _, backend := range []string{"ollama", ""} {
		if WorkerSpawnAccountAuto(backend) {
			t.Errorf("WorkerSpawnAccountAuto(%q) = true, want the backend's own authentication", backend)
		}
	}
}

// TestApplyWorkerSpawnEnvironment_CodexAsksForItsPooledAccount guards the
// defect above at the seam that actually decides it: the review runner spawns
// codex through ApplyWorkerSpawnEnvironment, and if AccountAuto is not set
// there, nothing downstream ever resolves a credential.
func TestApplyWorkerSpawnEnvironment_CodexAsksForItsPooledAccount(t *testing.T) {
	t.Parallel()
	req := &SpawnSessionRequest{Backend: "codex"}
	ApplyWorkerSpawnEnvironment(req, "")
	if !req.AccountAuto {
		t.Fatal("AccountAuto = false for a codex spawn; the reviewer would run with no credential")
	}
	// A spawn that already names an account keeps it: auto is the fallback,
	// never an override of an operator's explicit choice.
	pinned := &SpawnSessionRequest{Backend: "codex", AccountID: "acct-1"}
	ApplyWorkerSpawnEnvironment(pinned, "")
	if pinned.AccountAuto {
		t.Error("AccountAuto = true although the spawn named AccountID; auto must not override it")
	}
}

// TestApplyWorkerSpawnEnvironment_ComposesWithoutReplacing: the engine's
// grant, directory and credential are added beside what the agent spec
// already said, and an explicit permission grant is left alone.
func TestApplyWorkerSpawnEnvironment_ComposesWithoutReplacing(t *testing.T) {
	t.Parallel()
	req := &SpawnSessionRequest{Backend: "claude", ExtraArgs: []string{"--verbose"}, Env: map[string]string{"KEEP": "1"}}
	ApplyWorkerSpawnEnvironment(req, "/cfg")
	if !slices.Equal(req.ExtraArgs, []string{"--verbose", "--permission-mode", "acceptEdits"}) {
		t.Fatalf("ExtraArgs = %v", req.ExtraArgs)
	}
	if req.Env["KEEP"] != "1" || req.Env["CLAUDE_CONFIG_DIR"] != "/cfg" {
		t.Fatalf("Env = %v", req.Env)
	}
	if !req.AccountAuto {
		t.Fatal("a claude spawn into a prepared directory must run on a pooled account")
	}

	explicit := &SpawnSessionRequest{Backend: "claude", ExtraArgs: []string{"--permission-mode", "plan"}}
	ApplyWorkerSpawnEnvironment(explicit, "")
	if !slices.Equal(explicit.ExtraArgs, []string{"--permission-mode", "plan"}) {
		t.Fatalf("an explicit grant must be kept: %v", explicit.ExtraArgs)
	}
	if explicit.Env != nil {
		t.Fatalf("no directory means no env: %v", explicit.Env)
	}

	pinned := &SpawnSessionRequest{Backend: "opencode", AccountID: "acct-1"}
	ApplyWorkerSpawnEnvironment(pinned, "")
	if pinned.AccountAuto {
		t.Fatal("a pinned account must not be overridden with auto selection")
	}
	if pinned.ExtraArgs != nil {
		t.Fatalf("opencode takes no permission flag: %v", pinned.ExtraArgs)
	}
}

// TestApplyWorkerSpawnEnvironment_TheGrantFollowsTheIsolation is the whole of
// #927's rule, asserted at the seam every spawn passes through: a request that
// states it requires a container takes full control, one that does not takes
// the rationed host grant, and a role that states its own grant keeps it.
func TestApplyWorkerSpawnEnvironment_TheGrantFollowsTheIsolation(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		req  *SpawnSessionRequest
		want []string
	}{
		"a container is asked for by mode": {
			&SpawnSessionRequest{Backend: "claude", SandboxMode: SandboxModeRequired},
			[]string{WorkerFullControlFlag},
		},
		// Sandboxed=true is the legacy spelling of the same request, mapped by
		// EffectiveSandboxMode; it must not resolve to a weaker grant.
		"a container is asked for by the legacy flag": {
			&SpawnSessionRequest{Backend: "claude", Sandboxed: true},
			[]string{WorkerFullControlFlag},
		},
		// SandboxModeDefault is not yet resolved against policy — it may still
		// become a host session — so the narrower grant is the safe reading.
		"the mode is still unresolved": {
			&SpawnSessionRequest{Backend: "claude", SandboxMode: SandboxModeDefault},
			[]string{"--permission-mode", WorkerPermissionMode},
		},
		"a host session is asked for explicitly": {
			&SpawnSessionRequest{Backend: "claude", SandboxMode: SandboxModeNone},
			[]string{"--permission-mode", WorkerPermissionMode},
		},
		// The opt-out triage relies on: a sandboxed role that states the
		// narrower grant keeps it, so read-only work stays read-only.
		"a sandboxed role that narrows itself": {
			&SpawnSessionRequest{
				Backend:     "claude",
				SandboxMode: SandboxModeRequired,
				ExtraArgs:   []string{"--permission-mode", WorkerPermissionMode},
			},
			[]string{"--permission-mode", WorkerPermissionMode},
		},
	} {
		t.Run(name, func(t *testing.T) {
			ApplyWorkerSpawnEnvironment(tc.req, "")
			// THE GRANT IS THE PREFIX, not the whole argv. This case is about
			// what the request is permitted to do, and the seam also appends the
			// serena MCP registration, which grants nothing — asserting the
			// exact slice would make every future piece of engine-supplied
			// configuration look like a change to the grant.
			// TestApplyWorkerSpawnEnvironment_SerenaIsRegisteredForASandboxedWorker
			// owns that half.
			if len(tc.req.ExtraArgs) < len(tc.want) || !slices.Equal(tc.req.ExtraArgs[:len(tc.want)], tc.want) {
				t.Fatalf("ExtraArgs = %v, want it to begin with the grant %v", tc.req.ExtraArgs, tc.want)
			}
			if got := tc.req.ExtraArgs[len(tc.want):]; !slices.Equal(got, WorkerMCPArgs(tc.req.Backend, tc.req.EffectiveSandboxMode() == SandboxModeRequired)) {
				t.Fatalf("argv after the grant = %v; only the engine's MCP registration belongs there", got)
			}
		})
	}
}

// TestApplyWorkerSpawnEnvironment_SerenaIsRegisteredForASandboxedWorker: the
// charters tell every role to search with serena, so the seam that puts the
// engine's configuration on a spawn has to actually register it.
//
// SANDBOXED ONLY, because the config file lives in the worker IMAGE. Claude Code
// treats an unreadable --mcp-config as a startup failure, so naming it on a host
// session would turn "serena is unavailable" into "the worker never started".
func TestApplyWorkerSpawnEnvironment_SerenaIsRegisteredForASandboxedWorker(t *testing.T) {
	t.Parallel()

	sandboxed := &SpawnSessionRequest{Backend: "claude", SandboxMode: SandboxModeRequired}
	ApplyWorkerSpawnEnvironment(sandboxed, "")
	if !slices.Contains(sandboxed.ExtraArgs, "--mcp-config") ||
		!slices.Contains(sandboxed.ExtraArgs, SerenaClaudeMCPConfig()) {
		t.Errorf("ExtraArgs = %v, want the serena MCP config registered", sandboxed.ExtraArgs)
	}

	// A host session names no file, because there is none to name.
	host := &SpawnSessionRequest{Backend: "claude", SandboxMode: SandboxModeNone}
	ApplyWorkerSpawnEnvironment(host, "")
	if slices.Contains(host.ExtraArgs, "--mcp-config") {
		t.Errorf("ExtraArgs = %v; the image path does not exist on the host", host.ExtraArgs)
	}

	// A role that states its own MCP configuration is stating the whole set:
	// Claude Code merges every --mcp-config it is given, so appending the
	// engine's would put back a server that role deliberately left out.
	narrowed := &SpawnSessionRequest{
		Backend:     "claude",
		SandboxMode: SandboxModeRequired,
		ExtraArgs:   []string{"--mcp-config", "/etc/quicbaas/mcp/only-mine.json"},
	}
	ApplyWorkerSpawnEnvironment(narrowed, "")
	if slices.Contains(narrowed.ExtraArgs, SerenaClaudeMCPConfig()) {
		t.Errorf("ExtraArgs = %v; a caller's own MCP configuration must not be added to", narrowed.ExtraArgs)
	}

	// A backend that is not Claude Code takes no config file: codex is
	// registered through -c overrides where that backend is constructed, and
	// handing it a flag it does not accept kills it at exec time.
	if got := WorkerMCPArgs("codex", true); got != nil {
		t.Errorf("WorkerMCPArgs(codex) = %v, want nil", got)
	}
	if got := WorkerMCPArgs("opencode", true); got != nil {
		t.Errorf("WorkerMCPArgs(opencode) = %v, want nil", got)
	}
}

// TestApplyWorkerSpawnEnvironment_NilAndForeignBackend: a nil request is a
// no-op and a backend outside both sets is spawned exactly as it was.
func TestApplyWorkerSpawnEnvironment_NilAndForeignBackend(t *testing.T) {
	t.Parallel()
	ApplyWorkerSpawnEnvironment(nil, "/cfg")
	req := &SpawnSessionRequest{Backend: "ollama", SandboxMode: SandboxModeRequired}
	ApplyWorkerSpawnEnvironment(req, "/cfg")
	if req.ExtraArgs != nil || req.Env != nil || req.AccountAuto {
		t.Fatalf("a foreign backend must be untouched: %+v", req)
	}
}

// TestHasPermissionGrant covers both spellings the CLI accepts, and that
// nothing else is mistaken for one.
func TestHasPermissionGrant(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		args []string
		want bool
	}{
		"the mode flag":            {[]string{"--permission-mode", "plan"}, true},
		"the full-control flag":    {[]string{WorkerFullControlFlag}, true},
		"the flag among others":    {[]string{"--verbose", WorkerFullControlFlag, "--print"}, true},
		"no grant":                 {[]string{"--verbose", "--print"}, false},
		"nothing at all":           {nil, false},
		"a value, not the flag":    {[]string{"acceptEdits"}, false},
		"a similar but other flag": {[]string{"--permission-modes"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := hasPermissionGrant(tc.args); got != tc.want {
				t.Fatalf("hasPermissionGrant(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestQwenProviderConfigArgs_PointsCodexAtALocalModel pins the argv that makes
// the local reviewer work, every element of which was verified against the
// burn-in GPU host rather than read off documentation.
func TestQwenProviderConfigArgs_PointsCodexAtALocalModel(t *testing.T) {
	args := QwenProviderConfigArgs("http://gpu.invalid:11434")
	joined := strings.Join(args, " ")

	// wire_api MUST be "responses": codex 0.154.0 refuses `wire_api = "chat"`
	// outright ("no longer supported"), which is the whole reason this points
	// at /v1/responses rather than /v1/chat/completions.
	if !strings.Contains(joined, `wire_api="responses"`) {
		t.Fatalf("wire_api must be responses; codex refuses chat: %s", joined)
	}
	// The /v1 suffix is appended here so a caller configures a HOST.
	if !strings.Contains(joined, `base_url="http://gpu.invalid:11434/v1"`) {
		t.Fatalf("base_url must carry the /v1 suffix: %s", joined)
	}
	// Credential-free is the point: a local model has no token to mint.
	if !strings.Contains(joined, "requires_openai_auth=false") {
		t.Fatalf("a local model must not demand OpenAI auth: %s", joined)
	}
	if !strings.Contains(joined, "model_provider="+strconv.Quote(QwenProviderName)) {
		t.Fatalf("the provider must be selected, not merely defined: %s", joined)
	}
	// Every -c flag must be followed by its value.
	for i := 0; i < len(args); i += 2 {
		if args[i] != "-c" {
			t.Fatalf("argv %d = %q, want -c (flags and values must pair)", i, args[i])
		}
		if i+1 >= len(args) {
			t.Fatal("trailing -c with no value")
		}
	}
}

// TestQwenProviderConfigArgs_TrimsAndRefusesEmpty covers the input shapes an
// operator actually types. An empty endpoint yields NO argv rather than a
// malformed provider, so the backend falls back to codex's own default instead
// of pointing at "/v1".
func TestQwenProviderConfigArgs_TrimsAndRefusesEmpty(t *testing.T) {
	if got := QwenProviderConfigArgs("  "); got != nil {
		t.Fatalf("a blank endpoint must produce no argv, got %v", got)
	}
	if got := QwenProviderConfigArgs(""); got != nil {
		t.Fatalf("an empty endpoint must produce no argv, got %v", got)
	}
	trailing := strings.Join(QwenProviderConfigArgs("http://gpu.invalid:11434/"), " ")
	if !strings.Contains(trailing, `base_url="http://gpu.invalid:11434/v1"`) {
		t.Fatalf("a trailing slash must not double up: %s", trailing)
	}
}
