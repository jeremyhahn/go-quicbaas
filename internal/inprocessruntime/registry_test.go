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

package inprocessruntime

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// The registration is process-wide, so these tests do not run in parallel.

func TestRegistered_NilWhenNoneRegistered(t *testing.T) {
	Register(nil)
	if Registered() != nil {
		t.Fatal("expected no registered builder")
	}
}

func TestRegister_ReturnsTheRegisteredBuilder(t *testing.T) {
	var gotRoot string
	Register(func(root string, _ interfaces.Logger) (orchestration.ContainerRuntime, error) {
		gotRoot = root
		return nil, nil
	})
	t.Cleanup(func() { Register(nil) })

	build := Registered()
	if build == nil {
		t.Fatal("expected the registered builder")
	}
	if _, err := build("r", nil); err != nil {
		t.Fatalf("builder: %v", err)
	}
	if gotRoot != "r" {
		t.Fatalf("expected root %q, got %q", "r", gotRoot)
	}
}

func TestRegister_NilUnregisters(t *testing.T) {
	Register(func(string, interfaces.Logger) (orchestration.ContainerRuntime, error) {
		return nil, nil
	})
	Register(nil)
	if Registered() != nil {
		t.Fatal("expected nil to unregister the builder")
	}
}
