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

package agent

import (
	"errors"
	"strings"
	"testing"
)

// TestConfig_MaxConcurrentJobs_AcceptsADeclaredCount is the ordinary case: a host
// that has counted the agent containers it can carry declares the number, and the
// control plane sums it into the fleet's worker-slot capacity.
func TestConfig_MaxConcurrentJobs_AcceptsADeclaredCount(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	cfg.MaxConcurrentJobs = 4
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a declared slot count must validate: %v", err)
	}
}

// TestConfig_MaxConcurrentJobs_ZeroReportsNothing: zero is the legitimate
// "this host declares no count" value and must validate, because it is the
// shipped default for every daemon that predates the field.
func TestConfig_MaxConcurrentJobs_ZeroReportsNothing(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	if cfg.MaxConcurrentJobs != 0 {
		t.Fatalf("the zero value must be the unreported one, got %d", cfg.MaxConcurrentJobs)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an unreported slot count must validate: %v", err)
	}
}

// TestConfig_MaxConcurrentJobs_RefusesANegativeCount: a negative number cannot be
// a capacity, and clamping it to zero would turn an operator's typo into a host
// that silently stopped counting towards the fleet.
func TestConfig_MaxConcurrentJobs_RefusesANegativeCount(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	cfg.MaxConcurrentJobs = -2

	err := cfg.Validate()
	if !errors.Is(err, ErrConfigNegativeCapacity) {
		t.Fatalf("want ErrConfigNegativeCapacity, got %v", err)
	}
	if !strings.Contains(err.Error(), "max_concurrent_jobs") {
		t.Fatalf("the refusal must name the setting: %v", err)
	}
}

// TestHeartbeatBody_CarriesTheDeclaredCapacity proves the field reaches the wire.
// The fleet's capacity is summed from what daemons report, so a count the daemon
// holds and never sends is a count the fleet does not have.
func TestHeartbeatBody_CarriesTheDeclaredCapacity(t *testing.T) {
	t.Parallel()

	body := heartbeatRequestBody(HeartbeatStatus{
		ActiveSessions:    1,
		Backends:          []string{"claude"},
		MaxConcurrentJobs: 6,
		SequenceNumber:    3,
		BindAddr:          "127.0.0.1:9091",
	})
	if !strings.Contains(body, `"max_concurrent_jobs":6`) {
		t.Fatalf("the heartbeat must report the declared slot count: %s", body)
	}
}

// TestHeartbeatBody_ReportsNothingWhenNothingIsDeclared: an undeclared count goes
// out as zero, which the control plane reads as "reported nothing" and which
// leaves the record's existing count alone.
func TestHeartbeatBody_ReportsNothingWhenNothingIsDeclared(t *testing.T) {
	t.Parallel()

	body := heartbeatRequestBody(HeartbeatStatus{Backends: []string{"claude"}})
	if !strings.Contains(body, `"max_concurrent_jobs":0`) {
		t.Fatalf("an undeclared count must still be a well-formed zero: %s", body)
	}
}
