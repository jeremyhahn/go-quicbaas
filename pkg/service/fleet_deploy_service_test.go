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
	"crypto/x509"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
	"golang.org/x/crypto/ssh"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// -----------------------------------------------------------------------------
// Test doubles
// -----------------------------------------------------------------------------

// fakeSession satisfies sshSession.
type fakeSession struct{ closed bool }

// acmeCtx is a caller authenticated in tenant acme, the tenant the test specs
// deploy into: a node is deployed only in the caller's own tenant.
var acmeCtx = tenantctx.WithTenant(context.Background(), "acme")

func (s *fakeSession) Close() error { s.closed = true; return nil }

type fakeDialer struct {
	sess     sshSession
	err      error
	seen     *deploy.Spec
	seenAuth []ssh.AuthMethod
	// seenHostKey records the host-key policy the service resolved, so
	// tests can assert a pinned deploy dials with verification in place
	// and an insecure one does not.
	seenHostKey *deploy.HostKeyPolicy
}

func (d *fakeDialer) Dial(ctx context.Context, spec *deploy.Spec, hostKey *deploy.HostKeyPolicy) (sshSession, error) {
	d.seen = spec
	d.seenHostKey = hostKey
	if d.err != nil {
		return nil, d.err
	}
	if d.sess == nil {
		d.sess = &fakeSession{}
	}
	return d.sess, nil
}

// DialWithAuth satisfies the extended sshDialer interface so the fake
// can serve both spec-credential and SSHKeyName dial paths.
func (d *fakeDialer) DialWithAuth(ctx context.Context, spec *deploy.Spec, auth []ssh.AuthMethod, hostKey *deploy.HostKeyPolicy) (sshSession, error) {
	d.seen = spec
	d.seenAuth = auth
	d.seenHostKey = hostKey
	if d.err != nil {
		return nil, d.err
	}
	if d.sess == nil {
		d.sess = &fakeSession{}
	}
	return d.sess, nil
}

type fakeUploader struct {
	err        error
	localPath  string
	remotePath string
	calls      int
}

func (u *fakeUploader) Upload(ctx context.Context, sess sshSession, localPath, remotePath string) error {
	u.calls++
	u.localPath = localPath
	u.remotePath = remotePath
	return u.err
}

// fakeFileUploader records every WriteRemoteFile call so tests can
// assert that cert/key/CA material was uploaded to the expected paths
// with the expected modes.
type fakeFileUploader struct {
	mu    sync.Mutex
	err   error
	files map[string]fakeFile
}

type fakeFile struct {
	content []byte
	mode    uint32
}

func newFakeFileUploader() *fakeFileUploader {
	return &fakeFileUploader{files: map[string]fakeFile{}}
}

func (f *fakeFileUploader) WriteRemoteFile(ctx context.Context, sess sshSession, remotePath string, content []byte, mode uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	// Copy to avoid callers mutating the underlying slice (secure.Buffer
	// owns the backing memory — tests want a snapshot).
	cp := make([]byte, len(content))
	copy(cp, content)
	f.files[remotePath] = fakeFile{content: cp, mode: mode}
	return nil
}

func (f *fakeFileUploader) get(path string) (fakeFile, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.files[path]
	return v, ok
}

// fakeCertIssuer returns a pre-built mTLS identity for tests.
type fakeCertIssuer struct {
	err     error
	calls   atomic.Int32
	seenReq *FleetNodeCertificateRequest
}

func (f *fakeCertIssuer) IssueFleetNodeCertificate(ctx context.Context, req *FleetNodeCertificateRequest) (*IssuedFleetNodeCertificate, error) {
	f.calls.Add(1)
	f.seenReq = req
	if f.err != nil {
		return nil, f.err
	}
	keyPEM := []byte("-----BEGIN TEST KEY-----\nabc\n-----END TEST KEY-----\n")
	cpy := make([]byte, len(keyPEM))
	copy(cpy, keyPEM)
	buf, err := secure.NewBufferFromBytes(cpy)
	if err != nil {
		return nil, err
	}
	return &IssuedFleetNodeCertificate{
		CertPEM:    []byte("-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n"),
		PrivateKey: buf,
		CAPEM:      []byte("-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----\n"),
		ParsedCert: &x509.Certificate{SerialNumber: big.NewInt(0xdeadbeef)},
	}, nil
}

// FleetCABundle lets the fake issuer serve preflight's control-plane check.
func (f *fakeCertIssuer) FleetCABundle(ctx context.Context, tenantID string) ([]byte, error) {
	return []byte("-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----\n"), nil
}

type fakeSystemd struct {
	detect bool
	err    error
	unit   string
	calls  int
}

func (s *fakeSystemd) Detect(ctx context.Context, sess sshSession) bool { return s.detect }
func (s *fakeSystemd) InstallAndStart(ctx context.Context, sess sshSession, unitContent, unitName string) error {
	s.calls++
	s.unit = unitContent
	return s.err
}

type fakeNohup struct {
	err        error
	script     string
	scriptPath string
	calls      int
}

func (n *fakeNohup) Start(ctx context.Context, sess sshSession, scriptContent, scriptPath string) error {
	n.calls++
	n.script = scriptContent
	n.scriptPath = scriptPath
	return n.err
}

type fakeRenderer struct{}

func (fakeRenderer) RenderUnit(spec *deploy.Spec) string        { return "unit-for-" + spec.Tenant }
func (fakeRenderer) RenderStartScript(spec *deploy.Spec) string { return "script-for-" + spec.Tenant }

type fakeLister struct {
	responses []*orchestratorSDK.ListFleetNodesResponse
	errs      []error
	calls     atomic.Int32
}

// maxFakeListerCalls caps the fake's patience. A wait-online loop that
// never terminates would otherwise poll until the call counter overflows,
// turning a logic bug into a multi-minute test with an unreadable panic.
const maxFakeListerCalls = 10_000

