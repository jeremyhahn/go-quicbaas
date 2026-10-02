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

package service

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// eventLog records the order node operations and registry calls happen in,
// across the fakes that share it.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.events, ",")
}

// fakeNode stands in for the node-side primitives.
type fakeNode struct {
	log *eventLog

	checks      []deploy.PreflightCheck
	preflightIn *deploy.PreflightInput

	cert         *x509.Certificate
	certErr      error
	supervisor   deploy.Supervisor
	stopErr      error
	startErr     error
	uninstallErr error
	purged       bool
	layout       deploy.Layout
}

func (n *fakeNode) record(event string) {
	if n.log != nil {
		n.log.add(event)
	}
}

func (n *fakeNode) Preflight(_ context.Context, _ sshSession, in deploy.PreflightInput) []deploy.PreflightCheck {
	n.record("preflight")
	n.preflightIn = &in
	if n.checks == nil {
		return []deploy.PreflightCheck{deploy.PassedCheck(deploy.CheckControlPlane, "ok")}
	}
	return n.checks
}

func (n *fakeNode) Stop(_ context.Context, _ sshSession, l deploy.Layout) (deploy.Supervisor, error) {
	n.record("stop")
	n.layout = l
	return n.supervisor, n.stopErr
}

func (n *fakeNode) Start(_ context.Context, _ sshSession, l deploy.Layout) (deploy.Supervisor, error) {
	n.record("start")
	n.layout = l
	return n.supervisor, n.startErr
}

func (n *fakeNode) Uninstall(_ context.Context, _ sshSession, l deploy.Layout, purge bool) (*deploy.UninstallResult, error) {
	n.record("uninstall")
	n.layout = l
	n.purged = purge
	return &deploy.UninstallResult{Supervisor: n.supervisor, Removed: []string{l.Binary}}, n.uninstallErr
}

func (n *fakeNode) ReadCertificate(_ context.Context, _ sshSession, l deploy.Layout) (*x509.Certificate, error) {
	n.record("read-cert")
	if n.certErr != nil {
		return nil, n.certErr
	}
	return n.cert, nil
}

// fakeRemover records fleet registry removals.
type fakeRemover struct {
	log  *eventLog
	err  error
	seen *orchestratorSDK.RemoveFleetNodeRequest
}

func (r *fakeRemover) RemoveFleetNode(_ context.Context, req *orchestratorSDK.RemoveFleetNodeRequest) (*orchestratorSDK.RemoveFleetNodeResponse, error) {
	r.log.add("deregister")
	r.seen = req
	if r.err != nil {
		return nil, r.err
	}
	return &orchestratorSDK.RemoveFleetNodeResponse{NodeID: req.NodeID, Removed: true}, nil
}

const lifecycleCN = "agent-remote.example.com"

// lifecycleService is a service wired for lifecycle tests, with the node and
// the registry sharing one event log.
func lifecycleService(t *testing.T) (*fleetDeployService, *fakeNode, *fakeRemover, *eventLog) {
	t.Helper()
	log := &eventLog{}
	node := &fakeNode{
		log:        log,
		cert:       &x509.Certificate{Subject: pkix.Name{CommonName: lifecycleCN}},
		supervisor: deploy.SupervisorSystemd,
	}
	remover := &fakeRemover{log: log}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Unix(0, 0))
	svc.node = node
	svc.remover = remover
	return svc, node, remover, log
}

// -----------------------------------------------------------------------------
// Deploy × preflight
// -----------------------------------------------------------------------------

