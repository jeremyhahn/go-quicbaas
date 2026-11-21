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

// A reclaim retried after an earlier one destroyed the engine state, or of a
// container whose create never reached the engine, finds nothing and is done.
func TestLibcontainerEngineReclaim_AContainerItDoesNotKnowIsAlreadyReclaimed(t *testing.T) {
	engine := newLibcontainerEngine(t.TempDir())
	require.NoError(t, engine.Reclaim("0123456789abcdef"))
}