func (l *fakeLister) ListFleetNodes(ctx context.Context, req *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error) {
	i := int(l.calls.Add(1)) - 1
	if i >= maxFakeListerCalls {
		panic("fakeLister: wait-online polled without ever terminating")
	}
	var resp *orchestratorSDK.ListFleetNodesResponse
	var err error
	if i < len(l.responses) {
		resp = l.responses[i]
	}
	if i < len(l.errs) {
		err = l.errs[i]
	}
	return resp, err
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func validSpec() *deploy.Spec {
	return &deploy.Spec{
		Host:            "remote.example.com:22",
		SSHUser:         "deploy",
		SSHPassword:     "pw",
		InsecureHostKey: true,
		Server:          "https://c:8443",
		Tenant:          "acme",
		NodeBind:        "0.0.0.0:9091",
		Backends:        []string{"acp"},
		Binary:          "/tmp/bin/quicbaas",
		Node:            fleet.Config{MaxConcurrentJobs: 2, SandboxPolicy: orchestration.SandboxPolicyRequired},
	}
}

// newTestService wires the test doubles into a fleetDeployService.
// A non-nil cert issuer is always supplied so the validate() check
// doesn't short-circuit tests that want to exercise downstream logic.
func newTestService(
	dialer *fakeDialer,
	up *fakeUploader,
	sys *fakeSystemd,
	no *fakeNohup,
	lst *fakeLister,
	now time.Time,
) *fleetDeployService {
	return &fleetDeployService{
		ssh:         dialer,
		upload:      up,
		files:       newFakeFileUploader(),
		systemd:     sys,
		nohup:       no,
		renderer:    fakeRenderer{},
		node:        &fakeNode{},
		agents:      nilOr(lst),
		certIssuer:  &fakeCertIssuer{},
		now:         func() time.Time { return now },
		sleep:       func(ctx context.Context, d time.Duration) error { return nil },
		resolveHome: func(ctx context.Context, sess sshSession) (string, error) { return "/home/deploy", nil },
		// A probe that passes, so tests exercising the registry half of
		// wait-online do not dial the network. Tests that care about the
		// liveness half replace it. It is set explicitly because the
		// service dials for real when probe is nil: a missing probe is
		// never read as a probe that passed.
		probe: func(ctx context.Context, addr string) error { return nil },
	}
}

// nilOr returns the interface value when non-nil, and a true nil
// FleetNodeLister otherwise. This prevents a typed-nil *fakeLister from
// slipping past the s.agents != nil check in the service.
func nilOr(l *fakeLister) FleetNodeLister {
	if l == nil {
		return nil
	}
	return l
}

// -----------------------------------------------------------------------------
// Validation
// -----------------------------------------------------------------------------

func TestDeploy_Validate(t *testing.T) {
	base := validSpec()
	cases := []struct {
		name   string
		mutate func(*deploy.Spec)
		field  string
	}{
		{"nil spec", func(s *deploy.Spec) { *s = deploy.Spec{} }, "host"},
		{"no host", func(s *deploy.Spec) { s.Host = "" }, "host"},
		{"no user", func(s *deploy.Spec) { s.SSHUser = "" }, "ssh_user"},
		{"no server", func(s *deploy.Spec) { s.Server = "" }, "server"},
		{"no tenant", func(s *deploy.Spec) { s.Tenant = "" }, "tenant"},
		{"no binary", func(s *deploy.Spec) { s.Binary = "" }, "binary"},
		{"no bind", func(s *deploy.Spec) { s.NodeBind = "" }, "agent_bind"},
		{"no backends", func(s *deploy.Spec) { s.Backends = nil }, "backends"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := *base
			tc.mutate(&spec)
			svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, &fakeLister{}, time.Now())
			_, err := svc.Deploy(acmeCtx, &spec)
			if err == nil {
				t.Fatal("expected error")
			}
			var ise *deploy.InvalidSpecError
			if !errors.As(err, &ise) {
				t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
			}
			if ise.Field != tc.field {
				t.Fatalf("field=%q want %q", ise.Field, tc.field)
			}
		})
	}
}

func TestDeploy_ValidateNilSpec(t *testing.T) {
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, &fakeLister{}, time.Now())
	_, err := svc.Deploy(acmeCtx, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var ise *deploy.InvalidSpecError
	if !errors.As(err, &ise) {
		t.Fatalf("want InvalidSpecError, got %T", err)
	}
}

// TestDeploy_ValidateRequiresCertIssuer asserts that Deploy() rejects a
// service wired without a cert issuer before it dials SSH. This is the
// guardrail for production wiring mistakes.
func TestDeploy_ValidateRequiresCertIssuer(t *testing.T) {
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	svc.certIssuer = nil
	_, err := svc.Deploy(acmeCtx, validSpec())
	if err == nil {
		t.Fatal("expected error when cert issuer is nil")
	}
	var ise *deploy.InvalidSpecError
	if !errors.As(err, &ise) {
		t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
	}
	if ise.Field != "cert_issuer" {
		t.Fatalf("field=%q want %q", ise.Field, "cert_issuer")
	}
}

// -----------------------------------------------------------------------------
// Happy paths
// -----------------------------------------------------------------------------

func TestDeploy_SuccessSystemd_ExplicitFlag(t *testing.T) {
	spec := validSpec()
	on := true
	spec.Systemd = &on
	dialer := &fakeDialer{}
	up := &fakeUploader{}
	sys := &fakeSystemd{}
	no := &fakeNohup{}
	svc := newTestService(dialer, up, sys, no, nil, time.Now())

	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if result.ServiceStatus != "systemd" {
		t.Fatalf("status=%q", result.ServiceStatus)
	}
	if sys.calls != 1 || no.calls != 0 {
		t.Fatalf("systemd calls=%d nohup calls=%d", sys.calls, no.calls)
	}
	if !strings.Contains(up.remotePath, "quicbaas") {
		t.Fatalf("remote path=%q", up.remotePath)
	}
	if !strings.Contains(sys.unit, "unit-for-acme") {
		t.Fatalf("unit content=%q", sys.unit)
	}
	if !dialer.sess.(*fakeSession).closed {
		t.Fatal("session not closed")
	}
	if result.NodeCertPath == "" {
		t.Fatal("expected NodeCertPath to be set in result")
	}
	if result.NodeCertSerial == "" {
		t.Fatal("expected NodeCertSerial to be set in result")
	}
}

