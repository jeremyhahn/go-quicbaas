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

package fleet

import (
	"context"
	"errors"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// FileMCPPresetResolver implements orchestration.MCPPresetResolver for a
// remotely deployed fleet node daemon. Unlike the server-side resolver
// (pkg/server.AgentMCPPresetResolver), which mints a fresh ephemeral
// certificate per spawn AND enrolls it as a session-scoped qrdb principal, this
// resolver mints nothing and enrolls nothing: it points the spawned agent at the
// long-lived mTLS client certificate the deploy flow already uploaded to the
// host (agent.crt / agent.key / server-ca.crt), so no new CA dependency or
// per-session key material is introduced on the remote side.
//
// The consequence is stated rather than assumed: a session reached this way
// authenticates as the DAEMON's identity, not its own, and therefore holds the
// daemon's agent-runtime grant rather than the narrower agent-session floor.
// go-qrdb resolves an mTLS principal's roles from the user record its Subject
// CommonName names, and the certificate on that host names the daemon
// (go-quicbaas#901). Giving a remote session its own principal needs the same
// mint-and-enroll seam the server-side resolver runs, which needs a CA and an
// identity store the remote host does not have; it is a control-plane-issued
// per-session credential, not a change to this file.
//
// The endpoint URL is supplied out-of-band by the control plane at deploy time
// (the `--mcp-endpoint` flag) because the MCP listener runs on a separate port
// from the control-plane port the daemon dials, and that port is not advertised
// over the v1 wire format. When no endpoint was configured, no resolver is
// constructed and the "quicbaas" preset is forwarded unmodified (harmless: it
// carries no URL).
type FileMCPPresetResolver struct {
	endpointURL string
	certFile    string
	keyFile     string
	caFile      string
}

// Compile-time interface check.
var _ orchestration.MCPPresetResolver = (*FileMCPPresetResolver)(nil)

// NewFileMCPPresetResolver constructs a resolver that hands the spawned agent
// the endpoint URL and the paths of the on-disk mTLS material. endpointURL is
// the fully-qualified https://host:port/mcp address. certFile, keyFile, and
// caFile are the long-lived agent certificate, private key, and trusted CA
// bundle the deploy flow installed. All four are required; an empty value is a
// wiring error and is rejected so the daemon never advertises an unusable
// preset.
func NewFileMCPPresetResolver(endpointURL, certFile, keyFile, caFile string) (*FileMCPPresetResolver, error) {
	if strings.TrimSpace(endpointURL) == "" {
		return nil, ErrFileMCPResolverNoEndpoint
	}
	if strings.TrimSpace(certFile) == "" {
		return nil, ErrFileMCPResolverNoCert
	}
	if strings.TrimSpace(keyFile) == "" {
		return nil, ErrFileMCPResolverNoKey
	}
	if strings.TrimSpace(caFile) == "" {
		return nil, ErrFileMCPResolverNoCA
	}
	return &FileMCPPresetResolver{
		endpointURL: endpointURL,
		certFile:    certFile,
		keyFile:     keyFile,
		caFile:      caFile,
	}, nil
}

// Resolve implements orchestration.MCPPresetResolver. It returns the configured
// endpoint URL and an env map pointing at the on-disk cert/key/ca files. There
// is no per-session state to provision, so the cleanup func is nil — the cert
// material is long-lived and shared across every session; deleting it would
// break subsequent spawns. The tenant is intentionally ignored: the on-disk
// certificate already encodes O=<tenant>, and the MCP authorizer scopes the
// agent from that attribute, not from this call.
func (r *FileMCPPresetResolver) Resolve(_ context.Context, _, _ string) (string, map[string]string, func(), error) {
	env := map[string]string{
		orchestration.MCPCertEnvVar: r.certFile,
		orchestration.MCPKeyEnvVar:  r.keyFile,
		orchestration.MCPCAEnvVar:   r.caFile,
	}
	return r.endpointURL, env, nil, nil
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrFileMCPResolverNoEndpoint is returned by NewFileMCPPresetResolver when no
// MCP endpoint URL is supplied.
var ErrFileMCPResolverNoEndpoint = errors.New("agent: file mcp preset resolver: endpoint url is required")

// ErrFileMCPResolverNoCert is returned when the agent certificate path is empty.
var ErrFileMCPResolverNoCert = errors.New("agent: file mcp preset resolver: agent certificate path is required")

// ErrFileMCPResolverNoKey is returned when the agent private key path is empty.
var ErrFileMCPResolverNoKey = errors.New("agent: file mcp preset resolver: agent private key path is required")

// ErrFileMCPResolverNoCA is returned when the trusted CA bundle path is empty.
var ErrFileMCPResolverNoCA = errors.New("agent: file mcp preset resolver: trusted ca bundle path is required")
