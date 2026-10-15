// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/moby/sys/user"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// RuntimeConfig holds configuration for the embedded container runtime.
type RuntimeConfig struct {
	// Root is the state directory for containers
	Root string `json:"root" yaml:"root"`

	// RunRoot is the runtime state directory
	RunRoot string `json:"run_root" yaml:"run_root"`

	// TmpDir is the directory for temporary files
	TmpDir string `json:"tmp_dir" yaml:"tmp_dir"`

	// GraphDriverName is the storage driver (overlay, vfs, etc.)
	GraphDriverName string `json:"graph_driver" yaml:"graph_driver"`

	// StaticDir is the directory for static files
	StaticDir string `json:"static_dir" yaml:"static_dir"`

	// VolumePath is the directory for volumes
	VolumePath string `json:"volume_path" yaml:"volume_path"`

	// ImageStorePath is the image and layer storage root. Empty means
	// Root/storage.
	ImageStorePath string `json:"image_store_path" yaml:"image_store_path"`

	// InsecureRegistries lists registry hosts (host[:port]) that may be
	// pulled from over plain HTTP or with unverified TLS.
	InsecureRegistries []string `json:"insecure_registries,omitempty" yaml:"insecure_registries,omitempty"`

	// SignaturePolicyPath is the containers-policy.json the image pull
	// enforces. Empty means the host default, /etc/containers/policy.json.
	SignaturePolicyPath string `json:"signature_policy_path,omitempty" yaml:"signature_policy_path,omitempty"`
}

// The runtime carries no TPM default.
//
// A host-level "enable TPM access" flag used to grant /dev/tpm0 to every
// container the runtime started, whatever its configuration said. The platform
// mirrors its own default into the container specification before measuring it,
// so the platform's path was covered — but any other caller received a device
// that appeared in no container configuration, and therefore in no
// configuration digest. The grant now has exactly one source: the container
// configuration the measurement is taken over
// (ContainerConfig.TPMAccess, itself derived from the measured /dev/tpm0 mount
// by PodmanContainerConfig.GrantsTPMDevice). A caller that wants a default must
// put it in the specification, where it is measured, before the container is
// created.

// DefaultRuntimeConfig returns sensible defaults for the embedded runtime.
func DefaultRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		Root:            "/var/lib/trusted-platform/containers",
		RunRoot:         "/run/trusted-platform/containers",
		TmpDir:          "/run/trusted-platform/tmp",
		GraphDriverName: "overlay",
		StaticDir:       "/var/lib/trusted-platform/containers/static",
		VolumePath:      "/var/lib/trusted-platform/containers/volumes",
		ImageStorePath:  "/var/lib/trusted-platform/containers/images",
	}
}

// Validate validates the runtime configuration.
func (c *RuntimeConfig) Validate() error {
	if c.Root == "" {
		return NewConfigValidationError("root", "root directory is required")
	}

	if c.RunRoot == "" {
		return NewConfigValidationError("run_root", "run root directory is required")
	}

	validDrivers := map[string]bool{
		"overlay":   true,
		"vfs":       true,
		"btrfs":     true,
		"zfs":       true,
		"devmapper": true,
	}

	if c.GraphDriverName != "" && !validDrivers[c.GraphDriverName] {
		return NewConfigValidationError("graph_driver", "invalid storage driver: "+c.GraphDriverName)
	}

	return nil
}

// Runtime provides an embedded container runtime for trusted computing. It
// runs in-process: runc libcontainer creates and runs containers,
// go.podman.io/image/v5 pulls images and go.podman.io/storage unpacks their
// layers into each container's root filesystem. No binary is executed.
type Runtime struct {
	config      *RuntimeConfig
	logger      *slog.Logger
	backends    *backends
	containers  sync.Map // map[string]*containerState
	idCounter   atomic.Uint64
	initialized atomic.Bool
	closed      atomic.Bool
}