func TestDeploy_SuccessNohup_ExplicitFlag(t *testing.T) {
	spec := validSpec()
	off := false
	spec.Systemd = &off
	dialer := &fakeDialer{}
	up := &fakeUploader{}
	sys := &fakeSystemd{}
	no := &fakeNohup{}
	svc := newTestService(dialer, up, sys, no, nil, time.Now())

	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if result.ServiceStatus != "nohup" {
		t.Fatalf("status=%q", result.ServiceStatus)
	}
	if sys.calls != 0 || no.calls != 1 {
		t.Fatalf("systemd=%d nohup=%d", sys.calls, no.calls)
	}
	if !strings.Contains(no.scriptPath, "start-quicbaas-agent.sh") {
		t.Fatalf("script path=%q", no.scriptPath)
	}
}

func TestDeploy_AutoDetectSystemd(t *testing.T) {
	spec := validSpec() // Systemd nil → auto
	dialer := &fakeDialer{}
	up := &fakeUploader{}
	sys := &fakeSystemd{detect: true}
	no := &fakeNohup{}
	svc := newTestService(dialer, up, sys, no, nil, time.Now())

	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.ServiceStatus != "systemd" {
		t.Fatalf("expected systemd auto-detected, got %q", result.ServiceStatus)
	}
}

func TestDeploy_AutoDetectFallbackNohup(t *testing.T) {
	spec := validSpec()
	dialer := &fakeDialer{}
	up := &fakeUploader{}
	sys := &fakeSystemd{detect: false}
	no := &fakeNohup{}
	svc := newTestService(dialer, up, sys, no, nil, time.Now())

	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.ServiceStatus != "nohup" {
		t.Fatalf("expected nohup fallback, got %q", result.ServiceStatus)
	}
}

// TestDeploy_UploadsIdentityFiles asserts that the deploy flow uploads
// the cert, key, and CA bundle to DataDir with the expected modes.
func TestDeploy_UploadsIdentityFiles(t *testing.T) {
	spec := validSpec()
	on := true
	spec.Systemd = &on
	files := newFakeFileUploader()
	svc := &fleetDeployService{
		ssh:         &fakeDialer{},
		upload:      &fakeUploader{},
		files:       files,
		systemd:     &fakeSystemd{},
		nohup:       &fakeNohup{},
		renderer:    fakeRenderer{},
		node:        &fakeNode{},
		certIssuer:  &fakeCertIssuer{},
		now:         time.Now,
		sleep:       func(ctx context.Context, d time.Duration) error { return nil },
		resolveHome: func(ctx context.Context, sess sshSession) (string, error) { return "/home/deploy", nil },
	}

	_, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	certPath := "/home/deploy/.quicbaas/agent/" + deploy.NodeCertFilename
	keyPath := "/home/deploy/.quicbaas/agent/" + deploy.NodeKeyFilename
	caPath := "/home/deploy/.quicbaas/agent/" + deploy.ServerCAFilename

	if cert, ok := files.get(certPath); !ok {
		t.Fatalf("expected cert upload at %q", certPath)
	} else if cert.mode != 0o644 {
		t.Fatalf("cert mode=%o want 0644", cert.mode)
	}
	if key, ok := files.get(keyPath); !ok {
		t.Fatalf("expected key upload at %q", keyPath)
	} else if key.mode != 0o600 {
		t.Fatalf("key mode=%o want 0600", key.mode)
	}
	if ca, ok := files.get(caPath); !ok {
		t.Fatalf("expected CA upload at %q", caPath)
	} else if ca.mode != 0o644 {
		t.Fatalf("ca mode=%o want 0644", ca.mode)
	}
}

// TestDeploy_CertIssuerError surfaces issuer failures without touching
// the remote host.
func TestDeploy_CertIssuerError(t *testing.T) {
	dialer := &fakeDialer{}
	want := errors.New("ca misconfigured")
	up := &fakeUploader{}
	svc := newTestService(dialer, up, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	svc.certIssuer = &fakeCertIssuer{err: want}

	// Preflight's read-only dial is the only contact a failed issuance
	// allows; with it skipped, the host is not dialed at all.
	spec := validSpec()
	spec.SkipPreflight = true
	_, err := svc.Deploy(acmeCtx, spec)
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if dialer.seen != nil {
		t.Fatal("expected dial to be skipped when issuer fails")
	}

	// With preflight, the node is probed but nothing is installed.
	if _, err := svc.Deploy(acmeCtx, validSpec()); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
	if up.calls != 0 {
		t.Fatal("a failed issuance still uploaded the binary")
	}
}

// -----------------------------------------------------------------------------
// Error propagation
// -----------------------------------------------------------------------------

func TestDeploy_DialError(t *testing.T) {
	want := &deploy.SSHDialError{Host: "h"}
	dialer := &fakeDialer{err: want}
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	_, err := svc.Deploy(acmeCtx, validSpec())
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestDeploy_UploadError(t *testing.T) {
	want := &deploy.SFTPUploadError{RemotePath: "/x"}
	up := &fakeUploader{err: want}
	svc := newTestService(&fakeDialer{}, up, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	_, err := svc.Deploy(acmeCtx, validSpec())
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestDeploy_IdentityUploadError(t *testing.T) {
	files := newFakeFileUploader()
	files.err = errors.New("permission denied")
	svc := &fleetDeployService{
		ssh:         &fakeDialer{},
		upload:      &fakeUploader{},
		files:       files,
		systemd:     &fakeSystemd{},
		nohup:       &fakeNohup{},
		renderer:    fakeRenderer{},
		node:        &fakeNode{},
		certIssuer:  &fakeCertIssuer{},
		now:         time.Now,
		sleep:       func(ctx context.Context, d time.Duration) error { return nil },
		resolveHome: func(ctx context.Context, sess sshSession) (string, error) { return "", nil },
	}
	_, err := svc.Deploy(acmeCtx, validSpec())
	var identErr *FleetNodeIdentityUploadError
	if !errors.As(err, &identErr) {
		t.Fatalf("want FleetNodeIdentityUploadError, got %T: %v", err, err)
	}
}

func TestDeploy_SystemdError(t *testing.T) {
	spec := validSpec()
	on := true
	spec.Systemd = &on
	want := &deploy.SystemdInstallError{Unit: "u"}
	sys := &fakeSystemd{err: want}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, sys, &fakeNohup{}, nil, time.Now())
	_, err := svc.Deploy(acmeCtx, spec)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestDeploy_NohupError(t *testing.T) {
	spec := validSpec()
	off := false
	spec.Systemd = &off
	want := &deploy.RemoteExecError{Command: "x"}
	no := &fakeNohup{err: want}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, no, nil, time.Now())
	_, err := svc.Deploy(acmeCtx, spec)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

// -----------------------------------------------------------------------------
// Wait-online
// -----------------------------------------------------------------------------

func TestDeploy_WaitOnline_Success(t *testing.T) {
	spec := validSpec()
	off := false
	spec.Systemd = &off
	spec.WaitOnline = 10 * time.Second

	now := time.Unix(1_700_000_000, 0)
	lastSeen := time.Unix(1_700_000_010, 0).UnixNano()
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: "a-1", Host: "0.0.0.0:9091", Status: "running", LastSeen: lastSeen},
		},
		Count: 1,
	}
	lst := &fakeLister{responses: []*orchestratorSDK.ListFleetNodesResponse{resp}}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, lst, now)

	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if result.NodeID != "a-1" {
		t.Fatalf("agent id=%q", result.NodeID)
	}
	if result.OnlineAt == "" {
		t.Fatal("expected online timestamp")
	}
	if int(lst.calls.Load()) != 1 {
		t.Fatalf("poll count=%d", lst.calls.Load())
	}
}

