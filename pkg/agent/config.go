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
	"path/filepath"
	"strconv"
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
	// (e.g., "https://quicbaas.example.com:8443"). Required.
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

	// ProjectsRoot is where this daemon provisions the named workspaces the
	// control plane's spawns ask for:
	// <projects_root>/<tenant_id>/<project_id>/workspace/<key>. Empty takes
	// <work_dir>/projects. See EffectiveProjectsRoot.
	//
	// The checkouts of every job this daemon runs live here, and a job's
	// later sessions are pinned back to it, so it must persist across
	// restarts — a root on tmpfs turns every restart into a stale workspace.
	ProjectsRoot string `json:"projects_root,omitempty" yaml:"projects_root,omitempty"`

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

	// MaxConcurrentJobs is how many orchestration jobs this host can run at
	// once. It is reported on every heartbeat and summed across the live
	// daemons to give admission the fleet's worker-slot capacity, which
	// per-project quotas cannot supply: N projects each inside their own quota
	// will otherwise oversubscribe the fleet (go-quicbaas#906).
	//
	// ZERO REPORTS NOTHING, AND THAT IS NOT ZERO SLOTS. A host that declares no
	// count leaves the fleet's sum a lower bound rather than a bound, and
	// admission then leaves the fleet axis out entirely instead of enforcing a
	// number it knows is too small. Declaring the count on EVERY daemon is what
	// turns the axis on; see orchestration.NewAgentFleetCapacity.
	//
	// IT IS A DECLARATION, NOT A LOCAL LIMITER. The daemon does not refuse a
	// spawn past this number — admission does, which is the one authority for
	// the bound. Set it to what the host can actually sustain: the number of
	// concurrent agent containers its CPU, memory and disk can carry.
	MaxConcurrentJobs int `json:"max_concurrent_jobs,omitempty" yaml:"max_concurrent_jobs,omitempty"`

	// EnrollRetryBase is the first wait the daemon takes before retrying a
	// TRANSIENT enrollment failure — the control plane is not listening
	// yet, its name does not resolve yet, or it answered 5xx while still
	// starting. Each subsequent wait doubles up to EnrollRetryMax. Zero
	// means 1 second. Terminal failures (a rejected identity, a bad cert)
	// are never retried regardless of this setting.
	EnrollRetryBase time.Duration `json:"enroll_retry_base,omitempty" yaml:"enroll_retry_base,omitempty"`

	// EnrollRetryMax caps a single enrollment backoff wait. Zero means 60
	// seconds. It bounds journal noise while an agent host waits out a
	// control plane that boots after it; it is not a total budget, because
	// an agent that gives up would just be restarted into the same wait by
	// its supervisor.
	EnrollRetryMax time.Duration `json:"enroll_retry_max,omitempty" yaml:"enroll_retry_max,omitempty"`

	// SandboxNetwork is the container network sandboxed sessions run in.
	// Empty resolves to orchestration.SandboxNetworkDefault: an isolated
	// namespace with egress, so an agent reaches its provider API but not the
	// host's loopback-bound services. Setting it to "host" is the explicit
	// operator decision that removes network isolation entirely; see
	// orchestration.ParseSandboxNetwork.
	SandboxNetwork string `json:"sandbox_network,omitempty" yaml:"sandbox_network,omitempty"`

	// SandboxCapAdd is the capability set a sandboxed container is given back
	// after --cap-drop ALL. Empty takes orchestration.DefaultSandboxCapAdd
	// (CHOWN, DAC_OVERRIDE, FOWNER, SETGID, SETUID: what sudo and apk need).
	// A non-empty list REPLACES the default; "ALL" is refused.
	SandboxCapAdd []string `json:"sandbox_cap_add,omitempty" yaml:"sandbox_cap_add,omitempty"`

	// SandboxPidsLimit bounds processes plus threads in a sandboxed
	// container (--pids-limit). Zero takes
	// orchestration.DefaultSandboxPidsLimit; negative is refused.
	SandboxPidsLimit int64 `json:"sandbox_pids_limit,omitempty" yaml:"sandbox_pids_limit,omitempty"`

	// OllamaBaseURL is the OpenAI-compatible base URL of the ollama server
	// this daemon's local-model backends talk to, WITHOUT the /v1 suffix.
	// Empty takes DefaultOllamaBaseURL.
	//
	// IT IS CONFIGURABLE BECAUSE THE MODEL IS RARELY ON THIS BOX. It was
	// hardcoded to loopback, which is right only for a daemon running on the
	// GPU host itself; every other node in a fleet has to reach that host over
	// the network, and had no way to say so.
	OllamaBaseURL string `json:"ollama_base_url,omitempty" yaml:"ollama_base_url,omitempty"`

	// OpenVINOBaseURL is the OpenVINO Model Server this daemon's `openvino`
	// backend talks to, WITHOUT the /v3 suffix. Empty leaves the backend
	// unregistered — there is no sensible default, because the server is a
	// deployment's own and pointing at a guess would register a backend that
	// fails on first use.
	OpenVINOBaseURL string `json:"openvino_base_url,omitempty" yaml:"openvino_base_url,omitempty"`

	// OpenVINOModel is the model that server serves, e.g.
	// "Qwen3-8B-int4-cw-ov" exported for an NPU.
	OpenVINOModel string `json:"openvino_model,omitempty" yaml:"openvino_model,omitempty"`

	// OpenVINOContextTokens is how much prompt that server accepts. Zero
	// declares nothing and the backend learns the ceiling from the first
	// refusal; declaring it is what turns the first oversized prompt from a
	// wasted round trip into an immediate, named refusal.
	//
	// ON AN NPU THIS IS A MEMORY DECISION, NOT ONLY A CORRECTNESS ONE. The
	// plugin compiles a STATIC shape, so the server reserves for the declared
	// maximum whether or not anything uses it. Measured on one host with an
	// 8B int4 model: 8192 tokens reserved 14.2 GiB and loaded in 45 seconds;
	// 32768 reserved 63.7 GiB, took seven minutes, and drove the machine into
	// swap. Size it for the work, not for the model's maximum.
	OpenVINOContextTokens int `json:"openvino_context_tokens,omitempty" yaml:"openvino_context_tokens,omitempty"`

	// OllamaAgentModel and OllamaAgentContextTokens configure the
	// "ollama-agent" backend: the container-and-tools agent loop pointed at
	// the same ollama endpoint the plain "ollama" backend uses. It exists
	// because triage must read a repository and a model server cannot.
	OllamaAgentModel string `json:"ollama_agent_model,omitempty" yaml:"ollama_agent_model,omitempty"`

	// OllamaAgentContextTokens is what that endpoint's model accepts; zero
	// learns the ceiling from the first refusal.
	OllamaAgentContextTokens int `json:"ollama_agent_context_tokens,omitempty" yaml:"ollama_agent_context_tokens,omitempty"`

	// VLLMBaseURL, VLLMModel and VLLMContextTokens configure the "vllm"
	// backend: the same container-and-tools agent loop against a vLLM server.
	//
	// IT IS REGISTERED ALONGSIDE ollama RATHER THAN INSTEAD OF IT. Both stay
	// available, so moving a project between them is an edit to its binding
	// rather than a redeploy — and moving BACK is the same edit. vLLM is
	// preferred for throughput: it batches continuously and keeps a
	// cross-request prefix cache, neither of which ollama does. Measured on
	// one fleet at 268.7 triage turns/min against ollama's 21.0 at the same
	// concurrency, with ollama flat across every concurrency tested.
	VLLMBaseURL string `json:"vllm_base_url,omitempty" yaml:"vllm_base_url,omitempty"`

	// VLLMModel is the served-model name vLLM answers to (its
	// --served-model-name), not a file path.
	VLLMModel string `json:"vllm_model,omitempty" yaml:"vllm_model,omitempty"`

	// VLLMContextTokens is what that model accepts; zero learns the ceiling
	// from the first refusal.
	VLLMContextTokens int `json:"vllm_context_tokens,omitempty" yaml:"vllm_context_tokens,omitempty"`

	// CodexReasoningEffort is the depth the codex backend reasons at:
	// minimal, low, medium or high. Empty takes DefaultCodexReasoningEffort.
	//
	// IT IS STATED RATHER THAN INHERITED because code review is a GATE. A
	// shallower pass does not fail visibly — it approves what it should have
	// caught — so the depth reviews run at must be a deployment decision an
	// operator can read, not whatever the installed CLI defaults to.
	CodexReasoningEffort string `json:"codex_reasoning_effort,omitempty" yaml:"codex_reasoning_effort,omitempty"`

	// AllowedMountRoots is the operator's allowlist of absolute host
	// directories a spawn request may bind-mount into a sandbox container on
	// this daemon. It is the daemon-side counterpart of the control plane's
	// orchestration.sandbox.allowed_mount_roots.
	//
	// Deny-by-default: the zero value is an empty allowlist, under which a
	// sandboxed spawn may mount nothing but the workspaces this daemon
	// provisioned itself under its projects root (tenant-scoped, exactly as
	// the control plane grants its own). Any other directory — a sockets
	// root, a shared data directory — must be named here. There is
	// deliberately no setting that turns the check off.
	AllowedMountRoots []string `json:"allowed_mount_roots,omitempty" yaml:"allowed_mount_roots,omitempty"`

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
	if _, err := orchestration.ParseSandboxNetwork(c.SandboxNetwork); err != nil {
		return &SentinelDetailError{
			Sentinel: ErrConfigUnknownNetwork,
			Detail:   fmt.Sprintf("%q", c.SandboxNetwork),
		}
	}
	if err := c.EffectiveSandboxSecurity().Validate(); err != nil {
		return &SentinelDetailError{
			Sentinel: ErrConfigInvalidSandboxSecurity,
			Detail:   err.Error(),
		}
	}
	if c.EnrollRetryBase < 0 {
		return &SentinelDetailError{
			Sentinel: ErrConfigInvalidRetryWindow,
			Detail:   "enroll_retry_base: " + c.EnrollRetryBase.String(),
		}
	}
	if c.EnrollRetryMax < 0 {
		return &SentinelDetailError{
			Sentinel: ErrConfigInvalidRetryWindow,
			Detail:   "enroll_retry_max: " + c.EnrollRetryMax.String(),
		}
	}
	// A negative slot count cannot be a capacity. Refused at start-up rather
	// than clamped to zero, because zero is the "reported nothing" reading and
	// clamping would turn an operator's typo into a host that silently stopped
	// counting towards the fleet.
	if c.MaxConcurrentJobs < 0 {
		return &SentinelDetailError{
			Sentinel: ErrConfigNegativeCapacity,
			Detail:   "max_concurrent_jobs: " + strconv.Itoa(c.MaxConcurrentJobs),
		}
	}
	// Mount roots are compared against absolute container mount sources, so a
	// relative entry could never match and would silently disable the grant
	// the operator meant to configure. Reject it at startup instead.
	for i, root := range c.AllowedMountRoots {
		if !filepath.IsAbs(root) {
			return &SentinelDetailError{
				Sentinel: ErrConfigMountRootNotAbsolute,
				Detail:   fmt.Sprintf("allowed_mount_roots[%d]: %q", i, root),
			}
		}
	}
	// The projects root is a mount grant and a path the control plane pins
	// jobs to, so a relative one — which would move with the process's
	// working directory — is refused for the allowlist's own reason.
	if root := c.EffectiveProjectsRoot(); !filepath.IsAbs(root) {
		return &SentinelDetailError{
			Sentinel: ErrConfigProjectsRootNotAbsolute,
			Detail:   fmt.Sprintf("projects_root: %q", root),
		}
	}
	return nil
}

