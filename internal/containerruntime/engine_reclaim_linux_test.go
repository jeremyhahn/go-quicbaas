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

//go:build linux && cgo

package containerruntime

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/opencontainers/cgroups"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestLibcontainerEngine_RegistersTheCgroupDeviceHandlers pins the import
// that fills the cgroup managers' device hooks. specconv gives every
// container default device rules, and both managers refuse those rules with
// ErrDevicesUnsupported while the hooks are nil, so without it every start
// fails after the runtime has initialised.
func TestLibcontainerEngine_RegistersTheCgroupDeviceHandlers(t *testing.T) {
	require.NotNil(t, cgroups.DevicesSetV1, "cgroup v1 device handler is not registered")
	require.NotNil(t, cgroups.DevicesSetV2, "cgroup v2 device handler is not registered")
}

// killProcessTree kills a real shell and the child it backgrounded, so the
// child no longer holds the shell's output pipe and Wait returns.
func TestKillProcessTree_KillsAShellAndItsChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 600 & wait")
	var out bytes.Buffer
	cmd.Stdout = &out
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	require.Eventually(t, func() bool { return len(processChildren(cmd.Process.Pid)) > 0 },
		5*time.Second, 10*time.Millisecond, "the shell never started its child")

	require.NoError(t, killProcessTree(cmd.Process.Pid))
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("the shell's child still holds its output pipe")
	}
}

// killRecorder is a reapGuard kill that records every PID it signals.
type killRecorder struct {
	mu    sync.Mutex
	calls []int
}

func (r *killRecorder) kill(pid int, sig unix.Signal) error {
	r.mu.Lock()
	r.calls = append(r.calls, pid)
	r.mu.Unlock()
	return unix.Kill(pid, sig)
}

func (r *killRecorder) recorded() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.calls...)
}

func TestReapGuard_KillTreeAfterTheReapSignalsNoPID(t *testing.T) {
	var rec killRecorder
	g := &reapGuard{kill: rec.kill}
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	state, err := g.reap(cmd.Process.Pid, cmd.Process.Wait)
	require.NoError(t, err)
	require.Equal(t, 0, state.ExitCode())

	// Cancellation after the leader exited: its PID may be reused.
	require.NoError(t, g.killTree(cmd.Process.Pid, cmd.Process.Signal))
	require.Empty(t, rec.recorded())
}

func TestReapGuard_KillTreeBeforeTheReapKillsTheTree(t *testing.T) {
	var rec killRecorder
	g := &reapGuard{kill: rec.kill}
	cmd := exec.Command("sh", "-c", "sleep 600 & wait")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	pid := cmd.Process.Pid
	require.Eventually(t, func() bool { return len(processChildren(pid)) > 0 },
		5*time.Second, 10*time.Millisecond, "the shell never started its child")
	children := processChildren(pid)

	type reaped struct {
		state *os.ProcessState
		err   error
	}
	done := make(chan reaped, 1)
	go func() {
		state, err := g.reap(pid, cmd.Process.Wait)
		done <- reaped{state, err}
	}()

	require.NoError(t, g.killTree(pid, cmd.Process.Signal))
	calls := rec.recorded()
	require.Contains(t, calls, pid)
	for _, child := range children {
		require.Contains(t, calls, child)
	}

	select {
	case r := <-done:
		require.NoError(t, r.err)
		status, ok := r.state.Sys().(syscall.WaitStatus)
		require.True(t, ok)
		require.Equal(t, syscall.SIGKILL, status.Signal())
	case <-time.After(5 * time.Second):
		t.Fatal("the killed shell was never reaped")
	}
}

func TestReapGuard_KillTreeOfAnExitedUnreapedLeaderIsSafe(t *testing.T) {
	var rec killRecorder
	g := &reapGuard{kill: rec.kill}
	cmd := exec.Command("true")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	pid := cmd.Process.Pid

	// The leader is a zombie: its PID is still pinned.
	require.NoError(t, waitExited(pid))
	require.NoError(t, g.killTree(pid, cmd.Process.Signal))
	before := len(rec.recorded())

	state, err := g.reap(pid, cmd.Process.Wait)
	require.NoError(t, err)
	require.Equal(t, 0, state.ExitCode())

	require.NoError(t, g.killTree(pid, cmd.Process.Signal))
	require.Len(t, rec.recorded(), before)
}