// TestDeploy_PreflightFailureChangesNothing: a node that fails preflight is
// left exactly as it was — no certificate minted, nothing uploaded.
func TestDeploy_PreflightFailureChangesNothing(t *testing.T) {
	up := &fakeUploader{}
	svc := newTestService(&fakeDialer{}, up, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	issuer := &fakeCertIssuer{}
	svc.certIssuer = issuer
	svc.node = &fakeNode{checks: []deploy.PreflightCheck{
		deploy.FailedCheck(deploy.CheckLinger, &deploy.LingerNotEnabledError{User: "deploy", Command: "sudo loginctl enable-linger deploy"}),
	}}

	_, err := svc.Deploy(acmeCtx, validSpec())
	var failed *deploy.PreflightFailedError
	var linger *deploy.LingerNotEnabledError
	if !errors.As(err, &failed) || !errors.As(err, &linger) {
		t.Fatalf("want a PreflightFailedError exposing the linger failure, got %v", err)
	}
	if issuer.calls.Load() != 0 || up.calls != 0 {
		t.Fatalf("a failed preflight still issued (%d) or uploaded (%d)", issuer.calls.Load(), up.calls)
	}
}

func TestDeploy_RunsPreflightUnlessSkipped(t *testing.T) {
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{detect: true}, &fakeNohup{}, nil, time.Now())
	node := &fakeNode{}
	svc.node = node

	result, err := svc.Deploy(acmeCtx, validSpec())
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if node.preflightIn == nil || result.Preflight == nil || !result.Preflight.Passed() {
		t.Fatalf("deploy did not run preflight first: %+v", result.Preflight)
	}

	node.preflightIn = nil
	spec := validSpec()
	spec.SkipPreflight = true
	result, err = svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if node.preflightIn != nil || result.Preflight != nil {
		t.Fatal("--skip-preflight still ran preflight")
	}
}

// TestDeploy_UploadsRenderedConfig: the daemon's whole configuration — its
// capacity included — reaches the node in the config file the unit runs.
func TestDeploy_UploadsRenderedConfig(t *testing.T) {
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{detect: true}, &fakeNohup{}, nil, time.Now())
	files := newFakeFileUploader()
	svc.files = files
	spec := validSpec()
	spec.SkipPreflight = true
	spec.Node.AllowedMountRoots = []string{"/srv/sockets"}

	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	const configPath = "/home/deploy/.quicbaas/bin/agent.yaml"
	if result.ConfigPath != configPath {
		t.Fatalf("ConfigPath = %q", result.ConfigPath)
	}
	file, ok := files.get(configPath)
	if !ok || file.mode != 0o600 {
		t.Fatalf("config not uploaded owner-only: %+v %v", file, ok)
	}
	for _, want := range []string{
		"max_concurrent_jobs: 2",
		"server_addr: https://c:8443",
		"node_cert_file: /home/deploy/.quicbaas/agent/agent.crt",
		"- /srv/sockets",
		"sandbox_policy: required",
	} {
		if !strings.Contains(string(file.content), want) {
			t.Fatalf("config missing %q:\n%s", want, file.content)
		}
	}

	svc.files = &fakeFileUploader{files: map[string]fakeFile{}, err: errors.New("disk full")}
	var identityErr *FleetNodeIdentityUploadError
	if _, err := svc.Deploy(acmeCtx, spec); !errors.As(err, &identityErr) {
		t.Fatalf("want the first upload failure typed, got %v", err)
	}
}

// TestDeploy_RefusesUndeclaredCapacity is why a deployed host can no longer
// report its capacity as undeclared.
func TestDeploy_RefusesUndeclaredCapacity(t *testing.T) {
	dialer := &fakeDialer{}
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	spec := validSpec()
	spec.Node.MaxConcurrentJobs = 0

	var undeclared *deploy.CapacityUndeclaredError
	if _, err := svc.Deploy(acmeCtx, spec); !errors.As(err, &undeclared) {
		t.Fatalf("want CapacityUndeclaredError, got %v", err)
	}
	if _, err := svc.Preflight(context.Background(), spec); !errors.As(err, &undeclared) {
		t.Fatalf("preflight: want CapacityUndeclaredError, got %v", err)
	}
	if dialer.seen != nil {
		t.Fatal("an invalid spec reached the node")
	}
}

// -----------------------------------------------------------------------------
// Preflight
// -----------------------------------------------------------------------------

