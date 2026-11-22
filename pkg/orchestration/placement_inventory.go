// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// PlacementHost is one host as placement sees it: the fleet registry records
// that name it, grouped, with the union of their capabilities. The union
// answers "can anything on this host do X", which is all a host-level
// constraint can ask; picking the one fleet node that runs a task stays with
// ControlLoop.matchFleetNodeForTask, which hard-matches every required
// capability on the node itself.
type PlacementHost struct {
	Host         string   `json:"host"`
	Capabilities []string `json:"capabilities,omitempty"`
	NodeIDs      []string `json:"node_ids"`
}

// PlacementInventory lists the hosts a tenant's placement specs are compared
// against, one entry per host, ordered by host.
type PlacementInventory interface {
	PlacementHosts(ctx context.Context, tenantID string) ([]PlacementHost, error)
}

// IndexPlacementHosts indexes an inventory by host. It refuses an inventory
// that describes one host twice (*PlacementInventoryAmbiguousError) and an
// entry that names no host (*PlacementInventoryInvalidError): either would
// make the answer depend on which entry was read last.
func IndexPlacementHosts(hosts []PlacementHost) (map[string]PlacementHost, error) {
	index := make(map[string]PlacementHost, len(hosts))
	for _, host := range hosts {
		if strings.TrimSpace(host.Host) == "" {
			return nil, &PlacementInventoryInvalidError{NodeID: strings.Join(host.NodeIDs, ",")}
		}
		if _, dup := index[host.Host]; dup {
			return nil, &PlacementInventoryAmbiguousError{Host: host.Host}
		}
		index[host.Host] = host
	}
	return index, nil
}

// FleetInventory is the PlacementInventory over the fleet registry. A
// registry record is one enrolled daemon, and several can name one host, so
// it groups the records by PlacementHostIdentity(FleetNode.Host), the host
// without the daemon's port, before anything compares them.
type FleetInventory struct {
	registry FleetRegistry
}

// tenantFleetObserver is the strict read of a tenant's fleet records, the
// fleet counterpart of observeTenantSessions: every record as stored, with
// no record skipped, so the drift report never under-reports. The
// production registry implements it; a registry that does not is read
// through ListFleetNodes.
type tenantFleetObserver interface {
	observeTenantFleetNodes(ctx context.Context, tenantID string) ([]*FleetNode, error)
}

// ErrFleetInventoryNoRegistry refuses a FleetInventory built over nothing.
var ErrFleetInventoryNoRegistry = errors.New("placement: fleet inventory needs a fleet registry")

// NewFleetInventory returns the inventory over registry.
func NewFleetInventory(registry FleetRegistry) (*FleetInventory, error) {
	if registry == nil {
		return nil, ErrFleetInventoryNoRegistry
	}
	return &FleetInventory{registry: registry}, nil
}

// PlacementHosts lists the tenant's hosts. A record that names no host is
// refused with the record named, not dropped: dropping it would hide work
// the drift report can never place. The production registry is read without
// the operator listing's skips, so an unreadable record or one that names
// another tenant refuses the inventory instead of vanishing from it.
func (f *FleetInventory) PlacementHosts(ctx context.Context, tenantID string) ([]PlacementHost, error) {
	var (
		nodes []*FleetNode
		err   error
	)
	if observer, ok := f.registry.(tenantFleetObserver); ok {
		nodes, err = observer.observeTenantFleetNodes(ctx, tenantID)
	} else {
		nodes, err = f.registry.ListFleetNodes(ctx, tenantID, nil)
	}
	if err != nil {
		return nil, err
	}
	return groupFleetNodesByHost(tenantID, nodes)
}

func groupFleetNodesByHost(tenantID string, nodes []*FleetNode) ([]PlacementHost, error) {
	type group struct {
		capabilities map[string]struct{}
		nodeIDs      []string
	}
	groups := make(map[string]*group)
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.TenantID != tenantID {
			return nil, &PlacementInventoryInvalidError{
				TenantID: tenantID,
				NodeID:   node.ID,
				Reason:   fmt.Sprintf("names tenant %q", node.TenantID),
			}
		}
		host := PlacementHostIdentity(node.Host)
		if host == "" {
			return nil, &PlacementInventoryInvalidError{TenantID: tenantID, NodeID: node.ID}
		}
		g, ok := groups[host]
		if !ok {
			g = &group{capabilities: make(map[string]struct{})}
			groups[host] = g
		}
		for _, capability := range node.Capabilities {
			g.capabilities[capability] = struct{}{}
		}
		g.nodeIDs = append(g.nodeIDs, node.ID)
	}

	hosts := make([]PlacementHost, 0, len(groups))
	for host, g := range groups {
		entry := PlacementHost{Host: host, NodeIDs: g.nodeIDs}
		for capability := range g.capabilities {
			entry.Capabilities = append(entry.Capabilities, capability)
		}
		sort.Strings(entry.Capabilities)
		sort.Strings(entry.NodeIDs)
		hosts = append(hosts, entry)
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Host < hosts[j].Host })
	return hosts, nil
}

// PlacementHostIdentity is the host an address names, which is how
// placement tells hosts apart. A fleet record's Host and a session's
// HostAffinity are a daemon's dialable host:port, and several daemons on
// one machine differ only in the port, so the port is dropped. An address
// with no port is the host itself, without IPv6 brackets. One machine
// enrolled under two names (an IP and a DNS name) is still two hosts: the
// registry holds no verified machine identity to join them.
func PlacementHostIdentity(address string) string {
	address = strings.TrimSpace(address)
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		return address[1 : len(address)-1]
	}
	return address
}
