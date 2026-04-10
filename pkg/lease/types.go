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

// Package lease provides lease management for time-limited resources.
package lease

import (
	"time"
)

// LeaseState represents the current state of a lease.
type LeaseState int

const (
	// LeaseStateActive indicates the lease is currently active and valid.
	LeaseStateActive LeaseState = iota

	// LeaseStateExpired indicates the lease has exceeded its TTL.
	LeaseStateExpired

	// LeaseStateRevoked indicates the lease was manually revoked.
	LeaseStateRevoked
)

// String returns the string representation of the lease state.
func (s LeaseState) String() string {
	switch s {
	case LeaseStateActive:
		return "active"
	case LeaseStateExpired:
		return "expired"
	case LeaseStateRevoked:
		return "revoked"
	default:
		return "unknown"
	}
}

// Lease represents a time-limited grant for a resource.
// Leases are used for dynamic secrets, session tokens, and temporary credentials.
type Lease struct {
	// LeaseID is the unique identifier for this lease.
	LeaseID string `json:"lease_id"`

	// SecretPath is the path to the secret or resource this lease grants access to.
	SecretPath string `json:"secret_path"`

	// IssuedAt is the timestamp when the lease was created.
	IssuedAt time.Time `json:"issued_at"`

	// ExpiresAt is the timestamp when the lease will expire.
	ExpiresAt time.Time `json:"expires_at"`

	// TTL is the time-to-live duration for this lease.
	TTL time.Duration `json:"ttl"`

	// RenewCount tracks how many times this lease has been renewed.
	RenewCount int `json:"renew_count"`

	// MaxRenewals is the maximum number of times this lease can be renewed.
	// A value of 0 means unlimited renewals.
	MaxRenewals int `json:"max_renewals"`

	// Renewable indicates whether this lease can be renewed.
	Renewable bool `json:"renewable"`

	// RevokedAt is the timestamp when the lease was revoked (nil if not revoked).
	RevokedAt *time.Time `json:"revoked_at,omitempty"`

	// Metadata contains additional key-value data about this lease.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// State returns the current state of the lease.
func (l *Lease) State() LeaseState {
	if l.RevokedAt != nil {
		return LeaseStateRevoked
	}
	if time.Now().After(l.ExpiresAt) {
		return LeaseStateExpired
	}
	return LeaseStateActive
}

// IsValid returns true if the lease is active and not expired or revoked.
func (l *Lease) IsValid() bool {
	return l.State() == LeaseStateActive
}

// CanRenew returns true if the lease can be renewed.
func (l *Lease) CanRenew() bool {
	if !l.Renewable {
		return false
	}
	if l.State() != LeaseStateActive {
		return false
	}
	if l.MaxRenewals > 0 && l.RenewCount >= l.MaxRenewals {
		return false
	}
	return true
}

// RemainingTTL returns the time remaining until the lease expires.
// Returns 0 if the lease is already expired.
func (l *Lease) RemainingTTL() time.Duration {
	remaining := time.Until(l.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// LeaseOptions contains optional parameters for lease creation.
type LeaseOptions struct {
	// Renewable indicates whether the lease can be renewed.
	Renewable bool

	// MaxRenewals is the maximum number of times the lease can be renewed.
	// A value of 0 means unlimited renewals.
	MaxRenewals int

	// Metadata contains additional key-value data to store with the lease.
	Metadata map[string]string
}

// DefaultLeaseOptions returns default lease options.
func DefaultLeaseOptions() *LeaseOptions {
	return &LeaseOptions{
		Renewable:   true,
		MaxRenewals: 0,
		Metadata:    make(map[string]string),
	}
}