func TestPreflight_ReportsDialChecksAndNodeChecks(t *testing.T) {
	svc, node, _, _ := lifecycleService(t)
	spec := validSpec()
	spec.ProjectsRoot = "/srv/projects"

	report, err := svc.Preflight(context.Background(), spec)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	names := make([]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		names = append(names, check.Name)
	}
	want := []string{deploy.CheckSSH, deploy.CheckHostKey, deploy.CheckDeployKey, deploy.CheckControlPlane}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("checks = %v, want %v", names, want)
	}
	if !strings.Contains(report.Checks[1].Detail, "NOT verified") {
		t.Fatalf("an insecure host key must be reported as unverified: %q", report.Checks[1].Detail)
	}
	if node.preflightIn.Layout.ProjectsRoot != "/srv/projects" || node.preflightIn.Layout.Home != "/home/deploy" {
		t.Fatalf("layout not resolved against the remote home: %+v", node.preflightIn.Layout)
	}
	if !strings.Contains(string(node.preflightIn.ServerCAPEM), "ca") {
		t.Fatal("preflight did not receive the deploy's CA bundle")
	}
}

func TestPreflight_CABundleUnavailable(t *testing.T) {
	svc, _, _, _ := lifecycleService(t)
	svc.certIssuer = &capturingCertIssuer{}

	report, err := svc.Preflight(context.Background(), validSpec())
	var unreachable *deploy.ControlPlaneUnreachableError
	var unavailable *FleetCABundleUnavailableError
	if !errors.As(err, &unreachable) || unreachable.Stage != "ca" || !errors.As(err, &unavailable) {
		t.Fatalf("want a ca-stage control-plane failure naming the issuer, got %v", err)
	}
	if report.Passed() {
		t.Fatal("report passed without a CA")
	}
}

// TestPreflight_DialFailuresNameTheirCheck: the report says which of the
// SSH-level checks failed and carries its fix, instead of "ssh failed".
func TestPreflight_DialFailuresNameTheirCheck(t *testing.T) {
	authErr := &deploy.SSHDialError{Host: "remote.example.com:22", User: "deploy",
		Cause: errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey]")}
	cases := []struct {
		name    string
		err     error
		failed  string
		fixHint string
	}{
		{"unreachable", &deploy.SSHDialError{Host: "remote.example.com:22", Cause: errors.New("connection refused")}, deploy.CheckSSH, "sshd"},
		{"host key rotated", &deploy.SSHDialError{Cause: &SSHHostKeyMismatchError{Host: "h", Algorithm: "ssh-ed25519", Presented: "SHA256:new"}}, deploy.CheckHostKey, "known-hosts add"},
		{"wrong algorithm", &deploy.SSHDialError{Cause: &SSHHostKeyAlgorithmMismatchError{Host: "h", Presented: "ecdsa-sha2-nistp256", Offered: "SHA256:x", Pinned: []string{"ssh-ed25519"}}}, deploy.CheckHostKey, "--algorithm ecdsa-sha2-nistp256"},
		{"deploy key not authorized", authErr, deploy.CheckDeployKey, "authorized_keys: ssh-ed25519 "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, _ := lifecycleService(t)
			svc.ssh = &fakeDialer{err: tc.err}
			svc.sshKeys = &fakeSignerResolver{signer: ed25519TestSigner(t)}
			spec := validSpec()
			spec.SSHPassword = ""
			spec.SSHKeyName = "fleet-deploy"

			report, err := svc.Preflight(context.Background(), spec)
			if err == nil {
				t.Fatal("a failed dial passed preflight")
			}
			last := report.Checks[len(report.Checks)-1]
			if last.Name != tc.failed || last.Passed {
				t.Fatalf("failed check = %+v, want %s", last, tc.failed)
			}
			if !strings.Contains(last.Fix, tc.fixHint) {
				t.Fatalf("fix %q does not contain %q", last.Fix, tc.fixHint)
			}
		})
	}
}

