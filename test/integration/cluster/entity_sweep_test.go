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

package cluster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entityLookupTimeout bounds one entity lookup, so a request stuck in its
// client's retries costs one lookup rather than the whole poll attempt.
const entityLookupTimeout = 2 * time.Second

// entitySweep remembers, per node, how many entities in order have been
// found there. A poll attempt resumes each node where the previous attempt
// stopped instead of starting over at the first entity, so a sweep of many
// entities makes progress even when no single attempt can finish it.
type entitySweep struct {
	entities int
	next     []int
}

func newEntitySweep(nodes, entities int) *entitySweep {
	return &entitySweep{entities: entities, next: make([]int, nodes)}
}

// advance looks up each node's entities from where that node stopped, giving
// every lookup its own perLookup deadline inside ctx. A miss or an error stops
// that node for this attempt and keeps its progress; the other nodes still
// advance. It reports done once every node has every entity, and otherwise
// the last error seen.
func (s *entitySweep) advance(ctx context.Context, perLookup time.Duration,
	found func(ctx context.Context, node, entity int) (bool, error)) (bool, error) {
	done := true
	var lastErr error
	for node := range s.next {
		for s.next[node] < s.entities {
			lookupCtx, cancel := context.WithTimeout(ctx, perLookup)
			ok, err := found(lookupCtx, node, s.next[node])
			cancel()
			if err != nil {
				lastErr = err
			}
			if err != nil || !ok {
				done = false
				break
			}
			s.next[node]++
		}
	}
	return done, lastErr
}

// lookup is one call the sweep made: which node and which entity.
type lookup struct{ node, entity int }

// errLookupFailed stands in for a request a node fails.
var errLookupFailed = errors.New("lookup failed")

func TestEntitySweep_Advance(t *testing.T) {
	tests := []struct {
		name string
		// result is what a lookup returns on the attempt it is made in,
		// counted from zero.
		result   func(attempt int, l lookup) (bool, error)
		attempts int
		wantDone bool
		wantErr  error
		// wantCalls are the lookups of the last attempt only.
		wantCalls []lookup
	}{
		{
			name:      "every entity on every node in one attempt",
			result:    func(int, lookup) (bool, error) { return true, nil },
			attempts:  1,
			wantDone:  true,
			wantCalls: []lookup{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}},
		},
		{
			name: "an error keeps the progress made before it",
			result: func(attempt int, l lookup) (bool, error) {
				if attempt == 0 && l == (lookup{0, 2}) {
					return false, errLookupFailed
				}
				return true, nil
			},
			attempts:  2,
			wantDone:  true,
			wantCalls: []lookup{{0, 2}},
		},
		{
			name: "a miss on one node does not stop the others",
			result: func(_ int, l lookup) (bool, error) {
				return l != (lookup{0, 1}), nil
			},
			attempts:  1,
			wantDone:  false,
			wantCalls: []lookup{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {1, 2}},
		},
		{
			name: "the last error is reported while the sweep is unfinished",
			result: func(_ int, l lookup) (bool, error) {
				if l.entity == 1 {
					return false, errLookupFailed
				}
				return true, nil
			},
			attempts:  2,
			wantDone:  false,
			wantErr:   errLookupFailed,
			wantCalls: []lookup{{0, 1}, {1, 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sweep := newEntitySweep(2, 3)
			var done bool
			var err error
			var calls []lookup
			for attempt := range tt.attempts {
				calls = nil
				done, err = sweep.advance(t.Context(), time.Minute, func(_ context.Context, node, entity int) (bool, error) {
					l := lookup{node, entity}
					calls = append(calls, l)
					return tt.result(attempt, l)
				})
			}
			assert.Equal(t, tt.wantDone, done)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCalls, calls)
		})
	}
}

// A lookup that hangs is cut off at its own deadline, not the attempt's, and
// the sweep goes on to the next node within the same attempt.
func TestEntitySweep_EachLookupHasItsOwnDeadline(t *testing.T) {
	const perLookup = 20 * time.Millisecond
	attemptCtx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	sweep := newEntitySweep(2, 1)
	var calls []lookup
	done, err := sweep.advance(attemptCtx, perLookup, func(ctx context.Context, node, entity int) (bool, error) {
		calls = append(calls, lookup{node, entity})
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "a lookup runs without a deadline")
		assert.LessOrEqual(t, time.Until(deadline), perLookup)
		if node == 1 {
			return true, nil
		}
		<-ctx.Done()
		return false, ctx.Err()
	})

	assert.False(t, done)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NoError(t, attemptCtx.Err(), "the hung lookup spent the attempt's deadline")
	assert.Equal(t, []lookup{{0, 0}, {1, 0}}, calls)
}
