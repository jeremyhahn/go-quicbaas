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

// Package hash provides consistent hashing with bounded load balancing.
//
// Consistent hashing is a distributed hashing scheme that operates independently
// of the number of servers or objects in a distributed hash table by assigning
// them a position on an abstract circle (hash ring). This allows servers and
// objects to scale without affecting the overall system.
//
// References:
//   - https://en.wikipedia.org/wiki/Consistent_hashing
//   - https://research.googleblog.com/2017/04/consistent-hashing-with-bounded-loads.html
package hash

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
)

var (
	// ErrNoHosts is returned when an operation is attempted on an empty hash ring.
	ErrNoHosts = errors.New("hash: no hosts in ring")

	// ErrHostExists is returned when attempting to add a host that already exists.
	ErrHostExists = errors.New("hash: host already exists")

	// ErrHostNotFound is returned when attempting to operate on a non-existent host.
	ErrHostNotFound = errors.New("hash: host not found")

	// ErrInvalidVnodes is returned when virtual node count is invalid.
	ErrInvalidVnodes = errors.New("hash: virtual nodes must be greater than 0")

	// ErrEmptyHostName is returned when a host name is empty.
	ErrEmptyHostName = errors.New("hash: host name cannot be empty")
)

// Host represents a node in the consistent hash ring with load tracking.
type Host struct {
	// Name is the unique identifier for this host
	Name string

	// Load is the current load on this host (number of active requests)
	// This field is accessed atomically for thread-safety
	Load int64
}

// ConsistentHash implements a consistent hash ring with bounded load balancing.
//
// The ring uses virtual nodes (vnodes) to ensure even distribution of keys
// across hosts. Each physical host is mapped to multiple positions on the ring,
// reducing the impact of adding or removing hosts.
//
// Bounded load balancing ensures no single host becomes overloaded by capping
// each host's load at (avg_load * 1.25). If a host would exceed this threshold,
// the next host on the ring is selected instead.
//
// Thread-safety: All methods are safe for concurrent use.
type ConsistentHash struct {
	// vnodes is the number of virtual nodes per physical host
	// More vnodes = better distribution, but more memory usage
	// Typical values: 150-500
	vnodes int

	// hashFunc is the hash function used for mapping keys to ring positions
	hashFunc HashFunc

	// hosts maps hash positions to host names
	hosts map[uint64]string

	// sortedSet contains all hash positions in sorted order for binary search
	sortedSet []uint64

	// loadMap tracks load information for each host
	loadMap map[string]*Host

	// totalLoad is the sum of all host loads
	// Accessed atomically for thread-safety
	totalLoad int64

	// mu protects all fields except atomic fields
	mu sync.RWMutex
}

// Config holds configuration options for creating a ConsistentHash.
type Config struct {
	// VirtualNodes is the number of virtual nodes per physical host.
	// Higher values provide better distribution but use more memory.
	// Recommended: 150-500
	// Default: 150
	VirtualNodes int

	// HashFunc is the hash function to use for key distribution.
	// If nil, DefaultHash (Blake2b) will be used.
	HashFunc HashFunc
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		VirtualNodes: 150,
		HashFunc:     DefaultHash,
	}
}

// New creates a new ConsistentHash with the given configuration.
// Returns an error if the configuration is invalid.
func New(cfg Config) (*ConsistentHash, error) {
	if cfg.VirtualNodes <= 0 {
		return nil, ErrInvalidVnodes
	}

	hashFunc := cfg.HashFunc
	if hashFunc == nil {
		hashFunc = DefaultHash
	}

	return &ConsistentHash{
		vnodes:    cfg.VirtualNodes,
		hashFunc:  hashFunc,
		hosts:     make(map[uint64]string),
		sortedSet: make([]uint64, 0),
		loadMap:   make(map[string]*Host),
		totalLoad: 0,
	}, nil
}

// NewHashRing creates a new ConsistentHash with the specified number of virtual nodes
// and the default hash function. This is a convenience function for simple use cases.
//
// Deprecated: Use New with Config for more control.
func NewHashRing(vnodes int) (*ConsistentHash, error) {
	return New(Config{
		VirtualNodes: vnodes,
		HashFunc:     DefaultHash,
	})
}

