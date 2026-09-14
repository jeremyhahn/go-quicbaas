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

import "strconv"

// =============================================================================
// Workspace storage tiers — where a sandboxed agent's checkout actually lives
// =============================================================================
//
// A sandboxed agent writes continuously: a clone, a build cache, test
// binaries, and every edit it makes. Bind-mounted from the host, all of it
// lands on the operator's SSD, per attempt, forever. The EPHEMERAL tier moves
// the checkout into a sized tmpfs inside the container instead, so the writes
// never reach a disk at all.
//
// THE THREE TIERS, AND WHICH ONE THIS FILE OWNS:
//
//   - EPHEMERAL (this file): the checkout, on a container-local tmpfs sized by
//     the project record and bounded by the container's own memory cgroup.
//     Nothing survives the container.
//
//   - SHARED PERSISTENT: caches worth keeping across attempts — a module
//     cache, an npm cache, a layer cache. THIS IS ALREADY BUILT AND IS NOT
//     REBUILT HERE: it is SpawnSessionRequest.ExtraMounts, authorized against
//     the operator's MountPolicy before any side effect (mount_policy.go). A
//     second mount path for the same job would be a second policy surface to
//     get wrong, and the tmpfs checkout deliberately leaves those mounts
//     untouched — an ephemeral workspace with an ephemeral module cache would
//     invert the goal, because a cold download per attempt costs more than the
//     writes the tmpfs saves.
//
//   - PER-SESSION PERSISTENT: what must outlive the container even though the
//     checkout does not. The transcript is already the daemon's. The WORK is
//     the branch, and it persists BY BEING PUSHED, not by living in a
//     surviving working tree — see JobRun.PushTurn, which publishes after
//     every turn that touched the tree.
//
// EVERY FIELD HERE IS INERT AT ITS ZERO VALUE. An unset tier keeps exactly the
// host bind mount every project has always had, an unset size leaves the
// runtime's own default, and a spec built before this dimension existed
// produces a byte-identical command line and a byte-identical
// ContainerConfigDigest. That is deliberate: the tier changes where a tenant's
// work lives, so it is opted into per project and never arrived at by default.

// WorkspaceTier names the storage tier a project's sandboxed checkout mount
// uses.
type WorkspaceTier string

const (
	// WorkspaceTierUnset is the zero value, and it means what the project
	// meant before the tier existed: the checkout is a host bind mount.
	// Resolving the zero value to the ephemeral tier instead would silently
	// move every existing project's work into RAM and lose anything a turn
	// had not pushed, so "unset" and "persistent" deliberately behave
	// identically rather than the default being the new behaviour.
	WorkspaceTierUnset WorkspaceTier = ""

	// WorkspaceTierPersistent states the historical behaviour explicitly:
	// the checkout is bind-mounted from the host workspace root, on
	// whatever filesystem that root lives on, and survives the container.
	WorkspaceTierPersistent WorkspaceTier = "persistent"

	// WorkspaceTierEphemeral puts the checkout on a container-local tmpfs.
	// The clone, the build and every edit live in the container's memory
	// cgroup and vanish with the container, so no agent write reaches the
	// host's disk.
	//
	// A restart therefore loses committed-but-unpushed work, which is why
	// this tier makes the post-turn push mandatory rather than tidy: see
	// JobRun.PushTurn.
	WorkspaceTierEphemeral WorkspaceTier = "ephemeral"
)

// workspaceTiers is the set of tiers a project record may name. An
// unrecognized tier is refused rather than treated as the zero value: the
// zero value is the WEAKEST reading of an operator's intent, so a typo in
// "ephemeral" would quietly leave the project writing to the disk it was
// configured off.
var workspaceTiers = map[WorkspaceTier]struct{}{
	WorkspaceTierUnset:      {},
	WorkspaceTierPersistent: {},
	WorkspaceTierEphemeral:  {},
}

// bytesPerMB converts the project record's megabyte-denominated sizes to the
// bytes the container spec and the runtime speak.
const bytesPerMB int64 = 1024 * 1024