// containerState tracks runtime state of a container. mu guards the fields
// the init process's waiter updates when it exits: PID, State, ExitCode,
// StartedAt and FinishedAt.
type containerState struct {
	ID         string
	Name       string
	Image      string
	BundleDir  string
	PID        int
	State      ContainerState
	ExitCode   int
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	Config     *ContainerConfig

	mu sync.Mutex

	// init is the init process specification; exec reuses its environment,
	// working directory and user.
	init processSpec

	// process is the running init process, nil before start.
	process containerProcess

	// exited is closed once the init process has exited and its exit code
	// is recorded. It is nil before start.
	exited chan struct{}
}

// snapshot returns a copy of the state that is safe to read while the
// container runs.
func (s *containerState) snapshot() *containerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &containerState{
		ID:         s.ID,
		Name:       s.Name,
		Image:      s.Image,
		BundleDir:  s.BundleDir,
		PID:        s.PID,
		State:      s.State,
		ExitCode:   s.ExitCode,
		CreatedAt:  s.CreatedAt,
		StartedAt:  s.StartedAt,
		FinishedAt: s.FinishedAt,
		Config:     s.Config,
	}
}

// RuntimeOption configures a Runtime.
type RuntimeOption func(*Runtime)

// WithRuntimeLogger sets the logger for the runtime.
func WithRuntimeLogger(logger *slog.Logger) RuntimeOption {
	return func(r *Runtime) {
		r.logger = logger
	}
}

// NewRuntime creates a new embedded container runtime.
func NewRuntime(ctx context.Context, config *RuntimeConfig, opts ...RuntimeOption) (*Runtime, error) {
	if config == nil {
		config = DefaultRuntimeConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	r := &Runtime{
		config: config,
	}

	for _, opt := range opts {
		opt(r)
	}

	if r.logger == nil {
		r.logger = slog.Default()
	}

	// Ensure directories exist
	if err := r.ensureDirectories(); err != nil {
		return nil, &RuntimeError{Operation: "init", Err: err}
	}

	if r.backends == nil {
		b, err := newDefaultBackends(config)
		if err != nil {
			return nil, &RuntimeError{Operation: "init backends", Err: err}
		}
		r.backends = b
	}

	r.initialized.Store(true)

	r.logger.Info("embedded container runtime initialized",
		"root", config.Root,
		"graph_driver", config.GraphDriverName,
	)

	return r, nil
}

// ensureDirectories creates required directories if they don't exist.
func (r *Runtime) ensureDirectories() error {
	dirs := []string{
		r.config.Root,
		r.config.RunRoot,
		r.config.TmpDir,
		r.config.StaticDir,
		r.config.VolumePath,
		r.config.ImageStorePath,
		filepath.Join(r.config.RunRoot, "bundles"),
	}

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil { // #nosec G301 -- container storage roots that libcontainer and non-root container users traverse.
			return &WrappedError{Msg: fmt.Sprintf("failed to create directory %s", dir), Err: err}
		}
	}

	return nil
}

// Close shuts down the runtime.
func (r *Runtime) Close() error {
	if r.closed.Swap(true) {
		return nil
	}

	r.initialized.Store(false)

	if r.backends != nil && r.backends.close != nil {
		if err := r.backends.close(); err != nil {
			return &RuntimeError{Operation: "close", Err: err}
		}
	}

	r.logger.Info("embedded container runtime shut down")
	return nil
}

// Config returns the runtime configuration.
func (r *Runtime) Config() *RuntimeConfig {
	return r.config
}

// IsInitialized returns true if the runtime is initialized.
func (r *Runtime) IsInitialized() bool {
	return r.initialized.Load()
}

// generateContainerID generates a unique container ID.
func (r *Runtime) generateContainerID() string {
	id := r.idCounter.Add(1)
	timestamp := time.Now().UnixNano()
	data := fmt.Sprintf("%d-%d-%d", id, timestamp, os.Getpid())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:64]
}

