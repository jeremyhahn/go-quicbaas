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

package hash

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{
			name: "valid config with defaults",
			cfg:  DefaultConfig(),
		},
		{
			name: "valid config with custom vnodes",
			cfg: Config{
				VirtualNodes: 500,
				HashFunc:     Blake2b,
			},
		},
		{
			name: "valid config with FNV1a",
			cfg: Config{
				VirtualNodes: 150,
				HashFunc:     FNV1a,
			},
		},
		{
			name: "invalid vnodes - zero",
			cfg: Config{
				VirtualNodes: 0,
			},
			wantErr: ErrInvalidVnodes,
		},
		{
			name: "invalid vnodes - negative",
			cfg: Config{
				VirtualNodes: -1,
			},
			wantErr: ErrInvalidVnodes,
		},
		{
			name: "nil hash func uses default",
			cfg: Config{
				VirtualNodes: 150,
				HashFunc:     nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := New(tt.cfg)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, ch)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, ch)
				assert.Equal(t, tt.cfg.VirtualNodes, ch.vnodes)
			}
		})
	}
}

func TestNewHashRing(t *testing.T) {
	tests := []struct {
		name    string
		vnodes  int
		wantErr error
	}{
		{
			name:   "valid vnodes",
			vnodes: 150,
		},
		{
			name:   "high vnodes",
			vnodes: 1000,
		},
		{
			name:    "invalid vnodes - zero",
			vnodes:  0,
			wantErr: ErrInvalidVnodes,
		},
		{
			name:    "invalid vnodes - negative",
			vnodes:  -1,
			wantErr: ErrInvalidVnodes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewHashRing(tt.vnodes)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, ch)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, ch)
				assert.Equal(t, tt.vnodes, ch.vnodes)
			}
		})
	}
}

func TestConsistentHash_Add(t *testing.T) {
	tests := []struct {
		name      string
		hosts     []string
		wantCount int
		wantErr   error
	}{
		{
			name:      "add single host",
			hosts:     []string{"host1"},
			wantCount: 1,
		},
		{
			name:      "add multiple hosts",
			hosts:     []string{"host1", "host2", "host3"},
			wantCount: 3,
		},
		{
			name:      "add duplicate host",
			hosts:     []string{"host1", "host1"},
			wantCount: 1,
			wantErr:   ErrHostExists,
		},
		{
			name:    "add empty host name",
			hosts:   []string{""},
			wantErr: ErrEmptyHostName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := New(DefaultConfig())
			require.NoError(t, err)

			var count int
			for _, host := range tt.hosts {
				count, err = ch.Add(host)
			}

			if tt.wantErr != nil {
				assert.Error(t, err)
				if errors.Is(tt.wantErr, ErrHostExists) {
					assert.ErrorIs(t, err, tt.wantErr)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
				assert.Equal(t, tt.wantCount, len(ch.Hosts()))
			}
		})
	}
}

func TestConsistentHash_Remove(t *testing.T) {
	ch, err := New(DefaultConfig())
	require.NoError(t, err)

	hosts := []string{"host1", "host2", "host3"}
	for _, host := range hosts {
		_, err := ch.Add(host)
		require.NoError(t, err)
	}

	tests := []struct {
		name    string
		host    string
		wantErr error
	}{
		{
			name: "remove existing host",
			host: "host1",
		},
		{
			name:    "remove non-existent host",
			host:    "nonexistent",
			wantErr: ErrHostNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ch.Remove(tt.host)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				// Verify host is actually removed
				remaining := ch.Hosts()
				assert.NotContains(t, remaining, tt.host)
			}
		})
	}
}

func TestConsistentHash_Get(t *testing.T) {
	ch, err := New(DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name    string
		setup   func()
		key     string
		wantErr error
	}{
		{
			name:    "empty ring",
			setup:   func() {},
			key:     "key1",
			wantErr: ErrNoHosts,
		},
		{
			name: "single host",
			setup: func() {
				_, err := ch.Add("host1")
				require.NoError(t, err)
			},
			key: "key1",
		},
		{
			name: "multiple hosts - consistent mapping",
			setup: func() {
				_, err := ch.Add("host2")
				require.NoError(t, err)
				_, err = ch.Add("host3")
				require.NoError(t, err)
			},
			key: "key1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			host, err := ch.Get(tt.key)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, host)

				// Verify consistency - same key always maps to same host
				host2, err := ch.Get(tt.key)
				assert.NoError(t, err)
				assert.Equal(t, host, host2)
			}
		})
	}
}

