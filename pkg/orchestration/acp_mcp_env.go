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

import (
	"context"
	"encoding/json"
)

// QuicBaaSMCPPresetName is the MCPServerConfig.Name an operator selects when
// attaching the in-cluster QuicBaaS MCP endpoint in the Spawn dialog. The
// preset arrives at the backend carrying NO URL and NO connection material —
// it is a request, not a description. The backend resolves it at spawn time
// into a concrete endpoint URL plus a fresh per-spawn mTLS agent certificate
// (see MCPPresetResolver). A preset whose URL is already populated is treated
// as operator-supplied and left untouched.
const QuicBaaSMCPPresetName = "quicbaas"

// MCPPresetResolver resolves the empty-URL QuicBaaS MCP preset into a concrete
// endpoint URL and the connection material (mTLS cert/key/ca file paths) the
// spawned agent needs to authenticate to that endpoint. It is implemented at
// the server layer, where the agent-cert issuer and the MCP endpoint address
// live; the orchestration package depends only on this narrow seam so it stays
// free of server/cert/TLS dependencies.
//
// Resolve mints an ephemeral, short-lived certificate scoped to a single
// session and writes it to a private per-session directory. The returned
// cleanup deletes that directory; the backend attaches it to the session so it
// fires exactly once when the session is closed — the certificate never
// outlives the agent it was minted for. cleanup may be nil when there is
// nothing to clean up. An error means no material could be provisioned; the
// caller leaves the preset as-is rather than failing the spawn.
type MCPPresetResolver interface {
	Resolve(ctx context.Context, sessionID, tenantID string) (url string, env map[string]string, cleanup func(), err error)
}

// MCPServersEnvVar is the environment variable an ACP agent reads to discover
// the MCP servers preconfigured for its session. Its value is a JSON array of
// MCPServerConfig objects.
//
// Why an environment variable and not the ACP session/new mcpServers param:
// the claude / codex / opencode agents do not share a single mcpServers JSON
// shape, but they all inherit the spawned process environment. Delivering the
// preconfigured servers through a stable, self-describing env var lets the
// agent (or a thin MCP bootstrap shim in the agent runtime) read exactly the
// configuration the operator selected — including the QuicBaaS endpoint and
// the agent's mTLS cert/key paths carried in each server's Env map — without
// the orchestrator guessing a per-agent wire contract.
const MCPServersEnvVar = "QUICBAAS_MCP_SERVERS"

// MCP mTLS material env var names. A spawned ACP agent reads these from the
// per-server Env map (round-tripped through MCPServersEnvVar) to locate the
// client certificate, private key, and CA bundle it presents when dialing the
// QuicBaaS MCP endpoint. The MCP transport authenticates the agent by the
// client certificate at MCPCertEnvVar / MCPKeyEnvVar, verified against the CA
// at MCPCAEnvVar. These are the single source of truth shared by every
// MCPPresetResolver implementation (the server ephemeral-cert resolver and the
// remote-agent file-based resolver).
const (
	MCPCertEnvVar = "QUICBAAS_MCP_CERT"
	MCPKeyEnvVar  = "QUICBAAS_MCP_KEY"
	MCPCAEnvVar   = "QUICBAAS_MCP_CA"
)

