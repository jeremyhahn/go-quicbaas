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
	"strings"
	"testing"
)

// =============================================================================
// Workspace storage tiers
// =============================================================================
//
// Two properties carry this whole file. The first is INERTNESS: an unconfigured
// project must launch the container it has always launched, byte for byte,
// because the tier decides whether a tenant's work survives a restart. The
// second is that an UNDER-PROVISIONED BUDGET IS REFUSED — tmpfs is memory, so a
// container whose limit is its own scratch space OOM-kills itself partway
// through an attempt, which reads as a flaky build rather than as the
// misconfiguration it is.

const testMB = 1024 * 1024

// TestProjectStorage_Validate_AcceptsEveryHonestConfiguration covers the
// configurations an operator legitimately writes, including the unset one.
func TestProjectStorage_Validate_AcceptsEveryHonestConfiguration(t *testing.T) {
	cases := []struct {
		name    string
		storage ProjectStorage
	}{
		{"Unset", ProjectStorage{}},
		{"PersistentTierAlone", ProjectStorage{WorkspaceTier: WorkspaceTierPersistent}},
		{"PersistentTierWithAMemoryLimit", ProjectStorage{
			WorkspaceTier:     WorkspaceTierPersistent,
			ContainerMemoryMB: 4096,
		}},
		{"EphemeralTierAlone", ProjectStorage{WorkspaceTier: WorkspaceTierEphemeral}},
		{"EphemeralTierFullySized", ProjectStorage{
			WorkspaceTier:     WorkspaceTierEphemeral,
			WorkspaceTmpfsMB:  4096,
			ContainerMemoryMB: 6144,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.storage.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

// TestProjectStorage_Validate_RefusesABadRecord is the bad-input twin. The
// unknown-tier case is the important one: rounding a typo down to the zero
// value would leave the project writing to the disk it was configured off.
func TestProjectStorage_Validate_RefusesABadRecord(t *testing.T) {
	cases := []struct {
		name    string
		storage ProjectStorage
		field   string
	}{
		{"UnknownTier", ProjectStorage{WorkspaceTier: "ram"}, "storage.workspace_tier"},
		{"MisspelledTier", ProjectStorage{WorkspaceTier: "ephemerial"}, "storage.workspace_tier"},
		{"NegativeTmpfsSize", ProjectStorage{
			WorkspaceTier:    WorkspaceTierEphemeral,
			WorkspaceTmpfsMB: -1,
		}, "storage.workspace_tmpfs_mb"},
		{"NegativeMemoryLimit", ProjectStorage{ContainerMemoryMB: -1}, "storage.container_memory_mb"},
		{"TmpfsSizedOnTheWrongTier", ProjectStorage{
			WorkspaceTier:    WorkspaceTierPersistent,
			WorkspaceTmpfsMB: 4096,
		}, "storage.workspace_tmpfs_mb"},
		{"TmpfsSizedWithNoTierAtAll", ProjectStorage{WorkspaceTmpfsMB: 4096}, "storage.workspace_tmpfs_mb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.storage.Validate()
			var invalid *InputValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("Validate() = %T %v, want an InputValidationError", err, err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("Field = %q, want %q", invalid.Field, tc.field)
			}
		})
	}
}

// TestProjectStorage_Plan_ConvertsMegabytesOnce proves the unit boundary is
// crossed in exactly one place: the record is megabytes because that is what an
// operator writes, and everything below the plan is bytes.
func TestProjectStorage_Plan_ConvertsMegabytesOnce(t *testing.T) {
	plan := ProjectStorage{
		WorkspaceTier:     WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 6144,
	}.Plan()

	if plan.Tier != WorkspaceTierEphemeral {
		t.Errorf("Tier = %q, want %q", plan.Tier, WorkspaceTierEphemeral)
	}
	if plan.TmpfsSizeBytes != 4096*testMB {
		t.Errorf("TmpfsSizeBytes = %d, want %d", plan.TmpfsSizeBytes, 4096*testMB)
	}
	if plan.MemoryBytes != 6144*testMB {
		t.Errorf("MemoryBytes = %d, want %d", plan.MemoryBytes, 6144*testMB)
	}
	if !plan.Ephemeral() {
		t.Error("Ephemeral() = false for the ephemeral tier")
	}
	if plan.Inert() {
		t.Error("Inert() = true for a fully configured plan")
	}
}

// TestProjectStorage_Plan_AnUnconfiguredRecordPlansNothing is the inertness
// property at the record boundary: every project in the fleet today is this
// one, and its plan must ask for nothing.
func TestProjectStorage_Plan_AnUnconfiguredRecordPlansNothing(t *testing.T) {
	plan := ProjectStorage{}.Plan()
	if !plan.Inert() {
		t.Fatalf("plan = %+v, want the inert plan", plan)
	}
	if plan.Ephemeral() {
		t.Error("Ephemeral() = true for an unconfigured project")
	}
}

// TestApplyWorkspaceStorage_AnInertPlanLeavesTheSpecUntouched is the property
// every existing spawn depends on: with no storage configuration the launched
// container is the one it has always been, down to the mounts and the digest.
func TestApplyWorkspaceStorage_AnInertPlanLeavesTheSpecUntouched(t *testing.T) {
	spec := &ContainerSpec{
		Image:  "worker:1",
		Mounts: []ContainerMount{{Source: "/srv/work/api", Target: "/workspace"}},
	}
	before, err := ContainerConfigDigest(spec)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}

	if err := ApplyWorkspaceStorage(spec, "/workspace", WorkspaceStoragePlan{}); err != nil {
		t.Fatalf("ApplyWorkspaceStorage: %v", err)
	}

	if len(spec.Mounts) != 1 || spec.Mounts[0].Source != "/srv/work/api" || spec.Mounts[0].Type != "" {
		t.Fatalf("mounts = %+v, want the untouched host bind mount", spec.Mounts)
	}
	if spec.MemoryBytes != 0 {
		t.Errorf("MemoryBytes = %d, want the runtime's own default", spec.MemoryBytes)
	}
	after, err := ContainerConfigDigest(spec)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	if before != after {
		t.Fatal("an inert plan changed the measured configuration")
	}
}

// TestApplyWorkspaceStorage_ThePersistentTierKeepsTheHostBindMount proves the
// explicit tier and the unset one agree about the checkout, and that a memory
// limit is still honoured on it: bounding a container's memory is useful
// whether or not its workspace is in RAM.
func TestApplyWorkspaceStorage_ThePersistentTierKeepsTheHostBindMount(t *testing.T) {
	spec := &ContainerSpec{
		Image:  "worker:1",
		Mounts: []ContainerMount{{Source: "/srv/work/api", Target: "/workspace"}},
	}
	plan := ProjectStorage{
		WorkspaceTier:     WorkspaceTierPersistent,
		ContainerMemoryMB: 2048,
	}.Plan()

	if err := ApplyWorkspaceStorage(spec, "/workspace", plan); err != nil {
		t.Fatalf("ApplyWorkspaceStorage: %v", err)
	}

	if len(spec.Mounts) != 1 || spec.Mounts[0].Source != "/srv/work/api" {
		t.Fatalf("mounts = %+v, want the host bind mount kept", spec.Mounts)
	}
	if spec.Mounts[0].Type == MountTypeTmpfs {
		t.Fatal("the persistent tier mounted a tmpfs")
	}
	if spec.MemoryBytes != 2048*testMB {
		t.Errorf("MemoryBytes = %d, want the configured limit", spec.MemoryBytes)
	}
}

// TestApplyWorkspaceStorage_TheEphemeralTierReplacesTheBindMount is the change
// the whole tier exists for: the checkout stops being a host directory. It must
// REPLACE, not accompany — a tmpfs beside the bind mount would leave the
// operator's disk carrying the writes.
func TestApplyWorkspaceStorage_TheEphemeralTierReplacesTheBindMount(t *testing.T) {
	spec := &ContainerSpec{
		Image:  "worker:1",
		Mounts: []ContainerMount{{Source: "/srv/work/api", Target: "/workspace"}},
	}
	plan := ProjectStorage{
		WorkspaceTier:     WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 6144,
	}.Plan()

	if err := ApplyWorkspaceStorage(spec, "/workspace", plan); err != nil {
		t.Fatalf("ApplyWorkspaceStorage: %v", err)
	}

	if len(spec.Mounts) != 1 {
		t.Fatalf("mounts = %+v, want exactly the tmpfs workspace", spec.Mounts)
	}
	got := spec.Mounts[0]
	if got.Type != MountTypeTmpfs {
		t.Errorf("Type = %q, want %q", got.Type, MountTypeTmpfs)
	}
	if got.Source != "" {
		t.Errorf("Source = %q, want no host source at all", got.Source)
	}
	if got.Target != "/workspace" {
		t.Errorf("Target = %q, want /workspace", got.Target)
	}
	if got.TmpfsSizeBytes != 4096*testMB {
		t.Errorf("TmpfsSizeBytes = %d, want the configured size", got.TmpfsSizeBytes)
	}
	if spec.MemoryBytes != 6144*testMB {
		t.Errorf("MemoryBytes = %d, want the configured limit", spec.MemoryBytes)
	}
	// The spec must still be launchable: Validate is what every runtime path
	// calls, and a tier that produced a spec Validate refuses would fail at
	// launch rather than here.
	if err := spec.Validate(); err != nil {
		t.Fatalf("the tiered spec does not validate: %v", err)
	}
}

// TestApplyWorkspaceStorage_TheEphemeralTierMountsAWorkspaceThatWasNotThere
// covers the spec that carried no workspace mount — a "." work directory, or a
// caller-supplied ContainerConfig — where there is nothing to replace.
func TestApplyWorkspaceStorage_TheEphemeralTierMountsAWorkspaceThatWasNotThere(t *testing.T) {
	spec := &ContainerSpec{
		Image:  "worker:1",
		Mounts: []ContainerMount{{Source: "/srv/cache/go", Target: "/cache", ReadOnly: true}},
	}
	plan := ProjectStorage{
		WorkspaceTier:    WorkspaceTierEphemeral,
		WorkspaceTmpfsMB: 1024,
	}.Plan()

	if err := ApplyWorkspaceStorage(spec, "/workspace", plan); err != nil {
		t.Fatalf("ApplyWorkspaceStorage: %v", err)
	}

	if len(spec.Mounts) != 2 {
		t.Fatalf("mounts = %+v, want the tmpfs workspace added", spec.Mounts)
	}
	if spec.Mounts[0].Target != "/workspace" || spec.Mounts[0].Type != MountTypeTmpfs {
		t.Errorf("mounts[0] = %+v, want the tmpfs workspace first", spec.Mounts[0])
	}
	if spec.Mounts[1].Source != "/srv/cache/go" {
		t.Errorf("mounts[1] = %+v, want the pre-existing mount kept", spec.Mounts[1])
	}
}

// TestApplyWorkspaceStorage_TheEphemeralTierKeepsTheSharedPersistentMounts is
// the shared-persistent tier's real property. Those mounts are the
// MountPolicy-authorized ExtraMounts a spawn already carries — a module cache,
// an npm cache — and an ephemeral CHECKOUT must not make them ephemeral too: a
// cold download per attempt costs more than the writes the tmpfs saves.
func TestApplyWorkspaceStorage_TheEphemeralTierKeepsTheSharedPersistentMounts(t *testing.T) {
	spec := &ContainerSpec{
		Image: "worker:1",
		Mounts: []ContainerMount{
			{Source: "/srv/work/api", Target: "/workspace"},
			{Source: "/srv/cache/gomodcache", Target: "/home/agent/go/pkg/mod"},
			{Source: "/srv/cache/npm", Target: "/home/agent/.npm"},
		},
	}
	plan := ProjectStorage{
		WorkspaceTier:     WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  2048,
		ContainerMemoryMB: 4096,
	}.Plan()

	if err := ApplyWorkspaceStorage(spec, "/workspace", plan); err != nil {
		t.Fatalf("ApplyWorkspaceStorage: %v", err)
	}

	if len(spec.Mounts) != 3 {
		t.Fatalf("mounts = %+v, want three", spec.Mounts)
	}
	if spec.Mounts[0].Type != MountTypeTmpfs {
		t.Errorf("the checkout is not on a tmpfs: %+v", spec.Mounts[0])
	}
	for _, cache := range spec.Mounts[1:] {
		if cache.Type == MountTypeTmpfs {
			t.Errorf("a shared cache became ephemeral: %+v", cache)
		}
		if cache.Source == "" {
			t.Errorf("a shared cache lost its host source: %+v", cache)
		}
	}
}

// TestApplyWorkspaceStorage_RefusesAnUnderProvisionedMemoryBudget is the
// fail-closed requirement, and the message is part of it: the refusal names the
// shortfall so an operator can act on it without reading this code.
func TestApplyWorkspaceStorage_RefusesAnUnderProvisionedMemoryBudget(t *testing.T) {
	spec := &ContainerSpec{
		Image:  "worker:1",
		Mounts: []ContainerMount{{Source: "/srv/work/api", Target: "/workspace"}},
	}
	// 4 GiB of tmpfs inside a 4 GiB container: every byte of the limit is the
	// scratch space, so the build that runs there has nowhere to live.
	plan := ProjectStorage{
		WorkspaceTier:     WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 4096,
	}.Plan()

	err := ApplyWorkspaceStorage(spec, "/workspace", plan)
	var budget *MemoryBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("ApplyWorkspaceStorage() = %T %v, want a MemoryBudgetError", err, err)
	}
	if want := WorkspaceBuildHeadroomBytes; budget.ShortfallBytes() != want {
		t.Errorf("ShortfallBytes() = %d, want %d", budget.ShortfallBytes(), want)
	}
	message := budget.Error()
	for _, want := range []string{"short by", "tmpfs", "build headroom"} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal %q does not mention %q", message, want)
		}
	}
	if !strings.Contains(message, "4294967296") {
		t.Errorf("the refusal %q does not name the tmpfs total", message)
	}
}