// SigningAgents is the identifier of every control-plane sockets root this
// daemon can mount a signing socket from: the markers found under the roots
// its mount policy admits (orchestration.DiscoverSigningRootIDs). It is never
// nil. A daemon on another host than the control plane finds none, which is
// the point — it cannot mount that socket, and the control plane must not
// place a signing spawn on it (go-quicbaas#981).
func (c *Config) SigningAgents() []string {
	return orchestration.DiscoverSigningRootIDs(c.AllowedMountRoots)
}

// EffectiveProjectsRoot is the root this daemon provisions named workspaces
// under: ProjectsRoot when set, <WorkDir>/projects otherwise.
func (c *Config) EffectiveProjectsRoot() string {
	if root := strings.TrimSpace(c.ProjectsRoot); root != "" {
		return filepath.Clean(root)
	}
	return filepath.Join(c.WorkDir, defaultProjectsDirName)
}

// defaultProjectsDirName is the projects root's directory under the work
// directory when none is configured.
const defaultProjectsDirName = "projects"

// MountPolicy returns the bind-mount policy this daemon enforces on every
// sandboxed spawn: the workspaces it provisioned under its projects root,
// tenant-scoped, plus the operator's allowlist. It is the control plane's
// policy shape exactly, so a workspace is admitted here by the same grant that
// admits it on the daemon.
func (c *Config) MountPolicy() *orchestration.MountPolicy {
	return &orchestration.MountPolicy{
		ProjectsRoot: c.EffectiveProjectsRoot(),
		AllowedRoots: c.AllowedMountRoots,
	}
}

