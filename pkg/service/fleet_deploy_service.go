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

package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// Fleet Deploy Service
// =============================================================================
//
// FleetDeployService is the single entry point for provisioning a
// remote QuicBaaS fleet node daemon. It orchestrates four primitive steps:
//
//   1. Mint an mTLS client certificate for the daemon via the
//      server-embedded CA. The daemon presents this certificate on
//      POST /api/v1/fleet/enroll to bootstrap OAuth2 credentials.
//   2. Dial the target host over SSH and upload the static CLI binary
//      via SFTP. The mTLS certificate, private key, and server CA
//      bundle are uploaded alongside the binary.
//   3. Install the daemon as either a systemd --user unit or a nohup
//      fallback script, depending on the spec and what the remote host
//      supports.
//   4. Optionally poll the control plane's fleet registry via
//      the SDK until the freshly-deployed fleet node reports online.
//
// The service depends on narrow interfaces so tests can replace every
// primitive with fakes. The "real" adapters delegate to pkg/fleet/deploy.

// FleetDeployService installs a QuicBaaS fleet node daemon on a remote host.
type FleetDeployService interface {
	// Deploy runs the full deployment workflow described by spec and
	// returns the resulting *deploy.Result on success. All errors are
	// typed values from pkg/fleet/deploy so callers can distinguish
	// failure modes without parsing strings.
	Deploy(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error)

	// Preflight checks, without changing anything on the node, that a
	// deploy of spec would work: SSH and the pinned host key, the deploy
	// key, systemd --user and lingering, the container runtime, disk, the
	// worker images and their uid, and that the node itself can reach the
	// control plane over verified TLS. The report lists every check; the
	// error, when any failed, is a *deploy.PreflightFailedError carrying
	// each failure's typed error and fix. Deploy runs the same checks first
	// unless spec.SkipPreflight is set.
	Preflight(ctx context.Context, spec *deploy.Spec) (*deploy.PreflightReport, error)

	// Stop stops the fleet node on the node and removes it from the fleet
	// registry, so a stopped node never reads "running".
	Stop(ctx context.Context, spec *deploy.Spec) (*deploy.LifecycleResult, error)

	// Start starts an installed fleet node and, when spec.WaitOnline is set,
	// waits for it to come back online.
	Start(ctx context.Context, spec *deploy.Spec) (*deploy.LifecycleResult, error)

	// Undeploy stops and disables the fleet node, removes everything the deploy
	// installed (unit, config, binary, identity), and removes it from the
	// fleet registry. Workspaces are kept unless opts.PurgeWorkspaces.
	Undeploy(ctx context.Context, spec *deploy.Spec, opts UndeployOptions) (*deploy.LifecycleResult, error)
}

// UndeployOptions tunes Undeploy.
type UndeployOptions struct {
	// PurgeWorkspaces also removes the projects root and the daemon's work
	// directory. Off by default: they hold job checkouts a pinned job may
	// still need.
	PurgeWorkspaces bool
}

// =============================================================================
// Dependency Interfaces
// =============================================================================

// sshSession is an opaque handle returned by the dialer and threaded
// through the other primitives. Tests pass a struct{} sentinel; the
// production adapter threads the concrete *deploy.SSHClient.
type sshSession interface {
	Close() error
}

// sshDialer abstracts the SSH dial step. Both entry points take the
// deploy.HostKeyPolicy the service resolved from the known-hosts store;
// a nil policy leaves the spec's own policy (InsecureHostKey) in
// charge, which refuses to dial unless the operator opted out.
type sshDialer interface {
	// Dial opens an SSH connection using credentials read from the spec
	// (ssh_password).
	Dial(ctx context.Context, spec *deploy.Spec, hostKey *deploy.HostKeyPolicy) (sshSession, error)

	// DialWithAuth opens an SSH connection using an explicit set of
	// ssh.AuthMethod values. FleetDeployService uses this to inject an
	// SSHSignerResolver-backed ssh.Signer when spec.SSHKeyName is set.
	DialWithAuth(ctx context.Context, spec *deploy.Spec, auth []ssh.AuthMethod, hostKey *deploy.HostKeyPolicy) (sshSession, error)
}

// binaryUploader abstracts the SFTP binary upload.
type binaryUploader interface {
	Upload(ctx context.Context, sess sshSession, localPath, remotePath string) error
}

// fileUploader abstracts the SFTP upload of arbitrary byte payloads
// (certificate PEM, private key PEM, CA bundle PEM) with an explicit
// mode. Kept separate from binaryUploader so tests can substitute a
// fake without also stubbing the binary-upload path.
type fileUploader interface {
	WriteRemoteFile(ctx context.Context, sess sshSession, remotePath string, content []byte, mode uint32) error
}

// systemdInstaller abstracts systemd unit install + start.
type systemdInstaller interface {
	Detect(ctx context.Context, sess sshSession) bool
	InstallAndStart(ctx context.Context, sess sshSession, unitContent, unitName string) error
}

// nohupStarter abstracts the fallback nohup start.
type nohupStarter interface {
	Start(ctx context.Context, sess sshSession, scriptContent, scriptPath string) error
}