// CreateContainer creates a new container.
func (r *Runtime) CreateContainer(ctx context.Context, config *ContainerConfig) (*containerState, error) {
	if config == nil {
		return nil, ErrInvalidContainerConfig
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	// The image must already be in local storage, and a pinned reference
	// must still resolve to the pinned digest.
	image, err := r.backends.images.Inspect(ctx, config.Image)
	if err != nil {
		return nil, NewImageError(config.Image, "create", err)
	}
	if err := verifyPinnedDigest(config.Image, image.Digests); err != nil {
		return nil, NewImageError(config.Image, "create", err)
	}

	// Generate container ID
	id := r.generateContainerID()

	r.logger.Info("creating container", "name", config.Name, "image", config.Image, "id", id)

	bundleDir := filepath.Join(r.config.RunRoot, "bundles", id)
	if err := os.MkdirAll(bundleDir, 0700); err != nil {
		return nil, &RuntimeError{Operation: "create bundle", Err: err}
	}

	state, err := r.createInBundle(ctx, id, bundleDir, image.ID, config)
	if err != nil {
		if rmErr := os.RemoveAll(bundleDir); rmErr != nil {
			r.logger.Warn("failed to remove bundle after failed create", "id", id, "error", rmErr)
		}
		return nil, err
	}

	// Store container state
	r.containers.Store(id, state)
	r.containers.Store(config.Name, state)

	r.logger.Info("created container", "name", config.Name, "id", id)
	return state, nil
}

// createInBundle unpacks the image into the container's root filesystem,
// writes the OCI configuration into the bundle and registers the container
// with the engine. On failure it removes the root filesystem it created.
func (r *Runtime) createInBundle(ctx context.Context, id, bundleDir, imageID string, config *ContainerConfig) (*containerState, error) {
	rootfsDir, err := r.backends.rootfs.Create(ctx, id, imageID)
	if err != nil {
		return nil, &RuntimeError{Operation: "unpack rootfs", Err: err}
	}

	state, err := r.configureContainer(id, bundleDir, rootfsDir, config)
	if err != nil {
		if rmErr := r.backends.rootfs.Remove(id); rmErr != nil {
			r.logger.Warn("failed to remove rootfs after failed create", "id", id, "error", rmErr)
		}
		return nil, err
	}
	return state, nil
}

// configureContainer builds the OCI configuration for an unpacked root
// filesystem, records it in the bundle and creates the container.
func (r *Runtime) configureContainer(id, bundleDir, rootfsDir string, config *ContainerConfig) (*containerState, error) {
	processUser, err := parseUserSpec(config.User, rootfsDir)
	if err != nil {
		return nil, &RuntimeError{Operation: "resolve user", Err: err}
	}

	oci := r.generateOCIConfig(config, rootfsDir)
	oci.Process.User = processUser

	configData, err := json.MarshalIndent(oci, "", "  ")
	if err != nil {
		return nil, &RuntimeError{Operation: "marshal config", Err: err}
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "config.json"), configData, 0600); err != nil {
		return nil, &RuntimeError{Operation: "write config", Err: err}
	}

	if err := r.backends.engine.Create(id, oci); err != nil {
		return nil, &RuntimeError{Operation: "create container", Err: err}
	}

	return &containerState{
		ID:        id,
		Name:      config.Name,
		Image:     config.Image,
		BundleDir: bundleDir,
		State:     ContainerStateCreated,
		CreatedAt: time.Now(),
		Config:    config,
		init: processSpec{
			Args: oci.Process.Args,
			Env:  oci.Process.Env,
			Cwd:  oci.Process.Cwd,
			User: processUser,
		},
	}, nil
}

// ociConfig represents a minimal OCI runtime specification.
type ociConfig struct {
	OCIVersion string      `json:"ociVersion"`
	Process    *ociProcess `json:"process"`
	Root       *ociRoot    `json:"root"`
	Hostname   string      `json:"hostname,omitempty"`
	Mounts     []ociMount  `json:"mounts,omitempty"`
	Linux      *ociLinux   `json:"linux,omitempty"`
}

type ociProcess struct {
	Terminal        bool             `json:"terminal"`
	User            ociUser          `json:"user"`
	Args            []string         `json:"args"`
	Env             []string         `json:"env,omitempty"`
	Cwd             string           `json:"cwd"`
	Capabilities    *ociCapabilities `json:"capabilities,omitempty"`
	Rlimits         []ociRlimit      `json:"rlimits,omitempty"`
	NoNewPrivileges bool             `json:"noNewPrivileges"`
}

