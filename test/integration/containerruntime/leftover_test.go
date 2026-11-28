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

//go:build integration && linux && cgo && containers_image_openpgp && exclude_graphdriver_btrfs

package containerruntime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/jeremyhahn/go-quicbaas/internal/containerruntime"
)

// The environment of the leftover-reclaim owner process: the directory that
// holds its runtime, the image it runs and the mode, running or exited.
const (
	leftoverOwnerDirEnv   = "CONTAINERRUNTIME_LEFTOVER_OWNER_DIR"
	leftoverOwnerImageEnv = "CONTAINERRUNTIME_LEFTOVER_OWNER_IMAGE"
	leftoverOwnerModeEnv  = "CONTAINERRUNTIME_LEFTOVER_OWNER_MODE"
)

// leftoverRuntimeConfig is the runtime layout the owner process and the
// reclaiming test share under dir. The caller writes dir/policy.json.
func leftoverRuntimeConfig(dir, registry string) *containerruntime.RuntimeConfig {
	return &containerruntime.RuntimeConfig{
		Root:                filepath.Join(dir, "root"),
		RunRoot:             filepath.Join(dir, "run"),
		TmpDir:              filepath.Join(dir, "tmp"),
		VolumePath:          filepath.Join(dir, "volumes"),
		GraphDriverName:     "vfs",
		InsecureRegistries:  []string{registry},
		SignaturePolicyPath: filepath.Join(dir, "policy.json"),
	}
}

// ownerReport is what the owner process tells the test about its container.
type ownerReport struct {
	ID  string `json:"id"`
	PID int    `json:"pid"`
}

// leftoverCommand is the container command for each owner mode.
func leftoverCommand(mode string) ([]string, error) {
	switch mode {
	case "running":
		return []string{"/bin/sh", "-c", "exec /bin/busybox sleep 600"}, nil
	case "exited":
		return []string{"/bin/sh", "-c", "exit 0"}, nil
	default:
		return nil, fmt.Errorf("unknown owner mode %q", mode)
	}
}

// runLeftoverOwner is the owner process: it starts a container, reports it in
// owner.json and blocks until it is killed. It never closes its runtime or
// removes the container, which is the crash the test simulates.
func runLeftoverOwner() int {
	if err := startLeftoverOwner(); err != nil {
		fmt.Fprintln(os.Stderr, "leftover owner:", err)
		return 2
	}
	select {}
}

func startLeftoverOwner() error {
	dir := os.Getenv(leftoverOwnerDirEnv)
	image := os.Getenv(leftoverOwnerImageEnv)
	mode := os.Getenv(leftoverOwnerModeEnv)
	command, err := leftoverCommand(mode)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	rt, err := containerruntime.NewRuntime(ctx, leftoverRuntimeConfig(dir, os.Getenv(registryEnv)))
	if err != nil {
		return fmt.Errorf("open runtime: %w", err)
	}
	if err := rt.PullImage(ctx, image); err != nil {
		return fmt.Errorf("pull %s: %w", image, err)
	}
	created, err := rt.CreateContainer(ctx, &containerruntime.ContainerConfig{
		Name:    "leftover-" + mode,
		Image:   image,
		Command: command,
	})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if err := rt.StartContainer(ctx, created.ID); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	// The PID is read before an exited container is waited for: the
	// runtime clears it once the init has exited.
	state, err := rt.InspectContainer(ctx, created.ID)
	if err != nil {
		return fmt.Errorf("inspect: %w", err)
	}
	report := ownerReport{ID: state.ID, PID: state.PID}
	if mode == "exited" {
		if _, err := rt.WaitContainer(ctx, created.ID); err != nil {
			return fmt.Errorf("wait: %w", err)
		}
	}

	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "owner.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "owner.json"))
}

// procState is the state field of /proc/<pid>/stat, and whether the process
// exists.
func procState(pid int) (string, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", false
	}
	stat := string(data)
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return "", true
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) == 0 {
		return "", true
	}
	return fields[0], true
}

// requireAlive fails unless pid is a live, non-zombie process.
func requireAlive(t *testing.T, pid int, when string) {
	t.Helper()
	state, ok := procState(pid)
	require.True(t, ok, "init %d is gone %s", pid, when)
	require.NotEqual(t, "Z", state, "init %d is a zombie %s", pid, when)
}