func TestDeploy_WaitOnline_Timeout(t *testing.T) {
	spec := validSpec()
	off := false
	spec.Systemd = &off
	spec.WaitOnline = 1 * time.Millisecond

	now := time.Unix(0, 0)
	// Responses are empty → timeout.
	lst := &fakeLister{responses: []*orchestratorSDK.ListFleetNodesResponse{
		{Nodes: []*orchestratorSDK.FleetNode{}},
	}}
	advancing := now
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, lst, now)
	// After the first poll make "now" cross the deadline so the loop exits.
	svc.now = func() time.Time {
		advancing = advancing.Add(time.Second)
		return advancing
	}

	_, err := svc.Deploy(acmeCtx, spec)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var timeoutErr *deploy.StartTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("want StartTimeoutError, got %T: %v", err, err)
	}
}

// TestDeploy_WaitOnline_ProbesAfterSessionClosed pins the ordering the
// remote-agent incident exposed (go-quicbaas#980): the liveness probe has to run
// after the deploy's SSH session is closed, because a systemd --user
// agent whose manager does not linger only dies at that moment.
func TestDeploy_WaitOnline_ProbesAfterSessionClosed(t *testing.T) {
	spec := validSpec()
	on := true
	spec.Systemd = &on
	spec.WaitOnline = 10 * time.Second

	now := time.Unix(1_700_000_000, 0)
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: "a-1", Host: "0.0.0.0:9091", Status: "running", LastSeen: now.UnixNano()},
		},
		Count: 1,
	}
	lst := &fakeLister{responses: []*orchestratorSDK.ListFleetNodesResponse{resp}}
	sess := &fakeSession{}
	svc := newTestService(&fakeDialer{sess: sess}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, lst, now)

	var probedAddr string
	var closedAtProbe bool
	probeCalls := 0
	svc.probe = func(ctx context.Context, addr string) error {
		probeCalls++
		probedAddr = addr
		closedAtProbe = sess.closed
		return nil
	}

	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if probeCalls != 1 {
		t.Fatalf("probe calls=%d want 1", probeCalls)
	}
	if !closedAtProbe {
		t.Fatal("the ssh session must be closed before the agent is probed")
	}
	if want := "remote.example.com:9091"; probedAddr != want {
		t.Fatalf("probed %q want %q", probedAddr, want)
	}
	if result.NodeID != "a-1" {
		t.Fatalf("agent id=%q", result.NodeID)
	}
}

// TestDeploy_WaitOnline_RejectsUnreachableAgent is the remote-agent regression:
// the fleet registry still reported the agent as running from its last
// heartbeat while every dial to its port was refused. A deploy must not
// report Online on that evidence.
func TestDeploy_WaitOnline_RejectsUnreachableAgent(t *testing.T) {
	spec := validSpec()
	on := true
	spec.Systemd = &on
	spec.WaitOnline = time.Second

	now := time.Unix(1_700_000_000, 0)
	registered := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: "a-1", Host: "0.0.0.0:9091", Status: "running", LastSeen: now.UnixNano()},
		},
		Count: 1,
	}
	lst := &fakeLister{responses: []*orchestratorSDK.ListFleetNodesResponse{registered, registered}}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, lst, now)

	refused := &deploy.FleetNodeUnreachableError{
		Addr:  "remote.example.com:9091",
		Cause: errors.New("connect: connection refused"),
	}
	svc.probe = func(ctx context.Context, addr string) error { return refused }
	advancing := now
	svc.now = func() time.Time {
		advancing = advancing.Add(time.Second)
		return advancing
	}

	result, err := svc.Deploy(acmeCtx, spec)
	var timeoutErr *deploy.StartTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("want StartTimeoutError, got %T: %v", err, err)
	}
	var unreachable *deploy.FleetNodeUnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("timeout must carry why the agent was rejected, got %v", err)
	}
	if result != nil && result.NodeID != "" {
		t.Fatalf("an unreachable agent must not be reported online: %q", result.NodeID)
	}
}