// EffectiveSandboxNetwork returns the configured container network for
// sandboxed sessions, resolved to the isolated default when unset. A value
// that fails validation cannot reach here: Validate rejects it.
func (c *Config) EffectiveSandboxNetwork() orchestration.SandboxNetwork {
	network, err := orchestration.ParseSandboxNetwork(c.SandboxNetwork)
	if err != nil {
		return orchestration.SandboxNetworkDefault
	}
	return network
}

// EffectiveSandboxSecurity returns the privilege policy every sandboxed
// container this agent launches runs under. Its zero fields resolve to the
// secure defaults inside orchestration.SandboxSecurity.
func (c *Config) EffectiveSandboxSecurity() orchestration.SandboxSecurity {
	return orchestration.SandboxSecurity{
		CapAdd:    c.SandboxCapAdd,
		PidsLimit: c.SandboxPidsLimit,
	}
}

// DefaultOllamaBaseURL is the ollama server a daemon uses when none is
// configured: the local one.
const DefaultOllamaBaseURL = "http://127.0.0.1:11434"

// DefaultCodexReasoningEffort is the depth codex reviews run at when the
// deployment states none. Medium is the deliberate middle: minimal and low
// skim, and high costs materially more for a gate that already reads whole
// files through serena.
const DefaultCodexReasoningEffort = "medium"

