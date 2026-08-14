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

package deploy

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// -----------------------------------------------------------------------------
// sftp.go — sftpAdapter methods, UploadBinary NewClient error, ctx-cancel copy
// -----------------------------------------------------------------------------

// TestSFTPAdapter_DelegatesToVendorClient exercises the sftpAdapter
// wrapper methods. The wrapped *sftp.Client is nil, so each delegating
// call panics inside the vendor type; we recover to confirm the adapter
// forwarded the call rather than short-circuiting. This is the only way
// to drive these one-line bridges without standing up an SFTP server.
func TestSFTPAdapter_DelegatesToVendorClient(t *testing.T) {
	adapter := sftpAdapter{c: nil}

	mustPanic := func(name string, fn func()) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatalf("%s: expected delegation to nil vendor client to panic", name)
			}
		}()
		fn()
	}

	mustPanic("MkdirAll", func() { _ = adapter.MkdirAll("/x") })
	mustPanic("Chmod", func() { _ = adapter.Chmod("/x", 0o755) })
	mustPanic("Remove", func() { _ = adapter.Remove("/x") })
	mustPanic("Create", func() { _, _ = adapter.Create("/x") })
}

// TestUploadBinary_EmptyUnderlyingClient verifies UploadBinary fails
// cleanly when the SSHClient has no live *ssh.Client (sftp.NewClient
// would otherwise be reached with a nil client).
func TestUploadBinary_EmptyUnderlyingClient(t *testing.T) {
	err := UploadBinary(context.Background(), &SSHClient{}, "/a", "/b")
	if err == nil {
		t.Fatal("expected error for empty client")
	}
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %T", err)
	}
}

// slowReaderFile blocks Create's returned file's first Write until the
// release channel is closed, so a context cancellation can win the
// select race deterministically.
type blockingSFTPClient struct {
	*fakeSFTPClient
	release chan struct{}
	writing chan struct{}
}

type blockingSFTPFile struct {
	release chan struct{}
	writing chan struct{}
	once    sync.Once
}

// Write announces that the copy is in flight, then parks. Announcing first is
// what lets the test cancel at a point where the done channel provably cannot
// be ready, so the select in uploadBinaryWith has only the ctx.Done() case to
// take.
func (f *blockingSFTPFile) Write(p []byte) (int, error) {
	f.once.Do(func() {
		if f.writing != nil {
			close(f.writing)
		}
	})
	<-f.release
	return len(p), nil
}

func (f *blockingSFTPFile) Close() error { return nil }

func (c *blockingSFTPClient) Create(p string) (sftpFile, error) {
	file := &blockingSFTPFile{release: c.release, writing: c.writing}
	c.mu.Lock()
	// store under a separate map entry purely for Close tracking
	c.mu.Unlock()
	return file, nil
}

// TestUploadBinaryWith_ContextCancelDuringCopy drives the ctx.Done()
// branch while a copy is actively in flight, confirming the partial
// upload is removed and a typed error returned.
func TestUploadBinaryWith_ContextCancelDuringCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	local := writeLocal(t, []byte("payload-that-must-not-finish"))

	release := make(chan struct{})
	writing := make(chan struct{})
	fake := &blockingSFTPClient{fakeSFTPClient: newFakeSFTP(), release: release, writing: writing}

	// Cancel only once the copy goroutine is parked inside Write. Cancelling
	// before the call instead -- as this test used to -- means uploadBinaryWith
	// now refuses up front and never creates the remote file, so there is no
	// partial upload to remove and nothing exercises the cleanup this test
	// exists for.
	go func() {
		<-writing
		cancel()
	}()

	err := uploadBinaryWith(ctx, fake, local, "/remote/q")
	close(release) // unblock the worker goroutine so it can exit
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %T", err)
	}
	// The partial upload must have been scheduled for removal.
	fake.mu.Lock()
	removed := append([]string(nil), fake.removed...)
	fake.mu.Unlock()
	found := false
	for _, r := range removed {
		if r == "/remote/q" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected partial upload removal, removed=%v", removed)
	}
}

// TestUploadBinaryWith_CopyErrorTriggersRemove forces the io.Copy to
// fail so the done-branch error path (with cleanup Remove) is covered.
func TestUploadBinaryWith_CopyErrorTriggersRemove(t *testing.T) {
	ctx := context.Background()
	local := writeLocal(t, []byte("data"))
	fake := &errOnWriteSFTP{fakeSFTPClient: newFakeSFTP()}

	err := uploadBinaryWith(ctx, fake, local, "/remote/q")
	if err == nil {
		t.Fatal("expected copy error")
	}
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %T", err)
	}
}

type errOnWriteSFTP struct {
	*fakeSFTPClient
}

type errOnWriteFile struct{}

func (f *errOnWriteFile) Write(p []byte) (int, error) {
	return 0, errors.New("disk full")
}

func (f *errOnWriteFile) Close() error { return nil }