func TestPreflight_UnpinnedHostFailsBeforeDialing(t *testing.T) {
	svc, _, _, _ := lifecycleService(t)
	dialer := &fakeDialer{}
	svc.ssh = dialer
	svc.hostKeys = &fakeHostKeyResolver{err: &SSHHostKeyNotPinnedError{Host: "remote.example.com", Scope: "user", Owner: "op"}}
	spec := validSpec()
	spec.InsecureHostKey = false

	report, err := svc.Preflight(context.Background(), spec)
	var notPinned *SSHHostKeyNotPinnedError
	if !errors.As(err, &notPinned) || len(report.Checks) != 1 || report.Checks[0].Name != deploy.CheckHostKey {
		t.Fatalf("want a single host-key failure, got %v / %+v", err, report.Checks)
	}
	if !strings.Contains(report.Checks[0].Fix, "known-hosts add --host remote.example.com") {
		t.Fatalf("fix = %q", report.Checks[0].Fix)
	}
	if dialer.seen != nil {
		t.Fatal("dialed an unpinned host")
	}
}

func TestPreflight_HomeResolutionFails(t *testing.T) {
	svc, _, _, _ := lifecycleService(t)
	svc.resolveHome = func(context.Context, sshSession) (string, error) { return "", errors.New("no home") }
	report, err := svc.Preflight(context.Background(), validSpec())
	if err == nil || report.Checks[len(report.Checks)-1].Name != deploy.CheckRemoteUser {
		t.Fatalf("want a remote-user failure, got %v / %+v", err, report.Checks)
	}
}

// -----------------------------------------------------------------------------
// Stop / Start / Undeploy
// -----------------------------------------------------------------------------

// TestStop_StopsThenDeregisters is the rule that keeps a stopped node from
// reading "running": the agent is deregistered, and only after it has
// stopped, so its last heartbeat cannot re-register it.
func TestStop_StopsThenDeregisters(t *testing.T) {
	svc, _, remover, log := lifecycleService(t)

	result, err := svc.Stop(context.Background(), validSpec())
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := log.String(); got != "read-cert,stop,deregister" {
		t.Fatalf("order = %s", got)
	}
	wantID := iam.FleetNodeAppName("acme", lifecycleCN)
	if result.NodeID != wantID || remover.seen.NodeID != wantID || remover.seen.TenantID != "acme" {
		t.Fatalf("deregistered %+v, want %s", remover.seen, wantID)
	}
	if !result.Deregistered || result.Supervisor != deploy.SupervisorSystemd || result.Action != "stop" {
		t.Fatalf("result = %+v", result)
	}
}

func TestStop_AlreadyDeregisteredIsSuccess(t *testing.T) {
	svc, _, remover, _ := lifecycleService(t)
	remover.err = sdkerrors.NewNotFound("orchestrator.RemoveFleetNode", "agent not found")
	result, err := svc.Stop(context.Background(), validSpec())
	if err != nil || !result.Deregistered {
		t.Fatalf("an agent already gone from the registry should count as deregistered: %+v %v", result, err)
	}
}

func TestStop_DeregisterFailureIsTypedWithFix(t *testing.T) {
	svc, _, remover, _ := lifecycleService(t)
	remover.err = errors.New("control plane unavailable")
	_, err := svc.Stop(context.Background(), validSpec())
	var deregErr *FleetNodeDeregisterError
	if !errors.As(err, &deregErr) || !strings.Contains(deregErr.Fix(), "quicbaas fleet remove "+iam.FleetNodeAppName("acme", lifecycleCN)) {
		t.Fatalf("want FleetNodeDeregisterError with the remove command, got %v", err)
	}
}

