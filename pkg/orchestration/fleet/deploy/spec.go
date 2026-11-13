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
// deploy service to install and start the QuicBaaS fleet node daemon on a
// remote host.
//
// The package is intentionally narrow: it knows how to open an SSH
// session, upload a binary via SFTP, render a systemd unit or a
// nohup-backed start script, and invoke short remote commands. It
// contains no orchestration logic, no polling, and no SDK coupling.
// All orchestration lives in pkg/service/fleet_deploy_service.go.
package deploy

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
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
	Host string `json:"host" yaml:"host"`

	// SSHUser is the login user for the SSH session. Required.
	SSHUser string `json:"ssh_user" yaml:"ssh_user"`

	// SSHPassword is a password-based SSH auth option. Mutually
	// exclusive with SSHKeyName; when neither is set the dial falls
	// back to a caller-injected ssh.Signer via DialSSHWithAuth.
	SSHPassword string `json:"ssh_password,omitempty" yaml:"ssh_password,omitempty"`

	// SSHKeyName references a private key stored in the QuicBaaS SSH key
	// store (xkms-backed). When set, the deploy service resolves the key
	// through the configured SSHSignerResolver (or the xkms SSH client
	// directly); the private material never leaves xkms. Operators stage
	// keys into the store via `quicbaas ssh keys import` — there is no
	// local PEM-file fallback.
	SSHKeyName string `json:"ssh_key_name,omitempty" yaml:"ssh_key_name,omitempty"`

	// SSHKeyScope selects the xkms SSH store scope used to resolve
	// SSHKeyName. Valid values: "user", "tenant", "service". Empty
	// defaults to "user".
	SSHKeyScope string `json:"ssh_key_scope,omitempty" yaml:"ssh_key_scope,omitempty"`

	// SSHKeyOwner is the owner identifier for the SSH key lookup.
	// Empty defaults depend on SSHKeyScope: user → current OS user,
	// tenant → current tenant ID, service → "default".
	SSHKeyOwner string `json:"ssh_key_owner,omitempty" yaml:"ssh_key_owner,omitempty"`

	// InsecureHostKey disables host key verification. Exposed as an
	// explicit operator opt-out only; production deploys pin host keys
	// via the xkms-backed SSH known-hosts store, which is consulted
	// through a HostKeyPolicy injected into DialSSHWithAuthAndHostKey
	// at the service layer.
	InsecureHostKey bool `json:"insecure_host_key,omitempty" yaml:"insecure_host_key,omitempty"`

	// Server is the QuicBaaS control-plane address the agent will dial
	// (e.g. https://quicbaas.example.com:8443). The server may be a
	// single quicbaasd process or a Raft-replicated cluster — the agent
	// dials the same endpoint either way.
	Server string `json:"server" yaml:"server"`

	// Tenant is the tenant the agent runs under.
	Tenant string `json:"tenant" yaml:"tenant"`

	// NodeBind is the local bind address the fleet node daemon exposes for
	// its FleetRuntime gRPC service on the remote host.
	NodeBind string `json:"node_bind" yaml:"node_bind"`

	// Backends is the ordered list of backends the node serves, each by its
	// own name (fleet.BackendKind), or the "acp" group of every agent backend
	// — e.g. []string{"opencode", "openvino", "ollama"} for a node that must
	// never be handed claude or codex work.
	Backends []string `json:"backends" yaml:"backends"`

	// Binary is the local filesystem path to the static CLI binary
	// produced by `make build-static-cli`.
	Binary string `json:"binary" yaml:"binary"`

	// InstallDir is the remote directory the binary is uploaded to.
	// Defaults to ~/.quicbaas/bin when empty.
	InstallDir string `json:"install_dir,omitempty" yaml:"install_dir,omitempty"`

	// DataDir is the remote work directory the agent stores state in.
	// Defaults to ~/.quicbaas/agent when empty.
	DataDir string `json:"data_dir,omitempty" yaml:"data_dir,omitempty"`

	// Systemd controls whether a systemd --user unit is installed.
	// When nil, the service auto-detects support; when non-nil the
	// explicit value is honoured.
	Systemd *bool `json:"systemd,omitempty" yaml:"systemd,omitempty"`

	// WaitOnline is the maximum time the deploy will wait for the
	// agent to appear in the fleet registry with a live heartbeat.
	// Zero means "do not wait".
	WaitOnline time.Duration `json:"wait_online,omitempty" yaml:"wait_online,omitempty"`

	// MCPEndpoint is the fully-qualified https://host:port/mcp address of
	// the control plane's MCP listener. When set, the rendered daemon
	// command line carries --mcp-endpoint so spawned agents can resolve the
	// "quicbaas" MCP preset against the long-lived mTLS identity uploaded by
	// this deploy. Populated by the deploy service from its configured MCP
	// endpoint; empty when the control plane has MCP disabled or unconfigured,
	// in which case the flag is omitted entirely (no behavior change).
	MCPEndpoint string `json:"mcp_endpoint,omitempty" yaml:"mcp_endpoint,omitempty"`

	// ProjectsRoot is the directory the daemon provisions job workspaces
	// under. An absolute path is used as written; a relative one is taken
	// under the remote user's home, like InstallDir and DataDir. Empty takes
	// the daemon's default, <data-dir>/work/projects. The deploy owns this
	// setting — preflight measures its free space and undeploy
	// --purge-workspaces removes it — so it is declared here rather than in
	// the node section.
	ProjectsRoot string `json:"projects_root,omitempty" yaml:"projects_root,omitempty"`

	// WorkerImages are the container images this node's sandboxed sessions
	// run. Preflight checks each is present or pullable on the node and that
	// its build-time AGENT_UID matches the uid the daemon runs as; a
	// mismatch makes every signed commit in the container fail.
	WorkerImages []string `json:"worker_images,omitempty" yaml:"worker_images,omitempty"`

	// Agent is the daemon's own configuration: exactly fleet.Config, the
	// schema `fleet serve --config` reads. Deploy renders it into the config
	// file it installs next to the binary, filling in the settings the deploy
	// owns (server, tenant, bind address, backends, work and projects
	// directories, identity paths, MCP endpoint) from the fields above;
	// setting any of those here is refused rather than silently overwritten.
	//
	// agent.max_concurrent_jobs is required: a node that declares no capacity
	// leaves the whole fleet's capacity undeclared (see
	// fleet.Config.MaxConcurrentJobs).
	Node fleet.Config `json:"node" yaml:"node"`

	// SkipPreflight deploys without running the preflight checks first. It
	// is an operator's per-invocation choice (--skip-preflight), never part
	// of a committed node spec, so the spec file cannot set it.
	SkipPreflight bool `json:"skip_preflight,omitempty" yaml:"-"`

	// Local installs on the host this process runs on, through a
	// LocalTarget instead of ssh: a fleet node co-located with quicbaasd
	// needs no sshd. Host is then the host this node is advertised under,
	// and every ssh setting is empty (see LocalSSHConflict). It is an
	// operator's per-invocation choice (--local), so the spec file cannot
	// set it.
	Local bool `json:"local,omitempty" yaml:"-"`
}

