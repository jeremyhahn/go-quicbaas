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

package secrets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Storage-level write arbitration across nodes (#1655): two nodes are two
// independent KVBackends that share one store whose guarded verbs arbitrate
// inside the store, as the server's qrdb-backed store does. Writers on both
// nodes race on one secret. Every acknowledged write owns a distinct version,
// the history holds exactly the acknowledged writes, every refused write is
// ErrWriteConflict and left nothing behind, and the current version is the
// newest acknowledged one.
func TestKVBackend_TwoNodesArbitrateConcurrentWrites(t *testing.T) {
	store := newInMemKV()
	nodes := []*KVBackend{NewKVBackend(store), NewKVBackend(store)}
	ctx := context.Background()
	const path = "nodes/shared"
	require.NoError(t, nodes[0].Store(ctx, path, map[string][]byte{"k": []byte("seed")}, nil))

	const writersPerNode = 16
	type outcome struct {
		value   string
		version int
		err     error
	}
	outcomes := make(chan outcome, len(nodes)*writersPerNode)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for n, node := range nodes {
		for w := 0; w < writersPerNode; w++ {
			wg.Add(1)
			go func(node *KVBackend, value string) {
				defer wg.Done()
				<-start
				v, err := node.StoreVersion(ctx, path, map[string][]byte{"k": []byte(value)}, nil)
				outcomes <- outcome{value: value, version: v, err: err}
			}(node, fmt.Sprintf("node%d-writer%d", n, w))
		}
	}
	close(start)
	wg.Wait()
	close(outcomes)

	acked := map[int]string{}
	refused := 0
	for o := range outcomes {
		if o.err != nil {
			require.True(t, errors.Is(o.err, ErrWriteConflict), "a refused write is a write conflict, got %v", o.err)
			refused++
			continue
		}
		prev, dup := acked[o.version]
		require.False(t, dup, "version %d acknowledged to %q and %q", o.version, prev, o.value)
		acked[o.version] = o.value
	}
	require.NotEmpty(t, acked, "some write wins")
	assert.Equal(t, len(nodes)*writersPerNode, len(acked)+refused)

	// Both nodes read the same history: the seed and exactly the acked writes.
	for i, node := range nodes {
		versions, err := node.ListVersions(ctx, path)
		require.NoError(t, err)
		var got []int
		for _, vm := range versions {
			got = append(got, vm.Version)
		}
		want := []int{1}
		for v := range acked {
			want = append(want, v)
		}
		sort.Ints(got)
		sort.Ints(want)
		assert.Equal(t, want, got, "node %d sees the seed and only acknowledged versions", i)

		for v, value := range acked {
			data, _, err := node.GetVersion(ctx, path, v)
			require.NoError(t, err)
			assert.Equal(t, value, string(data["k"]), "node %d: version %d holds its writer's value", i, v)
		}
	}

	newest := 0
	for v := range acked {
		if v > newest {
			newest = v
		}
	}
	for i, node := range nodes {
		data, _, err := node.Get(ctx, path)
		require.NoError(t, err)
		assert.Equal(t, acked[newest], string(data["k"]), "node %d: the current version is the newest acknowledged write", i)
	}
}