// unitRenderer abstracts rendering systemd unit and start-script bodies.
type unitRenderer interface {
	RenderUnit(spec *deploy.Spec) string
	RenderStartScript(spec *deploy.Spec) string
}

// FleetNodeLister abstracts the SDK fleet registry poll. It is the
// dependency the service needs to confirm a deployed fleet node has come
// online. Callers wire their SDK orchestrator client into a concrete
// implementation of this interface.
type FleetNodeLister interface {
	ListFleetNodes(ctx context.Context, req *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error)
}

// =============================================================================
// Service Implementation
// =============================================================================

// fleetDeployService is the default, production FleetDeployService.
type fleetDeployService struct {
	ssh        sshDialer
	upload     binaryUploader
	files      fileUploader
	systemd    systemdInstaller
	nohup      nohupStarter
	renderer   unitRenderer
	node       nodeOperator
	agents     FleetNodeLister
	remover    FleetNodeRemover
	sshKeys    SSHSignerResolver
	hostKeys   SSHHostKeyResolver
	certIssuer FleetNodeCertIssuer
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration) error

	// probe dials the deployed fleet node's runtime port to confirm a process
	// is still holding it. The wait-online phase requires it to succeed
	// before it calls a fleet node online. Left nil, the service dials for
	// real via deploy.DialFleetRuntime: a missing probe must never be
	// read as a probe that passed.
	probe func(ctx context.Context, addr string) error

	// mcpEndpoint is the control plane's configured MCP listener address
	// (https://host:port/mcp). When non-empty it is stamped onto each Spec
	// (unless the Spec already carries one) so the deployed daemon is started
	// with --mcp-endpoint and its spawned fleet nodes can resolve the "quicbaas"
	// MCP preset. Empty when MCP is disabled or unconfigured: the flag is then
	// omitted and behavior is unchanged.
	mcpEndpoint string

	// resolveHome resolves the remote login user's $HOME on a connected
	// session. Production wires the real SSH-exec implementation; tests
	// substitute a deterministic value so the fake session need not
	// embed an *deploy.SSHClient.
	resolveHome func(ctx context.Context, sess sshSession) (string, error)

	// localTarget opens the install target for a local deploy
	// (Spec.Local): this host, reached with no ssh. Production wires
	// deploy.NewLocalTarget; tests substitute a recording target.
	localTarget func(host string) (deploy.Target, error)
}

// FleetDeployServiceDeps wires injectable dependencies for the
// production-configured service. Tests build a fleetDeployService
// directly; callers use NewFleetDeployService which fills defaults.
type FleetDeployServiceDeps struct {
	// FleetNodeLister is the SDK-backed orchestrator client used to poll
	// the fleet registry when Spec.WaitOnline > 0. Optional: when nil
	// the service skips the wait-online phase even if a budget is set.
	FleetNodeLister FleetNodeLister

	// FleetNodeRemover removes a fleet node from the fleet registry. Required by
	// Stop and Undeploy, which refuse to run without it: stopping a fleet node
	// the registry still calls "running" is the failure they exist to end.
	FleetNodeRemover FleetNodeRemover

	// SSHKeys resolves a Spec.SSHKeyName into an ssh.Signer backed by
	// an SSH key-management implementation (typically xkms). Optional:
	// when nil the service falls back to spec-embedded credentials
	// (SSHPassword), matching the legacy password-only workflow.
	SSHKeys SSHSignerResolver

	// SSHHostKeys resolves the deploy target's pinned host key from the
	// SSH known-hosts store — the same store `quicbaas ssh known-hosts
	// add/list` manage — into a deploy.HostKeyPolicy. Optional: when nil
	// the dial falls back to the spec's own policy, so a deploy without
	// Spec.InsecureHostKey fails rather than trusting an unverified host.
	SSHHostKeys SSHHostKeyResolver

	// CertIssuer produces the mTLS client identity the daemon presents
	// on POST /api/v1/fleet/enroll. Required for production deploys;
	// when nil the service returns a typed error before dialing SSH so
	// misconfigured callers fail fast.
	CertIssuer FleetNodeCertIssuer

	// MCPEndpoint is the control plane's configured MCP listener address
	// (https://host:port/mcp). Optional: when set, every deployed daemon is
	// started with --mcp-endpoint so its spawned fleet nodes can reach the QuicBaaS
	// MCP endpoint with the long-lived mTLS identity this deploy uploads. When
	// empty the daemon command line is unchanged (no behavior change).
	MCPEndpoint string
}