// resolveMCPPresets resolves every empty-URL QuicBaaS MCP preset in the
// request against the configured MCPPresetResolver, mutating the request's
// MCPServers in place: the resolved endpoint URL is set on the entry and the
// returned connection material is merged into the entry's Env map. It returns
// a single cleanup func that runs every per-preset cleanup; the caller attaches
// it to the resulting session so the ephemeral certificate material is removed
// on Close. Returns nil when no preset was resolved (no resolver configured, no
// matching preset, or every resolution failed) so the caller has nothing to
// attach.
//
// Resolution must happen BEFORE injectMCPServersEnv so the populated URL + Env
// are carried in the QUICBAAS_MCP_SERVERS payload the agent reads. A resolver
// error for a single preset is logged and skipped, never fatal: the preset
// falls through to the env var with its empty URL, which is harmless.
func (b *ACPBackend) resolveMCPPresets(ctx context.Context, req *SpawnRequest) func() {
	if req == nil || b.config.MCPPresetResolver == nil || len(req.MCPServers) == 0 {
		return nil
	}

	tenant := TenantFromContext(ctx)

	var cleanups []func()
	for i := range req.MCPServers {
		entry := &req.MCPServers[i]
		if entry.Name != QuicBaaSMCPPresetName || entry.URL != "" {
			continue
		}

		// The resolver needs a stable per-resolution identifier for the cert
		// CN and the ephemeral dir. The agent's eventual session ID is not yet
		// known at this point (it is assigned by the agent during the spawn
		// handshake), so a fresh random identifier is used — the CN is
		// informational; the MCP authz path extracts the tenant from O=.
		resolveID, idErr := generateSessionID()
		if idErr != nil {
			if b.logger != nil {
				b.logger.Warnf("acp_backend: mcp preset resolve skipped: id generation failed: %v", idErr)
			}
			continue
		}

		url, env, cleanup, err := b.config.MCPPresetResolver.Resolve(ctx, resolveID, tenant)
		if err != nil {
			if b.logger != nil {
				b.logger.Warnf("acp_backend: mcp preset %q resolve failed (leaving preset unresolved): %v", entry.Name, err)
			}
			continue
		}

		entry.URL = url
		if len(env) > 0 {
			if entry.Env == nil {
				entry.Env = make(map[string]string, len(env))
			}
			for k, v := range env {
				entry.Env[k] = v
			}
		}
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}
	}

	if len(cleanups) == 0 {
		return nil
	}
	return func() {
		for _, c := range cleanups {
			c()
		}
	}
}

// attachCleanup binds a resolved-preset cleanup to the spawned session so it
// runs when the session is closed. When the session can no longer be found
// (already closed, or a non-session value), the cleanup is run immediately so
// the ephemeral cert directory is never leaked.
func (b *ACPBackend) attachCleanup(sessionID string, cleanup func()) {
	if cleanup == nil {
		return
	}
	val, ok := b.sessions.Load(sessionID)
	if !ok {
		cleanup()
		return
	}
	sess, ok := val.(*acpSession)
	if !ok {
		cleanup()
		return
	}
	sess.mu.Lock()
	sess.cleanup = cleanup
	sess.mu.Unlock()
}

// injectMCPServersEnv adds the request's MCP server configuration to req.Env
// (allocating the map if needed) so it propagates to the spawned agent process
// and is persisted on the session for the prompt-time relaunch. It is a no-op
// when no MCP servers are configured, so sessions that opt out carry no extra
// environment. An operator-supplied QUICBAAS_MCP_SERVERS value is never
// overwritten silently — explicit env wins over the structured field.
func injectMCPServersEnv(req *SpawnRequest) {
	if req == nil || len(req.MCPServers) == 0 {
		return
	}
	if _, ok := req.Env[MCPServersEnvVar]; ok {
		// The caller already set the env var explicitly; respect it.
		return
	}
	encoded, err := encodeMCPServers(req.MCPServers)
	if err != nil {
		// A config that cannot be encoded is dropped rather than failing the
		// spawn — the agent simply starts without MCP preconfig. This cannot
		// happen for the well-formed structs the SDK produces; the guard keeps
		// the spawn path total.
		return
	}
	if req.Env == nil {
		req.Env = make(map[string]string, 1)
	}
	req.Env[MCPServersEnvVar] = encoded
}

// encodeMCPServers serializes the MCP server configs to the compact JSON array
// the agent reads from MCPServersEnvVar.
func encodeMCPServers(servers []MCPServerConfig) (string, error) {
	b, err := json.Marshal(servers)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
