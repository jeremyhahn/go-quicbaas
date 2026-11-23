package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// VolumeStateFile is the file in the attacher's staging dir where the
// VolumeRuntime records the volumes it holds. A claim name starts with a
// lower-case letter or digit, so it cannot clash with a volume's mount point.
const VolumeStateFile = ".quicbaas-attachments.json"

// VolumeStateLockFile is the file in the staging dir a VolumeRuntime holds an
// exclusive lock on, so one runtime at a time owns the dir's state file.
const VolumeStateLockFile = ".quicbaas-attachments.lock"

// VolumeRuntime is the node agent's launch path for workloads that declare
// volumes. It wraps the node's ContainerRuntime: Run and RunInteractive
// attach the spec's VolumeClaims (provisioning or growing them first), hand
// the inner runtime a copy of the spec with the volumes resolved into bind
// mounts, and release the volumes again when the start fails. Stop and Remove
// release a container's volumes after the inner runtime has stopped it.
//
// A volume has one owner on the node, from the start of its launch until its
// release: a launch claiming a volume another container holds, or one a
// failed release left pending, is refused with ErrVolumeInUse before anything
// is attached. The volumes held are written to VolumeStateFile in the staging
// dir after every change, so a restarted node keeps refusing them and
// releases them through ReleaseRecovered once the startup sweep has removed
// their containers.
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

	attacher  *VolumeAttacher
	statePath string

	mu sync.Mutex
	// lock holds the staging dir's VolumeStateLockFile until Close.
	lock *os.File
	// attached maps a container's ID and its name to its record.
	attached map[string]*volumeRecord
	// pending holds volumes a failed launch's rollback could not release; they
	// are retried before the next volume launch.
	pending []AttachedVolume
	// owned maps a claim name to the volume its owner holds; a launch's entry
	// has no VolumeID until the volume is attached.
	owned map[string]AttachedVolume
	// recovered holds the volumes read from the state file at start, owned by
	// containers of an earlier run of the node.
	recovered []AttachedVolume
}

// volumeRecord is one container's held volumes, filed under each of keys. Its
// own mutex serialises concurrent teardowns of that container, so a volume is
// never released twice.
type volumeRecord struct {
	mu   sync.Mutex
	keys []string
	vols []AttachedVolume
}

// volumeState is the state file's content.
type volumeState struct {
	Volumes []AttachedVolume `json:"volumes"`
}

var (
	_ RuntimeUnwrapper        = (*VolumeRuntime)(nil)
	_ ContainerVolumeReleaser = (*VolumeRuntime)(nil)
	_ RecoveredVolumeReleaser = (*VolumeRuntime)(nil)
)

// NewVolumeRuntime wraps inner with the attacher's volumes. It reads the
// volumes an earlier run of the node left held from the staging dir's state
// file: they stay owned until ReleaseRecovered releases them. A state file
// that cannot be read is an error, so the node does not start blind.
//
// The runtime holds the staging dir exclusively, through a lock on its
// VolumeStateLockFile, until Close or the process exits: a second runtime on
// the same dir gets ErrVolumeStateLocked.
func NewVolumeRuntime(inner ContainerRuntime, attacher *VolumeAttacher) (*VolumeRuntime, error) {
	if inner == nil {
		return nil, fmt.Errorf("%w: volume runtime: nil container runtime", ErrVolumeInvalidArgument)
	}
	if attacher == nil {
		return nil, fmt.Errorf("%w: volume runtime: nil attacher", ErrVolumeInvalidArgument)
	}
	if err := os.MkdirAll(attacher.stagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("volume runtime: staging dir: %w", err)
	}
	lockPath := filepath.Join(attacher.stagingDir, VolumeStateLockFile)
	lock, err := lockVolumeState(lockPath)
	if err != nil {
		return nil, fmt.Errorf("volume runtime: %s: %w", lockPath, err)
	}
	r := &VolumeRuntime{
		ContainerRuntime: inner,
		attacher:         attacher,
		statePath:        filepath.Join(attacher.stagingDir, VolumeStateFile),
		lock:             lock,
		attached:         make(map[string]*volumeRecord),
		owned:            make(map[string]AttachedVolume),
	}
	recovered, err := readVolumeState(r.statePath)
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("volume runtime: read %s: %w", r.statePath, err)
	}
	for _, v := range recovered {
		r.owned[v.Claim.Name] = v
	}
	r.recovered = recovered
	return r, nil
}

// Close releases the staging dir's lock. It is idempotent.
func (r *VolumeRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lock == nil {
		return nil
	}
	err := r.lock.Close() // closing the only descriptor drops the flock
	r.lock = nil
	return err
}