// NewFleetDeployService wires the default SSH/SFTP/systemd primitives
// from pkg/fleet/deploy plus the caller-provided SDK fleet node lister.
func NewFleetDeployService(deps FleetDeployServiceDeps) FleetDeployService {
	return &fleetDeployService{
		ssh:         realSSHDialer{},
		upload:      realUploader{},
		files:       realFileUploader{},
		systemd:     realSystemdInstaller{},
		nohup:       realNohupStarter{},
		renderer:    realRenderer{},
		node:        realNodeOperator{},
		agents:      deps.FleetNodeLister,
		remover:     deps.FleetNodeRemover,
		sshKeys:     deps.SSHKeys,
		hostKeys:    deps.SSHHostKeys,
		certIssuer:  deps.CertIssuer,
		now:         time.Now,
		sleep:       ctxSleep,
		probe:       deploy.DialFleetRuntime,
		resolveHome: defaultResolveRemoteHome,
		mcpEndpoint: deps.MCPEndpoint,
		localTarget: newLocalTarget,
	}
}

// Deploy implements FleetDeployService.Deploy.
//
// Steps execute linearly: each stage produces inputs for the next, and
// any failure aborts the workflow with a typed error. The SSH session
// is always closed on exit.
func (s *fleetDeployService) Deploy(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
	if err := s.validate(spec); err != nil {
		return nil, err
	}
	// A node is deployed, and its identity certificate minted, in the caller's
	// own tenant only — the system tenant included (go-qrdb v0.28: no caller
	// mints another tenant's certificates).
	if _, err := tenantctx.Target(ctx, "FleetDeployService.Deploy", spec.Tenant); err != nil {
		return nil, err
	}
	s.stampMCPEndpoint(spec)

	// Preflight first, on its own SSH session, before a certificate is
	// minted or a byte is written: a node that cannot run the fleet node is
	// reported with every reason at once, and left exactly as it was.
	var preflight *deploy.PreflightReport
	if !spec.SkipPreflight {
		report, err := s.preflight(ctx, spec)
		if err != nil {
			return nil, err
		}
		preflight = report
	}

	// Mint the fleet node's mTLS identity up-front, before we dial SSH. If
	// the CA rejects the request we surface the error immediately and
	// never touch the remote host.
	issued, err := s.issueNodeCert(ctx, spec)
	if err != nil {
		return nil, err
	}
	// The buffer must be destroyed the moment we're done reading its
	// bytes, even on error paths below. Declare the cleanup here so it
	// runs via defer regardless of how the function exits.
	defer issued.PrivateKey.Destroy()

	result, err := s.install(ctx, spec, issued)
	if err != nil {
		return nil, err
	}
	result.Preflight = preflight

	// The SSH session is closed by now, deliberately: see install().
	if spec.WaitOnline > 0 && s.agents != nil {
		expectedID := expectedNodeID(spec, issued)
		id, onlineAt, err := s.waitOnline(ctx, spec, expectedID)
		if err != nil {
			return result, err
		}
		result.NodeID = id
		result.OnlineAt = onlineAt
	}

	return result, nil
}

// install runs every deployment step that needs the SSH session and
// closes that session before returning.
//
// Closing before the caller verifies the fleet node is part of the contract,
// not tidy-up. A systemd --user manager that does not linger is torn
// down with the last session of its user, so a fleet node checked over the
// deploy's own connection can be reported online and still be dead a
// second later — the remote-agent failure this ordering exists to catch
// (go-quicbaas#980).
func (s *fleetDeployService) install(
	ctx context.Context,
	spec *deploy.Spec,
	issued *IssuedFleetNodeCertificate,
) (*deploy.Result, error) {
	sess, err := s.dial(ctx, spec)
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	// Resolve the remote user's $HOME once so downstream paths can be
	// absolute. SFTP doesn't expand shell variables, and systemd's %h
	// placeholder only applies when unit processing is active — using
	// an absolute path keeps every code path consistent.
	remoteHome, err := s.remoteHome(ctx, sess)
	if err != nil {
		return nil, err
	}

	layout := spec.Layout(remoteHome)
	if err := s.upload.Upload(ctx, sess, spec.Binary, layout.Binary); err != nil {
		return nil, err
	}

	if err := s.uploadNodeIdentity(ctx, sess, layout.DataDir, issued); err != nil {
		return nil, err
	}

	// The daemon's whole configuration, rendered from the spec's fleet node
	// section. The unit and the start script run `fleet serve --config` on
	// exactly this file. Owner-only, like the identity beside it.
	agentConfig, err := deploy.RenderNodeConfig(spec, layout)
	if err != nil {
		return nil, err
	}
	if err := s.files.WriteRemoteFile(ctx, sess, layout.ConfigFile, agentConfig, 0o600); err != nil {
		return nil, &FleetNodeConfigUploadError{Path: layout.ConfigFile, Cause: err}
	}

	serviceStatus, err := s.startDaemon(ctx, sess, spec, layout.InstallDir)
	if err != nil {
		return nil, err
	}

	return &deploy.Result{
		Host:           spec.Host,
		InstalledPath:  layout.Binary,
		ConfigPath:     layout.ConfigFile,
		ServiceStatus:  serviceStatus,
		NodeCertPath:   layout.CertFile,
		NodeCertSerial: formatSerial(issued),
	}, nil
}

