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

package deploy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
)

const (
	preflightImage   = "registry.example.com/quicbaas/worker:latest"
	preflightServer  = "https://quicbaas.example.com:8443"
	preflightCPAddr  = "quicbaas.example.com:8443"
	preflightProject = "/srv/quicbaas/projects"
	dfPlenty         = "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/vda1 1000000000 1000 900000000 1% /\n"
	dfTight          = "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/vda1 1000000000 1000 1048576 99% /\n"
)

// testCA is a throwaway CA and a server certificate it issued, standing in for
// the deploy's CA and the control plane's TLS identity.
type testCA struct {
	pem    []byte
	server tls.Certificate
}

func newTestCA(t *testing.T, serverName string) testCA {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("ca cert: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("leaf cert: %v", err)
	}
	return testCA{
		pem:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		server: tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey},
	}
}

// startTLSServer serves TLS handshakes with cert on a loopback port and
// returns its address.
func startTLSServer(t *testing.T, cert tls.Certificate) string {
	t.Helper()
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				if tc, ok := c.(*tls.Conn); ok {
					_ = tc.Handshake()
				}
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		<-done
	})
	return l.Addr().String()
}

// preflightFixture is a healthy node: every probe answers the way a node that
// is ready for a deploy does. Tests break one probe at a time.
type preflightFixture struct {
	srv    *testSSHServer
	spec   *Spec
	layout Layout
	ca     testCA
}

func newPreflightFixture(t *testing.T) *preflightFixture {
	t.Helper()
	srv := newTestSSHServer(t, "u", "p")
	spec := DefaultSpec()
	spec.Server = preflightServer
	spec.ProjectsRoot = preflightProject
	spec.WorkerImages = []string{preflightImage}
	spec.Node.ContainerRuntime = fleet.ContainerRuntimeDocker
	layout := spec.Layout("/home/u")

	srv.setExec(identityCommand, "u\n1001\n1001\nu docker\n", "", false)
	srv.setExec("systemctl --user --version >/dev/null 2>&1", "", "", false)
	srv.setExec("loginctl show-user 'u' -p Linger", "Linger=yes\n", "", false)
	srv.setExec(runtimeLookupCommand("docker"), "/usr/bin/docker\n", "", false)
	srv.setExec(runtimeInfoCommand("docker"), "/var/lib/docker\n", "", false)
	srv.setExec(diskFreeCommand("/var/lib/docker"), dfPlenty, "", false)
	srv.setExec(diskFreeCommand(preflightProject), dfPlenty, "", false)
	srv.setExec(imageUserCommand("docker", preflightImage), "1001:1001\n", "", false)

	ca := newTestCA(t, "quicbaas.example.com")
	srv.setForward(preflightCPAddr, startTLSServer(t, ca.server))
	return &preflightFixture{srv: srv, spec: spec, layout: layout, ca: ca}
}

func (f *preflightFixture) run(t *testing.T) *PreflightReport {
	t.Helper()
	c := lingerTestClient(t, f.srv)
	report := &PreflightReport{Host: c.Host()}
	report.Add(RunPreflight(context.Background(), c, PreflightInput{Spec: f.spec, Layout: f.layout, ServerCAPEM: f.ca.pem})...)
	return report
}

func findCheck(t *testing.T, report *PreflightReport, prefix string) PreflightCheck {
	t.Helper()
	for _, check := range report.Checks {
		if strings.HasPrefix(check.Name, prefix) {
			return check
		}
	}
	t.Fatalf("no %q check in report: %+v", prefix, report.Checks)
	return PreflightCheck{}
}

// assertOnlyFailure checks the report failed on exactly the named check, with
// an error of type T that carries a fix.
func assertOnlyFailure[T error](t *testing.T, report *PreflightReport, name string) T {
	t.Helper()
	var failed []string
	for _, check := range report.Checks {
		if !check.Passed {
			failed = append(failed, check.Name+": "+check.Error)
		}
	}
	if len(failed) != 1 || !strings.HasPrefix(failed[0], name) {
		t.Fatalf("want exactly one failure (%s), got %v", name, failed)
	}
	check := findCheck(t, report, name)
	var typed T
	if !errors.As(check.Err(), &typed) {
		t.Fatalf("%s failed with %T (%v), want %T", name, check.Err(), check.Err(), typed)
	}
	if check.Fix == "" {
		t.Fatalf("%s failure carries no fix: %v", name, check.Err())
	}
	var failedErr *PreflightFailedError
	if err := report.Err(); !errors.As(err, &failedErr) || !errors.As(err, &typed) {
		t.Fatalf("report error %v does not expose the %T", err, typed)
	}
	return typed
}

