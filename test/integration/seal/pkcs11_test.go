// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && pkcs11

// pkcs11_test.go exercises the PKCS#11 HSM barrier sealing strategy end-to-end
// against a SoftHSM2 token. It is compiled only when both the `integration`
// and `pkcs11` build tags are present.
//
// The test runner image must have:
//   - The softhsm2 runtime library at SOFTHSM2_LIB (default:
//     /usr/local/lib/softhsm/libsofthsm2.so, matching the SoftHSM Dockerfile).
//   - softhsm2-util (or softhsm2) on PATH so the test can initialize the token.
//   - SOFTHSM2_CONF pointing at a writable configuration file.
//
// The test initialises a fresh SoftHSM2 token (idempotent: the token dir is
// under t.TempDir so every run gets a clean token), then boots a daemon whose
// seal.pkcs11.{library, token_label, pin, key_label} point at that token, and
// drives the init -> seal -> unseal round-trip.
package seal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// softhsmLibEnv names the env var overriding the SoftHSM2 library path.
const softhsmLibEnv = "SOFTHSM2_LIB"

// softhsmDefaultLib is the library path produced by the test/integration/docker/
// softhsm/Dockerfile (installed from source to /usr/local/lib/softhsm).
const softhsmDefaultLib = "/usr/local/lib/softhsm/libsofthsm2.so"

// pkcs11TokenLabel / pkcs11PIN / pkcs11KeyLabel are the token and key identity
// constants written during initSoftHSMToken and passed to the daemon seal config.
const (
	pkcs11TokenLabel = "quicbaas-seal-test"
	pkcs11SOPIN      = "87654321"
	pkcs11PIN        = "12345678"
	pkcs11KeyLabel   = "barrier-root"
)

// resolveSoftHSMLib returns the filesystem path to libsofthsm2.so, preferring
// the env override over the default path from the test Dockerfile.
func resolveSoftHSMLib(t *testing.T) string {
	t.Helper()
	if lib := os.Getenv(softhsmLibEnv); lib != "" {
		if _, err := os.Stat(lib); err == nil {
			return lib
		}
		t.Logf("SOFTHSM2_LIB=%s not found, falling back to default", lib)
	}
	if _, err := os.Stat(softhsmDefaultLib); err == nil {
		return softhsmDefaultLib
	}
	// Try common alternative location (Debian/Ubuntu system package).
	const altLib = "/usr/lib/softhsm/libsofthsm2.so"
	if _, err := os.Stat(altLib); err == nil {
		return altLib
	}
	t.Fatalf("libsofthsm2.so not found at %s or %s; set $%s", softhsmDefaultLib, altLib, softhsmLibEnv)
	return ""
}

// softhsmConf writes a minimal softhsm2.conf pointing tokendir at dir and
// returns its path. The SOFTHSM2_CONF env must be set to this path before any
// softhsm2-util or pkcs11-backed call.
func softhsmConf(t *testing.T, tokenDir string) string {
	t.Helper()
	confPath := filepath.Join(tokenDir, "softhsm2.conf")
	content := fmt.Sprintf("directories.tokendir = %s\nobjectstore.backend = file\nlog.level = INFO\nslots.removable = false\n", tokenDir)
	if err := os.WriteFile(confPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write softhsm2.conf: %v", err)
	}
	return confPath
}

