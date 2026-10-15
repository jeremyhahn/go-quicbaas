// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build linux && cgo

package containerruntime

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestLibcontainerEngine_UnknownContainer(t *testing.T) {
	engine := newLibcontainerEngine(t.TempDir())

	_, err := engine.Start("ghost", processSpec{Args: []string{"/bin/true"}})
	require.ErrorIs(t, err, ErrContainerNotFound)
	_, err = engine.Exec("ghost", processSpec{Args: []string{"/bin/true"}})
	require.ErrorIs(t, err, ErrContainerNotFound)
	require.ErrorIs(t, engine.Destroy("ghost"), ErrContainerNotFound)
}

func TestLibcontainerEngine_CreateRejectsAConfigWithoutRoot(t *testing.T) {
	engine := newLibcontainerEngine(t.TempDir())

	err := engine.Create("no-root", &ociConfig{OCIVersion: "1.0.2", Process: &ociProcess{Args: []string{"/bin/true"}}})
	require.Error(t, err)
	_, err = engine.Start("no-root", processSpec{Args: []string{"/bin/true"}})
	require.ErrorIs(t, err, ErrContainerNotFound)
}

func TestLibcontainerProcess_NotStarted(t *testing.T) {
	console, err := os.CreateTemp(t.TempDir(), "console")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, console.Close()) })
	process := &libcontainerProcess{
		process: toLibcontainerProcess(processSpec{Args: []string{"/bin/true"}}, true),
		console: console,
	}

	assert.Zero(t, process.Pid())
	_, err = process.Wait()
	require.Error(t, err)
	require.Error(t, process.Signal(unix.SIGTERM))
	require.Error(t, killProcess(process.process))
	assert.Same(t, console, process.Console())
}

func TestToRuntimeSpec(t *testing.T) {
	oci := &ociConfig{
		OCIVersion: "1.0.2",
		Hostname:   "box",
		Root:       &ociRoot{Path: "/rootfs", Readonly: true},
		Process: &ociProcess{
			Args: []string{"/app"},
			Env:  []string{"A=b"},
			Cwd:  "/srv",
			User: ociUser{UID: 1000, GID: 2000, AdditionalGids: []uint32{44}},
		},
	}

	spec, err := toRuntimeSpec(oci)
	require.NoError(t, err)
	assert.Equal(t, "box", spec.Hostname)
	assert.Equal(t, "/rootfs", spec.Root.Path)
	assert.True(t, spec.Root.Readonly)
	assert.Equal(t, []string{"/app"}, spec.Process.Args)
	assert.Equal(t, []string{"A=b"}, spec.Process.Env)
	assert.Equal(t, "/srv", spec.Process.Cwd)
	assert.Equal(t, uint32(1000), spec.Process.User.UID)
	assert.Equal(t, uint32(2000), spec.Process.User.GID)
	assert.Equal(t, []uint32{44}, spec.Process.User.AdditionalGids)
}

func TestToRuntimeSpec_Empty(t *testing.T) {
	spec, err := toRuntimeSpec(&ociConfig{})
	require.NoError(t, err)
	assert.Nil(t, spec.Process)
	assert.Nil(t, spec.Root)
}

func TestToLibcontainerProcess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	spec := processSpec{
		Args:   []string{"/app"},
		Env:    []string{"A=b"},
		Cwd:    "/srv",
		User:   ociUser{UID: 1000, GID: 2000, AdditionalGids: []uint32{44, 29}},
		Stdout: &stdout,
		Stderr: &stderr,
	}

	process := toLibcontainerProcess(spec, true)
	assert.Equal(t, []string{"/app"}, process.Args)
	assert.Equal(t, []string{"A=b"}, process.Env)
	assert.Equal(t, "/srv", process.Cwd)
	assert.Equal(t, 1000, process.UID)
	assert.Equal(t, 2000, process.GID)
	assert.Equal(t, []int{44, 29}, process.AdditionalGroups)
	assert.True(t, process.Init)
	assert.Same(t, &stdout, process.Stdout)
	assert.Same(t, &stderr, process.Stderr)
}