func TestRunPreflight_HealthyNodePasses(t *testing.T) {
	report := newPreflightFixture(t).run(t)
	if !report.Passed() {
		t.Fatalf("healthy node failed preflight: %v", report.Err())
	}
	for _, name := range []string{CheckRemoteUser, CheckSystemd, CheckLinger, CheckContainerRuntime, CheckImageDisk, CheckWorkspaceDisk, CheckWorkerImage, CheckControlPlane} {
		findCheck(t, report, name)
	}
}

// TestRunPreflight_ChangesNothing: preflight's contract is that it only
// reads. Every command it ran must be one of the read-only probes.
func TestRunPreflight_ChangesNothing(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec("loginctl show-user 'u' -p Linger", "Linger=no\n", "", false)
	f.run(t)
	for _, cmd := range f.srv.ranCommands() {
		for _, mutating := range []string{"enable-linger", "pull", "mkdir", "cat >", "rm ", "systemctl --user enable", "systemctl --user start", "usermod"} {
			if strings.Contains(cmd, mutating) {
				t.Fatalf("preflight ran a mutating command: %q", cmd)
			}
		}
	}
}

func TestRunPreflight_LingerOffWithSudoPasses(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec("loginctl show-user 'u' -p Linger", "Linger=no\n", "", false)
	f.srv.setExec(sudoProbeCommand, "", "", false)
	check := findCheck(t, f.run(t), CheckLinger)
	if !check.Passed || !strings.Contains(check.Detail, "sudo") {
		t.Fatalf("linger off with sudo should pass and say the deploy enables it: %+v", check)
	}
}

func TestRunPreflight_LingerOffWithoutSudoFails(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec("loginctl show-user 'u' -p Linger", "Linger=no\n", "", false)
	f.srv.setExec(sudoProbeCommand, "", "sudo: a password is required", true)
	linger := assertOnlyFailure[*LingerNotEnabledError](t, f.run(t), CheckLinger)
	if linger.Fix() != "sudo loginctl enable-linger u" {
		t.Fatalf("fix = %q", linger.Fix())
	}
}

func TestRunPreflight_SystemdRequiredButMissing(t *testing.T) {
	f := newPreflightFixture(t)
	yes := true
	f.spec.Systemd = &yes
	f.srv.setExec("systemctl --user --version >/dev/null 2>&1", "", "", true)
	assertOnlyFailure[*SystemdUserUnavailableError](t, f.run(t), CheckSystemd)
}

func TestRunPreflight_SystemdAutoFallsBackToNohup(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec("systemctl --user --version >/dev/null 2>&1", "", "", true)
	report := f.run(t)
	if !report.Passed() {
		t.Fatalf("auto systemd on a host without it should fall back, not fail: %v", report.Err())
	}
	for _, check := range report.Checks {
		if check.Name == CheckLinger {
			t.Fatal("linger checked for a nohup install")
		}
	}
	no := false
	f.spec.Systemd = &no
	if check := findCheck(t, f.run(t), CheckSystemd); !check.Passed || !strings.Contains(check.Detail, "nohup") {
		t.Fatalf("explicit nohup should skip systemd: %+v", check)
	}
}

func TestRunPreflight_DockerGroupMissing(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec(identityCommand, "u\n1001\n1001\nu\n", "", false)
	f.srv.setExec(runtimeInfoCommand("docker"), "", "permission denied while trying to connect to the Docker daemon socket", true)
	report := f.run(t)
	runtimeErr := assertTypedCheckFailure[*ContainerRuntimeUnavailableError](t, report, CheckContainerRuntime)
	if runtimeErr.Reason != RuntimeNotInGroup || !strings.Contains(runtimeErr.Fix(), "usermod -aG docker u") {
		t.Fatalf("want the docker-group fix, got %v / %q", runtimeErr.Reason, runtimeErr.Fix())
	}
	// Without a runtime, the image cannot be checked either.
	assertTypedCheckFailure[*WorkerImageUnavailableError](t, report, CheckWorkerImage)
}

func TestRunPreflight_RuntimeNotInstalledOrNotResponding(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec(runtimeLookupCommand("docker"), "", "", true)
	runtimeErr := assertTypedCheckFailure[*ContainerRuntimeUnavailableError](t, f.run(t), CheckContainerRuntime)
	if runtimeErr.Reason != RuntimeNotInstalled {
		t.Fatalf("reason = %v", runtimeErr.Reason)
	}

	f = newPreflightFixture(t)
	f.srv.setExec(runtimeInfoCommand("docker"), "", "Cannot connect to the Docker daemon", true)
	runtimeErr = assertTypedCheckFailure[*ContainerRuntimeUnavailableError](t, f.run(t), CheckContainerRuntime)
	if runtimeErr.Reason != RuntimeNotResponding || !strings.Contains(runtimeErr.Fix(), "systemctl enable --now docker") {
		t.Fatalf("want the start-the-daemon fix, got %v / %q", runtimeErr.Reason, runtimeErr.Fix())
	}
}

