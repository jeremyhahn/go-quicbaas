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
	"slices"
	"testing"
)

func TestParseContainerUlimit_AcceptsTheRuntimesSyntax(t *testing.T) {
	cases := map[string]ContainerUlimit{
		"memlock=4294967296": {Name: UlimitMemlock, Soft: 4 << 30, Hard: 4 << 30},
		"memlock=1024:2048":  {Name: UlimitMemlock, Soft: 1024, Hard: 2048},
		" NOFILE=1024:4096 ": {Name: "nofile", Soft: 1024, Hard: 4096},
		"core=0":             {Name: "core", Soft: 0, Hard: 0},
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			got, err := ParseContainerUlimit(raw)
			if err != nil {
				t.Fatalf("ParseContainerUlimit(%q): %v", raw, err)
			}
			if got != want {
				t.Fatalf("ParseContainerUlimit(%q) = %+v, want %+v", raw, got, want)
			}
		})
	}
}

func TestParseContainerUlimit_RefusesWhatTheRuntimeWould(t *testing.T) {
	for _, raw := range []string{
		"",
		"memlock",
		"memlock=",
		"=1024",
		"memlock=big",
		"memlock=1024:big",
		"memlock=-1",
		"memlock=2048:1024",
		"memlok=1024",
		"memlock=1:2:3",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseContainerUlimit(raw); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ParseContainerUlimit(%q) = %v, want ErrInvalidInput", raw, err)
			}
		})
	}
}

func TestParseContainerUlimits(t *testing.T) {
	got, err := ParseContainerUlimits([]string{"nofile=1024:4096", "memlock=1073741824"})
	if err != nil {
		t.Fatalf("ParseContainerUlimits: %v", err)
	}
	want := []ContainerUlimit{
		{Name: "nofile", Soft: 1024, Hard: 4096},
		{Name: UlimitMemlock, Soft: 1 << 30, Hard: 1 << 30},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ParseContainerUlimits = %+v, want %+v", got, want)
	}

	if got, err := ParseContainerUlimits(nil); err != nil || got != nil {
		t.Fatalf("ParseContainerUlimits(nil) = %+v, %v; want nil, nil", got, err)
	}
}

func TestParseContainerUlimits_RefusesABadOrRepeatedEntry(t *testing.T) {
	cases := map[string][]string{
		"Malformed":   {"nofile=1024", "memlock"},
		"NamedTwice":  {"memlock=1024", "MEMLOCK=2048"},
		"UnknownName": {"openfiles=1024"},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseContainerUlimits(raw); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ParseContainerUlimits(%q) = %v, want ErrInvalidInput", raw, err)
			}
		})
	}
}

func TestContainerSpec_ValidateRefusesABadUlimit(t *testing.T) {
	cases := map[string][]ContainerUlimit{
		"Unknown":       {{Name: "memlok", Soft: 1, Hard: 1}},
		"Negative":      {{Name: UlimitMemlock, Soft: -1, Hard: 1}},
		"SoftAboveHard": {{Name: UlimitMemlock, Soft: 2, Hard: 1}},
		"NamedTwice":    {{Name: "nofile", Soft: 1, Hard: 1}, {Name: "nofile", Soft: 2, Hard: 2}},
	}
	for name, ulimits := range cases {
		t.Run(name, func(t *testing.T) {
			spec := &ContainerSpec{Image: "worker:1", Ulimits: ulimits}
			if err := spec.Validate(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Validate() = %v, want ErrInvalidInput", err)
			}
		})
	}
	valid := &ContainerSpec{Image: "worker:1", Ulimits: []ContainerUlimit{{Name: UlimitMemlock, Soft: 1, Hard: 2}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a valid ulimit was refused: %v", err)
	}
}

// TestRunArgs_UlimitsAreEmittedSortedAndOnlyWhenSet: a spec that never set a
// ulimit launches the command line it always did, and one that did launches
// them in the order the digest measures them.
func TestRunArgs_UlimitsAreEmittedSortedAndOnlyWhenSet(t *testing.T) {
	unset := detachedRunArgs(t, &ContainerSpec{Image: "alpine"})
	if slices.Contains(unset, "--ulimit") {
		t.Fatalf("a spec with no ulimits launched with --ulimit: %q", unset)
	}
	spec := &ContainerSpec{Image: "alpine", Ulimits: []ContainerUlimit{
		{Name: "nofile", Soft: 1024, Hard: 4096},
		{Name: UlimitMemlock, Soft: 1 << 30, Hard: 1 << 30},
	}}
	want := []string{"--ulimit", "memlock=1073741824:1073741824", "--ulimit", "nofile=1024:4096"}
	if got := spec.PrivilegeRunArgs(); !slices.Equal(got, want) {
		t.Fatalf("PrivilegeRunArgs() = %q, want %q", got, want)
	}
	if spec.Ulimits[0].Name != "nofile" {
		t.Fatal("rendering sorted the caller's slice in place")
	}
	requireSecureRunArgs(t, detachedRunArgs(t, spec), want)
}
