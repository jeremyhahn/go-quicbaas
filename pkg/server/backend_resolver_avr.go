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

package server

import (
	"context"
	"errors"
	"io"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	tfw "github.com/jeremyhahn/trusted-firmware/sdk/go"
	"github.com/jeremyhahn/trusted-firmware/sdk/go/serialport"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the trusted-firmware device backend: go-xkms's AVR
// adapter (pkg/backend/avr, re-exported as xkmssdk.NewAVRBackend) over a
// trusted-firmware device on a serial port. It is the first device backend
// adapter of the composition (SPEC row hardware-backend-composition): the
// adapter is composed here, in QuicBaaS, and passed into go-xkms as the KMS
// ServiceBackend; go-trusted-platform and xkey never import it.
//
// The backend is tag-free: it needs no cgo and no vendor library, only the
// serial port named by kms.avr.device. The device holds one fixed identity
// key, so it serves Signer/GetKey for that key and has no CA StoreType.

// openAVRPort opens the device's serial port. A test seam: tests substitute a
// fake device.
var openAVRPort = serialport.Open

// errAVRNoDevice is returned when the avr backend is built without a port.
var errAVRNoDevice = errors.New("kms.avr.device is required for the avr backend")

// avrKeyProvider is the go-xkms AVR backend plus the serial port QuicBaaS
// opened for it. go-xkms's backend does not own its device, so the port's
// lifetime is the composition's: Close closes the backend, then the port.
type avrKeyProvider struct {
	*xkmssdk.AVRBackend
	port io.Closer
}

// Close closes the backend and the serial port under it.
func (p *avrKeyProvider) Close() error {
	return errors.Join(p.AVRBackend.Close(), p.port.Close())
}

// buildAVRServiceBackend opens cfg.KMS.AVR.Device, connects go-xkms's AVR
// backend to the device on it and wraps it as the ServiceBackend. keyStore is
// unused: the key lives on the device and nowhere else.
func buildAVRServiceBackend(cfg *config.Config, _ BackendSpec, _ xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	ac := cfg.KMS.AVR
	if ac.Device == "" {
		return nil, &BackendConstructError{Backend: config.BackendTypeAVR, Cause: errAVRNoDevice}
	}
	baud := ac.Baud
	if baud == 0 {
		baud = config.DefaultAVRBaud
	}
	port, err := openAVRPort(ac.Device, baud)
	if err != nil {
		return nil, &BackendConstructError{Backend: config.BackendTypeAVR, Cause: err}
	}

	backendCfg := xkmssdk.DefaultAVRConfig()
	if ac.KeyCN != "" {
		backendCfg.KeyCN = ac.KeyCN
	}
	if ac.RequestTimeout > 0 {
		backendCfg.RequestTimeout = ac.RequestTimeout
	}
	backend, err := xkmssdk.NewAVRBackend(context.Background(), backendCfg, tfw.New(port))
	if err != nil {
		_ = port.Close()
		return nil, &BackendConstructError{Backend: config.BackendTypeAVR, Cause: err}
	}
	sb, err := wrapServiceBackend(config.BackendTypeAVR, &avrKeyProvider{AVRBackend: backend, port: port})
	if err != nil {
		_ = backend.Close()
		_ = port.Close()
		return nil, err
	}
	return sb, nil
}

// init registers the avr factory unconditionally (tag-free backend).
func init() {
	registerBackendFactory(config.BackendTypeAVR, buildAVRServiceBackend)
}
