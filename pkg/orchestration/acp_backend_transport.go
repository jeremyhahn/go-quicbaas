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

package orchestration

// ACPTransport names how this backend speaks to its agent CLI.
//
// WHY A SELECTOR AND NOT A SECOND BACKEND TYPE. Everything that makes a spawn
// safe is transport-independent and already implemented once here: the sandbox
// container and its spec (sandboxSpec), the storage tier, the owner label that
// lets a restarted daemon reap its predecessor's containers, the pooled-account
// resolution and per-session credential materialization, the environment scrub,
// and the MCP preset wiring. A separate backend per CLI would have to duplicate
// all of it — and the two that matter most, the sandbox and the credential
// path, are the two where a second implementation drifting is a security
// finding rather than an inconvenience. So the CLI-specific part is data on the
// config, and the shared part stays shared.
//
// This replaces the CLIMode boolean, which could only say "Claude Code's
// stream-json or JSON-RPC" and therefore had no way to describe a CLI that
// speaks neither. CLIMode is still honoured (see ACPBackendConfig.transport) so
// every existing construction keeps its behaviour unchanged.
type ACPTransport string

const (
	// ACPTransportJSONRPC speaks the agent-client-protocol over the process's
	// stdio. It is the zero value, so a config that names no transport keeps
	// the behaviour it had.
	ACPTransportJSONRPC ACPTransport = ""

	// ACPTransportStreamJSON speaks Claude Code's stream-json protocol to a
	// persistent subprocess. What CLIMode selected.
	ACPTransportStreamJSON ACPTransport = "stream_json"

	// ACPTransportExec runs one non-interactive command per turn, feeding the
	// prompt on stdin and reading a JSONL event stream back.
	//
	// THE SESSION IS A CONTAINER, NOT A PROCESS. A one-shot CLI has no
	// long-running process to hold a session open, so the sandboxed form keeps
	// the CONTAINER alive across turns and execs into it per prompt
	// (ContainerRuntime.Exec). That is what gives a multi-round review the same
	// workspace, the same materialized credential and the same installed
	// toolchain on round three as it had on round one, without a process that
	// has to survive in between.
	ACPTransportExec ACPTransport = "exec"
)

// transport is the transport this config selects, honouring CLIMode.
//
// CLIMode WINS when set, because it is what existing constructions use and its
// meaning is exactly ACPTransportStreamJSON. A config that sets both is not an
// error: it is a caller mid-migration, and the boolean is the older, narrower
// statement.
func (c ACPBackendConfig) transport() ACPTransport {
	if c.CLIMode {
		return ACPTransportStreamJSON
	}
	switch c.Transport {
	case ACPTransportStreamJSON, ACPTransportExec:
		return c.Transport
	default:
		return ACPTransportJSONRPC
	}
}

// isExec reports whether turns run as one-shot commands.
func (c ACPBackendConfig) isExec() bool { return c.transport() == ACPTransportExec }

// isStreamJSON reports whether turns run over Claude Code's stream-json.
//
// Every pre-existing `b.config.CLIMode` test reads through here so the
// stream-json paths cannot start disagreeing with the selector.
func (c ACPBackendConfig) isStreamJSON() bool { return c.transport() == ACPTransportStreamJSON }

// ExecTurnArgs is the argv for one exec-transport turn, model included.
//
// The prompt is NOT here: it travels on stdin, because a prompt in argv is a
// prompt in /proc/<pid>/cmdline, and a worker charter carries the whole task.
// The final "-" that tells the CLI to read stdin is the caller's to put in
// ExecArgs, since not every CLI spells it the same way.
func (c ACPBackendConfig) ExecTurnArgs(model string, extra []string) []string {
	args := make([]string, 0, len(c.ExecArgs)+len(extra)+2)
	args = append(args, c.ExecArgs...)
	if model != "" && c.ExecModelFlag != "" {
		args = append(args, c.ExecModelFlag, model)
	}
	args = append(args, extra...)
	if c.ExecStdinArg != "" {
		args = append(args, c.ExecStdinArg)
	}
	return args
}

// CredentialDirSpec locates a file-based pooled credential.
//
// A CLI whose credential is a FILE cannot be served by the environment alone,
// so the sandboxed spawn mounts the per-session materialized directory that
// AccountCredential.Files wrote. Both known cases are described here rather
// than branched on by name: OpenCode keeps a config directory, codex keeps
// ~/.codex and finds it through CODEX_HOME.
type CredentialDirSpec struct {
	// Rel is the path inside the materialized home the account's Files wrote,
	// e.g. ".codex" or ".config/opencode". Empty means this CLI has no file
	// credential and nothing is mounted.
	Rel string `json:"rel,omitempty"`

	// Target is the absolute path inside the container to mount it at.
	Target string `json:"target,omitempty"`

	// HomeEnv, when set, is an environment variable pointed at Target for a
	// CLI that locates its credential by variable rather than by $HOME.
	// codex reads CODEX_HOME; OpenCode reads none and leaves this empty.
	HomeEnv string `json:"home_env,omitempty"`

	// Writable mounts the directory read-write.
	//
	// READ-ONLY IS THE DEFAULT AND THE RIGHT ONE: a credential the agent
	// cannot alter is a credential it cannot corrupt or exfiltrate through.
	// But a CLI that treats its credential directory as its whole state
	// directory has to write there, and refusing it does not make the spawn
	// safer — it makes it fail. Measured with codex-cli 0.154.0 against a
	// read-only CODEX_HOME:
	//
	//	Error: failed to initialize in-process app-server client:
	//	       Read-only file system (os error 30)
	//
	// The blast radius is bounded by what the directory IS: a fresh per-spawn
	// materialization holding one account's files, released by the session's
	// accountCleanup. A write cannot reach another session's credential, and
	// cannot reach the operator's own — which is the property the per-session
	// materialization exists to provide, and the reason granting write here is
	// not the same as granting write to a shared config.
	Writable bool `json:"writable,omitempty"`
}

// configured reports whether this CLI has a file credential to mount.
func (c CredentialDirSpec) configured() bool {
	return c.Rel != "" && c.Target != ""
}
