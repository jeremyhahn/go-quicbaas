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
	"context"
	"io"
	"strconv"
	"strings"
)

// ContainerRuntime abstracts OCI container operations.
//
// The default implementation (ExecContainerRuntime) shells out to docker or
// podman via exec.Command. It implements Tier 0 of the standalone
// trusted-agent model — launching and verifying an image by immutable content
// digest — and nothing above it: Tier 0 needs no hardware, so every host can
// satisfy it.
//
// Tiers 1 and 2 (host TPM quote against expected PCRs, and measured launch)
// are NOT on this interface. They live on AttestingContainerRuntime, which
// embeds this one and adds Attest; see container_attestation.go. A caller
// that needs attestation type-asserts for AttestingContainerRuntime rather
// than requiring every runtime to carry a TPM dependency it cannot use.
//
// Tier 3 — fleet-scale device registry, scheduled re-attestation, quarantine,
// golden-PCR policy management, network isolation — is deliberately absent.
// That is go-trusted-platform's domain; it fills the AttestingContainerRuntime
// seam and the Security().Attestation() orchestration seam rather than
// extending this interface.
type ContainerRuntime interface {
	// Run creates and starts a detached container. Returns the container ID.
	Run(ctx context.Context, spec *ContainerSpec) (string, error)

	// RunInteractive starts a container with stdin/stdout pipes for interactive
	// stdio communication (e.g., ACP JSON-RPC). The container runs in
	// foreground mode (-i) and the caller gets pipes to communicate with the
	// container's entrypoint process. Returns an InteractiveProcess that owns
	// the pipes and container lifecycle.
	RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error)

	// Exec runs one command inside a container that is already running and
	// returns what it did. A non-zero exit status is reported on the result,
	// not raised as an error; an error means the command could not be run.
	// See container_exec.go — it is the engine's own publish path, and no
	// transport exposes it.
	Exec(ctx context.Context, containerID string, spec *ExecSpec) (*ExecResult, error)

	// Stop stops a running container.
	Stop(ctx context.Context, containerID string) error

	// Remove removes a container (force removes if running).
	Remove(ctx context.Context, containerID string) error

	// Logs returns the container's stdout/stderr logs.
	Logs(ctx context.Context, containerID string) (io.ReadCloser, error)

	// IsRunning checks if a container is running.
	IsRunning(ctx context.Context, containerID string) (bool, error)

	// IsAvailable checks if the container runtime (docker/podman) is installed.
	IsAvailable() bool

	// Pull fetches a container image from the remote registry. Used by agent
	// daemons at startup to pre-load the image map so first-spawn latency is
	// not gated on a network round-trip. Returns ContainerImagePullError on
	// failure; nil on success or when the image is already cached locally.
	Pull(ctx context.Context, image string) error
}

// InteractiveProcess represents a running interactive container with stdio pipes.
// The caller uses Stdin and Stdout to communicate with the container's entrypoint
// process over newline-delimited JSON-RPC.
type InteractiveProcess struct {
	// ContainerID is the container name used to identify the container for
	// stop/remove operations. For interactive containers, this is always the
	// generated name passed via --name.
	ContainerID string

	// Stdin is a writer connected to the container's stdin. Close it to
	// signal EOF to the container process.
	Stdin io.WriteCloser

	// Stdout is a reader connected to the container's stdout.
	Stdout io.ReadCloser

	// Cleanup releases the private files the launch needed — currently the
	// 0600 --env-file that carries the container's environment (see
	// container_env_file.go). The runtime process reads that file at its own
	// pace, so the runtime cannot delete it itself; the OWNER of the returned
	// process is responsible for calling Cleanup exactly once when the
	// container is torn down. Nil when the spec carried no environment.
	Cleanup func()
}

