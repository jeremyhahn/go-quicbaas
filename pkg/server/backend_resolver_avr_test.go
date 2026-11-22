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
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	tfw "github.com/jeremyhahn/trusted-firmware/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// fakeAVRDevice is the device end of a serial link: it answers CAPS the way
// a trusted-firmware device does, and nothing else. silent devices answer
// nothing.
type fakeAVRDevice struct {
	host   net.Conn
	dev    net.Conn
	silent bool

	// se makes the fake a secure-element device: it answers CAPS with the
	// attest feature and serves cert, page by page, to CmdAttestCert.
	se   bool
	cert []byte

	mu     sync.Mutex
	closed bool
}

func newFakeAVRDevice(t *testing.T, silent bool) *fakeAVRDevice {
	t.Helper()
	host, dev := net.Pipe()
	f := &fakeAVRDevice{host: host, dev: dev, silent: silent}
	go f.serve()
	t.Cleanup(func() { _ = dev.Close(); _ = host.Close() })
	return f
}

// capsPayload is a CAPS reply in the wire order of tfw.Caps: protocol
// major/minor, firmware major/minor/patch, board, pins, scheduler slots, max
// payload, crypto tier, features (LE u32) and algorithms (LE u16).
func capsPayload() []byte {
	return []byte{2, 0, 0, 3, 0, 1, 20, 4, 64, byte(tfw.CryptoNone), 0, 0, 0, 0, 0, 0}
}

// newSecureElementAVRDevice is a fake secure-element device whose attestation
// answer is cert, the alias certificate it hands out for any challenge.
func newSecureElementAVRDevice(t *testing.T, cert []byte) *fakeAVRDevice {
	t.Helper()
	host, dev := net.Pipe()
	f := &fakeAVRDevice{host: host, dev: dev, se: true, cert: cert}
	go f.serve()
	t.Cleanup(func() { _ = dev.Close(); _ = host.Close() })
	return f
}

// secureElementCapsPayload is the CAPS answer of a secure-element device with
// the attest feature, as go-xkms's own harness sends it.
func secureElementCapsPayload() []byte {
	p := make([]byte, 16)
	p[0], p[1] = 2, 0
	p[2], p[3], p[4] = 0, 2, 0
	p[5] = 1
	p[6] = 22
	p[7] = 8
	p[8] = tfw.MaxPayload
	p[9] = byte(tfw.CryptoSecureElement)
	binary.LittleEndian.PutUint32(p[10:14], tfw.FeatureAttest)
	binary.LittleEndian.PutUint16(p[14:16], uint16(tfw.AlgECDSAP256SHA256))
	return p
}

// avrAliasPage is the device's alias certificate page size.
const avrAliasPage = 60

// attestCertAnswer answers one CmdAttestCert request: a 32-byte payload (the
// challenge) asks for page 0, a 1-byte payload for that page.
func (f *fakeAVRDevice) attestCertAnswer(req []byte) (tfw.Status, []byte) {
	var page int
	switch len(req) {
	case 32:
		page = 0
	case 1:
		page = int(req[0])
	default:
		return tfw.StatusErrBadArgc, nil
	}
	start := page * avrAliasPage
	if start >= len(f.cert) {
		return tfw.StatusErrRange, nil
	}
	end := min(start+avrAliasPage, len(f.cert))
	out := []byte{byte(page), 0, 0}
	binary.LittleEndian.PutUint16(out[1:3], uint16(len(f.cert)))
	return tfw.StatusOK, append(out, f.cert[start:end]...)
}

// answer is the fake's reply to one command.
func (f *fakeAVRDevice) answer(fr tfw.Frame) (tfw.Status, []byte) {
	switch {
	case fr.Cmd() == tfw.CmdCaps && f.se:
		return tfw.StatusOK, secureElementCapsPayload()
	case fr.Cmd() == tfw.CmdCaps:
		return tfw.StatusOK, capsPayload()
	case fr.Cmd() == tfw.CmdAttestCert && f.se:
		return f.attestCertAnswer(fr.Payload)
	default:
		return tfw.Status(1), nil
	}
}

