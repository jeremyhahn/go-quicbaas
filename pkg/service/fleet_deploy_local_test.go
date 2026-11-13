package service

import (
	"context"
	"errors"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet/deploy"
)

// recordingTarget is a deploy.Target that runs nothing: it records every
// command, file and upload the real install adapters send it, and answers
// the linger probe so a systemd install completes.
type recordingTarget struct {
	commands []string
	files    map[string]os.FileMode
	uploads  map[string]string
}

func newRecordingTarget() *recordingTarget {
	return &recordingTarget{files: map[string]os.FileMode{}, uploads: map[string]string{}}
}

func (r *recordingTarget) Host() string { return "node01" }
func (r *recordingTarget) User() string { return "agent" }

func (r *recordingTarget) Exec(_ context.Context, cmd string) (string, string, error) {
	r.commands = append(r.commands, cmd)
	if strings.HasPrefix(cmd, "loginctl show-user") {
		return "Linger=yes\n", "", nil
	}
	return "", "", nil
}

func (r *recordingTarget) WriteFile(_ context.Context, path string, _ []byte, mode os.FileMode) error {
	r.files[path] = mode
	return nil
}

func (r *recordingTarget) Upload(_ context.Context, localPath, path string) error {
	r.uploads[path] = localPath
	return nil
}

func (r *recordingTarget) DialTCP(context.Context, string) (net.Conn, error) {
	return nil, errors.New("recordingTarget does not dial")
}

// installThrough returns a service whose install adapters are the
// production ones, so a deploy drives the deploy primitives against
// whatever target the session carries.
func installThrough(dialer *fakeDialer) *fleetDeployService {
	svc := newTestService(dialer, &fakeUploader{}, &fakeSystemd{}, &fakeNohup{}, nil, time.Unix(1_700_000_000, 0))
	svc.upload = realUploader{}
	svc.files = realFileUploader{}
	svc.systemd = realSystemdInstaller{}
	svc.nohup = realNohupStarter{}
	svc.renderer = realRenderer{}
	return svc
}

// localSpec is validSpec as a --local deploy: this host, no ssh settings.
func localSpec() *deploy.Spec {
	spec := validSpec()
	spec.Local = true
	spec.Host = "node01"
	spec.SSHUser = ""
	spec.SSHPassword = ""
	spec.InsecureHostKey = false
	return spec
}

func TestDeploy_LocalAndSSHRunTheSameInstall(t *testing.T) {
	systemd := true

	sshTarget := newRecordingTarget()
	sshSpec := validSpec()
	sshSpec.Host = "node01"
	sshSpec.Systemd = &systemd
	sshSpec.SkipPreflight = true
	sshSvc := installThrough(&fakeDialer{sess: &sshSessionAdapter{c: sshTarget}})
	if _, err := sshSvc.Deploy(acmeCtx, sshSpec); err != nil {
		t.Fatalf("ssh deploy: %v", err)
	}

	localTarget := newRecordingTarget()
	spec := localSpec()
	spec.Systemd = &systemd
	spec.SkipPreflight = true
	dialer := &fakeDialer{err: errors.New("a local deploy must not dial ssh")}
	localSvc := installThrough(dialer)
	var openedFor string
	localSvc.localTarget = func(host string) (deploy.Target, error) {
		openedFor = host
		return localTarget, nil
	}
	if _, err := localSvc.Deploy(acmeCtx, spec); err != nil {
		t.Fatalf("local deploy: %v", err)
	}

	if dialer.seen != nil {
		t.Fatal("the local deploy dialled ssh")
	}
	if openedFor != "node01" {
		t.Fatalf("local target opened for %q", openedFor)
	}
	if len(sshTarget.commands) == 0 || !strings.Contains(strings.Join(sshTarget.commands, "\n"), "systemctl --user restart") {
		t.Fatalf("the install did not start the unit: %q", sshTarget.commands)
	}
	if !reflect.DeepEqual(localTarget.commands, sshTarget.commands) {
		t.Fatalf("commands differ:\n ssh   %q\n local %q", sshTarget.commands, localTarget.commands)
	}
	if !reflect.DeepEqual(localTarget.files, sshTarget.files) {
		t.Fatalf("files differ:\n ssh   %v\n local %v", sshTarget.files, localTarget.files)
	}
	if !reflect.DeepEqual(localTarget.uploads, sshTarget.uploads) {
		t.Fatalf("uploads differ:\n ssh   %v\n local %v", sshTarget.uploads, localTarget.uploads)
	}
}

