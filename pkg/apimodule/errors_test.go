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

package apimodule

import (
	"errors"
	"fmt"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestSurfaceComposingError_MessageAndCode pins the message and the
// unavailable classification every transport maps the error to.
func TestSurfaceComposingError_MessageAndCode(t *testing.T) {
	err := &SurfaceComposingError{Surface: "mcp surface"}

	want := "quicbaas module: mcp surface is not yet available (server still composing)"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got := err.ErrorCode(); got != qberrors.ErrUnavailable {
		t.Fatalf("ErrorCode() = %v, want %v", got, qberrors.ErrUnavailable)
	}
}

// TestSurfaceComposingError_ErrorsAs proves a wrapped composing error is
// still recognised by type, and that an unrelated error is not.
func TestSurfaceComposingError_ErrorsAs(t *testing.T) {
	wrapped := fmt.Errorf("authenticate: %w", &SurfaceComposingError{Surface: "authentication"})

	var target *SurfaceComposingError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As did not match a wrapped *SurfaceComposingError")
	}
	if target.Surface != "authentication" {
		t.Fatalf("Surface = %q, want %q", target.Surface, "authentication")
	}

	if errors.As(errors.New("other"), &target) {
		t.Fatal("errors.As matched an unrelated error")
	}
}