func TestConsistentHash_GetN(t *testing.T) {
	ch, err := New(DefaultConfig())
	require.NoError(t, err)

	hosts := []string{"host1", "host2", "host3", "host4", "host5"}
	for _, host := range hosts {
		_, err := ch.Add(host)
		require.NoError(t, err)
	}

	tests := []struct {
		name      string
		key       string
		n         int
		wantCount int
		wantErr   error
	}{
		{
			name:      "get 3 hosts",
			key:       "key1",
			n:         3,
			wantCount: 3,
		},
		{
			name:      "get more hosts than available",
			key:       "key2",
			n:         10,
			wantCount: 5,
		},
		{
			name:      "get zero hosts",
			key:       "key3",
			n:         0,
			wantCount: 0,
		},
		{
			name:      "get negative hosts",
			key:       "key4",
			n:         -1,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ch.GetN(tt.key, tt.n)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				assert.Len(t, result, tt.wantCount)

				// Verify all hosts are unique
				seen := make(map[string]bool)
				for _, host := range result {
					assert.False(t, seen[host], "duplicate host in result")
					seen[host] = true
				}
			}
		})
	}
}

func TestConsistentHash_GetN_EmptyRing(t *testing.T) {
	ch, err := New(DefaultConfig())
	require.NoError(t, err)

	_, err = ch.GetN("key1", 3)
	assert.ErrorIs(t, err, ErrNoHosts)
}

func TestConsistentHash_GetLeast(t *testing.T) {
	ch, err := New(DefaultConfig())
	require.NoError(t, err)

	hosts := []string{"host1", "host2", "host3"}
	for _, host := range hosts {
		_, err := ch.Add(host)
		require.NoError(t, err)
	}

	t.Run("returns host under capacity", func(t *testing.T) {
		host, err := ch.GetLeast("key1")
		require.NoError(t, err)
		assert.NotEmpty(t, host)
	})

	t.Run("respects load balancing", func(t *testing.T) {
		// Set high load on one host
		err := ch.UpdateLoad("host1", 1000)
		require.NoError(t, err)

		// Should prefer lower-loaded hosts
		host, err := ch.GetLeast("key1")
		require.NoError(t, err)
		assert.NotEmpty(t, host)
	})

	t.Run("empty ring", func(t *testing.T) {
		emptyRing, err := New(DefaultConfig())
		require.NoError(t, err)

		_, err = emptyRing.GetLeast("key1")
		assert.ErrorIs(t, err, ErrNoHosts)
	})

	t.Run("all hosts at capacity fallback", func(t *testing.T) {
		// Create a fresh ring
		testRing, err := New(Config{VirtualNodes: 10, HashFunc: Blake2b})
		require.NoError(t, err)

		// Add a single host
		_, err = testRing.Add("overloaded-host")
		require.NoError(t, err)

		// Set extremely high load to exceed capacity
		err = testRing.UpdateLoad("overloaded-host", 10000)
		require.NoError(t, err)

		// Should still return the host even though it's at capacity
		host, err := testRing.GetLeast("some-key")
		require.NoError(t, err)
		assert.Equal(t, "overloaded-host", host)
	})
}

func TestConsistentHash_LoadManagement(t *testing.T) {
	ch, err := New(DefaultConfig())
	require.NoError(t, err)

	_, err = ch.Add("host1")
	require.NoError(t, err)

	t.Run("increment load", func(t *testing.T) {
		err := ch.Inc("host1")
		assert.NoError(t, err)

		loads := ch.GetLoads()
		assert.Equal(t, int64(1), loads["host1"])
	})

	t.Run("decrement load", func(t *testing.T) {
		err := ch.Dec("host1")
		assert.NoError(t, err)

		loads := ch.GetLoads()
		assert.Equal(t, int64(0), loads["host1"])
	})

	t.Run("update load", func(t *testing.T) {
		err := ch.UpdateLoad("host1", 42)
		assert.NoError(t, err)

		loads := ch.GetLoads()
		assert.Equal(t, int64(42), loads["host1"])
	})

	t.Run("operations on non-existent host", func(t *testing.T) {
		err := ch.Inc("nonexistent")
		assert.ErrorIs(t, err, ErrHostNotFound)

		err = ch.Dec("nonexistent")
		assert.ErrorIs(t, err, ErrHostNotFound)

		err = ch.UpdateLoad("nonexistent", 10)
		assert.ErrorIs(t, err, ErrHostNotFound)
	})
}