// DefaultNodePort is the port a fleet node's runtime binds when the spec
// names no bind address.
const DefaultNodePort = "9091"

// Hostname returns the host part of target, which may be "host",
// "host:port", "user@host[:port]" or an IPv6 literal with or without
// brackets and port.
func Hostname(target string) string {
	trimmed := strings.TrimSpace(target)
	if at := strings.IndexByte(trimmed, '@'); at >= 0 {
		trimmed = trimmed[at+1:]
	}
	// A host:port pair (including a bracketed IPv6 literal with a port)
	// splits cleanly; anything else is a bare host.
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return host
	}
	// A bare IPv6 literal carries too many colons for SplitHostPort. It
	// may still be bracketed when written without a port.
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return trimmed[1 : len(trimmed)-1]
	}
	return trimmed
}

// DefaultNodeBind fills an empty NodeBind with the host being deployed to
// and DefaultNodePort. The default is the advertised host, never the
// wildcard (go-quicbaas#185): a fleet node listens where it is reached,
// not on every interface. An explicit NodeBind, wildcard included, is the
// operator's choice and is left as it is.
func (s *Spec) DefaultNodeBind() error {
	if s.NodeBind != "" {
		return nil
	}
	host := Hostname(s.Host)
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		return &InvalidSpecError{
			Field:  "node_bind",
			Reason: "no bind address given and no advertised host to default it to; set node_bind or --bind",
		}
	}
	s.NodeBind = net.JoinHostPort(host, DefaultNodePort)
	return nil
}

