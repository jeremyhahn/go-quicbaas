package orchestration

// Block volumes for container workloads, over go-blockstore.
//
// go-blockstore owns the volumes: provisioning, attaching a volume to a node as
// a block device, mounting it, growing it, and the primary/secondary role of a
// replicated one. This file is the orchestration side of that seam. A workload
// names the volumes it needs as VolumeClaims; a VolumeAttacher, holding the
// node's VolumeProvider and node identity, turns each claim into a volume
// attached on this node and, for a claim with a filesystem, a host mount the
// container receives as a bind mount. Release undoes it.
//
// The types re-exported here are go-blockstore's own, aliased so a caller of
// pkg/orchestration (and of sdk/go/orchestration) needs no second import and
// sees one set of names.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"

	bscommon "github.com/jeremyhahn/go-blockstore/pkg/common"
	bsfactory "github.com/jeremyhahn/go-blockstore/pkg/factory"
)

type (
	// VolumeProvider is go-blockstore's block-storage backend contract.
	VolumeProvider = bscommon.VolumeProvider

	// ReplicatedVolumeProvider is a VolumeProvider with primary/secondary
	// roles (DRBD). Promote and Demote reach it.
	ReplicatedVolumeProvider = bscommon.ReplicatedProvider

	// VolumeSpec is the spec a volume is provisioned from.
	VolumeSpec = bscommon.VolumeSpec

	// Volume is a provisioned volume and the spec it came from.
	Volume = bscommon.Volume

	// VolumeStatus is the observed state of a volume.
	VolumeStatus = bscommon.VolumeStatus

	// VolumeAttachInfo is the result of attaching a volume to a node.
	VolumeAttachInfo = bscommon.AttachInfo

	// VolumeMountOptions controls how a provider mounts a volume.
	VolumeMountOptions = bscommon.MountOptions

	// VolumeNodeID names the node a volume is attached to.
	VolumeNodeID = bscommon.NodeID
)

var (
	// ErrVolumeNotFound is go-blockstore's ErrNotFound: the volume does not
	// exist.
	ErrVolumeNotFound = bscommon.ErrNotFound

	// ErrVolumeAlreadyExists is go-blockstore's ErrAlreadyExists.
	ErrVolumeAlreadyExists = bscommon.ErrAlreadyExists

	// ErrVolumeNotAttached is go-blockstore's ErrNotAttached.
	ErrVolumeNotAttached = bscommon.ErrNotAttached

	// ErrVolumeUnsupported is go-blockstore's ErrUnsupported: the provider
	// cannot serve a well-formed request (a promote on a provider without
	// replication, a node out of its reach).
	ErrVolumeUnsupported = bscommon.ErrUnsupported

	// ErrVolumeInvalidArgument is go-blockstore's ErrInvalidArgument.
	ErrVolumeInvalidArgument = bscommon.ErrInvalidArgument

	// ErrVolumeShrink is returned by a resize to below the volume's current
	// size. Volumes are grow-only: a shrink would cut the filesystem on it.
	ErrVolumeShrink = errors.New("volume: resize is grow-only")

	// ErrVolumeClaim is returned when a VolumeClaim is malformed.
	ErrVolumeClaim = errors.New("volume: invalid claim")
)

// NewVolumeProvider constructs the named go-blockstore provider ("local", or
// "drbd" in a build with the drbd tag) from its snake_case settings. An unknown
// provider or an unknown settings key is an error.
func NewVolumeProvider(name string, settings map[string]string) (VolumeProvider, error) {
	p, err := bsfactory.NewProvider(name, settings)
	if err != nil {
		return nil, fmt.Errorf("volume provider %q: %w", name, err)
	}
	return p, nil
}

