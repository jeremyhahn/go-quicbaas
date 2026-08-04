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

package quicbaas

import (
	"context"
	"errors"
	"testing"
)

// TestHealthCheckerReexport_ProbeLifecycle proves the health aliases are
// usable end-to-end through the quicbaas SDK surface: register a check,
// pass startup, and read readiness results.
func TestHealthCheckerReexport_ProbeLifecycle(t *testing.T) {
	c := NewHealthChecker()
	c.RegisterCheck("store", func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{Name: "store", Status: HealthCheckStatusHealthy}
	})
	c.MarkStarted()

	if !c.IsStarted() {
		t.Fatal("IsStarted = false after MarkStarted")
	}
	results := c.Ready(context.Background())
	if len(results) != 1 || results[0].Status != HealthCheckStatusHealthy {
		t.Fatalf("Ready = %+v, want one healthy result", results)
	}
	if got := AggregateHealthCheckStatus(results); got != HealthCheckStatusHealthy {
		t.Errorf("AggregateHealthCheckStatus = %v, want healthy", got)
	}
}

// TestHealthCheckerReexport_UnhealthyAggregation verifies the failure path:
// an unhealthy check dominates aggregation.
func TestHealthCheckerReexport_UnhealthyAggregation(t *testing.T) {
	results := []HealthCheckResult{
		{Name: "a", Status: HealthCheckStatusHealthy},
		{Name: "b", Status: HealthCheckStatusUnhealthy},
	}
	if got := AggregateHealthCheckStatus(results); got != HealthCheckStatusUnhealthy {
		t.Errorf("AggregateHealthCheckStatus = %v, want unhealthy", got)
	}
}

// TestValidationReexport_ErrorRoundTrip proves the validation aliases work:
// constructing a typed error and matching it via errors.As.
func TestValidationReexport_ErrorRoundTrip(t *testing.T) {
	err := NewValidationError("tenant_id", ValidationConstraintInvalidFormat, "must be lowercase")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("errors.As failed for %T", err)
	}
	if ve.Field != "tenant_id" {
		t.Errorf("Field = %q, want tenant_id", ve.Field)
	}
}

// TestValidationReexport_SanitizeForLog verifies the log-sanitizer alias
// neutralizes newline injection.
func TestValidationReexport_SanitizeForLog(t *testing.T) {
	out := SanitizeForLog("user\ninjected")
	if out == "user\ninjected" {
		t.Error("SanitizeForLog left raw newline in place")
	}
}
