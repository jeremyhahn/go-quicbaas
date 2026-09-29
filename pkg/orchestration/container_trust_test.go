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
	"errors"
	"reflect"
	"strings"
	"testing"
)

// testDigest is a well-formed SHA-256 OCI content digest.
const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// otherDigest is a second, distinct well-formed digest.
const otherDigest = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

func TestValidateImageDigest(t *testing.T) {
	tests := []struct {
		name    string
		digest  string
		wantErr bool
	}{
		{name: "Canonical", digest: testDigest},
		{name: "Empty", digest: "", wantErr: true},
		{name: "MissingPrefix", digest: strings.TrimPrefix(testDigest, "sha256:"), wantErr: true},
		{name: "WrongAlgorithm", digest: "sha512:" + strings.Repeat("a", 64), wantErr: true},
		{name: "ShortHex", digest: "sha256:" + strings.Repeat("a", 63), wantErr: true},
		{name: "LongHex", digest: "sha256:" + strings.Repeat("a", 65), wantErr: true},
		{name: "NonHex", digest: "sha256:" + strings.Repeat("z", 64), wantErr: true},
		{name: "Uppercase", digest: "sha256:" + strings.Repeat("A", 64), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateImageDigest(tt.digest)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for digest %q", tt.digest)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for digest %q: %v", tt.digest, err)
			}
		})
	}
}

func TestSplitImageReference(t *testing.T) {
	tests := []struct {
		name           string
		image          string
		wantRepository string
		wantDigest     string
		wantPinned     bool
	}{
		{
			name:           "TagOnly",
			image:          "alpine:3.20",
			wantRepository: "alpine:3.20",
		},
		{
			name:           "Pinned",
			image:          "alpine@" + testDigest,
			wantRepository: "alpine",
			wantDigest:     testDigest,
			wantPinned:     true,
		},
		{
			name:           "PinnedWithRegistryAndTag",
			image:          "registry.example.com:5000/team/agent:v1@" + testDigest,
			wantRepository: "registry.example.com:5000/team/agent:v1",
			wantDigest:     testDigest,
			wantPinned:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository, digest, pinned := SplitImageReference(tt.image)
			if repository != tt.wantRepository {
				t.Errorf("repository = %q, want %q", repository, tt.wantRepository)
			}
			if digest != tt.wantDigest {
				t.Errorf("digest = %q, want %q", digest, tt.wantDigest)
			}
			if pinned != tt.wantPinned {
				t.Errorf("pinned = %v, want %v", pinned, tt.wantPinned)
			}
		})
	}
}

func TestContainerSpec_PinnedImageReference(t *testing.T) {
	t.Run("ExplicitDigestRewritesTag", func(t *testing.T) {
		spec := &ContainerSpec{Image: "alpine:3.20", ImageDigest: testDigest}
		want := "alpine:3.20@" + testDigest
		if got := spec.PinnedImageReference(); got != want {
			t.Fatalf("PinnedImageReference() = %q, want %q", got, want)
		}
		if !spec.IsPinned() {
			t.Fatal("expected spec to report as pinned")
		}
	})

	t.Run("InlineDigestPreserved", func(t *testing.T) {
		spec := &ContainerSpec{Image: "alpine@" + testDigest}
		if got := spec.PinnedImageReference(); got != "alpine@"+testDigest {
			t.Fatalf("PinnedImageReference() = %q", got)
		}
		if got := spec.ResolvedImageDigest(); got != testDigest {
			t.Fatalf("ResolvedImageDigest() = %q", got)
		}
	})

	t.Run("UnpinnedUnchanged", func(t *testing.T) {
		spec := &ContainerSpec{Image: "alpine:3.20"}
		if got := spec.PinnedImageReference(); got != "alpine:3.20" {
			t.Fatalf("PinnedImageReference() = %q, want the reference unchanged", got)
		}
		if spec.IsPinned() {
			t.Fatal("expected unpinned spec to report as unpinned")
		}
	})
}