func readVolumeState(path string) ([]AttachedVolume, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st volumeState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	for _, v := range st.Volumes {
		if v.Claim.Name == "" || v.VolumeID == "" {
			return nil, fmt.Errorf("%w: a recorded volume has no claim name or volume id", ErrVolumeInvalidArgument)
		}
	}
	return st.Volumes, nil
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
	r.track(vols, id, spec.Name)
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
	r.track(vols, p.ContainerID, spec.Name)
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

// ReleaseContainer releases the volumes of the container filed under the
// first of keys (its ID or its name) that has a record. A caller that removed
// the container through the inner runtime, or knows it under another key,
// releases its volumes with it.
func (r *VolumeRuntime) ReleaseContainer(ctx context.Context, keys ...string) error {
	r.mu.Lock()
	var key string
	for _, k := range keys {
		if k != "" && r.attached[k] != nil {
			key = k
			break
		}
	}
	r.mu.Unlock()
	if key == "" {
		return nil
	}
	return r.release(ctx, key)
}

// ReleaseRecovered releases the volumes an earlier run of the node left held.
// Call it once their containers are gone (after the startup sweep). What
// fails to release stays owned and pending, and is retried before the next
// volume launch.
func (r *VolumeRuntime) ReleaseRecovered(ctx context.Context) error {
	r.mu.Lock()
	vols := r.recovered
	r.recovered = nil
	r.mu.Unlock()
	if len(vols) == 0 {
		return nil
	}
	cctx, cancel := cleanupContext(ctx)
	defer cancel()
	held, err := r.attacher.release(cctx, vols)
	werr := r.settle(vols, held, true)
	return errors.Join(err, werr)
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

// Attached returns the volumes held for a running container, by its ID or
// its name.
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
	if err := r.reserve(spec.Volumes); err != nil {
		return nil, nil, err
	}
	vols, held, err := r.attacher.attach(ctx, spec.Volumes)
	if err != nil {
		claims := make([]AttachedVolume, 0, len(spec.Volumes))
		for _, c := range spec.Volumes {
			claims = append(claims, AttachedVolume{Claim: c})
		}
		if werr := r.settle(claims, held, true); werr != nil {
			err = errors.Join(err, werr)
		}
		return nil, nil, err
	}
	r.mu.Lock()
	for _, v := range vols {
		r.owned[v.Claim.Name] = v
	}
	werr := r.persistLocked()
	r.mu.Unlock()
	if werr != nil {
		return nil, nil, r.releaseAfter(ctx, werr, vols)
	}
	resolved := *spec
	resolved.Volumes = nil
	resolved.Mounts = append(append([]ContainerMount(nil), spec.Mounts...), ContainerMounts(vols)...)
	if devs := ContainerDevices(vols); len(devs) > 0 {
		resolved.Devices = append(append([]ContainerDevice(nil), spec.Devices...), devs...)
	}
	return &resolved, vols, nil
}

// reserve makes the launch the owner of every claim, or of none when another
// owner holds one of them.
func (r *VolumeRuntime) reserve(claims []VolumeClaim) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range claims {
		if _, ok := r.owned[c.Name]; ok {
			return fmt.Errorf("%w: volume %q", ErrVolumeInUse, c.Name)
		}
	}
	for _, c := range claims {
		r.owned[c.Name] = AttachedVolume{Claim: c}
	}
	return nil
}

// settle records the outcome of releasing vols: what is still held keeps its
// owner (and goes to pending when pend is set), the rest is no longer owned.
// It writes the state file and returns the write's error.
func (r *VolumeRuntime) settle(vols, held []AttachedVolume, pend bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range vols {
		delete(r.owned, v.Claim.Name)
	}
	for _, v := range held {
		r.owned[v.Claim.Name] = v
	}
	if pend {
		r.pending = append(r.pending, held...)
	}
	return r.persistLocked()
}

// persistLocked writes the owned volumes that have a VolumeID to the state
// file, through a temporary file renamed over it. r.mu must be held.
func (r *VolumeRuntime) persistLocked() error {
	st := volumeState{Volumes: []AttachedVolume{}}
	for _, v := range r.owned {
		if v.VolumeID != "" {
			st.Volumes = append(st.Volumes, v)
		}
	}
	slices.SortFunc(st.Volumes, func(a, b AttachedVolume) int { return strings.Compare(a.Claim.Name, b.Claim.Name) })
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("volume runtime: encode state: %w", err)
	}
	dir := filepath.Dir(r.statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("volume runtime: write state: %w", err)
	}
	f, err := os.CreateTemp(dir, VolumeStateFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("volume runtime: write state: %w", err)
	}
	tmp := f.Name()
	werr := f.Chmod(0o600)
	if werr == nil {
		_, werr = f.Write(data)
	}
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp, r.statePath)
	}
	if werr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("volume runtime: write state %s: %w", r.statePath, werr)
	}
	return nil
}

// releaseAfter rolls a failed start back on a context of its own, since the
// start may have failed because its context was cancelled.
func (r *VolumeRuntime) releaseAfter(ctx context.Context, cause error, vols []AttachedVolume) error {
	cctx, cancel := cleanupContext(ctx)
	defer cancel()
	held, err := r.attacher.release(cctx, vols)
	werr := r.settle(vols, held, true)
	if err != nil {
		cause = errors.Join(cause, fmt.Errorf("release volumes after failed start: %w", err))
	}
	if werr != nil {
		cause = errors.Join(cause, werr)
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
	_ = r.settle(vols, held, true)
}

// track files the container's record under each of its non-empty keys.
func (r *VolumeRuntime) track(vols []AttachedVolume, keys ...string) {
	var uniq []string
	for _, k := range keys {
		if k != "" && !slices.Contains(uniq, k) {
			uniq = append(uniq, k)
		}
	}
	rec := &volumeRecord{keys: uniq, vols: vols}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range uniq {
		r.attached[k] = rec
	}
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
	vols := rec.vols
	remaining, err := r.attacher.release(ctx, vols)
	rec.vols = remaining
	werr := r.settle(vols, remaining, false)
	if len(remaining) == 0 {
		r.mu.Lock()
		for _, k := range rec.keys {
			if r.attached[k] == rec {
				delete(r.attached, k)
			}
		}
		r.mu.Unlock()
	}
	if werr != nil {
		return errors.Join(err, werr)
	}
	return err
}