func TestStop_Refusals(t *testing.T) {
	svc, _, _, log := lifecycleService(t)
	svc.remover = nil
	var noRegistry *FleetRegistryUnavailableError
	if _, err := svc.Stop(context.Background(), validSpec()); !errors.As(err, &noRegistry) {
		t.Fatalf("want FleetRegistryUnavailableError, got %v", err)
	}
	if log.String() != "" {
		t.Fatalf("touched the node without a registry to deregister from: %s", log)
	}

	svc, node, _, log := lifecycleService(t)
	node.certErr = &deploy.AgentIdentityMissingError{Path: "/x"}
	if _, err := svc.Stop(context.Background(), validSpec()); err == nil {
		t.Fatal("stop without an identity must fail")
	}
	if strings.Contains(log.String(), "stop") {
		t.Fatalf("stopped an agent it could not then deregister: %s", log)
	}

	svc, node, _, log = lifecycleService(t)
	node.stopErr = errors.New("systemctl failed")
	if _, err := svc.Stop(context.Background(), validSpec()); err == nil || strings.Contains(log.String(), "deregister") {
		t.Fatalf("a failed stop must not deregister: %v %s", err, log)
	}

	for field, mutate := range map[string]func(*deploy.Spec){
		"host":     func(s *deploy.Spec) { s.Host = "" },
		"ssh_user": func(s *deploy.Spec) { s.SSHUser = "" },
		"tenant":   func(s *deploy.Spec) { s.Tenant = "" },
	} {
		spec := validSpec()
		mutate(spec)
		var invalid *deploy.InvalidSpecError
		if _, err := svc.Stop(context.Background(), spec); !errors.As(err, &invalid) || invalid.Field != field {
			t.Fatalf("%s: want InvalidSpecError, got %v", field, err)
		}
	}
	if _, err := svc.Stop(context.Background(), nil); err == nil {
		t.Fatal("nil spec accepted")
	}
	conflict := validSpec()
	conflict.SSHKeyName = "k"
	var authConflict *SSHAuthConflictError
	if _, err := svc.Stop(context.Background(), conflict); !errors.As(err, &authConflict) {
		t.Fatalf("want SSHAuthConflictError, got %v", err)
	}
}

func TestStart_StartsAndWaitsForTheSameAgent(t *testing.T) {
	svc, _, _, log := lifecycleService(t)
	wantID := iam.FleetNodeAppName("acme", lifecycleCN)
	svc.agents = &fakeLister{responses: []*orchestratorSDK.ListFleetNodesResponse{{Nodes: []*orchestratorSDK.FleetNode{
		{ID: "someone-else", Status: "running", LastSeen: 9},
		{ID: wantID, Status: "running", LastSeen: time.Unix(100, 0).UnixNano()},
	}}}}
	spec := validSpec()
	spec.WaitOnline = time.Minute

	result, err := svc.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if log.String() != "read-cert,start" || result.NodeID != wantID || result.OnlineAt == "" {
		t.Fatalf("start = %+v (%s)", result, log)
	}
	if strings.Contains(log.String(), "deregister") {
		t.Fatal("start deregistered")
	}
}

func TestStart_Failures(t *testing.T) {
	svc, node, _, _ := lifecycleService(t)
	node.startErr = &deploy.NotDeployedError{Host: "h"}
	var notDeployed *deploy.NotDeployedError
	if _, err := svc.Start(context.Background(), validSpec()); !errors.As(err, &notDeployed) {
		t.Fatalf("want NotDeployedError, got %v", err)
	}

	svc, _, _, _ = lifecycleService(t)
	svc.agents = &fakeLister{}
	spec := validSpec()
	spec.WaitOnline = time.Minute
	spec.NodeBind = "unix:///run/agent.sock"
	var invalid *deploy.InvalidSpecError
	if _, err := svc.Start(context.Background(), spec); !errors.As(err, &invalid) {
		t.Fatalf("an undialable bind with a wait must be refused, got %v", err)
	}

	svc, _, _, _ = lifecycleService(t)
	svc.ssh = &fakeDialer{err: errors.New("unreachable")}
	if _, err := svc.Start(context.Background(), validSpec()); err == nil {
		t.Fatal("dial failure swallowed")
	}

	svc, _, _, _ = lifecycleService(t)
	svc.resolveHome = func(context.Context, sshSession) (string, error) { return "", errors.New("no home") }
	if _, err := svc.Start(context.Background(), validSpec()); err == nil {
		t.Fatal("home failure swallowed")
	}
}

