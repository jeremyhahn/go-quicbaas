package sandbox

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestResolver_TenantOverride_Tightens mirrors the orchestration-level
// override test at the daemon's resolver (moved from pkg/fleet).
func TestResolver_TenantOverride_Tightens(t *testing.T) {
	t.Parallel()
	r := NewResolver(orchestration.SandboxPolicyOptIn, false)

	// Baseline: opt-in + DEFAULT → NONE.
	mode, err := r.Resolve(orchestration.SandboxModeDefault)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if mode != orchestration.SandboxModeNone {
		t.Errorf("opt-in default: got %v, want NONE", mode)
	}

	// Tenant override to REQUIRED without runtime → violation.
	tightened := orchestration.SandboxPolicyRequired
	_, err = r.WithTenantOverride(&tightened).Resolve(orchestration.SandboxModeDefault)
	var v *orchestration.SandboxModeViolationError
	if !errors.As(err, &v) {
		t.Fatalf("expected violation, got %v", err)
	}

	// Same tenant override with runtime present → REQUIRED.
	withRT := NewResolver(orchestration.SandboxPolicyOptIn, true).WithTenantOverride(&tightened)
	mode, err = withRT.Resolve(orchestration.SandboxModeDefault)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if mode != orchestration.SandboxModeRequired {
		t.Errorf("got %v, want REQUIRED", mode)
	}
	if withRT.Policy() != orchestration.SandboxPolicyOptIn || !withRT.HasContainerRuntime() {
		t.Error("override changed the daemon policy or runtime flag")
	}
}
