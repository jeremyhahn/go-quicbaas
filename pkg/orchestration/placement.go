// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package orchestration

// Declarative placement: the DESIRED state of where a class of work may run.
//
// A PlacementSpec is written by an operator and says which hosts a workload
// class is permitted on. It is not the fleet registry: the registry
// (FleetRegistry, FleetNode) is OBSERVED state, one record per enrolled
// daemon, and several records can name one host. Placement compares a spec
// against the registry only through a PlacementInventory, which groups the
// records by host first (placement_inventory.go).
//
// Design: docs/agent-fleet/placement.md (go-quicbaas#764).

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PlacementSpec places a workload class onto hosts, within one tenant.
type PlacementSpec struct {
	// TenantID is the tenant the spec belongs to. The store sets it from the
	// tenant it is written in; a spec naming another tenant is refused.
	TenantID string `json:"tenant_id"`

	// Name identifies the spec within its tenant.
	Name string `json:"name"`

	// WorkloadClass is the class of work the spec places.
	WorkloadClass string `json:"workload_class"`

	// Hosts are the admissible hosts. Empty means any host that passes the
	// capability check.
	Hosts []string `json:"hosts,omitempty"`

	// RequiredCapabilities must ALL be present on a host: a hard match,
	// never a score.
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`

	// Actuate false reports drift only; true lets the actuator move work.
	Actuate bool `json:"actuate"`

	// Generation orders edits. A declare carries the generation it read
	// (zero to create) and the store writes that plus one by compare-and-swap,
	// so two racing edits cannot lose one silently.
	Generation uint64 `json:"generation"`
}

// Validate refuses a spec the store must not write: an unusable name or
// class, and an empty or repeated host or capability. A repeat is refused
// rather than collapsed, because it is the sign of an edit that meant
// something else.
func (s *PlacementSpec) Validate() error {
	if s == nil {
		return &PlacementSpecInvalidError{Field: "spec", Reason: "is required"}
	}
	if err := validateKeySegment("name", s.Name); err != nil {
		return &PlacementSpecInvalidError{Name: s.Name, Field: "name", Reason: err.Error()}
	}
	if strings.TrimSpace(s.WorkloadClass) == "" {
		return &PlacementSpecInvalidError{Name: s.Name, Field: "workload_class", Reason: "must not be empty"}
	}
	if err := validatePlacementList(s.Name, "hosts", s.Hosts); err != nil {
		return err
	}
	return validatePlacementList(s.Name, "required_capabilities", s.RequiredCapabilities)
}

func validatePlacementList(name, field string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			return &PlacementSpecInvalidError{
				Name: name, Field: field + "[" + strconv.Itoa(i) + "]", Reason: "must not be empty",
			}
		}
		if _, dup := seen[value]; dup {
			return &PlacementSpecInvalidError{
				Name: name, Field: field + "[" + strconv.Itoa(i) + "]", Reason: "repeats " + strconv.Quote(value),
			}
		}
		seen[value] = struct{}{}
	}
	return nil
}

// clone returns a deep copy, so a caller's later edit of what it passed in or
// got back cannot reach the stored value.
func (s *PlacementSpec) clone() *PlacementSpec {
	out := *s
	out.Hosts = append([]string(nil), s.Hosts...)
	out.RequiredCapabilities = append([]string(nil), s.RequiredCapabilities...)
	return &out
}

// Sentinels for errors.Is; each typed error below unwraps to one.
var (
	ErrPlacementSpecInvalid        = errors.New("placement: invalid spec")
	ErrPlacementNotFound           = errors.New("placement: not found")
	ErrPlacementSpecStale          = errors.New("placement: stale generation")
	ErrPlacementStoreUnguarded     = errors.New("placement: store cannot decide a guarded write")
	ErrPlacementInventoryAmbiguous = errors.New("placement: inventory describes a host twice")
	ErrPlacementInventoryInvalid   = errors.New("placement: inventory record names no host")
)

// PlacementSpecInvalidError names the field of a spec that cannot be written.
type PlacementSpecInvalidError struct {
	Name   string
	Field  string
	Reason string
}

func (e *PlacementSpecInvalidError) Error() string {
	return fmt.Sprintf("placement spec %q: %s %s", e.Name, e.Field, e.Reason)
}

func (e *PlacementSpecInvalidError) Unwrap() error { return ErrPlacementSpecInvalid }

// PlacementNotFoundError: no spec of that name in the tenant.
type PlacementNotFoundError struct {
	TenantID string
	Name     string
}

func (e *PlacementNotFoundError) Error() string {
	return fmt.Sprintf("placement spec %q not found in tenant %q", e.Name, e.TenantID)
}

func (e *PlacementNotFoundError) Unwrap() error { return ErrPlacementNotFound }

// PlacementSpecStaleError refuses an edit written against a generation that
// is no longer the stored one. The edit is refused, never merged: the caller
// re-reads and decides again.
type PlacementSpecStaleError struct {
	TenantID string
	Name     string
	// Read is the generation the edit was written against (zero: a create).
	Read uint64
	// Current is the stored generation, when the store could read it.
	Current uint64
}

func (e *PlacementSpecStaleError) Error() string {
	if e.Read == 0 {
		return fmt.Sprintf("placement spec %q in tenant %q already exists at generation %d; read it and edit that",
			e.Name, e.TenantID, e.Current)
	}
	return fmt.Sprintf("placement spec %q in tenant %q: edit read generation %d, stored generation is %d",
		e.Name, e.TenantID, e.Read, e.Current)
}

func (e *PlacementSpecStaleError) Unwrap() error { return ErrPlacementSpecStale }

// PlacementStoreUnguardedError refuses a KV store that cannot decide a
// contested write. A placement store on such a KV could lose an edit.
type PlacementStoreUnguardedError struct {
	// Missing names the guarded-write capability the KV lacks.
	Missing string
}

func (e *PlacementStoreUnguardedError) Error() string {
	return "placement store needs a KV with " + e.Missing + "; this one has none"
}

func (e *PlacementStoreUnguardedError) Unwrap() error { return ErrPlacementStoreUnguarded }

// PlacementInventoryAmbiguousError refuses an inventory with two entries for
// one host. Keeping either picks a capability set at random, a union
// over-credits and an intersection under-credits, so no answer is right.
// Grouping registry records by host is the adapter's job (FleetInventory); an
// ordinary multi-daemon host never trips this, only a broken adapter does.
type PlacementInventoryAmbiguousError struct {
	Host string
}

func (e *PlacementInventoryAmbiguousError) Error() string {
	return fmt.Sprintf("placement inventory describes host %q more than once", e.Host)
}

func (e *PlacementInventoryAmbiguousError) Unwrap() error { return ErrPlacementInventoryAmbiguous }

// PlacementInventoryInvalidError refuses a registry record that names no
// host. Dropping it would leave work the report can never place; it is
// named instead.
type PlacementInventoryInvalidError struct {
	TenantID string
	NodeID   string
}

func (e *PlacementInventoryInvalidError) Error() string {
	return fmt.Sprintf("placement inventory: fleet node %q in tenant %q names no host", e.NodeID, e.TenantID)
}

func (e *PlacementInventoryInvalidError) Unwrap() error { return ErrPlacementInventoryInvalid }
