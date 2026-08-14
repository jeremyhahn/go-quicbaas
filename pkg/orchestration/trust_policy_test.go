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

package orchestration

import (
	"testing"
)

// allCapabilities is a host that can satisfy every tier.
var allCapabilities = TrustCapabilities{DigestPinning: true, HostTPM: true, MeasuredLaunch: true}

// pinOnlyCapabilities is a host with no TPM: Tier 0 only.
var pinOnlyCapabilities = TrustCapabilities{DigestPinning: true}

func TestTrustLevel_StringAndParse(t *testing.T) {
	levels := []TrustLevel{
		TrustLevelDefault, TrustLevelNone, TrustLevelPinned,
		TrustLevelAttested, TrustLevelMeasured,
	}
	for _, level := range levels {
		parsed, err := ParseTrustLevel(level.String())
		if err != nil {
			t.Fatalf("ParseTrustLevel(%q): %v", level.String(), err)
		}
		if parsed != level {
			t.Fatalf("round-trip failed: %v -> %q -> %v", level, level.String(), parsed)
		}
	}

	t.Run("EmptyIsDefault", func(t *testing.T) {
		parsed, err := ParseTrustLevel("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed != TrustLevelDefault {
			t.Fatalf("empty string must map to default, got %v", parsed)
		}
	})

	t.Run("UnknownRejected", func(t *testing.T) {
		if _, err := ParseTrustLevel("paranoid"); err == nil {
			t.Fatal("expected an error for an unknown trust level")
		}
	})

	t.Run("UnknownStringsAsUnknown", func(t *testing.T) {
		if got := TrustLevel(99).String(); got != "unknown" {
			t.Fatalf("String() = %q, want unknown", got)
		}
		if TrustLevel(99).IsValid() {
			t.Fatal("out-of-range level must not be valid")
		}
	})
}

func TestTrustPolicy_StringAndParse(t *testing.T) {
	policies := []TrustPolicy{
		TrustPolicyOptIn, TrustPolicyPinned, TrustPolicyAttested, TrustPolicyMeasured,
	}
	for _, policy := range policies {
		parsed, err := ParseTrustPolicy(policy.String())
		if err != nil {
			t.Fatalf("ParseTrustPolicy(%q): %v", policy.String(), err)
		}
		if parsed != policy {
			t.Fatalf("round-trip failed: %v -> %q -> %v", policy, policy.String(), parsed)
		}
	}

	t.Run("EmptyIsOptIn", func(t *testing.T) {
		parsed, err := ParseTrustPolicy("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed != TrustPolicyOptIn {
			t.Fatalf("empty string must map to opt-in, got %v", parsed)
		}
	})

	t.Run("UnknownRejected", func(t *testing.T) {
		if _, err := ParseTrustPolicy("maximum"); err == nil {
			t.Fatal("expected an error for an unknown trust policy")
		}
		if got := TrustPolicy(99).String(); got != "unknown" {
			t.Fatalf("String() = %q, want unknown", got)
		}
	})
}

