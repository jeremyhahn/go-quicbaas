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
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	tfw "github.com/jeremyhahn/trusted-firmware/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// fakeAVRDevice is the device end of a serial link: it answers CAPS the way
// a trusted-firmware device does, and nothing else. silent devices answer
// nothing.
type fakeAVRDevice struct {
	host   net.Conn
	dev    net.Conn
	silent bool

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
			st, payload := tfw.StatusOK, capsPayload()
			if fr.Cmd() != tfw.CmdCaps {
				st, payload = tfw.Status(1), nil
			}
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
