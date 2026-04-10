//go:build raft

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

package discovery

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"
)

// DNSResolver is an interface for DNS resolution
// This allows us to mock DNS lookups in tests.
type DNSResolver interface {
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// DNSDiscovery implements discovery using DNS SRV records
// This is ideal for production deployments with service discovery systems like:
// - Consul (via DNS interface)
// - CoreDNS (Kubernetes)
// - Standard DNS servers with SRV support.
type DNSDiscovery struct {
	// srvRecord is the SRV record to query (e.g., "_quicbaas._tcp.example.com")
	srvRecord string

	// refreshInterval is how often to refresh the peer list
	refreshInterval time.Duration

	// resolver is the DNS resolver to use
	resolver DNSResolver

	// cachedPeers stores the last discovered peers
	cachedPeers []Peer
	cacheTime   time.Time
}

// DNSConfig contains configuration for DNS discovery.
type DNSConfig struct {
	// SRVRecord is the DNS SRV record to query
	// Format: _service._proto.domain (e.g., "_quicbaas._tcp.example.com")
	SRVRecord string

	// RefreshInterval is how often to refresh the DNS records
	// Default: 30 seconds
	RefreshInterval time.Duration

	// Resolver is a custom DNS resolver (optional)
	// If nil, uses system default resolver
	Resolver DNSResolver
}

// NewDNSDiscovery creates a new DNS-based discovery method.
func NewDNSDiscovery(config DNSConfig) (*DNSDiscovery, error) {
	if config.SRVRecord == "" {
		return nil, &DiscoveryError{
			Op:     "NewDNSDiscovery",
			Method: "dns",
			Err:    "srv_record cannot be empty",
		}
	}

	// Validate SRV record format
	if err := validateSRVRecord(config.SRVRecord); err != nil {
		return nil, &DiscoveryError{
			Op:     "NewDNSDiscovery",
			Method: "dns",
			Err:    "invalid srv_record: " + err.Error(),
		}
	}

	// Set defaults
	if config.RefreshInterval == 0 {
		config.RefreshInterval = 30 * time.Second
	}

	if config.Resolver == nil {
		config.Resolver = net.DefaultResolver
	}

	return &DNSDiscovery{
		srvRecord:       config.SRVRecord,
		refreshInterval: config.RefreshInterval,
		resolver:        config.Resolver,
		cachedPeers:     make([]Peer, 0),
	}, nil
}

// Name returns the name of this discovery method.
func (d *DNSDiscovery) Name() string {
	return "dns"
}

// Discover queries DNS SRV records to find peers.
func (d *DNSDiscovery) Discover(ctx context.Context) ([]Peer, error) {
	// Check cache first
	if time.Since(d.cacheTime) < d.refreshInterval && len(d.cachedPeers) > 0 {
		return d.cachedPeers, nil
	}

	// Parse SRV record into service, proto, and name
	service, proto, name, err := parseSRVRecord(d.srvRecord)
	if err != nil {
		return nil, &DiscoveryError{
			Op:     "Discover",
			Method: "dns",
			Err:    "failed to parse SRV record: " + err.Error(),
		}
	}

	// Query SRV records
	_, srvRecords, err := d.resolver.LookupSRV(ctx, service, proto, name)
	if err != nil {
		return nil, &DiscoveryError{
			Op:     "Discover",
			Method: "dns",
			Err:    "SRV lookup failed for " + d.srvRecord + ": " + err.Error(),
		}
	}

	if len(srvRecords) == 0 {
		return nil, &DiscoveryError{
			Op:     "Discover",
			Method: "dns",
			Err:    "no SRV records found for " + d.srvRecord,
		}
	}

	// Convert SRV records to Peers
	peers := make([]Peer, 0, len(srvRecords))
	now := time.Now()

	for i, srv := range srvRecords {
		// For DNS discovery, we don't have explicit node IDs in SRV records
		// We generate node IDs based on the order (this could be improved)
		// In production, you might want to use TXT records or other mechanisms
		// to store node IDs
		// Loop index i is bounded by SRV record count (typically < 1000)
		nodeID := uint64(i + 1) //nolint:gosec // G115: i bounded by SRV records length

		// Construct address
		address := net.JoinHostPort(
			strings.TrimSuffix(srv.Target, "."),
			strconv.Itoa(int(srv.Port)),
		)

		peer := Peer{
			NodeID:   nodeID,
			Address:  address,
			RaftAddr: address,
			Metadata: map[string]string{
				"priority": strconv.Itoa(int(srv.Priority)),
				"weight":   strconv.Itoa(int(srv.Weight)),
			},
			LastSeen: now,
			TTL:      d.refreshInterval,
		}

		peers = append(peers, peer)
	}

	// Update cache
	d.cachedPeers = peers
	d.cacheTime = now

	return peers, nil
}

// Register is a no-op for DNS discovery
// DNS records are typically managed externally (via Consul, CoreDNS, etc.)
func (d *DNSDiscovery) Register(ctx context.Context, self Peer) error {
	// No-op - DNS records are managed externally
	return nil
}

// Deregister is a no-op for DNS discovery.
func (d *DNSDiscovery) Deregister(ctx context.Context) error {
	// No-op - DNS records are managed externally
	return nil
}

// Close releases resources.
func (d *DNSDiscovery) Close() error {
	// Clear cache
	d.cachedPeers = nil
	return nil
}

// InvalidateCache forces a refresh on the next Discover call.
func (d *DNSDiscovery) InvalidateCache() {
	d.cacheTime = time.Time{}
}

// GetCachedPeers returns the currently cached peers without querying DNS.
func (d *DNSDiscovery) GetCachedPeers() []Peer {
	peers := make([]Peer, len(d.cachedPeers))
	copy(peers, d.cachedPeers)
	return peers
}

// SRVRecordEmptyError is returned when an SRV record is empty.
type SRVRecordEmptyError struct{}

// Error implements the error interface.
func (e *SRVRecordEmptyError) Error() string {
	return "SRV record cannot be empty"
}

// SRVRecordFormatError is returned when an SRV record has an invalid format.
type SRVRecordFormatError struct {
	Detail string
}

// Error implements the error interface.
func (e *SRVRecordFormatError) Error() string {
	return "invalid SRV record format: " + e.Detail
}

// SRVRecordComponentError is returned when an SRV record has an empty component.
type SRVRecordComponentError struct{}

// Error implements the error interface.
func (e *SRVRecordComponentError) Error() string {
	return "invalid SRV record: empty component"
}

// validateSRVRecord checks if the SRV record format is valid.
func validateSRVRecord(record string) error {
	// SRV records should be in format: _service._proto.domain
	// Examples:
	// - _quicbaas._tcp.example.com
	// - _raft._tcp.cluster.local
	// - _db._tcp.service.consul

	if record == "" {
		return &SRVRecordEmptyError{}
	}

	// Must start with underscore
	if !strings.HasPrefix(record, "_") {
		return &SRVRecordFormatError{Detail: "must start with underscore (e.g., _service._proto.domain)"}
	}

	// Must have at least two dots (three parts: _service, _proto, domain)
	parts := strings.Split(record, ".")
	if len(parts) < 3 {
		return &SRVRecordFormatError{Detail: "must have at least 3 parts: _service._proto.domain"}
	}

	// First part should be _service
	if !strings.HasPrefix(parts[0], "_") {
		return &SRVRecordFormatError{Detail: "service part must start with underscore"}
	}

	// Second part should be _proto
	if !strings.HasPrefix(parts[1], "_") {
		return &SRVRecordFormatError{Detail: "proto part must start with underscore"}
	}

	return nil
}

// parseSRVRecord parses a full SRV record into its components
// Input: "_quicbaas._tcp.example.com"
// Output: "quicbaas", "tcp", "example.com".
func parseSRVRecord(record string) (service, proto, name string, err error) {
	parts := strings.SplitN(record, ".", 3)
	if len(parts) < 3 {
		return "", "", "", &SRVRecordFormatError{Detail: "expected _service._proto.domain"}
	}

	// Remove leading underscores
	service = strings.TrimPrefix(parts[0], "_")
	proto = strings.TrimPrefix(parts[1], "_")
	name = parts[2]

	if service == "" || proto == "" || name == "" {
		return "", "", "", &SRVRecordComponentError{}
	}

	return service, proto, name, nil
}

// NewDNSDiscoveryFromSRV creates DNS discovery from a simple SRV record string
// This is a convenience function for the most common use case.
func NewDNSDiscoveryFromSRV(srvRecord string) (*DNSDiscovery, error) {
	return NewDNSDiscovery(DNSConfig{
		SRVRecord: srvRecord,
	})
}