// initSoftHSMToken initialises a fresh SoftHSM2 token in tokenDir and returns
// (confPath, tokenDir). The token is ready for PKCS#11 operations immediately
// after return. The caller is responsible for setting SOFTHSM2_CONF=confPath in
// any process that will use the token (including the daemon subprocess via env).
//
// Token identity constants (pkcs11TokenLabel, pkcs11PIN, pkcs11SOPIN) are
// package-level so they can be threaded into the daemon seal config YAML.
func initSoftHSMToken(t *testing.T) (confPath string) {
	t.Helper()
	tokenDir := t.TempDir()
	confPath = softhsmConf(t, tokenDir)

	// softhsm2-util --init-token --free --label <label> --pin <pin> --so-pin <sopin>
	cmd := exec.Command("softhsm2-util",
		"--init-token",
		"--free",
		"--label", pkcs11TokenLabel,
		"--pin", pkcs11PIN,
		"--so-pin", pkcs11SOPIN,
	) // #nosec G204 - args are test constants
	cmd.Env = append(os.Environ(), "SOFTHSM2_CONF="+confPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("softhsm2-util --init-token failed: %v\noutput:\n%s", err, string(out))
	}
	t.Logf("SoftHSM2 token initialized in %s", tokenDir)
	return confPath
}

// copySoftHSMLib copies libsofthsm2.so into the per-test temp dir and returns
// the copied path. This ensures the daemon subprocess has a stable, accessible
// library path even if the original is not world-readable in the container.
func copySoftHSMLib(t *testing.T, src string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "libsofthsm2.so")
	// #nosec G304 - src is a validated path, dst is under t.TempDir
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()
	// #nosec G304 - dst is under t.TempDir
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy library: %v", err)
	}
	return dst
}

// pkcs11ConfigYAML renders the daemon seal config for the pkcs11 strategy.
// softhsmConf is the path to the SOFTHSM2_CONF file the daemon must use to
// find the token (passed via environment); libPath points at libsofthsm2.so.
func pkcs11ConfigYAML(libPath string) string {
	return fmt.Sprintf(`seal:
  enabled: true
  strategy: pkcs11
  pkcs11:
    library: %q
    token_label: %q
    pin: %q
    key_label: %q
`, libPath, pkcs11TokenLabel, pkcs11PIN, pkcs11KeyLabel)
}

// TestPKCS11SealRoundTrip boots a daemon configured with the pkcs11 strategy
// pointing at a freshly-initialized SoftHSM2 token and asserts the
// init -> seal -> unseal round-trip recovers the barrier root key.
func TestPKCS11SealRoundTrip(t *testing.T) {
	lib := resolveSoftHSMLib(t)
	t.Logf("SoftHSM2 library: %s", lib)

	// Copy the library to a per-test path so the daemon subprocess can access
	// it independent of any volume mounts or permissions differences.
	localLib := copySoftHSMLib(t, lib)
	t.Logf("copied library to: %s", localLib)

	// Initialize the SoftHSM2 token. confPath must be passed to the daemon as
	// SOFTHSM2_CONF so the PKCS#11 backend can find the token directory.
	confPath := initSoftHSMToken(t)
	t.Logf("SoftHSM2 token initialized; SOFTHSM2_CONF=%s", confPath)

	// Patch spawnSealDaemon to export SOFTHSM2_CONF into the daemon env.
	// The harness builds the config YAML + daemon env separately; we augment
	// the config YAML with the library path and add SOFTHSM2_CONF via a custom
	// wrapper that injects the extra env var.
	cfgYAML := pkcs11ConfigYAML(localLib)
	runPKCS11SealRoundTrip(t, confPath, cfgYAML)
}

// runPKCS11SealRoundTrip is the PKCS#11-specific round-trip driver. It mirrors
// runCloudSealRoundTrip but injects SOFTHSM2_CONF into the daemon's env so the
// PKCS#11 library can locate the token directory.
//
// The SOFTHSM2_CONF injection is the only addition over the cloud harness: the
// daemon subprocess inherits a minimal PATH-only env, so without this the
// pkcs11 backend cannot find the token and Init fails with C_Initialize error.
func runPKCS11SealRoundTrip(t *testing.T, softhsmConf, cfgYAML string) {
	t.Helper()
	runCloudSealRoundTripWithEnv(t, "pkcs11", cfgYAML, []string{
		"SOFTHSM2_CONF=" + softhsmConf,
	})
}