func TestReapGuard_NotStartedSignalsThroughTheHandle(t *testing.T) {
	var rec killRecorder
	g := &reapGuard{kill: rec.kill}
	sentinel := errors.New("not started")
	var sent []os.Signal
	err := g.killTree(0, func(sig os.Signal) error {
		sent = append(sent, sig)
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, []os.Signal{unix.SIGKILL}, sent)
	require.Empty(t, rec.recorded())
}

func TestNewExecWorkload_CreatesASubCgroupUnderTheBase(t *testing.T) {
	base := t.TempDir()

	dir, err := newExecWorkload(base)
	require.NoError(t, err)
	require.DirExists(t, dir)
	require.Equal(t, base, filepath.Dir(dir))
	require.True(t, strings.HasPrefix(filepath.Base(dir), execWorkloadPrefix), dir)

	other, err := newExecWorkload(base)
	require.NoError(t, err)
	require.NotEqual(t, dir, other)

	none, err := newExecWorkload("")
	require.NoError(t, err)
	require.Empty(t, none)

	_, err = newExecWorkload(filepath.Join(base, "missing"))
	require.Error(t, err)
}

func TestNewKillableExecWorkload_RefusesWhatCannotBeKilled(t *testing.T) {
	// No cgroup v2 base (not unified, or no container cgroup).
	dir, err := newKillableExecWorkload("")
	require.ErrorIs(t, err, ErrExecNotKillable)
	require.Empty(t, dir)

	// A plain directory: the sub-directory it makes has no cgroup.kill, as
	// on Linux before 5.14, and is removed again.
	base := t.TempDir()
	dir, err = newKillableExecWorkload(base)
	require.ErrorIs(t, err, ErrExecNotKillable)
	require.Empty(t, dir)
	entries, err := os.ReadDir(base)
	require.NoError(t, err)
	require.Empty(t, entries)

	// The sub-cgroup cannot be created.
	_, err = newKillableExecWorkload(filepath.Join(base, "missing"))
	require.ErrorIs(t, err, ErrExecNotKillable)
}

func TestLibcontainerProcess_KillTreeKillsTheWorkloadAfterTheLeaderIsReaped(t *testing.T) {
	dir := t.TempDir()
	kill := filepath.Join(dir, "cgroup.kill")
	require.NoError(t, os.WriteFile(kill, nil, 0o600))

	p := &libcontainerProcess{
		process:  toLibcontainerProcess(processSpec{Args: []string{"/bin/true"}}, false),
		workload: dir,
	}
	// Cancellation after Wait has reaped the leader.
	p.guard.reaped = true

	require.NoError(t, p.KillTree())
	got, err := os.ReadFile(kill)
	require.NoError(t, err)
	require.Equal(t, "1", string(got))

	require.NoError(t, os.WriteFile(kill, nil, 0o600))
	require.NoError(t, p.Release())
	got, err = os.ReadFile(kill)
	require.NoError(t, err)
	require.Equal(t, "1", string(got))
}

func TestLibcontainerProcess_KillTreeWithoutCgroupKillFallsBackToThePIDWalk(t *testing.T) {
	for _, workload := range []string{t.TempDir(), filepath.Join(t.TempDir(), "gone")} {
		p := &libcontainerProcess{
			process:  toLibcontainerProcess(processSpec{Args: []string{"/bin/true"}}, false),
			workload: workload,
		}
		p.guard.reaped = true

		// The guard signals no reaped PID.
		require.NoError(t, p.KillTree(), workload)
	}
}

func TestKillWorkload_StopsTheDescendantsOfAReapedLeader(t *testing.T) {
	if os.Geteuid() != 0 || !cgroups.IsCgroup2UnifiedMode() {
		t.Skip("needs root on the cgroup v2 unified hierarchy")
	}
	own, err := cgroupDirForPID(os.Getpid())
	if err != nil {
		t.Skipf("own cgroup: %v", err)
	}
	dir, err := newExecWorkload(own)
	if err != nil {
		t.Skipf("sub-cgroup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cgroup.kill")); err != nil {
		_ = unix.Rmdir(dir)
		t.Skipf("no cgroup.kill: %v", err)
	}
	t.Cleanup(func() {
		_ = killWorkload(dir)
		deadline := time.Now().Add(5 * time.Second)
		for unix.Rmdir(dir) != nil && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	})

	fd, err := os.OpenFile(dir, unix.O_PATH|unix.O_DIRECTORY, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fd.Close() })
	marker := filepath.Join(t.TempDir(), "late")

	cmd := exec.Command("sh", "-c", "(sleep 1; touch \"$1\") & exit", "sh", marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(fd.Fd())}
	// The leader exits at once and is reaped; its subshell keeps running.
	require.NoError(t, cmd.Run())

	procs := filepath.Join(dir, "cgroup.procs")
	got, err := os.ReadFile(procs)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(string(got)))

	p := &libcontainerProcess{
		process:  toLibcontainerProcess(processSpec{Args: []string{"/bin/true"}}, false),
		workload: dir,
	}
	p.guard.reaped = true
	require.NoError(t, p.KillTree())

	require.Eventually(t, func() bool {
		got, err := os.ReadFile(procs)
		return err == nil && strings.TrimSpace(string(got)) == ""
	}, 5*time.Second, 20*time.Millisecond)
	time.Sleep(1500 * time.Millisecond)
	require.NoFileExists(t, marker)
}

// A reclaim retried after an earlier one destroyed the engine state, or of a
// container whose create never reached the engine, finds nothing and is done.
func TestLibcontainerEngineReclaim_AContainerItDoesNotKnowIsAlreadyReclaimed(t *testing.T) {
	engine := newLibcontainerEngine(t.TempDir())
	require.NoError(t, engine.Reclaim("0123456789abcdef"))
}
