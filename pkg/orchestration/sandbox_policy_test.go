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

package orchestration

import (
	"errors"
	"testing"
)

func TestSandboxMode_String(t *testing.T) {
	cases := map[SandboxMode]string{
		SandboxModeDefault:  "default",
		SandboxModeRequired: "required",
		SandboxModeNone:     "none",
		SandboxMode(99):     "unknown",
	}
	for mode, want := range cases {
		if got := mode.String(); got != want {
			t.Errorf("SandboxMode(%d).String() = %q, want %q", mode, got, want)
		}
	}
}

func TestSandboxPolicy_String(t *testing.T) {
	cases := map[SandboxPolicy]string{
		SandboxPolicyRequired:  "required",
		SandboxPolicyPreferred: "preferred",
		SandboxPolicyOptIn:     "opt-in",
		SandboxPolicy(99):      "unknown",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("SandboxPolicy(%d).String() = %q, want %q", p, got, want)
		}
	}
}

func TestParseSandboxMode(t *testing.T) {
	cases := []struct {
		in      string
		want    SandboxMode
		wantErr bool
	}{
		{"", SandboxModeDefault, false},
		{"default", SandboxModeDefault, false},
		{"required", SandboxModeRequired, false},
		{"none", SandboxModeNone, false},
		{"bogus", SandboxModeDefault, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseSandboxMode(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got=%v, want=%v", got, tc.want)
			}
		})
	}
}

func TestParseSandboxPolicy(t *testing.T) {
	cases := []struct {
		in      string
		want    SandboxPolicy
		wantErr bool
	}{
		{"", SandboxPolicyOptIn, false},
		{"required", SandboxPolicyRequired, false},
		{"preferred", SandboxPolicyPreferred, false},
		{"opt-in", SandboxPolicyOptIn, false},
		{"optin", SandboxPolicyOptIn, false},
		{"opt_in", SandboxPolicyOptIn, false},
		{"loose", SandboxPolicyOptIn, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseSandboxPolicy(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got=%v, want=%v", got, tc.want)
			}
		})
	}
}

