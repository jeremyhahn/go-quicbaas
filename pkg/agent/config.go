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

// Package agent provides the remote agent daemon that runs on GPU hosts and
// other worker machines, enrolls with the control plane QuicBaaS orchestrator, and
// serves ACP/Ollama spawn requests over a gRPC AgentRuntime service.
//
// The daemon reuses pkg/orchestration backends (ACPBackend, OllamaBackend,
// ContainerRuntime) rather than re-implementing spawn paths. Sandboxing,
// identity, enrollment, and policy resolution layer on top of those
// primitives.
package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// BackendKind enumerates the backends a daemon can expose. Empty backends
// list means "all compiled-in backends available".
type BackendKind string

const (
	BackendACP    BackendKind = "acp"
	BackendOllama BackendKind = "ollama"
)

// ContainerRuntimeSelector picks which OCI runtime the daemon uses for
// sandboxed sessions. "auto" defers to runtime detection.
type ContainerRuntimeSelector string

const (
	ContainerRuntimeAuto   ContainerRuntimeSelector = "auto"
	ContainerRuntimeDocker ContainerRuntimeSelector = "docker"
	ContainerRuntimePodman ContainerRuntimeSelector = "podman"
	ContainerRuntimeNone   ContainerRuntimeSelector = "none"
)

// Config captures everything needed to start a daemon. Validate() must be
// called before use. Fields are JSON/YAML serializable for config-file use.
type Config struct {
	// ServerAddr is the HTTPS endpoint of the QuicBaaS server
	// (e.g., "https://198.51.100.51:8443"). Required.
	ServerAddr string `json:"server_addr" yaml:"server_addr"`

	// TenantID is the tenant this daemon serves. Required. Requests for
	// any other tenant are rejected with a permission error.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// BindAddr is the local listener address for the AgentRuntime gRPC
	// service (e.g., "127.0.0.1:9091"). Required.
	BindAddr string `json:"bind_addr" yaml:"bind_addr"`

	// Backends lists which agent backends this daemon exposes. Empty slice
	// means both ACP and Ollama are enabled.
	Backends []BackendKind `json:"backends,omitempty" yaml:"backends,omitempty"`

	// SandboxPolicy governs how SandboxModeDefault is resolved and which
	// explicit modes are accepted. Daemons default to
	// orchestration.SandboxPolicyRequired for compliance-first operation.
	SandboxPolicy orchestration.SandboxPolicy `json:"sandbox_policy" yaml:"sandbox_policy"`

	// ContainerRuntime selects which OCI runtime to use for sandboxed
	// sessions. "auto" probes docker then podman.
	ContainerRuntime ContainerRuntimeSelector `json:"container_runtime,omitempty" yaml:"container_runtime,omitempty"`

	// ImageMap maps backend name ("claude-code", "codex", "opencode") to
	// the fully qualified image reference the daemon spawns for
	// sandboxed sessions.
	ImageMap map[string]string `json:"image_map,omitempty" yaml:"image_map,omitempty"`

	// ImageRegistry is an optional registry prefix applied to ImageMap
	// values that lack one (e.g., "ghcr.io/jeremyhahn").
	ImageRegistry string `json:"image_registry,omitempty" yaml:"image_registry,omitempty"`

	// AutoPull instructs the daemon to `docker pull` each image at startup
	// if it is not already present locally.
	AutoPull bool `json:"auto_pull,omitempty" yaml:"auto_pull,omitempty"`

	// WorkDir is the base directory for per-session scratch, persisted
	// state, and cached credentials. Created if missing.
	WorkDir string `json:"work_dir" yaml:"work_dir"`

	// AgentCertFile is the path to the daemon's PEM-encoded mTLS client
	// certificate issued by the QuicBaaS server's CA. Required.
	AgentCertFile string `json:"agent_cert_file" yaml:"agent_cert_file"`

	// AgentKeyFile is the path to the daemon's PEM-encoded mTLS private
	// key matching AgentCertFile. Required; must be 0600 on disk.
	AgentKeyFile string `json:"agent_key_file" yaml:"agent_key_file"`

	// ServerCAFile is the path to a PEM-encoded CA certificate bundle
	// the daemon trusts when dialing ServerAddr over TLS. Required for
	// self-signed or private-CA deployments (typical for on-prem
	// quicbaasd installations).
	ServerCAFile string `json:"server_ca_file" yaml:"server_ca_file"`

	// HeartbeatInterval is how often the daemon heartbeats into the control plane
	// fleet registry. Zero means 30 seconds.
	HeartbeatInterval time.Duration `json:"heartbeat_interval,omitempty" yaml:"heartbeat_interval,omitempty"`

	// ShutdownGrace is how long Daemon.Stop waits for in-flight sessions
	// to drain before forcefully terminating. Zero means 30 seconds.
	ShutdownGrace time.Duration `json:"shutdown_grace,omitempty" yaml:"shutdown_grace,omitempty"`

	// MCPEndpoint is the fully-qualified https://host:port/mcp address of the
	// QuicBaaS MCP listener this daemon's spawned agents should dial when an
	// operator attaches the "quicbaas" MCP preset. It is supplied by the
	// control plane at deploy time because the MCP port is not advertised over
	// the v1 wire format. Optional: when empty the preset is forwarded
	// unmodified and the spawned agent simply cannot reach the endpoint
	// (current behavior, no regression).
	MCPEndpoint string `json:"mcp_endpoint,omitempty" yaml:"mcp_endpoint,omitempty"`
}

