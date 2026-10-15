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

package quicbaas

import (
	"errors"
	"testing"

	xkmsfido2 "github.com/jeremyhahn/go-xkms/sdk/go/fido2"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isInternalErr reports whether err carries the SDK internal error code.
func isInternalErr(err error) bool {
	var qe *sdkerrors.QBError
	if !errors.As(err, &qe) {
		return false
	}
	return qe.Code == sdkerrors.CodeInternal
}

// fakeLister is an in-memory deviceLister used to drive the LocalDevices
// helper without touching real USB-HID hardware.
type fakeLister struct {
	devices  []xkmsfido2.Device
	listErr  error
	closed   bool
	closeErr error
}

func (f *fakeLister) ListDevices() ([]xkmsfido2.Device, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.devices, nil
}

func (f *fakeLister) Close() error {
	f.closed = true
	return f.closeErr
}

// newTestLocalDevices wires a localDevices around a single fakeLister.
func newTestLocalDevices(l *fakeLister, ctorErr error) *localDevices {
	return &localDevices{
		newLister: func() (deviceLister, error) {
			if ctorErr != nil {
				return nil, ctorErr
			}
			return l, nil
		},
	}
}

func TestLocalDevices_List_HappyPath(t *testing.T) {
	l := &fakeLister{devices: []xkmsfido2.Device{
		{Path: "/dev/hidraw0", VendorID: 0x1050, Manufacturer: "Yubico", Product: "YubiKey", SerialNumber: "abc", Transport: "usb"},
		{Path: "/dev/hidraw1", VendorID: 0x0483, Manufacturer: "SoloKeys", Product: "Solo", Transport: "usb"},
	}}
	d := newTestLocalDevices(l, nil)

	got, err := d.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(got))
	}
	if got[0].Path != "/dev/hidraw0" || got[0].Manufacturer != "Yubico" || got[0].VendorID != 0x1050 {
		t.Errorf("unexpected first device: %+v", got[0])
	}
	if got[0].Manufacturer == got[1].Manufacturer {
		t.Error("expected vendor-agnostic distinct manufacturers")
	}
	if !l.closed {
		t.Error("expected the lister to be closed after List")
	}
}

func TestLocalDevices_List_Empty(t *testing.T) {
	d := newTestLocalDevices(&fakeLister{devices: nil}, nil)
	got, err := d.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(got))
	}
}

func TestLocalDevices_List_ConstructorError(t *testing.T) {
	d := newTestLocalDevices(nil, errors.New("hidapi unavailable"))
	if _, err := d.List(); err == nil || !isInternalErr(err) {
		t.Errorf("expected Internal error, got %v", err)
	}
}

func TestLocalDevices_List_EnumerateError(t *testing.T) {
	d := newTestLocalDevices(&fakeLister{listErr: errors.New("permission denied")}, nil)
	if _, err := d.List(); err == nil || !isInternalErr(err) {
		t.Errorf("expected Internal error, got %v", err)
	}
}

func TestLocalDevices_Info_HappyPath(t *testing.T) {
	l := &fakeLister{devices: []xkmsfido2.Device{
		{Path: "/dev/hidraw0", Manufacturer: "Yubico"},
		{Path: "/dev/hidraw1", Manufacturer: "SoloKeys"},
	}}
	d := newTestLocalDevices(l, nil)

	info, err := d.Info("/dev/hidraw1")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Manufacturer != "SoloKeys" {
		t.Errorf("unexpected device: %+v", info)
	}
}

func TestLocalDevices_Info_EmptyPath(t *testing.T) {
	d := newTestLocalDevices(&fakeLister{}, nil)
	if _, err := d.Info(""); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput, got %v", err)
	}
}

func TestLocalDevices_Info_NotFound(t *testing.T) {
	d := newTestLocalDevices(&fakeLister{devices: []xkmsfido2.Device{{Path: "/dev/hidraw0"}}}, nil)
	if _, err := d.Info("/dev/nope"); err == nil || !sdkerrors.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestLocalDevices_Info_ListError(t *testing.T) {
	d := newTestLocalDevices(nil, errors.New("init failed"))
	if _, err := d.Info("/dev/hidraw0"); err == nil || !isInternalErr(err) {
		t.Errorf("expected Internal error propagated from List, got %v", err)
	}
}

func TestLocalDevices_Present(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		d := newTestLocalDevices(&fakeLister{devices: []xkmsfido2.Device{{Path: "/dev/hidraw0"}}}, nil)
		ok, err := d.Present()
		if err != nil {
			t.Fatalf("Present: %v", err)
		}
		if !ok {
			t.Error("expected Present to report true")
		}
	})
	t.Run("absent", func(t *testing.T) {
		d := newTestLocalDevices(&fakeLister{}, nil)
		ok, err := d.Present()
		if err != nil {
			t.Fatalf("Present: %v", err)
		}
		if ok {
			t.Error("expected Present to report false")
		}
	})
	t.Run("error", func(t *testing.T) {
		d := newTestLocalDevices(nil, errors.New("boom"))
		if _, err := d.Present(); err == nil || !isInternalErr(err) {
			t.Errorf("expected Internal error, got %v", err)
		}
	})
}

// TestNewLocalDevices_DefaultWiring verifies the production constructor wires
// the platform enumerator: the lister factory is set and constructs a handler.
//
// IT DOES NOT LIST. Enumerating authenticators reads the host's hidraw
// devices, which a unit test may not do: whether that read succeeds depends on
// the machine (it failed on the CI runner with "failed to read hidraw devices"
// and passed on a workstation), and the unit test contract is that the host
// is never touched. What is asserted is the wiring; what the wiring returns
// is the host's business.
func TestNewLocalDevices_DefaultWiring(t *testing.T) {
	d := newLocalDevices()
	if d == nil {
		t.Fatal("expected non-nil LocalDevices")
	}
	wired, ok := d.(*localDevices)
	if !ok || wired.newLister == nil {
		t.Fatalf("the default wiring must carry a lister factory, got %T", d)
	}
	lister, err := wired.newLister()
	if err != nil {
		t.Fatalf("constructing the platform lister must not touch the host: %v", err)
	}
	if lister == nil {
		t.Fatal("the platform lister factory returned nil")
	}
}