// TestDeploy_RejectsUndialableBindWhenWaitOnline pins the inverse of the
// agent09 regression: when the deploy is asked to confirm the agent came
// online but the bind gives it nothing to dial, it must refuse the spec
// — before touching the remote host — instead of falling back to the
// registry, whose stale "running" is what reported the remote fleet node Online in the
// first place (go-quicbaas#980).
func TestDeploy_RejectsUndialableBindWhenWaitOnline(t *testing.T) {
	binds := []struct {
		name string
		bind string
	}{
		{"ephemeral port", "0.0.0.0:0"},
		{"named port", "0.0.0.0:grpc"},
		{"unix socket", "unix:/run/agent.sock"},
	}
	for _, tc := range binds {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			on := true
			spec.Systemd = &on
			spec.NodeBind = tc.bind
			spec.WaitOnline = 10 * time.Second

			lst := &fakeLister{}
			dialer := &fakeDialer{}
			sys := &fakeSystemd{}
			svc := newTestService(dialer, &fakeUploader{}, sys, &fakeNohup{}, lst, time.Unix(1_700_000_000, 0))
			probeCalls := 0
			svc.probe = func(ctx context.Context, addr string) error {
				probeCalls++
				return nil
			}

			result, err := svc.Deploy(acmeCtx, spec)
			var invalid *deploy.InvalidSpecError
			if !errors.As(err, &invalid) {
				t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
			}
			if invalid.Field != "agent_bind" {
				t.Fatalf("field=%q want agent_bind", invalid.Field)
			}
			if result != nil {
				t.Fatalf("a rejected spec must not produce a result: %+v", result)
			}
			if dialer.seen != nil {
				t.Fatal("the spec must be rejected before the ssh dial")
			}
			if sys.calls != 0 {
				t.Fatalf("systemd installs=%d want 0", sys.calls)
			}
			if lst.calls.Load() != 0 {
				t.Fatalf("registry polls=%d want 0", lst.calls.Load())
			}
			if probeCalls != 0 {
				t.Fatalf("probe calls=%d want 0", probeCalls)
			}
		})
	}
}

// TestWaitOnline_UndialableBindIsNotOnline guards the same invariant one
// layer down, where an Online verdict is actually produced: even handed
// a registry that reports the agent running, waitOnline must not return
// an ID for a bind it cannot dial.
func TestWaitOnline_UndialableBindIsNotOnline(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	spec := validSpec()
	spec.NodeBind = "0.0.0.0:0"
	spec.WaitOnline = 10 * time.Second

	lst := &fakeLister{responses: []*orchestratorSDK.ListFleetNodesResponse{{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: "a-1", Host: "0.0.0.0:0", Status: "running", LastSeen: now.UnixNano()},
		},
		Count: 1,
	}}}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, lst, now)

	id, at, err := svc.waitOnline(context.Background(), spec, "a-1")
	var invalid *deploy.InvalidSpecError
	if !errors.As(err, &invalid) {
		t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
	}
	if id != "" || at != "" {
		t.Fatalf("no online verdict may be returned without a dial: id=%q at=%q", id, at)
	}
}

// TestProbeRuntimePort_NoInjectedProbeStillDials asserts the other way a
// probe can go missing: a service whose probe field was never wired must
// dial for real rather than report a pass it never made.
func TestProbeRuntimePort_NoInjectedProbeStillDials(t *testing.T) {
	svc := &fleetDeployService{}

	t.Run("closed port is unreachable", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		addr := ln.Addr().String()
		if err := ln.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		err = svc.probeRuntimePort(context.Background(), addr)
		var unreachable *deploy.FleetNodeUnreachableError
		if !errors.As(err, &unreachable) {
			t.Fatalf("want FleetNodeUnreachableError, got %T: %v", err, err)
		}
	})

	t.Run("empty address is invalid", func(t *testing.T) {
		err := svc.probeRuntimePort(context.Background(), "")
		var invalid *deploy.InvalidSpecError
		if !errors.As(err, &invalid) {
			t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
		}
	})
}

func TestAgentProbeAddr(t *testing.T) {
	cases := []struct {
		name string
		spec *deploy.Spec
		want string
	}{
		{name: "nil spec", spec: nil, want: ""},
		{
			name: "ssh port and wildcard bind",
			spec: &deploy.Spec{Host: "node01:2222", NodeBind: "0.0.0.0:9091"},
			want: "node01:9091",
		},
		{
			name: "user prefixed host",
			spec: &deploy.Spec{Host: "alice@node01", NodeBind: ":9091"},
			want: "node01:9091",
		},
		{
			name: "bind without port",
			spec: &deploy.Spec{Host: "node01", NodeBind: "unix:/run/agent.sock"},
			want: "",
		},
		{
			name: "named port is not dialable",
			spec: &deploy.Spec{Host: "node01", NodeBind: "0.0.0.0:grpc"},
			want: "",
		},
		{
			name: "ephemeral port is not dialable",
			spec: &deploy.Spec{Host: "node01", NodeBind: "0.0.0.0:0"},
			want: "",
		},
		{
			name: "port out of range",
			spec: &deploy.Spec{Host: "node01", NodeBind: "0.0.0.0:99999"},
			want: "",
		},
		{
			name: "empty bind",
			spec: &deploy.Spec{Host: "node01", NodeBind: ""},
			want: "",
		},
		{
			name: "empty host",
			spec: &deploy.Spec{Host: "", NodeBind: "0.0.0.0:9091"},
			want: "",
		},
		{
			name: "bracketed ipv6 host with ssh port",
			spec: &deploy.Spec{Host: "[2001:db8::1]:22", NodeBind: "0.0.0.0:9091"},
			want: "[2001:db8::1]:9091",
		},
		{
			name: "bare ipv6 host",
			spec: &deploy.Spec{Host: "2001:db8::1", NodeBind: "0.0.0.0:9091"},
			want: "[2001:db8::1]:9091",
		},
		{
			name: "user prefixed bracketed ipv6 host",
			spec: &deploy.Spec{Host: "alice@[2001:db8::1]:22", NodeBind: ":9091"},
			want: "[2001:db8::1]:9091",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nodeProbeAddr(tc.spec); got != tc.want {
				t.Fatalf("nodeProbeAddr=%q want %q", got, tc.want)
			}
		})
	}
}

func TestDeploy_WaitOnline_SkipWhenZero(t *testing.T) {
	spec := validSpec()
	off := false
	spec.Systemd = &off
	// WaitOnline=0 → no poll.
	lst := &fakeLister{}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, lst, time.Now())
	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.NodeID != "" {
		t.Fatalf("expected no agent id, got %q", result.NodeID)
	}
	if lst.calls.Load() != 0 {
		t.Fatalf("expected 0 polls, got %d", lst.calls.Load())
	}
}

func TestDeploy_WaitOnline_NilListerSkipped(t *testing.T) {
	spec := validSpec()
	off := false
	spec.Systemd = &off
	spec.WaitOnline = time.Second
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.NodeID != "" {
		t.Fatalf("expected empty id, got %q", result.NodeID)
	}
}

// -----------------------------------------------------------------------------
// Helper function tests
// -----------------------------------------------------------------------------