func TestDeploy_LocalTargetOpenFailure(t *testing.T) {
	svc := installThrough(&fakeDialer{})
	svc.localTarget = func(string) (deploy.Target, error) {
		return nil, &deploy.LocalTargetError{Cause: errors.New("no home")}
	}
	spec := localSpec()
	spec.SkipPreflight = true

	_, err := svc.Deploy(acmeCtx, spec)

	var targetErr *deploy.LocalTargetError
	if !errors.As(err, &targetErr) {
		t.Fatalf("want LocalTargetError, got %v", err)
	}
}

func TestDeploy_LocalRefusesSSHSettings(t *testing.T) {
	svc := installThrough(&fakeDialer{})
	spec := localSpec()
	spec.SSHPassword = "pw"

	_, err := svc.Deploy(acmeCtx, spec)

	var conflict *deploy.LocalSSHConflictError
	if !errors.As(err, &conflict) || conflict.Field != "ssh_password" {
		t.Fatalf("want LocalSSHConflictError on ssh_password, got %v", err)
	}
}

func TestPreflight_LocalSkipsSSHChecks(t *testing.T) {
	svc := installThrough(&fakeDialer{err: errors.New("a local preflight must not dial ssh")})
	svc.localTarget = func(string) (deploy.Target, error) { return newRecordingTarget(), nil }

	report, err := svc.Preflight(acmeCtx, localSpec())
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	for _, check := range report.Checks {
		switch check.Name {
		case deploy.CheckSSH, deploy.CheckHostKey, deploy.CheckDeployKey:
			t.Fatalf("local preflight ran the ssh check %q", check.Name)
		}
	}
}

func TestPreflight_LocalOpenFailureNamesTheUser(t *testing.T) {
	svc := installThrough(&fakeDialer{})
	svc.localTarget = func(string) (deploy.Target, error) {
		return nil, &deploy.LocalTargetError{Cause: errors.New("no home")}
	}

	report, err := svc.Preflight(acmeCtx, localSpec())

	if err == nil {
		t.Fatal("a preflight that cannot open the target passed")
	}
	if len(report.Checks) != 1 || report.Checks[0].Name != deploy.CheckRemoteUser {
		t.Fatalf("checks = %+v, want one failed %s check", report.Checks, deploy.CheckRemoteUser)
	}
}

// lifecycleVerbs runs each lifecycle verb against svc, keyed by its name.
var lifecycleVerbs = map[string]func(*fleetDeployService, *deploy.Spec) error{
	"stop": func(svc *fleetDeployService, spec *deploy.Spec) error {
		_, err := svc.Stop(context.Background(), spec)
		return err
	},
	"start": func(svc *fleetDeployService, spec *deploy.Spec) error {
		_, err := svc.Start(context.Background(), spec)
		return err
	},
	"undeploy": func(svc *fleetDeployService, spec *deploy.Spec) error {
		_, err := svc.Undeploy(context.Background(), spec, UndeployOptions{})
		return err
	},
}

func TestLifecycle_LocalReachesTheLocalTarget(t *testing.T) {
	for verb, run := range lifecycleVerbs {
		svc, _, _, log := lifecycleService(t)
		dialer := &fakeDialer{err: errors.New("a local " + verb + " must not dial ssh")}
		svc.ssh = dialer
		var openedFor string
		svc.localTarget = func(host string) (deploy.Target, error) {
			openedFor = host
			return newRecordingTarget(), nil
		}

		if err := run(svc, localSpec()); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		if dialer.seen != nil {
			t.Fatalf("%s dialled ssh", verb)
		}
		if openedFor != "node01" {
			t.Fatalf("%s opened the local target for %q", verb, openedFor)
		}
		if !strings.Contains(log.String(), "read-cert") {
			t.Fatalf("%s did not reach the node: %s", verb, log)
		}
	}
}

func TestLifecycle_LocalRefusesSSHSettings(t *testing.T) {
	for verb, run := range lifecycleVerbs {
		svc, _, _, log := lifecycleService(t)
		spec := localSpec()
		spec.SSHUser = "deploy"

		err := run(svc, spec)

		var conflict *deploy.LocalSSHConflictError
		if !errors.As(err, &conflict) || conflict.Field != "ssh_user" {
			t.Fatalf("%s: want LocalSSHConflictError on ssh_user, got %v", verb, err)
		}
		if log.String() != "" {
			t.Fatalf("%s touched the node: %s", verb, log)
		}
	}
}