// Add adds a host to the hash ring. If the host already exists, returns ErrHostExists.
// The host is replicated across the ring using virtual nodes for better distribution.
//
// Returns the total number of hosts in the ring after addition.
func (c *ConsistentHash) Add(host string) (int, error) {
	if host == "" {
		return 0, ErrEmptyHostName
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if host already exists
	if _, exists := c.loadMap[host]; exists {
		return len(c.loadMap), ErrHostExists
	}

	// Initialize host load tracking
	c.loadMap[host] = &Host{
		Name: host,
		Load: 0,
	}

	// Add virtual nodes to the ring
	for i := 0; i < c.vnodes; i++ {
		vnode := fmt.Sprintf("%s#%d", host, i)
		h := c.hashFunc(vnode)
		c.hosts[h] = host
		c.sortedSet = append(c.sortedSet, h)
	}

	// Sort ring positions for binary search
	sort.Slice(c.sortedSet, func(i, j int) bool {
		return c.sortedSet[i] < c.sortedSet[j]
	})

	return len(c.loadMap), nil
}

// Remove removes a host from the hash ring and returns true.
// If the host doesn't exist, returns ErrHostNotFound.
//
// Note: Removing a host will cause keys previously mapped to it to be
// redistributed to other hosts in the ring.
func (c *ConsistentHash) Remove(host string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if host exists
	if _, exists := c.loadMap[host]; !exists {
		return ErrHostNotFound
	}

	// Remove all virtual nodes for this host
	for i := 0; i < c.vnodes; i++ {
		vnode := fmt.Sprintf("%s#%d", host, i)
		h := c.hashFunc(vnode)
		delete(c.hosts, h)
		c.removeFromSortedSet(h)
	}

	// Remove from load map
	delete(c.loadMap, host)

	return nil
}

// Get returns the host that owns the given key using standard consistent hashing.
// This method does NOT consider load balancing - use GetLeast for load-aware selection.
//
// Returns ErrNoHosts if the ring is empty.
func (c *ConsistentHash) Get(key string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.hosts) == 0 {
		return "", ErrNoHosts
	}

	h := c.hashFunc(key)
	idx := c.search(h)
	return c.hosts[c.sortedSet[idx]], nil
}

// GetLeast returns the least loaded host that can serve the given key using
// consistent hashing with bounded loads (Google's algorithm).
//
// The algorithm ensures no host exceeds (average_load * 1.25) by walking the
// ring clockwise from the key's hash position until a host with capacity is found.
//
// This prevents hot spots and ensures more even load distribution across hosts.
//
// Returns ErrNoHosts if the ring is empty.
func (c *ConsistentHash) GetLeast(key string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.hosts) == 0 {
		return "", ErrNoHosts
	}

	h := c.hashFunc(key)
	idx := c.search(h)

	// Walk the ring to find a host with available capacity
	for i := 0; i < len(c.sortedSet); i++ {
		position := (idx + i) % len(c.sortedSet)
		host := c.hosts[c.sortedSet[position]]
		if c.loadOK(host) {
			return host, nil
		}
	}

	// All hosts are at capacity, return the primary host
	// This should rarely happen in practice
	return c.hosts[c.sortedSet[idx]], nil
}

// GetN returns the N closest hosts to the given key, ordered by distance on the ring.
// This is useful for replication scenarios where you want to store data on multiple hosts.
//
// Returns fewer than N hosts if the ring has fewer hosts than requested.
// Returns ErrNoHosts if the ring is empty.
func (c *ConsistentHash) GetN(key string, n int) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.hosts) == 0 {
		return nil, ErrNoHosts
	}

	if n <= 0 {
		return []string{}, nil
	}

	h := c.hashFunc(key)
	idx := c.search(h)

	// Track unique hosts to avoid duplicates (virtual nodes map to same host)
	seen := make(map[string]bool)
	result := make([]string, 0, n)

	// Walk the ring collecting unique hosts
	for i := 0; i < len(c.sortedSet) && len(result) < n; i++ {
		position := (idx + i) % len(c.sortedSet)
		host := c.hosts[c.sortedSet[position]]
		if !seen[host] {
			seen[host] = true
			result = append(result, host)
		}
	}

	return result, nil
}

// Inc increments the load of the specified host by 1.
// This should be called when a request is assigned to a host.
//
// Use this only with hosts obtained from GetLeast to maintain
// accurate load balancing statistics.
func (c *ConsistentHash) Inc(host string) error {
	c.mu.RLock()
	h, exists := c.loadMap[host]
	c.mu.RUnlock()

	if !exists {
		return ErrHostNotFound
	}

	atomic.AddInt64(&h.Load, 1)
	atomic.AddInt64(&c.totalLoad, 1)
	return nil
}

// Dec decrements the load of the specified host by 1.
// This should be called when a request completes.
//
// Use this only with hosts obtained from GetLeast to maintain
// accurate load balancing statistics.
func (c *ConsistentHash) Dec(host string) error {
	c.mu.RLock()
	h, exists := c.loadMap[host]
	c.mu.RUnlock()

	if !exists {
		return ErrHostNotFound
	}

	atomic.AddInt64(&h.Load, -1)
	atomic.AddInt64(&c.totalLoad, -1)
	return nil
}

// UpdateLoad sets the load of the specified host to an exact value.
// This is useful for synchronizing load from external sources.
func (c *ConsistentHash) UpdateLoad(host string, load int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	h, exists := c.loadMap[host]
	if !exists {
		return ErrHostNotFound
	}

	oldLoad := atomic.LoadInt64(&h.Load)
	atomic.StoreInt64(&h.Load, load)
	atomic.AddInt64(&c.totalLoad, load-oldLoad)
	return nil
}