// TestResolveTrustLevel_ZeroValuePreservesLegacyBehavior pins the additive
// guarantee on the policy axis: an orchestrator that never configured trusted
// computing resolves DEFAULT to NONE and rejects nothing.
func TestResolveTrustLevel_ZeroValuePreservesLegacyBehavior(t *testing.T) {
	resolved, err := ResolveTrustLevel(TrustLevelDefault, TrustPolicyOptIn, nil, TrustCapabilities{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != TrustLevelNone {
		t.Fatalf("resolved = %v, want none", resolved)
	}
}

func TestResolveTrustLevel(t *testing.T) {
	tests := []struct {
		name      string
		requested TrustLevel
		policy    TrustPolicy
		caps      TrustCapabilities
		want      TrustLevel
		wantErr   bool
	}{
		{
			name:      "DefaultUnderPinnedPolicyResolvesToPinned",
			requested: TrustLevelDefault,
			policy:    TrustPolicyPinned,
			caps:      pinOnlyCapabilities,
			want:      TrustLevelPinned,
		},
		{
			name:      "DefaultUnderAttestedPolicyResolvesToAttested",
			requested: TrustLevelDefault,
			policy:    TrustPolicyAttested,
			caps:      allCapabilities,
			want:      TrustLevelAttested,
		},
		{
			name:      "DefaultUnderMeasuredPolicyResolvesToMeasured",
			requested: TrustLevelDefault,
			policy:    TrustPolicyMeasured,
			caps:      allCapabilities,
			want:      TrustLevelMeasured,
		},
		{
			name:      "ExplicitAboveFloorIsHonored",
			requested: TrustLevelMeasured,
			policy:    TrustPolicyPinned,
			caps:      allCapabilities,
			want:      TrustLevelMeasured,
		},
		{
			name:      "ExplicitNoneUnderOptInIsHonored",
			requested: TrustLevelNone,
			policy:    TrustPolicyOptIn,
			caps:      TrustCapabilities{},
			want:      TrustLevelNone,
		},
		{
			name:      "ExplicitNoneBelowFloorIsRejected",
			requested: TrustLevelNone,
			policy:    TrustPolicyPinned,
			caps:      allCapabilities,
			wantErr:   true,
		},
		{
			name:      "ExplicitPinnedBelowAttestedFloorIsRejected",
			requested: TrustLevelPinned,
			policy:    TrustPolicyAttested,
			caps:      allCapabilities,
			wantErr:   true,
		},
		{
			name:      "RequiredTierWithoutCapabilityIsDenied",
			requested: TrustLevelAttested,
			policy:    TrustPolicyOptIn,
			caps:      pinOnlyCapabilities,
			wantErr:   true,
		},
		{
			name:      "MeasuredWithoutMeasuredLaunchIsDenied",
			requested: TrustLevelMeasured,
			policy:    TrustPolicyOptIn,
			caps:      TrustCapabilities{DigestPinning: true, HostTPM: true},
			wantErr:   true,
		},
		{
			name:      "PinnedWithoutDigestPinningIsDenied",
			requested: TrustLevelPinned,
			policy:    TrustPolicyOptIn,
			caps:      TrustCapabilities{},
			wantErr:   true,
		},
		{
			name:      "DefaultUnderRequiredPolicyIsDeniedNotDowngraded",
			requested: TrustLevelDefault,
			policy:    TrustPolicyAttested,
			caps:      pinOnlyCapabilities,
			wantErr:   true,
		},
		{
			name:      "UnknownLevelRejected",
			requested: TrustLevel(99),
			policy:    TrustPolicyOptIn,
			caps:      allCapabilities,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := ResolveTrustLevel(tt.requested, tt.policy, nil, tt.caps)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected a violation, got resolved=%v", resolved)
				}
				violation, ok := err.(*TrustLevelViolationError)
				if !ok {
					t.Fatalf("expected *TrustLevelViolationError, got %T", err)
				}
				if violation.Error() == "" {
					t.Fatal("violation message must not be empty")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resolved != tt.want {
				t.Fatalf("resolved = %v, want %v", resolved, tt.want)
			}
		})
	}
}

// TestResolveTrustLevel_TenantOverride mirrors the sandbox axis: a tenant with
// a stricter obligation raises the floor without the operator tightening the
// whole deployment, and a laxer tenant override is IGNORED — an override is a
// one-way ratchet, so no tenant record can lower the operator's baseline.
func TestResolveTrustLevel_TenantOverride(t *testing.T) {
	t.Run("StricterTenantRaisesFloor", func(t *testing.T) {
		override := TrustPolicyMeasured
		resolved, err := ResolveTrustLevel(TrustLevelDefault, TrustPolicyOptIn, &override, allCapabilities)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved != TrustLevelMeasured {
			t.Fatalf("resolved = %v, want measured", resolved)
		}
	})

	t.Run("StricterTenantRejectsExplicitNone", func(t *testing.T) {
		override := TrustPolicyPinned
		if _, err := ResolveTrustLevel(TrustLevelNone, TrustPolicyOptIn, &override, allCapabilities); err == nil {
			t.Fatal("expected the tenant override to reject an explicit none")
		}
	})

	t.Run("LaxerTenantCannotLowerFloor", func(t *testing.T) {
		override := TrustPolicyOptIn
		resolved, err := ResolveTrustLevel(TrustLevelDefault, TrustPolicyMeasured, &override, allCapabilities)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved != TrustLevelMeasured {
			t.Fatalf("resolved = %v, want the server floor measured", resolved)
		}
	})

	t.Run("LaxerTenantCannotAdmitExplicitNone", func(t *testing.T) {
		override := TrustPolicyOptIn
		if _, err := ResolveTrustLevel(
			TrustLevelNone, TrustPolicyMeasured, &override, allCapabilities); err == nil {
			t.Fatal("expected the server floor to reject an explicit none")
		}
	})
}

func TestTrustCapabilities_MissingCapability(t *testing.T) {
	tests := []struct {
		name  string
		caps  TrustCapabilities
		level TrustLevel
		want  string
	}{
		{name: "NoPinning", caps: TrustCapabilities{}, level: TrustLevelPinned, want: "image digest pinning"},
		{name: "NoTPM", caps: pinOnlyCapabilities, level: TrustLevelAttested, want: "host TPM attestation"},
		{
			name:  "NoMeasuredLaunch",
			caps:  TrustCapabilities{DigestPinning: true, HostTPM: true},
			level: TrustLevelMeasured,
			want:  "measured launch",
		},
		{name: "Satisfied", caps: allCapabilities, level: TrustLevelMeasured, want: "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.caps.missingCapability(tt.level); got != tt.want {
				t.Fatalf("missingCapability = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTrustAxisIsIndependentOfSandboxAxis documents (and pins) that the two
// axes do not interfere: an unsandboxed execution can still be digest-pinned,
// and a sandboxed one can carry no trust requirement.
func TestTrustAxisIsIndependentOfSandboxAxis(t *testing.T) {
	sandboxMode, err := ResolveSandboxMode(SandboxModeNone, SandboxPolicyOptIn, nil, false)
	if err != nil {
		t.Fatalf("unexpected sandbox error: %v", err)
	}
	if sandboxMode != SandboxModeNone {
		t.Fatalf("sandbox mode = %v, want none", sandboxMode)
	}

	trustLevel, err := ResolveTrustLevel(TrustLevelPinned, TrustPolicyOptIn, nil, pinOnlyCapabilities)
	if err != nil {
		t.Fatalf("unexpected trust error: %v", err)
	}
	if trustLevel != TrustLevelPinned {
		t.Fatalf("trust level = %v, want pinned", trustLevel)
	}
}
