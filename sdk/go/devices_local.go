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

// Package quicbaas — devices_local.go provides the client-LOCAL CTAP2
// authenticator helper exposed via Security().Auth().Devices().
//
// This is deliberately NOT a transport verb. A server cannot enumerate the
// USB/HID FIDO2 authenticators that are physically attached to a remote
// client, so device discovery and per-device info are intrinsically local
// operations. The WebAuthn ceremony (Security().Auth().WebAuthn()) remains
// the single canonical server round-trip for registration and login; this
// helper only answers "what authenticators can THIS process see right now?".
package quicbaas

import (
	xkmsfido2 "github.com/jeremyhahn/go-xkms/sdk/go/fido2"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// LocalDevices is the client-LOCAL CTAP2 authenticator helper. Every method
// inspects authenticators attached to the current host and performs NO
// transport request. It wraps the go-xkms USB-HID enumerator.
type LocalDevices interface {
	// List enumerates the FIDO2/CTAP2 authenticators currently attached to
	// this host over USB-HID. It is a local operation and never contacts a
	// server. An empty slice (not an error) is returned when no authenticator
	// is present.
	List() ([]LocalDevice, error)

	// Info returns the descriptor for the locally-attached authenticator at
	// the given device path (as reported by List). It is a local operation.
	Info(devicePath string) (*LocalDevice, error)

	// Present reports whether at least one FIDO2 authenticator is attached to
	// this host. It is a convenience over List used by login auto-discovery.
	Present() (bool, error)
}

// LocalDevice describes a single locally-attached FIDO2 authenticator. The
// fields mirror what the host HID layer exposes without performing a CTAP2
// GetInfo round-trip (which would require user presence on some keys).
type LocalDevice struct {
	// Path is the host device path; it is the stable identifier used by
	// Info and by the login flow to select a specific authenticator.
	Path string `json:"path"`

	// VendorID is the USB vendor identifier.
	VendorID uint16 `json:"vendor_id"`

	// ProductID is the USB product identifier.
	ProductID uint16 `json:"product_id"`

	// Manufacturer is the device vendor name.
	Manufacturer string `json:"manufacturer"`

	// Product is the device product string.
	Product string `json:"product"`

	// SerialNumber is the device serial number, when reported.
	SerialNumber string `json:"serial_number,omitempty"`

	// Transport is the discovery transport (e.g. "usb").
	Transport string `json:"transport"`
}

// deviceLister is the narrow slice of the go-xkms FIDO2 handler that the
// local-device helper depends on: enumerate attached authenticators and
// release the underlying HID handle. Keeping the dependency this small keeps
// the helper trivially testable with an in-memory fake.
type deviceLister interface {
	ListDevices() ([]xkmsfido2.Device, error)
	Close() error
}

// localDevices is the production LocalDevices implementation. It constructs a
// fresh deviceLister per call so enumeration always reflects the current
// hardware state and no HID handle is held open between calls.
type localDevices struct {
	newLister func() (deviceLister, error)
}

// newLocalDevices returns the default LocalDevices wired to the platform
// USB-HID enumerator.
func newLocalDevices() LocalDevices {
	return &localDevices{
		newLister: func() (deviceLister, error) {
			h, err := xkmsfido2.NewHandler(nil, xkmsfido2.NewDefaultEnumerator())
			if err != nil {
				return nil, err
			}
			return h, nil
		},
	}
}

func (d *localDevices) List() ([]LocalDevice, error) {
	h, err := d.newLister()
	if err != nil {
		return nil, sdkerrors.Wrap(err, sdkerrors.CodeInternal, "devices.List",
			"failed to initialize local FIDO2 handler")
	}
	defer func() { _ = h.Close() }()

	found, err := h.ListDevices()
	if err != nil {
		return nil, sdkerrors.Wrap(err, sdkerrors.CodeInternal, "devices.List",
			"failed to enumerate local FIDO2 authenticators")
	}
	out := make([]LocalDevice, 0, len(found))
	for i := range found {
		out = append(out, fromXKMSDevice(found[i]))
	}
	return out, nil
}

func (d *localDevices) Info(devicePath string) (*LocalDevice, error) {
	if devicePath == "" {
		return nil, sdkerrors.NewInvalidInput("devices.Info", "device path is required")
	}
	list, err := d.List()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Path == devicePath {
			dev := list[i]
			return &dev, nil
		}
	}
	return nil, sdkerrors.NewNotFound("devices.Info", "no local FIDO2 authenticator at path "+devicePath)
}

func (d *localDevices) Present() (bool, error) {
	list, err := d.List()
	if err != nil {
		return false, err
	}
	return len(list) > 0, nil
}

// fromXKMSDevice projects a go-xkms fido2.Device into the SDK LocalDevice.
func fromXKMSDevice(in xkmsfido2.Device) LocalDevice {
	return LocalDevice{
		Path:         in.Path,
		VendorID:     in.VendorID,
		ProductID:    in.ProductID,
		Manufacturer: in.Manufacturer,
		Product:      in.Product,
		SerialNumber: in.SerialNumber,
		Transport:    in.Transport,
	}
}