func TestToLibcontainerProcess_TTYLeavesTheStreamsToTheConsole(t *testing.T) {
	var stdout bytes.Buffer
	process := toLibcontainerProcess(processSpec{Args: []string{"/bin/sh"}, TTY: true, Stdout: &stdout}, false)

	assert.Nil(t, process.Stdout)
	assert.Nil(t, process.Stderr)
	assert.False(t, process.Init)
	assert.Empty(t, process.AdditionalGroups)
}

func TestReceiveConsole(t *testing.T) {
	local, remote := socketPair(t)
	console, err := os.CreateTemp(t.TempDir(), "console")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, console.Close()) })

	rights := unix.UnixRights(int(console.Fd()))
	require.NoError(t, unix.Sendmsg(int(remote.Fd()), []byte("/dev/pts/7"), rights, nil, 0))

	received, err := receiveConsole(local)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, received.Close()) })
	assert.Equal(t, "/dev/pts/7", received.Name())
}

func TestReceiveConsole_WithoutAFileDescriptor(t *testing.T) {
	local, remote := socketPair(t)
	require.NoError(t, unix.Sendmsg(int(remote.Fd()), []byte("/dev/pts/7"), nil, nil, 0))

	_, err := receiveConsole(local)
	require.ErrorIs(t, err, ErrConsoleNotReceived)
}

func TestReceiveConsole_WithTwoFileDescriptors(t *testing.T) {
	local, remote := socketPair(t)
	first, err := os.CreateTemp(t.TempDir(), "first")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, first.Close()) })

	rights := unix.UnixRights(int(first.Fd()), int(first.Fd()))
	require.NoError(t, unix.Sendmsg(int(remote.Fd()), []byte("/dev/pts/7"), rights, nil, 0))

	_, err = receiveConsole(local)
	require.ErrorIs(t, err, ErrConsoleNotReceived)
}

func TestReceiveConsole_ClosedSocket(t *testing.T) {
	local, _ := socketPair(t)
	require.NoError(t, local.Close())

	_, err := receiveConsole(local)
	require.Error(t, err)
}

// socketPair returns a connected pair of Unix sockets, closed at cleanup.
func socketPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	local := os.NewFile(uintptr(fds[0]), "local")
	remote := os.NewFile(uintptr(fds[1]), "remote")
	t.Cleanup(func() {
		closeQuietly(t, local)
		closeQuietly(t, remote)
	})
	return local, remote
}

// closeQuietly closes f, accepting that the test may have closed it already.
func closeQuietly(t *testing.T, f *os.File) {
	t.Helper()
	if err := f.Close(); err != nil {
		require.ErrorIs(t, err, os.ErrClosed)
	}
}

func TestCloseIfSet(t *testing.T) {
	require.NoError(t, closeIfSet(nil))

	f, err := os.Create(filepath.Join(t.TempDir(), "file"))
	require.NoError(t, err)
	require.NoError(t, closeIfSet(f))
	require.ErrorIs(t, closeIfSet(f), os.ErrClosed)
}

// exitStatusError is an error that carries a process exit code.
type exitStatusError struct{ code int }

func (e *exitStatusError) Error() string { return "exit status" }
func (e *exitStatusError) ExitCode() int { return e.code }

func TestIsExitStatus(t *testing.T) {
	assert.True(t, isExitStatus(&exitStatusError{code: 5}))
	assert.True(t, isExitStatus(errors.Join(errTestBackend, &exitStatusError{code: 1})))
	assert.False(t, isExitStatus(errTestBackend))
	assert.False(t, isExitStatus(nil))
}

func TestIsContainerInit(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		initPipe string
		want     bool
	}{
		{"libcontainer re-exec", []string{"quicbaas", "init"}, "3", true},
		{"init command without a pipe", []string{"quicbaas", "init"}, "", false},
		{"other command", []string{"quicbaas", "serve"}, "3", false},
		{"no command", []string{"quicbaas"}, "3", false},
		{"no args", nil, "3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isContainerInit(tt.args, tt.initPipe))
		})
	}
}

func TestRunContainerInit_NotAnInitProcess(t *testing.T) {
	t.Setenv(containerInitPipeEnv, "")
	assert.False(t, RunContainerInit())
}