// WorkspaceBuildHeadroomBytes is the memory a sandboxed worker needs OUTSIDE
// its tmpfs workspace: the agent process, a compiler, a linker, and the test
// binaries it runs. It is the term that makes the memory budget a real check
// rather than an identity — a container whose whole limit is its tmpfs has
// nowhere to run the build the tmpfs exists to hold.
//
// IT IS A CONSTANT, NOT A KNOB, on purpose. An operator who could dial it to
// zero to make a refusal go away would have configured exactly the
// under-provisioned container the check exists to reject, and the refusal is
// the cheap failure: the expensive one is an OOM kill partway through an
// attempt, which reads as a flaky build rather than as a misconfiguration.
//
// 1 GiB is measured against this repository's own worker image, where
// `go build ./...` followed by `go test -race` on a single package is the
// heaviest thing an attempt routinely does.
const WorkspaceBuildHeadroomBytes int64 = 1024 * bytesPerMB

// ProjectStorage is a project's storage-tier configuration: which tier its
// sandboxed checkout is mounted on, and the two sizes an ephemeral tier needs.
//
// The sizes are megabytes because that is the unit an operator reasons in;
// they are converted once, by Plan.
type ProjectStorage struct {
	// WorkspaceTier selects the tier. Empty is WorkspaceTierUnset, which
	// keeps the host bind mount.
	WorkspaceTier WorkspaceTier `json:"workspace_tier,omitempty" yaml:"workspace_tier,omitempty"`

	// WorkspaceTmpfsMB sizes the ephemeral tier's tmpfs checkout. Zero
	// leaves it unsized, which lets it grow into the whole memory cgroup —
	// the transitional default, not the recommended one.
	WorkspaceTmpfsMB int `json:"workspace_tmpfs_mb,omitempty" yaml:"workspace_tmpfs_mb,omitempty"`

	// ContainerMemoryMB is the container's memory cgroup limit. Zero leaves
	// the runtime's own default, which is usually unbounded. Setting it is
	// what turns the tmpfs from "RAM the host might run out of" into "RAM
	// this one container is allowed" — and what arms the budget check that
	// refuses a limit too small for the tmpfs plus the build.
	ContainerMemoryMB int `json:"container_memory_mb,omitempty" yaml:"container_memory_mb,omitempty"`
}

// Validate refuses a storage configuration that cannot be honoured.
//
// It is deliberately NOT where the memory budget is checked. The budget is a
// property of the assembled container — every tmpfs mount on it, not only the
// workspace — so it belongs to ContainerSpec.Validate, where no spawn path can
// route around it. This checks only what the record itself can be wrong about.
func (s ProjectStorage) Validate() error {
	if _, ok := workspaceTiers[s.WorkspaceTier]; !ok {
		return &InputValidationError{
			Field: "storage.workspace_tier",
			Message: "must be one of: " + string(WorkspaceTierPersistent) + ", " +
				string(WorkspaceTierEphemeral) + " (empty keeps the host bind mount)",
		}
	}
	if s.WorkspaceTmpfsMB < 0 {
		return &InputValidationError{Field: "storage.workspace_tmpfs_mb", Message: "cannot be negative"}
	}
	if s.ContainerMemoryMB < 0 {
		return &InputValidationError{Field: "storage.container_memory_mb", Message: "cannot be negative"}
	}
	// A sized tmpfs on a tier that mounts no tmpfs is an operator who
	// configured the ephemeral tier and forgot to select it. Accepting it
	// would leave the project on disk while its record claims otherwise.
	if s.WorkspaceTier != WorkspaceTierEphemeral && s.WorkspaceTmpfsMB > 0 {
		return &InputValidationError{
			Field:   "storage.workspace_tmpfs_mb",
			Message: "may only be set on the " + string(WorkspaceTierEphemeral) + " workspace tier",
		}
	}
	return nil
}

// Plan resolves the record into the decision one spawn acts on.
func (s ProjectStorage) Plan() WorkspaceStoragePlan {
	return WorkspaceStoragePlan{
		Tier:           s.WorkspaceTier,
		TmpfsSizeBytes: int64(s.WorkspaceTmpfsMB) * bytesPerMB,
		MemoryBytes:    int64(s.ContainerMemoryMB) * bytesPerMB,
	}
}

