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

// Package deploy provides the SSH + SFTP primitives used by the agent
// deploy service to install and start the QuicBaaS agent daemon on a
// remote host.
//
// The package is intentionally narrow: it knows how to open an SSH
// session, upload a binary via SFTP, render a systemd unit or a
// nohup-backed start script, and invoke short remote commands. It
// contains no orchestration logic, no polling, and no SDK coupling.
// All orchestration lives in pkg/service/agent_deploy_service.go.
package deploy

import (
	"time"
)

// Spec is the full deployment specification consumed by the agent
// deploy service and the CLI.
//
// Fields carry user intent. Default/derived values (InstallDir,
// DataDir, WaitOnline) are filled in by the service layer before the
// primitives are invoked, so the primitives themselves never need to
// inspect optional state.
type Spec struct {
	// Host is the SSH destination in "host" or "host:port" form.
	// When no port is supplied, port 22 is assumed.
	Host string `json:"host"`

	// SSHUser is the login user for the SSH session. Required.
	SSHUser string `json:"ssh_user"`

	// SSHPassword is a password-based SSH auth option. Mutually
	// exclusive with SSHKeyName; when neither is set the dial falls
	// back to a caller-injected ssh.Signer via DialSSHWithAuth.
	SSHPassword string `json:"ssh_password,omitempty"`

	// SSHKeyName references a private key stored in the QuicBaaS SSH key
	// store (xkms-backed). When set, the deploy service resolves the key
	// through the configured SSHSignerResolver (or the xkms SSH client
	// directly); the private material never leaves xkms. Operators stage
	// keys into the store via `quicbaas ssh keys import` — there is no
	// local PEM-file fallback.
	SSHKeyName string `json:"ssh_key_name,omitempty"`

	// SSHKeyScope selects the xkms SSH store scope used to resolve
	// SSHKeyName. Valid values: "user", "tenant", "service". Empty
	// defaults to "user".
	SSHKeyScope string `json:"ssh_key_scope,omitempty"`

	// SSHKeyOwner is the owner identifier for the SSH key lookup.
	// Empty defaults depend on SSHKeyScope: user → current OS user,
	// tenant → current tenant ID, service → "default".
	SSHKeyOwner string `json:"ssh_key_owner,omitempty"`

	// InsecureHostKey disables host key verification. Exposed as an
	// explicit operator opt-out only; production deploys pin host keys
	// via the xkms-backed SSH known-hosts store, which is consulted
	// through an ssh.HostKeyCallback injected into DialSSHWithAuth at
	// the service layer.
	InsecureHostKey bool `json:"insecure_host_key,omitempty"`

	// Server is the QuicBaaS control-plane address the agent will dial
	// (e.g. https://quicbaas.example.com:8443). The server may be a
	// single quicbaasd process or a Raft-replicated cluster — the agent
	// dials the same endpoint either way.
	Server string `json:"server"`

	// Tenant is the tenant the agent runs under.
	Tenant string `json:"tenant"`

	// AgentBind is the local bind address the agent daemon exposes for
	// its AgentRuntime gRPC service on the remote host.
	AgentBind string `json:"agent_bind"`

	// Backends is the ordered list of backends to enable
	// (e.g. []string{"acp", "ollama"}).
	Backends []string `json:"backends"`

	// Binary is the local filesystem path to the static CLI binary
	// produced by `make build-static-cli`.
	Binary string `json:"binary"`

	// InstallDir is the remote directory the binary is uploaded to.
	// Defaults to ~/.quicbaas/bin when empty.
	InstallDir string `json:"install_dir,omitempty"`

	// DataDir is the remote work directory the agent stores state in.
	// Defaults to ~/.quicbaas/agent when empty.
	DataDir string `json:"data_dir,omitempty"`

	// Systemd controls whether a systemd --user unit is installed.
	// When nil, the service auto-detects support; when non-nil the
	// explicit value is honoured.
	Systemd *bool `json:"systemd,omitempty"`

	// WaitOnline is the maximum time the deploy will wait for the
	// agent to appear in the fleet registry with a live heartbeat.
	// Zero means "do not wait".
	WaitOnline time.Duration `json:"wait_online,omitempty"`

	// MCPEndpoint is the fully-qualified https://host:port/mcp address of
	// the control plane's MCP listener. When set, the rendered daemon
	// command line carries --mcp-endpoint so spawned agents can resolve the
	// "quicbaas" MCP preset against the long-lived mTLS identity uploaded by
	// this deploy. Populated by the deploy service from its configured MCP
	// endpoint; empty when the control plane has MCP disabled or unconfigured,
	// in which case the flag is omitted entirely (no behavior change).
	MCPEndpoint string `json:"mcp_endpoint,omitempty"`
}

// Result describes the outcome of a successful deployment.
type Result struct {
	// Host is the SSH destination that was deployed to.
	Host string `json:"host"`

	// InstalledPath is the absolute path of the uploaded binary on
	// the remote host.
	InstalledPath string `json:"installed_path"`

	// ServiceStatus is a short description of how the daemon was
	// started: "systemd", "nohup", or "unknown".
	ServiceStatus string `json:"service_status"`

	// AgentID is the identifier the control plane assigned to
	// the agent once it enrolled. Empty when WaitOnline==0.
	AgentID string `json:"agent_id,omitempty"`

	// OnlineAt is the RFC3339 timestamp when the agent was first
	// observed online by the control plane. Empty when
	// WaitOnline==0.
	OnlineAt string `json:"online_at,omitempty"`

	// AgentCertPath is the absolute path on the remote host where the
	// daemon's mTLS client certificate was installed. Empty when the
	// deploy flow did not issue a certificate.
	AgentCertPath string `json:"agent_cert_path,omitempty"`

	// AgentCertSerial is the serial number of the issued agent
	// certificate, hex-encoded. Surfaced so operators can correlate
	// deploys against the CA's certificate store.
	AgentCertSerial string `json:"agent_cert_serial,omitempty"`
}

// UseSystemd returns true when the spec explicitly requests a systemd
// install. The service layer uses this helper to drive its auto-detect
// fallback when Systemd is nil.
func (s *Spec) UseSystemd() bool {
	if s == nil || s.Systemd == nil {
		return false
	}
	return *s.Systemd
}

// SystemdExplicit reports whether the caller pinned the Systemd field
// to a non-nil value. When false, the service layer should auto-detect.
func (s *Spec) SystemdExplicit() bool {
	return s != nil && s.Systemd != nil
}

// ResolvedInstallDir returns InstallDir or the default when empty.
func (s *Spec) ResolvedInstallDir() string {
	if s == nil || s.InstallDir == "" {
		return ".quicbaas/bin"
	}
	return s.InstallDir
}

// ResolvedDataDir returns DataDir or the default when empty.
func (s *Spec) ResolvedDataDir() string {
	if s == nil || s.DataDir == "" {
		return ".quicbaas/agent"
	}
	return s.DataDir
}