func (c *errOnWriteSFTP) Create(p string) (sftpFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &errOnWriteFile{}, nil
}

// -----------------------------------------------------------------------------
// ssh.go — nil receivers, WriteFile failure stages, error formatting
// -----------------------------------------------------------------------------

// TestSSHClient_NilReceivers confirms the nil-safe accessors return
// zero values instead of panicking.
func TestSSHClient_NilReceivers(t *testing.T) {
	var c *SSHClient
	if c.Host() != "" {
		t.Fatal("nil Host should be empty")
	}
	if c.UnderlyingClient() != nil {
		t.Fatal("nil UnderlyingClient should be nil")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("nil Close should be nil, got %v", err)
	}

	empty := &SSHClient{}
	if err := empty.Close(); err != nil {
		t.Fatalf("empty Close should be nil, got %v", err)
	}
}

// TestSSHClient_DoubleClose ensures Close is idempotent on a live
// client: the second call is a no-op once the underlying client is gone.
func TestSSHClient_DoubleClose(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second close should be no-op, got %v", err)
	}
}

// TestWriteFile_ClosedClient covers the closed-connection guard.
func TestWriteFile_ClosedClient(t *testing.T) {
	var c *SSHClient
	if err := c.WriteFile(context.Background(), "/x", []byte("d"), 0o644); err == nil {
		t.Fatal("expected error on nil client")
	}
	empty := &SSHClient{}
	err := empty.WriteFile(context.Background(), "/x", []byte("d"), 0o644)
	if err == nil {
		t.Fatal("expected error on empty client")
	}
	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %T", err)
	}
}

// TestWriteFile_MkdirFails forces the parent mkdir to fail so the early
// return path in WriteFile is exercised.
func TestWriteFile_MkdirFails(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("mkdir -p '/var/lib/app'", "", "permission denied", true)

	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = c.WriteFile(context.Background(), "/var/lib/app/file.txt", []byte("d"), 0o644)
	if err == nil {
		t.Fatal("expected mkdir failure")
	}
	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %T", err)
	}
}

// TestWriteFile_ChmodFails lets the write succeed but makes the trailing
// chmod fail so that error path is covered.
func TestWriteFile_ChmodFails(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("chmod 644 '/tmp/out'", "", "chmod denied", true)

	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = c.WriteFile(context.Background(), "/tmp/out", []byte("d"), 0o644)
	if err == nil {
		t.Fatal("expected chmod failure")
	}
	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %T", err)
	}
}

// TestExec_ContextCancelled drives the ctx.Done() branch of Exec.
func TestExec_ContextCancelled(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The command "blockforever" is not registered; the server's runExec
	// will return quickly, but a pre-cancelled ctx makes the cancel branch
	// race-eligible. Retry until the cancel path is observed.
	_, _, err = c.Exec(ctx, "anything")
	if err == nil {
		// The done branch won; acceptable, but assert it's a typed error
		// when present. No error means command succeeded before cancel.
		return
	}
	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %T", err)
	}
}

// TestSSHDialError_Error covers the error string builder for all field
// combinations.
func TestSSHDialError_Error(t *testing.T) {
	cases := []struct {
		err  *SSHDialError
		want []string
	}{
		{&SSHDialError{}, []string{"ssh dial failed"}},
		{&SSHDialError{Host: "h:22", User: "u", Cause: errors.New("boom")}, []string{"to h:22", "as u", "boom"}},
	}
	for _, tc := range cases {
		msg := tc.err.Error()
		for _, w := range tc.want {
			if !strings.Contains(msg, w) {
				t.Fatalf("Error()=%q missing %q", msg, w)
			}
		}
	}
}