func TestPickOnlineAgent(t *testing.T) {
	spec := &deploy.Spec{NodeBind: "0.0.0.0:9091"}

	cases := []struct {
		name string
		resp *orchestratorSDK.ListFleetNodesResponse
		want string
	}{
		{"nil", nil, ""},
		{"empty", &orchestratorSDK.ListFleetNodesResponse{}, ""},
		{
			name: "offline skipped",
			resp: &orchestratorSDK.ListFleetNodesResponse{Nodes: []*orchestratorSDK.FleetNode{
				{ID: "x", Host: "0.0.0.0:9091", Status: "offline"},
			}},
			want: "",
		},
		{
			name: "wrong host skipped",
			resp: &orchestratorSDK.ListFleetNodesResponse{Nodes: []*orchestratorSDK.FleetNode{
				{ID: "x", Host: "other:9091", Status: "running"},
			}},
			want: "",
		},
		{
			name: "picks newest of matches",
			resp: &orchestratorSDK.ListFleetNodesResponse{Nodes: []*orchestratorSDK.FleetNode{
				{ID: "old", Host: "0.0.0.0:9091", Status: "running", LastSeen: 1},
				{ID: "new", Host: "0.0.0.0:9091", Status: "idle", LastSeen: 2},
			}},
			want: "new",
		},
		{
			name: "host suffix match",
			resp: &orchestratorSDK.ListFleetNodesResponse{Nodes: []*orchestratorSDK.FleetNode{
				{ID: "sfx", Host: "remote.host/0.0.0.0:9091", Status: "running"},
			}},
			want: "sfx",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, _ := pickOnlineNode(tc.resp, spec, "")
			if id != tc.want {
				t.Fatalf("id=%q want %q", id, tc.want)
			}
		})
	}
}

func TestIsOnlineStatus(t *testing.T) {
	online := []string{"running", "idle", "ready"}
	for _, s := range online {
		if !isOnlineStatus(s) {
			t.Fatalf("status %q should be online", s)
		}
	}
	offline := []string{"", "offline", "error", "unknown"}
	for _, s := range offline {
		if isOnlineStatus(s) {
			t.Fatalf("status %q should be offline", s)
		}
	}
}

func TestPathJoinHome(t *testing.T) {
	cases := map[string]string{
		".quicbaas/bin": "$HOME/.quicbaas/bin",
		"/tmp/absolute": "$HOME/tmp/absolute",
		"plain":         "$HOME/plain",
	}
	for in, want := range cases {
		if got := pathJoinHome(in); got != want {
			t.Fatalf("pathJoinHome(%q)=%q want %q", in, got, want)
		}
	}
}

