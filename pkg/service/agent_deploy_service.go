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
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// Agent Deploy Service
// =============================================================================
//
// AgentDeployService is the single entry point for provisioning a
// remote QuicBaaS agent daemon. It orchestrates four primitive steps:
//
//   1. Mint an mTLS client certificate for the daemon via the
//      server-embedded CA. The daemon presents this certificate on
//      POST /api/v1/agents/register to bootstrap OAuth2 credentials.
//   2. Dial the target host over SSH and upload the static CLI binary
//      via SFTP. The mTLS certificate, private key, and server CA
//      bundle are uploaded alongside the binary.
//   3. Install the daemon as either a systemd --user unit or a nohup
//      fallback script, depending on the spec and what the remote host
//      supports.
//   4. Optionally poll the control plane's fleet registry via
//      the SDK until the freshly-deployed agent reports online.
//
// The service depends on narrow interfaces so tests can replace every
// primitive with fakes. The "real" adapters delegate to pkg/agent/deploy.

// AgentDeployService installs a QuicBaaS agent daemon on a remote host.
type AgentDeployService interface {
	// Deploy runs the full deployment workflow described by spec and
	// returns the resulting *deploy.Result on success. All errors are
	// typed values from pkg/agent/deploy so callers can distinguish
	// failure modes without parsing strings.
	Deploy(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error)
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

// sshDialer abstracts the SSH dial step.
type sshDialer interface {
	// Dial opens an SSH connection using credentials read from the spec
	// (ssh_key_path / ssh_password / ssh_host_key fields).
	Dial(ctx context.Context, spec *deploy.Spec) (sshSession, error)

	// DialWithAuth opens an SSH connection using an explicit set of
	// ssh.AuthMethod values. AgentDeployService uses this to inject an
	// SSHSignerResolver-backed ssh.Signer when spec.SSHKeyName is set.
	DialWithAuth(ctx context.Context, spec *deploy.Spec, auth []ssh.AuthMethod) (sshSession, error)
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

// AgentLister abstracts the SDK fleet registry poll. It is the
// dependency the service needs to confirm a deployed agent has come
// online. Callers wire their SDK orchestrator client into a concrete
// implementation of this interface.
type AgentLister interface {
	ListAgents(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error)
}

// =============================================================================
// Service Implementation
// =============================================================================

// agentDeployService is the default, production AgentDeployService.
type agentDeployService struct {
	ssh        sshDialer
	upload     binaryUploader
	files      fileUploader
	systemd    systemdInstaller
	nohup      nohupStarter
	renderer   unitRenderer
	agents     AgentLister
	sshKeys    SSHSignerResolver
	certIssuer AgentCertIssuer
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration) error

	// resolveHome resolves the remote login user's $HOME on a connected
	// session. Production wires the real SSH-exec implementation; tests
	// substitute a deterministic value so the fake session need not
	// embed an *deploy.SSHClient.
	resolveHome func(ctx context.Context, sess sshSession) (string, error)
}

// AgentDeployServiceDeps wires injectable dependencies for the
// production-configured service. Tests build an agentDeployService
// directly; callers use NewAgentDeployService which fills defaults.
type AgentDeployServiceDeps struct {
	// AgentLister is the SDK-backed orchestrator client used to poll
	// the fleet registry when Spec.WaitOnline > 0. Optional: when nil
	// the service skips the wait-online phase even if a budget is set.
	AgentLister AgentLister

	// SSHKeys resolves a Spec.SSHKeyName into an ssh.Signer backed by
	// an SSH key-management implementation (typically xkms). Optional:
	// when nil the service falls back to spec-embedded credentials
	// (SSHPassword), matching the legacy password-only workflow.
	SSHKeys SSHSignerResolver

	// CertIssuer produces the mTLS client identity the daemon presents
	// on POST /api/v1/agents/register. Required for production deploys;
	// when nil the service returns a typed error before dialing SSH so
	// misconfigured callers fail fast.
	CertIssuer AgentCertIssuer
}

// NewAgentDeployService wires the default SSH/SFTP/systemd primitives
// from pkg/agent/deploy plus the caller-provided SDK agent lister.
func NewAgentDeployService(deps AgentDeployServiceDeps) AgentDeployService {
	return &agentDeployService{
		ssh:         realSSHDialer{},
		upload:      realUploader{},
		files:       realFileUploader{},
		systemd:     realSystemdInstaller{},
		nohup:       realNohupStarter{},
		renderer:    realRenderer{},
		agents:      deps.AgentLister,
		sshKeys:     deps.SSHKeys,
		certIssuer:  deps.CertIssuer,
		now:         time.Now,
		sleep:       ctxSleep,
		resolveHome: defaultResolveRemoteHome,
	}
}

// Deploy implements AgentDeployService.Deploy.
//
// Steps execute linearly: each stage produces inputs for the next, and
// any failure aborts the workflow with a typed error. The SSH session
// is always closed on exit.
func (s *agentDeployService) Deploy(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
	if err := s.validate(spec); err != nil {
		return nil, err
	}

	// Mint the agent's mTLS identity up-front, before we dial SSH. If
	// the CA rejects the request we surface the error immediately and
	// never touch the remote host.
	issued, err := s.issueAgentCert(ctx, spec)
	if err != nil {
		return nil, err
	}
	// The buffer must be destroyed the moment we're done reading its
	// bytes, even on error paths below. Declare the cleanup here so it
	// runs via defer regardless of how the function exits.
	defer issued.PrivateKey.Destroy()

	sess, err := s.dial(ctx, spec)
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	// Resolve the remote user's $HOME once so downstream paths can be
	// absolute. SFTP doesn't expand shell variables, and systemd's %h
	// placeholder only applies when unit processing is active — using
	// an absolute path keeps every code path consistent.
	resolve := s.resolveHome
	if resolve == nil {
		resolve = defaultResolveRemoteHome
	}
	remoteHome, err := resolve(ctx, sess)
	if err != nil {
		return nil, err
	}

	installDir := pathJoinRemoteHome(remoteHome, spec.ResolvedInstallDir())
	dataDir := pathJoinRemoteHome(remoteHome, spec.ResolvedDataDir())
	remoteBin := path.Join(installDir, "quicbaas")
	if err := s.upload.Upload(ctx, sess, spec.Binary, remoteBin); err != nil {
		return nil, err
	}

	if err := s.uploadAgentIdentity(ctx, sess, dataDir, issued); err != nil {
		return nil, err
	}

	serviceStatus, err := s.startDaemon(ctx, sess, spec, installDir)
	if err != nil {
		return nil, err
	}

	result := &deploy.Result{
		Host:            spec.Host,
		InstalledPath:   remoteBin,
		ServiceStatus:   serviceStatus,
		AgentCertPath:   path.Join(dataDir, deploy.AgentCertFilename),
		AgentCertSerial: formatSerial(issued),
	}

	if spec.WaitOnline > 0 && s.agents != nil {
		expectedID := expectedAgentID(spec, issued)
		id, onlineAt, err := s.waitOnline(ctx, spec, expectedID)
		if err != nil {
			return result, err
		}
		result.AgentID = id
		result.OnlineAt = onlineAt
	}

	return result, nil
}

// validate returns an InvalidSpecError for the first missing required
// field. The primitives perform their own deeper validation on fields
// they consume directly.
func (s *agentDeployService) validate(spec *deploy.Spec) error {
	if spec == nil {
		return &deploy.InvalidSpecError{Field: "spec", Reason: "spec is required"}
	}
	if spec.Host == "" {
		return &deploy.InvalidSpecError{Field: "host", Reason: "host is required"}
	}
	if spec.SSHUser == "" {
		return &deploy.InvalidSpecError{Field: "ssh_user", Reason: "ssh user is required"}
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
	if spec.AgentBind == "" {
		return &deploy.InvalidSpecError{Field: "agent_bind", Reason: "agent bind address is required"}
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

// issueAgentCert requests a fresh mTLS identity from the configured CA.
// The caller owns the returned *secure.Buffer and MUST Destroy() it on
// every exit path. Deploy() does this via defer.
func (s *agentDeployService) issueAgentCert(ctx context.Context, spec *deploy.Spec) (*IssuedAgentCertificate, error) {
	req := &AgentCertificateRequest{
		TenantID: spec.Tenant,
		Hostname: hostnameForCert(spec.Host),
	}
	return s.certIssuer.IssueAgentCertificate(ctx, req)
}

// uploadAgentIdentity writes the daemon's mTLS materials into DataDir
// on the remote host with appropriate permissions:
//
//	agent.crt     0644 - public certificate, safe for world-readable
//	agent.key     0600 - private key, owner-only
//	server-ca.crt 0644 - trust anchor, safe for world-readable
//
// The caller retains ownership of issued.PrivateKey; this function
// only reads its bytes. Destruction happens in Deploy via defer.
func (s *agentDeployService) uploadAgentIdentity(
	ctx context.Context,
	sess sshSession,
	dataDir string,
	issued *IssuedAgentCertificate,
) error {
	if err := s.files.WriteRemoteFile(ctx, sess, path.Join(dataDir, deploy.AgentCertFilename), issued.CertPEM, 0o644); err != nil {
		return &AgentIdentityUploadError{File: deploy.AgentCertFilename, Cause: err}
	}

	keyBytes := issued.PrivateKey.Bytes()
	if err := s.files.WriteRemoteFile(ctx, sess, path.Join(dataDir, deploy.AgentKeyFilename), keyBytes, 0o600); err != nil {
		return &AgentIdentityUploadError{File: deploy.AgentKeyFilename, Cause: err}
	}

	if err := s.files.WriteRemoteFile(ctx, sess, path.Join(dataDir, deploy.ServerCAFilename), issued.CAPEM, 0o644); err != nil {
		return &AgentIdentityUploadError{File: deploy.ServerCAFilename, Cause: err}
	}
	return nil
}

// startDaemon installs and starts the daemon. It prefers systemd when
// the spec requests it (or auto-detects support), falling back to the
// nohup script otherwise. The returned string labels the chosen path
// for the Result.
func (s *agentDeployService) startDaemon(
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
	scriptPath := path.Join(installDir, "start-quicbaas-agent.sh")
	if err := s.nohup.Start(ctx, sess, script, scriptPath); err != nil {
		return "", err
	}
	return "nohup", nil
}

// waitOnline polls the orchestrator's fleet registry until the
// freshly-deployed agent reports a live status, or the WaitOnline
// budget elapses.
//
// The poll is scoped to the tenant only: filtering by Host on the
// server side would re-introduce the very mismatch this code path
// avoids, since the daemon registers under its resolved hostname while
// the deploy spec carries only the SSH-target / AgentBind. Selection is
// performed client-side by pickOnlineAgent, preferring an exact match
// against the AgentID the server will assign (computed from the mTLS
// certificate CN and the tenant) and falling back to a host-suffix
// heuristic for callers that do not pass an expected ID.
func (s *agentDeployService) waitOnline(ctx context.Context, spec *deploy.Spec, expectedID string) (string, string, error) {
	deadline := s.now().Add(spec.WaitOnline)
	const pollEvery = 2 * time.Second

	for {
		resp, err := s.agents.ListAgents(ctx, &orchestratorSDK.ListAgentsRequest{
			TenantID: spec.Tenant,
		})
		if err == nil {
			if id, at := pickOnlineAgent(resp, spec, expectedID); id != "" {
				return id, at, nil
			}
		}

		if !s.now().Before(deadline) {
			return "", "", &deploy.StartTimeoutError{
				Host:    spec.Host,
				Tenant:  spec.Tenant,
				Elapsed: spec.WaitOnline.String(),
			}
		}

		if err := s.sleep(ctx, pollEvery); err != nil {
			return "", "", err
		}
	}
}

// dial selects the SSH entry point. When spec.SSHKeyName is set it
// resolves the stored key via SSHSignerResolver and dials with an
// injected ssh.Signer so the private material never leaves its key
// store. Otherwise it delegates to the dialer's default credential
// path.
func (s *agentDeployService) dial(ctx context.Context, spec *deploy.Spec) (sshSession, error) {
	if spec.SSHKeyName == "" {
		return s.ssh.Dial(ctx, spec)
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
	return s.ssh.DialWithAuth(ctx, spec, []ssh.AuthMethod{ssh.PublicKeys(sshSigner)})
}

// pickOnlineAgent inspects a ListAgentsResponse for an agent that
// reports an online status and matches the deployment target.
//
// Selection precedence:
//
//  1. expectedID match — when the caller pre-computes the canonical
//     AgentID the server assigns (via iam.AgentAppName) we match by
//     that ID exactly and ignore Host. This is the deterministic path
//     used by the agent deploy service after it issues the daemon's
//     mTLS cert.
//
//  2. Host heuristic — when expectedID is empty (e.g. callers that
//     don't have a cert in hand) we fall back to the legacy match: an
//     online agent whose registered Host equals or ends with
//     spec.AgentBind. This preserves the historical contract for any
//     existing caller that constructs ListAgentsResponse manually.
//
// In both cases ties are broken by the most recent LastSeen timestamp
// so the freshest live registration wins.
func pickOnlineAgent(resp *orchestratorSDK.ListAgentsResponse, spec *deploy.Spec, expectedID string) (string, string) {
	if resp == nil || len(resp.Agents) == 0 {
		return "", ""
	}
	var best *orchestratorSDK.AgentRegistration
	for _, a := range resp.Agents {
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
		} else if spec.AgentBind != "" && !strings.HasSuffix(a.Host, spec.AgentBind) && a.Host != spec.AgentBind {
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

// expectedAgentID returns the canonical AgentID the server will assign
// when the daemon presents its issued mTLS certificate at
// /api/v1/agents/register. The value is derived from the certificate's
// Common Name plus the tenant via iam.AgentAppName so client and server
// agree on the identifier without an extra round-trip.
//
// Returns the empty string when no usable CN is present, in which case
// pickOnlineAgent falls back to its host-suffix heuristic.
func expectedAgentID(spec *deploy.Spec, issued *IssuedAgentCertificate) string {
	if spec == nil || issued == nil || issued.ParsedCert == nil {
		return ""
	}
	cn := strings.TrimSpace(issued.ParsedCert.Subject.CommonName)
	if cn == "" {
		return ""
	}
	return iam.AgentAppName(spec.Tenant, cn)
}

// isOnlineStatus reports whether the agent's status string means "live".
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

// pathJoinRemoteHome joins a home-relative directory under an
// already-resolved absolute remote home directory. Produces an
// absolute path safe for SFTP (which does not expand shell variables).
func pathJoinRemoteHome(remoteHome, relative string) string {
	relative = strings.TrimPrefix(relative, "/")
	if remoteHome == "" {
		return path.Join("$HOME", relative)
	}
	return path.Join(remoteHome, relative)
}

// defaultResolveRemoteHome is the production resolver wired into
// NewAgentDeployService. It is the package-level seam tests replace
// (by overriding agentDeployService.resolveHome) when the fake session
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

// resolveRemoteHome retains the old method-based API for any in-tree
// caller that already binds to it. It delegates to the package-level
// helper so behaviour is identical.
func (s *agentDeployService) resolveRemoteHome(ctx context.Context, sess sshSession) (string, error) {
	return defaultResolveRemoteHome(ctx, sess)
}

// hostnameForCert extracts the hostname portion of an SSH target. The
// target may be "host", "host:port", "user@host", or "user@host:port";
// we keep only the host segment since the cert's SANs should match the
// DNS name or IP the daemon will advertise.
func hostnameForCert(target string) string {
	trimmed := strings.TrimSpace(target)
	if at := strings.IndexByte(trimmed, '@'); at >= 0 {
		trimmed = trimmed[at+1:]
	}
	if colon := strings.LastIndexByte(trimmed, ':'); colon > 0 {
		trimmed = trimmed[:colon]
	}
	return trimmed
}

// formatSerial renders the certificate serial number for inclusion in
// the deploy result. Returns an empty string when the parsed cert is
// unavailable so callers render it as absent rather than "0".
func formatSerial(issued *IssuedAgentCertificate) string {
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

// AgentIdentityUploadError wraps an SFTP failure while writing one of
// the daemon's mTLS identity files (agent.crt, agent.key, server-ca.crt).
type AgentIdentityUploadError struct {
	// File is the basename of the identity artifact that failed upload.
	File string

	// Cause is the underlying transport error.
	Cause error
}

// Error implements the error interface.
func (e *AgentIdentityUploadError) Error() string {
	return fmt.Sprintf("agent deploy: upload %s: %v", e.File, e.Cause)
}

// Unwrap returns the wrapped cause.
func (e *AgentIdentityUploadError) Unwrap() error { return e.Cause }

// =============================================================================
// Production Adapters
// =============================================================================

// realSSHDialer wraps deploy.DialSSH.
type realSSHDialer struct{}

func (realSSHDialer) Dial(ctx context.Context, spec *deploy.Spec) (sshSession, error) {
	c, err := deploy.DialSSH(ctx, spec)
	if err != nil {
		return nil, err
	}
	return &sshSessionAdapter{c: c}, nil
}

// DialWithAuth dials using an explicit set of ssh.AuthMethods, letting
// AgentDeployService inject an SSHSignerResolver-backed signer without
// touching the spec.
func (realSSHDialer) DialWithAuth(ctx context.Context, spec *deploy.Spec, auth []ssh.AuthMethod) (sshSession, error) {
	c, err := deploy.DialSSHWithAuth(ctx, spec, auth)
	if err != nil {
		return nil, err
	}
	return &sshSessionAdapter{c: c}, nil
}

// sshSessionAdapter carries the concrete *deploy.SSHClient through the
// service without leaking the type across the service-local interfaces.
type sshSessionAdapter struct {
	c *deploy.SSHClient
}

// Close tears down the SSH connection.
func (a *sshSessionAdapter) Close() error { return a.c.Close() }

// underlyingClient extracts the inner *deploy.SSHClient for adapters
// that need to call the primitive API directly.
func underlyingClient(sess sshSession) *deploy.SSHClient {
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

// realRenderer wraps the template helpers in pkg/agent/deploy.
type realRenderer struct{}

func (realRenderer) RenderUnit(spec *deploy.Spec) string { return deploy.RenderUnit(spec) }
func (realRenderer) RenderStartScript(spec *deploy.Spec) string {
	return deploy.RenderStartScript(spec)
}