func (f *fakeAVRDevice) serve() {
	var buf []byte
	tmp := make([]byte, 256)
	for {
		n, err := f.dev.Read(tmp)
		if err != nil {
			return
		}
		buf = append(buf, tmp[:n]...)
		for {
			fr, used, derr := tfw.Decode(buf)
			if derr != nil || used == 0 {
				break
			}
			buf = buf[used:]
			if f.silent {
				continue
			}
			st, payload := f.answer(fr)
			resp, eerr := tfw.EncodeResponse(fr.Seq, fr.Cmd(), st, payload)
			if eerr != nil {
				return
			}
			if _, werr := f.dev.Write(resp); werr != nil {
				return
			}
		}
	}
}

// hostPort is what openAVRPort returns: the host end, recording Close.
func (f *fakeAVRDevice) hostPort() io.ReadWriteCloser { return &recordingPort{Conn: f.host, f: f} }

type recordingPort struct {
	net.Conn
	f *fakeAVRDevice
}

func (p *recordingPort) Close() error {
	p.f.mu.Lock()
	p.f.closed = true
	p.f.mu.Unlock()
	return p.Conn.Close()
}

func (f *fakeAVRDevice) portClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// withAVRPort substitutes the serial-port opener for the test.
func withAVRPort(t *testing.T, open AVRPortOpener) {
	t.Helper()
	prev := openAVRPort
	SetAVRPortOpener(open)
	t.Cleanup(func() { openAVRPort = prev })
}

func avrConfig() *config.Config {
	cfg := &config.Config{}
	cfg.KMS.AVR = config.AVRBackendConfig{Device: "/dev/ttyTFW0", RequestTimeout: 500 * time.Millisecond}
	return cfg
}

// TestBackendResolver_AVRRegistered proves the trusted-firmware adapter is
// registered in the composition: avr resolves (no BackendNotCompiledError).
func TestBackendResolver_AVRRegistered(t *testing.T) {
	r := NewBackendResolver(avrConfig())
	bt, err := r.Resolve(BackendSpec{Backend: config.BackendTypeAVR})
	if err != nil {
		t.Fatalf("Resolve(avr) = %v, want nil", err)
	}
	if bt != config.BackendTypeAVR {
		t.Fatalf("Resolve(avr) = %q, want avr", bt)
	}
}

// TestBackendResolver_AVRComposesDeviceBackend proves the resolver builds the
// go-xkms AVR backend over the device on kms.avr.device, at the default baud,
// as the ServiceBackend passed into go-xkms, and that closing it closes the
// port the composition opened.
func TestBackendResolver_AVRComposesDeviceBackend(t *testing.T) {
	dev := newFakeAVRDevice(t, false)
	var gotPath string
	var gotBaud int
	withAVRPort(t, func(path string, baud int) (io.ReadWriteCloser, error) {
		gotPath, gotBaud = path, baud
		return dev.hostPort(), nil
	})

	r := NewBackendResolver(avrConfig())
	sb, err := r.ServiceBackendWithStore(BackendSpec{Backend: config.BackendTypeAVR}, nil)
	if err != nil {
		t.Fatalf("ServiceBackendWithStore(avr) = %v, want nil", err)
	}
	if gotPath != "/dev/ttyTFW0" || gotBaud != config.DefaultAVRBaud {
		t.Fatalf("opened %q at %d, want /dev/ttyTFW0 at %d", gotPath, gotBaud, config.DefaultAVRBaud)
	}
	if got := sb.Type(); string(got) != string(xkmssdk.BackendTypeAVR) {
		t.Fatalf("ServiceBackend.Type() = %q, want %q", got, xkmssdk.BackendTypeAVR)
	}
	if err := sb.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if !dev.portClosed() {
		t.Fatal("Close() left the serial port open")
	}
}