// TestContainerSpec_ValidateTrust_ZeroValueUnchanged pins the additive
// guarantee: a spec built before the trust dimension existed must validate
// exactly as it did before.
func TestContainerSpec_ValidateTrust_ZeroValueUnchanged(t *testing.T) {
	spec := &ContainerSpec{
		Image: "alpine:3.20",
		Env:   map[string]string{"A": "1"},
		Mounts: []ContainerMount{
			{Source: "/host", Target: "/container"},
		},
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("zero-valued trust fields must not change validation: %v", err)
	}
}

func TestContainerSpec_ValidateTrust_Errors(t *testing.T) {
	tests := []struct {
		name    string
		spec    *ContainerSpec
		wantIs  error
		wantErr bool
	}{
		{
			name: "TagInsteadOfDigestRejectedAtPinnedLevel",
			spec: &ContainerSpec{
				Image:      "alpine:3.20",
				TrustLevel: TrustLevelPinned,
			},
			wantIs:  ErrImageNotPinned,
			wantErr: true,
		},
		{
			name: "TagInsteadOfDigestRejectedAtAttestedLevel",
			spec: &ContainerSpec{
				Image:        "alpine:3.20",
				TrustLevel:   TrustLevelAttested,
				ExpectedPCRs: map[uint32][]byte{7: {0x01}},
			},
			wantIs:  ErrImageNotPinned,
			wantErr: true,
		},
		{
			name: "InlineAndExplicitDigestDisagree",
			spec: &ContainerSpec{
				Image:       "alpine@" + testDigest,
				ImageDigest: otherDigest,
			},
			wantIs:  ErrImageDigestMismatch,
			wantErr: true,
		},
		{
			name: "SignatureDigestDisagrees",
			spec: &ContainerSpec{
				Image:       "alpine",
				ImageDigest: testDigest,
				ImageSignature: &ImageSignature{
					Digest:         otherDigest,
					Signature:      []byte{0x01},
					SignerChainPEM: [][]byte{[]byte("pem")},
				},
			},
			wantIs:  ErrImageDigestMismatch,
			wantErr: true,
		},
		{
			name: "MalformedExplicitDigest",
			spec: &ContainerSpec{
				Image:       "alpine",
				ImageDigest: "sha256:nothex",
			},
			wantErr: true,
		},
		{
			name: "AttestedWithoutExpectedPCRs",
			spec: &ContainerSpec{
				Image:      "alpine@" + testDigest,
				TrustLevel: TrustLevelAttested,
			},
			wantErr: true,
		},
		{
			name: "PCRIndexOutOfRange",
			spec: &ContainerSpec{
				Image:        "alpine@" + testDigest,
				TrustLevel:   TrustLevelAttested,
				ExpectedPCRs: map[uint32][]byte{MaxPCRIndex + 1: {0x01}},
			},
			wantErr: true,
		},
		{
			name: "EmptyExpectedPCRDigest",
			spec: &ContainerSpec{
				Image:        "alpine@" + testDigest,
				TrustLevel:   TrustLevelAttested,
				ExpectedPCRs: map[uint32][]byte{7: {}},
			},
			wantErr: true,
		},
		{
			name: "UnsupportedMeasurementBank",
			spec: &ContainerSpec{
				Image:           "alpine@" + testDigest,
				MeasurementBank: "md5",
			},
			wantErr: true,
		},
		{
			name: "MeasurementPCROutOfRange",
			spec: &ContainerSpec{
				Image:          "alpine@" + testDigest,
				MeasurementPCR: MaxPCRIndex + 1,
			},
			wantErr: true,
		},
		{
			name: "UnknownTrustLevel",
			spec: &ContainerSpec{
				Image:      "alpine@" + testDigest,
				TrustLevel: TrustLevel(99),
			},
			wantErr: true,
		},
		{
			name: "PinnedIsAccepted",
			spec: &ContainerSpec{
				Image:      "alpine@" + testDigest,
				TrustLevel: TrustLevelPinned,
			},
		},
		{
			name: "MeasuredIsAccepted",
			spec: &ContainerSpec{
				Image:        "alpine@" + testDigest,
				TrustLevel:   TrustLevelMeasured,
				ExpectedPCRs: map[uint32][]byte{7: {0x01}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("error %v does not wrap %v", err, tt.wantIs)
			}
		})
	}
}

