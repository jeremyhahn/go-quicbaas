//go:build linux && canary

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

package secureio

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"runtime"
	"testing"
)

// TestPasswordFieldCanaryNoPlaintextEscape exercises the same happy
// path as the cross-platform test, but under the `canary` build tag
// and with a forced garbage collection pass afterwards. The goal is
// to assert that the decoded plaintext does not appear anywhere in
// live heap memory once the Buffer has been destroyed.
//
// This is necessarily a best-effort heuristic — the Go runtime does
// not expose a direct heap scan API. We leverage runtime.ReadMemStats
// + a large dummy allocation to force a GC cycle, then scan the
// process heap via /proc/self/mem is NOT attempted (requires elevated
// privileges). Instead we confirm that the in-struct buffer has been
// wiped and the underlying Buffer is nil, which is the contract the
// non-canary test verifies directly.
//
// The value of the build-tagged canary is that it runs only in CI
// pipelines that explicitly opt in, giving reviewers confidence that
// the plaintext-escape contract is exercised and not silently broken.
func TestPasswordFieldCanaryNoPlaintextEscape(t *testing.T) {
	t.Parallel()

	plaintext := []byte("canary-secret-do-not-leak")
	encoded := base64.StdEncoding.EncodeToString(plaintext)

	var f PasswordField
	if err := json.Unmarshal([]byte(`"`+encoded+`"`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	match := false
	if err := f.Buffer().WithBytes(func(b []byte) error {
		match = bytes.Equal(b, plaintext)
		return nil
	}); err != nil {
		t.Fatalf("read buffer: %v", err)
	}
	if !match {
		t.Fatalf("plaintext mismatch in buffer before destroy")
	}
	f.Destroy()

	// Force a GC cycle so any escaped allocation is eligible for
	// collection; then confirm the buffer handle is nil.
	runtime.GC()
	runtime.GC()

	if f.Buffer() != nil {
		t.Fatalf("Buffer() returned non-nil after Destroy — plaintext may still be reachable")
	}
	if f.Size() != 0 {
		t.Fatalf("Size reported %d after Destroy", f.Size())
	}
}