// Hosts returns a slice of all host names in the ring.
// The order is not guaranteed.
func (c *ConsistentHash) Hosts() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hosts := make([]string, 0, len(c.loadMap))
	for name := range c.loadMap {
		hosts = append(hosts, name)
	}
	return hosts
}

// GetLoads returns a map of host names to their current load values.
// This is a snapshot at the time of the call.
func (c *ConsistentHash) GetLoads() map[string]int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	loads := make(map[string]int64, len(c.loadMap))
	for name, host := range c.loadMap {
		loads[name] = atomic.LoadInt64(&host.Load)
	}
	return loads
}

// MaxLoad returns the maximum allowed load for any single host.
// This is calculated as: ceil((total_load / num_hosts) * 1.25)
//
// The 1.25 factor (25% overhead) is from Google's bounded load algorithm,
// providing a good balance between load distribution and flexibility.
func (c *ConsistentHash) MaxLoad() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.loadMap) == 0 {
		return 0
	}

	totalLoad := atomic.LoadInt64(&c.totalLoad)
	if totalLoad < 0 {
		totalLoad = 0
	}

	avgLoad := float64(totalLoad) / float64(len(c.loadMap))
	if avgLoad == 0 {
		avgLoad = 1
	}

	return int64(math.Ceil(avgLoad * 1.25))
}

// Stats returns statistics about the hash ring.
type Stats struct {
	// HostCount is the number of physical hosts in the ring
	HostCount int

	// VirtualNodes is the number of virtual nodes per host
	VirtualNodes int

	// TotalVNodes is the total number of virtual nodes (HostCount * VirtualNodes)
	TotalVNodes int

	// TotalLoad is the sum of all host loads
	TotalLoad int64

	// MaxLoad is the maximum allowed load per host
	MaxLoad int64

	// AvgLoad is the average load across all hosts
	AvgLoad float64

	// MinHostLoad is the minimum load among all hosts
	MinHostLoad int64

	// MaxHostLoad is the maximum load among all hosts
	MaxHostLoad int64
}

// GetStats returns current statistics about the hash ring.
func (c *ConsistentHash) GetStats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := Stats{
		HostCount:    len(c.loadMap),
		VirtualNodes: c.vnodes,
		TotalVNodes:  len(c.sortedSet),
		TotalLoad:    atomic.LoadInt64(&c.totalLoad),
		MinHostLoad:  int64(math.MaxInt64),
		MaxHostLoad:  0,
	}

	if stats.HostCount > 0 {
		stats.AvgLoad = float64(stats.TotalLoad) / float64(stats.HostCount)
		stats.MaxLoad = c.maxLoadLocked()

		for _, host := range c.loadMap {
			load := atomic.LoadInt64(&host.Load)
			if load < stats.MinHostLoad {
				stats.MinHostLoad = load
			}
			if load > stats.MaxHostLoad {
				stats.MaxHostLoad = load
			}
		}

		if stats.MinHostLoad == int64(math.MaxInt64) {
			stats.MinHostLoad = 0
		}
	}

	return stats
}

// search performs a binary search to find the first hash position >= key.
// If no such position exists, wraps around to index 0.
func (c *ConsistentHash) search(key uint64) int {
	idx := sort.Search(len(c.sortedSet), func(i int) bool {
		return c.sortedSet[i] >= key
	})

	if idx >= len(c.sortedSet) {
		idx = 0
	}
	return idx
}

// removeFromSortedSet removes a hash value from the sorted set using binary search.
func (c *ConsistentHash) removeFromSortedSet(val uint64) {
	idx := sort.Search(len(c.sortedSet), func(i int) bool {
		return c.sortedSet[i] >= val
	})

	if idx < len(c.sortedSet) && c.sortedSet[idx] == val {
		c.sortedSet = append(c.sortedSet[:idx], c.sortedSet[idx+1:]...)
	}
}

// loadOK checks if a host can accept one more request without exceeding max load.
// Must be called with read lock held.
func (c *ConsistentHash) loadOK(host string) bool {
	maxLoad := c.maxLoadLocked()

	h, ok := c.loadMap[host]
	if !ok {
		return false
	}

	currentLoad := atomic.LoadInt64(&h.Load)
	return currentLoad+1 <= maxLoad
}

// maxLoadLocked calculates max load. Must be called with lock held.
func (c *ConsistentHash) maxLoadLocked() int64 {
	if len(c.loadMap) == 0 {
		return 0
	}

	totalLoad := atomic.LoadInt64(&c.totalLoad)
	if totalLoad < 0 {
		totalLoad = 0
	}

	avgLoad := float64(totalLoad+1) / float64(len(c.loadMap))
	if avgLoad == 0 {
		avgLoad = 1
	}

	return int64(math.Ceil(avgLoad * 1.25))
}