// LocalSSHConflict reports the first ssh setting a local install carries.
// A local install reaches no host over ssh, so any of them is a mistake.
func (s *Spec) LocalSSHConflict() error {
	for _, field := range []struct {
		name string
		set  bool
	}{
		{"ssh_user", s.SSHUser != ""},
		{"ssh_key_name", s.SSHKeyName != ""},
		{"ssh_password", s.SSHPassword != ""},
		{"insecure_host_key", s.InsecureHostKey},
	} {
		if field.set {
			return &LocalSSHConflictError{Field: field.name}
		}
	}
	return nil
}

// Result describes the outcome of a successful deployment.
type Result struct {
	// Host is the SSH destination that was deployed to.
	Host string `json:"host" yaml:"host"`

	// InstalledPath is the absolute path of the uploaded binary on
	// the remote host.
	InstalledPath string `json:"installed_path" yaml:"installed_path"`

	// ConfigPath is the absolute path of the rendered daemon config file
	// the installed unit runs with.
	ConfigPath string `json:"config_path,omitempty" yaml:"config_path,omitempty"`

	// ServiceStatus is a short description of how the daemon was
	// started: "systemd", "nohup", or "unknown".
	ServiceStatus string `json:"service_status" yaml:"service_status"`

	// AgentID is the identifier the control plane assigned to
	// the agent once it enrolled. Empty when WaitOnline==0.
	NodeID string `json:"node_id,omitempty" yaml:"node_id,omitempty"`

	// OnlineAt is the RFC3339 timestamp when the agent was first
	// observed online by the control plane. Empty when
	// WaitOnline==0.
	OnlineAt string `json:"online_at,omitempty" yaml:"online_at,omitempty"`

	// NodeCertPath is the absolute path on the remote host where the
	// daemon's mTLS client certificate was installed. Empty when the
	// deploy flow did not issue a certificate.
	NodeCertPath string `json:"node_cert_path,omitempty" yaml:"node_cert_path,omitempty"`

	// NodeCertSerial is the serial number of the issued agent
	// certificate, hex-encoded. Surfaced so operators can correlate
	// deploys against the CA's certificate store.
	NodeCertSerial string `json:"node_cert_serial,omitempty" yaml:"node_cert_serial,omitempty"`

	// Preflight is the report of the checks that ran before the install.
	// Nil when the deploy ran with SkipPreflight.
	Preflight *PreflightReport `json:"preflight,omitempty" yaml:"preflight,omitempty"`
}