// TestBackendResolver_AVRConstructErrors proves a missing device, an
// unopenable port and a device that never answers are typed
// BackendConstructErrors, never a silent software fallback.
func TestBackendResolver_AVRConstructErrors(t *testing.T) {
	openErr := errors.New("no such port")
	silent := newFakeAVRDevice(t, true)
	cases := map[string]struct {
		cfg  func() *config.Config
		open AVRPortOpener
	}{
		"no transport composed": {
			cfg:  avrConfig,
			open: nil,
		},
		"no device": {
			cfg: func() *config.Config { c := avrConfig(); c.KMS.AVR.Device = ""; return c },
			open: func(string, int) (io.ReadWriteCloser, error) {
				t.Fatal("port opened without a device")
				return nil, nil
			},
		},
		"port fails": {
			cfg:  avrConfig,
			open: func(string, int) (io.ReadWriteCloser, error) { return nil, openErr },
		},
		"device silent": {
			cfg:  avrConfig,
			open: func(string, int) (io.ReadWriteCloser, error) { return silent.hostPort(), nil },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withAVRPort(t, tc.open)
			_, err := NewBackendResolver(tc.cfg()).ServiceBackendWithStore(BackendSpec{Backend: config.BackendTypeAVR}, nil)
			var ce *BackendConstructError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want *BackendConstructError", err)
			}
			if ce.Backend != config.BackendTypeAVR {
				t.Fatalf("BackendConstructError.Backend = %q, want avr", ce.Backend)
			}
		})
	}
	if !silent.portClosed() {
		t.Fatal("a failed construction left the serial port open")
	}
}

// avrTestNonceHex is the 32-byte challenge the pinned alias certificate
// carries.
const avrTestNonceHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

// avrTestAliasCertHex is the frozen alias certificate the firmware's vectors
// pin (go-xkms pkg/backend/avr/vectors_test.go): CN "ag Alias", 471 bytes,
// minted for avrTestNonceHex.
const avrTestAliasCertHex = "" +
	"308201d33082017aa00302010202084001020304050607300a06082a8648ce3d" +
	"0403023016311430120603550403130b61672044657669636549443020170d32" +
	"30303130313030303030305a180f39393939313233313233353935395a301331" +
	"11300f06035504031308616720416c6961733059301306072a8648ce3d020106" +
	"082a8648ce3d03010703420004798953e7e8134fdf3c139f63d3fbccc252a28b" +
	"6ca5059e618374a81231240f3fc83267aec725e18b66176c3685d1257201a670" +
	"33819585a22a296350159ae70ba381b23081af3081ac06066781050504060101" +
	"ff04819e30819b048198a30a5820000102030405060708090a0b0c0d0e0f1011" +
	"12131415161718191a1b1c1d1e1f1901005101a1b2c3d4e5f60718293a4b5c6d" +
	"7e8f9019011181821901025855a500505eec7874aaa557fdaaaef3f9fe0ccfb4" +
	"016561672d667702a2181f62616718210103a111a20782015820ba7816bf8f01" +
	"cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad181863617070" +
	"0c00300a06082a8648ce3d040302034700304402202f79b26e954e1e7523e979" +
	"dff8630a55271b0635dec8e88feaa4f72c54aaeeff02207778dba4b957648559" +
	"9fa7b12997a9395458151c7366e50762d6d36233f5eabb"

// avrTestVectors decodes the pinned challenge and alias certificate.
func avrTestVectors(t *testing.T) (nonce, der []byte) {
	t.Helper()
	nonce, err := hex.DecodeString(avrTestNonceHex)
	if err != nil || len(nonce) != 32 {
		t.Fatalf("nonce: len %d, err %v", len(nonce), err)
	}
	der, err = hex.DecodeString(avrTestAliasCertHex)
	if err != nil || len(der) != 471 {
		t.Fatalf("alias certificate: len %d, err %v", len(der), err)
	}
	return nonce, der
}

