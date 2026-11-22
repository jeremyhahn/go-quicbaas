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
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jeremyhahn/go-xkms/pkg/attestation"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	tfw "github.com/jeremyhahn/trusted-firmware/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the trusted-firmware device backend: go-xkms's AVR
// adapter (pkg/backend/avr, re-exported as xkmssdk.NewAVRBackend) over a
// trusted-firmware device on a serial port. It is the first device backend
// adapter of the composition (SPEC row hardware-backend-composition): the
// adapter is composed here, in QuicBaaS, and passed into go-xkms as the KMS
// ServiceBackend; go-trusted-platform and xkey never import it.
//
// The backend is tag-free: it needs no cgo and no vendor library. The device
// holds one fixed identity key, so it serves Signer/GetKey for that key and has
// no CA StoreType.
//
// The serial transport is composed by the daemon, not here: pkg/server is
// linked into the QuicBaaS SDK (its embedded server), and the SDK's consumers
// (go-trusted-platform) must not link a device transport. cmd/quicbaasd calls
// SetAVRPortOpener with trusted-firmware's serialport.Open; without it the avr
// backend fails with errAVRNoTransport.

// AVRPortOpener opens a trusted-firmware device's serial port at a baud rate.
type AVRPortOpener func(path string, baud int) (io.ReadWriteCloser, error)

// openAVRPort is the composed serial-port opener; nil until the daemon sets it.
var openAVRPort AVRPortOpener

// SetAVRPortOpener composes the serial transport of the avr backend. The
// daemon calls it once at start-up, before the KMS backend is resolved.
func SetAVRPortOpener(open AVRPortOpener) { openAVRPort = open }

// errAVRNoTransport is returned when no serial transport is composed.
var errAVRNoTransport = errors.New("the avr backend has no serial transport: this binary does not compose one (quicbaasd does)")

// errAVRNoDevice is returned when the avr backend is built without a port.
var errAVRNoDevice = errors.New("kms.avr.device is required for the avr backend")

// errAVRAliasNotDeviceSigned is returned when the alias certificate's
// signature does not verify with the DeviceID key read from the device.
var errAVRAliasNotDeviceSigned = errors.New("avr: the alias certificate is not signed by this device's DeviceID key")

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

// AttestKey attests the device key and returns the result in the form the
// go-xkms service requires. The AVR backend returns its own *AVRAttestation,
// which XKMSService.AttestKey refuses as an unexpected result type.
//
// The alias certificate is signed by the DeviceID key over its TBS bytes, so
// the statement carries the certificate's signature over RawTBSCertificate,
// with the DeviceID public key (read from the element by GetKey) as the
// attesting key and the alias key as the attested key. The certificate stays
// in the chain as the DICE evidence. The adapter verifies the pair before it
// returns and refuses an alias certificate this device did not sign.
func (p *avrKeyProvider) AttestKey(ctx context.Context, attrs *xkmssdk.KeyAttributes, nonce []byte) (interface{}, error) {
	res, err := p.AVRBackend.AttestKey(ctx, attrs, nonce)
	if err != nil {
		return nil, err
	}
	att, ok := res.(*xkmssdk.AVRAttestation)
	if !ok || att == nil || att.Certificate == nil {
		return nil, fmt.Errorf("avr: unexpected attestation result %T", res)
	}
	key, err := p.AVRBackend.GetKey(ctx, attrs)
	if err != nil {
		return nil, fmt.Errorf("avr: read the DeviceID key: %w", err)
	}
	dk, ok := key.(*xkmssdk.AVRDeviceKey)
	if !ok || dk == nil {
		return nil, fmt.Errorf("avr: unexpected device key %T", key)
	}
	pub, ok := dk.Public().(*ecdsa.PublicKey)
	if !ok || pub == nil {
		return nil, fmt.Errorf("avr: unexpected device key %T", key)
	}
	cert := att.Certificate
	stmt := &attestation.AttestationStatement{
		Format:                att.Format,
		AttestingKeyAlgorithm: x509.ECDSA,
		AttestingKeyPublic:    pub,
		Signature:             cert.Signature,
		SignatureAlgorithm:    cert.SignatureAlgorithm,
		CertificateChain:      []*x509.Certificate{cert},
		AttestedKeyPublic:     cert.PublicKey,
		AttestationData:       cert.RawTBSCertificate,
		CreatedAt:             time.Now().UTC().Format(time.RFC3339),
		Backend:               string(xkmssdk.BackendTypeAVR),
		Nonce:                 att.Nonce,
	}
	if err := attestation.NewVerifier(nil).VerifySignature(stmt, nil); err != nil {
		return nil, fmt.Errorf("%w: %w", errAVRAliasNotDeviceSigned, err)
	}
	return stmt, nil
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
	if openAVRPort == nil {
		return nil, &BackendConstructError{Backend: config.BackendTypeAVR, Cause: errAVRNoTransport}
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
