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

package registry

import (
	"strconv"
)

// ServiceName is the service identifier every transport routes registry
// requests through.
const ServiceName = "quicbaas.Registry"

// Lease TTL bounds the server enforces.
const (
	// MinTTLSeconds is the shortest lease a registration may hold.
	MinTTLSeconds int64 = 1

	// MaxTTLSeconds is the longest lease a registration may hold.
	MaxTTLSeconds int64 = 86400
)

// ServiceInstance is a registered instance of a named service.
type ServiceInstance struct {
	// ID uniquely identifies the instance within its service.
	ID string `json:"id"`

	// Service is the logical service name.
	Service string `json:"service"`

	// Address is the instance's network address (host:port).
	Address string `json:"address"`

	// Metadata carries optional descriptive key-value pairs.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RegisterRequest registers an instance under a new lease.
type RegisterRequest struct {
	// Instance is the instance to list.
	Instance ServiceInstance `json:"instance"`

	// TTLSeconds is the lease TTL, in [MinTTLSeconds, MaxTTLSeconds].
	TTLSeconds int64 `json:"ttl_seconds"`
}

// RegisterResponse carries the lease backing a registration.
type RegisterResponse struct {
	// LeaseID identifies the lease; Renew and Deregister take it. JSON
	// carries it as a string so no JSON client truncates it to a float.
	LeaseID int64 `json:"lease_id,string"`

	// TTLSeconds is the granted lease TTL.
	TTLSeconds int64 `json:"ttl_seconds"`
}

// RenewRequest renews a registration's lease.
type RenewRequest struct {
	// LeaseID is the lease Register returned.
	LeaseID int64 `json:"lease_id,string"`
}

// RenewResponse reports a renewed lease.
type RenewResponse struct {
	// LeaseID echoes the renewed lease.
	LeaseID int64 `json:"lease_id,string"`

	// TTLSeconds is the TTL the lease was reset to.
	TTLSeconds int64 `json:"ttl_seconds"`
}

// DeregisterRequest removes a registration.
type DeregisterRequest struct {
	// LeaseID is the lease Register returned.
	LeaseID int64 `json:"lease_id,string"`
}

// ResolveRequest looks up a service.
type ResolveRequest struct {
	// Service is the service name.
	Service string `json:"service"`
}

// ResolveResponse lists a service's instances.
type ResolveResponse struct {
	// Instances are the registered instances, ordered by ID.
	Instances []ServiceInstance `json:"instances"`

	// Revision is the store revision the instances were read at. Watching
	// from Revision+1 observes every later change.
	Revision int64 `json:"revision"`
}

// WatchRequest opens a watch.
type WatchRequest struct {
	// Service is the service name.
	Service string `json:"service"`

	// FromRevision is the first revision to deliver. Zero watches the
	// changes after the service's current revision.
	FromRevision int64 `json:"from_revision,omitempty"`
}

// EventType distinguishes a registration from a deregistration.
type EventType string

// Event types.
const (
	// EventPut reports an instance registered or updated.
	EventPut EventType = "PUT"

	// EventDelete reports an instance deregistered or its lease expired.
	EventDelete EventType = "DELETE"
)

// Event is one registration change delivered by Watch.
type Event struct {
	// Type is the kind of change.
	Type EventType `json:"type"`

	// Instance is the affected instance. A DELETE carries at least the
	// service and ID.
	Instance ServiceInstance `json:"instance"`

	// Revision is the store revision of the change.
	Revision int64 `json:"revision"`

	// Err is set only on the final event of a watch that did not end by its
	// own context: *WatchCompactedError, *WatchEndedError or
	// *WatchDisconnectedError. The channel closes after it.
	Err error `json:"-"`
}

// WatchCompactedError ends a watch whose start revision is older than the
// store's retained history. Resolve again and watch from the revision it
// returns.
type WatchCompactedError struct {
	// Service is the watched service.
	Service string

	// FromRevision is the revision the watch tried to start from.
	FromRevision int64

	// Message is the server's description.
	Message string
}

// Error returns a human-readable description.
func (e *WatchCompactedError) Error() string {
	return "registry: watch of " + e.Service + " from revision " + strconv.FormatInt(e.FromRevision, 10) +
		" is older than the retained history; resolve again and watch from its revision: " + e.Message
}

// WatchEndedError ends a watch the server's store cancelled for a reason
// other than compaction. Resolve again before trusting local state.
type WatchEndedError struct {
	// Service is the watched service.
	Service string

	// Message is the server's description.
	Message string
}

// Error returns a human-readable description.
func (e *WatchEndedError) Error() string {
	return "registry: watch of " + e.Service + " ended by the server: " + e.Message
}

// WatchDisconnectedError ends a watch whose connection dropped and could not
// be re-established within the retry budget. Nothing after LastRevision was
// delivered; watching again from LastRevision+1 loses nothing that the store
// still retains.
type WatchDisconnectedError struct {
	// Service is the watched service.
	Service string

	// LastRevision is the last revision delivered.
	LastRevision int64

	// Attempts is the number of reconnects tried.
	Attempts int

	// Cause is the final failure.
	Cause error
}

// Error returns a human-readable description.
func (e *WatchDisconnectedError) Error() string {
	return "registry: watch of " + e.Service + " disconnected after revision " +
		strconv.FormatInt(e.LastRevision, 10) + " and could not be resumed in " +
		strconv.Itoa(e.Attempts) + " attempts: " + e.Cause.Error()
}

// Unwrap returns the final failure.
func (e *WatchDisconnectedError) Unwrap() error {
	return e.Cause
}
