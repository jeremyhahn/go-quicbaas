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
//
// Optional capabilities of the wrapped runtime (image listing, the orphan
// sweep, the reaper's listing, trust tiers) are found through RuntimeAs, not
// by type assertion on the wrapper.
type VolumeRuntime struct {
	ContainerRuntime

	attacher *VolumeAttacher

	mu       sync.Mutex
	attached map[string]*volumeRecord // container ID -> volumes still held
	// pending holds volumes a failed launch's rollback could not release; they
	// are retried before the next volume launch.
	pending []AttachedVolume
}

// volumeRecord is one container's held volumes. Its own mutex serialises
// concurrent teardowns of that container, so a volume is never released twice.
type volumeRecord struct {
	mu   sync.Mutex
	vols []AttachedVolume
}

var _ RuntimeUnwrapper = (*VolumeRuntime)(nil)

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
		attached:         make(map[string]*volumeRecord),
	}, nil
}

// Unwrap returns the runtime this one wraps.
func (r *VolumeRuntime) Unwrap() ContainerRuntime { return r.ContainerRuntime }

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
	return r.teardown(ctx, containerID, r.ContainerRuntime.Stop(ctx, containerID))
}

// Remove removes the container, then releases its volumes if Stop has not.
func (r *VolumeRuntime) Remove(ctx context.Context, containerID string) error {
	return r.teardown(ctx, containerID, r.ContainerRuntime.Remove(ctx, containerID))
}

// teardown releases the container's volumes after the inner Stop or Remove
// returned err. An interactive container runs with --rm, so one that exited by
// itself is already gone and the inner call reports it not found: its volumes
// are still released then. A release failure is returned alone, not joined
// with the not-found error, so a caller that treats not-found as success
// still sees it; when the release succeeds the inner error is returned as is.
func (r *VolumeRuntime) teardown(ctx context.Context, containerID string, err error) error {
	var notFound *ContainerNotFoundError
	if err != nil && !errors.As(err, &notFound) {
		return err
	}
	if rerr := r.release(ctx, containerID); rerr != nil {
		return rerr
	}
	return err
}

// Attached returns the volumes held for a running container.
func (r *VolumeRuntime) Attached(containerID string) []AttachedVolume {
	r.mu.Lock()
	rec := r.attached[containerID]
	r.mu.Unlock()
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]AttachedVolume(nil), rec.vols...)
}

func (r *VolumeRuntime) attach(ctx context.Context, spec *ContainerSpec) (*ContainerSpec, []AttachedVolume, error) {
	for _, c := range spec.Volumes {
		if c.FSType == "" && spec.PrivilegeClass != PrivilegeClassBroker {
			return nil, nil, fmt.Errorf("%w: volume %q: a raw volume is passed through as a device, which only the %q class may have",
				ErrDevicePassthroughDenied, c.Name, PrivilegeClassBroker)
		}
	}
	r.retryPending(ctx)
	vols, held, err := r.attacher.attach(ctx, spec.Volumes)
	if err != nil {
		r.addPending(held)
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

// releaseAfter rolls a failed start back on a context of its own, since the
// start may have failed because its context was cancelled.
func (r *VolumeRuntime) releaseAfter(ctx context.Context, cause error, vols []AttachedVolume) error {
	cctx, cancel := cleanupContext(ctx)
	defer cancel()
	held, err := r.attacher.release(cctx, vols)
	r.addPending(held)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("release volumes after failed start: %w", err))
	}
	return cause
}

// retryPending retries the volumes an earlier rollback could not release. Its
// errors are dropped: the launch that left them already returned that error.
func (r *VolumeRuntime) retryPending(ctx context.Context) {
	r.mu.Lock()
	vols := r.pending
	r.pending = nil
	r.mu.Unlock()
	if len(vols) == 0 {
		return
	}
	cctx, cancel := cleanupContext(ctx)
	defer cancel()
	held, _ := r.attacher.release(cctx, vols)
	r.addPending(held)
}

func (r *VolumeRuntime) addPending(vols []AttachedVolume) {
	if len(vols) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, vols...)
}

func (r *VolumeRuntime) track(id string, vols []AttachedVolume) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attached[id] = &volumeRecord{vols: vols}
}

// release releases what is still held for the container. What fails to
// release stays on its record for the next Stop or Remove; the record goes
// once nothing is held.
func (r *VolumeRuntime) release(ctx context.Context, id string) error {
	r.mu.Lock()
	rec := r.attached[id]
	r.mu.Unlock()
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.vols) == 0 {
		return nil // a concurrent teardown finished it
	}
	remaining, err := r.attacher.release(ctx, rec.vols)
	rec.vols = remaining
	if len(remaining) == 0 {
		r.mu.Lock()
		if r.attached[id] == rec {
			delete(r.attached, id)
		}
		r.mu.Unlock()
	}
	return err
}