func TestImageSignature_Validate(t *testing.T) {
	t.Run("Complete", func(t *testing.T) {
		sig := &ImageSignature{
			Digest:         testDigest,
			Signature:      []byte{0x01},
			SignerChainPEM: [][]byte{[]byte("pem")},
		}
		if err := sig.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("Incomplete", func(t *testing.T) {
		cases := map[string]*ImageSignature{
			"Nil":          nil,
			"BadDigest":    {Digest: "nope", Signature: []byte{1}, SignerChainPEM: [][]byte{{1}}},
			"NoSignature":  {Digest: testDigest, SignerChainPEM: [][]byte{{1}}},
			"NoSignerCert": {Digest: testDigest, Signature: []byte{1}},
		}
		for name, sig := range cases {
			t.Run(name, func(t *testing.T) {
				if err := sig.Validate(); err == nil {
					t.Fatal("expected an error")
				}
			})
		}
	})
}

// =============================================================================
// Deterministic Configuration Digest
// =============================================================================

func TestContainerConfigDigest_Deterministic(t *testing.T) {
	build := func() *ContainerSpec {
		return &ContainerSpec{
			Name:        "instance-a",
			Image:       "alpine:3.20",
			ImageDigest: testDigest,
			Env:         map[string]string{"A": "1", "B": "2", "C": "3"},
			Labels:      map[string]string{"x": "1", "y": "2", "z": "3"},
			Mounts: []ContainerMount{
				{Source: "/a", Target: "/x", ReadOnly: true},
				{Source: "/b", Target: "/y"},
			},
			Ports:      []string{"8080:80"},
			WorkDir:    "/work",
			Entrypoint: []string{"/bin/agent"},
			Command:    []string{"serve"},
			Args:       []string{"--verbose"},
			Network:    "bridge",
			User:       "1000:1000",
			TrustLabel: "prod-agent",
		}
	}

	first, err := ContainerConfigDigest(build())
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	if !strings.HasPrefix(first, "sha256:") {
		t.Fatalf("digest %q is not canonical", first)
	}

	for i := 0; i < 32; i++ {
		next, err := ContainerConfigDigest(build())
		if err != nil {
			t.Fatalf("ContainerConfigDigest: %v", err)
		}
		if next != first {
			t.Fatalf("digest is not deterministic: %q != %q", next, first)
		}
	}
}

// TestContainerConfigDigest_DeterministicAcrossMapOrdering is the guard the
// measured-launch design depends on: Go randomizes map iteration order per
// process AND per range statement, so a naive range over Env or Labels would
// produce a different measurement on every launch. Building the same logical
// spec from maps populated in different insertion orders, many times, must
// still yield one digest.
func TestContainerConfigDigest_DeterministicAcrossMapOrdering(t *testing.T) {
	forward := &ContainerSpec{
		Image:       "alpine",
		ImageDigest: testDigest,
		Env:         map[string]string{},
		Labels:      map[string]string{},
	}
	reverse := &ContainerSpec{
		Image:       "alpine",
		ImageDigest: testDigest,
		Env:         map[string]string{},
		Labels:      map[string]string{},
	}

	keys := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	for i, k := range keys {
		forward.Env[k] = k + "-value"
		forward.Labels[k] = k + "-label"
		rk := keys[len(keys)-1-i]
		reverse.Env[rk] = rk + "-value"
		reverse.Labels[rk] = rk + "-label"

		// The capability sets carry the same guarantee: order-independent,
		// and therefore reproducible across launches.
		forward.CapAdd = append(forward.CapAdd, "CAP_"+strings.ToUpper(k))
		reverse.CapAdd = append(reverse.CapAdd, "CAP_"+strings.ToUpper(rk))
		forward.CapDrop = append(forward.CapDrop, "CAP_DROP_"+strings.ToUpper(k))
		reverse.CapDrop = append(reverse.CapDrop, "CAP_DROP_"+strings.ToUpper(rk))
	}

	want, err := ContainerConfigDigest(forward)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}

	// Repeat enough times that a map-order dependency would almost certainly
	// surface: each call performs its own randomized range.
	for i := 0; i < 200; i++ {
		got, err := ContainerConfigDigest(reverse)
		if err != nil {
			t.Fatalf("ContainerConfigDigest: %v", err)
		}
		if got != want {
			t.Fatalf("digest depends on map iteration order: %q != %q", got, want)
		}
	}
}

// TestContainerConfigDigest_ExcludesInstanceName proves the digest measures
// the approved configuration, not the instance: two launches of the same
// configuration under different generated container names must measure the
// same.
func TestContainerConfigDigest_ExcludesInstanceName(t *testing.T) {
	a := &ContainerSpec{Name: "quicbaas-acp-aaaa", Image: "alpine", ImageDigest: testDigest}
	b := &ContainerSpec{Name: "quicbaas-acp-bbbb", Image: "alpine", ImageDigest: testDigest}

	da, err := ContainerConfigDigest(a)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	db, err := ContainerConfigDigest(b)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	if da != db {
		t.Fatalf("container name must not affect the measurement: %q != %q", da, db)
	}
}

func TestContainerConfigDigest_DistinguishesConfigurations(t *testing.T) {
	base := func() *ContainerSpec {
		return &ContainerSpec{
			Image:       "alpine",
			ImageDigest: testDigest,
			Env:         map[string]string{"A": "1"},
		}
	}

	baseDigest, err := ContainerConfigDigest(base())
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}

	mutations := map[string]func(*ContainerSpec){
		"DifferentImageDigest": func(s *ContainerSpec) { s.ImageDigest = otherDigest },
		"DifferentEnvValue":    func(s *ContainerSpec) { s.Env["A"] = "2" },
		"ExtraEnvKey":          func(s *ContainerSpec) { s.Env["B"] = "1" },
		"DifferentEntrypoint":  func(s *ContainerSpec) { s.Entrypoint = []string{"/bin/sh"} },
		"DifferentMount":       func(s *ContainerSpec) { s.Mounts = []ContainerMount{{Source: "/a", Target: "/b"}} },
		"DifferentTrustLabel":  func(s *ContainerSpec) { s.TrustLabel = "staging" },
		"DifferentUser":        func(s *ContainerSpec) { s.User = "0:0" },
		"GPUEnabled":           func(s *ContainerSpec) { s.GPUEnabled = true },

		// The privilege dimension. Before these fields existed a container
		// flipped to privileged, or granted CAP_SYS_ADMIN, measured
		// identically to the safe one an operator approved.
		"Privileged": func(s *ContainerSpec) { s.Privileged = true },
		"CapAdd":     func(s *ContainerSpec) { s.CapAdd = []string{"CAP_SYS_ADMIN"} },
		"CapDrop":    func(s *ContainerSpec) { s.CapDrop = []string{"ALL"} },
		"MountType": func(s *ContainerSpec) {
			s.Mounts = []ContainerMount{{Source: "/a", Target: "/b", Type: MountTypeBind}}
		},
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			spec := base()
			mutate(spec)
			got, err := ContainerConfigDigest(spec)
			if err != nil {
				t.Fatalf("ContainerConfigDigest: %v", err)
			}
			if got == baseDigest {
				t.Fatalf("%s must change the configuration digest", name)
			}
		})
	}
}