func TestConsistentHash_MaxLoad(t *testing.T) {
	ch, err := New(DefaultConfig())
	require.NoError(t, err)

	hosts := []string{"host1", "host2", "host3"}
	for _, host := range hosts {
		_, err := ch.Add(host)
		require.NoError(t, err)
	}

	t.Run("empty ring", func(t *testing.T) {
		emptyRing, err := New(DefaultConfig())
		require.NoError(t, err)
		assert.Equal(t, int64(0), emptyRing.MaxLoad())
	})

	t.Run("no load", func(t *testing.T) {
		maxLoad := ch.MaxLoad()
		assert.Greater(t, maxLoad, int64(0))
	})

	t.Run("with load", func(t *testing.T) {
		for i, host := range hosts {
			err := ch.UpdateLoad(host, int64(i*10))
			require.NoError(t, err)
		}

		maxLoad := ch.MaxLoad()
		assert.Greater(t, maxLoad, int64(0))

		// Verify it's approximately (totalLoad / numHosts) * 1.25
		stats := ch.GetStats()
		expectedMax := int64(math.Ceil(stats.AvgLoad * 1.25))
		assert.Equal(t, expectedMax, maxLoad)
	})

	t.Run("with negative total load", func(t *testing.T) {
		// Create a fresh ring
		testRing, err := New(DefaultConfig())
		require.NoError(t, err)

		_, err = testRing.Add("host1")
		require.NoError(t, err)

		// Manipulate total load to be negative (edge case)
		err = testRing.UpdateLoad("host1", -100)
		require.NoError(t, err)

		// MaxLoad should handle negative load gracefully
		maxLoad := testRing.MaxLoad()
		assert.Greater(t, maxLoad, int64(0))
	})
}

func TestConsistentHash_Distribution(t *testing.T) {
	tests := []struct {
		name         string
		vnodes       int
		numHosts     int
		numKeys      int
		maxDeviation float64 // Maximum acceptable % deviation from perfect distribution
	}{
		{
			name:         "150 vnodes - 3 hosts",
			vnodes:       150,
			numHosts:     3,
			numKeys:      10000,
			maxDeviation: 0.20, // 20% deviation - realistic for 150 vnodes
		},
		{
			name:         "300 vnodes - 5 hosts",
			vnodes:       300,
			numHosts:     5,
			numKeys:      10000,
			maxDeviation: 0.18, // 18% deviation
		},
		{
			name:         "500 vnodes - 10 hosts",
			vnodes:       500,
			numHosts:     10,
			numKeys:      10000,
			maxDeviation: 0.15, // 15% deviation
		},
		{
			name:         "1000 vnodes - 10 hosts - better distribution",
			vnodes:       1000,
			numHosts:     10,
			numKeys:      10000,
			maxDeviation: 0.10, // 10% deviation with more vnodes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := New(Config{
				VirtualNodes: tt.vnodes,
				HashFunc:     Blake2b,
			})
			require.NoError(t, err)

			// Add hosts
			for i := 0; i < tt.numHosts; i++ {
				_, err := ch.Add(fmt.Sprintf("host%d", i))
				require.NoError(t, err)
			}

			// Count key distribution
			distribution := make(map[string]int)
			for i := 0; i < tt.numKeys; i++ {
				key := fmt.Sprintf("key%d", i)
				host, err := ch.Get(key)
				require.NoError(t, err)
				distribution[host]++
			}

			// Verify distribution is reasonably even
			expectedPerHost := float64(tt.numKeys) / float64(tt.numHosts)
			for host, count := range distribution {
				deviation := math.Abs(float64(count)-expectedPerHost) / expectedPerHost
				assert.LessOrEqual(t, deviation, tt.maxDeviation,
					"Host %s has %d keys (%.2f%% deviation), expected ~%.0f",
					host, count, deviation*100, expectedPerHost)
			}
		})
	}
}

func TestConsistentHash_Rebalancing(t *testing.T) {
	ch, err := New(Config{
		VirtualNodes: 300,
		HashFunc:     Blake2b,
	})
	require.NoError(t, err)

	// Add initial hosts
	initialHosts := []string{"host1", "host2", "host3"}
	for _, host := range initialHosts {
		_, err := ch.Add(host)
		require.NoError(t, err)
	}

	// Map keys to initial hosts
	numKeys := 10000
	initialMapping := make(map[string]string)
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%d", i)
		host, err := ch.Get(key)
		require.NoError(t, err)
		initialMapping[key] = host
	}

	// Add a new host
	_, err = ch.Add("host4")
	require.NoError(t, err)

	// Check how many keys moved
	moved := 0
	for key, oldHost := range initialMapping {
		newHost, err := ch.Get(key)
		require.NoError(t, err)
		if newHost != oldHost {
			moved++
		}
	}

	// Verify minimal rebalancing (should be ~25% = 1/4 of keys)
	expectedMoved := float64(numKeys) / float64(len(initialHosts)+1)
	actualPercent := float64(moved) / float64(numKeys) * 100
	expectedPercent := expectedMoved / float64(numKeys) * 100

	t.Logf("Added 1 host to %d hosts: %d/%d keys moved (%.2f%%, expected ~%.2f%%)",
		len(initialHosts), moved, numKeys, actualPercent, expectedPercent)

	// Allow 50% margin of error (e.g., 12.5% - 37.5% for 25% expected)
	assert.InDelta(t, expectedPercent, actualPercent, expectedPercent*0.5,
		"Too many keys moved during rebalancing")
}