func TestRunPreflight_AutoRuntimeFallsBackToPodman(t *testing.T) {
	f := newPreflightFixture(t)
	f.spec.Node.ContainerRuntime = fleet.ContainerRuntimeAuto
	f.spec.WorkerImages = nil
	f.srv.setExec(runtimeLookupCommand("docker"), "", "", true)
	f.srv.setExec(runtimeLookupCommand("podman"), "/usr/bin/podman\n", "", false)
	f.srv.setExec(runtimeInfoCommand("podman"), "/home/u/.local/share/containers/storage\n", "", false)
	f.srv.setExec(diskFreeCommand("/home/u/.local/share/containers/storage"), dfPlenty, "", false)
	report := f.run(t)
	if check := findCheck(t, report, CheckContainerRuntime); !check.Passed || !strings.HasPrefix(check.Detail, "podman") {
		t.Fatalf("auto should settle on podman: %+v", check)
	}
}

func TestRunPreflight_RuntimeNoneSkipsImages(t *testing.T) {
	f := newPreflightFixture(t)
	f.spec.Node.ContainerRuntime = fleet.ContainerRuntimeNone
	f.spec.WorkerImages = nil
	report := f.run(t)
	if !report.Passed() {
		t.Fatalf("runtime none should not fail: %v", report.Err())
	}
	if check := findCheck(t, report, CheckContainerRuntime); !strings.Contains(check.Detail, "not used") {
		t.Fatalf("runtime check detail = %q", check.Detail)
	}
}

func TestRunPreflight_DiskTooSmall(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec(diskFreeCommand(preflightProject), dfTight, "", false)
	disk := assertOnlyFailure[*InsufficientDiskError](t, f.run(t), CheckWorkspaceDisk)
	if disk.FreeBytes != 1<<30 || !strings.Contains(disk.Fix(), "projects_root") {
		t.Fatalf("disk error = %+v fix %q", disk, disk.Fix())
	}

	f = newPreflightFixture(t)
	f.srv.setExec(diskFreeCommand("/var/lib/docker"), dfTight, "", false)
	disk = assertOnlyFailure[*InsufficientDiskError](t, f.run(t), CheckImageDisk)
	if !strings.Contains(disk.Fix(), "prune") {
		t.Fatalf("image disk fix = %q", disk.Fix())
	}

	f = newPreflightFixture(t)
	f.srv.setExec(diskFreeCommand(preflightProject), "garbage", "", false)
	var execErr *RemoteExecError
	if check := findCheck(t, f.run(t), CheckWorkspaceDisk); check.Passed || !errors.As(check.Err(), &execErr) {
		t.Fatalf("unparseable df output should fail the check: %+v", check)
	}
}

// TestRunPreflight_ImageUIDMismatch is the remote-agent failure: the worker image
// was built for another uid than the one the daemon runs as.
func TestRunPreflight_ImageUIDMismatch(t *testing.T) {
	for _, imageUser := range []string{"1000:1000", "", "agent"} {
		f := newPreflightFixture(t)
		f.srv.setExec(imageUserCommand("docker", preflightImage), imageUser+"\n", "", false)
		mismatch := assertOnlyFailure[*WorkerImageUIDMismatchError](t, f.run(t), CheckWorkerImage)
		if !strings.Contains(mismatch.Fix(), "AGENT_UID=1001") || !strings.Contains(mismatch.Fix(), "AGENT_GID=1001") {
			t.Fatalf("image user %q: fix = %q", imageUser, mismatch.Fix())
		}
	}
}

// TestRunPreflight_ImageMissing is the other remote-agent failure: the worker image
// was never on the node. Pullable passes; not pullable fails with the fix.
func TestRunPreflight_ImageMissing(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec(imageUserCommand("docker", preflightImage), "", "No such image", true)
	f.srv.setExec(imagePullableCommand("docker", preflightImage), "", "", false)
	if check := findCheck(t, f.run(t), CheckWorkerImage); !check.Passed || !strings.Contains(check.Detail, "pullable") {
		t.Fatalf("pullable image should pass: %+v", check)
	}

	f.srv.setExec(imagePullableCommand("docker", preflightImage), "", "manifest unknown", true)
	missing := assertOnlyFailure[*WorkerImageUnavailableError](t, f.run(t), CheckWorkerImage)
	if !strings.Contains(missing.Fix(), "docker pull "+preflightImage) {
		t.Fatalf("fix = %q", missing.Fix())
	}
}

func TestRunPreflight_ControlPlaneUnreachable(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setForward(preflightCPAddr, "127.0.0.1:1")
	delete(f.srv.forward, preflightCPAddr)
	unreachable := assertOnlyFailure[*ControlPlaneUnreachableError](t, f.run(t), CheckControlPlane)
	if unreachable.Stage != "dial" || !strings.Contains(unreachable.Fix(), preflightCPAddr) {
		t.Fatalf("want a dial failure naming the address, got %+v / %q", unreachable, unreachable.Fix())
	}
}