// ContainerSpec defines a container to run. No external library dependencies.
//
// The trust fields at the bottom are additive: every one of them is inert at
// its zero value, so a spec built before the trust dimension existed launches
// byte-for-byte the same command line it always did.
type ContainerSpec struct {
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	Env        map[string]string `json:"env,omitempty"`
	Mounts     []ContainerMount  `json:"mounts,omitempty"`
	Ports      []string          `json:"ports,omitempty"` // "host:container"
	WorkDir    string            `json:"work_dir,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty"` // override container entrypoint
	Command    []string          `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	AutoRemove bool              `json:"auto_remove,omitempty"` // --rm flag
	Network    string            `json:"network,omitempty"`     // host, bridge, or custom
	GPUEnabled bool              `json:"gpu_enabled,omitempty"` // --gpus all
	User       string            `json:"user,omitempty"`        // --user uid:gid

	// MemoryBytes is the container's memory cgroup limit in bytes (--memory).
	// Zero leaves the runtime's own default, which is usually unbounded. A
	// tmpfs mount (ContainerMount.TmpfsSizeBytes) is backed by this same
	// memory cgroup, so a spec that sizes a tmpfs workspace without also
	// bounding this field can be OOM-killed by its own scratch space —
	// see ValidateTmpfsBudget, which a caller invokes explicitly once it
	// knows its own build/test memory headroom.
	MemoryBytes int64 `json:"memory_bytes,omitempty"`

	// --- Privilege dimension (measured; see ContainerConfigDigest) ---
	//
	// These three fields decide how much of the host a container can reach.
	// They are part of the launch configuration, not of the trust policy, so
	// they are folded into ContainerConfigDigest: a container flipped to
	// privileged, or granted CAP_SYS_ADMIN, MUST NOT measure the same as the
	// safe one an operator approved. Each is inert at its zero value, so a
	// spec that never sets them produces the command line it always did.

	// Privileged runs the container with the runtime's isolation removed
	// (--privileged): the full capability set, unmasked /proc and /sys, and
	// no seccomp or AppArmor confinement. It is close to host-equivalent
	// access and is therefore the single most important bit of the launch
	// configuration to measure.
	Privileged bool `json:"privileged,omitempty"`

	// CapAdd lists Linux capabilities granted on top of the runtime's
	// default set (--cap-add), in either the "CAP_SYS_ADMIN" or the bare
	// "SYS_ADMIN" spelling the runtime accepts. Ordering is not significant:
	// the digest and the emitted command line both sort the list, so two
	// specs naming the same capabilities measure and launch identically.
	CapAdd []string `json:"cap_add,omitempty"`

	// CapDrop lists Linux capabilities removed from the default set
	// (--cap-drop); "ALL" drops every one. Ordering is normalized exactly as
	// for CapAdd.
	CapDrop []string `json:"cap_drop,omitempty"`

	// --- Trust dimension (see container_trust.go and trust_policy.go) ---

	// ImageDigest pins the container to an immutable OCI content digest in
	// canonical "sha256:<hex>" form. When set, the runtime launches
	// "repository@sha256:<hex>" instead of the mutable tag in Image, and
	// verifies after launch that the container resolves to that digest.
	// It may also be supplied inline as "repository@sha256:<hex>" in Image;
	// when both are present they must agree.
	ImageDigest string `json:"image_digest,omitempty"`

	// ImageSignature carries optional detached endorsement material for
	// ImageDigest: a signature over the canonical digest string plus the
	// signer certificate chain. Verification is performed by the
	// Security().Attestation() surface, which composes the go-xkms
	// attestation verifier.
	ImageSignature *ImageSignature `json:"image_signature,omitempty"`

	// ExpectedPCRs is the golden PCR set a Tier 1 host quote is checked
	// against, keyed by PCR index. An empty map means no PCR is checked,
	// which is only valid below TrustLevelAttested.
	ExpectedPCRs map[uint32][]byte `json:"expected_pcrs,omitempty"`

	// TrustLevel is the integrity tier required of this container. The zero
	// value (TrustLevelDefault) defers to the server's TrustPolicy, which
	// itself defaults to opt-in — so an unset TrustLevel changes nothing.
	TrustLevel TrustLevel `json:"trust_level,omitempty"`

	// TrustLabel is an operator-assigned name for the approved workload
	// identity. It is folded into the configuration digest, so one image
	// and configuration can carry two distinct measured identities.
	TrustLabel string `json:"trust_label,omitempty"`

	// MeasurementPCR is the PCR index extended with the image and
	// configuration digest at measured launch (Tier 2). PCR 16 — the TCG
	// debug/application register — is the conventional choice for
	// application-supplied measurements and is what MeasurementPCRDefault
	// resolves to.
	MeasurementPCR int `json:"measurement_pcr,omitempty"`

	// MeasurementBank names the PCR bank hash algorithm used for measured
	// launch: sha1, sha256, sha384 or sha512. Empty resolves to sha256.
	MeasurementBank string `json:"measurement_bank,omitempty"`
}

// ContainerMount defines a mount between host and container.
type ContainerMount struct {
	Source   string `json:"source"`   // host path
	Target   string `json:"target"`   // container path
	ReadOnly bool   `json:"readonly"` // :ro or :rw

	// Type names the OCI mount kind: MountTypeBind, MountTypeVolume or
	// MountTypeTmpfs. The zero value keeps the historical short form
	// (-v source:target[:ro]), whose kind the runtime infers from the
	// source, so existing specs are unaffected. Naming the kind explicitly
	// makes it part of the measured configuration: a bind mount of the host
	// filesystem cannot then be substituted for a tmpfs — or the reverse —
	// without changing ContainerConfigDigest.
	Type string `json:"type,omitempty"`

	// TmpfsSizeBytes bounds a MountTypeTmpfs mount's size in bytes
	// (rendered as "tmpfs-size=<bytes>" in the docker/podman --mount CSV
	// form). Zero leaves the mount unbounded, up to the container's whole
	// memory cgroup — see ContainerSpec.MemoryBytes and
	// ValidateTmpfsBudget. Meaningless, and rejected by Validate, on any
	// mount kind other than tmpfs.
	TmpfsSizeBytes int64 `json:"tmpfs_size_bytes,omitempty"`
}

// Mount kinds accepted by ContainerMount.Type. They are the docker/podman
// --mount types; the empty string keeps the untyped short form.
const (
	// MountTypeBind mounts a host path into the container.
	MountTypeBind = "bind"

	// MountTypeVolume mounts a runtime-managed named volume.
	MountTypeVolume = "volume"

	// MountTypeTmpfs mounts a kernel tmpfs, which has no host source.
	MountTypeTmpfs = "tmpfs"
)

// supportedMountTypes is the set of kinds ContainerMount.Type accepts. An
// unrecognized kind is rejected at validation rather than passed through to
// the runtime, where it would fail with a runtime-specific message long after
// the configuration was measured.
var supportedMountTypes = map[string]struct{}{
	MountTypeBind:   {},
	MountTypeVolume: {},
	MountTypeTmpfs:  {},
}

// Validate checks that the ContainerSpec has all required fields.
func (s *ContainerSpec) Validate() error {
	if s.Image == "" {
		return &InputValidationError{Field: "image", Message: "cannot be empty"}
	}
	if s.MemoryBytes < 0 {
		return &InputValidationError{Field: "memory_bytes", Message: "cannot be negative"}
	}
	// The environment reaches the runtime through a line-oriented --env-file,
	// so an entry that cannot be represented there is rejected here — before
	// any file is written and before the launch is measured.
	if err := validateContainerEnv(s.Env); err != nil {
		return err
	}
	for i, m := range s.Mounts {
		if err := m.Validate(); err != nil {
			return &InputValidationError{
				Field:   "mounts[" + intToStr(i) + "]",
				Message: err.Error(),
			}
		}
	}
	// A tmpfs mount is memory, not disk, so a spec may be individually valid
	// in every field and still describe a container that cannot run: see
	// ValidateMemoryBudget. It is checked HERE rather than by the caller that
	// sized the tmpfs, because Validate is what every launch path already
	// calls (ExecContainerRuntime.Run and RunInteractive both do), and a check
	// a caller could forget is a check the fleet does not have.
	if err := ValidateMemoryBudget(s); err != nil {
		return err
	}
	return s.validateTrust()
}

// Validate checks that the ContainerMount has all required fields.
func (m *ContainerMount) Validate() error {
	if m.Type != "" {
		if _, ok := supportedMountTypes[m.Type]; !ok {
			return &InputValidationError{
				Field:   "type",
				Message: "must be one of: " + MountTypeBind + ", " + MountTypeVolume + ", " + MountTypeTmpfs,
			}
		}
	}
	if m.TmpfsSizeBytes < 0 {
		return &InputValidationError{Field: "tmpfs_size_bytes", Message: "cannot be negative"}
	}
	if m.TmpfsSizeBytes > 0 && m.Type != MountTypeTmpfs {
		return &InputValidationError{
			Field:   "tmpfs_size_bytes",
			Message: "may only be set on a " + MountTypeTmpfs + " mount",
		}
	}
	// A tmpfs is materialized by the kernel and has no host source. Requiring
	// one would be unsatisfiable; accepting one and dropping it would let a
	// measured field vanish from the launched container.
	if m.Type == MountTypeTmpfs {
		if m.Source != "" {
			return &InputValidationError{
				Field:   "source",
				Message: "must be empty for a " + MountTypeTmpfs + " mount",
			}
		}
	} else if m.Source == "" {
		return &InputValidationError{Field: "source", Message: "cannot be empty"}
	}
	if m.Target == "" {
		return &InputValidationError{Field: "target", Message: "cannot be empty"}
	}
	return nil
}

// Format returns the mount as a docker-compatible short-form volume string.
// Format: source:target[:ro]. It carries no mount kind; a mount that names
// one is rendered by MountSpec instead. Use RunArgs to obtain the flags for
// either shape.
func (m *ContainerMount) Format() string {
	s := m.Source + ":" + m.Target
	if m.ReadOnly {
		s += ":ro"
	}
	return s
}

// MountSpec renders the mount in docker/podman --mount CSV form, which is the
// only form that can express the mount kind.
func (m *ContainerMount) MountSpec() string {
	fields := make([]string, 0, 5)
	fields = append(fields, "type="+m.Type)
	if m.Source != "" {
		fields = append(fields, "source="+m.Source)
	}
	fields = append(fields, "destination="+m.Target)
	if m.ReadOnly {
		fields = append(fields, "readonly")
	}
	if m.TmpfsSizeBytes > 0 {
		fields = append(fields, "tmpfs-size="+strconv.FormatInt(m.TmpfsSizeBytes, 10))
	}
	return strings.Join(fields, ",")
}

// RunArgs returns the command-line flags that materialize the mount. An
// untyped mount keeps the short -v form, so a spec written before Type existed
// produces a byte-identical command line; a typed mount uses --mount, because
// -v cannot express the kind that was measured.
func (m *ContainerMount) RunArgs() []string {
	if m.Type == "" {
		return []string{"-v", m.Format()}
	}
	return []string{"--mount", m.MountSpec()}
}

// intToStr converts an integer to a string without importing strconv.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