// LifecycleResult describes the outcome of `fleet stop`, `fleet start` or
// `fleet undeploy` on one node.
type LifecycleResult struct {
	// Host is the SSH destination the command ran against.
	Host string `json:"host" yaml:"host"`

	// Action is "stop", "start" or "undeploy".
	Action string `json:"action" yaml:"action"`

	// Supervisor is how the agent was installed: "systemd", "nohup", or
	// empty when nothing was installed.
	Supervisor Supervisor `json:"supervisor,omitempty" yaml:"supervisor,omitempty"`

	// AgentID is the fleet registry ID of the node's agent, derived from the
	// certificate the deploy installed. Empty when it could not be read.
	NodeID string `json:"node_id,omitempty" yaml:"node_id,omitempty"`

	// Deregistered reports that the agent is no longer in the fleet
	// registry after the command: it was removed, or was not there.
	Deregistered bool `json:"deregistered" yaml:"deregistered"`

	// Removed lists the remote paths undeploy removed.
	Removed []string `json:"removed,omitempty" yaml:"removed,omitempty"`

	// OnlineAt is when a started agent was observed online. Empty when
	// start did not wait.
	OnlineAt string `json:"online_at,omitempty" yaml:"online_at,omitempty"`

	// Note explains anything the command could not do that did not fail
	// it, such as a registration it had no ID to remove.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
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

// DefaultSpec returns a Spec carrying the defaults a node spec's agent
// section starts from. The one that matters is the sandbox policy: the
// fleet.Config zero value is opt-in, the least secure policy, while a daemon
// started without a policy has always run "required". Seeding it here keeps a
// spec that says nothing about sandboxing on "required".
func DefaultSpec() *Spec {
	return &Spec{
		Node: fleet.Config{SandboxPolicy: orchestration.SandboxPolicyRequired},
	}
}

// LoadSpecFile decodes the YAML node spec at path onto into. Keys the file
// does not name keep into's values, so a caller seeds into with its defaults
// (DefaultSpec, plus any flag defaults) and the file overrides what it states.
//
// Unknown keys are refused, so a misspelt setting fails the deploy instead of
// being dropped. A node spec is a committed file, so it may not carry an SSH
// password: that belongs on the command line or, better, in the SSH key store.
func LoadSpecFile(path string, into *Spec) error {
	if into == nil {
		return &InvalidSpecError{Field: "spec", Reason: "spec is required"}
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the path is the operator's own --spec argument.
	if err != nil {
		return &SpecFileError{Path: path, Cause: err}
	}
	var probe struct {
		SSHPassword string `yaml:"ssh_password"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return &SpecFileError{Path: path, Cause: err}
	}
	if probe.SSHPassword != "" {
		return &SpecFileError{Path: path, Cause: &InvalidSpecError{
			Field:  "ssh_password",
			Reason: "a node spec is committed, so it must not carry a password; use ssh_key_name or --ssh-password",
		}}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(into); err != nil && !errors.Is(err, io.EOF) {
		return &SpecFileError{Path: path, Cause: err}
	}
	return nil
}

// deployOwnedNodeFields lists the agent-section keys the deploy fills in
// itself, each with the spec field that declares it. Setting one in the agent
// section would put the same setting in two places, and the deploy would have
// to pick a winner in silence; ValidateDaemonConfig refuses it instead.
var deployOwnedNodeFields = []struct {
	key   string
	owner string
	isSet func(*fleet.Config) bool
}{
	{"server_addr", "server", func(c *fleet.Config) bool { return c.ServerAddr != "" }},
	{"tenant_id", "tenant", func(c *fleet.Config) bool { return c.TenantID != "" }},
	{"bind_addr", "agent_bind", func(c *fleet.Config) bool { return c.BindAddr != "" }},
	{"backends", "backends", func(c *fleet.Config) bool { return len(c.Backends) > 0 }},
	{"work_dir", "data_dir", func(c *fleet.Config) bool { return c.WorkDir != "" }},
	{"projects_root", "projects_root", func(c *fleet.Config) bool { return c.ProjectsRoot != "" }},
	{"node_cert_file", "data_dir", func(c *fleet.Config) bool { return c.NodeCertFile != "" }},
	{"node_key_file", "data_dir", func(c *fleet.Config) bool { return c.NodeKeyFile != "" }},
	{"server_ca_file", "data_dir", func(c *fleet.Config) bool { return c.ServerCAFile != "" }},
	{"mcp_endpoint", "mcp_endpoint", func(c *fleet.Config) bool { return c.MCPEndpoint != "" }},
}

// ValidateDaemonConfig checks the daemon half of the spec before anything is
// issued or installed: no deploy-owned key is set in the node section, the
// node declares its capacity, and the config the deploy would render passes
// fleet.Config.Validate — so a daemon that would refuse to start is refused
// here, on the operator's machine, instead of in a restart loop on the node.
func (s *Spec) ValidateDaemonConfig() error {
	if s == nil {
		return &InvalidSpecError{Field: "spec", Reason: "spec is required"}
	}
	for _, field := range deployOwnedNodeFields {
		if field.isSet(&s.Node) {
			return &DeployOwnedFieldError{Field: "node." + field.key, Owner: field.owner}
		}
	}
	if s.Node.MaxConcurrentJobs <= 0 {
		return &CapacityUndeclaredError{Host: s.Host, Declared: s.Node.MaxConcurrentJobs}
	}
	// The remote home is not known until the SSH session resolves it; "/"
	// stands in for it so every derived path is absolute, which is all
	// Validate asks of them.
	if err := s.DaemonConfig(s.Layout("/")).Validate(); err != nil {
		return &AgentConfigInvalidError{Cause: err}
	}
	return nil
}

// NodeConfigFilename is the basename of the daemon config file deploy
// renders next to the binary. The installed unit runs
// `quicbaas fleet serve --config <install-dir>/agent.yaml`.
const NodeConfigFilename = "agent.yaml"

// StartScriptFilename is the basename of the nohup fallback launcher.
const StartScriptFilename = "start-quicbaas-agent.sh"

// Layout is every remote path one deployment owns, resolved against the
// remote user's home directory. The install, the lifecycle commands and
// preflight all read paths from here, so none of them derive a path the others
// would not.
type Layout struct {
	Home         string
	InstallDir   string
	Binary       string
	ConfigFile   string
	StartScript  string
	DataDir      string
	WorkDir      string
	ProjectsRoot string
	CertFile     string
	KeyFile      string
	ServerCAFile string
	LogFile      string
	PIDFile      string
	UnitFile     string
}

// Layout resolves the spec's directories under home. InstallDir and DataDir
// are always taken as home-relative (a leading "/" is ignored), matching the
// %h and $HOME joins the rendered unit and script make. ProjectsRoot is the
// exception: an absolute one is used as written, because it names a volume the
// operator chose rather than a place inside the deploy's own tree.
//
// An empty home yields "$HOME"-prefixed paths for use inside a remote shell.
func (s *Spec) Layout(home string) Layout {
	if home == "" {
		home = "$HOME"
	}
	underHome := func(p string) string {
		return path.Join(home, strings.TrimPrefix(p, "/"))
	}
	installDir := underHome(s.ResolvedInstallDir())
	dataDir := underHome(s.ResolvedDataDir())
	workDir := path.Join(dataDir, "work")

	projectsRoot := path.Join(workDir, "projects")
	if root := strings.TrimSpace(s.ProjectsRoot); root != "" {
		projectsRoot = root
		if !path.IsAbs(root) {
			projectsRoot = underHome(root)
		}
	}

	return Layout{
		Home:         home,
		InstallDir:   installDir,
		Binary:       path.Join(installDir, "quicbaas"),
		ConfigFile:   path.Join(installDir, NodeConfigFilename),
		StartScript:  path.Join(installDir, StartScriptFilename),
		DataDir:      dataDir,
		WorkDir:      workDir,
		ProjectsRoot: projectsRoot,
		CertFile:     path.Join(dataDir, NodeCertFilename),
		KeyFile:      path.Join(dataDir, NodeKeyFilename),
		ServerCAFile: path.Join(dataDir, ServerCAFilename),
		LogFile:      path.Join(dataDir, "agent.log"),
		PIDFile:      path.Join(dataDir, "agent.pid"),
		UnitFile:     path.Join(home, ".config/systemd/user", DefaultUnitName),
	}
}

// DaemonConfig is the fleet.Config the deployed daemon runs with: the spec's
// agent section with every deploy-owned setting filled in from the spec and
// the layout.
func (s *Spec) DaemonConfig(l Layout) *fleet.Config {
	cfg := s.Node
	cfg.ServerAddr = s.Server
	cfg.TenantID = s.Tenant
	cfg.BindAddr = s.NodeBind
	cfg.Backends = make([]fleet.BackendKind, 0, len(s.Backends))
	for _, backend := range s.Backends {
		cfg.Backends = append(cfg.Backends, fleet.BackendKind(backend))
	}
	cfg.WorkDir = l.WorkDir
	cfg.ProjectsRoot = l.ProjectsRoot
	cfg.NodeCertFile = l.CertFile
	cfg.NodeKeyFile = l.KeyFile
	cfg.ServerCAFile = l.ServerCAFile
	cfg.MCPEndpoint = strings.TrimSpace(s.MCPEndpoint)
	return &cfg
}

// agentConfigHeader opens every rendered config file, so an operator reading
// it on the node knows where its settings come from.
const agentConfigHeader = "# Rendered by `quicbaas fleet deploy` from this node's spec.\n" +
	"# Edit the spec and redeploy; changes made here are lost on the next deploy.\n"

// RenderNodeConfig renders the daemon config file for the spec under the
// given layout. The config is validated first, so a file the daemon would
// refuse is never written.
func RenderNodeConfig(s *Spec, l Layout) ([]byte, error) {
	cfg := s.DaemonConfig(l)
	if err := cfg.Validate(); err != nil {
		return nil, &AgentConfigInvalidError{Cause: err}
	}
	body, err := fleet.MarshalConfig(cfg)
	if err != nil {
		return nil, &AgentConfigInvalidError{Cause: err}
	}
	return append([]byte(agentConfigHeader), body...), nil
}