// validate returns an InvalidSpecError for the first missing required
// field. The primitives perform their own deeper validation on fields
// they consume directly.
func (s *fleetDeployService) validate(spec *deploy.Spec) error {
	if spec == nil {
		return &deploy.InvalidSpecError{Field: "spec", Reason: "spec is required"}
	}
	if err := validateTarget(spec); err != nil {
		return err
	}
	if spec.Server == "" {
		return &deploy.InvalidSpecError{Field: "server", Reason: "server is required"}
	}
	if spec.Tenant == "" {
		return &deploy.InvalidSpecError{Field: "tenant", Reason: "tenant is required"}
	}
	if spec.Binary == "" {
		return &deploy.InvalidSpecError{Field: "binary", Reason: "binary path is required"}
	}
	if spec.NodeBind == "" {
		return &deploy.InvalidSpecError{Field: "agent_bind", Reason: "agent bind address is required"}
	}
	// A wait-online budget is a promise to verify the fleet node from this
	// side once the SSH session is closed, and that verification is a
	// dial. A bind the deploy cannot turn into a dial target — a named
	// port, a unix socket, or the ephemeral ":0" whose real port only the
	// remote process knows — makes the promise unkeepable, so it is
	// refused here, before anything is installed, rather than silently
	// downgraded to "the registry says running": that is the exact
	// evidence which reported the remote fleet node Online while every connection to it
	// was refused (go-quicbaas#980).
	if spec.WaitOnline > 0 && s.agents != nil && nodeProbeAddr(spec) == "" {
		return undialableBindError(spec)
	}
	if len(spec.Backends) == 0 {
		return &deploy.InvalidSpecError{Field: "backends", Reason: "at least one backend is required"}
	}
	if s.certIssuer == nil {
		return &deploy.InvalidSpecError{Field: "cert_issuer", Reason: "agent certificate issuer is required"}
	}
	if conflict := sshAuthConflict(spec); conflict != nil {
		return conflict
	}
	// The daemon half: declared capacity, no deploy-owned key in the fleet node
	// section, and a rendered config the daemon would accept.
	return spec.ValidateDaemonConfig()
}

// stampMCPEndpoint copies the control plane's configured MCP endpoint onto
// the spec so the deployed daemon's config carries it. A caller-supplied
// value on the spec wins; an empty service endpoint leaves the spec untouched
// and the rendered config omits the key.
func (s *fleetDeployService) stampMCPEndpoint(spec *deploy.Spec) {
	if strings.TrimSpace(spec.MCPEndpoint) == "" && s.mcpEndpoint != "" {
		spec.MCPEndpoint = s.mcpEndpoint
	}
}

// validateTarget checks that spec names the node and how to reach it: an
// ssh user for an ssh install, no ssh settings at all for a local one.
func validateTarget(spec *deploy.Spec) error {
	if spec.Host == "" {
		return &deploy.InvalidSpecError{Field: "host", Reason: "host is required"}
	}
	if spec.Local {
		return spec.LocalSSHConflict()
	}
	if spec.SSHUser == "" {
		return &deploy.InvalidSpecError{Field: "ssh_user", Reason: "ssh user is required"}
	}
	return nil
}

// sshAuthConflict rejects specs that set more than one of
// {SSHKeyName, SSHPassword}. Callers must pick exactly one auth mode;
// ambiguous specs would otherwise silently resolve to whichever mode
// the dial path checks first.
func sshAuthConflict(spec *deploy.Spec) error {
	var set []string
	if spec.SSHKeyName != "" {
		set = append(set, "ssh_key_name")
	}
	if spec.SSHPassword != "" {
		set = append(set, "ssh_password")
	}
	if len(set) <= 1 {
		return nil
	}
	return &SSHAuthConflictError{Fields: set}
}

// issueNodeCert requests a fresh mTLS identity from the configured CA.
// The caller owns the returned *secure.Buffer and MUST Destroy() it on
// every exit path. Deploy() does this via defer.
func (s *fleetDeployService) issueNodeCert(ctx context.Context, spec *deploy.Spec) (*IssuedFleetNodeCertificate, error) {
	req := &FleetNodeCertificateRequest{
		TenantID: spec.Tenant,
		Hostname: hostnameForCert(spec.Host),
	}
	return s.certIssuer.IssueFleetNodeCertificate(ctx, req)
}

// uploadNodeIdentity writes the daemon's mTLS materials into DataDir
// on the remote host with appropriate permissions:
//
//	fleet node.crt     0644 - public certificate, safe for world-readable
//	fleet node.key     0600 - private key, owner-only
//	server-ca.crt 0644 - trust anchor, safe for world-readable
//
// The caller retains ownership of issued.PrivateKey; this function
// only reads its bytes. Destruction happens in Deploy via defer.
func (s *fleetDeployService) uploadNodeIdentity(
	ctx context.Context,
	sess sshSession,
	dataDir string,
	issued *IssuedFleetNodeCertificate,
) error {
	if err := s.files.WriteRemoteFile(ctx, sess, path.Join(dataDir, deploy.NodeCertFilename), issued.CertPEM, 0o644); err != nil {
		return &FleetNodeIdentityUploadError{File: deploy.NodeCertFilename, Cause: err}
	}

	//secure:unborrowed injected-interface WriteRemoteFile requires a byte slice and does not retain the key past upload.
	keyBytes := issued.PrivateKey.Bytes()
	if err := s.files.WriteRemoteFile(ctx, sess, path.Join(dataDir, deploy.NodeKeyFilename), keyBytes, 0o600); err != nil {
		return &FleetNodeIdentityUploadError{File: deploy.NodeKeyFilename, Cause: err}
	}

	if err := s.files.WriteRemoteFile(ctx, sess, path.Join(dataDir, deploy.ServerCAFilename), issued.CAPEM, 0o644); err != nil {
		return &FleetNodeIdentityUploadError{File: deploy.ServerCAFilename, Cause: err}
	}
	return nil
}