// WorkspaceStoragePlan is the resolved storage decision for one sandboxed
// spawn: the tier, and the two sizes in the bytes a container spec speaks.
//
// IT IS RESOLVED ONCE, at the orchestrator's pre-side-effect chokepoint, and
// carried to whichever path builds the container — the orchestrator's own
// detached spawn or a backend that launches its own sandbox. Both apply it
// through ApplyWorkspaceStorage, so a project's tier cannot reach one path and
// miss the other.
type WorkspaceStoragePlan struct {
	// Tier is the resolved tier; the zero value keeps the host bind mount.
	Tier WorkspaceTier `json:"tier,omitempty"`

	// TmpfsSizeBytes sizes the ephemeral tier's tmpfs. Zero is unsized.
	TmpfsSizeBytes int64 `json:"tmpfs_size_bytes,omitempty"`

	// MemoryBytes is the container's memory cgroup limit. Zero leaves the
	// runtime's default.
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
}

// Ephemeral reports whether the plan puts the checkout on a tmpfs — the one
// question every caller of this plan actually asks.
func (p WorkspaceStoragePlan) Ephemeral() bool {
	return p.Tier == WorkspaceTierEphemeral
}

// Inert reports whether the plan asks for nothing, which is the state every
// project is in until an operator configures one. An inert plan leaves the
// container spec byte-identical.
func (p WorkspaceStoragePlan) Inert() bool {
	return p == WorkspaceStoragePlan{}
}

