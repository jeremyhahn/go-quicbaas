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

package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// settleTrainCars records into settled, by pull request, every entry that
// is landed or ejected, and every car whose entry is gone and whose pull
// request is closed, as landed without a merge SHA. It reports whether every
// car is settled. The landed entry is transient: Hooks.Complete deletes it
// last, after the pull request is closed, so a car that completes between
// two polls is never seen landed. The closed pull request is durable.
func settleTrainCars(settled map[int]*sdlc.TrainEntry, cars []int, entries []*sdlc.TrainEntry, closed func(pr int) (bool, error)) (bool, error) {
	present := map[int]bool{}
	for _, entry := range entries {
		present[entry.PullRequest] = true
		if entry.State == sdlc.TrainEntryLanded || entry.State == sdlc.TrainEntryEjected {
			settled[entry.PullRequest] = entry
		}
	}
	done := true
	for _, car := range cars {
		if settled[car] != nil {
			continue
		}
		if present[car] {
			done = false
			continue
		}
		isClosed, err := closed(car)
		if err != nil {
			return false, err
		}
		if !isClosed {
			done = false
			continue
		}
		settled[car] = &sdlc.TrainEntry{PullRequest: car, State: sdlc.TrainEntryLanded}
	}
	return done, nil
}

func TestSettleTrainCars_ACarCompletedBetweenPollsCountsAsLanded(t *testing.T) {
	settled := map[int]*sdlc.TrainEntry{}
	cars := []int{1, 2}
	closedCars := map[int]bool{}
	closed := func(pr int) (bool, error) { return closedCars[pr], nil }

	done, err := settleTrainCars(settled, cars, []*sdlc.TrainEntry{
		{PullRequest: 1, State: sdlc.TrainEntryRiding},
		{PullRequest: 2, State: sdlc.TrainEntryRiding},
	}, closed)
	require.NoError(t, err)
	require.False(t, done, "riding cars are not settled")
	require.Empty(t, settled)

	// Car 1 landed and completed between the polls: its entry is gone and
	// its pull request is closed. Car 2 is still seen landed.
	closedCars[1] = true
	landed := &sdlc.TrainEntry{PullRequest: 2, State: sdlc.TrainEntryLanded, MergeSHA: "m"}
	done, err = settleTrainCars(settled, cars, []*sdlc.TrainEntry{landed}, closed)
	require.NoError(t, err)
	require.True(t, done, "a closed car whose entry is gone has landed")
	require.Equal(t, sdlc.TrainEntryLanded, settled[1].State)
	require.Empty(t, settled[1].MergeSHA, "a car settled from its pull request carries no merge SHA")
	require.Same(t, landed, settled[2])
}

func TestSettleTrainCars_AnOpenCarWithoutAnEntryIsNotSettled(t *testing.T) {
	settled := map[int]*sdlc.TrainEntry{}
	done, err := settleTrainCars(settled, []int{1}, nil, func(int) (bool, error) { return false, nil })
	require.NoError(t, err)
	require.False(t, done)
	require.Empty(t, settled)
}

func TestSettleTrainCars_AnEjectedEntryIsKept(t *testing.T) {
	settled := map[int]*sdlc.TrainEntry{}
	ejected := &sdlc.TrainEntry{PullRequest: 3, State: sdlc.TrainEntryEjected}
	done, err := settleTrainCars(settled, []int{3}, []*sdlc.TrainEntry{ejected}, func(pr int) (bool, error) {
		t.Fatalf("closed asked for car %d, which has an ejected entry", pr)
		return false, nil
	})
	require.NoError(t, err)
	require.True(t, done)
	require.Same(t, ejected, settled[3])
}