// TestRunPreflight_ControlPlaneWrongCA: a control plane whose certificate the
// deploy's CA did not issue fails, and is never accepted insecurely.
func TestRunPreflight_ControlPlaneWrongCA(t *testing.T) {
	f := newPreflightFixture(t)
	f.ca.pem = newTestCA(t, "quicbaas.example.com").pem
	unreachable := assertOnlyFailure[*ControlPlaneUnreachableError](t, f.run(t), CheckControlPlane)
	if unreachable.Stage != "tls" {
		t.Fatalf("stage = %q, want tls", unreachable.Stage)
	}
}

func TestRunPreflight_ControlPlaneBadInputs(t *testing.T) {
	f := newPreflightFixture(t)
	f.spec.Server = "http://quicbaas.example.com:8443"
	if unreachable := assertOnlyFailure[*ControlPlaneUnreachableError](t, f.run(t), CheckControlPlane); unreachable.Stage != "url" {
		t.Fatalf("stage = %q, want url", unreachable.Stage)
	}

	f = newPreflightFixture(t)
	f.ca.pem = nil
	if unreachable := assertOnlyFailure[*ControlPlaneUnreachableError](t, f.run(t), CheckControlPlane); unreachable.Stage != "ca" {
		t.Fatalf("stage = %q, want ca", unreachable.Stage)
	}
}

func TestRunPreflight_IdentityProbeFails(t *testing.T) {
	f := newPreflightFixture(t)
	f.srv.setExec(identityCommand, "", "id: not found", true)
	report := f.run(t)
	if check := findCheck(t, report, CheckRemoteUser); check.Passed {
		t.Fatalf("identity probe failure not reported: %+v", check)
	}
	f.srv.setExec(identityCommand, "u\n", "", false)
	if check := findCheck(t, f.run(t), CheckRemoteUser); check.Passed {
		t.Fatalf("short identity output accepted: %+v", check)
	}
}

// assertTypedCheckFailure checks one named check failed with a T carrying a
// fix, without requiring it to be the only failure.
func assertTypedCheckFailure[T interface {
	error
	Fixer
}](t *testing.T, report *PreflightReport, name string) T {
	t.Helper()
	check := findCheck(t, report, name)
	var typed T
	if check.Passed || !errors.As(check.Err(), &typed) {
		t.Fatalf("%s: want a %T failure, got %+v", name, typed, check)
	}
	if check.Fix == "" || check.Fix != typed.Fix() {
		t.Fatalf("%s: fix %q not carried into the report", name, check.Fix)
	}
	return typed
}

func TestPreflightFailedError_ListsEachFailureWithItsFix(t *testing.T) {
	err := &PreflightFailedError{Host: "agent-01:22", Failures: []error{
		&LingerNotEnabledError{User: "u", Command: "sudo loginctl enable-linger u"},
		&InsufficientDiskError{Path: "/srv", Purpose: purposeWorkspaces, FreeBytes: 1 << 30, RequiredBytes: 10 << 30},
	}}
	msg := err.Error()
	for _, want := range []string{"agent-01:22", "2 checks", "fix: sudo loginctl enable-linger u", "1.0 GiB free", "10.0 GiB"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
	var disk *InsufficientDiskError
	if !errors.As(err, &disk) {
		t.Fatal("errors.As cannot reach an individual failure")
	}
	if one := (&PreflightFailedError{Host: "h", Failures: []error{errClosed}}).Error(); !strings.Contains(one, "(1 check)") {
		t.Fatalf("singular form wrong: %s", one)
	}
}

func TestIsSSHAuthFailure(t *testing.T) {
	if !IsSSHAuthFailure(&SSHDialError{Cause: &stringError{msg: "ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password]"}}) {
		t.Fatal("auth failure not recognised")
	}
	if IsSSHAuthFailure(&SSHDialError{Cause: &stringError{msg: "connection refused"}}) || IsSSHAuthFailure(nil) {
		t.Fatal("non-auth failure recognised as auth")
	}
}

func TestSSHKeyNotAuthorizedError_Fix(t *testing.T) {
	withKey := &SSHKeyNotAuthorizedError{Host: "agent-01", User: "u", AuthorizedKey: "ssh-ed25519 AAAA deploy\n"}
	if withKey.Fix() != "on agent-01 as u, append to ~/.ssh/authorized_keys: ssh-ed25519 AAAA deploy" {
		t.Fatalf("fix = %q", withKey.Fix())
	}
	if !strings.Contains((&SSHKeyNotAuthorizedError{Host: "h", User: "u"}).Fix(), "password") {
		t.Fatal("password deploy fix should name the password")
	}
}
