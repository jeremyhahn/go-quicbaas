package deploy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// localHelperEnv marks the re-executed test binary as the stand-in shell.
const localHelperEnv = "GO_WANT_LOCAL_TARGET_HELPER"

// TestLocalTargetHelperProcess is not a test: it is the shell the
// LocalTarget tests run commands under. Each test re-executes this binary
// in its place, so no test runs /bin/sh or any other host binary.
func TestLocalTargetHelperProcess(t *testing.T) {
	if os.Getenv(localHelperEnv) != "1" {
		return
	}
	switch cmd := os.Args[len(os.Args)-1]; cmd {
	case "succeed":
		fmt.Fprint(os.Stdout, "out")
		fmt.Fprint(os.Stderr, "note")
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "failed: %s\n", cmd)
		os.Exit(3)
	}
}

// helperTarget returns a LocalTarget whose shell is this test binary,
// with home at a temporary directory.
func helperTarget(t *testing.T) *LocalTarget {
	t.Helper()
	t.Setenv(localHelperEnv, "1")
	return &LocalTarget{
		host:  "node01",
		user:  "agent",
		home:  t.TempDir(),
		shell: []string{os.Args[0], "-test.run=^TestLocalTargetHelperProcess$", "--"},
	}
}

func TestLocalTarget_ExecReturnsOutput(t *testing.T) {
	target := helperTarget(t)

	stdout, stderr, err := target.Exec(context.Background(), "succeed")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if stdout != "out" || stderr != "note" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestLocalTarget_ExecFailureIsTyped(t *testing.T) {
	target := helperTarget(t)

	_, stderr, err := target.Exec(context.Background(), "exit-3")

	var execErr *LocalExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want LocalExecError, got %v", err)
	}
	if execErr.Command != "exit-3" || execErr.Stderr != "failed: exit-3" {
		t.Fatalf("error = %+v", execErr)
	}
	if stderr != "failed: exit-3\n" {
		t.Fatalf("stderr = %q", stderr)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("exit status not carried: %v", err)
	}
}

func TestLocalTarget_ExecCancelled(t *testing.T) {
	target := helperTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := target.Exec(ctx, "succeed")

	var execErr *LocalExecError
	if !errors.As(err, &execErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("want LocalExecError wrapping context.Canceled, got %v", err)
	}
}

func TestLocalTarget_WriteFileIsAtomicWithMode(t *testing.T) {
	target := helperTarget(t)
	dir := filepath.Join(t.TempDir(), "a", "b")
	dest := filepath.Join(dir, "agent.yaml")

	if err := target.WriteFile(context.Background(), dest, []byte("first"), 0o600); err != nil {
		t.Fatalf("first write: %v", err)
	}
	before, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.WriteFile(context.Background(), dest, []byte("second"), 0o640); err != nil {
		t.Fatalf("second write: %v", err)
	}

	after, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", after.Mode().Perm())
	}
	if os.SameFile(before, after) {
		t.Fatal("file was rewritten in place; want a new file renamed over it")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "second" {
		t.Fatalf("content = %q, err %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging file left behind: %v (err %v)", entries, err)
	}
}

func TestLocalTarget_WriteFileCreatesPrivateDirectories(t *testing.T) {
	target := helperTarget(t)
	root := t.TempDir()
	// What the process umask leaves of a fully open mode; the created
	// directories may be no wider than installDirMode under it.
	probe := filepath.Join(root, "probe")
	if err := os.Mkdir(probe, 0o777); err != nil {
		t.Fatal(err)
	}
	probeInfo, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}
	want := installDirMode & probeInfo.Mode().Perm()

	if err := target.WriteFile(context.Background(), filepath.Join(root, "a", "b", "agent.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for _, dir := range []string{filepath.Join(root, "a"), filepath.Join(root, "a", "b")} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want || got&0o027 != 0 {
			t.Fatalf("%s mode = %o, want %o", dir, got, want)
		}
	}
}

func TestWriteAtomic_FailedRenameRemovesStagedFile(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory at dest makes the rename fail after the staged
	// file is written and closed.
	dest := filepath.Join(dir, "agent.yaml")
	if err := os.MkdirAll(filepath.Join(dest, "occupied"), 0o700); err != nil {
		t.Fatal(err)
	}

	err := writeAtomic(context.Background(), dest, strings.NewReader("x"), 0o600)

	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) {
		t.Fatalf("want the rename's own error, got %v", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("staged file left behind: %v (err %v)", entries, readErr)
	}
}

func TestLocalTarget_WriteFileExpandsHome(t *testing.T) {
	target := helperTarget(t)

	if err := target.WriteFile(context.Background(), "$HOME/.config/systemd/user/a.service", []byte("unit"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target.home, ".config", "systemd", "user", "a.service")); err != nil {
		t.Fatalf("unit not under home: %v", err)
	}
}