// startDaemon installs and starts the daemon. It prefers systemd when
// the spec requests it (or auto-detects support), falling back to the
// nohup script otherwise. The returned string labels the chosen path
// for the Result.
func (s *fleetDeployService) startDaemon(
	ctx context.Context,
	sess sshSession,
	spec *deploy.Spec,
	installDir string,
) (string, error) {
	wantSystemd := spec.UseSystemd()
	if !spec.SystemdExplicit() {
		wantSystemd = s.systemd.Detect(ctx, sess)
	}

	if wantSystemd {
		unit := s.renderer.RenderUnit(spec)
		if err := s.systemd.InstallAndStart(ctx, sess, unit, deploy.DefaultUnitName); err != nil {
			return "", err
		}
		return "systemd", nil
	}

	script := s.renderer.RenderStartScript(spec)
	scriptPath := path.Join(installDir, deploy.StartScriptFilename)
	if err := s.nohup.Start(ctx, sess, script, scriptPath); err != nil {
		return "", err
	}
	return "nohup", nil
}

// waitOnline polls the orchestrator's fleet registry until the
// freshly-deployed fleet node reports a live status AND its runtime port
// answers a dial, or the WaitOnline budget elapses.
//
// Both halves are needed. A registration keeps the status of its last
// heartbeat after the process behind it is gone, so the registry alone
// cannot tell a running fleet node from one that died seconds ago — which is
// exactly what it reported on the remote fleet node while every dial to the fleet node's port
// was refused. The dial is made from here, after install() closed the
// SSH session, so what it proves is the property the deploy claims: the
// fleet node outlived the session that started it.
//
// The poll is scoped to the tenant only: filtering by Host on the
// server side would re-introduce the very mismatch this code path
// avoids, since the daemon registers under its resolved hostname while
// the deploy spec carries only the SSH-target / NodeBind. Selection is
// performed client-side by pickOnlineNode, preferring an exact match
// against the AgentID the server will assign (computed from the mTLS
// certificate CN and the tenant) and falling back to a host-suffix
// heuristic for callers that do not pass an expected ID.
func (s *fleetDeployService) waitOnline(ctx context.Context, spec *deploy.Spec, expectedID string) (string, string, error) {
	deadline := s.now().Add(spec.WaitOnline)
	const pollEvery = 2 * time.Second

	probeAddr := nodeProbeAddr(spec)
	if probeAddr == "" {
		// validate() rejects this combination before the install runs;
		// re-checking here keeps the invariant local to the only code
		// that could otherwise report Online without dialing anything.
		return "", "", undialableBindError(spec)
	}
	var probeErr error

	for {
		resp, err := s.agents.ListFleetNodes(ctx, &orchestratorSDK.ListFleetNodesRequest{
			TenantID: spec.Tenant,
		})
		if err == nil {
			if id, at := pickOnlineNode(resp, spec, expectedID); id != "" {
				probeErr = s.probeRuntimePort(ctx, probeAddr)
				if probeErr == nil {
					return id, at, nil
				}
			}
		}

		if !s.now().Before(deadline) {
			return "", "", &deploy.StartTimeoutError{
				Host:    spec.Host,
				Tenant:  spec.Tenant,
				Elapsed: spec.WaitOnline.String(),
				Cause:   probeErr,
			}
		}

		if err := s.sleep(ctx, pollEvery); err != nil {
			return "", "", err
		}
	}
}

// probeRuntimePort dials the fleet node's runtime listener once.
//
// There is no path through here that reports success without a dial. A
// service built without an injected probe falls back to the real dialer
// instead of waving the check through, and an empty address is rejected
// by deploy.DialFleetRuntime as an invalid spec — callers never reach
// this with one, because both validate() and waitOnline() refuse a bind
// that has no dial target.
func (s *fleetDeployService) probeRuntimePort(ctx context.Context, addr string) error {
	probe := s.probe
	if probe == nil {
		probe = deploy.DialFleetRuntime
	}
	return probe(ctx, addr)
}

// undialableBindError reports a Spec whose NodeBind gives the deploy no
// address to verify the fleet node on, in the one case where that matters:
// a non-zero WaitOnline. The message names the shape that works so the
// operator can fix the flag without reading this code.
func undialableBindError(spec *deploy.Spec) error {
	bind := ""
	if spec != nil {
		bind = strings.TrimSpace(spec.NodeBind)
	}
	return &deploy.InvalidSpecError{
		Field: "agent_bind",
		Reason: "waiting for the agent to come online requires a bind address the deploy can dial back, " +
			"i.e. host:port with a fixed numeric port such as node01:9091; got " + strconv.Quote(bind),
	}
}