// EffectiveCodexReasoningEffort returns the configured effort, or the default.
func (c *Config) EffectiveCodexReasoningEffort() string {
	if effort := strings.TrimSpace(c.CodexReasoningEffort); effort != "" {
		return effort
	}
	return DefaultCodexReasoningEffort
}

// EffectiveOllamaBaseURL returns the configured ollama base URL, or the
// local default. The trailing slash is trimmed so callers can append a path
// without producing a double slash.
func (c *Config) EffectiveOllamaBaseURL() string {
	url := strings.TrimRight(strings.TrimSpace(c.OllamaBaseURL), "/")
	if url == "" {
		return DefaultOllamaBaseURL
	}
	return url
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

// Enrollment retry defaults. One second is short enough that a control
// plane which comes up moments after its agents is joined almost
// immediately; one minute is long enough that a control plane which is
// down for hours costs one journal line per minute instead of thousands.
const (
	defaultEnrollRetryBase = time.Second
	defaultEnrollRetryMax  = time.Minute
)

// EffectiveEnrollRetryBase returns EnrollRetryBase with its 1s default.
func (c *Config) EffectiveEnrollRetryBase() time.Duration {
	if c.EnrollRetryBase > 0 {
		return c.EnrollRetryBase
	}
	return defaultEnrollRetryBase
}

// EffectiveEnrollRetryMax returns EnrollRetryMax with its 60s default. A
// cap configured below the base is raised to the base: a schedule that
// shrank as it retried would be a surprise, not a policy.
func (c *Config) EffectiveEnrollRetryMax() time.Duration {
	base := c.EffectiveEnrollRetryBase()
	max := c.EnrollRetryMax
	if max <= 0 {
		max = defaultEnrollRetryMax
	}
	if max < base {
		return base
	}
	return max
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
	ErrConfigUnknownNetwork = errors.New("agent: invalid sandbox network")

	// ErrConfigInvalidSandboxSecurity reports a sandbox privilege policy that
	// cannot be applied: a negative pids limit, or a capability add-back that
	// is malformed or "ALL". It is refused at startup, because the same policy
	// applied at spawn time would fail every sandboxed session.
	ErrConfigInvalidSandboxSecurity = errors.New("agent: invalid sandbox security policy")

	// ErrConfigInvalidRetryWindow reports a negative enrollment backoff
	// setting. A negative wait would make the retry loop the tight spin
	// the backoff exists to prevent, so it is rejected at startup rather
	// than silently clamped.
	ErrConfigInvalidRetryWindow = errors.New("agent: enrollment retry window must not be negative")

	// ErrConfigMountRootNotAbsolute reports an allowed_mount_roots entry that
	// is not an absolute path. Such an entry can never match a container mount
	// source, so accepting it would leave the operator believing they had
	// granted a root they had not.
	ErrConfigMountRootNotAbsolute = errors.New("agent: allowed mount root must be an absolute path")

	// ErrConfigProjectsRootNotAbsolute reports a projects root — configured,
	// or derived from a relative work directory — that is not absolute.
	ErrConfigProjectsRootNotAbsolute = errors.New("agent: projects root must be an absolute path")

	// ErrConfigNegativeCapacity reports a negative max_concurrent_jobs. Zero is
	// the legitimate "this host reports no count" value; a negative number is
	// one somebody wrote on purpose and got wrong, and it cannot be told apart
	// from the unreported case once summed.
	ErrConfigNegativeCapacity = errors.New("agent: max concurrent jobs must not be negative")
)