type ociUser struct {
	UID            uint32   `json:"uid"`
	GID            uint32   `json:"gid"`
	AdditionalGids []uint32 `json:"additionalGids,omitempty"`
}

type ociCapabilities struct {
	Bounding    []string `json:"bounding,omitempty"`
	Effective   []string `json:"effective,omitempty"`
	Inheritable []string `json:"inheritable,omitempty"`
	Permitted   []string `json:"permitted,omitempty"`
	Ambient     []string `json:"ambient,omitempty"`
}

type ociRlimit struct {
	Type string `json:"type"`
	Hard uint64 `json:"hard"`
	Soft uint64 `json:"soft"`
}

type ociRoot struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly"`
}

type ociMount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type"`
	Source      string   `json:"source"`
	Options     []string `json:"options,omitempty"`
}

type ociLinux struct {
	Namespaces    []ociNamespace   `json:"namespaces,omitempty"`
	Resources     *ociResources    `json:"resources,omitempty"`
	Devices       []ociLinuxDevice `json:"devices,omitempty"`
	CgroupsPath   string           `json:"cgroupsPath,omitempty"`
	MaskedPaths   []string         `json:"maskedPaths,omitempty"`
	ReadonlyPaths []string         `json:"readonlyPaths,omitempty"`
}

type ociNamespace struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

type ociResources struct {
	Memory *ociMemory `json:"memory,omitempty"`
	CPU    *ociCPU    `json:"cpu,omitempty"`
	Pids   *ociPids   `json:"pids,omitempty"`
}

type ociMemory struct {
	Limit       *int64 `json:"limit,omitempty"`
	Reservation *int64 `json:"reservation,omitempty"`
	Swap        *int64 `json:"swap,omitempty"`
}

type ociCPU struct {
	Shares *uint64 `json:"shares,omitempty"`
	Quota  *int64  `json:"quota,omitempty"`
	Period *uint64 `json:"period,omitempty"`
}

type ociPids struct {
	Limit int64 `json:"limit"`
}

type ociLinuxDevice struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Major int64  `json:"major"`
	Minor int64  `json:"minor"`
}