// nodeProbeAddr returns the host:port the operator can dial to reach the
// deployed daemon: the SSH target's host joined with the port the fleet node
// binds. Returns "" when there is nothing dialable — no host, or an
// NodeBind without a fixed numeric TCP port. net.SplitHostPort accepts
// a service name, which no dialer here can resolve to a fixed target,
// and port 0 asks the kernel for an ephemeral port that only the remote
// process ever learns.
func nodeProbeAddr(spec *deploy.Spec) string {
	if spec == nil {
		return ""
	}
	host := hostnameForCert(spec.Host)
	if host == "" {
		return ""
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(spec.NodeBind))
	if err != nil {
		return ""
	}
	number, err := strconv.Atoi(port)
	if err != nil || number <= 0 || number > 65535 {
		return ""
	}
	return net.JoinHostPort(host, port)
}

// dial selects the SSH entry point. Either way the target's host key is
// verified against its pin first: hostKeyPolicy resolves the pin
// before any connection is made, so an unpinned or rotated host fails
// with a typed error instead of a half-finished deployment.
//
// When spec.SSHKeyName is set the stored key is resolved via
// SSHSignerResolver and dialed with an injected ssh.Signer so the
// private material never leaves its key store. Otherwise the dialer's
// default credential path (password) applies.
func (s *fleetDeployService) dial(ctx context.Context, spec *deploy.Spec) (sshSession, error) {
	if spec.Local {
		return s.openLocal(spec)
	}
	hostKey, err := s.hostKeyPolicy(ctx, spec)
	if err != nil {
		return nil, err
	}
	if spec.SSHKeyName == "" {
		return s.ssh.Dial(ctx, spec, hostKey)
	}
	if s.sshKeys == nil {
		return nil, &deploy.InvalidSpecError{
			Field:  "ssh_key_name",
			Reason: "ssh key service not configured",
		}
	}
	sshSigner, err := s.sshKeys.Signer(ctx, SSHKeyRef{
		Name:  spec.SSHKeyName,
		Scope: spec.SSHKeyScope,
		Owner: spec.SSHKeyOwner,
	})
	if err != nil {
		return nil, err
	}
	return s.ssh.DialWithAuth(ctx, spec, []ssh.AuthMethod{ssh.PublicKeys(sshSigner)}, hostKey)
}

// openLocal opens this host as the install target of a local deploy. The
// install that follows is the ssh deploy's, step for step: only the
// transport differs.
func (s *fleetDeployService) openLocal(spec *deploy.Spec) (sshSession, error) {
	open := s.localTarget
	if open == nil {
		open = newLocalTarget
	}
	target, err := open(spec.Host)
	if err != nil {
		return nil, err
	}
	return &sshSessionAdapter{c: target}, nil
}

// newLocalTarget is the production local target: this host, as the
// current user.
func newLocalTarget(host string) (deploy.Target, error) {
	return deploy.NewLocalTarget(host)
}

// hostKeyPolicy resolves the target's pinned host key into the policy
// the dial verifies it with: the callback, and the host-key algorithms
// that make the host present a key the pins cover.
//
// Spec.InsecureHostKey short-circuits the lookup entirely: an operator
// who asked to skip verification is not made to pin a host first. With
// no resolver configured the policy is nil and the dial falls back to
// the spec's own policy, which refuses an unverified host.
//
// The known-hosts store is addressed by the same (scope, owner) pair as
// the SSH key store, so --ssh-key-scope / --ssh-key-owner select both.
func (s *fleetDeployService) hostKeyPolicy(ctx context.Context, spec *deploy.Spec) (*deploy.HostKeyPolicy, error) {
	if spec.InsecureHostKey || s.hostKeys == nil {
		return nil, nil
	}
	return s.hostKeys.HostKeyPolicy(ctx, SSHHostRef{
		Host:  spec.Host,
		Scope: spec.SSHKeyScope,
		Owner: spec.SSHKeyOwner,
	})
}

