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

package sdlc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestEnsureSigningSocketRoot_KeepsItsSigningIdentifier: preparing the root
// again — a restarted daemon — keeps the identifier, so the agents that proved
// they reach it stay placeable without re-registering; and a root whose marker
// cannot be written refuses the daemon rather than leaving signing spawns with
// nowhere provable to go.
func TestEnsureSigningSocketRoot_KeepsItsSigningIdentifier(t *testing.T) {
	root := filepath.Join(testSocketsRoot(t), "kept")
	if _, err := EnsureSigningSocketRoot(root); err != nil {
		t.Fatalf("first: %v", err)
	}
	first, err := orchestration.ReadSigningRootID(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSigningSocketRoot(root); err != nil {
		t.Fatalf("second: %v", err)
	}
	second, err := orchestration.ReadSigningRootID(root)
	if err != nil || second != first {
		t.Fatalf("identifier %q -> %q (%v), want it kept", first, second, err)
	}

	// A marker that cannot be read or written: a DIRECTORY at the marker's
	// name. Reading it fails EISDIR and creating it fails EEXIST for every
	// uid, root included, so the refusal is staged the same everywhere.
	unmarkable := filepath.Join(testSocketsRoot(t), "unmarkable")
	if err := os.MkdirAll(filepath.Join(unmarkable, orchestration.SigningRootMarkerFile), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = EnsureSigningSocketRoot(unmarkable)
	var unusable *orchestration.SigningSocketRootUnusableError
	if !errors.As(err, &unusable) || !strings.Contains(err.Error(), "signing-root marker") {
		t.Fatalf("err = %v, want the unmarkable root refused", err)
	}
}