// reclaimAll reclaims every leftover under config, best effort, so a failed
// run leaves no container behind in the runner.
func reclaimAll(config *containerruntime.RuntimeConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	rt, err := containerruntime.NewRuntime(ctx, config)
	if err != nil {
		return
	}
	defer func() { _ = rt.Close() }()
	leftovers, err := rt.ListLeftovers()
	if err != nil {
		return
	}
	for _, leftover := range leftovers {
		_ = rt.ReclaimLeftover(leftover.ID)
	}
}

// waitForOwnerReport waits for the owner's owner.json, failing if the owner
// exits first.
func waitForOwnerReport(t *testing.T, dir string, exited <-chan error, stderr *bytes.Buffer) ownerReport {
	t.Helper()
	path := filepath.Join(dir, "owner.json")
	deadline := time.Now().Add(testTimeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			var report ownerReport
			require.NoError(t, json.Unmarshal(data, &report))
			return report
		}
		require.True(t, errors.Is(err, os.ErrNotExist), "read owner.json: %v", err)
		select {
		case err := <-exited:
			require.FailNow(t, "the owner exited before it reported", "%v\n%s", err, stderr.String())
		case <-time.After(100 * time.Millisecond):
		}
		require.True(t, time.Now().Before(deadline), "no owner.json after %s\n%s", testTimeout, stderr.String())
	}
}

// A container left by a process that was killed with SIGKILL is listed and
// reclaimed by a fresh runtime on the same root: its init process, its
// libcontainer state, its rootfs layer and its bundle are all gone.
func TestReclaimLeftover_ContainerOfAKilledOwner(t *testing.T) {
	for _, mode := range []string{"running", "exited"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.json"), []byte(acceptAnythingPolicy), 0o600))
			config := leftoverRuntimeConfig(dir, registryHost(t))
			image, _ := pushShellImage(t, "containerruntime/leftover-"+mode)

			exe, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.Command(exe, "-test.run=^$")
			cmd.Env = append(os.Environ(),
				leftoverOwnerDirEnv+"="+dir,
				leftoverOwnerImageEnv+"="+image,
				leftoverOwnerModeEnv+"="+mode,
			)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			require.NoError(t, cmd.Start())
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			ownerDone := false
			t.Cleanup(func() {
				if !ownerDone {
					_ = cmd.Process.Signal(syscall.SIGKILL)
					<-exited
				}
				reclaimAll(config)
			})

			report := waitForOwnerReport(t, dir, exited, &stderr)
			require.NotEmpty(t, report.ID)
			require.Positive(t, report.PID)
			if mode == "running" {
				requireAlive(t, report.PID, "before its owner was killed")
			}

			require.NoError(t, cmd.Process.Signal(syscall.SIGKILL))
			<-exited
			ownerDone = true
			if mode == "running" {
				requireAlive(t, report.PID, "after its owner was killed")
			}

			layersDir := filepath.Join(config.Root, "storage", "vfs", "dir")
			before := layerEntries(t, layersDir)
			require.Len(t, before, 2, "the image layer and the container's rootfs")
			stateDir := filepath.Join(config.RunRoot, "libcontainer", report.ID)
			bundleDir := filepath.Join(config.RunRoot, "bundles", report.ID)
			require.DirExists(t, stateDir)
			require.FileExists(t, filepath.Join(bundleDir, "container.json"))

			rt, err := containerruntime.NewRuntime(testContext(t), config)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, rt.Close()) })

			leftovers, err := rt.ListLeftovers()
			require.NoError(t, err)
			require.Len(t, leftovers, 1)
			assert.Equal(t, report.ID, leftovers[0].ID)
			assert.Equal(t, "leftover-"+mode, leftovers[0].Name)
			assert.Equal(t, image, leftovers[0].Image)

			require.NoError(t, rt.ReclaimLeftover(report.ID))

			if mode == "running" {
				// The runner's test binary is PID 1 of its container and
				// reaps no orphans, so the killed init it inherited may stay
				// a zombie; that counts as gone.
				if state, ok := procState(report.PID); ok {
					assert.Equal(t, "Z", state, "init %d still runs after reclaim", report.PID)
				}
				_, _ = unix.Wait4(report.PID, nil, unix.WNOHANG, nil)
			}
			assert.NoDirExists(t, stateDir)
			assert.NoDirExists(t, bundleDir)

			after := layerEntries(t, layersDir)
			require.Len(t, after, 1, "only the image layer is left")
			assert.Contains(t, before, after[0])

			leftovers, err = rt.ListLeftovers()
			require.NoError(t, err)
			assert.Empty(t, leftovers)

			require.NoError(t, rt.ReclaimLeftover(report.ID), "reclaim is idempotent")
		})
	}
}

// layerEntries lists the vfs layer directories under dir.
func layerEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