// TestContainerConfigDigest_FieldsCannotAlias proves the length-prefixed
// encoding: shifting a character across a field boundary must change the
// digest, which a naive concatenation would not.
// TestContainerConfigDigest_MeasuresPrivilege states the guarantee the
// privilege fields exist for: no two of these configurations may share a
// measurement, because each grants strictly more host access than the last.
func TestContainerConfigDigest_MeasuresPrivilege(t *testing.T) {
	configurations := map[string]*ContainerSpec{
		"Safe":              {Image: "alpine", ImageDigest: testDigest},
		"Privileged":        {Image: "alpine", ImageDigest: testDigest, Privileged: true},
		"SysAdmin":          {Image: "alpine", ImageDigest: testDigest, CapAdd: []string{"CAP_SYS_ADMIN"}},
		"SysAdminAndNetRaw": {Image: "alpine", ImageDigest: testDigest, CapAdd: []string{"CAP_SYS_ADMIN", "CAP_NET_RAW"}},
		"DropAll":           {Image: "alpine", ImageDigest: testDigest, CapDrop: []string{"ALL"}},
		"PrivilegedDropAll": {Image: "alpine", ImageDigest: testDigest, Privileged: true, CapDrop: []string{"ALL"}},
		"Memlock8MiB": {Image: "alpine", ImageDigest: testDigest,
			Ulimits: []ContainerUlimit{{Name: UlimitMemlock, Soft: 8 << 20, Hard: 8 << 20}}},
		"Memlock4GiB": {Image: "alpine", ImageDigest: testDigest,
			Ulimits: []ContainerUlimit{{Name: UlimitMemlock, Soft: 4 << 30, Hard: 4 << 30}}},
		"Memlock4GiBHardOnly": {Image: "alpine", ImageDigest: testDigest,
			Ulimits: []ContainerUlimit{{Name: UlimitMemlock, Soft: 8 << 20, Hard: 4 << 30}}},
		"TmpfsMount": {
			Image:       "alpine",
			ImageDigest: testDigest,
			Mounts:      []ContainerMount{{Target: "/scratch", Type: MountTypeTmpfs}},
		},
		"BindMount": {
			Image:       "alpine",
			ImageDigest: testDigest,
			Mounts:      []ContainerMount{{Source: "/scratch", Target: "/scratch", Type: MountTypeBind}},
		},
	}

	seen := make(map[string]string, len(configurations))
	for name, spec := range configurations {
		digest, err := ContainerConfigDigest(spec)
		if err != nil {
			t.Fatalf("ContainerConfigDigest(%s): %v", name, err)
		}
		if other, collided := seen[digest]; collided {
			t.Fatalf("%s and %s measure identically (%s)", name, other, digest)
		}
		seen[digest] = name
	}
}