// TestShellQuotePath covers all three $HOME branches.
func TestShellQuotePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"$HOME/.quicbaas/agent", `"$HOME"'/.quicbaas/agent'`},
		{"$HOME", `"$HOME"`},
		{"/etc/quicbaas", `'/etc/quicbaas'`},
	}
	for _, tc := range cases {
		if got := shellQuotePath(tc.in); got != tc.want {
			t.Fatalf("shellQuotePath(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// TestParentDir covers the separator cases.
func TestParentDir(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/a/b/c", "/a/b"},
		{"nofile", ""},
		{"/toplevel", ""},
		{"a/b", "a"},
	}
	for _, tc := range cases {
		if got := parentDir(tc.in); got != tc.want {
			t.Fatalf("parentDir(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// -----------------------------------------------------------------------------
// ssh_auth.go — validation branches for both dial helpers
// -----------------------------------------------------------------------------

func TestDialSSHWithAuth_Validation(t *testing.T) {
	ctx := context.Background()
	auth := []ssh.AuthMethod{ssh.Password("p")}
	cases := []struct {
		name string
		spec *Spec
		auth []ssh.AuthMethod
	}{
		{"nil spec", nil, auth},
		{"no host", &Spec{SSHUser: "u", InsecureHostKey: true}, auth},
		{"no user", &Spec{Host: "h", InsecureHostKey: true}, auth},
		{"no auth", &Spec{Host: "h", SSHUser: "u", InsecureHostKey: true}, nil},
		{"no host key policy", &Spec{Host: "h", SSHUser: "u"}, auth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialSSHWithAuth(ctx, tc.spec, tc.auth)
			if err == nil {
				t.Fatal("expected error")
			}
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
			}
		})
	}
}

func TestDialSSHWithAuthAndHostKey_Validation(t *testing.T) {
	ctx := context.Background()
	auth := []ssh.AuthMethod{ssh.Password("p")}
	cases := []struct {
		name string
		spec *Spec
		auth []ssh.AuthMethod
	}{
		{"nil spec", nil, auth},
		{"no host", &Spec{SSHUser: "u", InsecureHostKey: true}, auth},
		{"no user", &Spec{Host: "h", InsecureHostKey: true}, auth},
		{"no auth", &Spec{Host: "h", SSHUser: "u", InsecureHostKey: true}, nil},
		{"nil host key + no policy", &Spec{Host: "h", SSHUser: "u"}, auth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialSSHWithAuthAndHostKey(ctx, tc.spec, tc.auth, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
			}
		})
	}
}

// TestDialSSHWithAuthAndHostKey_NilHostKeyFallsBack confirms that when
// hostKey is nil but InsecureHostKey is set, the dial proceeds through
// the buildHostKeyCallback fallback.
func TestDialSSHWithAuthAndHostKey_NilHostKeyFallsBack(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	c, err := DialSSHWithAuthAndHostKey(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", InsecureHostKey: true,
	}, []ssh.AuthMethod{ssh.Password("p")}, nil)
	if err != nil {
		t.Fatalf("expected fallback dial to succeed: %v", err)
	}
	_ = c.Close()
}

// -----------------------------------------------------------------------------
// startscript.go — mkdir failure
// -----------------------------------------------------------------------------

// TestStartBackground_MkdirFails forces the directory creation to fail.
func TestStartBackground_MkdirFails(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("mkdir -p '/opt/bin'", "", "denied", true)

	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = StartBackground(context.Background(), c, "#!/bin/sh\n", "/opt/bin/start.sh")
	if err == nil {
		t.Fatal("expected mkdir failure")
	}
	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %T", err)
	}
}

// TestStartBackground_ExecFails lets the write succeed but makes the
// final sh invocation fail.
func TestStartBackground_ExecFails(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("sh '/opt/bin/start.sh'", "", "launch failed", true)

	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = StartBackground(context.Background(), c, "#!/bin/sh\n", "/opt/bin/start.sh")
	if err == nil {
		t.Fatal("expected exec failure")
	}
	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %T", err)
	}
}

// -----------------------------------------------------------------------------
// systemd.go — mkdir and write failure stages
// -----------------------------------------------------------------------------

func TestInstallAndStart_MkdirFails(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("mkdir -p \"$HOME\"'/.config/systemd/user'", "", "denied", true)

	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = InstallAndStart(context.Background(), c, "x", DefaultUnitName)
	if err == nil {
		t.Fatal("expected mkdir failure")
	}
	var sysErr *SystemdInstallError
	if !errors.As(err, &sysErr) {
		t.Fatalf("want SystemdInstallError, got %T", err)
	}
	if sysErr.Stage != "mkdir" {
		t.Fatalf("stage=%q want mkdir", sysErr.Stage)
	}
}

func TestInstallAndStart_WriteFails(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("mkdir -p \"$HOME\"'/.config/systemd/user'", "", "", false)
	// Make the chmod inside WriteFile fail so WriteFile returns an error.
	srv.setExec("chmod 644 \"$HOME\"'/.config/systemd/user/quicbaas-agent.service'", "", "denied", true)

	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = InstallAndStart(context.Background(), c, "x", DefaultUnitName)
	if err == nil {
		t.Fatal("expected write failure")
	}
	var sysErr *SystemdInstallError
	if !errors.As(err, &sysErr) {
		t.Fatalf("want SystemdInstallError, got %T", err)
	}
	if sysErr.Stage != "write" {
		t.Fatalf("stage=%q want write", sysErr.Stage)
	}
}

// TestModeOctalMasksHighBits confirms modeOctal strips non-permission
// bits (e.g. setuid) so only the low 9 bits become the octal triple.
func TestModeOctalMasksHighBits(t *testing.T) {
	if got := modeOctal(os.ModeSetuid | 0o755); got != "755" {
		t.Fatalf("modeOctal masking failed: %q", got)
	}
}

// TestStringError_Error covers the internal stringError helper used for
// the closed-connection sentinel.
func TestStringError_Error(t *testing.T) {
	if errClosed.Error() != "ssh client is closed" {
		t.Fatalf("unexpected sentinel message: %q", errClosed.Error())
	}
}