// startAVRService composes an AVR service backend over a secure-element fake
// device that answers with der, and installs it as the go-xkms service's only
// backend.
func startAVRService(t *testing.T, der []byte) xkmssdk.ServiceBackend {
	t.Helper()
	dev := newSecureElementAVRDevice(t, der)
	withAVRPort(t, func(string, int) (io.ReadWriteCloser, error) { return dev.hostPort(), nil })

	sb, err := NewBackendResolver(avrConfig()).ServiceBackendWithStore(BackendSpec{Backend: config.BackendTypeAVR}, nil)
	if err != nil {
		t.Fatalf("ServiceBackendWithStore(avr) = %v, want nil", err)
	}

	xkms.Reset()
	t.Cleanup(xkms.Reset)
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	if err := xkms.Initialize(&xkms.Config{
		DefaultBackend: "avr",
		Backends:       map[string]xkmssdk.ServiceBackend{"avr": sb},
		Logger:         logger.NewAdapter(log),
	}); err != nil {
		t.Fatalf("xkms.Initialize: %v", err)
	}
	return sb
}

// TestBackendResolver_AVRAttestThroughService attests through the go-xkms
// service and gets the device's alias certificate back. On BASE this call
// failed with "unexpected attestation result type *avr.Attestation".
func TestBackendResolver_AVRAttestThroughService(t *testing.T) {
	nonce, der := avrTestVectors(t)
	startAVRService(t, der)

	svc, err := xkmssdk.Get()
	if err != nil {
		t.Fatalf("xkms.Get: %v", err)
	}
	resp, err := svc.AttestKey(context.Background(), &xkmssdk.AttestKeyRequest{Backend: "avr", KeyID: "avr-device-id", Nonce: nonce})
	if err != nil {
		t.Fatalf("AttestKey = %v, want nil", err)
	}
	if resp.Format != "dice" {
		t.Fatalf("Format = %q, want dice", resp.Format)
	}
	if len(resp.CertificateChain) != 1 || !bytes.Equal(resp.CertificateChain[0], der) {
		t.Fatalf("CertificateChain has %d entries, want the alias certificate only", len(resp.CertificateChain))
	}
	if !bytes.Equal(resp.AttestationData, der) {
		t.Fatal("AttestationData is not the device's alias certificate")
	}
	if !bytes.Equal(resp.Nonce, nonce) {
		t.Fatalf("Nonce = %x, want %x", resp.Nonce, nonce)
	}
	if resp.Backend != "avr" {
		t.Fatalf("Backend = %q, want avr", resp.Backend)
	}
	if resp.SignatureAlgorithm != "ECDSA-SHA256" {
		t.Fatalf("SignatureAlgorithm = %q, want ECDSA-SHA256", resp.SignatureAlgorithm)
	}
}

// TestBackendResolver_AVRRegistersNoAlgorithms checks the KMS registry gives
// the attest-only AVR backend no algorithm to route on. On BASE AVR fell into
// the defaults and registered AES, RSA, ECDSA, Ed25519 and HMAC.
func TestBackendResolver_AVRRegistersNoAlgorithms(t *testing.T) {
	_, der := avrTestVectors(t)
	startAVRService(t, der)

	f, err := kms.NewBackendFactory(&kms.BackendFactoryConfig{})
	if err != nil {
		t.Fatalf("NewBackendFactory: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if err := f.InitializeFromXKMS(context.Background(), nil); err != nil {
		t.Fatalf("InitializeFromXKMS = %v, want nil", err)
	}
	var info *kms.BackendInfo
	for _, bi := range f.Registry().List() {
		if bi.Name == "avr" {
			info = bi
		}
	}
	if info == nil {
		t.Fatal("the registry has no avr backend")
	}
	if len(info.Algorithms) != 0 {
		t.Fatalf("avr registered algorithms %v, want none", info.Algorithms)
	}
}
