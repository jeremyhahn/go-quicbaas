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

package secure_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"testing"

	xkmssecure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// TestSecretRoundTrip is the case this package exists for: a credential read
// at startup, sealed for the process lifetime, and opened only for the moment
// it is used.
func TestSecretRoundTrip(t *testing.T) {
	const plaintext = "bank-api-token"

	sec, err := secure.NewSecret([]byte(plaintext))
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	t.Cleanup(sec.Destroy)

	opened, err := sec.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer opened.Destroy()

	if !bytes.Equal(opened.Bytes(), []byte(plaintext)) {
		t.Errorf("Open() = %q, want %q", opened.Bytes(), plaintext)
	}
}

// TestNewSecretWipesItsSource pins the documented contract that the caller's
// slice must not survive the call. A re-export that quietly failed to carry
// this behaviour would leave the plaintext in unmanaged heap memory while the
// caller believed it had been enclaved.
func TestNewSecretWipesItsSource(t *testing.T) {
	src := []byte("wipe-me")

	sec, err := secure.NewSecret(src)
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	t.Cleanup(sec.Destroy)

	if !bytes.Equal(src, make([]byte, len(src))) {
		t.Errorf("source slice still holds %q after NewSecret", src)
	}
}

// TestSecretFromBufferRoundTrip covers the Buffer -> Secret promotion, which
// is how a passphrase read through a provider becomes a long-lived credential.
func TestSecretFromBufferRoundTrip(t *testing.T) {
	const plaintext = "sealed-from-buffer"

	buf, err := secure.NewBufferFromBytes([]byte(plaintext))
	if err != nil {
		t.Fatalf("NewBufferFromBytes: %v", err)
	}

	sec, err := secure.NewSecretFromBuffer(buf)
	if err != nil {
		t.Fatalf("NewSecretFromBuffer: %v", err)
	}
	t.Cleanup(sec.Destroy)

	opened, err := sec.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer opened.Destroy()

	if !bytes.Equal(opened.Bytes(), []byte(plaintext)) {
		t.Errorf("Open() = %q, want %q", opened.Bytes(), plaintext)
	}
}

// TestNewBufferRejectsNonPositiveSize is the error path, and it checks the
// error's identity rather than only that one was returned: the re-exported
// error types are aliases precisely so errors.As keeps working across the
// module boundary.
func TestNewBufferRejectsNonPositiveSize(t *testing.T) {
	buf, err := secure.NewBuffer(0)
	if err == nil {
		buf.Destroy()
		t.Fatal("NewBuffer(0) returned no error")
	}

	var invalid *secure.InvalidSizeError
	if !errors.As(err, &invalid) {
		t.Errorf("errors.As(*secure.InvalidSizeError) = false, err is %T", err)
	}
}

// TestWipeClearsCallerOwnedBytes covers the one helper that operates on an
// ordinary slice, since a no-op alias would be invisible to every other test
// here.
func TestWipeClearsCallerOwnedBytes(t *testing.T) {
	b := []byte("scrub-this")
	secure.Wipe(b)

	if !bytes.Equal(b, make([]byte, len(b))) {
		t.Errorf("Wipe left %q", b)
	}
}

// TestAliasedValuesCrossTheModuleBoundary is the property that makes these
// aliases worth having rather than wrappers: a value built through the
// quicbaas SDK must be accepted by the upstream xkms API unchanged. A wrapper
// type, or an alias re-pointed at a lookalike, would not compile here.
func TestAliasedValuesCrossTheModuleBoundary(t *testing.T) {
	const plaintext = "cross-boundary"

	buf, err := secure.NewBufferFromBytes([]byte(plaintext))
	if err != nil {
		t.Fatalf("NewBufferFromBytes: %v", err)
	}

	sec, err := xkmssecure.NewSecretFromBuffer(buf)
	if err != nil {
		t.Fatalf("xkms NewSecretFromBuffer rejected a quicbaas SDK Buffer: %v", err)
	}
	t.Cleanup(sec.Destroy)

	opened, err := sec.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer opened.Destroy()

	if !bytes.Equal(opened.Bytes(), []byte(plaintext)) {
		t.Errorf("round trip = %q, want %q", opened.Bytes(), plaintext)
	}
}

// wipeAllChildEnv marks the child process TestWipeAll re-executes. WipeAll
// destroys the process sealer, so it runs where no other test can observe it.
const wipeAllChildEnv = "QUICBAAS_SECURE_WIPEALL_CHILD"

// TestWipeAll proves the deferred shutdown wipe works through the re-export:
// a live Secret becomes unopenable and every live enclave is reclaimed.
func TestWipeAll(t *testing.T) {
	if os.Getenv(wipeAllChildEnv) == "1" {
		wipeAllChild(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWipeAll$", "-test.v")
	cmd.Env = append(os.Environ(), wipeAllChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("WipeAll child failed: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("--- PASS: TestWipeAll")) {
		t.Fatalf("WipeAll child did not run its test:\n%s", out)
	}
}

func wipeAllChild(t *testing.T) {
	sec, err := secure.NewSecret([]byte("shutdown-wiped"))
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	remaining, err := secure.WipeAll()
	if err != nil {
		t.Fatalf("WipeAll: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("WipeAll left %d enclaves held", remaining)
	}
	if opened, err := sec.Open(); err == nil {
		opened.Destroy()
		t.Fatal("a Secret opened after WipeAll destroyed the process sealer")
	}
	if remaining, err := secure.WipeAll(); err != nil || remaining != 0 {
		t.Fatalf("a second WipeAll = %d, %v; want an idempotent 0, nil", remaining, err)
	}
}
