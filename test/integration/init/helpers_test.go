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

//go:build integration

package init

import (
	"errors"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// backendConfigFile is the subset of the daemon configuration the backend
// matrix writes, decoded the way the daemon reads it.
type backendConfigFile struct {
	IAM struct {
		SPIFFETrustDomain string `yaml:"spiffe_trust_domain"`
	} `yaml:"iam"`
	Storage struct {
		Backend string `yaml:"backend"`
	} `yaml:"storage"`
}

// TestBackendMatrixStart_ReadsTheInitConfig pins the fix for #1732: the
// matrix's `quicbaasd start` must read the same config file as its init.
// On a non-durable engine start runs first-boot again, and first-boot refuses
// to issue the admin certificate without iam.spiffe_trust_domain, so a start
// with no config never boots.
func TestBackendMatrixStart_ReadsTheInitConfig(t *testing.T) {
	ports := PortBundle{REST: 8443, Unix: "/run/quicbaas-test.sock"}
	for _, backend := range matrixBackends {
		t.Run(backend, func(t *testing.T) {
			dataDir := t.TempDir()
			cfgPath, err := writeBackendConfig(dataDir, backend)
			if err != nil {
				t.Fatalf("writeBackendConfig: %v", err)
			}

			args, err := daemonStartArgs(dataDir, cfgPath, "rest", ports)
			if err != nil {
				t.Fatalf("daemonStartArgs: %v", err)
			}
			i := slices.Index(args, "--config")
			if i < 0 || i+1 >= len(args) || args[i+1] != cfgPath {
				t.Fatalf("start args %v do not pass --config %s", args, cfgPath)
			}

			// #nosec G304 - cfgPath is rooted at t.TempDir()
			raw, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			var cfg backendConfigFile
			if err := yaml.Unmarshal(raw, &cfg); err != nil {
				t.Fatalf("decode config: %v", err)
			}
			if cfg.IAM.SPIFFETrustDomain != matrixTrustDomain {
				t.Errorf("iam.spiffe_trust_domain = %q, want %q",
					cfg.IAM.SPIFFETrustDomain, matrixTrustDomain)
			}
			if cfg.Storage.Backend != backend {
				t.Errorf("storage.backend = %q, want %q", cfg.Storage.Backend, backend)
			}
		})
	}
}

// TestDaemonStartArgs_NoConfig keeps the other matrices' start command as it
// was: no config path means no --config flag.
func TestDaemonStartArgs_NoConfig(t *testing.T) {
	args, err := daemonStartArgs(t.TempDir(), "", "grpc", PortBundle{GRPC: 9443, Unix: "/run/q.sock"})
	if err != nil {
		t.Fatalf("daemonStartArgs: %v", err)
	}
	if slices.Contains(args, "--config") {
		t.Errorf("start args %v carry --config with no config path", args)
	}
	if !slices.Contains(args, "9443") {
		t.Errorf("start args %v lack the gRPC port", args)
	}
}

func TestAllocPortBundle_QUICIsTheRESTPort(t *testing.T) {
	bundle := allocPortBundle(t, "quic", t.TempDir())
	if bundle.REST == 0 {
		t.Fatalf("allocPortBundle(quic) REST port is zero")
	}
	if bundle.QUIC != bundle.REST {
		t.Errorf("allocPortBundle(quic) QUIC = %d, REST = %d; HTTP/3 is served on the REST port", bundle.QUIC, bundle.REST)
	}
}

// TestDaemonStartArgs_UnknownTransport checks a transport the matrix does
// not know is refused with a typed error rather than started.
func TestDaemonStartArgs_UnknownTransport(t *testing.T) {
	_, err := daemonStartArgs(t.TempDir(), "", "carrier-pigeon", PortBundle{})
	var unknown unknownTransportError
	if !errors.As(err, &unknown) {
		t.Fatalf("daemonStartArgs error = %v, want unknownTransportError", err)
	}
	if string(unknown) != "carrier-pigeon" {
		t.Errorf("unknownTransportError = %q, want carrier-pigeon", string(unknown))
	}
}

// TestAllocPortBundle_QUICRowIsTheRESTPort pins the QUIC row to the port the
// daemon serves it on. quicbaasd starts no QUIC listener of its own: QUIC is
// REST over HTTP/3 on the REST port. A separate QUIC port is one nothing
// binds, so the readiness probe on it is refused until the start deadline.
func TestAllocPortBundle_QUICRowIsTheRESTPort(t *testing.T) {
	quicRow := allocPortBundle(t, "quic", t.TempDir())
	if quicRow.REST == 0 || quicRow.QUIC != quicRow.REST {
		t.Errorf("quic row: QUIC port = %d, REST port = %d, want the same non-zero port",
			quicRow.QUIC, quicRow.REST)
	}

	restRow := allocPortBundle(t, "rest", t.TempDir())
	if restRow.QUIC != 0 {
		t.Errorf("rest row: QUIC port = %d, want 0", restRow.QUIC)
	}
}