// volumeNamePattern bounds a claim's name. The name becomes the volume's name
// in the backend (an LVM logical volume, a DRBD resource) and a directory
// under the staging root, so it is kept to a portable, path-safe set.
var volumeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,62}$`)

// VolumeClaim is a workload's request for one block volume.
//
// A claim with FSType is mounted on the host and reaches the container as a
// bind mount at Target. A claim without FSType is a raw block device: the
// attacher reports its device path and the claim carries no Target.
type VolumeClaim struct {
	// Name identifies the volume. The attacher looks the volume up under this
	// name and provisions it when it does not exist, so a relaunch reattaches
	// the same volume.
	Name string `json:"name"`

	// SizeBytes is the usable capacity. A volume found smaller than this is
	// grown to it; one found larger is left as it is.
	SizeBytes int64 `json:"size_bytes"`

	// FSType is the filesystem the volume is created with ("ext4", "xfs").
	// Empty means a raw block device.
	FSType string `json:"fs_type,omitempty"`

	// Target is the absolute path inside the container. Required with FSType,
	// rejected without it.
	Target string `json:"target,omitempty"`

	// ReadOnly mounts the volume read-only.
	ReadOnly bool `json:"readonly,omitempty"`

	// Replicas is passed to the provider; zero means the provider's natural
	// count.
	Replicas int `json:"replicas,omitempty"`

	// Labels are passed to the provider as the volume's labels.
	Labels map[string]string `json:"labels,omitempty"`
}

// Validate reports whether the claim is well-formed. It checks only what the
// orchestration side owns; the provider validates the rest (the filesystem
// type against its allowlist, the name against its backend's rules).
func (c VolumeClaim) Validate() error {
	if !volumeNamePattern.MatchString(c.Name) {
		return fmt.Errorf("%w: name %q must match %s", ErrVolumeClaim, c.Name, volumeNamePattern)
	}
	if c.SizeBytes <= 0 {
		return fmt.Errorf("%w: volume %q: size_bytes must be positive", ErrVolumeClaim, c.Name)
	}
	if c.Replicas < 0 {
		return fmt.Errorf("%w: volume %q: replicas must not be negative", ErrVolumeClaim, c.Name)
	}
	if c.FSType == "" {
		if c.Target != "" {
			return fmt.Errorf("%w: volume %q: a raw volume (no fs_type) takes no target", ErrVolumeClaim, c.Name)
		}
		return nil
	}
	if c.Target == "" || !path.IsAbs(c.Target) || path.Clean(c.Target) != c.Target || c.Target == "/" {
		return fmt.Errorf("%w: volume %q: target %q must be a clean absolute path other than /", ErrVolumeClaim, c.Name, c.Target)
	}
	return nil
}

func (c VolumeClaim) spec() VolumeSpec {
	return VolumeSpec{
		Name:      c.Name,
		SizeBytes: c.SizeBytes,
		FSType:    c.FSType,
		Replicas:  c.Replicas,
		Labels:    c.Labels,
	}
}

// AttachedVolume is a claim attached on this node.
type AttachedVolume struct {
	// Claim is the claim the volume was attached for.
	Claim VolumeClaim `json:"claim"`

	// VolumeID is the provider's identifier for the volume.
	VolumeID string `json:"volume_id"`

	// DevicePath is the block device on this node.
	DevicePath string `json:"device_path"`

	// HostPath is where the volume is mounted on the host; empty for a raw
	// volume.
	HostPath string `json:"host_path,omitempty"`
}

// ContainerMount returns the bind mount that hands the volume to the
// container, and false for a raw volume, which has none.
func (v AttachedVolume) ContainerMount() (ContainerMount, bool) {
	if v.HostPath == "" {
		return ContainerMount{}, false
	}
	return ContainerMount{
		Source:   v.HostPath,
		Target:   v.Claim.Target,
		ReadOnly: v.Claim.ReadOnly,
		Type:     MountTypeBind,
	}, true
}

// VolumeAttacher attaches workload volumes on one node.
type VolumeAttacher struct {
	provider   VolumeProvider
	node       VolumeNodeID
	stagingDir string
}

// NewVolumeAttacher returns an attacher for the node, mounting filesystem
// volumes under stagingDir/<volume id>. stagingDir must be absolute.
func NewVolumeAttacher(provider VolumeProvider, node VolumeNodeID, stagingDir string) (*VolumeAttacher, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: volume attacher: nil provider", ErrVolumeInvalidArgument)
	}
	if node == "" {
		return nil, fmt.Errorf("%w: volume attacher: empty node id", ErrVolumeInvalidArgument)
	}
	if !filepath.IsAbs(stagingDir) {
		return nil, fmt.Errorf("%w: volume attacher: staging dir %q must be absolute", ErrVolumeInvalidArgument, stagingDir)
	}
	return &VolumeAttacher{provider: provider, node: node, stagingDir: filepath.Clean(stagingDir)}, nil
}

// Provider returns the attacher's provider.
func (a *VolumeAttacher) Provider() VolumeProvider { return a.provider }

// Node returns the node the attacher attaches to.
func (a *VolumeAttacher) Node() VolumeNodeID { return a.node }

// Ensure returns the ID of the claim's volume, provisioning it when it does not
// exist and growing it when it is smaller than the claim.
func (a *VolumeAttacher) Ensure(ctx context.Context, claim VolumeClaim) (string, error) {
	if err := claim.Validate(); err != nil {
		return "", err
	}
	st, err := a.provider.Status(ctx, claim.Name)
	if errors.Is(err, bscommon.ErrNotFound) {
		vol, perr := a.provider.Provision(ctx, claim.spec())
		if perr != nil {
			return "", fmt.Errorf("volume %q: provision: %w", claim.Name, perr)
		}
		return vol.ID, nil
	}
	if err != nil {
		return "", fmt.Errorf("volume %q: status: %w", claim.Name, err)
	}
	id := st.ID
	if id == "" {
		id = claim.Name
	}
	if st.SizeBytes < claim.SizeBytes {
		if err := a.provider.Resize(ctx, id, claim.SizeBytes); err != nil {
			return "", fmt.Errorf("volume %q: grow to %d bytes: %w", claim.Name, claim.SizeBytes, err)
		}
	}
	return id, nil
}

// Resize grows the volume to newSize bytes. A size below the current one is
// refused with ErrVolumeShrink before the provider is asked; an equal size is
// still passed on, so a repeat finishes a filesystem grow a previous call left
// undone (go-blockstore's Resize contract).
func (a *VolumeAttacher) Resize(ctx context.Context, volID string, newSize int64) error {
	if newSize <= 0 {
		return fmt.Errorf("%w: volume %q: size must be positive", ErrVolumeInvalidArgument, volID)
	}
	st, err := a.provider.Status(ctx, volID)
	if err != nil {
		return fmt.Errorf("volume %q: status: %w", volID, err)
	}
	if newSize < st.SizeBytes {
		return fmt.Errorf("%w: volume %q is %d bytes, asked for %d", ErrVolumeShrink, volID, st.SizeBytes, newSize)
	}
	if err := a.provider.Resize(ctx, volID, newSize); err != nil {
		return fmt.Errorf("volume %q: resize: %w", volID, err)
	}
	return nil
}

// Promote makes this node's copy of a replicated volume the primary. A
// provider without replication returns ErrVolumeUnsupported.
func (a *VolumeAttacher) Promote(ctx context.Context, volID string) error {
	rp, ok := a.provider.(ReplicatedVolumeProvider)
	if !ok {
		return fmt.Errorf("%w: volume %q: promote needs a replicated provider", ErrVolumeUnsupported, volID)
	}
	if err := rp.Promote(ctx, volID); err != nil {
		return fmt.Errorf("volume %q: promote: %w", volID, err)
	}
	return nil
}

// Demote makes this node's copy of a replicated volume a secondary. A provider
// without replication returns ErrVolumeUnsupported.
func (a *VolumeAttacher) Demote(ctx context.Context, volID string) error {
	rp, ok := a.provider.(ReplicatedVolumeProvider)
	if !ok {
		return fmt.Errorf("%w: volume %q: demote needs a replicated provider", ErrVolumeUnsupported, volID)
	}
	if err := rp.Demote(ctx, volID); err != nil {
		return fmt.Errorf("volume %q: demote: %w", volID, err)
	}
	return nil
}

// Attach ensures, attaches and (for a filesystem claim) mounts every claim, in
// order. The claims are validated together first: two claims may not share a
// name or a target. If any claim fails, the volumes already attached are
// released again and the error is returned, so a failed launch leaves nothing
// attached.
func (a *VolumeAttacher) Attach(ctx context.Context, claims []VolumeClaim) ([]AttachedVolume, error) {
	if err := validateClaims(claims); err != nil {
		return nil, err
	}
	attached := make([]AttachedVolume, 0, len(claims))
	for _, claim := range claims {
		v, err := a.attachOne(ctx, claim)
		if v.VolumeID != "" {
			attached = append(attached, v)
		}
		if err != nil {
			if rerr := a.Release(ctx, attached); rerr != nil {
				err = errors.Join(err, fmt.Errorf("release after failed attach: %w", rerr))
			}
			return nil, err
		}
	}
	return attached, nil
}

// attachOne returns the partly attached volume with the error when a step
// after the provider's Attach fails, so the caller can release it.
func (a *VolumeAttacher) attachOne(ctx context.Context, claim VolumeClaim) (AttachedVolume, error) {
	id, err := a.Ensure(ctx, claim)
	if err != nil {
		return AttachedVolume{}, err
	}
	if id == "" || id != filepath.Base(id) || id == "." || id == ".." {
		return AttachedVolume{}, fmt.Errorf("%w: volume %q: provider returned unusable id %q", ErrVolumeInvalidArgument, claim.Name, id)
	}
	info, err := a.provider.Attach(ctx, id, a.node)
	if err != nil {
		return AttachedVolume{}, fmt.Errorf("volume %q: attach to node %q: %w", claim.Name, a.node, err)
	}
	v := AttachedVolume{Claim: claim, VolumeID: id, DevicePath: info.DevicePath}
	if claim.FSType == "" {
		return v, nil
	}
	host := filepath.Join(a.stagingDir, id)
	if err := os.MkdirAll(host, 0o700); err != nil {
		return v, fmt.Errorf("volume %q: staging dir: %w", claim.Name, err)
	}
	if err := a.provider.Mount(ctx, id, host, VolumeMountOptions{ReadOnly: claim.ReadOnly}); err != nil {
		return v, fmt.Errorf("volume %q: mount at %s: %w", claim.Name, host, err)
	}
	v.HostPath = host
	return v, nil
}

// Release unmounts and detaches the volumes in reverse order. It carries on
// past a failure so one stuck volume does not keep the others attached, and
// returns every failure joined.
func (a *VolumeAttacher) Release(ctx context.Context, vols []AttachedVolume) error {
	var errs []error
	for i := len(vols) - 1; i >= 0; i-- {
		v := vols[i]
		if v.HostPath != "" {
			if err := a.provider.Unmount(ctx, v.VolumeID, v.HostPath); err != nil {
				errs = append(errs, fmt.Errorf("volume %q: unmount %s: %w", v.Claim.Name, v.HostPath, err))
				continue
			}
		}
		if err := a.provider.Detach(ctx, v.VolumeID, a.node); err != nil {
			errs = append(errs, fmt.Errorf("volume %q: detach from node %q: %w", v.Claim.Name, a.node, err))
		}
	}
	return errors.Join(errs...)
}

// ContainerMounts returns the bind mounts for the attached volumes that have a
// filesystem, in order.
func ContainerMounts(vols []AttachedVolume) []ContainerMount {
	var out []ContainerMount
	for _, v := range vols {
		if m, ok := v.ContainerMount(); ok {
			out = append(out, m)
		}
	}
	return out
}

func validateClaims(claims []VolumeClaim) error {
	names := make(map[string]struct{}, len(claims))
	targets := make(map[string]struct{}, len(claims))
	for _, c := range claims {
		if err := c.Validate(); err != nil {
			return err
		}
		if _, dup := names[c.Name]; dup {
			return fmt.Errorf("%w: volume %q claimed twice", ErrVolumeClaim, c.Name)
		}
		names[c.Name] = struct{}{}
		if c.Target == "" {
			continue
		}
		if _, dup := targets[c.Target]; dup {
			return fmt.Errorf("%w: two volumes target %q", ErrVolumeClaim, c.Target)
		}
		targets[c.Target] = struct{}{}
	}
	return nil
}