// generateOCIConfig generates an OCI runtime config from ContainerConfig.
func (r *Runtime) generateOCIConfig(config *ContainerConfig, rootfsPath string) *ociConfig {
	// Build environment variables
	var env []string
	env = append(env,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM=xterm")
	for k, v := range config.Environment {
		env = append(env, k+"="+v)
	}

	// Build args
	args := config.Command
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}

	// Working directory
	cwd := config.WorkDir
	if cwd == "" {
		cwd = "/"
	}

	oci := &ociConfig{
		OCIVersion: "1.0.2",
		Process: &ociProcess{
			Terminal:        false,
			User:            ociUser{UID: 0, GID: 0},
			Args:            args,
			Env:             env,
			Cwd:             cwd,
			NoNewPrivileges: !config.Privileged,
		},
		Root: &ociRoot{
			Path:     rootfsPath,
			Readonly: config.ReadOnly,
		},
		Hostname: config.Hostname,
		Linux: &ociLinux{
			Namespaces: []ociNamespace{
				{Type: "pid"},
				{Type: "network"},
				{Type: "ipc"},
				{Type: "uts"},
				{Type: "mount"},
			},
			MaskedPaths: []string{
				"/proc/acpi",
				"/proc/kcore",
				"/proc/keys",
				"/proc/latency_stats",
				"/proc/timer_list",
				"/proc/timer_stats",
				"/proc/sched_debug",
				"/sys/firmware",
			},
			ReadonlyPaths: []string{
				"/proc/asound",
				"/proc/bus",
				"/proc/fs",
				"/proc/irq",
				"/proc/sys",
				"/proc/sysrq-trigger",
			},
		},
	}

	// Add default mounts
	oci.Mounts = []ociMount{
		{Destination: "/proc", Type: "proc", Source: "proc"},
		{Destination: "/dev", Type: "tmpfs", Source: "tmpfs", Options: []string{"nosuid", "strictatime", "mode=755", "size=65536k"}},
		{Destination: "/dev/pts", Type: "devpts", Source: "devpts", Options: []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"}},
		{Destination: "/dev/shm", Type: "tmpfs", Source: "shm", Options: []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"}},
		{Destination: "/dev/mqueue", Type: "mqueue", Source: "mqueue", Options: []string{"nosuid", "noexec", "nodev"}},
		{Destination: "/sys", Type: "sysfs", Source: "sysfs", Options: []string{"nosuid", "noexec", "nodev", "ro"}},
		{Destination: "/sys/fs/cgroup", Type: "cgroup", Source: "cgroup", Options: []string{"nosuid", "noexec", "nodev", "relatime", "ro"}},
	}

	// Add user-specified mounts
	for _, m := range config.Mounts {
		opts := m.Options
		if m.ReadOnly {
			opts = append(opts, "ro")
		}
		oci.Mounts = append(oci.Mounts, ociMount{
			Destination: m.Target,
			Type:        m.Type,
			Source:      m.Source,
			Options:     opts,
		})
	}

	// Add the TPM device when the container's own configuration grants it. There
	// is no host-level default: see the note on RuntimeConfig.
	if config.TPMAccess {
		oci.Linux.Devices = append(oci.Linux.Devices, ociLinuxDevice{
			Path:  TPMDevicePath,
			Type:  "c",
			Major: 10,
			Minor: 224,
		})
	}

	// Add resource limits
	if config.Resources != nil {
		oci.Linux.Resources = &ociResources{}
		if config.Resources.MemoryLimit > 0 {
			oci.Linux.Resources.Memory = &ociMemory{
				Limit: &config.Resources.MemoryLimit,
			}
			if config.Resources.MemoryReservation > 0 {
				oci.Linux.Resources.Memory.Reservation = &config.Resources.MemoryReservation
			}
			if config.Resources.MemorySwap > 0 {
				oci.Linux.Resources.Memory.Swap = &config.Resources.MemorySwap
			}
		}
		if config.Resources.CPUShares > 0 || config.Resources.CPUQuota > 0 {
			oci.Linux.Resources.CPU = &ociCPU{}
			if config.Resources.CPUShares > 0 {
				shares := uint64(config.Resources.CPUShares)
				oci.Linux.Resources.CPU.Shares = &shares
			}
			if config.Resources.CPUQuota > 0 {
				oci.Linux.Resources.CPU.Quota = &config.Resources.CPUQuota
			}
			if config.Resources.CPUPeriod > 0 {
				period := uint64(config.Resources.CPUPeriod)
				oci.Linux.Resources.CPU.Period = &period
			}
		}
		if config.Resources.PidsLimit > 0 {
			oci.Linux.Resources.Pids = &ociPids{
				Limit: config.Resources.PidsLimit,
			}
		}
	}

	// Handle capabilities
	if !config.Privileged {
		defaultCaps := []string{
			"CAP_CHOWN",
			"CAP_DAC_OVERRIDE",
			"CAP_FSETID",
			"CAP_FOWNER",
			"CAP_MKNOD",
			"CAP_NET_RAW",
			"CAP_SETGID",
			"CAP_SETUID",
			"CAP_SETFCAP",
			"CAP_SETPCAP",
			"CAP_NET_BIND_SERVICE",
			"CAP_SYS_CHROOT",
			"CAP_KILL",
			"CAP_AUDIT_WRITE",
		}
		oci.Process.Capabilities = &ociCapabilities{
			Bounding:    defaultCaps,
			Effective:   defaultCaps,
			Inheritable: defaultCaps,
			Permitted:   defaultCaps,
			Ambient:     defaultCaps,
		}

		// Add extra capabilities
		for _, cap := range config.CapAdd {
			capName := cap
			if !strings.HasPrefix(capName, "CAP_") {
				capName = "CAP_" + capName
			}
			oci.Process.Capabilities.Bounding = append(oci.Process.Capabilities.Bounding, capName)
			oci.Process.Capabilities.Effective = append(oci.Process.Capabilities.Effective, capName)
			oci.Process.Capabilities.Permitted = append(oci.Process.Capabilities.Permitted, capName)
		}

		// Remove dropped capabilities
		for _, cap := range config.CapDrop {
			capName := cap
			if !strings.HasPrefix(capName, "CAP_") {
				capName = "CAP_" + capName
			}
			oci.Process.Capabilities.Bounding = removeString(oci.Process.Capabilities.Bounding, capName)
			oci.Process.Capabilities.Effective = removeString(oci.Process.Capabilities.Effective, capName)
			oci.Process.Capabilities.Permitted = removeString(oci.Process.Capabilities.Permitted, capName)
		}
	}

	return oci
}

// removeString removes a string from a slice.
func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

// StartContainer starts a container's init process. Its stdout and stderr
// go to stdout.log and stderr.log in the container's bundle.
func (r *Runtime) StartContainer(ctx context.Context, id string) error {
	state, err := r.lookupContainer(id)
	if err != nil {
		return err
	}

	r.logger.Info("starting container", "id", id, "name", state.Name)

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.State == ContainerStateRunning {
		return NewContainerError(id, "start", ErrContainerAlreadyRunning)
	}
	if state.process != nil {
		return NewContainerError(id, "start", ErrContainerExited)
	}

	logs, err := openContainerLogs(state.BundleDir)
	if err != nil {
		return NewContainerError(id, "start", err)
	}

	init := state.init
	init.Stdout = logs.stdout
	init.Stderr = logs.stderr

	process, err := r.backends.engine.Start(state.ID, init)
	if err != nil {
		if closeErr := logs.close(); closeErr != nil {
			r.logger.Warn("failed to close container logs", "id", id, "error", closeErr)
		}
		r.logger.Error("failed to start container", "id", id, "error", err)
		return NewContainerError(id, "start", err)
	}

	now := time.Now()
	state.process = process
	state.PID = process.Pid()
	state.State = ContainerStateRunning
	state.StartedAt = &now
	state.exited = make(chan struct{})

	go r.awaitExit(state, process, logs)

	r.logger.Info("started container", "id", id, "pid", state.PID)
	return nil
}

// awaitExit waits for a container's init process, records its exit code and
// closes the state's exited channel.
func (r *Runtime) awaitExit(state *containerState, process containerProcess, logs *containerLogs) {
	exitCode, err := process.Wait()
	if err != nil {
		r.logger.Error("failed to wait for container init", "id", state.ID, "error", err)
		exitCode = -1
	}
	if closeErr := logs.close(); closeErr != nil {
		r.logger.Warn("failed to close container logs", "id", state.ID, "error", closeErr)
	}

	now := time.Now()
	state.mu.Lock()
	state.ExitCode = exitCode
	state.State = ContainerStateExited
	state.PID = 0
	state.FinishedAt = &now
	exited := state.exited
	state.mu.Unlock()

	close(exited)
	r.logger.Info("container exited", "id", state.ID, "exit_code", exitCode)
}

// StopContainer stops a running container: SIGTERM, then SIGKILL once
// timeout seconds pass without the init process exiting.
func (r *Runtime) StopContainer(ctx context.Context, id string, timeout uint) error {
	state, err := r.lookupContainer(id)
	if err != nil {
		return err
	}

	r.logger.Info("stopping container", "id", id, "timeout", timeout)

	state.mu.Lock()
	process, exited := state.process, state.exited
	state.mu.Unlock()

	if process != nil {
		if err := stopProcess(ctx, process, exited, time.Duration(timeout)*time.Second); err != nil {
			return NewContainerError(id, "stop", err)
		}
	}

	now := time.Now()
	state.mu.Lock()
	state.State = ContainerStateStopped
	if state.FinishedAt == nil {
		state.FinishedAt = &now
	}
	state.mu.Unlock()

	r.logger.Info("stopped container", "id", id)
	return nil
}

// stopProcess sends SIGTERM and waits up to grace for exited to close, then
// sends SIGKILL and waits for it again.
func stopProcess(ctx context.Context, process containerProcess, exited <-chan struct{}, grace time.Duration) error {
	select {
	case <-exited:
		return nil
	default:
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return &RuntimeError{Operation: "signal SIGTERM", Err: err}
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case <-exited:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}

	if err := process.Signal(syscall.SIGKILL); err != nil {
		return &RuntimeError{Operation: "signal SIGKILL", Err: err}
	}

	select {
	case <-exited:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RemoveContainer removes a container: its engine state, its root
// filesystem and its bundle. A running container is refused unless force is
// set, in which case it is stopped first.
func (r *Runtime) RemoveContainer(ctx context.Context, id string, force bool) error {
	state, err := r.lookupContainer(id)
	if err != nil {
		return err
	}

	r.logger.Info("removing container", "id", id, "force", force)

	state.mu.Lock()
	running := state.State == ContainerStateRunning
	state.mu.Unlock()

	if running {
		if !force {
			return NewContainerError(id, "remove", ErrContainerRunning)
		}
		if err := r.StopContainer(ctx, id, forcedRemoveStopTimeout); err != nil {
			return err
		}
	}

	if err := r.backends.engine.Destroy(state.ID); err != nil {
		return NewContainerError(id, "remove", err)
	}
	if err := r.backends.rootfs.Remove(state.ID); err != nil {
		return NewContainerError(id, "remove", err)
	}
	if state.BundleDir != "" {
		if err := os.RemoveAll(state.BundleDir); err != nil {
			return NewContainerError(id, "remove", err)
		}
	}

	// Remove from state maps
	r.containers.Delete(state.ID)
	r.containers.Delete(state.Name)

	r.logger.Info("removed container", "id", id)
	return nil
}

// forcedRemoveStopTimeout is the grace period, in seconds, a forced remove
// gives a running container before killing it.
const forcedRemoveStopTimeout = 5

// InspectContainer returns container details.
func (r *Runtime) InspectContainer(ctx context.Context, id string) (*containerState, error) {
	state, err := r.lookupContainer(id)
	if err != nil {
		return nil, err
	}
	return state.snapshot(), nil
}

// ListContainers returns all containers.
func (r *Runtime) ListContainers(ctx context.Context, all bool) ([]*containerState, error) {
	var result []*containerState
	seen := make(map[string]bool)

	r.containers.Range(func(key, value interface{}) bool {
		state, ok := value.(*containerState)
		if !ok || seen[state.ID] {
			return true
		}
		seen[state.ID] = true

		snapshot := state.snapshot()
		if all || snapshot.State == ContainerStateRunning {
			result = append(result, snapshot)
		}
		return true
	})

	return result, nil
}

// WaitContainer waits for a started container to exit and returns its exit
// code.
func (r *Runtime) WaitContainer(ctx context.Context, id string) (int32, error) {
	state, err := r.lookupContainer(id)
	if err != nil {
		return -1, err
	}

	state.mu.Lock()
	exited := state.exited
	state.mu.Unlock()

	if exited == nil {
		return -1, NewContainerError(id, "wait", ErrContainerNotRunning)
	}

	select {
	case <-exited:
	case <-ctx.Done():
		return -1, ctx.Err()
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	return int32(state.ExitCode), nil // #nosec G115 -- exit codes are 0-255, or -1 for an unknown status.
}

// Logs returns what a started container's init process has written to
// stdout and stderr.
func (r *Runtime) Logs(id string) (stdout, stderr []byte, err error) {
	state, err := r.lookupContainer(id)
	if err != nil {
		return nil, nil, err
	}

	stdout, err = readContainerLog(filepath.Join(state.BundleDir, stdoutLogName))
	if err != nil {
		return nil, nil, NewContainerError(id, "logs", err)
	}
	stderr, err = readContainerLog(filepath.Join(state.BundleDir, stderrLogName))
	if err != nil {
		return nil, nil, NewContainerError(id, "logs", err)
	}
	return stdout, stderr, nil
}

// lookupContainer finds a container by ID or name.
func (r *Runtime) lookupContainer(id string) (*containerState, error) {
	if value, ok := r.containers.Load(id); ok {
		if state, isState := value.(*containerState); isState {
			return state, nil
		}
	}
	return nil, NewContainerError(id, "lookup", ErrContainerNotFound)
}

// PullImage pulls an image from its registry into local storage, honouring
// the signature policy. A reference pinned by digest must resolve to that
// digest.
func (r *Runtime) PullImage(ctx context.Context, imageRef string) error {
	if imageRef == "" {
		return NewImageError("", "pull", ErrImageNotFound)
	}

	r.logger.Info("pulling image", "image", imageRef)

	image, err := r.backends.images.Pull(ctx, imageRef)
	if err != nil {
		return NewImageError(imageRef, "pull", errors.Join(ErrImagePullFailed, err))
	}
	if err := verifyPinnedDigest(imageRef, image.Digests); err != nil {
		return NewImageError(imageRef, "pull", err)
	}

	r.logger.Info("pulled image", "image", imageRef, "id", image.ID)
	return nil
}

// RemoveImage removes an image from local storage.
func (r *Runtime) RemoveImage(ctx context.Context, imageRef string, force bool) error {
	if imageRef == "" {
		return NewImageError("", "remove", ErrImageNotFound)
	}

	if err := r.backends.images.Remove(ctx, imageRef, force); err != nil {
		return NewImageError(imageRef, "remove", err)
	}

	r.logger.Info("removed image", "image", imageRef)
	return nil
}

// verifyPinnedDigest checks that an image reference pinned by digest
// ("name@sha256:...") resolved to an image carrying that digest. An unpinned
// reference passes.
func verifyPinnedDigest(imageRef string, digests []string) error {
	_, pinned, isPinned := orchestration.SplitImageReference(imageRef)
	if !isPinned {
		return nil
	}
	if err := orchestration.ValidateImageDigest(pinned); err != nil {
		return err
	}
	if slices.Contains(digests, pinned) {
		return nil
	}

	actual := ""
	if len(digests) > 0 {
		actual = digests[0]
	}
	return &orchestration.ImageDigestMismatchError{Image: imageRef, Expected: pinned, Actual: actual}
}

// RuntimeError represents errors from the embedded runtime.
type RuntimeError struct {
	Operation string
	Err       error
}

// Error returns the error message.
func (e *RuntimeError) Error() string {
	if e.Err != nil {
		return "runtime " + e.Operation + ": " + e.Err.Error()
	}
	return "runtime " + e.Operation
}

// Unwrap returns the underlying error.
func (e *RuntimeError) Unwrap() error {
	return e.Err
}

// parseUserSpec resolves a container user ("name", "uid", "name:group",
// "uid:gid") against the passwd and group files inside rootfs, the way an OCI
// runtime does. Numeric IDs need no entry; names must have one. An empty spec
// is root.
func parseUserSpec(spec, rootfs string) (ociUser, error) {
	if spec == "" {
		return ociUser{}, nil
	}

	passwdPath, err := securejoin.SecureJoin(rootfs, "/etc/passwd")
	if err != nil {
		return ociUser{}, err
	}
	groupPath, err := securejoin.SecureJoin(rootfs, "/etc/group")
	if err != nil {
		return ociUser{}, err
	}

	execUser, err := user.GetExecUserPath(spec, &user.ExecUser{}, passwdPath, groupPath)
	if err != nil {
		return ociUser{}, err
	}

	uid, err := toContainerID(execUser.Uid)
	if err != nil {
		return ociUser{}, err
	}
	gid, err := toContainerID(execUser.Gid)
	if err != nil {
		return ociUser{}, err
	}
	resolved := ociUser{UID: uid, GID: gid}
	for _, sgid := range execUser.Sgids {
		id, err := toContainerID(sgid)
		if err != nil {
			return ociUser{}, err
		}
		resolved.AdditionalGids = append(resolved.AdditionalGids, id)
	}
	return resolved, nil
}

// toContainerID converts a resolved user or group ID to the OCI uint32 form.
func toContainerID(id int) (uint32, error) {
	if id < 0 || uint64(id) > math.MaxUint32 {
		return 0, ErrInvalidUser
	}
	return uint32(id), nil
}