// TestApplyWorkspaceStorage_ATmpfsWorkspacePlusAFullBuildFitsTheLimit is the
// twin: the sizing an operator SHOULD write is accepted, and the container has
// room for the build the tmpfs exists to hold.
func TestApplyWorkspaceStorage_ATmpfsWorkspacePlusAFullBuildFitsTheLimit(t *testing.T) {
	spec := &ContainerSpec{Image: "worker:1"}
	// 4 GiB of checkout plus the build headroom, inside 6 GiB.
	plan := ProjectStorage{
		WorkspaceTier:     WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 6144,
	}.Plan()

	if err := ApplyWorkspaceStorage(spec, "/workspace", plan); err != nil {
		t.Fatalf("ApplyWorkspaceStorage: %v", err)
	}
	if err := ValidateMemoryBudget(spec); err != nil {
		t.Fatalf("ValidateMemoryBudget: %v", err)
	}
	free := spec.MemoryBytes - spec.Mounts[0].TmpfsSizeBytes
	if free < WorkspaceBuildHeadroomBytes {
		t.Fatalf("the accepted spec leaves %d bytes for a build, want at least %d",
			free, WorkspaceBuildHeadroomBytes)
	}
}

// TestApplyWorkspaceStorage_RefusesBadInput covers every way a plan can be
// wrong by the time it reaches the applier, including the one the record
// validator cannot catch: an ephemeral tier with no container path to mount.
func TestApplyWorkspaceStorage_RefusesBadInput(t *testing.T) {
	t.Run("NilSpec", func(t *testing.T) {
		err := ApplyWorkspaceStorage(nil, "/workspace", WorkspaceStoragePlan{Tier: WorkspaceTierEphemeral})
		var invalid *InputValidationError
		if !errors.As(err, &invalid) || invalid.Field != "spec" {
			t.Fatalf("err = %T %v, want an InputValidationError on spec", err, err)
		}
	})
	t.Run("UnknownTier", func(t *testing.T) {
		err := ApplyWorkspaceStorage(&ContainerSpec{Image: "w:1"}, "/workspace",
			WorkspaceStoragePlan{Tier: "ram"})
		var invalid *InputValidationError
		if !errors.As(err, &invalid) || invalid.Field != "storage.workspace_tier" {
			t.Fatalf("err = %T %v, want an InputValidationError on the tier", err, err)
		}
	})
	t.Run("NegativeTmpfsSize", func(t *testing.T) {
		err := ApplyWorkspaceStorage(&ContainerSpec{Image: "w:1"}, "/workspace",
			WorkspaceStoragePlan{Tier: WorkspaceTierEphemeral, TmpfsSizeBytes: -1})
		var invalid *InputValidationError
		if !errors.As(err, &invalid) || invalid.Field != "storage.tmpfs_size_bytes" {
			t.Fatalf("err = %T %v, want an InputValidationError on the tmpfs size", err, err)
		}
	})
	t.Run("NegativeMemoryLimit", func(t *testing.T) {
		err := ApplyWorkspaceStorage(&ContainerSpec{Image: "w:1"}, "/workspace",
			WorkspaceStoragePlan{Tier: WorkspaceTierEphemeral, MemoryBytes: -1})
		var invalid *InputValidationError
		if !errors.As(err, &invalid) || invalid.Field != "storage.memory_bytes" {
			t.Fatalf("err = %T %v, want an InputValidationError on the memory limit", err, err)
		}
	})
	t.Run("EphemeralTierWithNoContainerPath", func(t *testing.T) {
		err := ApplyWorkspaceStorage(&ContainerSpec{Image: "w:1"}, "",
			WorkspaceStoragePlan{Tier: WorkspaceTierEphemeral, TmpfsSizeBytes: 1024})
		var invalid *InputValidationError
		if !errors.As(err, &invalid) || invalid.Field != "target" {
			t.Fatalf("err = %T %v, want an InputValidationError on the target", err, err)
		}
	})
}