// TestHostnameForCert strips user@ and :port off SSH targets.
func TestHostnameForCert(t *testing.T) {
	cases := map[string]string{
		"host":                "host",
		"host:22":             "host",
		"alice@host":          "host",
		"alice@host:22":       "host",
		"alice@gpu-host-01":   "gpu-host-01",
		"alice@remote.lan:22": "remote.lan",
		// IPv6 literals survive whole, without their brackets.
		"[2001:db8::1]:22":       "2001:db8::1",
		"[2001:db8::1]":          "2001:db8::1",
		"2001:db8::1":            "2001:db8::1",
		"alice@[2001:db8::1]:22": "2001:db8::1",
		"alice@2001:db8::1":      "2001:db8::1",
		"::1":                    "::1",
	}
	for in, want := range cases {
		if got := hostnameForCert(in); got != want {
			t.Fatalf("hostnameForCert(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCtxSleep_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ctxSleep(ctx, time.Second); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestCtxSleep_Elapsed(t *testing.T) {
	ctx := context.Background()
	// Use 1ns to avoid real wall-clock delay.
	if err := ctxSleep(ctx, time.Nanosecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Real adapters
// -----------------------------------------------------------------------------

func TestRealRenderer_Passthrough(t *testing.T) {
	r := realRenderer{}
	spec := validSpec()
	if r.RenderUnit(spec) == "" {
		t.Fatal("unit empty")
	}
	if r.RenderStartScript(spec) == "" {
		t.Fatal("script empty")
	}
}

func TestRealAdapters_NoUnderlyingClient(t *testing.T) {
	// Passing a session that is not an sshSessionAdapter exercises
	// the nil-client guards in the real adapters.
	sess := &fakeSession{}
	ctx := context.Background()

	if err := (realUploader{}).Upload(ctx, sess, "/a", "/b"); err == nil {
		t.Fatal("upload must fail with no underlying client")
	}
	if err := (realFileUploader{}).WriteRemoteFile(ctx, sess, "/p", []byte("x"), 0o600); err == nil {
		t.Fatal("file write must fail with no underlying client")
	}
	if (realSystemdInstaller{}).Detect(ctx, sess) {
		t.Fatal("detect must be false with no underlying client")
	}
	if err := (realSystemdInstaller{}).InstallAndStart(ctx, sess, "u", "u.service"); err == nil {
		t.Fatal("install must fail with no underlying client")
	}
	if err := (realNohupStarter{}).Start(ctx, sess, "s", "/p"); err == nil {
		t.Fatal("start must fail with no underlying client")
	}
}

func TestNewAgentDeployService_DefaultWiring(t *testing.T) {
	svc := NewFleetDeployService(FleetDeployServiceDeps{})
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	// A nil probe skips the liveness check, so production wiring must
	// supply one.
	concrete, ok := svc.(*fleetDeployService)
	if !ok {
		t.Fatalf("unexpected service type %T", svc)
	}
	if concrete.probe == nil {
		t.Fatal("production service must wire the agent liveness probe")
	}
	// Calling Deploy with an invalid spec must surface a typed error
	// without touching the network.
	_, err := svc.Deploy(acmeCtx, &deploy.Spec{})
	var ise *deploy.InvalidSpecError
	if !errors.As(err, &ise) {
		t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
	}
}

// TestDeploy_PrivateKeyDestroyedAfterDeploy is a canary test: the Buffer
// returned by the cert issuer must be destroyed before Deploy returns.
// Reading Bytes() on a destroyed buffer returns nil, so we assert that.
func TestDeploy_PrivateKeyDestroyedAfterDeploy(t *testing.T) {
	spec := validSpec()
	on := true
	spec.Systemd = &on
	// The key's lifetime is the install's concern; preflight issues nothing.
	spec.SkipPreflight = true
	issuer := &fakeCertIssuer{}
	svc := &fleetDeployService{
		ssh:         &fakeDialer{},
		upload:      &fakeUploader{},
		files:       newFakeFileUploader(),
		systemd:     &fakeSystemd{},
		nohup:       &fakeNohup{},
		renderer:    fakeRenderer{},
		node:        &fakeNode{},
		certIssuer:  issuer,
		now:         time.Now,
		sleep:       func(ctx context.Context, d time.Duration) error { return nil },
		resolveHome: func(ctx context.Context, sess sshSession) (string, error) { return "/home/deploy", nil },
	}
	// Capture a reference to the issued identity by wrapping the fake.
	var captured *IssuedFleetNodeCertificate
	wrapped := &capturingCertIssuer{inner: issuer, sink: &captured}
	svc.certIssuer = wrapped

	if _, err := svc.Deploy(acmeCtx, spec); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if captured == nil {
		t.Fatal("issuer not invoked")
	}
	// After Deploy returns, the Buffer must be destroyed. Bytes() on a
	// destroyed buffer returns nil.
	if len(captured.PrivateKey.Bytes()) != 0 {
		t.Fatal("expected private key buffer to be destroyed after Deploy returns")
	}
}

// capturingCertIssuer is a tiny decorator that lets the canary test see
// the issued identity after Deploy returns.
type capturingCertIssuer struct {
	inner FleetNodeCertIssuer
	sink  **IssuedFleetNodeCertificate
}

func (c *capturingCertIssuer) IssueFleetNodeCertificate(ctx context.Context, req *FleetNodeCertificateRequest) (*IssuedFleetNodeCertificate, error) {
	issued, err := c.inner.IssueFleetNodeCertificate(ctx, req)
	if err != nil {
		return nil, err
	}
	*c.sink = issued
	return issued, nil
}

// -----------------------------------------------------------------------------
// AgentID-based wait-online matching
// -----------------------------------------------------------------------------

// cnCertIssuer returns an issued cert with a caller-controlled CN so
// the test can drive expectedNodeID through Deploy()'s wait-online
// branch.
// FleetCABundle lets the CN issuer serve preflight's control-plane check.
func (i *cnCertIssuer) FleetCABundle(context.Context, string) ([]byte, error) {
	return []byte("ca"), nil
}

type cnCertIssuer struct {
	cn string
}

func (i *cnCertIssuer) IssueFleetNodeCertificate(ctx context.Context, req *FleetNodeCertificateRequest) (*IssuedFleetNodeCertificate, error) {
	keyPEM := []byte("-----BEGIN TEST KEY-----\nabc\n-----END TEST KEY-----\n")
	cpy := make([]byte, len(keyPEM))
	copy(cpy, keyPEM)
	buf, err := secure.NewBufferFromBytes(cpy)
	if err != nil {
		return nil, err
	}
	parsed := &x509.Certificate{
		SerialNumber: big.NewInt(0xfeedface),
	}
	parsed.Subject.CommonName = i.cn
	return &IssuedFleetNodeCertificate{
		CertPEM:    []byte("-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n"),
		PrivateKey: buf,
		CAPEM:      []byte("-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----\n"),
		ParsedCert: parsed,
	}, nil
}

// TestPickOnlineAgent_PrefersExpectedID reproduces the production bug:
// the daemon registers under its resolved hostname (e.g. agent-02:9091)
// while the deploy spec carries NodeBind=0.0.0.0:9091. The host-suffix
// heuristic skips this row, but matching on the canonical AgentID
// computed by iam.FleetNodeAppName picks it correctly.
func TestPickOnlineAgent_PrefersExpectedID(t *testing.T) {
	spec := &deploy.Spec{NodeBind: "0.0.0.0:9091"}
	// Server-side: AgentID is "agent-default-agent-198.51.100.91"
	expected := "agent-default-agent-198.51.100.91"
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: "wrong", Host: "0.0.0.0:9091", Status: "running", LastSeen: 100},
			{ID: expected, Host: "agent-02:9091", Status: "running", LastSeen: 200},
		},
	}
	id, _ := pickOnlineNode(resp, spec, expected)
	if id != expected {
		t.Fatalf("got id=%q, want %q", id, expected)
	}
}

// TestPickOnlineAgent_ExpectedIDIgnoresHostMismatch confirms that when
// the caller passes an expectedID, host strings are not consulted —
// only the ID is. This is what unblocks deploys against hosts whose
// resolved hostname differs from the SSH target.
func TestPickOnlineAgent_ExpectedIDIgnoresHostMismatch(t *testing.T) {
	spec := &deploy.Spec{NodeBind: "0.0.0.0:9091"}
	expected := "agent-acme-agent-cloud-1"
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: expected, Host: "totally-different-host:31337", Status: "running"},
		},
	}
	id, _ := pickOnlineNode(resp, spec, expected)
	if id != expected {
		t.Fatalf("got id=%q, want %q", id, expected)
	}
}

// TestPickOnlineAgent_ExpectedIDFiltersStaleEntries asserts that a
// stale registration still keyed by the legacy host suffix is NOT
// picked when the caller asked for a specific ID that no online agent
// currently advertises.
func TestPickOnlineAgent_ExpectedIDFiltersStaleEntries(t *testing.T) {
	spec := &deploy.Spec{NodeBind: "0.0.0.0:9091"}
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: "stale", Host: "0.0.0.0:9091", Status: "running"},
		},
	}
	id, _ := pickOnlineNode(resp, spec, "agent-default-agent-newcomer")
	if id != "" {
		t.Fatalf("expected no match, got %q", id)
	}
}