// pickOnlineNode inspects a ListFleetNodesResponse for a fleet node that
// reports an online status and matches the deployment target.
//
// Selection precedence:
//
//  1. expectedID match — when the caller pre-computes the canonical
//     AgentID the server assigns (via iam.FleetNodeAppName) we match by
//     that ID exactly and ignore Host. This is the deterministic path
//     used by the fleet node deploy service after it issues the daemon's
//     mTLS cert.
//
//  2. Host heuristic — when expectedID is empty (e.g. callers that
//     don't have a cert in hand) we fall back to the legacy match: an
//     online fleet node whose registered Host equals or ends with
//     spec.NodeBind. This preserves the historical contract for any
//     existing caller that constructs ListFleetNodesResponse manually.
//
// In both cases ties are broken by the most recent LastSeen timestamp
// so the freshest live registration wins.
func pickOnlineNode(resp *orchestratorSDK.ListFleetNodesResponse, spec *deploy.Spec, expectedID string) (string, string) {
	if resp == nil || len(resp.Nodes) == 0 {
		return "", ""
	}
	var best *orchestratorSDK.FleetNode
	for _, a := range resp.Nodes {
		if a == nil {
			continue
		}
		if !isOnlineStatus(a.Status) {
			continue
		}
		if expectedID != "" {
			if a.ID != expectedID {
				continue
			}
		} else if spec.NodeBind != "" && !strings.HasSuffix(a.Host, spec.NodeBind) && a.Host != spec.NodeBind {
			continue
		}
		if best == nil || a.LastSeen > best.LastSeen {
			best = a
		}
	}
	if best == nil {
		return "", ""
	}
	at := ""
	if best.LastSeen > 0 {
		at = time.Unix(0, best.LastSeen).UTC().Format(time.RFC3339)
	}
	return best.ID, at
}

// expectedNodeID returns the canonical AgentID the server will assign
// when the daemon presents its issued mTLS certificate at
// /api/v1/fleet/enroll. The value is derived from the certificate's
// Common Name plus the tenant via iam.FleetNodeAppName so client and server
// agree on the identifier without an extra round-trip.
//
// Returns the empty string when no usable CN is present, in which case
// pickOnlineNode falls back to its host-suffix heuristic.
func expectedNodeID(spec *deploy.Spec, issued *IssuedFleetNodeCertificate) string {
	if spec == nil || issued == nil || issued.ParsedCert == nil {
		return ""
	}
	cn := strings.TrimSpace(issued.ParsedCert.Subject.CommonName)
	if cn == "" {
		return ""
	}
	return iam.FleetNodeAppName(spec.Tenant, cn)
}

// isOnlineStatus reports whether the fleet node's status string means "live".
func isOnlineStatus(s string) bool {
	switch s {
	case "running", "idle", "ready":
		return true
	default:
		return false
	}
}

// pathJoinHome joins a home-relative directory into a $HOME-prefixed
// path. The literal "$HOME" is preserved so the remote login shell
// performs the expansion.
func pathJoinHome(relative string) string {
	relative = strings.TrimPrefix(relative, "/")
	return path.Join("$HOME", relative)
}

// defaultResolveRemoteHome is the production resolver wired into
// NewFleetDeployService. It is the package-level seam tests replace
// (by overriding fleetDeployService.resolveHome) when the fake session
// has no underlying SSH client to dispatch the printf $HOME exec.
func defaultResolveRemoteHome(ctx context.Context, sess sshSession) (string, error) {
	c := underlyingClient(sess)
	if c == nil {
		return "", &deploy.InvalidSpecError{Field: "ssh_session", Reason: "no underlying client"}
	}
	stdout, _, err := c.Exec(ctx, `printf '%s' "$HOME"`)
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(stdout)
	if home == "" {
		return "", &deploy.InvalidSpecError{Field: "remote_home", Reason: "remote $HOME is empty"}
	}
	return home, nil
}

// hostnameForCert extracts the hostname portion of an SSH target. The
// target may be "host", "host:port", "user@host", or "user@host:port";
// we keep only the host segment since the cert's SANs should match the
// DNS name or IP the daemon will advertise.
//
// IPv6 literals are preserved whole and unbracketed: "[2001:db8::1]:22"
// and "2001:db8::1" both yield "2001:db8::1", so the SAN matches the
// address and net.JoinHostPort can re-bracket it for a dial. Splitting
// at the last colon would truncate the bare form and keep the brackets
// on the other.
func hostnameForCert(target string) string {
	return deploy.Hostname(target)
}

// formatSerial renders the certificate serial number for inclusion in
// the deploy result. Returns an empty string when the parsed cert is
// unavailable so callers render it as absent rather than "0".
func formatSerial(issued *IssuedFleetNodeCertificate) string {
	if issued == nil || issued.ParsedCert == nil || issued.ParsedCert.SerialNumber == nil {
		return ""
	}
	return hex.EncodeToString(issued.ParsedCert.SerialNumber.Bytes())
}

// ctxSleep is a time.Sleep substitute that honours context cancellation.
func ctxSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// =============================================================================
// Typed errors
// =============================================================================

// FleetNodeIdentityUploadError wraps an SFTP failure while writing one of
// the daemon's mTLS identity files (fleet node.crt, fleet node.key, server-ca.crt).
type FleetNodeIdentityUploadError struct {
	// File is the basename of the identity artifact that failed upload.
	File string

	// Cause is the underlying transport error.
	Cause error
}

// Error implements the error interface.
func (e *FleetNodeIdentityUploadError) Error() string {
	return fmt.Sprintf("agent deploy: upload %s: %v", e.File, e.Cause)
}

// Unwrap returns the wrapped cause.
func (e *FleetNodeIdentityUploadError) Unwrap() error { return e.Cause }

