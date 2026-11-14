package orchestration

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// VolumeRuntime is the node agent's launch path for workloads that declare
// volumes. It wraps the node's ContainerRuntime: Run and RunInteractive
// attach the spec's VolumeClaims (provisioning or growing them first), hand
// the inner runtime a copy of the spec with the volumes resolved into bind
// mounts, and release the volumes again when the start fails. Stop and Remove
// release a container's volumes after the inner runtime has stopped it.
//
// It must be the outermost runtime, so an attesting runtime inside it measures
// the resolved mounts: the launch measurement covers where each volume is
// mounted, not just that one was asked for.
//
// Raw claims (no FSType) are passed through as devices at their Target, which
// only a PrivilegeClassBroker spec may have; any other spec's raw claim is
// refused with ErrDevicePassthroughDenied before anything is provisioned.
type VolumeRuntime struct {
	ContainerRuntime

	attacher *VolumeAttacher

	mu       sync.Mutex
	attached map[string][]AttachedVolume // container ID -> volumes
}

// NewVolumeRuntime wraps inner with the attacher's volumes.
func NewVolumeRuntime(inner ContainerRuntime, attacher *VolumeAttacher) (*VolumeRuntime, error) {
	if inner == nil {
		return nil, fmt.Errorf("%w: volume runtime: nil container runtime", ErrVolumeInvalidArgument)
	}
	if attacher == nil {
		return nil, fmt.Errorf("%w: volume runtime: nil attacher", ErrVolumeInvalidArgument)
	}
	return &VolumeRuntime{
		ContainerRuntime: inner,
		attacher:         attacher,
		attached:         make(map[string][]AttachedVolume),
	}, nil
}

// Attacher returns the runtime's volume attacher.
func (r *VolumeRuntime) Attacher() *VolumeAttacher { return r.attacher }

// Run attaches the spec's volumes and runs the container with them mounted.
func (r *VolumeRuntime) Run(ctx context.Context, spec *ContainerSpec) (string, error) {
	if spec == nil || len(spec.Volumes) == 0 {
		return r.ContainerRuntime.Run(ctx, spec)
	}
	resolved, vols, err := r.attach(ctx, spec)
	if err != nil {
		return "", err
	}
	id, err := r.ContainerRuntime.Run(ctx, resolved)
	if err != nil {
		return "", r.releaseAfter(ctx, err, vols)
	}
	r.track(id, vols)
	return id, nil
}

// RunInteractive attaches the spec's volumes and starts the container with
// them mounted.
func (r *VolumeRuntime) RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	if spec == nil || len(spec.Volumes) == 0 {
		return r.ContainerRuntime.RunInteractive(ctx, spec)
	}
	resolved, vols, err := r.attach(ctx, spec)
	if err != nil {
		return nil, err
	}
	p, err := r.ContainerRuntime.RunInteractive(ctx, resolved)
	if err != nil {
		return nil, r.releaseAfter(ctx, err, vols)
	}
	r.track(p.ContainerID, vols)
	return p, nil
}

// Stop stops the container, then releases its volumes.
func (r *VolumeRuntime) Stop(ctx context.Context, containerID string) error {
	if err := r.ContainerRuntime.Stop(ctx, containerID); err != nil {
		return err
	}
	return r.release(ctx, containerID)
}

// Remove removes the container, then releases its volumes if Stop has not.
func (r *VolumeRuntime) Remove(ctx context.Context, containerID string) error {
	if err := r.ContainerRuntime.Remove(ctx, containerID); err != nil {
		return err
	}
	return r.release(ctx, containerID)
}

// Attached returns the volumes held for a running container.
func (r *VolumeRuntime) Attached(containerID string) []AttachedVolume {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AttachedVolume(nil), r.attached[containerID]...)
}

func (r *VolumeRuntime) attach(ctx context.Context, spec *ContainerSpec) (*ContainerSpec, []AttachedVolume, error) {
	for _, c := range spec.Volumes {
		if c.FSType == "" && spec.PrivilegeClass != PrivilegeClassBroker {
			return nil, nil, fmt.Errorf("%w: volume %q: a raw volume is passed through as a device, which only the %q class may have",
				ErrDevicePassthroughDenied, c.Name, PrivilegeClassBroker)
		}
	}
	vols, err := r.attacher.Attach(ctx, spec.Volumes)
	if err != nil {
		return nil, nil, err
	}
	resolved := *spec
	resolved.Volumes = nil
	resolved.Mounts = append(append([]ContainerMount(nil), spec.Mounts...), ContainerMounts(vols)...)
	if devs := ContainerDevices(vols); len(devs) > 0 {
		resolved.Devices = append(append([]ContainerDevice(nil), spec.Devices...), devs...)
	}
	return &resolved, vols, nil
}

func (r *VolumeRuntime) releaseAfter(ctx context.Context, cause error, vols []AttachedVolume) error {
	if err := r.attacher.Release(ctx, vols); err != nil {
		return errors.Join(cause, fmt.Errorf("release volumes after failed start: %w", err))
	}
	return cause
}

func (r *VolumeRuntime) track(id string, vols []AttachedVolume) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attached[id] = vols
}

func (r *VolumeRuntime) release(ctx context.Context, id string) error {
	r.mu.Lock()
	vols, ok := r.attached[id]
	delete(r.attached, id)
	r.mu.Unlock()
	if !ok {
		return nil
	}
	return r.attacher.Release(ctx, vols)
}