// Validate checks that required fields are set and values are sane.
func (c *Config) Validate() error {
	if c == nil {
		return ErrConfigNil
	}
	if strings.TrimSpace(c.ServerAddr) == "" {
		return &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "server_addr"}
	}
	if strings.TrimSpace(c.TenantID) == "" {
		return &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "tenant_id"}
	}
	if strings.TrimSpace(c.BindAddr) == "" {
		return &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "bind_addr"}
	}
	if strings.TrimSpace(c.WorkDir) == "" {
		return &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "work_dir"}
	}
	if strings.TrimSpace(c.AgentCertFile) == "" {
		return &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "agent_cert_file"}
	}
	if strings.TrimSpace(c.AgentKeyFile) == "" {
		return &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "agent_key_file"}
	}
	if strings.TrimSpace(c.ServerCAFile) == "" {
		return &SentinelDetailError{Sentinel: ErrConfigFieldRequired, Detail: "server_ca_file"}
	}
	for _, b := range c.Backends {
		switch b {
		case BackendACP, BackendOllama:
			// ok
		default:
			return &SentinelDetailError{Sentinel: ErrConfigUnknownBackend, Detail: fmt.Sprintf("%q", b)}
		}
	}
	switch c.ContainerRuntime {
	case "", ContainerRuntimeAuto, ContainerRuntimeDocker, ContainerRuntimePodman, ContainerRuntimeNone:
		// ok
	default:
		return &SentinelDetailError{Sentinel: ErrConfigUnknownRuntime, Detail: fmt.Sprintf("%q", c.ContainerRuntime)}
	}
	return nil
}

// EffectiveBackends returns the backend list with defaults applied.
func (c *Config) EffectiveBackends() []BackendKind {
	if len(c.Backends) > 0 {
		return c.Backends
	}
	return []BackendKind{BackendACP, BackendOllama}
}

// EffectiveHeartbeat returns HeartbeatInterval with a 30s default.
func (c *Config) EffectiveHeartbeat() time.Duration {
	if c.HeartbeatInterval > 0 {
		return c.HeartbeatInterval
	}
	return 30 * time.Second
}

// EffectiveShutdownGrace returns ShutdownGrace with a 30s default.
func (c *Config) EffectiveShutdownGrace() time.Duration {
	if c.ShutdownGrace > 0 {
		return c.ShutdownGrace
	}
	return 30 * time.Second
}

// EffectiveContainerRuntime returns the runtime selector with "auto" default.
func (c *Config) EffectiveContainerRuntime() ContainerRuntimeSelector {
	if c.ContainerRuntime == "" {
		return ContainerRuntimeAuto
	}
	return c.ContainerRuntime
}

// Config error sentinels.
var (
	ErrConfigNil            = errors.New("agent: config is nil")
	ErrConfigFieldRequired  = errors.New("agent: config field required")
	ErrConfigUnknownBackend = errors.New("agent: unknown backend")
	ErrConfigUnknownRuntime = errors.New("agent: unknown container runtime selector")
)