// FleetNodeConfigUploadError wraps a failure while writing the rendered daemon
// config file onto the node.
type FleetNodeConfigUploadError struct {
	// Path is the remote path of the config file.
	Path string

	// Cause is the underlying transport error.
	Cause error
}

// Error implements the error interface.
func (e *FleetNodeConfigUploadError) Error() string {
	return "agent deploy: upload " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *FleetNodeConfigUploadError) Unwrap() error { return e.Cause }

// =============================================================================
// Production Adapters
// =============================================================================

// realSSHDialer wraps deploy.DialSSH.
type realSSHDialer struct{}

func (realSSHDialer) Dial(ctx context.Context, spec *deploy.Spec, hostKey *deploy.HostKeyPolicy) (sshSession, error) {
	c, err := deploy.DialSSHWithHostKey(ctx, spec, hostKey)
	if err != nil {
		return nil, err
	}
	return &sshSessionAdapter{c: c}, nil
}

// DialWithAuth dials using an explicit set of ssh.AuthMethods, letting
// FleetDeployService inject an SSHSignerResolver-backed signer and a
// known-hosts-backed host-key policy without touching the spec.
func (realSSHDialer) DialWithAuth(ctx context.Context, spec *deploy.Spec, auth []ssh.AuthMethod, hostKey *deploy.HostKeyPolicy) (sshSession, error) {
	c, err := deploy.DialSSHWithAuthAndHostKey(ctx, spec, auth, hostKey)
	if err != nil {
		return nil, err
	}
	return &sshSessionAdapter{c: c}, nil
}

// sshSessionAdapter carries the install target (an ssh connection or this
// host) through the service without leaking the type across the
// service-local interfaces.
type sshSessionAdapter struct {
	c deploy.Target
}

// Close tears down the SSH connection; a local target holds nothing open.
func (a *sshSessionAdapter) Close() error {
	if closer, ok := a.c.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// underlyingClient extracts the install target for adapters that need to
// call the primitive API directly.
func underlyingClient(sess sshSession) deploy.Target {
	if a, ok := sess.(*sshSessionAdapter); ok {
		return a.c
	}
	return nil
}

// realUploader wraps deploy.UploadBinary.
type realUploader struct{}

func (realUploader) Upload(ctx context.Context, sess sshSession, localPath, remotePath string) error {
	c := underlyingClient(sess)
	if c == nil {
		return &deploy.SFTPUploadError{
			LocalPath:  localPath,
			RemotePath: remotePath,
			Cause:      &deploy.InvalidSpecError{Field: "ssh_session", Reason: "no underlying client"},
		}
	}
	return deploy.UploadBinary(ctx, c, localPath, remotePath)
}

// realFileUploader writes arbitrary byte payloads over the shared
// *deploy.SSHClient. It delegates to SSHClient.WriteFile, which does
// mkdir -p + chmod on the destination directory.
type realFileUploader struct{}

// WriteRemoteFile writes content to remotePath with mode.
func (realFileUploader) WriteRemoteFile(ctx context.Context, sess sshSession, remotePath string, content []byte, mode uint32) error {
	c := underlyingClient(sess)
	if c == nil {
		return &deploy.SFTPUploadError{
			RemotePath: remotePath,
			Cause:      &deploy.InvalidSpecError{Field: "ssh_session", Reason: "no underlying client"},
		}
	}
	return c.WriteFile(ctx, remotePath, content, os.FileMode(mode))
}

// realSystemdInstaller wraps deploy.DetectSystemdUser + InstallAndStart.
type realSystemdInstaller struct{}

func (realSystemdInstaller) Detect(ctx context.Context, sess sshSession) bool {
	c := underlyingClient(sess)
	if c == nil {
		return false
	}
	return deploy.DetectSystemdUser(ctx, c)
}

func (realSystemdInstaller) InstallAndStart(ctx context.Context, sess sshSession, unitContent, unitName string) error {
	c := underlyingClient(sess)
	if c == nil {
		return &deploy.SystemdInstallError{
			Unit:  unitName,
			Stage: "ssh",
			Cause: &deploy.InvalidSpecError{Field: "ssh_session", Reason: "no underlying client"},
		}
	}
	return deploy.InstallAndStart(ctx, c, unitContent, unitName)
}

// realNohupStarter wraps deploy.StartBackground.
type realNohupStarter struct{}

func (realNohupStarter) Start(ctx context.Context, sess sshSession, scriptContent, scriptPath string) error {
	c := underlyingClient(sess)
	if c == nil {
		return &deploy.RemoteExecError{
			Command: "start " + scriptPath,
			Cause:   &deploy.InvalidSpecError{Field: "ssh_session", Reason: "no underlying client"},
		}
	}
	return deploy.StartBackground(ctx, c, scriptContent, scriptPath)
}

// realRenderer wraps the template helpers in pkg/fleet/deploy.
type realRenderer struct{}

func (realRenderer) RenderUnit(spec *deploy.Spec) string { return deploy.RenderUnit(spec) }
func (realRenderer) RenderStartScript(spec *deploy.Spec) string {
	return deploy.RenderStartScript(spec)
}