// TestContainerConfigDigest_UlimitOrderIsNotSignificant: ulimits are keyed by
// name, so declaring the same limits in another order must measure the same,
// exactly as the command line renders them the same.
func TestContainerConfigDigest_UlimitOrderIsNotSignificant(t *testing.T) {
	memlock := ContainerUlimit{Name: UlimitMemlock, Soft: 4 << 30, Hard: 4 << 30}
	nofile := ContainerUlimit{Name: "nofile", Soft: 1024, Hard: 4096}
	forward := &ContainerSpec{Image: "alpine", ImageDigest: testDigest, Ulimits: []ContainerUlimit{memlock, nofile}}
	reversed := &ContainerSpec{Image: "alpine", ImageDigest: testDigest, Ulimits: []ContainerUlimit{nofile, memlock}}
	want, err := ContainerConfigDigest(forward)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	got, err := ContainerConfigDigest(reversed)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	if got != want {
		t.Fatalf("ulimit order changed the measurement: %q != %q", got, want)
	}
	if reversed.Ulimits[0] != nofile {
		t.Fatal("ContainerConfigDigest reordered the caller's Ulimits")
	}
}

// TestContainerConfigDigest_CapabilityOrderIsNotSignificant proves the
// normalization: a capability grant is a set, so listing the same members in a
// different order must not change the measurement — otherwise a redeploy that
// merely reordered a YAML list would look like a different workload.
func TestContainerConfigDigest_CapabilityOrderIsNotSignificant(t *testing.T) {
	forward := &ContainerSpec{
		Image:       "alpine",
		ImageDigest: testDigest,
		CapAdd:      []string{"CAP_NET_RAW", "CAP_SYS_ADMIN", "CAP_CHOWN"},
		CapDrop:     []string{"CAP_MKNOD", "CAP_AUDIT_WRITE"},
	}
	shuffled := &ContainerSpec{
		Image:       "alpine",
		ImageDigest: testDigest,
		CapAdd:      []string{"CAP_SYS_ADMIN", "CAP_CHOWN", "CAP_NET_RAW"},
		CapDrop:     []string{"CAP_AUDIT_WRITE", "CAP_MKNOD"},
	}

	want, err := ContainerConfigDigest(forward)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	got, err := ContainerConfigDigest(shuffled)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	if got != want {
		t.Fatalf("capability order changed the measurement: %q != %q", got, want)
	}

	// Normalization must not reach into the caller's slices: a spec is
	// hashed many times and may be launched afterwards.
	if !reflect.DeepEqual(forward.CapAdd, []string{"CAP_NET_RAW", "CAP_SYS_ADMIN", "CAP_CHOWN"}) {
		t.Fatalf("ContainerConfigDigest mutated the caller's CapAdd: %v", forward.CapAdd)
	}

	// A genuinely different set must still measure differently.
	extra := &ContainerSpec{
		Image:       "alpine",
		ImageDigest: testDigest,
		CapAdd:      []string{"CAP_NET_RAW", "CAP_SYS_ADMIN", "CAP_CHOWN", "CAP_SYS_PTRACE"},
		CapDrop:     []string{"CAP_MKNOD", "CAP_AUDIT_WRITE"},
	}
	other, err := ContainerConfigDigest(extra)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	if other == want {
		t.Fatal("an added capability must change the measurement")
	}

	// CapAdd and CapDrop must not be interchangeable.
	swapped := &ContainerSpec{
		Image:       "alpine",
		ImageDigest: testDigest,
		CapAdd:      []string{"CAP_MKNOD", "CAP_AUDIT_WRITE"},
		CapDrop:     []string{"CAP_NET_RAW", "CAP_SYS_ADMIN", "CAP_CHOWN"},
	}
	swappedDigest, err := ContainerConfigDigest(swapped)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	if swappedDigest == want {
		t.Fatal("granting and dropping the same capabilities must not measure alike")
	}
}

