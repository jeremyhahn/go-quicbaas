package orchestration

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// PrivilegeClassBroker is the privilege class of a service that brokers host
// resources to the fleet: the boot node and its NBD export
// (docs/service-plane/architecture.md). It is the only class that may be
// handed host block devices, which is how a broker reaches the raw
// go-blockstore volumes it exports.
const PrivilegeClassBroker = "broker"

// ErrDevicePassthroughDenied reports a spec that asks for host devices outside
// the broker privilege class.
var ErrDevicePassthroughDenied = errors.New("orchestration: device passthrough needs the broker privilege class")

// ContainerDevice passes one host device node into the container (--device).
type ContainerDevice struct {
	// HostPath is the absolute device node on the host, e.g. /dev/nbd0.
	HostPath string `json:"host_path"`

	// ContainerPath is where the device appears in the container. Empty means
	// HostPath.
	ContainerPath string `json:"container_path,omitempty"`

	// Permissions is the cgroup access the container gets: a subset of "rwm".
	// Empty means the runtime default, "rwm".
	Permissions string `json:"permissions,omitempty"`
}

// RunArg renders the device as the --device flag's value.
func (d ContainerDevice) RunArg() string {
	arg := d.HostPath
	if d.ContainerPath != "" || d.Permissions != "" {
		target := d.ContainerPath
		if target == "" {
			target = d.HostPath
		}
		arg += ":" + target
	}
	if d.Permissions != "" {
		arg += ":" + d.Permissions
	}
	return arg
}

// Validate checks the paths are absolute and clean and the permissions are a
// subset of "rwm".
func (d ContainerDevice) Validate() error {
	paths := []string{d.HostPath}
	if d.ContainerPath != "" {
		paths = append(paths, d.ContainerPath)
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.Contains(p, ":") {
			return fmt.Errorf("device path %q must be absolute, clean and contain no colon", p)
		}
	}
	for _, c := range d.Permissions {
		if !strings.ContainsRune("rwm", c) || strings.Count(d.Permissions, string(c)) > 1 {
			return fmt.Errorf("device permissions %q must be a subset of \"rwm\"", d.Permissions)
		}
	}
	return nil
}

// validatePrivilegeClass checks the class is known and that only the broker
// class carries devices.
func (s *ContainerSpec) validatePrivilegeClass() error {
	switch s.PrivilegeClass {
	case "", PrivilegeClassBroker:
	default:
		return &InputValidationError{Field: "privilege_class", Message: fmt.Sprintf("unknown class %q", s.PrivilegeClass)}
	}
	if len(s.Devices) > 0 && s.PrivilegeClass != PrivilegeClassBroker {
		return fmt.Errorf("%w: %d device(s) requested in class %q", ErrDevicePassthroughDenied, len(s.Devices), s.PrivilegeClass)
	}
	for i, d := range s.Devices {
		if err := d.Validate(); err != nil {
			return &InputValidationError{Field: "devices[" + intToStr(i) + "]", Message: err.Error()}
		}
	}
	return nil
}

// ContainerDevices returns the device passthrough for the raw volumes among
// vols: each attached device node appears at its claim's Target, read-only
// ("r") when the claim is.
func ContainerDevices(vols []AttachedVolume) []ContainerDevice {
	var out []ContainerDevice
	for _, v := range vols {
		if v.Claim.FSType != "" || v.DevicePath == "" {
			continue
		}
		perms := "rw"
		if v.Claim.ReadOnly {
			perms = "r"
		}
		out = append(out, ContainerDevice{HostPath: v.DevicePath, ContainerPath: v.Claim.Target, Permissions: perms})
	}
	return out
}
