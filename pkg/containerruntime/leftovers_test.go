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

package containerruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// restartedJobRuntime is the runtime a restarted agent builds over the root
// the earlier one, jr, used: same directories and storage, and an engine
// that has registered nothing.
func restartedJobRuntime(t *testing.T, jr *JobRuntime, doubles *testBackends) (*JobRuntime, *fakeEngine) {
	t.Helper()
	engine := newFakeEngine()
	rt, err := NewRuntime(context.Background(), jr.runtime.config, withBackends(&backends{
		engine: engine,
		images: doubles.images,
		rootfs: doubles.rootfs,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	return NewJobRuntime(rt), engine
}

func bundleExists(t *testing.T, jr *JobRuntime, id string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(jr.runtime.bundlesDir(), id))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	require.NoError(t, err)
	return true
}

// An agent killed while its jobs ran leaves their containers behind. The
// next agent never registered them, yet its sweep reclaims its owner's and
// leaves every other owner's alone.
func TestSweepOwned_ReclaimsWhatAnEarlierAgentLeft(t *testing.T) {
	before, doubles := newTestJobRuntime(t)
	mine := runOwned(t, before, "daemon-a")
	theirs := runOwned(t, before, "daemon-b")

	after, engine := restartedJobRuntime(t, before, doubles)
	sweep, err := after.SweepOwned(t.Context(), "daemon-a")
	require.NoError(t, err)

	assert.Equal(t, []string{mine}, sweep.Removed)
	assert.Equal(t, []string{mine}, engine.reclaimed, "the engine must kill and destroy the leftover")
	assert.Contains(t, doubles.rootfs.removed, mine, "the leftover's root filesystem must go")
	assert.False(t, bundleExists(t, after, mine), "the leftover's bundle must go")
	assert.True(t, bundleExists(t, after, theirs), "another owner's container was touched")

	again, err := after.SweepOwned(t.Context(), "daemon-a")
	require.NoError(t, err)
	assert.Empty(t, again.Removed, "a reclaimed container is gone, not reclaimed twice")
}

// A process's own containers are not leftovers: they are swept through the
// registry, and ListLeftovers names only what no one here registered.
func TestListLeftovers_NamesOnlyWhatThisProcessDidNotCreate(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	id := runOwned(t, jr, "daemon-a")

	own, err := jr.runtime.ListLeftovers()
	require.NoError(t, err)
	assert.Empty(t, own)

	after, _ := restartedJobRuntime(t, jr, doubles)
	leftovers, err := after.runtime.ListLeftovers()
	require.NoError(t, err)
	require.Len(t, leftovers, 1)
	assert.Equal(t, id, leftovers[0].ID)
	assert.Equal(t, "daemon-a", leftovers[0].Labels[orchestration.ContainerOwnerLabel])
}

// A bundle whose create never wrote its record holds nothing that runs and
// nothing that names an owner; the sweep passes over it.
func TestListLeftovers_SkipsABundleWithoutARecord(t *testing.T) {
	jr, _ := newTestJobRuntime(t)
	require.NoError(t, os.MkdirAll(filepath.Join(jr.runtime.bundlesDir(), "half-created"), 0700))

	leftovers, err := jr.runtime.ListLeftovers()
	require.NoError(t, err)
	assert.Empty(t, leftovers)
}

// TestSweepOwned_ReportsAnEnumerationItCouldNotMake: a sweep that cannot read
// what is on disk cannot say it left nothing of its owner's, so it reports
// the failure, naming no container.
func TestSweepOwned_ReportsAnEnumerationItCouldNotMake(t *testing.T) {
	jr, _ := newTestJobRuntime(t)
	bundle := filepath.Join(jr.runtime.bundlesDir(), "unreadable")
	require.NoError(t, os.MkdirAll(bundle, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(bundle, containerRecordFile), []byte("{"), 0600))

	_, err := jr.SweepOwned(t.Context(), "daemon-a")
	var swept *orchestration.ContainerSweepError
	require.ErrorAs(t, err, &swept)
	assert.Empty(t, swept.ContainerID, "an enumeration failure named a container")
}

// An identified leftover that refuses to go is reported by name and kept, so
// that the next sweep tries it again.
func TestSweepOwned_ReportsALeftoverItCouldNotReclaim(t *testing.T) {
	before, doubles := newTestJobRuntime(t)
	mine := runOwned(t, before, "daemon-a")

	after, engine := restartedJobRuntime(t, before, doubles)
	engine.reclaimErr = errors.New("still running after SIGKILL")

	_, err := after.SweepOwned(t.Context(), "daemon-a")
	var swept *orchestration.ContainerSweepError
	require.ErrorAs(t, err, &swept)
	assert.Equal(t, mine, swept.ContainerID)
	assert.True(t, bundleExists(t, after, mine), "a leftover that was not reclaimed lost its record")
}

func TestReclaimLeftover_RefusesAPathOrAContainerOfItsOwn(t *testing.T) {
	jr, doubles := newTestJobRuntime(t)
	own := runOwned(t, jr, "daemon-a")

	for _, id := range []string{"", ".", "..", "../escape", "a/b", own} {
		require.Error(t, jr.runtime.ReclaimLeftover(id), "reclaimed %q", id)
	}
	assert.Empty(t, doubles.engine.reclaimed)
	assert.True(t, bundleExists(t, jr, own))
}
