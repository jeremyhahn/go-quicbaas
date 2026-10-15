package deploy

import (
	"context"
	"errors"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
)

// recordingTarget is a Target that runs nothing: it records every command,
// file and upload, answers the linger probe, and fails any command that
// contains failOn. It proves the install primitives need only the Target
// seam, whichever transport stands behind it.
type recordingTarget struct {
	host     string
	user     string
	failOn   string
	commands []string
	files    map[string]recordedFile
	uploads  map[string]string
}

type recordedFile struct {
	data string
	mode os.FileMode
}

func newRecordingTarget() *recordingTarget {
	return &recordingTarget{
		host:    "node01",
		user:    "agent",
		files:   map[string]recordedFile{},
		uploads: map[string]string{},
	}
}

func (r *recordingTarget) Host() string { return r.host }
func (r *recordingTarget) User() string { return r.user }

func (r *recordingTarget) Exec(_ context.Context, cmd string) (string, string, error) {
	r.commands = append(r.commands, cmd)
	if r.failOn != "" && strings.Contains(cmd, r.failOn) {
		return "", "boom", &RemoteExecError{Command: cmd, Stderr: "boom"}
	}
	if strings.HasPrefix(cmd, "loginctl show-user") {
		return "Linger=yes\n", "", nil
	}
	return "", "", nil
}

func (r *recordingTarget) WriteFile(_ context.Context, path string, data []byte, mode os.FileMode) error {
	r.files[path] = recordedFile{data: string(data), mode: mode}
	return nil
}

func (r *recordingTarget) Upload(_ context.Context, localPath, path string) error {
	r.uploads[path] = localPath
	return nil
}

func (r *recordingTarget) DialTCP(context.Context, string) (net.Conn, error) {
	return nil, errors.New("recordingTarget does not dial")
}

func TestInstallAndStart_OnTarget(t *testing.T) {
	target := newRecordingTarget()
	unit := RenderUnit(DefaultSpec())

	if err := InstallAndStart(context.Background(), target, unit, "quicbaas-agent.service"); err != nil {
		t.Fatalf("InstallAndStart: %v", err)
	}

	want := []string{
		`mkdir -p "$HOME"'/.config/systemd/user'`,
		"systemctl --user daemon-reload",
		"systemctl --user enable 'quicbaas-agent.service'",
		"loginctl show-user 'agent' -p Linger",
		"systemctl --user restart 'quicbaas-agent.service'",
	}
	if !reflect.DeepEqual(target.commands, want) {
		t.Fatalf("commands:\n got %q\nwant %q", target.commands, want)
	}
	file, ok := target.files["$HOME/.config/systemd/user/quicbaas-agent.service"]
	if !ok {
		t.Fatalf("unit not written; files: %v", target.files)
	}
	if file.data != unit || file.mode != 0o644 {
		t.Fatalf("unit written with mode %o, content match %v", file.mode, file.data == unit)
	}
}

func TestInstallAndStart_OnTargetFailureNamesStage(t *testing.T) {
	target := newRecordingTarget()
	target.failOn = "daemon-reload"

	err := InstallAndStart(context.Background(), target, "unit", "quicbaas-agent.service")

	var installErr *SystemdInstallError
	if !errors.As(err, &installErr) || installErr.Stage != "daemon-reload" {
		t.Fatalf("want SystemdInstallError at daemon-reload, got %v", err)
	}
	if last := target.commands[len(target.commands)-1]; last != "systemctl --user daemon-reload" {
		t.Fatalf("install kept going after the failure: last command %q", last)
	}
}

func TestStartBackground_OnTarget(t *testing.T) {
	target := newRecordingTarget()
	script := RenderStartScript(DefaultSpec())

	if err := StartBackground(context.Background(), target, script, "/home/agent/.local/bin/start.sh"); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}

	want := []string{
		"mkdir -p '/home/agent/.local/bin'",
		"sh '/home/agent/.local/bin/start.sh'",
	}
	if !reflect.DeepEqual(target.commands, want) {
		t.Fatalf("commands:\n got %q\nwant %q", target.commands, want)
	}
	file := target.files["/home/agent/.local/bin/start.sh"]
	if file.data != script || file.mode != 0o755 {
		t.Fatalf("script written with mode %o, content match %v", file.mode, file.data == script)
	}
}

func TestStartBackground_OnTargetRunFailure(t *testing.T) {
	target := newRecordingTarget()
	target.failOn = "sh '"

	err := StartBackground(context.Background(), target, "#!/bin/sh\n", "/opt/start.sh")

	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %v", err)
	}
}

func TestUploadBinary_DelegatesToTarget(t *testing.T) {
	target := newRecordingTarget()

	if err := UploadBinary(context.Background(), target, "bin/quicbaas-static", "/home/agent/.local/bin/quicbaas"); err != nil {
		t.Fatalf("UploadBinary: %v", err)
	}
	if got := target.uploads["/home/agent/.local/bin/quicbaas"]; got != "bin/quicbaas-static" {
		t.Fatalf("upload source = %q", got)
	}
}

func TestUploadBinary_NilTarget(t *testing.T) {
	err := UploadBinary(context.Background(), nil, "a", "b")

	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %v", err)
	}
}

func TestEnsureLinger_OnTargetAlreadyLingering(t *testing.T) {
	target := newRecordingTarget()

	if err := EnsureLinger(context.Background(), target, "agent"); err != nil {
		t.Fatalf("EnsureLinger: %v", err)
	}
	if len(target.commands) != 1 {
		t.Fatalf("a lingering user needs only the probe; ran %q", target.commands)
	}
}