// TestValidateMemoryBudget_TheUncheckedSpecs documents the two shapes that are
// deliberately not refused, both of which are correct configurations today.
func TestValidateMemoryBudget_TheUncheckedSpecs(t *testing.T) {
	t.Run("NoMemoryLimitHasNoBudgetToExceed", func(t *testing.T) {
		spec := &ContainerSpec{
			Image:  "worker:1",
			Mounts: []ContainerMount{{Target: "/workspace", Type: MountTypeTmpfs, TmpfsSizeBytes: 64 << 30}},
		}
		if err := ValidateMemoryBudget(spec); err != nil {
			t.Fatalf("ValidateMemoryBudget: %v", err)
		}
	})
	t.Run("ALimitedContainerWithNoTmpfsIsNotUnderProvisioned", func(t *testing.T) {
		spec := &ContainerSpec{Image: "sidecar:1", MemoryBytes: 64 * testMB}
		if err := ValidateMemoryBudget(spec); err != nil {
			t.Fatalf("ValidateMemoryBudget: %v", err)
		}
	})
	t.Run("AnUnsizedTmpfsIsBoundedByTheCgroupItself", func(t *testing.T) {
		spec := &ContainerSpec{
			Image:       "worker:1",
			MemoryBytes: 64 * testMB,
			Mounts:      []ContainerMount{{Target: "/workspace", Type: MountTypeTmpfs}},
		}
		if err := ValidateMemoryBudget(spec); err != nil {
			t.Fatalf("ValidateMemoryBudget: %v", err)
		}
	})
	t.Run("NilSpecIsRefused", func(t *testing.T) {
		err := ValidateMemoryBudget(nil)
		var invalid *InputValidationError
		if !errors.As(err, &invalid) || invalid.Field != "spec" {
			t.Fatalf("err = %T %v, want an InputValidationError on spec", err, err)
		}
	})
}