func TestLocalTarget_WriteFileErrorIsTyped(t *testing.T) {
	target := helperTarget(t)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(blocker, "child")

	err := target.WriteFile(context.Background(), dest, []byte("x"), 0o644)

	var writeErr *LocalWriteError
	if !errors.As(err, &writeErr) || writeErr.Path != dest {
		t.Fatalf("want LocalWriteError for %s, got %v", dest, err)
	}
}

func TestLocalTarget_WriteFileCancelledWritesNothing(t *testing.T) {
	target := helperTarget(t)
	dest := filepath.Join(t.TempDir(), "agent.yaml")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := target.WriteFile(ctx, dest, []byte("x"), 0o644)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if _, statErr := os.Stat(dest); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("cancelled write left %s: %v", dest, statErr)
	}
}

func TestLocalTarget_UploadInstallsExecutable(t *testing.T) {
	target := helperTarget(t)
	src := filepath.Join(t.TempDir(), "quicbaas-static")
	if err := os.WriteFile(src, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := target.Upload(context.Background(), src, "$HOME/.local/bin/quicbaas"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	dest := filepath.Join(target.home, ".local", "bin", "quicbaas")
	info, err := os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed binary: %v, err %v", info, err)
	}
}

func TestLocalTarget_UploadMissingSourceIsTyped(t *testing.T) {
	target := helperTarget(t)
	src := filepath.Join(t.TempDir(), "missing")

	err := target.Upload(context.Background(), src, filepath.Join(t.TempDir(), "quicbaas"))

	var writeErr *LocalWriteError
	if !errors.As(err, &writeErr) || writeErr.Source != src || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want LocalWriteError naming %s, got %v", src, err)
	}
}

func TestLocalTarget_DialTCP(t *testing.T) {
	target := helperTarget(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()

	conn, err := target.DialTCP(context.Background(), addr)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := target.DialTCP(context.Background(), addr); err == nil {
		t.Fatal("dial to a closed listener succeeded")
	}
}

func TestLocalTarget_ExpandHome(t *testing.T) {
	target := &LocalTarget{home: "/home/agent"}
	cases := map[string]string{
		"$HOME":               "/home/agent",
		"$HOME/.local/bin/q":  "/home/agent/.local/bin/q",
		"/opt/quicbaas":       "/opt/quicbaas",
		"$HOMEWARD/elsewhere": "$HOMEWARD/elsewhere",
	}
	for in, want := range cases {
		if got := target.expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewLocalTarget_CurrentUser(t *testing.T) {
	target, err := NewLocalTarget("node01")
	if err != nil {
		t.Fatalf("NewLocalTarget: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if target.Host() != "node01" || target.User() == "" || target.home != home {
		t.Fatalf("target = %+v", target)
	}
}

// stubCurrentUser replaces the user lookup for one test.
func stubCurrentUser(t *testing.T, lookup func() (*user.User, error)) {
	t.Helper()
	original := lookupCurrentUser
	lookupCurrentUser = lookup
	t.Cleanup(func() { lookupCurrentUser = original })
}

// A uid with no passwd entry (the toolchain image runs as any non-root uid)
// names the target user by its uid instead of failing.
func TestNewLocalTarget_UidWithoutPasswdEntry(t *testing.T) {
	stubCurrentUser(t, func() (*user.User, error) {
		return nil, user.UnknownUserIdError(10001)
	})
	home := t.TempDir()
	t.Setenv("HOME", home)

	target, err := NewLocalTarget("node01")
	if err != nil {
		t.Fatalf("NewLocalTarget: %v", err)
	}
	if target.User() != "10001" || target.home != home {
		t.Fatalf("target = %+v, want user 10001 and home %s", target, home)
	}
}

// A uid that resolves keeps its user name.
func TestNewLocalTarget_ResolvedUserKeepsItsName(t *testing.T) {
	stubCurrentUser(t, func() (*user.User, error) {
		return &user.User{Uid: "1000", Username: "builder"}, nil
	})

	target, err := NewLocalTarget("node01")
	if err != nil {
		t.Fatalf("NewLocalTarget: %v", err)
	}
	if target.User() != "builder" {
		t.Fatalf("User() = %q, want builder", target.User())
	}
}

// Any other lookup failure is still a LocalTargetError.
func TestNewLocalTarget_LookupFailure(t *testing.T) {
	cause := errors.New("nss unavailable")
	stubCurrentUser(t, func() (*user.User, error) { return nil, cause })

	_, err := NewLocalTarget("node01")

	var targetErr *LocalTargetError
	if !errors.As(err, &targetErr) || !errors.Is(err, cause) {
		t.Fatalf("want LocalTargetError wrapping %v, got %v", cause, err)
	}
}

func TestNewLocalTarget_NoHome(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := NewLocalTarget("node01")

	var targetErr *LocalTargetError
	if !errors.As(err, &targetErr) {
		t.Fatalf("want LocalTargetError, got %v", err)
	}
}