// TestExpectedAgentID_DerivesFromCertCN verifies the helper that the
// deploy service uses to compute the expected ID from the issued
// certificate. The construction MUST stay in lock-step with the REST
// register handler (both call iam.FleetNodeAppName).
func TestExpectedAgentID_DerivesFromCertCN(t *testing.T) {
	spec := &deploy.Spec{Tenant: "default"}
	issued := &IssuedFleetNodeCertificate{
		ParsedCert: &x509.Certificate{},
	}
	issued.ParsedCert.Subject.CommonName = "agent-198.51.100.91"
	got := expectedNodeID(spec, issued)
	want := "agent-default-agent-198.51.100.91"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestExpectedAgentID_EmptyWhenCertMissing returns empty so callers
// fall back to the host-suffix heuristic without panicking.
func TestExpectedAgentID_EmptyWhenCertMissing(t *testing.T) {
	cases := []struct {
		name   string
		spec   *deploy.Spec
		issued *IssuedFleetNodeCertificate
	}{
		{"nil spec", nil, &IssuedFleetNodeCertificate{ParsedCert: &x509.Certificate{}}},
		{"nil issued", &deploy.Spec{Tenant: "t"}, nil},
		{"nil parsed", &deploy.Spec{Tenant: "t"}, &IssuedFleetNodeCertificate{}},
		{"blank cn", &deploy.Spec{Tenant: "t"}, &IssuedFleetNodeCertificate{ParsedCert: &x509.Certificate{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expectedNodeID(tc.spec, tc.issued); got != "" {
				t.Fatalf("expected empty, got %q", got)
			}
		})
	}
}

// TestDeploy_WaitOnline_MatchesByExpectedID is the end-to-end version
// of the bug repro: the fleet registry only carries an entry whose Host
// disagrees with spec.NodeBind, but its ID matches what the deploy
// service computes from the issued cert's CN. Without the fix the poll
// loop would time out; with it the loop returns the matching agent.
func TestDeploy_WaitOnline_MatchesByExpectedID(t *testing.T) {
	spec := validSpec()
	off := false
	spec.Systemd = &off
	spec.WaitOnline = 10 * time.Second
	// Drive the cert issuer to mint a CN that the test can predict.
	cn := "agent-cloud-host-1"
	expectedID := "agent-" + spec.Tenant + "-" + cn

	now := time.Unix(1_700_000_000, 0)
	lastSeen := time.Unix(1_700_000_010, 0).UnixNano()
	// The fleet registry shows a Host that does NOT end with NodeBind,
	// reproducing the field condition the host-only matcher missed.
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: expectedID, Host: "remote.cloud.example:9091", Status: "running", LastSeen: lastSeen},
		},
	}
	lst := &fakeLister{responses: []*orchestratorSDK.ListFleetNodesResponse{resp}}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, lst, now)
	svc.certIssuer = &cnCertIssuer{cn: cn}

	result, err := svc.Deploy(acmeCtx, spec)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if result.NodeID != expectedID {
		t.Fatalf("agent id=%q, want %q", result.NodeID, expectedID)
	}
	if result.OnlineAt == "" {
		t.Fatal("expected online timestamp")
	}
}

// TestDeploy_WaitOnline_ScopedToTenantOnly asserts that the SDK list
// request the deploy service issues filters by tenant only — passing
// Host on the wire would re-introduce the host mismatch this fix is
// designed to eliminate.
func TestDeploy_WaitOnline_ScopedToTenantOnly(t *testing.T) {
	spec := validSpec()
	off := false
	spec.Systemd = &off
	spec.WaitOnline = 10 * time.Second

	// Fail the first poll so the test inspects the ListFleetNodes request
	// shape via the recording lister.
	cn := "agent-only"
	expectedID := "agent-" + spec.Tenant + "-" + cn
	resp := &orchestratorSDK.ListFleetNodesResponse{
		Nodes: []*orchestratorSDK.FleetNode{
			{ID: expectedID, Host: "h:1", Status: "running", LastSeen: 10},
		},
	}
	lst := &recordingLister{inner: &fakeLister{responses: []*orchestratorSDK.ListFleetNodesResponse{resp}}}
	svc := newTestService(&fakeDialer{}, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	svc.agents = lst
	svc.certIssuer = &cnCertIssuer{cn: cn}

	if _, err := svc.Deploy(acmeCtx, spec); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if lst.lastReq == nil {
		t.Fatal("expected at least one ListFleetNodes call")
	}
	if lst.lastReq.Host != "" {
		t.Fatalf("expected Host filter to be empty, got %q", lst.lastReq.Host)
	}
	if lst.lastReq.TenantID != spec.Tenant {
		t.Fatalf("expected TenantID=%q, got %q", spec.Tenant, lst.lastReq.TenantID)
	}
}

// recordingLister captures the most recent ListFleetNodes request so tests
// can assert the request shape the deploy service emits.
type recordingLister struct {
	inner   *fakeLister
	lastReq *orchestratorSDK.ListFleetNodesRequest
}

func (l *recordingLister) ListFleetNodes(ctx context.Context, req *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error) {
	l.lastReq = req
	return l.inner.ListFleetNodes(ctx, req)
}

// TestDeploy_RefusesAnotherTenant pins go-qrdb v0.28's confinement: no caller,
// the system tenant included, deploys a node (and mints its certificate) in
// another tenant.
func TestDeploy_RefusesAnotherTenant(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"system tenant":  tenantctx.WithSystem(context.Background()),
		"another tenant": tenantctx.WithTenant(context.Background(), "globex"),
	} {
		t.Run(name, func(t *testing.T) {
			dialer := &fakeDialer{}
			svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
			_, err := svc.Deploy(ctx, validSpec())
			var qb *qberrors.QBError
			var cross *tenantctx.CrossTenantError
			if !errors.As(err, &qb) || qb.Code != qberrors.ErrForbidden || !errors.As(err, &cross) {
				t.Fatalf("Deploy into acme as %s = %v, want ErrForbidden wrapping *tenantctx.CrossTenantError", name, err)
			}
			if dialer.seen != nil {
				t.Fatal("a refused deploy dialled the host")
			}
		})
	}
}

// TestDeploy_RefusesAnUnboundCaller proves a deploy with no authenticated
// tenant is refused before any host is touched.
func TestDeploy_RefusesAnUnboundCaller(t *testing.T) {
	dialer := &fakeDialer{}
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Now())
	_, err := svc.Deploy(context.Background(), validSpec())
	var missing *tenantctx.MissingTenantError
	if !errors.As(err, &missing) {
		t.Fatalf("Deploy with no tenant = %v, want *tenantctx.MissingTenantError", err)
	}
	if dialer.seen != nil {
		t.Fatal("a refused deploy dialled the host")
	}
}