func TestConsistentHash_ConcurrentOperations(t *testing.T) {
	ch, err := New(DefaultConfig())
	require.NoError(t, err)

	// Pre-populate with some hosts
	for i := 0; i < 5; i++ {
		_, err := ch.Add(fmt.Sprintf("host%d", i))
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	numGoroutines := 100
	opsPerGoroutine := 100

	// Concurrent Gets
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key%d-%d", id, j)
				_, err := ch.Get(key)
				assert.NoError(t, err)
			}
		}(i)
	}

	// Concurrent GetLeast
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key%d-%d", id, j)
				host, err := ch.GetLeast(key)
				assert.NoError(t, err)
				if err == nil {
					_ = ch.Inc(host)
					_ = ch.Dec(host)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify ring is still valid
	stats := ch.GetStats()
	assert.Equal(t, 5, stats.HostCount)
	assert.Equal(t, int64(0), stats.TotalLoad) // All Inc/Dec should balance out
}

func TestConsistentHash_Stats(t *testing.T) {
	ch, err := New(Config{
		VirtualNodes: 150,
	})
	require.NoError(t, err)

	t.Run("empty ring stats", func(t *testing.T) {
		stats := ch.GetStats()
		assert.Equal(t, 0, stats.HostCount)
		assert.Equal(t, 150, stats.VirtualNodes)
		assert.Equal(t, 0, stats.TotalVNodes)
		assert.Equal(t, int64(0), stats.TotalLoad)
		assert.Equal(t, 0.0, stats.AvgLoad)
		// When empty, MinHostLoad should be MaxInt64 (initialized value)
		assert.Equal(t, int64(math.MaxInt64), stats.MinHostLoad)
		assert.Equal(t, int64(0), stats.MaxHostLoad)
		assert.Equal(t, int64(0), stats.MaxLoad)
	})

	hosts := []string{"host1", "host2", "host3"}
	for _, host := range hosts {
		_, err := ch.Add(host)
		require.NoError(t, err)
	}

	t.Run("populated ring stats", func(t *testing.T) {
		// Set different loads
		err = ch.UpdateLoad("host1", 10)
		require.NoError(t, err)
		err = ch.UpdateLoad("host2", 20)
		require.NoError(t, err)
		err = ch.UpdateLoad("host3", 30)
		require.NoError(t, err)

		stats := ch.GetStats()

		assert.Equal(t, 3, stats.HostCount)
		assert.Equal(t, 150, stats.VirtualNodes)
		assert.Equal(t, 450, stats.TotalVNodes)
		assert.Equal(t, int64(60), stats.TotalLoad)
		assert.Equal(t, 20.0, stats.AvgLoad)
		assert.Equal(t, int64(10), stats.MinHostLoad)
		assert.Equal(t, int64(30), stats.MaxHostLoad)
		assert.Greater(t, stats.MaxLoad, int64(0))
	})
}

// Benchmarks

func BenchmarkConsistentHash_Get(b *testing.B) {
	ch, _ := New(DefaultConfig())
	for i := 0; i < 10; i++ {
		ch.Add(fmt.Sprintf("host%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ch.Get(fmt.Sprintf("key%d", i))
	}
}

func BenchmarkConsistentHash_GetLeast(b *testing.B) {
	ch, _ := New(DefaultConfig())
	for i := 0; i < 10; i++ {
		ch.Add(fmt.Sprintf("host%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ch.GetLeast(fmt.Sprintf("key%d", i))
	}
}

func BenchmarkConsistentHash_Add(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ch, _ := New(DefaultConfig())
		for j := 0; j < 100; j++ {
			ch.Add(fmt.Sprintf("host%d", j))
		}
	}
}

func BenchmarkConsistentHash_LoadOps(b *testing.B) {
	ch, _ := New(DefaultConfig())
	for i := 0; i < 10; i++ {
		ch.Add(fmt.Sprintf("host%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		host := fmt.Sprintf("host%d", i%10)
		_ = ch.Inc(host)
		_ = ch.Dec(host)
	}
}

func BenchmarkHashFunctions(b *testing.B) {
	key := "benchmark-key-12345678"

	b.Run("Blake2b", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = Blake2b(key)
		}
	})

	b.Run("FNV1a", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = FNV1a(key)
		}
	})
}

func BenchmarkConsistentHash_VirtualNodes(b *testing.B) {
	vnodeCounts := []int{50, 150, 300, 500, 1000}

	for _, vnodes := range vnodeCounts {
		b.Run(fmt.Sprintf("vnodes=%d", vnodes), func(b *testing.B) {
			ch, _ := New(Config{VirtualNodes: vnodes})
			for i := 0; i < 10; i++ {
				ch.Add(fmt.Sprintf("host%d", i))
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = ch.Get(fmt.Sprintf("key%d", i))
			}
		})
	}
}