func TestUndeploy_RemovesAndDeregisters(t *testing.T) {
	svc, node, remover, log := lifecycleService(t)

	result, err := svc.Undeploy(context.Background(), validSpec(), UndeployOptions{PurgeWorkspaces: true})
	if err != nil {
		t.Fatalf("Undeploy: %v", err)
	}
	if log.String() != "read-cert,uninstall,deregister" {
		t.Fatalf("order = %s", log)
	}
	if !node.purged || !result.Deregistered || remover.seen.NodeID != iam.FleetNodeAppName("acme", lifecycleCN) {
		t.Fatalf("result = %+v purged=%v", result, node.purged)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "/home/deploy/.quicbaas/bin/quicbaas" {
		t.Fatalf("removed = %v", result.Removed)
	}
}

// TestUndeploy_WithoutIdentityStillRemoves: a half-installed node has no
// certificate, so there is no registration this command can name; it still
// removes what is there and says what it could not do.
func TestUndeploy_WithoutIdentityStillRemoves(t *testing.T) {
	svc, node, _, log := lifecycleService(t)
	node.certErr = &deploy.AgentIdentityMissingError{Path: "/x"}

	result, err := svc.Undeploy(context.Background(), validSpec(), UndeployOptions{})
	if err != nil {
		t.Fatalf("Undeploy: %v", err)
	}
	if log.String() != "read-cert,uninstall" || result.Note == "" || result.Deregistered || node.purged {
		t.Fatalf("result = %+v (%s)", result, log)
	}
}

func TestUndeploy_UninstallFailureDoesNotDeregister(t *testing.T) {
	svc, node, _, log := lifecycleService(t)
	node.uninstallErr = errors.New("rm failed")
	if _, err := svc.Undeploy(context.Background(), validSpec(), UndeployOptions{}); err == nil || strings.Contains(log.String(), "deregister") {
		t.Fatalf("a failed uninstall must not deregister: %v %s", err, log)
	}
}

func TestRealNodeOperator_NoUnderlyingClient(t *testing.T) {
	op := realNodeOperator{}
	sess := &fakeSession{}
	if checks := op.Preflight(context.Background(), sess, deploy.PreflightInput{}); len(checks) != 1 || checks[0].Passed {
		t.Fatalf("preflight without a client: %+v", checks)
	}
	if _, err := op.Stop(context.Background(), sess, deploy.Layout{}); err == nil {
		t.Fatal("stop without a client")
	}
	if _, err := op.Start(context.Background(), sess, deploy.Layout{}); err == nil {
		t.Fatal("start without a client")
	}
	if _, err := op.Uninstall(context.Background(), sess, deploy.Layout{}, false); err == nil {
		t.Fatal("uninstall without a client")
	}
	if _, err := op.ReadCertificate(context.Background(), sess, deploy.Layout{}); err == nil {
		t.Fatal("read certificate without a client")
	}
}

func TestLifecycleErrors_Messages(t *testing.T) {
	if !strings.Contains((&FleetRegistryUnavailableError{}).Error(), "deregistered") {
		t.Fatal("registry error does not say what it could not do")
	}
	if !strings.Contains((&FleetCABundleUnavailableError{}).Error(), "CA bundle") {
		t.Fatal("ca bundle error unclear")
	}
	cause := errors.New("down")
	deregErr := &FleetNodeDeregisterError{AgentID: "a", Tenant: "t", Cause: cause}
	if !errors.Is(deregErr, cause) || !strings.Contains(deregErr.Error(), "stopped") {
		t.Fatalf("deregister error = %v", deregErr)
	}
	uploadErr := &FleetNodeConfigUploadError{Path: "/p", Cause: cause}
	if !errors.Is(uploadErr, cause) || !strings.Contains(uploadErr.Error(), "/p") {
		t.Fatalf("config upload error = %v", uploadErr)
	}
}
