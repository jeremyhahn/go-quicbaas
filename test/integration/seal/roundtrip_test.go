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

//go:build integration

package seal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// barrierStatus is the JSON shape emitted by `barrier status --output json`
// (cmd/quicbaas/barrier.go formatBarrierStatusResponse).
type barrierStatus struct {
	Sealed         bool   `json:"sealed"`
	Strategy       string `json:"strategy"`
	HardwareBacked bool   `json:"hardware_backed"`
	InitializedAt  string `json:"initialized_at"`
}

// readStatus runs `barrier status --output json` and decodes the envelope.
func readStatus(t *testing.T, daemon *sealDaemon) barrierStatus {
	t.Helper()
	out, err := runCLI(t, daemon, "barrier", "status", "--output", "json")
	if err != nil {
		t.Fatalf("barrier status failed: %v\n%s", err, out)
	}
	// The CLI may interleave a structured log line on stderr (merged into
	// out); the JSON envelope is the last brace-balanced object. Decode the
	// trimmed final line that parses as JSON.
	var snap barrierStatus
	if jerr := json.Unmarshal(bytes.TrimSpace([]byte(lastJSONObject(out))), &snap); jerr != nil {
		t.Fatalf("decode barrier status JSON: %v\nout=%s", jerr, out)
	}
	return snap
}

// lastJSONObject extracts the final top-level {...} object from mixed CLI
// output. The barrier status envelope is always the last JSON object emitted.
func lastJSONObject(out string) string {
	start := strings.LastIndex(out, "{")
	end := strings.LastIndex(out, "}")
	if start >= 0 && end > start {
		return out[start : end+1]
	}
	return out
}

// runCloudSealRoundTrip drives the canonical cloud-strategy lifecycle against
// a freshly-spawned daemon and asserts the init -> seal -> unseal round-trip
// recovers the barrier:
//
//  1. barrier init   — the data-plane StorageBarrier seals a fresh root key
//     via the configured cloud KMS strategy. The CLI requires
//     --barrier-strategy passphrase --passphrase <x> for client-side validation;
//     the server ignores both and uses the cloud strategy the barrier was
//     built with (CloudKMSStrategy.SealRootKey does not consult creds).
//  2. status         — must report unsealed + the cloud strategy + hardware.
//  3. barrier seal   — drops the in-memory root key.
//  4. status         — must report sealed.
//  5. barrier unseal — re-fetches + decrypts the sealed root key via the
//     cloud KMS, proving the seal blob round-trips through the emulator.
//  6. status         — must report unsealed again.
//
// strategyID is the canonical qrdb StrategyID ("awskms", "vault", ...) the
// server reports in the status envelope; sealSecret is the dummy passphrase
// the CLI requires (the cloud strategy never reads it).
func runCloudSealRoundTrip(t *testing.T, strategyID, cfgYAML string) {
	runCloudSealRoundTripWithEnv(t, strategyID, cfgYAML, nil)
}

// runCloudSealRoundTripWithEnv is runCloudSealRoundTrip extended with extra
// env vars forwarded to the daemon start process. Hardware-strategy tests use
// this to inject backend-specific env (e.g. SOFTHSM2_CONF for pkcs11) that
// the base daemon env does not carry.
func runCloudSealRoundTripWithEnv(t *testing.T, strategyID, cfgYAML string, extraEnv []string) {
	t.Helper()

	daemon := spawnSealDaemonWithEnv(t, strategyID, cfgYAML, extraEnv)

	// 1. Init. The cloud strategy seals the root key via the emulator.
	if out, err := runCLI(t, daemon,
		"barrier", "init", "--barrier-strategy", "passphrase", "--passphrase", "cloud-seal-roundtrip",
	); err != nil {
		t.Fatalf("barrier init (cloud=%s) failed: %v\n%s", strategyID, err, out)
	}

	// 2. Status: unsealed, cloud strategy, hardware-backed.
	got := readStatus(t, daemon)
	if got.Sealed {
		t.Fatalf("post-init barrier reports sealed; want unsealed: %+v", got)
	}
	if got.Strategy != strategyID {
		t.Fatalf("post-init strategy=%q; want %q: %+v", got.Strategy, strategyID, got)
	}
	if !got.HardwareBacked {
		t.Fatalf("cloud strategy %s must report hardware_backed=true: %+v", strategyID, got)
	}

	// 3. Seal.
	if out, err := runCLI(t, daemon, "barrier", "seal", "--assume-yes"); err != nil {
		t.Fatalf("barrier seal failed: %v\n%s", err, out)
	}

	// 4. Status: sealed.
	if got := readStatus(t, daemon); !got.Sealed {
		t.Fatalf("post-seal barrier reports unsealed; want sealed: %+v", got)
	}

	// 5. Unseal via the cloud KMS (no passphrase needed for cloud).
	if out, err := runCLI(t, daemon, "barrier", "unseal", "--passphrase", "cloud-seal-roundtrip"); err != nil {
		t.Fatalf("barrier unseal (cloud=%s) failed: %v\n%s", strategyID, err, out)
	}

	// 6. Status: unsealed again — the seal blob round-tripped through the KMS.
	if got := readStatus(t, daemon); got.Sealed {
		t.Fatalf("post-unseal barrier reports sealed; want unsealed: %+v", got)
	}
}