func TestContainerConfigDigest_FieldsCannotAlias(t *testing.T) {
	a := &ContainerSpec{Image: "alpine", ImageDigest: testDigest, Env: map[string]string{"AB": "C"}}
	b := &ContainerSpec{Image: "alpine", ImageDigest: testDigest, Env: map[string]string{"A": "BC"}}

	da, err := ContainerConfigDigest(a)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	db, err := ContainerConfigDigest(b)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	if da == db {
		t.Fatal("field boundaries must be unambiguous in the canonical encoding")
	}
}

func TestContainerConfigDigest_NilSpec(t *testing.T) {
	if _, err := ContainerConfigDigest(nil); err == nil {
		t.Fatal("expected an error for a nil spec")
	}
}

func TestTrustErrorsUnwrap(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
	}{
		{
			name:   "ImageNotPinned",
			err:    &ImageNotPinnedError{Image: "alpine", TrustLevel: TrustLevelPinned},
			target: ErrImageNotPinned,
		},
		{
			name:   "ImageDigestMismatch",
			err:    &ImageDigestMismatchError{Image: "alpine", Expected: testDigest, Actual: otherDigest},
			target: ErrImageDigestMismatch,
		},
		{
			name:   "ImageDigestUnresolvedNoCause",
			err:    &ImageDigestUnresolvedError{Image: "alpine"},
			target: ErrImageDigestUnresolved,
		},
		{
			name:   "ImageDigestUnresolvedWithCause",
			err:    &ImageDigestUnresolvedError{Image: "alpine", Cause: errors.New("boom")},
			target: ErrImageDigestUnresolved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.target) {
				t.Fatalf("%v does not wrap %v", tt.err, tt.target)
			}
			if tt.err.Error() == "" {
				t.Fatal("error message must not be empty")
			}
		})
	}
}