// ApplyWorkspaceStorage applies a resolved plan to the container spec a
// sandboxed session will launch in, mounting the checkout on the tier the
// project chose and bounding the container's memory.
//
// IT IS THE SINGLE APPLIER, AND THAT IS THE WHOLE POINT. There are two paths
// that build a sandbox container — agentServiceImpl.spawnSandboxedSession for
// a backend that does not containerize itself, and ACPBackend.sandboxSpec for
// one that does, which is the path every software-factory worker actually
// takes. A tier wired into one of them would be a tier that silently does not
// apply to half the fleet.
//
// target is the container-side path of the checkout: the mount it replaces,
// and the mount it creates when the spec has none. EVERY OTHER MOUNT IS LEFT
// EXACTLY AS IT WAS, which is what keeps the shared-persistent tier — the
// MountPolicy-authorized ExtraMounts a spawn already carries — persistent on
// an ephemeral workspace.
//
// It finishes by validating the assembled budget, so an under-provisioned
// container is refused here, before it is launched and long before it is
// OOM-killed.
func ApplyWorkspaceStorage(spec *ContainerSpec, target string, plan WorkspaceStoragePlan) error {
	if spec == nil {
		return &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if plan.Inert() {
		return nil
	}
	if _, ok := workspaceTiers[plan.Tier]; !ok {
		return &InputValidationError{
			Field:   "storage.workspace_tier",
			Message: "unknown tier " + strconv.Quote(string(plan.Tier)),
		}
	}
	if plan.TmpfsSizeBytes < 0 {
		return &InputValidationError{Field: "storage.tmpfs_size_bytes", Message: "cannot be negative"}
	}
	if plan.MemoryBytes < 0 {
		return &InputValidationError{Field: "storage.memory_bytes", Message: "cannot be negative"}
	}

	if plan.MemoryBytes > 0 {
		spec.MemoryBytes = plan.MemoryBytes
	}

	if plan.Ephemeral() {
		if target == "" {
			return &InputValidationError{
				Field:   "target",
				Message: "the container-side checkout path is required for the " + string(WorkspaceTierEphemeral) + " tier",
			}
		}
		mountWorkspaceTmpfs(spec, target, plan.TmpfsSizeBytes)
	}

	return ValidateMemoryBudget(spec)
}

// mountWorkspaceTmpfs replaces the spec's mount at target with a sized tmpfs,
// or prepends one when the spec mounts nothing there.
//
// REPLACING RATHER THAN ADDING is what actually removes the host bind mount:
// a tmpfs added beside it would be shadowed by whichever the runtime applied
// last, and the checkout would still be on the operator's disk. Prepending,
// when there was nothing to replace, keeps the checkout the spec's first
// mount, which is the order every reader and every digest already sees.
func mountWorkspaceTmpfs(spec *ContainerSpec, target string, sizeBytes int64) {
	workspace := ContainerMount{
		Target:         target,
		Type:           MountTypeTmpfs,
		TmpfsSizeBytes: sizeBytes,
	}
	for i := range spec.Mounts {
		if spec.Mounts[i].Target == target {
			spec.Mounts[i] = workspace
			return
		}
	}
	spec.Mounts = append([]ContainerMount{workspace}, spec.Mounts...)
}

// MemoryBudgetError reports a container whose memory limit cannot hold its own
// tmpfs mounts plus the build that runs in them.
//
// A tmpfs is not disk with a different name: every byte written to it is a
// byte of the container's memory cgroup, shared with the compiler, the linker
// and the test binaries running beside it. A container whose limit equals its
// tmpfs size has no memory to work in, and it does not fail at launch — it
// fails partway through an attempt, as an OOM kill that reads like a flaky
// build. Refusing it at validation is the difference between a configuration
// error an operator can see and a recurring mystery.
type MemoryBudgetError struct {
	// TmpfsBytes is the sum of TmpfsSizeBytes across the spec's tmpfs
	// mounts.
	TmpfsBytes int64

	// HeadroomBytes is the non-tmpfs footprint the check reserves; see
	// WorkspaceBuildHeadroomBytes.
	HeadroomBytes int64

	// MemoryBytes is the limit the spec set.
	MemoryBytes int64
}

// ShortfallBytes is how much more memory the container needs — the number an
// operator raises the limit by, or lowers the tmpfs by.
func (e *MemoryBudgetError) ShortfallBytes() int64 {
	return e.TmpfsBytes + e.HeadroomBytes - e.MemoryBytes
}

// Error names the shortfall and every term that produced it, so the message
// alone is enough to fix the configuration.
func (e *MemoryBudgetError) Error() string {
	return "container memory limit of " + strconv.FormatInt(e.MemoryBytes, 10) +
		" bytes is short by " + strconv.FormatInt(e.ShortfallBytes(), 10) +
		" bytes: " + strconv.FormatInt(e.TmpfsBytes, 10) +
		" bytes of tmpfs mounts plus " + strconv.FormatInt(e.HeadroomBytes, 10) +
		" bytes of build headroom needs " +
		strconv.FormatInt(e.TmpfsBytes+e.HeadroomBytes, 10) + " bytes"
}

// ValidateMemoryBudget checks that a spec's memory limit leaves room to work
// in after its tmpfs mounts are accounted for. ContainerSpec.Validate calls
// it, so every launch path is covered and none can opt out.
//
// TWO SPECS ARE NOT CHECKED, and both are the same judgement:
//
//   - A spec with no memory limit (MemoryBytes <= 0) has no budget to exceed.
//     It is the transitional default — unbounded, as it has always been —
//     and refusing it would reject every container in the fleet today.
//   - A spec with a limit but no sized tmpfs has nothing sharing its memory
//     with a build, so the headroom term does not apply: a 64 MiB helper
//     container is correctly configured, not under-provisioned.
//
// An UNSIZED tmpfs on a limited container is likewise not a shortfall to
// compute — it is bounded by the cgroup itself — so it contributes nothing
// here and is left to the limit to contain.
func ValidateMemoryBudget(spec *ContainerSpec) error {
	if spec == nil {
		return &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if spec.MemoryBytes <= 0 {
		return nil
	}
	var tmpfsTotal int64
	for i := range spec.Mounts {
		if spec.Mounts[i].Type == MountTypeTmpfs {
			tmpfsTotal += spec.Mounts[i].TmpfsSizeBytes
		}
	}
	if tmpfsTotal == 0 {
		return nil
	}
	if tmpfsTotal+WorkspaceBuildHeadroomBytes <= spec.MemoryBytes {
		return nil
	}
	return &MemoryBudgetError{
		TmpfsBytes:    tmpfsTotal,
		HeadroomBytes: WorkspaceBuildHeadroomBytes,
		MemoryBytes:   spec.MemoryBytes,
	}
}