// TestValidateMemoryBudget_EveryTmpfsCounts proves the budget is a property of
// the whole container rather than of the workspace: a second tmpfs mount is
// memory too, and a check that measured only the checkout would approve a
// container whose combined scratch space cannot fit.
func TestValidateMemoryBudget_EveryTmpfsCounts(t *testing.T) {
	spec := &ContainerSpec{
		Image:       "worker:1",
		MemoryBytes: 2560 * testMB,
		Mounts: []ContainerMount{
			{Target: "/workspace", Type: MountTypeTmpfs, TmpfsSizeBytes: 1024 * testMB},
			{Target: "/tmp", Type: MountTypeTmpfs, TmpfsSizeBytes: 1024 * testMB},
		},
	}
	err := ValidateMemoryBudget(spec)
	var budget *MemoryBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("ValidateMemoryBudget() = %T %v, want a MemoryBudgetError", err, err)
	}
	if budget.TmpfsBytes != 2048*testMB {
		t.Errorf("TmpfsBytes = %d, want both tmpfs mounts summed", budget.TmpfsBytes)
	}
	if budget.ShortfallBytes() != 512*testMB {
		t.Errorf("ShortfallBytes() = %d, want %d", budget.ShortfallBytes(), 512*testMB)
	}
	// Either mount alone fits; only their SUM does not. That is the whole
	// point of measuring the container rather than the checkout.
	spec.Mounts = spec.Mounts[:1]
	if err := ValidateMemoryBudget(spec); err != nil {
		t.Fatalf("one tmpfs mount alone was refused: %v", err)
	}
}

// TestContainerSpecValidate_EnforcesTheMemoryBudget is the fail-closed wiring
// itself: the check lives on the path every launch already takes, so no caller
// can build a spec that skips it. ExecContainerRuntime.Run and RunInteractive
// both call Validate.
func TestContainerSpecValidate_EnforcesTheMemoryBudget(t *testing.T) {
	spec := &ContainerSpec{
		Image:       "worker:1",
		MemoryBytes: 1024 * testMB,
		Mounts: []ContainerMount{
			{Target: "/workspace", Type: MountTypeTmpfs, TmpfsSizeBytes: 1024 * testMB},
		},
	}
	var budget *MemoryBudgetError
	if err := spec.Validate(); !errors.As(err, &budget) {
		t.Fatalf("Validate() = %T %v, want a MemoryBudgetError", err, err)
	}

	spec.MemoryBytes = 1024*testMB + WorkspaceBuildHeadroomBytes
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate() refused a spec inside its budget: %v", err)
	}
}