// TestContainerConfigDigest_MeasuresResourceLimits proves a configuration
// cannot have its memory limit lifted, or its tmpfs workspace resized, and
// still measure as the configuration an operator approved.
//
// IT IS THE SAME ARGUMENT AS PRIVILEGE. An unbounded tmpfs can consume the
// host's memory; a limit removed from a measured spec is a denial-of-service
// dimension, and a digest that ignored it would certify the safe container
// while the dangerous one ran.
func TestContainerConfigDigest_MeasuresResourceLimits(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	configurations := map[string]*ContainerSpec{
		"Unbounded": {Image: "alpine", ImageDigest: testDigest},
		"Bounded": {
			Image: "alpine", ImageDigest: testDigest,
			MemoryBytes: 4 * gib,
		},
		"BoundedDifferently": {
			Image: "alpine", ImageDigest: testDigest,
			MemoryBytes: 8 * gib,
		},
		"UnsizedTmpfs": {
			Image: "alpine", ImageDigest: testDigest,
			MemoryBytes: 4 * gib,
			Mounts:      []ContainerMount{{Target: "/workspace", Type: MountTypeTmpfs}},
		},
		"SizedTmpfs": {
			Image: "alpine", ImageDigest: testDigest,
			MemoryBytes: 4 * gib,
			Mounts: []ContainerMount{
				{Target: "/workspace", Type: MountTypeTmpfs, TmpfsSizeBytes: gib},
			},
		},
		"SizedTmpfsResized": {
			Image: "alpine", ImageDigest: testDigest,
			MemoryBytes: 4 * gib,
			Mounts: []ContainerMount{
				{Target: "/workspace", Type: MountTypeTmpfs, TmpfsSizeBytes: 2 * gib},
			},
		},
		"PidsBounded": {
			Image: "alpine", ImageDigest: testDigest,
			PidsLimit: 4096,
		},
		"PidsBoundedDifferently": {
			Image: "alpine", ImageDigest: testDigest,
			PidsLimit: 8192,
		},
	}

	seen := make(map[string]string, len(configurations))
	for name, spec := range configurations {
		digest, err := ContainerConfigDigest(spec)
		if err != nil {
			t.Fatalf("ContainerConfigDigest(%s): %v", name, err)
		}
		if other, collided := seen[digest]; collided {
			t.Fatalf("%s and %s measure identically (%s)", name, other, digest)
		}
		seen[digest] = name
	}
}

// TestContainerConfigDigest_ResourceLimitsAreStableAcrossMeasurements is the
// twin: the same limits measure the same way every time, so a redeploy of an
// approved configuration is not mistaken for a new workload.
func TestContainerConfigDigest_ResourceLimitsAreStableAcrossMeasurements(t *testing.T) {
	spec := &ContainerSpec{
		Image:       "alpine",
		ImageDigest: testDigest,
		MemoryBytes: 6 * 1024 * 1024 * 1024,
		Mounts: []ContainerMount{
			{Target: "/workspace", Type: MountTypeTmpfs, TmpfsSizeBytes: 4 * 1024 * 1024 * 1024},
		},
	}
	first, err := ContainerConfigDigest(spec)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	for i := 0; i < 8; i++ {
		again, err := ContainerConfigDigest(spec)
		if err != nil {
			t.Fatalf("ContainerConfigDigest: %v", err)
		}
		if again != first {
			t.Fatalf("measurement %d = %s, want %s", i, again, first)
		}
	}
}