// TestResolveSandboxMode covers the full matrix of (requested mode, server
// policy, runtime availability) plus a tenant override axis. This is the
// load-bearing policy-engine test for Phase 1.
func TestResolveSandboxMode(t *testing.T) {
	ptr := func(p SandboxPolicy) *SandboxPolicy { return &p }

	type tc struct {
		name           string
		requested      SandboxMode
		serverPolicy   SandboxPolicy
		tenantOverride *SandboxPolicy
		hasRuntime     bool
		wantMode       SandboxMode
		wantViolation  bool
	}

	cases := []tc{
		// SandboxModeRequired: always honored if runtime present, rejected otherwise.
		{"required_with_runtime_server_required", SandboxModeRequired, SandboxPolicyRequired, nil, true, SandboxModeRequired, false},
		{"required_with_runtime_server_preferred", SandboxModeRequired, SandboxPolicyPreferred, nil, true, SandboxModeRequired, false},
		{"required_with_runtime_server_optin", SandboxModeRequired, SandboxPolicyOptIn, nil, true, SandboxModeRequired, false},
		{"required_no_runtime", SandboxModeRequired, SandboxPolicyPreferred, nil, false, SandboxModeRequired, true},

		// SandboxModeNone: honored except under required policy.
		{"none_server_required", SandboxModeNone, SandboxPolicyRequired, nil, true, SandboxModeNone, true},
		{"none_server_preferred", SandboxModeNone, SandboxPolicyPreferred, nil, true, SandboxModeNone, false},
		{"none_server_optin", SandboxModeNone, SandboxPolicyOptIn, nil, true, SandboxModeNone, false},
		{"none_no_runtime_server_optin", SandboxModeNone, SandboxPolicyOptIn, nil, false, SandboxModeNone, false},

		// SandboxModeDefault under required: container, needs runtime.
		{"default_server_required_with_runtime", SandboxModeDefault, SandboxPolicyRequired, nil, true, SandboxModeRequired, false},
		{"default_server_required_no_runtime", SandboxModeDefault, SandboxPolicyRequired, nil, false, SandboxModeRequired, true},

		// SandboxModeDefault under preferred: container when available, fallback to raw.
		{"default_server_preferred_with_runtime", SandboxModeDefault, SandboxPolicyPreferred, nil, true, SandboxModeRequired, false},
		{"default_server_preferred_no_runtime", SandboxModeDefault, SandboxPolicyPreferred, nil, false, SandboxModeNone, false},

		// SandboxModeDefault under opt-in: raw regardless of runtime.
		{"default_server_optin_with_runtime", SandboxModeDefault, SandboxPolicyOptIn, nil, true, SandboxModeNone, false},
		{"default_server_optin_no_runtime", SandboxModeDefault, SandboxPolicyOptIn, nil, false, SandboxModeNone, false},

		// Tenant override tightens a loose server policy.
		{"tenant_required_overrides_optin_default", SandboxModeDefault, SandboxPolicyOptIn, ptr(SandboxPolicyRequired), true, SandboxModeRequired, false},
		{"tenant_required_overrides_optin_default_no_runtime", SandboxModeDefault, SandboxPolicyOptIn, ptr(SandboxPolicyRequired), false, SandboxModeRequired, true},
		{"tenant_required_overrides_none_request", SandboxModeNone, SandboxPolicyOptIn, ptr(SandboxPolicyRequired), true, SandboxModeNone, true},

		// A tenant override may only TIGHTEN: a laxer one is ignored and the
		// server policy still governs. Otherwise a tenant record would be a way
		// out of the operator's baseline.
		{"tenant_optin_cannot_loosen_required_none", SandboxModeNone, SandboxPolicyRequired, ptr(SandboxPolicyOptIn), true, SandboxModeNone, true},
		{"tenant_preferred_cannot_loosen_required_with_runtime", SandboxModeDefault, SandboxPolicyRequired, ptr(SandboxPolicyPreferred), true, SandboxModeRequired, false},
		{"tenant_preferred_cannot_loosen_required_no_runtime", SandboxModeDefault, SandboxPolicyRequired, ptr(SandboxPolicyPreferred), false, SandboxModeRequired, true},

		// Nil tenant override is a no-op.
		{"nil_override_equals_server_policy", SandboxModeDefault, SandboxPolicyRequired, nil, true, SandboxModeRequired, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSandboxMode(tc.requested, tc.serverPolicy, tc.tenantOverride, tc.hasRuntime)
			if tc.wantViolation {
				var v *SandboxModeViolationError
				if !errors.As(err, &v) {
					t.Fatalf("expected SandboxModeViolationError, got %T: %v", err, err)
				}
				if v.Requested != tc.requested {
					t.Errorf("violation.Requested = %v, want %v", v.Requested, tc.requested)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantMode {
				t.Errorf("mode = %v, want %v", got, tc.wantMode)
			}
		})
	}
}

// TestResolveSandboxMode_UnknownMode verifies defensive handling of invalid
// SandboxMode values (e.g. from malformed wire data).
func TestResolveSandboxMode_UnknownMode(t *testing.T) {
	_, err := ResolveSandboxMode(SandboxMode(99), SandboxPolicyPreferred, nil, true)
	var v *SandboxModeViolationError
	if !errors.As(err, &v) {
		t.Fatalf("expected SandboxModeViolationError, got %T: %v", err, err)
	}
}

// TestResolveSandboxMode_UnknownPolicy verifies defensive handling of invalid
// SandboxPolicy values.
func TestResolveSandboxMode_UnknownPolicy(t *testing.T) {
	_, err := ResolveSandboxMode(SandboxModeDefault, SandboxPolicy(99), nil, true)
	var v *SandboxModeViolationError
	if !errors.As(err, &v) {
		t.Fatalf("expected SandboxModeViolationError, got %T: %v", err, err)
	}
}

// TestSandboxModeViolationError_Message checks that the error carries enough
// diagnostic context for operators (mode + policy + reason).
func TestSandboxModeViolationError_Message(t *testing.T) {
	err := &SandboxModeViolationError{
		Requested: SandboxModeRequired,
		Policy:    SandboxPolicyOptIn,
		Reason:    "no container runtime available on target host",
	}
	msg := err.Error()
	for _, want := range []string{"required", "opt-in", "no container runtime"} {
		if !contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
}

// TestSpawnRequest_EffectiveSandboxMode verifies the backend-level request
// helper maps legacy Sandboxed bool to the new mode.
func TestSpawnRequest_EffectiveSandboxMode(t *testing.T) {
	cases := []struct {
		name string
		req  SpawnRequest
		want SandboxMode
	}{
		{"explicit_required_wins", SpawnRequest{SandboxMode: SandboxModeRequired, Sandboxed: false}, SandboxModeRequired},
		{"explicit_none_wins_over_sandboxed_true", SpawnRequest{SandboxMode: SandboxModeNone, Sandboxed: true}, SandboxModeNone},
		{"legacy_sandboxed_true_becomes_required", SpawnRequest{Sandboxed: true}, SandboxModeRequired},
		{"legacy_sandboxed_false_stays_default", SpawnRequest{}, SandboxModeDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.EffectiveSandboxMode(); got != tc.want {
				t.Errorf("EffectiveSandboxMode() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSpawnSessionRequest_EffectiveSandboxMode mirrors the backend-level
// check for the orchestrator-facing request type.
func TestSpawnSessionRequest_EffectiveSandboxMode(t *testing.T) {
	cases := []struct {
		name string
		req  SpawnSessionRequest
		want SandboxMode
	}{
		{"explicit_required", SpawnSessionRequest{SandboxMode: SandboxModeRequired}, SandboxModeRequired},
		{"explicit_none_overrides_legacy", SpawnSessionRequest{SandboxMode: SandboxModeNone, Sandboxed: true}, SandboxModeNone},
		{"legacy_sandboxed_true", SpawnSessionRequest{Sandboxed: true}, SandboxModeRequired},
		{"zero_value", SpawnSessionRequest{}, SandboxModeDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.EffectiveSandboxMode(); got != tc.want {
				t.Errorf("EffectiveSandboxMode() = %v, want %v", got, tc.want)
			}
		})
	}
}

// contains is a local substring helper that avoids pulling in strings just
// for test assertions.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
